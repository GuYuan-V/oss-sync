// Package webui 提供公开注册页和管理员控制台。
package webui

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultbackup"
)

const adminCookieName = "oss_admin_session"

//go:embed templates/*.html assets/*
var webFS embed.FS

type Handler struct {
	DB            *gorm.DB
	Cfg           *config.Config
	tpl           *template.Template
	loginLimit    *auth.AttemptLimiter
	registerLimit *auth.AttemptLimiter
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
	Vaults              []adminVaultRow
	Backups             []adminBackupRow
	BackupCount         int
	Saved               bool
}

type adminBackupRow struct {
	ID        string
	VaultName string
	Owner     string
	FileName  string
	Size      string
	CreatedAt string
}

type adminVaultRow struct {
	ID          string
	Name        string
	Owner       string
	MemberCount int64
	StorageUsed string
}

type vaultMemberView struct {
	AdminUsername string
	VaultID       string
	VaultName     string
	Owner         string
	Members       []adminVaultMemberRow
	Saved         bool
	Error         string
}

type adminVaultMemberRow struct {
	UserID   uint
	Username string
	Role     string
}

func New(db *gorm.DB, cfg *config.Config) (*Handler, error) {
	tpl, err := template.ParseFS(webFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse web UI templates: %w", err)
	}
	return &Handler{DB: db, Cfg: cfg, tpl: tpl, loginLimit: auth.NewAttemptLimiter(8, time.Minute), registerLimit: auth.NewAttemptLimiter(5, time.Minute)}, nil
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
		adminGroup.GET("/vaults/:vault_id", h.vaultMembersPage)
		adminGroup.POST("/vaults/:vault_id/members", h.addVaultMember)
		adminGroup.POST("/vaults/:vault_id/members/:user_id/role", h.updateVaultMemberRole)
		adminGroup.POST("/vaults/:vault_id/members/:user_id/delete", h.deleteVaultMember)
		adminGroup.GET("/backups/:id/download", h.downloadBackup)
		adminGroup.POST("/backups/:id/delete", h.deleteBackup)
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
	if !h.registerLimit.Allow("web-register:" + c.ClientIP()) {
		h.renderStatus(c, http.StatusTooManyRequests, "register.html", registerView{Error: "尝试次数过多，请稍后再试。"})
		return
	}
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
	if !h.loginLimit.Allow("admin-login:" + c.ClientIP()) {
		h.renderStatus(c, http.StatusTooManyRequests, "admin_login.html", adminLoginView{Error: "尝试次数过多，请稍后再试。"})
		return
	}
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
	backups, err := h.backupRows()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load vault backups")
		return
	}
	vaults, err := h.vaultRows()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load vaults")
		return
	}
	h.render(c, "admin.html", adminView{
		AdminUsername:       current.Username,
		RegistrationEnabled: enabled,
		UserCount:           len(users),
		AdminCount:          adminCount,
		Users:               rows,
		Vaults:              vaults,
		Backups:             backups,
		BackupCount:         len(backups),
		Saved:               c.Query("saved") == "1",
	})
}

func (h *Handler) vaultRows() ([]adminVaultRow, error) {
	var vaults []models.Vault
	if err := h.DB.Order("created_at desc").Find(&vaults).Error; err != nil {
		return nil, err
	}
	ownerIDs := make([]uint, 0, len(vaults))
	for _, vault := range vaults {
		ownerIDs = append(ownerIDs, vault.OwnerID)
	}
	var users []models.User
	if len(ownerIDs) > 0 {
		if err := h.DB.Where("id IN ?", ownerIDs).Find(&users).Error; err != nil {
			return nil, err
		}
	}
	owners := map[uint]string{}
	for _, user := range users {
		owners[user.ID] = user.Username
	}
	rows := make([]adminVaultRow, 0, len(vaults))
	for _, vault := range vaults {
		var members int64
		if err := h.DB.Model(&models.VaultMember{}).Where("vault_id = ?", vault.ID).Count(&members).Error; err != nil {
			return nil, err
		}
		rows = append(rows, adminVaultRow{
			ID: vault.ID, Name: vault.Name, Owner: owners[vault.OwnerID],
			MemberCount: members, StorageUsed: formatBytes(vault.StorageUsed),
		})
	}
	return rows, nil
}

func (h *Handler) vaultMembersPage(c *gin.Context) {
	current := c.MustGet("oss.admin").(*models.User)
	vault, owner, members, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	h.render(c, "vault_members.html", vaultMemberView{
		AdminUsername: current.Username, VaultID: vault.ID, VaultName: vault.Name,
		Owner: owner.Username, Members: members, Saved: c.Query("saved") == "1", Error: c.Query("error"),
	})
}

func (h *Handler) addVaultMember(c *gin.Context) {
	vault, _, _, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	role := strings.ToLower(strings.TrimSpace(c.PostForm("role")))
	if username == "" || (role != "manager" && role != "participant") {
		h.redirectVaultMembers(c, vault.ID, "成员用户名或角色无效")
		return
	}
	var user models.User
	if err := h.DB.Where("username = ?", username).First(&user).Error; err != nil {
		h.redirectVaultMembers(c, vault.ID, "未找到该用户")
		return
	}
	if user.ID == vault.OwnerID {
		h.redirectVaultMembers(c, vault.ID, "所有者已拥有完整权限")
		return
	}
	var member models.VaultMember
	err = h.DB.Where("vault_id = ? AND user_id = ?", vault.ID, user.ID).First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = h.DB.Create(&models.VaultMember{VaultID: vault.ID, UserID: user.ID, Role: role}).Error
	} else if err == nil {
		err = h.DB.Model(&member).Update("role", role).Error
	}
	if err != nil {
		h.redirectVaultMembers(c, vault.ID, "保存成员失败")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vault.ID+"?saved=1")
}

