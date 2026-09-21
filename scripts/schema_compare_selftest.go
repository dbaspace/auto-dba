//go:build ignore

// 自测：
//   1) 目标不存在（left_only）
//   2) 目标存在但与源端不一致（differ）
// 用法：go run ./scripts/schema_compare_selftest.go [all|empty|differ]
// MySQL 见：scripts/schema_compare_selftest_mysql.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/dbmig"
)

const (
	srcSchema  = "cmp_selftest_src"
	tgtSchema  = "cmp_selftest_tgt"
	tableName  = "tbl_selftest_demo"
)

func main() {
	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	dsn := os.Getenv("SELFTEST_DSN")
	if dsn == "" {
		fmt.Println("跳过：未设置 SELFTEST_DSN")
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail(fmt.Errorf("连接失败: %w", err))
	}
	fmt.Println("== 已连接 Postgres ==")

	var failed bool
	run := func(name string, fn func(context.Context, *sql.DB) error) {
		fmt.Printf("\n######## 场景: %s ########\n", name)
		cleanup(ctx, db)
		if err := fn(ctx, db); err != nil {
			fmt.Println("== 自测失败 ==", err)
			failed = true
			return
		}
		fmt.Println("== 自测通过 ==")
	}

	switch mode {
	case "empty":
		run("目标不存在", scenarioEmptyTarget)
	case "differ":
		run("目标存在但不一致", scenarioDifferTarget)
	default:
		run("目标不存在", scenarioEmptyTarget)
		run("目标存在但不一致", scenarioDifferTarget)
	}
	cleanup(ctx, db)
	if failed {
		os.Exit(1)
	}
}

func scenarioEmptyTarget(ctx context.Context, db *sql.DB) error {
	if err := setupSource(ctx, db); err != nil {
		return err
	}
	if err := setupTargetEmpty(ctx, db); err != nil {
		return err
	}
	fmt.Println("== 源端齐全，目标仅空 schema ==")
	return runCompareAndFix(ctx, db, expect{
		tableStatus: "left_only",
		needSQL: []string{
			"CREATE TABLE", "CREATE SEQUENCE", "COMMENT ON",
			"CREATE OR REPLACE FUNCTION", "CREATE TRIGGER",
			"ENABLE ROW LEVEL SECURITY", "CREATE POLICY", "ADD CONSTRAINT",
		},
		needDiffKinds: nil, // left_only 无明细
	})
}

func scenarioDifferTarget(ctx context.Context, db *sql.DB) error {
	if err := setupSource(ctx, db); err != nil {
		return err
	}
	if err := setupTargetPartial(ctx, db); err != nil {
		return err
	}
	fmt.Println("== 源端齐全，目标有残缺表（缺列/默认值/约束/索引/注释/RLS/函数/触发器） ==")
	return runCompareAndFix(ctx, db, expect{
		tableStatus: "differ",
		needSQL: []string{
			"ADD COLUMN", "SET DEFAULT", "CREATE SEQUENCE",
			"ADD CONSTRAINT", "CREATE INDEX", "COMMENT ON",
			"ENABLE ROW LEVEL SECURITY", "CREATE POLICY",
			"CREATE OR REPLACE FUNCTION", "CREATE TRIGGER",
		},
		needDiffKinds: []string{"column", "constraint", "index", "comment", "rls", "policy", "trigger"},
	})
}

type expect struct {
	tableStatus   string
	needSQL       []string
	needDiffKinds []string
}

