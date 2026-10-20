package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// LogOpts Pod 日志选项。
type LogOpts struct {
	Container  string
	TailLines  int64
	Follow     bool
	Previous   bool
	Timestamps bool
}

// GetPodLogs 拉取日志（非 follow）。
func (c *Client) GetPodLogs(ctx context.Context, ns, name string, opts LogOpts) (string, error) {
	req := c.Clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     false,
		Previous:   opts.Previous,
		Timestamps: opts.Timestamps,
	})
	if opts.TailLines > 0 {
		req = c.Clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{
			Container:  opts.Container,
			Follow:     false,
			Previous:   opts.Previous,
			Timestamps: opts.Timestamps,
			TailLines:  &opts.TailLines,
		})
	}
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	b, err := io.ReadAll(io.LimitReader(stream, 4<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// StreamPodLogsWS 将 follow 日志写到 WebSocket。
func (c *Client) StreamPodLogsWS(ginCtx *gin.Context, ns, name string, opts LogOpts) error {
	conn, err := wsUpgrader.Upgrade(ginCtx.Writer, ginCtx.Request, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	tail := opts.TailLines
	if tail <= 0 {
		tail = 200
	}
	req := c.Clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{
		Container:  opts.Container,
		Follow:     true,
		Previous:   opts.Previous,
		Timestamps: opts.Timestamps,
		TailLines:  &tail,
	})
	ctx, cancel := context.WithCancel(ginCtx.Request.Context())
	defer cancel()
	stream, err := req.Stream(ctx)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("ERROR: "+err.Error()))
		return err
	}
	defer stream.Close()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err != io.EOF {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("\nERROR: "+err.Error()))
			}
			return nil
		}
	}
}

// ExecPodWS Pod 终端（WebSocket ↔ SPDY）。
func (c *Client) ExecPodWS(ginCtx *gin.Context, ns, name, container string, command []string) error {
	if len(command) == 0 {
		command = []string{"/bin/sh", "-c", "exec /bin/bash || exec /bin/sh"}
	}
	conn, err := wsUpgrader.Upgrade(ginCtx.Writer, ginCtx.Request, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", req.URL())
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("ERROR: "+err.Error()))
		return err
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	go func() {
		defer stdinW.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) == 0 {
				continue
			}
			// 简单协议：首字节 'r' 表示 resize，其余为 stdin
			if msg[0] == 'r' {
				continue
			}
			if msg[0] == '0' {
				msg = msg[1:]
			}
			if _, err := stdinW.Write(msg); err != nil {
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(ginCtx.Request.Context(), 2*time.Hour)
	defer cancel()

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdinR,
		Stdout: stdoutW,
		Stderr: stdoutW,
		Tty:    true,
	})
	_ = stdoutW.Close()
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n[exec ended: %v]\r\n", err)))
	}
	return nil
}
