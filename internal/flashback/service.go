package flashback

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

type FlashbackImpl struct {
	store Store
}

func New() *FlashbackImpl {
	return &FlashbackImpl{store: Store{}}
}

var flashbackCancel sync.Map

func flashbackTrack(id string, cancel context.CancelFunc) {
	id = strings.TrimSpace(id)
	if id == "" || cancel == nil {
		return
	}
	flashbackCancel.Store(id, cancel)
}

func flashbackUntrack(id string) {
	flashbackCancel.Delete(strings.TrimSpace(id))
}

// Stop 取消正在执行的闪回任务。
func (s *FlashbackImpl) Stop(id string) {
	id = strings.TrimSpace(id)
	if v, ok := flashbackCancel.LoadAndDelete(id); ok {
		if cancel, ok := v.(context.CancelFunc); ok && cancel != nil {
			cancel()
		}
	}
	if s == nil || id == "" {
		return
	}
	if runtime == nil || runtime.Store == nil {
		return
	}
	_ = s.store.UpdateStatus(context.Background(), id, store.StatusStopped, "执行已终止", "")
}

var (
	flashbackBootstrapOnce sync.Once
	flashbackBootstrapErr  error
)

func Bootstrap(ctx context.Context) error {
	flashbackBootstrapOnce.Do(func() {
		if runtime == nil || runtime.Store == nil {
			flashbackBootstrapErr = fmt.Errorf("flashback runtime 未初始化")
			return
		}
		n, err := runtime.Store.FailStuckFlashbacks("进程退出后任务未跑完（重启、部署或资源不足），无法续跑")
		if err != nil {
			fbLog("fail stuck running", err)
		} else if n > 0 {
			fbLog("reclaimed stuck tasks", n)
		}
	})
	return flashbackBootstrapErr
}

func (s *FlashbackImpl) ensureReady(ctx context.Context) error {
	return Bootstrap(ctx)
}

func (s *FlashbackImpl) Create(ctx context.Context, req *FlashbackTaskReq, createdBy string) (*store.FlashbackTask, error) {
	if err := Bootstrap(ctx); err != nil {
		return nil, err
	}
	req.normalize()
	ctx = WithActor(ctx, createdBy)
	pre, err := s.Precheck(ctx, req)
	if err != nil {
		return nil, err
	}
	if pre == nil || !pre.OK {
		msg := "预检查未通过"
		if pre != nil {
			for _, it := range pre.Items {
				if it.Status == flashbackCheckFailed {
					msg = it.Name + ": " + it.Message
					break
				}
			}
		}
		return nil, fmt.Errorf("%s", msg)
	}
	target, _ := flashbackParseTime(req.TargetTime)
	var endPtr *time.Time
	if strings.TrimSpace(req.EndTime) != "" {
		et, _ := flashbackParseTime(req.EndTime)
		endPtr = &et
	}
	kind := strings.ToLower(strings.TrimSpace(req.OutputKind))
	if kind == "" {
		kind = OutputFlashback
	}
	reviewer := flashbackResolveReviewer(req)
	if reviewer == "" {
		return nil, fmt.Errorf("审核人必填")
	}
	taskID := NewID()
	issue := &store.Issue{
		IssueType:   store.IssueTypeFlashback,
		Type:        store.IssueTypeFlashback,
		InstanceID:  req.InstanceID,
		DBName:      strings.TrimSpace(req.Database),
		SQLContent:  flashbackIssueSQL(taskID, req, kind),
		Description: fmt.Sprintf("数据闪回 %s.%s", strings.TrimSpace(req.Database), flashbackTablesBrief(req.Tables)),
		Submitter:   createdBy,
		AssignedTo:  reviewer,
	}
	if err := runtime.Store.CreateIssue(issue); err != nil {
		return nil, err
	}
	go notify.IssueCreated(runtime.Store, runtime.Secret, issue)
	row := &TaskRow{
		ID: taskID, Host: pre.Host, Port: pre.Port, InstanceID: req.InstanceID,
		DatabaseName: strings.TrimSpace(req.Database), Tables: flashbackTablesJSON(req.Tables),
		TargetTime: target, EndTime: endPtr, StartXID: req.StartXID, StopXID: req.StopXID,
		StartFile: strings.TrimSpace(req.StartFile), StartPos: req.StartPos,
		StopFile: strings.TrimSpace(req.StopFile), StopPos: req.StopPos,
		SQLType: strings.TrimSpace(req.SQLType), OutputKind: kind, Status: StatusPending,
		CreatedBy: createdBy, Reviewer: reviewer, IssueID: issue.ID, ApprovalStatus: store.StatusPending,
	}
	if err := s.store.InsertTask(ctx, row); err != nil {
		return nil, err
	}
	_ = s.store.InsertLog(ctx, row.ID, "INFO", "任务已创建，等待审批通过后执行")
	s.persistTaskDictionary(ctx, row.ID, req)
	saved, _ := runtime.Store.GetFlashback(row.ID)
	return saved, nil
}

