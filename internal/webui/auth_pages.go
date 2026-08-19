package webui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultbackup"
)

// registerView 注册页数据。
type registerView struct {
	RegistrationEnabled bool
	Username            string
	Error               string
	Success             bool
	SuccessMessage      string
}

func (h *Handler) registerPage(c *gin.Context) {
	enabled, err := auth.RegistrationEnabled(h.DB, h.Cfg.Auth.AllowAnonymousRegistration)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load registration settings")
		return
	}
	h.renderAuth(c, http.StatusOK, "register", registerView{RegistrationEnabled: enabled})
}

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
	role, err := auth.ResolveRegistrationRole(h.DB)
	if err != nil {
		view.Error = h.t(c, "err.admin_status_failed")
		h.renderAuth(c, http.StatusInternalServerError, "register", view)
		return
	}
	if _, err := auth.CreateAccount(h.DB, username, password, role); err != nil {
		view.Error = h.t(c, "err.username_taken")
		h.renderAuth(c, http.StatusConflict, "register", view)
		return
	}
	view.Success = true
	view.Username = username
	if role == "admin" {
		view.SuccessMessage = "已注册为首个管理员。"
	}
	h.renderAuth(c, http.StatusOK, "register", view)
}

// errorsIsNotFound 判断是否为 gorm.ErrRecordNotFound。
func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// vaultbackupPurge 在事务内删除仓库并生成备份。
func vaultbackupPurge(tx *gorm.DB, h *Handler, vault models.Vault) (models.VaultBackup, error) {
	return vaultbackup.PurgeWithTx(tx, h.Cfg.Storage.DataDir, vault)
}
