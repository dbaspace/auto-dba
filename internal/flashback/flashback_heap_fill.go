package flashback

// 堆页扫描参考 PDU-PostgreSQLDataUnloader decode.c / read.c
// https://github.com/wublabdubdub/PDU-PostgreSQLDataUnloader
// Licensed under Apache License 2.0

import (
	"context"
	"database/sql"
	"encoding/binary"
	"strconv"
	"strings"
)

const (
	flashbackHeapPageSize        = 8192
	flashbackPageHeaderSize      = 24
	flashbackItemIDSize          = 4
	flashbackLPNormal            = 1
	flashbackHeapXmaxInvalid     = 0x0800
	flashbackOldSourceWAL        = "wal"
	flashbackOldSourceFPW        = "fpw"
	flashbackOldSourceHeapSlot   = "heap_slot"
	flashbackOldSourceHeapScan   = "heap_scan"
	flashbackOldSourceIncomplete = "incomplete"
)

type flashbackHeapTuple struct {
	XMin     uint32
	XMax     uint32
	Block    uint32
	Offnum   uint16
	Infomask uint16
	Hoff     int
	Raw      []byte
	Dead     bool
	LSN      uint64
	Values   map[string]string
}

type flashbackOldFillStats struct {
	FromWAL, FromFPW, HeapSlot, HeapScan, Incomplete, ScanTrunc int
}

func (s *flashbackOldFillStats) note(src string, truncated bool) {
	if s == nil {
		return
	}
	switch src {
	case flashbackOldSourceWAL:
		s.FromWAL++
	case flashbackOldSourceFPW:
		s.FromFPW++
	case flashbackOldSourceHeapSlot:
		s.HeapSlot++
	case flashbackOldSourceHeapScan:
		s.HeapScan++
	case flashbackOldSourceIncomplete:
		s.Incomplete++
	}
	if truncated {
		s.ScanTrunc++
	}
}

func (s flashbackOldFillStats) WarnSuffix() string {
	parts := []string{}
	if s.FromWAL > 0 {
		parts = append(parts, "old_from_wal="+strconv.Itoa(s.FromWAL))
	}
	if s.FromFPW > 0 {
		parts = append(parts, "old_from_fpw="+strconv.Itoa(s.FromFPW))
	}
	if s.HeapSlot > 0 {
		parts = append(parts, "old_from_heap_slot="+strconv.Itoa(s.HeapSlot))
	}
	if s.HeapScan > 0 {
		parts = append(parts, "old_from_heap_scan="+strconv.Itoa(s.HeapScan))
	}
	if s.Incomplete > 0 {
		parts = append(parts, "old_incomplete="+strconv.Itoa(s.Incomplete))
	}
	if s.ScanTrunc > 0 {
		parts = append(parts, "heap_scan_truncated="+strconv.Itoa(s.ScanTrunc))
	}
	if len(parts) == 0 {
		return ""
	}
	return "旧行来源统计：" + strings.Join(parts, "，") + "。"
}

func flashbackScanHeapPage(page []byte, blk uint32, rel *flashbackRelation, includeDead bool) []flashbackHeapTuple {
	if len(page) < flashbackPageHeaderSize {
		return nil
	}
	pdLower := binary.LittleEndian.Uint16(page[12:14])
	pdUpper := binary.LittleEndian.Uint16(page[14:16])
	pdSpecial := binary.LittleEndian.Uint16(page[16:18])
	if int(pdLower) < flashbackPageHeaderSize || int(pdUpper) > len(page) || pdSpecial > uint16(len(page)) || pdLower > pdUpper {
		return nil
	}
	lsn := binary.LittleEndian.Uint64(page[0:8])
	nitems := (int(pdLower) - flashbackPageHeaderSize) / flashbackItemIDSize
	var out []flashbackHeapTuple
	for i := 0; i < nitems; i++ {
		off := flashbackPageHeaderSize + i*flashbackItemIDSize
		item := binary.LittleEndian.Uint32(page[off : off+4])
		lpOff := uint16(item & 0x7FFF)
		lpFlags := (item >> 15) & 0x3
		lpLen := uint16((item >> 17) & 0x7FFF)
		if lpFlags != flashbackLPNormal || lpLen < flashbackSizeofHeapTuple {
			continue
		}
		if int(lpOff)+int(lpLen) > len(page) {
			continue
		}
		raw := make([]byte, lpLen)
		copy(raw, page[lpOff:int(lpOff)+int(lpLen)])
		if len(raw) < flashbackSizeofHeapTuple {
			continue
		}
		xmin := binary.LittleEndian.Uint32(raw[0:4])
		xmax := binary.LittleEndian.Uint32(raw[4:8])
		infomask := binary.LittleEndian.Uint16(raw[20:22])
		hoff := int(raw[22])
		if hoff < flashbackSizeofHeapTuple || hoff > len(raw) {
			continue
		}
		dead := xmax != 0 && infomask&flashbackHeapXmaxInvalid == 0
		if dead && !includeDead {
			continue
		}
		tup := flashbackHeapTuple{
			XMin: xmin, XMax: xmax, Block: blk, Offnum: uint16(i + 1),
			Infomask: infomask, Hoff: hoff, Raw: raw, Dead: dead, LSN: lsn,
		}
		if rel != nil {
			tup.Values = flashbackDecodeAttrs(rel, raw, infomask, flashbackSizeofHeapTuple, hoff)
		}
		out = append(out, tup)
	}
	return out
}

