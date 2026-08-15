<<<<<<< HEAD
// Package webui 提供统一登录、注册和带侧边栏的网页控制台。
package webui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
=======
// Package webui 提供公开注册页和管理员控制台。
package webui

import (
	"embed"
	"errors"
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
<<<<<<< HEAD
=======
	"path/filepath"
	"strconv"
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
<<<<<<< HEAD
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// sessionCookie 是登录后网页会话的 HttpOnly cookie。
const sessionCookie = "oss_web_session"

// csrfCookie 是 double-submit CSRF token cookie（非 HttpOnly，供 JS 读取）。
const csrfCookie = "oss_csrf"

//go:embed templates/*.html templates/partials/*.html assets/*
var webFS embed.FS

// Handler 持有控制台依赖。
=======
	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultbackup"
)

const adminCookieName = "oss_admin_session"

//go:embed templates/*.html assets/*
var webFS embed.FS

>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
type Handler struct {
	DB            *gorm.DB
	Cfg           *config.Config
	tpl           *template.Template
	loginLimit    *auth.AttemptLimiter
	registerLimit *auth.AttemptLimiter
}

<<<<<<< HEAD
// layoutData 是所有控制台页面共用的外壳数据。
type layoutData struct {
	Page             string // 要渲染的页面模板名，如 "overview"
	Title            string
	Username         string
	IsAdmin          bool
	CSRF             string
	ShowSidebar      bool // 登录/注册页为 false
	ActiveGroup      string
	ActivePage       string
	CurrentVault     *vaultNav // 进入仓库页后为当前仓库导航
	Flash            string
	FlashKind        string // success / error
	ConsoleThemeName string
	Language         string
	ContentHTML      template.HTML
}

func (ld layoutData) T(key string, args ...any) string {
	return translate(ld.Language, key, args...)
}

// vaultNav 侧边栏"当前仓库"菜单的上下文。
type vaultNav struct {
	ID                 string
	Name               string
	HasThemeSettings   bool
	ThemeSettingsLabel string
}

func New(db *gorm.DB, cfg *config.Config) (*Handler, error) {
	funcs := template.FuncMap{
		"formatBytes": formatBytes,
		"timeFmt": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04")
		},
		"sub":      func(a, b int) int { return a - b },
		"urlquery": url.QueryEscape,
	}
	tpl, err := template.New("web").Funcs(funcs).ParseFS(webFS,
		"templates/layout.html",
		"templates/partials/*.html",
		"templates/*.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse web UI templates: %w", err)
	}
	return &Handler{
		DB: db, Cfg: cfg, tpl: tpl,
		loginLimit:    auth.NewAttemptLimiter(8, time.Minute),
		registerLimit: auth.NewAttemptLimiter(5, time.Minute),
	}, nil
}

