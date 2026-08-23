//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

func detachHelper(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
