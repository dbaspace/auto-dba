package aiops

import (
	"regexp"
	"strings"
)

// Utterance 用户原句的词法、语法分析结果。直答和拒绝都只看整句含义。
type Utterance struct {
	Action  string // 查看 | 排查 | 更新 | 删除 | 修改 | 改 …
	Object  string // 主机或实例名
	Target  string // 数据 | 表 | 库 | 记录 | 配置
	Engine  string
	Time    string
	Symptom string
	Write   bool
}

var (
	hostRE = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?\.)+(?:[a-z]{2,})\b`)
	ipRE   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	timeRE = regexp.MustCompile(`最近\s*\d+\s*(?:个)?(?:小时|分钟|天|秒)|近\s*\d+\s*(?:小时|分钟|天)|今天|昨日`)
)

// 症状专名。泛词（运行、情况、最近、小时）不在此列。
var symptomLex = []struct {
	needles []string
	id      string
}{
	{[]string{"too many connections", "连接打满", "连接满", "连接接近", "连接使用率", "连接数", "连接"}, "连接"},
	{[]string{"notready", "not ready"}, "notready"},
	{[]string{"慢查询", "慢sql", "slow query", "slow_query"}, "慢查询"},
	{[]string{"大key", "大 key", "bigkey"}, "大key"},
	{[]string{"热key", "热 key", "hotkey"}, "热key"},
	{[]string{"be宕机", "be 宕机", "宕机"}, "宕机"},
	{[]string{"wal", "pg_wal"}, "wal"},
	{[]string{"死锁", "deadlock"}, "死锁"},
	{[]string{"膨胀", "bloat"}, "膨胀"},
	{[]string{"复制延迟", "主从延迟"}, "延迟"},
	{[]string{"binlog"}, "binlog"},
	{[]string{"oomkilled", "oom"}, "oom"},
}

// 写动作按长短排列，避免「改」抢在「修改」前面。
var writeLex = []string{"修改", "更新", "删除", "插入", "清空", "恢复", "授权", "闪回", "杀掉", "改", "删"}

var writeTargets = []string{"数据", "配置", "记录", "表", "库"}

// ParseUtterance 把原句翻译成「在干什么」。
// 语气词（下 / 一下 / 了 / 的）不进入含义，写动作和对象会重新拼在一起。
func ParseUtterance(q string) Utterance {
	raw := strings.TrimSpace(q)
	low := strings.ToLower(raw)
	u := Utterance{
		Object:  detectObject(raw),
		Engine:  detectQuestionEngine(low),
		Time:    compactTime(timeRE.FindString(low)),
		Symptom: detectSymptom(low),
	}
	if verb, target, ok := detectWrite(raw); ok {
		u.Write = true
		u.Action = verb
		u.Target = target
		return u
	}
	u.Action = detectAction(raw)
	return u
}

// Meaning 标准语意，供判断和测试核对。
func (u Utterance) Meaning() string {
	if u.Write {
		if u.Target != "" {
			return u.Action + u.Target
		}
		return u.Action
	}
	action := u.Action
	if action == "" {
		action = "查看"
	}
	if u.Symptom != "" {
		if name := engineLabel(u.Engine); name != "" {
			return action + " " + name + " " + u.Symptom
		}
		return action + " " + u.Symptom
	}
	if u.Object != "" || u.Time != "" {
		s := action
		if u.Object != "" {
			s += "实例 " + u.Object
		}
		if u.Time != "" {
			s += " " + u.Time
		}
		return s + "的运行情况"
	}
	return action
}

func detectWrite(q string) (verb, target string, ok bool) {
	for _, word := range writeLex {
		idx := strings.Index(q, word)
		if idx < 0 {
			continue
		}
		rest := stripLeadingParticles(q[idx+len(word):])
		for _, t := range writeTargets {
			if strings.HasPrefix(rest, t) {
				return word, t, true
			}
		}
		return word, "", true
	}
	return "", "", false
}

func stripLeadingParticles(s string) string {
	for {
		s = strings.TrimLeft(s, " \t，,、")
		switch {
		case strings.HasPrefix(s, "一下"):
			s = strings.TrimPrefix(s, "一下")
		case strings.HasPrefix(s, "下"), strings.HasPrefix(s, "了"), strings.HasPrefix(s, "的"):
			_, rest := splitFirstRune(s)
			s = rest
		default:
			return s
		}
	}
}

func splitFirstRune(s string) (string, string) {
	for i := range s {
		if i > 0 {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

func detectAction(q string) string {
	switch {
	case strings.Contains(q, "排查"), strings.Contains(q, "原因"), strings.Contains(q, "怎么"), strings.Contains(q, "如何"), strings.Contains(q, "处理"):
		return "排查"
	default:
		return "查看"
	}
}

func detectObject(q string) string {
	if h := hostRE.FindString(q); h != "" {
		return h
	}
	return ipRE.FindString(q)
}

func detectQuestionEngine(low string) string {
	switch {
	case strings.Contains(low, "starrocks"):
		return "starrocks"
	case strings.Contains(low, "redis"):
		return "redis"
	case strings.Contains(low, "mysql"):
		return "mysql"
	case strings.Contains(low, "postgres"), strings.Contains(low, "postgresql"):
		return "postgres"
	case strings.Contains(low, "doris"):
		return "doris"
	case strings.Contains(low, "kubernetes"), strings.Contains(low, "k8s"):
		return "k8s"
	default:
		return ""
	}
}

func detectSymptom(low string) string {
	for _, rule := range symptomLex {
		for _, n := range rule.needles {
			if strings.Contains(low, n) {
				return rule.id
			}
		}
	}
	return ""
}

func compactTime(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
}

func engineLabel(engine string) string {
	switch engine {
	case "postgres":
		return "PostgreSQL"
	case "mysql":
		return "MySQL"
	case "redis":
		return "Redis"
	case "starrocks":
		return "StarRocks"
	case "doris":
		return "Doris"
	case "k8s":
		return "K8s"
	default:
		return ""
	}
}

// utteranceCovers 整句每一段都落在同一条案例上才算命中。
// 时间窗是运行情况的范围，内置案例不覆盖具体时段。
func utteranceCovers(u Utterance, h RAGHit) bool {
	if u.Symptom == "" || u.Time != "" {
		return false
	}
	hay := strings.ToLower(strings.Join([]string{
		h.Title, h.MetricKey, h.AlertNames, h.Snippet, h.RootCause, h.Action,
	}, " "))
	if u.Object != "" && !strings.Contains(hay, strings.ToLower(u.Object)) {
		return false
	}
	qEng := u.Engine
	hEng := InferCaseEngine(h.Fingerprint, h.Title, h.AlertNames, h.MetricKey)
	if qEng == "k8s" {
		if !strings.Contains(hay, "k8s") && !strings.Contains(hay, "notready") && !strings.Contains(hay, "pod") {
			return false
		}
	} else if qEng != "" && hEng != "" && qEng != hEng {
		return false
	} else if qEng != "" && hEng == "" && qEng != "k8s" {
		return false
	}
	return hitHasSymptom(hay, u.Symptom)
}

func hitHasSymptom(hay, id string) bool {
	switch id {
	case "连接":
		return strings.Contains(hay, "连接")
	case "wal":
		return strings.Contains(hay, "wal")
	case "慢查询":
		return strings.Contains(hay, "慢查询") || strings.Contains(hay, "slow")
	case "notready":
		return strings.Contains(hay, "notready") || strings.Contains(hay, "not ready")
	case "大key":
		return strings.Contains(hay, "大 key") || strings.Contains(hay, "大key") || strings.Contains(hay, "bigkey")
	case "热key":
		return strings.Contains(hay, "热 key") || strings.Contains(hay, "热key") || strings.Contains(hay, "hotkey")
	case "宕机":
		return strings.Contains(hay, "宕机") || strings.Contains(hay, "不可用")
	case "死锁":
		return strings.Contains(hay, "死锁") || strings.Contains(hay, "deadlock")
	case "膨胀":
		return strings.Contains(hay, "膨胀") || strings.Contains(hay, "bloat")
	case "延迟":
		return strings.Contains(hay, "延迟") || strings.Contains(hay, "lag")
	case "binlog":
		return strings.Contains(hay, "binlog")
	case "oom":
		return strings.Contains(hay, "oom")
	default:
		return strings.Contains(hay, id)
	}
}
