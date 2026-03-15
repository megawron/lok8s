package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/megawron/lok8s/logs"
	"github.com/megawron/lok8s/types"
)

const maxBackoff = 5 * time.Minute

type managedPod struct {
	pod        types.Pod
	engineType string
	target     string
	env        []types.EnvVar
	cancel     context.CancelFunc

	logBuffer *logs.RingBuffer
	stdout    io.Writer
	stderr    io.Writer

	mu           sync.Mutex
	restartCount int
	ready        bool
}

type LifecycleManager struct {
	registry *Registry
	pods     sync.Map
}

func NewLifecycleManager(registry *Registry) *LifecycleManager {
	return &LifecycleManager{registry: registry}
}

func (lm *LifecycleManager) Launch(pod *types.Pod, engineType, target string, env []types.EnvVar) error {
	key := podKey(pod.Metadata.Namespace, pod.Metadata.Name)

	if _, loaded := lm.pods.Load(key); loaded {
		return fmt.Errorf("pod %q already managed", key)
	}

	ctx, cancel := context.WithCancel(context.Background())

	logBuf := logs.NewRingBuffer(logs.DefaultCapacity)
	prefix := fmt.Sprintf("[%s] ", pod.Metadata.Name)
	termOut := logs.NewPrefixWriter(os.Stdout, prefix)
	termErr := logs.NewPrefixWriter(os.Stderr, prefix)
	stdout := io.MultiWriter(logBuf, termOut)
	stderr := io.MultiWriter(logBuf, termErr)

	mp := &managedPod{
		pod:        *pod,
		engineType: engineType,
		target:     target,
		env:        env,
		cancel:     cancel,
		logBuffer:  logBuf,
		stdout:     stdout,
		stderr:     stderr,
	}

	lm.pods.Store(key, mp)

	if len(pod.Spec.InitContainers) > 0 {
		if err := lm.runInitContainers(ctx, pod.Metadata.Name, pod.Spec.InitContainers, env, stdout, stderr); err != nil {
			lm.pods.Delete(key)
			cancel()
			return fmt.Errorf("init containers failed: %w", err)
		}
	}

	eng, err := lm.registry.Get(engineType)
	if err != nil {
		lm.pods.Delete(key)
		cancel()
		return err
	}

	if err := eng.Start(ctx, pod, target, env, stdout, stderr); err != nil {
		lm.pods.Delete(key)
		cancel()
		return err
	}

	mp.mu.Lock()
	mp.ready = true
	mp.mu.Unlock()

	go lm.monitor(ctx, mp, key)

	return nil
}

func (lm *LifecycleManager) Terminate(namespace, name string) error {
	key := podKey(namespace, name)

	val, ok := lm.pods.LoadAndDelete(key)
	if !ok {
		return fmt.Errorf("pod %s/%s not managed", namespace, name)
	}

	mp := val.(*managedPod)
	mp.cancel()

	if eng, err := lm.registry.Get(mp.engineType); err == nil {
		eng.Stop(name)
	}

	return nil
}

func (lm *LifecycleManager) Status(namespace, name string) (types.PodStatus, bool) {
	key := podKey(namespace, name)

	val, ok := lm.pods.Load(key)
	if !ok {
		return types.PodStatus{}, false
	}

	mp := val.(*managedPod)

	eng, err := lm.registry.Get(mp.engineType)
	if err != nil {
		return types.PodStatus{Phase: types.PodFailed, Message: err.Error()}, true
	}

	phase := eng.Status(name)

	mp.mu.Lock()
	restartCount := mp.restartCount
	ready := mp.ready
	mp.mu.Unlock()

	status := types.PodStatus{
		Phase:        phase,
		RestartCount: restartCount,
		StartTime:    mp.pod.Status.StartTime,
	}

	if len(mp.pod.Spec.Containers) > 0 {
		cs := types.ContainerStatus{
			Name:         mp.pod.Spec.Containers[0].Name,
			Ready:        ready && phase == types.PodRunning,
			RestartCount: restartCount,
			State:        string(phase),
		}
		status.ContainerStatuses = []types.ContainerStatus{cs}
	}

	return status, true
}

func (lm *LifecycleManager) Logs(namespace, name string) (*logs.RingBuffer, bool) {
	key := podKey(namespace, name)

	val, ok := lm.pods.Load(key)
	if !ok {
		return nil, false
	}

	mp := val.(*managedPod)
	return mp.logBuffer, true
}

