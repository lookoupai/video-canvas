//go:build !unix

package reference

import (
	"os/exec"
	"time"
)

// configureCommand 限制分析器取消后等待管道关闭的时间。
func configureCommand(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
