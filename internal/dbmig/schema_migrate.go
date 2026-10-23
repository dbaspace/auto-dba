package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// schemaDeferred 是建表之后才能执行的语句。外键和触发器要等全部表与数据完成。
type schemaDeferred struct {
	Created  bool
	FKs      []string
	Triggers []string
	SeqSyncs []pgSeqSync
}

type pgSeqSync struct {
	Schema    string
	Table     string
	Column    string
	LastValue int64
	HasLast   bool
	Fallback  string // schema.seq，pg_get_serial_sequence 为空时使用
}

type pgTablePair struct {
	SrcSchema string
	SrcTable  string
	TgtSchema string
	TgtTable  string
}

type migCol struct {
	Name              string
	DataType          string
	NotNull           bool
	Default           string
	Identity          string
	Generated         string
	SerialKind        string
	SerialSequence    string
	ExternalSeqSchema string
	ExternalSeqName   string
	Comment           string
}

type migSeq struct {
	Schema        string
	Name          string
	DataType      string
	StartValue    int64
	MinValue      int64
	MaxValue      int64
	Increment     int64
	Cycle         bool
	Cache         int64
	LastValue     int64
	HasLast       bool
	OwnedByColumn string
	SetOwnedBy    bool
}

type migTrigger struct {
	Name        string
	Def         string
	Enabled     string
	FunctionSQL string
}

type pgMigTable struct {
	Columns      []migCol
	Constraints  []pgConstraintInfo
	ForeignKeys  []pgConstraintInfo
	Indexes      []pgIndexInfo
	Triggers     []migTrigger
	Sequences    []migSeq
	TableComment string
	RowSecurity  bool
	ForceRLS     bool
	Policies     []pgPolicyInfo
	PartitionBy  string
	SeqSyncs     []pgSeqSync
}

type pgMigPlan struct {
	Body     []string
	FKs      []string
	Triggers []string
	SeqSyncs []pgSeqSync
}

func tablePairsFromObjects(objs []store.MigrationObject) []pgTablePair {
	out := make([]pgTablePair, 0, len(objs))
	for _, o := range objs {
		ss := firstNonEmpty(o.SourceSchema, "public")
		ts := firstNonEmpty(o.TargetSchema, ss)
		out = append(out, pgTablePair{
			SrcSchema: ss,
			SrcTable:  o.SourceTable,
			TgtSchema: ts,
			TgtTable:  o.TargetTable,
		})
	}
	return out
}

func execSQL(ctx context.Context, db *sql.DB, label, stmt string) error {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" || stmt == ";" {
		return nil
	}
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("%s失败: %w\nSQL: %s", label, err, stmt)
	}
	return nil
}

