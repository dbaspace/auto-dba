package promclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeout = 20 * time.Second
	DefaultStep    = 60 * time.Second
	maxBodyBytes   = 64 << 20 // 64MB；触及上限视为截断
)

// Config Prometheus HTTP 客户端配置。
type Config struct {
	BaseURL         string
	AuthToken       string
	PartialResponse bool
	Dedup           bool
	Timeout         time.Duration
}

// Client Prometheus API 客户端。
type Client struct {
	baseURL         string
	authToken       string
	http            *http.Client
	timeout         time.Duration
	partialResponse bool
	dedup           bool
}

// New 创建客户端；baseURL 为空返回 nil。
func New(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil
	}
	base = strings.TrimSuffix(base, "/graph")
	base = strings.TrimRight(base, "/")
	to := cfg.Timeout
	if to <= 0 {
		to = DefaultTimeout
	}
	return &Client{
		baseURL:         base,
		authToken:       strings.TrimSpace(cfg.AuthToken),
		http:            &http.Client{Timeout: to},
		timeout:         to,
		partialResponse: cfg.PartialResponse,
		dedup:           cfg.Dedup,
	}
}

func (c *Client) baseParams() url.Values {
	q := url.Values{}
	if c.partialResponse {
		q.Set("partial_response", "true")
	}
	if c.dedup {
		q.Set("dedup", "true")
	}
	return q
}

type apiResponse struct {
	Status    string  `json:"status"`
	Data      apiData `json:"data"`
	ErrorType string  `json:"errorType"`
	Error     string  `json:"error"`
}

type apiData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

// Sample 单条 Prometheus 结果。
type Sample struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
	Values [][]interface{}   `json:"values"`
}

// Point 时序点。
type Point struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// timeoutForSpan 长窗放大 HTTP 超时，降低误报 Client.Timeout。
func timeoutForSpan(base time.Duration, start, end time.Time) time.Duration {
	if base <= 0 {
		base = DefaultTimeout
	}
	span := end.Sub(start)
	var want time.Duration
	switch {
	case span >= 3*24*time.Hour:
		want = 60 * time.Second
	case span >= 6*time.Hour:
		want = 45 * time.Second
	default:
		want = base
	}
	if want > base {
		return want
	}
	return base
}

// QueryRange 范围查询（POST，避免长 PromQL GET 400）。
func (c *Client) QueryRange(ctx context.Context, promql string, start, end time.Time, step time.Duration) ([]Sample, error) {
	if c == nil {
		return nil, fmt.Errorf("prometheus client not configured")
	}
	if step <= 0 {
		step = DefaultStep
	}
	q := c.baseParams()
	q.Set("query", promql)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))
	return c.do(ctx, "/api/v1/query_range", q, timeoutForSpan(c.timeout, start, end))
}

// Query 瞬时查询。
func (c *Client) Query(ctx context.Context, promql string, ts time.Time) ([]Sample, error) {
	if c == nil {
		return nil, fmt.Errorf("prometheus client not configured")
	}
	q := c.baseParams()
	q.Set("query", promql)
	if !ts.IsZero() {
		q.Set("time", strconv.FormatInt(ts.Unix(), 10))
	}
	return c.do(ctx, "/api/v1/query", q, c.timeout)
}

func (c *Client) do(ctx context.Context, path string, q url.Values, timeout time.Duration) ([]Sample, error) {
	// POST form：兼容网关对长 querystring 的 400，并正确传特殊字符
	body := q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.authToken != "" {
		req.Header.Set("Authorization", c.authToken)
	}
	cli := c.http
	if timeout > 0 && timeout != c.timeout {
		cli = &http.Client{Timeout: timeout, Transport: c.http.Transport}
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("prometheus empty response")
	}
	if len(raw) >= maxBodyBytes {
		return nil, fmt.Errorf("prometheus 响应过大（>%dMB），请缩小时间范围或增大 step", maxBodyBytes>>20)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 240 {
			msg = msg[:240] + "…"
		}
		if msg == "" {
			return nil, fmt.Errorf("prometheus http %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("prometheus http %d: %s", resp.StatusCode, msg)
	}
	var out apiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		preview := strings.TrimSpace(string(raw))
		if len(preview) > 160 {
			preview = preview[:160] + "…"
		}
		return nil, fmt.Errorf("prometheus json: %w (%s)", err, preview)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s %s", out.ErrorType, out.Error)
	}
	if len(out.Data.Result) == 0 || string(out.Data.Result) == "null" {
		return []Sample{}, nil
	}
	var samples []Sample
	if err := json.Unmarshal(out.Data.Result, &samples); err != nil {
		return nil, fmt.Errorf("prometheus result json: %w", err)
	}
	return samples, nil
}

// SampleToPoints 将 range 结果转为点列。
func SampleToPoints(s Sample) []Point {
	pts := make([]Point, 0, len(s.Values))
	for _, v := range s.Values {
		if len(v) != 2 {
			continue
		}
		ts, ok := ToFloat(v[0])
		if !ok {
			continue
		}
		val, ok := ToFloat(v[1])
		if !ok {
			continue
		}
		pts = append(pts, Point{T: int64(ts), V: val})
	}
	return pts
}

// InstantValue 取瞬时值。
func InstantValue(s Sample) (float64, bool) {
	if len(s.Value) == 2 {
		return ToFloat(s.Value[1])
	}
	if len(s.Values) > 0 {
		last := s.Values[len(s.Values)-1]
		if len(last) == 2 {
			return ToFloat(last[1])
		}
	}
	return 0, false
}

// ToFloat 解析 Prometheus JSON 数值。
func ToFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" || s == "NaN" || s == "+Inf" || s == "-Inf" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// SeriesStats 计算 max / current。
func SeriesStats(pts []Point) (max, current float64, ok bool) {
	if len(pts) == 0 {
		return 0, 0, false
	}
	max = pts[0].V
	for _, p := range pts {
		if p.V > max {
			max = p.V
		}
	}
	return max, pts[len(pts)-1].V, true
}
