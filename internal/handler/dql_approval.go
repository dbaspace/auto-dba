package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

const (
	dqlPhaseAssignee = "assignee"
	dqlPhaseDBA      = "dba"
	dqlPhaseSecurity = "security"
	dqlPhaseDone     = "done"
)

type dqlApprovalExtra struct {
	NeedSensitive      bool   `json:"need_sensitive_data"`
	DBAAssigned        string `json:"dba_assigned_to"`
	SecAssigned        string `json:"security_assigned_to"`
	ExportAssigned     string `json:"export_assigned_to"`
	Phase              string `json:"dql_approval_phase"`
	RowCount           int    `json:"row_count"`
	AssigneeApprovedBy string `json:"assignee_approved_by,omitempty"`
	DBAApprovedBy      string `json:"dba_approved_by,omitempty"`
	SecApprovedBy      string `json:"security_approved_by,omitempty"`
	RejectedPhase      string `json:"rejected_phase,omitempty"`
	RejectedBy         string `json:"rejected_by,omitempty"`
	// 异步导出元数据（与审批字段共存于 need_rollback）
	ExportFormat   string `json:"export_format,omitempty"`
	ExportFile     string `json:"export_file,omitempty"`
	ExportBytes    int64  `json:"export_bytes,omitempty"`
	ExportReadyAt  string `json:"export_ready_at,omitempty"`
	ExportError    string `json:"export_error,omitempty"`
	ExportVia      string `json:"export_via,omitempty"`
	ExportExecutor string `json:"export_executor,omitempty"`
	ExportStage    string `json:"export_stage,omitempty"`
}

func parseDQLApprovalExtra(raw string) (*dqlApprovalExtra, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &dqlApprovalExtra{}, nil
	}
	var ex dqlApprovalExtra
	if err := json.Unmarshal([]byte(raw), &ex); err != nil {
		return nil, fmt.Errorf("解析 DQL 审批扩展失败")
	}
	ex.DBAAssigned = strings.TrimSpace(ex.DBAAssigned)
	ex.SecAssigned = strings.TrimSpace(ex.SecAssigned)
	ex.ExportAssigned = strings.TrimSpace(ex.ExportAssigned)
	ex.Phase = strings.TrimSpace(ex.Phase)
	ex.AssigneeApprovedBy = strings.TrimSpace(ex.AssigneeApprovedBy)
	ex.DBAApprovedBy = strings.TrimSpace(ex.DBAApprovedBy)
	ex.SecApprovedBy = strings.TrimSpace(ex.SecApprovedBy)
	ex.RejectedPhase = strings.TrimSpace(ex.RejectedPhase)
	ex.RejectedBy = strings.TrimSpace(ex.RejectedBy)
	return &ex, nil
}

