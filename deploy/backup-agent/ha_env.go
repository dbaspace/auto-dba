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

func handleHAEnvCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Kind     string `json:"kind"`
		Conf     string `json:"conf"`
		ConfRoot string `json:"conf_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "mha"
	}
	switch kind {
	case "mha":
		runMHAEnvCheck(w, r.Context(), req.Conf, req.ConfRoot)
	case "pg_autofailover":
		handlePGAFEnvCheck(w, req.Conf)
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "summary": "不支持的 kind: " + kind,
			"checks": []map[string]any{{"name": "kind", "ok": false, "detail": kind}},
		})
	}
}

func handlePGAFEnvCheck(w http.ResponseWriter, monitorOrPgdata string) {
	var checks []map[string]any
	allOK := true
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": ok, "detail": detail})
		if !ok {
			allOK = false
		}
	}
	if *dryRun {
		add("pg_autoctl", true, "dry-run stub ok")
		add("show_state", true, "dry-run stub ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": "dry-run pg_autofailover 环境检查通过", "checks": checks})
		return
	}
	if !lookPath("pg_autoctl") {
		add("tool_pg_autoctl", false, "未找到 pg_autoctl")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "summary": "缺少 pg_auto_failover", "checks": checks})
		return
	}
	add("tool_pg_autoctl", true, "found")
	args := []string{"show", "state", "--json"}
	if strings.TrimSpace(monitorOrPgdata) != "" {
		if strings.Contains(monitorOrPgdata, "://") {
			args = append(args, "--monitor", monitorOrPgdata)
		} else {
			args = append(args, "--pgdata", monitorOrPgdata)
		}
	}
	out, err := runPgAutoCtl(60*time.Second, args...)
	if err != nil {
		add("show_state", false, err.Error())
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "summary": "show state 失败", "checks": checks, "raw": trimOut(out)})
		return
	}
	add("show_state", true, trimOut(out))
	writeJSON(w, http.StatusOK, map[string]any{"ok": allOK, "summary": "pg_autofailover 环境检查通过", "checks": checks})
}

func runMHAEnvCheck(w http.ResponseWriter, parent context.Context, conf, confRoot string) {
	confRoot = strings.TrimSpace(confRoot)
	if confRoot == "" {
		confRoot = "/etc/mha"
	}
	conf = strings.TrimSpace(conf)
	if conf == "" {
		conf = filepath.Join(confRoot, "app.cnf")
	}
	if !pathUnderRoot(conf, confRoot) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "summary": "conf 必须位于 conf_root 下",
			"checks": []map[string]any{{"name": "conf_path", "ok": false, "detail": conf}},
		})
		return
	}

	var checks []map[string]any
	allOK := true
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": ok, "detail": detail})
		if !ok {
			allOK = false
		}
	}

	if *dryRun {
		add("masterha_check_status", true, "dry-run stub ok")
		add("masterha_check_repl", true, "dry-run stub ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": "dry-run 环境检查通过", "checks": checks})
		return
	}

	if !lookPath("masterha_check_status") {
		add("tool_masterha_check_status", false, "未找到 masterha_check_status，请安装 MHA")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "summary": "缺少 MHA 工具", "checks": checks})
		return
	}
	add("tool_masterha_check_status", true, "found")

	if _, err := os.Stat(conf); err != nil {
		add("conf_exists", false, err.Error())
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "summary": "配置文件不存在", "checks": checks})
		return
	}
	add("conf_exists", true, conf)

	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()

	out1, err1 := runMasterha(ctx, "masterha_check_status", "--conf="+conf)
	add("masterha_check_status", err1 == nil, trim(out1))
	out2, err2 := runMasterha(ctx, "masterha_check_repl", "--conf="+conf)
	add("masterha_check_repl", err2 == nil, trim(out2))

	summary := "高可用环境检查通过"
	if !allOK {
		summary = "高可用环境检查未通过，请查看 checks"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": allOK, "summary": summary, "checks": checks, "conf": conf})
}

func runMasterha(ctx context.Context, bin string, args ...string) (string, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s not found", bin)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func pathUnderRoot(p, root string) bool {
	absP, err1 := filepath.Abs(p)
	absR, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absR, absP)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
