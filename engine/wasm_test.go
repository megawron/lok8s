package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/megawron/lok8s/types"
)

func TestWasmEngine_Minimal(t *testing.T) {
	// 1. Create a temp directory and write a minimal valid WASM binary
	tmpDir, err := os.MkdirTemp("", "lok8s-wasm-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wasmPath := filepath.Join(tmpDir, "minimal.wasm")
	// Minimal WebAssembly binary module (\x00asm\x01\x00\x00\x00)
	minimalWasmBytes := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, minimalWasmBytes, 0644); err != nil {
		t.Fatalf("failed to write wasm file: %v", err)
	}

	// 2. Initialize WasmEngine
	eng := NewWasmEngine()

	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name:      "test-wasm-pod",
			Namespace: "default",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 3. Start WASM execution
	err = eng.Start(ctx, pod, wasmPath, nil, nil, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("failed to start wasm execution: %v", err)
	}

	// 4. Poll status until completed (Succeeded)
	start := time.Now()
	timeout := 5 * time.Second
	for {
		phase := eng.Status("test-wasm-pod")
		if phase == types.PodSucceeded {
			t.Log("Wasm module completed execution successfully")
			break
		}
		if phase == types.PodFailed {
			t.Fatal("Wasm module failed execution")
		}

		if time.Since(start) > timeout {
			t.Fatal("Timeout waiting for Wasm module completion")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
