// Package collaboration 提供 Markdown 文件协作：邀请、接受、正文写入与事件订阅。
package collaboration

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
)

// 协作状态。
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRevoked  = "revoked"
)

var (
	ErrNotOwnerOrManager = errors.New("只有 owner 或 manager 可以邀请协作者")
	ErrSelfInvite        = errors.New("不能邀请自己")
	ErrUserNotFound      = errors.New("用户不存在")
	ErrDuplicate         = errors.New("该用户与文件已存在未结束的协作关系")
	ErrNotCollaborator   = errors.New("你不是该文件的协作者")
	ErrInvalidStatus     = errors.New("无效的协作状态")
	ErrFileNotFound      = errors.New("文件不存在")
)

// Service 协作业务服务。
type Service struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{DB: db} }

// Invite 邀请用户协作一个 Markdown 文件。
// 只有 owner 或 manager 可以邀请；不能邀请自己。
func (s *Service) Invite(ownerID uint, vaultID, filePath, username string) (*models.Collaboration, error) {
	var vault models.Vault
	if err := s.DB.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		return nil, ErrFileNotFound
	}
	if vault.OwnerID != ownerID {
		var member models.VaultMember
		if err := s.DB.Where("vault_id = ? AND user_id = ?", vaultID, ownerID).First(&member).Error; err != nil {
			return nil, ErrNotOwnerOrManager
		}
		if member.Role != "manager" {
			return nil, ErrNotOwnerOrManager
		}
	}
	// 目标文件必须存在且是 Markdown。
	var file models.File
	if err := s.DB.Where(
		"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ? AND type = ?",
		vault.OwnerID, vaultID, filePath, false, "markdown",
	).First(&file).Error; err != nil {
		return nil, ErrFileNotFound
	}
	var target models.User
	if err := s.DB.Where("username = ?", username).First(&target).Error; err != nil {
		return nil, ErrUserNotFound
	}
	if target.ID == ownerID {
		return nil, ErrSelfInvite
	}
	// 重复关系检查：pending 或 accepted 视为未结束。
	var existing int64
	if err := s.DB.Model(&models.Collaboration{}).
		Where("vault_id = ? AND file_id = ? AND collaborator_id = ? AND status IN ?",
			vaultID, file.ID, target.ID, []string{StatusPending, StatusAccepted}).
		Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrDuplicate
	}
	row := models.Collaboration{
		VaultID: vaultID, FileID: file.ID, OwnerID: ownerID,
		CollaboratorID: target.ID, Status: StatusPending,
	}
	if err := s.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListForUser 返回用户收到的邀请和接受的协作。
func (s *Service) ListForUser(userID uint) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("collaborator_id = ?", userID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListForVault 返回仓库内全部协作关系（owner/manager 视角）。
func (s *Service) ListForVault(vaultID string) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("vault_id = ?", vaultID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Respond 被邀请者接受或拒绝。
func (s *Service) Respond(userID uint, collabID uint, accept bool) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var row models.Collaboration
		if err := tx.Where("id = ? AND collaborator_id = ?", collabID, userID).First(&row).Error; err != nil {
			return ErrNotCollaborator
		}
		if row.Status != StatusPending {
			return ErrInvalidStatus
		}
		status := StatusRevoked
		if accept {
			status = StatusAccepted
		}
		return tx.Model(&row).Update("status", status).Error
	})
}

// Revoke 撤回 pending 邀请或解除 accepted 协作（owner/manager）。
func (s *Service) Revoke(ownerID uint, collabID uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var row models.Collaboration
		if err := tx.Where("id = ?", collabID).First(&row).Error; err != nil {
			return errors.New("协作关系不存在")
		}
		if !s.canManage(tx, ownerID, row.VaultID) {
			return ErrNotOwnerOrManager
		}
		return tx.Model(&row).Update("status", StatusRevoked).Error
	})
}

// Leave ends an accepted collaboration at the collaborator's request.
func (s *Service) Leave(collaboratorID uint, collabID uint) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var row models.Collaboration
		if err := tx.Where("id = ? AND collaborator_id = ?", collabID, collaboratorID).First(&row).Error; err != nil {
			return ErrNotCollaborator
		}
		if row.Status != StatusAccepted {
			return ErrInvalidStatus
		}
		return tx.Model(&row).Update("status", StatusRevoked).Error
	})
}

