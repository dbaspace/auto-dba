package handler

import (
	"encoding/json"
	"strings"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/store"
)

const ArgQueryDataMaskColumns = "query_data_mask_columns"

// applyQueryDataMask 按全局参数 query_data_mask_columns（JSON 字符串数组或逗号分隔列名）对结果列脱敏。
// 未配置则不改动，不影响现有能力。
func applyQueryDataMask(st *store.DB, res *dbprobe.QueryResult) {
	if st == nil || res == nil || len(res.Columns) == 0 || len(res.Rows) == 0 {
		return
	}
	raw, err := st.GetGlobalArg(ArgQueryDataMaskColumns)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	cols := parseMaskColumns(raw)
	if len(cols) == 0 {
		return
	}
	idx := map[int]struct{}{}
	for i, c := range res.Columns {
		if _, ok := cols[strings.ToLower(strings.TrimSpace(c))]; ok {
			idx[i] = struct{}{}
		}
	}
	if len(idx) == 0 {
		return
	}
	for r := range res.Rows {
		for i := range idx {
			if i < len(res.Rows[r]) && res.Rows[r][i] != nil {
				res.Rows[r][i] = "***"
			}
		}
	}
}

func parseMaskColumns(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil {
			for _, s := range arr {
				s = strings.ToLower(strings.TrimSpace(s))
				if s != "" {
					out[s] = struct{}{}
				}
			}
			return out
		}
	}
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '，'
	}) {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}
