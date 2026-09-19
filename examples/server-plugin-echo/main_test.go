package main

import (
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

func TestPluginHandlers_whenManifestDeclaresEchoRoute_registersCallback(t *testing.T) {
	// Given
	client := ossplugin.New(strings.NewReader(""), io.Discard)
	handlers := pluginHandlers(client)
	handler, ok := handlers["POST:/echo"]
	if !ok {
		t.Fatal("POST:/echo callback is not registered")
	}

	// When
	response, err := handler(t.Context(), ossplugin.Request{Method: "POST", Path: "/echo"})

	// Then
	if err != nil {
		t.Fatalf("echo callback: %v", err)
	}
	body, err := base64.StdEncoding.DecodeString(response.BodyBase64)
	if err != nil {
		t.Fatalf("decode echo response: %v", err)
	}
	if response.Status != 200 || string(body) != "echo from SDK" {
		t.Fatalf("echo response = %d %q, want 200 %q", response.Status, body, "echo from SDK")
	}
}
