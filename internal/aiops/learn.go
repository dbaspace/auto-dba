package aiops

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"auto-dba/internal/store"
)

// LearnOptions 人工确认学习入库参数。
type LearnOptions struct {
	AsSystem    bool   // true：标记为系统案例（平台知识库，参与 RAG，库内显示「系统」）
	Title       string // 可选自定义标题
	MergeSystem bool   // 是否合并进已对齐的内置指纹 action（默认 true）
}

// LearnResult 学习入库结果。
type LearnResult struct {
	Fingerprint string `json:"fingerprint"`
	Title       string `json:"title"`
	MetricKey   string `json:"metric_key"`
	AlertNames  string `json:"alert_names"`
	System      bool   `json:"system"`
	Engine      string `json:"engine,omitempty"`
}

// AbsorbLLMAnswer 将大模型回答沉淀为库内案例（兼容旧调用，默认非系统、可合并对齐案例）。
func (s *Service) AbsorbLLMAnswer(ctx context.Context, question, answer string, rag []RAGHit) error {
	_, err := s.ConfirmLearn(ctx, question, answer, rag, LearnOptions{AsSystem: false, MergeSystem: true})
	return err
}

// ConfirmLearn 人工确认后写入知识库；AsSystem=true 时作为平台系统案例（等价「加入内置目录」的运行时形态）。
func (s *Service) ConfirmLearn(ctx context.Context, question, answer string, rag []RAGHit, opt LearnOptions) (*LearnResult, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("store 不可用")
	}
	q := strings.TrimSpace(question)
	a := strings.TrimSpace(answer)
	if q == "" || a == "" {
		return nil, fmt.Errorf("问题与回答不能为空")
	}
	if utf8.RuneCountInString(a) < 40 {
		return nil, fmt.Errorf("回答过短，不建议入库")
	}
	low := strings.ToLower(a)
	if strings.Contains(low, "llm 调用失败") || strings.Contains(low, "未配置") {
		return nil, fmt.Errorf("失败/未配置类回答不可入库")
	}

	q = strings.ToValidUTF8(q, "")
	a = strings.ToValidUTF8(a, "")
	fp, metric, alertNames, title, mergeSystem := pickLearnTarget(q, a, rag)
	if t := strings.TrimSpace(opt.Title); t != "" {
		title = strings.ToValidUTF8(t, "")
	}
	if !opt.MergeSystem {
		mergeSystem = false
	}
	now := time.Now().UTC()
	learnedFP := "learn|" + shortHash(q)[:12]
	// 学习正文不截断（列为 TEXT）；仅清洗非法 UTF-8，并做可读换行
	learned := &store.SqlInsightCaseRow{
		ID: "learn-" + shortHash(q)[:16], Fingerprint: learnedFP,
		MetricKey: metric, Severity: "info", Direction: "learned",
		Title: strings.ToValidUTF8(title, ""),
		RootCauseTemplate: strings.ToValidUTF8("来自助手问答沉淀："+q, ""),
		ActionTemplate:    strings.ToValidUTF8(normalizeLearnedText(a), ""),
		AlertNames:        alertNames, System: opt.AsSystem, LastSeenAt: &now,
	}
	if err := s.Store.UpsertSqlInsightCase(ctx, learned); err != nil {
		return nil, err
	}
	// Upsert 不强制 system 字段时再 patch
	if opt.AsSystem {
		learned.System = true
		_ = s.Store.PatchSqlInsightSystemCase(ctx, learned)
	}

	if mergeSystem && fp != "" {
		existing, err := s.Store.GetSqlInsightCaseByFingerprint(ctx, fp)
		if err != nil {
			return nil, err
		}
		if existing != nil && topicsCompatible(q, a, existing.Title, existing.ActionTemplate, existing.AlertNames) {
			merged := mergeLearnedAction(existing.ActionTemplate, a)
			if merged != existing.ActionTemplate {
				existing.ActionTemplate = merged
				if existing.AlertNames == "" {
					existing.AlertNames = alertNames
				}
				_ = s.Store.PatchSqlInsightSystemCase(ctx, existing)
			}
		}
	}
	eng := InferCaseEngine(learnedFP, title, alertNames, metric)
	return &LearnResult{
		Fingerprint: learnedFP, Title: title, MetricKey: metric,
		AlertNames: alertNames, System: opt.AsSystem, Engine: eng,
	}, nil
}

// pickLearnTarget 优先按问题主题归类；RAG 仅在与问题主题对齐时采用（避免 MHA 挂到 PG 慢查询）。
func pickLearnTarget(q, answer string, rag []RAGHit) (fingerprint, metric, alertNames, title string, mergeSystem bool) {
	if t, ok := topicFromQuestion(q); ok {
		// 若 RAG 同主题且强命中，可挂靠该指纹以便合并系统案例
		if h, ok2 := firstAlignedRAG(q, rag); ok2 && sameEngineTopic(t.engine, h) {
			return h.Fingerprint, coalesce(h.MetricKey, t.metric), coalesce(h.AlertNames, t.alertNames),
				learnTitle(q), true
		}
		return t.fingerprint, t.metric, t.alertNames, learnTitle(q), t.fingerprint != ""
	}
	if h, ok := firstAlignedRAG(q, rag); ok {
		// 标题用问题，避免「学习补充：无关案例标题」
		return h.Fingerprint, coalesce(h.MetricKey, "oncall_triage"), h.AlertNames,
			learnTitle(q), true
	}
	// 答案自称与知识库不对齐：只按问题落独立学习条，不合并系统案例
	return "", "oncall_triage", defaultAlertForQuestion(q), learnTitle(q), false
}

