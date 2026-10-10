package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SchemaCompareTask 表结构对比异步任务。
type SchemaCompareTask struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id,omitempty"`
	Status            string          `json:"status"`
	LeftInstanceID    string          `json:"left_instance_id"`
	RightInstanceID   string          `json:"right_instance_id"`
	LeftDB            string          `json:"left_db"`
	RightDB           string          `json:"right_db"`
	LeftSchema        string          `json:"left_schema"`
	RightSchema       string          `json:"right_schema"`
	TablesJSON        string          `json:"-"`
	Tables            []string        `json:"tables"`
	Engine            string          `json:"engine"`
	SummaryJSON       string          `json:"-"`
	Summary           map[string]any  `json:"summary,omitempty"`
	ResultJSON        string          `json:"-"`
	Items             json.RawMessage `json:"items,omitempty"`
	FixSQL            string          `json:"fix_sql,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	Progress          int             `json:"progress"`
	ProgressMessage   string          `json:"progress_message,omitempty"`
	CreatedBy         string          `json:"created_by,omitempty"`
	StartedAt         string          `json:"started_at,omitempty"`
	FinishedAt        string          `json:"finished_at,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	LeftInstanceName  string          `json:"left_instance_name,omitempty"`
	RightInstanceName string          `json:"right_instance_name,omitempty"`
}

const schemaCompareTaskCols = `id, status, left_instance_id, right_instance_id, left_db, right_db,
	left_schema, right_schema, tables_json, engine, summary_json, result_json, COALESCE(fix_sql,''), error_message,
	progress, progress_message, created_by, started_at, finished_at, created_at, updated_at`

func attachSchemaCompareJSON(t *SchemaCompareTask, withResult bool) {
	if t.TablesJSON != "" {
		_ = json.Unmarshal([]byte(t.TablesJSON), &t.Tables)
	}
	if t.Tables == nil {
		t.Tables = []string{}
	}
	if t.SummaryJSON != "" && t.SummaryJSON != "{}" {
		_ = json.Unmarshal([]byte(t.SummaryJSON), &t.Summary)
	}
	if withResult && t.ResultJSON != "" {
		t.Items = json.RawMessage(t.ResultJSON)
	}
	if !withResult {
		t.FixSQL = ""
	}
}

func scanSchemaCompareTask(s interface{ Scan(...any) error }, t *SchemaCompareTask) error {
	return s.Scan(
		&t.ID, &t.Status, &t.LeftInstanceID, &t.RightInstanceID, &t.LeftDB, &t.RightDB,
		&t.LeftSchema, &t.RightSchema, &t.TablesJSON, &t.Engine, &t.SummaryJSON, &t.ResultJSON, &t.FixSQL, &t.ErrorMessage,
		&t.Progress, &t.ProgressMessage, &t.CreatedBy, &t.StartedAt, &t.FinishedAt, &t.CreatedAt, &t.UpdatedAt,
	)
}

func (d *DB) CreateSchemaCompareTask(t *SchemaCompareTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	if t.LeftSchema == "" {
		t.LeftSchema = "public"
	}
	if t.RightSchema == "" {
		t.RightSchema = "public"
	}
	if t.Tables == nil {
		t.Tables = []string{}
	}
	b, _ := json.Marshal(t.Tables)
	t.TablesJSON = string(b)
	if t.SummaryJSON == "" {
		t.SummaryJSON = "{}"
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_schema_compare_tasks(
			id, status, left_instance_id, right_instance_id, left_db, right_db,
			left_schema, right_schema, tables_json, engine, summary_json, result_json, fix_sql, error_message,
			progress, progress_message, created_by, started_at, finished_at, created_at, updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Status, t.LeftInstanceID, t.RightInstanceID, t.LeftDB, t.RightDB,
		t.LeftSchema, t.RightSchema, t.TablesJSON, t.Engine, t.SummaryJSON, t.ResultJSON, t.FixSQL, t.ErrorMessage,
		t.Progress, t.ProgressMessage, t.CreatedBy, t.StartedAt, t.FinishedAt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if t.TenantID == "" {
		t.TenantID = d.inheritTenantFromInstance(t.LeftInstanceID)
	}
	return d.AssignTenantID("tbl_schema_compare_tasks", t.ID, t.TenantID)
}

func (d *DB) GetSchemaCompareTask(id string, withResult bool) (*SchemaCompareTask, error) {
	t := &SchemaCompareTask{}
	err := scanSchemaCompareTask(d.QueryRow(`SELECT `+schemaCompareTaskCols+` FROM tbl_schema_compare_tasks WHERE id=?`, id), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attachSchemaCompareJSON(t, withResult)
	return t, nil
}

func (d *DB) GetSchemaCompareTaskInTenant(id, tenantID string, withResult bool) (*SchemaCompareTask, error) {
	t, err := d.GetSchemaCompareTask(id, withResult)
	return ownedInTenant(d, t, err, "tbl_schema_compare_tasks", id, tenantID)
}

func (d *DB) ListSchemaCompareTasks(limit int) ([]SchemaCompareTask, error) {
	return d.ListSchemaCompareTasksInTenant("", limit)
}

func (d *DB) ListSchemaCompareTasksInTenant(tenantID string, limit int) ([]SchemaCompareTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + schemaCompareTaskCols + ` FROM tbl_schema_compare_tasks WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SchemaCompareTask
	for rows.Next() {
		var t SchemaCompareTask
		if err := scanSchemaCompareTask(rows, &t); err != nil {
			return nil, err
		}
		attachSchemaCompareJSON(&t, false)
		// 列表不回传全文；has_fix_sql 以 summary 为准，缺省时按是否含 DDL 推断
		fix := t.FixSQL
		t.FixSQL = ""
		if t.Summary == nil {
			t.Summary = map[string]any{}
		}
		if _, ok := t.Summary["has_fix_sql"]; !ok {
			t.Summary["has_fix_sql"] = strings.Contains(fix, "CREATE ") || strings.Contains(fix, "ALTER ")
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) UpdateSchemaCompareProgress(id string, progress int, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_schema_compare_tasks SET progress=?, progress_message=?, updated_at=? WHERE id=?`,
		progress, msg, now, id,
	)
	return err
}

func (d *DB) MarkSchemaCompareRunning(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_schema_compare_tasks SET status=?, started_at=?, progress=?, progress_message=?, updated_at=? WHERE id=?`,
		StatusRunning, now, 1, "开始对比", now, id,
	)
	return err
}

func (d *DB) SaveSchemaCompareResult(id string, summary map[string]any, resultJSON, fixSQL string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	sum := "{}"
	if summary != nil {
		b, _ := json.Marshal(summary)
		sum = string(b)
	}
	_, err := d.Exec(
		`UPDATE tbl_schema_compare_tasks SET status=?, summary_json=?, result_json=?, fix_sql=?, error_message='',
			progress=100, progress_message=?, finished_at=?, updated_at=? WHERE id=?`,
		StatusCompleted, sum, resultJSON, fixSQL, "完成", now, now, id,
	)
	return err
}

func (d *DB) FailSchemaCompareTask(id, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`UPDATE tbl_schema_compare_tasks SET status=?, error_message=?, progress_message=?, finished_at=?, updated_at=? WHERE id=?`,
		StatusFailed, strings.TrimSpace(errMsg), "失败", now, now, id,
	)
	return err
}

func (d *DB) DeleteSchemaCompareTask(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_schema_compare_tasks WHERE id=?`, id)
	return err
}
