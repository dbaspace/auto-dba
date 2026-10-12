package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

// 需能加载仓库 config（含 Nacos）；用于本地自测登录耗时，不作为 CI 硬门槛。
func TestLoginLatencyPlatformAdmin(t *testing.T) {
	if os.Getenv("AUTO_DBA_LOGIN_LATENCY") != "1" {
		t.Skip("set AUTO_DBA_LOGIN_LATENCY=1 to run")
	}
	gin.SetMode(gin.ReleaseMode)
	root, _ := os.Getwd()
	cfgPath := filepath.Join(root, "..", "..", "config", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		cfgPath = "config/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	h := &AuthHandler{Store: db, Cfg: cfg}

	runOnce := func() time.Duration {
		body, _ := json.Marshal(map[string]string{
			"username":  "admin",
			"password":  "admin123",
			"auth_mode": "platform",
		})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		start := time.Now()
		h.Login(c)
		elapsed := time.Since(start)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		return elapsed
	}

	cold := runOnce()
	warm := runOnce()
	warm2 := runOnce()
	t.Logf("platform admin login cold=%s warm=%s warm2=%s", cold, warm, warm2)
	time.Sleep(200 * time.Millisecond) // 等异步 login log / clear failures
	if warm > 600*time.Millisecond {
		t.Fatalf("warm login too slow: %s (want <600ms with deferred menus)", warm)
	}
}
