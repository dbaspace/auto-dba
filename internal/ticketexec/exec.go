package ticketexec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
)

const (
	// 单目标连库执行上限；大 SQL / 多语句需足够长，避免误杀后台任务
	ddlTimeout = 2 * time.Hour
	// 成功明细可截断以控制落库体积；失败明细必须保留全文便于排障
	maxSQLLen = 8 << 10 // 8KiB
)

// AsyncAutoExecMaxWait 审批/手动触发后的后台总墙钟上限（多实例串行）
const AsyncAutoExecMaxWait = 6 * time.Hour

type Target struct {
	Host      string
	Port      int
	Username  string
	Password  string
	DBName    string
	DBType    string
	ConnectDB string // 实际连库名；建库工单连管理库，与 DBName（新建库）分离
	// LongStmtTimeout PG 长耗时语句单条超时；<=0 用 DefaultLongStmtTimeout
	LongStmtTimeout time.Duration
	// Light 为真时不在内存里保留每条语句的完整 SQL，只留计数和失败信息。
	Light bool
	// OnProgress 每条语句开始前回调。sessionID 为当前连接的后端会话号，取不到时为 0。
	OnProgress func(stmtIndex, stmtTotal int, sessionID int64)
}

type Result struct {
	Success        bool         `json:"success"`
	Skipped        bool         `json:"skipped,omitempty"`
	Message        string       `json:"message,omitempty"`
	Summary        string       `json:"summary,omitempty"`
	StartedAt      string       `json:"started_at,omitempty"`
	EndedAt        string       `json:"ended_at,omitempty"`
	Engine         string       `json:"engine,omitempty"`
	ChangeID       string       `json:"change_id,omitempty"`
	StatementCount int          `json:"statement_count,omitempty"` // 计划执行条数（SplitSQL）
	SuccessCount   int          `json:"success_count,omitempty"`
	FailCount      int          `json:"fail_count,omitempty"`
	RolledBack     bool         `json:"rolled_back,omitempty"`
	Compacted      bool         `json:"compacted,omitempty"`
	TargetStats    []TargetStat `json:"target_stats,omitempty"`
	Records        []Record     `json:"records,omitempty"`
	SuccessSQLs    []SuccSQL    `json:"success_sqls,omitempty"`

	// 执行期有界采集（不落库）
	sampleKept  map[string]int         `json:"-"`
	targetHit   map[string]*TargetStat `json:"-"`
	compactMode bool                   `json:"-"`
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
	case store.IssueTypeDDL, store.IssueTypeDML, store.IssueTypeCreateDB, store.IssueTypeHA,
		store.IssueTypeSrCluster, store.IssueTypeDbDeploy, store.IssueTypeResourceApply, store.IssueTypeRedis,
		store.IssueTypeK8sChange, store.IssueTypeBackupRestore, store.IssueTypeDnsChange:
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
	case store.StatusApproved, store.StatusFailed, store.StatusStopped:
		return true
	default:
		return false
	}
}

// IsExecuting 工单是否正在后台执行（不可重复触发）
func IsExecuting(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), store.StatusRunning)
}

func EngineOf(dbType string) string {
	return store.NormalizeReviewEngine(dbType)
}

func Executable(dbType string) bool {
	switch EngineOf(dbType) {
	case "postgres", "starrocks", "mysql":
		return true
	default:
		return false
	}
}

