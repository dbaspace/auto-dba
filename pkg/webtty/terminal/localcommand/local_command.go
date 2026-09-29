package localcommand

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/pkg/errors"
)

const (
	DefaultCloseSignal  = syscall.SIGINT
	DefaultCloseTimeout = 10 * time.Second
)

// LocalCommand represents a local command.
type LocalCommand struct {
	command string
	argv    []string
	envs    map[string]string

	closeSignal  syscall.Signal
	closeTimeout time.Duration

	cleanInputs  []string
	cleanCommand string
	cleanArgs    []string

	cmd *exec.Cmd
	pty *os.File

	l         sync.Mutex
	closeFlag bool

	logger *slog.Logger
}

// New creates a new instance of LocalCommand.
func New(ctx context.Context, command string, argv []string, options ...Option) (*LocalCommand, error) {
	cmd := exec.Command(command, argv...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	lcmd := &LocalCommand{
		command: command,
		argv:    argv,
		envs:    make(map[string]string),

		closeSignal:  DefaultCloseSignal,
		closeTimeout: DefaultCloseTimeout,

		cmd: cmd,
	}

	for _, option := range options {
		option(lcmd)
	}

	for k, v := range lcmd.envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	pty, err := startWithPty(cmd)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to start command `%s`", command)
	}
	lcmd.pty = pty

	if lcmd.cmd.Process == nil {
		return nil, errors.Errorf("failed to get process of command `%s`", command)
	}
	pid := lcmd.cmd.Process.Pid
	lcmd.logger = slog.With("command", lcmd.command, "argv", lcmd.argv, "pid", pid)

	// When the process is closed by the user,
	// close pty so that Read() on the pty breaks with an EOF.
	go func() {
		defer lcmd.Close()

		_ = lcmd.cmd.Wait()
		lcmd.logger.Debug("command exited")
	}()

	go func() {
		<-ctx.Done()
		lcmd.logger.Debug("context done")
		lcmd.Close()
	}()

	return lcmd, nil
}

func (lcmd *LocalCommand) CleanCommand() {
	if lcmd.cleanCommand == "" {
		return
	}
	lcmd.logger.Debug("cleaning command", "command", lcmd.cleanCommand, "args", lcmd.cleanArgs)
	cmd := exec.Command(lcmd.cleanCommand, lcmd.cleanArgs...) // nolint:gosec
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	for k, v := range lcmd.envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		lcmd.logger.Warn("failed to clean command", "error", err, "output", string(out))
		return
	}
	lcmd.logger.Debug("cleaned command result", "output", string(out))
}

func (lcmd *LocalCommand) CleanInputs() {
	for _, input := range lcmd.cleanInputs {
		input = strings.TrimSpace(input)
		lcmd.logger.Debug("sending input to command", "input", input)
		if _, err := lcmd.pty.Write([]byte(fmt.Sprintf("\n%s\n", input))); err != nil {
			if strings.Contains(err.Error(), "input/output error") {
				lcmd.logger.Debug("pyt is closed")
				return
			}
			lcmd.logger.Warn("sending input to command", "input", input, "error", err)
		}
		if input == "exit" {
			time.Sleep(lcmd.closeTimeout)
		}
	}
}

func (lcmd *LocalCommand) Read(p []byte) (n int, err error) {
	return lcmd.pty.Read(p)
}

func (lcmd *LocalCommand) Write(p []byte) (n int, err error) {
	return lcmd.pty.Write(p)
}

func (lcmd *LocalCommand) Close() {
	go func() {
		lcmd.l.Lock()
		defer lcmd.l.Unlock()

		if lcmd.closeFlag {
			return
		}

		lcmd.CleanInputs()
		lcmd.CleanCommand()
		lcmd.close()
		_ = lcmd.pty.Close()

		lcmd.closeFlag = true
	}()
}

func (lcmd *LocalCommand) close() {
	lcmd.logger.Debug("sending close signal to command", "signal", lcmd.closeSignal)
	if err := lcmd.cmd.Process.Signal(lcmd.closeSignal); err != nil {
		if strings.Contains(err.Error(), "os: process already finished") {
			lcmd.logger.Debug("process already finished")
			return
		}
		lcmd.logger.Warn("send close signal to command", "error", err)
		lcmd.kill()
		return
	}

	time.Sleep(lcmd.closeTimeout)
	lcmd.kill()
}

func (lcmd *LocalCommand) kill() {
	lcmd.logger.Debug("sending kill signal to command", "pid", lcmd.cmd.Process.Pid)
	if err := lcmd.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		if strings.Contains(err.Error(), "os: process already finished") {
			lcmd.logger.Debug("process already finished")
			return
		}
		lcmd.logger.Warn("send kill signal to command", "error", err)
	}
}

func (lcmd *LocalCommand) WindowTitleVariables() map[string]interface{} {
	return map[string]interface{}{
		"command": lcmd.command,
		"argv":    lcmd.argv,
		"pid":     lcmd.cmd.Process.Pid,
	}
}

func (lcmd *LocalCommand) ResizeTerminal(width int, height int) error {
	window := struct {
		row uint16
		col uint16
		x   uint16
		y   uint16
	}{
		uint16(height),
		uint16(width),
		0,
		0,
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		lcmd.pty.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(&window)),
	)
	if errno != 0 {
		return errno
	} else {
		return nil
	}
}
