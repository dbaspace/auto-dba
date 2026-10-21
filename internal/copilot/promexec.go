package copilot

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

// 面板里常见的聚合函数。这些标识不是指标名，不能被套上标签。
var promFunc = map[string]bool{
	"rate": true, "irate": true, "sum": true, "avg": true, "max": true, "min": true,
	"count": true, "clamp_min": true, "clamp_max": true, "topk": true, "bottomk": true,
	"by": true, "without": true, "on": true, "ignoring": true, "group_left": true,
	"group_right": true, "and": true, "or": true, "unless": true, "offset": true,
	"bool": true, "increase": true, "abs": true, "label_replace": true,
}

var (
	promIdentRE = regexp.MustCompile(`[A-Za-z_:][A-Za-z0-9_:]*`)
	promBraceRE = regexp.MustCompile(`\{[^{}]*\}`)
	promLabelRE = regexp.MustCompile(`(?i)\b(instance|domain|job|host|url|tenant)\s*=`)
	promModeRE  = regexp.MustCompile(`^mode="[A-Za-z0-9_-]+"$`)
	promNameRE  = regexp.MustCompile(`^__name__=~"[A-Za-z0-9_.*]+"$`)
)

// SeriesStat 一条序列在窗口内的汇总。执行器只认这个结构，不认具体指标。
type SeriesStat struct {
	Labels map[string]string `json:"labels"`
	Min    float64           `json:"min"`
	Avg    float64           `json:"avg"`
	Max    float64           `json:"max"`
	N      int               `json:"n"`
	Trend  string            `json:"trend,omitempty"` // 升高 | 持平 | 下降
}

// PromResult 一次表达式在若干标签候选上的结果。
type PromResult struct {
	OK       bool
	PromQL   string
	Selector string
	Tried    []string
	Series   []SeriesStat
	Note     string
}

// ExecProm 用平台实例的标签候选执行任意指标表达式。
// hours>0 走 query_range，否则走瞬时查询。空序列换下一个标签；转义错误会去掉反斜杠再查一次。
func (s *Service) ExecProm(ctx context.Context, inst *store.Instance, expr string, hours int) PromResult {
	if err := validatePromExpr(expr); err != nil {
		return PromResult{Note: err.Error()}
	}
	if inst == nil {
		return PromResult{Note: "未对上平台实例，未查询 Prometheus"}
	}
	cands := selectorCandidates(inst)
	if len(cands) == 0 {
		return PromResult{Note: "实例没有可用的 Prometheus 标签"}
	}
	var lastErr error
	var tried []string
	for _, sel := range cands {
		promql := applySelector(expr, sel)
		samples, err := s.queryPromSamples(ctx, promql, hours)
		tried = append(tried, sel)
		if err != nil && strings.Contains(err.Error(), "unknown escape sequence") {
			fixed := stripPromEscape(promql)
			samples, err = s.queryPromSamples(ctx, fixed, hours)
			tried = append(tried, sel+" #strip")
			promql = fixed
		}
		if err != nil {
			lastErr = err
			continue
		}
		if len(samples) == 0 {
			continue
		}
		if len(samples) > maxSeries {
			samples = samples[:maxSeries]
		}
		return PromResult{
			OK: true, PromQL: promql, Selector: sel, Tried: tried,
			Series: seriesStats(samples),
		}
	}
	note := "候选用完仍没有序列"
	if lastErr != nil {
		note = lastErr.Error()
	}
	return PromResult{Tried: tried, Note: note}
}

func (r PromResult) Text() string {
	if !r.OK {
		return fmt.Sprintf("未得到序列。已尝试：%s。%s", strings.Join(r.Tried, " | "), r.Note)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "标签 %s\n", r.Selector)
	for _, s := range r.Series {
		fmt.Fprintf(&b, "- %s min=%.1f avg=%.1f max=%.1f n=%d\n", labelText(s.Labels), s.Min, s.Avg, s.Max, s.N)
	}
	return b.String()
}

