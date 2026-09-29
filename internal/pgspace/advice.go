package pgspace

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// AdviceReport 读取预制建议。
func (s *Service) AdviceReport(ctx context.Context, instanceID, host string, port int64) (*AdviceData, error) {
	target, err := s.resolveTarget(instanceID, host, port)
	if err != nil {
		return nil, err
	}
	row, ok, err := s.Store.GetSpaceAdviceLatest(ctx, target.inst.ID, target.host, target.port)
	if err != nil {
		return nil, err
	}
	if !ok {
		row, ok, err = s.Store.GetSpaceAdviceLatest(ctx, "", target.host, target.port)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return &AdviceData{
			InstanceID: target.inst.ID, InstanceName: target.inst.Name,
			Host: target.host, Port: target.port,
			Severity: pgSpaceAdviceSevNone, Advices: []*Advice{},
			Message: "暂无预制建议，请先触发表空间采集（建议在采集完成后自动生成）",
		}, nil
	}
	rep, err := decodeAdviceReport(row)
	if err != nil {
		return nil, err
	}
	if rep.InstanceID == "" {
		rep.InstanceID = row.InstanceID
	}
	if rep.InstanceName == "" {
		rep.InstanceName = row.InstanceName
	}
	if rep.Host == "" {
		rep.Host = row.Host
	}
	if rep.Port == 0 {
		rep.Port = row.Port
	}
	if rep.CollectedAt == nil {
		t := row.CollectedAt
		rep.CollectedAt = &t
	}
	if rep.Advices == nil {
		rep.Advices = []*Advice{}
	}
	return rep, nil
}

// AdviceFleet 跨实例建议汇总。
func (s *Service) AdviceFleet(ctx context.Context, severity string, limit int) (*AdviceFleetData, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	sevFilter := strings.ToLower(strings.TrimSpace(severity))

	rows, err := s.Store.ListSpaceAdviceLatest(ctx)
	if err != nil {
		return nil, err
	}
	physicals, _ := s.enumeratePhysicalPostgresInstances()
	totalInstances := len(physicals)
	if totalInstances == 0 {
		totalInstances = len(rows)
	}

	prefabByHost := map[string]*store.SpaceAdviceLatest{}
	for _, r := range rows {
		prefabByHost[fmt.Sprintf("%s:%d", strings.ToLower(r.Host), r.Port)] = r
	}

	out := &AdviceFleetData{
		TotalInstances:  totalInstances,
		AdviceTypeStats: []*AdviceTypeStat{},
		Instances:       []*AdviceInstanceBrief{},
	}
	typeAgg := map[string]*AdviceTypeStat{}
	var briefs []*AdviceInstanceBrief
	seen := map[string]bool{}

	consume := func(r *store.SpaceAdviceLatest) {
		if r == nil {
			return
		}
		key := fmt.Sprintf("%s:%d", strings.ToLower(r.Host), r.Port)
		if seen[key] {
			return
		}
		seen[key] = true

		switch r.Severity {
		case pgSpaceAdviceSevCritical:
			out.CriticalCount++
		case pgSpaceAdviceSevWarning:
			out.WarningCount++
		default:
			out.NoneCount++
		}

		if sevFilter == pgSpaceAdviceSevCritical && r.Severity != pgSpaceAdviceSevCritical {
			return
		}
		if sevFilter == pgSpaceAdviceSevWarning && r.Severity == pgSpaceAdviceSevNone {
			return
		}
		if sevFilter != "" && sevFilter != pgSpaceAdviceSevWarning && sevFilter != pgSpaceAdviceSevCritical {
			return
		}
		if r.AdviceCount == 0 {
			return
		}

		rep, err := decodeAdviceReport(r)
		if err != nil || rep == nil {
			return
		}
		out.IssueInstanceCount++
		out.AdviceTotal += r.AdviceCount
		out.ReclaimableBytesTotal += r.ReclaimableBytes

		top := rep.Advices
		if len(top) > 3 {
			top = top[:3]
		}
		briefs = append(briefs, &AdviceInstanceBrief{
			InstanceID: r.InstanceID, InstanceName: r.InstanceName,
			Host: r.Host, Port: r.Port,
			Severity: r.Severity, AdviceCount: r.AdviceCount,
			ReclaimableBytes: r.ReclaimableBytes, TopAdvices: top,
		})
		for _, a := range rep.Advices {
			if a == nil {
				continue
			}
			st := typeAgg[a.Key]
			if st == nil {
				st = &AdviceTypeStat{Key: a.Key, Title: a.Title, Action: a.Action}
				typeAgg[a.Key] = st
			}
			st.Count++
			if a.Severity == pgSpaceAdviceSevCritical {
				st.CriticalCount++
			} else {
				st.WarningCount++
			}
		}
	}

	if len(physicals) > 0 {
		for _, p := range physicals {
			k := fmt.Sprintf("%s:%d", strings.ToLower(p.target.host), p.target.port)
			if r := prefabByHost[k]; r != nil {
				consume(r)
			} else {
				out.NoneCount++
			}
		}
	} else {
		for _, r := range rows {
			consume(r)
		}
	}

	for _, st := range typeAgg {
		out.AdviceTypeStats = append(out.AdviceTypeStats, st)
	}
	sort.SliceStable(out.AdviceTypeStats, func(i, j int) bool {
		if out.AdviceTypeStats[i].CriticalCount != out.AdviceTypeStats[j].CriticalCount {
			return out.AdviceTypeStats[i].CriticalCount > out.AdviceTypeStats[j].CriticalCount
		}
		return out.AdviceTypeStats[i].Count > out.AdviceTypeStats[j].Count
	})
	sort.SliceStable(briefs, func(i, j int) bool {
		pi, pj := advicePriority(briefs[i].Severity, briefs[i].TopAdvices), advicePriority(briefs[j].Severity, briefs[j].TopAdvices)
		if pi != pj {
			return pi < pj
		}
		return briefs[i].AdviceCount > briefs[j].AdviceCount
	})
	if len(briefs) > limit {
		briefs = briefs[:limit]
	}
	out.Instances = briefs
	return out, nil
}

