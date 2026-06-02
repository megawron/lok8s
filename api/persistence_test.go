package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

func TestStateRecovery(t *testing.T) {
	// 1. Create a temporary directory for the bbolt database
	tmpDir, err := os.MkdirTemp("", "lok8s-test-db-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "lok8s.db")

	// Helper function to build a server instance using the db at dbPath
	createServer := func(t *testing.T) (*Server, *store.DB, *engine.LifecycleManager, *network.PortPool) {
		db, err := store.Open(dbPath)
		if err != nil {
			t.Fatalf("failed to open store: %v", err)
		}

		reg := engine.NewRegistry()
		mockEng := &mockEngine{
			status: make(map[string]types.PodPhase),
		}
		// Register under "native" so manifest parser matches it
		reg.Register("native", mockEng)

		pool := network.NewPortPool(34000, 34500)
		configStore := config.NewStore(db)
		controllerStore := controller.NewStore(db)
		lm := engine.NewLifecycleManager(reg, pool, configStore)

		srv := NewServer("127.0.0.1:0", lm, pool, configStore, controllerStore, db)
		return srv, db, lm, pool
	}

	// === Phase 1: Initialize server, write resources, shutdown ===
	srv1, db1, lm1, _ := createServer(t)

	// Create a client server
	ts1 := httptest.NewServer(srv1.httpServer.Handler)
	
	// Create ConfigMap
	cmJSON := `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {
			"name": "my-cm",
			"namespace": "default"
		},
		"data": {
			"key": "value"
		}
	}`
	res, err := http.Post(ts1.URL+"/api/v1/namespaces/default/configmaps", "application/json", bytes.NewBufferString(cmJSON))
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create ConfigMap: err=%v status=%d", err, res.StatusCode)
	}
	res.Body.Close()

	// Create Service
	svcJSON := `{
		"apiVersion": "v1",
		"kind": "Service",
		"metadata": {
			"name": "my-svc",
			"namespace": "default"
		},
		"spec": {
			"ports": [
				{
					"port": 0
				}
			],
			"selector": {
				"app": "my-app"
			}
		}
	}`
	res, err = http.Post(ts1.URL+"/api/v1/namespaces/default/services", "application/json", bytes.NewBufferString(svcJSON))
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create Service: err=%v status=%d", err, res.StatusCode)
	}
	var createdSvc types.Service
	_ = json.NewDecoder(res.Body).Decode(&createdSvc)
	res.Body.Close()

	allocatedNodePort := createdSvc.Spec.Ports[0].NodePort
	if allocatedNodePort == 0 {
		t.Fatal("service NodePort was not allocated")
	}

	// Create Pod (needs annotations for native engine to pass manifest check)
	podJSON := `{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "my-pod",
			"namespace": "default",
			"annotations": {
				"lok8s.io/engine": "native",
				"lok8s.io/executable-path": "echo"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "test-container",
					"image": "echo:latest",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	}`
	res, err = http.Post(ts1.URL+"/api/v1/namespaces/default/pods", "application/json", bytes.NewBufferString(podJSON))
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create Pod: err=%v status=%d", err, res.StatusCode)
	}
	var createdPod types.Pod
	_ = json.NewDecoder(res.Body).Decode(&createdPod)
	res.Body.Close()

	if createdPod.Status.Phase != types.PodRunning {
		t.Fatalf("expected pod phase to be Running, got %s", createdPod.Status.Phase)
	}

	allocatedHostPort := createdPod.Status.HostPort
	if allocatedHostPort == 0 {
		t.Fatal("pod hostPort was not allocated")
	}

	// Close the test server 1 and its DB
	ts1.Close()
	_ = srv1.Shutdown(context.Background())
	lm1.Shutdown()
	_ = db1.Close()

	// Wait briefly to ensure ports are freed by OS
	time.Sleep(100 * time.Millisecond)

	// === Phase 2: Start new server with same DB file, trigger Recovery ===
	srv2, db2, lm2, pool2 := createServer(t)
	defer func() {
		_ = srv2.Shutdown(context.Background())
		lm2.Shutdown()
		_ = db2.Close()
	}()

	// Trigger recover state
	srv2.RecoverState()

	// Verify ConfigMap exists in configStore
	cm, exists := srv2.configStore.LoadConfigMap("default", "my-cm")
	if !exists {
		t.Fatal("recovered configStore is missing the ConfigMap")
	}
	if cm.Data["key"] != "value" {
		t.Errorf("expected CM data value to be 'value', got %q", cm.Data["key"])
	}

	// Verify Service exists in srv2
	svc, exists := srv2.services.Load("default", "my-svc")
	if !exists {
		t.Fatal("recovered serviceStore is missing the Service")
	}
	if svc.Spec.Ports[0].NodePort != allocatedNodePort {
		t.Errorf("expected NodePort %d, got %d", allocatedNodePort, svc.Spec.Ports[0].NodePort)
	}

	// Verify that the port is reserved in pool2
	nodePortOwner, nodePortErr := pool2.Lookup("default/my-svc")
	if nodePortErr != nil || nodePortOwner != allocatedNodePort {
		t.Errorf("expected Service port %d to be reserved in PortPool, got error=%v port=%d", allocatedNodePort, nodePortErr, nodePortOwner)
	}

	// Verify Pod exists in srv2 and its state is recovered
	recoveredPod, exists := srv2.loadPod("default", "my-pod")
	if !exists {
		t.Fatal("recovered server is missing the Pod")
	}
	if recoveredPod.Status.HostPort != allocatedHostPort {
		t.Errorf("expected hostPort %d, got %d", allocatedHostPort, recoveredPod.Status.HostPort)
	}

	// Verify that the host port is reserved in pool2
	podPortOwner, podPortErr := pool2.Lookup("default/my-pod")
	if podPortErr != nil || podPortOwner != allocatedHostPort {
		t.Errorf("expected Pod hostPort %d to be reserved in PortPool, got error=%v port=%d", allocatedHostPort, podPortErr, podPortOwner)
	}

	// Verify that the pod process is running in LifecycleManager
	podStatus, managed := lm2.Status("default", "my-pod")
	if !managed {
		t.Fatal("pod is not managed by the recovered LifecycleManager")
	}
	if podStatus.Phase != types.PodRunning {
		t.Errorf("expected recovered pod phase to be Running, got %s", podStatus.Phase)
	}
}
