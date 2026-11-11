package slowbus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

var errBadMessage = errors.New("慢日志消息不合法")

const (
	Topic      = "cloud-pg-slowlog"
	DLQTopic   = "cloud-pg-slowlog-dlq"
	Group      = "auto-dba-slowlog"
	Partitions = 12
	// RetentionMS 是主题保留 7 天。
	RetentionMS = 7 * 24 * 3600 * 1000
	// MaxFieldBytes 是单条 SQL 或原始 JSON 写入消息前的上限。
	MaxFieldBytes = 256 * 1024
	Version       = 1
)

// Message 是一条慢 SQL。键用云实例 ID，值是这份 JSON。
type Message struct {
	V            int    `json:"v"`
	Region       string `json:"region"`
	AccountID    string `json:"account_id"`
	TenantID     string `json:"tenant_id"`
	Vendor       string `json:"vendor"`
	CloudID      string `json:"cloud_instance_id"`
	WindowFrom   string `json:"window_from"`
	WindowTo     string `json:"window_to"`
	LogTime      string `json:"log_time"`
	DBName       string `json:"db_name"`
	DBUser       string `json:"db_user"`
	Client       string `json:"client"`
	Duration     int    `json:"duration_ms"`
	SQL          string `json:"sql_text"`
	SQLTruncated bool   `json:"sql_truncated,omitempty"`
	Fingerprint  string `json:"fingerprint"`
	Raw          string `json:"raw_json"`
	RawTruncated bool   `json:"raw_truncated,omitempty"`
	Dedupe       string `json:"dedupe_key"`
}

// DedupeKey 用截断前的原文计算，重拉同一条仍然对得上。
func DedupeKey(raw, sql string) string {
	if raw == "" {
		raw = sql
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Prepare 补上版本号，并把超长字段截到 256KB。
func Prepare(m Message) Message {
	m.V = Version
	m.SQL, m.SQLTruncated = clip(m.SQL, MaxFieldBytes)
	m.Raw, m.RawTruncated = clip(m.Raw, MaxFieldBytes)
	return m
}

func clip(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	b := []byte(s[:max])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b), true
}

// Encode 序列化一条已 Prepare 的消息。
func Encode(m Message) ([]byte, error) {
	return json.Marshal(Prepare(m))
}

// Decode 解析消息。版本不对或缺少云实例 ID 视为坏消息。
func Decode(b []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return Message{}, err
	}
	if m.V != Version || m.CloudID == "" || m.Dedupe == "" {
		return Message{}, errBadMessage
	}
	return m, nil
}
