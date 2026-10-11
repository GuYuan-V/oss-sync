package serverplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Close 发送 shutdown 帧并等待进程退出
// ctx 为 nil 时报错；重复关闭在宽限期内复用已关闭结果，超时则强杀并返回 ctx 错误
func (p *executablePlugin) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("executable plugin close context is nil")
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, processShutdownTimeout)
	defer cancel()
	p.mu.Lock()
	alreadyClosed := p.closed
	if !alreadyClosed {
		p.closed = true
		p.exitErr = ErrPluginProcessClosed
	}
	p.mu.Unlock()
	if alreadyClosed {
		select {
		case <-p.waitDone:
			return nil
		case <-shutdownCtx.Done():
			_ = p.kill()
			_ = p.stdin.Close()
			return shutdownCtx.Err()
		}
	}
	shutdownErr := p.writeFrame(shutdownCtx, []byte(`{"type":"shutdown"}`))
	if err := p.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		_ = p.kill()
		return fmt.Errorf("close executable plugin stdin: %w", err)
	}
	if shutdownErr != nil {
		_ = p.kill()
		return fmt.Errorf("send executable plugin shutdown: %w", shutdownErr)
	}
	select {
	case <-p.waitDone:
		return nil
	case <-shutdownCtx.Done():
		_ = p.kill()
		return shutdownCtx.Err()
	}
}

// kill 终止子进程，进程已退出或未启动时视为成功
func (p *executablePlugin) kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

// Healthy 表示进程是否仍在运行
func (p *executablePlugin) Healthy() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// protocolFailure 协议或 IO 出错时强杀进程并记录失败原因
func (p *executablePlugin) protocolFailure(err error) {
	_ = p.kill()
	p.fail(err)
}
