package webui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/vaultbackup"
)

// registerView 注册页数据
type registerView struct {
	RegistrationEnabled bool
	Username            string
	Error               string
	Success             bool
	SuccessMessage      string
}

// registerPage 渲染注册页：GET /register
// 注册开关读取失败时返回 500 而不渲染页面
func (h *Handler) registerPage(c *gin.Context) {
	enabled, err := auth.RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load registration settings")
		return
	}
	h.renderAuth(c, http.StatusOK, "register", registerView{RegistrationEnabled: enabled})
}

// registerSubmit 处理注册提交：POST /register
// 先按来源 IP 限流，再校验注册开关、两次密码一致性与账户输入规则
// 注册关闭返回 403，用户名冲突返回 409，首注账户成为 admin 并在成功页提示
func (h *Handler) registerSubmit(c *gin.Context) {
	if !h.registerLimit.Allow("web-register:" + c.ClientIP()) {
		h.renderAuth(c, http.StatusTooManyRequests, "register", registerView{Error: h.t(c, "err.too_many_attempts")})
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
		view.Error = h.t(c, "err.registration_closed")
		h.renderAuth(c, http.StatusForbidden, "register", view)
		return
	}
	password := c.PostForm("password")
	if password != c.PostForm("password_confirm") {
		view.Error = h.t(c, "err.password_mismatch")
		h.renderAuth(c, http.StatusBadRequest, "register", view)
		return
	}
	if err := auth.ValidateAccountInput(username, password); err != nil {
		view.Error = err.Error()
		h.renderAuth(c, http.StatusBadRequest, "register", view)
		return
	}
	// 原子化首注判定与创建，避免并发产生多个 admin（跨 web/API 入口）
	user, err := auth.CreateAccountForAnonymousRegistration(h.DB, username, password)
	if err != nil {
		if auth.IsUsernameTakenError(err) {
			view.Error = h.t(c, "err.username_taken")
			h.renderAuth(c, http.StatusConflict, "register", view)
		} else {
			view.Error = h.t(c, "err.admin_status_failed")
			h.renderAuth(c, http.StatusInternalServerError, "register", view)
		}
		return
	}
	view.Success = true
	view.Username = username
	if user.Role == "admin" {
		view.SuccessMessage = "已注册为首个管理员。"
	}
	h.renderAuth(c, http.StatusOK, "register", view)
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// vaultbackupPurge 在事务内删除仓库并生成备份
func vaultbackupPurge(tx *gorm.DB, h *Handler, vault models.Vault) (models.VaultBackup, error) {
	return vaultbackup.PurgeWithTx(tx, h.Cfg.Storage.DataDir, vault)
}
