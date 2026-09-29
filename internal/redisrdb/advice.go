package redisrdb

import (
	"fmt"
	"strings"
)

func buildAdvice(r *Report) []AdviceItem {
	if r == nil {
		return nil
	}
	var out []AdviceItem
	if r.TotalKeys == 0 {
		out = append(out, AdviceItem{
			Level: "info", Code: "empty_rdb", Title: "RDB 无业务 Key",
			Detail: "解析完成但未发现业务对象（可能为空库或仅含元数据）。",
		})
		return out
	}

	if r.TotalKeys > 0 {
		ttlRatio := float64(r.KeysWithTTL) / float64(r.TotalKeys)
		if ttlRatio < 0.05 && r.TotalKeys >= 1000 {
			out = append(out, AdviceItem{
				Level: "warn", Code: "low_ttl_ratio", Title: "绝大多数 Key 无过期时间",
				Detail:  fmt.Sprintf("有 TTL 的 Key 仅约 %.1f%%（%d/%d）。", ttlRatio*100, r.KeysWithTTL, r.TotalKeys),
				Suggest: "为缓存类前缀设置合理 EXPIRE/TTL，或使用 volatile-* 淘汰策略并配合 maxmemory。",
			})
		}
	}

	for _, b := range r.TTLBuckets {
		if b.Bucket == "expired" && b.Count > 0 {
			out = append(out, AdviceItem{
				Level: "info", Code: "expired_present", Title: "RDB 中仍含已过期对象",
				Detail:  fmt.Sprintf("约 %d 个 Key（%.2f MB）在 dump 时刻已过期，仍占用 RDB。", b.Count, float64(b.Bytes)/(1024*1024)),
				Suggest: "检查 lazyfree / active expire；过期量过大时可在低峰做一次主动清理后再备份。",
			})
			break
		}
	}

	if len(r.TopKeys) > 0 {
		top := r.TopKeys[0]
		if top.Bytes >= 10*1024*1024 {
			out = append(out, AdviceItem{
				Level: "critical", Code: "huge_key", Title: "存在超大 Key",
				Detail:  fmt.Sprintf("最大 Key「%s」(%s) 约 %.2f MB，元素数 %d。", truncate(top.Key, 80), top.Type, float64(top.Bytes)/(1024*1024), top.ElemCount),
				Suggest: "拆分大 Hash/List/ZSet，或改用外部对象存储；避免单 Key 阻塞主线程。",
			})
		} else if top.Bytes >= 1*1024*1024 {
			out = append(out, AdviceItem{
				Level: "warn", Code: "large_key", Title: "存在较大 Key",
				Detail:  fmt.Sprintf("最大 Key「%s」(%s) 约 %.2f MB。", truncate(top.Key, 80), top.Type, float64(top.Bytes)/(1024*1024)),
				Suggest: "评估是否可压缩、分片或降低元素粒度。",
			})
		}
	}

	if len(r.ByPrefix) > 0 && r.TotalBytes > 0 {
		p := r.ByPrefix[0]
		share := float64(p.Bytes) / float64(r.TotalBytes)
		if share >= 0.4 && p.Count >= 100 {
			out = append(out, AdviceItem{
				Level: "warn", Code: "prefix_hotspot", Title: "前缀内存高度集中",
				Detail:  fmt.Sprintf("前缀「%s」约占内存 %.1f%%（%d keys）。", p.Prefix, share*100, p.Count),
				Suggest: "针对该业务前缀做容量规划、TTL 与数据结构专项优化。",
			})
		}
	}

	for _, t := range r.ByType {
		if (t.Type == "list" || t.Type == "hash" || t.Type == "zset" || t.Type == "set") && t.Count > 0 {
			avg := float64(t.Bytes) / float64(t.Count)
			if avg >= 512*1024 {
				out = append(out, AdviceItem{
					Level: "warn", Code: "heavy_collection", Title: fmt.Sprintf("%s 类型平均体积偏大", strings.ToUpper(t.Type)),
					Detail:  fmt.Sprintf("%s 共 %d 个，平均约 %.1f KB。", t.Type, t.Count, avg/1024),
					Suggest: "检查是否滥用大集合；考虑 ziplist/listpack 编码阈值或业务拆分。",
				})
			}
		}
	}

	if len(out) == 0 {
		out = append(out, AdviceItem{
			Level: "info", Code: "healthy", Title: "未发现明显内存风险",
			Detail: "类型分布与大 Key 规模在常见阈值内；建议结合在线 INFO 与监控曲线持续观察。",
		})
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