// CollaborationsForFile 返回文件当前的 accepted 协作关系（用于事件通知）。
func (s *Service) CollaborationsForFile(vaultID string, fileID uint) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("vault_id = ? AND file_id = ? AND status = ?",
		vaultID, fileID, StatusAccepted).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// RevokeForPath 文件被删除或重命名时撤销相关协作。
func (s *Service) RevokeForPath(vaultID, oldPath string, fileID uint) error {
	if fileID > 0 {
		return s.DB.Model(&models.Collaboration{}).
			Where("vault_id = ? AND file_id = ? AND status = ?", vaultID, fileID, StatusAccepted).
			Update("status", StatusRevoked).Error
	}
	// 按路径反查 file id。
	var files []models.File
	if err := s.DB.Where("vault_id = ? AND path = ?", vaultID, oldPath).Find(&files).Error; err != nil {
		return err
	}
	for _, f := range files {
		if err := s.DB.Model(&models.Collaboration{}).
			Where("vault_id = ? AND file_id = ? AND status = ?", vaultID, f.ID, StatusAccepted).
			Update("status", StatusRevoked).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) canManage(tx *gorm.DB, userID uint, vaultID string) bool {
	var vault models.Vault
	if err := tx.Where("id = ?", vaultID).First(&vault).Error; err == nil && vault.OwnerID == userID {
		return true
	}
	var member models.VaultMember
	if err := tx.Where("vault_id = ? AND user_id = ?", vaultID, userID).First(&member).Error; err == nil {
		return member.Role == "manager"
	}
	return false
}

// 事件总线

// Event 协作事件。
type Event struct {
	VaultID  string `json:"vault_id"`
	FileID   uint   `json:"file_id"`
	FilePath string `json:"file_path"`
	Kind     string `json:"kind"` // changed / revoked / invited
	Revision int64  `json:"revision"`
	At       int64  `json:"at"`
}

// Broker 按 Vault 分发协作事件。
type Broker struct {
	mu      sync.Mutex
	subs    map[string][]chan Event
	version map[string]int64
}

func NewBroker() *Broker {
	return &Broker{
		subs:    map[string][]chan Event{},
		version: map[string]int64{},
	}
}

// Subscribe 订阅某 Vault 的事件，返回事件通道与当前版本号。
func (b *Broker) Subscribe(vaultID string) (chan Event, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 16)
	b.subs[vaultID] = append(b.subs[vaultID], ch)
	return ch, b.version[vaultID]
}

// Unsubscribe 移除订阅。
func (b *Broker) Unsubscribe(vaultID string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[vaultID]
	for i, c := range subs {
		if c == ch {
			b.subs[vaultID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish 广播事件并递增版本号。
func (b *Broker) Publish(ev Event) {
	b.PublishTo(ev.VaultID, ev)
}

// PublishTo publishes an event on an explicit Vault or account topic.
func (b *Broker) PublishTo(topic string, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.version[topic]++
	ev.Revision = b.version[topic]
	for _, ch := range b.subs[topic] {
		select {
		case ch <- ev:
		default:
			// 订阅者落后时丢弃，等待下一次拉取。
		}
	}
}

// WaitVersion 阻塞直到版本号超过 last，用于长轮询。
func (b *Broker) WaitVersion(vaultID string, last int64, timeout time.Duration) (int64, bool) {
	ch, _ := b.Subscribe(vaultID)
	defer b.Unsubscribe(vaultID, ch)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		b.mu.Lock()
		cur := b.version[vaultID]
		b.mu.Unlock()
		if cur > last {
			return cur, true
		}
		select {
		case <-ch:
			continue
		case <-timer.C:
			return b.CurrentVersion(vaultID), false
		}
	}
}

// CurrentVersion 返回 Vault 当前事件版本。
func (b *Broker) CurrentVersion(vaultID string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version[vaultID]
}

var _ = fmt.Sprintf