func ToJSON(r *Result) string {
	if r == nil {
		return ""
	}
	Compact(r)
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
		res.Summary = "该引擎工单仅套审核规则，不自动连库执行"
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
		return fail(res, t, "实例未配置连接主机或端口")
	}
	if strings.TrimSpace(t.Username) == "" {
		return fail(res, t, "实例未配置执行账号")
	}
	if issueType != store.IssueTypeCreateDB && strings.TrimSpace(t.DBName) == "" {
		return fail(res, t, "工单未指定数据库")
	}
	stmts := sqlcheck.SplitSQL(sqlContent)
	res.StatementCount = len(stmts)
	if len(stmts) == 0 {
		return fail(res, t, "SQL 不能为空")
	}
	// 语句数已超阈值：一开始就按库抽样，避免执行过程中堆出百兆明细
	if len(stmts) > DetailCompactThreshold {
		res.enableCompactMode()
	}

	runCtx, cancel := context.WithTimeout(ctx, runTimeout(res.Engine, stmts, t.LongStmtTimeout))
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
			res.Message = formatExecErr(err)
		}
		if res.SuccessCount > 0 && !res.RolledBack {
			res.Summary = fmt.Sprintf("部分执行后失败，已成功 %d / %d 条语句", res.SuccessCount, res.StatementCount)
		}
		return Compact(res)
	}
	res.Success = true
	res.Summary = fmt.Sprintf("成功执行 %d / %d 条语句（按分号拆分）", res.SuccessCount, res.StatementCount)
	return Compact(res)
}

// ExecStatements 连库串行执行（无事务），供建库后 CREATE USER / GRANT 使用。
func ExecStatements(ctx context.Context, t Target, stmts []string) *Result {
	res := &Result{StartedAt: nowRFC3339(), Engine: EngineOf(t.DBType)}
	if len(stmts) == 0 {
		res.Success = true
		res.EndedAt = nowRFC3339()
		return res
	}
	if len(stmts) > DetailCompactThreshold {
		res.enableCompactMode()
	}
	if strings.TrimSpace(t.Host) == "" || t.Port <= 0 {
		return fail(res, t, "实例未配置连接主机或端口")
	}
	runCtx, cancel := context.WithTimeout(ctx, runTimeout(res.Engine, stmts, t.LongStmtTimeout))
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
			res.Message = formatExecErr(err)
		}
		return Compact(res)
	}
	res.Success = true
	return Compact(res)
}

// PrecheckSQL 未连库即失败时明细里的 SQL 占位
const PrecheckSQL = "(执行前检查)"

func fail(res *Result, t Target, msg string) *Result {
	res.Success = false
	res.Message = msg
	res.appendPrecheckFailure(Record{Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username}, msg)
	res.EndedAt = nowRFC3339()
	return res
}

// FailBeforeExec 未连库即失败：同时写 Message 与一条失败明细，前端按库展示具体原因。
func FailBeforeExec(msg string, rec Record) *Result {
	now := nowRFC3339()
	res := &Result{Success: false, Message: msg, StartedAt: now, EndedAt: now}
	res.appendPrecheckFailure(rec, msg)
	return res
}

func (r *Result) appendPrecheckFailure(rec Record, msg string) {
	if strings.TrimSpace(rec.SQL) == "" {
		rec.SQL = PrecheckSQL
	}
	now := nowRFC3339()
	if rec.StartedAt == "" {
		rec.StartedAt = now
	}
	if rec.EndedAt == "" {
		rec.EndedAt = now
	}
	rec.Status = "failed"
	rec.ErrorMessage = msg
	r.FailCount++
	r.appendRecord(rec)
}

func (r *Result) bumpTarget(host, db, status string) {
	if r == nil {
		return
	}
	if r.targetHit == nil {
		r.targetHit = map[string]*TargetStat{}
	}
	key := targetKey(host, db)
	st := r.targetHit[key]
	if st == nil {
		st = &TargetStat{Host: host, DB: db}
		r.targetHit[key] = st
	}
	if strings.EqualFold(status, "failed") {
		st.Failed++
	} else if strings.EqualFold(status, "success") {
		st.Success++
	}
}

func (r *Result) appendRecord(rec Record) {
	rec.Order = len(r.Records) + 1
	r.Records = append(r.Records, rec)
}

