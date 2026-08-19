package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/history"
	"github.com/oss/oss-server/internal/models"
)

func TestWebConsoleHistory_whenFiltersCombined_returnsOnlyMatchingRows(t *testing.T) {
	t.Chdir(t.TempDir())

	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "history-filter-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	session, csrf := webLogin(t, router, "history-filter-owner", "password123")
	localTime := func(hour int) time.Time {
		return time.Date(2026, time.August, 10, hour, 0, 0, 0, time.Local)
	}
	rows := []models.FileHistory{
		{VaultID: vaultID, FilePath: "match.md", Action: history.ActionModify, Version: 1, Username: "alice", DeviceName: "Laptop", ClientID: "client-a", CreatedAt: localTime(10)},
		{VaultID: vaultID, FilePath: "wrong-action.md", Action: history.ActionCreate, Version: 1, Username: "alice", DeviceName: "Laptop", ClientID: "client-a", CreatedAt: localTime(10)},
		{VaultID: vaultID, FilePath: "wrong-user.md", Action: history.ActionModify, Version: 1, Username: "bob", DeviceName: "Laptop", ClientID: "client-a", CreatedAt: localTime(10)},
		{VaultID: vaultID, FilePath: "wrong-device.md", Action: history.ActionModify, Version: 1, Username: "alice", DeviceName: "Phone", ClientID: "client-b", CreatedAt: localTime(10)},
		{VaultID: vaultID, FilePath: "too-early.md", Action: history.ActionModify, Version: 1, Username: "alice", DeviceName: "Laptop", ClientID: "client-a", CreatedAt: localTime(8)},
		{VaultID: vaultID, FilePath: "too-late.md", Action: history.ActionModify, Version: 1, Username: "alice", DeviceName: "Laptop", ClientID: "client-a", CreatedAt: localTime(12)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	filters := url.Values{
		"action":   {history.ActionModify},
		"username": {"alice"},
		"device":   {"Laptop"},
		"from":     {"2026-08-10T09:00"},
		"to":       {"2026-08-10T11:00"},
	}

	// When
	page := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/history?"+filters.Encode(), nil, session, csrf)

	// Then
	if page.Code != http.StatusOK {
		t.Fatalf("history page: status=%d body=%s", page.Code, page.Body)
	}
	body := page.Body.String()
	if !strings.Contains(body, "match.md") {
		t.Fatalf("matching history row missing: %s", body)
	}
	for _, unwanted := range []string{"wrong-action.md", "wrong-user.md", "wrong-device.md", "too-early.md", "too-late.md"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("history filter retained %s", unwanted)
		}
	}
	for _, selected := range []string{
		`name="action"`, `value="modify" selected`, `name="username" value="alice"`,
		`name="device" value="Laptop"`, `name="from" value="2026-08-10T09:00"`,
		`name="to" value="2026-08-10T11:00"`,
	} {
		if !strings.Contains(body, selected) {
			t.Errorf("history filter form missing %q", selected)
		}
	}
}
