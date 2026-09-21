package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"auto-dba/internal/store"
)

const SchemaCompareMaxTables = 200

// SchemaCompareSpec 无状态双库结构对比请求（表名为裸表名；PG 用 LeftSchema/RightSchema）。
type SchemaCompareSpec struct {
	Engine      string
	LeftSchema  string
	RightSchema string
	Tables      []string // 空则取两侧交集（上限 SchemaCompareMaxTables）
}

// SchemaDiffItem 单条结构差异。
type SchemaDiffItem struct {
	Kind    string `json:"kind"` // column | constraint | foreign_key | index | rls | policy | comment | trigger | function
	Name    string `json:"name,omitempty"`
	Left    string `json:"left,omitempty"`
	Right   string `json:"right,omitempty"`
	Message string `json:"message"`
}

// TableSchemaDiff 单表对比结果。
type TableSchemaDiff struct {
	Table   string           `json:"table"`
	Status  string           `json:"status"` // match | differ | left_only | right_only
	Message string           `json:"message,omitempty"`
	Diffs   []SchemaDiffItem `json:"diffs,omitempty"`
	FixSQL  string           `json:"fix_sql,omitempty"` // 该表修复语句（源→目标）
}

// CompareSchemas 对比两侧已打开连接中的表结构。tables 为空时取同名表交集。
func CompareSchemas(ctx context.Context, left, right *sql.DB, spec SchemaCompareSpec) ([]TableSchemaDiff, error) {
	engine := store.NormalizeDBType(spec.Engine)
	eng, err := selectSchemaEngine(engine)
	if err != nil {
		return nil, err
	}
	leftSchema := firstNonEmpty(spec.LeftSchema, "public")
	rightSchema := firstNonEmpty(spec.RightSchema, "public")
	if engine != "postgres" {
		leftSchema, rightSchema = "", ""
	}

	tables := normalizeTableList(spec.Tables)
	if len(tables) == 0 {
		leftTables, err := listTableNames(ctx, left, engine, leftSchema)
		if err != nil {
			return nil, fmt.Errorf("列出左侧表失败: %w", err)
		}
		rightTables, err := listTableNames(ctx, right, engine, rightSchema)
		if err != nil {
			return nil, fmt.Errorf("列出右侧表失败: %w", err)
		}
		tables = intersectSorted(leftTables, rightTables)
		if len(tables) > SchemaCompareMaxTables {
			tables = tables[:SchemaCompareMaxTables]
		}
		onlyLeft := diffSorted(leftTables, rightTables)
		onlyRight := diffSorted(rightTables, leftTables)
		var out []TableSchemaDiff
		for _, t := range tables {
			d, err := eng.FetchAndDiff(ctx, left, right, leftSchema, rightSchema, t)
			if err != nil {
				out = append(out, TableSchemaDiff{Table: t, Status: "differ", Message: err.Error()})
				continue
			}
			out = append(out, d)
		}
		remain := SchemaCompareMaxTables - len(out)
		for i := 0; i < len(onlyLeft) && remain > 0; i++ {
			out = append(out, TableSchemaDiff{Table: onlyLeft[i], Status: "left_only", Message: "仅源端存在"})
			remain--
		}
		for i := 0; i < len(onlyRight) && remain > 0; i++ {
			out = append(out, TableSchemaDiff{Table: onlyRight[i], Status: "right_only", Message: "仅目标端存在"})
			remain--
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
		out = append(out, eng.DiffRoutines(ctx, left, right, leftSchema, rightSchema)...)
		return out, nil
	}

	if len(tables) > SchemaCompareMaxTables {
		tables = tables[:SchemaCompareMaxTables]
	}
	leftSet, err := tableNameSet(ctx, left, engine, leftSchema)
	if err != nil {
		return nil, fmt.Errorf("列出左侧表失败: %w", err)
	}
	rightSet, err := tableNameSet(ctx, right, engine, rightSchema)
	if err != nil {
		return nil, fmt.Errorf("列出右侧表失败: %w", err)
	}
	var out []TableSchemaDiff
	for _, t := range tables {
		_, lOK := leftSet[strings.ToLower(t)]
		_, rOK := rightSet[strings.ToLower(t)]
		switch {
		case !lOK && !rOK:
			out = append(out, TableSchemaDiff{Table: t, Status: "differ", Message: "两侧均不存在"})
		case !lOK:
			out = append(out, TableSchemaDiff{Table: t, Status: "right_only", Message: "仅目标端存在"})
		case !rOK:
			out = append(out, TableSchemaDiff{Table: t, Status: "left_only", Message: "仅源端存在"})
		default:
			d, err := eng.FetchAndDiff(ctx, left, right, leftSchema, rightSchema, t)
			if err != nil {
				out = append(out, TableSchemaDiff{Table: t, Status: "differ", Message: err.Error()})
				continue
			}
			out = append(out, d)
		}
	}
	out = append(out, eng.DiffRoutines(ctx, left, right, leftSchema, rightSchema)...)
	return out, nil
}

func diffPGTableSchema(left, right *pgTableSchemaInfo) []SchemaDiffItem {
	var diffs []SchemaDiffItem
	lCols := map[string]pgColInfo{}
	rCols := map[string]pgColInfo{}
	for _, c := range left.Columns {
		lCols[strings.ToLower(c.Name)] = c
	}
	for _, c := range right.Columns {
		rCols[strings.ToLower(c.Name)] = c
	}
	allCol := map[string]struct{}{}
	for k := range lCols {
		allCol[k] = struct{}{}
	}
	for k := range rCols {
		allCol[k] = struct{}{}
	}
	names := sortedKeys(allCol)
	for _, k := range names {
		lc, lok := lCols[k]
		rc, rok := rCols[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: rc.Name, Right: fmt.Sprintf("%s null=%v", rc.DataType, !rc.NotNull), Message: "仅右侧有该列"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: lc.Name, Left: fmt.Sprintf("%s null=%v", lc.DataType, !lc.NotNull), Message: "仅左侧有该列"})
		default:
			if !strings.EqualFold(normalizePGTypeName(lc.DataType), normalizePGTypeName(rc.DataType)) || lc.NotNull != rc.NotNull {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "column", Name: lc.Name,
					Left: fmt.Sprintf("%s null=%v", lc.DataType, !lc.NotNull),
					Right: fmt.Sprintf("%s null=%v", rc.DataType, !rc.NotNull),
					Message: "列定义不一致",
				})
			}
		}
	}
	diffs = append(diffs, diffConstraintSide(left.Constraints, right.Constraints, "constraint")...)
	diffs = append(diffs, diffConstraintSide(left.ForeignKeys, right.ForeignKeys, "foreign_key")...)
	diffs = append(diffs, diffIndexSide(left.Indexes, right.Indexes)...)
	return diffs
}

