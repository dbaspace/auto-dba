package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TTLHoldHolding  = "holding"
	TTLHoldRestored = "restored"
	TTLHoldSkipped  = "skipped"
	TTLHoldInvalid  = "invalid"

	DefaultLoweredTTL = 30
	MaxHoldMinutes    = 7 * 24 * 60
)

// TTLHoldSpec 预降参数。OrigTTL 为 记录 ID → 预降前 TTL。
type TTLHoldSpec struct {
	LoweredTTL int64
	RestoreAt  int64
	OrigTTL    map[string]int64
}

type TTLHold struct {
	ID           string `json:"id"`
	RecordID     string `json:"record_id"`
	RecordName   string `json:"record_name,omitempty"`
	RecordType   string `json:"record_type,omitempty"`
	ZoneOrigin   string `json:"zone_origin,omitempty"`
	OrigTTL      int64  `json:"orig_ttl"`
	LoweredTTL   int64  `json:"lowered_ttl"`
	RestoreAt    int64  `json:"restore_at"`
	BatchID      string `json:"batch_id"`
	Actor        string `json:"actor"`
	Status       string `json:"status"`
	RestoredTime int64  `json:"restored_time"`
	Message      string `json:"message"`
	CreatedTime  int64  `json:"created_time"`
}

func insertTTLHolds(ctx context.Context, tx *sql.Tx, meta OpMeta, batchID string, now int64) error {
	spec := meta.TTLHold
	for rid, orig := range spec.OrigTTL {
		if _, err := tx.ExecContext(ctx, `UPDATE tbl_dns_ttl_holds SET status=$2, message=$3, restored_time=$4 WHERE record_id=$1 AND status=$5`,
			rid, TTLHoldSkipped, "被新的预降替代", now, TTLHoldHolding); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tbl_dns_ttl_holds(id, record_id, orig_ttl, lowered_ttl, restore_at, batch_id, actor, status, restored_time, message, created_time)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,0,'',$9)`,
			uuid.NewString(), rid, orig, spec.LoweredTTL, spec.RestoreAt, batchID, meta.Actor, TTLHoldHolding, now); err != nil {
			return err
		}
	}
	return nil
}

// LowerTTL 把记录 TTL 降到 lowered，minutes 分钟后由定时任务恢复。
// 已在预降中的记录沿用最早记下的原 TTL。
func (s *Store) LowerTTL(ctx context.Context, recordIDs []string, lowered int64, minutes int, actor string) (*ApplyResult, error) {
	if len(recordIDs) == 0 {
		return nil, fmt.Errorf("请选择记录")
	}
	if lowered <= 0 {
		lowered = DefaultLoweredTTL
	}
	if _, err := NormalizeTTL(lowered); err != nil {
		return nil, err
	}
	if minutes <= 0 || minutes > MaxHoldMinutes {
		return nil, fmt.Errorf("恢复时间须在 1 分钟到 7 天之间")
	}
	spec := &TTLHoldSpec{LoweredTTL: lowered, RestoreAt: time.Now().Add(time.Duration(minutes) * time.Minute).Unix(), OrigTTL: map[string]int64{}}
	var ops []Op
	for _, id := range recordIDs {
		id = strings.TrimSpace(id)
		cur, err := getRecordSnap(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		if cur == nil {
			return nil, fmt.Errorf("记录 %s 不存在", id)
		}
		orig := cur.TTL
		var held int64
		err = s.db.QueryRowContext(ctx, `SELECT orig_ttl FROM tbl_dns_ttl_holds WHERE record_id=$1 AND status=$2 ORDER BY created_time LIMIT 1`, id, TTLHoldHolding).Scan(&held)
		if err == nil {
			orig = held
		} else if err != sql.ErrNoRows {
			return nil, err
		}
		if orig <= lowered {
			return nil, fmt.Errorf("记录 %s 当前 TTL %d 秒不高于预降值 %d 秒", cur.Name, orig, lowered)
		}
		spec.OrigTTL[id] = orig
		expect, _ := json.Marshal(cur)
		next := *cur
		next.TTL = lowered
		op := RecordOp(ActionUpdate, id, next)
		op.Expect = expect
		ops = append(ops, op)
	}
	return s.ApplyOps(ctx, ops, OpMeta{
		Actor: actor, Source: SourceTTL, Reason: fmt.Sprintf("预降 TTL 至 %d 秒，%d 分钟后恢复", lowered, minutes), TTLHold: spec,
	})
}

// RestoreTTL 恢复指定预降；记录 TTL 已被人改过（不等于预降值）时跳过，不覆盖人工修改。
func (s *Store) RestoreTTL(ctx context.Context, holdID, actor string) (string, error) {
	var h TTLHold
	err := s.db.QueryRowContext(ctx, `SELECT id, record_id, orig_ttl, lowered_ttl, status FROM tbl_dns_ttl_holds WHERE id=$1`, holdID).
		Scan(&h.ID, &h.RecordID, &h.OrigTTL, &h.LoweredTTL, &h.Status)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("预降记录不存在")
	}
	if err != nil {
		return "", err
	}
	if h.Status != TTLHoldHolding {
		return "", fmt.Errorf("该预降已结束")
	}
	now := time.Now().Unix()
	cur, err := getRecordSnap(ctx, s.db, h.RecordID)
	if err != nil {
		return "", err
	}
	finish := func(status, msg string) error {
		_, err := s.db.ExecContext(ctx, `UPDATE tbl_dns_ttl_holds SET status=$2, message=$3, restored_time=$4 WHERE id=$1`, h.ID, status, msg, now)
		return err
	}
	if cur == nil {
		return TTLHoldInvalid, finish(TTLHoldInvalid, "记录已删除")
	}
	if cur.TTL != h.LoweredTTL {
		msg := fmt.Sprintf("TTL 已被改为 %d 秒，未覆盖", cur.TTL)
		return TTLHoldSkipped, finish(TTLHoldSkipped, msg)
	}
	expect, _ := json.Marshal(cur)
	next := *cur
	next.TTL = h.OrigTTL
	op := RecordOp(ActionUpdate, h.RecordID, next)
	op.Expect = expect
	res, err := s.ApplyOps(ctx, []Op{op}, OpMeta{Actor: actor, Source: SourceTTL, Reason: fmt.Sprintf("恢复 TTL 至 %d 秒", h.OrigTTL)})
	if err != nil {
		_ = finish(TTLHoldHolding, "恢复失败："+err.Error())
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tbl_dns_ttl_holds SET status=$2, message=$3, restored_time=$4 WHERE id=$1`,
		h.ID, TTLHoldRestored, "批次 "+res.BatchID, now)
	return TTLHoldRestored, err
}

