package terminal_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/internal/terminal"
)

func TestOpenTerminalBridge(t *testing.T) {
	cfgPath := os.Getenv("AUTO_DBA_CONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join("..", "..", "config", "config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Terminal.Enabled {
		t.Skip("terminal disabled")
	}
	// ensure identity exists for local verify
	if _, err := os.Stat(cfg.Terminal.SSHIdentityFile); err != nil {
		home, _ := os.UserHomeDir()
		alt := filepath.Join(home, ".ssh", "id_rsa")
		if _, err2 := os.Stat(alt); err2 == nil {
			cfg.Terminal.SSHIdentityFile = alt
		} else {
			t.Skip("no ssh identity file")
		}
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	list, err := db.ListInstances("host", "")
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	var inst *store.Instance
	for i := range list {
		if !store.IsHostInstanceType(list[i].DBType) {
			continue
		}
		h := strings.TrimSpace(list[i].Host)
		if h == "" {
			h = strings.TrimSpace(list[i].DomainName)
		}
		if h != "" {
			inst = &list[i]
			break
		}
	}
	if inst == nil {
		t.Skip("no host instance with Host")
	}

	h := &terminal.Handler{Store: db, Cfg: cfg, Secret: cfg.App.JWTSecret}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/instances/:id/open-terminal", func(c *gin.Context) {
		c.Set(middleware.CtxUsername, "verify-user")
		h.OpenInstance(c)
	})
	r.POST("/terminal", func(c *gin.Context) {
		c.Set(middleware.CtxUsername, "verify-user")
		h.Open(c)
	})
	r.GET("/terminal", h.Page)
	r.GET("/terminal/ws", h.WS)
	h.MountStatic(r)

	// open-terminal
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/instances/"+inst.ID+"/open-terminal", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("open-terminal status=%d body=%s", w.Code, w.Body.String())
	}
	var open struct {
		Data struct {
			Token       string `json:"token"`
			TerminalURL string `json:"terminal_url"`
			Host        string `json:"host"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &open); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if open.Data.Token == "" || open.Data.TerminalURL == "" {
		t.Fatalf("missing token/url: %+v", open)
	}
	t.Logf("host=%s url=%s", open.Data.Host, open.Data.TerminalURL)

	// page
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/terminal?token="+open.Data.Token, nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("page status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !bytes.Contains(w2.Body.Bytes(), []byte("gotty.js")) {
		t.Fatalf("page missing gotty.js")
	}

	// static
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/terminal/static/js/gotty.js", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != 200 || w3.Body.Len() == 0 {
		t.Fatalf("static gotty status=%d size=%d", w3.Code, w3.Body.Len())
	}

	// reject bad action
	w4 := httptest.NewRecorder()
	bad := `{"action_type":"command","action_name":"evil","args":{"host":"` + open.Data.Host + `"}}`
	req4 := httptest.NewRequest(http.MethodPost, "/terminal", bytes.NewReader([]byte(bad)))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4, req4)
	if w4.Code == 200 {
		t.Fatalf("expected reject bad action, got 200")
	}

	// missing instance
	w5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodPost, "/instances/does-not-exist/open-terminal", bytes.NewReader([]byte("{}")))
	req5.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w5, req5)
	if w5.Code == 200 {
		t.Fatalf("expected missing instance error")
	}
}
