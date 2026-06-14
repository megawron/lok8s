package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/megawron/lok8s/types"
)

func TestProbeRunner_HTTP(t *testing.T) {
	var requestCount int64
	var shouldFail int32 // 0 = false, 1 = true

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		if atomic.LoadInt32(&shouldFail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	var port int
	_, err := fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if err != nil {
		t.Fatalf("failed to parse test server port: %v", err)
	}

	probe := types.Probe{
		HTTPGet: &types.HTTPGetAction{
			Port: port,
			Path: "/",
		},
		PeriodSeconds:    1,
		FailureThreshold: 2,
		SuccessThreshold: 1,
	}

	failCalled := make(chan struct{}, 1)
	recoverCalled := make(chan struct{}, 1)

	runner := NewProbeRunner(probe, "test-pod", "liveness",
		func() { failCalled <- struct{}{} },
		func() { recoverCalled <- struct{}{} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runner.Run(ctx)

	// Wait for a few successful checks
	time.Sleep(1500 * time.Millisecond)
	if atomic.LoadInt64(&requestCount) == 0 {
		t.Fatal("probe executed 0 times, expected successful runs")
	}

	// Trigger failure
	atomic.StoreInt32(&shouldFail, 1)

	// Wait for failure threshold
	select {
	case <-failCalled:
		t.Log("liveness failure successfully triggered")
	case <-time.After(3 * time.Second):
		t.Fatal("liveness failure callback was not called")
	}

	// Trigger recovery
	atomic.StoreInt32(&shouldFail, 0)

	// Wait for recovery threshold
	select {
	case <-recoverCalled:
		t.Log("liveness recovery successfully triggered")
	case <-time.After(3 * time.Second):
		t.Fatal("liveness recovery callback was not called")
	}
}

func TestProbeRunner_TCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port

	probe := types.Probe{
		TCPSocket: &types.TCPSocketAction{
			Port: port,
		},
		PeriodSeconds:    1,
		FailureThreshold: 1,
	}

	failCalled := make(chan struct{}, 1)
	runner := NewProbeRunner(probe, "test-pod", "readiness",
		func() { failCalled <- struct{}{} },
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runner.Run(ctx)

	// Check should succeed initially
	time.Sleep(1200 * time.Millisecond)
	select {
	case <-failCalled:
		t.Fatal("probe failed unexpectedly when port is listening")
	default:
	}

	l.Close()

	select {
	case <-failCalled:
		t.Log("readiness probe failure detected closed port successfully")
	case <-time.After(3 * time.Second):
		t.Fatal("probe failed to report failure when port was closed")
	}
}