func decodeAdviceReport(row *store.SpaceAdviceLatest) (*AdviceData, error) {
	if row == nil {
		return nil, fmt.Errorf("nil advice row")
	}
	rep := &AdviceData{}
	if len(row.ReportJSON) > 0 {
		if err := json.Unmarshal(row.ReportJSON, rep); err != nil {
			return nil, fmt.Errorf("decode advice report: %w", err)
		}
	}
	return rep, nil
}

func (s *Service) persistAdviceFromRows(ctx context.Context, rows []*store.SpaceRow, instanceID, instanceName, host string, port int64, collectedAt time.Time) {
	if len(rows) == 0 {
		return
	}
	rep := s.evaluateAdvice(ctx, rows)
	rep.InstanceID = instanceID
	rep.InstanceName = instanceName
	rep.Host = host
	rep.Port = port
	ca := collectedAt
	rep.CollectedAt = &ca
	rep.Summary = nil

	raw, err := json.Marshal(rep)
	if err != nil {
		return
	}
	_ = s.Store.UpsertSpaceAdviceLatest(ctx, &store.SpaceAdviceLatest{
		InstanceID: instanceID, InstanceName: instanceName,
		Host: host, Port: port, CollectedAt: collectedAt,
		Severity: rep.Severity, AdviceCount: rep.AdviceCount,
		ReclaimableBytes: rep.ReclaimableBytes,
		XIDRiskCount:     rep.XIDRiskCount, SequenceRiskCount: rep.SequenceRiskCount,
		VacuumRiskCount: rep.VacuumRiskCount, AccessRiskCount: rep.AccessRiskCount,
		CacheRiskCount: rep.CacheRiskCount, HotTableCount: rep.HotTableCount,
		ReportJSON: raw,
	})
}

