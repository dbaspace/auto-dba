package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/store"
)

const ArgQueryDataMaskColumns = "query_data_mask_columns"

const queryMaskArgMessage = "请到配置中心 → 基础设置 → 敏感数据脱敏 修改"

type queryMaskConfig struct {
	Enabled bool     `json:"enabled"`
	Columns []string `json:"columns"`
}

// applyQueryDataMask 按 query_data_mask_columns 对结果列脱敏。开关关闭或未配置时不改动。
func applyQueryDataMask(st *store.DB, res *dbprobe.QueryResult) {
	if st == nil || res == nil || len(res.Columns) == 0 || len(res.Rows) == 0 {
		return
	}
	raw, err := st.GetGlobalArg(ArgQueryDataMaskColumns)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	maskQueryResult(raw, res)
}

func maskQueryResult(raw string, res *dbprobe.QueryResult) {
	cfg := loadQueryMask(raw)
	if !cfg.Enabled || len(cfg.Columns) == 0 || res == nil {
		return
	}
	want := map[string]struct{}{}
	for _, c := range cfg.Columns {
		want[strings.ToLower(c)] = struct{}{}
	}
	idx := maskColumnIndexes(res.ValidSQL, res.Columns, want)
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

type maskSelectItem struct {
	kind string
	name string
}

func maskColumnIndexes(sql string, resultCols []string, want map[string]struct{}) map[int]struct{} {
	items, ok := parseOuterSelectItems(sql)
	if !ok {
		return indexesByResultName(resultCols, want)
	}
	stars, nonStar := 0, 0
	for _, it := range items {
		if it.kind == "star" {
			stars++
		} else {
			nonStar++
		}
	}
	if stars == 0 {
		if len(items) != len(resultCols) {
			return indexesByResultName(resultCols, want)
		}
		idx := map[int]struct{}{}
		for i, it := range items {
			if it.kind == "col" {
				if _, ok := want[it.name]; ok {
					idx[i] = struct{}{}
				}
			}
		}
		return idx
	}
	if stars == 1 && nonStar <= len(resultCols) {
		starWidth := len(resultCols) - nonStar
		idx := map[int]struct{}{}
		pos := 0
		for _, it := range items {
			if it.kind == "star" {
				for k := 0; k < starWidth && pos < len(resultCols); k++ {
					if _, ok := want[strings.ToLower(strings.TrimSpace(resultCols[pos]))]; ok {
						idx[pos] = struct{}{}
					}
					pos++
				}
				continue
			}
			if pos >= len(resultCols) {
				break
			}
			if it.kind == "col" {
				if _, ok := want[it.name]; ok {
					idx[pos] = struct{}{}
				}
			}
			pos++
		}
		return idx
	}
	return indexesByResultName(resultCols, want)
}

func indexesByResultName(resultCols []string, want map[string]struct{}) map[int]struct{} {
	idx := map[int]struct{}{}
	for i, c := range resultCols {
		if _, ok := want[strings.ToLower(strings.TrimSpace(c))]; ok {
			idx[i] = struct{}{}
		}
	}
	return idx
}

func parseOuterSelectItems(sql string) ([]maskSelectItem, bool) {
	list, ok := outerSelectList(sql)
	if !ok {
		return nil, false
	}
	parts := splitSelectList(list)
	if len(parts) == 0 {
		return nil, false
	}
	items := make([]maskSelectItem, 0, len(parts))
	for _, part := range parts {
		items = append(items, classifySelectItem(part))
	}
	return items, true
}

func outerSelectList(sql string) (string, bool) {
	s := sql
	n := len(s)
	i := 0
	depth := 0
	for i < n {
		i = skipSQLGap(s, i)
		if i >= n {
			break
		}
		if s[i] == '\'' || s[i] == '"' || s[i] == '`' {
			i = skipSQLQuote(s, i)
			continue
		}
		if s[i] == '(' {
			depth++
			i++
			continue
		}
		if s[i] == ')' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 && matchSQLWord(s, i, "select") {
			i += len("select")
			i = skipSQLGap(s, i)
			if matchSQLWord(s, i, "distinct") {
				i += len("distinct")
			} else if matchSQLWord(s, i, "all") {
				i += len("all")
			}
			start := i
			for i < n {
				i = skipSQLGap(s, i)
				if i >= n {
					break
				}
				if s[i] == '\'' || s[i] == '"' || s[i] == '`' {
					i = skipSQLQuote(s, i)
					continue
				}
				if s[i] == '(' {
					depth++
					i++
					continue
				}
				if s[i] == ')' {
					if depth > 0 {
						depth--
					}
					i++
					continue
				}
				if depth == 0 && matchSQLWord(s, i, "from") {
					return strings.TrimSpace(s[start:i]), true
				}
				i++
			}
			rest := strings.TrimSpace(s[start:])
			if cut := topLevelLimit(rest); cut >= 0 {
				rest = strings.TrimSpace(rest[:cut])
			}
			return rest, rest != ""
		}
		i++
	}
	return "", false
}

func splitSelectList(list string) []string {
	var parts []string
	depth := 0
	start := 0
	i := 0
	for i < len(list) {
		i = skipSQLGap(list, i)
		if i >= len(list) {
			break
		}
		if list[i] == '\'' || list[i] == '"' || list[i] == '`' {
			i = skipSQLQuote(list, i)
			continue
		}
		if list[i] == '(' {
			depth++
			i++
			continue
		}
		if list[i] == ')' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if list[i] == ',' && depth == 0 {
			parts = append(parts, strings.TrimSpace(list[start:i]))
			i++
			start = i
			continue
		}
		i++
	}
	if tail := strings.TrimSpace(list[start:]); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func classifySelectItem(item string) maskSelectItem {
	expr := stripSelectAlias(item)
	if isSelectStar(expr) {
		return maskSelectItem{kind: "star"}
	}
	if name, ok := columnRefName(expr); ok {
		return maskSelectItem{kind: "col", name: strings.ToLower(name)}
	}
	return maskSelectItem{kind: "expr"}
}

func stripSelectAlias(item string) string {
	item = strings.TrimSpace(item)
	if expr, ok := cutAliasAS(item); ok {
		return strings.TrimSpace(expr)
	}
	expr, alias, ok := splitTrailingAlias(item)
	if ok && alias != "" {
		if _, col := columnRefName(expr); col {
			return strings.TrimSpace(expr)
		}
	}
	return item
}

func cutAliasAS(item string) (string, bool) {
	depth := 0
	last := -1
	i := 0
	for i < len(item) {
		i = skipSQLGap(item, i)
		if i >= len(item) {
			break
		}
		if item[i] == '\'' || item[i] == '"' || item[i] == '`' {
			i = skipSQLQuote(item, i)
			continue
		}
		if item[i] == '(' {
			depth++
			i++
			continue
		}
		if item[i] == ')' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 && matchSQLWord(item, i, "as") {
			j := skipSQLGap(item, i+2)
			id, end, ok := readSQLIdent(item, j)
			if ok && strings.TrimSpace(item[end:]) == "" && id != "" {
				last = i
			}
			i = end
			continue
		}
		i++
	}
	if last < 0 {
		return "", false
	}
	return item[:last], true
}

func splitTrailingAlias(item string) (string, string, bool) {
	i := len(item)
	for i > 0 && (item[i-1] == ' ' || item[i-1] == '\t' || item[i-1] == '\n' || item[i-1] == '\r') {
		i--
	}
	end := i
	for i > 0 && isIdentByte(item[i-1]) {
		i--
	}
	if i == end || i == 0 {
		return "", "", false
	}
	if item[i-1] == '.' {
		return "", "", false
	}
	alias := item[i:end]
	expr := strings.TrimSpace(item[:i])
	if expr == "" || !isPlainIdent(alias) {
		return "", "", false
	}
	return expr, alias, true
}

func isSelectStar(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "*" {
		return true
	}
	parts := strings.Split(expr, ".")
	if len(parts) < 2 || strings.TrimSpace(parts[len(parts)-1]) != "*" {
		return false
	}
	for _, p := range parts[:len(parts)-1] {
		if _, ok := unquoteIdent(strings.TrimSpace(p)); !ok {
			return false
		}
	}
	return true
}

func columnRefName(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" || strings.ContainsAny(expr, " \t\n\r()+-*/|&") {
		return "", false
	}
	parts := strings.Split(expr, ".")
	var name string
	for _, p := range parts {
		id, ok := unquoteIdent(strings.TrimSpace(p))
		if !ok || id == "*" {
			return "", false
		}
		name = id
	}
	return name, name != ""
}

func unquoteIdent(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	if s[0] == '"' || s[0] == '`' {
		if len(s) < 2 || s[len(s)-1] != s[0] {
			return "", false
		}
		return strings.ReplaceAll(s[1:len(s)-1], string(s[0])+string(s[0]), string(s[0])), true
	}
	if !isPlainIdent(s) {
		return "", false
	}
	return s, true
}

func isPlainIdent(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

func readSQLIdent(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	if s[i] == '"' || s[i] == '`' {
		j := skipSQLQuote(s, i)
		if j <= i+1 {
			return "", i, false
		}
		return s[i:j], j, true
	}
	if !isIdentStart(s[i]) {
		return "", i, false
	}
	j := i + 1
	for j < len(s) && isIdentByte(s[j]) {
		j++
	}
	return s[i:j], j, true
}

func matchSQLWord(s string, i int, word string) bool {
	if i+len(word) > len(s) || !strings.EqualFold(s[i:i+len(word)], word) {
		return false
	}
	if i > 0 && isIdentByte(s[i-1]) {
		return false
	}
	if i+len(word) < len(s) && isIdentByte(s[i+len(word)]) {
		return false
	}
	return true
}

func skipSQLGap(s string, i int) int {
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			i++
			continue
		}
		if i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
			i += 2
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		break
	}
	return i
}