func diffConstraintSide(left, right []pgConstraintInfo, kind string) []SchemaDiffItem {
	lMap := map[string]pgConstraintInfo{}
	rMap := map[string]pgConstraintInfo{}
	for _, c := range left {
		lMap[strings.ToLower(c.Name)] = c
	}
	for _, c := range right {
		rMap[strings.ToLower(c.Name)] = c
	}
	all := map[string]struct{}{}
	for k := range lMap {
		all[k] = struct{}{}
	}
	for k := range rMap {
		all[k] = struct{}{}
	}
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lc, lok := lMap[k]
		rc, rok := rMap[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: kind, Name: rc.Name, Right: rc.Def, Message: "仅右侧有该约束"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: kind, Name: lc.Name, Left: lc.Def, Message: "仅左侧有该约束"})
		default:
			if !strings.EqualFold(lc.Type, rc.Type) || normalizePGConstraintDef(lc.Def) != normalizePGConstraintDef(rc.Def) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: kind, Name: lc.Name, Left: lc.Def, Right: rc.Def, Message: "约束定义不一致",
				})
			}
		}
	}
	return diffs
}

func diffIndexSide(left, right []pgIndexInfo) []SchemaDiffItem {
	lMap := map[string]pgIndexInfo{}
	rMap := map[string]pgIndexInfo{}
	for _, c := range left {
		lMap[strings.ToLower(c.Name)] = c
	}
	for _, c := range right {
		rMap[strings.ToLower(c.Name)] = c
	}
	all := map[string]struct{}{}
	for k := range lMap {
		all[k] = struct{}{}
	}
	for k := range rMap {
		all[k] = struct{}{}
	}
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lc, lok := lMap[k]
		rc, rok := rMap[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "index", Name: rc.Name, Right: rc.Def, Message: "仅右侧有该索引"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "index", Name: lc.Name, Left: lc.Def, Message: "仅左侧有该索引"})
		default:
			if strings.Join(strings.Fields(strings.ToLower(lc.Def)), " ") != strings.Join(strings.Fields(strings.ToLower(rc.Def)), " ") {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "index", Name: lc.Name, Left: lc.Def, Right: rc.Def, Message: "索引定义不一致",
				})
			}
		}
	}
	return diffs
}

