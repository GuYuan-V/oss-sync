package server

import (
	"net/http"
	"net/url"
	"testing"
)

func TestVaultRolesAuthorizeSyncAndManagement(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	ownerToken := registerAndLogin(t, router, "vault-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)

	code, managerLogin := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "vault-manager", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register manager: %d %v", code, managerLogin)
	}
	managerToken := managerLogin["token"].(string)
	code, _ = doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "vault-participant", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register participant: %d", code)
	}

	code, _ = doJSON(t, router, http.MethodPost, "/api/vaults/"+url.PathEscape(vaultID)+"/members", ownerToken, map[string]string{"username": "vault-manager", "role": "manager"})
	if code != http.StatusNoContent {
		t.Fatalf("add manager: %d", code)
	}
	code, _ = doJSON(t, router, http.MethodPost, "/api/vaults/"+url.PathEscape(vaultID)+"/members", managerToken, map[string]string{"username": "vault-participant", "role": "participant"})
	if code != http.StatusNoContent {
		t.Fatalf("manager add participant: %d", code)
	}
	code, _ = doJSON(t, router, http.MethodPost, "/api/vaults/"+url.PathEscape(vaultID)+"/members", managerToken, map[string]string{"username": "vault-participant", "role": "manager"})
	if code != http.StatusForbidden {
		t.Fatalf("manager promoted participant: %d", code)
	}

	code, listed := doJSON(t, router, http.MethodGet, "/api/vaults", managerToken, nil)
	if code != http.StatusOK || listed["vaults"].([]any)[0].(map[string]any)["access_role"] != "manager" {
		t.Fatalf("manager vault list: %d %v", code, listed)
	}
	code, body := uploadV2(t, router, managerToken, vaultID, "Shared.md", "manager-write", 0, "manager-device", "manager-write")
	if code != http.StatusOK || body["path"] != "Shared.md" {
		t.Fatalf("manager sync upload: %d %v", code, body)
	}

	code, participantLogin := doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]string{"username": "vault-participant", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("participant login: %d", code)
	}
	participantToken := participantLogin["token"].(string)
	code, _ = doJSON(t, router, http.MethodPatch, "/api/vaults/"+url.PathEscape(vaultID), participantToken, map[string]string{"name": "nope"})
	if code != http.StatusForbidden {
		t.Fatalf("participant updated vault: %d", code)
	}
	code, _ = doJSON(t, router, http.MethodPost, "/api/shares", participantToken, map[string]any{"vault_id": vaultID, "target_path": "Shared.md"})
	if code != http.StatusForbidden {
		t.Fatalf("participant created share: %d", code)
	}
}