func (s *Service) evaluateAdvice(ctx context.Context, rows []*store.SpaceRow) *AdviceData {
	rep := &AdviceData{Severity: pgSpaceAdviceSevNone, Advices: []*Advice{}}
	var tables, indexes, sequences, databases []*store.SpaceRow
	for _, r := range rows {
		switch r.ObjectType {
		case "table":
			tables = append(tables, r)
		case "index":
			indexes = append(indexes, r)
		case "sequence":
			sequences = append(sequences, r)
		case "database":
			databases = append(databases, r)
		}
	}

	var advices []*Advice
	advices = append(advices, adviceXID(databases, tables)...)
	advices = append(advices, adviceSequences(sequences)...)
	advices = append(advices, adviceDeadVacuum(tables)...)
	advices = append(advices, adviceAnalyzeAccess(tables)...)
	advices = append(advices, adviceCacheHot(tables)...)
	advices = append(advices, adviceUnusedIndexes(indexes)...)
	advices = append(advices, s.adviceGrowth(ctx, databases)...)

	sort.SliceStable(advices, func(i, j int) bool {
		pi, pj := adviceKeyPriority(advices[i]), adviceKeyPriority(advices[j])
		if pi != pj {
			return pi < pj
		}
		if advices[i].Severity != advices[j].Severity {
			return advices[i].Severity == pgSpaceAdviceSevCritical
		}
		return advices[i].Key < advices[j].Key
	})
	rep.Advices = advices
	rep.AdviceCount = len(advices)

	for _, a := range advices {
		if a.Severity == pgSpaceAdviceSevCritical {
			rep.Severity = pgSpaceAdviceSevCritical
		} else if rep.Severity != pgSpaceAdviceSevCritical {
			rep.Severity = pgSpaceAdviceSevWarning
		}
		switch a.Key {
		case "xid_age_high", "xid_age_critical":
			rep.XIDRiskCount++
		case "sequence_low", "sequence_exhaust", "sequence_cycle_risk":
			rep.SequenceRiskCount++
		case "dead_tuples", "table_bloat", "vacuum_stale":
			rep.VacuumRiskCount++
		case "seq_scan_heavy", "analyze_stale":
			rep.AccessRiskCount++
		case "cache_hit_low":
			rep.CacheRiskCount++
		case "hot_table_dml":
			rep.HotTableCount++
		}
		if a.Key == "unused_index_bytes" || a.Key == "table_bloat" || a.Key == "dead_tuples" {
			for _, ev := range a.Evidence {
				if ev.SizeBytes != nil && a.Key == "unused_index_bytes" {
					rep.ReclaimableBytes += *ev.SizeBytes
				}
				if ev.BloatBytes != nil && (a.Key == "table_bloat" || a.Key == "dead_tuples") {
					rep.ReclaimableBytes += *ev.BloatBytes
				}
			}
		}
	}
	return rep
}

func adviceXID(dbs, tables []*store.SpaceRow) []*Advice {
	var out []*Advice
	const warnAge, critAge = float64(5e8), float64(1e9)
	add := func(r *store.SpaceRow, age int64, title string) {
		sev := pgSpaceAdviceSevWarning
		key := "xid_age_high"
		th := warnAge
		if float64(age) >= critAge {
			sev = pgSpaceAdviceSevCritical
			key = "xid_age_critical"
			th = critAge
		} else if float64(age) < warnAge {
			return
		}
		v := float64(age)
		out = append(out, &Advice{
			Key: key, Title: title, Severity: sev, Action: "vacuum_freeze",
			Message: fmt.Sprintf("%s %s 冻结事务年龄约 %.2e，存在事务 ID 回卷风险，请尽快 VACUUM (FREEZE) 或排查阻塞 autovacuum 的长事务。",
				r.ObjectType, objLabel(r), v),
			Value: &v, Threshold: &th, Unit: "xid_age",
			Evidence: []*Object{rowToObject(r)},
		})
	}
	for _, r := range dbs {
		if r.DatFrozenXIDAge != nil {
			add(r, *r.DatFrozenXIDAge, "库事务冻结年龄")
		}
	}
	for _, r := range tables {
		if r.RelFrozenXIDAge != nil && valInt64(r.SizeBytes) >= 100*1024*1024 {
			add(r, *r.RelFrozenXIDAge, "表事务冻结年龄")
		}
	}
	return out
}

