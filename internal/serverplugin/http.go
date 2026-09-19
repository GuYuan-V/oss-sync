package serverplugin

import (
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/deviceauth"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/vaultaccess"
)

// RegisterRoutes mounts public and authenticated plugin namespaces.
func (m *Manager) RegisterRoutes(r *gin.Engine, cfg *config.Config) {
	r.GET("/api/plugin-capabilities", auth.Middleware(m.db, cfg), m.capabilities)
	r.POST("/api/plugin-hooks/:hook", auth.Middleware(m.db, cfg), m.applyHookHTTP)
	r.Any("/plugins/:plugin_id/*path", m.servePublic)
	authenticated := r.Group("/api/plugins", auth.Middleware(m.db, cfg))
	authenticated.Any("/:plugin_id/*path", m.serveAuthenticated)
	m.registerDynamicRoutes(r, cfg)
}

func (m *Manager) applyHookHTTP(c *gin.Context) {
	if c.Param("hook") != "editor.command" {
		c.Status(http.StatusNotFound)
		return
	}
	user, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	did, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader))
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxRequestBytes)
	var payload blog.PluginHookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plugin hook payload"})
		return
	}
	if _, _, err := vaultaccess.Resolve(m.db, user.ID, payload.VaultID); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if err := deviceauth.CheckVaultAccess(m.db, user.ID, string(did), payload.VaultID); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "device is not authorized for vault"})
		return
	}
	pluginID, _ := payload.Metadata["plugin_id"].(string)
	commandID, _ := payload.Metadata["command_id"].(string)
	registration, ok := m.RegistrationFor(pluginID)
	if !ok || commandID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	var command *RegisteredHook
	for _, hook := range registration.Hooks {
		if hook.Name == "editor.command" && hook.ID == commandID {
			command = &hook
			break
		}
	}
	if command == nil {
		c.Status(http.StatusNotFound)
		return
	}
	callback := command.Callback
	if callback == "" {
		callback = command.Name
	}
	response, err := m.invokeCallback(c.Request.Context(), pluginID, callback, PluginRequest{
		Method: "HOOK", Path: "/hooks/editor.command", Hook: "editor.command",
		User: &PluginUser{ID: user.ID, Username: user.Username, Role: user.Role},
		Payload: map[string]any{
			"vault_id": payload.VaultID, "content": payload.Content,
			"metadata": map[string]any{"plugin_id": pluginID, "command_id": commandID, "client_id": string(did)},
		},
		Settings: pluginSettings(m.db, payload.VaultID, pluginID),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "plugin hook failed"})
		return
	}
	content := payload.Content
	if command.Kind != "action" {
		body, err := decodePluginBody(response)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "plugin response failed"})
			return
		}
		content = string(body)
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

func (m *Manager) capabilities(c *gin.Context) {
	var records []models.ServerPlugin
	if err := m.db.Where("enabled = ?", true).Order("id asc").Find(&records).Error; err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	type capability struct {
		PluginID string     `json:"plugin_id"`
		Name     string     `json:"name"`
		Hooks    []HookSpec `json:"hooks"`
	}
	result := make([]capability, 0, len(records))
	for _, record := range records {
		registration, ok := m.RegistrationFor(record.ID)
		if !ok {
			continue
		}
		hooks := make([]HookSpec, 0)
		for _, hook := range registration.Hooks {
			if hook.Name == "editor.command" && hook.ID != "" && hook.Label != "" {
				hooks = append(hooks, HookSpec{Name: hook.Name, ID: hook.ID, Label: hook.Label})
			}
		}
		if len(hooks) > 0 {
			result = append(result, capability{PluginID: record.ID, Name: record.Name, Hooks: hooks})
		}
	}
	c.JSON(http.StatusOK, gin.H{"plugins": result})
}

func (m *Manager) servePublic(c *gin.Context) {
	m.serve(c, true)
}

func (m *Manager) serveAuthenticated(c *gin.Context) {
	m.serve(c, false)
}

func (m *Manager) serve(c *gin.Context, public bool) {
	record, err := m.record(c.Param("plugin_id"))
	if err != nil || !record.Enabled {
		c.Status(http.StatusNotFound)
		return
	}
	manifest, err := ParseManifest([]byte(record.ManifestJSON))
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	if strings.HasPrefix(path, "/assets/") {
		m.serveRegisteredAsset(c, record.ID, strings.TrimPrefix(path, "/assets/"))
		return
	}
	if !routeExists(manifest, c.Request.Method, path, public) {
		c.Status(http.StatusNotFound)
		return
	}

	m.mu.RLock()
	instance := m.modules[record.ID]
	m.mu.RUnlock()
	if instance == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxRequestBytes+1))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if len(body) > MaxRequestBytes {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	query := make(map[string][]string, len(c.Request.URL.Query()))
	for key, values := range c.Request.URL.Query() {
		query[key] = append([]string(nil), values...)
	}
	settings := map[string]any{}
	if !public {
		if vaultID := c.Query("vault_id"); vaultID != "" {
			user := auth.CurrentUser(c)
			if user == nil {
				c.Status(http.StatusUnauthorized)
				return
			}
			if _, _, err := vaultaccess.Resolve(m.db, user.ID, vaultID); err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			var setting models.VaultPluginSetting
			if err := m.db.Where("vault_id = ? AND plugin_id = ?", vaultID, record.ID).First(&setting).Error; err == nil && setting.Config != nil {
				settings = setting.Config
			}
		}
	}
	response, err := instance.Invoke(c.Request.Context(), PluginRequest{
		Method:     c.Request.Method,
		Path:       path,
		Query:      query,
		Settings:   settings,
		BodyBase64: base64.StdEncoding.EncodeToString(body),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "plugin execution failed"})
		return
	}
	writePluginResponse(c, response)
}

func (m *Manager) serveRegisteredAsset(c *gin.Context, pluginID, assetPath string) {
	registration, ok := m.RegistrationFor(pluginID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	for _, asset := range registration.Assets {
		if asset.Path != assetPath {
			continue
		}
		content, err := readInstalledFile(filepath.Join(m.root, pluginID, filepath.FromSlash(assetPath)), MaxPluginFileBytes)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(assetPath))
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		c.Data(http.StatusOK, contentType, content)
		return
	}
	c.Status(http.StatusNotFound)
}

func writePluginResponse(c *gin.Context, response PluginResponse) {
	responseBody, err := decodePluginBody(response)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "plugin response failed"})
		return
	}
	contentType := responseHeader(response, "Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	for name, value := range response.Headers {
		c.Header(name, value)
	}
	c.Data(response.Status, contentType, responseBody)
}

func routeExists(manifest Manifest, method, path string, public bool) bool {
	for _, route := range manifest.Routes {
		if route.Method == method && route.Path == path && route.Public == public {
			return true
		}
	}
	return false
}

func responseHeader(response PluginResponse, wanted string) string {
	for name, value := range response.Headers {
		if strings.EqualFold(name, wanted) {
			return value
		}
	}
	return ""
}