// RestoreDueTTL 恢复所有到期的预降，返回处理条数。
func (s *Store) RestoreDueTTL(ctx context.Context, now int64) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tbl_dns_ttl_holds WHERE status=$1 AND restore_at <= $2 ORDER BY restore_at`, TTLHoldHolding, now)
	if err != nil {
		return 0, err
	}
	var idsDue []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		idsDue = append(idsDue, id)
	}
	rows.Close()
	n := 0
	var firstErr error
	for _, id := range idsDue {
		if _, err := s.RestoreTTL(ctx, id, "system"); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// ListTTLHolds 列出预降记录。status 为空时列全部，最多 500 条。
func (s *Store) ListTTLHolds(ctx context.Context, status string) ([]TTLHold, error) {
	q := `SELECT h.id, h.record_id, COALESCE(r.name,''), COALESCE(r.type,''), COALESCE(z.origin,''), h.orig_ttl, h.lowered_ttl, h.restore_at,
  h.batch_id, h.actor, h.status, h.restored_time, h.message, h.created_time
FROM tbl_dns_ttl_holds h
LEFT JOIN rrs r ON r.id = h.record_id
LEFT JOIN zones z ON z.id = r.zone_id`
	args := []any{}
	if strings.TrimSpace(status) != "" {
		q += ` WHERE h.status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY h.created_time DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TTLHold{}
	for rows.Next() {
		var h TTLHold
		if err := rows.Scan(&h.ID, &h.RecordID, &h.RecordName, &h.RecordType, &h.ZoneOrigin, &h.OrigTTL, &h.LoweredTTL, &h.RestoreAt,
			&h.BatchID, &h.Actor, &h.Status, &h.RestoredTime, &h.Message, &h.CreatedTime); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
