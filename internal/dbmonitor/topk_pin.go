package dbmonitor

import (
	"fmt"
	"strconv"
	"strings"

	"auto-dba/internal/promclient"
)

// splitOuterTopK 解析 topk(N, INNER)；用于 query_range 前先 instant 钉死序列。
func splitOuterTopK(promql string) (k int, inner string, ok bool) {
	s := strings.TrimSpace(promql)
	if len(s) < 7 {
		return 0, "", false
	}
	if !strings.EqualFold(s[:5], "topk(") {
		return 0, "", false
	}
	rest := s[5:]
	comma := -1
	depth := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				comma = i
				break
			}
		}
		if comma >= 0 {
			break
		}
	}
	if comma < 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:comma]))
	if err != nil || n <= 0 {
		return 0, "", false
	}
	innerPart := strings.TrimSpace(rest[comma+1:])
	depth = 1
	for i := 0; i < len(innerPart); i++ {
		switch innerPart[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner = strings.TrimSpace(innerPart[:i])
				if inner == "" {
					return 0, "", false
				}
				return n, inner, true
			}
		}
	}
	return 0, "", false
}

// pinLabelKey 选择用于钉死 topk 的标签（优先 datname）。
func pinLabelKey(keys []string) string {
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "datname" || k == "db" || k == "database" {
			return k
		}
	}
	if len(keys) > 0 {
		return strings.TrimSpace(keys[0])
	}
	return "datname"
}

func matcherFromSamples(samples []promclient.Sample, label string, limit int) string {
	label = strings.TrimSpace(label)
	if label == "" || limit <= 0 {
		return ""
	}
	vals := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for _, sm := range samples {
		v := strings.TrimSpace(sm.Metric[label])
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		vals = append(vals, regexpEscape(v))
		if len(vals) >= limit {
			break
		}
	}
	if len(vals) == 0 {
		return ""
	}
	return fmt.Sprintf(`%s=~"%s"`, label, strings.Join(vals, "|"))
}

// injectLabelMatcher 把 matcher 注入 PromQL 中每一处 {...} 选择器。
func injectLabelMatcher(promql, matcher string) string {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" {
		return promql
	}
	var b strings.Builder
	b.Grow(len(promql) + 64)
	for i := 0; i < len(promql); {
		if promql[i] != '{' {
			b.WriteByte(promql[i])
			i++
			continue
		}
		j := i + 1
		depth := 1
		for j < len(promql) && depth > 0 {
			switch promql[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			j++
		}
		content := strings.TrimSpace(promql[i+1 : j-1])
		if content == "" {
			b.WriteString("{")
			b.WriteString(matcher)
			b.WriteString("}")
		} else {
			b.WriteString("{")
			b.WriteString(content)
			b.WriteString(",")
			b.WriteString(matcher)
			b.WriteString("}")
		}
		i = j
	}
	return b.String()
}
