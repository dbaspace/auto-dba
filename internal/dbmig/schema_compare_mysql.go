package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ---- MySQL 表结构快照 ----

type mysqlColInfo struct {
	Name      string
	Type      string
	Nullable  bool
	Default   sql.NullString
	Extra     string
	Comment   string
	Ordinal   int
}

type mysqlConstraintInfo struct {
	Name string
	Type string // PRIMARY KEY | UNIQUE | CHECK
	Def  string // 列列表或 CHECK 表达式
}

type mysqlIndexInfo struct {
	Name    string
	Unique  bool
	Columns string // 逗号拼接
	Type    string // BTREE/...
	Def     string // 规范化摘要
}

type mysqlFKInfo struct {
	Name       string
	Columns    string
	RefTable   string
	RefColumns string
	OnUpdate   string
	OnDelete   string
}

type mysqlTriggerInfo struct {
	Name      string
	Timing    string
	Event     string
	Statement string
	CreateSQL string
}

type mysqlTableInfo struct {
	Columns      []mysqlColInfo
	Constraints  []mysqlConstraintInfo // PK / UNIQUE / CHECK（不含 FK）
	Indexes      []mysqlIndexInfo      // 非 PK 的索引（含 UNIQUE KEY 以 index 呈现时去重）
	ForeignKeys  []mysqlFKInfo
	TableComment string
	Triggers     []mysqlTriggerInfo
}

type mysqlRoutineInfo struct {
	Name       string
	Type       string // FUNCTION | PROCEDURE
	Definition string
}

func fetchMySQLTableInfo(ctx context.Context, db *sql.DB, table string) (*mysqlTableInfo, error) {
	out := &mysqlTableInfo{}
	cols, err := fetchMySQLColumns(ctx, db, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("表 %s 无可用列", table)
	}
	out.Columns = cols

	_ = db.QueryRowContext(ctx, `
SELECT COALESCE(TABLE_COMMENT,'') FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&out.TableComment)

	cons, backed, err := fetchMySQLConstraintsAndIndexBacked(ctx, db, table)
	if err != nil {
		return nil, err
	}
	out.Constraints = cons

	idxs, err := fetchMySQLIndexes(ctx, db, table, backed)
	if err != nil {
		return nil, err
	}
	out.Indexes = idxs

	fks, err := fetchMySQLForeignKeys(ctx, db, table)
	if err != nil {
		// 部分引擎可能不支持，降级为空
		fks = nil
	}
	out.ForeignKeys = fks

	// CHECK（MySQL 8.0.16+）；失败则跳过
	if checks, err := fetchMySQLChecks(ctx, db, table); err == nil {
		out.Constraints = append(out.Constraints, checks...)
	}

	trigs, err := fetchMySQLTriggers(ctx, db, table)
	if err == nil {
		out.Triggers = trigs
	}
	return out, nil
}

func fetchMySQLColumns(ctx context.Context, db *sql.DB, table string) ([]mysqlColInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT, ORDINAL_POSITION
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlColInfo
	for rows.Next() {
		var c mysqlColInfo
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &c.Default, &c.Extra, &c.Comment, &c.Ordinal); err != nil {
			return nil, err
		}
		c.Nullable = strings.EqualFold(nullable, "YES")
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchMySQLConstraintsAndIndexBacked(ctx context.Context, db *sql.DB, table string) ([]mysqlConstraintInfo, map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
SELECT tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE,
	GROUP_CONCAT(kcu.COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION SEPARATOR ',')
FROM information_schema.TABLE_CONSTRAINTS tc
JOIN information_schema.KEY_COLUMN_USAGE kcu
	ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
	AND tc.TABLE_NAME = kcu.TABLE_NAME
	AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = DATABASE() AND tc.TABLE_NAME = ?
	AND tc.CONSTRAINT_TYPE IN ('PRIMARY KEY','UNIQUE')
GROUP BY tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE
ORDER BY tc.CONSTRAINT_TYPE, tc.CONSTRAINT_NAME`, table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []mysqlConstraintInfo
	backed := map[string]struct{}{}
	for rows.Next() {
		var c mysqlConstraintInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Def); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
		backed[strings.ToLower(c.Name)] = struct{}{}
		if c.Type == "PRIMARY KEY" {
			backed["primary"] = struct{}{}
		}
	}
	return out, backed, rows.Err()
}

