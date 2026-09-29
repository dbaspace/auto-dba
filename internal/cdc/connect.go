package cdc

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
)

const connectHTTPTimeout = 30 * time.Second

// NormalizeConnectBaseURL 将连接器 dbz_host 规范为 Kafka Connect REST 根地址（无末尾 /）。
// 对齐 hub normalizeConnectBaseURL：已有 scheme 原样保留；host:port 补 http://；仅 host 补 :8083。
func NormalizeConnectBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return strings.TrimRight(s, "/")
	}
	if strings.Contains(s, "://") {
		return strings.TrimRight(s, "/")
	}
	if strings.Contains(s, ":") {
		return "http://" + s
	}
	return "http://" + s + ":8083"
}

// ResolveConnectBaseURL 按优先级取第一个非空 Connect 地址：任务 extra → 连接器 dbz_host → 全局配置。
func ResolveConnectBaseURL(override, workerURL, connectorHost, cfgURL string) string {
	for _, s := range []string{override, workerURL, connectorHost, cfgURL} {
		if u := NormalizeConnectBaseURL(s); u != "" {
			return u
		}
	}
	return ""
}

// ResolveConnectBase 注册表 dbz_host 优先于任务 extra 中创建时固化的 connect_base_url，
// 否则 Debezium 服务迁址后任务仍指向旧地址。
func (o *Orchestrator) ResolveConnectBase(extra map[string]string) string {
	dbz := ""
	if o != nil && o.Store != nil && extra != nil {
		if id := strings.TrimSpace(extra["connector_id"]); id != "" {
			if c, err := o.Store.GetConnector(id); err == nil && c != nil {
				dbz = c.DBZHost
			}
		}
	}
	cfgURL := ""
	if o != nil {
		cfgURL = o.Cfg.DebeziumURL
	}
	extraURL, worker := "", ""
	if extra != nil {
		extraURL = extra["connect_base_url"]
		worker = extra["worker_url"]
	}
	return ResolveConnectBaseURL(dbz, extraURL, worker, cfgURL)
}

func (o *Orchestrator) persistConnectBase(extra map[string]string) string {
	base := o.ResolveConnectBase(extra)
	if extra != nil && base != "" {
		extra["connect_base_url"] = base
	}
	return base
}

func connectHTTPClient() *http.Client {
	return &http.Client{Timeout: connectHTTPTimeout}
}

func setConnectJSONHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
}

func connectDo(ctx context.Context, base, method, path string, body any) ([]byte, int, error) {
	base = NormalizeConnectBaseURL(base)
	if base == "" {
		return nil, 0, fmt.Errorf("未配置 Connect 地址：请在任务中选择 Debezium 连接器，或在 config.yaml 填写 cdc.debezium_url")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		setConnectJSONHeaders(req)
	} else {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := connectHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, nil
}

func connectConnectorExists(ctx context.Context, base, name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, code, err := connectDo(ctx, base, http.MethodGet, "/connectors/"+url.PathEscape(name), nil)
	return err == nil && code == http.StatusOK
}

func connectPutConfig(ctx context.Context, base, name string, cfg map[string]string) error {
	raw, code, err := connectDo(ctx, base, http.MethodPut, "/connectors/"+url.PathEscape(name)+"/config", cfg)
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fmt.Errorf("PUT %s/connectors/%s/config: status %d: %s", NormalizeConnectBaseURL(base), name, code, strings.TrimSpace(string(raw)))
	}
	return nil
}

func connectGetConfig(ctx context.Context, base, name string) (map[string]string, error) {
	raw, code, err := connectDo(ctx, base, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/config", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("debezium GET config -> %d %s", code, strings.TrimSpace(string(raw)))
	}
	return ParseConfigMap(string(raw)), nil
}

func connectGetStatus(ctx context.Context, base, name string) (map[string]any, error) {
	raw, code, err := connectDo(ctx, base, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/status", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if code >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(out) == 0 {
			return map[string]any{"ok": false, "http_status": code, "message": fmt.Sprintf("debezium GET status -> %d %s", code, msg)}, nil
		}
		out["http_status"] = code
		return out, fmt.Errorf("debezium GET status -> %d %s", code, msg)
	}
	return out, nil
}

// EnsureDebeziumConnector 幂等创建/更新：已存在则 PUT 扁平 config，否则 POST envelope；409 再 PUT。
func EnsureDebeziumConnector(ctx context.Context, base, name string, cfg map[string]string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("connector 名称为空")
	}
	if connectConnectorExists(ctx, base, name) {
		if err := connectPutConfig(ctx, base, name, cfg); err != nil {
			return fmt.Errorf("update connector config: %w", explainDebeziumConnectError(err, cfg))
		}
		return nil
	}
	envelope := map[string]any{"name": name, "config": cfg}
	raw, code, err := connectDo(ctx, base, http.MethodPost, "/connectors", envelope)
	if err != nil {
		return fmt.Errorf("create connector: %w", err)
	}
	if code/100 == 2 {
		return nil
	}
	if code == http.StatusConflict {
		if err := connectPutConfig(ctx, base, name, cfg); err == nil {
			return nil
		}
	}
	err = fmt.Errorf("POST %s/connectors: status %d: %s", NormalizeConnectBaseURL(base), code, strings.TrimSpace(string(raw)))
	return fmt.Errorf("create connector: %w", explainDebeziumConnectError(err, cfg))
}