func adviceSequences(seqs []*store.SpaceRow) []*Advice {
	var out []*Advice
	for _, r := range seqs {
		used := valFloat64(r.SeqUsedRatio)
		rem := valInt64(r.SeqRemaining)
		cycle := r.SeqCycle != nil && *r.SeqCycle
		if cycle && used >= 0.8 {
			v := used
			th := 0.8
			out = append(out, &Advice{
				Key: "sequence_cycle_risk", Title: "循环序列使用偏高", Severity: pgSpaceAdviceSevWarning,
				Action:  "review_sequence_cycle",
				Message: fmt.Sprintf("序列 %s 为 CYCLE 且已用约 %.1f%%，请评估循环覆盖风险。", objLabel(r), used*100),
				Value:   &v, Threshold: &th, Unit: "ratio", Evidence: []*Object{rowToObject(r)},
			})
			continue
		}
		if cycle {
			continue
		}
		sev, key, th := "", "", 0.0
		if used >= 0.95 || (rem > 0 && rem <= 1e7) || (rem == 0 && used > 0) {
			sev, key, th = pgSpaceAdviceSevCritical, "sequence_exhaust", 0.95
		} else if used >= 0.80 || (rem > 0 && rem <= 1e8) {
			sev, key, th = pgSpaceAdviceSevWarning, "sequence_low", 0.80
		} else {
			continue
		}
		v := used
		out = append(out, &Advice{
			Key: key, Title: "序列余量风险", Severity: sev, Action: "expand_sequence",
			Message: fmt.Sprintf("序列 %s 已用 %.1f%%，剩余约 %d，建议 ALTER SEQUENCE 扩容或主键改 bigint。",
				objLabel(r), used*100, rem),
			Value: &v, Threshold: &th, Unit: "ratio", Evidence: []*Object{rowToObject(r)},
		})
	}
	return out
}

func adviceDeadVacuum(tables []*store.SpaceRow) []*Advice {
	var out []*Advice
	now := time.Now().UTC()
	const minSize = int64(100 * 1024 * 1024)
	for _, r := range tables {
		if valInt64(r.SizeBytes) < minSize {
			continue
		}
		dr := valFloat64(r.DeadRatio)
		if dr >= 0.10 {
			sev, th := pgSpaceAdviceSevWarning, 0.10
			key, action, title := "dead_tuples", "vacuum_dead", "死元组比例偏高"
			if dr >= 0.20 || valInt64(r.BloatBytes) >= 1<<30 {
				sev, th = pgSpaceAdviceSevCritical, 0.20
				if valInt64(r.BloatBytes) >= 1<<30 {
					key, action, title = "table_bloat", "vacuum_bloat", "表膨胀严重"
				}
			}
			v := dr
			out = append(out, &Advice{
				Key: key, Title: title, Severity: sev, Action: action,
				Message: fmt.Sprintf("表 %s 死元组比约 %.1f%%（轻量估算膨胀），建议 VACUUM；自统计重置以来累计口径。",
					objLabel(r), dr*100),
				Value: &v, Threshold: &th, Unit: "ratio", Evidence: []*Object{rowToObject(r)},
			})
		}
		vacAge := maintenanceAgeHours(now, r.LastVacuum, r.LastAutovacuum)
		neverVac := vacAge < 0
		if neverVac || vacAge >= 7*24 {
			if !neverVac && vacAge < 30*24 && dr < 0.1 {
				// skip noise
			} else {
				sev := pgSpaceAdviceSevWarning
				if neverVac || vacAge >= 30*24 {
					sev = pgSpaceAdviceSevCritical
				}
				days := vacAge / 24
				msg := fmt.Sprintf("表 %s 约 %.0f 天未 VACUUM（含 autovacuum），建议检查清理策略。", objLabel(r), days)
				if neverVac {
					msg = fmt.Sprintf("表 %s 未见 VACUUM/autovacuum 记录，建议检查 autovacuum。", objLabel(r))
					days = -1
				}
				v := days
				th := 7.0
				out = append(out, &Advice{
					Key: "vacuum_stale", Title: "长期未 VACUUM", Severity: sev, Action: "run_vacuum",
					Message: msg, Value: &v, Threshold: &th, Unit: "days",
					Evidence: []*Object{rowToObject(r)},
				})
			}
		}
	}
	return out
}

