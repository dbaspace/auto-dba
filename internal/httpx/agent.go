package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	sharedOnce      sync.Once
	sharedTransport *http.Transport
	longOnce        sync.Once
	longTransport   *http.Transport
)

// AgentTransport 返回控制面 → Agent 共用连接池（勿每次 New，否则 Idle KeepAlive 连接泄漏）。
func AgentTransport() *http.Transport {
	sharedOnce.Do(func() {
		sharedTransport = newAgentTransport(60 * time.Second)
	})
	return sharedTransport
}

func agentTransportLong() *http.Transport {
	longOnce.Do(func() {
		t := newAgentTransport(0)
		longTransport = t
	})
	return longTransport
}

func newAgentTransport(responseHeaderTimeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout, // 0 = 允许慢首包（RDB 流式）
	}
}

// AgentHTTP 短调用客户端（探活 / precheck / job 查询）。共享 Transport，按 timeout 克隆 Client。
func AgentHTTP(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: AgentTransport()}
}

// AgentHTTPLong 长调用客户端（备份/RDB 流；整体 deadline 由 ctx 或 Client.Timeout 控制）。
func AgentHTTPLong(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 6 * time.Hour
	}
	return &http.Client{Timeout: timeout, Transport: agentTransportLong()}
}
