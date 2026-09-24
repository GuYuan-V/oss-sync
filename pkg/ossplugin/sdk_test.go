package ossplugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestClientRunsRegisteredCallback(t *testing.T) {
	input := `{"type":"request","id":"1","request":{"method":"GET","path":"/hello","callback":"hello"}}` + "\n"
	var output bytes.Buffer
	client := New(strings.NewReader(input), &output)
	if err := client.On("hello", func(_ context.Context, request Request) (Response, error) {
		if request.Path != "/hello" {
			t.Fatalf("request path = %q", request.Path)
		}
		return Response{Status: 200}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := client.Run(Registration{Routes: []Route{{Method: "GET", Path: "/hello", Callback: "hello"}}}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2", len(lines))
	}
	var ready frame
	if err := json.Unmarshal([]byte(lines[0]), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Type != "ready" || ready.Registration == nil || len(ready.Registration.Routes) != 1 {
		t.Fatalf("ready frame = %+v", ready)
	}
	var response frame
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "response" || response.ID != "1" {
		t.Fatalf("response frame = %+v", response)
	}
}

func TestClientHostCallRoundTrip(t *testing.T) {
	pluginInput, hostInput := io.Pipe()
	hostOutput, pluginOutput := io.Pipe()
	client := New(pluginInput, pluginOutput)
	if err := client.On("host", func(ctx context.Context, _ Request) (Response, error) {
		models, err := client.Services().Models(ctx)
		if err != nil {
			return Response{}, err
		}
		return Response{Status: len(models)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- client.Run(Registration{}) }()
	scanner := bufio.NewScanner(hostOutput)
	if !scanner.Scan() {
		t.Fatal("missing ready frame")
	}
	if _, err := io.WriteString(hostInput, `{"type":"request","id":"1","request":{"method":"GET","path":"/host","callback":"host"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() {
		t.Fatal("missing host call")
	}
	var call frame
	if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
		t.Fatal(err)
	}
	if call.Type != "host_call" {
		t.Fatalf("frame = %+v", call)
	}
	if _, err := io.WriteString(hostInput, `{"type":"host_response","id":"`+call.ID+`","result":["users"]}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() {
		t.Fatal("missing plugin response")
	}
	if !strings.Contains(scanner.Text(), `"status":1`) {
		t.Fatalf("response = %s", scanner.Text())
	}
	if _, err := io.WriteString(hostInput, `{"type":"shutdown"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = hostInput.Close()
	_ = hostOutput.Close()
}

func TestWriteJSONResponse(t *testing.T) {
	client := &Client{}
	response, err := client.WriteJSONResponse(201, map[string]any{"ok": true, "name": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != 201 || response.Headers["Content-Type"] != "application/json; charset=utf-8" {
		t.Fatalf("response = %#v", response)
	}
	raw, err := base64.StdEncoding.DecodeString(response.BodyBase64)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ok"] != true || decoded["name"] != "demo" {
		t.Fatalf("body = %#v", decoded)
	}
}
