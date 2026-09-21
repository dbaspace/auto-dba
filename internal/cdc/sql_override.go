package cdc

import (
	"encoding/json"
	"strings"
)

type SQLOverride struct {
	TargetTable    string `json:"target_table"`
	CreateTableSQL string `json:"create_table_sql"`
	RoutineLoadSQL string `json:"routine_load_sql"`
}

func ParseSQLOverrides(raw string) []SQLOverride {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "[]" || raw == "null" {
		return nil
	}
	var arr []SQLOverride
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		return normalizeSQLOverrides(arr)
	}
	var m map[string]SQLOverride
	if json.Unmarshal([]byte(raw), &m) == nil && len(m) > 0 {
		out := make([]SQLOverride, 0, len(m))
		for k, v := range m {
			if strings.TrimSpace(v.TargetTable) == "" {
				v.TargetTable = k
			}
			out = append(out, v)
		}
		return normalizeSQLOverrides(out)
	}
	var gen map[string]any
	if json.Unmarshal([]byte(raw), &gen) != nil {
		return nil
	}
	out := make([]SQLOverride, 0, len(gen))
	for k, v := range gen {
		ov := SQLOverride{TargetTable: k}
		switch x := v.(type) {
		case map[string]any:
			ov.CreateTableSQL = strings.TrimSpace(anyString(x["create_table_sql"]))
			if ov.CreateTableSQL == "" {
				ov.CreateTableSQL = strings.TrimSpace(anyString(x["ddl"]))
			}
			ov.RoutineLoadSQL = strings.TrimSpace(anyString(x["routine_load_sql"]))
			if ov.RoutineLoadSQL == "" {
				ov.RoutineLoadSQL = strings.TrimSpace(anyString(x["load"]))
			}
		case string:
			ov.CreateTableSQL = strings.TrimSpace(x)
		}
		out = append(out, ov)
	}
	return normalizeSQLOverrides(out)
}

func ApplySQLOverrides(items []TargetSQLItem, overrides []SQLOverride) []TargetSQLItem {
	if len(items) == 0 || len(overrides) == 0 {
		return items
	}
	by := map[string]SQLOverride{}
	for _, ov := range overrides {
		key := strings.ToLower(strings.TrimSpace(ov.TargetTable))
		if key == "" {
			continue
		}
		by[key] = ov
	}
	for i := range items {
		ov, ok := by[strings.ToLower(strings.TrimSpace(items[i].Table))]
		if !ok {
			continue
		}
		if ov.CreateTableSQL != "" {
			items[i].DDL = ov.CreateTableSQL
			items[i].DDLCustomized = true
			items[i].Skipped = false
		}
		if ov.RoutineLoadSQL != "" {
			if topicsOK, expected := routineLoadTopicsMatch(ov.RoutineLoadSQL, expectedTopicsFromLoad(items[i].Load)); topicsOK || expected == "" {
				items[i].Load = ov.RoutineLoadSQL
				items[i].LoadCustomized = true
			} else {
				items[i].Reason = strings.TrimSpace(items[i].Reason + " 自定义 Routine Load 的 kafka_topic 与当前 topic 不一致，已保留自动生成 SQL")
			}
		}
	}
	return items
}

func normalizeSQLOverrides(in []SQLOverride) []SQLOverride {
	out := make([]SQLOverride, 0, len(in))
	for _, ov := range in {
		ov.TargetTable = strings.TrimSpace(ov.TargetTable)
		ov.CreateTableSQL = strings.TrimSpace(ov.CreateTableSQL)
		ov.RoutineLoadSQL = strings.TrimSpace(ov.RoutineLoadSQL)
		if ov.TargetTable == "" || (ov.CreateTableSQL == "" && ov.RoutineLoadSQL == "") {
			continue
		}
		out = append(out, ov)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func anyString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(toString(v))
}

func toString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	return strings.Trim(s, `"`)
}

func expectedTopicsFromLoad(sql string) string {
	return strings.TrimSpace(extractQuotedSQLProp(sql, "kafka_topic"))
}

func routineLoadTopicsMatch(customSQL, expected string) (bool, string) {
	got := strings.TrimSpace(extractQuotedSQLProp(customSQL, "kafka_topic"))
	if got == "" || expected == "" {
		return got == "" || expected == "", expected
	}
	return equalCSVFold(got, expected), expected
}

func extractQuotedSQLProp(sql, key string) string {
	lower := strings.ToLower(sql)
	markers := []string{`"` + strings.ToLower(key) + `"`, `'` + strings.ToLower(key) + `'`}
	idx, keyLen := -1, 0
	for _, m := range markers {
		if i := strings.Index(lower, m); i >= 0 {
			idx = i
			keyLen = len(m)
			break
		}
	}
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(sql[idx+keyLen:])
	eq := strings.Index(rest, "=")
	if eq < 0 {
		return ""
	}
	return extractQuotedSQLValue(strings.TrimSpace(rest[eq+1:]))
}

func extractQuotedSQLValue(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	quote := s[0]
	if quote != '"' && quote != '\'' {
		return ""
	}
	s = s[1:]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				b.WriteByte(s[i])
				i++
				continue
			}
			return strings.TrimSpace(b.String())
		}
		b.WriteByte(s[i])
	}
	return ""
}

func equalCSVFold(a, b string) bool {
	as := splitCSVFold(a)
	bs := splitCSVFold(b)
	if len(as) != len(bs) {
		return false
	}
	seen := map[string]int{}
	for _, x := range as {
		seen[x]++
	}
	for _, x := range bs {
		if seen[x] == 0 {
			return false
		}
		seen[x]--
	}
	return true
}

func splitCSVFold(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