// Register 注册公开页面、登录会话和受保护的控制台路由。
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/ui/assets/console.css", h.styles)
	r.GET("/ui/assets/app.js", h.script("app.js", "text/javascript; charset=utf-8"))
	r.GET("/ui/assets/metrics.js", h.script("metrics.js", "text/javascript; charset=utf-8"))
	r.GET("/ui/assets/theme.js", h.script("theme.js", "text/javascript; charset=utf-8"))
	r.GET("/ui/themes/:theme/*filepath", h.consoleThemeAsset)

	// 登录、注册、登出（公开）。
	r.GET("/login", h.loginPage)
	r.POST("/login", h.loginSubmit)
	r.GET("/register", h.registerPage)
	r.POST("/register", h.registerSubmit)
	r.POST("/logout", h.logout)

	// 受保护的控制台。
	console := r.Group("/dashboard", h.requireSession)
	{
		console.GET("", h.overviewPage)
		console.GET("/metrics", h.systemMetricsPage)
		console.GET("/vaults", h.vaultsPage)
		console.POST("/vaults", h.createVault)
		console.GET("/vaults/new", h.newVaultPage)
		console.GET("/vaults/:vault_id", h.vaultFilesPage)
		console.POST("/vaults/:vault_id/files/delete", h.deleteFile)
		console.GET("/vaults/:vault_id/files/preview", h.previewMarkdownFile)
		console.GET("/vaults/:vault_id/files/download", h.downloadFile)
		console.GET("/vaults/:vault_id/shares", h.sharesPage)
		console.POST("/vaults/:vault_id/shares", h.createShare)
		console.POST("/vaults/:vault_id/shares/:share_id/allow_copy", h.toggleShareCopy)
		console.POST("/vaults/:vault_id/shares/:share_id/delete", h.deleteShare)
		console.GET("/vaults/:vault_id/recycle", h.recyclePage)
		console.POST("/vaults/:vault_id/recycle/:file_id/restore", h.restoreRecycle)
		console.POST("/vaults/:vault_id/recycle/:file_id/delete", h.purgeRecycle)
		console.GET("/vaults/:vault_id/history", h.historyPage)
		console.GET("/vaults/:vault_id/history/:history_id", h.historyDetailPage)
		console.POST("/vaults/:vault_id/history/:history_id/restore", h.restoreHistory)
		console.GET("/vaults/:vault_id/members", h.membersPage)
		console.POST("/vaults/:vault_id/members", h.addMember)
		console.POST("/vaults/:vault_id/members/:user_id/role", h.updateMemberRole)
		console.POST("/vaults/:vault_id/members/:user_id/delete", h.removeMember)
		console.POST("/vaults/:vault_id/members/:user_id/collaborations/revoke", h.revokeMemberCollaborations)
		console.GET("/vaults/:vault_id/settings", h.vaultSettingsPage)
		console.POST("/vaults/:vault_id/settings", h.saveVaultSettings)
		console.GET("/vaults/:vault_id/theme-settings", h.themeSettingsPage)
		console.POST("/vaults/:vault_id/theme-settings", h.saveThemeSettings)
		console.GET("/vaults/:vault_id/papertrail", func(c *gin.Context) {
			c.Redirect(http.StatusMovedPermanently, "/dashboard/vaults/"+url.PathEscape(c.Param("vault_id"))+"/theme-settings")
		})
		console.POST("/vaults/:vault_id/delete", h.deleteVault)
		console.GET("/devices", h.devicesPage)
		console.POST("/devices/:client_id/approve", h.approveDevice)
		console.POST("/devices/:client_id/rename", h.renameDevice)
		console.POST("/devices/:client_id/authorize", h.authorizeDevice)
		console.POST("/devices/:client_id/revoke", h.revokeDevice)
		console.GET("/account", h.accountPage)
		console.POST("/account/settings", h.saveAccountSettings)
		console.POST("/account/language", h.saveAccountLanguage)
		console.POST("/account/theme", h.saveConsoleTheme)
		console.POST("/account/password", h.changePassword)
	}

	// 管理员控制台。
	adminGroup := console.Group("/admin", h.requireAdmin)
	{
		adminGroup.GET("", h.adminUsersPage)
		adminGroup.POST("/users/:user_id/role", h.adminSetUserRole)
		adminGroup.POST("/users/:user_id/reset-password", h.adminResetPassword)
		adminGroup.POST("/users/:user_id/delete", h.adminDeleteUser)
		adminGroup.GET("/vaults", h.adminVaultsPage)
		adminGroup.GET("/vaults/:vault_id", h.adminVaultDetailPage)
		adminGroup.GET("/devices", h.adminDevicesPage)
		adminGroup.POST("/devices/:client_id/authorize", h.adminAuthorizeDevice)
		adminGroup.POST("/devices/:client_id/revoke", h.adminRevokeDevice)
		adminGroup.GET("/system", h.adminSystemPage)
		adminGroup.POST("/system", h.adminSaveSystem)
		adminGroup.GET("/data", h.adminDataPage)
		adminGroup.POST("/system/database", h.adminSaveDatabase)
		adminGroup.GET("/themes", h.adminThemesPage)
		adminGroup.POST("/themes/upload", h.adminThemeUpload)
		adminGroup.POST("/themes/scaffold", h.adminThemeScaffold)
		adminGroup.GET("/themes/:name/download", h.adminThemeDownload)
		adminGroup.POST("/themes/:name/delete", h.adminThemeDelete)
		adminGroup.POST("/themes/:name/files/save", h.adminThemeFileSave)
		adminGroup.GET("/console-themes", h.adminConsoleThemesPage)
		adminGroup.POST("/console-themes/upload", h.adminConsoleThemeUpload)
		adminGroup.POST("/console-themes/scaffold", h.adminConsoleThemeScaffold)
		adminGroup.GET("/console-themes/:name/download", h.adminConsoleThemeDownload)
		adminGroup.POST("/console-themes/:name/files/save", h.adminConsoleThemeFileSave)
		adminGroup.POST("/console-themes/:name/delete", h.adminConsoleThemeDelete)
		adminGroup.GET("/backups/:id/download", h.downloadBackup)
		adminGroup.POST("/backups/:id/delete", h.deleteBackup)
	}

	// 旧 /admin 路由重定向兼容，不再提供服务页面。
	r.GET("/admin/login", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/login")
	})
	r.GET("/admin", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/dashboard/admin")
	})
	r.GET("/admin/vaults/:vault_id", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/dashboard/vaults/"+url.PathEscape(c.Param("vault_id")))
	})
	r.POST("/admin/login", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/login")
	})
	r.POST("/admin/logout", h.logout)
}

