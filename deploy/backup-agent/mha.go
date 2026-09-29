package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MHA 计划切换 / 检查：与 deploy/mha-agent 同路径，供归属含 ha 的平台 Agent 作为 HA 控制器执行面。

func handleMHAStatus(w http.ResponseWriter, r *http.Request) {
	conf, err := resolveMHAConf(r.URL.Query().Get("conf"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, code, err := runMasterhaCode(ctx, "masterha_check_status", "--conf="+conf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": out, "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "check_status ok", "output": out})
}

func handleMHARepl(w http.ResponseWriter, r *http.Request) {
	conf, err := resolveMHAConf(r.URL.Query().Get("conf"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, code, err := runMasterhaCode(ctx, "masterha_check_repl", "--conf="+conf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": out, "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "check_repl ok", "output": out})
}

func handleMHASwitchPlanned(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Conf          string `json:"conf"`
		NewMasterHost string `json:"new_master_host"`
		NewMasterPort int    `json:"new_master_port"`
		TimeoutSec    int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	conf, err := resolveMHAConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.NewMasterHost) == "" || req.NewMasterPort <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "new_master_host/port required"})
		return
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	args := []string{
		"--conf=" + conf,
		"--master_state=alive",
		"--new_master_host=" + req.NewMasterHost,
		fmt.Sprintf("--new_master_port=%d", req.NewMasterPort),
		"--interactive=0",
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	out, code, err := runMasterhaCode(ctx, "masterha_master_switch", args...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error(), "output": trimMHAOut(out), "exit_code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "planned switch ok", "output": trimMHAOut(out)})
}

func handleMHAPutConf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Conf    string `json:"conf"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "content required"})
		return
	}
	conf, err := resolveMHAConf(req.Conf)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run write skipped", "conf": conf, "bytes": len(req.Content)})
		return
	}
	dir := filepath.Dir(conf)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "mkdir: " + err.Error()})
		return
	}
	tmp := conf + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, []byte(req.Content), 0o600); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "write: " + err.Error()})
		return
	}
	if err := os.Rename(tmp, conf); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "rename: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "conf written", "conf": conf, "bytes": len(req.Content)})
}

func resolveMHAConf(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		p = filepath.Join(*confRoot, "app.cnf")
	}
	if !pathUnderRoot(p, *confRoot) {
		return "", fmt.Errorf("conf outside allowed root: %s", p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func runMasterhaCode(ctx context.Context, bin string, args ...string) (string, int, error) {
	mu.Lock()
	defer mu.Unlock()
	if *dryRun {
		return fmt.Sprintf("dry-run %s %s", bin, strings.Join(args, " ")), 0, nil
	}
	out, err := runMasterha(ctx, bin, args...)
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return out, code, fmt.Errorf("%s failed: %w", bin, err)
	}
	return out, 0, nil
}

func trimMHAOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}
