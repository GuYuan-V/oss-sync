package serverplugin

import (
	"encoding/base64"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
)

const (
	webSessionCookie = "oss_web_session"
	webCSRFCookie    = "oss_csrf"
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
	if !m.authenticatePluginRequest(c, cfg) {
		return false
	}
	if authMode == "admin" {
		_, ok := auth.RequireAdmin(c)
		return ok
	}
	return true
}

// authenticatePluginRequest 先用 Bearer 令牌认证，无令牌时回退到控制台会话 cookie
// cookie 会话对写方法强制双提交 CSRF；Bearer 请求天然免疫 CSRF 不做校验
func (m *Manager) authenticatePluginRequest(c *gin.Context, cfg *config.Config) bool {
	if c.GetHeader("Authorization") != "" {
		return auth.Authenticate(c, m.db, cfg)
	}
	token, err := c.Cookie(webSessionCookie)
	if err != nil || token == "" {
		return auth.Authenticate(c, m.db, cfg)
	}
	user, err := auth.AuthenticateToken(m.db, cfg, token)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return false
	}
	if isStateChangingMethod(c.Request.Method) && !validWebCSRF(c) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "csrf token invalid"})
		return false
	}
	c.Set(auth.ContextKeyCurrentUser, user)
	return true
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// validWebCSRF 校验控制台会话的双提交 CSRF：oss_csrf cookie 需与 X-CSRF-Token 请求头一致
func validWebCSRF(c *gin.Context) bool {
	expected, err := c.Cookie(webCSRFCookie)
	if err != nil || expected == "" {
		return false
	}
	got := c.GetHeader("X-CSRF-Token")
	return got != "" && got == expected
}
