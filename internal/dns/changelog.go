package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ChangeLog struct {
	ID          string          `json:"id"`
	BatchID     string          `json:"batch_id"`
	Seq         int             `json:"seq"`
	Source      string          `json:"source"`
	Actor       string          `json:"actor"`
	TicketID    string          `json:"ticket_id"`
	Reason      string          `json:"reason"`
	Action      string          `json:"action"`
	ObjectType  string          `json:"object_type"`
	ObjectID    string          `json:"object_id"`
	ZoneID      string          `json:"zone_id"`
	Summary     string          `json:"summary"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	CreatedTime int64           `json:"created_time"`
}

type ChangeBatch struct {
	BatchID     string `json:"batch_id"`
	Source      string `json:"source"`
	Actor       string `json:"actor"`
	TicketID    string `json:"ticket_id"`
	Reason      string `json:"reason"`
	Count       int    `json:"count"`
	Summary     string `json:"summary"`
	CreatedTime int64  `json:"created_time"`
	// RolledBackBy 回滚本批次的批次号（Reason 中记录）；只做展示。
	RolledBackBy string `json:"rolled_back_by,omitempty"`
}

type ChangeFilter struct {
	ObjectID string
	ZoneID   string
	Actor    string
	Source   string
	Keyword  string
	From     int64
	To       int64
	Page     int
	PageSize int
}

// ListChangeBatches 按批次倒序列出变更。
func (s *Store) ListChangeBatches(ctx context.Context, f ChangeFilter) ([]ChangeBatch, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 20
	}
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.ObjectID != "" {
		add("object_id=$%d", f.ObjectID)
	}
	if f.ZoneID != "" {
		add("zone_id=$%d", f.ZoneID)
	}
	if f.Actor != "" {
		add("actor ILIKE $%d", "%"+f.Actor+"%")
	}
	if f.Source != "" {
		add("source=$%d", f.Source)
	}
	if f.Keyword != "" {
		add("summary ILIKE $%d", "%"+f.Keyword+"%")
	}
	if f.From > 0 {
		add("created_time >= $%d", f.From)
	}
	if f.To > 0 {
		add("created_time <= $%d", f.To)
	}
	wh := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT batch_id) FROM tbl_dns_change_logs WHERE `+wh, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	q := fmt.Sprintf(`
SELECT l.batch_id, MIN(l.source), MIN(l.actor), MIN(l.ticket_id), MIN(l.reason), COUNT(1), (array_agg(l.summary ORDER BY l.seq))[1], MAX(l.created_time)
FROM tbl_dns_change_logs l
WHERE l.batch_id IN (SELECT DISTINCT batch_id FROM tbl_dns_change_logs WHERE %s)
GROUP BY l.batch_id
ORDER BY MAX(l.created_time) DESC, l.batch_id
LIMIT $%d OFFSET $%d`, wh, len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ChangeBatch{}
	for rows.Next() {
		var b ChangeBatch
		if err := rows.Scan(&b.BatchID, &b.Source, &b.Actor, &b.TicketID, &b.Reason, &b.Count, &b.Summary, &b.CreatedTime); err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		var by string
		_ = s.db.QueryRowContext(ctx, `SELECT batch_id FROM tbl_dns_change_logs WHERE source=$1 AND reason=$2 LIMIT 1`,
			SourceRollback, rollbackReason(out[i].BatchID)).Scan(&by)
		out[i].RolledBackBy = by
	}
	return out, total, nil
}

// GetChangeBatch 一个批次内的全部变更，按执行顺序。
func (s *Store) GetChangeBatch(ctx context.Context, batchID string) ([]ChangeLog, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, batch_id, seq, source, actor, ticket_id, reason, action, object_type, object_id, zone_id, summary,
  COALESCE(before_json::text,''), COALESCE(after_json::text,''), created_time
FROM tbl_dns_change_logs WHERE batch_id=$1 ORDER BY seq`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeLog{}
	for rows.Next() {
		var l ChangeLog
		var b, a string
		if err := rows.Scan(&l.ID, &l.BatchID, &l.Seq, &l.Source, &l.Actor, &l.TicketID, &l.Reason, &l.Action, &l.ObjectType,
			&l.ObjectID, &l.ZoneID, &l.Summary, &b, &a, &l.CreatedTime); err != nil {
			return nil, err
		}
		if b != "" {
			l.Before = json.RawMessage(b)
		}
		if a != "" {
			l.After = json.RawMessage(a)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func rollbackReason(batchID string) string {
	return "回滚批次 " + batchID
}

// RollbackOps 生成撤销某批次的逆操作：倒序执行，并以该批次的修改后值作为期望值。
func RollbackOps(logs []ChangeLog) ([]Op, error) {
	if len(logs) == 0 {
		return nil, fmt.Errorf("批次不存在")
	}
	ops := make([]Op, 0, len(logs))
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		op := Op{Object: l.ObjectType, ID: l.ObjectID}
		switch l.Action {
		case ActionCreate:
			op.Action = ActionDelete
			op.Expect = l.After
		case ActionDelete:
			op.Action = ActionCreate
			op.After = l.Before
			op.Expect = json.RawMessage("null")
		case ActionUpdate:
			op.Action = ActionUpdate
			op.After = l.Before
			op.Expect = l.After
		default:
			return nil, fmt.Errorf("无法回滚操作 %s", l.Action)
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// Rollback 撤销一个批次。期间若对象已被改动，返回 ConflictError，不做任何修改。
// meta 的 Source 与 Reason 由本函数填写。
func (s *Store) Rollback(ctx context.Context, batchID string, meta OpMeta) (*ApplyResult, error) {
	logs, err := s.GetChangeBatch(ctx, batchID)
	if err != nil {
		return nil, err
	}
	var done string
	_ = s.db.QueryRowContext(ctx, `SELECT batch_id FROM tbl_dns_change_logs WHERE source=$1 AND reason=$2 LIMIT 1`,
		SourceRollback, rollbackReason(batchID)).Scan(&done)
	if done != "" {
		return nil, fmt.Errorf("该批次已被批次 %s 回滚", done)
	}
	ops, err := RollbackOps(logs)
	if err != nil {
		return nil, err
	}
	meta.Source, meta.Reason = SourceRollback, rollbackReason(batchID)
	return s.ApplyOps(ctx, ops, meta)
}
