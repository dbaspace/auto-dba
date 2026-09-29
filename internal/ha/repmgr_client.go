package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-dba/internal/httpx"
	"auto-dba/internal/store"
)

// RepmgrOptions 控制器 remark JSON。
type RepmgrOptions struct {
	ConfFile string `json:"conf"`
	Node     string `json:"node"`
	Force    bool   `json:"force"`
}

func ParseRepmgrOptions(remark string) RepmgrOptions {
	opt := RepmgrOptions{}
	s := strings.TrimSpace(remark)
	if strings.HasPrefix(s, "{") {
		_ = json.Unmarshal([]byte(s), &opt)
	} else if s != "" {
		opt.ConfFile = s
	}
	return opt
}

func repmgrAgentSwitchover(ctx context.Context, ctrl ControllerAuth, candidate *store.Instance) error {
	opt := ParseRepmgrOptions(ctrl.AppConf)
	node := strings.TrimSpace(opt.Node)
	if candidate != nil {
		if n := strings.TrimSpace(candidate.Name); n != "" {
			node = n
		} else if h := strings.TrimSpace(candidate.Host); h != "" {
			node = h
		}
	}
	body, _ := json.Marshal(map[string]any{
		"node":  node,
		"conf":  opt.ConfFile,
		"force": opt.Force,
	})
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return fmt.Errorf("未配置 repmgr Agent Base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/repmgr/switchover", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	resp, err := httpx.AgentHTTP(3 * time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("repmgr Agent: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Raw     string `json:"raw"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || !out.OK {
		msg := firstNonEmptyHA(out.Message, out.Raw, string(raw))
		return fmt.Errorf("repmgr switchover 失败: %s", msg)
	}
	return nil
}

// RepmgrEnvCheck 探测 Agent + cluster show。
func RepmgrEnvCheck(ctx context.Context, ctrl ControllerAuth) (ok bool, summary string, checks []map[string]any) {
	ok = true
	add := func(name string, good bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": good, "detail": detail})
		if !good {
			ok = false
		}
	}
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		add("addr", false, "未配置 Base URL")
		return false, "repmgr Agent 地址无效", checks
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/repmgr/status", nil)
	if err != nil {
		add("request", false, err.Error())
		return false, "构造请求失败", checks
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	resp, err := httpx.AgentHTTP(60 * time.Second).Do(req)
	if err != nil {
		add("reachability", false, err.Error())
		return false, "repmgr Agent 不可达", checks
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Raw     string `json:"raw"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || !out.OK {
		add("cluster_show", false, firstNonEmptyHA(out.Message, string(raw)))
		return false, "repmgr cluster show 失败", checks
	}
	add("cluster_show", true, trimOrch(out.Raw))
	summary = "repmgr 环境检查通过"
	return ok, summary, checks
}
