//go:build ignore

// MySQL 族表结构对比自测。
//
// 双库（推荐）：
//   SELFTEST_MYSQL_DSN='u:p@tcp(host:3306)/srcdb' \
//   SELFTEST_MYSQL_DSN_TGT='u:p@tcp(host:3306)/tgtdb' \
//   go run ./scripts/schema_compare_selftest_mysql.go
//
// 单库：仅验证源表存在时的 Fix SQL 生成（模拟 differ 项）
//   SELFTEST_MYSQL_DSN='...' go run ./scripts/schema_compare_selftest_mysql.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"auto-dba/internal/dbmig"
)

const tableName = "tbl_selftest_demo_mysql"

func main() {
	srcDSN := os.Getenv("SELFTEST_MYSQL_DSN")
	if srcDSN == "" {
		fmt.Println("跳过：未设置 SELFTEST_MYSQL_DSN")
		os.Exit(0)
	}
	tgtDSN := os.Getenv("SELFTEST_MYSQL_DSN_TGT")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	src, err := sql.Open("mysql", srcDSN)
	if err != nil {
		fail(err)
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		fail(err)
	}
	fmt.Println("== 已连接 MySQL 源库 ==")

	cleanup(ctx, src)
	if err := setupSource(ctx, src); err != nil {
		fail(err)
	}

	if tgtDSN == "" {
		fmt.Println("== 单库模式：模拟 differ 生成修复 SQL ==")
		if err := runSingleDBFixCheck(ctx, src); err != nil {
			fail(err)
		}
		cleanup(ctx, src)
		fmt.Println("== 自测通过（单库） ==")
		return
	}

	tgt, err := sql.Open("mysql", tgtDSN)
	if err != nil {
		fail(err)
	}
	defer tgt.Close()
	if err := tgt.PingContext(ctx); err != nil {
		fail(err)
	}
	fmt.Println("== 已连接 MySQL 目标库 ==")

	var failed bool
	// empty: 目标无表
	fmt.Println("\n######## 场景: 目标不存在 ########")
	cleanup(ctx, tgt)
	if err := runPair(ctx, src, tgt, "left_only", []string{"CREATE TABLE"}, nil); err != nil {
		fmt.Println("失败:", err)
		failed = true
	} else {
		fmt.Println("== 自测通过 ==")
	}

	// differ: 目标残缺
	fmt.Println("\n######## 场景: 目标存在但不一致 ########")
	cleanup(ctx, tgt)
	if err := setupTargetPartial(ctx, tgt); err != nil {
		fail(err)
	}
	if err := runPair(ctx, src, tgt, "differ",
		[]string{"ADD COLUMN", "ADD", "COMMENT", "CREATE TRIGGER"},
		[]string{"column", "constraint", "index", "comment", "trigger"},
	); err != nil {
		fmt.Println("失败:", err)
		failed = true
	} else {
		fmt.Println("== 自测通过 ==")
	}

	cleanup(ctx, src)
	cleanup(ctx, tgt)
	if failed {
		os.Exit(1)
	}
}

func runSingleDBFixCheck(ctx context.Context, db *sql.DB) error {
	item := dbmig.TableSchemaDiff{
		Table:  tableName,
		Status: "differ",
		Diffs: []dbmig.SchemaDiffItem{
			{Kind: "column", Name: "score", Message: "仅源端有该列"},
			{Kind: "comment", Name: "name", Left: "名称", Message: "仅源端有列注释"},
			{Kind: "index", Name: "idx_tenant", Message: "仅源端有该索引"},
			{Kind: "constraint", Name: "uq_tenant_name", Message: "仅源端有该约束"},
			{Kind: "trigger", Name: "trg_selftest_bi", Message: "仅源端有该触发器"},
			{Kind: "comment", Left: "自测表", Message: "仅源端有表注释"},
		},
	}
	fix, err := dbmig.BuildSchemaFixSQL(ctx, db, "mysql", "", "", []dbmig.TableSchemaDiff{item})
	if err != nil {
		return err
	}
	fmt.Println(fix)
	for _, n := range []string{"ADD COLUMN", "ADD INDEX", "ADD UNIQUE", "CREATE TRIGGER", "COMMENT="} {
		if !strings.Contains(fix, n) {
			// ADD UNIQUE 可能写作 ADD UNIQUE `uq`
			if n == "ADD UNIQUE" && strings.Contains(fix, "UNIQUE") {
				continue
			}
			if n == "ADD INDEX" && strings.Contains(fix, "INDEX") {
				continue
			}
			if n == "COMMENT=" && strings.Contains(fix, "COMMENT") {
				continue
			}
			return fmt.Errorf("修复 SQL 缺少 %s", n)
		}
	}
	return nil
}

