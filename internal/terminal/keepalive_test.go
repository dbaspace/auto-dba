package terminal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestKeepAliveWSSendsPing(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		stop := keepAliveWS(ws, 50*time.Millisecond, time.Second)
		defer stop()
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	gotPing := make(chan struct{}, 1)
	ws.SetPingHandler(func(appData string) error {
		select {
		case gotPing <- struct{}{}:
		default:
		}
		return ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-gotPing:
	case <-time.After(time.Second):
		t.Fatal("expected websocket ping from keepAliveWS")
	}
}
