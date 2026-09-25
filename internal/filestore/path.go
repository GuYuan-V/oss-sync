// Package filestore 定义 Vault 内容的安全存储键与磁盘路径
package filestore

import (
	"path/filepath"
	"strconv"

	"github.com/helantianshen/oss-sync/internal/models"
)

// VaultStorageKey 返回 Vault 文件的标准存储键
func VaultStorageKey(vaultID, relativePath string) string {
	return filepath.ToSlash(filepath.Join("vaults", vaultID, "files", filepath.FromSlash(relativePath)))
}

// DiskPath 解析标准 Vault 路径或用户目录存储路径
func DiskPath(dataDir string, file models.File) string {
	if file.StorageKey != "" {
		return filepath.Join(dataDir, filepath.FromSlash(file.StorageKey))
	}
	return filepath.Join(
		dataDir,
		strconv.FormatUint(uint64(file.UserID), 10),
		filepath.FromSlash(file.Path),
	)
}
