package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/models"
)

func withLocalHTTPServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on ephemeral port: %v", err)
	}

	server := &http.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shutdown server: %v", err)
		}

		if err := <-errCh; err != http.ErrServerClosed && err != nil {
			t.Errorf("listener exit: %v", err)
		}
	})

	return "http://" + listener.Addr().String()
}

func TestHealthzAndReadyzOverLiveListener(t *testing.T) {
	srv, _, _ := newTestServer(t)
	baseURL := withLocalHTTPServer(t, srv.Router())
	client := &http.Client{Timeout: 2 * time.Second}

	t.Run("healthz is reachable and reports ok", func(t *testing.T) {
		t.Helper()
		resp, err := client.Get(baseURL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /healthz: status=%d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /healthz: %v", err)
		}
		if body["status"] != "ok" {
			t.Fatalf("/healthz: status=%v, want ok", body["status"])
		}
	})

	t.Run("readyz is reachable and ready after startup", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /readyz: status=%d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /readyz: %v", err)
		}
		if body["ready"] != true {
			t.Fatalf("/readyz: ready=%v, want true", body["ready"])
		}
	})
}

func TestReadyzOverLiveListener_reportsUnreadyWhenStorageIssues(t *testing.T) {
	srv, db, _ := newTestServer(t)
	baseURL := withLocalHTTPServer(t, srv.Router())
	client := &http.Client{Timeout: 2 * time.Second}

	issue := models.StorageIssue{
		VaultID:     "vault-a",
		StorageKey:  "vaults/vault-a/files/Missing.md",
		Kind:        "missing",
		Detail:      "missing",
		FirstSeenAt: time.Now(),
		LastSeenAt:  time.Now(),
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("insert storage issue: %v", err)
	}

	t.Run("readyz is 503 when unresolved issues exist", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("GET /readyz: status=%d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /readyz: %v", err)
		}
		if body["ready"] != false {
			t.Fatalf("/readyz: ready=%v, want false", body["ready"])
		}
	})

	if err := db.Model(&models.StorageIssue{}).Where("id = ?", issue.ID).
		Update("resolved_at", time.Now()).Error; err != nil {
		t.Fatalf("resolve storage issue: %v", err)
	}

	t.Run("readyz is ready again after issue resolved", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /readyz after resolve: status=%d", resp.StatusCode)
		}

		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /readyz: %v", err)
		}
		if body["ready"] != true {
			t.Fatalf("/readyz after resolve: ready=%v, want true", body["ready"])
		}
	})
}
