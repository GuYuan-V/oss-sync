// Package collaboration 提供 Markdown 协作状态与事件发布
package collaboration

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
)

// 协作关系的生命周期状态
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

// Service 提供协作关系的持久化与鉴权
type Service struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Service { return &Service{DB: db} }

// Invite 为 owner 或 manager 创建待接受的 Markdown 文件协作
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
	// 目标须为存在且未删除的 Markdown 文件
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
	// pending 与 accepted 均视为未结束的协作关系
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

// ListForUser 返回某用户收到的全部协作关系
func (s *Service) ListForUser(userID uint) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("collaborator_id = ?", userID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListForVault 返回 Vault 的 owner 或 manager 可见的全部协作关系
func (s *Service) ListForVault(vaultID string) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("vault_id = ?", vaultID).
		Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Respond 接受或拒绝待处理的邀请
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

// Revoke 供 owner 或 manager 结束待处理邀请或已接受的协作
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

// Leave 供协作者主动结束已接受的协作
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

// CollaborationsForFile 返回已接受的协作关系，供事件广播确定接收者
func (s *Service) CollaborationsForFile(vaultID string, fileID uint) ([]models.Collaboration, error) {
	var rows []models.Collaboration
	if err := s.DB.Where("vault_id = ? AND file_id = ? AND status = ?",
		vaultID, fileID, StatusAccepted).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// RevokeForPath 在文件删除或重命名时结束已接受的协作
func (s *Service) RevokeForPath(vaultID, oldPath string, fileID uint) error {
	if fileID > 0 {
		return s.DB.Model(&models.Collaboration{}).
			Where("vault_id = ? AND file_id = ? AND status = ?", vaultID, fileID, StatusAccepted).
			Update("status", StatusRevoked).Error
	}
	// 允许只传路径的调用，此处按路径解析文件标识
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

// Event 描述一条协作通知
type Event struct {
	VaultID  string `json:"vault_id"`
	FileID   uint   `json:"file_id"`
	FilePath string `json:"file_path"`
	Kind     string `json:"kind"` // Kind 取值：changed、revoked、invited
	Revision int64  `json:"revision"`
	At       int64  `json:"at"`
}

// Broker 按 Vault 或账号主题分发协作事件
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

// Subscribe 注册 Vault 主题订阅者并返回当前版本号
func (b *Broker) Subscribe(vaultID string) (chan Event, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 16)
	b.subs[vaultID] = append(b.subs[vaultID], ch)
	return ch, b.version[vaultID]
}

// Unsubscribe 移除主题订阅者并关闭其通道
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

// Publish 在事件所属 Vault 主题上广播
func (b *Broker) Publish(ev Event) {
	b.PublishTo(ev.VaultID, ev)
}

// PublishTo 在指定的 Vault 或账号主题上发布事件
func (b *Broker) PublishTo(topic string, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.version[topic]++
	ev.Revision = b.version[topic]
	for _, ch := range b.subs[topic] {
		select {
		case ch <- ev:
		default:
			// 慢订阅者通过下次轮询恢复，发布者不阻塞
		}
	}
}

// WaitVersion 阻塞等待主题版本超过 last，直至超时
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

// CurrentVersion 返回主题的当前版本号
func (b *Broker) CurrentVersion(vaultID string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.version[vaultID]
}

var _ = fmt.Sprintf