// 会话

func (h *Handler) sessionUser(c *gin.Context) *models.User {
	token, err := c.Cookie(sessionCookie)
=======
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
	ThemeName     string
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
		adminGroup.POST("/vaults/:vault_id/theme", h.updateVaultTheme)
		adminGroup.POST("/vaults/:vault_id/theme/development", h.createDevelopmentTheme)
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
	themeName, err := h.vaultThemeName(vault.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load vault theme")
		return
	}
	h.render(c, "vault_members.html", vaultMemberView{
		AdminUsername: current.Username, VaultID: vault.ID, VaultName: vault.Name,
		Owner: owner.Username, ThemeName: themeName, Members: members, Saved: c.Query("saved") == "1", Error: c.Query("error"),
	})
}

func (h *Handler) updateVaultTheme(c *gin.Context) {
	vault, _, _, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	themeName := strings.TrimSpace(c.PostForm("theme_name"))
	if err := h.validateSelectableTheme(themeName); err != nil {
		h.redirectVaultMembers(c, vault.ID, err.Error())
		return
	}
	if err := h.saveVaultTheme(vault, themeName); err != nil {
		h.redirectVaultMembers(c, vault.ID, "保存博客主题失败")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vault.ID+"?saved=1")
}

func (h *Handler) createDevelopmentTheme(c *gin.Context) {
	vault, _, _, err := h.loadVaultMembers(c.Param("vault_id"))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin")
		return
	}
	themeName := strings.TrimSpace(c.PostForm("theme_name"))
	if _, err := blog.CreateDevelopmentTheme(h.Cfg.Storage.DataDir, themeName); err != nil {
		h.redirectVaultMembers(c, vault.ID, err.Error())
		return
	}
	if err := h.saveVaultTheme(vault, themeName); err != nil {
		h.redirectVaultMembers(c, vault.ID, "开发模板已创建，但未能启用；请在主题选择中手动启用")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/vaults/"+vault.ID+"?saved=1")
}

func (h *Handler) validateSelectableTheme(themeName string) error {
	if themeName == "default" {
		return nil
	}
	if err := blog.ValidateThemeName(themeName); err != nil {
		return err
	}
	if !blog.CustomThemeExists(h.Cfg.Storage.DataDir, themeName) {
		return errors.New("未找到该主题的 template.html；请先创建开发模板或放入完整主题目录")
	}
	return nil
}

func (h *Handler) vaultThemeName(vaultID string) (string, error) {
	var setting models.VaultSetting
	err := h.DB.Where("vault_id = ?", vaultID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "default", nil
	}
	if err != nil {
		return "", err
	}
	if setting.ThemeName == "" {
		return "default", nil
	}
	return setting.ThemeName, nil
}

