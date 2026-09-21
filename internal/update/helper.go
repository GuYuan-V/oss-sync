package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/helantianshen/oss-sync/internal/version"
)

// RunHelper 为 helper 进程入口，直接以进程退出码结束而不正常返回
// main.go 中的调用方应执行 os.Exit(RunHelper())
func RunHelper(markerPath string) int {
	if markerPath == "" {
		fmt.Fprintln(os.Stderr, "helper: marker path empty")
		return 0
	}
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read marker %s: %v (ordinary startup, no rollback)\n", markerPath, err)
		return 0
	}
	var m HandoffMarker
	if err := json.Unmarshal(markerData, &m); err != nil {
		fmt.Fprintf(os.Stderr, "helper: corrupt marker: %v\n", err)
		return 2
	}
	// 校验标记引用的操作仍活跃，非活跃视为常规启动，不回滚
	_, op, err := recoverActiveMarker(markerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: no active marker, skipping: %v\n", err)
		// 无有效标记时不回滚，直接退出
		return 0
	}
	// 变更前先校验路径安全性、digest 与目标版本
	if err := validateMarkerSafe(&m); err != nil {
		fmt.Fprintf(os.Stderr, "helper: unsafe marker: %v\n", err)
		return 2
	}
	// 变更前先等待父进程退出
	parentPID := m.ParentPID
	if parentPID > 0 {
		if err := waitForParentFn(parentPID, 10*time.Second); err != nil {
			_ = recordRollback(&m, fmt.Sprintf("parent wait failed: %v", err))
			return 3
		}
	}

	// 替换前复核暂存文件的存在性、digest、魔数与版本
	if err := verifyStagedFileFn(m.StagedPath, m.Digest, m.TargetVersion); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("staged verification failed: %v", err))
		return 4
	}
	// 替换前确认备份存在，回滚依赖该备份
	if _, err := os.Stat(m.BackupPath); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("backup missing: %v", err))
		return 4
	}

	// 父进程退出后执行原子替换
	if err := atomicReplaceFn(m.StagedPath, m.ExecPath); err != nil {
		_ = recordRollback(&m, fmt.Sprintf("swap failed: %v", err))
		return 5
	}

	// 按原始参数、环境与工作目录拉起新二进制
	child, err := startNewServerFn(&m)
	if err != nil {
		// 替换成功但拉起失败，回滚
		_ = recordRollback(&m, fmt.Sprintf("launch new binary failed: %v", err))
		return 6
	}

	// 探测 /readyz，要求 ready 为真且版本精确匹配
	if err := probeReadyzWithVersionFn(m.ReadyURL, m.TargetVersion, 30*time.Second, 500*time.Millisecond); err != nil {
		// 子进程仍存活时先终止
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
		}
		_ = recordRollback(&m, fmt.Sprintf("readiness/version check failed: %v", err))
		return 7
	}
	// 就绪后子进程可能已快速退出，此处复核
	if child != nil && child.Process != nil {
		done := make(chan error, 1)
		go func() { _, e := child.Process.Wait(); done <- e }()
		select {
		case err := <-done:
			if err != nil {
				_ = recordRollback(&m, fmt.Sprintf("child exited after ready: %v", err))
				return 7
			}
		case <-time.After(200 * time.Millisecond):
			// 仍在运行视为成功，分离后返回
			_ = child.Process.Release()
		}
	}
	// 探测通过即表示版本已匹配
	_ = op
	_ = recordSuccess(&m)
	return 0
}

var startNewServerFn = startNewServer

func SetStartNewServerFn(fn func(*HandoffMarker) (*exec.Cmd, error)) {
	if fn == nil {
		startNewServerFn = startNewServer
	} else {
		startNewServerFn = fn
	}
}
func SetProbeReadyzWithVersionFn(fn func(string, string, time.Duration, time.Duration) error) {
	if fn == nil {
		probeReadyzWithVersionFn = probeReadyzWithVersion
	} else {
		probeReadyzWithVersionFn = fn
	}
}

func startNewServer(m *HandoffMarker) (*exec.Cmd, error) {
	args := m.OrigArgs
	if len(args) == 0 {
		args = []string{m.ExecPath}
	}
	// 首个元素为程序名，exec.Command 只需其后的参数
	childArgs := []string{}
	if len(args) > 1 {
		childArgs = args[1:]
	}
	cmd := exec.Command(m.ExecPath, childArgs...)
	cmd.Env = os.Environ()
	if m.WorkDir != "" {
		cmd.Dir = m.WorkDir
	}
	// 继承标准输出，避免启动初期失败无声，helper 日志不受影响
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

var probeReadyzWithVersionFn = probeReadyzWithVersion

func probeReadyzWithVersion(readyURL, wantVersion string, timeout, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	wantNorm := version.Normalize(wantVersion)
	var lastErr error
	for {
		ready, ver, err := readyzOnceWithVersion(client, readyURL)
		if err == nil && ready {
			if wantNorm == "" || version.Normalize(ver) == wantNorm {
				return nil
			}
			lastErr = fmt.Errorf("ready but version mismatch: got %q (normalized %q) want %q (normalized %q)", ver, version.Normalize(ver), wantVersion, wantNorm)
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("not ready (version %q)", ver)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(pollInterval)
		if time.Now().After(deadline) {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout after %s", timeout)
	}
	return fmt.Errorf("readyz probe failed: %w", lastErr)
}

func readyzOnceWithVersion(client *http.Client, url string) (ready bool, version string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var payload struct {
		Ready   bool   `json:"ready"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(body, &payload)
	if resp.StatusCode != http.StatusOK {
		return false, payload.Version, fmt.Errorf("readyz %d", resp.StatusCode)
	}
	if !payload.Ready {
		return false, payload.Version, fmt.Errorf("ready=false")
	}
	return true, payload.Version, nil
}
