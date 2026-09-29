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
)

// PatroniCluster 摘要（GET /cluster）。
type PatroniCluster struct {
	Members []PatroniMember `json:"members"`
	Raw     map[string]any  `json:"raw,omitempty"`
}

type PatroniMember struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	State  string `json:"state"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	APIURL string `json:"api_url,omitempty"`
	Lag    any    `json:"lag,omitempty"`
}

func patroniHTTP(ctx context.Context, ctrl ControllerAuth, method, path string, body any) ([]byte, int, error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return nil, 0, fmt.Errorf("未配置 Patroni Base URL")
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(ctrl.Username) != "" {
		req.SetBasicAuth(ctrl.Username, ctrl.Password)
	}
	resp, err := httpx.AgentHTTP(60 * time.Second).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, nil
}

// PatroniGetCluster 拉取 /cluster。
func PatroniGetCluster(ctx context.Context, ctrl ControllerAuth) (*PatroniCluster, error) {
	raw, code, err := patroniHTTP(ctx, ctrl, http.MethodGet, "/cluster", nil)
	if err != nil {
		return nil, fmt.Errorf("Patroni /cluster: %w", err)
	}
	if code >= 300 {
		return nil, fmt.Errorf("Patroni HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var envelope map[string]any
	_ = json.Unmarshal(raw, &envelope)
	out := &PatroniCluster{Raw: envelope}
	// Patroni 返回 {"members":[...]} 或直接数组
	var members []map[string]any
	if m, ok := envelope["members"].([]any); ok {
		for _, it := range m {
			if mm, ok := it.(map[string]any); ok {
				members = append(members, mm)
			}
		}
	}
	for _, mm := range members {
		mem := PatroniMember{
			Name:   fmt.Sprint(mm["name"]),
			Role:   fmt.Sprint(mm["role"]),
			State:  fmt.Sprint(mm["state"]),
			Host:   fmt.Sprint(mm["host"]),
			APIURL: fmt.Sprint(mm["api_url"]),
			Lag:    mm["lag"],
		}
		switch p := mm["port"].(type) {
		case float64:
			mem.Port = int(p)
		case json.Number:
			v, _ := p.Int64()
			mem.Port = int(v)
		}
		out.Members = append(out.Members, mem)
	}
	return out, nil
}

// PatroniEnvCheck 控制器可达与成员健康摘要。
func PatroniEnvCheck(ctx context.Context, ctrl ControllerAuth) (ok bool, summary string, checks []map[string]any) {
	add := func(name string, good bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": good, "detail": detail})
		if !good {
			ok = false
		}
	}
	ok = true
	cl, err := PatroniGetCluster(ctx, ctrl)
	if err != nil {
		add("patroni_cluster", false, err.Error())
		return false, "Patroni 不可达", checks
	}
	add("patroni_cluster", true, fmt.Sprintf("%d 个成员", len(cl.Members)))
	leaders := 0
	running := 0
	for _, m := range cl.Members {
		role := strings.ToLower(m.Role)
		state := strings.ToLower(m.State)
		if role == "leader" || role == "master" {
			leaders++
		}
		if state == "running" || state == "" {
			running++
		}
	}
	if leaders != 1 {
		add("leader", false, fmt.Sprintf("leader 数量=%d（期望 1）", leaders))
	} else {
		add("leader", true, "存在唯一 leader")
	}
	add("members_running", running > 0, fmt.Sprintf("running≈%d/%d", running, len(cl.Members)))
	if ok {
		summary = "Patroni 环境检查通过"
	} else {
		summary = "Patroni 环境检查未通过"
	}
	return ok, summary, checks
}

func patroniSwitchover(ctx context.Context, ctrl ControllerAuth, candidateName string) error {
	payload := map[string]string{"candidate": candidateName}
	raw, code, err := patroniHTTP(ctx, ctrl, http.MethodPost, "/switchover", payload)
	if err != nil {
		return fmt.Errorf("Patroni switchover: %w", err)
	}
	if code >= 300 {
		return fmt.Errorf("Patroni HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	return nil
}

func patroniFailoverAPI(ctx context.Context, ctrl ControllerAuth, candidateName string) error {
	payload := map[string]string{}
	if candidateName != "" {
		payload["candidate"] = candidateName
	}
	raw, code, err := patroniHTTP(ctx, ctrl, http.MethodPost, "/failover", payload)
	if err != nil {
		return fmt.Errorf("Patroni failover: %w", err)
	}
	if code >= 300 {
		return fmt.Errorf("Patroni HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	return nil
}
