package serverplugin

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/config"
)

// Middleware invokes registered executable plugin middleware around host requests.
func (m *Manager) Middleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		originalWriter := c.Writer
		var capture *responseCapture
		if len(m.matchingMiddleware("after", c.Request.URL.Path)) > 0 && !streamingRequest(c.Request) {
			capture = newResponseCapture(originalWriter)
			c.Writer = capture
			defer func() {
				c.Writer = originalWriter
				capture.commit()
			}()
		}
		beforePayload := map[string]any{
			"method":  c.Request.Method,
			"path":    c.Request.URL.Path,
			"query":   c.Request.URL.Query(),
			"headers": cloneHeaders(c.Request.Header),
		}
		event := coreRequestEvent(c.Request.Method, c.Request.URL.Path)
		if m.requestBodyFilterNeeded(event, c.Request) {
			body, err := io.ReadAll(c.Request.Body)
			if err == nil {
				c.Request.Body = io.NopCloser(bytes.NewReader(body))
				beforePayload["body_base64"] = base64.StdEncoding.EncodeToString(body)
			}
		}
		if filtered, err := m.RunHook(c.Request.Context(), "http.before", beforePayload); err == nil {
			applyRequestFilter(c.Request, filtered)
		}
		if event.Before != "" {
			if filtered, err := m.RunHook(c.Request.Context(), event.Before, beforePayload); err == nil {
				applyRequestFilter(c.Request, filtered)
			}
		}
		if m.runMiddlewareStage(c, "before") {
			return
		}
		if registered, ok := m.matchingRoute(c.Request.Method, c.Request.URL.Path); ok {
			m.dispatchDynamicRoute(c, cfg, registered)
			return
		}
		c.Next()
		_ = m.runMiddlewareStage(c, "after")
		afterPayload := map[string]any{
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"status": c.Writer.Status(),
		}
		_, _ = m.RunHook(c.Request.Context(), "http.after", afterPayload)
		if event.After != "" && c.Writer.Status() < http.StatusBadRequest {
			_, _ = m.RunHook(c.Request.Context(), event.After, afterPayload)
		}
	}
}

func (m *Manager) requestBodyFilterNeeded(event coreEvent, request *http.Request) bool {
	if request.Body == nil || request.ContentLength < 0 || request.ContentLength > MaxRequestBytes {
		return false
	}
	return len(m.matchingHooks("http.before")) > 0 || event.Before != "" && len(m.matchingHooks(event.Before)) > 0
}

func applyRequestFilter(request *http.Request, filtered any) {
	value, ok := filtered.(map[string]any)
	if !ok {
		return
	}
	if encodedBody, ok := value["body_base64"].(string); ok {
		if body, err := base64.StdEncoding.DecodeString(encodedBody); err == nil {
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
		}
	}
	if rawQuery, ok := value["query"].(map[string]any); ok {
		query := make(url.Values, len(rawQuery))
		for name, rawValues := range rawQuery {
			if values, ok := rawValues.([]any); ok {
				for _, rawValue := range values {
					if value, ok := rawValue.(string); ok {
						query.Add(name, value)
					}
				}
			}
		}
		request.URL.RawQuery = query.Encode()
	}
	if rawHeaders, ok := value["headers"].(map[string]any); ok {
		for name, rawValues := range rawHeaders {
			request.Header.Del(name)
			if values, ok := rawValues.([]any); ok {
				for _, rawValue := range values {
					if value, ok := rawValue.(string); ok {
						request.Header.Add(name, value)
					}
				}
			}
		}
	}
}

func streamingRequest(request *http.Request) bool {
	return strings.EqualFold(request.Header.Get("Upgrade"), "websocket") ||
		strings.Contains(request.Header.Get("Accept"), "text/event-stream") ||
		strings.HasSuffix(request.URL.Path, "/stream")
}

func (m *Manager) runMiddlewareStage(c *gin.Context, stage string) bool {
	for _, registration := range m.matchingMiddleware(stage, c.Request.URL.Path) {
		callback := registration.Middleware.Callback
		if callback == "" {
			callback = registration.Middleware.Name
		}
		response, err := m.invokeCallback(c.Request.Context(), registration.PluginID, callback, PluginRequest{
			Method:  c.Request.Method,
			Path:    c.Request.URL.Path,
			Query:   c.Request.URL.Query(),
			Headers: cloneHeaders(c.Request.Header),
			Cookies: requestCookies(c.Request),
			User:    pluginUser(c),
			Payload: map[string]any{
				"stage":   stage,
				"status":  c.Writer.Status(),
				"headers": c.Writer.Header(),
			},
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "plugin middleware failed"})
			return true
		}
		if stage == "before" && response.Status != http.StatusNoContent {
			writePluginResponse(c, response)
			c.Abort()
			return true
		}
		if stage == "after" && response.Status != http.StatusNoContent {
			capture, ok := c.Writer.(*responseCapture)
			if !ok {
				return true
			}
			if err := capture.replace(response); err != nil {
				c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "plugin response failed"})
				return true
			}
		}
	}
	return false
}
