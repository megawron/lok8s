package manifest

import (
	"os"
	"testing"

	"github.com/megawron/lok8s/types"
)

func TestExtractEngineConfig_Annotations(t *testing.T) {
	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name: "test-pod",
			Annotations: map[string]string{
				"lok8s.io/executable-path": "/usr/bin/python3",
			},
		},
	}

	eng, target, err := ExtractEngineConfig(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng != EngineNative || target != "/usr/bin/python3" {
		t.Errorf("expected native/python3, got %s/%s", eng, target)
	}

	podWasm := &types.Pod{
		Metadata: types.ObjectMeta{
			Name: "test-pod-wasm",
			Annotations: map[string]string{
				"lok8s.io/wasm-module": "./module.wasm",
			},
		},
	}

	engWasm, targetWasm, err := ExtractEngineConfig(podWasm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engWasm != EngineWasm || targetWasm != "./module.wasm" {
		t.Errorf("expected wasm/module.wasm, got %s/%s", engWasm, targetWasm)
	}
}

func TestExtractEngineConfig_FallbackNative(t *testing.T) {
	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name: "test-fallback-native",
		},
		Spec: types.PodSpec{
			Containers: []types.Container{
				{
					Name:  "web",
					Image: "nginx:latest",
				},
			},
		},
	}

	eng, target, err := ExtractEngineConfig(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng != EngineNative || target != "nginx" {
		t.Errorf("expected native/nginx fallback, got %s/%s", eng, target)
	}
}

func TestExtractEngineConfig_FallbackWasm(t *testing.T) {
	// Create a temp .wasm file to simulate a local wasm candidate
	tmpDir, err := os.MkdirTemp("", "lok8s-parser-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	// Create wasm candidate file
	wasmFile := "my-wasm-app.wasm"
	if err := os.WriteFile(wasmFile, []byte("wasm-bytecode"), 0644); err != nil {
		t.Fatal(err)
	}

	pod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name: "test-fallback-wasm",
		},
		Spec: types.PodSpec{
			Containers: []types.Container{
				{
					Name:  "worker",
					Image: "my-wasm-app:v1.2",
				},
			},
		},
	}

	eng, target, err := ExtractEngineConfig(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng != EngineWasm {
		t.Errorf("expected wasm fallback, got %s/%s", eng, target)
	}
}
