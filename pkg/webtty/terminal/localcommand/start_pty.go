package localcommand

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// startWithPty 启动带 PTY 的子进程。
// 不用 creack/pty.Start：v1.1.7 把父进程 tty fd 写进 Ctty，在 Go 1.15+ 会报
// "Setctty set but Ctty not valid in child"。Stdin=tty 时 Ctty 应为子进程 fd 0。
func startWithPty(c *exec.Cmd) (*os.File, error) {
	p, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close()

	if c.Stdout == nil {
		c.Stdout = tty
	}
	if c.Stderr == nil {
		c.Stderr = tty
	}
	if c.Stdin == nil {
		c.Stdin = tty
	}
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
	c.SysProcAttr.Setctty = true
	c.SysProcAttr.Ctty = 0

	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}
