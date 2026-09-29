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

// pg_auto_failover Agent：部署在 Monitor 同机（或可连 Monitor 的跳板），白名单执行 pg_autoctl。

func handlePGAFStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	formation := strings.TrimSpace(r.URL.Query().Get("formation"))
	if formation == "" {
		formation = "default"
	}
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	monitor := strings.TrimSpace(r.URL.Query().Get("monitor"))
	pgdata := strings.TrimSpace(r.URL.Query().Get("pgdata"))

	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "formation": formation, "raw": "dry-run state",
			"nodes": []map[string]any{
				{"name": "node1", "role": "primary", "state": "primary"},
				{"name": "node2", "role": "secondary", "state": "secondary"},
			},
		})
		return
	}
	if !lookPath("pg_autoctl") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "未找到 pg_autoctl，请安装 pg_auto_failover"})
		return
	}
	args := []string{"show", "state", "--formation", formation, "--json"}
	if group != "" {
		args = append(args, "--group", group)
	}
	args = append(args, pgafConnArgs(monitor, pgdata)...)
	out, err := runPgAutoCtl(90*time.Second, args...)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": out})
		return
	}
	var parsed any
	_ = json.Unmarshal([]byte(out), &parsed)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "formation": formation, "raw": out, "state": parsed})
}

func handlePGAFSwitchover(w http.ResponseWriter, r *http.Request) {
	handlePGAFPerform(w, r, "switchover")
}

func handlePGAFFailover(w http.ResponseWriter, r *http.Request) {
	handlePGAFPerform(w, r, "failover")
}

func handlePGAFPerform(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Formation string `json:"formation"`
		Group     string `json:"group"`
		Monitor   string `json:"monitor"`
		PGData    string `json:"pgdata"`
		Candidate string `json:"candidate"` // 节点名，可选；有则走 promotion
		Timeout   int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	formation := strings.TrimSpace(req.Formation)
	if formation == "" {
		formation = "default"
	}
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "action": action, "formation": formation, "candidate": req.Candidate, "message": "dry-run ok",
		})
		return
	}
	if !lookPath("pg_autoctl") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "未找到 pg_autoctl"})
		return
	}

	var args []string
	cand := strings.TrimSpace(req.Candidate)
	if cand != "" && action == "switchover" {
		// 指定节点提升：perform promotion
		args = []string{"perform", "promotion", "--formation", formation, "--name", cand}
	} else {
		args = []string{"perform", action, "--formation", formation}
	}
	if g := strings.TrimSpace(req.Group); g != "" {
		args = append(args, "--group", g)
	}
	args = append(args, pgafConnArgs(req.Monitor, req.PGData)...)
	out, err := runPgAutoCtl(timeout, args...)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": trimOut(out)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "formation": formation, "raw": trimOut(out)})
}

func pgafConnArgs(monitor, pgdata string) []string {
	var args []string
	if strings.TrimSpace(monitor) != "" {
		args = append(args, "--monitor", strings.TrimSpace(monitor))
	}
	if strings.TrimSpace(pgdata) != "" {
		args = append(args, "--pgdata", strings.TrimSpace(pgdata))
	}
	return args
}

func runPgAutoCtl(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pg_autoctl", args...)
	out, err := cmd.CombinedOutput()
	s := string(out)
	if ctx.Err() == context.DeadlineExceeded {
		return s, fmt.Errorf("pg_autoctl 超时: %v", args)
	}
	if err != nil {
		return s, fmt.Errorf("pg_autoctl %v: %w (%s)", args, err, trimOut(s))
	}
	return s, nil
}

func trimOut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000] + "..."
	}
	return s
}
