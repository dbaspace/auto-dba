package sshx

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

// Run 在远端执行命令，返回 stdout/stderr。
func (c *Client) Run(ctx context.Context, cmdline string) (stdout, stderr string, err error) {
	if c == nil || c.conn == nil {
		return "", "", fmt.Errorf("SSH 未连接")
	}
	session, err := c.conn.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("创建 SSH session 失败: %w", err)
	}
	defer session.Close()

	var outBuf, errBuf bytes.Buffer
	session.Stdout = &outBuf
	session.Stderr = &errBuf

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmdline)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return outBuf.String(), errBuf.String(), fmt.Errorf("SSH 命令超时或取消: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			return outBuf.String(), errBuf.String(), fmt.Errorf("远端命令失败: %w; stderr=%s", err, trimErr(errBuf.String()))
		}
		return outBuf.String(), errBuf.String(), nil
	}
}

// RunTimeout 带超时的 Run。
func (c *Client) RunTimeout(timeout time.Duration, cmdline string) (stdout, stderr string, err error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Run(ctx, cmdline)
}

// RunEscalated 按 mode 提权后执行（见 WrapEscalate）。
func (c *Client) RunEscalated(ctx context.Context, escalate, cmdline string) (stdout, stderr string, err error) {
	wrapped := WrapEscalate(cmdline, escalate)
	out, errOut, err := c.Run(ctx, wrapped)
	if err != nil && NormalizeEscalate(escalate) != EscalateNone {
		err = fmt.Errorf("%w%s", err, EscalateHint(escalate))
	}
	return out, errOut, err
}

func trimErr(s string) string {
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}
