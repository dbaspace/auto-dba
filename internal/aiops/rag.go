package aiops

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

// RAGHit 关键词检索命中。
type RAGHit struct {
	Source      string  `json:"source"` // case | builtin
	Fingerprint string  `json:"fingerprint,omitempty"`
	Title       string  `json:"title"`
	MetricKey   string  `json:"metric_key,omitempty"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	AlertNames  string  `json:"alert_names,omitempty"`
	Action      string  `json:"action,omitempty"`
	RootCause   string  `json:"root_cause,omitempty"`
}

// StrongRAGThreshold 直答阈值：达到则优先用知识库、不调大模型。
const StrongRAGThreshold = 2.0

// RAGSearch 对库内案例 + 内置知识库做简单关键词打分。
func (s *Service) RAGSearch(ctx context.Context, q string, limit int) ([]RAGHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	tokens := tokenize(q)
	var hits []RAGHit

	if s != nil && s.Store != nil {
		rows, _, err := s.Store.ListSqlInsightCases(ctx, 0, 300)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r == nil {
				continue
			}
			text := strings.Join([]string{r.Title, r.MetricKey, r.AlertNames, r.RootCauseTemplate, r.ActionTemplate}, " ")
			sc := scoreText(text, tokens)
			if sc <= 0 {
				continue
			}
			hits = append(hits, RAGHit{
				Source: "case", Fingerprint: r.Fingerprint, Title: r.Title, MetricKey: r.MetricKey,
				Snippet: trimSnippet(r.RootCauseTemplate, 160), Score: sc, AlertNames: r.AlertNames,
				RootCause: r.RootCauseTemplate, Action: r.ActionTemplate,
			})
		}
	}
	for _, kc := range AllKnowledgeCases() {
		text := strings.Join([]string{
			kc.Title, kc.MetricKey, strings.Join(kc.AlertNames, " "),
			kc.RootCauseTemplate, kc.ActionTemplate, strings.Join(kc.Tags, " "),
		}, " ")
		sc := scoreText(text, tokens)
		if sc <= 0 {
			continue
		}
		hits = append(hits, RAGHit{
			Source: "builtin", Fingerprint: kc.Fingerprint, Title: kc.Title, MetricKey: kc.MetricKey,
			Snippet: trimSnippet(kc.RootCauseTemplate, 160), Score: sc + 0.01, // 略抬内置便于曝光
			AlertNames: JoinAlertNames(kc.AlertNames),
			RootCause:  kc.RootCauseTemplate, Action: kc.ActionTemplate,
		})
	}
	// 按 fingerprint 去重，保留高分
	best := map[string]RAGHit{}
	for _, h := range hits {
		k := h.Fingerprint
		if k == "" {
			k = h.Title
		}
		if old, ok := best[k]; !ok || h.Score > old.Score {
			best[k] = h
		}
	}
	hits = hits[:0]
	for _, h := range best {
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// StrongRAGHit 是否足以直答（跳过 LLM）。hits 应先经 FilterRelevantRAGHits。
func StrongRAGHit(hits []RAGHit) bool {
	if len(hits) == 0 {
		return false
	}
	return hits[0].Score >= StrongRAGThreshold
}

// FilterRelevantRAGHits 去掉仅靠「数据库/原因」等停用词凑分的无关命中。
func FilterRelevantRAGHits(q string, hits []RAGHit) []RAGHit {
	content := contentTokens(tokenize(q))
	if len(content) == 0 {
		return nil
	}
	var out []RAGHit
	for _, h := range hits {
		hay := strings.ToLower(strings.Join([]string{
			h.Title, h.MetricKey, h.AlertNames, h.Snippet, h.RootCause, h.Action,
		}, " "))
		ok := false
		for _, t := range content {
			if strings.Contains(hay, t) {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

// FormatRAGAnswer 把高分命中格式化为中文直答。
func FormatRAGAnswer(q string, hits []RAGHit) string {
	hits = FilterRelevantRAGHits(q, hits)
	var b strings.Builder
	b.WriteString("【知识库直答】已命中内置/库内案例（未调用大模型）。\n")
	b.WriteString("问题：" + strings.TrimSpace(q) + "\n")
	shown := 0
	for _, h := range hits {
		if shown > 0 && h.Score < StrongRAGThreshold*0.6 {
			continue
		}
		shown++
		if shown > 3 {
			break
		}
		b.WriteString("\n## ")
		b.WriteString(h.Title)
		b.WriteString("\n来源：")
		b.WriteString(h.Source)
		if h.AlertNames != "" {
			b.WriteString(" · alertname=")
			b.WriteString(h.AlertNames)
		}
		b.WriteString("\n")
		if rc := strings.TrimSpace(h.RootCause); rc != "" {
			b.WriteString("根因：")
			b.WriteString(rc)
			b.WriteString("\n")
		} else if sn := strings.TrimSpace(h.Snippet); sn != "" {
			b.WriteString("摘要：")
			b.WriteString(sn)
			b.WriteString("\n")
		}
		if act := strings.TrimSpace(h.Action); act != "" {
			b.WriteString("建议：")
			b.WriteString(act)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n若需结合当前告警实例指标再分析，可附 alert 后追问，或点助手抽屉顶部/底部的「强制刷新」按钮调用大模型。")
	return b.String()
}

// 停用/泛化词：几乎每条案例都可能沾边，权重压低，且不算「主题相关」。
var ragStopwords = map[string]struct{}{
	"postgresql": {}, "postgres": {}, "pgsql": {}, "pg": {},
	"数据库": {}, "数据": {}, "据库": {}, "库的": {},
	"原因": {}, "的原": {}, "大的": {}, "大的原因": {}, "什么": {}, "怎么": {}, "如何": {},
	"问题": {}, "请问": {}, "一下": {}, "偏高": {}, "偏低": {}, "过高": {}, "过低": {},
	"使用": {}, "使用率": {}, "主机": {}, "实例": {}, "建议": {}, "排查": {}, "诊断": {},
}

// 技术词加权：专名命中应压过「数据库」这类泛词。
var ragTechBoost = map[string]float64{
	"wal": 4, "pg_wal": 4, "pgwal": 4, "槽": 3, "slot": 3, "replication": 2.5,
	"xid": 3.5, "wraparound": 3.5, "vacuum": 3, "autovacuum": 3, "膨胀": 3, "bloat": 3,
	"死锁": 3, "deadlock": 3, "连接": 3, "打满": 3.5, "max_connections": 3,
	"缓存": 2.5, "cache": 2.5, "慢查询": 3, "归档": 3, "archive": 3,
	"复制": 2.5, "lag": 2.5, "checkpoint": 2.5, "锁等待": 3,
	"redis": 3, "bigkey": 4, "hotkey": 4, "sentinel": 3, "slowlog": 3,
	"mysql": 2.5, "binlog": 3.5, "innodb": 3, "processlist": 3,
	"starrocks": 3.5, "tablet": 3, "broker": 3, "compaction": 3, "be": 2.5, "fe": 2.5,
	"mha": 4, "orchestrator": 3.5, "proxysql": 3, "高可用": 3,
}

// 运维同义词：口语 → 知识库常见词。
var ragSynonyms = map[string][]string{
	"打满":      {"连接", "连接使用率", "max_connections", "pgconnhigh", "remaining connection"},
	"连不上":     {"连接", "连接使用率", "max_connections"},
	"连接满":     {"连接使用率", "连接接近打满", "pgconnhigh"},
	"膨胀":      {"死元组", "bloat", "vacuum"},
	"延迟":      {"复制", "lag", "replication"},
	"慢sql":     {"慢查询", "slow_query"},
	"慢查询":     {"slow_query", "topsql"},
	"死锁":      {"deadlock", "锁"},
	"长事务":     {"xact", "idle in transaction"},
	"空闲事务":    {"idle in transaction", "长事务"},
	"复制槽":     {"slot", "wal", "replication"},
	"磁盘满":     {"存储", "disk", "pg_wal"},
	"wraparound":  {"xid", "冻结", "vacuum"},
	"wal":       {"pg_wal", "wal_retain_bytes", "复制槽"},
	"wal大":     {"wal", "pg_wal", "wal_retain_bytes", "复制槽"},
	"wal高":     {"wal", "pg_wal", "wal_retain_bytes", "复制槽"},
	"wal涨":     {"wal", "pg_wal", "archive", "复制槽"},
	"大key":     {"bigkey", "unlink", "redis"},
	"热key":     {"hotkey", "redis"},
	"主从延迟":    {"replication", "lag", "复制"},
	"too many connections": {"连接", "max_connections", "mysql"},
	"binlog":    {"mysql", "disk", "replication"},
	"starrocks": {"be", "fe", "tablet", "导入"},
	"导入失败":    {"load", "starrocks", "broker"},
	"be宕机":     {"be", "starrocks", "heartbeat"},
	"mha":       {"mysql", "高可用", "主从", "切换"},
	"高可用":     {"ha", "mha", "切换"},
}

func tokenize(q string) []string {
	q = strings.ToLower(q)
	seen := map[string]struct{}{}
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			return
		}
		// ASCII：至少 2 字符；CJK：至少 2 个 rune
		if isASCII(t) {
			if len(t) < 2 {
				return
			}
		} else if runeLen(t) < 2 {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
		for _, syn := range ragSynonyms[t] {
			s := strings.ToLower(syn)
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}

	var ascii strings.Builder
	var cjk []rune
	flushASCII := func() {
		if ascii.Len() > 0 {
			add(ascii.String())
			ascii.Reset()
		}
	}
	flushCJK := func() {
		if len(cjk) == 0 {
			return
		}
		// 二元组：连接 / 打满 / 排查 …
		for i := 0; i+1 < len(cjk); i++ {
			add(string(cjk[i : i+2]))
		}
		// 短词整段（≤6）便于专名命中
		if len(cjk) >= 2 && len(cjk) <= 6 {
			add(string(cjk))
		}
		// 四元组：连接打满、使用率偏 等
		for i := 0; i+3 < len(cjk); i++ {
			add(string(cjk[i : i+4]))
		}
		cjk = cjk[:0]
	}

	for _, r := range q {
		switch {
		case r <= 127 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'):
			flushCJK()
			ascii.WriteRune(r)
		case r > 127 && (unicode.IsLetter(r) || unicode.Is(unicode.Han, r)):
			flushASCII()
			cjk = append(cjk, r)
		default:
			flushASCII()
			flushCJK()
		}
	}
	flushASCII()
	flushCJK()
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func runeLen(s string) int {
	return len([]rune(s))
}

func contentTokens(tokens []string) []string {
	var out []string
	for _, t := range tokens {
		if _, stop := ragStopwords[t]; stop {
			continue
		}
		out = append(out, t)
	}
	return out
}

func tokenWeight(t string) float64 {
	if _, stop := ragStopwords[t]; stop {
		return 0.1
	}
	if w, ok := ragTechBoost[t]; ok {
		return w
	}
	// 默认：ASCII 标识略升，普通中文二元组保持 1
	if isASCII(t) {
		return 1.8
	}
	return 1.0
}

func scoreText(text string, tokens []string) float64 {
	low := strings.ToLower(text)
	head := low
	if len(head) > 120 {
		head = head[:120]
	}
	var sc float64
	for _, t := range tokens {
		if !strings.Contains(low, t) {
			continue
		}
		w := tokenWeight(t)
		sc += w
		if strings.Contains(head, t) {
			sc += w * 0.5
		}
	}
	return sc
}

func trimSnippet(s string, n int) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "")
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
