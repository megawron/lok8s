package engine

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/megawron/lok8s/types"
)

type ProbeRunner struct {
	probe     types.Probe
	podName   string
	probeType string

	onThresholdReached func()
	onRecovered        func()
}

func NewProbeRunner(probe types.Probe, podName, probeType string, onFail, onRecover func()) *ProbeRunner {
	return &ProbeRunner{
		probe:              probe,
		podName:            podName,
		probeType:          probeType,
		onThresholdReached: onFail,
		onRecovered:        onRecover,
	}
}

func (pr *ProbeRunner) Run(ctx context.Context) {
	delay := time.Duration(pr.probe.InitialDelaySeconds) * time.Second
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}

	period := pr.probe.PeriodSeconds
	if period <= 0 {
		period = 10
	}

	failThreshold := pr.probe.FailureThreshold
	if failThreshold <= 0 {
		failThreshold = 3
	}

	successThreshold := pr.probe.SuccessThreshold
	if successThreshold <= 0 {
		successThreshold = 1
	}

	var consecutiveFails int
	var consecutiveSuccesses int
	failed := false

	ticker := time.NewTicker(time.Duration(period) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if pr.execute(ctx) {
				consecutiveFails = 0
				consecutiveSuccesses++
				if failed && consecutiveSuccesses >= successThreshold {
					failed = false
					log.Printf("[%s] pod %q probe recovered", pr.probeType, pr.podName)
					if pr.onRecovered != nil {
						pr.onRecovered()
					}
				}
			} else {
				consecutiveSuccesses = 0
				consecutiveFails++
				if !failed && consecutiveFails >= failThreshold {
					failed = true
					log.Printf("[%s] pod %q probe threshold reached (%d consecutive failures)",
						pr.probeType, pr.podName, consecutiveFails)
					if pr.onThresholdReached != nil {
						pr.onThresholdReached()
					}
					consecutiveFails = 0
				}
			}
		}
	}
}

func (pr *ProbeRunner) execute(ctx context.Context) bool {
	timeout := pr.probe.TimeoutSeconds
	if timeout <= 0 {
		timeout = 1
	}

	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	switch {
	case pr.probe.HTTPGet != nil:
		return runHTTPProbe(probeCtx, pr.probe.HTTPGet)
	case pr.probe.TCPSocket != nil:
		return runTCPProbe(probeCtx, pr.probe.TCPSocket)
	case pr.probe.Exec != nil:
		return runExecProbe(probeCtx, pr.probe.Exec)
	default:
		return true
	}
}

func runHTTPProbe(ctx context.Context, action *types.HTTPGetAction) bool {
	scheme := action.Scheme
	if scheme == "" {
		scheme = "http"
	}

	url := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, action.Port, action.Path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func runTCPProbe(ctx context.Context, action *types.TCPSocketAction) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", action.Port)

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func runExecProbe(ctx context.Context, action *types.ExecAction) bool {
	if len(action.Command) == 0 {
		return false
	}

	cmd := exec.CommandContext(ctx, action.Command[0], action.Command[1:]...)
	return cmd.Run() == nil
}
