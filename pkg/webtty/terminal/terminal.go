package terminal

import (
	"io"

	"auto-dba/pkg/webtty/terminal/localcommand"
)

var _ Terminal = &localcommand.LocalCommand{}

// Slave represents a PTY slave, typically it's a local command.
type Terminal interface {
	io.ReadWriter

	// WindowTitleVariables returns any values that can be used to fill out
	// the title of a terminal.
	WindowTitleVariables() map[string]any

	// ResizeTerminal sets a new size of the terminal.
	ResizeTerminal(columns int, rows int) error

	// Close the slave.
	Close()
}
