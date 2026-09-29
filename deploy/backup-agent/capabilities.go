package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	groups := strings.Split(r.URL.Query().Get("groups"), ",")
	enabled := normalizeGroups(groups)
	if len(enabled) == 0 {
		enabled = []string{"backup", "rdb", "ha"}
	}
	probe := map[string]bool{
		"logical_pg":     lookPath("pg_dump"),
		"logical_mysql":  lookPath("mysqldump"),
		"physical_pg":    lookPath("pg_basebackup"),
		"physical_mysql": lookPath("xtrabackup") || lookPath("mariabackup"),
		"redis_rdb":      lookPath("redis-cli"),
		"ha_mha":             lookPath("masterha_check_status"),
		"ha_pg_autofailover": lookPath("pg_autoctl"),
		"ha_repmgr":          lookPath("repmgr"),
		"proxy_haproxy":      lookPath("haproxy") && lookPath("keepalived"),
		"proxy_nginx":        lookPath("nginx") && lookPath("keepalived"),
		"proxy_lvs":          lookPath("keepalived"),
		"proxy_ipvsadm":      lookPath("ipvsadm"),
		"runtime_disk":       diskOK(*workdir),
	}
	rep := buildCapReport(enabled, probe)
	writeJSON(w, http.StatusOK, rep)
}

func lookPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func diskOK(dir string) bool {
	_ = os.MkdirAll(dir, 0o750)
	// 简化：目录可创建即认为基本可用；细粒度 df 由平台预检完成
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

func normalizeGroups(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range in {
		g = strings.ToLower(strings.TrimSpace(g))
		switch g {
		case "backup", "rdb", "ha":
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

type capItem struct {
	Key    string `json:"key"`
	Group  string `json:"group"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Tool   string `json:"tool,omitempty"`
}

func buildCapReport(enabled []string, probe map[string]bool) map[string]any {
	catalog := []capItem{
		{Key: "logical_pg", Group: "backup", Tool: "pg_dump", Hint: "请安装 PostgreSQL 客户端（含 pg_dump）"},
		{Key: "logical_mysql", Group: "backup", Tool: "mysqldump", Hint: "请安装 mysql-client（含 mysqldump）"},
		{Key: "physical_pg", Group: "backup", Tool: "pg_basebackup", Hint: "请安装 postgresql 客户端工具"},
		{Key: "physical_mysql", Group: "backup", Tool: "xtrabackup|mariabackup", Hint: "请安装 XtraBackup 或 MariaBackup"},
		{Key: "redis_rdb", Group: "rdb", Tool: "redis-cli", Hint: "请安装 redis-tools（含 redis-cli）"},
		{Key: "ha_mha", Group: "ha", Tool: "masterha_check_status", Hint: "请安装 MHA 并将 masterha_* 加入 PATH"},
		{Key: "ha_pg_autofailover", Group: "ha", Tool: "pg_autoctl", Hint: "请安装 pg_auto_failover（含 pg_autoctl）"},
		{Key: "ha_repmgr", Group: "ha", Tool: "repmgr", Hint: "请安装 repmgr 并将 repmgr 加入 PATH"},
		{Key: "proxy_haproxy", Group: "ha", Tool: "haproxy+keepalived", Hint: "请安装 haproxy 与 keepalived（VIP）"},
		{Key: "proxy_nginx", Group: "ha", Tool: "nginx+keepalived", Hint: "请安装 nginx 与 keepalived（VIP）"},
		{Key: "proxy_lvs", Group: "ha", Tool: "keepalived", Hint: "请安装 keepalived（LVS/VIP）"},
		{Key: "proxy_ipvsadm", Group: "ha", Tool: "ipvsadm", Hint: "请安装 ipvsadm（查看 IPVS 表）"},
		{Key: "runtime_disk", Group: "runtime", Tool: "df", Hint: "请保证 workdir 磁盘空间充足"},
	}
	enabledSet := map[string]bool{}
	for _, g := range enabled {
		enabledSet[g] = true
	}
	req := map[string]bool{}
	for _, g := range enabled {
		switch g {
		case "backup":
			req["logical_pg"] = true
			req["logical_mysql"] = true
		case "rdb":
			req["redis_rdb"] = true
		case "ha":
			req["ha_mha"] = true
			req["ha_pg_autofailover"] = true
			req["ha_repmgr"] = true
		}
	}
	okAll := true
	var items []capItem
	for _, def := range catalog {
		it := def
		if def.Group != "runtime" && !enabledSet[def.Group] {
			it.Status = "skipped"
			it.Detail = "未启用该能力组"
			items = append(items, it)
			continue
		}
		if probe[def.Key] {
			it.Status = "ok"
			it.Detail = "available"
		} else {
			it.Status = "missing"
			it.Detail = "not found"
			if req[def.Key] {
				okAll = false
			}
		}
		items = append(items, it)
	}
	return map[string]any{
		"ok":             okAll,
		"enabled_groups": enabled,
		"items":          items,
		"workdir":        filepath.Clean(*workdir),
	}
}