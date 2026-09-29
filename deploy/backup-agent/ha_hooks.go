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

// 切换后 VIP/流量脚本钩子：仅允许执行 conf-root 或 workdir 下的脚本。

func handleHAHookRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Script string            `json:"script"`
		Args   []string          `json:"args"`
		Env    map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	script := strings.TrimSpace(req.Script)
	if script == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "script 必填"})
		return
	}
	abs, err := filepath.Abs(script)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	allowed := pathUnderRoot(abs, *confRoot) || pathUnderRoot(abs, *workdir) || pathUnderRoot(abs, "/etc/auto-dba/hooks")
	if !allowed {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": "script 必须位于 conf-root、workdir 或 /etc/auto-dba/hooks",
		})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run hook", "script": abs})
		return
	}
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "脚本不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, abs, req.Args...)
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		if k == "" {
			continue
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": trimOut(string(out))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "raw": trimOut(string(out))})
}

func handlePGAFService(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/v1/pgaf/service/")
	action = strings.Trim(action, "/")
	if action == "status" && r.Method == http.MethodGet {
		unit := r.URL.Query().Get("unit")
		if unit == "" {
			unit = "pgautofailover"
		}
		if *dryRun {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active": false, "unit": unit, "message": "dry-run"})
			return
		}
		out, err := exec.Command("systemctl", "is-active", unit).CombinedOutput()
		state := strings.TrimSpace(string(out))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "active": err == nil && state == "active", "state": state, "unit": unit,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Unit string `json:"unit"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	unit := strings.TrimSpace(req.Unit)
	if unit == "" {
		unit = "pgautofailover"
	}
	if action != "start" && action != "stop" && action != "restart" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "action 须为 start|stop|restart|status"})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": unit, "action": action, "message": "dry-run"})
		return
	}
	out, err := exec.Command("systemctl", action, unit).CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": strings.TrimSpace(string(out))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": unit, "action": action})
}
