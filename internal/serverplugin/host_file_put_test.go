package serverplugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/helantianshen/oss-sync/internal/models"
)

type stubFileWriter struct {
	userID  uint
	vaultID string
	path    string
	content []byte
	mtime   int64
	result  models.File
	err     error
}

func (s *stubFileWriter) WriteFileContent(userID uint, vaultID, path string, content []byte, mtime int64) (models.File, error) {
	s.userID, s.vaultID, s.path, s.content, s.mtime = userID, vaultID, path, content, mtime
	if s.err != nil {
		return models.File{}, s.err
	}
	return s.result, nil
}

func TestHostFilePutResolvesOwnerAndReturnsMetadata(t *testing.T) {
	m := reviewManager(t)
	mustReview(t, m.db.Create(&models.Vault{ID: "v1", OwnerID: 42, Name: "V"}))
	writer := &stubFileWriter{result: models.File{ID: 9, VaultID: "v1", Path: "Notes/a.md", Hash: "abc", Size: 4, Revision: 5}}
	m.SetFileWriter(writer)

	res, err := m.hostCall(context.Background(), "plugin", "host.file.put", map[string]json.RawMessage{
		"vault_id": json.RawMessage(`"v1"`),
		"path":     json.RawMessage(`"Notes/a.md"`),
		"content":  json.RawMessage(`"# Hi"`),
	})
	if err != nil {
		t.Fatalf("host.file.put: %v", err)
	}
	if writer.userID != 42 || writer.vaultID != "v1" || writer.path != "Notes/a.md" || string(writer.content) != "# Hi" {
		t.Fatalf("writer received wrong args: %+v", writer)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if out["revision"].(int64) != 5 || out["hash"].(string) != "abc" || out["id"].(uint) != 9 {
		t.Fatalf("wire metadata mismatch: %+v", out)
	}
}

func TestHostFilePutWithoutWriterReturnsError(t *testing.T) {
	m := reviewManager(t)
	if _, err := m.hostCall(context.Background(), "plugin", "host.file.put", map[string]json.RawMessage{
		"vault_id": json.RawMessage(`"v1"`),
		"path":     json.RawMessage(`"a.md"`),
		"content":  json.RawMessage(`"x"`),
	}); err == nil {
		t.Fatal("expected error when file writer is not configured")
	}
}
