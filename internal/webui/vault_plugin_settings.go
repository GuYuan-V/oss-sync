package webui

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/serverplugin"
	"github.com/helantianshen/oss-sync/internal/vaultaccess"
)

type pluginSettingFieldView struct {
	Schema blog.ThemeSettingField
	Value  string
	Rows   []themeSettingRowView
}

type pluginSettingsData struct {
	VaultID       string
	VaultName     string
	PluginID      string
	PluginName    string
	PluginVersion string
	Fields        []pluginSettingFieldView
	Error         string
	Saved         bool
}

func (h *Handler) pluginSettingsPage(c *gin.Context) {
	if c.FullPath() == "/dashboard/vaults/:vault_id/plugins/:plugin_id/settings" {
		c.Redirect(http.StatusMovedPermanently, "/dashboard/plugins/"+url.PathEscape(c.Param("plugin_id"))+"/settings?vault_id="+url.QueryEscape(c.Param("vault_id")))
		return
	}
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	manifest, config, err := h.loadPluginSettings(c.Param("plugin_id"), vault.ID)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	d := pluginSettingsData{
		VaultID:       vault.ID,
		VaultName:     vault.Name,
		PluginID:      manifest.ID,
		PluginName:    manifest.Name,
		PluginVersion: manifest.Version,
		Fields:        buildPluginSettingViews(manifest.Settings, config),
		Error:         c.Query("error"),
		Saved:         c.Query("saved") == "1",
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	ld.ActivePluginID = manifest.ID
	h.renderVault(c, ld, "vault-plugin-settings", h.t(c, "page.plugin_settings", vault.Name, manifest.Name), d)
}

func (h *Handler) pluginSettingsGlobalPage(c *gin.Context) {
	if !h.setPluginSettingsVaultParam(c, c.Query("vault_id")) {
		c.Status(http.StatusNotFound)
		return
	}
	h.pluginSettingsPage(c)
}

func (h *Handler) savePluginSettingsGlobal(c *gin.Context) {
	if !h.setPluginSettingsVaultParam(c, c.PostForm("vault_id")) {
		c.Status(http.StatusNotFound)
		return
	}
	h.savePluginSettings(c)
}

func (h *Handler) setPluginSettingsVaultParam(c *gin.Context, requested string) bool {
	u := h.webUser(c)
	if requested != "" {
		if _, _, err := vaultaccess.Resolve(h.DB, u.ID, requested); err == nil {
			c.Params = append(c.Params, gin.Param{Key: "vault_id", Value: requested})
			return true
		}
		if u.Role == "admin" {
			var vault models.Vault
			if err := h.DB.Where("id = ?", requested).First(&vault).Error; err == nil {
				c.Params = append(c.Params, gin.Param{Key: "vault_id", Value: requested})
				return true
			}
		}
	}
	if c.Param("plugin_id") == "papertrail-settings" {
		var vaultIDs []string
		if u.Role == "admin" {
			if err := h.DB.Model(&models.Vault{}).Pluck("id", &vaultIDs).Error; err != nil {
				return false
			}
		} else {
			if err := h.DB.Model(&models.Vault{}).Where("owner_id = ?", u.ID).Pluck("id", &vaultIDs).Error; err != nil {
				return false
			}
			var memberIDs []string
			if err := h.DB.Model(&models.VaultMember{}).
				Where("user_id = ? AND role IN ?", u.ID, []string{vaultaccess.RoleManager, vaultaccess.RoleParticipant}).
				Pluck("vault_id", &memberIDs).Error; err != nil {
				return false
			}
			vaultIDs = append(vaultIDs, memberIDs...)
		}
		if len(vaultIDs) > 0 {
			var vault models.Vault
			if err := h.DB.Joins("JOIN vault_settings ON vault_settings.vault_id = vaults.id").
				Where("vaults.id IN ? AND vault_settings.theme_name = ?", vaultIDs, "papertrail").
				Order("vaults.is_default desc, vaults.created_at asc").First(&vault).Error; err == nil {
				c.Params = append(c.Params, gin.Param{Key: "vault_id", Value: vault.ID})
				return true
			}
		}
	}
	var vault models.Vault
	if _, _, err := vaultaccess.Resolve(h.DB, u.ID, requested); requested != "" && err == nil {
		vault, _, _ = vaultaccess.Resolve(h.DB, u.ID, requested)
	} else if err := h.DB.Where("owner_id = ?", u.ID).Order("is_default desc, created_at asc").First(&vault).Error; err != nil {
		return false
	}
	c.Params = append(c.Params, gin.Param{Key: "vault_id", Value: vault.ID})
	return true
}

func (h *Handler) savePluginSettings(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	redirect := "/dashboard/plugins/" + url.PathEscape(c.Param("plugin_id")) + "/settings?vault_id=" + url.QueryEscape(vault.ID)
	errorRedirect := redirect + "&error="
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	manifest, _, err := h.loadPluginSettings(c.Param("plugin_id"), vault.ID)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	raw := pluginSettingsFromForm(c, manifest.Settings)
	clean, err := blog.ValidateSettingConfig(manifest.Settings, raw)
	if err != nil {
		d := pluginSettingsData{VaultID: vault.ID, VaultName: vault.Name, PluginID: manifest.ID, PluginName: manifest.Name, PluginVersion: manifest.Version, Fields: buildPluginSettingViews(manifest.Settings, models.JSONMap(raw)), Error: err.Error()}
		ld := layoutData{}
		h.setVaultLayout(&ld, vault)
		ld.ActivePluginID = manifest.ID
		h.renderVaultStatus(c, http.StatusBadRequest, ld, "vault-plugin-settings", h.t(c, "page.plugin_settings", vault.Name, manifest.Name), d)
		return
	}
	var setting models.VaultPluginSetting
	query := h.DB.Where("vault_id = ? AND plugin_id = ?", vault.ID, manifest.ID).First(&setting)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		setting = models.VaultPluginSetting{VaultID: vault.ID, PluginID: manifest.ID, Config: models.JSONMap(clean)}
		if err := h.DB.Create(&setting).Error; err != nil {
			c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.save_failed")))
			return
		}
	} else if query.Error != nil {
		c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.save_failed")))
		return
	} else if err := h.DB.Model(&setting).Update("config", models.JSONMap(clean)).Error; err != nil {
		c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.save_failed")))
		return
	}
	if manifest.ID == "papertrail-settings" {
		if err := h.DB.Model(&models.VaultSetting{}).Where("vault_id = ?", vault.ID).Update("theme_config", models.JSONMap(clean)).Error; err != nil {
			c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.save_failed")))
			return
		}
	}
	c.Redirect(http.StatusSeeOther, redirect+"&saved=1")
}