func diffMySQLColumns(ctx context.Context, left, right *sql.DB, table string) ([]SchemaDiffItem, error) {
	lCols, err := listMySQLColDefs(ctx, left, table)
	if err != nil {
		return nil, fmt.Errorf("读左侧列: %w", err)
	}
	rCols, err := listMySQLColDefs(ctx, right, table)
	if err != nil {
		return nil, fmt.Errorf("读右侧列: %w", err)
	}
	lMap := map[string]colPair{}
	rMap := map[string]colPair{}
	for _, c := range lCols {
		lMap[strings.ToLower(c.Name)] = c
	}
	for _, c := range rCols {
		rMap[strings.ToLower(c.Name)] = c
	}
	all := map[string]struct{}{}
	for k := range lMap {
		all[k] = struct{}{}
	}
	for k := range rMap {
		all[k] = struct{}{}
	}
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lc, lok := lMap[k]
		rc, rok := rMap[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: rc.Name, Right: rc.Typ, Message: "仅右侧有该列"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: lc.Name, Left: lc.Typ, Message: "仅左侧有该列"})
		default:
			if !strings.EqualFold(lc.Typ, rc.Typ) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "column", Name: lc.Name, Left: lc.Typ, Right: rc.Typ, Message: "列类型不一致",
				})
			}
		}
	}
	return diffs, nil
}

type colPair struct{ Name, Typ string }

func listMySQLColDefs(ctx context.Context, db *sql.DB, table string) ([]colPair, error) {
	rows, err := db.QueryContext(ctx, `
SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []colPair
	for rows.Next() {
		var c colPair
		if err := rows.Scan(&c.Name, &c.Typ); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func listTableNames(ctx context.Context, db *sql.DB, engine, schema string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if store.NormalizeDBType(engine) == "postgres" {
		if schema == "" {
			schema = "public"
		}
		rows, err = db.QueryContext(ctx, `
SELECT table_name FROM information_schema.tables
WHERE table_schema=$1 AND table_type='BASE TABLE'
ORDER BY table_name`, schema)
	} else {
		rows, err = db.QueryContext(ctx, `
SELECT table_name FROM information_schema.tables
WHERE table_schema=DATABASE() AND table_type='BASE TABLE'
ORDER BY table_name`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func tableNameSet(ctx context.Context, db *sql.DB, engine, schema string) (map[string]struct{}, error) {
	names, err := listTableNames(ctx, db, engine, schema)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[strings.ToLower(n)] = struct{}{}
	}
	return out, nil
}

func normalizeTableList(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// 允许 schema.table，取表名
		if i := strings.LastIndex(t, "."); i >= 0 {
			t = t[i+1:]
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

func intersectSorted(a, b []string) []string {
	set := map[string]string{}
	for _, x := range a {
		set[strings.ToLower(x)] = x
	}
	var out []string
	for _, y := range b {
		if x, ok := set[strings.ToLower(y)]; ok {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func diffSorted(a, b []string) []string {
	set := map[string]struct{}{}
	for _, x := range b {
		set[strings.ToLower(x)] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := set[strings.ToLower(x)]; !ok {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SameEngineFamily 判断两侧 db_type 是否可互比。
func SameEngineFamily(a, b string) (family string, ok bool) {
	na, nb := store.NormalizeDBType(a), store.NormalizeDBType(b)
	fa, fb := engineFamily(na), engineFamily(nb)
	if fa == "" || fa != fb {
		return "", false
	}
	return fa, true
}

func engineFamily(t string) string {
	switch store.NormalizeDBType(t) {
	case "postgres":
		return "postgres"
	case "mysql", "tidb", "oceanbase":
		return "mysql"
	default:
		return ""
	}
}
