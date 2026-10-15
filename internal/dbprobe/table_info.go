package dbprobe

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"auto-dba/internal/store"
)

type TableColumnInfo struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type TableInfo struct {
	Schema  string            `json:"schema"`
	Table   string            `json:"table"`
	Columns []TableColumnInfo `json:"columns"`
	DDL     string            `json:"ddl"`
}

var reFromTable = regexp.MustCompile(`(?is)\bfrom\s+((?:["\x60\[]?[A-Za-z_][\w$]*["\x60\]]?\.)?["\x60\[]?[A-Za-z_][\w$]*["\x60\]]?)`)

// FirstTableFromSQL 取 FROM 后第一个表（忽略后续 JOIN / 多表）。
func FirstTableFromSQL(raw string) (schema, table string, err error) {
	s := stripSQLComments(strings.TrimSpace(raw))
	if s == "" {
		return "", "", fmt.Errorf("SQL 不能为空")
	}
	m := reFromTable.FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf("未在 SQL 中找到 FROM 表")
	}
	ref := strings.TrimSpace(m[1])
	parts := strings.Split(ref, ".")
	if len(parts) == 1 {
		return "", unquoteIdent(parts[0]), nil
	}
	return unquoteIdent(parts[0]), unquoteIdent(parts[len(parts)-1]), nil
}

func unquoteIdent(s string) string {
	return strings.Trim(strings.TrimSpace(s), "\"`[]")
}

func stripSQLComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSingle, inDouble := false, false
	for i := 0; i < len(s); {
		if !inSingle && !inDouble && i+1 < len(s) && s[i] == '-' && s[i+1] == '-' {
			if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
				i += j + 1
				continue
			}
			break
		}
		if !inSingle && !inDouble && i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			if j := strings.Index(s[i:], "*/"); j >= 0 {
				i += j + 2
				continue
			}
			break
		}
		c := s[i]
		if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '"' && !inSingle {
			inDouble = !inDouble
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// DescribeTable 查询表字段与建表 SQL（mysql 族 / postgres）。
func DescribeTable(ctx context.Context, inst *store.Instance, password, dbName, schema, table string) (*TableInfo, error) {
	host, user, dbType, err := instConn(inst)
	if err != nil {
		return nil, err
	}
	table = strings.TrimSpace(table)
	if table == "" {
		return nil, fmt.Errorf("表名不能为空")
	}
	switch family(dbType) {
	case "mysql":
		return describeMySQLTable(ctx, host, inst.Port, user, password, dbName, table)
	case "postgres":
		if strings.TrimSpace(schema) == "" {
			schema = "public"
		}
		return describePostgresTable(ctx, host, inst.Port, user, password, dbName, schema, table)
	default:
		return nil, fmt.Errorf("暂不支持该实例类型查看表结构: %s", inst.DBType)
	}
}

func describeMySQLTable(ctx context.Context, host string, port int, user, password, dbName, table string) (*TableInfo, error) {
	db, err := openMySQL(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE,
			IFNULL(COLUMN_DEFAULT, ''), IFNULL(COLUMN_COMMENT, '')
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, dbName, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make([]TableColumnInfo, 0)
	for rows.Next() {
		var name, typ, nullable, def, comment string
		if err := rows.Scan(&name, &typ, &nullable, &def, &comment); err != nil {
			return nil, err
		}
		cols = append(cols, TableColumnInfo{
			Name:     name,
			DataType: typ,
			Nullable: strings.EqualFold(nullable, "YES"),
			Default:  def,
			Comment:  comment,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("表不存在或无字段: %s", table)
	}

	var tbl, ddl string
	q := "SHOW CREATE TABLE `" + strings.ReplaceAll(table, "`", "``") + "`"
	if err := db.QueryRowContext(ctx, q).Scan(&tbl, &ddl); err != nil {
		return nil, fmt.Errorf("SHOW CREATE TABLE: %w", err)
	}
	ddl = strings.TrimSpace(ddl)
	if ddl != "" && !strings.HasSuffix(ddl, ";") {
		ddl += ";"
	}
	// 附带触发器
	if trigDDL, err := mysqlTriggersDDL(ctx, db, dbName, table); err == nil && trigDDL != "" {
		ddl += "\n" + trigDDL
	}

	return &TableInfo{
		Schema:  dbName,
		Table:   table,
		Columns: cols,
		DDL:     strings.TrimSpace(ddl) + "\n",
	}, nil
}

func mysqlTriggersDDL(ctx context.Context, db *sql.DB, dbName, table string) (string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT TRIGGER_NAME, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
		FROM information_schema.TRIGGERS
		WHERE TRIGGER_SCHEMA = ? AND EVENT_OBJECT_TABLE = ?
		ORDER BY TRIGGER_NAME`, dbName, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var name, timing, event, stmt string
		if err := rows.Scan(&name, &timing, &event, &stmt); err != nil {
			return "", err
		}
		createSQL := fmt.Sprintf(
			"CREATE TRIGGER `%s` %s %s ON `%s` FOR EACH ROW %s",
			strings.ReplaceAll(name, "`", "``"),
			timing, event,
			strings.ReplaceAll(table, "`", "``"),
			stmt,
		)
		if !strings.HasSuffix(strings.TrimSpace(createSQL), ";") {
			createSQL = strings.TrimSpace(createSQL) + ";"
		}
		b.WriteString(createSQL + "\n")
	}
	return b.String(), rows.Err()
}

func describePostgresTable(ctx context.Context, host string, port int, user, password, dbName, schema, table string) (*TableInfo, error) {
	db, err := openPostgres(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT a.attname AS column_name,
			pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
			NOT a.attnotnull AS is_nullable,
			COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') AS column_default,
			COALESCE(col_description(a.attrelid, a.attnum), '') AS comment
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2
			AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make([]TableColumnInfo, 0)
	for rows.Next() {
		var name, typ, def, comment string
		var nullable bool
		if err := rows.Scan(&name, &typ, &nullable, &def, &comment); err != nil {
			return nil, err
		}
		cols = append(cols, TableColumnInfo{
			Name:     name,
			DataType: typ,
			Nullable: nullable,
			Default:  def,
			Comment:  comment,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("表不存在或无字段: %s.%s", schema, table)
	}

	ddl, err := buildPostgresTableDDL(ctx, db, schema, table, cols)
	if err != nil {
		return nil, err
	}

	return &TableInfo{
		Schema:  schema,
		Table:   table,
		Columns: cols,
		DDL:     ddl,
	}, nil
}

var rePGNextval = regexp.MustCompile(`(?i)nextval\s*\(\s*'([^']+)'\s*(?:::\s*regclass)?\s*\)`)

func buildPostgresTableDDL(ctx context.Context, db *sql.DB, schema, table string, cols []TableColumnInfo) (string, error) {
	var b strings.Builder
	fq := pgQuoteIdent(schema) + "." + pgQuoteIdent(table)

	// 扩展依赖（字段类型 / 表依赖的插件）
	for _, ext := range pgTableExtensions(ctx, db, schema, table) {
		b.WriteString(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s;\n", pgQuoteIdent(ext)))
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}

	// 序列：建表前先建序列，避免 DEFAULT nextval 执行失败
	seqOwned := map[string]string{} // "schema.seq" -> "schema.table.col"
	for _, c := range cols {
		seqSchema, seqName := parsePGNextvalSeq(c.Default, schema)
		if seqName == "" {
			var serialSeq sql.NullString
			_ = db.QueryRowContext(ctx, `SELECT pg_get_serial_sequence($1, $2)`,
				schema+"."+table, c.Name).Scan(&serialSeq)
			if serialSeq.Valid && strings.TrimSpace(serialSeq.String) != "" {
				seqSchema, seqName = splitPGQualifiedName(serialSeq.String, schema)
			}
		}
		if seqName == "" {
			continue
		}
		key := seqSchema + "." + seqName
		if _, ok := seqOwned[key]; !ok {
			seqOwned[key] = schema + "." + table + "." + c.Name
			if ddl := pgSequenceCreateSQL(ctx, db, seqSchema, seqName); ddl != "" {
				b.WriteString(ddl)
			}
		}
	}
	if len(seqOwned) > 0 {
		b.WriteByte('\n')
	}

	b.WriteString("CREATE TABLE " + fq + " (\n")
	for i, c := range cols {
		line := "    " + pgQuoteIdent(c.Name) + " " + c.DataType
		if !c.Nullable {
			line += " NOT NULL"
		}
		if d := strings.TrimSpace(c.Default); d != "" {
			line += " DEFAULT " + d
		}
		if i < len(cols)-1 {
			line += ","
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(");\n\n")

	// 主键约束
	var pkName, pkCols string
	err := db.QueryRowContext(ctx, `
		SELECT con.conname,
			array_to_string(ARRAY(
				SELECT a.attname
				FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
				ORDER BY k.ord
			), ', ')
		FROM pg_constraint con
		JOIN pg_class t ON t.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE con.contype = 'p' AND n.nspname = $1 AND t.relname = $2
		LIMIT 1`, schema, table).Scan(&pkName, &pkCols)
	if err == nil && pkName != "" {
		b.WriteString(fmt.Sprintf(
			"ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s);\n",
			fq, pgQuoteIdent(pkName), quotePGColList(pkCols),
		))
	}

	// CHECK 约束（UNIQUE 由下方索引定义表达，避免重复）
	conRows, err := db.QueryContext(ctx, `
		SELECT con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		JOIN pg_class t ON t.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND t.relname = $2
			AND con.contype = 'c'
		ORDER BY con.conname`, schema, table)
	if err == nil {
		defer conRows.Close()
		for conRows.Next() {
			var name, def string
			if err := conRows.Scan(&name, &def); err != nil {
				continue
			}
			def = strings.TrimSpace(def)
			if def == "" {
				continue
			}
			b.WriteString(fmt.Sprintf(
				"ALTER TABLE %s ADD CONSTRAINT %s %s;\n",
				fq, pgQuoteIdent(name), def,
			))
		}
	}

	// 索引（含唯一索引；跳过主键对应索引，已用 CONSTRAINT 表达）
	idxRows, err := db.QueryContext(ctx, `
		SELECT i.relname, pg_get_indexdef(i.oid, 0, true), ix.indisprimary
		FROM pg_class t
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_index ix ON ix.indrelid = t.oid
		JOIN pg_class i ON i.oid = ix.indexrelid
		WHERE n.nspname = $1 AND t.relname = $2
		ORDER BY ix.indisprimary DESC, i.relname`, schema, table)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var name, def string
			var primary bool
			if err := idxRows.Scan(&name, &def, &primary); err != nil {
				continue
			}
			if primary {
				continue
			}
			def = strings.TrimSpace(def)
			if def == "" {
				continue
			}
			if !strings.HasSuffix(def, ";") {
				def += ";"
			}
			b.WriteString(def + "\n")
		}
	}

	// 外键
	fkRows, err := db.QueryContext(ctx, `
		SELECT con.conname, pg_get_constraintdef(con.oid, true)
		FROM pg_constraint con
		JOIN pg_class t ON t.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE con.contype = 'f' AND n.nspname = $1 AND t.relname = $2
		ORDER BY con.conname`, schema, table)
	if err == nil {
		defer fkRows.Close()
		for fkRows.Next() {
			var name, def string
			if err := fkRows.Scan(&name, &def); err != nil {
				continue
			}
			def = strings.TrimSpace(def)
			if def == "" {
				continue
			}
			b.WriteString(fmt.Sprintf(
				"ALTER TABLE %s ADD CONSTRAINT %s %s;\n",
				fq, pgQuoteIdent(name), def,
			))
		}
	}

	// 表注释
	var tableComment string
	_ = db.QueryRowContext(ctx, `
		SELECT COALESCE(obj_description(c.oid, 'pg_class'), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, schema, table).Scan(&tableComment)
	if strings.TrimSpace(tableComment) != "" {
		b.WriteString(fmt.Sprintf("COMMENT ON TABLE %s IS %s;\n", fq, pgQuoteString(tableComment)))
	}

	// 列注释
	for _, c := range cols {
		if strings.TrimSpace(c.Comment) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(
			"COMMENT ON COLUMN %s.%s IS %s;\n",
			fq, pgQuoteIdent(c.Name), pgQuoteString(c.Comment),
		))
	}

	// 序列归属
	seqKeys := make([]string, 0, len(seqOwned))
	for k := range seqOwned {
		seqKeys = append(seqKeys, k)
	}
	sort.Strings(seqKeys)
	for _, seqKey := range seqKeys {
		owner := seqOwned[seqKey]
		parts := strings.Split(seqKey, ".")
		ownerParts := strings.Split(owner, ".")
		if len(parts) != 2 || len(ownerParts) != 3 {
			continue
		}
		b.WriteString(fmt.Sprintf(
			"ALTER SEQUENCE %s.%s OWNED BY %s.%s.%s;\n",
			pgQuoteIdent(parts[0]), pgQuoteIdent(parts[1]),
			pgQuoteIdent(ownerParts[0]), pgQuoteIdent(ownerParts[1]), pgQuoteIdent(ownerParts[2]),
		))
	}

	// 触发器
	trigRows, err := db.QueryContext(ctx, `
		SELECT t.tgname, pg_get_triggerdef(t.oid, true)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
			AND NOT t.tgisinternal
		ORDER BY t.tgname`, schema, table)
	if err == nil {
		defer trigRows.Close()
		for trigRows.Next() {
			var name, def string
			if err := trigRows.Scan(&name, &def); err != nil {
				continue
			}
			def = strings.TrimSpace(def)
			if def == "" {
				continue
			}
			if !strings.HasSuffix(def, ";") {
				def += ";"
			}
			b.WriteString(def + "\n")
		}
	}

	return strings.TrimSpace(b.String()) + "\n", nil
}

func pgQuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func pgQuoteString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func quotePGColList(csv string) string {
	parts := strings.Split(csv, ", ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, pgQuoteIdent(p))
	}
	return strings.Join(out, ", ")
}

func parsePGNextvalSeq(def, defaultSchema string) (schema, name string) {
	m := rePGNextval.FindStringSubmatch(strings.TrimSpace(def))
	if len(m) < 2 {
		return "", ""
	}
	return splitPGQualifiedName(m[1], defaultSchema)
}

func splitPGQualifiedName(raw, defaultSchema string) (schema, name string) {
	raw = strings.TrimSpace(strings.Trim(raw, `"'`))
	if raw == "" {
		return "", ""
	}
	parts := strings.Split(raw, ".")
	if len(parts) >= 2 {
		return strings.Trim(parts[0], `"'`), strings.Trim(parts[len(parts)-1], `"'`)
	}
	return defaultSchema, strings.Trim(parts[0], `"'`)
}

func pgTableExtensions(ctx context.Context, db *sql.DB, schema, table string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		// 内置/核心扩展无需输出
		switch name {
		case "plpgsql":
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	// 列类型所属扩展
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT e.extname
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_type t ON t.oid = a.atttypid
		JOIN pg_depend d ON d.objid = t.oid AND d.deptype = 'e'
		JOIN pg_extension e ON e.oid = d.refobjid
		WHERE n.nspname = $1 AND c.relname = $2
			AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY 1`, schema, table)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				add(name)
			}
		}
	}

	// 索引操作符类 / 访问方法所属扩展（如 pg_trgm、btree_gin）
	idxRows, err := db.QueryContext(ctx, `
		SELECT DISTINCT e.extname
		FROM pg_class t
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_index ix ON ix.indrelid = t.oid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_depend d ON d.objid = i.oid AND d.deptype = 'e'
		JOIN pg_extension e ON e.oid = d.refobjid
		WHERE n.nspname = $1 AND t.relname = $2
		ORDER BY 1`, schema, table)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var name string
			if idxRows.Scan(&name) == nil {
				add(name)
			}
		}
	}

	// 默认值/表达式中用到的函数所属扩展（如 uuid_generate_v4）
	fnRows, err := db.QueryContext(ctx, `
		SELECT DISTINCT e.extname
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		JOIN pg_depend d ON d.objid = ad.oid AND d.deptype = 'n'
		JOIN pg_proc p ON p.oid = d.refobjid
		JOIN pg_depend de ON de.objid = p.oid AND de.deptype = 'e'
		JOIN pg_extension e ON e.oid = de.refobjid
		WHERE n.nspname = $1 AND c.relname = $2
			AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY 1`, schema, table)
	if err == nil {
		defer fnRows.Close()
		for fnRows.Next() {
			var name string
			if fnRows.Scan(&name) == nil {
				add(name)
			}
		}
	}

	return out
}

func pgSequenceCreateSQL(ctx context.Context, db *sql.DB, schema, name string) string {
	var dataType, startVal, minVal, maxVal, increment, cacheSize string
	var cycle bool
	err := db.QueryRowContext(ctx, `
		SELECT data_type::text,
			start_value::text,
			min_value::text,
			max_value::text,
			increment_by::text,
			cache_size::text,
			cycle
		FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = $2`, schema, name).Scan(
		&dataType, &startVal, &minVal, &maxVal, &increment, &cacheSize, &cycle,
	)
	seqRef := pgQuoteIdent(schema) + "." + pgQuoteIdent(name)
	if err != nil {
		// 序列可能对当前用户不可见，至少输出最小建序列语句
		return fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s;\n", seqRef)
	}

	var b strings.Builder
	b.WriteString("CREATE SEQUENCE IF NOT EXISTS " + seqRef + "\n")
	if dt := strings.TrimSpace(dataType); dt != "" && !strings.EqualFold(dt, "bigint") {
		b.WriteString("    AS " + dt + "\n")
	}
	if startVal != "" {
		b.WriteString("    START WITH " + startVal + "\n")
	}
	if increment != "" {
		b.WriteString("    INCREMENT BY " + increment + "\n")
	}
	if minVal != "" {
		// 常见默认小值用 NO MINVALUE 更可读
		if minVal == "1" || minVal == "-9223372036854775808" {
			b.WriteString("    NO MINVALUE\n")
		} else {
			b.WriteString("    MINVALUE " + minVal + "\n")
		}
	}
	if maxVal != "" {
		if maxVal == "9223372036854775807" || maxVal == "2147483647" {
			b.WriteString("    NO MAXVALUE\n")
		} else {
			b.WriteString("    MAXVALUE " + maxVal + "\n")
		}
	}
	if cacheSize != "" {
		b.WriteString("    CACHE " + cacheSize + "\n")
	}
	if cycle {
		b.WriteString("    CYCLE;\n")
	} else {
		b.WriteString("    NO CYCLE;\n")
	}
	return b.String()
}
