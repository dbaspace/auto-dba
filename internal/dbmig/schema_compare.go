package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type pgColInfo struct {
	Name     string
	DataType string
	NotNull  bool
	Default  string // pg_get_expr；无默认值为空
	Identity string // '' | a (ALWAYS) | d (BY DEFAULT)
	Comment  string
}

type pgConstraintInfo struct {
	Name string
	Type string // p / u / c / f
	Def  string
}

type pgIndexInfo struct {
	Name string
	Def  string
}

type pgPolicyInfo struct {
	Name       string
	Permissive bool
	Cmd        string // SELECT/INSERT/UPDATE/DELETE/ALL
	Roles      string // PUBLIC 或角色列表
	Qual       string
	WithCheck  string
}

type pgTriggerInfo struct {
	Name    string
	Def     string
	Enabled string // O/D/R/A
}

type pgFunctionInfo struct {
	Name       string
	Args       string // identity arguments
	Definition string
}

type pgTableSchemaInfo struct {
	Columns          []pgColInfo
	Constraints      []pgConstraintInfo // PK / UNIQUE / CHECK
	ForeignKeys      []pgConstraintInfo
	Indexes          []pgIndexInfo
	TableComment     string
	RowSecurity      bool
	ForceRowSecurity bool
	Policies         []pgPolicyInfo
	Triggers         []pgTriggerInfo
}

