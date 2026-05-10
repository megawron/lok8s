package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"time"

	"github.com/megawron/lok8s/types"
)

type DeploymentController struct {
	store  *Store
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewDeploymentController(store *Store) *DeploymentController {
	return &DeploymentController{
		store:  store,
		stopCh: make(chan struct{}),
	}
}

func (dc *DeploymentController) Start(ctx context.Context) {
	dc.wg.Add(1)
	go func() {
		defer dc.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-dc.stopCh:
				return
			case <-ticker.C:
				dc.ReconcileAll()
			}
		}
	}()
}

func (dc *DeploymentController) Stop() {
	close(dc.stopCh)
	dc.wg.Wait()
}

func (dc *DeploymentController) ReconcileAll() {
	deps := dc.store.ListDeployments("")
	for _, dep := range deps {
		if err := dc.Reconcile(dep); err != nil {
			log.Printf("[deployment-controller] Error reconciling Deployment %s/%s: %v", dep.Metadata.Namespace, dep.Metadata.Name, err)
		}
	}
}

func (dc *DeploymentController) Reconcile(dep types.Deployment) error {
	hash := hashTemplateSpec(&dep.Spec.Template)
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}

	var newRS *types.ReplicaSet
	var oldRSs []types.ReplicaSet

	allRSs := dc.store.ListReplicaSets(dep.Metadata.Namespace)
	for _, rs := range allRSs {
		if rs.Metadata.Annotations != nil && rs.Metadata.Annotations["lok8s.io/owner"] == "Deployment/"+dep.Metadata.Name {
			if rs.Metadata.Labels != nil && rs.Metadata.Labels["pod-template-hash"] == hash {
				newRS = &rs
			} else {
				oldRSs = append(oldRSs, rs)
			}
		}
	}

	if newRS == nil {
		newRSName := fmt.Sprintf("%s-%s", dep.Metadata.Name, hash)
		initReplicas := int32(0)
		if len(oldRSs) == 0 {
			initReplicas = desired
		}

		rs := types.ReplicaSet{
			TypeMeta: types.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
			},
			Metadata: types.ObjectMeta{
				Name:              newRSName,
				Namespace:         dep.Metadata.Namespace,
				Labels:            map[string]string{"pod-template-hash": hash},
				Annotations:       map[string]string{"lok8s.io/owner": "Deployment/" + dep.Metadata.Name},
				CreationTimestamp: time.Now().UTC(),
			},
			Spec: types.ReplicaSetSpec{
				Replicas: &initReplicas,
				Selector: dep.Spec.Selector,
				Template: dep.Spec.Template,
			},
		}

		if dep.Spec.Selector != nil {
			for k, v := range dep.Spec.Selector.MatchLabels {
				rs.Metadata.Labels[k] = v
			}
		}

		dc.store.StoreReplicaSet(rs)
		newRS = &rs
	}

	if len(oldRSs) > 0 {
		currentNewReplicas := int32(0)
		if newRS.Spec.Replicas != nil {
			currentNewReplicas = *newRS.Spec.Replicas
		}

		// 1. Scale up the new RS by 1 per tick until it reaches desired
		if currentNewReplicas < desired {
			currentNewReplicas++
			newRS.Spec.Replicas = &currentNewReplicas
			dc.store.StoreReplicaSet(*newRS)
		}

		// 2. Scale down old RSs based on ready replicas of the new RS
		readyNew := newRS.Status.ReadyReplicas
		maxOldReplicas := desired - readyNew
		if maxOldReplicas < 0 {
			maxOldReplicas = 0
		}

		var remainingOld = maxOldReplicas
		for i := range oldRSs {
			oldRS := &oldRSs[i]
			currentOldDesired := int32(0)
			if oldRS.Spec.Replicas != nil {
				currentOldDesired = *oldRS.Spec.Replicas
			}

			if currentOldDesired > remainingOld {
				currentOldDesired = remainingOld
			}
			oldRS.Spec.Replicas = &currentOldDesired
			dc.store.StoreReplicaSet(*oldRS)

			remainingOld -= currentOldDesired
			if remainingOld < 0 {
				remainingOld = 0
			}
		}
	} else {
		currentNewReplicas := int32(0)
		if newRS.Spec.Replicas != nil {
			currentNewReplicas = *newRS.Spec.Replicas
		}
		if currentNewReplicas != desired {
			newRS.Spec.Replicas = &desired
			dc.store.StoreReplicaSet(*newRS)
		}
	}

	// Update status
	// Refresh allRSs to get updated replicas from the store
	allRSs = dc.store.ListReplicaSets(dep.Metadata.Namespace)
	var totalReplicas int32
	var totalReady int32
	var updatedReplicas int32

	for _, rs := range allRSs {
		if rs.Metadata.Annotations != nil && rs.Metadata.Annotations["lok8s.io/owner"] == "Deployment/"+dep.Metadata.Name {
			totalReplicas += rs.Status.Replicas
			totalReady += rs.Status.ReadyReplicas
			if rs.Metadata.Labels != nil && rs.Metadata.Labels["pod-template-hash"] == hash {
				updatedReplicas = rs.Status.Replicas
			}
		}
	}

	dep.Status.Replicas = totalReplicas
	dep.Status.UpdatedReplicas = updatedReplicas
	dep.Status.ReadyReplicas = totalReady
	dep.Status.AvailableReplicas = totalReady
	dep.Status.UnavailableReplicas = desired - totalReady
	if dep.Status.UnavailableReplicas < 0 {
		dep.Status.UnavailableReplicas = 0
	}

	dc.store.StoreDeployment(dep)

	return nil
}

func hashTemplateSpec(template *types.PodTemplateSpec) string {
	b, _ := json.Marshal(template.Spec)
	h := fnv.New32a()
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum32())
}