func flashbackValueIsNull(v string) bool {
	return v == `\N` || strings.EqualFold(v, "NULL") || strings.TrimSpace(v) == ""
}

func flashbackValuesAllNull(vals map[string]string) bool {
	if len(vals) == 0 {
		return true
	}
	for _, v := range vals {
		if !flashbackValueIsNull(v) {
			return false
		}
	}
	return true
}

func flashbackValuesIncomplete(rel *flashbackRelation, vals map[string]string) bool {
	if flashbackValuesAllNull(vals) {
		return true
	}
	if rel == nil {
		for _, v := range vals {
			if flashbackValueIsNull(v) {
				return true
			}
		}
		return false
	}
	for _, c := range rel.Columns {
		if c.Dropped {
			continue
		}
		v, ok := vals[c.Name]
		if !ok || flashbackValueIsNull(v) {
			return true
		}
	}
	return false
}

func flashbackMergeNonNull(dst, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		if !flashbackValueIsNull(v) {
			dst[k] = v
		}
	}
	return dst
}

func flashbackHeapValsMatch(vals, known map[string]string) bool {
	if len(known) == 0 {
		return false
	}
	for k, want := range known {
		got, ok := vals[k]
		if !ok || flashbackValueIsNull(got) || got != want {
			return false
		}
	}
	return true
}

// flashbackLooksLikeNewRow 判断候选行是否像 UPDATE 之后的新行（禁止当 Old 使用）。
func flashbackLooksLikeNewRow(vals, newVals map[string]string) bool {
	if len(newVals) == 0 || len(vals) == 0 {
		return false
	}
	matched := 0
	for k, want := range newVals {
		if flashbackValueIsNull(want) {
			continue
		}
		got, ok := vals[k]
		if !ok || flashbackValueIsNull(got) || got != want {
			return false
		}
		matched++
	}
	return matched > 0
}

func flashbackKnownOldKeys(ch *flashbackChange) map[string]string {
	if ch == nil {
		return nil
	}
	keys := map[string]string{}
	for _, pk := range ch.PKCols {
		if v, ok := ch.Old[pk]; ok && !flashbackValueIsNull(v) {
			keys[pk] = v
		}
	}
	if len(keys) == 0 {
		for k, v := range ch.Old {
			if !flashbackValueIsNull(v) {
				keys[k] = v
			}
		}
	}
	return keys
}

func flashbackHeapTupleUsableAsOld(ch *flashbackChange, t flashbackHeapTuple) bool {
	if ch == nil {
		return false
	}
	if strings.EqualFold(ch.Op, "UPDATE") {
		if flashbackLooksLikeNewRow(t.Values, ch.New) {
			return false
		}
		if !t.Dead {
			return false
		}
	}
	return true
}

