// Package auth 提供用户注册、登录和请求鉴权。
//
//	POST /api/auth/register  注册（由数据库开关控制匿名普通用户注册）
//	POST /api/auth/login      登录，返回 JWT
//
// Middleware 同时支持 Bearer JWT 与 Basic 认证。
// 任何 handler 用 auth.RequireUser(c) 取当前用户。
package auth

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/jwt"
	"github.com/oss/oss-server/internal/models"
)

// Handler 持有 auth 路由所需依赖。
type Handler struct {
	DB            *gorm.DB
	Cfg           *config.Config
	registerMu    sync.Mutex
	loginLimit    *AttemptLimiter
	registerLimit *AttemptLimiter
}

// NewHandler 创建 auth handler。
func NewHandler(db *gorm.DB, cfg *config.Config) *Handler {
	return &Handler{DB: db, Cfg: cfg, loginLimit: NewAttemptLimiter(8, time.Minute), registerLimit: NewAttemptLimiter(5, time.Minute)}
}

// Register 在 gin 引擎上挂载 auth 路由组。
func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api/auth")
	{
		g.GET("/status", h.Status)
		g.POST("/register", OptionalMiddleware(h.DB, h.Cfg), h.RegisterUser)
		g.POST("/login", h.Login)
		g.GET("/device-status", Middleware(h.DB, h.Cfg), h.DeviceStatus)
	}

	accountGroup := r.Group("/api/account", Middleware(h.DB, h.Cfg))
	{
		accountGroup.POST("/password", h.ChangePassword)
	}
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Role     string `json:"role"` // 可空，默认 user
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"` // 秒
	UserID    uint   `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	// DeviceStatus 仅在插件登录时返回：pending / approved / revoked。
	DeviceStatus string `json:"device_status,omitempty"`
	// DeviceName 服务端确认的设备名称。
	DeviceName string `json:"device_name,omitempty"`
}

// RegisterUser 处理 POST /api/auth/register。
// 匿名请求只能在数据库注册开关开启时创建普通用户；管理员始终可以创建用户。
func (h *Handler) RegisterUser(c *gin.Context) {
	if !h.registerLimit.Allow("register:" + c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many registration attempts; try again later"})
		return
	}
	h.registerMu.Lock()
	defer h.registerMu.Unlock()

	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "用户名或密码不符合要求（用户名 3-64 位，密码至少 8 位且不超过 72 字节）: " + err.Error(),
		})
		return
	}
	if err := ValidateAccountInput(req.Username, req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role := strings.ToLower(req.Role)
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role 必须为 admin 或 user"})
		return
	}

	cur := CurrentUser(c)
	if cur == nil {
		enabled, err := RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取注册设置失败"})
			return
		}
		if !enabled {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "管理员已关闭新用户注册",
				"code":  "registration_closed",
			})
			return
		}
		// 无管理员时第一个注册账户自动成为 admin。
		role, err = ResolveRegistrationRole(h.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取管理员状态失败"})
			return
		}
	} else if cur.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "仅 admin 可创建其他用户，当前用户 " + cur.Username + " 无权限",
			"code":  "not_admin",
		})
		return
	}

	u, err := CreateAccount(h.DB, req.Username, req.Password, role)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	token, expiresIn, err := IssueToken(h.Cfg, *u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sign failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, AuthResponse{
		Token:     token,
		ExpiresIn: expiresIn,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
	})
}

func (h *Handler) Status(c *gin.Context) {
	var adminCount int64
	if err := h.DB.Model(&models.User{}).Where("role = ?", "admin").Count(&adminCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query auth status"})
		return
	}
	enabled, err := RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query registration status"})
		return
	}
	mode := "closed"
	if enabled {
		mode = "open"
	}
	c.JSON(http.StatusOK, gin.H{
		"needs_first_admin":    adminCount == 0,
		"registration_enabled": enabled,
		"registration_mode":    mode,
		"registration_url":     "/register",
		"admin_url":            "/admin",
	})
}

// Login 处理 POST /api/auth/login。
func (h *Handler) Login(c *gin.Context) {
	if !h.loginLimit.Allow("login:" + c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts; try again later"})
		return
	}
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := AuthenticateCredentials(h.DB, req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, expiresIn, err := IssueToken(h.Cfg, *u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sign failed: " + err.Error()})
		return
	}
	resp := AuthResponse{
		Token:     token,
		ExpiresIn: expiresIn,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
	}

	// 插件登录携带设备头：登记设备并返回设备授权状态。
	clientID := deviceauth.NormalizeClientID(c.GetHeader(deviceauth.ClientIDHeader))
	if clientID != "" {
		deviceName := deviceauth.DecodeDeviceName(c.GetHeader(deviceauth.DeviceNameHeader))
		status, err := deviceauth.RegisterDevice(h.DB, u.ID, clientID, deviceName, time.Now())
		if err != nil {
			if errors.Is(err, deviceauth.ErrRevoked) {
				c.JSON(http.StatusForbidden, gin.H{"error": "this device has been revoked", "code": "device_revoked"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "登记设备失败: " + err.Error()})
			return
		}
		resp.DeviceStatus = status
		resp.DeviceName = deviceName
	}
	c.JSON(http.StatusOK, resp)
}

// DeviceStatus 处理 GET /api/auth/device-status?client_id=xxx。
// 插件在设备待授权期间每 5 秒轮询该接口。
func (h *Handler) DeviceStatus(c *gin.Context) {
	u, ok := RequireUser(c)
	if !ok {
		return
	}
	clientID := deviceauth.NormalizeClientID(c.Query("client_id"))
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		return
	}
	status, name, err := deviceauth.GetDevice(h.DB, u.ID, clientID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":      status,
		"device_name": name,
	})
}

// changePasswordRequest 修改自己的密码请求体。
type changePasswordRequest struct {
	OldPassword     string `json:"old_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