func fetchPGTableSchemaInfo(ctx context.Context, db *sql.DB, schema, table string) (*pgTableSchemaInfo, error) {
	if schema == "" {
		schema = "public"
	}
	out := &pgTableSchemaInfo{}
	cols, err := fetchPGColumnsInfo(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("表 %s.%s 无可用列", schema, table)
	}
	out.Columns = cols

	cons, err := fetchPGConstraintsInfo(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	for _, c := range cons {
		if c.Type == "f" {
			out.ForeignKeys = append(out.ForeignKeys, c)
		} else {
			out.Constraints = append(out.Constraints, c)
		}
	}

	idxs, err := fetchPGIndexesInfo(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	out.Indexes = idxs

	if err := fetchPGTableExtras(ctx, db, schema, table, out); err != nil {
		return nil, err
	}
	return out, nil
}

func fetchPGColumnsInfo(ctx context.Context, db *sql.DB, schema, table string) ([]pgColInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
	a.attname,
	pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
	a.attnotnull,
	COALESCE(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), '') AS col_default,
	a.attidentity::text,
	COALESCE(pg_catalog.col_description(a.attrelid, a.attnum), '') AS col_comment
FROM pg_attribute a
JOIN pg_class cl ON a.attrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE n.nspname = $1
	AND cl.relname = $2
	AND a.attnum > 0
	AND NOT a.attisdropped
ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgColInfo
	for rows.Next() {
		var c pgColInfo
		if err := rows.Scan(&c.Name, &c.DataType, &c.NotNull, &c.Default, &c.Identity, &c.Comment); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchPGTableExtras(ctx context.Context, db *sql.DB, schema, table string, out *pgTableSchemaInfo) error {
	err := db.QueryRowContext(ctx, `
SELECT c.relrowsecurity, c.relforcerowsecurity, COALESCE(pg_catalog.obj_description(c.oid), '')
FROM pg_class c
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r','p')`, schema, table).
		Scan(&out.RowSecurity, &out.ForceRowSecurity, &out.TableComment)
	if err != nil {
		return fmt.Errorf("读表 RLS/注释: %w", err)
	}
	pols, err := fetchPGPolicies(ctx, db, schema, table)
	if err != nil {
		return err
	}
	out.Policies = pols
	trigs, err := fetchPGTriggers(ctx, db, schema, table)
	if err != nil {
		return err
	}
	out.Triggers = trigs
	return nil
}

func fetchPGPolicies(ctx context.Context, db *sql.DB, schema, table string) ([]pgPolicyInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
	p.polname,
	p.polpermissive,
	CASE p.polcmd
		WHEN 'r' THEN 'SELECT'
		WHEN 'a' THEN 'INSERT'
		WHEN 'w' THEN 'UPDATE'
		WHEN 'd' THEN 'DELETE'
		ELSE 'ALL'
	END AS cmd,
	CASE
		WHEN COALESCE(cardinality(p.polroles), 0) = 0 THEN 'PUBLIC'
		ELSE array_to_string(ARRAY(
			SELECT quote_ident(r.rolname) FROM pg_roles r WHERE r.oid = ANY (p.polroles) ORDER BY r.rolname
		), ', ')
	END AS roles,
	COALESCE(pg_catalog.pg_get_expr(p.polqual, p.polrelid), ''),
	COALESCE(pg_catalog.pg_get_expr(p.polwithcheck, p.polrelid), '')
FROM pg_policy p
JOIN pg_class c ON p.polrelid = c.oid
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2
ORDER BY p.polname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgPolicyInfo
	for rows.Next() {
		var p pgPolicyInfo
		if err := rows.Scan(&p.Name, &p.Permissive, &p.Cmd, &p.Roles, &p.Qual, &p.WithCheck); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func fetchPGTriggers(ctx context.Context, db *sql.DB, schema, table string) ([]pgTriggerInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT t.tgname, pg_catalog.pg_get_triggerdef(t.oid, true), t.tgenabled::text
FROM pg_trigger t
JOIN pg_class c ON t.tgrelid = c.oid
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2 AND NOT t.tgisinternal
ORDER BY t.tgname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgTriggerInfo
	for rows.Next() {
		var t pgTriggerInfo
		if err := rows.Scan(&t.Name, &t.Def, &t.Enabled); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func fetchPGSchemaFunctions(ctx context.Context, db *sql.DB, schema string) ([]pgFunctionInfo, error) {
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `
SELECT p.proname,
	pg_catalog.pg_get_function_identity_arguments(p.oid),
	pg_catalog.pg_get_functiondef(p.oid)
FROM pg_proc p
JOIN pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = $1
	AND p.prokind IN ('f', 'p')
	AND NOT EXISTS (
		SELECT 1 FROM pg_depend d
		JOIN pg_extension e ON d.refobjid = e.oid
		WHERE d.objid = p.oid AND d.deptype = 'e'
	)
ORDER BY p.proname, 2`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgFunctionInfo
	for rows.Next() {
		var f pgFunctionInfo
		if err := rows.Scan(&f.Name, &f.Args, &f.Definition); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func pgFuncKey(f pgFunctionInfo) string {
	return strings.ToLower(f.Name) + "(" + strings.ToLower(strings.TrimSpace(f.Args)) + ")"
}

func normalizePGDefault(def string) string {
	s := strings.ToLower(strings.TrimSpace(def))
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.Join(strings.Fields(s), "")
	return s
}

func fetchPGConstraintsInfo(ctx context.Context, db *sql.DB, schema, table string) ([]pgConstraintInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT c.conname, c.contype::text, pg_get_constraintdef(c.oid, true) AS def
FROM pg_constraint c
JOIN pg_class cl ON c.conrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
WHERE n.nspname = $1
	AND cl.relname = $2
ORDER BY
	CASE c.contype
		WHEN 'p' THEN 1
		WHEN 'u' THEN 2
		WHEN 'c' THEN 3
		WHEN 'f' THEN 4
		ELSE 5
	END,
	c.conname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pgConstraintInfo
	for rows.Next() {
		var c pgConstraintInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Def); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchPGIndexesInfo(ctx context.Context, db *sql.DB, schema, table string) ([]pgIndexInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ci.relname
FROM pg_constraint c
JOIN pg_class cl ON c.conrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
JOIN pg_class ci ON c.conindid = ci.oid
WHERE n.nspname = $1
	AND cl.relname = $2
	AND c.conindid <> 0`, schema, table)
	if err != nil {
		return nil, err
	}
	backed := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		backed[name] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	idxRows, err := db.QueryContext(ctx, `
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = $1 AND tablename = $2
ORDER BY indexname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer idxRows.Close()
	var out []pgIndexInfo
	for idxRows.Next() {
		var idx pgIndexInfo
		if err := idxRows.Scan(&idx.Name, &idx.Def); err != nil {
			return nil, err
		}
		if _, ok := backed[idx.Name]; ok {
			continue
		}
		out = append(out, idx)
	}
	return out, idxRows.Err()
}

func comparePGTableSchemaInfo(src, tgt *pgTableSchemaInfo, tableLabel string) error {
	if src == nil || tgt == nil {
		return fmt.Errorf("表 %s 结构信息缺失", tableLabel)
	}
	if len(src.Columns) != len(tgt.Columns) {
		return fmt.Errorf("表 %s 列数量不一致: 源 %d 目标 %d", tableLabel, len(src.Columns), len(tgt.Columns))
	}
	for i := range src.Columns {
		sc, tc := src.Columns[i], tgt.Columns[i]
		if !strings.EqualFold(sc.Name, tc.Name) {
			return fmt.Errorf("表 %s 列名不匹配: 源 %s vs 目标 %s", tableLabel, sc.Name, tc.Name)
		}
		if !strings.EqualFold(normalizePGTypeName(sc.DataType), normalizePGTypeName(tc.DataType)) {
			return fmt.Errorf("表 %s 列 %s 类型不匹配: 源 %s vs 目标 %s", tableLabel, sc.Name, sc.DataType, tc.DataType)
		}
		if sc.NotNull != tc.NotNull {
			return fmt.Errorf("表 %s 列 %s NOT NULL 不一致: 源 %v 目标 %v", tableLabel, sc.Name, sc.NotNull, tc.NotNull)
		}
		if sc.Identity != tc.Identity {
			return fmt.Errorf("表 %s 列 %s IDENTITY 不一致: 源 %q 目标 %q", tableLabel, sc.Name, sc.Identity, tc.Identity)
		}
		if normalizePGDefault(sc.Default) != normalizePGDefault(tc.Default) {
			return fmt.Errorf("表 %s 列 %s 默认值不一致: 源 %q 目标 %q", tableLabel, sc.Name, truncateForErr(sc.Default, 120), truncateForErr(tc.Default, 120))
		}
	}
	if len(src.Constraints) != len(tgt.Constraints) {
		return fmt.Errorf("表 %s 表级约束数量不一致: 源 %d 目标 %d", tableLabel, len(src.Constraints), len(tgt.Constraints))
	}
	if err := comparePGConstraintList(src.Constraints, tgt.Constraints, tableLabel, "表级约束"); err != nil {
		return err
	}
	if len(src.ForeignKeys) != len(tgt.ForeignKeys) {
		return fmt.Errorf("表 %s 外键数量不一致: 源 %d 目标 %d", tableLabel, len(src.ForeignKeys), len(tgt.ForeignKeys))
	}
	if err := comparePGConstraintList(src.ForeignKeys, tgt.ForeignKeys, tableLabel, "外键"); err != nil {
		return err
	}
	if len(src.Indexes) != len(tgt.Indexes) {
		return fmt.Errorf("表 %s 索引数量不一致: 源 %d 目标 %d", tableLabel, len(src.Indexes), len(tgt.Indexes))
	}
	if pgIndexSummary(src.Indexes) != pgIndexSummary(tgt.Indexes) {
		return fmt.Errorf("表 %s 索引定义不一致", tableLabel)
	}
	return nil
}

func comparePGConstraintList(src, tgt []pgConstraintInfo, tableLabel, kindLabel string) error {
	for i := range src {
		sc, tc := src[i], tgt[i]
		if !strings.EqualFold(sc.Type, tc.Type) || !strings.EqualFold(sc.Name, tc.Name) {
			return fmt.Errorf("表 %s %s不一致: 源 %s/%s vs 目标 %s/%s",
				tableLabel, kindLabel, sc.Type, sc.Name, tc.Type, tc.Name)
		}
		if normalizePGConstraintDef(sc.Def) != normalizePGConstraintDef(tc.Def) {
			return fmt.Errorf("表 %s %s不一致: 约束 %s 源=%q 目标=%q",
				tableLabel, kindLabel, sc.Name, truncateForErr(sc.Def, 200), truncateForErr(tc.Def, 200))
		}
	}
	return nil
}

func pgIndexSummary(idxs []pgIndexInfo) string {
	names := make([]string, 0, len(idxs))
	byName := map[string]string{}
	for _, idx := range idxs {
		names = append(names, idx.Name)
		byName[idx.Name] = idx.Def
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "idx:%s:%s;", n, byName[n])
	}
	return b.String()
}

func truncateForErr(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// reArrayTextCast 匹配 ARRAY[...]::text[]，把「整段数组 cast」折成与「元素 ::text」等价形式。
var reArrayTextCast = regexp.MustCompile(`(?i)ARRAY\[(.*?)\]::text\[\]`)

// normalizePGConstraintDef 对 pg_get_constraintdef 输出做轻量归一，消除语义等价但 deparse 写法不同的误报。
// 覆盖：ARRAY['x'::character varying,...]::text[] vs ARRAY['x'::character varying::text,...]
func normalizePGConstraintDef(def string) string {
	s := strings.Join(strings.Fields(strings.TrimSpace(def)), " ")
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "::character varying::text", "::text")
	s = strings.ReplaceAll(s, "::CHARACTER VARYING::text", "::text")
	s = strings.ReplaceAll(s, "::character varying", "::varchar")
	s = strings.ReplaceAll(s, "::CHARACTER VARYING", "::varchar")
	s = reArrayTextCast.ReplaceAllStringFunc(s, func(m string) string {
		sub := reArrayTextCast.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := strings.ReplaceAll(sub[1], "::varchar", "::text")
		return "ARRAY[" + inner + "]"
	})
	return strings.ToLower(s)
}

func normalizePGTypeName(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}
