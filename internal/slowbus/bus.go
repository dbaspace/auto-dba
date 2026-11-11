package slowbus

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"auto-dba/internal/config"
)

// Bus 是平台自己的慢日志总线。kafka-go 0.4 的 Writer 不暴露幂等生产者序号，
// 这里用 acks=all 的同步写入；重复投递由 dedupe_key 收成一行。
type Bus struct {
	writer *kafka.Writer
	dlq    *kafka.Writer
	reader *kafka.Reader

	cancel context.CancelFunc
	done   chan struct{}

	dlqN       atomic.Int64
	produceErr atomic.Int64
	lag        atomic.Int64
	closed     atomic.Bool

	mu       sync.Mutex
	lagSince time.Time
	lastLag  int64
	onAlert  func(kind, detail string, firing bool)
}

// Open 按配置连接并确保主题存在。密码只用于握手，不写入日志。
func Open(cfg config.SlowlogBusConfig) (*Bus, error) {
	brokers := splitBrokers(cfg.Brokers)
	if len(brokers) == 0 {
		return nil, errors.New("slowlog brokers empty")
	}
	mech, err := saslMechanism(cfg)
	if err != nil {
		return nil, err
	}
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	transport := &kafka.Transport{SASL: mech, TLS: tlsCfg, DialTimeout: 10 * time.Second}
	client := &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second, Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := ensureTopics(ctx, client, brokers); err != nil {
		return nil, err
	}
	writer := newWriter(brokers, transport)
	dlq := newWriter(brokers, transport)
	dialer := &kafka.Dialer{Timeout: 10 * time.Second, DualStack: true, SASLMechanism: mech, TLS: tlsCfg}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        Group,
		Topic:          Topic,
		Dialer:         dialer,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		MaxWait:        time.Second,
		CommitInterval: 0,
		StartOffset:    kafka.FirstOffset,
	})
	log.Printf("slowlog bus %s", cfg.LogLine())
	return &Bus{writer: writer, dlq: dlq, reader: reader, done: make(chan struct{})}, nil
}

func newWriter(brokers []string, transport *kafka.Transport) *kafka.Writer {
	return &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		Compression:            kafka.Lz4,
		BatchSize:              100,
		BatchTimeout:           200 * time.Millisecond,
		AllowAutoTopicCreation: false,
		Transport:              transport,
	}
}

// SetAlerter 接收积压和死信告警。kind 是 backlog 或 dlq。
func (b *Bus) SetAlerter(fn func(kind, detail string, firing bool)) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.onAlert = fn
	b.mu.Unlock()
}

func (b *Bus) Stats() (lag, dlq, produceErr int64) {
	if b == nil {
		return 0, 0, 0
	}
	return b.lag.Load(), b.dlqN.Load(), b.produceErr.Load()
}

// Publish 把一页慢 SQL 写成消息。全部确认后才返回。
func (b *Bus) Publish(ctx context.Context, msgs []Message) error {
	if b == nil || b.writer == nil {
		return errors.New("slowlog bus closed")
	}
	if len(msgs) == 0 {
		return nil
	}
	out := make([]kafka.Message, 0, len(msgs))
	for _, m := range msgs {
		body, err := Encode(m)
		if err != nil {
			return err
		}
		out = append(out, kafka.Message{Topic: Topic, Key: []byte(m.CloudID), Value: body})
	}
	if err := b.writer.WriteMessages(ctx, out...); err != nil {
		b.produceErr.Add(1)
		return redactErr(err)
	}
	return nil
}

// Run 消费到 ctx 结束。入库成功才提交位移；坏消息重试 3 次后进死信。
func (b *Bus) Run(ctx context.Context, insert func(context.Context, []Message) error) {
	if b == nil {
		return
	}
	defer close(b.done)
	var batch []kafka.Message
	var rows []Message
	start := time.Now()
	flush := func(fctx context.Context) error {
		if len(rows) == 0 {
			return nil
		}
		if err := insert(fctx, rows); err != nil {
			return err
		}
		if err := b.reader.CommitMessages(fctx, batch...); err != nil {
			return err
		}
		batch = nil
		rows = nil
		return nil
	}
	for {
		if ctx.Err() != nil || b.closed.Load() {
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = flush(cctx)
			cancel()
			return
		}
		if len(rows) >= 500 || (len(rows) > 0 && time.Since(start) >= time.Second) {
			if err := flush(ctx); err != nil {
				log.Printf("slowlog consume: %v", redactErr(err))
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
				continue
			}
		}
		fctx, cancel := context.WithTimeout(ctx, time.Second)
		msg, err := b.reader.FetchMessage(fctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil || b.closed.Load() {
				continue
			}
			b.noteLag()
			continue
		}
		row, bad := decodeRetry(msg.Value)
		if bad {
			if err := b.writeDLQ(ctx, msg); err != nil {
				log.Printf("slowlog dlq: %v", redactErr(err))
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
				continue
			}
			if err := b.reader.CommitMessages(ctx, msg); err != nil {
				log.Printf("slowlog commit dlq: %v", redactErr(err))
			}
			continue
		}
		if len(rows) == 0 {
			start = time.Now()
		}
		batch = append(batch, msg)
		rows = append(rows, row)
		b.noteLag()
	}
}

func decodeRetry(b []byte) (Message, bool) {
	var last error
	for i := 0; i < 3; i++ {
		m, err := Decode(b)
		if err == nil {
			return m, false
		}
		last = err
	}
	_ = last
	return Message{}, true
}

