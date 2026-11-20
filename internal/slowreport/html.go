package slowreport

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CompareRow 是周报对比表的一行。
type CompareRow struct {
	Name  string
	This  string
	Last  string
	Delta string
	Note  string
}

// RankRow 是一条慢 SQL 排名。
type RankRow struct {
	Rank   int
	SQL    string
	DB     string
	Calls  int
	Avg    string
	P99    string
	Rows   string
	Tables string
}

// EnvSection 是一个环境的指标和排名。
type EnvSection struct {
	Name      string
	Instances string
	Compare   []CompareRow
	Ranks     []RankRow
	Empty     bool
}

// Document 是一封周报。
type Document struct {
	Title    string
	Tenant   string
	Note     string
	ThisFrom time.Time
	ThisTo   time.Time
	Envs     []EnvSection
}

func FormatDuration(ms float64) string {
	if math.IsNaN(ms) || ms < 0 {
		return "—"
	}
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.0fms", ms)
}

func ChangePercent(cur, prev float64) string {
	if cur == 0 && prev == 0 {
		return "—"
	}
	if prev == 0 {
		return "新增"
	}
	pct := (cur - prev) / prev * 100
	return signedPercent(pct, "%")
}

func ChangePoints(cur, prev float64) string {
	if cur == 0 && prev == 0 {
		return "—"
	}
	if prev == 0 && cur > 0 {
		return "新增"
	}
	return signedPercent(cur-prev, "pp")
}

func signedPercent(v float64, unit string) string {
	if math.Abs(v) < 0.05 {
		return "0%"
	}
	arrow := "↑ "
	n := v
	if v < 0 {
		arrow = "↓ "
		n = -v
	}
	if unit == "pp" {
		return fmt.Sprintf("%s%.0fpp", arrow, n)
	}
	return fmt.Sprintf("%s%.0f%%", arrow, n)
}

var tableRE = regexp.MustCompile(`(?i)\b(?:from|join)\s+([a-zA-Z_][\w\.]*)`)

func TablesFromSQL(sql string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range tableRE.FindAllStringSubmatch(sql, -1) {
		name := strings.Trim(m[1], `"`)
		low := strings.ToLower(name)
		if low == "select" || low == "lateral" || strings.HasPrefix(low, "(") {
			continue
		}
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "—"
	}
	return strings.Join(out, ", ")
}

func RowsFromRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return "—"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "—"
	}
	scan := firstNumber(m, "ScanRows", "RowsExamined", "ReadRows", "SharedReadBlks")
	ret := firstNumber(m, "ReturnRows", "RowsSent", "Rows", "SentRows")
	if scan == "" && ret == "" {
		return "—"
	}
	if scan == "" {
		scan = "—"
	}
	if ret == "" {
		ret = "—"
	}
	return scan + " / " + ret
}

func firstNumber(m map[string]any, keys ...string) string {
	lower := map[string]any{}
	for k, v := range m {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		v, ok := lower[strings.ToLower(k)]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			return fmt.Sprintf("%.0f", n)
		case string:
			if strings.TrimSpace(n) != "" {
				return strings.TrimSpace(n)
			}
		}
	}
	return ""
}

