package syncapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/collaboration"
	"github.com/helantianshen/oss-sync/internal/deviceauth"
	"github.com/helantianshen/oss-sync/internal/models"
)

// collaborationUserTopic 返回账号级协作事件的订阅主题，按用户 ID 隔离
func collaborationUserTopic(userID uint) string {
	return "user:" + strconv.FormatUint(uint64(userID), 10)
}

// publishCollaborationEvent 广播协作事件，同时按用户主题与其所属仓库主题投递，
// invited 事件在仓库主题上降级为 changed 以兼容旧客户端
func (h *Handler) publishCollaborationEvent(event collaboration.Event, userIDs []uint) {
	h.broker.Publish(event)
	seenUsers := make(map[uint]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if _, exists := seenUsers[userID]; exists {
			continue
		}
		seenUsers[userID] = struct{}{}
		h.broker.PublishTo(collaborationUserTopic(userID), event)

		var ownedVaultIDs []string
		_ = h.DB.Model(&models.Vault{}).Where("owner_id = ?", userID).Pluck("id", &ownedVaultIDs).Error
		var memberVaultIDs []string
		_ = h.DB.Model(&models.VaultMember{}).Where("user_id = ?", userID).Pluck("vault_id", &memberVaultIDs).Error
		legacyEvent := event
		if legacyEvent.Kind == "invited" {
			legacyEvent.Kind = "changed"
		}
		seenVaults := map[string]struct{}{event.VaultID: {}}
		for _, vaultID := range append(ownedVaultIDs, memberVaultIDs...) {
			if _, exists := seenVaults[vaultID]; exists {
				continue
			}
			seenVaults[vaultID] = struct{}{}
			h.broker.PublishTo(vaultID, legacyEvent)
		}
	}
}

// collaborationEventUsers 返回参与该文件协作的所有用户，包含所有者与协作者，
// 查询失败时返回 nil
func (h *Handler) collaborationEventUsers(vaultID string, fileID uint) []uint {
	var rows []models.Collaboration
	if err := h.DB.Where("vault_id = ? AND file_id = ?", vaultID, fileID).Find(&rows).Error; err != nil {
		return nil
	}
	userIDs := make([]uint, 0, len(rows)*2)
	for _, row := range rows {
		userIDs = append(userIDs, row.OwnerID, row.CollaboratorID)
	}
	return userIDs
}

// CollabAccountSSE 以 SSE 长连接推送账号级协作事件，
// 鉴权失败时由内部处理器直接返回状态码，连接期每 20 秒发送一次心跳
func (h *Handler) CollabAccountSSE(c *gin.Context) {
	user, ok := h.collabEventUser(c)
	if !ok {
		return
	}
	topic := collaborationUserTopic(user.ID)
	ch, _ := h.broker.Subscribe(topic)
	defer h.broker.Unsubscribe(topic, ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	fmt.Fprintf(c.Writer, "event: ready\ndata: {\"user_id\":%d}\n\n", user.ID)
	c.Writer.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-ch:
			fmt.Fprintf(c.Writer, "event: %s\ndata: {\"file_id\":%d,\"file_path\":%q,\"vault_id\":%q}\n\n",
				event.Kind, event.FileID, event.FilePath, event.VaultID)
			c.Writer.Flush()
		case <-heartbeat.C:
			fmt.Fprint(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

// CollabAccountPoll 长轮询账号级协作事件版本，wait 超出 0 到 30 秒范围时收敛为 30 秒，
// after 非数字或为负数时返回 400
func (h *Handler) CollabAccountPoll(c *gin.Context) {
	user, _, ok := h.requireCollaborationDevice(c)
	if !ok {
		return
	}
	last, err := parseInt64Default(c.Query("after"), 0)
	if err != nil || last < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid after"})
		return
	}
	waitSec, err := parseIntDefault(c.Query("wait"), 30)
	if err != nil || waitSec < 0 || waitSec > 30 {
		waitSec = 30
	}
	topic := collaborationUserTopic(user.ID)
	version, changed := h.broker.WaitVersion(topic, last, time.Duration(waitSec)*time.Second)
	c.JSON(http.StatusOK, gin.H{"changed": changed, "version": version})
}

// collabEventUser 解析 SSE 请求的凭据，查询参数 token 仅允许出现在 TLS、
// https 转发或本机回环链路，其余情况直接 403；
// 依次校验身份令牌与设备审批状态，任一失败直接写响应并返回 false
func (h *Handler) collabEventUser(c *gin.Context) (*models.User, bool) {
	token := ""
	if queryToken := c.Query("token"); queryToken != "" {
		if !collabQueryTokenAllowed(c.Request, c.GetHeader("X-Forwarded-Proto")) {
			c.AbortWithStatus(http.StatusForbidden)
			return nil, false
		}
		token = queryToken
	} else {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return nil, false
		}
		token = strings.TrimPrefix(header, "Bearer ")
	}
	identity, err := auth.AuthenticateIdentityToken(h.DB, h.Cfg, token)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil, false
	}
	c.Set(auth.ContextKeyCurrentUser, identity.User)
	c.Set(auth.ContextKeyIdentity, identity)
	did, ok := auth.RequireDeviceID(c, c.Query("client_id"), c.GetHeader(deviceauth.ClientIDHeader))
	if !ok {
		return nil, false
	}
	if err := deviceauth.CheckApproved(h.DB, identity.User.ID, string(did)); err != nil {
		h.writeDeviceAuthError(c, err)
		return nil, false
	}
	return identity.User, true
}

// requireCollaborationDevice 从已认证上下文取用户与设备 ID，
// 并校验设备审批状态，失败时直接写响应并返回 false
func (h *Handler) requireCollaborationDevice(c *gin.Context) (*models.User, string, bool) {
	user, ok := auth.RequireUser(c)
	if !ok {
		return nil, "", false
	}
	did, ok := auth.RequireDeviceID(c, c.Query("client_id"), c.GetHeader(deviceauth.ClientIDHeader))
	if !ok {
		return nil, "", false
	}
	if err := deviceauth.CheckApproved(h.DB, user.ID, string(did)); err != nil {
		h.writeDeviceAuthError(c, err)
		return nil, "", false
	}
	return user, string(did), true
}
