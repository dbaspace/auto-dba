package copilot

import (
	"context"
	"strings"
	"testing"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

func TestChooseScaffoldsByEngine(t *testing.T) {
	redisQ := "分析下beijing 环境 redis.bj.restosuite.cn 实例最近1小时cpu使用情况"
	redisCalls := chooseScaffolds(redisQ, &store.Instance{DBType: "redis", Host: "redis.bj.restosuite.cn"})
	if len(redisCalls) != 1 || redisCalls[0].Kind != "prometheus" || !strings.Contains(redisCalls[0].Spec, "redis_cpu_") {
		t.Fatalf("redis %+v", redisCalls)
	}
	if strings.Contains(redisCalls[0].Spec, "starrocks_be_cpu") {
		t.Fatal("redis 不应走 BE CPU")
	}

	pgCalls := chooseScaffolds("PostgreSQL 连接", &store.Instance{DBType: "PostgreSQL"})
	if len(pgCalls) != 3 || pgCalls[0].Spec != "connections" || pgCalls[1].Spec != "activity" || pgCalls[2].Kind != "prometheus" {
		t.Fatalf("pg %+v", pgCalls)
	}
	if !strings.Contains(pgCalls[2].Spec, "pg_stat_activity_count") {
		t.Fatal(pgCalls[2].Spec)
	}

	chCalls := chooseScaffolds("clickhouse 最近1小时慢查询", &store.Instance{DBType: "clickhouse"})
	if len(chCalls) != 2 || chCalls[0].Kind != "clickhouse" || chCalls[0].Spec != "queries" || chCalls[1].Kind != "prometheus" {
		t.Fatalf("ch %+v", chCalls)
	}
	if !strings.Contains(chCalls[1].Spec, "OSIdleTimeCPU") {
		t.Fatal(chCalls[1].Spec)
	}

	srCalls := chooseScaffolds("分析 be cpu 和内存", &store.Instance{DBType: "starrocks", Host: "doris.example.cn"})
	if len(srCalls) != 2 || !strings.Contains(srCalls[0].Spec, "starrocks_be_cpu") || !strings.Contains(srCalls[1].Spec, "starrocks_be_process_mem_bytes") {
		t.Fatalf("sr %+v", srCalls)
	}
}

func TestScaffoldsContinueAfterError(t *testing.T) {
	called := 0
	s := &Service{promSamples: func(_ context.Context, _ string, _ int) ([]promclient.Sample, error) {
		called++
		return []promclient.Sample{{
			Metric: map[string]string{"instance": "pg:9187"},
			Values: [][]any{{1, "10"}, {2, "12"}, {3, "11"}, {4, "13"}},
		}}, nil
	}}
	inst := &store.Instance{DBType: "postgres", Host: "pg.example.com"}
	calls := chooseScaffolds("PostgreSQL 连接数", inst)
	var items []judgeItem
	for _, c := range calls {
		step, payload := s.runScaffold(context.Background(), "t", inst, c.Kind, c.Spec, c.Hours)
		items = append(items, judgeItem{Action: step.Action, Detail: step.Detail, Status: step.Status, Output: step.Output, Raw: payload})
		if c.Kind == "postgres" && step.Status != "error" {
			t.Fatalf("无密码时应失败: %+v", step)
		}
	}
	if called != 1 {
		t.Fatalf("prometheus 应在 SQL 失败后仍执行，called=%d", called)
	}
	text := judgeFindings(items)
	if !strings.Contains(text, "缺失的数据：") || !strings.Contains(text, "postgres") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "min=10.0") || !strings.Contains(text, "预判：") {
		t.Fatal(text)
	}
}

func TestJudgeUsesBothSources(t *testing.T) {
	items := []judgeItem{
		{Action: "postgres", Detail: "connections", Status: "ok", Raw: `{"columns":["datname","sessions"],"rows":[["app",40],["ops",5]]}`},
		{Action: "prometheus", Detail: "pg", Status: "ok", Raw: `{"series":[{"labels":{"instance":"pg"},"min":10,"avg":20,"max":80,"trend":"升高"}]}`},
	}
	text := judgeFindings(items)
	if !strings.Contains(text, "datname=app") || !strings.Contains(text, "max=80.0") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "窗口内出现冲高，若持续需关注") {
		t.Fatal(text)
	}
	items[1].Raw = `{"series":[{"labels":{"instance":"pg"},"min":10,"avg":11,"max":12,"trend":"持平"}]}`
	text = judgeFindings(items)
	if !strings.Contains(text, "当前窗口未见明显恶化") || !strings.Contains(text, "datname=app") {
		t.Fatal(text)
	}
	items[0].Status = "error"
	items[0].Raw = ""
	items[0].Output = "连接失败"
	text = judgeFindings(items)
	if !strings.Contains(text, "缺失的数据：postgres") || !strings.Contains(text, "当前窗口未见明显恶化") {
		t.Fatal(text)
	}
}

