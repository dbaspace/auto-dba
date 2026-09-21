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
	return ResolveConnectBaseURL(extraURL, worker, dbz, cfgURL)
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
			return map[string]any{"ok": false, "message": fmt.Sprintf("debezium GET status -> %d %s", code, msg)}, nil
		}
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