// learnTitle 标题保留完整问题（列表展示由前端 ellipsis）。
func learnTitle(q string) string {
	q = strings.TrimSpace(strings.ToValidUTF8(q, ""))
	if q == "" {
		return "学习补充"
	}
	return "学习补充：" + q
}

type learnTopic struct {
	fingerprint, metric, alertNames, engine string
}

func topicFromQuestion(q string) (learnTopic, bool) {
	hay := strings.ToLower(q)
	switch {
	case strings.Contains(hay, "mha") || strings.Contains(hay, "orchestrator") ||
		(strings.Contains(hay, "mysql") && (strings.Contains(hay, "高可用") || strings.Contains(hay, "切换") || strings.Contains(hay, "部署"))):
		return learnTopic{
			fingerprint: "mysql_mha|info|manual", metric: "mysql_ha",
			alertNames: "MySQLHA", engine: "mysql",
		}, true
	case strings.Contains(hay, "proxysql") || strings.Contains(hay, "读写分离"):
		return learnTopic{
			fingerprint: "mysql_proxysql|info|manual", metric: "mysql_ha",
			alertNames: "MySQLHA", engine: "mysql",
		}, true
	case strings.Contains(hay, "redis") && (strings.Contains(hay, "哨兵") || strings.Contains(hay, "sentinel") || strings.Contains(hay, "集群") || strings.Contains(hay, "切换")):
		return learnTopic{
			fingerprint: "failover|warning|above|redis", metric: "failover_events",
			alertNames: "RedisFailover", engine: "redis",
		}, true
	case strings.Contains(hay, "starrocks") || strings.Contains(hay, "be 宕") || strings.Contains(hay, "be宕"):
		return learnTopic{
			fingerprint: "be_down|warning|above|sr", metric: "be_alive_ratio",
			alertNames: "SRBEDown", engine: "starrocks",
		}, true
	case strings.Contains(hay, "wal") || strings.Contains(hay, "pg_wal"):
		return learnTopic{
			fingerprint: "wal_size|warning|above", metric: "wal_retain_bytes",
			alertNames: "PGStorage", engine: "postgres",
		}, true
	case (strings.Contains(hay, "连接") || strings.Contains(hay, "打满") || strings.Contains(hay, "too many")) &&
		!strings.Contains(hay, "部署"):
		eng, an, fp := "postgres", "PGConnHigh", "conn_usage_ratio|warning|above"
		if strings.Contains(hay, "mysql") {
			eng, an, fp = "mysql", "MySQLConn", "conn_usage_ratio|warning|above|mysql"
		}
		if strings.Contains(hay, "redis") {
			eng, an, fp = "redis", "RedisConnHigh", "conn_usage_ratio|warning|above|redis"
		}
		return learnTopic{fingerprint: fp, metric: "conn_usage_ratio", alertNames: an, engine: eng}, true
	case strings.Contains(hay, "主从延迟") || (strings.Contains(hay, "复制延迟") && !strings.Contains(hay, "部署")):
		eng, an, fp := "postgres", "PGReplicationLag", "replication_lag|warning|above"
		if strings.Contains(hay, "mysql") {
			eng, an, fp = "mysql", "MySQLReplicationLag", "replication_lag|warning|above|mysql"
		}
		return learnTopic{fingerprint: fp, metric: "replication_lag", alertNames: an, engine: eng}, true
	case strings.Contains(hay, "死锁"):
		eng, an, fp := "postgres", "PGDeadlocks", "deadlocks_ps|warning|above"
		if strings.Contains(hay, "mysql") {
			eng, an, fp = "mysql", "MySQLDeadlocks", "deadlocks_ps|warning|above|mysql"
		}
		return learnTopic{fingerprint: fp, metric: "deadlocks_ps", alertNames: an, engine: eng}, true
	case (strings.Contains(hay, "慢查询") || strings.Contains(hay, "慢 sql") || strings.Contains(hay, "slow query")) &&
		!strings.Contains(hay, "部署") && !strings.Contains(hay, "mha"):
		eng, an, fp := "postgres", "PGSlowQuery", "slow_query_cnt|warning|above"
		if strings.Contains(hay, "mysql") {
			eng, an, fp = "mysql", "MySQLSlowQuery", "slow_query_cnt|warning|above|mysql"
		}
		return learnTopic{fingerprint: fp, metric: "slow_query_cnt", alertNames: an, engine: eng}, true
	case strings.Contains(hay, "大key") || strings.Contains(hay, "bigkey"):
		return learnTopic{
			fingerprint: "bigkey|warning|above|redis", metric: "bigkey_count",
			alertNames: "RedisBigKey", engine: "redis",
		}, true
	}
	return learnTopic{}, false
}

