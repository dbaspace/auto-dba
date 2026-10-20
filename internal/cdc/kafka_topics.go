package cdc

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

var kafkaEnsureLocks sync.Map

func lockKafkaBrokers(brokers string) func() {
	v, _ := kafkaEnsureLocks.LoadOrStore(strings.TrimSpace(brokers), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func BuildKafkaTopicName(prefix, schema, table string) string {
	prefix = defaultStr(prefix, "cdc")
	schema = defaultStr(schema, "public")
	table = strings.TrimSpace(table)
	if table == "" {
		return ""
	}
	return prefix + "." + schema + "." + table
}

// IsShardMapping 对齐 hub isShardMapping：分表组走 logical topic（目标表名）。
// 条件：is_shard_group / 多源表 / shard_pattern / 任一 source 与 target 不等（如 tbl_x0→tbl_x）。
func IsShardMapping(m TableMapping) bool {
	src := cleanStringSlice(m.SourceTables)
	if m.IsShardGroup || len(src) > 1 || strings.TrimSpace(m.ShardPattern) != "" {
		return true
	}
	tgt := strings.ToLower(strings.TrimSpace(m.TargetTable))
	if tgt == "" {
		return false
	}
	for _, s := range src {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if s != tgt {
			return true
		}
	}
	return false
}

func CollectMappingKafkaTopics(prefix, schema string, m TableMapping) []string {
	src := cleanStringSlice(m.SourceTables)
	tgt := strings.TrimSpace(m.TargetTable)
	if IsShardMapping(m) {
		name := tgt
		if name == "" && len(src) > 0 {
			name = src[0]
		}
		if t := BuildKafkaTopicName(prefix, schema, name); t != "" {
			return []string{t}
		}
		return nil
	}
	out := make([]string, 0, len(src))
	for _, t := range src {
		if topic := BuildKafkaTopicName(prefix, schema, t); topic != "" {
			out = append(out, topic)
		}
	}
	if len(out) == 0 && tgt != "" {
		if topic := BuildKafkaTopicName(prefix, schema, tgt); topic != "" {
			out = append(out, topic)
		}
	}
	return out
}

func CollectTaskKafkaTopics(prefix, schema string, maps []TableMapping, fallbackTables []string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(topic string) {
		topic = strings.TrimSpace(topic)
		if topic == "" {
			return
		}
		if _, ok := seen[topic]; ok {
			return
		}
		seen[topic] = struct{}{}
		out = append(out, topic)
	}
	if len(maps) == 0 {
		for _, t := range fallbackTables {
			add(BuildKafkaTopicName(prefix, schema, t))
		}
		sort.Strings(out)
		return out
	}
	for _, m := range maps {
		for _, t := range CollectMappingKafkaTopics(prefix, schema, m) {
			add(t)
		}
	}
	sort.Strings(out)
	return out
}

// CollectPipelineTopicNames 对齐 hub collectPipelineTopicNames：先按 mapping 计算，空则回退落库 kafka_topics。
func CollectPipelineTopicNames(prefix, schema string, maps []TableMapping, fallbackTables []string) []string {
	out := CollectTaskKafkaTopics(prefix, schema, maps, fallbackTables)
	if len(out) > 0 {
		return out
	}
	seen := map[string]struct{}{}
	var stored []string
	for _, m := range maps {
		for _, t := range m.KafkaTopics {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			stored = append(stored, t)
		}
	}
	sort.Strings(stored)
	return stored
}

// PipelineActiveMappings 全链路状态用：排除 deleted / failed（对齐 hub activeMappings）。
func PipelineActiveMappings(maps []TableMapping) []TableMapping {
	out := make([]TableMapping, 0, len(maps))
	for _, m := range maps {
		st := strings.ToLower(strings.TrimSpace(m.Status))
		if st == "deleted" || st == "failed" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func EnsureKafkaTopics(ctx context.Context, brokers string, topics []string, partitions, replicationFactor int) error {
	brokers = strings.TrimSpace(brokers)
	if brokers == "" {
		return fmt.Errorf("kafka brokers empty")
	}
	topics = cleanStringSlice(topics)
	if len(topics) == 0 {
		return fmt.Errorf("kafka topics empty")
	}
	if partitions <= 0 {
		partitions = 3
	}
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	unlock := lockKafkaBrokers(brokers)
	defer unlock()

	addr := firstKafkaAddr(brokers)
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := kafkago.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("kafka dial %s: %w", addr, err)
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("kafka controller: %w", err)
	}
	ctrlConn, err := kafkago.DialContext(dialCtx, "tcp", net.JoinHostPort(controller.Host, fmt.Sprintf("%d", controller.Port)))
	if err != nil {
		return fmt.Errorf("kafka controller dial: %w", err)
	}
	defer ctrlConn.Close()

	existing := map[string]struct{}{}
	if parts, rerr := ctrlConn.ReadPartitions(); rerr == nil {
		for _, p := range parts {
			if p.Topic != "" {
				existing[p.Topic] = struct{}{}
			}
		}
	}
	cfgs := make([]kafkago.TopicConfig, 0, len(topics))
	for _, topic := range topics {
		if _, ok := existing[topic]; ok {
			continue
		}
		cfgs = append(cfgs, kafkago.TopicConfig{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
		})
	}
	if len(cfgs) == 0 {
		return nil
	}
	if err := ctrlConn.CreateTopics(cfgs...); err != nil && !kafkaTopicAlreadyExists(err) {
		return fmt.Errorf("create kafka topics: %w", err)
	}
	return nil
}

func ProbeKafkaTopics(ctx context.Context, brokers string, topics []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, t := range topics {
		if s := strings.TrimSpace(t); s != "" {
			out[s] = false
		}
	}
	brokers = strings.TrimSpace(brokers)
	if brokers == "" || len(out) == 0 {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := kafkago.DialContext(dialCtx, "tcp", firstKafkaAddr(brokers))
	if err != nil {
		return out, fmt.Errorf("kafka dial: %w", err)
	}
	defer conn.Close()
	// 先按待查列表拉元数据，避免全集群 ReadPartitions 漏检；失败再回退全量。
	names := make([]string, 0, len(out))
	for t := range out {
		names = append(names, t)
	}
	sort.Strings(names)
	parts, err := conn.ReadPartitions(names...)
	if err != nil {
		parts, err = conn.ReadPartitions()
		if err != nil {
			return out, err
		}
	}
	for _, p := range parts {
		if _, ok := out[p.Topic]; ok {
			out[p.Topic] = true
		}
	}
	return out, nil
}

func firstKafkaAddr(brokers string) string {
	for _, p := range strings.Split(brokers, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(p); err == nil {
			return p
		}
		return net.JoinHostPort(p, "9092")
	}
	return "localhost:9092"
}

func kafkaTopicAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "already exists") || strings.Contains(low, "topic_already_exists") || strings.Contains(low, "existed")
}
