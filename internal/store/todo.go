package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 待办来源
const (
	TodoSourceAlert      = "alert"
	TodoSourceTicket     = "ticket"
	TodoSourceChange     = "change"
	TodoSourceInspection = "inspection"
	TodoSourceManual     = "manual"
)

// 待办状态；TodoStatusOverdue 仅用于展示与筛选（未闭环且已过计划完成时间），不落库。
const (
	TodoStatusPending   = "pending"
	TodoStatusFollowing = "following"
	TodoStatusHandover  = "handover"
	TodoStatusClosed    = "closed"
	TodoStatusOverdue   = "overdue"
)

// 待办页签
const (
	TodoTabMine    = "mine"
	TodoTabFollow  = "follow"
	TodoTabCreated = "created"
	TodoTabTeam    = "team"
	TodoTabClosed  = "closed"
	TodoTabWarning = "warning"
)

// TodoWarnWindow 超期预警：已超期，或距计划完成时间不足该时长。
const TodoWarnWindow = 48 * time.Hour

var TodoTabs = []string{TodoTabMine, TodoTabFollow, TodoTabCreated, TodoTabTeam, TodoTabClosed, TodoTabWarning}

type Todo struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id,omitempty"`
	Title          string   `json:"title"`
	Source         string   `json:"source"`
	SourceRef      string   `json:"source_ref"`
	Owner          string   `json:"owner"`
	Followers      []string `json:"followers"`
	PlanAt         string   `json:"plan_at"`
	LatestProgress string   `json:"latest_progress"`
	NextAction     string   `json:"next_action"`
	Status         string   `json:"status"`
	ClosedAt       string   `json:"closed_at"`
	Description    string   `json:"description"`
	CreatedBy      string   `json:"created_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	// 以下由 DecorateTodo 计算
	Overdue       bool   `json:"overdue"`
	DueSoon       bool   `json:"due_soon"`
	DisplayStatus string `json:"display_status"`
	// 子任务统计，由 SummarizeSubtasks / 列表接口填充
	SubtaskTotal int `json:"subtask_total"`
	SubtaskDone  int `json:"subtask_done"`
}

// 子任务状态
const (
	SubtaskPending = "pending"
	SubtaskDoing   = "doing"
	SubtaskDone    = "done"
)

type TodoSubtask struct {
	ID          string `json:"id"`
	TodoID      string `json:"todo_id"`
	Seq         int    `json:"seq"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	PlanAt      string `json:"plan_at"`
	Status      string `json:"status"`
	Progress    string `json:"progress"`
	DoneAt      string `json:"done_at"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TodoLog struct {
	ID         string `json:"id"`
	TodoID     string `json:"todo_id"`
	SubtaskID  string `json:"subtask_id"`
	Action     string `json:"action"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Content    string `json:"content"`
	Operator   string `json:"operator"`
	CreatedAt  string `json:"created_at"`
}

