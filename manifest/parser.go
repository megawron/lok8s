package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/megawron/lok8s/types"
	"gopkg.in/yaml.v3"
)

const (
	AnnotationExecutablePath = "lok8s.io/executable-path"
	AnnotationWasmModule     = "lok8s.io/wasm-module"

	EngineNative = "native"
	EngineWasm   = "wasm"
)

func Parse(data []byte) (*types.Pod, error) {
	if len(data) == 0 {
		return nil, errors.New("empty manifest")
	}

	pod := &types.Pod{}

	if isJSON(data) {
		if err := json.Unmarshal(data, pod); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, pod); err != nil {
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
	}

	if pod.Metadata.Name == "" {
		return nil, errors.New("metadata.name is required")
	}

	return pod, nil
}

func ExtractEngineConfig(pod *types.Pod) (engineType string, target string, err error) {
	annotations := pod.Metadata.Annotations

	execPath, hasExec := annotations[AnnotationExecutablePath]
	wasmPath, hasWasm := annotations[AnnotationWasmModule]

	if hasExec && hasWasm {
		return "", "", fmt.Errorf(
			"pod %q has both %s and %s; pick one",
			pod.Metadata.Name, AnnotationExecutablePath, AnnotationWasmModule,
		)
	}
	if hasExec {
		if execPath == "" {
			return "", "", fmt.Errorf("annotation %s is empty", AnnotationExecutablePath)
		}
		return EngineNative, execPath, nil
	}
	if hasWasm {
		if wasmPath == "" {
			return "", "", fmt.Errorf("annotation %s is empty", AnnotationWasmModule)
		}
		return EngineWasm, wasmPath, nil
	}

	// Fallback to Image name if annotations are missing
	if len(pod.Spec.Containers) > 0 {
		image := pod.Spec.Containers[0].Image
		if image != "" {
			parts := strings.Split(image, "/")
			lastPart := parts[len(parts)-1]
			imageName := strings.Split(lastPart, ":")[0]

			// Check if imageName has .wasm extension
			if strings.HasSuffix(imageName, ".wasm") {
				return EngineWasm, imageName, nil
			}

			// Candidates for wasm modules locally
			wasmCandidates := []string{
				imageName + ".wasm",
				"./" + imageName + ".wasm",
				filepath.Join(".", "bin", imageName+".wasm"),
			}
			for _, c := range wasmCandidates {
				if _, err := os.Stat(c); err == nil {
					return EngineWasm, c, nil
				}
			}

			// Otherwise default to running natively by searching host executable matching imageName
			return EngineNative, imageName, nil
		}
	}

	return "", "", fmt.Errorf(
		"pod %q missing container image or engine annotation (%s or %s)",
		pod.Metadata.Name, AnnotationExecutablePath, AnnotationWasmModule,
	)
}

func CollectEnvVars(containers []types.Container) []types.EnvVar {
	seen := make(map[string]struct{})
	var merged []types.EnvVar

	for _, c := range containers {
		for _, e := range c.Env {
			if _, dup := seen[e.Name]; dup {
				continue
			}
			seen[e.Name] = struct{}{}
			merged = append(merged, e)
		}
	}

	return merged
}

func isJSON(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

func ParseService(data []byte) (*types.Service, error) {
	if len(data) == 0 {
		return nil, errors.New("empty manifest")
	}

	svc := &types.Service{}

	if isJSON(data) {
		if err := json.Unmarshal(data, svc); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, svc); err != nil {
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
	}

	if svc.Metadata.Name == "" {
		return nil, errors.New("metadata.name is required")
	}

	return svc, nil
}

func ParseConfigMap(data []byte) (*types.ConfigMap, error) {
	if len(data) == 0 {
		return nil, errors.New("empty manifest")
	}

	cm := &types.ConfigMap{}

	if isJSON(data) {
		if err := json.Unmarshal(data, cm); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, cm); err != nil {
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
	}

	if cm.Metadata.Name == "" {
		return nil, errors.New("metadata.name is required")
	}

	return cm, nil
}

func ParseSecret(data []byte) (*types.Secret, error) {
	if len(data) == 0 {
		return nil, errors.New("empty manifest")
	}

	sec := &types.Secret{}

	if isJSON(data) {
		if err := json.Unmarshal(data, sec); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, sec); err != nil {
			return nil, fmt.Errorf("yaml decode: %w", err)
		}
	}

	if sec.Metadata.Name == "" {
		return nil, errors.New("metadata.name is required")
	}

	return sec, nil
}
