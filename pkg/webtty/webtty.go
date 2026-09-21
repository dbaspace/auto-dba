package webtty

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	"auto-dba/pkg/webtty/conn"
	"auto-dba/pkg/webtty/terminal"
)

var (
	defaultBufferSize = 1024
)

// WebTTY bridges a PTY slave and its PTY master.
// To support text-based streams and side channel commands such as
// terminal resizing, WebTTY uses an original protocol.
type WebTTY struct {
	conn     *conn.Conn
	terminal terminal.Terminal

	windowTitle string
	permitWrite bool
	columns     int
	rows        int
	reconnect   int // in seconds
	masterPrefs []byte
	mtod        string

	decoder Decoder

	bufferSize int
}

// New creates a new instance of WebTTY.
// webConn is a connection to the PTY master,
// typically it's a websocket connection to a client.
// terminal is a PTY slave such as a local command with a PTY.
func New(conn *conn.Conn, terminal terminal.Terminal, options ...Option) (*WebTTY, error) {
	wt := &WebTTY{
		conn:     conn,
		terminal: terminal,

		permitWrite: false,
		columns:     0,
		rows:        0,

		decoder: &NullCodec{},

		bufferSize: defaultBufferSize,
	}

	for _, option := range options {
		if err := option(wt); err != nil {
			return nil, errors.Wrapf(err, "failed to apply option")
		}
	}

	return wt, nil
}

// Run starts the main process of the WebTTY.
// This method blocks until the context is canceled.
// Note that the master and slave are left intact even
// after the context is canceled. Closing them is caller's
// responsibility.
// If the connection to one end gets closed, returns ErrSlaveClosed or ErrMasterClosed.
func (wt *WebTTY) Run(ctx context.Context) error {
	defer wt.terminal.Close()

	err := wt.sendInitializeMessage()
	if err != nil {
		return errors.Wrapf(err, "failed to send initializing message")
	}

	errCh := make(chan error, 2)

	go func() {
		buffer := make([]byte, wt.bufferSize)
		for {
			n, err := wt.terminal.Read(buffer)
			if err != nil {
				errCh <- errors.Wrapf(err, "read from terminal")
				return
			}

			if n > 0 {
				_, err = wt.conn.Write(conn.Output, buffer[:n])
				if err != nil {
					errCh <- errors.Wrapf(err, "write to webConn")
					return
				}
			}
		}
	}()

	go func() {
		buffer := make([]byte, wt.bufferSize)
		for {
			n, err := wt.conn.Read(buffer)
			if err != nil {
				if strings.Contains(err.Error(), "websocket: close 1005") {
					slog.Info("websocket closed")
					errCh <- nil
					return
				}
				errCh <- errors.Wrapf(err, "read from webConn")
				return
			}

			if n > 0 {
				err = wt.handleWebConnReadEvent(buffer[:n])
				if err != nil {
					errCh <- errors.Wrapf(err, "handle webConn event")
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			if stderrors.Is(err, io.EOF) {
				return nil
			}
			if strings.Contains(err.Error(), "input/output error") {
				return nil
			}
		}
		return err
	}
}

func (wt *WebTTY) sendInitializeMessage() error {
	if wt.windowTitle != "" {
		_, err := wt.conn.Write(conn.SetWindowTitle, []byte(wt.windowTitle))
		if err != nil {
			return errors.Wrapf(err, "failed to send window title")
		}
	}

	if wt.mtod != "" {
		_, err := wt.conn.Write(conn.Output, []byte(wt.mtod))
		if err != nil {
			return errors.Wrapf(err, "failed to send message of the day")
		}
	}

	if wt.reconnect > 0 {
		reconnect, _ := json.Marshal(wt.reconnect)
		_, err := wt.conn.Write(conn.SetReconnect, reconnect)
		if err != nil {
			return errors.Wrapf(err, "failed to set reconnect")
		}
	}

	if wt.masterPrefs != nil {
		_, err := wt.conn.Write(conn.SetPreferences, wt.masterPrefs)
		if err != nil {
			return errors.Wrapf(err, "failed to set preferences")
		}
	}

	return nil
}

func (wt *WebTTY) handleWebConnReadEvent(data []byte) error {
	if len(data) == 0 {
		return errors.New("unexpected zero length read from master")
	}

	switch data[0] {
	case conn.Input:
		if !wt.permitWrite {
			return nil
		}

		if len(data) <= 1 {
			return nil
		}

		var decodedBuffer = make([]byte, len(data))
		n, err := wt.decoder.Decode(decodedBuffer, data[1:])
		if err != nil {
			return errors.Wrapf(err, "failed to decode received data from master")
		}

		_, err = wt.terminal.Write(decodedBuffer[:n])
		if err != nil {
			return errors.Wrapf(err, "failed to write received data to slave")
		}
	case conn.Ping:
		_, err := wt.conn.Write(conn.Pong, nil)
		if err != nil {
			return errors.Wrapf(err, "failed to return Pong message to master")
		}
	case conn.ResizeTerminal:
		return wt.resizeTerminal(data)
	case conn.SetEncoding:
		slog.Debug("received encoding string", "data", string(data))
		switch string(data[1:]) {
		case "base64":
			wt.decoder = base64.StdEncoding
		case "null":
			wt.decoder = NullCodec{}
		}
	case conn.JsonArgs:
		slog.Info("received json string", "data", string(data))
	default:
		return errors.Errorf("unknown message type `%c`, %s", data[0], data)
	}
	return nil
}

func (wt *WebTTY) resizeTerminal(data []byte) error {
	if wt.columns != 0 && wt.rows != 0 {
		return nil
	}

	if len(data) <= 1 {
		return errors.New("received malformed remote command for terminal resize: empty payload")
	}

	var args argResizeTerminal
	err := json.Unmarshal(data[1:], &args)
	if err != nil {
		return errors.Wrapf(err, "received malformed data for terminal resize")
	}
	rows := wt.rows
	if rows == 0 {
		rows = int(args.Rows)
	}

	columns := wt.columns
	if columns == 0 {
		columns = int(args.Columns)
	}

	err = wt.terminal.ResizeTerminal(columns, rows)
	if err != nil {
		return errors.Wrapf(err, "failed to resize terminal")
	}

	return nil
}

type argResizeTerminal struct {
	Columns float64
	Rows    float64
}
