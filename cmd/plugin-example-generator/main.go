// Package main 生成服务端插件示例包

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type manifest struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	APIVersion int     `json:"api_version"`
	Routes     []route `json:"routes"`
}

type route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Public bool   `json:"public"`
}

func main() {
	manifestBytes, err := json.Marshal(manifest{
		ID: "hello-world", Name: "Hello World", Version: "1.0.0", APIVersion: 1,
		Routes: []route{{Method: "GET", Path: "/hello", Public: true}},
	})
	if err != nil {
		panic(err)
	}
	response := []byte(`{"status":200,"headers":{"Content-Type":"text/plain; charset=utf-8"},"body_base64":"SGVsbG8gZnJvbSBPU1MgU2VydmVyIFBsdWdpbiE="}`)
	wasm := makeModule(response, 256)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	writeEntry(writer, "manifest.json", manifestBytes)
	writeEntry(writer, "plugin.wasm", wasm)
	if err := writer.Close(); err != nil {
		panic(err)
	}
	path := "server-plugin-hello-world.zip"
	if err := os.WriteFile(path, archive.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("%s (%d bytes, wasm %d bytes)\n", path, archive.Len(), len(wasm))
}

func writeEntry(writer *zip.Writer, name string, content []byte) {
	entry, err := writer.Create(name)
	if err != nil {
		panic(err)
	}
	if _, err := entry.Write(content); err != nil {
		panic(err)
	}
}

func makeModule(response []byte, offset uint32) []byte {
	types := []byte{
		0x03,
		0x60, 0x00, 0x01, 0x7f,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	}
	functions := []byte{0x03, 0x00, 0x01, 0x02}
	memory := []byte{0x01, 0x00, 0x01}
	exports := []byte{0x04}
	exports = append(exports, wasmExport("memory", 0x02, 0)...)
	exports = append(exports, wasmExport("oss_abi_version", 0x00, 0)...)
	exports = append(exports, wasmExport("oss_alloc", 0x00, 1)...)
	exports = append(exports, wasmExport("oss_handle", 0x00, 2)...)
	packed := uint64(offset)<<32 | uint64(len(response))
	code := []byte{0x03}
	code = append(code, wasmFunctionBody([]byte{0x41, 0x01})...)
	code = append(code, wasmFunctionBody([]byte{0x41, 0x80, 0x01})...)
	code = append(code, wasmFunctionBody(append([]byte{0x42}, signedLEB(int64(packed))...))...)
	data := []byte{0x01, 0x00, 0x41}
	data = append(data, unsignedLEB(offset)...)
	data = append(data, 0x0b)
	data = append(data, unsignedLEB(uint32(len(response)))...)
	data = append(data, response...)
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = append(wasm, wasmSection(1, types)...)
	wasm = append(wasm, wasmSection(3, functions)...)
	wasm = append(wasm, wasmSection(5, memory)...)
	wasm = append(wasm, wasmSection(7, exports)...)
	wasm = append(wasm, wasmSection(10, code)...)
	wasm = append(wasm, wasmSection(11, data)...)
	return wasm
}

func wasmSection(id byte, content []byte) []byte {
	return append([]byte{id}, append(unsignedLEB(uint32(len(content))), content...)...)
}

func wasmExport(name string, kind, index byte) []byte {
	return append(append(append([]byte{}, unsignedLEB(uint32(len(name)))...), []byte(name)...), kind, index)
}

func wasmFunctionBody(instructions []byte) []byte {
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