func fetchMySQLChecks(ctx context.Context, db *sql.DB, table string) ([]mysqlConstraintInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT tc.CONSTRAINT_NAME, cc.CHECK_CLAUSE
FROM information_schema.TABLE_CONSTRAINTS tc
JOIN information_schema.CHECK_CONSTRAINTS cc
	ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA
	AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = DATABASE() AND tc.TABLE_NAME = ?
	AND tc.CONSTRAINT_TYPE = 'CHECK'
ORDER BY tc.CONSTRAINT_NAME`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlConstraintInfo
	for rows.Next() {
		var c mysqlConstraintInfo
		c.Type = "CHECK"
		if err := rows.Scan(&c.Name, &c.Def); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func fetchMySQLIndexes(ctx context.Context, db *sql.DB, table string, constraintBacked map[string]struct{}) ([]mysqlIndexInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT INDEX_NAME, NON_UNIQUE, INDEX_TYPE,
	GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',')
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
GROUP BY INDEX_NAME, NON_UNIQUE, INDEX_TYPE
ORDER BY INDEX_NAME`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlIndexInfo
	for rows.Next() {
		var name, idxType, cols string
		var nonUnique int
		if err := rows.Scan(&name, &nonUnique, &idxType, &cols); err != nil {
			return nil, err
		}
		key := strings.ToLower(name)
		if key == "primary" {
			continue
		}
		if _, ok := constraintBacked[key]; ok {
			continue // UNIQUE 约束已在 constraints 里
		}
		idx := mysqlIndexInfo{
			Name: name, Unique: nonUnique == 0, Columns: cols, Type: idxType,
		}
		kind := "INDEX"
		if idx.Unique {
			kind = "UNIQUE INDEX"
		}
		idx.Def = fmt.Sprintf("%s (%s) USING %s", kind, cols, idxType)
		out = append(out, idx)
	}
	return out, rows.Err()
}

func fetchMySQLForeignKeys(ctx context.Context, db *sql.DB, table string) ([]mysqlFKInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
	rc.CONSTRAINT_NAME,
	GROUP_CONCAT(kcu.COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION SEPARATOR ','),
	kcu.REFERENCED_TABLE_NAME,
	GROUP_CONCAT(kcu.REFERENCED_COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION SEPARATOR ','),
	rc.UPDATE_RULE, rc.DELETE_RULE
FROM information_schema.REFERENTIAL_CONSTRAINTS rc
JOIN information_schema.KEY_COLUMN_USAGE kcu
	ON rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
	AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
	AND rc.TABLE_NAME = kcu.TABLE_NAME
WHERE rc.CONSTRAINT_SCHEMA = DATABASE() AND rc.TABLE_NAME = ?
GROUP BY rc.CONSTRAINT_NAME, kcu.REFERENCED_TABLE_NAME, rc.UPDATE_RULE, rc.DELETE_RULE
ORDER BY rc.CONSTRAINT_NAME`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlFKInfo
	for rows.Next() {
		var f mysqlFKInfo
		if err := rows.Scan(&f.Name, &f.Columns, &f.RefTable, &f.RefColumns, &f.OnUpdate, &f.OnDelete); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func fetchMySQLTriggers(ctx context.Context, db *sql.DB, table string) ([]mysqlTriggerInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT TRIGGER_NAME, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
FROM information_schema.TRIGGERS
WHERE TRIGGER_SCHEMA = DATABASE() AND EVENT_OBJECT_TABLE = ?
ORDER BY TRIGGER_NAME`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlTriggerInfo
	for rows.Next() {
		var t mysqlTriggerInfo
		if err := rows.Scan(&t.Name, &t.Timing, &t.Event, &t.Statement); err != nil {
			return nil, err
		}
		// 优先用 information_schema 拼装，避免 SHOW CREATE TRIGGER 列数差异
		t.CreateSQL = fmt.Sprintf("CREATE TRIGGER `%s` %s %s ON `%s` FOR EACH ROW %s",
			t.Name, t.Timing, t.Event, table, t.Statement)
		out = append(out, t)
	}
	return out, rows.Err()
}

