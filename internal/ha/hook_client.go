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

// HAHookOptions 集群 remark JSON：切换后 VIP/流量脚本（脚本内容由现场维护，平台只触发）。
type HAHookOptions struct {
	Script   string            `json:"vip_hook_script"`
	Args     []string          `json:"vip_hook_args"`
	Env      map[string]string `json:"vip_hook_env"`
	AgentURL string            `json:"hook_agent_url"`
	Username string            `json:"hook_agent_user"`
	Password string            `json:"hook_agent_pass"`
}

func ParseHAHookOptions(remark string) HAHookOptions {
	opt := HAHookOptions{}
	s := strings.TrimSpace(remark)
	if strings.HasPrefix(s, "{") {
		_ = json.Unmarshal([]byte(s), &opt)
	}
	return opt
}

// MaybeRunPostSwitchHook 若集群 remark 配置了 vip_hook_script，则经 Agent 白名单执行。
func MaybeRunPostSwitchHook(ctx context.Context, cluster *store.HACluster, ctrl ControllerAuth, newPrimary *store.Instance) error {
	if cluster == nil {
		return nil
	}
	opt := ParseHAHookOptions(cluster.Remark)
	if strings.TrimSpace(opt.Script) == "" {
		return nil
	}
	auth := ctrl
	if u := strings.TrimSpace(opt.AgentURL); u != "" {
		auth.BaseURL = u
		if opt.Username != "" {
			auth.Username = opt.Username
			auth.Password = opt.Password
		}
	}
	kind := strings.ToLower(strings.TrimSpace(auth.Kind))
	switch kind {
	case "mha", "pg_autofailover", "repmgr", "":
	default:
		if strings.TrimSpace(opt.AgentURL) == "" {
			return fmt.Errorf("VIP 钩子需要 hook_agent_url（当前控制器 kind=%s 非 Agent）", kind)
		}
	}
	env := map[string]string{}
	for k, v := range opt.Env {
		env[k] = v
	}
	if newPrimary != nil {
		env["NEW_PRIMARY_HOST"] = firstNonEmptyHA(newPrimary.Host, newPrimary.DomainName)
		env["NEW_PRIMARY_PORT"] = fmt.Sprintf("%d", newPrimary.Port)
		env["NEW_PRIMARY_NAME"] = firstNonEmptyHA(newPrimary.Name, newPrimary.Host)
	}
	if vip := strings.TrimSpace(cluster.VIP); vip != "" {
		env["HA_VIP"] = vip
	}
	env["HA_CLUSTER"] = cluster.Name
	env["HA_ENGINE"] = cluster.Engine
	body, _ := json.Marshal(map[string]any{
		"script": opt.Script,
		"args":   opt.Args,
		"env":    env,
	})
	base := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if base == "" {
		return fmt.Errorf("VIP 钩子缺少 Agent Base URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/ha/hook/run", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(auth.Username) != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
	resp, err := httpx.AgentHTTP(2 * time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("VIP 钩子调用失败: %w", err)
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
		return fmt.Errorf("VIP 钩子失败: %s", firstNonEmptyHA(out.Message, out.Raw, string(raw)))
	}
	return nil
}

// PGAFAgentServiceCtrl systemd 启停 Monitor/Keeper 单元（unit 默认 pgautofailover）。
func PGAFAgentServiceCtrl(ctx context.Context, ctrl ControllerAuth, action, unit string) (map[string]any, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("未配置 Agent Base URL")
	}
	if action == "status" {
		u := base + "/v1/pgaf/service/status"
		if strings.TrimSpace(unit) != "" {
			u += "?unit=" + unit
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(ctrl.Username) != "" {
			req.SetBasicAuth(ctrl.Username, ctrl.Password)
		}
		resp, err := httpx.AgentHTTP(30 * time.Second).Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = map[string]any{"raw": string(raw)}
		}
		return out, nil
	}
	body, _ := json.Marshal(map[string]string{"unit": unit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/pgaf/service/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	resp, err := httpx.AgentHTTP(60 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		msg := string(raw)
		if out != nil {
			if m, ok := out["message"].(string); ok {
				msg = m
			}
		}
		return out, fmt.Errorf("pgaf service %s: %s", action, msg)
	}
	return out, nil
}