func filterPGExtensions(names []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || strings.EqualFold(n, "plpgsql") {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func extensionInstallError(ext, sqlText string, err error) error {
	return fmt.Errorf("安装 PostgreSQL 扩展 %s 失败: %w\n请在目标实例安装对应扩展包（如 postgis）并授权后重试\nSQL: %s", ext, err, sqlText)
}

func preparePGDatabase(ctx context.Context, src, tgt *sql.DB) error {
	if err := applyPGExtensions(ctx, src, tgt); err != nil {
		return err
	}
	return applyPGTextSearchConfigs(ctx, src, tgt)
}

func applyPGExtensions(ctx context.Context, src, tgt *sql.DB) error {
	rows, err := src.QueryContext(ctx, `SELECT extname FROM pg_extension ORDER BY 1`)
	if err != nil {
		return fmt.Errorf("读取源库扩展失败: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ext := range filterPGExtensions(names) {
		stmt := "CREATE EXTENSION IF NOT EXISTS " + quoteIdent("postgres", ext)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return extensionInstallError(ext, stmt, err)
		}
	}
	return nil
}

func pgSourceRelationError(found bool, relkind, schema, table string) error {
	if !found {
		return fmt.Errorf("源表 %s.%s 不存在", schema, table)
	}
	switch relkind {
	case "r", "p":
		return nil
	default:
		return fmt.Errorf("源对象 %s.%s 不是表（relkind=%s）", schema, table, relkind)
	}
}

func planPGTable(ctx context.Context, src *sql.DB, ss, st, ts, tt string, pairs []pgTablePair) (*pgMigPlan, error) {
	var relkind string
	err := src.QueryRowContext(ctx, `
SELECT c.relkind::text
FROM pg_class c
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2`, ss, st).Scan(&relkind)
	if err == sql.ErrNoRows {
		return nil, pgSourceRelationError(false, "", ss, st)
	}
	if err != nil {
		return nil, err
	}
	if err := pgSourceRelationError(true, relkind, ss, st); err != nil {
		return nil, err
	}
	info, err := fetchPGMigTable(ctx, src, ss, st, ts)
	if err != nil {
		return nil, err
	}
	if relkind == "p" {
		var part sql.NullString
		_ = src.QueryRowContext(ctx, `
SELECT pg_get_partkeydef(c.oid)
FROM pg_class c
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2`, ss, st).Scan(&part)
		info.PartitionBy = strings.TrimSpace(part.String)
	}
	return buildPGMigrationPlan(ss, st, ts, tt, info, pairs)
}

func fetchPGMigTable(ctx context.Context, src *sql.DB, schema, table, tgtSchema string) (*pgMigTable, error) {
	cols, err := fetchMigColumns(ctx, src, schema, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("源表 %s.%s 无列", schema, table)
	}
	cons, err := fetchPGConstraintsInfo(ctx, src, schema, table)
	if err != nil {
		return nil, err
	}
	info := &pgMigTable{Columns: cols}
	for _, c := range cons {
		if c.Type == "f" {
			info.ForeignKeys = append(info.ForeignKeys, c)
		} else {
			info.Constraints = append(info.Constraints, c)
		}
	}
	idxs, err := fetchPGIndexesInfo(ctx, src, schema, table)
	if err != nil {
		return nil, err
	}
	info.Indexes = idxs
	base, err := fetchPGTableSchemaInfo(ctx, src, schema, table)
	if err != nil {
		return nil, err
	}
	info.TableComment = base.TableComment
	info.RowSecurity = base.RowSecurity
	info.ForceRLS = base.ForceRowSecurity
	info.Policies = base.Policies
	for _, t := range base.Triggers {
		fn, _, ferr := fetchTriggerFunctionSQL(ctx, src, schema, tgtSchema, t.Def)
		if ferr != nil {
			return nil, fmt.Errorf("读取触发器 %s 的函数失败: %w", t.Name, ferr)
		}
		info.Triggers = append(info.Triggers, migTrigger{Name: t.Name, Def: t.Def, Enabled: t.Enabled, FunctionSQL: fn})
	}
	seqs, syncs, err := fetchExternalSequences(ctx, src, schema, table, info.Columns)
	if err != nil {
		return nil, err
	}
	info.Sequences = seqs
	info.SeqSyncs = syncs
	return info, nil
}

func fetchMigColumns(ctx context.Context, db *sql.DB, schema, table string) ([]migCol, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
	a.attname,
	pg_catalog.format_type(a.atttypid, a.atttypmod),
	a.attnotnull,
	COALESCE(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), ''),
	COALESCE(a.attidentity::text, ''),
	COALESCE(a.attgenerated::text, ''),
	COALESCE(pg_catalog.pg_get_serial_sequence(quote_ident(n.nspname) || '.' || quote_ident(cl.relname), a.attname), ''),
	COALESCE(pg_catalog.col_description(a.attrelid, a.attnum), '')
FROM pg_attribute a
JOIN pg_class cl ON a.attrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE n.nspname = $1 AND cl.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []migCol
	for rows.Next() {
		var c migCol
		if err := rows.Scan(&c.Name, &c.DataType, &c.NotNull, &c.Default, &c.Identity, &c.Generated, &c.SerialSequence, &c.Comment); err != nil {
			return nil, err
		}
		c.Identity = strings.TrimSpace(c.Identity)
		c.Generated = strings.TrimSpace(c.Generated)
		c.SerialSequence = strings.TrimSpace(c.SerialSequence)
		c.SerialKind = detectSerialKind(c.DataType, c.Default, c.SerialSequence)
		if c.Identity != "" {
			c.SerialKind = ""
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out = normalizeGeneratedDefaults(out)
	out = annotateExternalSequences(out, schema)
	for _, c := range out {
		if isNextvalDefault(c.Default) && c.SerialKind == "" && c.ExternalSeqName == "" && c.Identity == "" && c.Generated == "" {
			seq := sequenceNameFromNextval(c.Default)
			if seq == "" {
				seq = c.Default
			}
			return nil, fmt.Errorf("列 %s 使用外部序列 %s 作为默认值，当前不支持自动迁移外部序列", c.Name, seq)
		}
	}
	return out, nil
}

func fetchExternalSequences(ctx context.Context, db *sql.DB, schema, table string, cols []migCol) ([]migSeq, []pgSeqSync, error) {
	seen := map[string]struct{}{}
	var seqs []migSeq
	var syncs []pgSeqSync
	for _, c := range cols {
		if c.SerialKind != "" || c.Identity != "" {
			sync := pgSeqSync{Schema: schema, Table: table, Column: c.Name}
			if c.SerialSequence != "" {
				sch, name := parseSerialSequenceRef(c.SerialSequence, schema)
				seq, err := fetchPGSequenceDef(ctx, db, sch, name)
				if err != nil {
					if !strings.Contains(err.Error(), "源库未找到序列") {
						return nil, nil, err
					}
				} else {
					sync.LastValue = seq.LastValue
					sync.HasLast = seq.HasLast
					sync.Fallback = sch + "." + name
				}
			}
			syncs = append(syncs, sync)
			continue
		}
		if c.ExternalSeqName == "" {
			continue
		}
		key := c.ExternalSeqSchema + "\x00" + c.ExternalSeqName
		sync := pgSeqSync{Schema: schema, Table: table, Column: c.Name, Fallback: c.ExternalSeqSchema + "." + c.ExternalSeqName}
		if _, ok := seen[key]; ok {
			syncs = append(syncs, sync)
			continue
		}
		seen[key] = struct{}{}
		seq, err := fetchPGSequenceDef(ctx, db, c.ExternalSeqSchema, c.ExternalSeqName)
		if err != nil {
			return nil, nil, err
		}
		owners, err := pgSequenceOwnerCount(ctx, db, c.ExternalSeqSchema, c.ExternalSeqName)
		if err != nil {
			return nil, nil, err
		}
		seq.OwnedByColumn = c.Name
		seq.SetOwnedBy = owners <= 1
		seq.Schema = c.ExternalSeqSchema
		seqs = append(seqs, *seq)
		sync.LastValue = seq.LastValue
		sync.HasLast = seq.HasLast
		syncs = append(syncs, sync)
	}
	return seqs, syncs, nil
}

func fetchPGSequenceDef(ctx context.Context, db *sql.DB, schema, name string) (*migSeq, error) {
	if schema == "" {
		schema = "public"
	}
	const q = `
SELECT
	CASE t.typname
		WHEN 'int2' THEN 'smallint'
		WHEN 'int4' THEN 'integer'
		WHEN 'int8' THEN 'bigint'
		ELSE t.typname
	END,
	s.seqstart::text, s.seqmin::text, s.seqmax::text, s.seqincrement::text,
	CASE WHEN s.seqcycle THEN 'YES' ELSE 'NO' END,
	s.seqcache::text,
	COALESCE((
		SELECT p.last_value::text FROM pg_catalog.pg_sequences p
		WHERE p.schemaname = n.nspname AND p.sequencename = c.relname
	), s.seqstart::text)
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_sequence s ON s.seqrelid = c.oid
JOIN pg_catalog.pg_type t ON t.oid = s.seqtypid
WHERE c.relkind = 'S' AND n.nspname = $1 AND c.relname = $2`
	var dataType, start, minV, maxV, inc, cycleOpt, cache, lastVal string
	err := db.QueryRowContext(ctx, q, schema, name).Scan(&dataType, &start, &minV, &maxV, &inc, &cycleOpt, &cache, &lastVal)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("源库未找到序列 %s.%s", schema, name)
	}
	if err != nil {
		return nil, err
	}
	parse := func(v string, fallback int64) int64 {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return fallback
		}
		return n
	}
	return &migSeq{
		Schema: schema, Name: name, DataType: dataType,
		StartValue: parse(start, 1), MinValue: parse(minV, 1), MaxValue: parse(maxV, 9223372036854775807),
		Increment: parse(inc, 1), Cycle: strings.EqualFold(cycleOpt, "YES"), Cache: parse(cache, 1),
		LastValue: parse(lastVal, 1), HasLast: true,
	}, nil
}

func pgSequenceOwnerCount(ctx context.Context, db *sql.DB, schema, name string) (int, error) {
	const q = `
SELECT COUNT(*)::int
FROM pg_depend d
JOIN pg_class c ON d.objid = c.oid
JOIN pg_namespace n ON c.relnamespace = n.oid
WHERE c.relkind = 'S' AND n.nspname = $1 AND c.relname = $2 AND d.deptype = 'a'`
	var n int
	err := db.QueryRowContext(ctx, q, schema, name).Scan(&n)
	return n, err
}

func buildPGMigrationPlan(ss, st, ts, tt string, info *pgMigTable, pairs []pgTablePair) (*pgMigPlan, error) {
	if info == nil {
		return nil, fmt.Errorf("源表 %s.%s 无列", ss, st)
	}
	cols := normalizeGeneratedDefaults(append([]migCol(nil), info.Columns...))
	cols = annotateExternalSequences(cols, ss)
	for _, c := range cols {
		if isNextvalDefault(c.Default) && c.SerialKind == "" && c.ExternalSeqName == "" && c.Identity == "" && c.Generated == "" {
			seq := sequenceNameFromNextval(c.Default)
			if seq == "" {
				seq = c.Default
			}
			return nil, fmt.Errorf("列 %s 使用外部序列 %s 作为默认值，当前不支持自动迁移外部序列", c.Name, seq)
		}
	}
	ref := tableRef("postgres", ts, tt)
	var colParts []string
	for _, c := range cols {
		colParts = append(colParts, buildMigColumnDef(c, ss, ts))
	}
	for _, c := range info.Constraints {
		if c.Type == "f" || strings.TrimSpace(c.Def) == "" {
			continue
		}
		def := rewritePGObjectRefs(c.Def, pairs)
		colParts = append(colParts, fmt.Sprintf("CONSTRAINT %s %s", quoteIdent("postgres", c.Name), def))
	}
	if len(colParts) == 0 {
		return nil, fmt.Errorf("源表 %s.%s 无列", ss, st)
	}
	create := fmt.Sprintf("CREATE TABLE %s (%s)", ref, strings.Join(colParts, ", "))
	if strings.TrimSpace(info.PartitionBy) != "" {
		create += " PARTITION BY " + info.PartitionBy
	}
	plan := &pgMigPlan{}
	plan.Body = append(plan.Body, "CREATE SCHEMA IF NOT EXISTS "+quoteIdent("postgres", ts))
	seenSeq := map[string]struct{}{}
	for _, seq := range info.Sequences {
		key := strings.ToLower(seq.Name)
		if _, ok := seenSeq[key]; ok {
			continue
		}
		seenSeq[key] = struct{}{}
		plan.Body = append(plan.Body, buildCreateSequenceSQL(seq, ts))
	}
	plan.Body = append(plan.Body, create)
	for _, seq := range info.Sequences {
		if sqlText := buildOwnedBySQL(seq, ts, tt); sqlText != "" {
			plan.Body = append(plan.Body, sqlText)
		}
	}
	for _, idx := range info.Indexes {
		rewritten := rewritePGIndexDef(idx.Def, ss, st, ts, tt)
		if rewritten == "" {
			rewritten = rewriteIndexOnOnly(idx.Def, ss, st, ts, tt)
		}
		if rewritten == "" {
			return nil, fmt.Errorf("无法改写索引 %s，拒绝静默跳过\nSQL: %s", idx.Name, idx.Def)
		}
		plan.Body = append(plan.Body, rewritten)
	}
	if strings.TrimSpace(info.TableComment) != "" {
		plan.Body = append(plan.Body, fmt.Sprintf("COMMENT ON TABLE %s IS %s", ref, quotePGString(info.TableComment)))
	}
	for _, c := range cols {
		if strings.TrimSpace(c.Comment) == "" {
			continue
		}
		plan.Body = append(plan.Body, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", ref, quoteIdent("postgres", c.Name), quotePGString(c.Comment)))
	}
	if info.RowSecurity {
		plan.Body = append(plan.Body, fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", ref))
	}
	if info.ForceRLS {
		plan.Body = append(plan.Body, fmt.Sprintf("ALTER TABLE %s FORCE ROW LEVEL SECURITY", ref))
	}
	for _, p := range info.Policies {
		plan.Body = append(plan.Body, strings.TrimSpace(buildPGCreatePolicySQL(ref, p)))
	}
	for _, fk := range info.ForeignKeys {
		def := strings.TrimSpace(rewritePGObjectRefs(fk.Def, pairs))
		if def == "" {
			continue
		}
		plan.FKs = append(plan.FKs, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s", ref, quoteIdent("postgres", fk.Name), def))
	}
	emitted := map[string]struct{}{}
	for _, t := range info.Triggers {
		rewritten, err := rewriteTriggerDef(t.Def, ss, st, ts, tt)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(t.FunctionSQL) != "" {
			key := strings.ToLower(strings.TrimSpace(t.FunctionSQL))
			if _, ok := emitted[key]; !ok {
				plan.Triggers = append(plan.Triggers, strings.TrimSpace(t.FunctionSQL))
				emitted[key] = struct{}{}
			}
		}
		plan.Triggers = append(plan.Triggers, rewritten)
		if t.Enabled == "D" {
			plan.Triggers = append(plan.Triggers, fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER %s", ref, quoteIdent("postgres", t.Name)))
		}
	}
	if len(info.SeqSyncs) > 0 {
		for _, s := range info.SeqSyncs {
			s.Schema = ts
			s.Table = tt
			plan.SeqSyncs = append(plan.SeqSyncs, s)
		}
	}
	return plan, nil
}

func buildMigColumnDef(c migCol, ss, ts string) string {
	colType := c.DataType
	if c.SerialKind != "" && c.Identity == "" && c.Generated == "" {
		colType = strings.ToUpper(c.SerialKind)
	}
	parts := []string{quoteIdent("postgres", c.Name) + " " + colType}
	switch {
	case c.Generated == "s":
		parts = append(parts, fmt.Sprintf("GENERATED ALWAYS AS (%s) STORED", c.Default))
	case c.Generated == "v":
		parts = append(parts, fmt.Sprintf("GENERATED ALWAYS AS (%s) VIRTUAL", c.Default))
	case c.Identity == "a":
		parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
	case c.Identity == "d":
		parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
	default:
		if c.SerialKind == "" && c.Default != "" {
			parts = append(parts, "DEFAULT "+rewriteNextvalSchema(c.Default, ss, ts))
		}
	}
	if c.NotNull && c.Generated == "" {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

func buildCreateSequenceSQL(seq migSeq, tgtSchema string) string {
	if tgtSchema == "" {
		tgtSchema = seq.Schema
	}
	cycle := "NO CYCLE"
	if seq.Cycle {
		cycle = "CYCLE"
	}
	cache := seq.Cache
	if cache <= 0 {
		cache = 1
	}
	return fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s AS %s START WITH %d INCREMENT BY %d MINVALUE %d MAXVALUE %d CACHE %d %s",
		tableRef("postgres", tgtSchema, seq.Name), sequenceTypeAS(seq.DataType),
		seq.StartValue, seq.Increment, seq.MinValue, seq.MaxValue, cache, cycle)
}

func buildOwnedBySQL(seq migSeq, tgtSchema, tgtTable string) string {
	if !seq.SetOwnedBy || seq.OwnedByColumn == "" {
		return ""
	}
	return fmt.Sprintf("ALTER SEQUENCE %s OWNED BY %s.%s",
		tableRef("postgres", tgtSchema, seq.Name),
		tableRef("postgres", tgtSchema, tgtTable),
		quoteIdent("postgres", seq.OwnedByColumn))
}

func sequenceTypeAS(dataType string) string {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "smallint", "int2":
		return "smallint"
	case "integer", "int", "int4":
		return "integer"
	default:
		return "bigint"
	}
}

func detectSerialKind(dataType, columnDefault, serialSequence string) string {
	if strings.TrimSpace(serialSequence) == "" || !isNextvalDefault(columnDefault) {
		return ""
	}
	if sequenceNameFromNextval(columnDefault) == "" || !serialNamesMatch(columnDefault, serialSequence) {
		return ""
	}
	switch strings.ToLower(baseType(dataType)) {
	case "smallint", "int2":
		return "smallserial"
	case "integer", "int", "int4":
		return "serial"
	case "bigint", "int8":
		return "bigserial"
	default:
		return ""
	}
}

func baseType(dataType string) string {
	s := strings.ToLower(strings.TrimSpace(dataType))
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func isNextvalDefault(def string) bool {
	return strings.Contains(strings.ToLower(def), "nextval(")
}

var reNextvalLoose = regexp.MustCompile(`(?i)nextval\s*\(\s*'([^']+)'`)

func sequenceNameFromNextval(def string) string {
	m := reNextvalLoose.FindStringSubmatch(def)
	if len(m) < 2 {
		return ""
	}
	ref := strings.ReplaceAll(strings.Trim(m[1], `"`), `"`, "")
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(ref))
}

func serialNamesMatch(columnDefault, serialSequence string) bool {
	return sequenceNameFromNextval(columnDefault) == sequenceNameFromNextval("nextval('"+serialSequence+"')") ||
		sequenceNameFromNextval(columnDefault) == normalizeSeqRef(serialSequence)
}

func normalizeSeqRef(ref string) string {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), `"`, "")
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[i+1:]
	}
	return strings.ToLower(ref)
}

func parseSerialSequenceRef(ref, defaultSchema string) (schema, name string) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), `"`, "")
	if ref == "" {
		return "", ""
	}
	if i := strings.LastIndex(ref, "."); i >= 0 {
		return strings.TrimSpace(ref[:i]), strings.TrimSpace(ref[i+1:])
	}
	return defaultSchema, ref
}