func fetchMySQLRoutines(ctx context.Context, db *sql.DB) ([]mysqlRoutineInfo, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ROUTINE_NAME, ROUTINE_TYPE
FROM information_schema.ROUTINES
WHERE ROUTINE_SCHEMA = DATABASE()
ORDER BY ROUTINE_TYPE, ROUTINE_NAME`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mysqlRoutineInfo
	for rows.Next() {
		var r mysqlRoutineInfo
		if err := rows.Scan(&r.Name, &r.Type); err != nil {
			return nil, err
		}
		def, err := showMySQLCreateRoutine(ctx, db, r.Type, r.Name)
		if err != nil {
			r.Definition = "-- unable to fetch body for " + r.Name + ": " + err.Error()
		} else {
			r.Definition = def
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func showMySQLCreateRoutine(ctx context.Context, db *sql.DB, routineType, name string) (string, error) {
	q := "SHOW CREATE FUNCTION `" + strings.ReplaceAll(name, "`", "``") + "`"
	if strings.EqualFold(routineType, "PROCEDURE") {
		q = "SHOW CREATE PROCEDURE `" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		return "", fmt.Errorf("no row")
	}
	raw := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return "", err
	}
	// Create Function / Create Procedure 通常在第 3 列（index 2）
	for _, idx := range []int{2, 1, 0} {
		if idx < len(raw) && raw[idx].Valid && strings.Contains(strings.ToUpper(raw[idx].String), "CREATE ") {
			return raw[idx].String, nil
		}
	}
	for _, v := range raw {
		if v.Valid && strings.Contains(strings.ToUpper(v.String), "CREATE ") {
			return v.String, nil
		}
	}
	return "", fmt.Errorf("create ddl not found")
}

func formatMySQLColSummary(c mysqlColInfo) string {
	null := "NULL"
	if !c.Nullable {
		null = "NOT NULL"
	}
	def := "(无)"
	if c.Default.Valid {
		def = c.Default.String
	}
	extra := strings.TrimSpace(c.Extra)
	s := fmt.Sprintf("%s %s default=%s", c.Type, null, def)
	if extra != "" {
		s += " " + extra
	}
	return s
}

func normalizeMySQLDefault(d sql.NullString) string {
	if !d.Valid {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(d.String))
}

func diffMySQLTableSchema(left, right *mysqlTableInfo) []SchemaDiffItem {
	var diffs []SchemaDiffItem
	lCols := map[string]mysqlColInfo{}
	rCols := map[string]mysqlColInfo{}
	for _, c := range left.Columns {
		lCols[strings.ToLower(c.Name)] = c
	}
	for _, c := range right.Columns {
		rCols[strings.ToLower(c.Name)] = c
	}
	all := map[string]struct{}{}
	for k := range lCols {
		all[k] = struct{}{}
	}
	for k := range rCols {
		all[k] = struct{}{}
	}
	for _, k := range sortedKeys(all) {
		lc, lok := lCols[k]
		rc, rok := rCols[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: rc.Name, Right: formatMySQLColSummary(rc), Message: "仅目标端有该列"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "column", Name: lc.Name, Left: formatMySQLColSummary(lc), Message: "仅源端有该列"})
		default:
			typeDiff := !strings.EqualFold(lc.Type, rc.Type)
			nullDiff := lc.Nullable != rc.Nullable
			defDiff := normalizeMySQLDefault(lc.Default) != normalizeMySQLDefault(rc.Default)
			extraDiff := !strings.EqualFold(strings.TrimSpace(lc.Extra), strings.TrimSpace(rc.Extra))
			if typeDiff {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "column", Name: lc.Name,
					Left: formatMySQLColSummary(lc), Right: formatMySQLColSummary(rc),
					Message: "列类型不一致",
				})
			} else if nullDiff || defDiff || extraDiff {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "column", Name: lc.Name,
					Left: formatMySQLColSummary(lc), Right: formatMySQLColSummary(rc),
					Message: "列定义不一致",
				})
			}
			if strings.TrimSpace(lc.Comment) != strings.TrimSpace(rc.Comment) {
				switch {
				case strings.TrimSpace(lc.Comment) != "" && strings.TrimSpace(rc.Comment) == "":
					diffs = append(diffs, SchemaDiffItem{Kind: "comment", Name: lc.Name, Left: lc.Comment, Message: "仅源端有列注释"})
				case strings.TrimSpace(lc.Comment) == "" && strings.TrimSpace(rc.Comment) != "":
					diffs = append(diffs, SchemaDiffItem{Kind: "comment", Name: rc.Name, Right: rc.Comment, Message: "仅目标端有列注释"})
				default:
					diffs = append(diffs, SchemaDiffItem{Kind: "comment", Name: lc.Name, Left: lc.Comment, Right: rc.Comment, Message: "列注释不一致"})
				}
			}
		}
	}

	// 表注释
	lt, rt := strings.TrimSpace(left.TableComment), strings.TrimSpace(right.TableComment)
	if lt != rt {
		switch {
		case lt != "" && rt == "":
			diffs = append(diffs, SchemaDiffItem{Kind: "comment", Left: lt, Message: "仅源端有表注释"})
		case lt == "" && rt != "":
			diffs = append(diffs, SchemaDiffItem{Kind: "comment", Right: rt, Message: "仅目标端有表注释"})
		default:
			diffs = append(diffs, SchemaDiffItem{Kind: "comment", Left: lt, Right: rt, Message: "表注释不一致"})
		}
	}

	diffs = append(diffs, diffMySQLConstraints(left.Constraints, right.Constraints)...)
	diffs = append(diffs, diffMySQLIndexes(left.Indexes, right.Indexes)...)
	diffs = append(diffs, diffMySQLFKs(left.ForeignKeys, right.ForeignKeys)...)
	diffs = append(diffs, diffMySQLTriggers(left.Triggers, right.Triggers)...)
	return diffs
}

