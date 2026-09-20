package serverplugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNonReadingPluginChild(t *testing.T) {
	if os.Getenv("OSS_PLUGIN_ID") != "non-reading-child" {
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(processFrame{Type: "ready", APIVersion: executableProtocolVersion}); err != nil {
		os.Exit(2)
	}
	time.Sleep(time.Hour)
	os.Exit(0)
}

func TestCloseInterruptsBlockedExecutableWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.exe"), readTestBinary(t), 0700); err != nil {
		t.Fatal(err)
	}
	mf := Manifest{ID: "non-reading-child", Name: "Non reading", Version: "1.0.0", APIVersion: 1, Runtime: RuntimeExecutable, Entrypoints: map[string]string{"any": "plugin.exe"}, Args: []string{"-test.run=^TestNonReadingPluginChild$"}}
	p, err := startExecutablePlugin(t.Context(), dir, mf, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.kill() })
	p.writeOnce.Do(func() { p.writeGate = make(chan struct{}, 1) })
	pending := make(chan error, 1)
	go func() {
		_, err := p.Invoke(context.Background(), PluginRequest{Method: "POST", Path: "/block", BodyBase64: strings.Repeat("a", 128*1024)})
		pending <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(p.writeGate) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Invoke did not acquire the pipe writer")
		}
		time.Sleep(time.Millisecond)
	}
	// Close 需同时覆盖写门限、关闭帧与进程等待。
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- p.Close(ctx) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on plugin stdin")
	}
	select {
	case <-pending:
	case <-time.After(time.Second):
		t.Fatal("Invoke remained blocked after Close")
	}
	select {
	case <-p.waitDone:
	case <-time.After(time.Second):
		t.Fatal("child process was not reaped")
	}
}

func TestQueuedWriteHonorsCancellationWithoutBreakingProcess(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	p := &executablePlugin{stdin: wr, pending: map[string]chan processResult{}, done: make(chan struct{}), ready: make(chan error, 1)}
	p.writeOnce.Do(func() { p.writeGate = make(chan struct{}, 1) })
	p.writeGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = p.Invoke(ctx, PluginRequest{Method: "GET", Path: "/queued"})
	<-p.writeGate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.Healthy() {
		t.Fatal("canceled queued write broke an otherwise healthy process")
	}
}