func annotateExternalSequences(cols []migCol, tableSchema string) []migCol {
	out := append([]migCol(nil), cols...)
	for i := range out {
		if out[i].SerialKind != "" || out[i].Identity != "" || !isNextvalDefault(out[i].Default) {
			continue
		}
		m := reNextvalLoose.FindStringSubmatch(out[i].Default)
		if len(m) < 2 {
			continue
		}
		raw := strings.Trim(m[1], `"`)
		raw = strings.ReplaceAll(raw, `"`, "")
		sch, name := "", raw
		if i := strings.LastIndex(raw, "."); i >= 0 {
			sch = strings.TrimSpace(raw[:i])
			name = strings.TrimSpace(raw[i+1:])
		}
		if name == "" {
			continue
		}
		if sch == "" {
			sch = tableSchema
		}
		out[i].ExternalSeqSchema = sch
		out[i].ExternalSeqName = name
	}
	return out
}

func normalizeGeneratedDefaults(cols []migCol) []migCol {
	out := append([]migCol(nil), cols...)
	for i := range out {
		if out[i].Generated != "" || out[i].Identity != "" || out[i].SerialKind != "" || out[i].Default == "" {
			continue
		}
		if defaultReferencesOtherColumn(out[i].Default, out[i].Name, cols) {
			out[i].Generated = "s"
		}
	}
	return out
}