func diffMySQLConstraints(left, right []mysqlConstraintInfo) []SchemaDiffItem {
	lMap := map[string]mysqlConstraintInfo{}
	rMap := map[string]mysqlConstraintInfo{}
	for _, c := range left {
		lMap[strings.ToLower(c.Name)+"|"+strings.ToUpper(c.Type)] = c
	}
	for _, c := range right {
		rMap[strings.ToLower(c.Name)+"|"+strings.ToUpper(c.Type)] = c
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
			diffs = append(diffs, SchemaDiffItem{Kind: "constraint", Name: rc.Name, Right: rc.Type + " " + rc.Def, Message: "仅目标端有该约束"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "constraint", Name: lc.Name, Left: lc.Type + " " + lc.Def, Message: "仅源端有该约束"})
		default:
			if !strings.EqualFold(lc.Def, rc.Def) || !strings.EqualFold(lc.Type, rc.Type) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "constraint", Name: lc.Name,
					Left: lc.Type + " " + lc.Def, Right: rc.Type + " " + rc.Def,
					Message: "约束定义不一致",
				})
			}
		}
	}
	return diffs
}

func diffMySQLIndexes(left, right []mysqlIndexInfo) []SchemaDiffItem {
	lMap := map[string]mysqlIndexInfo{}
	rMap := map[string]mysqlIndexInfo{}
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
			diffs = append(diffs, SchemaDiffItem{Kind: "index", Name: rc.Name, Right: rc.Def, Message: "仅目标端有该索引"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "index", Name: lc.Name, Left: lc.Def, Message: "仅源端有该索引"})
		default:
			if !strings.EqualFold(lc.Def, rc.Def) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "index", Name: lc.Name, Left: lc.Def, Right: rc.Def, Message: "索引定义不一致",
				})
			}
		}
	}
	return diffs
}

func diffMySQLFKs(left, right []mysqlFKInfo) []SchemaDiffItem {
	lMap := map[string]mysqlFKInfo{}
	rMap := map[string]mysqlFKInfo{}
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
	format := func(f mysqlFKInfo) string {
		return fmt.Sprintf("(%s) REFERENCES %s(%s) ON UPDATE %s ON DELETE %s",
			f.Columns, f.RefTable, f.RefColumns, f.OnUpdate, f.OnDelete)
	}
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lc, lok := lMap[k]
		rc, rok := rMap[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "foreign_key", Name: rc.Name, Right: format(rc), Message: "仅目标端有该外键"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "foreign_key", Name: lc.Name, Left: format(lc), Message: "仅源端有该外键"})
		default:
			if format(lc) != format(rc) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "foreign_key", Name: lc.Name, Left: format(lc), Right: format(rc), Message: "外键定义不一致",
				})
			}
		}
	}
	return diffs
}

