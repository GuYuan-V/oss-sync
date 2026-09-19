package serverplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
)

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

func (p *executablePlugin) Healthy() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *executablePlugin) protocolFailure(err error) {
	_ = p.kill()
	p.fail(err)
}
