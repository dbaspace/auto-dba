package k8s

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// AgentHub 管理 k8s-agent 反向隧道会话。
// Agent 主动连入平台 WebSocket，平台通过该连接转发到 API Server 的 HTTP 请求。
type AgentHub struct {
	mu      sync.RWMutex
	agents  map[string]*agentSession
	upgrader websocket.Upgrader
}

type agentSession struct {
	id   string
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewAgentHub() *AgentHub {
	return &AgentHub{
		agents: make(map[string]*agentSession),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// ServeAgentWS Agent 注册入口。
func (h *AgentHub) ServeAgentWS(w http.ResponseWriter, r *http.Request, agentID string) error {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	sess := &agentSession{id: agentID, conn: conn}
	h.mu.Lock()
	if old, ok := h.agents[agentID]; ok {
		_ = old.conn.Close()
	}
	h.agents[agentID] = sess
	h.mu.Unlock()

	// 保活读循环
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			h.mu.Lock()
			if h.agents[agentID] == sess {
				delete(h.agents, agentID)
			}
			h.mu.Unlock()
			_ = conn.Close()
			return err
		}
	}
}

// RoundTripper 返回经隧道转发的 Transport。
func (h *AgentHub) RoundTripper(agentID string) http.RoundTripper {
	return &agentTransport{hub: h, agentID: agentID}
}

type agentTransport struct {
	hub     *AgentHub
	agentID string
}

func (t *agentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.hub.mu.RLock()
	sess := t.hub.agents[t.agentID]
	t.hub.mu.RUnlock()
	if sess == nil {
		return nil, fmt.Errorf("k8s-agent not connected: %s", t.agentID)
	}
	// 简化协议：把 HTTP 请求序列化为 WebSocket binary 帧，期望 Agent 回写完整 HTTP 响应。
	// 生产环境建议换 remotedialer；此处提供可联调的骨架。
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_ = sess.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := sess.conn.WriteMessage(websocket.TextMessage, []byte("HTTP "+req.Method+" "+req.URL.String())); err != nil {
		return nil, err
	}
	// 骨架：返回 503，提示走 Agent 时需完整隧道实现
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Status:     "503 agent tunnel stub",
		Body:       io.NopCloser(nilReader{}),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type nilReader struct{}

func (nilReader) Read([]byte) (int, error) { return 0, io.EOF }

// Connected 是否在线。
func (h *AgentHub) Connected(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.agents[agentID]
	return ok
}
