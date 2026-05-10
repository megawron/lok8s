package controller

import (
	"strings"
	"sync"
	"testing"

	"github.com/megawron/lok8s/types"
)

type mockPodManager struct {
	mu   sync.Mutex
	pods map[string]types.Pod
}

func newMockPodManager() *mockPodManager {
	return &mockPodManager{
		pods: make(map[string]types.Pod),
	}
}

func (m *mockPodManager) LaunchPod(pod *types.Pod) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pods[pod.Metadata.Namespace+"/"+pod.Metadata.Name] = *pod
	return nil
}

func (m *mockPodManager) DeletePod(namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pods, namespace+"/"+name)
	return nil
}

func (m *mockPodManager) ListPods(namespace string) []types.Pod {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []types.Pod
	for _, p := range m.pods {
		if namespace == "" || p.Metadata.Namespace == namespace {
			result = append(result, p)
		}
	}
	return result
}

func TestReplicaSetController_Reconcile(t *testing.T) {
	store := NewStore()
	pm := newMockPodManager()
	rsc := NewReplicaSetController(store, pm)

	replicas := int32(3)
	rs := types.ReplicaSet{
		Metadata: types.ObjectMeta{
			Name:      "nginx-rs",
			Namespace: "default",
		},
		Spec: types.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &types.LabelSelector{
				MatchLabels: map[string]string{"app": "nginx"},
			},
			Template: types.PodTemplateSpec{
				Metadata: types.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
				Spec: types.PodSpec{
					Containers: []types.Container{
						{Name: "nginx", Image: "nginx"},
					},
				},
			},
		},
	}

	store.StoreReplicaSet(rs)

	// Reconcile scale up from 0 to 3
	if err := rsc.Reconcile(rs); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	pods := pm.ListPods("default")
	if len(pods) != 3 {
		t.Errorf("Expected 3 pods, got %d", len(pods))
	}

	for _, pod := range pods {
		if !strings.HasPrefix(pod.Metadata.Name, "nginx-rs-") {
			t.Errorf("Expected pod name prefix 'nginx-rs-', got %q", pod.Metadata.Name)
		}
		if pod.Metadata.Annotations["lok8s.io/owner"] != "ReplicaSet/nginx-rs" {
			t.Errorf("Expected owner annotation, got %q", pod.Metadata.Annotations["lok8s.io/owner"])
		}
		if pod.Metadata.Labels["app"] != "nginx" {
			t.Errorf("Expected label 'app=nginx', got %v", pod.Metadata.Labels)
		}
	}

	// Verify ReplicaSet status in store
	updatedRS, _ := store.LoadReplicaSet("default", "nginx-rs")
	if updatedRS.Status.Replicas != 3 {
		t.Errorf("Expected RS status replicas 3, got %d", updatedRS.Status.Replicas)
	}

	// Set one pod to Ready and verify RS ReadyReplicas
	pm.mu.Lock()
	for k, v := range pm.pods {
		v.Status.ContainerStatuses = []types.ContainerStatus{{Ready: true}}
		pm.pods[k] = v
		break // just make one ready
	}
	pm.mu.Unlock()

	if err := rsc.Reconcile(updatedRS); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedRS, _ = store.LoadReplicaSet("default", "nginx-rs")
	if updatedRS.Status.ReadyReplicas != 1 {
		t.Errorf("Expected 1 ready replica, got %d", updatedRS.Status.ReadyReplicas)
	}

	// Scale down to 1
	newReplicas := int32(1)
	updatedRS.Spec.Replicas = &newReplicas
	store.StoreReplicaSet(updatedRS)

	if err := rsc.Reconcile(updatedRS); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	pods = pm.ListPods("default")
	if len(pods) != 1 {
		t.Errorf("Expected 1 pod after scale down, got %d", len(pods))
	}

	updatedRS, _ = store.LoadReplicaSet("default", "nginx-rs")
	if updatedRS.Status.Replicas != 1 {
		t.Errorf("Expected RS status replicas 1, got %d", updatedRS.Status.Replicas)
	}
}

func TestReplicaSetController_NonOwnedPods(t *testing.T) {
	store := NewStore()
	pm := newMockPodManager()
	rsc := NewReplicaSetController(store, pm)

	// Create a pod not owned by the RS
	unownedPod := &types.Pod{
		Metadata: types.ObjectMeta{
			Name:      "manual-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "nginx"},
		},
	}
	_ = pm.LaunchPod(unownedPod)

	replicas := int32(1)
	rs := types.ReplicaSet{
		Metadata: types.ObjectMeta{
			Name:      "nginx-rs",
			Namespace: "default",
		},
		Spec: types.ReplicaSetSpec{
			Replicas: &replicas,
			Selector: &types.LabelSelector{
				MatchLabels: map[string]string{"app": "nginx"},
			},
			Template: types.PodTemplateSpec{
				Metadata: types.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
				Spec: types.PodSpec{
					Containers: []types.Container{{Name: "nginx"}},
				},
			},
		},
	}
	store.StoreReplicaSet(rs)

	// Reconcile. Should create 1 RS-owned pod, ignoring the unowned manual-pod (even though labels match)
	// because lok8s relies on owner annotations for safety.
	if err := rsc.Reconcile(rs); err != nil {
		t.Fatal(err)
	}

	pods := pm.ListPods("default")
	if len(pods) != 2 { // manual-pod + rs-owned-pod
		t.Errorf("Expected 2 total pods in system, got %d", len(pods))
	}

	foundRSOwned := false
	for _, p := range pods {
		if p.Metadata.Annotations != nil && p.Metadata.Annotations["lok8s.io/owner"] == "ReplicaSet/nginx-rs" {
			foundRSOwned = true
		}
	}
	if !foundRSOwned {
		t.Error("Expected ReplicaSet to create an owned pod")
	}
}
