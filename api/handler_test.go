package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/types"
)

// mockEngine implements engine.PodEngine
type mockEngine struct {
	mu     sync.Mutex
	status map[string]types.PodPhase
	stdout io.Writer
}

func (m *mockEngine) Start(ctx context.Context, pod *types.Pod, target string, env []types.EnvVar, stdout, stderr io.Writer) error {
	m.mu.Lock()
	m.status[pod.Metadata.Name] = types.PodRunning
	m.stdout = stdout
	m.mu.Unlock()

	// Write some initial logs
	_, _ = stdout.Write([]byte("init log 1\n"))
	_, _ = stdout.Write([]byte("init log 2\n"))

	return nil
}

func (m *mockEngine) Stop(podName string) error {
	m.mu.Lock()
	m.status[podName] = types.PodSucceeded
	m.mu.Unlock()
	return nil
}

func (m *mockEngine) Status(podName string) types.PodPhase {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status[podName]
}

func TestServer_GetPodLogs(t *testing.T) {
	// Create registry & mock engine
	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("mock", mockEng)

	// Create LifecycleManager & Server
	lm := engine.NewLifecycleManager(reg)
	srv := NewServer("127.0.0.1:0", lm)

	// We can test the HTTP handler directly using httptest.NewRecorder() or httptest.NewServer()
	// Let's use httptest.NewServer to properly test streaming / follow.
	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// 1. Get logs for a non-existing pod (returns 404)
	res, err := http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods/my-pod/log", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent pod, got %d", res.StatusCode)
	}

	// 2. Launch a mock pod
	pod := &types.Pod{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		Metadata: types.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"lok8s.io/engine": "mock",
				"lok8s.io/target": "dummy",
			},
		},
		Spec: types.PodSpec{
			Containers: []types.Container{
				{Name: "main"},
			},
		},
	}

	// We can store pod manually in srv.pods
	srv.storePod(*pod)

	err = lm.Launch(pod, "mock", "dummy", nil)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// 3. Get all logs
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods/test-pod/log", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	expected := "init log 1\ninit log 2\n"
	if string(body) != expected {
		t.Errorf("Expected logs %q, got %q", expected, string(body))
	}

	// 4. Test tailLines=1
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods/test-pod/log?tailLines=1", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", res.StatusCode)
	}
	body, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "init log 2\n" {
		t.Errorf("Expected last line 'init log 2\\n', got %q", string(body))
	}

	// 5. Test follow=true
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods/test-pod/log?follow=true", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", res.StatusCode)
	}

	reader := bufio.NewReader(res.Body)
	
	// Read the pre-existing logs
	line, err := reader.ReadString('\n')
	if err != nil || line != "init log 1\n" {
		t.Fatalf("Expected 'init log 1\\n', got %q, err=%v", line, err)
	}
	line, err = reader.ReadString('\n')
	if err != nil || line != "init log 2\n" {
		t.Fatalf("Expected 'init log 2\\n', got %q, err=%v", line, err)
	}

	// Write live log
	mockEng.mu.Lock()
	stdout := mockEng.stdout
	mockEng.mu.Unlock()

	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = stdout.Write([]byte("live log message\n"))
	}()

	line, err = reader.ReadString('\n')
	if err != nil || line != "live log message\n" {
		t.Errorf("Expected 'live log message\\n', got %q, err=%v", line, err)
	}

	res.Body.Close()
}

func TestServer_GetPodLogs_InvalidParams(t *testing.T) {
	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("mock", mockEng)

	lm := engine.NewLifecycleManager(reg)
	srv := NewServer("127.0.0.1:0", lm)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	pod := &types.Pod{
		TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		Metadata: types.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"lok8s.io/engine": "mock",
				"lok8s.io/target": "dummy",
			},
		},
	}
	srv.storePod(*pod)
	_ = lm.Launch(pod, "mock", "dummy", nil)

	// Test invalid tailLines
	res, err := http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods/test-pod/log?tailLines=abc", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid tailLines, got %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "invalid tailLines parameter") {
		t.Errorf("Expected error message 'invalid tailLines parameter', got %q", string(body))
	}
}