func (h *Handler) loadPluginSettings(pluginID, vaultID string) (serverplugin.Manifest, models.JSONMap, error) {
	var record models.ServerPlugin
	if err := h.DB.Where("id = ? AND enabled = ?", pluginID, true).First(&record).Error; err != nil {
		for _, builtin := range serverplugin.BuiltinManifests() {
			if builtin.ID == pluginID {
				return h.loadPluginSettingValues(builtin, vaultID)
			}
		}
		return serverplugin.Manifest{}, nil, err
	}
	manifest, err := serverplugin.ParseManifest([]byte(record.ManifestJSON))
	if err != nil || len(manifest.Settings) == 0 {
		return serverplugin.Manifest{}, nil, errors.New("plugin settings unavailable")
	}
	if h.pluginManager != nil {
		if registration, ok := h.pluginManager.RegistrationFor(manifest.ID); ok && len(registration.Settings) > 0 {
			manifest.Settings = registration.Settings
		}
	}
	if !h.pluginSettingsLinkedToVault(manifest.ID, vaultID) {
		return serverplugin.Manifest{}, nil, errors.New("plugin settings are not active for this Vault")
	}
	return h.loadPluginSettingValues(manifest, vaultID)
}

func (h *Handler) pluginSettingsLinkedToVault(pluginID, vaultID string) bool {
	for _, builtin := range serverplugin.BuiltinManifests() {
		if builtin.ID == pluginID {
			var theme models.VaultSetting
			return h.DB.Where("vault_id = ?", vaultID).First(&theme).Error == nil && theme.ThemeName == "papertrail"
		}
	}
	var count int64
	if err := h.DB.Model(&models.ServerPluginAssociation{}).Where("plugin_id = ? AND kind = ? AND target_id = (SELECT theme_name FROM vault_settings WHERE vault_id = ?)", pluginID, "blog_theme", vaultID).Count(&count).Error; err != nil {
		return false
	}
	return count > 0 || pluginHasNoAssociations(h.DB, pluginID)
}

func (h *Handler) loadPluginSettingValues(manifest serverplugin.Manifest, vaultID string) (serverplugin.Manifest, models.JSONMap, error) {
	var setting models.VaultPluginSetting
	err := h.DB.Where("vault_id = ? AND plugin_id = ?", vaultID, manifest.ID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if manifest.ID == "papertrail-settings" {
			var themeSetting models.VaultSetting
			if themeErr := h.DB.Where("vault_id = ?", vaultID).First(&themeSetting).Error; themeErr == nil && themeSetting.ThemeConfig != nil {
				return manifest, themeSetting.ThemeConfig, nil
			}
		}
		return manifest, models.JSONMap{}, nil
	}
	if err != nil {
		return serverplugin.Manifest{}, nil, err
	}
	return manifest, setting.Config, nil
}

func buildPluginSettingViews(fields []blog.ThemeSettingField, config models.JSONMap) []pluginSettingFieldView {
	views := make([]pluginSettingFieldView, 0, len(fields))
	for _, field := range fields {
		view := pluginSettingFieldView{Schema: field}
		if field.Type == "group" {
			view.Rows = buildThemeSettingRows(field, config[field.Key])
		} else if value, ok := config[field.Key].(string); ok {
			view.Value = value
		}
		views = append(views, view)
	}
	return views
}

func pluginSettingsFromForm(c *gin.Context, fields []blog.ThemeSettingField) map[string]any {
	raw := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Type != "group" {
			raw[field.Key] = c.PostForm("setting_" + field.Key)
			continue
		}
		columns := make(map[string][]string, len(field.Fields))
		rowCount := 0
		for _, child := range field.Fields {
			values := c.PostFormArray("group_" + field.Key + "_" + child.Key)
			columns[child.Key] = values
			if len(values) > rowCount {
				rowCount = len(values)
			}
		}
		if rowCount > field.MaxItems {
			rowCount = field.MaxItems
		}
		rows := make([]any, 0, rowCount)
		for index := 0; index < rowCount; index++ {
			row := make(map[string]any, len(field.Fields))
			for _, child := range field.Fields {
				values := columns[child.Key]
				if index < len(values) {
					row[child.Key] = values[index]
				}
			}
			rows = append(rows, row)
		}
		raw[field.Key] = rows
	}
	return raw
}
