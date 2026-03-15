package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/megawron/lok8s/types"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type wasmProc struct {
	cancel  context.CancelFunc
	runtime wazero.Runtime
	phase   types.PodPhase
}

type WasmEngine struct {
	mu    sync.RWMutex
	procs map[string]*wasmProc
}

func NewWasmEngine() *WasmEngine {
	return &WasmEngine{
		procs: make(map[string]*wasmProc),
	}
}

func (e *WasmEngine) Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, stdout, stderr io.Writer) error {
	wasmBytes, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read wasm module: %w", err)
	}

	e.mu.RLock()
	existing, exists := e.procs[pod.Metadata.Name]
	e.mu.RUnlock()
	if exists && existing.phase == types.PodRunning {
		return fmt.Errorf("pod %q already running", pod.Metadata.Name)
	}

	modCtx, cancel := context.WithCancel(ctx)

	wp := &wasmProc{
		cancel: cancel,
		phase:  types.PodRunning,
	}

	e.mu.Lock()
	e.procs[pod.Metadata.Name] = wp
	e.mu.Unlock()

	log.Printf("[wasm] pod %q starting module %s", pod.Metadata.Name, target)

	go func() {
		phase := e.runModule(modCtx, pod.Metadata.Name, wasmBytes, env, wp, stdout, stderr)

		e.mu.Lock()
		if p, ok := e.procs[pod.Metadata.Name]; ok {
			p.phase = phase
		}
		e.mu.Unlock()
	}()

	return nil
}

func (e *WasmEngine) runModule(ctx context.Context, podName string, wasmBytes []byte, env []types.EnvVar, wp *wasmProc, stdout, stderr io.Writer) types.PodPhase {
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wp.runtime = rt

	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	config := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr).
		WithArgs(podName)

	for _, v := range env {
		config = config.WithEnv(v.Name, v.Value)
	}

	_, err := rt.InstantiateWithConfig(ctx, wasmBytes, config)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[wasm] pod %q cancelled", podName)
			return types.PodFailed
		}
		log.Printf("[wasm] pod %q failed: %v", podName, err)
		return types.PodFailed
	}

	log.Printf("[wasm] pod %q completed", podName)
	return types.PodSucceeded
}

func (e *WasmEngine) Stop(podName string) error {
	e.mu.RLock()
	p, ok := e.procs[podName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pod %q not found", podName)
	}

	p.cancel()
	return nil
}

func (e *WasmEngine) Status(podName string) types.PodPhase {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if p, ok := e.procs[podName]; ok {
		return p.phase
	}
	return types.PodPending
}
