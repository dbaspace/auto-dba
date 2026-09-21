package localcommand

import (
	"syscall"
	"time"
)

type Option func(*LocalCommand)

func WithCleanInputs(commands []string) Option {
	return func(lcmd *LocalCommand) {
		lcmd.cleanInputs = commands
	}
}

func WithCleanCommand(cmd string, args []string) Option {
	return func(lcmd *LocalCommand) {
		lcmd.cleanCommand = cmd
		lcmd.cleanArgs = args
	}
}

func WithCloseSignal(signal syscall.Signal) Option {
	return func(lcmd *LocalCommand) {
		lcmd.closeSignal = signal
	}
}

func WithEnvs(envs map[string]string) Option {
	return func(lcmd *LocalCommand) {
		lcmd.envs = envs
	}
}

func WithCloseTimeout(timeout time.Duration) Option {
	return func(lcmd *LocalCommand) {
		lcmd.closeTimeout = timeout
	}
}
