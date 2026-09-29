package awr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"strings"
)

// AwrReportExportHTML 取报告详情并渲染为自包含 HTML；format 目前支持 html（默认）。
// 返回 (html 内容, 建议文件名, error)。PDF 需外部依赖，未配置时回退 HTML。
func (s *Service) ExportHTML(ctx context.Context, q *ReportDetailQuery, format string) (string, string, error) {
	detail, err := s.DetailReport(ctx, q)
	if err != nil {
		return "", "", err
	}
	if detail == nil {
		return "", "", fmt.Errorf("awr report not found")
	}

	var sections AwrSections
	if len(detail.Sections) > 0 {
		_ = json.Unmarshal(detail.Sections, &sections)
	}

	view := awrRenderView{
		Item:     &detail.ReportItem,
		Sections: &sections,
	}
	var buf bytes.Buffer
	if err := awrReportTemplate.Execute(&buf, view); err != nil {
		return "", "", err
	}
	filename := fmt.Sprintf("awr_%s_%d_%s.html",
		sanitizeFilename(detail.Host), detail.Port, detail.ReportDate)
	// format 预留：非 html（如 pdf）暂统一回退 HTML 内容。
	_ = strings.ToLower(strings.TrimSpace(format))
	return buf.String(), filename, nil
}

type awrRenderView struct {
	Item     *ReportItem
	Sections *AwrSections
}

// AwrSummaryExportHTML 取全 domain 异常汇总并渲染为自包含 HTML。
// 返回 (html 内容, 建议文件名, error)。
func (s *Service) ExportSummaryHTML(ctx context.Context, q *SummaryQuery) (string, string, error) {
	data, err := s.SummaryReport(ctx, q)
	if err != nil {
		return "", "", err
	}
	if data == nil {
		return "", "", fmt.Errorf("awr summary not found")
	}
	var buf bytes.Buffer
	if err := awrSummaryTemplate.Execute(&buf, data); err != nil {
		return "", "", err
	}
	filename := fmt.Sprintf("awr_summary_%s.html", data.ReportDate)
	return buf.String(), filename, nil
}

func sanitizeFilename(s string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "\"", "_")
	return repl.Replace(strings.TrimSpace(s))
}

// awrFmtPtr 格式化可空浮点指标值。
func awrFmtPtr(v *float64) string {
	if v == nil {
		return "-"
	}
	return awrFmtDisplay(*v)
}

// awrFmtMetric 按单位感知格式化指标值：bytes 单位换算为 GB/MB，其余走通用格式化。
func awrFmtMetric(unit string, v *float64) string {
	if v == nil {
		return "-"
	}
	if strings.EqualFold(strings.TrimSpace(unit), "bytes") {
		return awrFmtBytes(*v)
	}
	return awrFmtDisplay(*v)
}

// awrFmtBytes 字节换算：>=1GB 用 GB，否则用 MB（1GB=1024^3，1MB=1024^2）。
func awrFmtBytes(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "-"
	}
	const (
		mb = 1024.0 * 1024.0
		gb = mb * 1024.0
	)
	if math.Abs(f) >= gb {
		return awrFmtDisplay(f/gb) + " GB"
	}
	return awrFmtDisplay(f/mb) + " MB"
}

// awrFmtDisplay 人类可读数值：不使用科学计数，按量级自适应小数位，整数部分加千分位。
func awrFmtDisplay(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "-"
	}
	a := math.Abs(f)
	var s string
	switch {
	case a == 0:
		return "0"
	case a >= 1000:
		s = strconv.FormatFloat(f, 'f', 0, 64) // 整数
	case a >= 100:
		s = strconv.FormatFloat(f, 'f', 1, 64)
	case a >= 1:
		s = strconv.FormatFloat(f, 'f', 2, 64)
	default:
		s = strconv.FormatFloat(f, 'f', 4, 64) // 小数(比率等)
	}
	return awrGroupThousands(awrTrimZeros(s))
}

// awrTrimZeros 去掉小数末尾多余的 0 与孤立的小数点（整数格式无小数点则原样返回）。
func awrTrimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// awrGroupThousands 对整数部分加千分位逗号，保留负号与小数部分。
func awrGroupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	n := len(intPart)
	if n > 3 {
		var b strings.Builder
		if pre := n % 3; pre > 0 {
			b.WriteString(intPart[:pre])
			if n > pre {
				b.WriteByte(',')
			}
		}
		for i := n % 3; i < n; i += 3 {
			b.WriteString(intPart[i : i+3])
			if i+3 < n {
				b.WriteByte(',')
			}
		}
		intPart = b.String()
	}
	out := intPart + frac
	if neg {
		out = "-" + out
	}
	return out
}

