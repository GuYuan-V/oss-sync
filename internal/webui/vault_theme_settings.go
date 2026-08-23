// 主题设置
package webui

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

type themeSettingRowView struct {
	Values map[string]string
}

type themeSettingFieldView struct {
	Schema blog.ThemeSettingField
	Value  string
	Rows   []themeSettingRowView
}

type themeSettingsData struct {
	VaultID       string
	VaultName     string
	ThemeName     string
	SettingsLabel string
	Fields        []themeSettingFieldView
	Error         string
	Saved         bool
}

// themeSettingsLabel 返回模板名本身；"设置" 后缀由模板通过 common.settings 键按语言渲染。
func themeSettingsLabel(themeName string) string {
	return themeName
}

func (h *Handler) themeSettingsPage(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	setting, err := h.loadVaultThemeSetting(vault.ID)
	if err != nil {
		h.renderThemeSettingsError(c, vault, h.t(c, "err.load_theme_settings_failed"))
		return
	}
	d := themeSettingsData{
		VaultID: vault.ID, VaultName: vault.Name, ThemeName: setting.ThemeName,
		SettingsLabel: themeSettingsLabel(setting.ThemeName),
		Error:         c.Query("error"), Saved: c.Query("saved") == "1",
	}
	fields, err := blog.ThemeSettings(h.Cfg.Storage.DataDir, setting.ThemeName)
	if err != nil {
		d.Error = h.t(c, "err.theme_settings_invalid", err.Error())
	} else {
		d.Fields = buildThemeSettingViews(fields, setting.ThemeConfig)
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-theme-settings", h.t(c, "page.vault_theme_settings", vault.Name, d.SettingsLabel), d)
}

func (h *Handler) saveThemeSettings(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	redirectPath := "/dashboard/vaults/" + vault.ID + "/theme-settings"
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, redirectPath+"?error="+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	setting, err := h.loadVaultThemeSetting(vault.ID)
	if err != nil {
		c.Redirect(http.StatusSeeOther, redirectPath+"?error="+url.QueryEscape(h.t(c, "err.load_theme_settings_failed")))
		return
	}
	fields, err := blog.ThemeSettings(h.Cfg.Storage.DataDir, setting.ThemeName)
	if err != nil {
		c.Redirect(http.StatusSeeOther, redirectPath+"?error="+url.QueryEscape(h.t(c, "err.theme_settings_declaration_invalid")))
		return
	}
	if len(fields) == 0 {
		c.Redirect(http.StatusSeeOther, redirectPath+"?error="+url.QueryEscape(h.t(c, "err.theme_no_configurable_fields")))
		return
	}
	raw := themeConfigFromForm(c, fields)
	clean, err := blog.ValidateThemeConfig(fields, raw)
	if err != nil {
		d := themeSettingsData{
			VaultID:       vault.ID,
			VaultName:     vault.Name,
			ThemeName:     setting.ThemeName,
			SettingsLabel: themeSettingsLabel(setting.ThemeName),
			Fields:        buildThemeSettingViews(fields, models.JSONMap(raw)),
			Error:         err.Error(),
		}
		ld := layoutData{}
		h.setVaultLayout(&ld, vault)
		h.renderVaultStatus(c, http.StatusBadRequest, ld, "vault-theme-settings", h.t(c, "page.vault_theme_settings", vault.Name, d.SettingsLabel), d)
		return
	}
	if err := h.DB.Model(&setting).Update("theme_config", models.JSONMap(clean)).Error; err != nil {
		c.Redirect(http.StatusSeeOther, redirectPath+"?error="+url.QueryEscape(h.t(c, "err.save_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, redirectPath+"?saved=1")
}

func (h *Handler) loadVaultThemeSetting(vaultID string) (models.VaultSetting, error) {
	var setting models.VaultSetting
	err := h.DB.Where("vault_id = ?", vaultID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.VaultSetting{
			VaultID: vaultID, ThemeName: "default", ThemeConfig: models.JSONMap{},
		}, nil
	}
	if err != nil {
		return models.VaultSetting{}, err
	}
	if setting.ThemeName == "" {
		setting.ThemeName = "default"
	}
	return setting, nil
}

func (h *Handler) renderThemeSettingsError(c *gin.Context, vault models.Vault, message string) {
	d := themeSettingsData{
		VaultID: vault.ID, VaultName: vault.Name, ThemeName: "default",
		SettingsLabel: "default", Error: message,
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-theme-settings", h.t(c, "page.vault_theme_settings", vault.Name, "default"), d)
}

func buildThemeSettingViews(fields []blog.ThemeSettingField, config models.JSONMap) []themeSettingFieldView {
	views := make([]themeSettingFieldView, 0, len(fields))
	for _, field := range fields {
		view := themeSettingFieldView{Schema: field}
		if field.Type == "group" {
			view.Rows = buildThemeSettingRows(field, config[field.Key])
		} else if value, ok := config[field.Key].(string); ok {
			view.Value = value
		}
		views = append(views, view)
	}
	return views
}

func buildThemeSettingRows(field blog.ThemeSettingField, raw any) []themeSettingRowView {
	rows := make([]themeSettingRowView, 0, field.MaxItems)
	if stored, ok := raw.([]any); ok {
		for _, item := range stored {
			values := make(map[string]string, len(field.Fields))
			if entry, ok := item.(map[string]any); ok {
				for _, child := range field.Fields {
					if value, ok := entry[child.Key].(string); ok {
						values[child.Key] = value
					}
				}
			}
			rows = append(rows, themeSettingRowView{Values: values})
			if len(rows) == field.MaxItems {
				break
			}
		}
	}
	return rows
}

func themeConfigFromForm(c *gin.Context, fields []blog.ThemeSettingField) map[string]any {
	raw := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Type != "group" {
			raw[field.Key] = c.PostForm("setting_" + field.Key)
			continue
		}
		columns := make(map[string][]string, len(field.Fields))
		rowCount := 0
		for _, child := range field.Fields {
			values := c.PostFormArray(fmt.Sprintf("group_%s_%s", field.Key, child.Key))
			columns[child.Key] = values
			if len(values) > rowCount {
				rowCount = len(values)
			}
		}
		if rowCount > field.MaxItems {
			rowCount = field.MaxItems
		}
		rows := make([]any, 0, rowCount)
		for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
			row := make(map[string]any, len(field.Fields))
			for _, child := range field.Fields {
				values := columns[child.Key]
				if rowIndex < len(values) {
					row[child.Key] = values[rowIndex]
				}
			}
			rows = append(rows, row)
		}
		raw[field.Key] = rows
	}
	return raw
}

