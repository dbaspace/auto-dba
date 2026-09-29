package kafka

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"auto-dba/internal/store"
)

type TopicDetail struct {
	Name              string `json:"name"`
	PartitionCount    int    `json:"partition_count"`
	ReplicationFactor int    `json:"replication_factor"`
	MessageCount      int64  `json:"message_count"`
	SizeBytes         int64  `json:"size_bytes"`
	LastRecord        string `json:"last_record,omitempty"`
	LastPartition     int    `json:"last_partition"`
}

type lastNeed struct {
	topic     string
	partition int
	offset    int64
}

const topicListCacheTTL = 45 * time.Second

type topicListCacheEntry struct {
	at   time.Time
	list []TopicDetail
}

var (
	topicListCacheMu sync.Mutex
	topicListCache   = map[string]topicListCacheEntry{}
)

func topicCacheKey(instID string, lite bool) string {
	if lite {
		return instID + ":lite"
	}
	return instID + ":full"
}

func getTopicListCache(key string) ([]TopicDetail, bool) {
	topicListCacheMu.Lock()
	defer topicListCacheMu.Unlock()
	e, ok := topicListCache[key]
	if !ok || time.Since(e.at) > topicListCacheTTL {
		return nil, false
	}
	out := make([]TopicDetail, len(e.list))
	copy(out, e.list)
	return out, true
}

func setTopicListCache(key string, list []TopicDetail) {
	topicListCacheMu.Lock()
	defer topicListCacheMu.Unlock()
	cp := make([]TopicDetail, len(list))
	copy(cp, list)
	topicListCache[key] = topicListCacheEntry{at: time.Now(), list: cp}
}

func InvalidateTopicListCache(instID string) {
	topicListCacheMu.Lock()
	defer topicListCacheMu.Unlock()
	delete(topicListCache, topicCacheKey(instID, true))
	delete(topicListCache, topicCacheKey(instID, false))
}

// ListTopicDetails 拉取 Topic 列表。
// lite=true 仅 Metadata（快）；lite=false 补 Count/Size/LastRecord。
// refresh=true 跳过缓存。
func ListTopicDetails(ctx context.Context, inst *store.Instance, lite, refresh bool) ([]TopicDetail, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	instID := ""
	if inst != nil {
		instID = inst.ID
	}
	key := topicCacheKey(instID, lite)
	if !refresh {
		if cached, ok := getTopicListCache(key); ok {
			return cached, nil
		}
	}

	var (
		list []TopicDetail
		err  error
	)
	if lite {
		list, err = listTopicDetailsLite(ctx, inst)
	} else {
		list, err = listTopicDetailsFull(ctx, inst)
	}
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []TopicDetail{}
	}
	setTopicListCache(key, list)
	return list, nil
}