// flashbackPickHeapOld 从候选中选旧行：匹配主键的 dead > 精确槽位 dead >（仅 DELETE）匹配主键的 live。
func flashbackPickHeapOld(ch *flashbackChange, cands []flashbackHeapTuple) map[string]string {
	if ch == nil || len(cands) == 0 {
		return nil
	}
	keys := flashbackKnownOldKeys(ch)
	var deadMatch, slotDead, liveMatch map[string]string
	for _, t := range cands {
		if !flashbackHeapTupleUsableAsOld(ch, t) {
			continue
		}
		matchKey := len(keys) > 0 && flashbackHeapValsMatch(t.Values, keys)
		matchSlot := ch.Offnum != 0 && t.Offnum == ch.Offnum && (ch.Block == 0 || t.Block == ch.Block)
		if t.Dead && matchKey {
			deadMatch = t.Values
		}
		if t.Dead && matchSlot && slotDead == nil {
			slotDead = t.Values
		}
		if !t.Dead && matchKey && strings.EqualFold(ch.Op, "DELETE") {
			liveMatch = t.Values
		}
	}
	if deadMatch != nil {
		return deadMatch
	}
	if slotDead != nil {
		return slotDead
	}
	return liveMatch
}

func flashbackFillOldFromDB(ctx context.Context, db *sql.DB, dict *flashbackDictionary, ch *flashbackChange, maxPages int, st *flashbackOldFillStats) {
	if db == nil || dict == nil || ch == nil || ch.MySQL {
		return
	}
	rel := dict.match(ch.Schema, ch.Table)
	if rel == nil {
		return
	}
	if ch.Old == nil {
		ch.Old = map[string]string{}
	}
	if !flashbackValuesIncomplete(rel, ch.Old) {
		src := ch.OldSource
		if src == "" {
			src = flashbackOldSourceWAL
		}
		st.note(src, false)
		return
	}
	truncated := false
	if ch.Offnum > 0 {
		page := flashbackReadHeapPageDB(ctx, db, rel, dict.DBOID, ch.Block)
		if flashbackMergeOldFromPage(rel, ch, page, ch.Block) != "" {
			if !flashbackValuesIncomplete(rel, ch.Old) {
				ch.OldSource = flashbackOldSourceHeapSlot
				st.note(flashbackOldSourceHeapSlot, false)
				return
			}
		}
	}
	if flashbackValuesIncomplete(rel, ch.Old) {
		vals, trunc := flashbackScanHeapByKnownDB(ctx, db, rel, dict.DBOID, ch, maxPages)
		truncated = trunc
		if len(vals) > 0 {
			ch.Old = flashbackMergeNonNull(ch.Old, vals)
			if !flashbackValuesIncomplete(rel, ch.Old) {
				ch.OldSource = flashbackOldSourceHeapScan
				st.note(flashbackOldSourceHeapScan, truncated)
				return
			}
		}
	}
	ch.OldSource = flashbackOldSourceIncomplete
	st.note(flashbackOldSourceIncomplete, truncated)
}

func flashbackMergeOldFromPage(rel *flashbackRelation, ch *flashbackChange, page []byte, blk uint32) string {
	if ch == nil || len(page) == 0 {
		return ""
	}
	cands := flashbackScanHeapPage(page, blk, rel, true)
	picked := flashbackPickHeapOld(ch, cands)
	if len(picked) == 0 {
		for _, t := range cands {
			if ch.Offnum != 0 && t.Offnum != ch.Offnum {
				continue
			}
			if strings.EqualFold(ch.Op, "UPDATE") && (!t.Dead || flashbackLooksLikeNewRow(t.Values, ch.New)) {
				continue
			}
			ch.Old = flashbackMergeNonNull(ch.Old, t.Values)
			return flashbackOldSourceHeapSlot
		}
		return ""
	}
	ch.Old = flashbackMergeNonNull(ch.Old, picked)
	return flashbackOldSourceHeapSlot
}

