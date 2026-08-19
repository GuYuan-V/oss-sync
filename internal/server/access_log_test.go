package server

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAccessLogOmitsQueryCredentials(t *testing.T) {
	// Given: Gin reports an EventSource path containing a query token.
	params := gin.LogFormatterParams{
		TimeStamp:  time.Date(2026, 8, 10, 23, 0, 0, 0, time.UTC),
		StatusCode: 500,
		Latency:    time.Millisecond,
		ClientIP:   "127.0.0.1",
		Method:     "GET",
		Path:       "/api/vaults/vault-1/collaborations/stream?token=secret-token&client_id=device-1",
	}

	// When: the access log line is formatted.
	line := formatAccessLog(params)

	// Then: the route remains visible without any query credential.
	if strings.Contains(line, "secret-token") || strings.Contains(line, "client_id") || strings.Contains(line, "?") {
		t.Fatalf("access log leaked query data: %q", line)
	}
	if !strings.Contains(line, "/api/vaults/vault-1/collaborations/stream") {
		t.Fatalf("access log omitted route path: %q", line)
	}
}

func TestAccessLogOmitsSuccessfulRequests(t *testing.T) {
	// Given
	params := gin.LogFormatterParams{StatusCode: 200}

	// When
	line := formatAccessLog(params)

	// Then
	if line != "" {
		t.Fatalf("successful request log = %q, want empty", line)
	}
}

func TestAccessLogLogsClientErrors(t *testing.T) {
	if line := formatAccessLog(gin.LogFormatterParams{StatusCode: 399}); line != "" {
		t.Fatalf("status 399 log = %q, want empty", line)
	}
	if line := formatAccessLog(gin.LogFormatterParams{StatusCode: 400, Path: "/bad-request"}); !strings.Contains(line, "/bad-request") {
		t.Fatalf("status 400 log = %q, want route", line)
	}
}
