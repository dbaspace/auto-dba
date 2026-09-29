package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureFSBackupInPlace(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "fe")
	if err := os.MkdirAll(filepath.Join(src, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "meta", "x"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldBak := filepath.Join(src, "bak.old")
	if err := os.MkdirAll(filepath.Join(oldBak, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"paths":         []string{src},
		"backup_suffix": "testbak",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/sr/fs/ensure", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handleEnsureFS(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("resp=%v", out)
	}
	bak := filepath.Join(src, "bak.testbak")
	if _, err := os.Stat(filepath.Join(bak, "meta", "x")); err != nil {
		t.Fatalf("in-place bak missing: %v msg=%v", err, out["message"])
	}
	if _, err := os.Stat(filepath.Join(src, "meta")); !os.IsNotExist(err) {
		t.Fatalf("meta should have been moved into bak")
	}
	if _, err := os.Stat(filepath.Join(oldBak, "keep")); err != nil {
		t.Fatalf("old bak should stay: %v", err)
	}
	msg, _ := out["message"].(string)
	if !strings.Contains(msg, bak) {
		t.Fatalf("message=%v", out["message"])
	}
}

func TestEnsureFSRejectBadBackupSuffix(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"paths":         []string{t.TempDir()},
		"backup_suffix": "../evil",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/sr/fs/ensure", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handleEnsureFS(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
