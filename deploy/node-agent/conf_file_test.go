package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func postJSON(t *testing.T, h http.HandlerFunc, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestFilePutBackupReturnsPrevious(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fe", "conf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "fe.conf")
	if err := os.WriteFile(conf, []byte("http_port = 8030\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := postJSON(t, handleFilePut, "/v1/sr/file/put", map[string]any{
		"path": conf, "content": "http_port = 8040\n", "backup": true,
	})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("code=%d out=%v", code, out)
	}
	if out["previous_content"] != "http_port = 8030\n" || out["previous_sha256"] == "" {
		t.Fatalf("应回传旧内容: %v", out)
	}
	bak, _ := out["backup_path"].(string)
	if b, err := os.ReadFile(bak); err != nil || string(b) != "http_port = 8030\n" {
		t.Fatalf("备份文件: %v %q", err, b)
	}
	if b, _ := os.ReadFile(conf); string(b) != "http_port = 8040\n" {
		t.Fatalf("新内容未写入: %q", b)
	}

	// 首次下发（文件不存在）不报错、不回传旧内容
	fresh := filepath.Join(dir, "be.conf")
	code, out = postJSON(t, handleFilePut, "/v1/sr/file/put", map[string]any{"path": fresh, "content": "x\n", "backup": true})
	if code != http.StatusOK || out["previous_content"] != nil {
		t.Fatalf("首次下发: code=%d out=%v", code, out)
	}
}

func TestPruneConfBackups(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "fe.conf")
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < confBackupKeep+3; i++ {
		if _, err := backupConfFile(conf, []byte(fmt.Sprint(i)), 0o644, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(conf + ".bak.*")
	if len(matches) != confBackupKeep {
		t.Fatalf("应只保留 %d 份，实际 %d", confBackupKeep, len(matches))
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "3" {
		t.Fatalf("应删掉最旧的，最早保留的是 %q", b)
	}
}

func TestFileGetWhitelist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "be", "conf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "be.conf")
	if err := os.WriteFile(conf, []byte("be_port = 9060\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := postJSON(t, handleFileGet, "/v1/sr/file/get", map[string]any{"path": conf})
	if code != http.StatusOK || out["content"] != "be_port = 9060\n" || out["exists"] != true || out["sha256"] == "" {
		t.Fatalf("code=%d out=%v", code, out)
	}
	code, out = postJSON(t, handleFileGet, "/v1/sr/file/get", map[string]any{"path": filepath.Join(dir, "fe.conf")})
	if code != http.StatusOK || out["exists"] != false {
		t.Fatalf("不存在的文件: code=%d out=%v", code, out)
	}
	for _, bad := range []string{"/etc/passwd", "conf/be.conf", dir + "/../conf/be.conf", "/x/conf/be.conf.bak", ""} {
		if code, _ := postJSON(t, handleFileGet, "/v1/sr/file/get", map[string]any{"path": bad}); code != http.StatusBadRequest {
			t.Fatalf("%q 应被拒绝, code=%d", bad, code)
		}
	}
}
