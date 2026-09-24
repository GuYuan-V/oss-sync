package syncapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/collaboration"
	"github.com/helantianshen/oss-sync/internal/deviceauth"
	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/history"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/settingspolicy"
	"github.com/helantianshen/oss-sync/internal/storagequota"
)

const fallbackMaxFileSizeMB = 100

// Upload 接收 Obsidian 原始字节流，同时兼容 multipart/form-data 客户端
func (h *Handler) Upload(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	did, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader), c.Query("client_id"), c.PostForm("client_id"))
	if !ok {
		return
	}

	rawUpload := strings.HasPrefix(c.GetHeader("Content-Type"), "application/octet-stream")
	path := c.PostForm("path")
	mtimeText := c.PostForm("mtime")
	if rawUpload {
		path = c.Query("path")
		mtimeText = c.Query("mtime")
	}
	path, valid := normalizeRelativePath(path)
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path contains illegal segments"})
		return
	}
	if mtimeText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mtime is required"})
		return
	}
	var mtime int64
	if _, err := fmt.Sscanf(mtimeText, "%d", &mtime); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mtime must be integer milliseconds"})
		return
	}
	effective, err := settingspolicy.EffectiveForUser(h.DB, u.ID, h.maxUploadBytes())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	maxBytes := effective.UploadSizeBytes
	src, declaredSize, err := uploadSource(c, rawUpload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	if declaredSize > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("file size %d exceeds limit %d bytes", declaredSize, maxBytes),
		})
		return
	}

	vaultID, err := defaultVaultID(h.DB, u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := deviceauth.CheckVaultAccess(h.DB, u.ID, string(did), vaultID); err != nil {
		h.writeDeviceAuthError(c, err)
		return
	}
	if !h.recordDeviceActivityWithDID(c, u.ID, vaultID, did) {
		return
	}
	storageKey := filestore.VaultStorageKey(vaultID, path)
	targetPath := filepath.Join(h.Cfg.Storage.DataDir, filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mkdir failed: " + err.Error()})
		return
	}

	dst, err := os.CreateTemp(filepath.Dir(targetPath), ".oss-upload-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open target failed: " + err.Error()})
		return
	}
	tmpPath := dst.Name()
	defer os.Remove(tmpPath)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(dst, hasher), io.LimitReader(src, maxBytes+1))
	if copyErr != nil {
		closeAndRemove(dst, tmpPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "io.Copy failed: " + copyErr.Error()})
		return
	}
	if written > maxBytes {
		closeAndRemove(dst, tmpPath)
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file exceeds configured size limit"})
		return
	}
	if err := dst.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "close tmp failed: " + err.Error()})
		return
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	if _, err = h.commitFileWrite(u.ID, vaultID, path, storageKey, targetPath, tmpPath, hash, written, mtime); err != nil {
		if errors.Is(err, storagequota.ErrExceeded) {
			c.JSON(http.StatusInsufficientStorage, gin.H{"error": "project storage quota exceeded", "code": "project_storage_quota_exceeded"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"path": path, "hash": hash, "mtime": mtime, "server_time": time.Now().UnixMilli(),
	})
}

func uploadSource(c *gin.Context, rawUpload bool) (io.ReadCloser, int64, error) {
	if rawUpload {
		return c.Request.Body, c.Request.ContentLength, nil
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return nil, 0, fmt.Errorf("file is required: %w", err)
	}
	src, err := fileHeader.Open()
	if err != nil {
		return nil, 0, fmt.Errorf("open uploaded file failed: %w", err)
	}
	return src, fileHeader.Size, nil
}

func (h *Handler) upsertFile(
	userID uint,
	vaultID, path, fileType, hash, storageKey string,
	mtime, size int64,
) (models.File, error) {
	effective, err := settingspolicy.EffectiveForUser(h.DB, userID, h.maxUploadBytes())
	if err != nil {
		return models.File{}, err
	}
	var saved models.File
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var existing models.File
		err := tx.Where("user_id = ? AND vault_id = ? AND path = ?", userID, vaultID, path).First(&existing).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := ensureVaultQuota(tx, vaultQuotaChange{
			VaultID: vaultID, NewSize: size, Current: existing, Exists: exists,
			PolicyLimit: effective.VaultStorageBytes,
		}); err != nil {
			return err
		}
		revision, revisionErr := nextVaultRevision(tx, vaultID)
		if revisionErr != nil {
			return revisionErr
		}
		if !exists {
			saved = models.File{
				UserID: userID, VaultID: vaultID, Path: path, Type: fileType,
				Hash: hash, MTime: mtime, Size: size, Revision: revision, StorageKey: storageKey,
			}
			return tx.Create(&saved).Error
		}
		if err := tx.Model(&models.File{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"type": fileType, "hash": hash, "m_time": mtime, "size": size,
			"revision": revision, "is_deleted": false, "deleted_at": nil, "storage_key": storageKey,
		}).Error; err != nil {
			return err
		}
		existing.Type = fileType
		existing.Hash = hash
		existing.MTime = mtime
		existing.Size = size
		existing.Revision = revision
		existing.IsDeleted = false
		existing.StorageKey = storageKey
		saved = existing
		return nil
	})
	return saved, err
}

