package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

func TestKubectlCompatibility(t *testing.T) {
	// Setup mock engine & LifecycleManager
	reg := engine.NewRegistry()
	mockEng := &mockEngine{
		status: make(map[string]types.PodPhase),
	}
	reg.Register("mock", mockEng)

	pool := network.NewPortPool(30000, 32767)
	configStore := config.NewStore()
	lm := engine.NewLifecycleManager(reg, pool, configStore)
	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controller.NewStore())

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	t.Run("API Discovery Root", func(t *testing.T) {
		res, err := http.Get(fmt.Sprintf("%s/api", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", res.StatusCode)
		}

		var groupList types.APIGroupList
		if err := json.NewDecoder(res.Body).Decode(&groupList); err != nil {
			t.Fatal(err)
		}

		if groupList.Kind != "APIVersions" || len(groupList.APIVersions) != 1 || groupList.APIVersions[0] != "v1" {
			t.Errorf("Unexpected APIGroupList: %+v", groupList)
		}
	})

	t.Run("API Discovery V1 Resources", func(t *testing.T) {
		res, err := http.Get(fmt.Sprintf("%s/api/v1", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", res.StatusCode)
		}

		var resList types.APIResourceList
		if err := json.NewDecoder(res.Body).Decode(&resList); err != nil {
			t.Fatal(err)
		}

		if resList.Kind != "APIResourceList" || resList.GroupVersion != "v1" {
			t.Errorf("Unexpected resource list metadata: %+v", resList)
		}

		foundPods := false
		for _, resource := range resList.APIResources {
			if resource.Name == "pods" {
				foundPods = true
				if !resource.Namespaced || resource.Kind != "Pod" {
					t.Errorf("Pods resource malformed: %+v", resource)
				}
			}
		}

		if !foundPods {
			t.Error("Expected to find 'pods' in API resources list")
		}
	})

	t.Run("APIs Discovery", func(t *testing.T) {
		res, err := http.Get(fmt.Sprintf("%s/apis", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", res.StatusCode)
		}

		var m map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
			t.Fatal(err)
		}

		if m["kind"] != "APIGroupList" {
			t.Errorf("Expected APIGroupList kind, got %v", m["kind"])
		}
	})

	t.Run("Version API", func(t *testing.T) {
		res, err := http.Get(fmt.Sprintf("%s/version", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		var m map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
			t.Fatal(err)
		}

		if m["gitVersion"] != "v1.29.0" {
			t.Errorf("Expected gitVersion 'v1.29.0', got %v", m["gitVersion"])
		}
	})

	t.Run("Table Content Negotiation", func(t *testing.T) {
		// Launch a mock pod first
		pod := &types.Pod{
			TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			Metadata: types.ObjectMeta{
				UID:       "pod-table-uid",
				Name:      "test-table-pod",
				Namespace: "default",
				Annotations: map[string]string{
					"lok8s.io/engine": "mock",
					"lok8s.io/target": "dummy",
				},
				Labels: map[string]string{
					"app": "frontend",
				},
			},
			Spec: types.PodSpec{
				Containers: []types.Container{{Name: "main"}},
			},
		}
		srv.storePod(*pod)
		if err := lm.Launch(pod, "mock", "dummy", nil); err != nil {
			t.Fatalf("Launch pod failed: %v", err)
		}

		// Request listing as Table
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/namespaces/default/pods", ts.URL), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", res.StatusCode)
		}

		var table types.Table
		if err := json.NewDecoder(res.Body).Decode(&table); err != nil {
			t.Fatal(err)
		}

		if table.Kind != "Table" {
			t.Errorf("Expected kind 'Table', got %q", table.Kind)
		}

		if len(table.Rows) == 0 {
			t.Fatal("Expected at least one row in table output")
		}

		if table.Rows[0].Cells[0] != "test-table-pod" {
			t.Errorf("Expected cell 0 to be pod name 'test-table-pod', got %v", table.Rows[0].Cells[0])
		}
	})

	t.Run("Selector Filtering", func(t *testing.T) {
		// Add another pod
		pod2 := &types.Pod{
			TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			Metadata: types.ObjectMeta{
				UID:       "pod-table-uid-2",
				Name:      "other-pod",
				Namespace: "default",
				Annotations: map[string]string{
					"lok8s.io/engine": "mock",
					"lok8s.io/target": "dummy",
				},
				Labels: map[string]string{
					"app": "backend",
				},
			},
			Spec: types.PodSpec{
				Containers: []types.Container{{Name: "main"}},
			},
		}
		srv.storePod(*pod2)
		if err := lm.Launch(pod2, "mock", "dummy", nil); err != nil {
			t.Fatalf("Launch pod failed: %v", err)
		}

		// List with labelSelector=app=backend
		res, err := http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods?labelSelector=app%%3Dbackend", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		var list types.PodList
		if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}

		if len(list.Items) != 1 {
			t.Fatalf("Expected 1 filtered pod, got %d", len(list.Items))
		}

		if list.Items[0].Metadata.Name != "other-pod" {
			t.Errorf("Expected pod 'other-pod', got %q", list.Items[0].Metadata.Name)
		}
	})

	t.Run("Streaming Watch", func(t *testing.T) {
		// Stream watch on default namespace
		res, err := http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/pods?watch=true", ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", res.StatusCode)
		}

		reader := bufio.NewReader(res.Body)

		// Wait, the current active pods ("test-table-pod" and "other-pod") are broadcast as ADDED events on start of watch.
		// Let's read the first event.
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}

		var event1 types.WatchEvent
		if err := json.Unmarshal(bytes.TrimSpace(line), &event1); err != nil {
			t.Fatalf("Failed to parse event: %v", err)
		}

		if event1.Type != "ADDED" {
			t.Errorf("Expected initial event to be ADDED, got %q", event1.Type)
		}

		// Read the second pre-existing pod's ADDED event.
		line2, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}

		var event2 types.WatchEvent
		if err := json.Unmarshal(bytes.TrimSpace(line2), &event2); err != nil {
			t.Fatalf("Failed to parse second event: %v", err)
		}

		if event2.Type != "ADDED" {
			t.Errorf("Expected second event to be ADDED, got %q", event2.Type)
		}

		// Now launch a new pod asynchronously and verify it generates a live watch event
		newPod := &types.Pod{
			TypeMeta: types.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			Metadata: types.ObjectMeta{
				UID:       "new-live-pod-uid",
				Name:      "live-pod",
				Namespace: "default",
				Annotations: map[string]string{
					"lok8s.io/engine": "mock",
					"lok8s.io/target": "dummy",
				},
			},
			Spec: types.PodSpec{
				Containers: []types.Container{{Name: "main"}},
			},
		}

		go func() {
			time.Sleep(100 * time.Millisecond)
			srv.storePod(*newPod)
			_ = lm.Launch(newPod, "mock", "dummy", nil)
		}()

		// Read the live event
		line3, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}

		var event3 types.WatchEvent
		if err := json.Unmarshal(bytes.TrimSpace(line3), &event3); err != nil {
			t.Fatalf("Failed to parse third event: %v", err)
		}

		if event3.Type != "ADDED" || event3.Object.Metadata.Name != "live-pod" {
			t.Errorf("Expected ADDED event for 'live-pod', got %s for %q", event3.Type, event3.Object.Metadata.Name)
		}
	})
}
