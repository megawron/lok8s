package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (e *NativeEngine) Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, volumes map[string]string, stdout, stderr io.Writer) error {
	resolvedTarget, err := resolveExecutable(target)
	if err != nil {
		return err
	}

	e.mu.RLock()
	existing, exists := e.procs[pod.Metadata.Name]
	e.mu.RUnlock()
	if exists && existing.phase == types.PodRunning {
		return fmt.Errorf("pod %q already running", pod.Metadata.Name)
	}

	// Project volume mounts for native process
	if len(pod.Spec.Containers) > 0 {
		c := pod.Spec.Containers[0]
		for _, vm := range c.VolumeMounts {
			hostDir, ok := volumes[vm.Name]
			if !ok {
				continue
			}
			mountNativeVolume(hostDir, vm.MountPath)

			envVarName := "LOK8S_VOLUME_" + strings.ToUpper(vm.Name)
			envVarName = strings.ReplaceAll(envVarName, "-", "_")
			env = append(env, types.EnvVar{Name: envVarName, Value: hostDir})
		}
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

	cmd := exec.CommandContext(procCtx, resolvedTarget, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = buildOSEnv(env)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start %q (resolved: %q): %w", target, resolvedTarget, err)
	}

	np := &nativeProc{
		cmd:    cmd,
		cancel: cancel,
		phase:  types.PodRunning,
	}

	e.mu.Lock()
	e.procs[pod.Metadata.Name] = np
	e.mu.Unlock()

	log.Printf("[native] pod %q started (pid %d, resolved target: %q)", pod.Metadata.Name, cmd.Process.Pid, resolvedTarget)

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

func mountNativeVolume(hostDir, mountPath string) {
	parent := filepath.Dir(mountPath)
	_ = os.MkdirAll(parent, 0755)

	_ = os.RemoveAll(mountPath)

	err := os.Symlink(hostDir, mountPath)
	if err == nil {
		return
	}

	// Fallback to copy dir if symlink fails (e.g. permission restriction on Windows)
	_ = copyDir(hostDir, mountPath)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, entry.Type().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveExecutable(target string) (string, error) {
	// 1. Check if target is a direct absolute or relative path that exists
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}

	// 2. Check if it's in the current directory (e.g. "./target")
	localPath := "./" + target
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// 2a. On Windows, check for ".exe" suffix
	localPathExe := "./" + target + ".exe"
	if _, err := os.Stat(localPathExe); err == nil {
		return localPathExe, nil
	}

	targetExe := target + ".exe"
	if _, err := os.Stat(targetExe); err == nil {
		return targetExe, nil
	}

	// 3. Check in a "./bin" folder
	binPath := filepath.Join(".", "bin", target)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}
	binPathExe := filepath.Join(".", "bin", target+".exe")
	if _, err := os.Stat(binPathExe); err == nil {
		return binPathExe, nil
	}

	// 4. Try looking up in system PATH
	pathLook, err := exec.LookPath(target)
	if err == nil {
		return pathLook, nil
	}

	return "", fmt.Errorf("executable %q not found in current dir, ./bin/, or system PATH. (Note: lok8s runs workloads as native host processes. Ensure %q is built/compiled locally, installed on your host system, or running in an external container/cluster)", target, target)
}