func (h *Handler) saveVaultTheme(vault models.Vault, themeName string) error {
	var setting models.VaultSetting
	err := h.DB.Where("vault_id = ?", vault.ID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		setting = models.VaultSetting{VaultID: vault.ID, ThemeName: themeName, KeepDirectoryTree: true}
		// Preserve installations created before settings became Vault-scoped.
		var legacy models.UserSetting
		if err := h.DB.Where("user_id = ?", vault.OwnerID).First(&legacy).Error; err == nil {
			setting.ThemeConfig = legacy.ThemeConfig
			setting.CustomHeader = legacy.CustomHeader
			setting.CustomFooter = legacy.CustomFooter
			setting.KeepDirectoryTree = legacy.KeepDirectoryTree
		}
		return h.DB.Create(&setting).Error
	}
	if err != nil {
		return err
	}
	return h.DB.Model(&setting).Update("theme_name", themeName).Error
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
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if err != nil || token == "" {
		return nil
	}
	user, err := auth.AuthenticateToken(h.DB, h.Cfg, token)
<<<<<<< HEAD
	if err != nil {
=======
	if err != nil || user.Role != "admin" {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		return nil
	}
	return user
}

<<<<<<< HEAD
// setSessionCookie 设置登录会话 cookie 与 CSRF cookie。
func (h *Handler) setSessionCookie(c *gin.Context, user *models.User) {
	token, expiresIn, err := auth.IssueToken(h.Cfg, *user)
	if err != nil {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(expiresIn),
		HttpOnly: true,
		Secure:   requestIsHTTPS(c),
		SameSite: http.SameSiteLaxMode,
	})
	// CSRF token 与会话同生命周期。
	if _, err := c.Cookie(csrfCookie); err != nil {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     csrfCookie,
			Value:    randomToken(),
			Path:     "/",
			MaxAge:   int(expiresIn),
			HttpOnly: false,
			Secure:   requestIsHTTPS(c),
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(c.Writer, &http.Cookie{Name: csrfCookie, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode})
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// requireSession 要求已登录，并校验状态修改请求的 CSRF token。
func (h *Handler) requireSession(c *gin.Context) {
	user := h.sessionUser(c)
	if user == nil {
		requestedWebLanguage(c)
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		if !h.validCSRF(c) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
	c.Set("oss.web_user", user)
	c.Next()
}

func (h *Handler) requireAdmin(c *gin.Context) {
	user, _ := c.Get("oss.web_user")
	u, _ := user.(*models.User)
	if u == nil || u.Role != "admin" {
		c.Redirect(http.StatusSeeOther, "/dashboard")
		c.Abort()
		return
	}
	c.Next()
}

func (h *Handler) validCSRF(c *gin.Context) bool {
	expected, err := c.Cookie(csrfCookie)
	if err != nil || expected == "" {
		return false
	}
	got := c.PostForm("_csrf")
	if got == "" {
		got = c.GetHeader("X-CSRF-Token")
	}
	return got != "" && got == expected
}

func (h *Handler) webUser(c *gin.Context) *models.User {
	user, _ := c.Get("oss.web_user")
	u, _ := user.(*models.User)
	return u
}

func (h *Handler) userLang(c *gin.Context) string {
	if language := requestedWebLanguage(c); language != "" {
		return language
	}
	if u := h.webUser(c); u != nil {
		return h.selectedWebLanguage(u.ID)
	}
	return defaultWebLanguage
}

func requestedWebLanguage(c *gin.Context) string {
	return ""
}

func (h *Handler) t(c *gin.Context, key string, args ...any) string {
	return translate(h.userLang(c), key, args...)
}

// 渲染

// render 使用统一布局渲染控制台页面。page 为页面模板名。
func (h *Handler) render(c *gin.Context, status int, page, title string, activeGroup, activePage string, data any) {
	u := h.webUser(c)
	ld := layoutData{
		Page:        page,
		Title:       title,
		Username:    "",
		IsAdmin:     false,
		ShowSidebar: u != nil,
		ActiveGroup: activeGroup,
		ActivePage:  activePage,
	}
	if u != nil {
		ld.Username = u.Username
		ld.IsAdmin = u.Role == "admin"
		ld.ConsoleThemeName = h.selectedConsoleTheme(u.ID)
		ld.Language = h.userLang(c)
	}
	if token, err := c.Cookie(csrfCookie); err == nil {
		ld.CSRF = token
	}
	h.renderWithLayout(c, status, ld, data)
}

// renderWithLayout 渲染页面内容并把结果注入统一布局。
func (h *Handler) renderWithLayout(c *gin.Context, status int, ld layoutData, data any) {
	pageData := struct {
		Layout layoutData
		Data   any
	}{Layout: ld, Data: data}
	var buf strings.Builder
	if err := h.tpl.ExecuteTemplate(&buf, ld.Page, pageData); err != nil {
		fmt.Fprintf(os.Stderr, "webui template %s: %v\n", ld.Page, err)
		buf.Reset()
		if err := h.tpl.ExecuteTemplate(&buf, "overview", pageData); err != nil {
			ld.ContentHTML = template.HTML("<p>template error</p>")
		}
	}
	ld.ContentHTML = template.HTML(buf.String())
	setPageHeaders(c)
	c.Status(status)
	_ = h.tpl.ExecuteTemplate(c.Writer, "layout", struct {
		Layout layoutData
		Data   any
	}{Layout: ld, Data: data})
=======
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
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
}

func setPageHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header(
		"Content-Security-Policy",
<<<<<<< HEAD
		"default-src 'none'; connect-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https:; "+
=======
		"default-src 'none'; style-src 'self'; img-src 'self' data:; "+
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
	)
}

func requestIsHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil ||
		strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}
<<<<<<< HEAD

// 静态资源

func (h *Handler) styles(c *gin.Context) {
	raw, err := webFS.ReadFile("assets/console.css")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/css; charset=utf-8", raw)
}

func (h *Handler) script(name, contentType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := webFS.ReadFile("assets/" + name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, contentType, raw)
	}
}

