// 更新辅助
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/oss/oss-server/internal/version"
)

// RunHelper is the entry point for the helper process. It never returns
// normally — it exits the process with the appropriate code. Caller in
// main.go should call os.Exit(RunHelper()).
func RunHelper(markerPath string) int {
	if markerPath == "" {
		fmt.Fprintln(os.Stderr, "helper: marker path empty")
		return 0
	}
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read marker %s: %v (ordinary startup, no rollback)\n", markerPath, err)
		return 0
	}
	var m HandoffMarker
	if err := json.Unmarshal(markerData, &m); err != nil {
		fmt.Fprintf(os.Stderr, "helper: corrupt marker: %v\n", err)
		return 2
	}
	// Validate durable marker references active operation — if not active, this is ordinary startup; do not rollback.
	_, op, err := recoverActiveMarker(markerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: no active marker, skipping: %v\n", err)
		// Do not perform rollback; just exit. Marker retention rule: no valid marker -> no action.
		return 0
	}
	// Validate safe paths/digest/target before any mutation.
	if err := validateMarkerSafe(&m); err != nil {
		fmt.Fprintf(os.Stderr, "helper: unsafe marker: %v\n", err)
		return 2
	}
	// Ensure helper handles parent wait before any mutation.
	parentPID := m.ParentPID
	if parentPID > 0 {
		if err := waitForParentFn(parentPID, 10*time.Second); err != nil {
			_ = recordRollback(&m, fmt.Sprintf("parent wait failed: %v", err))
			return 3
		}
	}

	// Re-verify staged file exists and matches digest/magic/version BEFORE swap.
	if err := verifyStagedFileFn(m.StagedPath, m.Digest, m.TargetVersion); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("staged verification failed: %v", err))
		return 4
	}
	// Ensure backup exists before swap (required for rollback).
	if _, err := os.Stat(m.BackupPath); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("backup missing: %v", err))
		return 4
	}

	// Atomic replacement after parent exit.
	if err := atomicReplaceFn(m.StagedPath, m.ExecPath); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("swap failed: %v", err))
		return 5
	}

	// Launch new binary preserving original args/env/workdir.
	child, err := startNewServerFn(&m)
	if err != nil {
		// Swap succeeded but launch failed — rollback.
		_ = recordRollback(&m, fmt.Sprintf("launch new binary failed: %v", err))
		return 6
	}

	// Probe /readyz for ready:true and exact target version.
	if err := probeReadyzWithVersionFn(m.ReadyURL, m.TargetVersion, 30*time.Second, 500*time.Millisecond); err != nil {
		// Terminate failed child if still alive.
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
		}
		_ = recordRollback(&m, fmt.Sprintf("readiness/version check failed: %v", err))
		return 7
	}
	// Child may have exited quickly after becoming ready; check.
	if child != nil && child.Process != nil {
		done := make(chan error, 1)
		go func() { _, e := child.Process.Wait(); done <- e }()
		select {
		case err := <-done:
			if err != nil {
				_ = recordRollback(&m, fmt.Sprintf("child exited after ready: %v", err))
				return 7
			}
		case <-time.After(200 * time.Millisecond):
			// Still running — success; detach.
			_ = child.Process.Release()
		}
	}
	// Detect wrong version already covered by probe; if probe passed, version matched.
	_ = op // reference to avoid unused
	_ = recordSuccess(&m)
	return 0
}

var startNewServerFn = startNewServer

func SetStartNewServerFn(fn func(*HandoffMarker) (*exec.Cmd, error)) {
	if fn == nil {
		startNewServerFn = startNewServer
	} else {
		startNewServerFn = fn
	}
}
func SetProbeReadyzWithVersionFn(fn func(string, string, time.Duration, time.Duration) error) {
	if fn == nil {
		probeReadyzWithVersionFn = probeReadyzWithVersion
	} else {
		probeReadyzWithVersionFn = fn
	}
}

func startNewServer(m *HandoffMarker) (*exec.Cmd, error) {
	args := m.OrigArgs
	if len(args) == 0 {
		args = []string{m.ExecPath}
	}
	// First element is program name; exec.Command expects args without it.
	childArgs := []string{}
	if len(args) > 1 {
		childArgs = args[1:]
	}
	cmd := exec.Command(m.ExecPath, childArgs...)
	cmd.Env = os.Environ()
	if m.WorkDir != "" {
		cmd.Dir = m.WorkDir
	}
	// Inherit stdio to avoid silent failures in early startup; helper logs remain.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

var probeReadyzWithVersionFn = probeReadyzWithVersion

func probeReadyzWithVersion(readyURL, wantVersion string, timeout, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	wantNorm := version.Normalize(wantVersion)
	var lastErr error
	for {
		ready, ver, err := readyzOnceWithVersion(client, readyURL)
		if err == nil && ready {
			if wantNorm == "" || version.Normalize(ver) == wantNorm {
				return nil
			}
			lastErr = fmt.Errorf("ready but version mismatch: got %q (normalized %q) want %q (normalized %q)", ver, version.Normalize(ver), wantVersion, wantNorm)
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("not ready (version %q)", ver)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(pollInterval)
		if time.Now().After(deadline) {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout after %s", timeout)
	}
	return fmt.Errorf("readyz probe failed: %w", lastErr)
}

func readyzOnceWithVersion(client *http.Client, url string) (ready bool, version string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var payload struct {
		Ready   bool   `json:"ready"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(body, &payload)
	if resp.StatusCode != http.StatusOK {
		return false, payload.Version, fmt.Errorf("readyz %d", resp.StatusCode)
	}
	if !payload.Ready {
		return false, payload.Version, fmt.Errorf("ready=false")
	}
	return true, payload.Version, nil
}

