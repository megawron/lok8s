package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

func TestServer_DeploymentLifecycle(t *testing.T) {
	// Setup mock engine & registry
	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("native", mockEng)

	pool := network.NewPortPool(30000, 32767)
	configStore := config.NewStore(nil)
	controllerStore := controller.NewStore(nil)
	lm := engine.NewLifecycleManager(reg, pool, configStore)

	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controllerStore, nil)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Instantiate controllers for manual reconciliation
	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	// 1. Create a Deployment via POST
	depJSON := `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "web-deployment"
		},
		"spec": {
			"replicas": 2,
			"selector": {
				"matchLabels": {
					"app": "web-app"
				}
			},
			"template": {
				"metadata": {
					"labels": {
						"app": "web-app"
					},
					"annotations": {
						"lok8s.io/executable-path": "dummy"
					}
				},
				"spec": {
					"containers": [
						{
							"name": "main",
							"image": "nginx"
						}
					]
				}
			}
		}
	}`

	res, err := http.Post(
		fmt.Sprintf("%s/apis/apps/v1/namespaces/default/deployments", ts.URL),
		"application/json",
		bytes.NewBufferString(depJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d", res.StatusCode)
	}

	var createdDep types.Deployment
	if err := json.NewDecoder(res.Body).Decode(&createdDep); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if createdDep.Metadata.Name != "web-deployment" {
		t.Errorf("Expected deployment name 'web-deployment', got %q", createdDep.Metadata.Name)
	}

	// Verify it is in store
	_, ok := controllerStore.LoadDeployment("default", "web-deployment")
	if !ok {
		t.Fatal("Deployment not found in store after POST")
	}

	// 2. Reconcile Deployment -> should create ReplicaSet
	depController.ReconcileAll()

	rss := controllerStore.ListReplicaSets("default")
	if len(rss) != 1 {
		t.Fatalf("Expected 1 ReplicaSet to be created by Deployment reconciliation, got %d", len(rss))
	}
	rs := rss[0]
	if *rs.Spec.Replicas != 2 {
		t.Errorf("Expected ReplicaSet replicas to be 2, got %d", *rs.Spec.Replicas)
	}

	// 3. Reconcile ReplicaSet -> should create and launch 2 Pods
	rsController.ReconcileAll()

	pods := srv.ListPods("default")
	if len(pods) != 2 {
		t.Fatalf("Expected 2 pods to be launched by ReplicaSet reconciliation, got %d", len(pods))
	}

	// 4. Update pod status to simulate them becoming ready
	for _, p := range pods {
		if status, ok := lm.Status(p.Metadata.Namespace, p.Metadata.Name); ok {
			status.ContainerStatuses = []types.ContainerStatus{{Ready: true}}
			// Update status in lifecycle
			// (mock engine keeps running status, we just need the Ready count updated)
		}
	}

	// Reconcile again to update counts in ReplicaSet and Deployment status
	rsController.ReconcileAll()
	depController.ReconcileAll()

	// GET Deployment via HTTP and check status
	res, err = http.Get(fmt.Sprintf("%s/apis/apps/v1/namespaces/default/deployments/web-deployment", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", res.StatusCode)
	}

	var fetchedDep types.Deployment
	if err := json.NewDecoder(res.Body).Decode(&fetchedDep); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if fetchedDep.Status.Replicas != 2 {
		t.Errorf("Expected status replicas = 2, got %d", fetchedDep.Status.Replicas)
	}

	// 5. GET as Table format
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/apis/apps/v1/namespaces/default/deployments", ts.URL), nil)
	req.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for Table request, got %d", res.StatusCode)
	}
	var table types.Table
	if err := json.NewDecoder(res.Body).Decode(&table); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if table.Kind != "Table" || len(table.Rows) != 1 || table.Rows[0].Cells[0] != "web-deployment" {
		t.Errorf("Expected Table formatted output, got: %+v", table)
	}

	// 6. Delete Deployment and check cascading delete (ReplicaSets and Pods deleted)
	req, _ = http.NewRequest("DELETE", fmt.Sprintf("%s/apis/apps/v1/namespaces/default/deployments/web-deployment", ts.URL), nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK on deletion, got %d", res.StatusCode)
	}
	res.Body.Close()

	// Verify deployment deleted from store
	_, ok = controllerStore.LoadDeployment("default", "web-deployment")
	if ok {
		t.Error("Expected deployment to be deleted from store")
	}

	// Verify ReplicaSets deleted from store
	rss = controllerStore.ListReplicaSets("default")
	if len(rss) != 0 {
		t.Errorf("Expected all ReplicaSets to be deleted, got %d", len(rss))
	}

	// Verify Pods terminated and deleted
	pods = srv.ListPods("default")
	if len(pods) != 0 {
		t.Errorf("Expected all pods to be cascading-deleted, got %d", len(pods))
	}
}