func (ex *dqlApprovalExtra) encode() (string, error) {
	if ex == nil {
		return "{}", nil
	}
	b, err := json.Marshal(ex)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (ex *dqlApprovalExtra) needsMultiStep() bool {
	return ex != nil && ex.DBAAssigned != "" && ex.SecAssigned != ""
}

func (ex *dqlApprovalExtra) normalizePhase(assignedTo, status string) {
	if ex == nil {
		return
	}
	if !ex.needsMultiStep() {
		if ex.Phase == "" {
			ex.Phase = dqlPhaseDone
		}
		return
	}
	if status != store.StatusPending {
		ex.Phase = dqlPhaseDone
		return
	}
	switch strings.ToLower(ex.Phase) {
	case dqlPhaseAssignee, dqlPhaseDBA, dqlPhaseSecurity, dqlPhaseDone:
		return
	}
	// 兼容旧工单：按当前 assigned_to 推断阶段
	at := strings.TrimSpace(assignedTo)
	switch {
	case strings.EqualFold(at, ex.SecAssigned):
		ex.Phase = dqlPhaseSecurity
	case strings.EqualFold(at, ex.DBAAssigned):
		ex.Phase = dqlPhaseDBA
	default:
		ex.Phase = dqlPhaseAssignee
	}
}

func buildDQLSubmitExtra(req *ticketSubmitReq, exportAssignee string) (string, error) {
	dba := strings.TrimSpace(req.DBAAssigned)
	sec := strings.TrimSpace(req.SecAssigned)
	ex := &dqlApprovalExtra{
		NeedSensitive:  req.NeedSensitive,
		DBAAssigned:    dba,
		SecAssigned:    sec,
		ExportAssigned: strings.TrimSpace(exportAssignee),
		RowCount:       req.RowCount,
		Phase:          dqlPhaseAssignee,
	}
	if dba == "" || sec == "" {
		ex.Phase = dqlPhaseDone
	}
	return ex.encode()
}

// advanceDQLApproval 推进多级审批。advanced=true 表示仍 pending、已切到下一审批人。
func advanceDQLApproval(it *store.Issue, actor string) (advanced bool, err error) {
	if it == nil || it.IssueType != store.IssueTypeDQL {
		return false, nil
	}
	ex, err := parseDQLApprovalExtra(it.NeedRollback)
	if err != nil {
		return false, err
	}
	ex.normalizePhase(it.AssignedTo, it.Status)
	if !ex.needsMultiStep() || ex.Phase == dqlPhaseDone {
		return false, nil
	}
	actor = strings.TrimSpace(actor)
	switch ex.Phase {
	case dqlPhaseAssignee:
		ex.AssigneeApprovedBy = actor
		ex.Phase = dqlPhaseDBA
		it.AssignedTo = ex.DBAAssigned
		raw, err := ex.encode()
		if err != nil {
			return false, err
		}
		it.NeedRollback = raw
		return true, nil
	case dqlPhaseDBA:
		ex.DBAApprovedBy = actor
		ex.Phase = dqlPhaseSecurity
		it.AssignedTo = ex.SecAssigned
		raw, err := ex.encode()
		if err != nil {
			return false, err
		}
		it.NeedRollback = raw
		return true, nil
	case dqlPhaseSecurity:
		ex.SecApprovedBy = actor
		ex.Phase = dqlPhaseDone
		raw, err := ex.encode()
		if err != nil {
			return false, err
		}
		it.NeedRollback = raw
		return false, nil
	default:
		return false, nil
	}
}

func syncDQLExtraOnTransfer(it *store.Issue, newAssignee string) error {
	if it == nil || it.IssueType != store.IssueTypeDQL {
		return nil
	}
	ex, err := parseDQLApprovalExtra(it.NeedRollback)
	if err != nil {
		return err
	}
	if !ex.needsMultiStep() {
		return nil
	}
	ex.normalizePhase(it.AssignedTo, it.Status)
	switch ex.Phase {
	case dqlPhaseAssignee:
		ex.ExportAssigned = newAssignee
	case dqlPhaseDBA:
		ex.DBAAssigned = newAssignee
	case dqlPhaseSecurity:
		ex.SecAssigned = newAssignee
	default:
		return nil
	}
	raw, err := ex.encode()
	if err != nil {
		return err
	}
	it.NeedRollback = raw
	return nil
}

func dqlPhaseLabel(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case dqlPhaseDBA:
		return "DBA审批"
	case dqlPhaseSecurity:
		return "安全审批"
	case dqlPhaseAssignee:
		return "指定人员审批"
	default:
		return "审批"
	}
}

// markDQLRejected 记录驳回发生在哪一关（避免 UI 误标到最后一关）。
func markDQLRejected(it *store.Issue, actor string) error {
	if it == nil || it.IssueType != store.IssueTypeDQL {
		return nil
	}
	ex, err := parseDQLApprovalExtra(it.NeedRollback)
	if err != nil {
		return err
	}
	if !ex.needsMultiStep() {
		return nil
	}
	ex.normalizePhase(it.AssignedTo, store.StatusPending)
	ex.RejectedPhase = ex.Phase
	ex.RejectedBy = strings.TrimSpace(actor)
	raw, err := ex.encode()
	if err != nil {
		return err
	}
	it.NeedRollback = raw
	return nil
}
