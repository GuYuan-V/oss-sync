package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

type pluginSettingVaultOption struct {
	ID   string
	Name string
}

// mergeSettingFields 按 key 合并两组设置字段
// base 为基准，override 只更新同名键；base 独有键与 override 新增键均保留
// 嵌套的 group 子字段递归按同一规则合并
func mergeSettingFields(base, override []blog.ThemeSettingField) []blog.ThemeSettingField {
	byKey := make(map[string]blog.ThemeSettingField, len(override))
	for _, field := range override {
		byKey[field.Key] = field
	}
	merged := make([]blog.ThemeSettingField, 0, len(base)+len(override))
	used := make(map[string]struct{}, len(base))
	for _, field := range base {
		used[field.Key] = struct{}{}
		if next, ok := byKey[field.Key]; ok {
			field = mergeSettingField(field, next)
		}
		merged = append(merged, field)
	}
	for _, field := range override {
		if _, ok := used[field.Key]; ok {
			continue
		}
		merged = append(merged, field)
	}
	return merged
}

// mergeSettingField 合并单个字段；override 的非零值覆盖 base，嵌套子字段递归合并
func mergeSettingField(base, override blog.ThemeSettingField) blog.ThemeSettingField {
	if override.Label != "" {
		base.Label = override.Label
	}
	if override.Type != "" {
		base.Type = override.Type
	}
	if override.Placeholder != "" {
		base.Placeholder = override.Placeholder
	}
	if override.MaxLength != 0 {
		base.MaxLength = override.MaxLength
	}
	if override.Required {
		base.Required = true
	}
	if override.MaxItems != 0 {
		base.MaxItems = override.MaxItems
	}
	if len(override.Choices) > 0 {
		base.Choices = override.Choices
	}
	if len(override.Fields) > 0 {
		base.Fields = mergeSettingFields(base.Fields, override.Fields)
	}
	if override.Section != "" {
		base.Section = override.Section
	}
	return base
}

