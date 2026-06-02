package service

import (
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/megawron/lok8s/network"
	"github.com/megawron/lok8s/types"
)

// mockPodLister implements service.PodLister
type mockPodLister struct {
	pods []types.Pod
}

func (m *mockPodLister) ListActivePods() []types.Pod {
	return m.pods
}

func TestServiceProxy_LoadBalancing(t *testing.T) {
	// 1. Start two target backend servers
	var hit1, hit2 int64

	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	p1 := l1.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := l1.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&hit1, 1)
			_, _ = conn.Write([]byte("backend1"))
			conn.Close()
		}
	}()

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	p2 := l2.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := l2.Accept()
			if err != nil {
				return
			}
			atomic.AddInt64(&hit2, 1)
			_, _ = conn.Write([]byte("backend2"))
			conn.Close()
		}
	}()

	// 2. Set up mock PodLister with two backend pods matching selector app=web
	lister := &mockPodLister{
		pods: []types.Pod{
			{
				Metadata: types.ObjectMeta{
					Name:      "web-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "web"},
				},
				Status: types.PodStatus{
					Phase:    types.PodRunning,
					HostPort: p1,
				},
			},
			{
				Metadata: types.ObjectMeta{
					Name:      "web-2",
					Namespace: "default",
					Labels:    map[string]string{"app": "web"},
				},
				Status: types.PodStatus{
					Phase:    types.PodRunning,
					HostPort: p2,
				},
			},
			{
				// This pod doesn't match selector
				Metadata: types.ObjectMeta{
					Name:      "other",
					Namespace: "default",
					Labels:    map[string]string{"app": "db"},
				},
				Status: types.PodStatus{
					Phase:    types.PodRunning,
					HostPort: 9999,
				},
			},
		},
	}

	// 3. Create Service and start proxy
	svc := types.Service{
		Metadata: types.ObjectMeta{
			Name:      "web-svc",
			Namespace: "default",
		},
		Spec: types.ServiceSpec{
			Ports: []types.ServicePort{
				{Port: 0}, // Allocate dynamically
			},
			Selector: map[string]string{"app": "web"},
		},
	}

	pool := network.NewPortPool(36000, 37000)
	pm := NewProxyManager(lister, pool)

	allocatedPort, err := pm.StartProxy(&svc)
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer pm.StopProxy("default", "web-svc")

	// 4. Send requests to service proxy and verify Round-Robin load balancing
	var responses []string
	for i := 0; i < 4; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", allocatedPort))
		if err != nil {
			t.Fatalf("failed to dial service proxy: %v", err)
		}
		buf, err := io.ReadAll(conn)
		if err != nil {
			t.Fatal(err)
		}
		responses = append(responses, string(buf))
		conn.Close()
		time.Sleep(10 * time.Millisecond)
	}

	// Verify we hit both backends twice (since total requests = 4)
	h1 := atomic.LoadInt64(&hit1)
	h2 := atomic.LoadInt64(&hit2)

	if h1 != 2 || h2 != 2 {
		t.Errorf("Expected round-robin 2 hits each, got backend1=%d, backend2=%d", h1, h2)
	}

	// Verify responses alternate
	if responses[0] == responses[1] || responses[1] == responses[2] {
		t.Errorf("Expected alternating responses, got: %v", responses)
	}
}

func TestGenerateServiceEnv(t *testing.T) {
	store := NewStore(nil)
	pool := network.NewPortPool(38000, 38100)
	lister := &mockPodLister{}
	pm := NewProxyManager(lister, pool)

	svc := types.Service{
		Metadata: types.ObjectMeta{
			Name:      "my-cool-service",
			Namespace: "default",
		},
		Spec: types.ServiceSpec{
			Ports: []types.ServicePort{
				{Port: 8080},
			},
		},
	}

	store.Store(svc)

	envs := GenerateServiceEnv("default", store, pm)

	expected := map[string]string{
		"MY_COOL_SERVICE_SERVICE_HOST": "127.0.0.1",
		"MY_COOL_SERVICE_SERVICE_PORT": "8080",
	}

	if len(envs) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(envs))
	}

	for _, ev := range envs {
		expVal, exists := expected[ev.Name]
		if !exists {
			t.Errorf("unexpected env var %s", ev.Name)
		}
		if ev.Value != expVal {
			t.Errorf("expected %s=%s, got %s", ev.Name, expVal, ev.Value)
		}
	}
}
