package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// repmgr Agent：白名单执行 repmgr 命令（部署在 primary/witness 可执行机）。

func handleRepmgrStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "raw": "dry-run cluster show"})
		return
	}
	if !lookPath("repmgr") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "未找到 repmgr"})
		return
	}
	out, err := runRepmgr(60*time.Second, "cluster", "show")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "raw": out})
}

func handleRepmgrSwitchover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Node     string `json:"node"` // 新主节点名
		ConfFile string `json:"conf"`
		Force    bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run switchover", "node": req.Node})
		return
	}
	if !lookPath("repmgr") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "未找到 repmgr"})
		return
	}
	args := []string{"standby", "switchover", "--siblings-follow"}
	if strings.TrimSpace(req.ConfFile) != "" {
		args = append([]string{"-f", strings.TrimSpace(req.ConfFile)}, args...)
	}
	if req.Force {
		args = append(args, "--force")
	}
	out, err := runRepmgr(180*time.Second, args...)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": trimOut(out)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "raw": trimOut(out)})
}

func runRepmgr(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "repmgr", args...)
	b, err := cmd.CombinedOutput()
	s := string(b)
	if ctx.Err() == context.DeadlineExceeded {
		return s, fmt.Errorf("repmgr 超时")
	}
	if err != nil {
		return s, fmt.Errorf("repmgr %v: %w (%s)", args, err, trimOut(s))
	}
	return s, nil
}
