package dbmig

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

func Precheck(ctx context.Context, st *store.DB, secretKey string, t *store.MigrationTask) []store.MigrationPrecheck {
	var items []store.MigrationPrecheck
	add := func(item, level, status, msg string) {
		items = append(items, store.MigrationPrecheck{Item: item, Level: level, Status: status, Message: msg})
	}
	engine := store.NormalizeDBType(t.DBType)
	if engine != "mysql" && engine != "postgres" && engine != "tidb" && engine != "oceanbase" {
		add("db_type", "required", "failed", "仅支持 MySQL / PostgreSQL（含 TiDB / OceanBase）")
		return items
	}
	if t.TransferMode == "oss" {
		if !st.ResolveMigrationOSSConfiguredInTenant(st.TenantOfInstance(t.SourceID), t.Env) {
			add("transfer_mode", "required", "failed", "请先在配置中心为该环境配置 OSS 连接信息")
		} else {
			add("transfer_mode", "required", "passed", "OSS 中转")
		}
	}
	src, err := st.GetInstance(t.SourceID)
	if err != nil || src == nil {
		add("source_credential", "required", "failed", "源实例未登记")
		return items
	}
	add("source_credential", "required", "passed", instHost(src)+fmt.Sprintf(":%d", src.Port))
	tgt, err := st.GetInstance(t.TargetID)
	if err != nil || tgt == nil {
		add("target_credential", "required", "failed", "目标实例未登记")
		return items
	}
	add("target_credential", "required", "passed", instHost(tgt)+fmt.Sprintf(":%d", tgt.Port))

	srcDB, err := OpenInstance(ctx, src, secretKey, firstNonEmpty(t.SourceDB, defaultDB(engine)))
	if err != nil {
		// 源库名尚未存在时，退回默认库做实例级连通
		srcDB, err = OpenInstance(ctx, src, secretKey, defaultDB(engine))
	}
	if err != nil {
		add("source_connectivity", "required", "failed", err.Error())
	} else {
		defer srcDB.Close()
		ver := probeVersion(ctx, srcDB, engine)
		add("source_connectivity", "required", "passed", ver)
		if err := probeSelect(ctx, srcDB, engine); err != nil {
			add("source_privileges", "required", "failed", err.Error())
		} else {
			add("source_privileges", "required", "passed", "SELECT 可用")
		}
	}
	tgtDB, err := OpenInstance(ctx, tgt, secretKey, firstNonEmpty(t.TargetDB, defaultDB(engine)))
	if err != nil {
		tgtDB, err = OpenInstance(ctx, tgt, secretKey, defaultDB(engine))
	}
	if err != nil {
		add("target_connectivity", "required", "failed", err.Error())
	} else {
		defer tgtDB.Close()
		ver := probeVersion(ctx, tgtDB, engine)
		add("target_connectivity", "required", "passed", ver)
		if err := probeWrite(ctx, tgtDB, engine); err != nil {
			add("target_privileges", "required", "failed", err.Error())
		} else {
			add("target_privileges", "required", "passed", "临时表读写可用")
		}
	}
	// 多库抽样：对已选库做 SELECT 1，避免只看首表
	sampleDBs := uniqueSampleDBs(t, 3)
	if src != nil && len(sampleDBs) > 0 {
		var failed []string
		for _, dbName := range sampleDBs {
			sdb, err := OpenInstance(ctx, src, secretKey, dbName)
			if err != nil {
				failed = append(failed, dbName+": "+err.Error())
				continue
			}
			if err := probeSelect(ctx, sdb, engine); err != nil {
				failed = append(failed, dbName+": "+err.Error())
			}
			sdb.Close()
		}
		if len(failed) > 0 {
			add("sample_databases", "required", "failed", strings.Join(failed, "; "))
		} else {
			add("sample_databases", "required", "passed", "抽样库 "+strings.Join(sampleDBs, ","))
		}
	}
	if srcDB != nil && tgtDB != nil {
		checked := 0
		for _, o := range t.Objects {
			if o.TargetTable == "" {
				continue
			}
			tdb := firstNonEmpty(o.TargetDB, t.TargetDB)
			checkDB := tgtDB
			var closer func()
			if tdb != "" && tdb != firstNonEmpty(t.TargetDB, defaultDB(engine)) {
				opened, err := OpenInstance(ctx, tgt, secretKey, tdb)
				if err != nil {
					add("object_conflict", "warning", "warning", tdb+": "+err.Error())
					continue
				}
				checkDB = opened
				closer = func() { opened.Close() }
			}
			exist, err := tableExists(ctx, checkDB, engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
			if closer != nil {
				closer()
			}
			checked++
			if err != nil {
				add("object_conflict", "warning", "warning", err.Error())
			} else if exist && t.ConflictStrategy == "abort" && t.SchemaStrategy == "init" {
				add("object_conflict", "required", "warning", "目标端已有同名表 "+tdb+"."+o.TargetTable)
			}
			if checked >= 3 {
				break
			}
		}
		if checked == 0 && t.SourceDB != "" {
			exist, err := tableExists(ctx, tgtDB, engine, firstSchema(engine, t), firstTableHint(t))
			if err != nil {
				add("object_conflict", "warning", "warning", err.Error())
			} else if exist && t.ConflictStrategy == "abort" && t.SchemaStrategy == "init" {
				add("object_conflict", "required", "warning", "目标端已有同名表，创建时若冲突策略为 abort 将失败")
			} else {
				add("object_conflict", "required", "passed", "冲突策略 "+t.ConflictStrategy)
			}
		} else if checked > 0 {
			add("object_conflict", "required", "passed", "冲突策略 "+t.ConflictStrategy+"（已抽样 "+strconv.Itoa(checked)+" 表）")
		}
	}

	tableObjs := tableObjectsForPrecheck(t)
	if len(tableObjs) > 0 && src != nil && tgt != nil {
		appendSchemaPrechecks(ctx, &items, engine, src, tgt, secretKey, t, tableObjs)
	}
	return items
}

func tableObjectsForPrecheck(t *store.MigrationTask) []store.MigrationObject {
	var out []store.MigrationObject
	for _, o := range t.Objects {
		if strings.TrimSpace(o.SourceTable) == "" {
			continue
		}
		out = append(out, o)
	}
	return out
}

func appendSchemaPrechecks(ctx context.Context, items *[]store.MigrationPrecheck, engine string, src, tgt *store.Instance, secretKey string, t *store.MigrationTask, objs []store.MigrationObject) {
	add := func(item, level, status, msg string) {
		*items = append(*items, store.MigrationPrecheck{Item: item, Level: level, Status: status, Message: msg})
	}
	addDetail := func(item, level, status, msg, detail string) {
		*items = append(*items, store.MigrationPrecheck{Item: item, Level: level, Status: status, Message: msg, Detail: detail})
	}

	if engine == "postgres" {
		if err := checkPGSourceTables(ctx, src, secretKey, objs); err != nil {
			add("source_tables", "required", "failed", err.Error())
		} else {
			add("source_tables", "required", "passed", "源表存在且可读取列信息")
		}
	}

	if err := checkSchemaCompatibility(ctx, engine, src, tgt, secretKey, t, objs); err != nil {
		add("schema_compatibility", "required", "failed", err.Error())
	} else {
		add("schema_compatibility", "required", "passed", "基础结构检查通过")
	}

	est := estimateSourceRows(ctx, engine, src, secretKey, objs)
	detail, _ := jsonMarshalCompact(est)
	addDetail("source_row_estimate", "info", "passed", "统计完成", detail)
}

func checkPGSourceTables(ctx context.Context, src *store.Instance, secretKey string, objs []store.MigrationObject) error {
	conns := map[string]*sql.DB{}
	defer func() {
		for _, db := range conns {
			_ = db.Close()
		}
	}()
	open := func(dbName string) (*sql.DB, error) {
		if db, ok := conns[dbName]; ok {
			return db, nil
		}
		db, err := OpenInstance(ctx, src, secretKey, dbName)
		if err != nil {
			return nil, err
		}
		conns[dbName] = db
		return db, nil
	}
	for _, o := range objs {
		dbName := firstNonEmpty(o.SourceDB)
		if dbName == "" {
			continue
		}
		db, err := open(dbName)
		if err != nil {
			return fmt.Errorf("源库 %s: %w", dbName, err)
		}
		ss := firstNonEmpty(o.SourceSchema, "public")
		cols, err := fetchPGColumnsInfo(ctx, db, ss, o.SourceTable)
		if err != nil {
			return fmt.Errorf("源表 %s.%s: %w", ss, o.SourceTable, err)
		}
		if len(cols) == 0 {
			return fmt.Errorf("源表 %s.%s 无可用列", ss, o.SourceTable)
		}
	}
	return nil
}

func checkSchemaCompatibility(ctx context.Context, engine string, src, tgt *store.Instance, secretKey string, t *store.MigrationTask, objs []store.MigrationObject) error {
	if strings.EqualFold(t.SchemaStrategy, "no_init") && strings.EqualFold(t.MigrationMode, "schema_only") {
		return nil
	}
	srcConns := map[string]*sql.DB{}
	tgtConns := map[string]*sql.DB{}
	defer func() {
		for _, db := range srcConns {
			_ = db.Close()
		}
		for _, db := range tgtConns {
			_ = db.Close()
		}
	}()
	openSrc := func(dbName string) (*sql.DB, error) {
		if db, ok := srcConns[dbName]; ok {
			return db, nil
		}
		db, err := OpenInstance(ctx, src, secretKey, dbName)
		if err != nil {
			return nil, err
		}
		srcConns[dbName] = db
		return db, nil
	}
	openTgt := func(dbName string) (*sql.DB, error) {
		if db, ok := tgtConns[dbName]; ok {
			return db, nil
		}
		db, err := OpenInstance(ctx, tgt, secretKey, dbName)
		if err != nil {
			return nil, err
		}
		tgtConns[dbName] = db
		return db, nil
	}

	for _, o := range objs {
		srcDBName := firstNonEmpty(o.SourceDB, t.SourceDB)
		tgtDBName := firstNonEmpty(o.TargetDB, t.TargetDB, o.SourceDB)
		tgtTable := firstNonEmpty(o.TargetTable, o.SourceTable)
		ss := firstNonEmpty(o.SourceSchema, "public")
		ts := firstNonEmpty(o.TargetSchema, o.SourceSchema, "public")
		if srcDBName == "" || tgtDBName == "" || o.SourceTable == "" {
			continue
		}
		tgtDB, err := openTgt(tgtDBName)
		if err != nil {
			return fmt.Errorf("目标库 %s: %w", tgtDBName, err)
		}
		exists, err := tableExists(ctx, tgtDB, engine, ts, tgtTable)
		if err != nil {
			return err
		}
		if !exists {
			// 结构初始化会建表，目标尚不存在时跳过比对
			if strings.EqualFold(t.SchemaStrategy, "init") || t.SchemaStrategy == "" {
				continue
			}
			continue
		}

		srcDB, err := openSrc(srcDBName)
		if err != nil {
			return fmt.Errorf("源库 %s: %w", srcDBName, err)
		}

		if engine == "postgres" {
			srcSch, err := fetchPGTableSchemaInfo(ctx, srcDB, ss, o.SourceTable)
			if err != nil {
				return fmt.Errorf("源表 %s: %w", o.SourceTable, err)
			}
			tgtSch, err := fetchPGTableSchemaInfo(ctx, tgtDB, ts, tgtTable)
			if err != nil {
				return fmt.Errorf("目标表 %s: %w", tgtTable, err)
			}
			if err := comparePGTableSchemaInfo(srcSch, tgtSch, o.SourceTable); err != nil {
				return err
			}
			continue
		}

		srcCols, err := listTableColumns(ctx, srcDB, engine, ss, o.SourceTable)
		if err != nil {
			return fmt.Errorf("源表 %s: %w", o.SourceTable, err)
		}
		tgtCols, err := listTableColumns(ctx, tgtDB, engine, ts, tgtTable)
		if err != nil {
			return fmt.Errorf("目标表 %s: %w", tgtTable, err)
		}
		if srcCols != tgtCols {
			return fmt.Errorf("表 %s 列不一致", o.SourceTable)
		}
	}
	return nil
}

func estimateSourceRows(ctx context.Context, engine string, src *store.Instance, secretKey string, objs []store.MigrationObject) map[string]int64 {
	out := map[string]int64{}
	conns := map[string]*sql.DB{}
	defer func() {
		for _, db := range conns {
			_ = db.Close()
		}
	}()
	for _, o := range objs {
		dbName := firstNonEmpty(o.SourceDB)
		if dbName == "" || o.SourceTable == "" {
			continue
		}
		db, ok := conns[dbName]
		if !ok {
			opened, err := OpenInstance(ctx, src, secretKey, dbName)
			if err != nil {
				continue
			}
			conns[dbName] = opened
			db = opened
		}
		ss := firstNonEmpty(o.SourceSchema, "public")
		key := dbName + "." + ss + "." + o.SourceTable
		var n int64
		if err := countTableRows(ctx, db, engine, ss, o.SourceTable, &n); err != nil {
			continue
		}
		out[key] = n
	}
	return out
}

func jsonMarshalCompact(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func uniqueSampleDBs(t *store.MigrationTask, n int) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, o := range t.Objects {
		add(o.SourceDB)
		if len(out) >= n {
			return out
		}
	}
	add(t.SourceDB)
	if len(out) > n {
		return out[:n]
	}
	return out
}

func PrecheckPassed(items []store.MigrationPrecheck) bool {
	for _, it := range items {
		if it.Level == "required" && it.Status == "failed" {
			return false
		}
	}
	return len(items) > 0
}

func probeVersion(ctx context.Context, db *sql.DB, engine string) string {
	var v string
	q := "SELECT VERSION()"
	if store.NormalizeDBType(engine) == "postgres" {
		q = "SHOW server_version"
	}
	_ = db.QueryRowContext(ctx, q).Scan(&v)
	if v == "" {
		return "已连接"
	}
	return v
}

func probeSelect(ctx context.Context, db *sql.DB, engine string) error {
	q := "SELECT 1"
	if store.NormalizeDBType(engine) == "postgres" {
		q = "SELECT 1"
	}
	var n int
	return db.QueryRowContext(ctx, q).Scan(&n)
}

func probeWrite(ctx context.Context, db *sql.DB, engine string) error {
	name := "_auto_dba_mig_probe"
	if store.NormalizeDBType(engine) == "postgres" {
		if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE `+quoteIdent(engine, name)+` (id int)`); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO `+quoteIdent(engine, name)+` VALUES (1)`); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx, `DROP TABLE `+quoteIdent(engine, name))
		return err
	}
	if _, err := db.ExecContext(ctx, "CREATE TEMPORARY TABLE `"+name+"` (id int)"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+name+"` VALUES (1)"); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "DROP TEMPORARY TABLE `"+name+"`")
	return err
}

func tableExists(ctx context.Context, db *sql.DB, engine, schema, table string) (bool, error) {
	if table == "" {
		return false, nil
	}
	var n int
	if store.NormalizeDBType(engine) == "postgres" {
		if schema == "" {
			schema = "public"
		}
		err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2`, schema, table).Scan(&n)
		return n > 0, err
	}
	err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`, table).Scan(&n)
	return n > 0, err
}

func firstNonEmpty(vals ...string) string {
	for _, a := range vals {
		if strings.TrimSpace(a) != "" {
			return a
		}
	}
	return ""
}

func firstSchema(engine string, t *store.MigrationTask) string {
	if len(t.Objects) > 0 {
		return t.Objects[0].SourceSchema
	}
	if store.NormalizeDBType(engine) == "postgres" {
		return "public"
	}
	return ""
}

func firstTableHint(t *store.MigrationTask) string {
	if len(t.Objects) > 0 {
		return t.Objects[0].TargetTable
	}
	return ""
}