func defaultReferencesOtherColumn(expr, self string, cols []migCol) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	lower := strings.ToLower(expr)
	for _, c := range cols {
		if strings.EqualFold(c.Name, self) {
			continue
		}
		name := strings.ToLower(c.Name)
		if strings.Contains(lower, `"`+name+`"`) {
			return true
		}
		idx := 0
		for {
			pos := strings.Index(lower[idx:], name)
			if pos < 0 {
				break
			}
			pos += idx
			beforeOK := pos == 0 || !pgIdentChar(lower[pos-1])
			after := pos + len(name)
			afterOK := after >= len(lower) || !pgIdentChar(lower[after])
			if beforeOK && afterOK {
				return true
			}
			idx = pos + len(name)
		}
	}
	return false
}

func pgIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

func rewriteIndexOnOnly(def, ss, st, ts, tt string) string {
	src := []string{
		`ON ONLY "` + ss + `"."` + st + `"`,
		`ON ONLY ` + ss + `."` + st + `"`,
		`ON ONLY "` + ss + `".` + st,
		`ON ONLY ` + ss + `.` + st,
	}
	tgt := `ON ONLY ` + tableRef("postgres", ts, tt)
	for _, p := range src {
		if strings.Contains(def, p) {
			return strings.Replace(def, p, tgt, 1)
		}
	}
	return ""
}