// connectRestartBackoff 为 409（rebalance 中）时的重试间隔；变量便于单测缩短。
var connectRestartBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// connectRestartConnectorAndTasks 连同 Task 一起重启：Debezium PG 的复制连接由 Task 持有，
// 不带 includeTasks 的 restart 只重启 Connector 对象，slot 的 active_pid 不会变。
// Connect 3.0+（KIP-745）返回 202；旧版忽略参数返回 204，此时逐个 POST tasks/{id}/restart。
func connectRestartConnectorAndTasks(ctx context.Context, base, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("connector 名称为空")
	}
	esc := url.PathEscape(name)
	code, err := connectPostRetry(ctx, base, "/connectors/"+esc+"/restart?includeTasks=true&onlyFailed=false")
	if err != nil {
		return err
	}
	if code == http.StatusAccepted {
		return nil
	}
	ids, err := connectTaskIDs(ctx, base, name)
	if err != nil {
		return fmt.Errorf("connector %s 已重启，但读取 task 列表失败: %w", name, err)
	}
	for _, id := range ids {
		if _, err := connectPostRetry(ctx, base, fmt.Sprintf("/connectors/%s/tasks/%d/restart", esc, id)); err != nil {
			return fmt.Errorf("重启 %s task %d: %w", name, id, err)
		}
	}
	return nil
}

func connectPostRetry(ctx context.Context, base, path string) (int, error) {
	for attempt := 0; ; attempt++ {
		raw, code, err := connectDo(ctx, base, http.MethodPost, path, nil)
		if err != nil {
			return 0, err
		}
		if code/100 == 2 {
			return code, nil
		}
		msg := fmt.Errorf("POST %s -> %d %s", path, code, strings.TrimSpace(string(raw)))
		if code != http.StatusConflict || attempt >= len(connectRestartBackoff) {
			return code, msg
		}
		select {
		case <-ctx.Done():
			return code, fmt.Errorf("%v（等待重试时超时）", msg)
		case <-time.After(connectRestartBackoff[attempt]):
		}
	}
}

type connectStatusResp struct {
	Connector struct {
		State string `json:"state"`
		Trace string `json:"trace"`
	} `json:"connector"`
	Tasks []struct {
		ID    int    `json:"id"`
		State string `json:"state"`
		Trace string `json:"trace"`
	} `json:"tasks"`
}

func connectFetchStatus(ctx context.Context, base, name string) (*connectStatusResp, error) {
	raw, code, err := connectDo(ctx, base, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/status", nil)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("GET status -> %d %s", code, strings.TrimSpace(string(raw)))
	}
	var st connectStatusResp
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("解析 status: %w", err)
	}
	return &st, nil
}

func connectTaskIDs(ctx context.Context, base, name string) ([]int, error) {
	st, err := connectFetchStatus(ctx, base, name)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(st.Tasks))
	for _, t := range st.Tasks {
		ids = append(ids, t.ID)
	}
	return ids, nil
}

var errNotSettled = fmt.Errorf("connector 未在限定时间内进入 RUNNING")

// connectWaitPoll 为轮询间隔；变量便于单测缩短。
var connectWaitPoll = time.Second

// connectWaitRunning 等待 connector 与全部 task 进入 RUNNING；任一 FAILED 立即返回带 trace 首行的错误，超时返回 errNotSettled。
func connectWaitRunning(ctx context.Context, base, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			return errNotSettled
		case <-time.After(connectWaitPoll):
		}
		st, err := connectFetchStatus(ctx, base, name)
		if err == nil {
			if strings.EqualFold(st.Connector.State, "FAILED") {
				return fmt.Errorf("connector %s FAILED: %s", name, firstLine(st.Connector.Trace))
			}
			running := strings.EqualFold(st.Connector.State, "RUNNING") && len(st.Tasks) > 0
			for _, t := range st.Tasks {
				if strings.EqualFold(t.State, "FAILED") {
					return fmt.Errorf("connector %s task %d FAILED: %s", name, t.ID, firstLine(t.Trace))
				}
				if !strings.EqualFold(t.State, "RUNNING") {
					running = false
				}
			}
			if running {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errNotSettled
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func explainDebeziumConnectError(rawErr error, cfg map[string]string) error {
	if rawErr == nil {
		return nil
	}
	low := strings.ToLower(rawErr.Error())
	signals := []string{
		"the connection attempt failed",
		"connection refused",
		"could not connect",
		"connection timed out",
		"no route to host",
		"unknownhost",
		"i/o error",
	}
	matched := false
	for _, s := range signals {
		if strings.Contains(low, s) {
			matched = true
			break
		}
	}
	if !matched {
		return rawErr
	}
	return fmt.Errorf(
		"Kafka Connect 无法连接源 PostgreSQL %s:%s/%s。请确认 Connect Worker 到该地址网络连通，且账号/端口正确。原始错误：%s",
		cfg["database.hostname"], cfg["database.port"], cfg["database.dbname"], rawErr.Error(),
	)
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