func (b *Bus) writeDLQ(ctx context.Context, msg kafka.Message) error {
	err := b.dlq.WriteMessages(ctx, kafka.Message{Topic: DLQTopic, Key: msg.Key, Value: msg.Value})
	if err == nil {
		n := b.dlqN.Add(1)
		b.alert("dlq", fmt.Sprintf("死信 %d 条", n), true)
	}
	return err
}

func (b *Bus) noteLag() {
	if b.reader == nil {
		return
	}
	lag := b.reader.Stats().Lag
	b.lag.Store(lag)
	b.mu.Lock()
	defer b.mu.Unlock()
	if lag > b.lastLag && b.lastLag > 0 {
		if b.lagSince.IsZero() {
			b.lagSince = time.Now()
		}
	} else if lag == 0 {
		b.lagSince = time.Time{}
		b.alertLocked("backlog", "", false)
	}
	b.lastLag = lag
	if !b.lagSince.IsZero() && time.Since(b.lagSince) >= 10*time.Minute {
		b.alertLocked("backlog", fmt.Sprintf("积压 %d 持续 %s", lag, time.Since(b.lagSince).Round(time.Second)), true)
	}
}

func (b *Bus) alert(kind, detail string, firing bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.alertLocked(kind, detail, firing)
}

func (b *Bus) alertLocked(kind, detail string, firing bool) {
	if b.onAlert != nil {
		b.onAlert(kind, detail, firing)
	}
}

// Close 先停消费，再关闭生产者以刷出缓冲区。
func (b *Bus) Close() error {
	if b == nil {
		return nil
	}
	b.closed.Store(true)
	var err error
	if b.reader != nil {
		err = b.reader.Close()
	}
	if b.done != nil {
		select {
		case <-b.done:
		case <-time.After(5 * time.Second):
		}
	}
	if b.writer != nil {
		if e := b.writer.Close(); e != nil && err == nil {
			err = e
		}
	}
	if b.dlq != nil {
		if e := b.dlq.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

func splitBrokers(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func saslMechanism(cfg config.SlowlogBusConfig) (sasl.Mechanism, error) {
	user := strings.TrimSpace(cfg.Username)
	if user == "" && strings.TrimSpace(cfg.Password) == "" {
		return nil, nil
	}
	mech := strings.ToUpper(strings.TrimSpace(cfg.SASLMechanism))
	if mech == "" {
		mech = "PLAIN"
	}
	switch mech {
	case "PLAIN":
		return plain.Mechanism{Username: user, Password: cfg.Password}, nil
	case "SCRAM-SHA-256":
		return scram.Mechanism(scram.SHA256, user, cfg.Password)
	case "SCRAM-SHA-512":
		return scram.Mechanism(scram.SHA512, user, cfg.Password)
	default:
		return nil, fmt.Errorf("不支持的 sasl_mechanism")
	}
}

func ensureTopics(ctx context.Context, client *kafka.Client, brokers []string) error {
	rf := 1
	if md, err := client.Metadata(ctx, &kafka.MetadataRequest{Addr: kafka.TCP(brokers...)}); err == nil && len(md.Brokers) > 0 {
		rf = len(md.Brokers)
		if rf > 2 {
			rf = 2
		}
	}
	topics := []string{Topic, DLQTopic}
	for _, name := range topics {
		if err := createTopic(ctx, client, brokers, name, rf); err != nil {
			return err
		}
		if err := keepRetention(ctx, client, brokers, name); err != nil {
			log.Printf("slowlog topic %s retention: %v", name, redactErr(err))
		}
	}
	return nil
}

func createTopic(ctx context.Context, client *kafka.Client, brokers []string, name string, rf int) error {
	err := createTopicRF(ctx, client, brokers, name, rf)
	if err == nil || rf == 1 {
		return err
	}
	return createTopicRF(ctx, client, brokers, name, 1)
}

func createTopicRF(ctx context.Context, client *kafka.Client, brokers []string, name string, rf int) error {
	resp, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Addr: kafka.TCP(brokers...),
		Topics: []kafka.TopicConfig{{
			Topic:             name,
			NumPartitions:     Partitions,
			ReplicationFactor: rf,
			ConfigEntries: []kafka.ConfigEntry{
				{ConfigName: "retention.ms", ConfigValue: strconv.Itoa(RetentionMS)},
				{ConfigName: "cleanup.policy", ConfigValue: "delete"},
			},
		}},
	})
	if err != nil {
		return redactErr(err)
	}
	if resp != nil && resp.Errors[name] != nil && !errors.Is(resp.Errors[name], kafka.TopicAlreadyExists) {
		return redactErr(resp.Errors[name])
	}
	return nil
}

func keepRetention(ctx context.Context, client *kafka.Client, brokers []string, name string) error {
	_, err := client.IncrementalAlterConfigs(ctx, &kafka.IncrementalAlterConfigsRequest{
		Addr: kafka.TCP(brokers...),
		Resources: []kafka.IncrementalAlterConfigsRequestResource{{
			ResourceType: kafka.ResourceTypeTopic,
			ResourceName: name,
			Configs: []kafka.IncrementalAlterConfigsRequestConfig{
				{Name: "retention.ms", Value: strconv.Itoa(RetentionMS), ConfigOperation: kafka.ConfigOperationSet},
				{Name: "cleanup.policy", Value: "delete", ConfigOperation: kafka.ConfigOperationSet},
			},
		}},
	})
	return redactErr(err)
}

func redactErr(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if strings.Contains(strings.ToLower(s), "password") {
		return errors.New("kafka 连接失败")
	}
	return err
}
