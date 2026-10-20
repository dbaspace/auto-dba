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

// generatePublicationName 默认 Publication：{env}_{db}
func generatePublicationName(env, dbName string) string {
	return joinIdent(env, dbName)
}

// generateSlotName 默认 Slot：slot_{env}_{db}_{rand6}
func generateSlotName(env, dbName string) string {
	return "slot_" + joinIdent(env, dbName) + "_" + randomLowerHex(6)
}

func generateSlotNameFromPub(pub string) string {
	p := sanitizeIdent(pub)
	if p == "" {
		p = "cdc_task"
	}
	return "slot_" + p + "_" + randomLowerHex(6)
}

func generateConnectorName(env, dbName string) string {
	return joinIdent(env, dbName)
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

// ResolveInitNaming 对齐老项目：Publication/Connector/Topic={env}_{db}；
// Slot=slot_{env}_{db}_{rand6}。显式 publication 时 slot 为 slot_{publication}_{rand6}。
func ResolveInitNaming(in InitNamingInput) InitNaming {
	env := strings.TrimSpace(in.Env)
	db := strings.TrimSpace(in.SourceDB)
	if db == "" {
		db = strings.TrimSpace(in.TaskName)
	}

	pubExplicit := strings.TrimSpace(in.PublicationName) != ""
	connExplicit := strings.TrimSpace(in.ConnectorName) != ""
	topicExplicit := strings.TrimSpace(in.TopicPrefix) != ""
	slotExplicit := strings.TrimSpace(in.SlotName) != ""

	pub := resolveNamingField(in.PublicationName, generatePublicationName(env, db))

	connDefault := generateConnectorName(env, db)
	if pubExplicit && !connExplicit {
		connDefault = pub
	}
	connector := resolveNamingField(in.ConnectorName, connDefault)

	topicDefault := generateTopicPrefix(env, db)
	switch {
	case pubExplicit && !topicExplicit:
		topicDefault = pub
	case connExplicit && !topicExplicit && !pubExplicit:
		topicDefault = sanitizeIdent(in.ConnectorName)
	}
	topicPrefix := resolveNamingField(in.TopicPrefix, topicDefault)

	slotDefault := generateSlotName(env, db)
	if pubExplicit && !slotExplicit {
		slotDefault = generateSlotNameFromPub(pub)
	}
	slot := resolveNamingField(in.SlotName, slotDefault)

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