func skipSQLQuote(s string, i int) int {
	if i >= len(s) {
		return i
	}
	q := s[i]
	i++
	for i < len(s) {
		if s[i] == q {
			if i+1 < len(s) && s[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

func topLevelLimit(s string) int {
	depth := 0
	for i := 0; i < len(s); {
		i = skipSQLGap(s, i)
		if i >= len(s) {
			break
		}
		if s[i] == '\'' || s[i] == '"' || s[i] == '`' {
			i = skipSQLQuote(s, i)
			continue
		}
		if s[i] == '(' {
			depth++
			i++
			continue
		}
		if s[i] == ')' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 && matchSQLWord(s, i, "limit") {
			return i
		}
		i++
	}
	return -1
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isIdentByte(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9') || b == '$'
}

func loadQueryMask(raw string) queryMaskConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return queryMaskConfig{}
	}
	if strings.HasPrefix(raw, "{") {
		var cfg queryMaskConfig
		if json.Unmarshal([]byte(raw), &cfg) == nil {
			cfg.Columns = normalizeMaskColumns(cfg.Columns)
			return cfg
		}
	}
	cols := normalizeMaskColumns([]string{raw})
	return queryMaskConfig{Enabled: len(cols) > 0, Columns: cols}
}

func normalizeMaskColumns(parts []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		for _, p := range splitMaskColumn(part) {
			key := strings.ToLower(p)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

func splitMaskColumn(raw string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '，' || r == '；'
	}) {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *GlobalArgHandler) GetQueryMask(c *gin.Context) {
	raw := ""
	if h != nil && h.Store != nil {
		v, err := h.Store.GetGlobalArg(ArgQueryDataMaskColumns)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		raw = v
	}
	cfg := loadQueryMask(raw)
	if cfg.Columns == nil {
		cfg.Columns = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}

func (h *GlobalArgHandler) PutQueryMask(c *gin.Context) {
	var req queryMaskConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if h == nil || h.Store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "存储不可用"})
		return
	}
	cfg := queryMaskConfig{Enabled: req.Enabled, Columns: normalizeMaskColumns(req.Columns)}
	if cfg.Columns == nil {
		cfg.Columns = []string{}
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.SetGlobalArg(ArgQueryDataMaskColumns, string(b), "查询结果脱敏列名与开关，在配置中心「敏感数据脱敏」维护"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg})
}