func RenderHTML(doc Document) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(doc.Title))
	b.WriteString("</title><style>")
	b.WriteString(`body{font-family:sans-serif;font-size:14px;color:#222;margin:16px}
table{border-collapse:separate;border-spacing:0;width:100%;margin:8px 0;border-radius:0}
th,td{border:0;border-right:1px solid #d9d9d9;border-bottom:1px solid #d9d9d9;padding:6px;vertical-align:top;border-radius:0}
th:first-child,td:first-child{border-left:1px solid #d9d9d9}
tr:first-child>th,thead th{border-top:1px solid #d9d9d9}
th{background:#fafafa}
.env-radio{position:absolute;opacity:0;width:0;height:0}
.env-tab{display:inline-block;padding:4px 12px;border:1px solid #d9d9d9;border-radius:4px;margin:0 8px 8px 0;cursor:pointer}
.env-radio:checked+label{background:#1677ff;color:#fff;border-color:#1677ff}
.panel{display:none}
.sql-brief{word-break:break-all}
.sql-brief button{margin-left:8px;vertical-align:baseline}
pre{white-space:pre-wrap;word-break:break-all;margin:8px 0}
dialog{max-width:860px;width:90%;border:1px solid #d9d9d9;border-radius:8px}
dialog::backdrop{background:rgba(0,0,0,.35)}`)
	for i := range doc.Envs {
		fmt.Fprintf(&b, "#e%d:checked ~ .panels .p%d{display:block}", i, i)
	}
	b.WriteString("</style></head><body>")
	if doc.Note != "" {
		b.WriteString("<p>" + html.EscapeString(doc.Note) + "</p>")
	}
	b.WriteString("<p>业务线：" + html.EscapeString(emptyDash(doc.Tenant)) + "</p>")
	if !doc.ThisFrom.IsZero() {
		b.WriteString("<p>本周 " + html.EscapeString(spanText(doc.ThisFrom, doc.ThisTo)) + "</p>")
	}
	for i, env := range doc.Envs {
		checked := ""
		if i == 0 {
			checked = " checked"
		}
		fmt.Fprintf(&b, `<input class="env-radio" type="radio" name="env" id="e%d"%s><label class="env-tab" for="e%d">%s</label>`,
			i, checked, i, html.EscapeString(env.Name))
	}
	b.WriteString(`<div class="panels">`)
	for i, env := range doc.Envs {
		fmt.Fprintf(&b, `<section class="panel p%d">`, i)
		if env.Instances != "" {
			b.WriteString("<p>实例：" + html.EscapeString(env.Instances) + "</p>")
		}
		writeCompare(&b, env.Compare)
		b.WriteString("<h3>慢 SQL 排名</h3>")
		if env.Empty || len(env.Ranks) == 0 {
			b.WriteString("<p>这一周没有匹配的慢 SQL。</p>")
		} else {
			writeRanks(&b, i, env.Ranks)
		}
		b.WriteString("</section>")
	}
	b.WriteString("</div>")
	b.WriteString(`<dialog id="sqlDialog"><div style="display:flex;justify-content:space-between;align-items:center"><strong>SQL 详情</strong><button type="button" onclick="document.getElementById('sqlDialog').close()">关闭</button></div><pre id="sqlDialogBody"></pre></dialog>`)
	b.WriteString(`<script>
function openSQL(id){var src=document.getElementById(id);if(!src)return;document.getElementById('sqlDialogBody').textContent=src.textContent;document.getElementById('sqlDialog').showModal()}
function filterDB(sel){var table=sel.parentNode.nextElementSibling;if(!table)return;var v=sel.value;var rows=table.querySelectorAll('tbody tr');for(var i=0;i<rows.length;i++){var db=rows[i].getAttribute('data-db')||'';rows[i].style.display=(!v||db===v)?'':'none'}}
</script></body></html>`)
	return b.String()
}

func writeCompare(b *strings.Builder, rows []CompareRow) {
	b.WriteString("<table><tr><th>指标</th><th>本周数值</th><th>上周数值</th><th>环比变化</th><th>健康阈值/备注</th></tr>")
	for _, row := range rows {
		b.WriteString("<tr><td>" + html.EscapeString(row.Name) + "</td><td>" + html.EscapeString(row.This) + "</td><td>" + html.EscapeString(row.Last) + "</td><td>" + html.EscapeString(row.Delta) + "</td><td>" + html.EscapeString(row.Note) + "</td></tr>")
	}
	b.WriteString("</table>")
}

func writeRanks(b *strings.Builder, envIndex int, rows []RankRow) {
	dbs := map[string]struct{}{}
	var names []string
	for _, row := range rows {
		if row.DB == "" || row.DB == "—" {
			continue
		}
		if _, ok := dbs[row.DB]; ok {
			continue
		}
		dbs[row.DB] = struct{}{}
		names = append(names, row.DB)
	}
	sort.Strings(names)
	b.WriteString(`<p>库 <select onchange="filterDB(this)"><option value="">全部</option>`)
	for _, name := range names {
		b.WriteString(`<option value="` + html.EscapeString(name) + `">` + html.EscapeString(name) + `</option>`)
	}
	b.WriteString(`</select></p>`)
	b.WriteString(`<table class="ranks"><thead><tr><th>排名</th><th>SQL 指纹/摘要</th><th>库</th><th>执行次数</th><th>平均耗时</th><th>P99 耗时</th><th>扫描行数/返回行数</th><th>涉及表</th></tr></thead><tbody>`)
	for i, row := range rows {
		id := fmt.Sprintf("sql-%d-%d", envIndex, i)
		full := strings.Join(strings.Fields(row.SQL), " ")
		b.WriteString(`<tr data-db="` + html.EscapeString(row.DB) + `"><td>` + fmt.Sprint(row.Rank) + `</td><td>`)
		b.WriteString(`<span class="sql-brief">` + html.EscapeString(clip(full, 80)))
		b.WriteString(`<button type="button" onclick="openSQL('` + id + `')">查看</button></span>`)
		b.WriteString(`<pre id="` + id + `" hidden>` + html.EscapeString(full) + "</pre></td>")
		b.WriteString("<td>" + html.EscapeString(row.DB) + "</td><td>" + fmt.Sprint(row.Calls) + "</td><td>" + html.EscapeString(row.Avg) + "</td><td>" + html.EscapeString(row.P99) + "</td><td>" + html.EscapeString(row.Rows) + "</td><td>" + html.EscapeString(row.Tables) + "</td></tr>")
	}
	b.WriteString("</tbody></table>")
}

func spanText(from, to time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return from.In(loc).Format("2006-01-02 15:04") + " 至 " + to.In(loc).Format("2006-01-02 15:04")
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
