package update

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise the real download, extraction, staging and helper verification. Earlier
// service tests mocked verification, hiding archive-vs-executable digest errors.
func TestService_ArchiveDigestHandoff(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte(`package main
import "fmt"
func main() { fmt.Println("9.9.9") }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(dir, "oss-server")
	buildCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "go", "build", "-o", binPath, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build version fixture: %v\n%s", err, out)
	}
	binary, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	assetName, err := AssetName("9.9.9", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	archive := makeTarGz(t, map[string][]byte{"oss-server": binary})
	if strings.HasSuffix(assetName, ".zip") {
		archive = makeZip(t, map[string][]byte{"oss-server.exe": binary})
	}
	archiveDigest, binaryDigest := digestOfBytes(archive), digestOfBytes(binary)
	if archiveDigest == binaryDigest {
		t.Fatal("fixture must have different archive and binary digests")
	}
	for _, scenario := range []string{"success", "corrupt_download", "tampered_staging"} {
		t.Run(scenario, func(t *testing.T) {
			mgr, up, cfg, exePath := newServiceTestManager(t)
			payload := bytes.Clone(archive)
			if scenario == "corrupt_download" {
				payload[len(payload)/2] ^= 1
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				_, _ = w.Write(payload)
			}))
			defer srv.Close()
			cand, err := NewCandidate("9.9.9", runtime.GOOS, runtime.GOARCH,
				srv.URL+"/"+assetName, "https://example.com/releases/tag/9.9.9",
				int64(len(archive)), 1, 1, archiveDigest)
			if err != nil {
				t.Fatal(err)
			}
			checked, err := mgr.IssueChecked(*cand, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			var markerPath string
			origLaunch, origWait := launchHelperFn, waitForParentFn
			origStart, origProbe := startNewServerFn, probeReadyzWithVersionFn
			t.Cleanup(func() {
				launchHelperFn, waitForParentFn = origLaunch, origWait
				startNewServerFn, probeReadyzWithVersionFn = origStart, origProbe
			})
			// Only process lifecycle and readiness are simulated; file integrity,
			// executable magic, --version, persistence and replacement stay real.
			launchHelperFn = func(_, path string) error { markerPath = path; return nil }
			waitForParentFn = func(int, time.Duration) error { return nil }
			startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return nil, nil }
			probeReadyzWithVersionFn = func(string, string, time.Duration, time.Duration) error { return nil }
			svc := NewService(mgr, up, cfg)
			shutdown := make(chan struct{}, 1)
			svc.SetOnShutdown(func() { shutdown <- struct{}{} })
			op, err := svc.StartHelperUpdate(context.Background(), checked.ID, "", "")
			if scenario == "corrupt_download" {
				if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
					t.Fatalf("expected download digest rejection, got %v", err)
				}
				if markerPath != "" || mgr.ActiveOperation() != nil || svc.shutdownFired.Load() {
					t.Fatal("corrupt download must not initiate handoff or shutdown")
				}
				return
			}
			if err != nil {
				t.Fatalf("archive handoff: %v", err)
			}
			select {
			case <-shutdown:
			case <-time.After(time.Second):
				t.Fatal("successful handoff must signal shutdown")
			}
			data, err := os.ReadFile(markerPath)
			if err != nil {
				t.Fatal(err)
			}
			var marker HandoffMarker
			if err := json.Unmarshal(data, &marker); err != nil {
				t.Fatal(err)
			}
			if marker.Digest != binaryDigest {
				t.Fatalf("helper digest = %s, want executable digest %s", marker.Digest, binaryDigest)
			}
			stored, err := mgr.ValidateChecked(checked.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Digest != archiveDigest || op.Candidate.Digest != archiveDigest {
				t.Fatal("release asset digest must remain unchanged")
			}
			old, err := os.ReadFile(exePath)
			if err != nil || string(old) != "old-binary" {
				t.Fatalf("handoff modified running executable: %v", err)
			}
			wantCode, wantState, wantBinary := 0, StateDone, binary
			if scenario == "tampered_staging" {
				if err := os.WriteFile(marker.StagedPath, []byte("tampered"), 0o755); err != nil {
					t.Fatal(err)
				}
				wantCode, wantState, wantBinary = 4, StateFailed, old
			}
			if code := RunHelper(markerPath); code != wantCode {
				t.Fatalf("helper exit code = %d, want %d", code, wantCode)
			}
			got, err := os.ReadFile(exePath)
			if err != nil || !bytes.Equal(got, wantBinary) {
				t.Fatalf("unexpected installed executable: %v", err)
			}
			final, err := mgr.GetOperation(op.ID)
			if err != nil || final.State != wantState {
				t.Fatalf("unexpected terminal operation: %+v, %v", final, err)
			}
		})
	}
}

func TestHandoff_RejectsInvalidPreparedDigest(t *testing.T) {
	for _, scenario := range []string{"empty_digest", "malformed_digest", "changed_candidate"} {
		t.Run(scenario, func(t *testing.T) {
			mgr, up, _, exePath := newServiceTestManager(t)
			id := newCheckedForHelper(t, mgr, "9.9.9")
			candidatePath := candidatePathFor(id)
			digest := fakeDigestForFile(candidatePath)
			wantError := "prepared executable digest missing or malformed"
			switch scenario {
			case "empty_digest":
				digest = ""
			case "malformed_digest":
				digest = "sha256:bad"
			case "changed_candidate":
				if err := os.WriteFile(candidatePath, []byte("changed after hashing"), 0o755); err != nil {
					t.Fatal(err)
				}
				wantError = "staged digest mismatch"
			}
			origLaunch := launchHelperFn
			t.Cleanup(func() { launchHelperFn = origLaunch })
			launchHelperFn = func(string, string) error {
				t.Fatal("must not launch helper for invalid prepared binary")
				return nil
			}
			_, err := up.InitiateHelperHandoff(mgr, id, candidatePath, digest,
				"http://127.0.0.1:0/readyz", []string{exePath}, filepath.Dir(exePath))
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("want %s, got %v", wantError, err)
			}
			if mgr.ActiveOperation() != nil {
				t.Fatal("invalid prepared binary must not leave an active operation")
			}
			old, err := os.ReadFile(exePath)
			if err != nil || string(old) != "old-binary" {
				t.Fatalf("rejected handoff changed current executable: %v", err)
			}
		})
	}
}