// commitFileWrite 在持有 vault 与路径锁的前提下把已写入临时文件的内容原子落盘并登记同步修订
// 复用同一进程内的锁、修订通知与协作事件，保证宿主侧写入与客户端上传共享同一套并发与通知机制
func (h *Handler) commitFileWrite(userID uint, vaultID, path, storageKey, targetPath, tmpPath, hash string, size, mtime int64) (models.File, error) {
	vaultLock := h.vaultLock(vaultID)
	vaultLock.Lock()
	defer vaultLock.Unlock()
	pathLock := h.pathLock(vaultID + ":" + path)
	pathLock.Lock()
	defer pathLock.Unlock()
	backupPath := tmpPath + ".backup"
	var saved models.File
	err := storagequota.WithinLimit(h.Cfg.Storage.DataDir, h.Cfg.Storage.MaxTotalSizeBytes(), 0, func() error {
		if _, statErr := os.Stat(targetPath); statErr == nil {
			if err := os.Rename(targetPath, backupPath); err != nil {
				return fmt.Errorf("backup target failed: %w", err)
			}
		}
		if err := os.Rename(tmpPath, targetPath); err != nil {
			if _, backupErr := os.Stat(backupPath); backupErr == nil {
				_ = os.Rename(backupPath, targetPath)
			}
			return fmt.Errorf("rename failed: %w", err)
		}
		var saveErr error
		saved, saveErr = h.upsertFile(userID, vaultID, path, classifyFile(path), hash, storageKey, mtime, size)
		if saveErr != nil {
			_ = os.Remove(targetPath)
			if _, backupErr := os.Stat(backupPath); backupErr == nil {
				_ = os.Rename(backupPath, targetPath)
			}
		}
		return saveErr
	})
	if err != nil {
		return models.File{}, err
	}
	_ = os.Remove(backupPath)
	h.notifyRevision(saved.VaultID)
	if userIDs := h.collaborationEventUsers(saved.VaultID, saved.ID); len(userIDs) > 0 {
		h.publishCollaborationEvent(collaboration.Event{
			VaultID: saved.VaultID, FileID: saved.ID, FilePath: saved.Path,
			Kind: "changed", At: time.Now().UnixMilli(),
		}, userIDs)
	}
	return saved, nil
}

