package k8s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// AgentHub 管理 k8s-agent 反向隧道（非 SSH）。
// 模式对齐 Rancher remotedialer：Agent 主动连出到平台，平台经隧道访问集群 API Server。
// 因部分环境拉不到 github.com/rancher/remotedialer，此处实现兼容的 HTTP-over-WebSocket 帧协议；
// 后续网络允许时可直接替换为 remotedialer.Server/Client。
type AgentHub struct {
	mu       sync.RWMutex
	agents   map[string]*agentSession
	upgrader websocket.Upgrader
}

type agentSession struct {
	id     string
	conn   *websocket.Conn
	mu     sync.Mutex
	nextID uint64
	wait   map[uint64]chan *tunnelResp
}

type tunnelReq struct {
	ID     uint64              `json:"id"`
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header,omitempty"`
	Body   []byte              `json:"body,omitempty"`
}

type tunnelResp struct {
	ID         uint64              `json:"id"`
	StatusCode int                 `json:"status"`
	Header     map[string][]string `json:"header,omitempty"`
	Body       []byte              `json:"body,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func NewAgentHub() *AgentHub {
	return &AgentHub{
		agents: make(map[string]*agentSession),
		upgrader: websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  1 << 20,
			WriteBufferSize: 1 << 20,
		},
	}
}

// ServeAgentWS Agent 注册入口。
func (h *AgentHub) ServeAgentWS(w http.ResponseWriter, r *http.Request, agentID string) error {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	sess := &agentSession{id: agentID, conn: conn, wait: map[uint64]chan *tunnelResp{}}
	h.mu.Lock()
	if old, ok := h.agents[agentID]; ok {
		_ = old.conn.Close()
	}
	h.agents[agentID] = sess
	h.mu.Unlock()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			h.mu.Lock()
			if h.agents[agentID] == sess {
				delete(h.agents, agentID)
			}
			h.mu.Unlock()
			_ = conn.Close()
			return err
		}
		var resp tunnelResp
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		sess.mu.Lock()
		ch := sess.wait[resp.ID]
		if ch != nil {
			delete(sess.wait, resp.ID)
			select {
			case ch <- &resp:
			default:
			}
		}
		sess.mu.Unlock()
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
		return nil, fmt.Errorf("k8s-agent not connected: %s（反向隧道，非 SSH）", t.agentID)
	}
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = b
	}
	id := atomic.AddUint64(&sess.nextID, 1)
	ch := make(chan *tunnelResp, 1)
	sess.mu.Lock()
	sess.wait[id] = ch
	msg, _ := json.Marshal(tunnelReq{
		ID: id, Method: req.Method, URL: req.URL.String(), Header: req.Header, Body: body,
	})
	_ = sess.conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	err := sess.conn.WriteMessage(websocket.TextMessage, msg)
	sess.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("%s", resp.Error)
		}
		hr := &http.Response{
			StatusCode: resp.StatusCode,
			Status:     http.StatusText(resp.StatusCode),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(resp.Body)),
			Request:    req,
		}
		for k, vs := range resp.Header {
			for _, v := range vs {
				hr.Header.Add(k, v)
			}
		}
		return hr, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-time.After(60 * time.Second):
		sess.mu.Lock()
		delete(sess.wait, id)
		sess.mu.Unlock()
		return nil, fmt.Errorf("k8s-agent tunnel timeout")
	}
}

// Connected 是否在线。
func (h *AgentHub) Connected(agentID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.agents[agentID]
	return ok
}

// --- Agent 侧辅助：解析并转发到本地 API Server ---

// ServeTunnelClient Agent 进程内：读平台请求并转发。
func ServeTunnelClient(conn *websocket.Conn, local http.RoundTripper) error {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var req tunnelReq
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		if req.Method == "" && len(req.Body) == 0 {
			// ping
			continue
		}
		httpReq, err := http.NewRequest(req.Method, req.URL, bytes.NewReader(req.Body))
		if err != nil {
			_ = writeTunnelResp(conn, &tunnelResp{ID: req.ID, Error: err.Error()})
			continue
		}
		for k, vs := range req.Header {
			for _, v := range vs {
				httpReq.Header.Add(k, v)
			}
		}
		resp, err := local.RoundTrip(httpReq)
		if err != nil {
			_ = writeTunnelResp(conn, &tunnelResp{ID: req.ID, Error: err.Error()})
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		tr := &tunnelResp{ID: req.ID, StatusCode: resp.StatusCode, Header: resp.Header, Body: b}
		_ = writeTunnelResp(conn, tr)
	}
}

func writeTunnelResp(conn *websocket.Conn, r *tunnelResp) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, b)
}

