package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/dns"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

const (
	dnsTicketOps      = "ops"
	dnsTicketRollback = "rollback"

	dnsApproversArg = "dns_approvers"
)

// DnsChangePayload DNS 变更工单载荷（存于 sql_content）。Ops 的 Expect 为提交时的当前值，执行时据此检查冲突。
type DnsChangePayload struct {
	Kind    string            `json:"kind"`
	Ops     []dns.Op          `json:"ops,omitempty"`
	BatchID string            `json:"batch_id,omitempty"`
	Source  string            `json:"source"`
	Reason  string            `json:"reason"`
	Items   []dns.AppliedItem `json:"items"`
}

// dnsApprovers 全局参数 dns_approvers（用户名，逗号分隔）；未配置时为 admin。
func (h *DNSHandler) dnsApprovers() []string {
	if h.Store != nil {
		if raw, err := h.Store.GetGlobalArg(dnsApproversArg); err == nil {
			if list := store.ParseApproverWhitelist(raw); len(list) > 0 {
				return list
			}
		}
	}
	return []string{"admin"}
}

// IsDNSApprover 是否在 DNS 审批人名单中。全局审批人与 privileged 由通用审批规则放行。
func (h *DNSHandler) IsDNSApprover(username string) bool {
	if h == nil || strings.TrimSpace(username) == "" {
		return false
	}
	for _, u := range h.dnsApprovers() {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}

func (h *DNSHandler) createTicket(c *gin.Context, p DnsChangePayload, items []dns.AppliedItem) (*store.Issue, error) {
	if h.Store == nil {
		return nil, fmt.Errorf("平台库不可用")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	desc := "DNS 变更"
	if len(items) > 0 {
		desc = "DNS 变更：" + items[0].Summary
		if len(items) > 1 {
			desc += fmt.Sprintf(" 等 %d 项", len(items))
		}
	}
	if r := strings.TrimSpace(p.Reason); r != "" {
		desc += "（" + r + "）"
	}
	issue := &store.Issue{
		ID:          uuid.NewString(),
		TicketNo:    "D" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10],
		IssueType:   store.IssueTypeDnsChange,
		SQLContent:  string(raw),
		Description: desc,
		Submitter:   middleware.Username(c),
		AssignedTo:  h.dnsApprovers()[0],
	}
	stampTenantID(c, func(tid string) { issue.TenantID = tid })
	if err := h.Store.CreateIssue(issue); err != nil {
		return nil, err
	}
	NotifyIssueCreatedAsync(h.Store, h.Secret, issue)
	return issue, nil
}

// ExecuteDnsChangeTicket 审批通过后执行：重新校验并检查冲突，来源记为 ticket（回滚记为 rollback）。
func (h *DNSHandler) ExecuteDnsChangeTicket(ctx context.Context, it *store.Issue) *ticketexec.Result {
	started := time.Now().UTC().Format(time.RFC3339)
	res := &ticketexec.Result{StartedAt: started, Engine: "dns"}
	done := func(msg string) *ticketexec.Result {
		res.Message = msg
		res.EndedAt = time.Now().UTC().Format(time.RFC3339)
		return res
	}
	var p DnsChangePayload
	if err := json.Unmarshal([]byte(it.SQLContent), &p); err != nil {
		return done("载荷解析失败: " + err.Error())
	}
	st, err := h.db()
	if err != nil {
		return done(err.Error())
	}
	meta := dns.OpMeta{Actor: it.Submitter, Source: dns.SourceTicket, TicketID: it.TicketNo, Reason: p.Reason}
	var out *dns.ApplyResult
	switch p.Kind {
	case dnsTicketRollback:
		out, err = st.Rollback(ctx, p.BatchID, meta)
	default:
		out, err = st.ApplyOps(ctx, p.Ops, meta)
	}
	if err != nil {
		return done(err.Error())
	}
	res.Success = true
	res.Summary = fmt.Sprintf("批次 %s，共 %d 项变更", out.BatchID, len(out.Items))
	for i, item := range out.Items {
		res.Records = append(res.Records, ticketexec.Record{
			SQL: item.Summary, Status: "success", Order: i + 1, StartedAt: started, EndedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return done(res.Summary)
}
