package serverplugin

import (
	"encoding/base64"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
)

func (m *Manager) registerDynamicRoutes(router *gin.Engine, cfg *config.Config) {
	router.NoRoute(func(c *gin.Context) {
		registered, ok := m.matchingRoute(c.Request.Method, c.Request.URL.Path)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		m.dispatchDynamicRoute(c, cfg, registered)
	})
}

func (m *Manager) dispatchDynamicRoute(c *gin.Context, cfg *config.Config, registered registeredRoute) {
	if !m.authorizeDynamicRoute(c, cfg, registered.Route.Auth) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxRequestBytes+1))
	if err != nil || len(body) > MaxRequestBytes {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	callback := registered.Route.Callback
	if callback == "" {
		callback = registered.Route.Method + ":" + registered.Route.Path
	}
	response, err := m.invokeCallback(c.Request.Context(), registered.PluginID, callback, PluginRequest{
		Method:     c.Request.Method,
		Path:       c.Request.URL.Path,
		Params:     registered.Params,
		Query:      c.Request.URL.Query(),
		Headers:    cloneHeaders(c.Request.Header),
		Cookies:    requestCookies(c.Request),
		User:       pluginUser(c),
		BodyBase64: base64.StdEncoding.EncodeToString(body),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "plugin execution failed"})
		return
	}
	writePluginResponse(c, response)
	c.Abort()
}

func cloneHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		result[name] = append([]string(nil), values...)
	}
	return result
}

func requestCookies(request *http.Request) map[string]string {
	result := make(map[string]string)
	for _, cookie := range request.Cookies() {
		result[cookie.Name] = cookie.Value
	}
	return result
}

func pluginUser(c *gin.Context) *PluginUser {
	user := auth.CurrentUser(c)
	if user == nil {
		return nil
	}
	return &PluginUser{ID: user.ID, Username: user.Username, Role: user.Role}
}

func (m *Manager) authorizeDynamicRoute(c *gin.Context, cfg *config.Config, authMode string) bool {
	if authMode == "public" {
		return true
	}
	if !auth.Authenticate(c, m.db, cfg) {
		return false
	}
	if authMode == "admin" {
		_, ok := auth.RequireAdmin(c)
		return ok
	}
	return true
}
