package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/megawron/lok8s/logs"
	"github.com/megawron/lok8s/network"
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
	hostPort     int
	proxy        *network.Proxy
}

type LifecycleManager struct {
	registry *Registry
	portPool *network.PortPool
	pods     sync.Map
}

func NewLifecycleManager(registry *Registry, portPool *network.PortPool) *LifecycleManager {
	return &LifecycleManager{
		registry: registry,
		portPool: portPool,
	}
}

func (lm *LifecycleManager) Launch(pod *types.Pod, engineType, target string, env []types.EnvVar) error {
	key := podKey(pod.Metadata.Namespace, pod.Metadata.Name)

	if _, loaded := lm.pods.Load(key); loaded {
		return fmt.Errorf("pod %q already managed", key)
	}

	// 1. Allocate port if pod has ports defined
	var hostPort int
	var err error
	if len(pod.Spec.Containers) > 0 && len(pod.Spec.Containers[0].Ports) > 0 {
		hostPort, err = lm.portPool.Allocate(key)
		if err != nil {
			return fmt.Errorf("failed to allocate port: %w", err)
		}
	}

	// 2. Inject LOK8S_PORT environment variable if a port was allocated
	if hostPort > 0 {
		env = append(env, types.EnvVar{
			Name:  "LOK8S_PORT",
			Value: fmt.Sprintf("%d", hostPort),
		})
		pod.Status.HostPort = hostPort
		pod.Status.PodIP = "127.0.0.1"
	}

	containerPort := 0
	if val, ok := pod.Metadata.Annotations["lok8s.io/container-port"]; ok {
		var parseErr error
		containerPort, parseErr = strconv.Atoi(val)
		if parseErr != nil {
			if hostPort > 0 {
				lm.portPool.Release(key)
			}
			return fmt.Errorf("invalid lok8s.io/container-port annotation: %v", parseErr)
		}
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
		hostPort:   hostPort,
	}

	lm.pods.Store(key, mp)

	if len(pod.Spec.InitContainers) > 0 {
		if err := lm.runInitContainers(ctx, pod.Metadata.Name, pod.Spec.InitContainers, env, stdout, stderr); err != nil {
			if hostPort > 0 {
				lm.portPool.Release(key)
			}
			lm.pods.Delete(key)
			cancel()
			return fmt.Errorf("init containers failed: %w", err)
		}
	}

	eng, err := lm.registry.Get(engineType)
	if err != nil {
		if hostPort > 0 {
			lm.portPool.Release(key)
		}
		lm.pods.Delete(key)
		cancel()
		return err
	}

	if err := eng.Start(ctx, pod, target, env, stdout, stderr); err != nil {
		if hostPort > 0 {
			lm.portPool.Release(key)
		}
		lm.pods.Delete(key)
		cancel()
		return err
	}

	// Start Pod proxy if hostPort > 0 and containerPort > 0 and different
	if hostPort > 0 && containerPort > 0 && hostPort != containerPort {
		proxy := network.NewProxy(fmt.Sprintf("127.0.0.1:%d", hostPort), fmt.Sprintf("127.0.0.1:%d", containerPort))
		if err := proxy.Start(); err != nil {
			log.Printf("[lifecycle] Failed to start proxy for pod %q: %v", pod.Metadata.Name, err)
		} else {
			mp.mu.Lock()
			mp.proxy = proxy
			mp.mu.Unlock()
		}
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

	mp.mu.Lock()
	if mp.proxy != nil {
		mp.proxy.Close()
	}
	mp.mu.Unlock()

	if mp.hostPort > 0 {
		lm.portPool.Release(key)
	}

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
	hostPort := mp.hostPort
	mp.mu.Unlock()

	status := types.PodStatus{
		Phase:        phase,
		RestartCount: restartCount,
		StartTime:    mp.pod.Status.StartTime,
		HostPort:     hostPort,
	}
	if hostPort > 0 {
		status.PodIP = "127.0.0.1"
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

func (lm *LifecycleManager) ListActivePods() []types.Pod {
	var result []types.Pod
	lm.pods.Range(func(key, value any) bool {
		mp := value.(*managedPod)
		
		status, _ := lm.Status(mp.pod.Metadata.Namespace, mp.pod.Metadata.Name)
		
		podCopy := mp.pod
		podCopy.Status = status
		result = append(result, podCopy)
		return true
	})
	return result
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
