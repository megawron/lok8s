package api

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/store"
	"github.com/megawron/lok8s/types"
)

func TestE2EFlow(t *testing.T) {
	// Create temp dir for db and manifests
	tmpDir, err := os.MkdirTemp("", "lok8s-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "lok8s.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("native", mockEng)

	pool := network.NewPortPool(36000, 36500)
	configStore := config.NewStore(db)
	controllerStore := controller.NewStore(db)
	lm := engine.NewLifecycleManager(reg, pool, configStore)

	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controllerStore, db)

	// Start test HTTP server
	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Start controllers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rsController := controller.NewReplicaSetController(controllerStore, srv)
	depController := controller.NewDeploymentController(controllerStore)

	rsController.Start(ctx)
	depController.Start(ctx)

	// Write a deployment manifest
	manifestContent := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: e2e-deployment
  namespace: default
spec:
  replicas: 2
  selector:
    matchLabels:
      app: e2e-app
  template:
    metadata:
      labels:
        app: e2e-app
      annotations:
        lok8s.io/executable-path: "echo"
    spec:
      containers:
      - name: web
        image: nginx
`
	manifestPath := filepath.Join(tmpDir, "deployment.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// 1. Run CLI "apply" to deploy
	cmd := exec.Command("go", "run", "../main.go", "-s", ts.URL, "apply", "-f", manifestPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli apply failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "deployment/e2e-deployment created/configured") {
		t.Errorf("unexpected apply output: %s", string(output))
	}

	// Wait for controller to reconcile and create ReplicaSet and Pods
	time.Sleep(1500 * time.Millisecond)

	// 2. Run CLI "get deployments"
	cmd = exec.Command("go", "run", "../main.go", "-s", ts.URL, "get", "deployments")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli get deployments failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(strings.ToLower(string(output)), "e2e-deployment") {
		t.Errorf("unexpected get deployments output: %s", string(output))
	}

	// 3. Run CLI "get pods" and verify 2 pods are listed
	cmd = exec.Command("go", "run", "../main.go", "-s", ts.URL, "get", "pods")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli get pods failed: %v, output: %s", err, string(output))
	}
	// Count lines containing "e2e-deployment-"
	lines := strings.Split(string(output), "\n")
	podCount := 0
	for _, line := range lines {
		if strings.Contains(line, "e2e-deployment-") {
			podCount++
		}
	}
	if podCount != 2 {
		t.Errorf("expected 2 pods, got %d. Output: %s", podCount, string(output))
	}

	// 4. Run CLI "delete deployment e2e-deployment"
	cmd = exec.Command("go", "run", "../main.go", "-s", ts.URL, "delete", "deployment", "e2e-deployment")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli delete deployment failed: %v, output: %s", err, string(output))
	}
	if !strings.Contains(string(output), "deployment/e2e-deployment deleted") {
		t.Errorf("unexpected delete output: %s", string(output))
	}

	// Wait for controller cascading deletes to complete
	time.Sleep(1500 * time.Millisecond)

	// 5. Verify no pods exist
	cmd = exec.Command("go", "run", "../main.go", "-s", ts.URL, "get", "pods")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cli get pods failed: %v, output: %s", err, string(output))
	}
	if strings.Contains(string(output), "e2e-deployment-") {
		t.Errorf("pods should have been deleted, but got output: %s", string(output))
	}
}
