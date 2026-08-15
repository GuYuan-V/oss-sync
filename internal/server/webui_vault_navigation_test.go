package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestWebConsoleVaultSidebar_whenVaultOpen_identifiesVaultAndLinksSections(t *testing.T) {
	// Given
	t.Chdir(t.TempDir())
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "nav-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	session, csrf := webLogin(t, router, "nav-owner", "password123")

	// When
	page := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID, nil, session, csrf)

	// Then
	if page.Code != http.StatusOK {
		t.Fatalf("vault page status = %d, want %d", page.Code, http.StatusOK)
	}
	body := page.Body.String()
	if !strings.Contains(body, `<span class="side-nav__current-name">Test Vault</span>`) {
		t.Fatalf("current vault name missing from sidebar: %s", body)
	}
	for _, suffix := range []string{"", "/shares", "/recycle", "/history", "/members", "/settings"} {
		want := `href="/dashboard/vaults/` + vaultID + suffix + `"`
		if !strings.Contains(body, want) {
			t.Errorf("current vault sidebar missing %s", want)
		}
	}
}