var awrReportTemplate = template.Must(template.New("awr").Funcs(template.FuncMap{
	"fmtPtr":    awrFmtPtr,
	"fmtMetric": awrFmtMetric,
}).Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>AWR 报告 {{.Item.Host}}:{{.Item.Port}} {{.Item.ReportDate}}</title>
<style>
  body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", Arial, sans-serif; margin: 24px; color: #222; }
  h1 { font-size: 20px; }
  h2 { font-size: 16px; border-bottom: 2px solid #eee; padding-bottom: 4px; margin-top: 28px; }
  table { border-collapse: collapse; width: 100%; margin: 8px 0 16px; font-size: 13px; }
  th, td { border: 1px solid #ddd; padding: 6px 8px; text-align: left; }
  th { background: #f5f7fa; }
  .meta td { border: none; padding: 2px 12px 2px 0; }
  .status-success { color: #2e7d32; }
  .status-partial { color: #ef6c00; }
  .status-failed  { color: #c62828; }
  .sql { font-family: Menlo, Consolas, monospace; white-space: pre-wrap; word-break: break-all; max-width: 720px; }
  .warn { color: #ef6c00; font-size: 12px; }
  .sev-critical { color: #c62828; font-weight: bold; }
  .sev-warning  { color: #ef6c00; font-weight: bold; }
  .sev-none     { color: #2e7d32; }
</style>
</head>
<body>
<h1>每日数据库 AWR 报告</h1>
<table class="meta">
  <tr><td>实例</td><td>{{.Item.InstanceName}} ({{.Item.Host}}:{{.Item.Port}})</td></tr>
  <tr><td>数据源</td><td>{{.Item.Source}}</td></tr>
  <tr><td>报告日</td><td>{{.Item.ReportDate}}</td></tr>
  <tr><td>统计时段</td><td>{{.Item.PeriodStart}} ~ {{.Item.PeriodEnd}}</td></tr>
  <tr><td>状态</td><td class="status-{{.Item.Status}}">{{.Item.Status}}</td></tr>
  <tr><td>异常级别</td><td class="sev-{{.Item.Severity}}">{{.Item.Severity}}（命中 {{.Item.AnomalyCount}} 项）</td></tr>
</table>

{{if .Item.Anomalies}}
<h2>异常明细</h2>
<table>
  <thead><tr><th>级别</th><th>指标</th><th>类型</th><th>观测值</th><th>阈值/基线</th><th>说明</th></tr></thead>
  <tbody>
  {{range .Item.Anomalies}}
    <tr>
      <td class="sev-{{.Severity}}">{{.Severity}}</td>
      <td>{{.Title}}</td>
      <td>{{if eq .Kind "dod"}}环比昨日{{else}}阈值{{end}}</td>
      <td>{{fmtPtr .Value}}{{.Unit}}</td>
      <td>{{if eq .Kind "dod"}}{{fmtPtr .Baseline}}{{.Unit}}{{else}}{{fmtPtr .Threshold}}{{.Unit}}{{end}}</td>
      <td>{{.Message}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{range .Sections.Sections}}
<h2>{{.Title}}</h2>
<table>
  <thead><tr><th>指标</th><th>均值</th><th>峰值</th><th>最小</th><th>最新</th><th>单位</th></tr></thead>
  <tbody>
  {{range .Metrics}}
    <tr>
      <td>{{.Title}}{{if .Error}} <span class="warn">({{.Error}})</span>{{end}}</td>
      <td>{{fmtMetric .Unit .Avg}}</td>
      <td>{{fmtMetric .Unit .Max}}</td>
      <td>{{fmtMetric .Unit .Min}}</td>
      <td>{{fmtMetric .Unit .Last}}</td>
      <td>{{if eq .Unit "bytes"}}GB/MB{{else}}{{.Unit}}{{end}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .Sections.TopSQL}}
<h2>Top SQL（按出现次数）</h2>
<table>
  <thead><tr><th>#</th><th>次数</th><th>平均事务时长(s)</th><th>最大事务时长(s)</th><th>SQL 模板</th></tr></thead>
  <tbody>
  {{range $i, $s := .Sections.TopSQL}}
    <tr>
      <td>{{$i}}</td>
      <td>{{$s.Count}}</td>
      <td>{{fmtPtr $s.AvgXactDuration}}</td>
      <td>{{fmtPtr $s.MaxXactDuration}}</td>
      <td class="sql">{{$s.QueryNormalized}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .Sections.LongTxn}}
<h2>长事务 Top（按最大事务时长）</h2>
<table>
  <thead><tr><th>#</th><th>次数</th><th>平均事务时长(s)</th><th>最大事务时长(s)</th><th>SQL 模板</th></tr></thead>
  <tbody>
  {{range $i, $s := .Sections.LongTxn}}
    <tr>
      <td>{{$i}}</td>
      <td>{{$s.Count}}</td>
      <td>{{fmtPtr $s.AvgXactDuration}}</td>
      <td>{{fmtPtr $s.MaxXactDuration}}</td>
      <td class="sql">{{$s.QueryNormalized}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .Sections.Warnings}}
<h2>采集告警</h2>
<ul>
  {{range .Sections.Warnings}}<li class="warn">{{.}}</li>{{end}}
</ul>
{{end}}

</body>
</html>`))

// awrSummaryTemplate 全 domain 异常汇总报告 HTML 模板（仅异常信息）。
var awrSummaryTemplate = template.Must(template.New("awrSummary").Funcs(template.FuncMap{
	"fmtPtr": awrFmtPtr,
}).Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>每日数据库异常汇总报告 {{.ReportDate}}</title>
<style>
  body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", Arial, sans-serif; margin: 24px; color: #222; }
  h1 { font-size: 20px; }
  h2 { font-size: 15px; margin-top: 24px; }
  table { border-collapse: collapse; width: 100%; margin: 6px 0 14px; font-size: 13px; }
  th, td { border: 1px solid #ddd; padding: 6px 8px; text-align: left; }
  th { background: #f5f7fa; }
  .meta td { border: none; padding: 2px 12px 2px 0; }
  .sev-critical { color: #c62828; font-weight: bold; }
  .sev-warning  { color: #ef6c00; font-weight: bold; }
  .sev-none     { color: #2e7d32; }
  .st-failed    { color: #6a1b9a; font-weight: bold; }
  .empty { color: #2e7d32; margin: 16px 0; }
</style>
</head>
<body>
<h1>每日数据库异常汇总报告</h1>
<table class="meta">
  <tr><td>报告日</td><td>{{.ReportDate}}</td></tr>
  <tr><td>报告总数</td><td>{{.Total}}</td></tr>
  <tr><td>异常概览</td><td><span class="sev-critical">严重 {{.CriticalCount}}</span> / <span class="sev-warning">告警 {{.WarningCount}}</span> / <span class="st-failed">生成失败 {{.FailedCount}}</span> / 正常 {{.NoneCount}}</td></tr>
  <tr><td>问题实例数</td><td>{{.IssueDomainCount}}（指标异常 {{.AnomalyDomainCount}}，命中规则 {{.AnomalyTotal}} 项；生成失败 {{.FailedCount}}）</td></tr>
  {{if .Domains}}
  <tr><td>问题实例清单</td><td>
    {{range .Domains}}<div class="{{if eq .Status "failed"}}st-failed{{else}}sev-{{.Severity}}{{end}}">{{if .InstanceName}}{{.InstanceName}} · {{end}}{{.Host}}:{{.Port}} {{if eq .Status "failed"}}[生成失败]{{else}}[{{.Severity}}] 命中 {{.AnomalyCount}} 项{{end}}</div>{{end}}
  </td></tr>
  {{end}}
</table>

{{if .AnomalyTypeStats}}
<h2>异常类型统计</h2>
<table>
  <thead><tr><th>指标</th><th>板块</th><th>异常实例数</th><th>严重</th><th>告警</th></tr></thead>
  <tbody>
  {{range .AnomalyTypeStats}}
    <tr>
      <td>{{.Title}}</td>
      <td>{{.Section}}</td>
      <td>{{.Count}}</td>
      <td class="sev-critical">{{.CriticalCount}}</td>
      <td class="sev-warning">{{.WarningCount}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}

{{if .Domains}}
{{range .Domains}}
{{if eq .Status "failed"}}
<h2 class="st-failed">{{if .InstanceName}}{{.InstanceName}} {{end}}({{.Host}}:{{.Port}}) [生成失败]</h2>
<table>
  <thead><tr><th>状态</th><th>数据源</th><th>失败原因</th></tr></thead>
  <tbody>
    <tr>
      <td class="st-failed">failed</td>
      <td>{{.Source}}</td>
      <td>{{if .ErrorMessage}}{{.ErrorMessage}}{{else}}-{{end}}</td>
    </tr>
  </tbody>
</table>
{{else}}
<h2 class="sev-{{.Severity}}">{{if .InstanceName}}{{.InstanceName}} {{end}}({{.Host}}:{{.Port}}) [{{.Severity}}] 命中 {{.AnomalyCount}} 项</h2>
{{if .Anomalies}}
<table>
  <thead><tr><th>级别</th><th>指标</th><th>类型</th><th>观测值</th><th>阈值/基线</th><th>说明</th></tr></thead>
  <tbody>
  {{range .Anomalies}}
    <tr>
      <td class="sev-{{.Severity}}">{{.Severity}}</td>
      <td>{{.Title}}</td>
      <td>{{if eq .Kind "dod"}}环比昨日{{else}}阈值{{end}}</td>
      <td>{{fmtPtr .Value}}{{.Unit}}</td>
      <td>{{if eq .Kind "dod"}}{{fmtPtr .Baseline}}{{.Unit}}{{else}}{{fmtPtr .Threshold}}{{.Unit}}{{end}}</td>
      <td>{{.Message}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{end}}
{{end}}
{{end}}
{{else}}
<p class="empty">当日无问题实例。</p>
{{end}}

</body>
</html>`))
