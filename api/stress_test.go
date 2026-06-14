package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

func TestStressLoad(t *testing.T) {
	// Create temporary directory for database
	tmpDir, err := os.MkdirTemp("", "lok8s-stress-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "lok8s-stress.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Register mock engine under "native"
	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("native", mockEng)

	pool := network.NewPortPool(30000, 35000)
	configStore := config.NewStore(db)
	controllerStore := controller.NewStore(db)
	lm := engine.NewLifecycleManager(reg, pool, configStore)

	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controllerStore, db)
	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Start controllers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	rsController.Start(ctx)
	depController.Start(ctx)

	numDeployments := 15
	replicasPerDeployment := 3 // Total pods = 45
	var wg sync.WaitGroup

	t.Logf("Spawning %d Deployments (each with %d replicas, total %d pods)...", numDeployments, replicasPerDeployment, numDeployments*replicasPerDeployment)

	// Create Deployments concurrently
	for i := 1; i <= numDeployments; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			replicas := int32(replicasPerDeployment)
			dep := types.Deployment{
				TypeMeta: types.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				Metadata: types.ObjectMeta{
					Name:      fmt.Sprintf("stress-dep-%d", id),
					Namespace: "default",
				},
				Spec: types.DeploymentSpec{
					Replicas: &replicas,
					Selector: &types.LabelSelector{
						MatchLabels: map[string]string{"app": fmt.Sprintf("app-%d", id)},
					},
					Template: types.PodTemplateSpec{
						Metadata: types.ObjectMeta{
							Labels: map[string]string{"app": fmt.Sprintf("app-%d", id)},
							Annotations: map[string]string{
								"lok8s.io/executable-path": "echo",
							},
						},
						Spec: types.PodSpec{
							Containers: []types.Container{
								{Name: "main", Image: "nginx"},
							},
						},
					},
				},
			}
			controllerStore.StoreDeployment(dep)
		}(i)
	}

	wg.Wait()
	t.Log("All Deployment specs written to store. Waiting for reconciliation loop to scale pods...")

	// Poll until all 45 pods are Running
	start := time.Now()
	timeout := 15 * time.Second
	for {
		pods := srv.ListPods("default")
		runningCount := 0
		for _, pod := range pods {
			if pod.Status.Phase == types.PodRunning {
				runningCount++
			}
		}

		if runningCount == numDeployments*replicasPerDeployment {
			t.Logf("Success! All %d pods are running (took %v)", runningCount, time.Since(start))
			break
		}

		if time.Since(start) > timeout {
			t.Fatalf("Timeout waiting for pods to scale. Running pods: %d/%d", runningCount, numDeployments*replicasPerDeployment)
		}

		time.Sleep(200 * time.Millisecond)
	}

	// Deploy some Services concurrently
	numServices := 5
	t.Logf("Creating %d Services load-balancing across the pods...", numServices)
	for i := 1; i <= numServices; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			svc := types.Service{
				TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Service"},
				Metadata: types.ObjectMeta{
					Name:      fmt.Sprintf("stress-svc-%d", id),
					Namespace: "default",
				},
				Spec: types.ServiceSpec{
					Ports: []types.ServicePort{
						{Port: 0}, // Auto-allocate from pool
					},
					Selector: map[string]string{"app": fmt.Sprintf("app-%d", id)},
				},
			}
			_, err := srv.proxyManager.StartProxy(&svc)
			if err != nil {
				t.Errorf("failed to start proxy for service %d: %v", id, err)
			}
			srv.services.Store(svc)
		}(i)
	}
	wg.Wait()

	// Verify all proxies are listed in port pool
	for i := 1; i <= numServices; i++ {
		key := fmt.Sprintf("default/stress-svc-%d", i)
		port, err := pool.Lookup(key)
		if err != nil || port == 0 {
			t.Errorf("service %s did not register its port in PortPool", key)
		} else {
			t.Logf("Service %s successfully bound to port %d", key, port)
		}
	}

	// Clean up concurrently
	t.Log("Tearing down all Deployments and Services...")
	for i := 1; i <= numDeployments; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			depName := fmt.Sprintf("stress-dep-%d", id)
			controllerStore.DeleteDeployment("default", depName)

			// Clean up pods
			pods := srv.ListPods("default")
			for _, p := range pods {
				if strings.HasPrefix(p.Metadata.Name, fmt.Sprintf("stress-dep-%d-", id)) {
					_ = srv.DeletePod("default", p.Metadata.Name)
				}
			}
		}(i)
	}

	for i := 1; i <= numServices; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			srv.proxyManager.StopProxy("default", fmt.Sprintf("stress-svc-%d", id))
			srv.services.Delete("default", fmt.Sprintf("stress-svc-%d", id))
		}(i)
	}

	wg.Wait()
	t.Log("Cleanup complete!")
}
