// Package admin 提供管理员用户管理 API。
package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// Handler 持有 admin 路由依赖。
type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
}

// New 创建 admin handler。
func New(db *gorm.DB, cfg *config.Config) *Handler {
	return &Handler{DB: db, Cfg: cfg}
}

// Register 挂载管理员路由。
func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api/admin", auth.Middleware(h.DB, h.Cfg), h.requireAdmin)
	{
		g.GET("/users", h.ListUsers)
		g.PATCH("/users/:id", h.UpdateUser)
		g.PUT("/users/:id/password", h.ResetPassword)
		g.DELETE("/users/:id", h.DeleteUser)
		h.themesRouter(g)
	}
}

func (h *Handler) requireAdmin(c *gin.Context) {
	if _, ok := auth.RequireAdmin(c); !ok {
		c.Abort()
	}
}

type userOut struct {
	ID           uint   `json:"id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	StorageQuota int64  `json:"storage_quota"`
	CreatedAt    string `json:"created_at"`
}

func (h *Handler) ListUsers(c *gin.Context) {
	var users []models.User
	if err := h.DB.Order("created_at asc, id asc").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]userOut, 0, len(users))
	for _, u := range users {
		out = append(out, userOut{
			ID: u.ID, Username: u.Username, Role: u.Role,
			StorageQuota: u.StorageQuota,
			CreatedAt:    u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type updateUserRequest struct {
	Role         *string `json:"role"`
	StorageQuota *int64  `json:"storage_quota"`
}

func (h *Handler) UpdateUser(c *gin.Context) {
	actor, ok := auth.RequireAdmin(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var target models.User
	if err := h.DB.First(&target, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	updates := map[string]any{}
	if req.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*req.Role))
		if role != "admin" && role != "user" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or user"})
			return
		}
		if target.Role == "admin" && role != "admin" && target.ID == actor.ID {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot demote yourself", "code": "last_admin"})
			return
		}
		if target.Role == "admin" && role != "admin" {
			if err := h.ensureNotLastAdmin(target.ID); err != nil {
				c.JSON(http.StatusConflict, gin.H{"error": "cannot demote the last admin", "code": "last_admin"})
				return
			}
		}
		updates["role"] = role
	}
	if req.StorageQuota != nil && *req.StorageQuota >= 0 {
		updates["storage_quota"] = *req.StorageQuota
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"id": target.ID, "username": target.Username})
		return
	}
	if err := h.DB.Model(&models.User{}).Where("id = ?", target.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": target.ID, "username": target.Username, "role": target.Role})
}

type resetPasswordRequest struct {
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

func (h *Handler) ResetPassword(c *gin.Context) {
	if _, ok := auth.RequireAdmin(c); !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.NewPassword != req.ConfirmPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "两次输入的新密码不一致", "code": "password_mismatch"})
		return
	}
	if err := auth.SetPassword(h.DB, uint(userID), req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "密码已重置"})
}

func (h *Handler) DeleteUser(c *gin.Context) {
	actor, ok := auth.RequireAdmin(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var target models.User
	if err := h.DB.First(&target, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if target.ID == actor.ID {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot delete yourself", "code": "last_admin"})
		return
	}
	if target.Role == "admin" {
		if err := h.ensureNotLastAdmin(target.ID); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot delete the last admin", "code": "last_admin"})
			return
		}
	}
	if err := h.DB.Delete(&target).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ensureNotLastAdmin(excludeID uint) error {
	var count int64
	if err := h.DB.Model(&models.User{}).
		Where("role = ? AND id <> ?", "admin", excludeID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