func diffMySQLTriggers(left, right []mysqlTriggerInfo) []SchemaDiffItem {
	lMap := map[string]mysqlTriggerInfo{}
	rMap := map[string]mysqlTriggerInfo{}
	for _, t := range left {
		lMap[strings.ToLower(t.Name)] = t
	}
	for _, t := range right {
		rMap[strings.ToLower(t.Name)] = t
	}
	all := map[string]struct{}{}
	for k := range lMap {
		all[k] = struct{}{}
	}
	for k := range rMap {
		all[k] = struct{}{}
	}
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lt, lok := lMap[k]
		rt, rok := rMap[k]
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "trigger", Name: rt.Name, Right: rt.CreateSQL, Message: "仅目标端有该触发器"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "trigger", Name: lt.Name, Left: lt.CreateSQL, Message: "仅源端有该触发器"})
		default:
			if norm(lt.Statement) != norm(rt.Statement) || !strings.EqualFold(lt.Timing, rt.Timing) || !strings.EqualFold(lt.Event, rt.Event) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "trigger", Name: lt.Name, Left: lt.CreateSQL, Right: rt.CreateSQL, Message: "触发器定义不一致",
				})
			}
		}
	}
	return diffs
}

func compareMySQLRoutines(ctx context.Context, left, right *sql.DB) []TableSchemaDiff {
	lf, errL := fetchMySQLRoutines(ctx, left)
	rf, errR := fetchMySQLRoutines(ctx, right)
	if errL != nil {
		return []TableSchemaDiff{{Table: schemaRoutinesVirtualTable, Status: "differ", Message: "读源端例程失败: " + errL.Error()}}
	}
	if errR != nil {
		return []TableSchemaDiff{{Table: schemaRoutinesVirtualTable, Status: "differ", Message: "读目标端例程失败: " + errR.Error()}}
	}
	lMap := map[string]mysqlRoutineInfo{}
	rMap := map[string]mysqlRoutineInfo{}
	key := func(r mysqlRoutineInfo) string {
		return strings.ToUpper(r.Type) + ":" + strings.ToLower(r.Name)
	}
	for _, r := range lf {
		lMap[key(r)] = r
	}
	for _, r := range rf {
		rMap[key(r)] = r
	}
	all := map[string]struct{}{}
	for k := range lMap {
		all[k] = struct{}{}
	}
	for k := range rMap {
		all[k] = struct{}{}
	}
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	var diffs []SchemaDiffItem
	for _, k := range sortedKeys(all) {
		lr, lok := lMap[k]
		rr, rok := rMap[k]
		label := lr.Name
		rtype := lr.Type
		if !lok {
			label, rtype = rr.Name, rr.Type
		}
		switch {
		case !lok:
			diffs = append(diffs, SchemaDiffItem{Kind: "function", Name: rtype + " " + label, Right: truncateForErr(rr.Definition, 200), Message: "仅目标端有该函数"})
		case !rok:
			diffs = append(diffs, SchemaDiffItem{Kind: "function", Name: rtype + " " + label, Left: truncateForErr(lr.Definition, 200), Message: "仅源端有该函数"})
		default:
			if norm(lr.Definition) != norm(rr.Definition) {
				diffs = append(diffs, SchemaDiffItem{
					Kind: "function", Name: rtype + " " + label,
					Left: truncateForErr(lr.Definition, 200), Right: truncateForErr(rr.Definition, 200),
					Message: "函数定义不一致",
				})
			}
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return []TableSchemaDiff{{
		Table: schemaRoutinesVirtualTable, Status: "differ",
		Message: fmt.Sprintf("%d 处例程差异", len(diffs)), Diffs: diffs,
	}}
}

func quoteMySQLIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quoteMySQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func mysqlColDDLPart(c mysqlColInfo) string {
	part := quoteMySQLIdent(c.Name) + " " + c.Type
	if !c.Nullable {
		part += " NOT NULL"
	} else {
		part += " NULL"
	}
	if c.Default.Valid {
		part += " DEFAULT " + quoteMySQLDefault(c.Default.String, c.Type)
	}
	if strings.TrimSpace(c.Extra) != "" {
		part += " " + c.Extra
	}
	if strings.TrimSpace(c.Comment) != "" {
		part += " COMMENT " + quoteMySQLString(c.Comment)
	}
	return part
}
