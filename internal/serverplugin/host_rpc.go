package serverplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/helantianshen/oss-sync/internal/models"
)

func (m *Manager) hostCall(
	ctx context.Context,
	pluginID string,
	method string,
	params map[string]json.RawMessage,
) (any, error) {
	switch method {
	case "db.query":
		return m.hostQuery(ctx, params)
	case "db.exec":
		return m.hostExec(ctx, params)
	case "host.models":
		return hostModelNames(), nil
	case "host.model.list":
		return m.hostModelList(ctx, params)
	case "host.model.create":
		return m.hostModelCreate(ctx, params)
	case "host.model.update":
		return m.hostModelUpdate(ctx, params)
	case "host.model.delete":
		return m.hostModelDelete(ctx, params)
	case "host.vault.get":
		return modelWireResult(m.hostVaultGet(ctx, params))
	case "host.vault.create":
		return modelWireResult(m.hostVaultCreate(ctx, params))
	case "host.vault.update":
		return modelWireResult(m.hostVaultUpdate(ctx, params))
	case "host.vault.delete":
		return m.hostVaultDelete(ctx, params)
	case "host.file.get":
		return m.hostFileGet(ctx, params)
	case "host.share.create":
		return modelWireResult(m.hostShareCreate(ctx, params))
	case "host.share.update":
		return modelWireResult(m.hostShareUpdate(ctx, params))
	case "host.share.delete":
		return m.hostShareDelete(ctx, params)
	case "host.blog.get":
		return m.hostBlogGet(ctx, params)
	case "host.blog.filter":
		return m.hostBlogFilter(ctx, params)
	case "host.plugin.list":
		return m.hostPluginList(ctx)
	case "host.settings.get":
		return m.hostSettingsGet(ctx, pluginID, params)
	case "host.settings.set":
		return m.hostSettingsSet(ctx, pluginID, params)
	case "host.hook":
		return m.hostHook(ctx, params)
	default:
		return nil, fmt.Errorf("unsupported host method %q", method)
	}
}

func (m *Manager) hostModelCreate(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	model, err := requiredStringParam(params, "model")
	if err != nil {
		return nil, err
	}
	if !validModelName(model) {
		return nil, fmt.Errorf("invalid host model %q", model)
	}
	values := map[string]any{}
	if err := json.Unmarshal(params["values"], &values); err != nil {
		return nil, fmt.Errorf("decode model values: %w", err)
	}
	result := m.db.WithContext(ctx).Table(model).Create(values)
	if result.Error != nil {
		return nil, fmt.Errorf("create host model: %w", result.Error)
	}
	return map[string]int64{"rows_affected": result.RowsAffected}, nil
}

func (m *Manager) hostModelUpdate(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	model, err := requiredStringParam(params, "model")
	if err != nil {
		return nil, err
	}
	if !validModelName(model) {
		return nil, fmt.Errorf("invalid host model %q", model)
	}
	where := map[string]any{}
	values := map[string]any{}
	if err := json.Unmarshal(params["where"], &where); err != nil {
		return nil, fmt.Errorf("decode model where: %w", err)
	}
	if err := json.Unmarshal(params["values"], &values); err != nil {
		return nil, fmt.Errorf("decode model values: %w", err)
	}
	result := m.db.WithContext(ctx).Table(model).Where(where).Updates(values)
	if result.Error != nil {
		return nil, fmt.Errorf("update host model: %w", result.Error)
	}
	return map[string]int64{"rows_affected": result.RowsAffected}, nil
}

func (m *Manager) hostModelDelete(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	model, err := requiredStringParam(params, "model")
	if err != nil {
		return nil, err
	}
	if !validModelName(model) {
		return nil, fmt.Errorf("invalid host model %q", model)
	}
	where := map[string]any{}
	if err := json.Unmarshal(params["where"], &where); err != nil {
		return nil, fmt.Errorf("decode model where: %w", err)
	}
	result := m.db.WithContext(ctx).Table(model).Where(where).Delete(map[string]any{})
	if result.Error != nil {
		return nil, fmt.Errorf("delete host model: %w", result.Error)
	}
	return map[string]int64{"rows_affected": result.RowsAffected}, nil
}

func validModelName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (m *Manager) hostHook(ctx context.Context, params map[string]json.RawMessage) (any, error) {
	name, err := requiredStringParam(params, "name")
	if err != nil {
		return nil, err
	}
	var payload any
	if raw := params["payload"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode host hook payload: %w", err)
		}
	}
	return m.RunHook(ctx, name, payload)
}