func listTopicDetailsLite(ctx context.Context, inst *store.Instance) ([]TopicDetail, error) {
	cli := kafkaClient(inst)
	meta, err := cli.Metadata(ctx, &kafkago.MetadataRequest{})
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	out := make([]TopicDetail, 0, len(meta.Topics))
	for _, t := range meta.Topics {
		if t.Name == "" || t.Error != nil {
			continue
		}
		rf := 0
		for _, p := range t.Partitions {
			if len(p.Replicas) > 0 {
				rf = len(p.Replicas)
				break
			}
		}
		out = append(out, TopicDetail{
			Name:              t.Name,
			PartitionCount:    len(t.Partitions),
			ReplicationFactor: rf,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// TopicPageStats 只给指定 Topic 补 Count、Size、Last Record。
func TopicPageStats(ctx context.Context, inst *store.Instance, names []string) ([]TopicDetail, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wanted := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		wanted = append(wanted, n)
		if len(wanted) >= 100 {
			break
		}
	}
	if len(wanted) == 0 {
		return []TopicDetail{}, nil
	}

	cli := kafkaClient(inst)
	meta, err := cli.Metadata(ctx, &kafkago.MetadataRequest{Topics: wanted})
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}

	type agg struct {
		parts []kafkago.Partition
		rf    int
	}
	byTopic := make(map[string]*agg, len(wanted))
	offsetReqs := make(map[string][]kafkago.OffsetRequest, len(wanted))
	for _, t := range meta.Topics {
		if t.Name == "" || t.Error != nil {
			continue
		}
		if _, ok := seen[t.Name]; !ok {
			continue
		}
		a := &agg{parts: t.Partitions}
		reqs := make([]kafkago.OffsetRequest, 0, len(t.Partitions)*2)
		for _, p := range t.Partitions {
			if a.rf == 0 && len(p.Replicas) > 0 {
				a.rf = len(p.Replicas)
			}
			reqs = append(reqs, kafkago.FirstOffsetOf(p.ID), kafkago.LastOffsetOf(p.ID))
		}
		byTopic[t.Name] = a
		if len(reqs) > 0 {
			offsetReqs[t.Name] = reqs
		}
	}

	type partRange struct {
		first, last int64
	}
	var (
		sizeByTopicPart map[string]map[int]int64
		ranges          = map[string]map[int]partRange{}
		wg              sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		sizeByTopicPart = logSizesForTopics(ctx, inst, meta.Brokers, wanted)
	}()
	go func() {
		defer wg.Done()
		if len(offsetReqs) == 0 {
			return
		}
		offResp, oerr := cli.ListOffsets(ctx, &kafkago.ListOffsetsRequest{Topics: offsetReqs})
		if oerr != nil || offResp == nil {
			return
		}
		for topic, parts := range offResp.Topics {
			m := ranges[topic]
			if m == nil {
				m = map[int]partRange{}
				ranges[topic] = m
			}
			for _, p := range parts {
				cur := m[p.Partition]
				if p.FirstOffset >= 0 {
					cur.first = p.FirstOffset
				}
				if p.LastOffset >= 0 {
					cur.last = p.LastOffset
				}
				m[p.Partition] = cur
			}
		}
	}()
	wg.Wait()
	if sizeByTopicPart == nil {
		sizeByTopicPart = map[string]map[int]int64{}
	}

	out := make([]TopicDetail, 0, len(wanted))
	needs := make([]lastNeed, 0, len(wanted))
	for _, name := range wanted {
		a := byTopic[name]
		if a == nil {
			continue
		}
		d := TopicDetail{
			Name:              name,
			PartitionCount:    len(a.parts),
			ReplicationFactor: a.rf,
		}
		newestPart := 0
		newestLast := int64(-1)
		for _, p := range a.parts {
			if r, ok := ranges[name][p.ID]; ok && r.last > r.first {
				d.MessageCount += r.last - r.first
				if r.last > newestLast {
					newestLast = r.last
					newestPart = p.ID
				}
			}
			if sz := sizeByTopicPart[name][p.ID]; sz > 0 {
				d.SizeBytes += sz
			}
		}
		if newestLast > 0 {
			d.LastPartition = newestPart
			needs = append(needs, lastNeed{topic: name, partition: newestPart, offset: newestLast - 1})
		}
		out = append(out, d)
	}
	lastMap := fetchLastRecordTimes(ctx, inst, needs)
	for i := range out {
		if ts, ok := lastMap[out[i].Name]; ok && !ts.IsZero() {
			out[i].LastRecord = ts.UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}

func logSizesForTopics(ctx context.Context, inst *store.Instance, brokers []kafkago.Broker, topics []string) map[string]map[int]int64 {
	out := map[string]map[int]int64{}
	for _, e := range fetchTopicLogEntriesFor(ctx, inst, brokers, topics) {
		m := out[e.Topic]
		if m == nil {
			m = map[int]int64{}
			out[e.Topic] = m
		}
		m[e.Partition] += e.SizeBytes
	}
	return out
}

func listTopicDetailsFull(ctx context.Context, inst *store.Instance) ([]TopicDetail, error) {
	cli := kafkaClient(inst)
	meta, err := cli.Metadata(ctx, &kafkago.MetadataRequest{})
	if err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}

	type agg struct {
		parts []kafkago.Partition
		rf    int
	}
	byTopic := make(map[string]*agg)
	offsetReqs := make(map[string][]kafkago.OffsetRequest)

	for _, t := range meta.Topics {
		if t.Name == "" || t.Error != nil {
			continue
		}
		a := &agg{parts: t.Partitions}
		reqs := make([]kafkago.OffsetRequest, 0, len(t.Partitions)*2)
		for _, p := range t.Partitions {
			if a.rf == 0 && len(p.Replicas) > 0 {
				a.rf = len(p.Replicas)
			}
			reqs = append(reqs, kafkago.FirstOffsetOf(p.ID), kafkago.LastOffsetOf(p.ID))
		}
		byTopic[t.Name] = a
		if len(reqs) > 0 {
			offsetReqs[t.Name] = reqs
		}
	}

	type partRange struct {
		first, last int64
	}
	var (
		sizeByTopicPart map[string]map[int]int64
		ranges          = map[string]map[int]partRange{}
		wg              sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		sizeByTopicPart = fetchAllLogSizes(ctx, inst, meta.Brokers)
	}()
	go func() {
		defer wg.Done()
		if len(offsetReqs) == 0 {
			return
		}
		offResp, oerr := cli.ListOffsets(ctx, &kafkago.ListOffsetsRequest{Topics: offsetReqs})
		if oerr != nil || offResp == nil {
			return
		}
		for topic, parts := range offResp.Topics {
			m := ranges[topic]
			if m == nil {
				m = map[int]partRange{}
				ranges[topic] = m
			}
			for _, p := range parts {
				cur := m[p.Partition]
				if p.FirstOffset >= 0 {
					cur.first = p.FirstOffset
				}
				if p.LastOffset >= 0 {
					cur.last = p.LastOffset
				}
				m[p.Partition] = cur
			}
		}
	}()
	wg.Wait()
	if sizeByTopicPart == nil {
		sizeByTopicPart = map[string]map[int]int64{}
	}

	out := make([]TopicDetail, 0, len(byTopic))
	needs := make([]lastNeed, 0, len(byTopic))

	for name, a := range byTopic {
		d := TopicDetail{
			Name:              name,
			PartitionCount:    len(a.parts),
			ReplicationFactor: a.rf,
		}
		newestPart := 0
		newestLast := int64(-1)
		for _, p := range a.parts {
			if r, ok := ranges[name][p.ID]; ok && r.last > r.first {
				d.MessageCount += r.last - r.first
				if r.last > newestLast {
					newestLast = r.last
					newestPart = p.ID
				}
			}
			if sz := sizeByTopicPart[name][p.ID]; sz > 0 {
				d.SizeBytes += sz
			}
		}
		if newestLast > 0 {
			d.LastPartition = newestPart
			needs = append(needs, lastNeed{topic: name, partition: newestPart, offset: newestLast - 1})
		}
		out = append(out, d)
	}

	lastMap := fetchLastRecordTimes(ctx, inst, needs)
	for i := range out {
		if ts, ok := lastMap[out[i].Name]; ok && !ts.IsZero() {
			out[i].LastRecord = ts.UTC().Format(time.RFC3339)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func fetchAllLogSizes(ctx context.Context, inst *store.Instance, brokers []kafkago.Broker) map[string]map[int]int64 {
	out := map[string]map[int]int64{}
	entries := fetchTopicLogEntries(ctx, inst, brokers, "")
	for _, e := range entries {
		m := out[e.Topic]
		if m == nil {
			m = map[int]int64{}
			out[e.Topic] = m
		}
		m[e.Partition] += e.SizeBytes
	}
	return out
}

func fetchLastRecordTimes(ctx context.Context, inst *store.Instance, needs []lastNeed) map[string]time.Time {
	out := make(map[string]time.Time, len(needs))
	if len(needs) == 0 {
		return out
	}
	addr := bootstrapAddr(inst)
	var mu sync.Mutex
	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup
	for _, need := range needs {
		need := need
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			r := kafkago.NewReader(kafkago.ReaderConfig{
				Brokers:   []string{addr},
				Topic:     need.topic,
				Partition: need.partition,
				MaxBytes:  1e6,
				MaxWait:   300 * time.Millisecond,
			})
			defer r.Close()
			if err := r.SetOffset(need.offset); err != nil {
				return
			}
			msg, err := r.ReadMessage(readCtx)
			if err != nil || msg.Time.IsZero() {
				return
			}
			mu.Lock()
			out[need.topic] = msg.Time
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func DeleteTopic(ctx context.Context, inst *store.Instance, topic string) error {
	addr := bootstrapAddr(inst)
	conn, err := dialBroker(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("controller: %w", err)
	}
	ctrlConn, err := dialBroker(ctx, controllerAddr(controller))
	if err != nil {
		return err
	}
	defer ctrlConn.Close()
	if err := ctrlConn.DeleteTopics(topic); err != nil {
		return err
	}
	if inst != nil {
		InvalidateTopicListCache(inst.ID)
	}
	return nil
}
