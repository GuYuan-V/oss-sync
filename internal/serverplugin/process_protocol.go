package serverplugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// processFrame 是插件进程与宿主之间的换行分隔 JSON 帧
// Type 取值 ready、host_call、host_response、host_error、response、error
type processFrame struct {
	Type         string                 `json:"type"`
	ID           string                 `json:"id,omitempty"`
	APIVersion   int                    `json:"api_version,omitempty"`
	Request      json.RawMessage        `json:"request,omitempty"`
	Response     json.RawMessage        `json:"response,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Registration *ExtensionRegistration `json:"registration,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Params       json.RawMessage        `json:"params,omitempty"`
	Result       json.RawMessage        `json:"result,omitempty"`
}

// readLoop 逐行读取插件 stdout 帧
// 首帧必须是协议版本匹配的 ready 帧，可携带 registration；host_call 帧异步处理，其余帧按请求 ID 交付
func (p *executablePlugin) readLoop(stdout io.ReadCloser) {
	defer stdout.Close()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), MaxRequestBytes+MaxResponseBytes+4096)
	ready := false
	for scanner.Scan() {
		var frame processFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			p.protocolFailure(fmt.Errorf("decode executable plugin frame: %w", err))
			return
		}
		if !ready {
			if frame.Type != "ready" || frame.APIVersion != executableProtocolVersion {
				p.signalReady(errors.New("executable plugin handshake is invalid"))
				p.protocolFailure(errors.New("executable plugin handshake is invalid"))
				return
			}
			if frame.Registration != nil {
				if err := validateRegistration(*frame.Registration); err != nil {
					p.signalReady(fmt.Errorf("validate executable plugin registration: %w", err))
					p.protocolFailure(err)
					return
				}
				p.mu.Lock()
				p.registration = *frame.Registration
				p.mu.Unlock()
			}
			ready = true
			p.signalReady(nil)
			continue
		}
		if frame.Type == "host_call" {
			go func(call processFrame) {
				if err := p.handleHostCall(call); err != nil {
					p.protocolFailure(err)
				}
			}(frame)
			continue
		}
		if frame.Type != "response" && frame.Type != "error" {
			p.protocolFailure(fmt.Errorf("executable plugin sent unsupported frame type %q", frame.Type))
			return
		}
		if frame.ID == "" {
			p.protocolFailure(errors.New("executable plugin response has no request id"))
			return
		}
		if frame.Type == "error" {
			p.finish(frame.ID, processResult{err: errors.New(frame.Error)})
			continue
		}
		response, err := parseExecutablePluginResponse(frame.Response)
		if err != nil {
			p.protocolFailure(err)
			return
		}
		p.finish(frame.ID, processResult{response: response, err: err})
	}
	if err := scanner.Err(); err != nil {
		p.protocolFailure(fmt.Errorf("read executable plugin output: %w", err))
		return
	}
	p.protocolFailure(ErrPluginProcessExited)
}

// handleHostCall 执行插件发起的宿主调用并把结果回写成 host_response 或 host_error 帧
// 单次调用受 maxExecution 限制，写回失败视为协议失败
func (p *executablePlugin) handleHostCall(frame processFrame) error {
	if p.hostCall == nil || frame.ID == "" || frame.Method == "" {
		return errors.New("executable plugin host call is invalid")
	}
	params := make(map[string]json.RawMessage)
	if len(frame.Params) > 0 {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return fmt.Errorf("decode executable plugin host call: %w", err)
		}
	}
	callCtx, cancel := context.WithTimeout(context.Background(), maxExecution)
	defer cancel()
	result, err := p.hostCall(callCtx, frame.Method, params)
	response := processFrame{Type: "host_response", ID: frame.ID}
	if err != nil {
		response.Type = "host_error"
		response.Error = err.Error()
	} else {
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return fmt.Errorf("encode executable plugin host response: %w", marshalErr)
		}
		response.Result = encoded
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode executable plugin host frame: %w", err)
	}
	if err := p.writeFrame(callCtx, encoded); err != nil {
		return fmt.Errorf("write executable plugin host response: %w", err)
	}
	return nil
}

// waitLoop 等待子进程退出并关闭等待通道
// 退出原因统一包装为 ErrPluginProcessExited
func (p *executablePlugin) waitLoop() {
	defer close(p.waitDone)
	err := p.cmd.Wait()
	if err == nil {
		err = ErrPluginProcessExited
	} else {
		err = fmt.Errorf("%w: %v", ErrPluginProcessExited, err)
	}
	p.fail(err)
}

// writeProcessLine 向进程 stdin 写入一帧并追加换行分隔符
func writeProcessLine(writer io.Writer, frame []byte) error {
	if _, err := writer.Write(frame); err != nil {
		return err
	}
	_, err := writer.Write([]byte{'\n'})
	return err
}
