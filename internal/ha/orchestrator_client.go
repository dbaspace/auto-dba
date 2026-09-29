package ha

import (
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

// ParseOrchClusterAlias 从控制器 remark 解析 cluster_alias（纯字符串或 JSON）。
func ParseOrchClusterAlias(remark string) string {
	s := strings.TrimSpace(remark)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "{") {
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil {
			if v, ok := m["cluster_alias"].(string); ok {
				return strings.TrimSpace(v)
			}
			if v, ok := m["alias"].(string); ok {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	return s
}

// OrchestratorGetCluster 拉取实例所在集群拓扑摘要。
func OrchestratorGetCluster(ctx context.Context, ctrl ControllerAuth, host string, port int) (map[string]any, error) {
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("host/port 无效")
	}
	path := fmt.Sprintf("/api/cluster/%s/%d", url.PathEscape(host), port)
	raw, code, err := orchHTTP(ctx, ctrl, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("Orchestrator HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"raw": string(raw)}
	}
	return out, nil
}

// OrchestratorEnvCheck 探活 + 可选实例发现。
func OrchestratorEnvCheck(ctx context.Context, ctrl ControllerAuth, hintHost string, hintPort int) (ok bool, summary string, checks []map[string]any) {
	ok = true
	add := func(name string, good bool, detail string) {
		checks = append(checks, map[string]any{"name": name, "ok": good, "detail": detail})
		if !good {
			ok = false
		}
	}
	raw, code, err := orchHTTP(ctx, ctrl, http.MethodGet, "/api/health")
	if err != nil {
		// 部分版本无 /api/health，回退 clusters-info
		raw2, code2, err2 := orchHTTP(ctx, ctrl, http.MethodGet, "/api/clusters-info")
		if err2 != nil {
			add("reachability", false, err.Error())
			return false, "Orchestrator 不可达", checks
		}
		if code2 >= 300 {
			add("reachability", false, fmt.Sprintf("HTTP %d", code2))
			return false, "Orchestrator 不可达", checks
		}
		add("reachability", true, "clusters-info ok")
		_ = raw2
	} else if code >= 300 {
		add("health", false, fmt.Sprintf("HTTP %d: %s", code, trimOrch(string(raw))))
	} else {
		add("health", true, "ok")
	}
	if hintHost != "" && hintPort > 0 {
		_, err := OrchestratorGetCluster(ctx, ctrl, hintHost, hintPort)
		if err != nil {
			add("cluster", false, err.Error())
		} else {
			add("cluster", true, fmt.Sprintf("%s:%d", hintHost, hintPort))
		}
	}
	if ok {
		summary = "Orchestrator 环境检查通过"
	} else {
		summary = "Orchestrator 环境检查未通过"
	}
	return ok, summary, checks
}

func orchHTTP(ctx context.Context, ctrl ControllerAuth, method, path string) ([]byte, int, error) {
	base := strings.TrimRight(strings.TrimSpace(ctrl.BaseURL), "/")
	if base == "" {
		return nil, 0, fmt.Errorf("未配置 Orchestrator Base URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, 0, err
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

func trimOrch(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

// orchestratorTakeover 计划内提升指定副本。
func orchestratorTakeover(ctx context.Context, ctrl ControllerAuth, candidate *store.Instance) error {
	host := strings.TrimSpace(candidate.Host)
	if host == "" {
		host = strings.TrimSpace(candidate.DomainName)
	}
	if host == "" || candidate.Port <= 0 {
		return fmt.Errorf("目标实例缺少 host/port")
	}
	// 指定新主：/api/graceful-master-takeover/:designatedHost/:designatedPort
	path := fmt.Sprintf("/api/graceful-master-takeover/%s/%d", url.PathEscape(host), candidate.Port)
	raw, code, err := orchHTTP(ctx, ctrl, http.MethodGet, path)
	if err != nil {
		return fmt.Errorf("Orchestrator 调用失败: %w", err)
	}
	if code >= 300 {
		return fmt.Errorf("Orchestrator HTTP %d: %s", code, trimOrch(string(raw)))
	}
	return nil
}

// orchestratorForceFailover 强制切主（主宕/紧急）。
func orchestratorForceFailover(ctx context.Context, ctrl ControllerAuth, primary *store.Instance, clusterAlias string) error {
	alias := strings.TrimSpace(clusterAlias)
	if alias != "" {
		path := "/api/force-master-failover/" + url.PathEscape(alias)
		raw, code, err := orchHTTP(ctx, ctrl, http.MethodGet, path)
		if err != nil {
			return err
		}
		if code < 300 {
			return nil
		}
		// 回退 host/port
		_ = raw
	}
	if primary == nil {
		return fmt.Errorf("强制切换需要集群 alias（控制器备注）或当前主 host/port")
	}
	host := strings.TrimSpace(primary.Host)
	if host == "" {
		host = strings.TrimSpace(primary.DomainName)
	}
	if host == "" || primary.Port <= 0 {
		return fmt.Errorf("当前主缺少 host/port")
	}
	path := fmt.Sprintf("/api/force-master-failover/%s/%d", url.PathEscape(host), primary.Port)
	raw, code, err := orchHTTP(ctx, ctrl, http.MethodGet, path)
	if err != nil {
		return fmt.Errorf("Orchestrator force failover: %w", err)
	}
	if code >= 300 {
		return fmt.Errorf("Orchestrator HTTP %d: %s", code, trimOrch(string(raw)))
	}
	return nil
}
