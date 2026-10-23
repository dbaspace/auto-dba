package copilot

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

const taskDecidePrompt = "你是只读排查任务的调度器。根据已有步骤决定下一步，只输出一个 JSON 对象，不要解释。" +
	"强制规则：只能做查询，不能执行脚本，不能做查询以外的动作。" +
	`动作只能是 {"action":"catalog"}、{"action":"prometheus","expr":"指标表达式","hours":1}、{"action":"postgres","query":"activity|connections|databases|sizes"}、{"action":"postgres","database":"库名","sql":"一条查询 SQL"}、{"action":"clickhouse","query":"processes|queries|databases"}、{"action":"done"}。` +
	"postgres 可以选具名查询，也可以自己写一条查询 SQL。sql 只允许一条 SELECT、WITH ... SELECT、SHOW 或 EXPLAIN。" +
	"禁止 INSERT、UPDATE、DELETE、DDL、COPY、多条语句，以及 python、exec、os.system、subprocess 等脚本。" +
	"写 sql 时必须带 database，连到该库执行，不要连维护库 postgres，除非问题里的库就是 postgres。域名只用来认实例，查询仍走连接主机。" +
	"查某库有没有某表时，写 SELECT schemaname, tablename FROM pg_tables，并带上该库名。clickhouse 仍只能选查询名字。引擎以平台实例为准。" +
	"登记域名和连接主机若指向同一台实例，按这台实例继续查询，不要因为域名和 host 字符串不同就停住。" +
	"expr 只写聚合和指标名，可用 __SEL__ 或 mode=\"idle\"，不要自带 instance、domain、host、url、tenant。" +
	"hours 为 0 表示瞬时查询，1 到 6 表示最近几小时。统计完成或无法继续就 done。"

