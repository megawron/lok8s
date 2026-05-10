package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/megawron/lok8s/types"
)

type PodManager interface {
	LaunchPod(pod *types.Pod) error
	DeletePod(namespace, name string) error
	ListPods(namespace string) []types.Pod
}

type ReplicaSetController struct {
	store      *Store
	podManager PodManager
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

func NewReplicaSetController(store *Store, podManager PodManager) *ReplicaSetController {
	return &ReplicaSetController{
		store:      store,
		podManager: podManager,
		stopCh:     make(chan struct{}),
	}
}

func (rsc *ReplicaSetController) Start(ctx context.Context) {
	rsc.wg.Add(1)
	go func() {
		defer rsc.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-rsc.stopCh:
				return
			case <-ticker.C:
				rsc.ReconcileAll()
			}
		}
	}()
}

func (rsc *ReplicaSetController) Stop() {
	close(rsc.stopCh)
	rsc.wg.Wait()
}

func (rsc *ReplicaSetController) ReconcileAll() {
	rss := rsc.store.ListReplicaSets("")
	for _, rs := range rss {
		if err := rsc.Reconcile(rs); err != nil {
			log.Printf("[replicaset-controller] Error reconciling ReplicaSet %s/%s: %v", rs.Metadata.Namespace, rs.Metadata.Name, err)
		}
	}
}

func (rsc *ReplicaSetController) Reconcile(rs types.ReplicaSet) error {
	pods := rsc.podManager.ListPods(rs.Metadata.Namespace)

	var activePods []types.Pod
	var readyCount int32

	for _, pod := range pods {
		if pod.Metadata.Annotations != nil && pod.Metadata.Annotations["lok8s.io/owner"] == "ReplicaSet/"+rs.Metadata.Name {
			if pod.Status.Phase != types.PodSucceeded && pod.Status.Phase != types.PodFailed {
				activePods = append(activePods, pod)
				if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
					readyCount++
				}
			}
		}
	}

	desired := int32(1)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}

	currentCount := int32(len(activePods))

	if currentCount < desired {
		diff := desired - currentCount
		for i := int32(0); i < diff; i++ {
			suffix := randString(5)
			podName := fmt.Sprintf("%s-%s", rs.Metadata.Name, suffix)

			newPod := &types.Pod{
				TypeMeta: types.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				Metadata: types.ObjectMeta{
					Name:              podName,
					Namespace:         rs.Metadata.Namespace,
					UID:               uuid.New().String(),
					Labels:            make(map[string]string),
					Annotations:       make(map[string]string),
					CreationTimestamp: time.Now().UTC(),
				},
				Spec: rs.Spec.Template.Spec,
			}

			// Copy labels
			for k, v := range rs.Spec.Template.Metadata.Labels {
				newPod.Metadata.Labels[k] = v
			}
			// Copy template annotations
			for k, v := range rs.Spec.Template.Metadata.Annotations {
				newPod.Metadata.Annotations[k] = v
			}
			// Set Owner Annotation
			newPod.Metadata.Annotations["lok8s.io/owner"] = "ReplicaSet/" + rs.Metadata.Name

			if err := rsc.podManager.LaunchPod(newPod); err != nil {
				return fmt.Errorf("failed to launch pod %s: %w", podName, err)
			}
		}
		// Refresh pod list to update status replicas count accurately
		pods = rsc.podManager.ListPods(rs.Metadata.Namespace)
		activePods = nil
		readyCount = 0
		for _, pod := range pods {
			if pod.Metadata.Annotations != nil && pod.Metadata.Annotations["lok8s.io/owner"] == "ReplicaSet/"+rs.Metadata.Name {
				if pod.Status.Phase != types.PodSucceeded && pod.Status.Phase != types.PodFailed {
					activePods = append(activePods, pod)
					if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
						readyCount++
					}
				}
			}
		}
		currentCount = int32(len(activePods))
	} else if currentCount > desired {
		diff := currentCount - desired
		// Delete oldest or last pods
		for i := int32(0); i < diff; i++ {
			podToDelete := activePods[len(activePods)-1-int(i)]
			if err := rsc.podManager.DeletePod(podToDelete.Metadata.Namespace, podToDelete.Metadata.Name); err != nil {
				return fmt.Errorf("failed to delete pod %s: %w", podToDelete.Metadata.Name, err)
			}
		}
		currentCount = desired
	}

	// Update status
	rs.Status.Replicas = currentCount
	rs.Status.ReadyReplicas = readyCount
	rs.Status.AvailableReplicas = readyCount
	rsc.store.StoreReplicaSet(rs)

	return nil
}

func randString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "abcde"
	}
	return hex.EncodeToString(b)[:n]
}
