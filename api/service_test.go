package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

func TestServer_ServicesCRUD(t *testing.T) {
	reg := engine.NewRegistry()
	pool := network.NewPortPool(35000, 35500)
	lm := engine.NewLifecycleManager(reg, pool)
	srv := NewServer("127.0.0.1:0", lm, pool)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// 1. Create a Service
	svcJSON := `{
		"apiVersion": "v1",
		"kind": "Service",
		"metadata": {
			"name": "test-svc",
			"labels": {
				"app": "test"
			}
		},
		"spec": {
			"ports": [
				{
					"name": "http",
					"port": 0
				}
			],
			"selector": {
				"app": "test"
			}
		}
	}`

	res, err := http.Post(
		fmt.Sprintf("%s/api/v1/namespaces/default/services", ts.URL),
		"application/json",
		bytes.NewBufferString(svcJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 201 Created, got %d. Body: %s", res.StatusCode, string(body))
	}

	var createdSvc types.Service
	if err := json.NewDecoder(res.Body).Decode(&createdSvc); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if createdSvc.Metadata.Name != "test-svc" {
		t.Errorf("expected name 'test-svc', got %q", createdSvc.Metadata.Name)
	}
	if len(createdSvc.Spec.Ports) == 0 || createdSvc.Spec.Ports[0].NodePort == 0 {
		t.Errorf("expected allocated NodePort, got spec: %+v", createdSvc.Spec)
	}

	// 2. Get the Service
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/services/test-svc", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var fetchedSvc types.Service
	if err := json.NewDecoder(res.Body).Decode(&fetchedSvc); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if fetchedSvc.Metadata.UID != createdSvc.Metadata.UID {
		t.Errorf("expected UID %s, got %s", createdSvc.Metadata.UID, fetchedSvc.Metadata.UID)
	}

	// 3. List Services
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/services", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var list types.ServiceList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if len(list.Items) != 1 || list.Items[0].Metadata.Name != "test-svc" {
		t.Errorf("expected 1 service named 'test-svc' in list, got: %+v", list.Items)
	}

	// 4. Delete the Service
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/namespaces/default/services/test-svc", ts.URL), nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}
	res.Body.Close()

	// 5. Verify it is deleted
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/services/test-svc", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found after deletion, got %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestServer_ServicesCRUD_Invalid(t *testing.T) {
	reg := engine.NewRegistry()
	pool := network.NewPortPool(35501, 36000)
	lm := engine.NewLifecycleManager(reg, pool)
	srv := NewServer("127.0.0.1:0", lm, pool)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// Post invalid JSON
	res, err := http.Post(
		fmt.Sprintf("%s/api/v1/namespaces/default/services", ts.URL),
		"application/json",
		strings.NewReader(`{invalid`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for invalid JSON, got %d", res.StatusCode)
	}
	res.Body.Close()
}
