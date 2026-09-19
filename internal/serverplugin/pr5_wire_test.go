package serverplugin

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

func assertSDKValue[T any](t *testing.T, actual any, expected T) {
	t.Helper()
	raw, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("wire contract mismatch: json=%s got=%+v want=%+v", raw, decoded, expected)
	}
}

func TestHostModelListsDecodeWithSDKTypes(t *testing.T) {
	m := reviewManager(t)
	if err := m.db.AutoMigrate(&models.File{}, &models.Share{}, &models.ClientDevice{}, &models.Collaboration{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{ID: 11, Username: "wire-user", Role: "user", PasswordHash: "not-for-the-wire"}
	mustReview(t, m.db.Create(&user))
	vault := models.Vault{ID: "wire-vault", OwnerID: 11, Name: "Wire", Description: "Description", StorageQuota: 456, StorageUsed: 123}
	mustReview(t, m.db.Create(&vault))
	file := models.File{ID: 23, UserID: 11, VaultID: vault.ID, Path: "note.md", Type: "markdown", Hash: "abc", Size: 12, Revision: 37, IsDeleted: true}
	mustReview(t, m.db.Create(&file))
	device := models.ClientDevice{ID: 34, UserID: 11, ClientID: "wire-client", Name: "Device", Status: "approved"}
	mustReview(t, m.db.Create(&device))
	collab := models.Collaboration{ID: 45, VaultID: vault.ID, FileID: 23, OwnerID: 11, CollaboratorID: 99, Status: "accepted"}
	mustReview(t, m.db.Create(&collab))
	list := func(name string) any {
		t.Helper()
		raw, err := json.Marshal(name)
		if err != nil {
			t.Fatal(err)
		}
		value, err := m.hostCall(t.Context(), "test-plugin", "host.model.list", map[string]json.RawMessage{"model": raw})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertSDKValue(t, list("users"), []ossplugin.User{{ID: 11, Username: "wire-user", Role: "user"}})
	assertSDKValue(t, list("vaults"), []ossplugin.Vault{{ID: vault.ID, OwnerID: 11, Name: "Wire", Description: "Description", StorageQuota: 456, StorageUsed: 123}})
	assertSDKValue(t, list("files"), []ossplugin.File{{ID: 23, UserID: 11, VaultID: vault.ID, Path: "note.md", Type: "markdown", Hash: "abc", Size: 12, Revision: 37, IsDeleted: true}})
	assertSDKValue(t, list("devices"), []ossplugin.Device{{ID: 34, UserID: 11, ClientID: "wire-client", Name: "Device", Status: "approved"}})
	assertSDKValue(t, list("collaborations"), []ossplugin.Collaboration{{ID: 45, VaultID: vault.ID, FileID: 23, OwnerID: 11, CollaboratorID: 99, Status: "accepted"}})
	assertSDKValue(t, list("shares"), []ossplugin.Share{})
}

func TestTypedHostResponsesDecodeWithSDKTypes(t *testing.T) {
	m := reviewManager(t)
	if err := m.db.AutoMigrate(&models.Share{}, &models.File{}, &models.VaultSetting{}, &models.VaultSyncState{}); err != nil {
		t.Fatal(err)
	}
	call := func(method string, values map[string]any) any {
		t.Helper()
		params := map[string]json.RawMessage{}
		for key, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			params[key] = raw
		}
		value, err := m.hostCall(t.Context(), "test-plugin", method, params)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	created := call("host.vault.create", map[string]any{"name": "Wire Vault", "description": "Original"}).(ossplugin.Vault)
	assertSDKValue(t, call("host.vault.get", map[string]any{"vault_id": created.ID}), created)
	created.Description = "Updated"
	assertSDKValue(t, call("host.vault.update", map[string]any{"vault_id": created.ID, "input": map[string]string{"description": "Updated"}}), created)
	mustReview(t, m.db.Model(&models.Vault{}).Where("id = ?", created.ID).Update("owner_id", 42))
	mustReview(t, m.db.Create(&models.File{UserID: 42, VaultID: created.ID, Path: "note.md", Type: "markdown"}))
	share := call("host.share.create", map[string]any{"vault_id": created.ID, "target_path": "note.md", "allow_copy": true}).(ossplugin.Share)
	if share.ShareID == "" || share.UserID != 42 {
		t.Fatalf("share identity lost: %+v", share)
	}
	expected := ossplugin.Share{ShareID: share.ShareID, UserID: 42, VaultID: created.ID, TargetPath: "note.md", AllowCopy: true}
	assertSDKValue(t, share, expected)
	expected.AllowCopy = false
	assertSDKValue(t, call("host.share.update", map[string]any{"share_id": share.ShareID, "allow_copy": false}), expected)
}

func TestPluginShareCreationRejectsInvalidTargets(t *testing.T) {
	m := reviewManager(t)
	if err := m.db.AutoMigrate(&models.Share{}, &models.File{}); err != nil {
		t.Fatal(err)
	}
	mustReview(t, m.db.Create(&models.Vault{ID: "valid-vault", OwnerID: 42, Name: "Valid"}))
	for _, input := range []struct{ vault, path string }{{"missing-vault", "note.md"}, {"valid-vault", "missing.md"}, {"valid-vault", "../outside.md"}, {"valid-vault", ""}} {
		vault, _ := json.Marshal(input.vault)
		path, _ := json.Marshal(input.path)
		if _, err := m.hostShareCreate(t.Context(), map[string]json.RawMessage{"vault_id": vault, "target_path": path}); err == nil {
			t.Fatalf("invalid target accepted: %+v", input)
		}
	}
	var count int64
	mustReview(t, m.db.Model(&models.Share{}).Count(&count))
	if count != 0 {
		t.Fatal("invalid target created share records")
	}
}
