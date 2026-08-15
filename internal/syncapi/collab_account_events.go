package syncapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
)

func collaborationUserTopic(userID uint) string {
	return "user:" + strconv.FormatUint(uint64(userID), 10)
}

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

func (h *Handler) CollabAccountPoll(c *gin.Context) {
	user, ok := auth.RequireUser(c)
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

func (h *Handler) collabEventUser(c *gin.Context) (*models.User, bool) {
	if token := c.Query("token"); token != "" {
		if !collabQueryTokenAllowed(c.Request, c.GetHeader("X-Forwarded-Proto")) {
			c.AbortWithStatus(http.StatusForbidden)
			return nil, false
		}
		user, err := auth.AuthenticateToken(h.DB, h.Cfg, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return nil, false
		}
		return user, true
	}
	header := c.GetHeader("Authorization")
	if header == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil, false
	}
	user, err := auth.AuthenticateToken(h.DB, h.Cfg, strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}
