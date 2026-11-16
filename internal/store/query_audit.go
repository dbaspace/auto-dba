package store

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgconn"
)

const maxQueryAuditStatement = 8 * 1024

func TruncateQueryAuditStatement(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxQueryAuditStatement {
		return s
	}
	r := []rune(s)
	return string(r[:maxQueryAuditStatement]) + "…"
}

func (d *DB) CreateQueryAudit(it *QueryAudit) error {
	if it == nil {
		return nil
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	it.Kind = strings.TrimSpace(it.Kind)
	it.Username = strings.TrimSpace(it.Username)
	it.DisplayName = strings.TrimSpace(it.DisplayName)
	it.IP = strings.TrimSpace(it.IP)
	it.UserAgent = strings.TrimSpace(it.UserAgent)
	if len(it.UserAgent) > 512 {
		it.UserAgent = it.UserAgent[:512]
	}
	it.Statement = TruncateQueryAuditStatement(it.Statement)
	it.Message = TruncateQueryAuditStatement(it.Message)
	it.ClientType = strings.TrimSpace(it.ClientType)
	it.TokenID = strings.TrimSpace(it.TokenID)
	it.TenantID = strings.TrimSpace(it.TenantID)
	success := 0
	if it.Success {
		success = 1
	}
	_, err := d.Exec(
		`INSERT INTO tbl_query_audits(
			id, kind, user_id, username, display_name, ip, user_agent,
			instance_id, instance_name, db_type, target, statement,
			success, message, row_count, elapsed_ms, client_type, token_id, client_request_id, tenant_id, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Kind, it.UserID, it.Username, it.DisplayName, it.IP, it.UserAgent,
		it.InstanceID, it.InstanceName, it.DBType, it.Target, it.Statement,
		success, it.Message, it.RowCount, it.ElapsedMS, it.ClientType, it.TokenID, it.ClientRequestID, it.TenantID, it.CreatedAt,
	)
	if err != nil && it.ClientRequestID != "" && isUniqueViolation(err) {
		return nil
	}
	return err
}

// MarkQueryClientTimeout 把同一次查询改成网络超时。只改当前用户自己的记录，说明由调用方写死。
func (d *DB) MarkQueryClientTimeout(userID, username, clientRequestID, message string, elapsedMS int64) (int64, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if clientRequestID == "" {
		return 0, nil
	}
	res, err := d.Exec(
		`UPDATE tbl_query_audits SET success=0, message=?, elapsed_ms=?
		 WHERE client_request_id=? AND (user_id=? OR (COALESCE(user_id,'')='' AND username=? AND username<>''))`,
		message, elapsedMS, clientRequestID, strings.TrimSpace(userID), strings.TrimSpace(username),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const metaQueryAuditTenantScoped = "query_audit_tenant_scoped"

// detachInheritedQueryAuditTenants 旧审计没有记下真实租户，列上的默认租户不能当成隔离依据。
// 只清一次，之后按当前租户写入的记录保留。空租户不会出现在「我的历史」。
func (d *DB) detachInheritedQueryAuditTenants() error {
	if err := d.ensureSchemaMetaTable(); err != nil {
		return err
	}
	v, err := d.schemaMetaGet(metaQueryAuditTenantScoped)
	if err != nil {
		return err
	}
	if v == "1" {
		return nil
	}
	if _, err := d.Exec(`UPDATE tbl_query_audits SET tenant_id='' WHERE COALESCE(tenant_id,'')=?`, DefaultTenantID); err != nil {
		return err
	}
	return d.schemaMetaSet(metaQueryAuditTenantScoped, "1")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type QueryAuditFilter struct {
	Username   string
	Kind       string
	Success    *bool
	Q          string
	ClientType string
	DateFrom   string
	DateTo     string
	Page       int
	PageSize   int
}

// stripWorkflowTicketPrefix 把页面工单名 DQL-Txxxx 还原成审计里的工单号。
// 前缀必须是已知工单类型，其它关键字保持原样。
func stripWorkflowTicketPrefix(q string) string {
	q = strings.TrimSpace(q)
	i := strings.Index(q, "-")
	if i <= 0 || i >= len(q)-1 {
		return ""
	}
	if !ValidIssueType(strings.ToLower(q[:i])) {
		return ""
	}
	return strings.TrimSpace(q[i+1:])
}

func queryAuditKeywordLikes(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	likes := []string{"%" + q + "%"}
	if bare := stripWorkflowTicketPrefix(q); bare != "" && !strings.EqualFold(bare, q) {
		likes = append(likes, "%"+bare+"%")
	}
	return likes
}

func buildQueryAuditWhere(f QueryAuditFilter) (string, []any) {
	where := `WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(f.Username); s != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+s+"%")
	}
	if s := strings.ToLower(strings.TrimSpace(f.Kind)); s != "" {
		where += ` AND kind=?`
		args = append(args, s)
	}
	if f.Success != nil {
		v := 0
		if *f.Success {
			v = 1
		}
		where += ` AND success=?`
		args = append(args, v)
	}
	if s := strings.ToLower(strings.TrimSpace(f.ClientType)); s != "" {
		where += ` AND client_type=?`
		args = append(args, s)
	}
	if likes := queryAuditKeywordLikes(f.Q); len(likes) > 0 {
		var parts []string
		for _, like := range likes {
			parts = append(parts, `(statement LIKE ? OR target LIKE ? OR instance_name LIKE ?)`)
			args = append(args, like, like, like)
		}
		where += ` AND (` + strings.Join(parts, ` OR `) + `)`
	}
	if s := strings.TrimSpace(f.DateFrom); s != "" {
		where += ` AND created_at>=?`
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.DateTo); s != "" {
		where += ` AND created_at<=?`
		args = append(args, s)
	}
	return where, args
}

