package copilot

import (
	"context"
	"fmt"
	"log"
	"strings"
)

const (
	toolAnalyticsQuery = "analytics.query"
	maxToolCalls       = 4
)

// ToolSpec 只读工具。mode 固定 read。
type ToolSpec struct {
	Name string
	Mode string
}

func readOnlyTools() map[string]ToolSpec {
	return map[string]ToolSpec{
		toolAnalyticsQuery: {Name: toolAnalyticsQuery, Mode: "read"},
	}
}

// Gate 单轮只读工具执行器。租户由调用方注入，模型参数里的租户会被丢弃。
type Gate struct {
	Svc    *Service
	Tenant string
	User   string
	calls  int
}

type auditEvent struct {
	User   string
	Tenant string
	Tool   string
	Params string
	Rows   int
	Deny   string
}

func (g *Gate) audit(ev auditEvent) {
	log.Printf("copilot-audit user=%s tenant=%s tool=%s rows=%d deny=%s params=%s",
		ev.User, ev.Tenant, ev.Tool, ev.Rows, ev.Deny, Redact(trim(ev.Params, 240)))
}

// Exec 执行白名单工具。未注册、超次、写意图一律拒绝。
func (g *Gate) Exec(ctx context.Context, name string, args map[string]any) (string, error) {
	if g == nil {
		return "", fmt.Errorf("工具网关未初始化")
	}
	name = strings.TrimSpace(name)
	spec, ok := readOnlyTools()[name]
	if !ok || spec.Mode != "read" {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Deny: "未注册工具"})
		return "", fmt.Errorf("未注册的工具: %s", name)
	}
	if g.calls >= maxToolCalls {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Deny: "超过单轮次数"})
		return "", fmt.Errorf("单轮工具调用已达上限 %d", maxToolCalls)
	}
	g.calls++
	if err := rejectClientIdentity(args); err != nil {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Params: fmt.Sprint(args), Deny: err.Error()})
		return "", err
	}
	clean := sanitizeToolArgs(args)
	if reason := DetectDanger(fmt.Sprint(clean)); reason != "" {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Params: fmt.Sprint(clean), Deny: reason})
		return "", fmt.Errorf("%s", reason)
	}
	if name != toolAnalyticsQuery {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Deny: "非只读入口"})
		return "", fmt.Errorf("仅允许 %s", toolAnalyticsQuery)
	}
	body, rows, err := g.Svc.runTemplate(ctx, g.Tenant, clean)
	if err != nil {
		g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Params: fmt.Sprint(clean), Deny: err.Error()})
		return "", err
	}
	g.audit(auditEvent{User: g.User, Tenant: g.Tenant, Tool: name, Params: fmt.Sprint(clean), Rows: rows})
	return body, nil
}

func sanitizeToolArgs(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if id, ok := args["template_id"]; ok {
		out["template_id"] = id
	}
	if p, ok := args["params"]; ok {
		out["params"] = p
	}
	return out
}

func rejectClientIdentity(args map[string]any) error {
	for _, k := range []string{"base_url", "host", "url", "tenant_id", "role", "username"} {
		if _, ok := args[k]; ok {
			return fmt.Errorf("不允许由模型指定 %s", k)
		}
		if p, ok := args["params"].(map[string]any); ok {
			if _, ok := p[k]; ok {
				return fmt.Errorf("不允许由模型指定 %s", k)
			}
		}
	}
	return nil
}
