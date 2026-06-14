package engine

import (
	"context"
	"io"
	"testing"

	"github.com/megawron/lok8s/types"
)

type dummyEngine struct{}

func (d *dummyEngine) Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, volumes map[string]string, stdout, stderr io.Writer) error {
	return nil
}

func (d *dummyEngine) Stop(podName string) error {
	return nil
}

func (d *dummyEngine) Status(podName string) types.PodPhase {
	return types.PodRunning
}

func TestPodKey(t *testing.T) {
	k := podKey("default", "my-pod")
	if k != "default/my-pod" {
		t.Errorf("expected default/my-pod, got %q", k)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	mock := &dummyEngine{}
	r.Register("mock-eng", mock)

	eng, err := r.Get("mock-eng")
	if err != nil {
		t.Fatal(err)
	}
	if eng != mock {
		t.Error("registry returned different engine instance")
	}

	_, err = r.Get("unknown")
	if err == nil {
		t.Error("expected error for unknown engine, got nil")
	}
}

func TestLifecycleManager_ShouldRestart(t *testing.T) {
	lm := &LifecycleManager{}

	podAlways := &types.Pod{
		Spec: types.PodSpec{
			RestartPolicy: types.RestartAlways,
		},
	}
	if !lm.shouldRestart(podAlways, types.PodSucceeded) {
		t.Error("RestartAlways should restart on Succeeded")
	}
	if !lm.shouldRestart(podAlways, types.PodFailed) {
		t.Error("RestartAlways should restart on Failed")
	}

	podNever := &types.Pod{
		Spec: types.PodSpec{
			RestartPolicy: types.RestartNever,
		},
	}
	if lm.shouldRestart(podNever, types.PodSucceeded) {
		t.Error("RestartNever should not restart on Succeeded")
	}
	if lm.shouldRestart(podNever, types.PodFailed) {
		t.Error("RestartNever should not restart on Failed")
	}

	podOnFailure := &types.Pod{
		Spec: types.PodSpec{
			RestartPolicy: types.RestartOnFailure,
		},
	}
	if lm.shouldRestart(podOnFailure, types.PodSucceeded) {
		t.Error("RestartOnFailure should not restart on Succeeded")
	}
	if !lm.shouldRestart(podOnFailure, types.PodFailed) {
		t.Error("RestartOnFailure should restart on Failed")
	}
}
