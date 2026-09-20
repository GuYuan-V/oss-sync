package serverplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
	"gorm.io/gorm"
)

// RunHook 按名称依次触发已注册的动作与过滤器。
func (m *Manager) RunHook(ctx context.Context, name string, payload any) (any, error) {
	if !validExtensionName(name) {
		return nil, fmt.Errorf("invalid plugin hook %q", name)
	}
	current := payload
	for _, registration := range m.matchingHooks(name) {
		encoded, err := json.Marshal(current)
		if err != nil {
			return nil, fmt.Errorf("encode hook %s payload: %w", name, err)
		}
		response, err := m.invokeCallback(ctx, registration.PluginID, hookCallback(registration.Value.Hooks, name), PluginRequest{
			Method: "HOOK",
			Path:   "/hooks/" + name,
			Hook:   name,
			Payload: map[string]any{
				"value": json.RawMessage(encoded),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("plugin %s hook %s: %w", registration.PluginID, name, err)
		}
		hook, _ := m.registeredHook(registration.PluginID, name)
		if hook.Kind == "action" {
			continue
		}
		body, err := decodePluginBody(response)
		if err != nil {
			return nil, err
		}
		var next any
		if err := json.Unmarshal(body, &next); err != nil {
			return nil, fmt.Errorf("decode plugin %s hook %s result: %w", registration.PluginID, name, err)
		}
		current = next
	}
	return current, nil
}

// ApplyHook 沿用博客宿主原有契约，底层改用动态注册实现。
func (m *Manager) ApplyHook(ctx context.Context, hook string, payload blog.PluginHookPayload) (string, error) {
	registrations := m.matchingHooks(hook)
	if len(registrations) == 0 {
		return payload.Content, nil
	}
	content := payload.Content
	for _, registration := range registrations {
		response, err := m.invokeCallback(ctx, registration.PluginID, hookCallback(registration.Value.Hooks, hook), PluginRequest{
			Method: "HOOK",
			Path:   "/hooks/" + hook,
			Hook:   hook,
			Payload: map[string]any{
				"vault_id": payload.VaultID,
				"theme":    payload.Theme,
				"content":  content,
				"metadata": payload.Metadata,
			},
			Settings: pluginSettings(m.db, payload.VaultID, registration.PluginID),
		})
		if err != nil {
			return "", fmt.Errorf("plugin %s hook %s: %w", registration.PluginID, hook, err)
		}
		hookSpec, _ := m.registeredHook(registration.PluginID, hook)
		if hookSpec.Kind == "action" {
			continue
		}
		body, err := decodePluginBody(response)
		if err != nil {
			return "", err
		}
		content = string(body)
	}
	return content, nil
}

// RenderAdminPage 调用指定插件管理页注册的回调。
func (m *Manager) RenderAdminPage(ctx context.Context, pluginID, slug string) (string, error) {
	registration, ok := m.RegistrationFor(pluginID)
	if !ok {
		return "", fmt.Errorf("plugin %s registration is unavailable", pluginID)
	}
	for _, page := range registration.AdminPages {
		if page.Slug != slug {
			continue
		}
		callback := page.Callback
		if callback == "" {
			callback = page.Slug
		}
		response, err := m.invokeCallback(ctx, pluginID, callback, PluginRequest{
			Method:  "GET",
			Path:    "/admin/pages/" + slug,
			Payload: map[string]any{"plugin_id": pluginID, "slug": slug},
		})
		if err != nil {
			return "", err
		}
		body, err := decodePluginBody(response)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
	return "", fmt.Errorf("plugin %s admin page %s is not registered", pluginID, slug)
}

func pluginSettings(db *gorm.DB, vaultID, pluginID string) map[string]any {
	settings := map[string]any{}
	if vaultID == "" {
		return settings
	}
	var setting models.VaultPluginSetting
	if err := db.Where("vault_id = ? AND plugin_id = ?", vaultID, pluginID).First(&setting).Error; err == nil && setting.Config != nil {
		return setting.Config
	}
	return settings
}

func hookResponse(value any) (PluginResponse, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return PluginResponse{}, err
	}
	return PluginResponse{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString(encoded)}, nil
}