func adviceAnalyzeAccess(tables []*store.SpaceRow) []*Advice {
	var out []*Advice
	now := time.Now().UTC()
	const minSize = int64(100 * 1024 * 1024)
	for _, r := range tables {
		sz := valInt64(r.SizeBytes)
		if sz < minSize {
			continue
		}
		anaAge := maintenanceAgeHours(now, r.LastAnalyze, r.LastAutoanalyze)
		if anaAge < 0 || anaAge >= 7*24 {
			sev := pgSpaceAdviceSevWarning
			if anaAge < 0 || anaAge >= 30*24 {
				sev = pgSpaceAdviceSevCritical
			}
			days := anaAge / 24
			msg := fmt.Sprintf("表 %s 约 %.0f 天未 ANALYZE，统计可能过旧导致错误计划。", objLabel(r), days)
			if anaAge < 0 {
				msg = fmt.Sprintf("表 %s 未见 ANALYZE/autoanalyze 记录，建议 ANALYZE。", objLabel(r))
			}
			v := days
			th := 7.0
			out = append(out, &Advice{
				Key: "analyze_stale", Title: "长期未 ANALYZE", Severity: sev, Action: "run_analyze",
				Message: msg, Value: &v, Threshold: &th, Unit: "days",
				Evidence: []*Object{rowToObject(r)},
			})
		}
		seq, idx := valInt64(r.SeqScan), valInt64(r.TblIdxScan)
		total := seq + idx
		if total < 1000 {
			continue
		}
		ratio := float64(seq) / float64(total)
		sev, th := "", 0.0
		if sz >= 1<<30 && ratio >= 0.9 {
			sev, th = pgSpaceAdviceSevCritical, 0.9
		} else if ratio >= 0.8 {
			sev, th = pgSpaceAdviceSevWarning, 0.8
		} else {
			continue
		}
		v := ratio
		out = append(out, &Advice{
			Key: "seq_scan_heavy", Title: "顺序扫描过重", Severity: sev, Action: "review_index_or_analyze",
			Message: fmt.Sprintf("表 %s 顺序扫描占比 %.1f%%（seq_scan=%d, idx_scan=%d，自 reset 累计），建议检查缺失索引或 ANALYZE。",
				objLabel(r), ratio*100, seq, idx),
			Value: &v, Threshold: &th, Unit: "ratio", Evidence: []*Object{rowToObject(r)},
		})
	}
	return out
}