func (r *Result) appendSuccess(t Target, sqlText string, started, ended time.Time, affected int64) {
	r.SuccessCount++
	r.bumpTarget(t.Host, t.DBName, "success")
	if t.Light {
		return
	}
	if r.SuccessCount > DetailCompactThreshold {
		r.enableCompactMode()
	}
	if !r.shouldKeepSuccessDetail(t.Host, t.DBName) {
		return
	}
	full := strings.TrimSpace(sqlText)
	// SuccessSQLs 仅作辅助列表，可截断；Records 留给详情弹窗，保留全文（回滚后仍可看）
	r.SuccessSQLs = append(r.SuccessSQLs, SuccSQL{
		Host: t.Host, DB: t.DBName, SQL: displaySQL(full), Order: len(r.SuccessSQLs) + 1,
	})
	r.appendRecord(Record{
		Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
		SQL: full, StartedAt: started.UTC().Format(time.RFC3339), EndedAt: ended.UTC().Format(time.RFC3339),
		Status: "success", AffectedRow: affected,
	})
}

func (r *Result) appendFailure(t Target, sqlText string, started time.Time, err error) {
	r.FailCount++
	r.bumpTarget(t.Host, t.DBName, "failed")
	sqlText = strings.TrimSpace(sqlText)
	if t.Light {
		sqlText = ""
	}
	r.appendRecord(Record{
		Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
		// 失败语句保留全文，避免弹窗「查看」只能看到截断片段
		SQL: sqlText, StartedAt: started.UTC().Format(time.RFC3339), EndedAt: nowRFC3339(),
		Status: "failed", ErrorMessage: formatExecErr(err),
	})
}

// formatExecErr 在驱动错误文本后补充 PG 的 Detail / Hint / 位置 / 对象，便于定位 DML 失败原因。
func formatExecErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe == nil {
		return msg
	}
	var extra []string
	if s := strings.TrimSpace(pe.Detail); s != "" {
		extra = append(extra, "详情: "+s)
	}
	if s := strings.TrimSpace(pe.Hint); s != "" {
		extra = append(extra, "提示: "+s)
	}
	if pe.Position > 0 {
		extra = append(extra, fmt.Sprintf("位置: 第 %d 个字符", pe.Position))
	}
	if s := strings.TrimSpace(pe.Where); s != "" {
		extra = append(extra, "上下文: "+s)
	}
	var obj []string
	if tbl := strings.TrimSpace(pe.TableName); tbl != "" {
		if sc := strings.TrimSpace(pe.SchemaName); sc != "" {
			tbl = sc + "." + tbl
		}
		obj = append(obj, "表 "+tbl)
	}
	if s := strings.TrimSpace(pe.ColumnName); s != "" {
		obj = append(obj, "列 "+s)
	}
	if s := strings.TrimSpace(pe.ConstraintName); s != "" {
		obj = append(obj, "约束 "+s)
	}
	if len(obj) > 0 {
		extra = append(extra, "对象: "+strings.Join(obj, "，"))
	}
	if len(extra) == 0 {
		return msg
	}
	return msg + "\n" + strings.Join(extra, "\n")
}

func runPostgresDDL(ctx context.Context, t Target, stmts []string, collect *Result) error {
	db, err := openPostgres(t, runTimeout("postgres", stmts, t.LongStmtTimeout))
	if err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	return execSerial(ctx, db, t, stmts, collect, effectiveLongTimeout(t.LongStmtTimeout))
}

