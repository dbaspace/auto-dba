package conn

import (
	"encoding/base64"
	"errors"
	"io"
	"sync"
)

// Protocols defines the name of this protocol,
// which is supposed to be used to the subprotocol of Websockt streams.
var Protocols = []string{"webtty"}

const (
	UnknownInput   = '0' // Unknown message type, maybe sent by a bug
	Input          = '1' // User input typically from a keyboard
	Ping           = '2' // Ping to the server
	ResizeTerminal = '3' // Notify that the browser size has been changed
	SetEncoding    = '4' // Set encoding of the terminal
	JsonArgs       = '{' // Json encoded data
)

const (
	UnknownOutput  = '0' // Unknown message type, maybe set by a bug
	Output         = '1' // Normal output to the terminal
	Pong           = '2' // Pong to the browser
	SetWindowTitle = '3' // Set window title of the terminal
	SetPreferences = '4' // Set terminal preference
	SetReconnect   = '5' // Make terminal to reconnect
)

// Conn represents a web connect, usually it's a websocket connection.
type Conn struct {
	conn io.ReadWriter
	l    sync.Mutex
}

// Wrapper wraps a io.ReadWriter to a Conn.
func Wrapper(c io.ReadWriter) *Conn {
	return &Conn{conn: c}
}

// Write writes data to the connection.
func (c *Conn) Write(a byte, data []byte) (n int, err error) {
	switch a {
	case Output:
		safeMessage := base64.StdEncoding.EncodeToString(data)
		return c.conn.Write(append([]byte{a}, []byte(safeMessage)...))
	case SetWindowTitle, SetPreferences, SetReconnect:
		c.l.Lock()
		defer c.l.Unlock()
		return c.conn.Write(append([]byte{a}, data...))
	case Pong:
		return c.conn.Write([]byte{a})
	default:
		return 0, errors.New("unknown action")
	}
}

// Read reads data from the connection.
func (c *Conn) Read(bf []byte) (n int, err error) {
	return c.conn.Read(bf)
}
