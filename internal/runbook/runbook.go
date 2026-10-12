package runbook

import (
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

// Action 白名单动作。
const (
	ActionClaimAlert   = "claim_alert"
	ActionCreateTodo   = "create_todo"
	ActionSilence2h    = "silence_2h"
	ActionKillSession  = "kill_session"  // 仅草稿
	ActionCDCRestart   = "cdc_restart"   // 仅草稿
)

// Suggestion 建议动作。
type Suggestion struct {
	Action      string `json:"action"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Executable  bool   `json:"executable"`
	Reason      string `json:"reason"`
}

// PreviewResult preview 响应。
type PreviewResult struct {
	AlertID     string       `json:"alert_id"`
	Suggestions []Suggestion `json:"suggestions"`
}

// ExecuteResult execute 响应。
type ExecuteResult struct {
	Action  string `json:"action"`
	Status  string `json:"status"` // done | draft | rejected
	Detail  string `json:"detail"`
	RefID   string `json:"ref_id,omitempty"`
}

// Service Runbook。
type Service struct {
	Store *store.DB
}

func New(db *store.DB) *Service { return &Service{Store: db} }

// Preview 根据告警给出建议。
func (s *Service) Preview(a *store.Alert) *PreviewResult {
	out := &PreviewResult{AlertID: a.ID}
	an := aiops.AlertNameFromLabels(a.Labels)
	sev := a.Severity

	out.Suggestions = append(out.Suggestions, Suggestion{
		Action: ActionClaimAlert, Title: "认领告警", Description: "将告警标记为已认领并指派给当前用户",
		Executable: true, Reason: "标准处置第一步",
	})
	out.Suggestions = append(out.Suggestions, Suggestion{
		Action: ActionCreateTodo, Title: "转待办", Description: "创建跟进待办（24h）",
		Executable: true, Reason: "需要跨班次跟进时",
	})
	out.Suggestions = append(out.Suggestions, Suggestion{
		Action: ActionSilence2h, Title: "静默 2 小时", Description: "快捷静默匹配 labels",
		Executable: true, Reason: "已知抖动或维护窗口",
	})

	if an == "PGConnHigh" || strings.Contains(strings.ToLower(a.Title), "连接") {
		out.Suggestions = append(out.Suggestions, Suggestion{
			Action: ActionKillSession, Title: "终止空闲会话（草稿）", Description: "仅生成待办，不直接杀会话",
			Executable: false, Reason: "高危：需人工在会话管理执行",
		})
	}
	if sev == "P0" || sev == "P1" {
		out.Suggestions = append(out.Suggestions, Suggestion{
			Action: ActionCDCRestart, Title: "CDC 重启（草稿）", Description: "仅生成待办提示检查 CDC",
			Executable: false, Reason: "高危：无审批不直连",
		})
	}
	return out
}

// Execute 执行白名单动作。
func (s *Service) Execute(a *store.Alert, action, operator, tenantID string) (*ExecuteResult, error) {
	action = strings.TrimSpace(action)
	switch action {
	case ActionClaimAlert:
		a.Owner = operator
		a.Handle = store.AlertHandleAcknowledged
		if err := s.Store.SaveAlert(a); err != nil {
			return nil, err
		}
		_ = s.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "runbook", Detail: "runbook:claim_alert", Operator: operator,
		})
		return &ExecuteResult{Action: action, Status: "done", Detail: "已认领"}, nil

	case ActionCreateTodo:
		td := &store.Todo{
			Title: a.Title, Source: store.TodoSourceAlert, SourceRef: a.ID,
			Owner: operator, PlanAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			Description: "Runbook 自动创建", Status: store.TodoStatusPending, CreatedBy: operator,
			LatestProgress: "runbook create_todo", TenantID: tenantID,
		}
		if err := s.Store.CreateTodo(td); err != nil {
			return nil, err
		}
		_ = s.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "runbook", Detail: "runbook:create_todo " + td.ID, Operator: operator,
		})
		return &ExecuteResult{Action: action, Status: "done", Detail: "已转待办", RefID: td.ID}, nil

	case ActionSilence2h:
		now := time.Now().UTC()
		parts := []string{}
		for _, k := range []string{"service", "alertname", "cluster"} {
			if v := a.Labels[k]; v != "" {
				parts = append(parts, k+"="+v)
			}
		}
		if len(parts) == 0 {
			parts = append(parts, "fingerprint="+a.Fingerprint)
		}
		sil := &store.AlertSilence{
			Name: "runbook-silence-" + a.ID, Matchers: strings.Join(parts, ", "),
			StartsAt: now.Format(time.RFC3339), EndsAt: now.Add(2 * time.Hour).Format(time.RFC3339),
			Reason: "runbook silence_2h", CreatedBy: operator, TenantID: tenantID,
		}
		if err := s.Store.CreateAlertSilence(sil); err != nil {
			return nil, err
		}
		a.Handle = store.AlertHandleSilenced
		_ = s.Store.SaveAlert(a)
		_ = s.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "runbook", Detail: "runbook:silence_2h " + sil.ID, Operator: operator,
		})
		return &ExecuteResult{Action: action, Status: "done", Detail: "已静默 2 小时", RefID: sil.ID}, nil

	case ActionKillSession, ActionCDCRestart:
		td := &store.Todo{
			Title: "[草稿] " + action + " · " + a.Title,
			Source: store.TodoSourceAlert, SourceRef: a.ID,
			Owner: operator, PlanAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			Description: fmt.Sprintf("Runbook 高危动作草稿：%s。请人工确认后在对应模块执行。", action),
			Status: store.TodoStatusPending, CreatedBy: operator, TenantID: tenantID,
			LatestProgress: "runbook draft " + action,
		}
		if err := s.Store.CreateTodo(td); err != nil {
			return nil, err
		}
		_ = s.Store.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "runbook", Detail: "runbook:draft:" + action + " " + td.ID, Operator: operator,
		})
		return &ExecuteResult{Action: action, Status: "draft", Detail: "已生成待办草稿，未直接执行", RefID: td.ID}, nil

	default:
		return nil, fmt.Errorf("不支持的动作 %s", action)
	}
}
