package kafka

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"

	kafkago "github.com/segmentio/kafka-go"

	"auto-dba/internal/kafka/describelogdirs"
	"auto-dba/internal/store"
)

type PartitionDetail struct {
	ID           int    `json:"id"`
	Leader       int    `json:"leader"`
	Replicas     []int  `json:"replicas"`
	ISR          []int  `json:"isr,omitempty"`
	FirstOffset  int64  `json:"first_offset"`
	LastOffset   int64  `json:"last_offset"`
	MessageCount int64  `json:"message_count"`
	SizeBytes    int64  `json:"size_bytes"`
	OffsetsText  string `json:"offsets_text"`
	SizeText     string `json:"size_text"`
}

type TopicConfigEntry struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	ValueHint   string `json:"value_hint,omitempty"`
	Source      string `json:"source"`
	ReadOnly    bool   `json:"read_only"`
	IsSensitive bool   `json:"is_sensitive"`
}

type TopicLogEntry struct {
	Broker    int    `json:"broker"`
	Topic     string `json:"topic"`
	Partition int    `json:"partition"`
	SizeBytes int64  `json:"size_bytes"`
	SizeText  string `json:"size_text"`
	OffsetLag int64  `json:"offset_lag"`
	LogDir    string `json:"log_dir,omitempty"`
}

func kafkaClient(inst *store.Instance) *kafkago.Client {
	return &kafkago.Client{
		Addr:    kafkago.TCP(bootstrapAddr(inst)),
		Timeout: dialTimeout,
	}
}