func (d *DB) ensureTodoTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_todos (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT 'manual',
			source_ref TEXT NOT NULL DEFAULT '',
			owner TEXT NOT NULL DEFAULT '',
			followers TEXT NOT NULL DEFAULT '',
			plan_at TEXT NOT NULL DEFAULT '',
			latest_progress TEXT NOT NULL DEFAULT '',
			next_action TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			closed_at TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_todo_logs (
			id TEXT PRIMARY KEY,
			todo_id TEXT NOT NULL,
			subtask_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_logs_todo ON tbl_todo_logs(todo_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_todo_subtasks (
			id TEXT PRIMARY KEY,
			todo_id TEXT NOT NULL,
			seq INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL DEFAULT '',
			owner TEXT NOT NULL DEFAULT '',
			plan_at TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			progress TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			done_at TEXT NOT NULL DEFAULT '',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_subtasks_todo ON tbl_todo_subtasks(todo_id)`,
		`CREATE TABLE IF NOT EXISTS tbl_todo_images (
			id TEXT PRIMARY KEY,
			todo_id TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL DEFAULT '',
			ref_id TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			data BYTEA NOT NULL,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_todo_images_todo ON tbl_todo_images(todo_id, scope, ref_id)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func ValidTodoSource(s string) bool {
	switch s {
	case TodoSourceAlert, TodoSourceTicket, TodoSourceChange, TodoSourceInspection, TodoSourceManual:
		return true
	}
	return false
}

func ValidTodoStatus(s string) bool {
	switch s {
	case TodoStatusPending, TodoStatusFollowing, TodoStatusHandover, TodoStatusClosed:
		return true
	}
	return false
}

// ParseTodoTime 接受 RFC3339 或「2006-01-02 15:04[:05]」（按本地时区）。
func ParseTodoTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("时间格式无效: %s", s)
}

// NormalizeTodo 去空白、去重跟进人并校验必填项。
func NormalizeTodo(t *Todo) error {
	t.Title = strings.TrimSpace(t.Title)
	t.Source = strings.TrimSpace(t.Source)
	t.SourceRef = strings.TrimSpace(t.SourceRef)
	t.Owner = strings.TrimSpace(t.Owner)
	t.PlanAt = strings.TrimSpace(t.PlanAt)
	t.Followers = normalizeFollowers(t.Followers)
	if t.Source == "" {
		t.Source = TodoSourceManual
	}
	if t.Status == "" {
		t.Status = TodoStatusPending
	}
	switch {
	case t.Title == "":
		return errors.New("事项名称必填")
	case !ValidTodoSource(t.Source):
		return fmt.Errorf("来源无效: %s", t.Source)
	case t.Owner == "":
		return errors.New("负责人必填")
	case t.PlanAt == "":
		return errors.New("计划完成时间必填")
	case !ValidTodoStatus(t.Status):
		return fmt.Errorf("状态无效: %s", t.Status)
	}
	pt, err := ParseTodoTime(t.PlanAt)
	if err != nil {
		return err
	}
	// 统一存 UTC，字符串排序即时间排序
	t.PlanAt = pt.UTC().Format(time.RFC3339)
	return nil
}

func normalizeFollowers(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range in {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func splitFollowers(s string) []string {
	return normalizeFollowers(strings.Split(s, ","))
}

// TodoIsOverdue 未闭环且已过计划完成时间。
func TodoIsOverdue(t Todo, now time.Time) bool {
	if t.Status == TodoStatusClosed || t.PlanAt == "" {
		return false
	}
	pt, err := ParseTodoTime(t.PlanAt)
	return err == nil && now.After(pt)
}

// TodoIsDueSoon 未闭环、未超期且距计划完成时间不足 TodoWarnWindow。
func TodoIsDueSoon(t Todo, now time.Time) bool {
	if t.Status == TodoStatusClosed || t.PlanAt == "" {
		return false
	}
	pt, err := ParseTodoTime(t.PlanAt)
	return err == nil && !now.After(pt) && pt.Sub(now) <= TodoWarnWindow
}

// DecorateTodo 填充超期标记与展示状态。
func DecorateTodo(t *Todo, now time.Time) {
	t.Overdue = TodoIsOverdue(*t, now)
	t.DueSoon = TodoIsDueSoon(*t, now)
	t.DisplayStatus = t.Status
	if t.Overdue {
		t.DisplayStatus = TodoStatusOverdue
	}
}

// TodoRelated 用户是否为该事项的负责人、跟进人或发起人。
func TodoRelated(t Todo, user string) bool {
	if user == "" {
		return false
	}
	if t.Owner == user || t.CreatedBy == user {
		return true
	}
	for _, f := range t.Followers {
		if f == user {
			return true
		}
	}
	return false
}

// TodoInTab 页签归属：我的待办=我负责未闭环；待我跟进=我是跟进人未闭环；我发起的=我创建（含已闭环）；
// 团队待办=全部未闭环；已闭环=全部已闭环；超期预警=未闭环且已超期或 48 小时内到期。
func TodoInTab(t Todo, tab, user string, now time.Time) bool {
	open := t.Status != TodoStatusClosed
	switch tab {
	case TodoTabMine:
		return open && t.Owner == user
	case TodoTabFollow:
		if !open {
			return false
		}
		for _, f := range t.Followers {
			if f == user {
				return true
			}
		}
		return false
	case TodoTabCreated:
		return t.CreatedBy == user
	case TodoTabTeam:
		return open
	case TodoTabClosed:
		return !open
	case TodoTabWarning:
		return TodoIsOverdue(t, now) || TodoIsDueSoon(t, now)
	}
	return false
}

// TodoFilter 列表筛选条件；空值不过滤。Status 可为 overdue。
type TodoFilter struct {
	Tab     string
	Keyword string
	Source  string
	Status  string
	Owner   string
}

// FilterTodos 按页签与条件筛选并排序：超期预警按计划完成时间升序，已闭环按闭环时间倒序，其余按更新时间倒序。
func FilterTodos(list []Todo, f TodoFilter, user string, now time.Time) []Todo {
	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	out := []Todo{}
	for _, t := range list {
		DecorateTodo(&t, now)
		if f.Tab != "" && !TodoInTab(t, f.Tab, user, now) {
			continue
		}
		if f.Source != "" && t.Source != f.Source {
			continue
		}
		if f.Status != "" && t.DisplayStatus != f.Status {
			continue
		}
		if f.Owner != "" && t.Owner != f.Owner {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(strings.Join([]string{t.Title, t.SourceRef, t.LatestProgress, t.NextAction, t.Description, t.Owner, strings.Join(t.Followers, ",")}, " "))
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, t)
	}
	switch f.Tab {
	case TodoTabWarning:
		sort.SliceStable(out, func(i, j int) bool { return out[i].PlanAt < out[j].PlanAt })
	case TodoTabClosed:
		sort.SliceStable(out, func(i, j int) bool { return out[i].ClosedAt > out[j].ClosedAt })
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	}
	return out
}

// TodoTabCounts 各页签条数（不含关键字等筛选）。
func TodoTabCounts(list []Todo, user string, now time.Time) map[string]int {
	out := map[string]int{}
	for _, tab := range TodoTabs {
		out[tab] = 0
	}
	for _, t := range list {
		for _, tab := range TodoTabs {
			if TodoInTab(t, tab, user, now) {
				out[tab]++
			}
		}
	}
	return out
}

func ValidSubtaskStatus(s string) bool {
	switch s {
	case SubtaskPending, SubtaskDoing, SubtaskDone:
		return true
	}
	return false
}

// NormalizeSubtask 名称必填；负责人为空时沿用事项负责人；计划完成时间可选，统一存 UTC。
func NormalizeSubtask(s *TodoSubtask, defaultOwner string) error {
	s.Title = strings.TrimSpace(s.Title)
	s.Description = strings.TrimSpace(s.Description)
	s.Owner = strings.TrimSpace(s.Owner)
	s.PlanAt = strings.TrimSpace(s.PlanAt)
	if s.Owner == "" {
		s.Owner = strings.TrimSpace(defaultOwner)
	}
	if s.Status == "" {
		s.Status = SubtaskPending
	}
	if s.Title == "" {
		return errors.New("子任务名称必填")
	}
	if !ValidSubtaskStatus(s.Status) {
		return fmt.Errorf("子任务状态无效: %s", s.Status)
	}
	if s.PlanAt != "" {
		pt, err := ParseTodoTime(s.PlanAt)
		if err != nil {
			return err
		}
		s.PlanAt = pt.UTC().Format(time.RFC3339)
	}
	return nil
}

// ApplySubtaskProgress 更新子任务进展；未指定状态时待处理转为进行中。转为已完成时记录完成时间，退回则清空。
func ApplySubtaskProgress(s *TodoSubtask, status, progress string, now time.Time) error {
	progress = strings.TrimSpace(progress)
	if progress == "" {
		return errors.New("进展说明必填")
	}
	status = strings.TrimSpace(status)
	switch {
	case status == "" && s.Status == SubtaskPending:
		status = SubtaskDoing
	case status == "":
		status = s.Status
	case !ValidSubtaskStatus(status):
		return fmt.Errorf("子任务状态无效: %s", status)
	}
	if status == SubtaskDone {
		if s.Status != SubtaskDone || s.DoneAt == "" {
			s.DoneAt = now.UTC().Format(time.RFC3339)
		}
	} else {
		s.DoneAt = ""
	}
	s.Status = status
	s.Progress = progress
	return nil
}

// CountSubtasks 返回子任务总数与已完成数。
func CountSubtasks(subs []TodoSubtask) (total, done int) {
	for _, s := range subs {
		if s.Status == SubtaskDone {
			done++
		}
	}
	return len(subs), done
}

// SummarizeSubtasks 由子任务汇总事项的最新进展与下一步动作：
// 最新进展取最近一次有进展的子任务（均无进展时保留原值）；下一步动作为未完成子任务按顺序编号。
func SummarizeSubtasks(t *Todo, subs []TodoSubtask) {
	sorted := append([]TodoSubtask(nil), subs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	var latest *TodoSubtask
	var open []string
	for i := range sorted {
		s := &sorted[i]
		if s.Progress != "" && (latest == nil || s.UpdatedAt > latest.UpdatedAt) {
			latest = s
		}
		if s.Status != SubtaskDone {
			open = append(open, fmt.Sprintf("%d、%s", len(open)+1, s.Title))
		}
	}
	if latest != nil {
		t.LatestProgress = "【" + latest.Title + "】" + latest.Progress
	}
	t.NextAction = strings.Join(open, "\n")
	t.SubtaskTotal, t.SubtaskDone = CountSubtasks(subs)
}

// MarkTodoFollowing 子任务有进展时，待处理 / 待交接的事项转为跟进中。
func MarkTodoFollowing(t *Todo) {
	if t.Status == TodoStatusPending || t.Status == TodoStatusHandover {
		t.Status = TodoStatusFollowing
	}
}

// HandoverTodo 转交负责人，状态置为待交接；原负责人自动加入跟进人。
func HandoverTodo(t *Todo, newOwner string) error {
	newOwner = strings.TrimSpace(newOwner)
	if t.Status == TodoStatusClosed {
		return errors.New("事项已闭环，不可转交")
	}
	if newOwner == "" {
		return errors.New("请选择新负责人")
	}
	if newOwner == t.Owner {
		return errors.New("新负责人与当前负责人相同")
	}
	old := t.Owner
	t.Owner = newOwner
	t.Status = TodoStatusHandover
	t.Followers = normalizeFollowers(append(t.Followers, old))
	return nil
}

// MaybeAutoCloseTodo 子任务全部完成后自动闭环；无子任务或尚有未完成项时不处理。已闭环则幂等跳过。
// 须在 SummarizeSubtasks 填好 SubtaskTotal / SubtaskDone 之后调用。闭环不覆盖 latest_progress。
func MaybeAutoCloseTodo(t *Todo, now time.Time) (bool, error) {
	if t.Status == TodoStatusClosed {
		return false, nil
	}
	if t.SubtaskTotal == 0 || t.SubtaskDone != t.SubtaskTotal {
		return false, nil
	}
	if err := CloseTodo(t, "", 0, now); err != nil {
		return false, err
	}
	return true, nil
}

// CloseTodo 闭环；open 为未完成子任务数，须为 0。note 非空时作为最新进展。
func CloseTodo(t *Todo, note string, open int, now time.Time) error {
	if t.Status == TodoStatusClosed {
		return errors.New("事项已闭环")
	}
	if open > 0 {
		return fmt.Errorf("还有 %d 个子任务未完成，不能闭环", open)
	}
	if n := strings.TrimSpace(note); n != "" {
		t.LatestProgress = n
	}
	t.Status = TodoStatusClosed
	t.ClosedAt = now.UTC().Format(time.RFC3339)
	return nil
}

// ReopenTodo 重新打开已闭环事项，状态回到跟进中。
func ReopenTodo(t *Todo) error {
	if t.Status != TodoStatusClosed {
		return errors.New("事项未闭环")
	}
	t.Status = TodoStatusFollowing
	t.ClosedAt = ""
	return nil
}

const todoCols = `id, title, source, source_ref, owner, followers, plan_at, latest_progress, next_action, status, closed_at, description, created_by, created_at, updated_at`

func scanTodo(sc interface{ Scan(dest ...any) error }) (*Todo, error) {
	var t Todo
	var followers string
	if err := sc.Scan(&t.ID, &t.Title, &t.Source, &t.SourceRef, &t.Owner, &followers, &t.PlanAt, &t.LatestProgress,
		&t.NextAction, &t.Status, &t.ClosedAt, &t.Description, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Followers = splitFollowers(followers)
	return &t, nil
}

func (d *DB) ListTodos() ([]Todo, error) {
	return d.ListTodosInTenant("")
}

func (d *DB) ListTodosInTenant(tenantID string) ([]Todo, error) {
	q := `SELECT ` + todoCols + ` FROM tbl_todos WHERE 1=1`
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	q += ` ORDER BY updated_at DESC LIMIT 5000`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (d *DB) GetTodo(id string) (*Todo, error) {
	t, err := scanTodo(d.QueryRow(`SELECT `+todoCols+` FROM tbl_todos WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (d *DB) GetTodoInTenant(id, tenantID string) (*Todo, error) {
	t, err := d.GetTodo(id)
	if err != nil || t == nil {
		return t, err
	}
	ok, err := d.rowVisibleInTenant("tbl_todos", id, tenantID)
	if err != nil || !ok {
		return nil, err
	}
	t.TenantID = strings.TrimSpace(tenantID)
	return t, nil
}

func (d *DB) CreateTodo(t *Todo) error {
	if err := NormalizeTodo(t); err != nil {
		return err
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_todos (`+todoCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Source, t.SourceRef, t.Owner, strings.Join(t.Followers, ","), t.PlanAt, t.LatestProgress,
		t.NextAction, t.Status, t.ClosedAt, t.Description, t.CreatedBy, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_todos", t.ID, t.TenantID)
}

// SaveTodo 整行更新（created_by / created_at 不变）。
func (d *DB) SaveTodo(t *Todo) error {
	if err := NormalizeTodo(t); err != nil {
		return err
	}
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_todos SET title=?, source=?, source_ref=?, owner=?, followers=?, plan_at=?, latest_progress=?,
		next_action=?, status=?, closed_at=?, description=?, updated_at=? WHERE id=?`,
		t.Title, t.Source, t.SourceRef, t.Owner, strings.Join(t.Followers, ","), t.PlanAt, t.LatestProgress,
		t.NextAction, t.Status, t.ClosedAt, t.Description, t.UpdatedAt, t.ID)
	return err
}

func (d *DB) DeleteTodo(id string) error {
	for _, tbl := range []string{"tbl_todo_logs", "tbl_todo_subtasks", "tbl_todo_images"} {
		if _, err := d.Exec(`DELETE FROM `+tbl+` WHERE todo_id=?`, id); err != nil {
			return err
		}
	}
	_, err := d.Exec(`DELETE FROM tbl_todos WHERE id=?`, id)
	return err
}

func (d *DB) AddTodoLog(l *TodoLog) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	if l.CreatedAt == "" {
		l.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(`INSERT INTO tbl_todo_logs (id, todo_id, subtask_id, action, from_status, to_status, content, operator, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, l.ID, l.TodoID, l.SubtaskID, l.Action, l.FromStatus, l.ToStatus, l.Content, l.Operator, l.CreatedAt)
	return err
}

func (d *DB) ListTodoLogs(todoID string) ([]TodoLog, error) {
	rows, err := d.Query(`SELECT id, todo_id, subtask_id, action, from_status, to_status, content, operator, created_at
		FROM tbl_todo_logs WHERE todo_id=? ORDER BY created_at DESC, id DESC`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoLog{}
	for rows.Next() {
		var l TodoLog
		if err := rows.Scan(&l.ID, &l.TodoID, &l.SubtaskID, &l.Action, &l.FromStatus, &l.ToStatus, &l.Content, &l.Operator, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

const subtaskCols = `id, todo_id, seq, title, description, owner, plan_at, status, progress, done_at, created_by, created_at, updated_at`

func scanSubtask(sc interface{ Scan(dest ...any) error }) (*TodoSubtask, error) {
	var s TodoSubtask
	if err := sc.Scan(&s.ID, &s.TodoID, &s.Seq, &s.Title, &s.Description, &s.Owner, &s.PlanAt, &s.Status, &s.Progress, &s.DoneAt,
		&s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) ListTodoSubtasks(todoID string) ([]TodoSubtask, error) {
	rows, err := d.Query(`SELECT `+subtaskCols+` FROM tbl_todo_subtasks WHERE todo_id=? ORDER BY seq, created_at`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoSubtask{}
	for rows.Next() {
		s, err := scanSubtask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (d *DB) GetTodoSubtask(todoID, id string) (*TodoSubtask, error) {
	s, err := scanSubtask(d.QueryRow(`SELECT `+subtaskCols+` FROM tbl_todo_subtasks WHERE todo_id=? AND id=?`, todoID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// CreateTodoSubtask 追加到事项末尾（seq = 当前最大值 + 1）。
func (d *DB) CreateTodoSubtask(s *TodoSubtask, defaultOwner string) error {
	if err := NormalizeSubtask(s, defaultOwner); err != nil {
		return err
	}
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	var maxSeq sql.NullInt64
	if err := d.QueryRow(`SELECT MAX(seq) FROM tbl_todo_subtasks WHERE todo_id=?`, s.TodoID).Scan(&maxSeq); err != nil {
		return err
	}
	s.Seq = int(maxSeq.Int64) + 1
	now := time.Now().UTC().Format(time.RFC3339)
	s.CreatedAt, s.UpdatedAt = now, now
	_, err := d.Exec(`INSERT INTO tbl_todo_subtasks (`+subtaskCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.TodoID, s.Seq, s.Title, s.Description, s.Owner, s.PlanAt, s.Status, s.Progress, s.DoneAt, s.CreatedBy, s.CreatedAt, s.UpdatedAt)
	return err
}

func (d *DB) SaveTodoSubtask(s *TodoSubtask, defaultOwner string) error {
	if err := NormalizeSubtask(s, defaultOwner); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_todo_subtasks SET title=?, description=?, owner=?, plan_at=?, status=?, progress=?, done_at=?, updated_at=?
		WHERE todo_id=? AND id=?`, s.Title, s.Description, s.Owner, s.PlanAt, s.Status, s.Progress, s.DoneAt, s.UpdatedAt, s.TodoID, s.ID)
	return err
}

func (d *DB) DeleteTodoSubtask(todoID, id string) error {
	_, err := d.Exec(`DELETE FROM tbl_todo_subtasks WHERE todo_id=? AND id=?`, todoID, id)
	return err
}

// TodoSubtaskStats 各事项子任务的 [总数, 已完成数]。
func (d *DB) TodoSubtaskStats() (map[string][2]int, error) {
	rows, err := d.Query(`SELECT todo_id, COUNT(*), SUM(CASE WHEN status=? THEN 1 ELSE 0 END)
		FROM tbl_todo_subtasks GROUP BY todo_id`, SubtaskDone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int{}
	for rows.Next() {
		var id string
		var total, done int64
		if err := rows.Scan(&id, &total, &done); err != nil {
			return nil, err
		}
		out[id] = [2]int{int(total), int(done)}
	}
	return out, rows.Err()
}
