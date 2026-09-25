package serverplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/shares"
)

func (m *Manager) hostVaultGet(ctx context.Context, params map[string]json.RawMessage) (models.Vault, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return models.Vault{}, err
	}
	var vault models.Vault
	if err := m.db.WithContext(ctx).Where("id = ?", vaultID).First(&vault).Error; err != nil {
		return models.Vault{}, err
	}
	return vault, nil
}

func (m *Manager) hostVaultCreate(ctx context.Context, params map[string]json.RawMessage) (models.Vault, error) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	inputRaw, err := json.Marshal(params)
	if err != nil {
		return models.Vault{}, err
	}
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		return models.Vault{}, fmt.Errorf("decode vault input: %w", err)
	}
	if strings.TrimSpace(input.Name) == "" {
		return models.Vault{}, errors.New("vault name is required")
	}
	vault := models.Vault{ID: uuid.NewString(), Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description)}
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&vault).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.VaultSetting{VaultID: vault.ID}).Error; err != nil {
			return err
		}
		return tx.Create(&models.VaultSyncState{VaultID: vault.ID}).Error
	}); err != nil {
		return models.Vault{}, fmt.Errorf("create host vault: %w", err)
	}
	return vault, nil
}

func (m *Manager) hostVaultUpdate(ctx context.Context, params map[string]json.RawMessage) (models.Vault, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return models.Vault{}, err
	}
	var input map[string]string
	if err := json.Unmarshal(params["input"], &input); err != nil {
		return models.Vault{}, fmt.Errorf("decode vault update: %w", err)
	}
	updates := map[string]any{}
	if name := strings.TrimSpace(input["name"]); name != "" {
		updates["name"] = name
	}
	if description, ok := input["description"]; ok {
		updates["description"] = strings.TrimSpace(description)
	}
	if len(updates) > 0 {
		if err := m.db.WithContext(ctx).Model(&models.Vault{}).Where("id = ?", vaultID).Updates(updates).Error; err != nil {
			return models.Vault{}, err
		}
	}
	return m.hostVaultGet(ctx, map[string]json.RawMessage{"vault_id": json.RawMessage(fmt.Sprintf("%q", vaultID))})
}

func (m *Manager) hostVaultDelete(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	result := m.db.WithContext(ctx).Where("id = ?", vaultID).Delete(&models.Vault{})
	if result.Error != nil {
		return nil, result.Error
	}
	return map[string]int64{"rows_affected": result.RowsAffected}, nil
}

func (m *Manager) hostFileGet(ctx context.Context, params map[string]json.RawMessage) (map[string]any, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	path, err := requiredStringParam(params, "path")
	if err != nil {
		return nil, err
	}
	var file models.File
	if err := m.db.WithContext(ctx).Where("vault_id = ? AND path = ? AND is_deleted = ?", vaultID, path, false).First(&file).Error; err != nil {
		return nil, err
	}
	result := map[string]any{"id": file.ID, "user_id": file.UserID, "vault_id": file.VaultID, "path": file.Path, "type": file.Type, "hash": file.Hash, "size": file.Size, "revision": file.Revision, "is_deleted": file.IsDeleted}
	if file.Type == "markdown" {
		content, err := os.ReadFile(filestore.DiskPath(filepath.Dir(m.root), file))
		if err == nil {
			result["content"] = string(content)
		}
	}
	return result, nil
}

func (m *Manager) hostFilePut(ctx context.Context, params map[string]json.RawMessage) (map[string]any, error) {
	if m.fileWriter == nil {
		return nil, errors.New("file write is not available")
	}
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	path, err := requiredStringParam(params, "path")
	if err != nil {
		return nil, err
	}
	var content string
	if raw := params["content"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &content); err != nil {
			return nil, fmt.Errorf("decode file content: %w", err)
		}
	}
	var mtime int64
	if raw := params["mtime"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &mtime); err != nil {
			return nil, fmt.Errorf("decode file mtime: %w", err)
		}
	}
	var vault models.Vault
	if err := m.db.WithContext(ctx).Where("id = ?", vaultID).First(&vault).Error; err != nil {
		return nil, err
	}
	saved, err := m.fileWriter.WriteFileContent(vault.OwnerID, vaultID, path, []byte(content), mtime)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":       saved.ID,
		"vault_id": saved.VaultID,
		"path":     saved.Path,
		"hash":     saved.Hash,
		"size":     saved.Size,
		"revision": saved.Revision,
	}, nil
}

