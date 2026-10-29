package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func systemNoticeTestStore(t *testing.T) *store.DB {
	t.Helper()
	cfgPath := filepath.Join("..", "..", "config", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Skip("no config/config.yaml")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil || cfg == nil || cfg.DB.DSN == "" {
		t.Skip("cannot load DSN")
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSystemNoticeActiveAckVersionBump(t *testing.T) {
	db := systemNoticeTestStore(t)
	h := &SystemNoticeHandler{Store: db}
	gin.SetMode(gin.TestMode)

	admin, err := db.GetUserByUsername("admin")
	if err != nil || admin == nil {
		t.Skip("admin missing")
	}

	// 系统通知为单例模板：先清空，避免复用库内旧记录/ack 导致 show=false。
	_, _ = db.Exec(`DELETE FROM tbl_system_notice_acks`)
	_, _ = db.Exec(`DELETE FROM tbl_system_notices`)

	title := "notice-" + uuid.NewString()[:8]
	it := &store.SystemNotice{
		Title: title, Body: "please do X", Enabled: true,
		ShowMode: store.SystemNoticeModeOnVersionChange,
	}
	if err := db.UpsertSystemNotice(it, false, "admin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_system_notice_acks WHERE notice_id=?`, it.ID)
		_, _ = db.Exec(`DELETE FROM tbl_system_notices WHERE id=?`, it.ID)
	})
	if it.ID == "" || it.Version != 1 {
		t.Fatalf("upsert create: id=%q version=%d", it.ID, it.Version)
	}

	callActive := func() map[string]any {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Set(middleware.CtxUserID, admin.ID)
		c.Set(middleware.CtxUsername, "admin")
		h.Active(c)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != http.StatusOK {
			t.Fatalf("active code=%d body=%s", w.Code, w.Body.String())
		}
		data, _ := out["data"].(map[string]any)
		return data
	}

	data := callActive()
	if data["show"] != true {
		t.Fatalf("expect show before ack: %v", data)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	raw, _ := json.Marshal(map[string]any{"notice_id": it.ID, "version": it.Version})
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.CtxUserID, admin.ID)
	h.Ack(c)
	if w.Code != http.StatusOK {
		t.Fatalf("ack code=%d body=%s", w.Code, w.Body.String())
	}

	data = callActive()
	if data["show"] != false {
		t.Fatalf("expect hide after ack: %v", data)
	}

	it.Title = title
	it.Body = "please do Y"
	it.Enabled = true
	it.ShowMode = store.SystemNoticeModeOnVersionChange
	if err := db.UpsertSystemNotice(it, true, "admin"); err != nil {
		t.Fatal(err)
	}
	data = callActive()
	if data["show"] != true {
		t.Fatalf("expect show after version bump: %v", data)
	}

	// once: ack then never show
	it.ShowMode = store.SystemNoticeModeOnce
	if err := db.UpsertSystemNotice(it, false, "admin"); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`DELETE FROM tbl_system_notice_acks WHERE notice_id=? AND user_id=?`, it.ID, admin.ID)
	data = callActive()
	if data["show"] != true {
		t.Fatalf("once before ack: %v", data)
	}
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	raw, _ = json.Marshal(map[string]any{"notice_id": it.ID, "version": it.Version})
	c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.CtxUserID, admin.ID)
	h.Ack(c)
	data = callActive()
	if data["show"] != false {
		t.Fatalf("once after ack: %v", data)
	}

	// every_login always show
	it.ShowMode = store.SystemNoticeModeEveryLogin
	if err := db.UpsertSystemNotice(it, false, "admin"); err != nil {
		t.Fatal(err)
	}
	data = callActive()
	if data["show"] != true {
		t.Fatalf("every_login should show: %v", data)
	}
}
