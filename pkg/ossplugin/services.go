package ossplugin

import "context"

// Vault 是宿主返回的 Vault 摘要
type Vault struct {
	ID           string `json:"id"`
	OwnerID      uint   `json:"owner_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	StorageQuota int64  `json:"storage_quota"`
	StorageUsed  int64  `json:"storage_used"`
}

// File 是宿主返回的文件摘要
type File struct {
	ID        uint   `json:"id"`
	UserID    uint   `json:"user_id"`
	VaultID   string `json:"vault_id"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size"`
	Revision  int64  `json:"revision"`
	IsDeleted bool   `json:"is_deleted"`
	Content   string `json:"content,omitempty"`
}

// Share 是宿主返回的分享摘要
type Share struct {
	ShareID    string `json:"share_id"`
	UserID     uint   `json:"user_id"`
	VaultID    string `json:"vault_id"`
	TargetPath string `json:"target_path"`
	IsFolder   bool   `json:"is_folder"`
	AllowCopy  bool   `json:"allow_copy"`
	Views      int    `json:"views"`
}

// Device 是宿主返回的设备摘要
type Device struct {
	ID       uint   `json:"id"`
	UserID   uint   `json:"user_id"`
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

// Collaboration 是宿主返回的协作关系摘要
type Collaboration struct {
	ID             uint   `json:"id"`
	VaultID        string `json:"vault_id"`
	FileID         uint   `json:"file_id"`
	OwnerID        uint   `json:"owner_id"`
	CollaboratorID uint   `json:"collaborator_id"`
	Status         string `json:"status"`
}

// UserService 提供用户模型查询
type UserService struct{ client *Client }

// VaultService 提供 Vault 模型操作
type VaultService struct{ client *Client }

// FileService 提供文件模型查询与文件读取
type FileService struct{ client *Client }

// ShareService 提供分享模型操作
type ShareService struct{ client *Client }

// DeviceService 提供设备模型查询
type DeviceService struct{ client *Client }

// CollaborationService 提供协作关系查询
type CollaborationService struct{ client *Client }

func (s ServiceClient) Users() UserService     { return UserService{client: s.client} }
func (s ServiceClient) Vaults() VaultService   { return VaultService{client: s.client} }
func (s ServiceClient) Files() FileService     { return FileService{client: s.client} }
func (s ServiceClient) Shares() ShareService   { return ShareService{client: s.client} }
func (s ServiceClient) Devices() DeviceService { return DeviceService{client: s.client} }
func (s ServiceClient) Collaborations() CollaborationService {
	return CollaborationService{client: s.client}
}

func (s UserService) List(ctx context.Context, limit int) ([]User, error) {
	return modelList[User](ctx, s.client, "users", limit)
}
func (s VaultService) List(ctx context.Context, limit int) ([]Vault, error) {
	return modelList[Vault](ctx, s.client, "vaults", limit)
}
func (s FileService) List(ctx context.Context, limit int) ([]File, error) {
	return modelList[File](ctx, s.client, "files", limit)
}
func (s ShareService) List(ctx context.Context, limit int) ([]Share, error) {
	return modelList[Share](ctx, s.client, "shares", limit)
}
func (s DeviceService) List(ctx context.Context, limit int) ([]Device, error) {
	return modelList[Device](ctx, s.client, "devices", limit)
}
func (s CollaborationService) List(ctx context.Context, limit int) ([]Collaboration, error) {
	return modelList[Collaboration](ctx, s.client, "collaborations", limit)
}

func modelList[T any](ctx context.Context, client *Client, model string, limit int) ([]T, error) {
	var result []T
	err := client.HostCall(ctx, "host.model.list", map[string]any{"model": model, "limit": limit}, &result)
	return result, err
}
