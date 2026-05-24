package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/megawron/lok8s/types"
)

func captureStdout(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestCLI_Apply(t *testing.T) {
	// 1. Mock server that expects a POST to /api/v1/namespaces/default/pods
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/namespaces/default/pods" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m["metadata"].(map[string]interface{})["name"] != "nginx-pod" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// 2. Create temporary manifest file
	manifestContent := `
apiVersion: v1
kind: Pod
metadata:
  name: nginx-pod
spec:
  containers:
  - name: nginx
    image: nginx
`
	tmpFile, err := os.CreateTemp("", "manifest-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(manifestContent)); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// 3. Run apply command and capture output
	output := captureStdout(func() {
		handleApply(server.URL, "default", []string{"-f", tmpFile.Name()})
	})

	if !strings.Contains(output, "pod/nginx-pod created/configured") {
		t.Errorf("Expected success output, got %q", output)
	}
}

func TestCLI_GetList(t *testing.T) {
	// 1. Mock server returning a metav1.Table JSON payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/default/pods" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "as=Table") {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}

		table := types.Table{
			TypeMeta: types.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "Table"},
			ColumnDefinitions: []types.TableColumnDefinition{
				{Name: "Name", Type: "string"},
				{Name: "Ready", Type: "string"},
				{Name: "Status", Type: "string"},
			},
			Rows: []types.TableRow{
				{Cells: []interface{}{"pod-a", "1/1", "Running"}},
				{Cells: []interface{}{"pod-b-longer-name", "0/1", "Pending"}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(table)
	}))
	defer server.Close()

	// 2. Run get command and capture output
	output := captureStdout(func() {
		handleGet(server.URL, "default", []string{"pods"})
	})

	// 3. Verify headers and rows are formatted with correct spacing
	if !strings.Contains(output, "NAME") || !strings.Contains(output, "READY") || !strings.Contains(output, "STATUS") {
		t.Errorf("Headers missing in output: %q", output)
	}
	if !strings.Contains(output, "pod-a") || !strings.Contains(output, "pod-b-longer-name") {
		t.Errorf("Row data missing in output: %q", output)
	}
}

func TestCLI_GetDetail(t *testing.T) {
	// Mock server returning a raw pod resource
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/default/pods/my-pod" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"name":"my-pod"},"status":{"phase":"Running"}}`))
	}))
	defer server.Close()

	output := captureStdout(func() {
		handleGet(server.URL, "default", []string{"pods", "my-pod"})
	})

	if !strings.Contains(output, `"my-pod"`) || !strings.Contains(output, `"Running"`) {
		t.Errorf("Expected formatted JSON detail output, got: %q", output)
	}
}

func TestCLI_Delete(t *testing.T) {
	// Mock server returning deletion success
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/apis/apps/v1/namespaces/default/deployments/web" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"Success"}`))
	}))
	defer server.Close()

	output := captureStdout(func() {
		handleDelete(server.URL, "default", []string{"deployment", "web"})
	})

	if !strings.Contains(output, "deployment/web deleted") {
		t.Errorf("Expected deletion message, got: %q", output)
	}
}

func TestCLI_Logs(t *testing.T) {
	// Mock server returning log stream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/default/pods/my-pod/log" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("log line 1\nlog line 2\n"))
	}))
	defer server.Close()

	output := captureStdout(func() {
		handleLogs(server.URL, "default", []string{"my-pod"})
	})

	expected := "log line 1\nlog line 2\n"
	if output != expected {
		t.Errorf("Expected logs %q, got %q", expected, output)
	}
}
