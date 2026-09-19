package serverplugin

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestExecutablePluginInvokesConcurrentRequests(t *testing.T) {
	plugin := startTestExecutablePlugin(t)
	t.Cleanup(func() {
		if err := plugin.Close(context.Background()); err != nil {
			t.Errorf("close executable plugin: %v", err)
		}
	})

	const calls = 16
	var wait sync.WaitGroup
	wait.Add(calls)
	errorsFound := make(chan error, calls)
	for index := range calls {
		go func() {
			defer wait.Done()
			path := fmt.Sprintf("/echo/%d", index)
			response, err := plugin.Invoke(t.Context(), PluginRequest{Method: "GET", Path: path})
			if err != nil {
				errorsFound <- err
				return
			}
			body, err := decodePluginBody(response)
			if err != nil {
				errorsFound <- err
				return
			}
			if response.Status != 200 || string(body) != path {
				errorsFound <- fmt.Errorf("response = %+v, body = %q", response, body)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecutablePluginPropagatesProcessExit(t *testing.T) {
	plugin := startTestExecutablePlugin(t)
	defer plugin.Close(context.Background())

	_, err := plugin.Invoke(t.Context(), PluginRequest{Method: "GET", Path: "/crash"})
	if !errors.Is(err, ErrPluginProcessExited) {
		t.Fatalf("Invoke() error = %v, want process exit error", err)
	}
	if _, err := plugin.Invoke(t.Context(), PluginRequest{Method: "GET", Path: "/after-crash"}); !errors.Is(err, ErrPluginProcessExited) {
		t.Fatalf("Invoke() after crash error = %v, want process exit error", err)
	}
}

func TestExecutablePluginCallsHostDuringRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.exe"), readTestBinary(t), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		ID: "host-call-test", Name: "Host call test", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Runtime: RuntimeExecutable, Entrypoints: map[string]string{"any": "plugin.exe"},
		Args: []string{"-test.run=^TestExecutablePluginProcessChild$"},
	}
	plugin, err := startExecutablePlugin(t.Context(), dir, manifest, func(context.Context, string, map[string]json.RawMessage) (any, error) {
		return []string{"users", "vaults"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer plugin.Close(context.Background())
	response, err := plugin.Invoke(t.Context(), PluginRequest{Method: "GET", Path: "/host-call"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := decodePluginBody(response)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "host-call-ok" {
		t.Fatalf("host call response = %q", body)
	}
}

func TestExecutablePluginProcessChild(t *testing.T) {
	if os.Getenv("OSS_PLUGIN_ID") == "" {
		return
	}

	output := bufio.NewWriter(os.Stdout)
	registration := &ExtensionRegistration{
		Hooks: []RegisteredHook{
			{Name: "blog.content", Kind: "filter", Callback: "blog.content"},
			{Name: "orders.before_save", Kind: "action", Callback: "orders.before_save"},
		},
		Routes: []RegisteredRoute{
			{Method: "GET", Path: "/hello", Auth: "public", Callback: "GET:/hello"},
			{Method: "GET", Path: "/dynamic-hello", Auth: "public", Callback: "dynamic.hello"},
		},
		Lifecycle: RegisteredLifecycle{
			Activate: "plugin.activate", Deactivate: "plugin.deactivate", Uninstall: "plugin.uninstall",
		},
	}
	if err := json.NewEncoder(output).Encode(processFrame{Type: "ready", APIVersion: executableProtocolVersion, Registration: registration}); err != nil {
		os.Exit(2)
	}
	if err := output.Flush(); err != nil {
		os.Exit(2)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame processFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			os.Exit(3)
		}
		if frame.Type == "shutdown" {
			os.Exit(0)
		}
		var request PluginRequest
		if err := json.Unmarshal(frame.Request, &request); err != nil {
			os.Exit(4)
		}
		if request.Path == "/crash" {
			os.Exit(17)
		}
		if request.Path == "/host-call" {
			params, err := json.Marshal(map[string]any{})
			if err != nil {
				os.Exit(7)
			}
			if err := json.NewEncoder(output).Encode(processFrame{Type: "host_call", ID: "host-1", Method: "host.models", Params: params}); err != nil {
				os.Exit(8)
			}
			if err := output.Flush(); err != nil {
				os.Exit(8)
			}
			if !scanner.Scan() {
				os.Exit(9)
			}
			var hostResponse processFrame
			if err := json.Unmarshal(scanner.Bytes(), &hostResponse); err != nil || hostResponse.Type != "host_response" {
				os.Exit(10)
			}
			response := PluginResponse{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString([]byte("host-call-ok"))}
			responseBytes, err := json.Marshal(response)
			if err != nil {
				os.Exit(11)
			}
			if err := json.NewEncoder(output).Encode(processFrame{Type: "response", ID: frame.ID, Response: responseBytes}); err != nil {
				os.Exit(12)
			}
			if err := output.Flush(); err != nil {
				os.Exit(12)
			}
			continue
		}
		response := PluginResponse{
			Status: 200,
		}
		responseBody := request.Path
		if request.Hook != "" {
			if content, ok := request.Payload["content"].(string); ok {
				responseBody = "executable:" + content
			}
		}
		response.BodyBase64 = base64.StdEncoding.EncodeToString([]byte(responseBody))
		responseBytes, err := json.Marshal(response)
		if err != nil {
			os.Exit(5)
		}
		if err := json.NewEncoder(output).Encode(processFrame{
			Type:     "response",
			ID:       frame.ID,
			Response: responseBytes,
		}); err != nil {
			os.Exit(6)
		}
		if err := output.Flush(); err != nil {
			os.Exit(6)
		}
	}
	os.Exit(0)
}

func startTestExecutablePlugin(t *testing.T) *executablePlugin {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.exe"), readTestBinary(t), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		ID:          "process-test",
		Name:        "Process test",
		Version:     "1.0.0",
		APIVersion:  CurrentAPIVersion,
		Runtime:     RuntimeExecutable,
		Entrypoints: map[string]string{"any": "plugin.exe"},
		Args:        []string{"-test.run=^TestExecutablePluginProcessChild$"},
	}
	plugin, err := startExecutablePlugin(t.Context(), dir, manifest, nil)
	if err != nil {
		t.Fatalf("startExecutablePlugin() error = %v", err)
	}
	return plugin
}

func readTestBinary(t *testing.T) []byte {
	t.Helper()
	content, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return content
}