var reTriggerOnFor = regexp.MustCompile(`(?i)(\sON\s+)(?:"[^"]+"\s*\.\s*)?(?:"[^"]+"|[A-Za-z_][\w$]*)(\s+FOR\b)`)

func rewriteTriggerDef(def, ss, st, ts, tt string) (string, error) {
	onDelete := strings.Count(strings.ToUpper(def), "ON DELETE")
	out := rewritePGTriggerDef(def, ss, st, ts, tt)
	qualified := tableRef("postgres", ts, tt)
	if !strings.Contains(out, "ON "+qualified) && !strings.Contains(strings.ToLower(out), " on "+strings.ToLower(ts)+"."+strings.ToLower(tt)) {
		out = reTriggerOnFor.ReplaceAllString(out, "${1}"+qualified+"${2}")
	}
	if strings.Count(strings.ToUpper(out), "ON DELETE") != onDelete {
		return "", fmt.Errorf("触发器改写误伤 ON DELETE: %s", def)
	}
	if !strings.Contains(out, qualified) && (ss != ts || st != tt) {
		return "", fmt.Errorf("无法改写触发器定义: %s", def)
	}
	return out, nil
}

func rewritePGObjectRefs(sqlText string, pairs []pgTablePair) string {
	out := sqlText
	for _, p := range pairs {
		repls := [][2]string{
			{`"` + p.SrcSchema + `"."` + p.SrcTable + `"`, tableRef("postgres", p.TgtSchema, p.TgtTable)},
			{p.SrcSchema + `."` + p.SrcTable + `"`, tableRef("postgres", p.TgtSchema, p.TgtTable)},
			{`"` + p.SrcSchema + `".` + p.SrcTable, tableRef("postgres", p.TgtSchema, p.TgtTable)},
			{p.SrcSchema + "." + p.SrcTable, tableRef("postgres", p.TgtSchema, p.TgtTable)},
		}
		for _, r := range repls {
			if r[0] != "" && strings.Contains(out, r[0]) {
				out = strings.ReplaceAll(out, r[0], r[1])
			}
		}
	}
	return out
}

