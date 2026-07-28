// Package webui 提供公开注册页和管理员控制台。
package webui

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

const adminCookieName = "oss_admin_session"

//go:embed templates/*.html assets/*
var webFS embed.FS

type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
	tpl *template.Template
}

type registerView struct {
	RegistrationEnabled bool
	Username            string
	Error               string
	Success             bool
}

type adminLoginView struct {
	Error string
}

type adminUserRow struct {
	Username  string
	Role      string
	CreatedAt string
}

type adminView struct {
	AdminUsername       string
	RegistrationEnabled bool
	UserCount           int
	AdminCount          int
	Users               []adminUserRow
	Saved               bool
}

func New(db *gorm.DB, cfg *config.Config) (*Handler, error) {
	tpl, err := template.ParseFS(webFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse web UI templates: %w", err)
	}
	return &Handler{DB: db, Cfg: cfg, tpl: tpl}, nil
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/register")
	})
	r.GET("/ui/assets/console.css", h.styles)
	r.GET("/register", h.registerPage)
	r.POST("/register", h.registerSubmit)
	r.GET("/admin/login", h.adminLoginPage)
	r.POST("/admin/login", h.adminLoginSubmit)

	adminGroup := r.Group("/admin", h.requireAdmin)
	{
		adminGroup.GET("", h.adminDashboard)
		adminGroup.POST("/registration", h.updateRegistration)
		adminGroup.POST("/logout", h.adminLogout)
	}
}

func (h *Handler) registerPage(c *gin.Context) {
	enabled, err := auth.RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load registration settings")
		return
	}
	h.render(c, "register.html", registerView{RegistrationEnabled: enabled})
}

func (h *Handler) registerSubmit(c *gin.Context) {
	enabled, err := auth.RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load registration settings")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	view := registerView{RegistrationEnabled: enabled, Username: username}
	if !enabled {
		view.Error = "管理员已关闭新用户注册。"
		h.renderStatus(c, http.StatusForbidden, "register.html", view)
		return
	}
	password := c.PostForm("password")
	if password != c.PostForm("password_confirm") {
		view.Error = "两次输入的密码不一致。"
		h.renderStatus(c, http.StatusBadRequest, "register.html", view)
		return
	}
	if err := auth.ValidateAccountInput(username, password); err != nil {
		view.Error = err.Error()
		h.renderStatus(c, http.StatusBadRequest, "register.html", view)
		return
	}
	if _, err := auth.CreateAccount(h.DB, username, password, "user"); err != nil {
		view.Error = "该用户名已存在，请换一个用户名。"
		h.renderStatus(c, http.StatusConflict, "register.html", view)
		return
	}
	view.Success = true
	view.Username = username
	h.render(c, "register.html", view)
}

func (h *Handler) adminLoginPage(c *gin.Context) {
	if user := h.adminFromCookie(c); user != nil {
		c.Redirect(http.StatusFound, "/admin")
		return
	}
	h.render(c, "admin_login.html", adminLoginView{})
}

func (h *Handler) adminLoginSubmit(c *gin.Context) {
	user, err := auth.AuthenticateCredentials(
		h.DB,
		c.PostForm("username"),
		c.PostForm("password"),
	)
	if err != nil || user.Role != "admin" {
		h.renderStatus(c, http.StatusUnauthorized, "admin_login.html", adminLoginView{
			Error: "管理员账号或密码不正确。",
		})
		return
	}
	token, expiresIn, err := auth.IssueToken(h.Cfg, *user)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to create admin session")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     adminCookieName,
		Value:    token,
		Path:     "/admin",
		MaxAge:   int(expiresIn),
		HttpOnly: true,
		Secure:   requestIsHTTPS(c),
		SameSite: http.SameSiteStrictMode,
	})
	c.Redirect(http.StatusSeeOther, "/admin")
}

func (h *Handler) adminDashboard(c *gin.Context) {
	current := c.MustGet("oss.admin").(*models.User)
	enabled, err := auth.RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load registration settings")
		return
	}
	var users []models.User
	if err := h.DB.Order("created_at asc, id asc").Find(&users).Error; err != nil {
		c.String(http.StatusInternalServerError, "failed to load users")
		return
	}
	rows := make([]adminUserRow, 0, len(users))
	adminCount := 0
	for _, user := range users {
		if user.Role == "admin" {
			adminCount++
		}
		rows = append(rows, adminUserRow{
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt.Local().Format("2006-01-02 15:04"),
		})
	}
	h.render(c, "admin.html", adminView{
		AdminUsername:       current.Username,
		RegistrationEnabled: enabled,
		UserCount:           len(users),
		AdminCount:          adminCount,
		Users:               rows,
		Saved:               c.Query("saved") == "1",
	})
}

func (h *Handler) updateRegistration(c *gin.Context) {
	enabled := c.PostForm("registration_enabled") == "on"
	if err := auth.SetRegistrationEnabled(h.DB, enabled); err != nil {
		c.String(http.StatusInternalServerError, "failed to update registration settings")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin?saved=1")
}

func (h *Handler) adminLogout(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(c),
		SameSite: http.SameSiteStrictMode,
	})
	c.Redirect(http.StatusSeeOther, "/admin/login")
}

func (h *Handler) requireAdmin(c *gin.Context) {
	user := h.adminFromCookie(c)
	if user == nil {
		c.Redirect(http.StatusSeeOther, "/admin/login")
		c.Abort()
		return
	}
	c.Set("oss.admin", user)
	c.Next()
}

func (h *Handler) adminFromCookie(c *gin.Context) *models.User {
	token, err := c.Cookie(adminCookieName)
	if err != nil || token == "" {
		return nil
	}
	user, err := auth.AuthenticateToken(h.DB, h.Cfg, token)
	if err != nil || user.Role != "admin" {
		return nil
	}
	return user
}

func (h *Handler) styles(c *gin.Context) {
	raw, err := webFS.ReadFile("assets/console.css")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "text/css; charset=utf-8", raw)
}

func (h *Handler) render(c *gin.Context, name string, data any) {
	h.renderStatus(c, http.StatusOK, name, data)
}

func (h *Handler) renderStatus(c *gin.Context, status int, name string, data any) {
	setPageHeaders(c)
	c.Status(status)
	if err := h.tpl.ExecuteTemplate(c.Writer, name, data); err != nil &&
		!errors.Is(err, http.ErrHandlerTimeout) {
		_ = err
	}
}

func setPageHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'self'; img-src 'self' data:; "+
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
	)
}

func requestIsHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil ||
		strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}