func flashbackResolveReviewer(req *FlashbackTaskReq) string {
	if req == nil {
		return ""
	}
	for _, v := range []string{req.Reviewer, req.Approver, req.AssignedTo} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	if runtime == nil || runtime.Store == nil {
		return ""
	}
	if v, _ := runtime.Store.GetGlobalArg("sync_default_approver"); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	v, _ := runtime.Store.GetGlobalArg("migration_approver_user_ids")
	if i := strings.Index(v, ","); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func flashbackTablesBrief(tables []string) string {
	if len(tables) == 0 {
		return "整库"
	}
	return strings.Join(tables, ",")
}

func flashbackIssueSQL(taskID string, req *FlashbackTaskReq, kind string) string {
	end := strings.TrimSpace(req.EndTime)
	if end == "" {
		end = "现在"
	}
	return fmt.Sprintf("flashback %s\n库 %s 表 %s\n时间窗 %s → %s\n输出 %s",
		taskID, strings.TrimSpace(req.Database), flashbackTablesBrief(req.Tables),
		firstNonEmpty(req.StartTime, req.TargetTime), end, kind)
}

var flashbackStarting sync.Map

func (s *FlashbackImpl) StartApproved(ctx context.Context, id string) error {
	if err := Bootstrap(ctx); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	row, err := s.store.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("任务不存在")
	}
	if row.Status == StatusRunning || row.Status == StatusSucceeded {
		return nil
	}
	if row.Status == StatusCancelled || row.Status == store.StatusCanceled || row.Status == store.StatusRejected {
		return fmt.Errorf("任务已取消或驳回")
	}
	if !flashbackApproved(row) {
		return fmt.Errorf("闪回任务须审批通过后才能执行")
	}
	if _, loaded := flashbackStarting.LoadOrStore(id, true); loaded {
		return nil
	}
	_ = s.store.InsertLog(ctx, row.ID, "INFO", "审批已通过，开始执行")
	go func() {
		defer flashbackStarting.Delete(id)
		ctx, cancel := context.WithCancel(context.Background())
		flashbackTrack(id, cancel)
		defer func() {
			cancel()
			flashbackUntrack(id)
		}()
		s.runTask(ctx, id)
	}()
	return nil
}

func (s *FlashbackImpl) SyncAfterIssue(ctx context.Context, issueID, issueStatus string) error {
	if runtime == nil || runtime.Store == nil {
		return nil
	}
	t, err := runtime.Store.GetFlashbackByIssueID(issueID)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	switch issueStatus {
	case store.StatusRejected:
		_ = s.store.InsertLog(ctx, t.ID, "INFO", "审批已驳回")
		return runtime.Store.UpdateFlashbackApproval(t.ID, store.StatusRejected, store.StatusRejected)
	case store.StatusCanceled:
		_ = s.store.InsertLog(ctx, t.ID, "INFO", "审批已撤销")
		return runtime.Store.UpdateFlashbackApproval(t.ID, store.StatusCanceled, store.StatusCanceled)
	case store.StatusApproved:
		next := t.Status
		if next == store.StatusRejected || next == store.StatusCanceled || next == StatusCancelled {
			next = StatusPending
		}
		if err := runtime.Store.UpdateFlashbackApproval(t.ID, next, store.StatusApproved); err != nil {
			return err
		}
		return s.StartApproved(ctx, t.ID)
	default:
		return nil
	}
}

func flashbackApproved(row *TaskRow) bool {
	if row == nil {
		return false
	}
	if row.ApprovalStatus == store.StatusPending || row.ApprovalStatus == store.StatusRejected || row.ApprovalStatus == store.StatusCanceled {
		return false
	}
	if row.ApprovalStatus == "" && row.IssueID == "" {
		return true
	}
	return row.ApprovalStatus == store.StatusApproved
}

