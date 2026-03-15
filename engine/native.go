package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"github.com/megawron/lok8s/types"
)

type nativeProc struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	phase  types.PodPhase
}

type NativeEngine struct {
	mu    sync.RWMutex
	procs map[string]*nativeProc
}

func NewNativeEngine() *NativeEngine {
	return &NativeEngine{
		procs: make(map[string]*nativeProc),
	}
}

func (e *NativeEngine) Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, stdout, stderr io.Writer) error {
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("binary not found: %w", err)
	}

	e.mu.RLock()
	existing, exists := e.procs[pod.Metadata.Name]
	e.mu.RUnlock()
	if exists && existing.phase == types.PodRunning {
		return fmt.Errorf("pod %q already running", pod.Metadata.Name)
	}

	procCtx, cancel := context.WithCancel(ctx)

	var args []string
	if len(pod.Spec.Containers) > 0 {
		c := pod.Spec.Containers[0]
		if len(c.Command) > 1 {
			args = append(args, c.Command[1:]...)
		}
		args = append(args, c.Args...)
	}

	cmd := exec.CommandContext(procCtx, target, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = buildOSEnv(env)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start %q: %w", target, err)
	}

	np := &nativeProc{
		cmd:    cmd,
		cancel: cancel,
		phase:  types.PodRunning,
	}

	e.mu.Lock()
	e.procs[pod.Metadata.Name] = np
	e.mu.Unlock()

	log.Printf("[native] pod %q started (pid %d)", pod.Metadata.Name, cmd.Process.Pid)

	go func() {
		err := cmd.Wait()

		e.mu.Lock()
		defer e.mu.Unlock()

		if p, ok := e.procs[pod.Metadata.Name]; ok {
			if err != nil {
				p.phase = types.PodFailed
				log.Printf("[native] pod %q exited with error: %v", pod.Metadata.Name, err)
			} else {
				p.phase = types.PodSucceeded
				log.Printf("[native] pod %q exited successfully", pod.Metadata.Name)
			}
		}
	}()

	return nil
}

func (e *NativeEngine) Stop(podName string) error {
	e.mu.RLock()
	p, ok := e.procs[podName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pod %q not found", podName)
	}

	p.cancel()
	return nil
}

func (e *NativeEngine) Status(podName string) types.PodPhase {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if p, ok := e.procs[podName]; ok {
		return p.phase
	}
	return types.PodPending
}

func buildOSEnv(vars []types.EnvVar) []string {
	base := os.Environ()
	for _, v := range vars {
		base = append(base, v.Name+"="+v.Value)
	}
	return base
}
