package copilot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// judgeItem 一条脚手架的结果。Raw 是成功时的 JSON，失败时为空。
type judgeItem struct {
	Action string
	Detail string
	Status string
	Output string
	Raw    string
}

type judgeSeries struct {
	Labels map[string]string `json:"labels"`
	Min    float64           `json:"min"`
	Avg    float64           `json:"avg"`
	Max    float64           `json:"max"`
	Trend  string            `json:"trend"`
}

// judgeFindings 用已拿到的全部指标做分析和预判，不重新查库。
func judgeFindings(items []judgeItem) string {
	var facts []string
	var bases []string
	var missing []string
	spike := false
	hasTrend := false
	diskOutlook := ""
	for _, it := range items {
		if it.Action != "prometheus" && it.Action != "postgres" && it.Action != "clickhouse" {
			continue
		}
		title := it.Action
		if it.Detail != "" {
			title += " " + trim(it.Detail, 80)
		}
		if it.Status != "ok" {
			reason := strings.TrimSpace(it.Output)
			if reason == "" {
				reason = "未返回数据"
			}
			missing = append(missing, title+"："+trim(reason, 160))
			continue
		}
		if table, topName, topShare, dominates, ok := formatSizeTable(it.Raw); ok {
			facts = append(facts, fmt.Sprintf("容量最高的是 %s，占 %s", topName, formatPct(topShare)))
			bases = append(bases, table)
			if dominates {
				diskOutlook = "大库占满"
			} else if diskOutlook == "" {
				diskOutlook = "各库都不大"
			}
			continue
		}
		series := parseJudgeSeries(it.Raw)
		if len(series) > 0 {
			for _, s := range series {
				line := fmt.Sprintf("%s %s min=%.1f avg=%.1f max=%.1f", title, labelText(s.Labels), s.Min, s.Avg, s.Max)
				if s.Trend != "" {
					line += " 后半段" + s.Trend
					hasTrend = true
				}
				facts = append(facts, line)
				bases = append(bases, line)
				if s.Trend == "升高" && s.Avg > 0 && s.Max > s.Avg*1.5 {
					spike = true
				}
			}
			continue
		}
		if top := topSQLRow(it.Raw); top != "" {
			line := title + " 占用最高 " + top
			facts = append(facts, line)
			bases = append(bases, line)
			continue
		}
		if text := strings.TrimSpace(it.Output); text != "" {
			facts = append(facts, title+" "+trim(text, 180))
			bases = append(bases, title)
		}
	}
	phenomenon := "没有可用指标"
	if len(facts) > 0 {
		phenomenon = strings.Join(facts, "；")
	}
	basis := "无"
	if len(bases) > 0 {
		basis = strings.Join(bases, "；")
	}
	outlook := "没有序列，不做趋势预判"
	if diskOutlook != "" {
		outlook = diskOutlook
	} else if spike {
		outlook = "窗口内出现冲高，若持续需关注"
	} else if hasTrend {
		outlook = "当前窗口未见明显恶化"
	}
	gap := "无"
	if len(missing) > 0 {
		gap = strings.Join(missing, "；")
	}
	basisHead := "数据依据："
	if strings.Contains(basis, "|") {
		basisHead = "数据依据：\n"
	}
	return "现象：" + phenomenon + "\n" + basisHead + basis + "\n预判：" + outlook + "\n缺失的数据：" + gap
}

// judgeStepAnswer 有成功的 judge 时，回答直接用这段结论。
func judgeStepAnswer(steps []TaskStep) (string, bool) {
	for i := len(steps) - 1; i >= 0; i-- {
		st := steps[i]
		if st.Action == "judge" && st.Status == "ok" && strings.TrimSpace(st.Output) != "" {
			return st.Output, true
		}
	}
	return "", false
}

// formatSizeTable 把库字节数换成 GB 表格。靠前的库逐行，其余合并为「其他」。
func formatSizeTable(raw string) (table, topName string, topShare float64, dominates, ok bool) {
	var body struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil || len(body.Rows) == 0 {
		return "", "", 0, false, false
	}
	nameCol, bytesCol := -1, -1
	for i, col := range body.Columns {
		switch strings.ToLower(col) {
		case "datname", "name":
			if nameCol < 0 {
				nameCol = i
			}
		case "bytes":
			bytesCol = i
		}
	}
	if nameCol < 0 || bytesCol < 0 {
		return "", "", 0, false, false
	}
	type sizeRow struct {
		name  string
		bytes float64
	}
	var rows []sizeRow
	var total float64
	for _, row := range body.Rows {
		if nameCol >= len(row) || bytesCol >= len(row) {
			continue
		}
		n, okb := promFloat(row[bytesCol])
		if !okb {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(row[nameCol]))
		if name == "" {
			name = "(空)"
		}
		rows = append(rows, sizeRow{name: name, bytes: n})
		total += n
	}
	if len(rows) == 0 {
		return "", "", 0, false, false
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].bytes > rows[j].bytes })
	const topN = 5
	show := rows
	var rest float64
	if len(rows) > topN {
		show = rows[:topN]
		for _, r := range rows[topN:] {
			rest += r.bytes
		}
	}
	shareOf := func(bytes float64) float64 {
		if total <= 0 {
			return 0
		}
		return bytes / total * 100
	}
	var b strings.Builder
	b.WriteString("| 库名 | 容量 | 占比 |\n| --- | --- | --- |\n")
	for _, r := range show {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.name, formatGB(r.bytes), formatPct(shareOf(r.bytes)))
	}
	if len(rows) > topN {
		fmt.Fprintf(&b, "| 其他 | %s | %s |\n", formatGB(rest), formatPct(shareOf(rest)))
	}
	topShare = shareOf(rows[0].bytes)
	dominates = topShare >= 50 && (len(rows) == 1 || rows[0].bytes > rows[1].bytes)
	return strings.TrimRight(b.String(), "\n"), rows[0].name, topShare, dominates, true
}

func formatGB(bytes float64) string {
	gb := bytes / (1024 * 1024 * 1024)
	return strconv.FormatFloat(gb, 'f', 2, 64) + " GB"
}

func formatPct(p float64) string {
	return strconv.FormatFloat(p, 'f', 1, 64) + "%"
}

func parseJudgeSeries(raw string) []judgeSeries {
	var body struct {
		Series []judgeSeries `json:"series"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil {
		return nil
	}
	return body.Series
}

func topSQLRow(raw string) string {
	var body struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil || len(body.Rows) == 0 || len(body.Columns) == 0 {
		return ""
	}
	best := 0
	bestScore := -1.0
	scoreCol := -1
	for i, col := range body.Columns {
		switch strings.ToLower(col) {
		case "sessions", "elapsed", "query_duration_ms", "read_rows", "n":
			scoreCol = i
		}
	}
	if scoreCol >= 0 {
		for i, row := range body.Rows {
			if scoreCol >= len(row) {
				continue
			}
			n, ok := promFloat(row[scoreCol])
			if ok && n > bestScore {
				bestScore = n
				best = i
			}
		}
	}
	row := body.Rows[best]
	var parts []string
	for i, col := range body.Columns {
		if i >= len(row) {
			continue
		}
		low := strings.ToLower(col)
		switch low {
		case "usename", "user", "datname", "name", "query", "state", "sessions", "elapsed", "query_duration_ms":
			parts = append(parts, col+"="+trim(fmt.Sprint(row[i]), 80))
		}
	}
	return strings.Join(parts, " ")
}
