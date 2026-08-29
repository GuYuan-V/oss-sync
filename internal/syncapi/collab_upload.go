// 协作上传
package syncapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/history"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/settingspolicy"
	"github.com/oss/oss-server/internal/storagequota"
)

var (
	errCollaborationForbidden = errors.New("collaboration is not accepted")
	errCollaborationDeleted   = errors.New("collaboration file is deleted")
)

type collabUploadRequest struct {
	Content      string `json:"content"`
	BaseRevision *int64 `json:"base_revision"`
	OperationID  string `json:"operation_id"`
}

// CollabUpload writes an accepted collaboration using revision compare-and-swap.
func (h *Handler) CollabUpload(c *gin.Context) {
	user, clientID, ok := h.requireCollaborationDevice(c)
	if !ok {
		return
	}
	fileID, err := strconv.ParseUint(c.Param("file_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id"})
		return
	}
	vault, file, ok := h.collaborationUploadTarget(c, user.ID, uint(fileID))
	if !ok {
		return
	}
	var request collabUploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rawOperationID := request.OperationID
	request.OperationID = cleanIdentifier(request.OperationID)
	if request.BaseRevision == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_revision is required", "code": "missing_base_revision"})
		return
	}
	if *request.BaseRevision < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_revision is invalid", "code": "invalid_base_revision"})
		return
	}
	if strings.TrimSpace(rawOperationID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operation_id is required", "code": "missing_operation_id"})
		return
	}
	if request.OperationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operation_id is invalid", "code": "invalid_operation_id"})
		return
	}
	content := []byte(request.Content)
	effective, err := settingspolicy.EffectiveForVault(h.DB, vault.ID, h.maxUploadBytes())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if int64(len(content)) > effective.UploadSizeBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file exceeds configured size limit"})
		return
	}

	result, conflict, changed, err := h.writeCollaboration(c, collaborationWriteInput{
		User: user, Vault: vault, File: file, Content: content,
		BaseRevision: *request.BaseRevision, ClientID: clientID,
		OperationID: request.OperationID, PolicyLimit: effective.VaultStorageBytes,
	})
	if errors.Is(err, errRevisionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "revision conflict", "current": v2Meta(conflict)})
		return
	}
	if errors.Is(err, errCollaborationForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "collaboration is not accepted"})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if errors.Is(err, errCollaborationDeleted) {
		c.JSON(http.StatusGone, gin.H{"error": "file has been deleted"})
		return
	}
	if errors.Is(err, errVaultStorageQuotaExceeded) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "vault storage quota exceeded"})
		return
	}
	if errors.Is(err, storagequota.ErrExceeded) {
		c.JSON(http.StatusInsufficientStorage, gin.H{"error": "project storage quota exceeded", "code": "project_storage_quota_exceeded"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if changed {
		h.notifyRevision(vault.ID)
		h.publishCollaborationEvent(collaboration.Event{
			VaultID: vault.ID, FileID: result.ID, FilePath: result.Path,
			Kind: "changed", At: time.Now().UnixMilli(),
		}, h.collaborationEventUsers(vault.ID, result.ID))
	}
	meta := v2Meta(result)
	meta.ServerTime = time.Now().UnixMilli()
	c.JSON(http.StatusOK, meta)
}

type collaborationWriteInput struct {
	User         *models.User
	Vault        models.Vault
	File         models.File
	Content      []byte
	BaseRevision int64
	ClientID     string
	OperationID  string
	PolicyLimit  int64
}

func (h *Handler) writeCollaboration(c *gin.Context, input collaborationWriteInput) (models.File, models.File, bool, error) {
	targetKey := filestore.VaultStorageKey(input.Vault.ID, input.File.Path)
	targetPath := filepath.Join(h.Cfg.Storage.DataDir, filepath.FromSlash(targetKey))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return models.File{}, models.File{}, false, fmt.Errorf("create collaboration directory: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(targetPath), ".oss-collab-*")
	if err != nil {
		return models.File{}, models.File{}, false, fmt.Errorf("create collaboration temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(input.Content); err != nil {
		_ = tmpFile.Close()
		return models.File{}, models.File{}, false, fmt.Errorf("write collaboration temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return models.File{}, models.File{}, false, fmt.Errorf("close collaboration temp file: %w", err)
	}

	vaultLock := h.vaultLock(input.Vault.ID)
	vaultLock.Lock()
	defer vaultLock.Unlock()
	pathLock := h.pathLock(input.Vault.ID + ":" + input.File.Path)
	pathLock.Lock()
	defer pathLock.Unlock()

	var result, conflict models.File
	backupPath := ""
	moved := false
	changed := false
	sum := sha256.Sum256(input.Content)
	contentHash := hex.EncodeToString(sum[:])
	err = storagequota.WithinLimit(h.Cfg.Storage.DataDir, h.Cfg.Storage.MaxTotalSizeBytes(), historySnapshotReserve(targetPath), func() error {
		return h.DB.Transaction(func(tx *gorm.DB) error {
			var relation models.Collaboration
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
				"vault_id = ? AND file_id = ? AND collaborator_id = ? AND status = ?",
				input.Vault.ID, input.File.ID, input.User.ID, collaboration.StatusAccepted,
			).First(&relation).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errCollaborationForbidden
				}
				return fmt.Errorf("lock collaboration: %w", err)
			}
			current, exists, err := lockedFile(tx, input.Vault.OwnerID, input.Vault.ID, input.File.Path)
			if err != nil {
				return fmt.Errorf("lock collaboration file: %w", err)
			}
			if !exists || current.ID != input.File.ID {
				return gorm.ErrRecordNotFound
			}
			if current.IsDeleted {
				return errCollaborationDeleted
			}
			if current.LastWriterClientID == input.ClientID && current.LastOperationID == input.OperationID {
				result = current
				return nil
			}
			if current.Revision != input.BaseRevision {
				conflict = current
				return errRevisionConflict
			}
			if current.Hash == contentHash {
				result = current
				return nil
			}
			if err := ensureVaultQuota(tx, vaultQuotaChange{
				VaultID: input.Vault.ID, NewSize: int64(len(input.Content)), Current: current,
				Exists: true, PolicyLimit: input.PolicyLimit,
			}); err != nil {
				return err
			}
			backupPath = targetPath + ".backup-" + uuid.NewString()
			if err := os.Rename(targetPath, backupPath); err != nil {
				return fmt.Errorf("backup collaboration file: %w", err)
			}
			if err := os.Rename(tmpPath, targetPath); err != nil {
				restoreErr := os.Rename(backupPath, targetPath)
				backupPath = ""
				return errors.Join(fmt.Errorf("install collaboration file: %w", err), restoreErr)
			}
			moved = true
			revision, err := nextVaultRevision(tx, input.Vault.ID)
			if err != nil {
				return err
			}
			now := time.Now()
			current.Hash = contentHash
			current.Size = int64(len(input.Content))
			current.MTime = now.UnixMilli()
			current.Revision = revision
			current.IsDeleted = false
			current.DeletedAt = sql.NullTime{}
			current.StorageKey = targetKey
			current.LastWriterClientID = input.ClientID
			current.LastOperationID = input.OperationID
			current.UpdatedAt = now
			if err := tx.Save(&current).Error; err != nil {
				return fmt.Errorf("save collaboration file: %w", err)
			}
			if err := history.Record(tx, h.Cfg.Storage.DataDir, input.Vault.ID, h.historyActor(c, input.User),
				history.ActionModify, current.Path, "", backupPath, revision); err != nil {
				return fmt.Errorf("record collaboration history: %w", err)
			}
			result = current
			changed = true
			return nil
		})
	})
	if err != nil && moved {
		_ = os.Remove(targetPath)
		if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore collaboration file: %w", restoreErr))
		}
	}
	if err == nil && backupPath != "" {
		_ = os.Remove(backupPath)
	}
	return result, conflict, changed, err
}

func (h *Handler) collaborationUploadTarget(c *gin.Context, userID, fileID uint) (models.Vault, models.File, bool) {
	var vault models.Vault
	if err := h.DB.Where("id = ?", c.Param("vault_id")).First(&vault).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "vault not found"})
		return models.Vault{}, models.File{}, false
	}
	var file models.File
	if err := h.DB.Where("id = ? AND vault_id = ?", fileID, vault.ID).First(&file).Error; err != nil {
		var lookupErr error
		file, lookupErr = h.acceptedCollaborationFile(userID, fileID)
		if lookupErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return models.Vault{}, models.File{}, false
		}
		vault = models.Vault{}
		if err := h.DB.Where("id = ?", file.VaultID).First(&vault).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "vault not found"})
			return models.Vault{}, models.File{}, false
		}
	}
	var relation models.Collaboration
	if err := h.DB.Where("vault_id = ? AND file_id = ? AND collaborator_id = ? AND status = ?",
		vault.ID, fileID, userID, collaboration.StatusAccepted).First(&relation).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "collaboration is not accepted"})
		return models.Vault{}, models.File{}, false
	}
	return vault, file, true
}
