package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestInjectScopeMatchers(t *testing.T) {
	tmpl := `up{job="node",__SCOPE__}`
	got := InjectScopeMatchers(tmpl, `instance=~"a|b"`)
	if got != `up{job="node",instance=~"a|b"}` {
		t.Fatalf("got %q", got)
	}
	got = InjectScopeMatchers(`up{__SCOPE__}`, "")
	if got != `up` {
		t.Fatalf("empty selector: %q", got)
	}
	got = InjectScopeMatchers(`up{,__SCOPE__}`, `idc="prod"`)
	if got != `up{idc="prod"}` {
		t.Fatalf("leading comma: %q", got)
	}
}

func TestNormalizeAlertScope(t *testing.T) {
	s := AlertScope{Mode: AlertScopeModeInstances}
	if err := NormalizeAlertScope(&s); err == nil {
		t.Fatal("want error for empty instances")
	}
	s = AlertScope{Mode: AlertScopeModeCluster, Cluster: &AlertScopeCluster{IDC: "prod", Domain: "db-a"}}
	if err := NormalizeAlertScope(&s); err != nil {
		t.Fatal(err)
	}
	s = AlertScope{Mode: AlertScopeModeTags, Tags: []string{"env=prod"}}
	if err := NormalizeAlertScope(&s); err != nil {
		t.Fatal(err)
	}
}

func TestMatcherFromInstances(t *testing.T) {
	m, err := matcherFromInstances([]Instance{
		{DomainName: "db-a.example.com", Host: "10.0.0.1", Port: 9100},
		{Host: "10.0.0.2", Port: 9100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m, `instance=~"`) {
		t.Fatalf("matcher %q", m)
	}
	// PromQL 源码中点号须为 \\. （Go 字符串里两个反斜杠）
	if !strings.Contains(m, `10\\.0\\.0\\.1:9100`) {
		t.Fatalf("want double-escaped dots, got %q", m)
	}
}

func TestPromRegexAltPromQLString(t *testing.T) {
	got := promRegexAlt([]string{"pg.test.restosuite.ai"})
	want := `pg\\.test\\.restosuite\\.ai`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMatcherFromInstancesPostgresAuto(t *testing.T) {
	m, err := matcherFromInstancesWithKey([]Instance{
		{Name: "test-pg", DomainName: "pg.test.restosuite.ai", DBType: "postgres", IDC: "test"},
	}, AlertMatcherKeyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m, `d_b_instance_name=~"`) || !strings.Contains(m, `test-pg`) {
		t.Fatalf("want d_b_instance_name with test-pg, got %q", m)
	}
	if strings.Contains(m, `idc=`) {
		t.Fatalf("must not auto-append idc (avoids over-filter), got %q", m)
	}
}

func TestMatcherFromInstancesMixedRejected(t *testing.T) {
	_, err := matcherFromInstancesWithKey([]Instance{
		{Name: "test-pg", DBType: "postgres"},
		{Name: "h1", DBType: "host", Host: "10.0.0.1", Port: 9100},
	}, AlertMatcherKeyAuto)
	if err == nil {
		t.Fatal("want mixed error")
	}
}

func TestValidateMatcherKey(t *testing.T) {
	if err := validateMatcherKey("d_b_instance_name"); err != nil {
		t.Fatal(err)
	}
	if err := validateMatcherKey("bad-key"); err == nil {
		t.Fatal("want error")
	}
}

func TestCollapseInstancesToLabelMatcher(t *testing.T) {
	list := make([]Instance, 0, 60)
	for i := 0; i < 60; i++ {
		list = append(list, Instance{Name: fmt.Sprintf("pg-%d", i), DBType: "postgres", IDC: "prod"})
	}
	m, err := matcherFromInstancesWithKey(list, AlertMatcherKeyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if m != `idc="prod"` {
		t.Fatalf("want collapsed idc, got %q", m)
	}
}

func TestTooManyInstancesWithoutCollapse(t *testing.T) {
	list := make([]Instance, 0, MaxAlertScopeInstances+1)
	for i := 0; i < MaxAlertScopeInstances+1; i++ {
		list = append(list, Instance{
			Name: fmt.Sprintf("h-%d", i), DBType: "host", Host: fmt.Sprintf("10.0.0.%d", i%250), Port: 9100,
			IDC: fmt.Sprintf("idc-%d", i%3), // 无法折叠
		})
	}
	_, err := matcherFromInstancesWithKey(list, AlertMatcherKeyInstance)
	if err == nil {
		t.Fatal("want error for too many without collapse")
	}
}

func TestCompileAlertRuleScopeGlobal(t *testing.T) {
	d := &DB{}
	r := &AlertRule{
		Expr:  `up{job="node",__SCOPE__}`,
		Scope: AlertScope{Mode: AlertScopeModeGlobal},
	}
	if err := d.CompileAlertRuleScope(r); err != nil {
		t.Fatal(err)
	}
	if r.CompiledExpr != `up{job="node"}` {
		t.Fatalf("compiled %q", r.CompiledExpr)
	}
	if r.CompileError != "" {
		t.Fatal(r.CompileError)
	}
}

func TestCompileAlertRuleScopeRequiresToken(t *testing.T) {
	d := &DB{}
	r := &AlertRule{
		Expr:  `up{job="node"}`,
		Scope: AlertScope{Mode: AlertScopeModeCluster, Cluster: &AlertScopeCluster{IDC: "p", Domain: "d"}},
	}
	if err := d.CompileAlertRuleScope(r); err == nil {
		t.Fatal("want error")
	}
}

func TestEvalExpr(t *testing.T) {
	r := &AlertRule{Expr: "a", CompiledExpr: "b", Scope: AlertScope{Mode: AlertScopeModeGlobal}}
	if r.EvalExpr() != "b" {
		t.Fatal(r.EvalExpr())
	}
	r2 := &AlertRule{Expr: "a", Scope: AlertScope{Mode: AlertScopeModeInstances}}
	if r2.EvalExpr() != "" {
		t.Fatal("non-global without compile should be empty")
	}
}
