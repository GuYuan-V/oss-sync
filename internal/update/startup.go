// 启动更新
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// ErrRollbackRestart 表示健康检查未通过、已回滚到备份二进制，
// 需要重启进程（用回滚后的旧版本二进制）完成闭环。
var ErrRollbackRestart = errors.New("健康检查未通过，已回滚到备份二进制，需要重启服务")

// CheckReady 轮询 /readyz 直到返回 ready:true 或超过 timeout。
// 每次请求返回 200 且 JSON 的 ready 为 true 视为就绪；连接失败、
// 非 200 或 ready:false 均视为未就绪并继续重试。上下文取消时立即返回。
func CheckReady(ctx context.Context, url string, pollInterval, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for {
		ready, err := readyOnce(ctx, client, url)
		switch {
		case err == nil && ready:
			return nil
		case err != nil:
			lastErr = err
		default:
			lastErr = errors.New("readyz 返回未就绪")
		}
		log.Printf("[OSS] readyz 未就绪: %v", lastErr)
		if time.Now().After(deadline) {
			break
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("健康检查超时（%s）: %w", timeout, lastErr)
}

func readyOnce(ctx context.Context, client *http.Client, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload struct {
		Ready bool `json:"ready"`
	}
	_ = json.Unmarshal(body, &payload)
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("readyz 返回 HTTP %d", resp.StatusCode)
	}
	if !payload.Ready {
		return false, errors.New("readyz ready 为 false")
	}
	return true, nil
}

// Rollback 用备份二进制（更新前留存，<exe>.bak）原子替换当前二进制。
// 备份不存在时返回错误，不修改任何文件。
func (u *Updater) Rollback() error {
	if _, err := os.Stat(u.backup); err != nil {
		return fmt.Errorf("备份二进制 %s 不可用: %w", u.backup, err)
	}
	if err := swapBinary(u.backup, u.exe); err != nil {
		return fmt.Errorf("回滚到备份二进制失败: %w", err)
	}
	return nil
}

// StartupHealthCheck 是重启后的自检闭环：
// 仅当存在“更新待验证”标记（<exe>.updated，由 Update 在替换成功后写入）
// 时运行，避免对未发生更新的普通启动造成影响。
//
// 流程：轮询 /readyz 直到 ready:true；超时或失败则原子回滚到备份二进制
// 并返回 ErrRollbackRestart，由调用方重启进程完成闭环；就绪则返回 nil。
// 上下文被取消（例如更新流程已触发重启信号）时返回错误但不回滚。
func (u *Updater) StartupHealthCheck(ctx context.Context, healthURL string, pollInterval, timeout time.Duration) error {
	marker := u.exe + ".updated"
	if _, err := os.Stat(marker); err != nil {
		return nil // 无待验证更新，不执行自检
	}
	_ = os.Remove(marker)

	if err := CheckReady(ctx, healthURL, pollInterval, timeout); err != nil {
		if ctx.Err() != nil {
			return err // 上下文被取消（如收到更新完成信号），不触发回滚
		}
		log.Printf("[OSS] 更新后健康检查未通过: %v", err)
		if rbErr := u.Rollback(); rbErr != nil {
			log.Printf("[OSS] 回滚失败: %v", rbErr)
			return fmt.Errorf("健康检查未通过（%v）且回滚失败: %w", err, rbErr)
		}
		log.Printf("[OSS] 已回滚到备份二进制 %s，等待重启后再次健康检查", u.backup)
		return ErrRollbackRestart
	}
	log.Printf("[OSS] 更新后健康检查通过，服务就绪")
	return nil
}