func validatePromExpr(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("指标表达式为空")
	}
	if strings.Contains(expr, ";") || strings.Contains(expr, "`") || strings.Contains(expr, `\`) {
		return fmt.Errorf("表达式含非法字符")
	}
	if promLabelRE.MatchString(expr) {
		return fmt.Errorf("表达式不能自带 instance/domain/host/url/tenant 标签")
	}
	for _, m := range promBraceRE.FindAllString(expr, -1) {
		inner := strings.TrimSpace(strings.Trim(m, "{}"))
		if inner == "" || inner == "__SEL__" {
			continue
		}
		for _, part := range strings.Split(inner, ",") {
			part = strings.TrimSpace(part)
			if part == "" || part == "__SEL__" {
				continue
			}
			if !promModeRE.MatchString(part) && !promNameRE.MatchString(part) {
				return fmt.Errorf("表达式不能自带标签 %s", part)
			}
		}
	}
	return nil
}

func applySelector(expr, sel string) string {
	if strings.Contains(expr, "__SEL__") {
		return strings.ReplaceAll(expr, "__SEL__", sel)
	}
	out := expr
	if strings.Contains(out, "{") {
		out = promBraceRE.ReplaceAllStringFunc(out, func(g string) string {
			inner := strings.TrimSpace(strings.Trim(g, "{}"))
			if inner == "" {
				return "{" + sel + "}"
			}
			return "{" + inner + "," + sel + "}"
		})
	}
	return injectBareMetrics(out, sel)
}

func injectBareMetrics(expr, sel string) string {
	locs := promIdentRE.FindAllStringIndex(expr, -1)
	if len(locs) == 0 {
		return expr
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		id := expr[loc[0]:loc[1]]
		b.WriteString(expr[last:loc[1]])
		last = loc[1]
		if promFunc[strings.ToLower(id)] {
			continue
		}
		rest := strings.TrimLeft(expr[loc[1]:], " \t")
		if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "(") ||
			strings.HasPrefix(rest, ")") || strings.HasPrefix(rest, ",") {
			continue
		}
		b.WriteString("{" + sel + "}")
	}
	b.WriteString(expr[last:])
	return b.String()
}

func selectorCandidates(inst *store.Instance) []string {
	if inst == nil {
		return nil
	}
	host := strings.TrimSpace(inst.Host)
	domain := strings.TrimSpace(inst.DomainName)
	var out []string
	add := func(sel string) {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			return
		}
		for _, c := range out {
			if c == sel {
				return
			}
		}
		out = append(out, sel)
	}
	if isStarRocksLike(inst.DBType) {
		if inst.IDC != "" && domain != "" {
			add(fmt.Sprintf("idc=%q,domain=%q", inst.IDC, domain))
		}
		if domain != "" {
			add(fmt.Sprintf("domain=%q", domain))
		}
		if host != "" {
			add(fmt.Sprintf("domain=%q", host))
		}
	} else if domain != "" {
		add(fmt.Sprintf("domain=%q", domain))
	}
	for _, name := range []string{host, domain} {
		if err := validatePromHost(name); err != nil {
			continue
		}
		add(fmt.Sprintf(`instance=~"%s.*"`, promHostRegex(name)))
	}
	return out
}

func stripPromEscape(promql string) string {
	return strings.ReplaceAll(promql, `\`, "")
}

func (s *Service) queryPromSamples(ctx context.Context, promql string, hours int) ([]promclient.Sample, error) {
	if s != nil && s.promSamples != nil {
		return s.promSamples(ctx, promql, hours)
	}
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("未配置 Prometheus 地址")
	}
	base, _ := s.Store.GetGlobalArg(sourcePromBase)
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, fmt.Errorf("未配置 Prometheus 地址（awr_prometheus_base_url）")
	}
	token, _ := s.Store.GetGlobalArg(sourcePromToken)
	cli := promclient.New(promclient.Config{BaseURL: base, AuthToken: token, Timeout: sqlTimeout})
	if cli == nil {
		return nil, fmt.Errorf("Prometheus 地址无效")
	}
	if hours <= 0 {
		return cli.Query(ctx, promql, time.Now())
	}
	if hours > 6 {
		hours = 6
	}
	end := time.Now()
	start := end.Add(-time.Duration(hours) * time.Hour)
	return cli.QueryRange(ctx, promql, start, end, time.Minute)
}

func seriesStats(samples []promclient.Sample) []SeriesStat {
	out := make([]SeriesStat, 0, len(samples))
	for _, s := range samples {
		vals := sampleValues(s)
		if len(vals) == 0 {
			continue
		}
		min, max, sum := vals[0], vals[0], 0.0
		for _, v := range vals {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += v
		}
		out = append(out, SeriesStat{
			Labels: s.Metric, Min: min, Avg: sum / float64(len(vals)), Max: max, N: len(vals),
			Trend: seriesTrend(vals),
		})
	}
	return out
}

func seriesTrend(vals []float64) string {
	if len(vals) < 2 {
		return ""
	}
	mid := len(vals) / 2
	if mid == 0 {
		return ""
	}
	first := avgVals(vals[:mid])
	second := avgVals(vals[mid:])
	switch {
	case second > first*1.1:
		return "升高"
	case second < first*0.9:
		return "下降"
	default:
		return "持平"
	}
}

func avgVals(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func labelText(m map[string]string) string {
	if len(m) == 0 {
		return "(无标签)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}
