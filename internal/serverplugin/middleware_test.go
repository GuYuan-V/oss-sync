package serverplugin

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/config"
)

func TestResponseCaptureAllowsAfterMiddlewareToReplaceResponse(t *testing.T) {
	manager := &Manager{
		modules: make(map[string]pluginInstance),
		registrations: map[string]ExtensionRegistration{
			"plugin": {Middleware: []RegisteredMiddleware{{Name: "replace", Callback: "replace", Stage: "after"}}},
		},
	}
	manager.modules["plugin"] = &testPluginInstance{response: PluginResponse{
		Status:     http.StatusAccepted,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		BodyBase64: base64.StdEncoding.EncodeToString([]byte("replaced")),
	}}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(manager.Middleware(&config.Config{}))
	router.GET("/original", func(c *gin.Context) { c.String(http.StatusOK, "original") })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/original", nil))
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "replaced" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

type testPluginInstance struct{ response PluginResponse }

func (p *testPluginInstance) Invoke(_ context.Context, _ PluginRequest) (PluginResponse, error) {
	return p.response, nil
}
func (p *testPluginInstance) Close(context.Context) error         { return nil }
func (p *testPluginInstance) Healthy() bool                       { return true }
func (p *testPluginInstance) Registration() ExtensionRegistration { return ExtensionRegistration{} }