func (m *Manager) hostQuery(ctx context.Context, params map[string]json.RawMessage) ([]map[string]any, error) {
	query, err := requiredStringParam(params, "query")
	if err != nil {
		return nil, err
	}
	args, err := optionalArgsParam(params)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0)
	if err := m.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("plugin database query: %w", err)
	}
	return rows, nil
}

func (m *Manager) hostExec(ctx context.Context, params map[string]json.RawMessage) (map[string]int64, error) {
	query, err := requiredStringParam(params, "query")
	if err != nil {
		return nil, err
	}
	args, err := optionalArgsParam(params)
	if err != nil {
		return nil, err
	}
	result := m.db.WithContext(ctx).Exec(query, args...)
	if result.Error != nil {
		return nil, fmt.Errorf("plugin database execute: %w", result.Error)
	}
	return map[string]int64{"rows_affected": result.RowsAffected}, nil
}

func (m *Manager) hostModelList(ctx context.Context, params map[string]json.RawMessage) (any, error) {
	name, err := requiredStringParam(params, "model")
	if err != nil {
		return nil, err
	}
	limit := 100
	if raw := params["limit"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &limit); err != nil || limit < 1 || limit > 10000 {
			return nil, errors.New("host model limit must be 1..10000")
		}
	}
	query := m.db.WithContext(ctx).Limit(limit)
	if raw := params["where"]; len(raw) > 0 {
		where := map[string]any{}
		if err := json.Unmarshal(raw, &where); err != nil {
			return nil, fmt.Errorf("decode host model filter: %w", err)
		}
		query = query.Where(where)
	}
	switch name {
	case "users":
		rows := make([]models.User, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	case "vaults":
		rows := make([]models.Vault, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	case "files":
		rows := make([]models.File, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	case "shares":
		rows := make([]models.Share, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	case "collaborations":
		rows := make([]models.Collaboration, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	case "devices":
		rows := make([]models.ClientDevice, 0)
		err := query.Find(&rows).Error
		return modelWireResult(rows, err)
	default:
		return nil, fmt.Errorf("unsupported host model %q", name)
	}
}

func (m *Manager) hostPluginList(ctx context.Context) ([]map[string]any, error) {
	rows := make([]models.ServerPlugin, 0)
	if err := m.db.WithContext(ctx).Order("id asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list host plugins: %w", err)
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"id": row.ID, "name": row.Name, "version": row.Version,
			"runtime": row.Runtime, "enabled": row.Enabled,
		})
	}
	return result, nil
}

func (m *Manager) hostSettingsGet(
	ctx context.Context,
	pluginID string,
	params map[string]json.RawMessage,
) (models.JSONMap, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	var setting models.VaultPluginSetting
	result := m.db.WithContext(ctx).
		Where("vault_id = ? AND plugin_id = ?", vaultID, pluginID).
		First(&setting)
	if result.Error != nil {
		return models.JSONMap{}, result.Error
	}
	return setting.Config, nil
}

func (m *Manager) hostSettingsSet(
	ctx context.Context,
	pluginID string,
	params map[string]json.RawMessage,
) (map[string]bool, error) {
	vaultID, err := requiredStringParam(params, "vault_id")
	if err != nil {
		return nil, err
	}
	config := models.JSONMap{}
	if err := json.Unmarshal(params["config"], &config); err != nil {
		return nil, fmt.Errorf("decode plugin settings: %w", err)
	}
	setting := models.VaultPluginSetting{VaultID: vaultID, PluginID: pluginID, Config: config}
	if err := m.db.WithContext(ctx).Save(&setting).Error; err != nil {
		return nil, fmt.Errorf("save plugin settings: %w", err)
	}
	return map[string]bool{"saved": true}, nil
}

func requiredStringParam(params map[string]json.RawMessage, name string) (string, error) {
	var value string
	if err := json.Unmarshal(params[name], &value); err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("host parameter %q must be a non-empty string", name)
	}
	return value, nil
}

func optionalArgsParam(params map[string]json.RawMessage) ([]any, error) {
	args := make([]any, 0)
	if raw := params["args"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("decode host query args: %w", err)
		}
	}
	return args, nil
}

func hostModelNames() []string {
	return []string{"users", "vaults", "files", "shares", "collaborations", "devices"}
}
