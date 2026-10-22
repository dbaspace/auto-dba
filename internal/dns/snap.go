package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// querier 由 *sql.DB 与 *sql.Tx 实现，变更与读取共用。
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ZoneSnap 解析域在变更日志中的形态。
type ZoneSnap struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
	Status string `json:"status"`
}

// RangeSnap 解析范围在变更日志中的形态。
type RangeSnap struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	NetworkList []string `json:"network_list"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
}

// RecordSnap 记录在变更日志中的形态。Data 为库内原文（MX 为 邮件主机|优先级）。
// MailHost、Preference 仅作输入，写入前会折算进 Data 并清空。
type RecordSnap struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Data        string `json:"data"`
	TTL         int64  `json:"ttl"`
	ZoneID      string `json:"zone_id"`
	RangeID     string `json:"parsing_range_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	MailHost    string `json:"mail_host,omitempty"`
	Preference  int    `json:"preference,omitempty"`
}

func getZoneSnap(ctx context.Context, q querier, id string) (*ZoneSnap, error) {
	var z ZoneSnap
	err := q.QueryRowContext(ctx, `SELECT id, origin, status FROM zones WHERE id=$1`, id).Scan(&z.ID, &z.Origin, &z.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &z, nil
}

func getRangeSnap(ctx context.Context, q querier, id string) (*RangeSnap, error) {
	var r RangeSnap
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT id, name, network_list, description, status FROM parsing_ranges WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &raw, &r.Description, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &r.NetworkList)
	if r.NetworkList == nil {
		r.NetworkList = []string{}
	}
	return &r, nil
}

func getRecordSnap(ctx context.Context, q querier, id string) (*RecordSnap, error) {
	var r RecordSnap
	err := q.QueryRowContext(ctx, `SELECT id, name, type, data, ttl, zone_id, parsing_range_id, description, status FROM rrs WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Type, &r.Data, &r.TTL, &r.ZoneID, &r.RangeID, &r.Description, &r.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Snapshot 校验、模拟解析与体检使用的全量数据。Records 只含 Active。
type Snapshot struct {
	Zones   []Zone
	Ranges  []Range
	Records []RecordSnap
}

func loadSnapshot(ctx context.Context, q querier, activeOnly bool) (*Snapshot, error) {
	out := &Snapshot{}
	zr, err := q.QueryContext(ctx, `SELECT id, origin, ns, mbox, serial, ttl, status, updated_time FROM zones ORDER BY origin`)
	if err != nil {
		return nil, err
	}
	for zr.Next() {
		var z Zone
		if err := zr.Scan(&z.ID, &z.Origin, &z.NS, &z.Mbox, &z.Serial, &z.TTL, &z.Status, &z.UpdatedTime); err != nil {
			zr.Close()
			return nil, err
		}
		out.Zones = append(out.Zones, z)
	}
	zr.Close()
	rr, err := q.QueryContext(ctx, `SELECT id, name, network_list, description, status, updated_time FROM parsing_ranges ORDER BY created_time, id`)
	if err != nil {
		return nil, err
	}
	for rr.Next() {
		var r Range
		var raw []byte
		if err := rr.Scan(&r.ID, &r.Name, &raw, &r.Description, &r.Status, &r.UpdatedTime); err != nil {
			rr.Close()
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.NetworkList)
		out.Ranges = append(out.Ranges, r)
	}
	rr.Close()
	qs := `SELECT id, name, type, data, ttl, zone_id, parsing_range_id, description, status FROM rrs`
	args := []any{}
	if activeOnly {
		qs += ` WHERE status=$1`
		args = append(args, StatusActive)
	}
	recs, err := q.QueryContext(ctx, qs, args...)
	if err != nil {
		return nil, err
	}
	defer recs.Close()
	for recs.Next() {
		var r RecordSnap
		if err := recs.Scan(&r.ID, &r.Name, &r.Type, &r.Data, &r.TTL, &r.ZoneID, &r.RangeID, &r.Description, &r.Status); err != nil {
			return nil, err
		}
		out.Records = append(out.Records, r)
	}
	return out, recs.Err()
}

// Snapshot 读取当前全量数据。
func (s *Store) Snapshot(ctx context.Context, activeOnly bool) (*Snapshot, error) {
	return loadSnapshot(ctx, s.db, activeOnly)
}

// FQDN 记录的完整域名（不带结尾点，小写）。
func FQDN(name, origin string) string {
	o := strings.TrimSuffix(strings.ToLower(origin), ".")
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "@" {
		return o
	}
	return n + "." + o
}

// normName 统一目标域名写法：小写、去结尾点。
func normName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}