func runPostgresDML(ctx context.Context, t Target, stmts []string, collect *Result) error {
	db, err := openPostgres(t, ddlTimeout)
	if err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		collect.appendFailure(t, "(连接)", time.Now(), err)
		return fmt.Errorf("连接失败 %s/%s: %w", t.Host, t.DBName, err)
	}
	// 平台已包事务：剥离脚本内 BEGIN/COMMIT/ROLLBACK，避免双重提交导致 idle
	stmts = filterDMLTxnControlStatements(stmts)
	if len(stmts) == 0 {
		collect.appendFailure(t, "(过滤)", time.Now(), fmt.Errorf("仅含事务控制语句，无实际 DML"))
		return fmt.Errorf("仅含事务控制语句，无实际 DML")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		collect.appendFailure(t, "(BEGIN)", time.Now(), err)
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	sid := querySessionID(ctx, db, "postgres")
	total := len(stmts)
	for i, stmt := range stmts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.OnProgress != nil {
			t.OnProgress(i+1, total, sid)
		}
		started := time.Now()
		ret, exErr := tx.ExecContext(ctx, stmt)
		if exErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			collect.appendFailure(t, stmt, started, exErr)
			n := collect.markRolledBack()
			collect.Summary = fmt.Sprintf("第 %d 条 SQL 失败，事务已回滚，此前已执行的 %d 条均未生效", i+1, n)
			return fmt.Errorf("执行第 %d 条 SQL 失败: %w", i+1, exErr)
		}
		collect.appendSuccess(t, stmt, started, time.Now(), rowsAffected(ret))
	}
	if err := tx.Commit(); err != nil {
		collect.appendFailure(t, "(COMMIT)", time.Now(), err)
		n := collect.markRolledBack()
		collect.Summary = fmt.Sprintf("提交事务失败，事务已回滚，%d 条 SQL 均未生效", n)
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// markRolledBack 事务回滚后，把已记为成功的语句改为 rolled_back，返回回滚的语句数。
func (r *Result) markRolledBack() int {
	n := r.SuccessCount
	r.RolledBack = true
	r.SuccessCount = 0
	r.SuccessSQLs = nil
	for i := range r.Records {
		if strings.EqualFold(strings.TrimSpace(r.Records[i].Status), "success") {
			r.Records[i].Status = "rolled_back"
		}
	}
	for _, st := range r.targetHit {
		if st != nil {
			st.Success = 0
		}
	}
	return n
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
	return execSerial(ctx, db, t, stmts, collect, 0)
}

// execSerial 逐条执行；long>0（仅 PG）时按语句区分单条超时，并在 CREATE INDEX CONCURRENTLY 前清理同名无效索引。
func execSerial(ctx context.Context, db *sql.DB, t Target, stmts []string, collect *Result, long time.Duration) error {
	sid := querySessionID(ctx, db, EngineOf(t.DBType))
	total := len(stmts)
	for i, stmt := range stmts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.OnProgress != nil {
			t.OnProgress(i+1, total, sid)
		}
		if err := execOne(ctx, db, t, stmt, collect, long); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("执行第 %d 条 SQL 失败: %w", i+1, err)
		}
	}
	return nil
}

func querySessionID(ctx context.Context, db *sql.DB, engine string) int64 {
	if db == nil {
		return 0
	}
	q := ""
	switch engine {
	case "postgres":
		q = "SELECT pg_backend_pid()"
	case "mysql", "starrocks":
		q = "SELECT CONNECTION_ID()"
	default:
		return 0
	}
	var id int64
	if err := db.QueryRowContext(ctx, q).Scan(&id); err != nil || id <= 0 {
		return 0
	}
	return id
}

func execOne(ctx context.Context, db *sql.DB, t Target, stmt string, collect *Result, long time.Duration) error {
	stmtCtx := ctx
	per := stmtTimeout(stmt, long)
	if per > 0 {
		var cancel context.CancelFunc
		stmtCtx, cancel = context.WithTimeout(ctx, per)
		defer cancel()
	}
	if long > 0 {
		if err := cleanupInvalidCICIndex(stmtCtx, db, t, stmt, collect); err != nil {
			collect.appendFailure(t, stmt, time.Now(), err)
			return err
		}
	}
	started := time.Now()
	ret, err := db.ExecContext(stmtCtx, stmt)
	if err != nil {
		if per > 0 && errors.Is(stmtCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			err = fmt.Errorf("%w（超过单条执行超时 %s%s）", err, per, longTimeoutHint(stmt))
		}
		collect.appendFailure(t, stmt, started, err)
		return err
	}
	collect.appendSuccess(t, stmt, started, time.Now(), rowsAffected(ret))
	return nil
}

func longTimeoutHint(stmt string) string {
	if IsLongRunningPGStatement(stmt) {
		return "，可调整全局参数 " + store.ArgTicketDDLLongStmtTimeoutMinutes
	}
	return ""
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
	db.SetMaxIdleConns(0)
	return db, nil
}

func openPostgres(t Target, lifetime time.Duration) (*sql.DB, error) {
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
	db.SetConnMaxLifetime(lifetime)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
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