type probeSet struct {
	cancel context.CancelFunc
}

func (lm *LifecycleManager) newProbeSet(parent context.Context, mp *managedPod, eng PodEngine) *probeSet {
	ctx, cancel := context.WithCancel(parent)
	lm.startProbes(ctx, mp, eng)
	return &probeSet{cancel: cancel}
}

func (ps *probeSet) stop() {
	if ps != nil && ps.cancel != nil {
		ps.cancel()
	}
}

func (lm *LifecycleManager) monitor(ctx context.Context, mp *managedPod, key string) {
	eng, err := lm.registry.Get(mp.engineType)
	if err != nil {
		return
	}

	probes := lm.newProbeSet(ctx, mp, eng)
	defer probes.stop()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			phase := eng.Status(mp.pod.Metadata.Name)
			if phase != types.PodSucceeded && phase != types.PodFailed {
				continue
			}

			probes.stop()

			if !lm.shouldRestart(&mp.pod, phase) {
				return
			}

			mp.mu.Lock()
			mp.restartCount++
			count := mp.restartCount
			mp.ready = false
			mp.mu.Unlock()

			backoff := lm.backoffDuration(count)
			log.Printf("[lifecycle] pod %q exited (%s), restarting in %s (attempt %d)",
				mp.pod.Metadata.Name, phase, backoff, count)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}

			if err := eng.Start(ctx, &mp.pod, mp.target, mp.env, mp.stdout, mp.stderr); err != nil {
				log.Printf("[lifecycle] restart failed for pod %q: %v", mp.pod.Metadata.Name, err)
				return
			}

			mp.mu.Lock()
			mp.ready = true
			mp.mu.Unlock()

			probes = lm.newProbeSet(ctx, mp, eng)

			log.Printf("[lifecycle] pod %q restarted successfully", mp.pod.Metadata.Name)
		}
	}
}

func (lm *LifecycleManager) startProbes(ctx context.Context, mp *managedPod, eng PodEngine) {
	if len(mp.pod.Spec.Containers) == 0 {
		return
	}

	c := mp.pod.Spec.Containers[0]

	if c.LivenessProbe != nil {
		runner := NewProbeRunner(*c.LivenessProbe, mp.pod.Metadata.Name, "liveness",
			func() {
				log.Printf("[lifecycle] liveness failed for pod %q, killing process", mp.pod.Metadata.Name)
				eng.Stop(mp.pod.Metadata.Name)
			},
			nil,
		)
		go runner.Run(ctx)
	}

	if c.ReadinessProbe != nil {
		runner := NewProbeRunner(*c.ReadinessProbe, mp.pod.Metadata.Name, "readiness",
			func() {
				mp.mu.Lock()
				mp.ready = false
				mp.mu.Unlock()
			},
			func() {
				mp.mu.Lock()
				mp.ready = true
				mp.mu.Unlock()
			},
		)
		go runner.Run(ctx)
	}
}

func (lm *LifecycleManager) shouldRestart(pod *types.Pod, phase types.PodPhase) bool {
	policy := pod.Spec.RestartPolicy
	if policy == "" {
		policy = types.RestartAlways
	}

	switch policy {
	case types.RestartAlways:
		return true
	case types.RestartOnFailure:
		return phase == types.PodFailed
	case types.RestartNever:
		return false
	default:
		return false
	}
}

func (lm *LifecycleManager) backoffDuration(restartCount int) time.Duration {
	d := time.Duration(10<<uint(restartCount-1)) * time.Second
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

func (lm *LifecycleManager) runInitContainers(ctx context.Context, podName string, containers []types.Container, env []types.EnvVar, stdout, stderr io.Writer) error {
	for _, ic := range containers {
		if len(ic.Command) == 0 {
			continue
		}

		var args []string
		args = append(args, ic.Command[1:]...)
		args = append(args, ic.Args...)

		cmd := exec.CommandContext(ctx, ic.Command[0], args...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Env = buildOSEnv(env)

		for _, e := range ic.Env {
			cmd.Env = append(cmd.Env, e.Name+"="+e.Value)
		}

		log.Printf("[init] pod %q running init container %q", podName, ic.Name)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("init container %q: %w", ic.Name, err)
		}
		log.Printf("[init] pod %q init container %q completed", podName, ic.Name)
	}
	return nil
}

func podKey(namespace, name string) string {
	return namespace + "/" + name
}