// filterSettingsBySection 返回指定分组的设置字段；section 为空时返回全部字段
func filterSettingsBySection(fields []blog.ThemeSettingField, section string) []blog.ThemeSettingField {
	if section == "" {
		return fields
	}
	var filtered []blog.ThemeSettingField
	for _, field := range fields {
		if field.Section == section {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

type pluginSettingsData struct {
	VaultID       string
	VaultName     string
	PluginID      string
	PluginName    string
	PluginVersion string
	Fields        []pluginSettingFieldView
	// Vaults 是插件设置生效的仓库选择项
	Vaults []pluginSettingVaultOption
	// Section 是当前展示的设置分组名，空值表示全部
	Section string
	// NoVault 表示当前无满足插件生效条件的仓库
	NoVault bool
	Error   string
	Saved   bool
}

func (h *Handler) pluginSettingsPage(c *gin.Context) {
	if c.FullPath() == "/dashboard/vaults/:vault_id/plugins/:plugin_id/settings" {
		c.Redirect(http.StatusMovedPermanently, "/dashboard/plugins/"+url.PathEscape(c.Param("plugin_id"))+"/settings?vault_id="+url.QueryEscape(c.Param("vault_id")))
		return
	}
	u := h.webUser(c)
	pluginID := c.Param("plugin_id")
	vaultID, ok := h.pickPluginSettingVault(c, h.pluginSettingVaults(u, pluginID))
	if !ok {
		h.renderPluginSettingsNoVault(c, u, pluginID)
		return
	}
	setVaultParam(c, vaultID)
	vault, _, resolved := h.resolveVaultPage(c)
	if !resolved {
		return
	}
	manifest, config, err := h.loadPluginSettings(pluginID, vault.ID)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	pluginName := manifest.Name
	// section 参数限定当前展示的设置分组；缺省显示全部字段
	section := strings.TrimSpace(c.Query("section"))
	fields := manifest.Settings
	if section != "" {
		fields = filterSettingsBySection(manifest.Settings, section)
	}
	d := pluginSettingsData{
		VaultID:       vault.ID,
		VaultName:     vault.Name,
		PluginID:      manifest.ID,
		PluginName:    pluginName,
		PluginVersion: manifest.Version,
		Fields:        buildPluginSettingViews(fields, config),
		Vaults:        h.pluginSettingVaults(u, pluginID),
		Section:       section,
		Error:         c.Query("error"),
		Saved:         c.Query("saved") == "1",
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	ld.ActivePluginID = manifest.ID
	ld.ActiveSection = section
	h.renderVault(c, ld, "vault-plugin-settings", h.t(c, "page.plugin_settings", vault.Name, pluginName), d)
}

func (h *Handler) pluginSettingsGlobalPage(c *gin.Context) {
	h.pluginSettingsPage(c)
}

func (h *Handler) savePluginSettingsGlobal(c *gin.Context) {
	h.savePluginSettings(c)
}

// renderPluginSettingsNoVault 渲染无生效仓库时的引导页
func (h *Handler) renderPluginSettingsNoVault(c *gin.Context, u *models.User, pluginID string) {
	name := h.pluginDisplayName(pluginID)
	h.render(c, http.StatusOK, "vault-plugin-settings",
		h.t(c, "page.plugin_settings", "", name),
		"plugins", "vault-plugin-settings",
		pluginSettingsData{
			PluginID:   pluginID,
			PluginName: name,
			NoVault:    true,
			Error:      h.t(c, "vault.plugin_settings_no_vault"),
		})
}

func (h *Handler) pluginDisplayName(pluginID string) string {
	for _, manifest := range serverplugin.BuiltinManifests() {
		if manifest.ID == pluginID {
			return manifest.Name
		}
	}
	var record models.ServerPlugin
	if err := h.DB.Where("id = ?", pluginID).First(&record).Error; err == nil && record.Name != "" {
		return record.Name
	}
	return pluginID
}

// pluginSettingVaults 返回插件设置生效的仓库；关联插件按主题筛选
// 未关联插件对全部可访问仓库生效
func (h *Handler) pluginSettingVaults(u *models.User, pluginID string) []pluginSettingVaultOption {
	var vaults []vaultNav
	if u.Role == "admin" {
		var all []models.Vault
		if err := h.DB.Order("is_default desc, created_at asc").Find(&all).Error; err != nil {
			return nil
		}
		for _, vault := range all {
			vaults = append(vaults, vaultNav{ID: vault.ID, Name: vault.Name})
		}
	} else {
		vaults = h.accessibleVaults(u)
	}
	if len(vaults) == 0 {
		return nil
	}

	ids := make([]string, 0, len(vaults))
	for _, vault := range vaults {
		ids = append(ids, vault.ID)
	}
	var settings []models.VaultSetting
	if err := h.DB.Where("vault_id IN ?", ids).Find(&settings).Error; err != nil {
		return nil
	}
	themeByVault := make(map[string]string, len(settings))
	for _, setting := range settings {
		theme := setting.ThemeName
		if theme == "" {
			theme = "default"
		}
		themeByVault[setting.VaultID] = theme
	}

	for _, manifest := range serverplugin.BuiltinManifests() {
		if manifest.ID != pluginID {
			continue
		}
		out := make([]pluginSettingVaultOption, 0, len(vaults))
		for _, vault := range vaults {
			// 内置插件的设置只服务其绑定模板，避免选用其他模板的仓库出现无关设置项
			if themeByVault[vault.ID] == builtinThemeForBuiltinPlugin(pluginID) {
				out = append(out, pluginSettingVaultOption{ID: vault.ID, Name: vault.Name})
			}
		}
		return out
	}
	var plugin models.ServerPlugin
	if err := h.DB.Where("id = ? AND enabled = ?", pluginID, true).First(&plugin).Error; err == nil {
		manifest, parseErr := serverplugin.ParseManifest([]byte(plugin.ManifestJSON))
		if parseErr == nil && manifest.SettingsVisibleWhenUsed() && len(manifest.BlogThemes) > 0 {
			out := make([]pluginSettingVaultOption, 0, len(vaults))
			for _, vault := range vaults {
				if pluginBlogThemeInUse(manifest, themeByVault[vault.ID]) {
					out = append(out, pluginSettingVaultOption{ID: vault.ID, Name: vault.Name})
				}
			}
			return out
		}
	}

	var links []models.ServerPluginAssociation
	if err := h.DB.Where("plugin_id = ?", pluginID).Find(&links).Error; err != nil {
		return nil
	}
	allowed := make(map[string]bool, len(links))
	for _, link := range links {
		if link.Kind == "blog_theme" {
			allowed[link.TargetID] = true
		}
	}
	out := make([]pluginSettingVaultOption, 0, len(vaults))
	for _, vault := range vaults {
		if len(allowed) == 0 || allowed[themeByVault[vault.ID]] {
			out = append(out, pluginSettingVaultOption{ID: vault.ID, Name: vault.Name})
		}
	}
	return out
}

// pickPluginSettingVault 优先选择请求指定的仓库，否则选择第一个
func (h *Handler) pickPluginSettingVault(c *gin.Context, options []pluginSettingVaultOption) (string, bool) {
	if len(options) == 0 {
		return "", false
	}
	requested := c.Query("vault_id")
	if requested == "" {
		requested = c.PostForm("vault_id")
	}
	if requested == "" {
		requested = c.Param("vault_id")
	}
	for _, option := range options {
		if option.ID == requested {
			return option.ID, true
		}
	}
	return options[0].ID, true
}

// setVaultParam 使后续处理器使用指定 Vault ID
func setVaultParam(c *gin.Context, vaultID string) {
	for i, param := range c.Params {
		if param.Key == "vault_id" {
			c.Params[i].Value = vaultID
			return
		}
	}
	c.Params = append(c.Params, gin.Param{Key: "vault_id", Value: vaultID})
}

func (h *Handler) savePluginSettings(c *gin.Context) {
	u := h.webUser(c)
	pluginID := c.Param("plugin_id")
	vaultID, ok := h.pickPluginSettingVault(c, h.pluginSettingVaults(u, pluginID))
	if !ok {
		c.Redirect(http.StatusSeeOther, "/dashboard/plugins/"+url.PathEscape(pluginID)+
			"/settings?error="+url.QueryEscape(h.t(c, "vault.plugin_settings_no_vault")))
		return
	}
	setVaultParam(c, vaultID)
	vault, role, resolved := h.resolveVaultPage(c)
	if !resolved {
		return
	}
	// section 由表单携带，决定本次只接受并校验哪个分组的字段；
	// 其余分组沿用库里已有值，避免未展示的字段被空值覆盖
	section := strings.TrimSpace(c.PostForm("section"))
	redirect := "/dashboard/plugins/" + url.PathEscape(c.Param("plugin_id")) + "/settings?vault_id=" + url.QueryEscape(vault.ID)
	if section != "" {
		redirect += "&section=" + url.QueryEscape(section)
	}
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
	fields := manifest.Settings
	if section != "" {
		fields = filterSettingsBySection(manifest.Settings, section)
	}
	raw := pluginSettingsFromForm(c, fields)
	var existing models.JSONMap
	if _, prev, err := h.loadPluginSettings(manifest.ID, vault.ID); err == nil {
		existing = prev
	}
	// 未在本次提交中的字段保留原值，使校验与落库都只针对当前分区
	for key, value := range existing {
		if _, ok := raw[key]; !ok {
			raw[key] = value
		}
	}
	clean, err := blog.ValidateSettingConfig(manifest.Settings, raw)
	pluginName := manifest.Name
	if err != nil {
		d := pluginSettingsData{VaultID: vault.ID, VaultName: vault.Name, PluginID: manifest.ID, PluginName: pluginName, PluginVersion: manifest.Version, Fields: buildPluginSettingViews(fields, models.JSONMap(raw)), Section: section, Error: err.Error()}
		ld := layoutData{}
		h.setVaultLayout(&ld, vault)
		ld.ActivePluginID = manifest.ID
		ld.ActiveSection = section
		h.renderVaultStatus(c, http.StatusBadRequest, ld, "vault-plugin-settings", h.t(c, "page.plugin_settings", vault.Name, pluginName), d)
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var setting models.VaultPluginSetting
		query := tx.Where("vault_id = ? AND plugin_id = ?", vault.ID, manifest.ID).First(&setting)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			setting = models.VaultPluginSetting{VaultID: vault.ID, PluginID: manifest.ID, Config: models.JSONMap(clean)}
			if err := tx.Create(&setting).Error; err != nil {
				return err
			}
		} else if query.Error != nil {
			return query.Error
		} else if err := tx.Model(&setting).Update("config", models.JSONMap(clean)).Error; err != nil {
			return err
		}
		return h.syncBuiltinThemeConfig(tx, vault.ID, manifest.ID, clean)
	})
	if err != nil {
		c.Redirect(http.StatusSeeOther, errorRedirect+url.QueryEscape(h.t(c, "err.save_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, redirect+"&saved=1")
}

// syncBuiltinThemeConfig 把内置主题的公开配置同步到 VaultSetting.ThemeConfig
// 公开博客首页与 /b/<vault> 都以该字段为 base，内置主题没有 theme.json 白名单，
// 需要显式同步才能让配置生效；按声明键定向合并，不清掉其他主题写入的配置
func (h *Handler) syncBuiltinThemeConfig(tx *gorm.DB, vaultID, pluginID string, clean models.JSONMap) error {
	if !isBuiltinThemeSettingsPlugin(pluginID) {
		return nil
	}
	var themeSetting models.VaultSetting
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("vault_id = ?", vaultID).First(&themeSetting).Error; err != nil {
		return err
	}
	merged := make(models.JSONMap, len(themeSetting.ThemeConfig)+len(clean))
	for key, value := range themeSetting.ThemeConfig {
		merged[key] = value
	}
	for key, value := range clean {
		merged[key] = value
	}
	return tx.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).
		Update("theme_config", merged).Error
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
			// 按 key 合并：manifest 声明为基准，registration 只覆盖同名键
			// 整体替换会让 Go 侧漏声明 Choices 等字段时把下拉框退化成输入框
			manifest.Settings = mergeSettingFields(manifest.Settings, registration.Settings)
		}
	}
	if !h.pluginSettingsLinkedToVault(manifest, vaultID) {
		return serverplugin.Manifest{}, nil, errors.New("plugin settings are not active for this Vault")
	}
	return h.loadPluginSettingValues(manifest, vaultID)
}

// builtinPluginThemes 内置设置插件绑定的博客模板
var builtinPluginThemes = map[string]string{
	"papertrail-settings":   "papertrail",
	"sakura-settings":       "sakura",
	"glass-settings":        "glass",
	"starry-night-settings": "starry-night",
}

// builtinThemeForBuiltinPlugin 返回内置插件绑定的博客模板；未知内置插件返回空字符串
func builtinThemeForBuiltinPlugin(pluginID string) string {
	return builtinPluginThemes[pluginID]
}

// isBuiltinThemeSettingsPlugin 判断是否为内置主题的设置插件
// 这类插件的配置必须同步到 VaultSetting.ThemeConfig，公开博客才能读到
func isBuiltinThemeSettingsPlugin(pluginID string) bool {
	return builtinThemeForBuiltinPlugin(pluginID) != ""
}

func (h *Handler) pluginSettingsLinkedToVault(manifest serverplugin.Manifest, vaultID string) bool {
	if theme := builtinThemeForBuiltinPlugin(manifest.ID); theme != "" {
		var setting models.VaultSetting
		return h.DB.Where("vault_id = ?", vaultID).First(&setting).Error == nil &&
			(setting.ThemeName == theme || (setting.ThemeName == "" && theme == "default"))
	}
	if manifest.SettingsVisibleWhenUsed() && len(manifest.BlogThemes) > 0 {
		var setting models.VaultSetting
		return h.DB.Where("vault_id = ?", vaultID).First(&setting).Error == nil &&
			pluginBlogThemeInUse(manifest, setting.ThemeName)
	}
	var count int64
	if err := h.DB.Model(&models.ServerPluginAssociation{}).Where("plugin_id = ? AND kind = ? AND target_id = (SELECT theme_name FROM vault_settings WHERE vault_id = ?)", manifest.ID, "blog_theme", vaultID).Count(&count).Error; err != nil {
		return false
	}
	return count > 0 || pluginHasNoAssociations(h.DB, manifest.ID)
}

func pluginBlogThemeInUse(manifest serverplugin.Manifest, selected string) bool {
	for _, theme := range manifest.BlogThemes {
		if selected == theme.Key(manifest.ID) || selected == theme.Name || selected == theme.ID {
			return true
		}
	}
	return false
}

func (h *Handler) loadPluginSettingValues(manifest serverplugin.Manifest, vaultID string) (serverplugin.Manifest, models.JSONMap, error) {
	var setting models.VaultPluginSetting
	err := h.DB.Where("vault_id = ? AND plugin_id = ?", vaultID, manifest.ID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if isBuiltinThemeSettingsPlugin(manifest.ID) {
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
