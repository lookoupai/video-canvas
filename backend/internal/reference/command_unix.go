//go:build unix

package reference

import (
	"os/exec"
	"syscall"
	"time"
)

// configureCommand 取消时终止分析器的整个进程组，包括它启动的 FFmpeg。
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
}
