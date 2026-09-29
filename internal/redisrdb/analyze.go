package redisrdb

import (
	"container/heap"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hdt3213/rdb/core"
	"github.com/hdt3213/rdb/model"
)

var skipTypes = map[string]bool{
	"aux":       true,
	"dbsize":    true,
	"function":  true,
	"functions": true,
}

// AnalyzeFile 解析 RDB 文件并生成报告（专业离线内存画像）。
func AnalyzeFile(path string, topN int) (*Report, error) {
	if topN <= 0 {
		topN = DefaultTopN
	}
	if topN > MaxTopN {
		topN = MaxTopN
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 RDB 失败: %w", err)
	}
	defer f.Close()

	start := time.Now()
	rep := &Report{
		GeneratedAt: start,
		ByType:      nil,
		ByDB:        nil,
		ByPrefix:    nil,
		TTLBuckets:  nil,
		TopKeys:     nil,
		Advice:      nil,
	}

	typeAgg := map[string]*TypeStat{}
	dbAgg := map[int]*DBStat{}
	prefixAgg := map[string]*PrefixStat{}
	ttlAgg := map[string]*TTLBucket{
		"no_ttl":   {Bucket: "no_ttl"},
		"expired":  {Bucket: "expired"},
		"lt_1h":    {Bucket: "lt_1h"},
		"1h_24h":   {Bucket: "1h_24h"},
		"1d_7d":    {Bucket: "1d_7d"},
		"gt_7d":    {Bucket: "gt_7d"},
	}
	top := &keyHeap{limit: topN}
	heap.Init(top)
	now := time.Now()
	aux := map[string]string{}

	dec := core.NewDecoder(f).WithSpecialOpCode()
	err = dec.Parse(func(o model.RedisObject) bool {
		if o == nil {
			return true
		}
		typ := strings.ToLower(strings.TrimSpace(o.GetType()))
		if skipTypes[typ] {
			if typ == "aux" {
				if ao, ok := o.(*model.AuxObject); ok && ao != nil {
					aux[ao.GetKey()] = ao.Value
				}
			}
			return true
		}
		key := o.GetKey()
		if key == "" {
			return true
		}
		sz := o.GetSize()
		if sz < 0 {
			sz = 0
		}
		bytes := int64(sz)
		db := o.GetDBIndex()
		enc := o.GetEncoding()
		elem := o.GetElemCount()
		exp := o.GetExpiration()

		rep.TotalKeys++
		rep.TotalBytes += bytes

		ts := typeAgg[typ]
		if ts == nil {
			ts = &TypeStat{Type: typ}
			typeAgg[typ] = ts
		}
		ts.Count++
		ts.Bytes += bytes

		ds := dbAgg[db]
		if ds == nil {
			ds = &DBStat{DB: db}
			dbAgg[db] = ds
		}
		ds.Count++
		ds.Bytes += bytes

		pfx := keyPrefix(key)
		ps := prefixAgg[pfx]
		if ps == nil {
			ps = &PrefixStat{Prefix: pfx}
			prefixAgg[pfx] = ps
		}
		ps.Count++
		ps.Bytes += bytes

		item := KeyItem{
			DB:        db,
			Key:       key,
			Type:      typ,
			Encoding:  enc,
			Bytes:     bytes,
			ElemCount: elem,
		}
		bucket := "no_ttl"
		if exp == nil {
			rep.KeysPersistent++
		} else {
			rep.KeysWithTTL++
			item.HasTTL = true
			item.ExpireAt = exp.UTC().Format(time.RFC3339)
			sec := int64(exp.Sub(now).Seconds())
			item.TTLSeconds = sec
			switch {
			case sec < 0:
				bucket = "expired"
			case sec < 3600:
				bucket = "lt_1h"
			case sec < 86400:
				bucket = "1h_24h"
			case sec < 86400*7:
				bucket = "1d_7d"
			default:
				bucket = "gt_7d"
			}
		}
		tb := ttlAgg[bucket]
		tb.Count++
		tb.Bytes += bytes

		top.push(item)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("解析 RDB 失败: %w", err)
	}

	rep.RedisVersion = aux["redis-ver"]
	rep.RDBVersion = aux["rdb-ver"]
	rep.DurationMS = time.Since(start).Milliseconds()

	for _, v := range typeAgg {
		rep.ByType = append(rep.ByType, *v)
	}
	sort.Slice(rep.ByType, func(i, j int) bool { return rep.ByType[i].Bytes > rep.ByType[j].Bytes })

	for _, v := range dbAgg {
		rep.ByDB = append(rep.ByDB, *v)
	}
	sort.Slice(rep.ByDB, func(i, j int) bool { return rep.ByDB[i].Bytes > rep.ByDB[j].Bytes })

	for _, v := range prefixAgg {
		rep.ByPrefix = append(rep.ByPrefix, *v)
	}
	sort.Slice(rep.ByPrefix, func(i, j int) bool { return rep.ByPrefix[i].Bytes > rep.ByPrefix[j].Bytes })
	if len(rep.ByPrefix) > 50 {
		rep.ByPrefix = rep.ByPrefix[:50]
	}

	order := []string{"no_ttl", "expired", "lt_1h", "1h_24h", "1d_7d", "gt_7d"}
	for _, k := range order {
		rep.TTLBuckets = append(rep.TTLBuckets, *ttlAgg[k])
	}

	rep.TopKeys = top.sorted()
	rep.Advice = buildAdvice(rep)
	return rep, nil
}

func keyPrefix(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(empty)"
	}
	for _, sep := range []string{":", "|", "/", "_"} {
		if i := strings.Index(key, sep); i > 0 && i < 64 {
			return key[:i+1]
		}
	}
	if len(key) > 24 {
		return key[:24] + "…"
	}
	return key
}

type keyHeap struct {
	limit int
	data  []KeyItem
}

func (h *keyHeap) Len() int           { return len(h.data) }
func (h *keyHeap) Less(i, j int) bool { return h.data[i].Bytes < h.data[j].Bytes } // min-heap
func (h *keyHeap) Swap(i, j int)      { h.data[i], h.data[j] = h.data[j], h.data[i] }
func (h *keyHeap) Push(x any)         { h.data = append(h.data, x.(KeyItem)) }
func (h *keyHeap) Pop() any {
	n := len(h.data)
	x := h.data[n-1]
	h.data = h.data[:n-1]
	return x
}

func (h *keyHeap) push(it KeyItem) {
	if h.limit <= 0 {
		return
	}
	if h.Len() < h.limit {
		heap.Push(h, it)
		return
	}
	if it.Bytes > h.data[0].Bytes {
		heap.Pop(h)
		heap.Push(h, it)
	}
}

func (h *keyHeap) sorted() []KeyItem {
	out := append([]KeyItem(nil), h.data...)
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
