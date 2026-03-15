package engine

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/megawron/lok8s/types"
)

type PodEngine interface {
	Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, stdout, stderr io.Writer) error
	Stop(podName string) error
	Status(podName string) types.PodPhase
}

type Registry struct {
	mu      sync.RWMutex
	engines map[string]PodEngine
}

func NewRegistry() *Registry {
	return &Registry{
		engines: make(map[string]PodEngine),
	}
}

func (r *Registry) Register(name string, eng PodEngine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engines[name] = eng
}

func (r *Registry) Get(name string) (PodEngine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	eng, ok := r.engines[name]
	if !ok {
		return nil, fmt.Errorf("no engine registered for %q", name)
	}
	return eng, nil
}