func TestScaffoldSQLAllowlist(t *testing.T) {
	need := map[string][]string{
		"postgres":   {"activity", "connections", "databases", "sizes"},
		"clickhouse": {"processes", "queries", "databases"},
	}
	for kind, names := range need {
		for _, name := range names {
			_, sqlText, err := scaffoldSQL(kind, name)
			if err != nil {
				t.Fatalf("%s %s: %v", kind, name, err)
			}
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sqlText)), "SELECT") {
				t.Fatal(sqlText)
			}
		}
	}
	if _, _, err := scaffoldSQL("postgres", "drop"); err == nil {
		t.Fatal("want reject unknown query")
	}
	step, _ := (&Service{}).sqlScaffoldStep(nil, "t", &store.Instance{DBType: "redis"}, "postgres", "activity")
	if step.Status != "error" || !strings.Contains(step.Output, "不使用 postgres") {
		t.Fatal(step.Output)
	}
	_, sizeSQL, err := scaffoldSQL("postgres", "sizes")
	if err != nil || !strings.Contains(sizeSQL, "pg_database_size") {
		t.Fatalf("sizes sql=%s err=%v", sizeSQL, err)
	}
}

func TestDiskQuestionJudge(t *testing.T) {
	inst := &store.Instance{DBType: "PostgreSQL", Host: "pg.member04.bj.restosuite.cn", DomainName: "pg.member04.bj.restosuite.cn"}
	calls := chooseScaffolds("分析下磁盘使用高的原因", inst)
	if len(calls) != 1 || calls[0].Kind != "postgres" || calls[0].Spec != "sizes" {
		t.Fatalf("disk %+v", calls)
	}
	pgCalls := chooseScaffolds("PostgreSQL 连接", inst)
	if len(pgCalls) != 3 || pgCalls[0].Spec != "connections" || pgCalls[1].Spec != "activity" || pgCalls[2].Kind != "prometheus" {
		t.Fatalf("pg %+v", pgCalls)
	}

	raw := `{"columns":["datname","bytes"],"rows":[["db_marketing",1370000000000],["db_a",100000000000],["db_b",90000000000],["db_c",80000000000],["db_d",70000000000],["db_e",60000000000]]}`
	text := judgeFindings([]judgeItem{{Action: "postgres", Detail: "sizes", Status: "ok", Raw: raw}})
	if !strings.Contains(text, "1190.") && !strings.Contains(text, "1275.") {
		t.Fatal(text)
	}
	if strings.Contains(text, "e+") || strings.Contains(text, "E+") || !strings.Contains(text, " GB") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "其他") || !strings.Contains(text, "大库占满") || !strings.Contains(text, "| 库名 |") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "容量最高的是 db_marketing") {
		t.Fatal(text)
	}
	even := judgeFindings([]judgeItem{{Action: "postgres", Detail: "sizes", Status: "ok", Raw: `{"columns":["datname","bytes"],"rows":[["a",1000],["b",1000]]}`}})
	if !strings.Contains(even, "各库都不大") || strings.Contains(even, "e+") {
		t.Fatal(even)
	}

	var emitted []string
	s := &Service{catalogLookup: func(_, host string) (*store.Instance, error) {
		cp := *inst
		cp.Host = host
		return &cp, nil
	}}
	resp, err := s.llmAnswer(context.Background(), Config{}, "auto", ChatRequest{
		Message: "分析下 pg.member04.bj.restosuite.cn 磁盘使用高",
		OnStep:  func(st TaskStep) { emitted = append(emitted, st.Action+" "+st.Output) },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var judgeOut string
	sawSizes := false
	for _, st := range resp.Steps {
		if st.Action == "judge" && st.Status == "ok" {
			judgeOut = st.Output
		}
		if st.Action == "postgres" && st.Detail == "sizes" {
			sawSizes = true
		}
	}
	if !sawSizes || judgeOut == "" || resp.Answer != judgeOut {
		t.Fatalf("sizes=%v answer=%q judge=%q", sawSizes, resp.Answer, judgeOut)
	}
	for _, line := range emitted {
		if strings.Contains(line, "正在生成回答") {
			t.Fatal(emitted)
		}
	}
}
