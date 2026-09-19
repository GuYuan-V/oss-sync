package serverplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

func TestRuntimeInvokesValidatedV1Module(t *testing.T) {
	wasm := testResponseModule(t, PluginResponse{
		Status:     200,
		BodyBase64: base64.StdEncoding.EncodeToString([]byte("hello from wasm")),
	})
	runtime, err := NewRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(t.Context())

	compiled, err := runtime.Compile(t.Context(), wasm)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	defer compiled.Close(t.Context())

	response, err := runtime.Invoke(t.Context(), compiled, PluginRequest{Method: "GET", Path: "/hello"})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != 200 || response.BodyBase64 != base64.StdEncoding.EncodeToString([]byte("hello from wasm")) {
		t.Fatalf("Invoke() response = %+v", response)
	}
}

func TestParsePluginResponseRejectsOneHundredStatusAndTrailingJSON(t *testing.T) {
	for _, raw := range []string{
		`{"status":100,"body_base64":""}`,
		`{"status":200,"body_base64":""}{"status":201}`,
	} {
		if _, err := parsePluginResponse([]byte(raw)); err == nil {
			t.Fatalf("parsePluginResponse(%q) error = nil", raw)
		}
	}
}

func TestRuntimeSupportsConcurrentPluginInvocations(t *testing.T) {
	wasm := testResponseModule(t, PluginResponse{Status: 200})
	runtime, err := NewRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(t.Context()); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	compiled, err := runtime.Compile(t.Context(), wasm)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := compiled.Close(t.Context()); err != nil {
			t.Errorf("close compiled module: %v", err)
		}
	})

	const calls = 8
	var wait sync.WaitGroup
	wait.Add(calls)
	errorsFound := make(chan error, calls)
	for range calls {
		go func() {
			defer wait.Done()
			_, err := runtime.Invoke(context.Background(), compiled, PluginRequest{Method: "GET", Path: "/hello"})
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent Invoke() error = %v", err)
		}
	}
}

func testResponseModule(t *testing.T, response PluginResponse) []byte {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return makeWasmModule(encoded, 256)
}

func makeWasmModule(response []byte, responseOffset uint32) []byte {
	return makeWasmModuleWithAlloc(response, responseOffset, 128)
}

func makeWasmModuleWithAlloc(response []byte, responseOffset, allocOffset uint32) []byte {
	types := []byte{
		0x03,
		0x60, 0x00, 0x01, 0x7f,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	}
	functions := []byte{0x03, 0x00, 0x01, 0x02}
	memory := []byte{0x01, 0x00, 0x01}
	exports := []byte{0x04}
	exports = append(exports, export("memory", 0x02, 0)...)
	exports = append(exports, export("oss_abi_version", 0x00, 0)...)
	exports = append(exports, export("oss_alloc", 0x00, 1)...)
	exports = append(exports, export("oss_handle", 0x00, 2)...)
	code := []byte{0x03}
	code = append(code, functionBody([]byte{0x41, 0x01})...)
	code = append(code, functionBody(append([]byte{0x41}, unsignedLEB(allocOffset)...))...)
	packed := uint64(responseOffset)<<32 | uint64(len(response))
	code = append(code, functionBody(append([]byte{0x42}, signedLEB(int64(packed))...))...)
	data := append([]byte{0x01, 0x00, 0x41}, unsignedLEB(responseOffset)...)
	data = append(data, 0x0b)
	data = append(data, unsignedLEB(uint32(len(response)))...)
	data = append(data, response...)

	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = append(wasm, section(1, types)...)
	wasm = append(wasm, section(3, functions)...)
	wasm = append(wasm, section(5, memory)...)
	wasm = append(wasm, section(7, exports)...)
	wasm = append(wasm, section(10, code)...)
	wasm = append(wasm, section(11, data)...)
	return wasm
}

func section(id byte, content []byte) []byte {
	return append([]byte{id}, append(unsignedLEB(uint32(len(content))), content...)...)
}

func export(name string, kind byte, index byte) []byte {
	return append(append(append([]byte{}, unsignedLEB(uint32(len(name)))...), []byte(name)...), kind, index)
}

func functionBody(instructions []byte) []byte {
	body := append([]byte{0x00}, append(instructions, 0x0b)...)
	return append(unsignedLEB(uint32(len(body))), body...)
}

func unsignedLEB(value uint32) []byte {
	result := make([]byte, 0, 5)
	for {
		part := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			part |= 0x80
		}
		result = append(result, part)
		if value == 0 {
			return result
		}
	}
}

func signedLEB(value int64) []byte {
	result := make([]byte, 0, 10)
	for {
		part := byte(value & 0x7f)
		value >>= 7
		more := !((value == 0 && part&0x40 == 0) || (value == -1 && part&0x40 != 0))
		if more {
			part |= 0x80
		}
		result = append(result, part)
		if !more {
			return result
		}
	}
}
