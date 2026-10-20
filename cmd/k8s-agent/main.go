// k8s-agent：集群内反向隧道客户端（非 SSH）。
// Agent 主动连回 KeePlus，平台经隧道访问本集群 API Server（对齐 remotedialer 模式）。
package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/rest"

	"auto-dba/internal/k8s"
)

func main() {
	var (
		platformURL = flag.String("platform", env("KEEPLUS_PLATFORM_URL", "ws://127.0.0.1:8800"), "平台 WebSocket 地址")
		agentID     = flag.String("agent-id", env("KEEPLUS_AGENT_ID", ""), "Agent ID（与平台集群 agent_id 一致）")
		token       = flag.String("token", env("KEEPLUS_AGENT_TOKEN", ""), "可选注册 Token")
	)
	flag.Parse()
	if *agentID == "" {
		log.Fatal("agent-id 必填")
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("in-cluster config: %v（请在 Pod 内以 ServiceAccount 运行）", err)
	}
	baseRT, err := rest.TransportFor(cfg)
	if err != nil {
		log.Fatalf("transport: %v", err)
	}
	localHost := cfg.Host
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// 将平台发来的 URL 改写为集群内 API Server
		u, err := url.Parse(localHost)
		if err != nil {
			return nil, err
		}
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.Host = u.Host
		return baseRT.RoundTrip(req)
	})

	u, err := url.Parse(*platformURL)
	if err != nil {
		log.Fatal(err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "/api/v1/k8s/agents/" + *agentID + "/tunnel"
	q := u.Query()
	if *token != "" {
		q.Set("token", *token)
	}
	u.RawQuery = q.Encode()

	log.Printf("k8s-agent reverse-tunnel connecting to %s", u.String())
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	log.Printf("connected; forwarding to API Server %s via reverse tunnel (not SSH)", localHost)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() {
		errCh <- k8s.ServeTunnelClient(conn, rt)
	}()
	select {
	case <-stop:
	case err := <-errCh:
		if err != nil {
			log.Printf("tunnel closed: %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