func DescribePartitions(ctx context.Context, inst *store.Instance, topic string) ([]PartitionDetail, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cli := kafkaClient(inst)
	meta, err := cli.Metadata(ctx, &kafkago.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return nil, err
	}
	var parts []kafkago.Partition
	for _, t := range meta.Topics {
		if t.Name == topic {
			parts = t.Partitions
			break
		}
	}
	if len(parts) == 0 {
		return []PartitionDetail{}, nil
	}
	sizes := fetchTopicLogSizes(ctx, inst, meta.Brokers, topic)
	out := make([]PartitionDetail, 0, len(parts))
	for _, p := range parts {
		d := PartitionDetail{
			ID:       p.ID,
			Leader:   p.Leader.ID,
			Replicas: brokerIDs(p.Replicas),
			ISR:      brokerIDs(p.Isr),
		}
		first, last, oerr := partitionOffsets(ctx, inst, topic, p.ID)
		if oerr == nil {
			d.FirstOffset = first
			d.LastOffset = last
			if last > first {
				d.MessageCount = last - first
			}
		}
		d.SizeBytes = sizes[p.ID]
		d.OffsetsText = fmt.Sprintf("%d -> %d", d.FirstOffset, d.LastOffset)
		d.SizeText = formatPartitionSize(d.MessageCount, d.SizeBytes)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func DescribeTopicConfigs(ctx context.Context, inst *store.Instance, topic string) ([]TopicConfigEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cli := kafkaClient(inst)
	resp, err := cli.DescribeConfigs(ctx, &kafkago.DescribeConfigsRequest{
		Resources: []kafkago.DescribeConfigRequestResource{{
			ResourceType: kafkago.ResourceTypeTopic,
			ResourceName: topic,
		}},
		IncludeSynonyms: true,
	})
	if err != nil {
		return nil, err
	}
	out := []TopicConfigEntry{}
	for _, res := range resp.Resources {
		if res.Error != nil {
			return nil, res.Error
		}
		for _, e := range res.ConfigEntries {
			if e.IsSensitive {
				continue
			}
			out = append(out, TopicConfigEntry{
				Name:        e.ConfigName,
				Value:       e.ConfigValue,
				ValueHint:   configValueHint(e.ConfigName, e.ConfigValue),
				Source:      configSourceName(e.ConfigSource, e.IsDefault),
				ReadOnly:    e.ReadOnly,
				IsSensitive: e.IsSensitive,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func DescribeTopicLogs(ctx context.Context, inst *store.Instance, topic string) ([]TopicLogEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cli := kafkaClient(inst)
	meta, err := cli.Metadata(ctx, &kafkago.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return nil, err
	}
	entries := fetchTopicLogEntries(ctx, inst, meta.Brokers, topic)
	if len(entries) > 0 {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Broker != entries[j].Broker {
				return entries[i].Broker < entries[j].Broker
			}
			return entries[i].Partition < entries[j].Partition
		})
		return entries, nil
	}
	// DescribeLogDirs 不可用时，用 leader + 偏移量兜底
	var parts []kafkago.Partition
	for _, t := range meta.Topics {
		if t.Name == topic {
			parts = t.Partitions
			break
		}
	}
	out := make([]TopicLogEntry, 0, len(parts))
	for _, p := range parts {
		first, last, _ := partitionOffsets(ctx, inst, topic, p.ID)
		msgCount := int64(0)
		if last > first {
			msgCount = last - first
		}
		out = append(out, TopicLogEntry{
			Broker:    p.Leader.ID,
			Topic:     topic,
			Partition: p.ID,
			SizeBytes: 0,
			SizeText:  formatBytes(0) + fmt.Sprintf(" (%d msgs)", msgCount),
			OffsetLag: 0,
		})
	}
	return out, nil
}

func partitionOffsets(ctx context.Context, inst *store.Instance, topic string, partition int) (int64, int64, error) {
	addr := bootstrapAddr(inst)
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := kafkago.DialLeader(dialCtx, "tcp", addr, topic, partition)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	return conn.ReadOffsets()
}

func brokerIDs(bs []kafkago.Broker) []int {
	out := make([]int, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.ID)
	}
	return out
}

func fetchTopicLogSizes(ctx context.Context, inst *store.Instance, brokers []kafkago.Broker, topic string) map[int]int64 {
	out := map[int]int64{}
	for _, e := range fetchTopicLogEntries(ctx, inst, brokers, topic) {
		out[e.Partition] += e.SizeBytes
	}
	return out
}

func fetchTopicLogEntries(ctx context.Context, inst *store.Instance, brokers []kafkago.Broker, topic string) []TopicLogEntry {
	if topic == "" {
		return fetchTopicLogEntriesFor(ctx, inst, brokers, nil)
	}
	return fetchTopicLogEntriesFor(ctx, inst, brokers, []string{topic})
}

func fetchTopicLogEntriesFor(ctx context.Context, inst *store.Instance, brokers []kafkago.Broker, topics []string) []TopicLogEntry {
	if len(brokers) == 0 {
		return nil
	}
	transport := kafkago.DefaultTransport
	var req *describelogdirs.Request
	if len(topics) == 0 {
		// Topics=null => 拉取全部 topic 的 log dirs
		req = &describelogdirs.Request{Topics: nil}
	} else {
		reqTopics := make([]describelogdirs.RequestTopic, 0, len(topics))
		for _, name := range topics {
			if name == "" {
				continue
			}
			reqTopics = append(reqTopics, describelogdirs.RequestTopic{
				Name:       name,
				Partitions: []int32{},
			})
		}
		req = &describelogdirs.Request{Topics: reqTopics}
	}
	allow := map[string]struct{}{}
	for _, name := range topics {
		if name != "" {
			allow[name] = struct{}{}
		}
	}
	var out []TopicLogEntry
	for _, b := range brokers {
		if b.Host == "" || b.Port <= 0 {
			continue
		}
		addr := kafkago.TCP(net.JoinHostPort(b.Host, strconv.Itoa(b.Port)))
		raw, err := transport.RoundTrip(ctx, addr, req)
		if err != nil {
			continue
		}
		resp, ok := raw.(*describelogdirs.Response)
		if !ok || resp == nil {
			continue
		}
		for _, result := range resp.Results {
			if result.ErrorCode != 0 {
				continue
			}
			for _, t := range result.Topics {
				if len(allow) > 0 {
					if _, ok := allow[t.Name]; !ok {
						continue
					}
				}
				for _, p := range t.Partitions {
					if p.IsFuture {
						continue
					}
					out = append(out, TopicLogEntry{
						Broker:    b.ID,
						Topic:     t.Name,
						Partition: int(p.Partition),
						SizeBytes: p.Size,
						SizeText:  formatBytes(p.Size),
						OffsetLag: p.OffsetLag,
						LogDir:    result.LogDir,
					})
				}
			}
		}
	}
	return out
}

func formatPartitionSize(msgCount, sizeBytes int64) string {
	if sizeBytes > 0 {
		return fmt.Sprintf("%d - %s", msgCount, formatBytes(sizeBytes))
	}
	return fmt.Sprintf("%d", msgCount)
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%d KB", n/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func configSourceName(source int8, isDefault bool) string {
	switch source {
	case 1:
		return "DYNAMIC_TOPIC_CONFIG"
	case 2:
		return "DYNAMIC_BROKER_CONFIG"
	case 3:
		return "DYNAMIC_DEFAULT_BROKER_CONFIG"
	case 4:
		return "STATIC_BROKER_CONFIG"
	case 5:
		return "DEFAULT_CONFIG"
	}
	if isDefault {
		return "DEFAULT_CONFIG"
	}
	if source == 0 {
		return "DEFAULT_CONFIG"
	}
	return fmt.Sprintf("SOURCE_%d", source)
}

func configValueHint(name, value string) string {
	if value == "" {
		return ""
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return ""
	}
	switch {
	case len(name) >= 3 && name[len(name)-3:] == ".ms":
		return formatDurationMS(n)
	case len(name) >= 6 && name[len(name)-6:] == ".bytes":
		if n < 0 {
			return ""
		}
		return formatBytes(n)
	}
	return ""
}

func formatDurationMS(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%d seconds", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%d minutes", min)
	}
	hour := min / 60
	if hour < 24 {
		return fmt.Sprintf("%d hours", hour)
	}
	day := hour / 24
	if day < 365 {
		return fmt.Sprintf("%d days", day)
	}
	year := day / 365
	return fmt.Sprintf("%d years", year)
}