func adviceCacheHot(tables []*store.SpaceRow) []*Advice {
	var out []*Advice
	const minSize = int64(100 * 1024 * 1024)
	for _, r := range tables {
		if valInt64(r.SizeBytes) < minSize {
			continue
		}
		hit, read := valInt64(r.HeapBlksHit), valInt64(r.HeapBlksRead)
		if hit+read < 10000 {
			continue
		}
		ratio := valFloat64(r.HeapCacheHitRatio)
		if ratio <= 0 && hit+read > 0 {
			ratio = float64(hit) / float64(hit+read)
		}
		sev, th := "", 0.0
		if ratio <= 0.90 {
			sev, th = pgSpaceAdviceSevCritical, 0.90
		} else if ratio <= 0.95 {
			sev, th = pgSpaceAdviceSevWarning, 0.95
		}
		if sev != "" {
			v := ratio
			out = append(out, &Advice{
				Key: "cache_hit_low", Title: "堆缓存命中率偏低", Severity: sev, Action: "tune_memory_or_query",
				Message: fmt.Sprintf("表 %s 堆缓存命中率 %.1f%%（hit=%d, read=%d，自 reset 累计），建议评估内存或优化扫描范围。",
					objLabel(r), ratio*100, hit, read),
				Value: &v, Threshold: &th, Unit: "ratio", Evidence: []*Object{rowToObject(r)},
			})
		}
		dml := nTupDML(r)
		if dml >= 1e7 && valInt64(r.SizeBytes) >= 50*1024*1024 {
			sev2 := pgSpaceAdviceSevWarning
			th2 := 1e7
			if dml >= 1e8 || valFloat64(r.DeadRatio) >= 0.2 {
				sev2 = pgSpaceAdviceSevCritical
				th2 = 1e8
			}
			v := float64(dml)
			out = append(out, &Advice{
				Key: "hot_table_dml", Title: "DML 热点表", Severity: sev2, Action: "review_hot_table",
				Message: fmt.Sprintf("表 %s 累计 DML≈%.2e（ins/upd/del=%d/%d/%d，自 reset），建议评估分区/归档与 autovacuum。",
					objLabel(r), v, valInt64(r.NTupIns), valInt64(r.NTupUpd), valInt64(r.NTupDel)),
				Value: &v, Threshold: &th2, Unit: "rows", Evidence: []*Object{rowToObject(r)},
			})
		}
	}
	return out
}

func adviceUnusedIndexes(indexes []*store.SpaceRow) []*Advice {
	var unused []*store.SpaceRow
	var total int64
	for _, r := range indexes {
		if r.IsUnused != nil && *r.IsUnused {
			unused = append(unused, r)
			total += valInt64(r.SizeBytes)
		}
	}
	if total < 1<<30 {
		return nil
	}
	sev := pgSpaceAdviceSevWarning
	th := float64(1 << 30)
	if total >= 10*(1<<30) {
		sev = pgSpaceAdviceSevCritical
		th = float64(10 * (1 << 30))
	}
	sort.SliceStable(unused, func(i, j int) bool {
		return valInt64(unused[i].SizeBytes) > valInt64(unused[j].SizeBytes)
	})
	ev := rowsToDTO(head(unused, 10))
	v := float64(total)
	return []*Advice{{
		Key: "unused_index_bytes", Title: "无用索引浪费空间", Severity: sev, Action: "drop_unused_index",
		Message: fmt.Sprintf("无用索引合计约 %.2f GB（idx_scan 全分片为 0），建议评估后 DROP。", float64(total)/(1<<30)),
		Value:   &v, Threshold: &th, Unit: "bytes", Evidence: ev,
	}}
}

