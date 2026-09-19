package serverplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	executableProtocolVersion = 1
	processStartupTimeout     = 5 * time.Second
	processShutdownTimeout    = 5 * time.Second
)

var (
	ErrPluginProcessExited = errors.New("executable server plugin process exited")
	ErrPluginProcessClosed = errors.New("executable server plugin process is closed")
)

type executablePlugin struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu           sync.Mutex
	writeMu      sync.Mutex
	nextID       uint64
	pending      map[string]chan processResult
	closed       bool
	exitErr      error
	done         chan struct{}
	waitDone     chan struct{}
	ready        chan error
	registration ExtensionRegistration
	hostCall     func(context.Context, string, map[string]json.RawMessage) (any, error)
	readyOne     sync.Once
	doneOne      sync.Once
}

type processResult struct {
	response PluginResponse
	err      error
}

func startExecutablePlugin(
	ctx context.Context,
	dir string,
	manifest Manifest,
	hostCall func(context.Context, string, map[string]json.RawMessage) (any, error),
) (*executablePlugin, error) {
	if ctx == nil {
		return nil, errors.New("executable plugin context is nil")
	}
	entrypoint, err := manifestEntrypoint(manifest)
	if err != nil {
		return nil, err
	}
	commandPath := filepath.Join(dir, filepath.FromSlash(entrypoint))
	cmd := exec.Command(commandPath, manifest.Args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"OSS_PLUGIN_ID="+manifest.ID,
		"OSS_PLUGIN_DIR="+dir,
		"OSS_PLUGIN_PROTOCOL="+strconv.Itoa(executableProtocolVersion),
	)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create executable plugin stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create executable plugin stdout: %w", err), stdin.Close())
	}
	plugin := &executablePlugin{
		cmd:          cmd,
		stdin:        stdin,
		pending:      make(map[string]chan processResult),
		done:         make(chan struct{}),
		waitDone:     make(chan struct{}),
		ready:        make(chan error, 1),
		registration: registrationFromManifest(manifest),
		hostCall:     hostCall,
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(fmt.Errorf("start executable plugin: %w", err), stdin.Close(), stdout.Close())
	}
	go plugin.readLoop(stdout)
	go plugin.waitLoop()
	startupCtx, cancel := context.WithTimeout(ctx, processStartupTimeout)
	defer cancel()
	select {
	case err := <-plugin.ready:
		if err != nil {
			_ = plugin.Close(context.Background())
			return nil, err
		}
		return plugin, nil
	case <-startupCtx.Done():
		_ = plugin.kill()
		return nil, fmt.Errorf("wait for executable plugin: %w", startupCtx.Err())
	}
}

func (p *executablePlugin) Registration() ExtensionRegistration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.registration
}

func (p *executablePlugin) Invoke(ctx context.Context, request PluginRequest) (PluginResponse, error) {
	if ctx == nil {
		return PluginResponse{}, errors.New("executable plugin request context is nil")
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return PluginResponse{}, fmt.Errorf("encode executable plugin request: %w", err)
	}
	if len(requestBytes) > MaxRequestBytes {
		return PluginResponse{}, fmt.Errorf("plugin request exceeds %d bytes", MaxRequestBytes)
	}

	p.mu.Lock()
	if p.closed {
		err := p.exitErr
		p.mu.Unlock()
		if err == nil {
			err = ErrPluginProcessClosed
		}
		return PluginResponse{}, err
	}
	p.nextID++
	id := strconv.FormatUint(p.nextID, 10)
	resultCh := make(chan processResult, 1)
	p.pending[id] = resultCh
	p.mu.Unlock()

	frame, err := json.Marshal(processFrame{Type: "request", ID: id, Request: requestBytes})
	if err != nil {
		p.removePending(id)
		return PluginResponse{}, fmt.Errorf("encode executable plugin frame: %w", err)
	}
	p.writeMu.Lock()
	writeErr := writeProcessLine(p.stdin, frame)
	p.writeMu.Unlock()
	if writeErr != nil {
		p.removePending(id)
		p.fail(fmt.Errorf("%w: %v", ErrPluginProcessExited, writeErr))
		p.mu.Lock()
		processErr := p.exitErr
		p.mu.Unlock()
		if processErr == nil {
			processErr = writeErr
		}
		return PluginResponse{}, processErr
	}

	callCtx, cancel := context.WithTimeout(ctx, maxExecution)
	defer cancel()
	select {
	case result := <-resultCh:
		return result.response, result.err
	case <-callCtx.Done():
		p.removePending(id)
		return PluginResponse{}, callCtx.Err()
	case <-p.done:
		p.removePending(id)
		p.mu.Lock()
		err := p.exitErr
		p.mu.Unlock()
		if err == nil {
			err = ErrPluginProcessExited
		}
		return PluginResponse{}, err
	}
}

func (p *executablePlugin) signalReady(err error) {
	p.readyOne.Do(func() {
		p.ready <- err
	})
}

func (p *executablePlugin) finish(id string, result processResult) {
	p.mu.Lock()
	resultCh := p.pending[id]
	delete(p.pending, id)
	p.mu.Unlock()
	if resultCh != nil {
		resultCh <- result
	}
}

func (p *executablePlugin) removePending(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *executablePlugin) fail(err error) {
	p.doneOne.Do(func() {
		p.mu.Lock()
		p.closed = true
		if p.exitErr == nil || errors.Is(p.exitErr, ErrPluginProcessClosed) {
			p.exitErr = err
		}
		pending := p.pending
		p.pending = make(map[string]chan processResult)
		p.mu.Unlock()
		p.signalReady(err)
		close(p.done)
		for _, resultCh := range pending {
			resultCh <- processResult{err: err}
		}
	})
}
