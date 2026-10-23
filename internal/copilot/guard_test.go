package copilot

import (
	"context"
	"strings"
	"testing"

	"auto-dba/internal/aiops"
)

func TestUnregisteredToolDenied(t *testing.T) {
	g := &Gate{Svc: New(nil), Tenant: "t1", User: "u"}
	_, err := g.Exec(context.Background(), "instances.delete", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "未注册") {
		t.Fatalf("err=%v", err)
	}
}

func TestToolCallCap(t *testing.T) {
	g := &Gate{Svc: New(nil), Tenant: "t1"}
	g.calls = maxToolCalls
	_, err := g.Exec(context.Background(), toolAnalyticsQuery, map[string]any{"template_id": "platform.instances"})
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectClientTenantAndBaseURL(t *testing.T) {
	g := &Gate{Svc: New(nil), Tenant: "real-tenant", User: "u"}
	_, err := g.Exec(context.Background(), toolAnalyticsQuery, map[string]any{
		"template_id": "platform.instances",
		"tenant_id":   "other",
		"base_url":    "http://evil",
	})
	if err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("err=%v", err)
	}
}

func TestUnknownTemplate(t *testing.T) {
	g := &Gate{Svc: New(nil), Tenant: "t1"}
	_, err := g.Exec(context.Background(), toolAnalyticsQuery, map[string]any{"template_id": "prom.custom"})
	if err == nil || !strings.Contains(err.Error(), "未登记") {
		t.Fatalf("err=%v", err)
	}
}

func TestPlaceholderMustResolve(t *testing.T) {
	tpl, err := findTemplate("prom.pg.connections")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderTemplate(tpl, map[string]string{}); err == nil {
		t.Fatal("missing instance should fail")
	}
	got, err := renderTemplate(tpl, map[string]string{"instance": "db-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{") || strings.Contains(got, "query.universe") {
		t.Fatalf("rendered=%s", got)
	}
}

func TestSQLGuard(t *testing.T) {
	for _, bad := range []string{
		"INSERT INTO t VALUES (1)",
		"DELETE FROM t",
		"UPDATE t SET a=1",
		"SELECT 1; DELETE FROM t",
		"SELECT pg_read_file('/etc/passwd')",
	} {
		if _, err := CheckCopilotSQL(bad); err == nil {
			t.Fatalf("should reject %s", bad)
		}
	}
	if _, err := CheckCopilotSQL("SELECT 1"); err != nil {
		t.Fatal(err)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := "password=s3cret api_key=crsr_live token: abcdefgh"
	out := Redact(in)
	if strings.Contains(out, "s3cret") || strings.Contains(out, "crsr_live") || strings.Contains(out, "abcdefgh") {
		t.Fatalf("redact failed: %s", out)
	}
}

func TestDetectDanger(t *testing.T) {
	if DetectDanger("看一下连接数趋势") != "" {
		t.Fatal("analysis should pass")
	}
	if DetectDanger("请 DROP TABLE users") == "" {
		t.Fatal("drop should hit")
	}
	if DetectDanger("帮我改配置") == "" {
		t.Fatal("config change should hit")
	}
	if DetectDanger("删除") == "" {
		t.Fatal("删除 should hit")
	}
	q := "更新下数据，对应的实例和库信息是mysql.test、db_abc tbl_aa"
	if aiops.ParseUtterance(q).Meaning() != "更新数据" {
		t.Fatalf("meaning=%s", aiops.ParseUtterance(q).Meaning())
	}
	if DetectDanger(q) == "" {
		t.Fatal("更新下数据 should refuse before llm")
	}
	if DetectDanger("更新一下数据") == "" || DetectDanger("改下数据") == "" {
		t.Fatal("particle between verb and object should still be a write")
	}
	for _, q := range []string{"python print(1)", "os.system('rm')", "subprocess.run([])", "exec(payload)"} {
		if DetectDanger(q) == "" {
			t.Fatalf("script should hit: %s", q)
		}
	}
}
