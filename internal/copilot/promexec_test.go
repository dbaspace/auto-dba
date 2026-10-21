package copilot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

func TestExecPromUsesDomainLiteral(t *testing.T) {
	inst := &store.Instance{Host: "dorisdb.test.restosuite.ai", DBType: "starrocks", Port: 9030}
	var got []string
	s := &Service{promSamples: func(_ context.Context, promql string, hours int) ([]promclient.Sample, error) {
		got = append(got, promql)
		if hours != 1 {
			t.Fatalf("hours=%d", hours)
		}
		if strings.Contains(promql, `\`) {
			t.Fatalf("promql has backslash: %s", promql)
		}
		if len(got) == 1 {
			return nil, nil
		}
		return []promclient.Sample{{
			Metric: map[string]string{"instance": "be-1:8040"},
			Values: [][]any{{1, "10"}, {2, "30"}, {3, "20"}},
		}}, nil
	}}
	cpu := s.ExecProm(context.Background(), inst, beCPUExpr, 1)
	mem := s.ExecProm(context.Background(), inst, "starrocks_be_process_mem_bytes", 1)
	if !cpu.OK || !mem.OK {
		t.Fatalf("cpu=%+v mem=%+v", cpu, mem)
	}
	if len(cpu.Tried) < 2 || !strings.Contains(cpu.Tried[0], `domain="dorisdb.test.restosuite.ai"`) {
		t.Fatal(cpu.Tried)
	}
	if !strings.Contains(cpu.PromQL, `[.]`) || strings.Contains(cpu.PromQL, `\`) {
		t.Fatal(cpu.PromQL)
	}
	if !strings.Contains(mem.PromQL, "starrocks_be_process_mem_bytes{") || strings.Contains(mem.PromQL, `\`) {
		t.Fatal(mem.PromQL)
	}
	if !strings.Contains(cpu.Text(), "min=10.0") || !strings.Contains(cpu.Text(), "max=30.0") {
		t.Fatal(cpu.Text())
	}
}

func TestExecPromStripsEscapeAndRetries(t *testing.T) {
	inst := &store.Instance{Host: "be.example.cn", DomainName: "be.example.cn", DBType: "doris"}
	n := 0
	s := &Service{promSamples: func(_ context.Context, promql string, _ int) ([]promclient.Sample, error) {
		n++
		if n == 1 {
			return nil, fmt.Errorf("prometheus http 400: parse error: unknown escape sequence U+002E '.' in %s", promql)
		}
		if strings.Contains(promql, `\`) {
			t.Fatalf("retry kept backslash: %s", promql)
		}
		return []promclient.Sample{{
			Metric: map[string]string{"instance": "be"},
			Value:  []any{1, "4"},
		}}, nil
	}}
	res := s.ExecProm(context.Background(), inst, `starrocks_be_cpu`, 0)
	if !res.OK || n < 2 {
		t.Fatalf("n=%d res=%+v", n, res)
	}
}

func TestValidatePromExprRejectsLabels(t *testing.T) {
	if err := validatePromExpr(`sum(up{instance="x"})`); err == nil {
		t.Fatal("want reject instance label")
	}
	if err := validatePromExpr(`rate(starrocks_be_cpu{mode="idle",__SEL__}[5m])`); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(applySelector(`starrocks_be_process_mem_bytes`, `domain="a.b"`), `\.`) {
		t.Fatal("selector escaped dot")
	}
}

func TestRunTaskSameExecutorForAnotherExpr(t *testing.T) {
	var exprs []string
	s := &Service{promSamples: func(_ context.Context, promql string, _ int) ([]promclient.Sample, error) {
		exprs = append(exprs, promql)
		return []promclient.Sample{{
			Metric: map[string]string{"instance": "be"},
			Values: [][]any{{1, "1"}, {2, "3"}},
		}}, nil
	}}
	n := 0
	steps := s.runTaskDecide(context.Background(), Config{}, "", ChatRequest{
		Message: "分析下 dorisdb.test.restosuite.ai 最近1小时内存",
	}, func(string) TaskAction {
		n++
		if n == 1 {
			return TaskAction{Action: "prometheus", Expr: "starrocks_be_process_mem_bytes", Hours: 1}
		}
		return TaskAction{Action: "done"}
	})
	var prom TaskStep
	for _, st := range steps {
		if st.Action == "prometheus" {
			prom = st
		}
	}
	if prom.Status != "error" && prom.Status != "ok" {
		t.Fatalf("steps=%+v", steps)
	}
	if prom.Status == "ok" {
		t.Fatal("catalog 没有目录时不应查到序列")
	}
	if len(exprs) != 0 {
		t.Fatal("未对上实例不应查询")
	}
	res := s.ExecProm(context.Background(), &store.Instance{Host: "dorisdb.test.restosuite.ai", DBType: "starrocks"}, "starrocks_be_process_mem_bytes", 1)
	if !res.OK || !strings.Contains(res.PromQL, "starrocks_be_process_mem_bytes{") {
		t.Fatal(res.PromQL)
	}
}

func TestRunTaskEmitsSteps(t *testing.T) {
	var n int
	req := ChatRequest{
		Message: "分析下平台登记有多少postgres数据库实例",
		OnStep:  func(TaskStep) { n++ },
	}
	steps := (&Service{}).runTaskDecide(context.Background(), Config{}, "", req, func(string) TaskAction {
		t.Fatal("不应再决定下一步")
		return TaskAction{}
	})
	if n != 1 || len(steps) != 1 {
		t.Fatalf("emit=%d steps=%d", n, len(steps))
	}
}

func TestCatalogCountPostgresQuestion(t *testing.T) {
	q := "分析下平台登记有多少postgres数据库实例"
	if !wantsCatalogCount(q) {
		t.Fatal("want catalog count")
	}
	if !catalogEngineMatch("PostgreSQL", "postgres") || !catalogEngineMatch("pg", "postgres") {
		t.Fatal("postgres alias")
	}
	if catalogEngineMatch("mysql", "postgres") {
		t.Fatal("mysql is not postgres")
	}
	called := false
	steps := (&Service{}).runTaskDecide(context.Background(), Config{}, "", ChatRequest{Message: q}, func(string) TaskAction {
		called = true
		return TaskAction{Action: "prometheus", Expr: "up"}
	})
	if called {
		t.Fatal("登记数量不应再交给模型选工具")
	}
	if len(steps) != 1 || steps[0].Action != "catalog" || steps[0].Status != "error" {
		t.Fatalf("%+v", steps)
	}
	if !strings.Contains(steps[0].Output, "实例目录不可用") {
		t.Fatal(steps[0].Output)
	}
}

func TestRunTaskRefusesWriteBeforeLoop(t *testing.T) {
	s := &Service{}
	steps := s.runTaskDecide(context.Background(), Config{}, "", ChatRequest{
		Message: "更新下数据，对应的实例和库信息是mysql.test、db_abc tbl_aa",
	}, func(string) TaskAction {
		t.Fatal("写操作不应进入任务环")
		return TaskAction{}
	})
	if steps != nil {
		t.Fatal(steps)
	}
}

func TestAnalyzeScriptRejected(t *testing.T) {
	for _, script := range []string{"open('/etc/passwd')", "import socket", "import subprocess"} {
		if _, err := runAnalyzeScript(script, `{}`); err == nil {
			t.Fatalf("want reject %s", script)
		}
	}
}

func TestAnalyzeScriptReadsData(t *testing.T) {
	out, err := runAnalyzeScript(`print(data["series"][0]["max"])`, `{"series":[{"max":12.5}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "12.5" {
		t.Fatal(out)
	}
}