// TaskStep 平台任务的一步，返回给对话展示。
type TaskStep struct {
	Action string `json:"action"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Output string `json:"output,omitempty"`
}

// TaskAction 模型决定的下一步。host、url、tenant 不在这里，标签只来自平台实例。
type TaskAction struct {
	Action   string `json:"action"`
	Expr     string `json:"expr"`
	Query    string `json:"query"`
	Database string `json:"database"`
	SQL      string `json:"sql"`
	Hours    int    `json:"hours"`
	Script   string `json:"script"`
}

type taskState struct {
	inst     *store.Instance
	lastJSON string
}

func (s *Service) runTask(ctx context.Context, cfg Config, model string, req ChatRequest) []TaskStep {
	return s.runTaskDecide(ctx, cfg, model, req, nil)
}

func (s *Service) runTaskDecide(ctx context.Context, cfg Config, model string, req ChatRequest, decide func(string) TaskAction) []TaskStep {
	if DetectDanger(req.Message) != "" {
		return nil
	}
	st := &taskState{}
	var steps []TaskStep
	push := func(step TaskStep) {
		steps = append(steps, step)
		req.emit(step)
	}
	if wantsCatalogCount(req.Message) {
		step, payload := s.catalogCountStep(req.TenantID, req.Message)
		push(step)
		if payload != "" {
			st.lastJSON = payload
		}
		return steps
	}
	if host := aiops.ParseUtterance(req.Message).Object; validatePromHost(host) == nil {
		step, inst := s.catalogStep(req.TenantID, req.Message)
		push(step)
		st.inst = inst
	}
	if st.inst != nil {
		calls := chooseScaffolds(req.Message, st.inst)
		room := 6 - len(steps) - 1
		if room < 0 {
			room = 0
		}
		if len(calls) > room {
			calls = calls[:room]
		}
		var got []judgeItem
		for _, c := range calls {
			step, payload := s.runScaffold(ctx, req.TenantID, st.inst, c.Kind, c.Spec, c.Hours)
			push(step)
			if payload != "" {
				st.lastJSON = payload
			}
			got = append(got, judgeItem{Action: step.Action, Detail: step.Detail, Status: step.Status, Output: step.Output, Raw: payload})
		}
		if len(calls) > 0 {
			push(TaskStep{Action: "judge", Status: "ok", Output: judgeFindings(got)})
			return steps
		}
	}
	for len(steps) < 6 {
		transcript := "问题：" + strings.TrimSpace(req.Message) + "\n" + formatSteps(steps)
		var act TaskAction
		if decide != nil {
			act = decide(transcript)
		} else if s != nil {
			req.emit(TaskStep{Action: "decide", Status: "running", Output: "正在决定下一步"})
			act = s.decideStep(ctx, cfg, model, transcript)
		}
		switch strings.ToLower(strings.TrimSpace(act.Action)) {
		case "", "done":
			return steps
		case "analyze":
			push(TaskStep{Action: "analyze", Status: "error", Output: "助手只能查询，禁止执行脚本"})
			continue
		default:
		}
		if strings.TrimSpace(act.Script) != "" {
			push(TaskStep{Action: act.Action, Status: "error", Output: "助手只能查询，禁止执行脚本"})
			continue
		}
		if reason := DetectDanger(act.SQL); strings.TrimSpace(act.SQL) != "" && reason != "" {
			push(TaskStep{Action: act.Action, Status: "error", Detail: act.Database, Output: reason})
			continue
		}
		switch strings.ToLower(strings.TrimSpace(act.Action)) {
		case "", "done":
			return steps
		case "catalog":
			step, inst := s.catalogStep(req.TenantID, req.Message)
			push(step)
			if inst != nil {
				st.inst = inst
			}
		case "prometheus":
			if st.inst == nil {
				push(TaskStep{Action: "prometheus", Status: "error", Detail: act.Expr, Output: "未对上平台实例，未查询 Prometheus"})
				continue
			}
			step, payload := s.promStep(ctx, st.inst, act.Expr, act.Hours)
			push(step)
			if payload != "" {
				st.lastJSON = payload
			}
		case "postgres", "clickhouse":
			if st.inst == nil {
				push(TaskStep{Action: act.Action, Status: "error", Detail: act.Query, Output: "未对上平台实例，未查询。"})
				continue
			}
			var step TaskStep
			var payload string
			if act.Action == "postgres" && strings.TrimSpace(act.SQL) != "" {
				step, payload = s.sqlSelectStep(ctx, req.TenantID, st.inst, act.Database, act.SQL)
			} else {
				step, payload = s.runScaffold(ctx, req.TenantID, st.inst, act.Action, act.Query, 0)
			}
			push(step)
			if payload != "" {
				st.lastJSON = payload
			}
		default:
			push(TaskStep{Action: act.Action, Status: "error", Output: "未知动作"})
		}
	}
	return steps
}

func (req ChatRequest) emit(step TaskStep) {
	if req.OnStep != nil {
		req.OnStep(step)
	}
}

const beCPUExpr = `100 * (1 - sum by (instance) (rate(starrocks_be_cpu{mode="idle",__SEL__}[5m])) / clamp_min(sum by (instance) (rate(starrocks_be_cpu{__SEL__}[5m])), 1e-9))`

func (s *Service) decideStep(ctx context.Context, cfg Config, model, transcript string) TaskAction {
	var text string
	var err error
	if cfg.Provider == ProviderCursor {
		ans, e := s.cursorAnswer(ctx, cfg, model, taskDecidePrompt+"\n\n"+transcript, nil, "")
		err = e
		if ans != nil {
			text = ans.Answer
		}
	} else {
		text, _, err = s.openaiComplete(ctx, cfg, model, []any{
			map[string]string{"role": "system", "content": taskDecidePrompt},
			map[string]string{"role": "user", "content": transcript},
		}, false)
	}
	if err != nil {
		return TaskAction{Action: "done"}
	}
	act, _ := parseTaskAction(text)
	return act
}

func parseTaskAction(text string) (TaskAction, bool) {
	text = strings.TrimSpace(text)
	i := strings.Index(text, "{")
	j := strings.LastIndex(text, "}")
	if i < 0 || j <= i {
		return TaskAction{Action: "done"}, false
	}
	var act TaskAction
	if err := json.Unmarshal([]byte(text[i:j+1]), &act); err != nil {
		return TaskAction{Action: "done"}, false
	}
	act.Action = strings.ToLower(strings.TrimSpace(act.Action))
	act.Expr = strings.TrimSpace(act.Expr)
	act.Query = strings.TrimSpace(act.Query)
	act.Database = strings.TrimSpace(act.Database)
	act.SQL = strings.TrimSpace(act.SQL)
	if act.Hours < 0 {
		act.Hours = 0
	}
	if act.Hours > 6 {
		act.Hours = 6
	}
	return act, true
}

func wantsCatalogCount(message string) bool {
	if !strings.Contains(message, "实例") && !strings.Contains(strings.ToLower(message), "instance") {
		return false
	}
	return strings.Contains(message, "登记") || strings.Contains(message, "多少") ||
		strings.Contains(message, "几个") || strings.Contains(message, "数量") || strings.Contains(message, "几台")
}

func (s *Service) catalogCountStep(tenant, message string) (TaskStep, string) {
	engine := aiops.ParseUtterance(message).Engine
	if s == nil || s.Store == nil {
		return TaskStep{Action: "catalog", Status: "error", Detail: engine, Output: "实例目录不可用，未统计登记数量。"}, ""
	}
	list, err := s.Store.ListInstancesInTenant(tenant, "", "")
	if err != nil {
		return TaskStep{Action: "catalog", Status: "error", Detail: engine, Output: "查询实例目录失败：" + err.Error()}, ""
	}
	matched := make([]store.Instance, 0, len(list))
	for _, it := range list {
		if catalogEngineMatch(it.DBType, engine) {
			matched = append(matched, it)
		}
	}
	label := "全部"
	if engine != "" {
		label = engine
	}
	var b strings.Builder
	fmtCount := len(matched)
	b.WriteString("平台实例目录登记数量：")
	b.WriteString(label)
	b.WriteString(" ")
	b.WriteString(strconv.Itoa(fmtCount))
	b.WriteString(" 台\n")
	show := matched
	if len(show) > 30 {
		show = show[:30]
	}
	for _, it := range show {
		b.WriteString("- ")
		b.WriteString(formatInstanceFact(&it))
		b.WriteByte('\n')
	}
	if len(matched) > len(show) {
		b.WriteString("其余未列出\n")
	}
	payload, _ := json.Marshal(map[string]any{
		"engine": label,
		"count":  fmtCount,
	})
	return TaskStep{Action: "catalog", Status: "ok", Detail: label, Output: strings.TrimSpace(b.String())}, string(payload)
}

func catalogEngineMatch(dbType, want string) bool {
	if strings.TrimSpace(want) == "" {
		return true
	}
	got := store.NormalizeEngineType(dbType)
	switch want {
	case "starrocks", "doris":
		return got == "starrocks" || got == "doris" || got == "sr"
	default:
		return got == want
	}
}

func (s *Service) catalogStep(tenant, message string) (TaskStep, *store.Instance) {
	host := strings.TrimSpace(aiops.ParseUtterance(message).Object)
	if err := validatePromHost(host); err != nil {
		return TaskStep{Action: "catalog", Status: "error", Output: "未识别出合法实例主机，未到 Prometheus 查询。"}, nil
	}
	if s != nil && s.catalogLookup != nil {
		it, err := s.catalogLookup(tenant, host)
		if err != nil || it == nil {
			msg := "目录里没有实例 " + host + "，未到 Prometheus 查询。"
			if err != nil {
				msg = err.Error()
			}
			return TaskStep{Action: "catalog", Status: "error", Detail: host, Output: msg}, nil
		}
		return TaskStep{Action: "catalog", Status: "ok", Detail: it.DBType, Output: formatInstanceMatch(it, host)}, it
	}
	if s == nil || s.Store == nil {
		return TaskStep{Action: "catalog", Status: "error", Detail: host, Output: "实例目录不可用，未到 Prometheus 查询。"}, nil
	}
	list, err := s.Store.ListInstancesInTenant(tenant, "", host)
	if err != nil {
		return TaskStep{Action: "catalog", Status: "error", Detail: host, Output: "查询实例目录失败：" + err.Error()}, nil
	}
	it := pickInstance(list, host)
	if it == nil {
		return TaskStep{Action: "catalog", Status: "error", Detail: host, Output: "目录里没有实例 " + host + "，未到 Prometheus 查询。"}, nil
	}
	return TaskStep{Action: "catalog", Status: "ok", Detail: it.DBType, Output: formatInstanceMatch(it, host)}, it
}

func (s *Service) promStep(ctx context.Context, inst *store.Instance, expr string, hours int) (TaskStep, string) {
	res := s.ExecProm(ctx, inst, expr, hours)
	out := trim(res.Text(), 1500)
	if !res.OK {
		return TaskStep{Action: "prometheus", Status: "error", Detail: expr, Output: out}, ""
	}
	payload, _ := json.Marshal(map[string]any{
		"selector": res.Selector,
		"series":   res.Series,
	})
	return TaskStep{Action: "prometheus", Status: "ok", Detail: res.Selector, Output: out}, string(payload)
}

func analyzeStep(script, input string) TaskStep {
	_ = script
	_ = input
	return TaskStep{Action: "analyze", Status: "error", Output: "助手只能查询，禁止执行脚本"}
}

func formatSteps(steps []TaskStep) string {
	if len(steps) == 0 {
		return ""
	}
	var b strings.Builder
	for i, st := range steps {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(st.Action + " [" + st.Status + "]")
		if st.Detail != "" {
			b.WriteString(" " + st.Detail)
		}
		if st.Output != "" {
			b.WriteString("\n" + st.Output)
		}
	}
	return b.String()
}
