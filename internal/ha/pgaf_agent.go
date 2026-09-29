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

// PGAFOptions 存在控制器 remark（JSON）中。
type PGAFOptions struct {
	Formation string `json:"formation"`
	Group     string `json:"group"`
	Monitor   string `json:"monitor"` // postgres:// URI
	PGData    string `json:"pgdata"`  // monitor 本机 pgdata
}

func ParsePGAFOptions(remark string) PGAFOptions {
	opt := PGAFOptions{Formation: "default", Group: "0"}
	s := strings.TrimSpace(remark)
	if s == "" {
		return opt
	}
	if strings.HasPrefix(s, "{") {
		_ = json.Unmarshal([]byte(s), &opt)
	} else if strings.Contains(s, "://") {
		opt.Monitor = s
	} else {
		opt.PGData = s
	}
	if opt.Formation == "" {
		opt.Formation = "default"
	}
	return opt
}

func (o PGAFOptions) candidateName(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	if n := strings.TrimSpace(inst.Name); n != "" {
		return n
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	return host
}

// PGAFAgentShowState 查询 formation 状态。
func PGAFAgentShowState(ctx context.Context, ctrl ControllerAuth) (map[string]any, error) {
	opt := ParsePGAFOptions(ctrl.AppConf)
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	u, _ := url.Parse(base + "/v1/pgaf/status")
	q := u.Query()
	q.Set("formation", opt.Formation)
	if opt.Group != "" {
		q.Set("group", opt.Group)
	}
	if opt.Monitor != "" {
		q.Set("monitor", opt.Monitor)
	}
	if opt.PGData != "" {
		q.Set("pgdata", opt.PGData)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(90 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pgaf status: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if ok, _ := out["ok"].(bool); out["ok"] != nil && !ok {
		msg, _ := out["message"].(string)
		return out, fmt.Errorf("%s", firstNonEmptyHA(msg, string(raw)))
	}
	return out, nil
}

func pgafPerform(ctx context.Context, ctrl ControllerAuth, action string, candidate *store.Instance) error {
	opt := ParsePGAFOptions(ctrl.AppConf)
	payload := map[string]any{
		"formation":       opt.Formation,
		"group":           opt.Group,
		"monitor":         opt.Monitor,
		"pgdata":          opt.PGData,
		"timeout_seconds": 120,
	}
	if candidate != nil {
		payload["candidate"] = opt.candidateName(candidate)
	}
	body, _ := json.Marshal(payload)
	path := "/v1/pgaf/switchover"
	if action == "failover" {
		path = "/v1/pgaf/failover"
	}
	u := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	client := httpx.AgentHTTP(150 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pgaf %s: %w", action, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var out struct {
		OK  bool   `json:"ok"`
		Msg string `json:"message"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || !out.OK {
		return fmt.Errorf("%s", firstNonEmptyHA(out.Msg, string(raw)))
	}
	return nil
}

func pgafAgentPlannedSwitch(ctx context.Context, ctrl ControllerAuth, candidate *store.Instance) error {
	return pgafPerform(ctx, ctrl, "switchover", candidate)
}

func pgafAgentFailover(ctx context.Context, ctrl ControllerAuth, candidate *store.Instance) error {
	return pgafPerform(ctx, ctrl, "failover", candidate)
}

// PGAFAgentPackageInstall 触发装包（Agent /v1/pgaf/package/install）。
func PGAFAgentPackageInstall(ctx context.Context, ctrl ControllerAuth, body map[string]any) (jobID string, err error) {
	return HAAgentPackageInstall(ctx, ctrl, "/v1/pgaf/package/install", body)
}

// PatroniAgentPackageInstall 触发 Patroni 装包。
func PatroniAgentPackageInstall(ctx context.Context, ctrl ControllerAuth, body map[string]any) (jobID string, err error) {
	return HAAgentPackageInstall(ctx, ctrl, "/v1/patroni/package/install", body)
}

func HAAgentPackageInstall(ctx context.Context, ctrl ControllerAuth, path string, body map[string]any) (jobID string, err error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
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

// PGAFAgentPackageJob 查询装包任务。
func PGAFAgentPackageJob(ctx context.Context, ctrl ControllerAuth, jobID string) (map[string]any, error) {
	return HAAgentPackageJob(ctx, ctrl, "/v1/pgaf/package/jobs/", jobID)
}

// PatroniAgentPackageJob 查询 Patroni 装包任务。
func PatroniAgentPackageJob(ctx context.Context, ctrl ControllerAuth, jobID string) (map[string]any, error) {
	return HAAgentPackageJob(ctx, ctrl, "/v1/patroni/package/jobs/", jobID)
}

func HAAgentPackageJob(ctx context.Context, ctrl ControllerAuth, pathPrefix, jobID string) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+pathPrefix+jobID, nil)
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
