package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/store"
)

type VerifyReq struct {
	RowCount bool `json:"row_count"`
	Schema   bool `json:"schema"`
}

type VerifyItem struct {
	ObjectID    string `json:"object_id"`
	Table       string `json:"table"`
	CheckType   string `json:"check_type"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	RowMatch    *bool  `json:"row_match,omitempty"`
	SchemaMatch *bool  `json:"schema_match,omitempty"`
	SourceRows  int64  `json:"source_rows,omitempty"`
	TargetRows  int64  `json:"target_rows,omitempty"`
}

type VerifyResult struct {
	Status     string         `json:"status"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at"`
	Details    map[string]any `json:"details"`
}

func Verify(ctx context.Context, st *store.DB, secretKey string, t *store.MigrationTask, req VerifyReq) (*VerifyResult, error) {
	if t == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if t.Status != store.StatusCompleted {
		return nil, fmt.Errorf("任务未完成，无法校验")
	}
	engine := store.NormalizeDBType(t.DBType)
	srcInst, err := st.GetInstance(t.SourceID)
	if err != nil || srcInst == nil {
		return nil, fmt.Errorf("源实例不存在")
	}
	tgtInst, err := st.GetInstance(t.TargetID)
	if err != nil || tgtInst == nil {
		return nil, fmt.Errorf("目标实例不存在")
	}
	objs := t.Objects
	if len(objs) == 0 {
		objs, err = st.ListMigrationObjects(t.ID)
		if err != nil {
			return nil, err
		}
	}
	started := time.Now().UTC().Format(time.RFC3339)
	var items []VerifyItem
	for _, o := range objs {
		srcDB, err := OpenInstance(ctx, srcInst, secretKey, firstNonEmpty(o.SourceDB, firstNonEmpty(t.SourceDB, defaultDB(engine))))
		if err != nil {
			return nil, fmt.Errorf("连接源库: %w", err)
		}
		tgtDB, err := OpenInstance(ctx, tgtInst, secretKey, firstNonEmpty(o.TargetDB, firstNonEmpty(t.TargetDB, defaultDB(engine))))
		if err != nil {
			srcDB.Close()
			return nil, fmt.Errorf("连接目标库: %w", err)
		}
		if req.RowCount {
			var sr, tr int64
			_ = countTableRows(ctx, srcDB, engine, o.SourceSchema, o.SourceTable, &sr)
			_ = countTableRows(ctx, tgtDB, engine, o.TargetSchema, o.TargetTable, &tr)
			match := sr == tr
			stText := "不一致"
			if match {
				stText = "一致"
			}
			items = append(items, VerifyItem{
				ObjectID:   o.ID,
				Table:      o.SourceTable,
				CheckType:  "行数比对",
				Status:     stText,
				Detail:     fmt.Sprintf("源 %d 行 / 目标 %d 行", sr, tr),
				RowMatch:   &match,
				SourceRows: sr,
				TargetRows: tr,
			})
		}
		if req.Schema {
			ok, msg := compareTableSchema(ctx, srcDB, tgtDB, engine, o)
			stText := "不一致"
			if ok {
				stText = "一致"
			}
			items = append(items, VerifyItem{
				ObjectID:    o.ID,
				Table:       o.SourceTable,
				CheckType:   "结构比对",
				Status:      stText,
				Detail:      msg,
				SchemaMatch: &ok,
			})
		}
		srcDB.Close()
		tgtDB.Close()
	}
	finished := time.Now().UTC().Format(time.RFC3339)
	return &VerifyResult{
		Status:     "completed",
		StartedAt:  started,
		FinishedAt: finished,
		Details:    map[string]any{"objects": items},
	}, nil
}

func countTableRows(ctx context.Context, db *sql.DB, engine, schema, table string, out *int64) error {
	if db == nil || strings.TrimSpace(table) == "" {
		return fmt.Errorf("表名为空")
	}
	pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if store.NormalizeDBType(engine) == "postgres" {
		sch := schema
		if sch == "" {
			sch = "public"
		}
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`, quoteIdent(engine, sch), quoteIdent(engine, table))
		return db.QueryRowContext(pctx, q).Scan(out)
	}
	q := "SELECT COUNT(*) FROM `" + strings.ReplaceAll(table, "`", "``") + "`"
	return db.QueryRowContext(pctx, q).Scan(out)
}

func compareTableSchema(ctx context.Context, src, tgt *sql.DB, engine string, o store.MigrationObject) (bool, string) {
	if store.NormalizeDBType(engine) == "postgres" {
		ss := firstNonEmpty(o.SourceSchema, "public")
		ts := firstNonEmpty(o.TargetSchema, firstNonEmpty(o.SourceSchema, "public"))
		srcSch, err := fetchPGTableSchemaInfo(ctx, src, ss, o.SourceTable)
		if err != nil {
			return false, "读源表结构失败: " + err.Error()
		}
		tgtSch, err := fetchPGTableSchemaInfo(ctx, tgt, ts, o.TargetTable)
		if err != nil {
			return false, "读目标表结构失败: " + err.Error()
		}
		if err := comparePGTableSchemaInfo(srcSch, tgtSch, o.SourceTable); err != nil {
			return false, err.Error()
		}
		return true, "列、约束、索引一致"
	}
	srcCols, err := listTableColumns(ctx, src, engine, o.SourceSchema, o.SourceTable)
	if err != nil {
		return false, "读源表结构失败: " + err.Error()
	}
	tgtCols, err := listTableColumns(ctx, tgt, engine, o.TargetSchema, o.TargetTable)
	if err != nil {
		return false, "读目标表结构失败: " + err.Error()
	}
	if srcCols != tgtCols {
		return false, "列不一致"
	}
	return true, "列、约束、索引一致"
}

func listTableColumns(ctx context.Context, db *sql.DB, engine, schema, table string) (string, error) {
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var rows *sql.Rows
	var err error
	if store.NormalizeDBType(engine) == "postgres" {
		sch := schema
		if sch == "" {
			sch = "public"
		}
		rows, err = db.QueryContext(pctx, `
			SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema=$1 AND table_name=$2 ORDER BY ordinal_position`, sch, table)
	} else {
		rows, err = db.QueryContext(pctx, `
			SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema=DATABASE() AND table_name=? ORDER BY ordinal_position`, table)
	}
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return "", err
		}
		parts = append(parts, name+":"+typ)
	}
	return strings.Join(parts, ","), rows.Err()
}
