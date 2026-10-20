// k8s-agent：在目标集群内运行，主动连回 KeePlus 建立反向隧道（骨架）。
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
)

func main() {
	var (
		platformURL = flag.String("platform", env("KEEPLUS_PLATFORM_URL", "ws://127.0.0.1:8800"), "平台地址，如 ws://host:8800")
		agentID     = flag.String("agent-id", env("KEEPLUS_AGENT_ID", ""), "Agent ID")
		token       = flag.String("token", env("KEEPLUS_AGENT_TOKEN", ""), "注册 Token（可选）")
	)
	flag.Parse()
	if *agentID == "" {
		log.Fatal("agent-id 必填")
	}

	u, err := url.Parse(*platformURL)
	if err != nil {
		log.Fatal(err)
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	u.Path = "/api/v1/k8s/agents/" + *agentID + "/tunnel"
	q := u.Query()
	if *token != "" {
		q.Set("token", *token)
	}
	u.RawQuery = q.Encode()

	log.Printf("k8s-agent connecting to %s", u.String())
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	header := http.Header{}
	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	log.Printf("connected; waiting for tunnel frames (stub keepalive)")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
				log.Printf("ping: %v", err)
				return
			}
			time.Sleep(20 * time.Second)
		}
	}()
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Printf("read: %v", err)
				close(stop)
				return
			}
			log.Printf("recv: %s", string(msg))
			// 完整实现应解析 HTTP 请求并通过 in-cluster config 转发到 API Server
		}
	}()

	<-stop
	log.Printf("k8s-agent exiting")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
