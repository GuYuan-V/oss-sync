package serverplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	abiVersionExport = "oss_abi_version"
	allocExport      = "oss_alloc"
	handleExport     = "oss_handle"
	memoryExport     = "memory"
	maxExecution     = 2 * time.Second
)

// ErrInvalidModule 表示 WASM 模块不满足宿主 ABI 或大小限制
var ErrInvalidModule = errors.New("invalid server plugin wasm module")

// Runtime 编译并调用服务端插件，不向其开放宿主导入
type Runtime struct {
	runtime wazero.Runtime
}

func parseExecutablePluginResponse(raw []byte) (PluginResponse, error) {
	var response PluginResponse
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return PluginResponse{}, fmt.Errorf("decode executable response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return PluginResponse{}, errors.New("executable response must contain one JSON object")
	}
	if response.Status < 200 || response.Status > 599 {
		return PluginResponse{}, errors.New("executable response status is invalid")
	}
	if len(response.Headers) > 128 {
		return PluginResponse{}, errors.New("executable response has too many headers")
	}
	for name, value := range response.Headers {
		if textproto.CanonicalMIMEHeaderKey(name) == "" || strings.ContainsAny(value, "\r\n") {
			return PluginResponse{}, fmt.Errorf("executable response header %q is invalid", name)
		}
	}
	if _, err := decodePluginBody(response); err != nil {
		return PluginResponse{}, err
	}
	return response, nil
}

// NewRuntime 创建无宿主导入的 WASM 运行时
func NewRuntime(ctx context.Context) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("plugin runtime context is nil")
	}
	config := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(MaxPluginMemoryPages)
	return &Runtime{runtime: wazero.NewRuntimeWithConfig(ctx, config)}, nil
}

func (r *Runtime) Compile(ctx context.Context, wasm []byte) (compiled wazero.CompiledModule, err error) {
	if len(wasm) == 0 || len(wasm) > MaxWasmBytes {
		return nil, fmt.Errorf("%w: wasm size is invalid", ErrInvalidModule)
	}
	compiled, err = r.runtime.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("%w: compile module: %v", ErrInvalidModule, err)
	}
	if err := validateCompiledModule(ctx, r.runtime, compiled); err != nil {
		closeErr := compiled.Close(ctx)
		if closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close invalid compiled module: %w", closeErr))
		}
		return nil, err
	}
	return compiled, nil
}

func validateCompiledModule(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule) (err error) {
	if len(compiled.ImportedFunctions()) != 0 || len(compiled.ImportedMemories()) != 0 {
		return fmt.Errorf("%w: host imports are not allowed", ErrInvalidModule)
	}
	if compiled.ExportedMemories()[memoryExport] == nil {
		return fmt.Errorf("%w: exported memory is missing", ErrInvalidModule)
	}
	if err := validateFunction(compiled, abiVersionExport, nil, []api.ValueType{api.ValueTypeI32}); err != nil {
		return err
	}
	if err := validateFunction(compiled, allocExport, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}); err != nil {
		return err
	}
	if err := validateFunction(compiled, handleExport, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}); err != nil {
		return err
	}
	module, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return fmt.Errorf("%w: instantiate module: %v", ErrInvalidModule, err)
	}
	defer func() {
		if closeErr := module.Close(ctx); err == nil && closeErr != nil {
			err = fmt.Errorf("close validation module: %w", closeErr)
		}
	}()
	result, err := module.ExportedFunction(abiVersionExport).Call(ctx)
	if err != nil {
		return fmt.Errorf("%w: call abi version: %v", ErrInvalidModule, err)
	}
	if len(result) != 1 || result[0] != CurrentAPIVersion {
		return fmt.Errorf("%w: unsupported abi version", ErrInvalidModule)
	}
	return nil
}

func validateFunction(compiled wazero.CompiledModule, name string, params, results []api.ValueType) error {
	definition := compiled.ExportedFunctions()[name]
	if definition == nil {
		return fmt.Errorf("%w: exported function %q is missing", ErrInvalidModule, name)
	}
	if !sameValueTypes(definition.ParamTypes(), params) || !sameValueTypes(definition.ResultTypes(), results) {
		return fmt.Errorf("%w: exported function %q has an invalid signature", ErrInvalidModule, name)
	}
	return nil
}

