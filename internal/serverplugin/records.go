package serverplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/helantianshen/oss-sync/internal/models"
)

func infoFromRecord(record models.ServerPlugin) (PluginInfo, error) {
	manifest, err := ParseManifest([]byte(record.ManifestJSON))
	if err != nil {
		return PluginInfo{}, err
	}
	if manifest.Runtime == "" {
		manifest.Runtime = record.Runtime
		if manifest.Runtime == "" {
			manifest.Runtime = RuntimeWASM
		}
	}
	payloadHash := record.PayloadHash
	payloadSize := record.PayloadSize
	if payloadHash == "" {
		payloadHash = record.WasmHash
	}
	if payloadSize == 0 && record.WasmSize > 0 {
		payloadSize = record.WasmSize
	}
	return PluginInfo{
		Manifest:     manifest,
		Enabled:      record.Enabled,
		LastError:    record.LastError,
		WasmHash:     record.WasmHash,
		ManifestHash: record.ManifestHash,
		WasmSize:     record.WasmSize,
		PayloadHash:  payloadHash,
		PayloadSize:  payloadSize,
		InstalledAt:  record.InstalledAt,
		UpdatedAt:    record.UpdatedAt,
	}, nil
}

func cleanupInstalledPackage(path string, cause error) error {
	return errors.Join(cause, cleanupError(os.RemoveAll(path)))
}

func cleanupError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cleanup installed plugin: %w", err)
}

func manifestJSON(manifest Manifest) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode plugin manifest: %w", err)
	}
	return string(raw), nil
}
