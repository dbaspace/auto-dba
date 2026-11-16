package conn

import (
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// ReadIdle 读超时。约为数个 Ping 间隔，避免一次 Pong 稍晚就把正在看输出的会话拆掉。
const ReadIdle = 90 * time.Second

type wsWrapper struct {
	*websocket.Conn
	l sync.Mutex
}

// WrapperWs wraps a websocket connection.
func WrapperWs(c *websocket.Conn) *Conn {
	return Wrapper(&wsWrapper{c, sync.Mutex{}})
}

// Write writes data to the connection.
func (wsw *wsWrapper) Write(p []byte) (n int, err error) {
	wsw.l.Lock()
	defer wsw.l.Unlock()
	writer, err := wsw.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	n, err = writer.Write(p)
	if err == nil && n > 0 {
		_ = wsw.Conn.SetReadDeadline(time.Now().Add(ReadIdle))
	}
	return n, err
}

// Read reads data from the connection.
func (wsw *wsWrapper) Read(p []byte) (n int, err error) {
	for {
		msgType, reader, err := wsw.Conn.NextReader()
		if err != nil {
			return 0, err
		}
		// 有业务帧也刷新读超时，配合服务端协议层 Ping 和下行输出
		_ = wsw.Conn.SetReadDeadline(time.Now().Add(ReadIdle))

		if msgType != websocket.TextMessage {
			continue
		}

		b, err := io.ReadAll(reader)
		if len(b) > len(p) {
			return 0, errors.Wrapf(err, "Client message exceeded buffer size")
		}
		n = copy(p, b)
		return n, err
	}
}
