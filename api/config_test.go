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

	"github.com/megawron/lok8s/config"
	"github.com/megawron/lok8s/controller"
	"github.com/megawron/lok8s/engine"
	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

func TestServer_ConfigMapsCRUD(t *testing.T) {
	reg := engine.NewRegistry()
	pool := network.NewPortPool(30000, 31000)
	configStore := config.NewStore(nil)
	lm := engine.NewLifecycleManager(reg, pool, configStore)
	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controller.NewStore(nil), nil)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// 1. Create a ConfigMap
	cmJSON := `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"metadata": {
			"name": "my-config"
		},
		"data": {
			"app.properties": "env=dev\nverbose=true"
		}
	}`

	res, err := http.Post(
		fmt.Sprintf("%s/api/v1/namespaces/default/configmaps", ts.URL),
		"application/json",
		bytes.NewBufferString(cmJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 201 Created, got %d. Body: %s", res.StatusCode, string(body))
	}

	var createdCM types.ConfigMap
	if err := json.NewDecoder(res.Body).Decode(&createdCM); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if createdCM.Metadata.Name != "my-config" {
		t.Errorf("expected name 'my-config', got %q", createdCM.Metadata.Name)
	}

	// 2. Get the ConfigMap
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/configmaps/my-config", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var fetchedCM types.ConfigMap
	if err := json.NewDecoder(res.Body).Decode(&fetchedCM); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if fetchedCM.Data["app.properties"] != "env=dev\nverbose=true" {
		t.Errorf("unexpected value: %s", fetchedCM.Data["app.properties"])
	}

	// 3. List ConfigMaps
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/configmaps", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var list types.ConfigMapList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if len(list.Items) != 1 || list.Items[0].Metadata.Name != "my-config" {
		t.Errorf("expected 1 configmap named 'my-config', got: %+v", list.Items)
	}

	// 4. Delete the ConfigMap
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/namespaces/default/configmaps/my-config", ts.URL), nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}
	res.Body.Close()

	// 5. Verify it is deleted
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/configmaps/my-config", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found after deletion, got %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestServer_SecretsCRUD(t *testing.T) {
	reg := engine.NewRegistry()
	pool := network.NewPortPool(30000, 31000)
	configStore := config.NewStore(nil)
	lm := engine.NewLifecycleManager(reg, pool, configStore)
	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controller.NewStore(nil), nil)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// 1. Create a Secret
	secretJSON := `{
		"apiVersion": "v1",
		"kind": "Secret",
		"metadata": {
			"name": "my-secret"
		},
		"stringData": {
			"db.password": "supersecure"
		}
	}`

	res, err := http.Post(
		fmt.Sprintf("%s/api/v1/namespaces/default/secrets", ts.URL),
		"application/json",
		bytes.NewBufferString(secretJSON),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 201 Created, got %d. Body: %s", res.StatusCode, string(body))
	}

	var createdSecret types.Secret
	if err := json.NewDecoder(res.Body).Decode(&createdSecret); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if createdSecret.Metadata.Name != "my-secret" {
		t.Errorf("expected name 'my-secret', got %q", createdSecret.Metadata.Name)
	}

	// 2. Get the Secret
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/secrets/my-secret", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var fetchedSecret types.Secret
	if err := json.NewDecoder(res.Body).Decode(&fetchedSecret); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	// Stored in Data, not stringData (which is plain text write-only field)
	if string(fetchedSecret.Data["db.password"]) != "supersecure" {
		t.Errorf("unexpected value: %s", string(fetchedSecret.Data["db.password"]))
	}

	// 3. List Secrets
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/secrets", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}

	var list types.SecretList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if len(list.Items) != 1 || list.Items[0].Metadata.Name != "my-secret" {
		t.Errorf("expected 1 secret named 'my-secret', got: %+v", list.Items)
	}

	// 4. Delete the Secret
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/namespaces/default/secrets/my-secret", ts.URL), nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}
	res.Body.Close()

	// 5. Verify it is deleted
	res, err = http.Get(fmt.Sprintf("%s/api/v1/namespaces/default/secrets/my-secret", ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found after deletion, got %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestServer_ConfigMaps_Invalid(t *testing.T) {
	reg := engine.NewRegistry()
	pool := network.NewPortPool(30000, 31000)
	configStore := config.NewStore(nil)
	lm := engine.NewLifecycleManager(reg, pool, configStore)
	srv := NewServer("127.0.0.1:0", lm, pool, configStore, controller.NewStore(nil), nil)

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	res, err := http.Post(
		fmt.Sprintf("%s/api/v1/namespaces/default/configmaps", ts.URL),
		"application/json",
		strings.NewReader(`{invalid`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", res.StatusCode)
	}
	res.Body.Close()
}
