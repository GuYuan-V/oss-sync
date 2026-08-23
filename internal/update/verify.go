// 更新校验
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// checkExecutableMagic 快速校验文件头是否符合目标平台的可执行格式。
func checkExecutableMagic(path, goos string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 4)
	n, err := io.ReadFull(f, head)
	if n < 2 {
		if err != nil {
			return err
		}
		return errors.New("文件过小，不是可执行文件")
	}
	switch goos {
	case "windows":
		if head[0] != 'M' || head[1] != 'Z' {
			return errors.New("不是 Windows PE 可执行文件")
		}
	case "linux":
		if head[0] != 0x7f || head[1] != 'E' || head[2] != 'L' || head[3] != 'F' {
			return errors.New("不是 Linux ELF 可执行文件")
		}
	case "darwin":
		if !isMachOMagic(head) {
			return errors.New("不是 macOS Mach-O 可执行文件")
		}
	}
	return nil
}

func isMachOMagic(head []byte) bool {
	switch {
	case head[0] == 0xfe && head[1] == 0xed && head[2] == 0xfa && head[3] == 0xce: // 32 位大端
	case head[0] == 0xce && head[1] == 0xfa && head[2] == 0xed && head[3] == 0xfe: // 32 位小端
	case head[0] == 0xfe && head[1] == 0xed && head[2] == 0xfa && head[3] == 0xcf: // 64 位大端
	case head[0] == 0xcf && head[1] == 0xfa && head[2] == 0xed && head[3] == 0xfe: // 64 位小端
	case head[0] == 0xca && head[1] == 0xfe && head[2] == 0xba && head[3] == 0xbe: // 胖二进制大端
	default:
		return false
	}
	return true
}

// defaultVerifier 校验下载的二进制：格式正确且能用 --version 启动。
func defaultVerifier(path, wantVersion string) error {
	if err := checkExecutableMagic(path, runtime.GOOS); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("新二进制无法启动（--version 退出失败: %v）", err)
	}
	got := strings.TrimSpace(string(out))
	if got == "" {
		return errors.New("新二进制 --version 输出为空")
	}
	if wantVersion != "" && !strings.Contains(got, wantVersion) {
		return fmt.Errorf("新二进制版本 %q 与目标版本 %q 不一致", got, wantVersion)
	}
	return nil
}

// copyFile 复制文件并保留执行权限位。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncErr
}

// swapBinary 用新文件原子替换目标文件；目标被占用时先让位再替换，失败时回滚。
func swapBinary(prepared, target string) error {
	if err := os.Rename(prepared, target); err == nil {
		return nil
	}
	// Windows 上运行中的进程会锁定可执行文件：先把它移走再放入新文件。
	aside := target + ".old"
	if err := os.Rename(target, aside); err != nil {
		return err
	}
	if err := os.Rename(prepared, target); err != nil {
		_ = os.Rename(aside, target) // 回滚：恢复原二进制
		return err
	}
	_ = os.Remove(aside)
	return nil
}