func (s *Service) adviceGrowth(ctx context.Context, databases []*store.SpaceRow) []*Advice {
	var out []*Advice
	if len(databases) == 0 {
		return out
	}
	dbs := append([]*store.SpaceRow(nil), databases...)
	sort.SliceStable(dbs, func(i, j int) bool {
		return valInt64(dbs[i].SizeBytes) > valInt64(dbs[j].SizeBytes)
	})
	if len(dbs) > pgSpaceAdviceGrowthTopN {
		dbs = dbs[:pgSpaceAdviceGrowthTopN]
	}

	capacity := s.defaultCapacityBytes()
	start := time.Now().UTC().Add(-90 * 24 * time.Hour)
	for _, r := range dbs {
		points, err := s.Store.SpaceTrend(ctx, store.SpaceTrendFilter{
			InstanceID: r.InstanceID, Host: r.Host, Port: r.Port,
			ObjectType: "database", DbName: r.DbName, ObjectName: r.ObjectName,
			StartTime: &start,
		})
		if err != nil || len(points) < 2 {
			continue
		}
		first, last := points[0], points[len(points)-1]
		if first.SizeBytes == nil || last.SizeBytes == nil {
			continue
		}
		days := last.CollectedAt.Sub(first.CollectedAt).Hours() / 24
		if days < 0.5 {
			continue
		}
		growth := float64(*last.SizeBytes-*first.SizeBytes) / days
		sev, th := "", 0.0
		if growth >= 20*(1<<30) {
			sev, th = pgSpaceAdviceSevCritical, 20*(1<<30)
		} else if growth >= 5*(1<<30) {
			sev, th = pgSpaceAdviceSevWarning, 5*(1<<30)
		}
		if sev != "" {
			v := growth
			out = append(out, &Advice{
				Key: "db_growth", Title: "库日均增长过快", Severity: sev, Action: "review_growth",
				Message: fmt.Sprintf("库 %s 日均增长约 %.2f GB，请关注业务写入与归档。", r.ObjectName, growth/(1<<30)),
				Value:   &v, Threshold: &th, Unit: "bytes_per_day", Evidence: []*Object{rowToObject(r)},
			})
		}
		if capacity > 0 && growth > 0 {
			remain := float64(capacity - *last.SizeBytes)
			if remain > 0 {
				daysToFull := remain / growth
				sev2, th2 := "", 0.0
				if daysToFull <= 7 {
					sev2, th2 = pgSpaceAdviceSevCritical, 7
				} else if daysToFull <= 30 {
					sev2, th2 = pgSpaceAdviceSevWarning, 30
				}
				if sev2 != "" {
					v := daysToFull
					out = append(out, &Advice{
						Key: "days_to_full", Title: "预估满盘天数", Severity: sev2, Action: "expand_or_purge",
						Message: fmt.Sprintf("库 %s 按当前增速约 %.0f 天后达到配置容量，建议扩容或清理。", r.ObjectName, daysToFull),
						Value:   &v, Threshold: &th2, Unit: "days", Evidence: []*Object{rowToObject(r)},
					})
				}
			}
		}
	}
	return out
}

func maintenanceAgeHours(now time.Time, a, b *time.Time) float64 {
	var latest *time.Time
	if a != nil {
		latest = a
	}
	if b != nil && (latest == nil || b.After(*latest)) {
		latest = b
	}
	if latest == nil {
		return -1
	}
	return now.Sub(*latest).Hours()
}

func objLabel(r *store.SpaceRow) string {
	if r.SchemaName != "" {
		return r.SchemaName + "." + r.ObjectName
	}
	if r.DbName != "" && r.ObjectName != r.DbName {
		return r.DbName + "." + r.ObjectName
	}
	return r.ObjectName
}

func adviceKeyPriority(a *Advice) int {
	switch a.Key {
	case "xid_age_critical", "xid_age_high":
		return 1
	case "sequence_exhaust", "sequence_low", "sequence_cycle_risk":
		return 2
	case "dead_tuples", "table_bloat", "vacuum_stale":
		return 3
	case "analyze_stale", "seq_scan_heavy":
		return 4
	case "cache_hit_low", "hot_table_dml":
		return 5
	default:
		return 6
	}
}

func advicePriority(sev string, top []*Advice) int {
	if sev == pgSpaceAdviceSevCritical {
		for _, a := range top {
			if a.Key == "xid_age_critical" || a.Key == "xid_age_high" {
				return 0
			}
		}
		return 1
	}
	if sev == pgSpaceAdviceSevWarning {
		return 2
	}
	return 3
}