func filterDeferredByTables(stmts []deferredStmt, allowed map[string]struct{}) (kept []deferredStmt, skipped []string) {
	for _, s := range stmts {
		key := strings.ToLower(s.Table)
		if key == "" {
			kept = append(kept, s)
			continue
		}
		if _, ok := allowed[key]; ok {
			kept = append(kept, s)
			continue
		}
		skipped = append(skipped, s.Table)
	}
	return kept, skipped
}

type deferredStmt struct {
	DB    *sql.DB
	SQL   string
	Table string
}

func syncPGSequencesFromSource(ctx context.Context, tgt *sql.DB, syncs []pgSeqSync) error {
	for _, s := range syncs {
		if !s.HasLast || s.Column == "" {
			continue
		}
		seq, err := lookupTargetSequence(ctx, tgt, s)
		if err != nil {
			return err
		}
		if seq == "" {
			continue
		}
		stmt := fmt.Sprintf("SELECT setval(%s::regclass, %d, true)", quoteLiteral(seq), s.LastValue)
		if err := execSQL(ctx, tgt, "同步序列源端当前值", stmt); err != nil {
			return err
		}
	}
	return nil
}

func syncPGSequencesAfterData(ctx context.Context, tgt *sql.DB, schema, table string, cols []string) error {
	ref := tableRef("postgres", schema, table)
	for _, col := range cols {
		var seq sql.NullString
		q := fmt.Sprintf("SELECT pg_get_serial_sequence(%s, %s)", quoteLiteral(tableRef("postgres", schema, table)), quoteLiteral(col))
		if err := tgt.QueryRowContext(ctx, q).Scan(&seq); err != nil {
			return fmt.Errorf("查询序列 pg_get_serial_sequence(%s.%s, %s) 失败: %w", schema, table, col, err)
		}
		name := ""
		if seq.Valid {
			name = strings.TrimSpace(seq.String)
		}
		if name == "" {
			continue
		}
		stmt := fmt.Sprintf("SELECT setval(%s::regclass, COALESCE((SELECT MAX(%s) FROM %s), 1), true)",
			quoteLiteral(name), quoteIdent("postgres", col), ref)
		if err := execSQL(ctx, tgt, "同步序列当前值", stmt); err != nil {
			return fmt.Errorf("同步序列当前值失败 (table=%s.%s col=%s): %w\nSQL: %s", schema, table, col, err, stmt)
		}
	}
	return nil
}

func lookupTargetSequence(ctx context.Context, tgt *sql.DB, s pgSeqSync) (string, error) {
	var seq sql.NullString
	q := fmt.Sprintf("SELECT pg_get_serial_sequence(%s, %s)", quoteLiteral(tableRef("postgres", s.Schema, s.Table)), quoteLiteral(s.Column))
	if err := tgt.QueryRowContext(ctx, q).Scan(&seq); err != nil {
		return "", fmt.Errorf("查询序列 pg_get_serial_sequence(%s.%s, %s) 失败: %w", s.Schema, s.Table, s.Column, err)
	}
	if seq.Valid && strings.TrimSpace(seq.String) != "" {
		return strings.TrimSpace(seq.String), nil
	}
	return strings.TrimSpace(s.Fallback), nil
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func listSequenceColumns(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT a.attname
FROM pg_attribute a
JOIN pg_class cl ON a.attrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE n.nspname = $1 AND cl.relname = $2
	AND a.attnum > 0 AND NOT a.attisdropped
	AND (
		a.attidentity IN ('a','d')
		OR pg_catalog.pg_get_serial_sequence(quote_ident(n.nspname) || '.' || quote_ident(cl.relname), a.attname) IS NOT NULL
		OR COALESCE(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), '') ILIKE '%nextval(%'
	)
ORDER BY a.attnum`, schema, table)
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

func pgInsertSQL(ref, colSQL, values string, overriding bool) string {
	over := ""
	if overriding {
		over = " OVERRIDING SYSTEM VALUE"
	}
	return fmt.Sprintf("INSERT INTO %s (%s)%s VALUES %s", ref, colSQL, over, values)
}

func pgIdentityAlways(ctx context.Context, db *sql.DB, schema, table string) (bool, error) {
	if schema == "" {
		schema = "public"
	}
	var one int
	err := db.QueryRowContext(ctx, `
SELECT 1
FROM pg_attribute a
JOIN pg_class cl ON a.attrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
WHERE n.nspname = $1 AND cl.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped AND a.attidentity = 'a'
LIMIT 1`, schema, table).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

var reMySQLFK = regexp.MustCompile("(?i),?\\s*((?:CONSTRAINT\\s+`[^`]+`\\s+)?)FOREIGN\\s+KEY\\s*\\([^)]*\\)\\s*REFERENCES\\s+(?:`[^`]+`\\.)?`[^`]+`\\s*\\([^)]*\\)(?:\\s+ON\\s+DELETE\\s+(?:RESTRICT|CASCADE|SET NULL|NO ACTION|SET DEFAULT))?(?:\\s+ON\\s+UPDATE\\s+(?:RESTRICT|CASCADE|SET NULL|NO ACTION|SET DEFAULT))?")

