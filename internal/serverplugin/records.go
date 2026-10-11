package serverplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/helantianshen/oss-sync/internal/models"
)

// infoFromRecord 把数据库记录还原为管理面视图
// manifest 缺省 runtime 时回落到记录值，两者皆空视为 WASM
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

// cleanupInstalledPackage 删除已安装目录并合并失败原因
func cleanupInstalledPackage(path string, cause error) error {
	return errors.Join(cause, cleanupError(os.RemoveAll(path)))
}

// cleanupError 包装安装目录清理失败，nil 原样返回
func cleanupError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cleanup installed plugin: %w", err)
}

// manifestJSON 序列化 manifest 供数据库存储
func manifestJSON(manifest Manifest) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode plugin manifest: %w", err)
	}
	return string(raw), nil
}
