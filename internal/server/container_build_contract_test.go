package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerBuildContract_whenRepositoryIsPublished_keepsServerInputs(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	for _, path := range []string{
		filepath.Join(root, "cmd", "server", "main.go"),
		filepath.Join(root, "internal", "server", "server.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required container build input %q: %v", path, err)
		}
	}

	dockerfile := readBuildContractFile(t, filepath.Join(root, "Dockerfile"))
	for _, required := range []string{
		"COPY cmd/server ./cmd/server",
		"COPY internal ./internal",
		"github.com/helantianshen/oss-sync/internal/version.Version",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile missing required build contract %q", required)
		}
	}

	for _, ignoreFile := range []string{".gitignore", ".dockerignore"} {
		content := readBuildContractFile(t, filepath.Join(root, ignoreFile))
		for _, line := range strings.Split(content, "\n") {
			if strings.TrimSpace(line) == "server" {
				t.Errorf("%s contains an unanchored server rule that removes source directories", ignoreFile)
			}
		}
	}
}

func readBuildContractFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(content)
}