func runCompareAndFix(ctx context.Context, db *sql.DB, exp expect) error {
	items, err := dbmig.CompareSchemas(ctx, db, db, dbmig.SchemaCompareSpec{
		Engine: "postgres", LeftSchema: srcSchema, RightSchema: tgtSchema, Tables: []string{tableName},
	})
	if err != nil {
		return err
	}

	fmt.Println("== 对比结果 ==")
	var tableDiff, fnDiff *dbmig.TableSchemaDiff
	kindSeen := map[string]bool{}
	for i := range items {
		it := &items[i]
		fmt.Printf("  table=%s status=%s msg=%s diffs=%d\n", it.Table, it.Status, it.Message, len(it.Diffs))
		for _, d := range it.Diffs {
			fmt.Printf("    [%s] %s | %s\n", d.Kind, d.Name, d.Message)
			kindSeen[d.Kind] = true
		}
		if it.Table == tableName {
			tableDiff = it
		}
		if it.Table == "__schema_functions__" {
			fnDiff = it
		}
	}

	fixSQL, err := dbmig.BuildSchemaFixSQL(ctx, db, "postgres", srcSchema, tgtSchema, items)
	if err != nil {
		return err
	}
	fmt.Println("== 修复 SQL ==")
	fmt.Println(fixSQL)

	var errs []string
	if tableDiff == nil {
		errs = append(errs, "缺少表对比结果")
	} else if tableDiff.Status != exp.tableStatus {
		errs = append(errs, fmt.Sprintf("期望表 status=%s，实际=%s", exp.tableStatus, tableDiff.Status))
	}
	if fnDiff == nil || fnDiff.Status != "differ" {
		errs = append(errs, "期望检出 schema 函数差异")
	}
	for _, k := range exp.needDiffKinds {
		if !kindSeen[k] {
			// function 在虚拟表上
			if k == "function" {
				continue
			}
			errs = append(errs, "对比未检出 kind="+k)
		}
	}
	if fnDiff != nil {
		ok := false
		for _, d := range fnDiff.Diffs {
			if d.Kind == "function" && strings.Contains(d.Message, "仅源") {
				ok = true
			}
		}
		if !ok {
			errs = append(errs, "函数差异中未找到仅源端函数")
		}
	}
	for _, n := range exp.needSQL {
		if !strings.Contains(fixSQL, n) {
			errs = append(errs, "修复 SQL 缺少: "+n)
		}
	}

	if len(errs) == 0 {
		fmt.Println("== 在目标端执行修复 SQL ==")
		if _, err := db.ExecContext(ctx, stripSQLComments(fixSQL)); err != nil {
			errs = append(errs, fmt.Sprintf("执行失败: %v", err))
		}
	}
	if len(errs) == 0 {
		items2, err := dbmig.CompareSchemas(ctx, db, db, dbmig.SchemaCompareSpec{
			Engine: "postgres", LeftSchema: srcSchema, RightSchema: tgtSchema, Tables: []string{tableName},
		})
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			fmt.Println("== 修复后再次对比 ==")
			for _, it := range items2 {
				if it.Table == "__schema_functions__" {
					fmt.Printf("  table=%s status=%s msg=%s diffs=%d\n", it.Table, it.Status, it.Message, len(it.Diffs))
					continue
				}
				fmt.Printf("  table=%s status=%s msg=%s diffs=%d\n", it.Table, it.Status, it.Message, len(it.Diffs))
				for _, d := range it.Diffs {
					fmt.Printf("    [%s] %s | %s\n", d.Kind, d.Name, d.Message)
				}
				if it.Table == tableName && it.Status != "match" {
					errs = append(errs, fmt.Sprintf("修复后表仍不一致: %s (%d diffs)", it.Status, len(it.Diffs)))
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func setupSource(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE SCHEMA ` + srcSchema,
		`CREATE SEQUENCE ` + srcSchema + `.tbl_selftest_demo_id_seq`,
		`CREATE TABLE ` + srcSchema + `.tbl_selftest_demo (
			id bigint NOT NULL DEFAULT nextval('` + srcSchema + `.tbl_selftest_demo_id_seq'::regclass),
			tenant_id bigint NOT NULL,
			name varchar(64) NOT NULL,
			score numeric(10,2) DEFAULT 0,
			created_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT tbl_selftest_demo_pkey PRIMARY KEY (id),
			CONSTRAINT uq_selftest_tenant_name UNIQUE (tenant_id, name),
			CONSTRAINT chk_selftest_score CHECK (score >= 0)
		)`,
		`ALTER SEQUENCE ` + srcSchema + `.tbl_selftest_demo_id_seq OWNED BY ` + srcSchema + `.tbl_selftest_demo.id`,
		`CREATE INDEX idx_selftest_tenant ON ` + srcSchema + `.tbl_selftest_demo (tenant_id)`,
		`COMMENT ON TABLE ` + srcSchema + `.tbl_selftest_demo IS '自测演示表'`,
		`COMMENT ON COLUMN ` + srcSchema + `.tbl_selftest_demo.id IS '主键ID'`,
		`COMMENT ON COLUMN ` + srcSchema + `.tbl_selftest_demo.name IS '名称字段'`,
		`ALTER TABLE ` + srcSchema + `.tbl_selftest_demo ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + srcSchema + `.tbl_selftest_demo FORCE ROW LEVEL SECURITY`,
		`CREATE POLICY tenant_selftest_isolation ON ` + srcSchema + `.tbl_selftest_demo
			AS PERMISSIVE FOR ALL TO PUBLIC
			USING (tenant_id = COALESCE(current_setting('app.current_tenant_id', true), '0')::bigint)`,
		`CREATE OR REPLACE FUNCTION ` + srcSchema + `.fn_selftest_touch() RETURNS trigger
			LANGUAGE plpgsql AS $$
			BEGIN
				NEW.created_at := COALESCE(NEW.created_at, now());
				RETURN NEW;
			END;
			$$`,
		`CREATE TRIGGER trg_selftest_touch
			BEFORE INSERT OR UPDATE ON ` + srcSchema + `.tbl_selftest_demo
			FOR EACH ROW EXECUTE FUNCTION ` + srcSchema + `.fn_selftest_touch()`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("建源端失败: %w\nSQL: %s", err, truncate(s, 120))
		}
	}
	return nil
}

// setupTargetPartial：目标有同名表，但缺列/默认值/约束/索引/注释/RLS/触发器/函数。
func setupTargetPartial(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE SCHEMA ` + tgtSchema,
		`CREATE TABLE ` + tgtSchema + `.tbl_selftest_demo (
			id bigint NOT NULL,
			tenant_id bigint NOT NULL,
			name varchar(64) NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			CONSTRAINT tbl_selftest_demo_pkey PRIMARY KEY (id)
		)`,
		// 故意：无 score 列、无序列默认值、无 UNIQUE/CHECK、无索引、无注释、无 RLS、无函数/触发器
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("建目标残缺表失败: %w", err)
		}
	}
	return nil
}

func setupTargetEmpty(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE SCHEMA `+tgtSchema)
	return err
}

func cleanup(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+srcSchema+` CASCADE`)
	_, _ = db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+tgtSchema+` CASCADE`)
}

func stripSQLComments(script string) string {
	var b strings.Builder
	inDollar := false
	dollarTag := ""
	for _, line := range strings.Split(script, "\n") {
		if !inDollar {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "--") {
				continue
			}
		}
		i := 0
		for i < len(line) {
			if line[i] == '$' {
				j := i + 1
				for j < len(line) && ((line[j] >= 'a' && line[j] <= 'z') || (line[j] >= 'A' && line[j] <= 'Z') || (line[j] >= '0' && line[j] <= '9') || line[j] == '_') {
					j++
				}
				if j < len(line) && line[j] == '$' {
					tag := line[i : j+1]
					if !inDollar {
						inDollar = true
						dollarTag = tag
					} else if tag == dollarTag {
						inDollar = false
						dollarTag = ""
					}
					b.WriteString(tag)
					i = j + 1
					continue
				}
			}
			b.WriteByte(line[i])
			i++
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fail(err error) {
	fmt.Println("FATAL:", err)
	os.Exit(1)
}
