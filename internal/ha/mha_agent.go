package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/httpx"
	"auto-dba/internal/store"
)

const (
	mhaDefaultConf   = "/etc/mha/app.cnf"
	mhaSwitchTimeout = 15 * time.Minute
	mhaProbeTimeout  = 12 * time.Second
)

// ParseMHAAppConf 从控制器 remark 解析 app.cnf 路径。
// 支持纯路径，或 JSON：{"app_conf":"/etc/mha/app1.cnf"}。
func ParseMHAAppConf(remark string) string {
	raw := strings.TrimSpace(remark)
	if raw == "" {
		return mhaDefaultConf
	}
	if strings.HasPrefix(raw, "{") {
		var m map[string]any
		if json.Unmarshal([]byte(raw), &m) == nil {
			if v, ok := m["app_conf"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
			if v, ok := m["conf"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	if strings.Contains(raw, "/") || strings.HasSuffix(raw, ".cnf") {
		return raw
	}
	return mhaDefaultConf
}

func mhaAgentPlannedSwitch(ctx context.Context, ctrl ControllerAuth, cluster *store.HACluster, candidate *store.Instance) error {
	host := strings.TrimSpace(candidate.Host)
	if host == "" {
		host = strings.TrimSpace(candidate.DomainName)
	}
	if host == "" || candidate.Port <= 0 {
		return fmt.Errorf("目标实例缺少 host/port")
	}
	conf := ParseMHAAppConf(ctrl.AppConf)
	clusterName := ""
	if cluster != nil {
		clusterName = cluster.Name
	}
	payload, _ := json.Marshal(map[string]any{
		"conf":            conf,
		"new_master_host": host,
		"new_master_port": candidate.Port,
		"cluster_name":    clusterName,
		"timeout_seconds": int(mhaSwitchTimeout.Seconds()),
	})
	u := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/") + "/v1/mha/switch/planned"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(mhaSwitchTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("MHA Agent 调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("MHA Agent HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &out) == nil && !out.OK && strings.TrimSpace(out.Message) != "" {
		return fmt.Errorf("MHA 计划切换失败: %s", out.Message)
	}
	return nil
}

// MHAAgentStatus 探测 Manager Agent 健康与 check 摘要（失败只返回 warning 文本）。
func MHAAgentStatus(ctx context.Context, ctrl ControllerAuth) (healthy bool, detail string) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return false, "未配置 Base URL"
	}
	conf := ParseMHAAppConf(ctrl.AppConf)
	{
		u := base + "/healthz"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return false, err.Error()
		}
		if strings.TrimSpace(ctrl.Username) != "" {
			req.SetBasicAuth(ctrl.Username, ctrl.Password)
		}
		client := httpx.AgentHTTP(mhaProbeTimeout)
		resp, err := client.Do(req)
		if err != nil {
			return false, "Agent 不可达: " + err.Error()
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return false, fmt.Sprintf("Agent healthz HTTP %d", resp.StatusCode)
		}
	}
	u, _ := url.Parse(base + "/v1/mha/status")
	q := u.Query()
	q.Set("conf", conf)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return true, "healthz ok；status 请求构建失败"
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(mhaProbeTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return true, "healthz ok；status 调用失败: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return true, fmt.Sprintf("healthz ok；status HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 240 {
		msg = msg[:240] + "..."
	}
	return true, "MHA Agent 可达；status=" + msg
}

// MHAAgentReplOK 调用 check_repl；不可达时 ok=false。
func MHAAgentReplOK(ctx context.Context, ctrl ControllerAuth) (ok bool, detail string) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return false, "未配置 Base URL"
	}
	conf := ParseMHAAppConf(ctrl.AppConf)
	u, _ := url.Parse(base + "/v1/mha/repl")
	q := u.Query()
	q.Set("conf", conf)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err.Error()
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(mhaProbeTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return false, "repl 调用失败: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return false, fmt.Sprintf("repl HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &out) == nil {
		if out.OK {
			return true, firstNonEmptyHA(out.Message, "check_repl ok")
		}
		return false, firstNonEmptyHA(out.Message, "check_repl failed")
	}
	return true, strings.TrimSpace(string(body))
}

// MHAAgentPutConf 将 app.cnf 内容下发到 Manager Agent（须在 conf-root 下）。
func MHAAgentPutConf(ctx context.Context, ctrl ControllerAuth, confPath, content string) error {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return fmt.Errorf("未配置 Base URL")
	}
	path := strings.TrimSpace(confPath)
	if path == "" {
		path = ParseMHAAppConf(ctrl.AppConf)
	}
	payload, _ := json.Marshal(map[string]any{
		"conf":    path,
		"content": content,
	})
	u := base + "/v1/mha/conf"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("MHA Agent 下发失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("MHA Agent HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &out) == nil && !out.OK {
		return fmt.Errorf("下发失败: %s", firstNonEmptyHA(out.Message, string(body)))
	}
	return nil
}

// MHAAgentDeadSwitch 主宕机强制切换。
func MHAAgentDeadSwitch(ctx context.Context, ctrl ControllerAuth, cluster *store.HACluster, candidate *store.Instance) error {
	conf := ParseMHAAppConf(ctrl.AppConf)
	payload := map[string]any{
		"conf":            conf,
		"timeout_seconds": int(mhaSwitchTimeout.Seconds()),
	}
	if candidate != nil {
		host := strings.TrimSpace(candidate.Host)
		if host == "" {
			host = strings.TrimSpace(candidate.DomainName)
		}
		if host != "" && candidate.Port > 0 {
			payload["new_master_host"] = host
			payload["new_master_port"] = candidate.Port
		}
	}
	body, _ := json.Marshal(payload)
	u := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/") + "/v1/mha/switch/dead"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(mhaSwitchTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("MHA Agent dead switch 失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("MHA Agent HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &out) == nil && !out.OK {
		return fmt.Errorf("MHA dead switch 失败: %s", firstNonEmptyHA(out.Message, string(raw)))
	}
	return nil
}

// MHAAgentManagerCtrl start|stop|restart|status。
func MHAAgentManagerCtrl(ctx context.Context, ctrl ControllerAuth, action, confPath string) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("未配置 Base URL")
	}
	conf := strings.TrimSpace(confPath)
	if conf == "" {
		conf = ParseMHAAppConf(ctrl.AppConf)
	}
	action = strings.ToLower(strings.TrimSpace(action))
	var req *http.Request
	var err error
	if action == "status" {
		u, _ := url.Parse(base + "/v1/mha/manager/status")
		q := u.Query()
		q.Set("conf", conf)
		u.RawQuery = q.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	} else {
		payload, _ := json.Marshal(map[string]string{"conf": conf})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/mha/manager/"+action, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(60 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if ok, _ := out["ok"].(bool); !ok && out["ok"] != nil {
			msg, _ := out["message"].(string)
			return out, fmt.Errorf("%s", firstNonEmptyHA(msg, string(raw)))
		}
	}
	return out, nil
}

// MHAAgentPackageInstall 触发装包任务。
func MHAAgentPackageInstall(ctx context.Context, ctrl ControllerAuth, body map[string]any) (jobID string, err error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/mha/package/install", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var out struct {
		OK    bool   `json:"ok"`
		JobID string `json:"job_id"`
		Msg   string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || !out.OK {
		return "", fmt.Errorf("%s", firstNonEmptyHA(out.Msg, string(raw)))
	}
	return out.JobID, nil
}

// MHAAgentPackageJob 查询装包任务。
func MHAAgentPackageJob(ctx context.Context, ctrl ControllerAuth, jobID string) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/mha/package/jobs/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func firstNonEmptyHA(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