func (m *Manager) hostShareCreate(ctx context.Context, params map[string]json.RawMessage) (models.Share, error) {
	var input struct {
		VaultID    string `json:"vault_id"`
		TargetPath string `json:"target_path"`
		IsFolder   bool   `json:"is_folder"`
		AllowCopy  bool   `json:"allow_copy"`
	}
	inputRaw, err := json.Marshal(params)
	if err != nil {
		return models.Share{}, err
	}
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		return models.Share{}, err
	}
	db := m.db.WithContext(ctx)
	var vault models.Vault
	if err := db.First(&vault, "id = ?", input.VaultID).Error; err != nil {
		return models.Share{}, err
	}
	handler := &shares.Handler{DB: db}
	id, err := handler.CreateWeb(vault.OwnerID, vault.ID, input.TargetPath, input.IsFolder, input.AllowCopy)
	if err != nil {
		return models.Share{}, err
	}
	var share models.Share
	err = db.First(&share, "share_id = ?", id).Error
	return share, err
}

func (m *Manager) hostShareUpdate(ctx context.Context, params map[string]json.RawMessage) (models.Share, error) {
	shareID, err := requiredStringParam(params, "share_id")
	if err != nil {
		return models.Share{}, err
	}
	var allowCopy bool
	if err := json.Unmarshal(params["allow_copy"], &allowCopy); err != nil {
		return models.Share{}, err
	}
	var share models.Share
	if err := m.db.WithContext(ctx).Where("share_id = ?", shareID).First(&share).Error; err != nil {
		return models.Share{}, err
	}
	if err := m.db.WithContext(ctx).Model(&share).Update("allow_copy", allowCopy).Error; err != nil {
		return models.Share{}, err
	}
	share.AllowCopy = allowCopy
	return share, nil
}

func (m *Manager) hostShareDelete(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	shareID, err := requiredStringParam(params, "share_id")
	if err != nil {
		return nil, err
	}
	result := m.db.WithContext(ctx).Where("share_id = ?", shareID).Delete(&models.Share{})
	return map[string]int64{"rows_affected": result.RowsAffected}, result.Error
}

func (m *Manager) hostBlogGet(ctx context.Context, params map[string]json.RawMessage) (map[string]string, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	path, err := requiredStringParam(params, "path")
	if err != nil {
		return nil, err
	}
	var file models.File
	if err := m.db.WithContext(ctx).Where("vault_id = ? AND path = ? AND type = ? AND is_deleted = ?", vaultID, path, "markdown", false).First(&file).Error; err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filestore.DiskPath(filepath.Dir(m.root), file))
	if err != nil {
		return nil, err
	}
	return map[string]string{"vault_id": vaultID, "path": path, "content": string(content)}, nil
}

func (m *Manager) hostBlogFilter(ctx context.Context, params map[string]json.RawMessage) (map[string]string, error) {
	var input struct {
		Hook    string            `json:"hook"`
		Content map[string]string `json:"content"`
	}
	inputRaw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		return nil, err
	}
	filtered, err := m.RunHook(ctx, input.Hook, input.Content)
	if err != nil {
		return nil, err
	}
	content, ok := filtered.(map[string]any)
	if !ok {
		return input.Content, nil
	}
	result := map[string]string{}
	for key, value := range content {
		if text, ok := value.(string); ok {
			result[key] = text
		}
	}
	return result, nil
}