func (h *Handler) updateVaultMemberRole(c *gin.Context) {
	vault, _, _, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	userID, parseErr := strconv.ParseUint(c.Param("user_id"), 10, 64)
	role := strings.ToLower(strings.TrimSpace(c.PostForm("role")))
	if parseErr != nil || (role != "manager" && role != "participant") {
		h.redirectVaultMembers(c, vault.ID, "成员角色无效")
		return
	}
	result := h.DB.Model(&models.VaultMember{}).Where("vault_id = ? AND user_id = ?", vault.ID, uint(userID)).Update("role", role)
	if result.Error != nil || result.RowsAffected == 0 {
		h.redirectVaultMembers(c, vault.ID, "更新成员失败")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vault.ID+"?saved=1")
}

func (h *Handler) deleteVaultMember(c *gin.Context) {
	vault, _, _, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		h.redirectVaultMembers(c, vault.ID, "成员无效")
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND vault_id = ?", uint(userID), vault.ID).Delete(&models.DeviceVault{}).Error; err != nil {
			return err
		}
		result := tx.Where("vault_id = ? AND user_id = ?", vault.ID, uint(userID)).Delete(&models.VaultMember{})
		if result.Error != nil || result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		h.redirectVaultMembers(c, vault.ID, "移除成员失败")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vault.ID+"?saved=1")
}

func (h *Handler) loadVaultMembers(vaultID string) (models.Vault, models.User, []adminVaultMemberRow, error) {
	var vault models.Vault
	if err := h.DB.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		return models.Vault{}, models.User{}, nil, err
	}
	var owner models.User
	if err := h.DB.Where("id = ?", vault.OwnerID).First(&owner).Error; err != nil {
		return models.Vault{}, models.User{}, nil, err
	}
	var members []models.VaultMember
	if err := h.DB.Where("vault_id = ?", vault.ID).Order("created_at asc").Find(&members).Error; err != nil {
		return models.Vault{}, models.User{}, nil, err
	}
	ids := make([]uint, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	var users []models.User
	if len(ids) > 0 {
		if err := h.DB.Where("id IN ?", ids).Find(&users).Error; err != nil {
			return models.Vault{}, models.User{}, nil, err
		}
	}
	usernames := map[uint]string{}
	for _, user := range users {
		usernames[user.ID] = user.Username
	}
	rows := make([]adminVaultMemberRow, 0, len(members))
	for _, member := range members {
		rows = append(rows, adminVaultMemberRow{UserID: member.UserID, Username: usernames[member.UserID], Role: member.Role})
	}
	return vault, owner, rows, nil
}

func (h *Handler) redirectVaultMembers(c *gin.Context, vaultID, message string) {
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vaultID+"?error="+url.QueryEscape(message))
}

func (h *Handler) backupRows() ([]adminBackupRow, error) {
	var backups []models.VaultBackup
	if err := h.DB.Order("created_at desc").Find(&backups).Error; err != nil {
		return nil, err
	}
	ownerIDs := make([]uint, 0, len(backups))
	for _, backup := range backups {
		ownerIDs = append(ownerIDs, backup.OwnerID)
	}
	var users []models.User
	if len(ownerIDs) > 0 {
		if err := h.DB.Unscoped().Where("id IN ?", ownerIDs).Find(&users).Error; err != nil {
			return nil, err
		}
	}
	owners := map[uint]string{}
	for _, user := range users {
		owners[user.ID] = user.Username
	}
	rows := make([]adminBackupRow, 0, len(backups))
	for _, backup := range backups {
		rows = append(rows, adminBackupRow{ID: backup.ID, VaultName: backup.VaultName, Owner: owners[backup.OwnerID], FileName: backup.FileName, Size: formatBytes(backup.Size), CreatedAt: backup.CreatedAt.Local().Format("2006-01-02 15:04")})
	}
	return rows, nil
}

func (h *Handler) downloadBackup(c *gin.Context) {
	var backup models.VaultBackup
	if err := h.DB.Where("id = ?", c.Param("id")).First(&backup).Error; err != nil {
		c.String(http.StatusNotFound, "backup not found")
		return
	}
	path, err := vaultbackup.Path(backup.FileName)
	if err != nil {
		c.String(http.StatusNotFound, "backup not found")
		return
	}
	if _, err := os.Stat(path); err != nil {
		c.String(http.StatusNotFound, "backup archive is missing")
		return
	}
	c.FileAttachment(path, filepath.Base(path))
}

func (h *Handler) deleteBackup(c *gin.Context) {
	var backup models.VaultBackup
	if err := h.DB.Where("id = ?", c.Param("id")).First(&backup).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	path, err := vaultbackup.Path(backup.FileName)
	if err == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			c.String(http.StatusInternalServerError, "failed to delete backup archive")
			return
		}
	}
	if err := h.DB.Delete(&backup).Error; err != nil {
		c.String(http.StatusInternalServerError, "failed to delete backup record")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin?saved=1")
}

func formatBytes(size int64) string {
	const mib = 1024 * 1024
	if size >= mib {
		return fmt.Sprintf("%.1f MiB", float64(size)/mib)
	}
	if size >= 1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%d B", size)
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