func (s *FlashbackImpl) persistTaskDictionary(ctx context.Context, taskID string, req *FlashbackTaskReq) {
	if req == nil {
		return
	}
	if src := strings.TrimSpace(req.DictTaskID); src != "" {
		if err := flashbackCopyDictionaryFile(ctx, src, taskID); err != nil {
			_ = s.store.InsertLog(ctx, taskID, "WARN", "复用源任务字典失败，将在解析时现场导出: "+err.Error())
			return
		}
		_ = s.store.InsertLog(ctx, taskID, "INFO", "已从任务 "+src+" 加载数据字典快照")
		return
	}
	db, dom, _, err := flashbackConnectTarget(ctx, req.InstanceID, req.Database)
	if err != nil {
		_ = s.store.InsertLog(ctx, taskID, "WARN", "创建时导出字典失败，将在解析时重试: "+err.Error())
		return
	}
	defer db.Close()
	if dom != nil && flashbackIsMySQL(dom.DbType) {
		dict, err := flashbackLoadMySQLDictionary(ctx, db, req.Database, req.Tables)
		if err != nil {
			_ = s.store.InsertLog(ctx, taskID, "WARN", "创建时导出 MySQL 字典失败: "+err.Error())
			return
		}
		if err := flashbackSaveMySQLDictionaryFile(flashbackDictPath(ctx, taskID), dict); err != nil {
			_ = s.store.InsertLog(ctx, taskID, "WARN", "保存 MySQL 字典快照失败: "+err.Error())
			return
		}
		_ = s.store.InsertLog(ctx, taskID, "INFO", fmt.Sprintf("已保存 MySQL 数据字典快照（%d 张表）", len(dict.Wanted)))
		return
	}
	dict, err := flashbackLoadDictionary(ctx, db, req.Database, req.Tables)
	if err != nil {
		_ = s.store.InsertLog(ctx, taskID, "WARN", "创建时导出字典失败: "+err.Error())
		return
	}
	if err := flashbackSaveDictionaryFile(flashbackDictPath(ctx, taskID), dict); err != nil {
		_ = s.store.InsertLog(ctx, taskID, "WARN", "保存字典快照失败: "+err.Error())
		return
	}
	_ = s.store.InsertLog(ctx, taskID, "INFO", fmt.Sprintf("已保存数据字典快照（%d 张表）", len(dict.Wanted)))
}

func (s *FlashbackImpl) SubmitDML(ctx context.Context, id, reviewer, submitter, desc string) (*store.Issue, error) {
	if err := Bootstrap(ctx); err != nil {
		return nil, err
	}
	row, err := s.store.GetTask(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if row.Status != StatusSucceeded {
		return nil, fmt.Errorf("任务未成功完成，当前状态 %s", row.Status)
	}
	kind, ops := flashbackSQLPreviewFilter(row.OutputKind, row.SQLType, "", "")
	stmts, err := s.store.ListAllSQLStatements(ctx, row.ID, kind, ops)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, fmt.Errorf("没有可提交的 SQL")
	}
	var dml []string
	for _, stmt := range stmts {
		if flashbackSQLIsDDLStatement(stmt) {
			continue
		}
		dml = append(dml, stmt)
	}
	if len(dml) == 0 {
		return nil, fmt.Errorf("闪回 DDL SQL 一期不走 DML 工单，请人工审核后通过 DDL 工单执行")
	}
	if strings.TrimSpace(desc) == "" {
		desc = fmt.Sprintf("闪回任务 %s 生成的 %s SQL", row.ID, kind)
	}
	rev := strings.TrimSpace(reviewer)
	if rev == "" {
		rev = strings.TrimSpace(row.Reviewer)
	}
	it := &store.Issue{
		IssueType:   store.IssueTypeDML,
		InstanceID:  row.InstanceID,
		DBName:      row.DatabaseName,
		SQLContent:  strings.Join(dml, "\n"),
		Description: desc,
		Submitter:   submitter,
		AssignedTo:  rev,
	}
	if err := runtime.Store.CreateIssue(it); err != nil {
		return nil, err
	}
	go notify.IssueCreated(runtime.Store, runtime.Secret, it)
	_ = s.store.SetDMLTicketID(ctx, row.ID, it.ID)
	_ = s.store.InsertLog(ctx, row.ID, "INFO", "已提交 DML 工单 "+it.TicketNo)
	return it, nil
}
