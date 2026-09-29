package proxy

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
)

// AgentAuth 平台 Agent 鉴权。
type AgentAuth struct {
	BaseURL  string
	Username string
	Password string
}

func (a AgentAuth) do(ctx context.Context, method, path string, body any, timeout time.Duration) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("未配置 Agent Base URL")
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(a.Username) != "" {
		req.SetBasicAuth(a.Username, a.Password)
	}
	resp, err := httpx.AgentHTTP(timeout).Do(req)
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
	if resp.StatusCode >= 300 {
		msg, _ := out["message"].(string)
		if msg == "" {
			msg = string(raw)
		}
		return out, fmt.Errorf("Agent HTTP %d: %s", resp.StatusCode, msg)
	}
	if ok, has := out["ok"].(bool); has && !ok {
		msg, _ := out["message"].(string)
		if msg == "" {
			msg = string(raw)
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

// PutConf 下发配置文件（可选 validate）。
func PutConf(ctx context.Context, auth AgentAuth, kind, path, content string, validate bool) (map[string]any, error) {
	return auth.do(ctx, http.MethodPut, "/v1/proxy/conf", map[string]any{
		"kind": kind, "path": path, "content": content, "validate": validate,
	}, 90*time.Second)
}

// ValidateConf 仅校验。
func ValidateConf(ctx context.Context, auth AgentAuth, kind, path string) (map[string]any, error) {
	return auth.do(ctx, http.MethodPost, "/v1/proxy/validate", map[string]any{
		"kind": kind, "path": path,
	}, 60*time.Second)
}

// ServiceCtrl start|stop|reload|restart|status。
func ServiceCtrl(ctx context.Context, auth AgentAuth, kind, action, unit string) (map[string]any, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "status" {
		q := "/v1/proxy/service/status?kind=" + kind
		if strings.TrimSpace(unit) != "" {
			q += "&unit=" + unit
		}
		return auth.do(ctx, http.MethodGet, q, nil, 30*time.Second)
	}
	return auth.do(ctx, http.MethodPost, "/v1/proxy/service/"+action, map[string]any{
		"kind": kind, "unit": unit,
	}, 60*time.Second)
}

// LVSIpvs 读取 Agent 本机 IPVS 表（只读）。
func LVSIpvs(ctx context.Context, auth AgentAuth) (map[string]any, error) {
	return auth.do(ctx, http.MethodGet, "/v1/proxy/lvs/ipvs", nil, 30*time.Second)
}

// LVSStatus Keepalived + VIP 探测汇总。
func LVSStatus(ctx context.Context, auth AgentAuth, unit, vip string) (map[string]any, error) {
	q := "/v1/proxy/lvs/status"
	var parts []string
	if u := strings.TrimSpace(unit); u != "" {
		parts = append(parts, "unit="+u)
	}
	if v := strings.TrimSpace(vip); v != "" {
		parts = append(parts, "vip="+v)
	}
	if len(parts) > 0 {
		q += "?" + strings.Join(parts, "&")
	}
	return auth.do(ctx, http.MethodGet, q, nil, 30*time.Second)
}

// Deploy 触发 Agent 侧装包/启用（异步 job）；vip 用于装包前占用探测。
func Deploy(ctx context.Context, auth AgentAuth, kind, vip string, enableStart, skipInstall bool) (jobID string, err error) {
	out, err := auth.do(ctx, http.MethodPost, "/v1/proxy/deploy", map[string]any{
		"kind": kind, "vip": vip, "enable_start": enableStart, "skip_install": skipInstall,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	id, _ := out["job_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("Agent 未返回 job_id")
	}
	return id, nil
}

// DeployJob 查询部署任务。
func DeployJob(ctx context.Context, auth AgentAuth, jobID string) (map[string]any, error) {
	return auth.do(ctx, http.MethodGet, "/v1/proxy/deploy/jobs/"+strings.Trim(jobID, "/"), nil, 15*time.Second)
}

// CheckVIP 探测 VIP 是否已被本机或网络占用。
func CheckVIP(ctx context.Context, auth AgentAuth, vip string) (available bool, message string, err error) {
	out, err := auth.do(ctx, http.MethodPost, "/v1/proxy/vip/check", map[string]any{"vip": vip}, 20*time.Second)
	if err != nil {
		return false, "", err
	}
	avail, _ := out["available"].(bool)
	msg, _ := out["message"].(string)
	return avail, msg, nil
}