func (d *DB) ListQueryAudits(f QueryAuditFilter) ([]QueryAudit, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	where, args := buildQueryAuditWhere(f)
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_query_audits `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, kind, user_id, username, display_name, ip, user_agent,
		instance_id, instance_name, db_type, target, statement,
		success, message, row_count, elapsed_ms,
		COALESCE(client_type,''), COALESCE(token_id,''), created_at
		FROM tbl_query_audits ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []QueryAudit{}
	for rows.Next() {
		var it QueryAudit
		var success int
		if err := rows.Scan(
			&it.ID, &it.Kind, &it.UserID, &it.Username, &it.DisplayName, &it.IP, &it.UserAgent,
			&it.InstanceID, &it.InstanceName, &it.DBType, &it.Target, &it.Statement,
			&success, &it.Message, &it.RowCount, &it.ElapsedMS, &it.ClientType, &it.TokenID, &it.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		it.Success = success != 0
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// ListNotifyAuditsByTicket 按真实工单号取待审批通知审计，不带页面上的类型前缀。
func (d *DB) ListNotifyAuditsByTicket(ticketNo string, limit int) ([]QueryAudit, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	if ticketNo == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := d.Query(
		`SELECT id, kind, user_id, username, display_name, ip, user_agent,
			instance_id, instance_name, db_type, target, statement,
			success, message, row_count, elapsed_ms,
			COALESCE(client_type,''), COALESCE(token_id,''), created_at
		 FROM tbl_query_audits
		 WHERE kind='notify' AND instance_name=?
		 ORDER BY created_at DESC LIMIT ?`,
		ticketNo, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueryAudit{}
	for rows.Next() {
		var it QueryAudit
		var success int
		if err := rows.Scan(
			&it.ID, &it.Kind, &it.UserID, &it.Username, &it.DisplayName, &it.IP, &it.UserAgent,
			&it.InstanceID, &it.InstanceName, &it.DBType, &it.Target, &it.Statement,
			&success, &it.Message, &it.RowCount, &it.ElapsedMS, &it.ClientType, &it.TokenID, &it.CreatedAt,
		); err != nil {
			return nil, err
		}
		it.Success = success != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

// QueryAuditKindCount 是一类查询的次数。
type QueryAuditKindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// QueryAuditTrendPoint 是本地日历上的一天。
type QueryAuditTrendPoint struct {
	Date   string `json:"date"`
	Total  int    `json:"total"`
	Failed int    `json:"failed"`
}

// QueryAuditUserCount 是一个用户的查询次数。
type QueryAuditUserCount struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Count       int    `json:"count"`
}

// QueryAuditStats 是工作台近 N 天的查询审计汇总。范围与明细列表相同，不按租户再滤。
type QueryAuditStats struct {
	Total        int                    `json:"total"`
	Success      int                    `json:"success"`
	Failed       int                    `json:"failed"`
	SuccessRate  float64                `json:"success_rate"`
	AvgElapsedMS float64                `json:"avg_elapsed_ms"`
	Today        int                    `json:"today"`
	ByKind       []QueryAuditKindCount  `json:"by_kind"`
	Trend        []QueryAuditTrendPoint `json:"trend"`
	TopUsers     []QueryAuditUserCount  `json:"top_users"`
	SlowTop      []QueryAudit           `json:"slow_top"`
}

// QueryAuditStats 统计本地日历上的近 days 天，含当天。days 由调用方限制在 1–31。
func (d *DB) QueryAuditStats(days int) (*QueryAuditStats, error) {
	if days < 1 {
		days = 7
	}
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	from := start.UTC().Format(time.RFC3339)
	to := now.UTC().Format(time.RFC3339)
	out := &QueryAuditStats{
		ByKind:   []QueryAuditKindCount{},
		Trend:    []QueryAuditTrendPoint{},
		TopUsers: []QueryAuditUserCount{},
		SlowTop:  []QueryAudit{},
	}
	var totalN, elapsedSum, successN, failedN int64
	if err := d.QueryRow(
		`SELECT COUNT(1),
			COALESCE(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(elapsed_ms),0)
		 FROM tbl_query_audits WHERE created_at>=? AND created_at<=?`,
		from, to,
	).Scan(&totalN, &successN, &failedN, &elapsedSum); err != nil {
		return nil, err
	}
	out.Total = int(totalN)
	out.Success = int(successN)
	out.Failed = int(failedN)
	if out.Total > 0 {
		out.SuccessRate = float64(out.Success) / float64(out.Total)
		out.AvgElapsedMS = float64(elapsedSum) / float64(out.Total)
	}
	todayFrom := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UTC().Format(time.RFC3339)
	var todayN int64
	if err := d.QueryRow(
		`SELECT COUNT(1) FROM tbl_query_audits WHERE created_at>=? AND created_at<=?`,
		todayFrom, to,
	).Scan(&todayN); err != nil {
		return nil, err
	}
	out.Today = int(todayN)
	kindRows, err := d.Query(
		`SELECT kind, COUNT(1) FROM tbl_query_audits
		 WHERE created_at>=? AND created_at<=?
		 GROUP BY kind ORDER BY COUNT(1) DESC, kind`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer kindRows.Close()
	for kindRows.Next() {
		var row QueryAuditKindCount
		var n int64
		if err := kindRows.Scan(&row.Kind, &n); err != nil {
			return nil, err
		}
		row.Count = int(n)
		out.ByKind = append(out.ByKind, row)
	}
	if err := kindRows.Err(); err != nil {
		return nil, err
	}
	userRows, err := d.Query(
		`SELECT username, COALESCE(MAX(display_name),''), COUNT(1)
		 FROM tbl_query_audits
		 WHERE created_at>=? AND created_at<=?
		 GROUP BY username
		 ORDER BY COUNT(1) DESC, username
		 LIMIT 5`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()
	for userRows.Next() {
		var row QueryAuditUserCount
		var n int64
		if err := userRows.Scan(&row.Username, &row.DisplayName, &n); err != nil {
			return nil, err
		}
		row.Count = int(n)
		out.TopUsers = append(out.TopUsers, row)
	}
	if err := userRows.Err(); err != nil {
		return nil, err
	}
	slowRows, err := d.Query(
		`SELECT id, kind, username, target, statement, elapsed_ms, created_at
		 FROM tbl_query_audits
		 WHERE created_at>=? AND created_at<=?
		 ORDER BY elapsed_ms DESC, created_at DESC
		 LIMIT 5`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer slowRows.Close()
	for slowRows.Next() {
		var it QueryAudit
		if err := slowRows.Scan(&it.ID, &it.Kind, &it.Username, &it.Target, &it.Statement, &it.ElapsedMS, &it.CreatedAt); err != nil {
			return nil, err
		}
		out.SlowTop = append(out.SlowTop, it)
	}
	if err := slowRows.Err(); err != nil {
		return nil, err
	}
	trend, err := d.queryAuditTrend(start, now)
	if err != nil {
		return nil, err
	}
	out.Trend = trend
	return out, nil
}

func (d *DB) queryAuditTrend(start, end time.Time) ([]QueryAuditTrendPoint, error) {
	out := []QueryAuditTrendPoint{}
	to := end.UTC().Format(time.RFC3339)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		next := day.AddDate(0, 0, 1)
		var total, failed int64
		if err := d.QueryRow(
			`SELECT COUNT(1), COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0)
			 FROM tbl_query_audits
			 WHERE created_at>=? AND created_at<? AND created_at<=?`,
			day.UTC().Format(time.RFC3339), next.UTC().Format(time.RFC3339), to,
		).Scan(&total, &failed); err != nil {
			return nil, err
		}
		out = append(out, QueryAuditTrendPoint{Date: day.Format("2006-01-02"), Total: int(total), Failed: int(failed)})
	}
	return out, nil
}
