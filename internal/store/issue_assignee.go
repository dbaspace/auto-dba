package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseApproverWhitelist 解析超级审核白名单：逗号 / 换行 / 空白分隔。
func ParseApproverWhitelist(raw string) []string {
	raw = strings.ReplaceAll(raw, "，", ",")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n", ",")
	raw = strings.ReplaceAll(raw, "\t", ",")
	raw = strings.ReplaceAll(raw, " ", ",")
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// ExpandApproverNames 将 ["admin,dba","mayu"] 拆开并集去重为独立用户名。
func ExpandApproverNames(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, name)
	}
	for _, raw := range names {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.ContainsAny(raw, ",， \t\n\r") {
			for _, p := range ParseApproverWhitelist(raw) {
				add(p)
			}
			continue
		}
		add(raw)
	}
	return out
}

// IsGlobalApprover 当前用户是否在 issue_global_approver_whitelist 中（现读）。
func (d *DB) IsGlobalApprover(username string) bool {
	username = strings.TrimSpace(username)
	if d == nil || d.db == nil || username == "" {
		return false
	}
	raw, _ := d.GetGlobalArg(ArgIssueGlobalApproverWhitelist)
	for _, n := range ParseApproverWhitelist(raw) {
		if strings.EqualFold(n, username) {
			return true
		}
	}
	return false
}

// CanApproveIssue 指定审核人 / 白名单 / 特权（privilege 由调用方传入）。
func (d *DB) CanApproveIssue(it *Issue, username string, privileged bool) bool {
	if it == nil || strings.TrimSpace(username) == "" {
		return false
	}
	if privileged {
		return true
	}
	if it.AssignedTo != "" && strings.EqualFold(it.AssignedTo, username) {
		return true
	}
	return d.IsGlobalApprover(username)
}

// IsPlatformResourceApplyReviewer 平台管理员或超级审核白名单：可跨租审核平台资源申请。
func (d *DB) IsPlatformResourceApplyReviewer(userID, username string) bool {
	if d == nil {
		return false
	}
	if d.IsGlobalApprover(username) {
		return true
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || d.db == nil {
		return false
	}
	ok, err := d.UserIsPlatformAdmin(userID)
	return err == nil && ok
}

// CanApproveResourceApply 平台资源申请审批：指定审批人 / 白名单 / 平台管理员 / privileged。
// 其它工单类型请继续用 CanApproveIssue。
func (d *DB) CanApproveResourceApply(it *Issue, username, userID string, privileged bool) bool {
	if it == nil || it.IssueType != IssueTypeResourceApply {
		return false
	}
	if d.CanApproveIssue(it, username, privileged) {
		return true
	}
	return d.IsPlatformResourceApplyReviewer(userID, username)
}

// AppendAssigneeTransferHistory 追加审计行并返回 JSON 数组字符串。
func AppendAssigneeTransferHistory(prevJSON, line string) string {
	line = strings.TrimSpace(line)
	var hist []string
	prev := strings.TrimSpace(prevJSON)
	if prev != "" && prev != "null" {
		_ = json.Unmarshal([]byte(prev), &hist)
	}
	if hist == nil {
		hist = []string{}
	}
	if line != "" {
		hist = append(hist, line)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(hist); err != nil {
		return "[]"
	}
	return strings.TrimSpace(buf.String())
}

// UpdateIssueAssignee 仅更新审核人与转移历史（不改 status）。
func (d *DB) UpdateIssueAssignee(id, assignedTo, historyJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(historyJSON) == "" {
		historyJSON = "[]"
	}
	_, err := d.Exec(
		`UPDATE tbl_issues SET assigned_to=?, assignee_transfer_history=?, updated_at=? WHERE id=?`,
		assignedTo, historyJSON, now, id,
	)
	return err
}

// UpdateIssueAssigneeAndRollback 更新当前审核人、转移历史与 need_rollback（DQL 多级审批推进）。
func (d *DB) UpdateIssueAssigneeAndRollback(id, assignedTo, historyJSON, needRollback string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(historyJSON) == "" {
		historyJSON = "[]"
	}
	_, err := d.Exec(
		`UPDATE tbl_issues SET assigned_to=?, assignee_transfer_history=?, need_rollback=?, updated_at=? WHERE id=?`,
		assignedTo, historyJSON, needRollback, now, id,
	)
	return err
}

// UpdateIssueNeedRollback 仅更新 need_rollback。
func (d *DB) UpdateIssueNeedRollback(id, needRollback string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET need_rollback=?, updated_at=? WHERE id=?`, needRollback, now, id)
	return err
}

// FormatAssigneeTransferLine 生成审计文案。
func FormatAssigneeTransferLine(oldUser, newUser, by string) string {
	return fmt.Sprintf("%s->%s by=%s at=%s",
		strings.TrimSpace(oldUser), strings.TrimSpace(newUser), strings.TrimSpace(by),
		time.Now().UTC().Format(time.RFC3339))
}

// FormatWhitelistApprovalLine 白名单审批审计。
func FormatWhitelistApprovalLine(actor, action string) string {
	return fmt.Sprintf("%s_by_whitelist=%s at=%s",
		strings.TrimSpace(action), strings.TrimSpace(actor),
		time.Now().UTC().Format(time.RFC3339))
}