// 登录与注册

type loginView struct {
	Error string
}

func (h *Handler) loginPage(c *gin.Context) {
	if h.sessionUser(c) != nil {
		c.Redirect(http.StatusFound, "/dashboard")
		return
	}
	h.renderAuth(c, http.StatusOK, "login", loginView{})
}

func (h *Handler) loginSubmit(c *gin.Context) {
	if !h.loginLimit.Allow("web-login:" + c.ClientIP()) {
		h.renderAuth(c, http.StatusTooManyRequests, "login", loginView{Error: h.t(c, "err.too_many_attempts")})
		return
	}
	user, err := auth.AuthenticateCredentials(h.DB, c.PostForm("username"), c.PostForm("password"))
	if err != nil {
		h.renderAuth(c, http.StatusUnauthorized, "login", loginView{Error: h.t(c, "err.invalid_credentials")})
		return
	}
	h.setSessionCookie(c, user)
	c.Redirect(http.StatusSeeOther, "/dashboard")
}

func (h *Handler) logout(c *gin.Context) {
	clearSessionCookie(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

// renderAuth 渲染登录/注册等无侧边栏页面。
func (h *Handler) renderAuth(c *gin.Context, status int, page string, data any) {
	language := requestedWebLanguage(c)
	if language == "" {
		language = defaultWebLanguage
	}
	ld := layoutData{Page: page, ShowSidebar: false, Language: language}
	if token, err := c.Cookie(csrfCookie); err == nil {
		ld.CSRF = token
	}
	h.renderWithLayout(c, status, ld, data)
}

// 网页文件操作

func formatBytes(size int64) string {
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if size >= gib {
		return fmt.Sprintf("%.1f GiB", float64(size)/gib)
	}
	if size >= mib {
		return fmt.Sprintf("%.1f MiB", float64(size)/mib)
	}
	if size >= 1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%d B", size)
}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
