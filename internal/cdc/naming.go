package cdc

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// sanitizeIdent 清洗为标识符片段：保留 [a-zA-Z0-9_]，其余替换为 _。
func sanitizeIdent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_':
			b = append(b, c)
		default:
			b = append(b, '_')
		}
	}
	out := strings.Trim(string(b), "_")
	return strings.ToLower(out)
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func cleanStringSlice(in []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func joinIdent(parts ...string) string {
	var out []string
	for _, p := range parts {
		s := sanitizeIdent(p)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "cdc_task"
	}
	return strings.Join(out, "_")
}

// randomLowerHex 返回 n 位小写 hex（crypto/rand），失败时回退纳秒时间戳。
func randomLowerHex(n int) string {
	if n <= 0 {
		return ""
	}
	bytesN := (n + 1) / 2
	buf := make([]byte, bytesN)
	if _, err := crand.Read(buf); err == nil {
		s := hex.EncodeToString(buf)
		if len(s) >= n {
			return s[:n]
		}
		return s
	}
	s := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(s) >= n {
		return s[len(s)-n:]
	}
	return s
}

// generatePublicationName 默认 Publication / Connector：rt_{env}_{db}
func generatePublicationName(env, dbName string) string {
	return joinIdent("rt", env, dbName)
}

// generateSlotName 默认 Slot：slot_{env}_{db}_{rand6}。不跟随自定义 publication。
func generateSlotName(env, dbName string) string {
	return "slot_" + joinIdent(env, dbName) + "_" + randomLowerHex(6)
}

func generateConnectorName(env, dbName string) string {
	return generatePublicationName(env, dbName)
}

func generateTopicPrefix(env, dbName string) string {
	return joinIdent(env, dbName)
}

// GenerateRoutineLoadName 对齐 hub：rl_{env}_{src_db}_{target_table}（rl 在前缀）。
func GenerateRoutineLoadName(env, srcDbName, targetTable string) string {
	return "rl_" + joinIdent(env, srcDbName, targetTable)
}

// RoutineLoadNameFor 对齐 hub routineLoadNameFor：
// topic_prefix 非空且与默认 {env}_{src_db} 不同时用 rl_{topic_prefix}_{target}，否则 rl_{env}_{src_db}_{target}。
func RoutineLoadNameFor(env, srcDb, topicPrefix, target string) string {
	tp := strings.TrimSpace(topicPrefix)
	def := generateTopicPrefix(env, srcDb)
	if tp != "" && tp != def {
		return "rl_" + joinIdent(tp, target)
	}
	return GenerateRoutineLoadName(env, srcDb, target)
}

// AddTableRoutineLoadName 新增表的 Routine Load 名只保留 rl_ 加目标表名。
func AddTableRoutineLoadName(target string) string {
	t := sanitizeIdent(target)
	if t == "" {
		t = "table"
	}
	return "rl_" + t
}

// RoutineLoadNameForTask 新增表用短名，其它任务沿用原来的前缀规则。
func RoutineLoadNameForTask(taskType, env, srcDb, topicPrefix, target string) string {
	return cdcRoutineLoadName(taskType, env, srcDb, topicPrefix, target, false)
}

// RoutineLoadNameForExtra 分库汇总任务和 fan-out 子任务的 Routine Load 按共享 topic_prefix 命名。
func RoutineLoadNameForExtra(taskType string, extra map[string]string, srcDb, target string) string {
	env, topic := "", ""
	if extra != nil {
		env = extra["env"]
		topic = extra["topic_prefix"]
	}
	return cdcRoutineLoadName(taskType, env, srcDb, topic, target, ShardMergedRL(extra))
}

func cdcRoutineLoadName(taskType, env, srcDb, topicPrefix, target string, shardMerged bool) string {
	if strings.EqualFold(strings.TrimSpace(taskType), "add_table") {
		return AddTableRoutineLoadName(target)
	}
	if shardMerged && strings.TrimSpace(topicPrefix) != "" {
		return "rl_" + joinIdent(topicPrefix, target)
	}
	return RoutineLoadNameFor(env, srcDb, topicPrefix, target)
}

// ShardMergedRL 分库（一条任务多库，或拆出的单库子任务）共用 topic 上的同一个 Routine Load。
func ShardMergedRL(extra map[string]string) bool {
	if extra == nil {
		return false
	}
	if strings.TrimSpace(extra["cdc_fanout_from"]) != "" {
		return true
	}
	if extra["shard_db"] != "true" && extra["source_scope_mode"] != "multi_db" {
		return false
	}
	return len(ResolveShardDBs(extra, extra["representative_db"])) >= 2
}

// InitNaming 预检/创建最终生效的四项命名。
type InitNaming struct {
	Env             string
	PublicationName string
	SlotName        string
	ConnectorName   string
	TopicPrefix     string
}

// InitNamingInput 用户覆盖项；空则按 env+source_db 生成。
type InitNamingInput struct {
	Env             string
	SourceDB        string
	TaskName        string
	PublicationName string
	SlotName        string
	ConnectorName   string
	TopicPrefix     string
}

func resolveNamingField(userValue, defaultValue string) string {
	if strings.TrimSpace(userValue) != "" {
		return sanitizeIdent(userValue)
	}
	return defaultValue
}

// ResolveInitNaming 默认 Publication 与 Connector 同为 rt_{env}_{db}。
// 用户填写的 publication 原样采用，connector 与之相同，不再单独漂移。
// 未填写 topic 时与 publication 相同。Slot 固定 slot_{env}_{db}_{rand6}，不跟随自定义 publication。
func ResolveInitNaming(in InitNamingInput) InitNaming {
	env := strings.TrimSpace(in.Env)
	db := strings.TrimSpace(in.SourceDB)
	if db == "" {
		db = strings.TrimSpace(in.TaskName)
	}

	topicExplicit := strings.TrimSpace(in.TopicPrefix) != ""

	pub := resolveNamingField(in.PublicationName, generatePublicationName(env, db))
	connector := pub

	topicPrefix := pub
	if topicExplicit {
		topicPrefix = resolveNamingField(in.TopicPrefix, pub)
	}

	slot := resolveNamingField(in.SlotName, generateSlotName(env, db))

	return InitNaming{
		Env:             env,
		PublicationName: pub,
		SlotName:        slot,
		ConnectorName:   connector,
		TopicPrefix:     topicPrefix,
	}
}

func ApplyInitNaming(extra map[string]string, taskName, sourceDB string) InitNaming {
	if extra == nil {
		extra = map[string]string{}
	}
	n := ResolveInitNaming(InitNamingInput{
		Env:             extra["env"],
		SourceDB:        firstNonEmptyStr(sourceDB, extra["representative_db"], extra["source_db"]),
		TaskName:        taskName,
		PublicationName: extra["publication_name"],
		SlotName:        extra["slot_name"],
		ConnectorName:   extra["connector_name"],
		TopicPrefix:     extra["topic_prefix"],
	})
	extra["publication_name"] = n.PublicationName
	extra["slot_name"] = n.SlotName
	extra["connector_name"] = n.ConnectorName
	extra["topic_prefix"] = n.TopicPrefix
	return n
}

func firstNonEmptyStr(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func defaultTaskNames(env, sourceDB, taskName string) (pub, slot, topic string) {
	n := ResolveInitNaming(InitNamingInput{Env: env, SourceDB: sourceDB, TaskName: taskName})
	return n.PublicationName, n.SlotName, n.TopicPrefix
}
