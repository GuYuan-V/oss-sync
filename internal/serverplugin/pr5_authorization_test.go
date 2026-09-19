package serverplugin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/deviceauth"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestEditorCommandAuthorizationAndTarget(t *testing.T) {
	for _, scenario := range []string{"owner", "member", "foreign-vault", "archived-vault", "missing-device", "pending-device", "revoked-device", "ungranted-device", "mismatched-device", "internal-hook", "unknown-plugin", "unknown-command"} {
		t.Run(scenario, func(t *testing.T) {
			m := reviewManager(t)
			if err := m.db.AutoMigrate(&models.ClientDevice{}, &models.DeviceVaultAccess{}); err != nil {
				t.Fatal(err)
			}
			user, err := auth.CreateAccount(m.db, "command-owner", "pass12345", "user")
			if err != nil {
				t.Fatal(err)
			}
			vault := models.Vault{ID: "command-vault", OwnerID: user.ID, Name: "Command Vault"}
			if scenario == "member" || scenario == "foreign-vault" {
				vault.OwnerID++
			}
			mustReview(t, m.db.Create(&vault))
			if scenario == "member" {
				mustReview(t, m.db.Create(&models.VaultMember{VaultID: vault.ID, UserID: user.ID, Role: "participant"}))
			}
			if scenario == "archived-vault" {
				mustReview(t, m.db.Delete(&vault))
			}
			status := deviceauth.DeviceStatusApproved
			if scenario == "pending-device" {
				status = deviceauth.DeviceStatusPending
			}
			if scenario == "revoked-device" {
				status = deviceauth.DeviceStatusRevoked
			}
			mustReview(t, m.db.Create(&models.ClientDevice{UserID: user.ID, ClientID: "client-one", Status: status}))
			if scenario != "ungranted-device" {
				if err := deviceauth.GrantAccess(m.db, user.ID, "client-one", vault.ID, user.ID); err != nil {
					t.Fatal(err)
				}
			}
			inst, other := &reviewInstance{}, &reviewInstance{}
			m.modules["selected-plugin"], m.modules["other-plugin"] = inst, other
			m.registrations["selected-plugin"] = ExtensionRegistration{Hooks: []RegisteredHook{
				{Name: "editor.command", ID: "first", Callback: "first-command", Kind: "filter"},
				{Name: "editor.command", ID: "format", Callback: "selected-command", Kind: "filter"},
			}}
			m.registrations["other-plugin"] = ExtensionRegistration{Hooks: []RegisteredHook{{Name: "editor.command", ID: "format", Kind: "filter"}}}
			cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "command-test-secret", JWTTTLHours: 1}}
			token, _, err := auth.IssueDeviceToken(cfg, *user, "client-one")
			if scenario == "missing-device" {
				token, _, err = auth.IssueToken(cfg, *user)
			}
			if err != nil {
				t.Fatal(err)
			}
			pluginID, commandID, hook := "selected-plugin", "format", "editor.command"
			if scenario == "unknown-plugin" {
				pluginID = "missing"
			}
			if scenario == "unknown-command" {
				commandID = "missing"
			}
			if scenario == "internal-hook" {
				hook = "blog.content"
			}
			body, err := json.Marshal(map[string]any{"vault_id": vault.ID, "content": "original", "metadata": map[string]any{
				"plugin_id": pluginID, "command_id": commandID, "client_id": "spoofed", "user_id": 999,
			}})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			m.RegisterRoutes(router, cfg)
			req := httptest.NewRequest("POST", "/api/plugin-hooks/"+hook, strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			if scenario == "mismatched-device" {
				req.Header.Set(deviceauth.ClientIDHeader, "another-client")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			success := scenario == "owner" || scenario == "member"
			if !success {
				if response.Code < 400 || inst.calls != 0 || other.calls != 0 {
					t.Fatalf("unauthorized command reached plugin: status=%d selected=%d other=%d", response.Code, inst.calls, other.calls)
				}
				return
			}
			if response.Code != 200 || inst.calls != 1 || other.calls != 0 {
				t.Fatalf("wrong dispatch: status=%d selected=%d other=%d", response.Code, inst.calls, other.calls)
			}
			if inst.last.Callback != "selected-command" || inst.last.User == nil || inst.last.User.ID != user.ID {
				t.Fatalf("wrong callback or authenticated identity: %+v", inst.last)
			}
			metadata := inst.last.Payload["metadata"].(map[string]any)
			if metadata["client_id"] != "client-one" || metadata["user_id"] != nil {
				t.Fatalf("client identity was not sanitized: %#v", metadata)
			}
		})
	}
}
