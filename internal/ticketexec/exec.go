package ticketexec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
)

const (
	ddlTimeout = 90 * time.Second
	maxSQLLen  = 500
)

type Target struct {
	Host      string
	Port      int
	Username  string
	Password  string
	DBName    string
	DBType    string
	ConnectDB string // 实际连库名；建库工单连管理库，与 DBName（新建库）分离
}

type Result struct {
	Success     bool      `json:"success"`
	Skipped     bool      `json:"skipped,omitempty"`
	Message     string    `json:"message,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	StartedAt   string    `json:"started_at,omitempty"`
	EndedAt     string    `json:"ended_at,omitempty"`
	Engine      string    `json:"engine,omitempty"`
	Records     []Record  `json:"records,omitempty"`
	SuccessSQLs []SuccSQL `json:"success_sqls,omitempty"`
}

type Record struct {
	InstanceID   string `json:"instance_id,omitempty"`
	InstanceName string `json:"instance_name,omitempty"`
	Host         string `json:"host"`
	DB           string `json:"db"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	SQL          string `json:"sql"`
	StartedAt    string `json:"started_at,omitempty"`
	EndedAt      string `json:"ended_at,omitempty"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	AffectedRow  int64  `json:"affected_row"`
	Order        int    `json:"order"`
}

type SuccSQL struct {
	Host  string `json:"host"`
	DB    string `json:"db"`
	SQL   string `json:"sql"`
	Order int    `json:"order"`
}

func ShouldAutoRun(issueType string) bool {
	switch strings.ToLower(strings.TrimSpace(issueType)) {
	case store.IssueTypeDDL, store.IssueTypeDML, store.IssueTypeCreateDB:
		return true
	default:
		return false
	}
}

// AdminConnectDB 建库必须连已有库：PG 连 postgres，Cockroach 连 defaultdb，MySQL/SR 不指定库。
func AdminConnectDB(dbType string) string {
	switch EngineOf(dbType) {
	case "postgres":
		if strings.Contains(strings.ToLower(strings.TrimSpace(dbType)), "cockroach") {
			return "defaultdb"
		}
		return "postgres"
	default:
		return ""
	}
}

func connectDB(t Target) string {
	if s := strings.TrimSpace(t.ConnectDB); s != "" {
		return s
	}
	return strings.TrimSpace(t.DBName)
}

func CanRetry(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case store.StatusApproved, store.StatusFailed:
		return true
	default:
		return false
	}
}

func EngineOf(dbType string) string {
	return store.NormalizeReviewEngine(dbType)
}

func Executable(dbType string) bool {
	switch EngineOf(dbType) {
	case "postgres", "starrocks":
		return true
	default:
		return false
	}
}

func ToJSON(r *Result) string {
	if r == nil {
		return ""
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func Parse(raw string) *Result {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var r Result
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return &r
}

func createdbExecutable(dbType string) bool {
	switch EngineOf(dbType) {
	case "postgres", "starrocks", "mysql":
		return true
	default:
		return false
	}
}

func Run(ctx context.Context, issueType string, sqlContent string, t Target) *Result {
	res := &Result{StartedAt: nowRFC3339(), Engine: EngineOf(t.DBType)}
	issueType = strings.ToLower(strings.TrimSpace(issueType))
	if !ShouldAutoRun(issueType) {
		res.Skipped = true
		res.Success = true
		res.Message = "该工单类型不自动连库执行"
		res.EndedAt = nowRFC3339()
		return res
	}
	canExec := Executable(t.DBType)
	if issueType == store.IssueTypeCreateDB {
		canExec = createdbExecutable(t.DBType)
		if strings.TrimSpace(t.ConnectDB) == "" {
			t.ConnectDB = AdminConnectDB(t.DBType)
		}
	}
	if !canExec {
		res.Skipped = true
		res.Success = true
		res.Summary = "MySQL/TiDB 工单仅套审核规则，不自动连库执行（与 Hub 一致）"
		res.Message = res.Summary
		res.appendRecord(Record{
			Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
			SQL: displaySQL(sqlContent), Status: "skipped", ErrorMessage: res.Summary,
			StartedAt: res.StartedAt, EndedAt: nowRFC3339(),
		})
		res.EndedAt = nowRFC3339()
		return res
	}
	if strings.TrimSpace(t.Host) == "" || t.Port <= 0 {
		return fail(res, "实例未配置连接主机或端口")
	}
	if strings.TrimSpace(t.Username) == "" {
		return fail(res, "实例未配置执行账号")
	}
	if issueType != store.IssueTypeCreateDB && strings.TrimSpace(t.DBName) == "" {
		return fail(res, "工单未指定数据库")
	}
	stmts := sqlcheck.SplitSQL(sqlContent)
	if len(stmts) == 0 {
		return fail(res, "SQL 不能为空")
	}

	runCtx, cancel := context.WithTimeout(ctx, ddlTimeout)
	defer cancel()

	var err error
	if issueType == store.IssueTypeDML && res.Engine == "postgres" {
		err = runPostgresDML(runCtx, t, stmts, res)
	} else if res.Engine == "postgres" {
		err = runPostgresDDL(runCtx, t, stmts, res)
	} else {
		err = runMySQLFamily(runCtx, t, stmts, res)
	}
	res.EndedAt = nowRFC3339()
	if err != nil {
		res.Success = false
		if res.Message == "" {
			res.Message = err.Error()
		}
		if len(res.SuccessSQLs) > 0 {
			res.Summary = fmt.Sprintf("部分执行后失败，已成功 %d 条", len(res.SuccessSQLs))
		}
		return res
	}
	res.Success = true
	res.Summary = fmt.Sprintf("成功执行 %d 条 SQL", len(res.SuccessSQLs))
	return res
}

// ExecStatements 连库串行执行（无事务），供建库后 CREATE USER / GRANT 使用。
func ExecStatements(ctx context.Context, t Target, stmts []string) *Result {
	res := &Result{StartedAt: nowRFC3339(), Engine: EngineOf(t.DBType)}
	if len(stmts) == 0 {
		res.Success = true
		res.EndedAt = nowRFC3339()
		return res
	}
	if strings.TrimSpace(t.Host) == "" || t.Port <= 0 {
		return fail(res, "实例未配置连接主机或端口")
	}
	runCtx, cancel := context.WithTimeout(ctx, ddlTimeout)
	defer cancel()
	var err error
	if res.Engine == "postgres" {
		err = runPostgresDDL(runCtx, t, stmts, res)
	} else {
		err = runMySQLFamily(runCtx, t, stmts, res)
	}
	res.EndedAt = nowRFC3339()
	if err != nil {
		res.Success = false
		if res.Message == "" {
			res.Message = err.Error()
		}
		return res
	}
	res.Success = true
	return res
}

func fail(res *Result, msg string) *Result {
	res.Success = false
	res.Message = msg
	res.EndedAt = nowRFC3339()
	return res
}

func (r *Result) appendRecord(rec Record) {
	rec.Order = len(r.Records) + 1
	r.Records = append(r.Records, rec)
}

func (r *Result) appendSuccess(t Target, sqlText string, started, ended time.Time, affected int64) {
	disp := displaySQL(sqlText)
	r.SuccessSQLs = append(r.SuccessSQLs, SuccSQL{
		Host: t.Host, DB: t.DBName, SQL: disp, Order: len(r.SuccessSQLs) + 1,
	})
	r.appendRecord(Record{
		Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
		SQL: disp, StartedAt: started.UTC().Format(time.RFC3339), EndedAt: ended.UTC().Format(time.RFC3339),
		Status: "success", AffectedRow: affected,
	})
}

func (r *Result) appendFailure(t Target, sqlText string, started time.Time, err error) {
	r.appendRecord(Record{
		Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
		SQL: displaySQL(sqlText), StartedAt: started.UTC().Format(time.RFC3339), EndedAt: nowRFC3339(),
		Status: "failed", ErrorMessage: err.Error(),
	})
}

func runPostgresDDL(ctx context.Context, t Target, stmts []string, collect *Result) error {
	db, err := openPostgres(t)
	if err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	return execSerial(ctx, db, t, stmts, collect)
}

func runPostgresDML(ctx context.Context, t Target, stmts []string, collect *Result) error {
	db, err := openPostgres(t)
	if err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		collect.appendFailure(t, "(BEGIN)", time.Now(), err)
		return fmt.Errorf("开启事务失败: %w", err)
	}
	for i, stmt := range stmts {
		started := time.Now()
		ret, exErr := tx.ExecContext(ctx, stmt)
		if exErr != nil {
			_ = tx.Rollback()
			collect.appendFailure(t, stmt, started, exErr)
			return fmt.Errorf("执行第 %d 条 SQL 失败: %w", i+1, exErr)
		}
		collect.appendSuccess(t, stmt, started, time.Now(), rowsAffected(ret))
	}
	if err := tx.Commit(); err != nil {
		collect.appendFailure(t, "(COMMIT)", time.Now(), err)
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

func runMySQLFamily(ctx context.Context, t Target, stmts []string, collect *Result) error {
	db, err := openMySQL(t)
	if err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	return execSerial(ctx, db, t, stmts, collect)
}

func execSerial(ctx context.Context, db *sql.DB, t Target, stmts []string, collect *Result) error {
	for i, stmt := range stmts {
		started := time.Now()
		ret, err := db.ExecContext(ctx, stmt)
		if err != nil {
			collect.appendFailure(t, stmt, started, err)
			return fmt.Errorf("执行第 %d 条 SQL 失败: %w", i+1, err)
		}
		collect.appendSuccess(t, stmt, started, time.Now(), rowsAffected(ret))
	}
	return nil
}

func openMySQL(t Target) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = t.Username
	cfg.Passwd = t.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", t.Host, t.Port)
	cfg.DBName = connectDB(t)
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = ddlTimeout
	cfg.WriteTimeout = ddlTimeout
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(ddlTimeout)
	db.SetMaxOpenConns(1)
	return db, nil
}

func openPostgres(t Target) (*sql.DB, error) {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(t.Username, t.Password),
		Host:   fmt.Sprintf("%s:%d", t.Host, t.Port),
		Path:   "/" + connectDB(t),
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "8")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(ddlTimeout)
	db.SetMaxOpenConns(1)
	return db, nil
}

func rowsAffected(ret sql.Result) int64 {
	if ret == nil {
		return 0
	}
	n, _ := ret.RowsAffected()
	return n
}

func displaySQL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxSQLLen {
		return s[:maxSQLLen] + "..."
	}
	return s
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
