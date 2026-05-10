package controller

import (
	"testing"

	"github.com/megawron/lok8s/types"
)

func TestDeploymentController_Reconcile(t *testing.T) {
	store := NewStore()
	dc := NewDeploymentController(store)

	replicas := int32(2)
	dep := types.Deployment{
		Metadata: types.ObjectMeta{
			Name:      "web-dep",
			Namespace: "default",
		},
		Spec: types.DeploymentSpec{
			Replicas: &replicas,
			Selector: &types.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			Template: types.PodTemplateSpec{
				Metadata: types.ObjectMeta{
					Labels: map[string]string{"app": "web"},
				},
				Spec: types.PodSpec{
					Containers: []types.Container{
						{Name: "web", Image: "nginx:1.19"},
					},
				},
			},
		},
	}

	store.StoreDeployment(dep)

	// 1. Initial reconcile: should create a new ReplicaSet
	if err := dc.Reconcile(dep); err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	rss := store.ListReplicaSets("default")
	if len(rss) != 1 {
		t.Fatalf("Expected 1 ReplicaSet to be created, got %d", len(rss))
	}

	rs := rss[0]
	if rs.Metadata.Annotations["lok8s.io/owner"] != "Deployment/web-dep" {
		t.Errorf("Expected owner annotation, got %q", rs.Metadata.Annotations["lok8s.io/owner"])
	}
	if *rs.Spec.Replicas != 2 {
		t.Errorf("Expected ReplicaSet desired replicas to be 2, got %d", *rs.Spec.Replicas)
	}

	// 2. Simulate ReplicaSet Controller updating ReplicaSet status
	rs.Status.Replicas = 2
	rs.Status.ReadyReplicas = 2
	store.StoreReplicaSet(rs)

	// Reconcile again to update Deployment status
	if err := dc.Reconcile(dep); err != nil {
		t.Fatal(err)
	}

	updatedDep, _ := store.LoadDeployment("default", "web-dep")
	if updatedDep.Status.ReadyReplicas != 2 {
		t.Errorf("Expected deployment status ready replicas to be 2, got %d", updatedDep.Status.ReadyReplicas)
	}

	// 3. Update Pod Template image to trigger Rolling Update
	updatedDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.20"
	store.StoreDeployment(updatedDep)

	// First rolling update reconcile: should create a second ReplicaSet (desired replicas = 0 initially)
	if err := dc.Reconcile(updatedDep); err != nil {
		t.Fatal(err)
	}

	rss = store.ListReplicaSets("default")
	if len(rss) != 2 {
		t.Fatalf("Expected 2 ReplicaSets during rolling update, got %d", len(rss))
	}

	var newRS, oldRS types.ReplicaSet
	newHash := hashTemplateSpec(&updatedDep.Spec.Template)
	for _, r := range rss {
		if r.Metadata.Labels["pod-template-hash"] == newHash {
			newRS = r
		} else {
			oldRS = r
		}
	}

	// New RS starts at 1 desired replica (scaled up by 1)
	if *newRS.Spec.Replicas != 1 {
		t.Errorf("Expected new RS desired replicas to scale up to 1, got %d", *newRS.Spec.Replicas)
	}
	// Old RS remains at 2 replicas since new RS has 0 ready replicas
	if *oldRS.Spec.Replicas != 2 {
		t.Errorf("Expected old RS desired replicas to remain at 2, got %d", *oldRS.Spec.Replicas)
	}

	// Simulate new RS getting 1 ready replica
	newRS.Status.Replicas = 1
	newRS.Status.ReadyReplicas = 1
	store.StoreReplicaSet(newRS)

	// Reconcile: should scale down old RS to 1 replica (since readyNew is 1, maxOldAllowed = 2 - 1 = 1)
	// and scale up new RS to 2 replicas
	if err := dc.Reconcile(updatedDep); err != nil {
		t.Fatal(err)
	}

	newRS, _ = store.LoadReplicaSet("default", newRS.Metadata.Name)
	oldRS, _ = store.LoadReplicaSet("default", oldRS.Metadata.Name)

	if *newRS.Spec.Replicas != 2 {
		t.Errorf("Expected new RS desired replicas to scale up to 2, got %d", *newRS.Spec.Replicas)
	}
	if *oldRS.Spec.Replicas != 1 {
		t.Errorf("Expected old RS desired replicas to scale down to 1, got %d", *oldRS.Spec.Replicas)
	}

	// Simulate new RS getting 2 ready replicas, old RS scaled down to 1 running replica
	newRS.Status.Replicas = 2
	newRS.Status.ReadyReplicas = 2
	store.StoreReplicaSet(newRS)

	oldRS.Status.Replicas = 1
	oldRS.Status.ReadyReplicas = 1
	store.StoreReplicaSet(oldRS)

	// Reconcile: should scale old RS down to 0 replicas (since readyNew is 2, maxOldAllowed = 2 - 2 = 0)
	if err := dc.Reconcile(updatedDep); err != nil {
		t.Fatal(err)
	}

	newRS, _ = store.LoadReplicaSet("default", newRS.Metadata.Name)
	oldRS, _ = store.LoadReplicaSet("default", oldRS.Metadata.Name)

	if *newRS.Spec.Replicas != 2 {
		t.Errorf("Expected new RS desired replicas to remain at 2, got %d", *newRS.Spec.Replicas)
	}
	if *oldRS.Spec.Replicas != 0 {
		t.Errorf("Expected old RS desired replicas to scale down to 0, got %d", *oldRS.Spec.Replicas)
	}
}