func flashbackScanHeapByKnownDB(ctx context.Context, db *sql.DB, rel *flashbackRelation, dboid uint32, ch *flashbackChange, maxPages int) (map[string]string, bool) {
	if db == nil || rel == nil || ch == nil {
		return nil, false
	}
	if maxPages <= 0 {
		maxPages = flashbackDefaultHeapScanPages
	}
	keys := flashbackKnownOldKeys(ch)
	if len(keys) == 0 {
		return nil, false
	}
	var nPages int64
	if rel.OID != 0 {
		_ = db.QueryRowContext(ctx, `SELECT GREATEST(1, (pg_relation_size($1::oid) + 8191) / 8192)`, rel.OID).Scan(&nPages)
	}
	if nPages <= 0 && rel.RelNode != 0 {
		path, _ := flashbackHeapRelFile(dboid, rel.RelNode, 0)
		_ = db.QueryRowContext(ctx, `
SELECT COALESCE((SELECT (size + 8191) / 8192 FROM pg_stat_file($1, true) WHERE size IS NOT NULL), 0)`, path).Scan(&nPages)
	}
	if nPages <= 0 {
		nPages = int64(maxPages)
	}
	truncated := false
	limit := int64(maxPages)
	if nPages > limit {
		truncated = true
		nPages = limit
	}
	var deadMatch, liveMatch map[string]string
	for blk := uint32(0); int64(blk) < nPages; blk++ {
		page := flashbackReadHeapPageDB(ctx, db, rel, dboid, blk)
		if len(page) == 0 {
			continue
		}
		for _, t := range flashbackScanHeapPage(page, blk, rel, true) {
			if !flashbackHeapValsMatch(t.Values, keys) {
				continue
			}
			if !flashbackHeapTupleUsableAsOld(ch, t) {
				continue
			}
			if flashbackValuesIncomplete(rel, t.Values) {
				continue
			}
			if t.Dead {
				deadMatch = t.Values
			} else if strings.EqualFold(ch.Op, "DELETE") {
				liveMatch = t.Values
			}
		}
		if deadMatch != nil {
			return deadMatch, truncated
		}
	}
	return liveMatch, truncated
}

func flashbackHeapRelFile(dboid, relnode, blk uint32) (relPath string, off int64) {
	pagesPerSeg := uint32((1024 * 1024 * 1024) / flashbackHeapPageSize)
	seg := blk / pagesPerSeg
	name := strconv.FormatUint(uint64(relnode), 10)
	if dboid == 0 {
		relPath = "global/" + name
	} else {
		relPath = "base/" + strconv.FormatUint(uint64(dboid), 10) + "/" + name
	}
	if seg > 0 {
		relPath += "." + strconv.FormatUint(uint64(seg), 10)
	}
	return relPath, int64(blk%pagesPerSeg) * int64(flashbackHeapPageSize)
}

func flashbackReadHeapPageDB(ctx context.Context, db *sql.DB, rel *flashbackRelation, dboid, blk uint32) []byte {
	if db == nil || rel == nil {
		return nil
	}
	pagesPerSeg := uint32((1024 * 1024 * 1024) / flashbackHeapPageSize)
	seg := int(blk / pagesPerSeg)
	off := int64(blk%pagesPerSeg) * int64(flashbackHeapPageSize)
	var raw []byte
	if rel.OID != 0 {
		q := `SELECT pg_read_binary_file(
CASE WHEN $2 = 0 THEN pg_relation_filepath($1::oid)
     ELSE pg_relation_filepath($1::oid) || '.' || $2::text END, $3, $4)`
		if err := db.QueryRowContext(ctx, q, rel.OID, seg, off, flashbackHeapPageSize).Scan(&raw); err == nil && len(raw) == flashbackHeapPageSize {
			return raw
		}
	}
	if rel.RelNode != 0 {
		path, fileOff := flashbackHeapRelFile(dboid, rel.RelNode, blk)
		if err := db.QueryRowContext(ctx, `SELECT pg_read_binary_file($1, $2, $3)`, path, fileOff, flashbackHeapPageSize).Scan(&raw); err == nil && len(raw) == flashbackHeapPageSize {
			return raw
		}
	}
	return nil
}

func flashbackCol(name, typ string, typlen int, align string) flashbackColumn {
	return flashbackColumn{
		Name: name, TypeName: typ, Typlen: typlen, Typalign: align, TypType: "b", BaseName: typ,
	}
}

func flashbackBuildRel(cols []flashbackColumn) *flashbackRelation {
	rel := &flashbackRelation{colByNum: map[int]flashbackColumn{}}
	for i := range cols {
		cols[i].AttNum = i + 1
		rel.Columns = append(rel.Columns, cols[i])
		rel.colByNum[cols[i].AttNum] = cols[i]
	}
	return rel
}
