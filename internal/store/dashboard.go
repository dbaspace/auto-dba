package store

import (
	"strconv"
	"strings"
	"time"
)

type DashboardFilter struct {
	Q         string
	IssueType string
	Status    string
	DBType    string
	IDC       string
	IDCScope  string
	DateFrom  string
	DateTo    string
	Viewer    string
}

type StatCount struct {
	Status    string `json:"status,omitempty"`
	IssueType string `json:"issue_type,omitempty"`
	IDC       string `json:"idc,omitempty"`
	IDCScope  string `json:"idc_scope,omitempty"`
	Date      string `json:"date,omitempty"`
	Count     int    `json:"count"`
}

type CreatorCount struct {
	Username    string      `json:"username"`
	UserID      string      `json:"user_id"`
	Count       int         `json:"count"`
	ByIssueType []StatCount `json:"by_issue_type,omitempty"`
}

type DashboardStats struct {
	Total           int            `json:"total"`
	MyTotal         int            `json:"my_total"`
	TodayCount      int            `json:"today_count"`
	Last7dCount     int            `json:"last_7d_count"`
	CDCTotal        int            `json:"cdc_total"`
	CDCWaitApproval int            `json:"cdc_wait_approval"`
	CDCPassed       int            `json:"cdc_passed"`
	ByStatus        []StatCount    `json:"by_status"`
	ByIssueType     []StatCount    `json:"by_issue_type"`
	ByIDC           []StatCount    `json:"by_idc"`
	ByIDCScope      []StatCount    `json:"by_idc_scope"`
	Trend           []StatCount    `json:"trend"`
	TopCreators     []CreatorCount `json:"top_creators"`
}

type DashboardTodo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedBy   string `json:"created_by"`
	CreatorName string `json:"creator_name"`
	Status      string `json:"status"`
	IssueType   string `json:"issue_type"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Source      string `json:"source,omitempty"`
}

type DashboardActivity struct {
	At        string `json:"at"`
	EventType string `json:"event_type"`
	IssueID   string `json:"issue_id"`
	IssueName string `json:"issue_name"`
	Status    string `json:"status"`
	ActorName string `json:"actor_name,omitempty"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
	IssueType string `json:"issue_type,omitempty"`
}

type issueDashRow struct {
	Issue
	IDC    string
	DBType string
}

func idcScopeOf(idc string) string {
	s := strings.ToLower(strings.TrimSpace(idc))
	if s == "" || s == "default" || s == "unknown" {
		return "unknown"
	}
	if strings.Contains(s, "test") || strings.Contains(s, "dev") || s == "uat" {
		return "test"
	}
	return "prod"
}

func parseIssueTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (d *DB) listIssueDashRows(f DashboardFilter) ([]issueDashRow, error) {
	q := `SELECT i.id, i.ticket_no, i.issue_type, i.instance_id, i.database_id, i.db_name, i.sql_content, i.description,
		i.status, i.submitter, i.assigned_to, i.reject_reason, i.need_rollback, i.scheduled_at, i.created_at, i.updated_at,
		COALESCE(inst.idc,''), COALESCE(inst.db_type,'')
		FROM tbl_issues i LEFT JOIN tbl_instances inst ON inst.id = i.instance_id WHERE 1=1`
	var args []any
	if f.IssueType != "" {
		q += ` AND i.issue_type=?`
		args = append(args, f.IssueType)
	}
	if f.Status != "" {
		q += ` AND i.status=?`
		args = append(args, f.Status)
	}
	if f.DBType != "" {
		q += ` AND inst.db_type=?`
		args = append(args, f.DBType)
	}
	if f.IDC != "" {
		q += ` AND inst.idc=?`
		args = append(args, f.IDC)
	}
	if f.Q != "" {
		like := "%" + f.Q + "%"
		q += ` AND (i.ticket_no LIKE ? OR i.db_name LIKE ? OR i.description LIKE ? OR i.submitter LIKE ?)`
		args = append(args, like, like, like, like)
	}
	q += ` ORDER BY i.created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []issueDashRow
	for rows.Next() {
		var r issueDashRow
		if err := rows.Scan(
			&r.ID, &r.TicketNo, &r.IssueType, &r.InstanceID, &r.DatabaseID, &r.DBName, &r.SQLContent, &r.Description,
			&r.Status, &r.Submitter, &r.AssignedTo, &r.RejectReason, &r.NeedRollback, &r.ScheduledAt, &r.CreatedAt, &r.UpdatedAt,
			&r.IDC, &r.DBType,
		); err != nil {
			return nil, err
		}
		r.Type = r.IssueType
		if f.IDCScope != "" && idcScopeOf(r.IDC) != f.IDCScope {
			continue
		}
		if f.DateFrom != "" || f.DateTo != "" {
			ct := parseIssueTime(r.CreatedAt)
			if !ct.IsZero() {
				day := ct.UTC().Format("2006-01-02")
				if f.DateFrom != "" && day < f.DateFrom {
					continue
				}
				if f.DateTo != "" && day > f.DateTo {
					continue
				}
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) DashboardStats(f DashboardFilter) (*DashboardStats, error) {
	rows, err := d.listIssueDashRows(f)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -6).Format("2006-01-02")
	st := &DashboardStats{
		ByStatus:    []StatCount{},
		ByIssueType: []StatCount{},
		ByIDC:       []StatCount{},
		ByIDCScope:  []StatCount{},
		Trend:       []StatCount{},
		TopCreators: []CreatorCount{},
	}
	byStatus := map[string]int{}
	byType := map[string]int{}
	byIDC := map[string]int{}
	byScope := map[string]int{}
	byDay := map[string]int{}
	creatorN := map[string]int{}
	creatorType := map[string]map[string]int{}
	for _, r := range rows {
		st.Total++
		if f.Viewer != "" && (r.Submitter == f.Viewer || r.AssignedTo == f.Viewer) {
			st.MyTotal++
		}
		day := ""
		if t := parseIssueTime(r.CreatedAt); !t.IsZero() {
			day = t.UTC().Format("2006-01-02")
			if day == today {
				st.TodayCount++
			}
			if day >= weekAgo {
				st.Last7dCount++
			}
			byDay[day]++
		}
		byStatus[r.Status]++
		byType[r.IssueType]++
		idc := r.IDC
		if idc == "" {
			idc = "unknown"
		}
		byIDC[idc]++
		byScope[idcScopeOf(r.IDC)]++
		if r.IssueType == IssueTypeCDC {
			st.CDCTotal++
			if r.Status == StatusPending {
				st.CDCWaitApproval++
			}
			if r.Status == StatusApproved || r.Status == StatusExecuted {
				st.CDCPassed++
			}
		}
		who := r.Submitter
		if who == "" {
			who = "-"
		}
		creatorN[who]++
		if creatorType[who] == nil {
			creatorType[who] = map[string]int{}
		}
		creatorType[who][r.IssueType]++
	}
	for k, n := range byStatus {
		st.ByStatus = append(st.ByStatus, StatCount{Status: k, Count: n})
	}
	for k, n := range byType {
		st.ByIssueType = append(st.ByIssueType, StatCount{IssueType: k, Count: n})
	}
	for k, n := range byIDC {
		st.ByIDC = append(st.ByIDC, StatCount{IDC: k, Count: n})
	}
	for k, n := range byScope {
		st.ByIDCScope = append(st.ByIDCScope, StatCount{IDCScope: k, Count: n})
	}
	for i := 29; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		st.Trend = append(st.Trend, StatCount{Date: day, Count: byDay[day]})
	}
	type pair struct {
		name string
		n    int
	}
	var ranked []pair
	for name, n := range creatorN {
		ranked = append(ranked, pair{name, n})
	}
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].n > ranked[i].n {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	if len(ranked) > 10 {
		ranked = ranked[:10]
	}
	for _, p := range ranked {
		c := CreatorCount{Username: p.name, UserID: p.name, Count: p.n}
		for t, n := range creatorType[p.name] {
			c.ByIssueType = append(c.ByIssueType, StatCount{IssueType: t, Count: n})
		}
		st.TopCreators = append(st.TopCreators, c)
	}
	return st, nil
}

func (d *DB) DashboardTodos(f DashboardFilter, viewer string, page, pageSize int) ([]DashboardTodo, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	rows, err := d.listIssueDashRows(f)
	if err != nil {
		return nil, 0, err
	}
	var todos []DashboardTodo
	for _, r := range rows {
		if r.Status != StatusPending {
			continue
		}
		if viewer != "" && !strings.EqualFold(r.AssignedTo, viewer) {
			continue
		}
		name := r.Description
		if name == "" {
			name = r.TicketNo + " " + r.DBName
		}
		todos = append(todos, DashboardTodo{
			ID: r.ID, Name: strings.TrimSpace(name), CreatedBy: r.Submitter, CreatorName: r.Submitter,
			Status: r.Status, IssueType: r.IssueType, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Source: "issue",
		})
	}
	migs, err := d.ListMigrations()
	if err != nil {
		return nil, 0, err
	}
	for _, m := range migs {
		if m.Status != StatusPending && m.Status != StatusInit {
			continue
		}
		if f.IssueType != "" && f.IssueType != "migration" {
			continue
		}
		todos = append(todos, DashboardTodo{
			ID: m.ID, Name: m.Name, CreatedBy: m.CreatedBy, CreatorName: m.CreatedBy,
			Status: StatusPending, IssueType: "migration", CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Source: "migration",
		})
	}
	total := len(todos)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return todos[start:end], total, nil
}

func (d *DB) DashboardActivities(f DashboardFilter, limit int) ([]DashboardActivity, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.listIssueDashRows(f)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -3)
	var list []DashboardActivity
	for _, r := range rows {
		name := r.TicketNo
		if r.Description != "" {
			name = r.TicketNo + " " + r.Description
		}
		ct := parseIssueTime(r.CreatedAt)
		ut := parseIssueTime(r.UpdatedAt)
		if !ct.IsZero() && !ct.Before(cutoff) {
			list = append(list, DashboardActivity{
				At: r.CreatedAt, EventType: "created", IssueID: r.ID, IssueName: name,
				Status: r.Status, ActorName: r.Submitter, Source: "issue", IssueType: r.IssueType,
				Message: r.Submitter + " 提交了 " + issueTypeLabel(r.IssueType) + " 工单 " + r.TicketNo,
			})
		}
		if !ut.IsZero() && !ut.Before(cutoff) && r.UpdatedAt != r.CreatedAt {
			list = append(list, DashboardActivity{
				At: r.UpdatedAt, EventType: "status_changed", IssueID: r.ID, IssueName: name,
				Status: r.Status, ActorName: r.AssignedTo, Source: "issue", IssueType: r.IssueType,
				Message: r.TicketNo + " 状态变更为 " + statusLabel(r.Status),
			})
		}
	}
	migs, err := d.ListMigrations()
	if err != nil {
		return nil, err
	}
	for _, m := range migs {
		if f.IssueType != "" && f.IssueType != "migration" {
			continue
		}
		ct := parseIssueTime(m.CreatedAt)
		ut := parseIssueTime(m.UpdatedAt)
		if !ct.IsZero() && !ct.Before(cutoff) {
			list = append(list, DashboardActivity{
				At: m.CreatedAt, EventType: "created", IssueID: m.ID, IssueName: m.Name,
				Status: m.Status, ActorName: m.CreatedBy, Source: "migration", IssueType: "migration",
				Message: m.CreatedBy + " 创建了数据迁移任务 " + m.Name,
			})
		}
		if !ut.IsZero() && !ut.Before(cutoff) && m.UpdatedAt != m.CreatedAt {
			list = append(list, DashboardActivity{
				At: m.UpdatedAt, EventType: "status_changed", IssueID: m.ID, IssueName: m.Name,
				Status: m.Status, Source: "migration", IssueType: "migration",
				Message: "数据迁移 " + m.Name + " 状态变更为 " + m.Status,
			})
		}
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].At > list[i].At {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func issueTypeLabel(t string) string {
	switch t {
	case IssueTypeDDL:
		return "DDL"
	case IssueTypeDML:
		return "DML"
	case IssueTypeDCL:
		return "权限申请"
	case IssueTypeDQL:
		return "数据导出"
	case IssueTypeKafka:
		return "Kafka"
	case IssueTypeCDC:
		return "CDC"
	case IssueTypeArchive:
		return "归档"
	case IssueTypeFlashback:
		return "闪回"
	case IssueTypeMigration:
		return "数据迁移"
	case IssueTypeMullang:
		return "多语模版"
	case IssueTypeCreateDB:
		return "新增数据库"
	default:
		return t
	}
}

func statusLabel(s string) string {
	switch s {
	case StatusPending:
		return "待审批"
	case StatusApproved:
		return "已通过"
	case StatusExecuted:
		return "已完成"
	case StatusRejected:
		return "已驳回"
	case StatusCanceled:
		return "已取消"
	case StatusFailed:
		return "失败"
	case StatusRunning:
		return "执行中"
	default:
		return s
	}
}

func ParsePage(current, pageSize string) (int, int) {
	p, _ := strconv.Atoi(current)
	s, _ := strconv.Atoi(pageSize)
	if p < 1 {
		p = 1
	}
	if s < 1 {
		s = 10
	}
	return p, s
}
