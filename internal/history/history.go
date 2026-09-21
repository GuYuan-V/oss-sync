// Package history 在 Vault 数据根目录下保存文件修订与 gzip 快照
// 其中 create 事件仅记录元数据，快照元数据由数据库保存
package history

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
)

// Actor 标识产生历史条目的账号与设备
type Actor struct {
	Username   string
	DeviceName string
	ClientID   string
}

// History 动作取值持久化于 FileHistory.Action
const (
	ActionCreate  = "create"
	ActionModify  = "modify"
	ActionDelete  = "delete"
	ActionRestore = "restore"
	ActionRename  = "rename"
)

// Dir 返回 Vault 历史目录
func Dir(dataDir, vaultID string) string {
	return filepath.Join(dataDir, "vaults", vaultID, "history")
}

// ContentKey 返回由哈希派生的稳定快照键
func ContentKey(vaultID, hash string) string {
	return filepath.ToSlash(filepath.Join("vaults", vaultID, "history", hash+".gz"))
}

// DiskPath 将快照键解析为数据目录下的磁盘路径
func DiskPath(dataDir, contentKey string) string {
	return filepath.Join(dataDir, filepath.FromSlash(contentKey))
}

// StoreSnapshot 压缩正文并返回快照键、哈希与字节数
func StoreSnapshot(dataDir, vaultID, contentPath string) (string, string, int64, error) {
	src, err := os.Open(contentPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", 0, nil
		}
		return "", "", 0, err
	}
	defer src.Close()

	dir := Dir(dataDir, vaultID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", 0, err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return "", "", 0, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hasher := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(tmp, hasher))
	written, copyErr := io.Copy(gz, src)
	closeGzErr := gz.Close()
	closeTmpErr := tmp.Close()
	if copyErr != nil || closeGzErr != nil || closeTmpErr != nil {
		return "", "", 0, errors.New("failed to write history snapshot")
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	key := ContentKey(vaultID, hash)
	dest := DiskPath(dataDir, key)
	if err := os.Rename(tmpPath, dest); err != nil {
		if !os.IsExist(err) {
			return "", "", 0, err
		}
	}
	return key, hash, written, nil
}

// Record 写入历史元数据与可选快照
// 传入事务内数据库时，修订与快照元数据保持一致提交
func Record(db *gorm.DB, dataDir, vaultID string, actor Actor, action, filePath, prevPath, contentPath string, revision int64) error {
	if filePath == "" {
		return nil
	}
	contentKey, hash, size, err := StoreSnapshot(dataDir, vaultID, contentPath)
	if err != nil {
		return err
	}
	var version int64
	if err := db.Model(&models.FileHistory{}).
		Where("vault_id = ? AND file_path = ?", vaultID, filePath).
		Count(&version).Error; err != nil {
		return err
	}
	row := models.FileHistory{
		VaultID:      vaultID,
		FilePath:     filePath,
		PreviousPath: prevPath,
		Action:       action,
		Revision:     revision,
		Version:      int(version) + 1,
		ContentKey:   contentKey,
		Hash:         hash,
		Size:         size,
		Username:     actor.Username,
		DeviceName:   actor.DeviceName,
		ClientID:     actor.ClientID,
	}
	return db.Create(&row).Error
}

// ReadSnapshot 解压快照，键为空或快照缺失时返回空内容
func ReadSnapshot(dataDir, contentKey string) ([]byte, error) {
	if contentKey == "" {
		return nil, nil
	}
	f, err := os.Open(DiskPath(dataDir, contentKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}

// IsText 判断路径是否可做文本 diff
func IsText(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".md", ".txt", ".json", ".yaml", ".yml", ".css", ".js", ".html", ".csv", ".xml", ".ts", ".go"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// DiffLines 返回从旧内容到新内容的行级 diff
func DiffLines(oldContent, newContent []byte) []string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	return lcsDiff(oldLines, newLines)
}

func splitLines(b []byte) []string {
	s := string(b)
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func lcsDiff(oldLines, newLines []string) []string {
	n, m := len(oldLines), len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				if dp[i+1][j] > dp[i][j+1] {
					dp[i][j] = dp[i+1][j]
				} else {
					dp[i][j] = dp[i][j+1]
				}
			}
		}
	}
	out := make([]string, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			out = append(out, " "+oldLines[i])
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, "-"+oldLines[i])
			i++
		} else {
			out = append(out, "+"+newLines[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+oldLines[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+newLines[j])
	}
	return out
}

// CleanupVault 删除指定 Vault 的全部快照文件
func CleanupVault(dataDir, vaultID string) error {
	return os.RemoveAll(Dir(dataDir, vaultID))
}

// CleanupExpired 删除过期历史记录与无人引用的快照
// 保留天数小于等于 0 时不清理，文件删除失败时保留数据库记录以便重试
func CleanupExpired(db *gorm.DB, dataDir, vaultID string, retentionDays int, now time.Time) error {
	if retentionDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -retentionDays)

	var expired []models.FileHistory
	if err := db.Where("vault_id = ? AND created_at < ?", vaultID, cutoff).
		Find(&expired).Error; err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}

	keysToDelete := make(map[string]struct{})
	for _, h := range expired {
		if h.ContentKey == "" {
			continue
		}
		keysToDelete[h.ContentKey] = struct{}{}
	}
	if len(keysToDelete) > 0 {
		keys := make([]string, 0, len(keysToDelete))
		for k := range keysToDelete {
			keys = append(keys, k)
		}
		// 排除被未过期记录引用的 ContentKey
		var stillReferenced []string
		if err := db.Model(&models.FileHistory{}).
			Where("vault_id = ? AND content_key IN ? AND created_at >= ?", vaultID, keys, cutoff).
			Distinct("content_key").
			Pluck("content_key", &stillReferenced).Error; err != nil {
			return err
		}
		for _, k := range stillReferenced {
			delete(keysToDelete, k)
		}
	}

	// 先删除无人引用的快照文件再删数据库记录，删除失败时保留记录等待下次定时重试
	for k := range keysToDelete {
		diskPath := DiskPath(dataDir, k)
		if err := os.Remove(diskPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	ids := make([]uint, len(expired))
	for i, h := range expired {
		ids[i] = h.ID
	}
	if err := db.Where("id IN ?", ids).Delete(&models.FileHistory{}).Error; err != nil {
		return err
	}
	return nil
}