func sameValueTypes(left, right []api.ValueType) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (r *Runtime) Invoke(ctx context.Context, compiled wazero.CompiledModule, request PluginRequest) (response PluginResponse, err error) {
	callCtx, cancel := context.WithTimeout(ctx, maxExecution)
	defer cancel()
	module, err := r.runtime.InstantiateModule(callCtx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return PluginResponse{}, fmt.Errorf("instantiate plugin: %w", err)
	}
	defer func() {
		if closeErr := module.Close(callCtx); err == nil && closeErr != nil {
			err = fmt.Errorf("close plugin request: %w", closeErr)
		}
	}()

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return PluginResponse{}, fmt.Errorf("encode plugin request: %w", err)
	}
	if len(requestBytes) > MaxRequestBytes {
		return PluginResponse{}, fmt.Errorf("plugin request exceeds %d bytes", MaxRequestBytes)
	}
	memory := module.ExportedMemory(memoryExport)
	allocResult, err := module.ExportedFunction(allocExport).Call(callCtx, uint64(len(requestBytes)))
	if err != nil {
		return PluginResponse{}, fmt.Errorf("allocate plugin request: %w", err)
	}
	if len(allocResult) != 1 {
		return PluginResponse{}, fmt.Errorf("%w: allocator returned an invalid result", ErrInvalidModule)
	}
	requestPtr := uint32(allocResult[0])
	if !memory.Write(requestPtr, requestBytes) {
		return PluginResponse{}, fmt.Errorf("%w: plugin request does not fit memory", ErrInvalidModule)
	}
	handleResult, err := module.ExportedFunction(handleExport).Call(callCtx, uint64(requestPtr), uint64(len(requestBytes)))
	if err != nil {
		return PluginResponse{}, fmt.Errorf("plugin request failed: %w", err)
	}
	if len(handleResult) != 1 {
		return PluginResponse{}, fmt.Errorf("%w: handler returned an invalid result", ErrInvalidModule)
	}
	responsePtr := uint32(handleResult[0] >> 32)
	responseLength := uint32(handleResult[0])
	if responseLength > MaxResponseBytes {
		return PluginResponse{}, fmt.Errorf("plugin response exceeds %d bytes", MaxResponseBytes)
	}
	responseBytes, ok := memory.Read(responsePtr, responseLength)
	if !ok {
		return PluginResponse{}, fmt.Errorf("%w: plugin response is outside memory", ErrInvalidModule)
	}
	return parsePluginResponse(responseBytes)
}

func parsePluginResponse(raw []byte) (PluginResponse, error) {
	var response PluginResponse
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return PluginResponse{}, fmt.Errorf("%w: decode response: %v", ErrInvalidModule, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return PluginResponse{}, fmt.Errorf("%w: response must contain one JSON object", ErrInvalidModule)
	}
	if response.Status < 200 || response.Status > 599 {
		return PluginResponse{}, fmt.Errorf("%w: response status is invalid", ErrInvalidModule)
	}
	if len(response.Headers) > 16 {
		return PluginResponse{}, fmt.Errorf("%w: too many response headers", ErrInvalidModule)
	}
	for name, value := range response.Headers {
		if !allowedResponseHeader(name) || strings.ContainsAny(value, "\r\n") {
			return PluginResponse{}, fmt.Errorf("%w: response header is not allowed", ErrInvalidModule)
		}
	}
	if _, err := decodePluginBody(response); err != nil {
		return PluginResponse{}, err
	}
	return response, nil
}

func decodePluginBody(response PluginResponse) ([]byte, error) {
	body, err := base64.StdEncoding.DecodeString(response.BodyBase64)
	if err != nil {
		return nil, fmt.Errorf("%w: response body is not valid base64", ErrInvalidModule)
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("plugin response body exceeds %d bytes", MaxResponseBytes)
	}
	return body, nil
}

func allowedResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-type", "cache-control", "etag", "location":
		return true
	default:
		return false
	}
}

func (r *Runtime) Close(ctx context.Context) error {
	return r.runtime.Close(ctx)
}
