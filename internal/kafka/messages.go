package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"auto-dba/internal/store"
)

type KafkaMessage struct {
	Key       string            `json:"key"`
	Value     string            `json:"value"`
	Headers   map[string]string `json:"headers"`
	Partition int               `json:"partition"`
	Offset    int64             `json:"offset"`
	Timestamp time.Time         `json:"timestamp"`
}

func ReadMessages(ctx context.Context, inst *store.Instance, topic string, partition int, limit int, startOffset int64, sortOrder string, since time.Time) ([]KafkaMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if ctx == nil {
		ctx = context.Background()
	}
	newest := sortOrder == "newest"
	ids := []int{partition}
	scanAll := partition < 0
	if scanAll {
		var err error
		ids, err = ListPartitions(ctx, inst, topic)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []KafkaMessage{}, nil
		}
		// All 分区时限制扫描数量，避免 __consumer_offsets 等几十分区逐个 Dial 超时
		const maxAllParts = 8
		if len(ids) > maxAllParts {
			if newest {
				ids = ids[len(ids)-maxAllParts:]
			} else {
				ids = ids[:maxAllParts]
			}
		}
	}
	var out []KafkaMessage
	for _, id := range ids {
		need := limit
		if newest {
			need = limit - len(out)
			if need <= 0 {
				break
			}
		} else if len(out) >= limit {
			break
		}
		part, err := readPartitionMessages(ctx, inst, topic, id, need, startOffset, newest, since)
		if err != nil {
			if e := kafkaReadAbortErr(ctx, err); e != nil {
				// 已有部分结果则直接返回，避免整单超时失败
				if len(out) > 0 && (errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled)) {
					break
				}
				return out, e
			}
			return nil, err
		}
		out = append(out, part...)
	}
	if !since.IsZero() {
		filtered := out[:0]
		for _, m := range out {
			if m.Timestamp.IsZero() || !m.Timestamp.Before(since) {
				filtered = append(filtered, m)
			}
		}
		out = filtered
	}
	if newest {
		sort.Slice(out, func(i, j int) bool {
			if out[i].Timestamp.Equal(out[j].Timestamp) {
				return out[i].Offset > out[j].Offset
			}
			return out[i].Timestamp.After(out[j].Timestamp)
		})
	} else {
		sort.Slice(out, func(i, j int) bool {
			if out[i].Timestamp.Equal(out[j].Timestamp) {
				return out[i].Offset < out[j].Offset
			}
			return out[i].Timestamp.Before(out[j].Timestamp)
		})
	}
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []KafkaMessage{}
	}
	return out, nil
}

func readPartitionMessages(ctx context.Context, inst *store.Instance, topic string, partition int, limit int, startOffset int64, newest bool, since time.Time) ([]KafkaMessage, error) {
	if limit <= 0 {
		return []KafkaMessage{}, nil
	}
	addr := bootstrapAddr(inst)
	dialWait := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline); left > 0 && left < dialWait {
			dialWait = left
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialWait)
	defer cancel()
	conn, err := kafkago.DialLeader(dialCtx, "tcp", addr, topic, partition)
	if err != nil {
		return nil, err
	}
	first, last, err := conn.ReadOffsets()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	floor := first
	if !since.IsZero() {
		if off, oerr := conn.ReadOffset(since); oerr == nil && off > floor {
			floor = off
		}
	}
	_ = conn.Close()
	from, n := kafkaReadRange(first, last, startOffset, limit, newest, floor)
	// newest + since：时间下界若把窗口抬空，回退为读末尾 limit 条，再靠时间戳过滤
	if n <= 0 && newest && !since.IsZero() && last > first {
		from, n = kafkaReadRange(first, last, startOffset, limit, newest, first)
	}
	if n <= 0 {
		return []KafkaMessage{}, nil
	}
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        []string{addr},
		Topic:          topic,
		Partition:      partition,
		MaxBytes:       10e6,
		MaxWait:        200 * time.Millisecond,
		ReadBackoffMin: 5 * time.Millisecond,
		ReadBackoffMax: 50 * time.Millisecond,
	})
	defer r.Close()
	if err := r.SetOffset(from); err != nil {
		return nil, err
	}
	out := make([]KafkaMessage, 0, n)
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return out, err
		}
		m, err := r.ReadMessage(ctx)
		if err != nil {
			if e := kafkaReadAbortErr(ctx, err); e != nil {
				if len(out) > 0 && errors.Is(e, context.DeadlineExceeded) {
					break
				}
				return out, e
			}
			break
		}
		if !since.IsZero() && !m.Time.IsZero() && m.Time.Before(since) {
			continue
		}
		headers := make(map[string]string, len(m.Headers))
		for _, h := range m.Headers {
			headers[h.Key] = string(h.Value)
		}
		msg := KafkaMessage{
			Key:       string(m.Key),
			Value:     string(m.Value),
			Headers:   headers,
			Partition: m.Partition,
			Offset:    m.Offset,
			Timestamp: m.Time,
		}
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now()
		}
		out = append(out, msg)
	}
	return out, nil
}

// kafkaReadRange 根据水位 [first, last) 与时间下界 floor 计算只读已有消息的起点和条数。
func kafkaReadRange(first, last, requestedOffset int64, limit int, newest bool, floor int64) (start int64, count int) {
	if last <= first || limit <= 0 {
		return first, 0
	}
	if floor > first {
		first = floor
	}
	if last <= first {
		return first, 0
	}
	available := last - first
	n := int64(limit)
	if n > available {
		n = available
	}
	if newest {
		start = last - n
		if requestedOffset >= 0 {
			start = requestedOffset
			if start < first {
				start = first
			}
			if start >= last {
				return start, 0
			}
			n = last - start
			if n > int64(limit) {
				n = int64(limit)
			}
		}
		if start < first {
			start = first
			n = last - start
			if n > int64(limit) {
				n = int64(limit)
			}
		}
		return start, int(n)
	}
	start = first
	if requestedOffset >= 0 {
		start = requestedOffset
	}
	if start < first {
		start = first
	}
	if start >= last {
		return start, 0
	}
	n = last - start
	if n > int64(limit) {
		n = int64(limit)
	}
	return start, int(n)
}

func kafkaReadAbortErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func FormatMessageValue(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