// WriteFileContent 供受信宿主（如服务端插件）以内存内容写入 vault 文件
// 语义与 V2 同步写入一致：内容未变化时既不推进修订也不记历史；真实变更时快照旧内容、推进修订并唤醒同步与协作
// userID 通常取 vault 拥有者，mtime 非正数时取当前时间
func (h *Handler) WriteFileContent(userID uint, vaultID, path string, content []byte, mtime int64) (models.File, error) {
	normalized, valid := normalizeRelativePath(path)
	if !valid {
		return models.File{}, fmt.Errorf("path contains illegal segments")
	}
	if int64(len(content)) > h.maxUploadBytes() {
		return models.File{}, fmt.Errorf("file size %d exceeds limit %d bytes", len(content), h.maxUploadBytes())
	}
	if mtime <= 0 {
		mtime = time.Now().UnixMilli()
	}
	effective, err := settingspolicy.EffectiveForUser(h.DB, userID, h.maxUploadBytes())
	if err != nil {
		return models.File{}, err
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	size := int64(len(content))
	storageKey := filestore.VaultStorageKey(vaultID, normalized)
	targetPath := filepath.Join(h.Cfg.Storage.DataDir, filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return models.File{}, fmt.Errorf("mkdir failed: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), ".oss-plugin-write-*")
	if err != nil {
		return models.File{}, fmt.Errorf("open target failed: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return models.File{}, fmt.Errorf("write temp failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return models.File{}, fmt.Errorf("close temp failed: %w", err)
	}

	var owner models.User
	_ = h.DB.Select("username").First(&owner, userID).Error
	actor := history.Actor{Username: owner.Username, DeviceName: "网页控制台"}

	pathLock := h.pathLock(vaultID + ":" + normalized)
	pathLock.Lock()
	defer pathLock.Unlock()

	var result models.File
	var backupPath string
	moved := false
	changed := false
	err = storagequota.WithinLimit(h.Cfg.Storage.DataDir, h.Cfg.Storage.MaxTotalSizeBytes(), historySnapshotReserve(targetPath), func() error {
		return h.DB.Transaction(func(tx *gorm.DB) error {
			current, exists, lookupErr := lockedFile(tx, userID, vaultID, normalized)
			if lookupErr != nil {
				return lookupErr
			}
			// 内容未变化：返回既有文件，不推进修订也不记历史
			if exists && !current.IsDeleted && current.Hash == hash {
				result = current
				return nil
			}
			if err := ensureVaultQuota(tx, vaultQuotaChange{
				VaultID: vaultID, NewSize: size, Current: current, Exists: exists,
				PolicyLimit: effective.VaultStorageBytes,
			}); err != nil {
				return err
			}
			if _, statErr := os.Stat(targetPath); statErr == nil {
				backupPath = targetPath + ".backup-" + uuid.NewString()
				if err := os.Rename(targetPath, backupPath); err != nil {
					return err
				}
			}
			if err := os.Rename(tmpPath, targetPath); err != nil {
				if backupPath != "" {
					_ = os.Rename(backupPath, targetPath)
				}
				return err
			}
			moved = true
			revision, err := nextVaultRevision(tx, vaultID)
			if err != nil {
				return err
			}
			action := history.ActionCreate
			if !exists {
				current = models.File{UserID: userID, VaultID: vaultID, Path: normalized}
			} else if !current.IsDeleted {
				action = history.ActionModify
			}
			current.Type = classifyFile(normalized)
			current.Hash = hash
			current.Size = size
			current.MTime = mtime
			current.Revision = revision
			current.IsDeleted = false
			current.DeletedAt = sql.NullTime{}
			current.StorageKey = storageKey
			current.UpdatedAt = time.Now()
			if exists {
				if err := tx.Save(&current).Error; err != nil {
					return err
				}
			} else if err := tx.Create(&current).Error; err != nil {
				return err
			}
			result = current
			changed = true
			return history.Record(tx, h.Cfg.Storage.DataDir, vaultID, actor, action, normalized, "", backupPath, revision)
		})
	})
	if err != nil {
		if moved {
			_ = os.Remove(targetPath)
			if backupPath != "" {
				_ = os.Rename(backupPath, targetPath)
			}
		}
		return models.File{}, err
	}
	if backupPath != "" {
		_ = os.Remove(backupPath)
	}
	if changed {
		h.notifyRevision(vaultID)
		if userIDs := h.collaborationEventUsers(vaultID, result.ID); len(userIDs) > 0 {
			h.publishCollaborationEvent(collaboration.Event{
				VaultID: vaultID, FileID: result.ID, FilePath: result.Path,
				Kind: "changed", At: time.Now().UnixMilli(),
			}, userIDs)
		}
	}
	return result, nil
}

func closeAndRemove(file *os.File, path string) {
	_ = file.Close()
	_ = os.Remove(path)
}