// ChangePassword 处理 POST /api/account/password。
// 校验旧密码后更新密码并递增 token 版本；返回新 token 供调用方继续会话。
func (h *Handler) ChangePassword(c *gin.Context) {
	u, ok := RequireUser(c)
	if !ok {
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.NewPassword != req.ConfirmPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "两次输入的新密码不一致", "code": "password_mismatch"})
		return
	}
	if err := ChangePassword(h.DB, u.ID, req.OldPassword, req.NewPassword); err != nil {
		if errors.Is(err, errBadCred) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "旧密码不正确", "code": "wrong_old_password"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 重新读取用户（token 版本已递增），签发新 token 保持当前会话。
	var updated models.User
	if err := h.DB.First(&updated, u.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取用户失败"})
		return
	}
	token, expiresIn, err := IssueToken(h.Cfg, updated)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发新会话失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":    "密码已更新",
		"token":      token,
		"expires_in": expiresIn,
	})
}

func authenticateAny(db *gorm.DB, cfg *config.Config, header string) (*models.User, error) {
	if header == "" {
		return nil, errNoAuth
	}
	switch {
	case strings.HasPrefix(header, "Bearer "):
		return authenticateBearer(db, cfg, header[len("Bearer "):])
	case strings.HasPrefix(header, "Basic "):
		user, pass, ok := parseBasic(header[len("Basic "):])
		if !ok || pass == "" {
			return nil, errBadCred
		}
		var u models.User
		if err := db.Where("username = ?", user).First(&u).Error; err != nil {
			return nil, errUserNotFound
		}
		if u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
			return nil, errBadCred
		}
		return &u, nil
	default:
		return nil, errBadScheme
	}
}

func authenticateBearer(db *gorm.DB, cfg *config.Config, token string) (*models.User, error) {
	claims, err := jwt.Parse(cfg.Auth.JWTSecret, token)
	if err != nil {
		return nil, errors.Join(errBadCred, err)
	}
	var u models.User
	if err := db.First(&u, claims.UserID).Error; err != nil {
		return nil, errUserNotFound
	}
	if claims.TokenVersion != u.TokenVersion {
		return nil, errBadCred
	}
	return &u, nil
}