func splitMySQLForeignKeys(ddl, targetTable string, pairs []pgTablePair) (body string, fks []string) {
	body = ddl
	matches := reMySQLFK.FindAllStringSubmatchIndex(ddl, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(ddl), nil
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(ddl[last:m[0]])
		frag := strings.TrimSpace(ddl[m[0]:m[1]])
		frag = strings.TrimPrefix(frag, ",")
		frag = strings.TrimSpace(frag)
		frag = rewriteMySQLTableRefs(frag, pairs)
		fks = append(fks, fmt.Sprintf("ALTER TABLE %s ADD %s", quoteIdent("mysql", targetTable), frag))
		last = m[1]
	}
	b.WriteString(ddl[last:])
	body = b.String()
	body = regexp.MustCompile(`,\s*\)`).ReplaceAllString(body, ")")
	body = regexp.MustCompile(`\(\s*,`).ReplaceAllString(body, "(")
	return strings.TrimSpace(body), fks
}

func rewriteMySQLTableRefs(sqlText string, pairs []pgTablePair) string {
	out := sqlText
	for _, p := range pairs {
		out = strings.ReplaceAll(out, "`"+p.SrcTable+"`", "`"+p.TgtTable+"`")
	}
	return out
}

func mysqlTriggerDDLs(ctx context.Context, src *sql.DB, sourceTable, targetTable string) ([]string, error) {
	rows, err := src.QueryContext(ctx, `
SELECT TRIGGER_NAME, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
FROM information_schema.TRIGGERS
WHERE TRIGGER_SCHEMA = DATABASE() AND EVENT_OBJECT_TABLE = ?
ORDER BY TRIGGER_NAME, EVENT_MANIPULATION`, sourceTable)
	if err != nil {
		return nil, fmt.Errorf("读取 MySQL 触发器失败: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, timing, event, stmt string
		if err := rows.Scan(&name, &timing, &event, &stmt); err != nil {
			return nil, err
		}
		stmt = strings.TrimSpace(stmt)
		ddl := fmt.Sprintf("CREATE TRIGGER %s %s %s ON %s FOR EACH ROW %s",
			quoteIdent("mysql", name), timing, event, quoteIdent("mysql", targetTable), stmt)
		out = append(out, ddl)
	}
	return out, rows.Err()
}

func syncMySQLAutoIncrement(ctx context.Context, tgt *sql.DB, table string) error {
	rows, err := tgt.QueryContext(ctx, `
SELECT COLUMN_NAME FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND EXTRA LIKE '%auto_increment%'`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ref := quoteIdent("mysql", table)
	for _, col := range cols {
		var next int64
		q := fmt.Sprintf("SELECT COALESCE(MAX(%s),0)+1 FROM %s", quoteIdent("mysql", col), ref)
		if err := tgt.QueryRowContext(ctx, q).Scan(&next); err != nil {
			return fmt.Errorf("计算 AUTO_INCREMENT 失败: %w", err)
		}
		if next < 1 {
			next = 1
		}
		stmt := fmt.Sprintf("ALTER TABLE %s AUTO_INCREMENT = %d", ref, next)
		if err := execSQL(ctx, tgt, "更新 AUTO_INCREMENT", stmt); err != nil {
			return err
		}
	}
	return nil
}

func mysqlAlterAutoIncrement(table string, next int64) string {
	if next < 1 {
		next = 1
	}
	return fmt.Sprintf("ALTER TABLE %s AUTO_INCREMENT = %d", quoteIdent("mysql", table), next)
}

const pgCheckFunctionBodiesSQL = `SET check_function_bodies = false`

func applyDeferredTriggers(ctx context.Context, db *sql.DB, stmts []string) error {
	if len(stmts) == 0 || db == nil {
		return nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, pgCheckFunctionBodiesSQL); err != nil {
		return fmt.Errorf("设置 check_function_bodies=false 失败: %w", err)
	}
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already exists") {
				continue
			}
			return fmt.Errorf("触发器迁移失败: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

func pgConstraintExists(ctx context.Context, db *sql.DB, schema, table, name string) (bool, error) {
	var one int
	err := db.QueryRowContext(ctx, `
SELECT 1 FROM pg_constraint c
JOIN pg_class cl ON c.conrelid = cl.oid
JOIN pg_namespace n ON cl.relnamespace = n.oid
WHERE n.nspname = $1 AND cl.relname = $2 AND c.conname = $3 LIMIT 1`, schema, table, name).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func applyPGTextSearchConfigs(ctx context.Context, src, tgt *sql.DB) error {
	rows, err := src.QueryContext(ctx, `
SELECT c.cfgname
FROM pg_ts_config c
JOIN pg_namespace n ON c.cfgnamespace = n.oid
WHERE n.nspname <> 'pg_catalog'
ORDER BY c.cfgname`)
	if err != nil {
		return fmt.Errorf("读取源库全文检索配置失败: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if err := applyOneTextSearchConfig(ctx, src, tgt, name); err != nil {
			return fmt.Errorf("同步 text search configuration %s 失败: %w\n请在目标实例安装依赖扩展（如 zhparser）后重试", name, err)
		}
	}
	return nil
}

type tsMapping struct {
	Tokens []string
	Dict   string
}

func applyOneTextSearchConfig(ctx context.Context, src, tgt *sql.DB, name string) error {
	var exists bool
	if err := tgt.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = $1)`, name).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var parserSchema, parserName string
	err := src.QueryRowContext(ctx, `
SELECT pns.nspname, p.prsname
FROM pg_ts_config c
JOIN pg_ts_parser p ON c.cfgparser = p.oid
JOIN pg_namespace pns ON p.prsnamespace = pns.oid
WHERE c.cfgname = $1`, name).Scan(&parserSchema, &parserName)
	if err != nil {
		return fmt.Errorf("读取 text search configuration %s 失败: %w", name, err)
	}
	createSQL := fmt.Sprintf("CREATE TEXT SEARCH CONFIGURATION %s (PARSER = %s)",
		quoteIdent("postgres", name), tableRef("postgres", parserSchema, parserName))
	if _, err := tgt.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("%w\nSQL: %s", err, createSQL)
	}
	maps, err := fetchTextSearchMappings(ctx, src, name, parserName)
	if err != nil {
		return err
	}
	if len(maps) == 0 && strings.EqualFold(parserName, "zhparser") {
		maps = []tsMapping{{Tokens: []string{"a", "e", "i", "l", "n", "v"}, Dict: "pg_catalog.simple"}}
	}
	for _, m := range maps {
		stmt := fmt.Sprintf("ALTER TEXT SEARCH CONFIGURATION %s ADD MAPPING FOR %s WITH %s",
			quoteIdent("postgres", name), strings.Join(m.Tokens, ", "), m.Dict)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

func fetchTextSearchMappings(ctx context.Context, src *sql.DB, cfgName, parserName string) ([]tsMapping, error) {
	aliasRows, err := src.QueryContext(ctx, `SELECT tokid, alias FROM ts_token_type($1)`, parserName)
	if err != nil {
		return nil, fmt.Errorf("解析 parser %s token 类型失败: %w", parserName, err)
	}
	aliases := map[int]string{}
	for aliasRows.Next() {
		var id int
		var alias string
		if err := aliasRows.Scan(&id, &alias); err != nil {
			aliasRows.Close()
			return nil, err
		}
		if alias != "" {
			aliases[id] = alias
		}
	}
	aliasRows.Close()
	rows, err := src.QueryContext(ctx, `
SELECT m.maptokentype, COALESCE(dns.nspname, 'pg_catalog'), COALESCE(d.dictname, 'simple')
FROM pg_ts_config c
JOIN pg_ts_config_map m ON c.oid = m.mapcfg
LEFT JOIN pg_ts_dict d ON m.mapdict = d.oid
LEFT JOIN pg_namespace dns ON d.dictnamespace = dns.oid
WHERE c.cfgname = $1
ORDER BY m.mapseqno`, cfgName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string]*tsMapping{}
	var order []string
	for rows.Next() {
		var tok int
		var sch, dict string
		if err := rows.Scan(&tok, &sch, &dict); err != nil {
			return nil, err
		}
		alias := aliases[tok]
		if alias == "" {
			continue
		}
		key := sch + "." + dict
		m, ok := grouped[key]
		if !ok {
			m = &tsMapping{Dict: quoteIdent("postgres", sch) + "." + quoteIdent("postgres", dict)}
			grouped[key] = m
			order = append(order, key)
		}
		m.Tokens = append(m.Tokens, alias)
	}
	var out []tsMapping
	for _, k := range order {
		sort.Strings(grouped[k].Tokens)
		out = append(out, *grouped[k])
	}
	return out, rows.Err()
}

func pgSetvalAfterDataSQL(schema, table, col, seqName string) string {
	ref := tableRef("postgres", schema, table)
	return fmt.Sprintf("SELECT setval(%s::regclass, COALESCE((SELECT MAX(%s) FROM %s), 1), true)",
		quoteLiteral(seqName), quoteIdent("postgres", col), ref)
}

func pgSetvalFromSourceSQL(seqName string, last int64) string {
	return fmt.Sprintf("SELECT setval(%s::regclass, %d, true)", quoteLiteral(seqName), last)
}