func firstAlignedRAG(q string, rag []RAGHit) (RAGHit, bool) {
	filtered := FilterRelevantRAGHits(q, rag)
	for _, h := range filtered {
		if !StrongRAGHit([]RAGHit{h}) {
			continue
		}
		if !ragAlignedToQuestion(q, h) {
			continue
		}
		return h, true
	}
	return RAGHit{}, false
}

func ragAlignedToQuestion(q string, h RAGHit) bool {
	qEng := InferCaseEngine("", q, "", "")
	if qEng == "" {
		// 从问题关键词猜引擎
		if t, ok := topicFromQuestion(q); ok {
			qEng = t.engine
		}
	}
	hEng := InferCaseEngine(h.Fingerprint, h.Title, h.AlertNames, h.MetricKey)
	if qEng != "" && hEng != "" && qEng != hEng {
		return false
	}
	// 问题是 HA/部署类，禁止挂到慢查询/CPU 等性能案例
	ql := strings.ToLower(q)
	hl := strings.ToLower(h.Title + " " + h.MetricKey)
	if isHADeployQuestion(ql) && (strings.Contains(hl, "慢查询") || strings.Contains(hl, "slow") ||
		strings.Contains(hl, "cpu") || strings.Contains(hl, "连接使用")) {
		return false
	}
	return true
}

func isHADeployQuestion(ql string) bool {
	return strings.Contains(ql, "mha") || strings.Contains(ql, "orchestrator") ||
		strings.Contains(ql, "高可用") || strings.Contains(ql, "部署方案") ||
		(strings.Contains(ql, "部署") && (strings.Contains(ql, "mysql") || strings.Contains(ql, "主从") || strings.Contains(ql, "切换")))
}

func sameEngineTopic(engine string, h RAGHit) bool {
	if engine == "" {
		return true
	}
	hEng := InferCaseEngine(h.Fingerprint, h.Title, h.AlertNames, h.MetricKey)
	return hEng == "" || hEng == engine
}

func topicsCompatible(q, answer, title, action, alertNames string) bool {
	if answerClaimsMisaligned(answer) {
		return false
	}
	qEng := ""
	if t, ok := topicFromQuestion(q); ok {
		qEng = t.engine
	}
	tEng := InferCaseEngine("", title, alertNames, "")
	if qEng != "" && tEng != "" && qEng != tEng {
		return false
	}
	if isHADeployQuestion(strings.ToLower(q)) {
		al := strings.ToLower(title + " " + action)
		if strings.Contains(al, "慢查询") && !strings.Contains(al, "mha") && !strings.Contains(al, "高可用") {
			return false
		}
	}
	return true
}

func answerClaimsMisaligned(answer string) bool {
	low := strings.ToLower(answer)
	return strings.Contains(low, "不对齐") || strings.Contains(low, "不相关") ||
		strings.Contains(low, "与主题不符") || strings.Contains(low, "主题不对")
}

func defaultAlertForQuestion(q string) string {
	if t, ok := topicFromQuestion(q); ok {
		return t.alertNames
	}
	return "PGActiveSessions"
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func mergeLearnedAction(old, llm string) string {
	old = strings.TrimSpace(old)
	llm = strings.TrimSpace(llm)
	if llm == "" {
		return old
	}
	if answerClaimsMisaligned(llm) {
		return old
	}
	marker := "【助手补充】"
	snippet := normalizeLearnedText(strings.ToValidUTF8(llm, ""))
	if strings.Contains(old, marker) {
		idx := strings.Index(old, marker)
		base := strings.TrimSpace(old[:idx])
		return base + "\n" + marker + "\n" + snippet
	}
	if old == "" {
		return marker + "\n" + snippet
	}
	if strings.Contains(old, "确认是否真打满") && strings.Contains(old, "看谁占连接") {
		return old
	}
	return old + "\n" + marker + "\n" + snippet
}

// normalizeLearnedText 把模型一整段输出软折成多行，避免库内展示成超长单行。
func normalizeLearnedText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" || strings.Contains(s, "\n") {
		return s
	}
	s = strings.NewReplacer("。", "。\n", "；", "；\n", "！", "！\n", "？", "？\n").Replace(s)
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if i > 0 && runes[i] >= '1' && runes[i] <= '9' {
			if i+1 < len(runes) && (runes[i+1] == ')' || runes[i+1] == '.' || runes[i+1] == '、' || runes[i+1] == '．') {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
			}
		}
		b.WriteRune(runes[i])
	}
	out := b.String()
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(strings.ToLower(s))))
	return hex.EncodeToString(sum[:])
}

// FormatLearnNote 供测试/日志。
func FormatLearnNote(fp string) string {
	return fmt.Sprintf("absorbed→%s", fp)
}
