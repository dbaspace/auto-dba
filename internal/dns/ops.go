package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"

	ObjectZone   = "zone"
	ObjectRange  = "range"
	ObjectRecord = "record"

	SourceUI       = "ui"
	SourceImport   = "import"
	SourceRollback = "rollback"
	SourceTicket   = "ticket"
	SourceTTL      = "ttl"
)

// Op 一次变更。Expect 非空时要求当前值与之一致；Expect 为 null 表示要求对象不存在。
type Op struct {
	Action string          `json:"action"`
	Object string          `json:"object"`
	ID     string          `json:"id,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
	Expect json.RawMessage `json:"expect,omitempty"`
}

type OpMeta struct {
	Actor    string
	Source   string
	TicketID string
	Reason   string
	// 只在 Source 为 ttl 时使用：写入预降记录。
	TTLHold *TTLHoldSpec
	// DryRun 完整执行校验后回滚，不落库。
	DryRun bool
}

type AppliedItem struct {
	Seq     int    `json:"seq"`
	Action  string `json:"action"`
	Object  string `json:"object"`
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type ApplyResult struct {
	BatchID  string        `json:"batch_id"`
	Items    []AppliedItem `json:"items"`
	Warnings []Issue       `json:"warnings"`
}

// LintError 校验出现错误级问题，整批不写入。
type LintError struct {
	Issues []Issue
}

func (e *LintError) Error() string {
	msgs := make([]string, 0, len(e.Issues))
	for _, it := range e.Issues {
		msgs = append(msgs, it.Message)
	}
	return "校验未通过：" + strings.Join(msgs, "；")
}

// ConflictError 当前值与期望不一致（回滚或审批执行时数据已被他人修改）。
type ConflictError struct {
	Items []string
}

func (e *ConflictError) Error() string {
	return "数据已变更，无法执行：" + strings.Join(e.Items, "；")
}

type applyState struct {
	ctx       context.Context
	tx        *sql.Tx
	meta      OpMeta
	batchID   string
	now       int64
	syncZone  bool
	updRecs   bool
	recIDs    map[string]bool
	keys      map[string]bool
	names     map[string]bool
	conflicts []string
}

// ApplyOps 在一个事务里完成：写 zones/rrs/parsing_ranges、写变更日志、校验、serial 加一。
func (s *Store) ApplyOps(ctx context.Context, ops []Op, meta OpMeta) (*ApplyResult, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("没有需要执行的变更")
	}
	if strings.TrimSpace(meta.Source) == "" {
		meta.Source = SourceUI
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	st := &applyState{
		ctx: ctx, tx: tx, meta: meta, batchID: uuid.NewString(), now: time.Now().Unix(),
		recIDs: map[string]bool{}, keys: map[string]bool{}, names: map[string]bool{},
	}
	res := &ApplyResult{BatchID: st.batchID}
	for i, op := range ops {
		item, err := st.apply(i+1, op)
		if err != nil {
			if len(st.conflicts) > 0 {
				return nil, &ConflictError{Items: st.conflicts}
			}
			return nil, err
		}
		if item != nil {
			res.Items = append(res.Items, *item)
		}
	}
	if len(st.conflicts) > 0 {
		return nil, &ConflictError{Items: st.conflicts}
	}
	snap, err := loadSnapshot(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	var errs []Issue
	for _, it := range Lint(snap) {
		if !st.touches(it) {
			continue
		}
		switch it.Level {
		case LevelError:
			errs = append(errs, it)
		case LevelWarn:
			res.Warnings = append(res.Warnings, it)
		}
	}
	if len(errs) > 0 {
		return nil, &LintError{Issues: errs}
	}
	if st.syncZone {
		if err := bumpTx(ctx, tx, SerialSyncZone, st.now); err != nil {
			return nil, err
		}
	}
	if st.updRecs {
		if err := bumpTx(ctx, tx, SerialUpdateZones, st.now); err != nil {
			return nil, err
		}
	}
	if meta.TTLHold != nil {
		if err := insertTTLHolds(ctx, tx, meta, st.batchID, st.now); err != nil {
			return nil, err
		}
	}
	if !meta.DryRun {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	if res.Warnings == nil {
		res.Warnings = []Issue{}
	}
	return res, nil
}

func (st *applyState) touches(it Issue) bool {
	for _, id := range it.RecordIDs {
		if st.recIDs[id] {
			return true
		}
	}
	if it.Key != "" && st.keys[it.Key] {
		return true
	}
	for _, n := range it.Names {
		if st.names[n] {
			return true
		}
	}
	return false
}

func (st *applyState) conflict(format string, args ...any) {
	st.conflicts = append(st.conflicts, fmt.Sprintf(format, args...))
}

func (st *applyState) apply(seq int, op Op) (*AppliedItem, error) {
	switch op.Object {
	case ObjectZone:
		return st.applyZone(seq, op)
	case ObjectRange:
		return st.applyRange(seq, op)
	case ObjectRecord:
		return st.applyRecord(seq, op)
	default:
		return nil, fmt.Errorf("第 %d 项对象类型无效", seq)
	}
}

// expectMatch 判断当前值是否满足 Expect。cur 为 nil 表示不存在。
func expectMatch[T any](raw json.RawMessage, cur *T, norm func(*T)) (bool, error) {
	if len(raw) == 0 {
		return true, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return cur == nil, nil
	}
	if cur == nil {
		return false, nil
	}
	var want T
	if err := json.Unmarshal(raw, &want); err != nil {
		return false, err
	}
	c := *cur
	norm(&want)
	norm(&c)
	return reflect.DeepEqual(want, c), nil
}

func normZoneCmp(z *ZoneSnap) { z.Origin = strings.ToLower(z.Origin) }
func normRangeCmp(r *RangeSnap) {
	if r.NetworkList == nil {
		r.NetworkList = []string{}
	}
}
func normRecordCmp(r *RecordSnap) { r.MailHost, r.Preference = "", 0 }

func (st *applyState) log(seq int, op Op, objectID, zoneID, summary string, before, after any) error {
	var b, a []byte
	if before != nil && !reflect.ValueOf(before).IsNil() {
		b, _ = json.Marshal(before)
	}
	if after != nil && !reflect.ValueOf(after).IsNil() {
		a, _ = json.Marshal(after)
	}
	_, err := st.tx.ExecContext(st.ctx, `
INSERT INTO tbl_dns_change_logs(id, batch_id, seq, source, actor, ticket_id, reason, action, object_type, object_id, zone_id, summary, before_json, after_json, created_time)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		uuid.NewString(), st.batchID, seq, st.meta.Source, st.meta.Actor, st.meta.TicketID, st.meta.Reason,
		op.Action, op.Object, objectID, zoneID, summary, nullJSON(b), nullJSON(a), st.now)
	return err
}

func nullJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (st *applyState) applyZone(seq int, op Op) (*AppliedItem, error) {
	ctx, tx := st.ctx, st.tx
	id := strings.TrimSpace(op.ID)
	var after ZoneSnap
	if op.Action != ActionDelete {
		if err := json.Unmarshal(op.After, &after); err != nil {
			return nil, fmt.Errorf("第 %d 项解析域内容无效", seq)
		}
		if id == "" {
			id = strings.TrimSpace(after.ID)
		}
	}
	var before *ZoneSnap
	var err error
	if id != "" {
		if before, err = getZoneSnap(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if ok, err := expectMatch(op.Expect, before, normZoneCmp); err != nil {
		return nil, err
	} else if !ok {
		st.conflict("解析域 %s 当前值与期望不一致", firstNonEmpty(after.Origin, zoneLabel(before), id))
		return nil, nil
	}
	st.syncZone = true
	switch op.Action {
	case ActionCreate, ActionUpdate:
		origin, err := NormalizeOrigin(after.Origin)
		if err != nil {
			return nil, err
		}
		status := NormalizeStatus(after.Status)
		if status == "" {
			return nil, fmt.Errorf("解析域状态无效")
		}
		snap := &ZoneSnap{Origin: origin, Status: status}
		if op.Action == ActionCreate {
			if before != nil {
				return nil, fmt.Errorf("解析域 %s 已存在", origin)
			}
			if id == "" {
				id = uuid.NewString()
			}
			snap.ID = id
			_, err = tx.ExecContext(ctx, `
INSERT INTO zones(id, origin, ns, mbox, serial, refresh, retry, expire, mininum, ttl, xfer, bind, status, created_time, updated_time)
VALUES($1,$2,$3,$4,1,3600,600,86400,300,600,0,false,$5,$6,$6)`,
				id, origin, "ns."+origin, "hostmaster."+origin, status, st.now)
			if err != nil {
				return nil, mapDup(err, "解析域 "+origin+" 已存在")
			}
		} else {
			if before == nil {
				return nil, fmt.Errorf("解析域不存在")
			}
			snap.ID = id
			_, err = tx.ExecContext(ctx, `UPDATE zones SET origin=$2, ns=$3, mbox=$4, status=$5, updated_time=$6 WHERE id=$1`,
				id, origin, "ns."+origin, "hostmaster."+origin, status, st.now)
			if err != nil {
				return nil, mapDup(err, "解析域 "+origin+" 已存在")
			}
		}
		sum := op.Action + " " + origin
		if err := st.log(seq, op, id, id, sum, before, snap); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectZone, ID: id, Summary: sum}, nil
	case ActionDelete:
		if before == nil {
			return nil, fmt.Errorf("解析域不存在")
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM rrs WHERE zone_id=$1`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("解析域 %s 下仍有 %d 条记录，请先删除记录", before.Origin, n)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM zones WHERE id=$1`, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tbl_dns_zone_policies WHERE zone_id=$1`, id); err != nil {
			return nil, err
		}
		sum := "delete " + before.Origin
		if err := st.log(seq, op, id, id, sum, before, (*ZoneSnap)(nil)); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectZone, ID: id, Summary: sum}, nil
	default:
		return nil, fmt.Errorf("第 %d 项操作无效", seq)
	}
}

func (st *applyState) applyRange(seq int, op Op) (*AppliedItem, error) {
	ctx, tx := st.ctx, st.tx
	id := strings.TrimSpace(op.ID)
	var after RangeSnap
	if op.Action != ActionDelete {
		if err := json.Unmarshal(op.After, &after); err != nil {
			return nil, fmt.Errorf("第 %d 项解析范围内容无效", seq)
		}
		if id == "" {
			id = strings.TrimSpace(after.ID)
		}
	}
	if id == DefaultRangeID {
		return nil, fmt.Errorf("DEFAULT 为系统解析范围，不能修改")
	}
	var before *RangeSnap
	var err error
	if id != "" {
		if before, err = getRangeSnap(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if before != nil && before.Name == DefaultRangeName {
		return nil, fmt.Errorf("DEFAULT 为系统解析范围，不能修改")
	}
	if ok, err := expectMatch(op.Expect, before, normRangeCmp); err != nil {
		return nil, err
	} else if !ok {
		st.conflict("解析范围 %s 当前值与期望不一致", firstNonEmpty(after.Name, rangeLabel(before), id))
		return nil, nil
	}
	st.syncZone = true
	switch op.Action {
	case ActionCreate, ActionUpdate:
		name := strings.TrimSpace(after.Name)
		if name == "" {
			return nil, fmt.Errorf("解析范围名称为空")
		}
		if strings.EqualFold(name, DefaultRangeName) {
			return nil, fmt.Errorf("DEFAULT 为系统解析范围")
		}
		status := NormalizeStatus(after.Status)
		if status == "" {
			return nil, fmt.Errorf("解析范围状态无效")
		}
		clean := make([]string, 0, len(after.NetworkList))
		for _, c := range after.NetworkList {
			if c = strings.TrimSpace(c); c != "" {
				clean = append(clean, c)
			}
		}
		if len(clean) == 0 {
			return nil, fmt.Errorf("解析范围 %s 未填写来源网段", name)
		}
		if op.Action == ActionCreate && id == "" {
			id = uuid.NewString()
		}
		others, err := otherCIDRs(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if err := CheckCIDRList(clean, others); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(clean)
		snap := &RangeSnap{ID: id, Name: name, NetworkList: clean, Description: strings.TrimSpace(after.Description), Status: status}
		if op.Action == ActionCreate {
			if before != nil {
				return nil, fmt.Errorf("解析范围已存在")
			}
			_, err = tx.ExecContext(ctx, `
INSERT INTO parsing_ranges(id, name, network_list, description, status, bind, created_time, updated_time)
VALUES($1,$2,$3::jsonb,$4,$5,false,$6,$6)`, id, name, string(raw), snap.Description, status, st.now)
		} else {
			if before == nil {
				return nil, fmt.Errorf("解析范围不存在")
			}
			_, err = tx.ExecContext(ctx, `UPDATE parsing_ranges SET name=$2, network_list=$3::jsonb, description=$4, status=$5, updated_time=$6 WHERE id=$1`,
				id, name, string(raw), snap.Description, status, st.now)
		}
		if err != nil {
			return nil, err
		}
		sum := op.Action + " " + name + " " + strings.Join(clean, ",")
		if err := st.log(seq, op, id, "", sum, before, snap); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectRange, ID: id, Summary: sum}, nil
	case ActionDelete:
		if before == nil {
			return nil, fmt.Errorf("解析范围不存在")
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM rrs WHERE parsing_range_id=$1`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("解析范围 %s 下仍有 %d 条记录，请先删除记录", before.Name, n)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM parsing_ranges WHERE id=$1`, id); err != nil {
			return nil, err
		}
		sum := "delete " + before.Name
		if err := st.log(seq, op, id, "", sum, before, (*RangeSnap)(nil)); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectRange, ID: id, Summary: sum}, nil
	default:
		return nil, fmt.Errorf("第 %d 项操作无效", seq)
	}
}

func (st *applyState) applyRecord(seq int, op Op) (*AppliedItem, error) {
	ctx, tx := st.ctx, st.tx
	id := strings.TrimSpace(op.ID)
	var after RecordSnap
	if op.Action != ActionDelete {
		if err := json.Unmarshal(op.After, &after); err != nil {
			return nil, fmt.Errorf("第 %d 项记录内容无效", seq)
		}
		if id == "" {
			id = strings.TrimSpace(after.ID)
		}
	}
	var before *RecordSnap
	var err error
	if id != "" {
		if before, err = getRecordSnap(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if ok, err := expectMatch(op.Expect, before, normRecordCmp); err != nil {
		return nil, err
	} else if !ok {
		label := id
		if before != nil {
			label = before.Name + " " + before.Type
		} else if after.Name != "" {
			label = after.Name + " " + after.Type
		}
		st.conflict("记录 %s 当前值与期望不一致", label)
		return nil, nil
	}
	st.updRecs = true
	if before != nil {
		st.markRecord(ctx, before)
	}
	switch op.Action {
	case ActionCreate, ActionUpdate:
		snap, origin, err := normRecord(ctx, tx, after)
		if err != nil {
			return nil, fmt.Errorf("记录 %s：%w", after.Name, err)
		}
		if op.Action == ActionCreate {
			if before != nil {
				return nil, fmt.Errorf("记录已存在")
			}
			if id == "" {
				id = uuid.NewString()
			}
			snap.ID = id
			_, err = tx.ExecContext(ctx, `
INSERT INTO rrs(id, name, type, data, aux, ttl, serial, zone_id, parsing_range_id, description, status, created_by, last_update_by, created_time, updated_time)
VALUES($1,$2,$3,$4,'',$5,1,$6,$7,$8,$9,$10,$10,$11,$11)`,
				id, snap.Name, snap.Type, snap.Data, snap.TTL, snap.ZoneID, snap.RangeID, snap.Description, snap.Status, st.meta.Actor, st.now)
		} else {
			if before == nil {
				return nil, fmt.Errorf("记录不存在")
			}
			snap.ID = id
			_, err = tx.ExecContext(ctx, `
UPDATE rrs SET name=$2, type=$3, data=$4, ttl=$5, zone_id=$6, parsing_range_id=$7, description=$8, status=$9, last_update_by=$10, updated_time=$11
WHERE id=$1`, id, snap.Name, snap.Type, snap.Data, snap.TTL, snap.ZoneID, snap.RangeID, snap.Description, snap.Status, st.meta.Actor, st.now)
		}
		if err != nil {
			return nil, mapDup(err, fmt.Sprintf("记录 %s %s %s 已存在", snap.Name, snap.Type, snap.Data))
		}
		st.recIDs[id] = true
		st.keys[recordKey(snap.ZoneID, snap.RangeID, snap.Name)] = true
		st.names[FQDN(snap.Name, origin)] = true
		sum := fmt.Sprintf("%s %s %s → %s", op.Action, FQDN(snap.Name, origin), snap.Type, snap.Data)
		if err := st.log(seq, op, id, snap.ZoneID, sum, before, snap); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectRecord, ID: id, Summary: sum}, nil
	case ActionDelete:
		if before == nil {
			return nil, fmt.Errorf("记录不存在")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM rrs WHERE id=$1`, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tbl_dns_ttl_holds SET status=$2, message=$3, restored_time=$4 WHERE record_id=$1 AND status=$5`,
			id, TTLHoldInvalid, "记录已删除", st.now, TTLHoldHolding); err != nil {
			return nil, err
		}
		sum := fmt.Sprintf("delete %s %s %s", before.Name, before.Type, before.Data)
		if err := st.log(seq, op, id, before.ZoneID, sum, before, (*RecordSnap)(nil)); err != nil {
			return nil, err
		}
		return &AppliedItem{Seq: seq, Action: op.Action, Object: ObjectRecord, ID: id, Summary: sum}, nil
	default:
		return nil, fmt.Errorf("第 %d 项操作无效", seq)
	}
}

// markRecord 旧值涉及的主机名也纳入校验范围（例如删掉 CNAME 目标）。
func (st *applyState) markRecord(ctx context.Context, r *RecordSnap) {
	st.recIDs[r.ID] = true
	st.keys[recordKey(r.ZoneID, r.RangeID, r.Name)] = true
	var origin string
	if err := st.tx.QueryRowContext(ctx, `SELECT origin FROM zones WHERE id=$1`, r.ZoneID).Scan(&origin); err == nil {
		st.names[FQDN(r.Name, origin)] = true
	}
}

func recordKey(zoneID, rangeID, name string) string {
	return zoneID + "|" + rangeID + "|" + strings.ToLower(name)
}

// normRecord 校验并折算记录，返回所属域 origin。
func normRecord(ctx context.Context, q querier, in RecordSnap) (*RecordSnap, string, error) {
	rrType, err := NormalizeType(in.Type)
	if err != nil {
		return nil, "", err
	}
	ttl, err := NormalizeTTL(in.TTL)
	if err != nil {
		return nil, "", err
	}
	status := NormalizeStatus(in.Status)
	if status == "" {
		return nil, "", fmt.Errorf("状态无效")
	}
	host, err := NormalizeHost(in.Name)
	if err != nil {
		return nil, "", err
	}
	var data string
	if rrType == "MX" && strings.TrimSpace(in.MailHost) == "" {
		if _, _, ok := DecodeMX(in.Data); ok {
			data = strings.TrimSpace(in.Data)
		}
	}
	if data == "" {
		if data, err = NormalizeData(rrType, in.Data, in.MailHost, in.Preference); err != nil {
			return nil, "", err
		}
	}
	var origin string
	if err := q.QueryRowContext(ctx, `SELECT origin FROM zones WHERE id=$1`, in.ZoneID).Scan(&origin); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("解析域不存在")
		}
		return nil, "", err
	}
	var rangeName string
	if err := q.QueryRowContext(ctx, `SELECT name FROM parsing_ranges WHERE id=$1`, in.RangeID).Scan(&rangeName); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("解析范围不存在")
		}
		return nil, "", err
	}
	if rangeName == DefaultRangeName {
		return nil, "", fmt.Errorf("不能向 DEFAULT 解析范围添加记录")
	}
	return &RecordSnap{
		Name: host, Type: rrType, Data: data, TTL: ttl, ZoneID: in.ZoneID, RangeID: in.RangeID,
		Description: strings.TrimSpace(in.Description), Status: status,
	}, origin, nil
}

func otherCIDRs(ctx context.Context, q querier, exceptID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, network_list FROM parsing_ranges WHERE name <> $1`, DefaultRangeName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if id == exceptID {
			continue
		}
		var list []string
		_ = json.Unmarshal(raw, &list)
		out = append(out, list...)
	}
	return out, rows.Err()
}

func bumpTx(ctx context.Context, tx *sql.Tx, name string, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE serials SET serial = serial + 1, updated_time = $2 WHERE name = $1`, name, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO serials(id, name, serial, parsing_range_id, updated_time) VALUES($1,$2,1,'',$3)`, uuid.NewString(), name, now)
	}
	return err
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func zoneLabel(z *ZoneSnap) string {
	if z == nil {
		return ""
	}
	return z.Origin
}

func rangeLabel(r *RangeSnap) string {
	if r == nil {
		return ""
	}
	return r.Name
}

// PinExpect 给未带期望值的修改、删除补上当前值，供延后执行（审批）时做冲突检查。
func (s *Store) PinExpect(ctx context.Context, ops []Op) ([]Op, error) {
	out := make([]Op, len(ops))
	for i, op := range ops {
		out[i] = op
		if len(op.Expect) > 0 || op.Action == ActionCreate || strings.TrimSpace(op.ID) == "" {
			continue
		}
		var cur any
		var err error
		switch op.Object {
		case ObjectZone:
			cur, err = getZoneSnap(ctx, s.db, op.ID)
		case ObjectRange:
			cur, err = getRangeSnap(ctx, s.db, op.ID)
		case ObjectRecord:
			cur, err = getRecordSnap(ctx, s.db, op.ID)
		}
		if err != nil {
			return nil, err
		}
		out[i].Expect = mustJSON(cur)
	}
	return out, nil
}

// RecordOp 以完整记录内容构造 Op。
func RecordOp(action, id string, r RecordSnap) Op {
	raw, _ := json.Marshal(r)
	if action == ActionDelete {
		raw = nil
	}
	return Op{Action: action, Object: ObjectRecord, ID: id, After: raw}
}

func ZoneOp(action, id string, z ZoneSnap) Op {
	raw, _ := json.Marshal(z)
	if action == ActionDelete {
		raw = nil
	}
	return Op{Action: action, Object: ObjectZone, ID: id, After: raw}
}

func RangeOp(action, id string, r RangeSnap) Op {
	raw, _ := json.Marshal(r)
	if action == ActionDelete {
		raw = nil
	}
	return Op{Action: action, Object: ObjectRange, ID: id, After: raw}
}