func runPair(ctx context.Context, left, right *sql.DB, wantStatus string, needSQL, needKinds []string) error {
	items, err := dbmig.CompareSchemas(ctx, left, right, dbmig.SchemaCompareSpec{
		Engine: "mysql", Tables: []string{tableName},
	})
	if err != nil {
		return err
	}
	fmt.Println("== 对比结果 ==")
	var tableDiff *dbmig.TableSchemaDiff
	kinds := map[string]bool{}
	for i := range items {
		it := &items[i]
		fmt.Printf("  table=%s status=%s msg=%s diffs=%d\n", it.Table, it.Status, it.Message, len(it.Diffs))
		for _, d := range it.Diffs {
			fmt.Printf("    [%s] %s | %s\n", d.Kind, d.Name, d.Message)
			kinds[d.Kind] = true
		}
		if it.Table == tableName {
			tableDiff = it
		}
	}
	fixSQL, err := dbmig.BuildSchemaFixSQL(ctx, left, "mysql", "", "", items)
	if err != nil {
		return err
	}
	fmt.Println("== 修复 SQL ==")
	fmt.Println(fixSQL)

	var errs []string
	if tableDiff == nil {
		errs = append(errs, "缺少表结果")
	} else if tableDiff.Status != wantStatus {
		errs = append(errs, fmt.Sprintf("期望 status=%s 实际=%s", wantStatus, tableDiff.Status))
	}
	for _, k := range needKinds {
		if !kinds[k] {
			errs = append(errs, "未检出 "+k)
		}
	}
	for _, n := range needSQL {
		if !strings.Contains(fixSQL, n) {
			errs = append(errs, "SQL 缺少 "+n)
		}
	}
	if len(errs) == 0 {
		fmt.Println("== 在目标执行修复 SQL ==")
		if _, err := right.ExecContext(ctx, stripSQLComments(fixSQL)); err != nil {
			errs = append(errs, "执行失败: "+err.Error())
		} else {
			items2, _ := dbmig.CompareSchemas(ctx, left, right, dbmig.SchemaCompareSpec{Engine: "mysql", Tables: []string{tableName}})
			fmt.Println("== 修复后再次对比 ==")
			for _, it := range items2 {
				if it.Table != tableName {
					continue
				}
				fmt.Printf("  status=%s diffs=%d\n", it.Status, len(it.Diffs))
				for _, d := range it.Diffs {
					fmt.Printf("    [%s] %s | %s\n", d.Kind, d.Name, d.Message)
				}
				if it.Status != "match" {
					errs = append(errs, fmt.Sprintf("修复后仍不一致 (%d)", len(it.Diffs)))
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
		`DROP TRIGGER IF EXISTS trg_selftest_bi`,
		`DROP TABLE IF EXISTS ` + tableName,
		`CREATE TABLE ` + tableName + ` (
			id BIGINT NOT NULL AUTO_INCREMENT,
			tenant_id BIGINT NOT NULL,
			name VARCHAR(64) NOT NULL COMMENT '名称',
			score DECIMAL(10,2) DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			UNIQUE KEY uq_tenant_name (tenant_id, name),
			KEY idx_tenant (tenant_id)
		) COMMENT='自测表'`,
		`CREATE TRIGGER trg_selftest_bi BEFORE INSERT ON ` + tableName + `
			FOR EACH ROW SET NEW.score = COALESCE(NEW.score, 0)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("%w\n%s", err, s)
		}
	}
	return nil
}

func setupTargetPartial(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`DROP TRIGGER IF EXISTS trg_selftest_bi`,
		`DROP TABLE IF EXISTS ` + tableName,
		`CREATE TABLE ` + tableName + ` (
			id BIGINT NOT NULL AUTO_INCREMENT,
			tenant_id BIGINT NOT NULL,
			name VARCHAR(64) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func cleanup(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_selftest_bi`)
	_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS `+tableName)
}

func stripSQLComments(script string) string {
	var b strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func fail(err error) {
	fmt.Println("FATAL:", err)
	os.Exit(1)
}
