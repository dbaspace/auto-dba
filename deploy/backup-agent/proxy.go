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

var proxyConfRoots = []string{
	"/etc/haproxy",
	"/etc/nginx",
	"/etc/keepalived",
	"/etc/auto-dba/proxy",
}

func proxyPathAllowed(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	for _, root := range proxyConfRoots {
		if pathUnderRoot(abs, root) {
			return true
		}
	}
	if pathUnderRoot(abs, *workdir) {
		return true
	}
	return false
}

func handleProxyPutConf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind     string `json:"kind"`
		Path     string `json:"path"`
		Content  string `json:"content"`
		Validate bool   `json:"validate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" || strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "path/content 必填"})
		return
	}
	if !proxyPathAllowed(path) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": "conf 路径须位于 /etc/haproxy|/etc/nginx|/etc/keepalived|/etc/auto-dba/proxy 或 workdir",
		})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run put conf", "path": path})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	tmp := path + ".auto-dba.tmp"
	if err := os.WriteFile(tmp, []byte(req.Content), 0o640); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if req.Validate {
		if out, err := runProxyValidate(req.Kind, tmp); err != nil {
			_ = os.Remove(tmp)
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": out})
			return
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}

func handleProxyValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" || !proxyPathAllowed(path) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "path 无效或不在白名单"})
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "dry-run validate"})
		return
	}
	out, err := runProxyValidate(req.Kind, path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "raw": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "raw": out})
}

func handleProxyService(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/v1/proxy/service/")
	action = strings.Trim(action, "/")
	kind := r.URL.Query().Get("kind")
	unit := r.URL.Query().Get("unit")
	if r.Method == http.MethodPost {
		var req struct {
			Kind string `json:"kind"`
			Unit string `json:"unit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if kind == "" {
			kind = req.Kind
		}
		if unit == "" {
			unit = req.Unit
		}
	}
	unit = strings.TrimSpace(unit)
	if unit == "" {
		unit = defaultProxyUnit(kind)
	}
	if action == "status" && r.Method == http.MethodGet {
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
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "action 须为 start|stop|restart|reload|status"})
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

func defaultProxyUnit(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "nginx":
		return "nginx"
	case "lvs":
		return "keepalived"
	default:
		return "haproxy"
	}
}

func runProxyValidate(kind, confPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "haproxy":
		if !lookPath("haproxy") {
			return "", fmt.Errorf("未找到 haproxy")
		}
		cmd = exec.CommandContext(ctx, "haproxy", "-c", "-f", confPath)
	case "nginx":
		if !lookPath("nginx") {
			return "", fmt.Errorf("未找到 nginx")
		}
		cmd = exec.CommandContext(ctx, "nginx", "-t", "-c", confPath)
	case "lvs":
		if !lookPath("keepalived") {
			return "", fmt.Errorf("未找到 keepalived")
		}
		// keepalived -t：语法检查（官方 man keepalived）
		cmd = exec.CommandContext(ctx, "keepalived", "-t", "-f", confPath)
	default:
		return "", fmt.Errorf("不支持的 kind: %s", kind)
	}
	b, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(b))
	if err != nil {
		return s, fmt.Errorf("校验失败: %w (%s)", err, s)
	}
	return s, nil
}

// handleProxyLVSIpvs 读取本机 IPVS 表（只读，不改规则）。
func handleProxyLVSIpvs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "message": "dry-run", "ipvsadm": "(dry-run)", "proc": "",
		})
		return
	}
	out := map[string]any{"ok": true, "ipvsadm_available": lookPath("ipvsadm")}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if lookPath("ipvsadm") {
		b, err := exec.CommandContext(ctx, "ipvsadm", "-Ln", "--stats").CombinedOutput()
		out["ipvsadm"] = strings.TrimSpace(string(b))
		if err != nil {
			out["ipvsadm_err"] = err.Error()
			// 无权限时退回无 stats
			b2, err2 := exec.CommandContext(ctx, "ipvsadm", "-Ln").CombinedOutput()
			if err2 == nil {
				out["ipvsadm"] = strings.TrimSpace(string(b2))
				delete(out, "ipvsadm_err")
			}
		}
	} else {
		out["ipvsadm"] = ""
		out["message"] = "未安装 ipvsadm，仅返回 /proc/net/ip_vs"
	}
	if raw, err := os.ReadFile("/proc/net/ip_vs"); err == nil {
		out["proc"] = string(raw)
	} else {
		out["proc"] = ""
		out["proc_err"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// handleProxyLVSStatus Keepalived + VIP + IPVS 汇总（只读）。
func handleProxyLVSStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	unit := strings.TrimSpace(r.URL.Query().Get("unit"))
	if unit == "" {
		unit = "keepalived"
	}
	vipHint := strings.TrimSpace(r.URL.Query().Get("vip"))
	if *dryRun {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "unit": unit, "active": false, "message": "dry-run",
		})
		return
	}
	out := map[string]any{"ok": true, "unit": unit}
	if b, err := exec.Command("systemctl", "is-active", unit).CombinedOutput(); err == nil {
		state := strings.TrimSpace(string(b))
		out["state"] = state
		out["active"] = state == "active"
	} else {
		out["state"] = strings.TrimSpace(string(b))
		out["active"] = false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if lookPath("ipvsadm") {
		b, _ := exec.CommandContext(ctx, "ipvsadm", "-Ln").CombinedOutput()
		out["ipvs"] = strings.TrimSpace(string(b))
	}
	if vipHint != "" {
		b, _ := exec.CommandContext(ctx, "ip", "addr", "show").CombinedOutput()
		text := string(b)
		out["vip_present"] = strings.Contains(text, vipHint)
		// 截取含 VIP 的行便于排查
		var lines []string
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, vipHint) {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
		out["vip_lines"] = lines
	}
	writeJSON(w, http.StatusOK, out)
}
