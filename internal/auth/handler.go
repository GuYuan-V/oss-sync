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
		role = "user"
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
	c.JSON(http.StatusOK, AuthResponse{
		Token:     token,
		ExpiresIn: expiresIn,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
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
	return &u, nil
}
