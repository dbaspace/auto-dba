package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS zones (
  id TEXT PRIMARY KEY,
  origin TEXT NOT NULL,
  ns TEXT NOT NULL DEFAULT '',
  mbox TEXT NOT NULL DEFAULT '',
  serial BIGINT NOT NULL DEFAULT 1,
  refresh BIGINT NOT NULL DEFAULT 3600,
  retry BIGINT NOT NULL DEFAULT 600,
  expire BIGINT NOT NULL DEFAULT 86400,
  mininum BIGINT NOT NULL DEFAULT 300,
  ttl BIGINT NOT NULL DEFAULT 600,
  xfer BIGINT NOT NULL DEFAULT 0,
  bind BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'Active',
  created_time BIGINT NOT NULL DEFAULT 0,
  updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_zones_origin ON zones(origin);
CREATE TABLE IF NOT EXISTS parsing_ranges (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  network_list JSONB NOT NULL DEFAULT '[]',
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'Active',
  bind BOOLEAN NOT NULL DEFAULT FALSE,
  created_time BIGINT NOT NULL DEFAULT 0,
  updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS rrs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  data TEXT NOT NULL,
  aux TEXT NOT NULL DEFAULT '',
  ttl BIGINT NOT NULL DEFAULT 600,
  serial BIGINT NOT NULL DEFAULT 1,
  zone_id TEXT NOT NULL,
  parsing_range_id TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'Active',
  created_by TEXT NOT NULL DEFAULT '',
  last_update_by TEXT NOT NULL DEFAULT '',
  created_time BIGINT NOT NULL DEFAULT 0,
  updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_rrs_identity ON rrs(zone_id, parsing_range_id, name, type, data);
CREATE TABLE IF NOT EXISTS serials (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  serial BIGINT NOT NULL DEFAULT 1,
  parsing_range_id TEXT NOT NULL DEFAULT '',
  updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_serials_name ON serials(name);
CREATE TABLE IF NOT EXISTS coredns (
  id BIGSERIAL PRIMARY KEY,
  ip TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  updated_time BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  data_last_update_time BIGINT NOT NULL DEFAULT 0,
  parsing_range_ids JSONB NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS tbl_dns_change_logs (
  id TEXT PRIMARY KEY,
  batch_id TEXT NOT NULL,
  seq INT NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  ticket_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  zone_id TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  before_json JSONB,
  after_json JSONB,
  created_time BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_dns_change_logs_batch ON tbl_dns_change_logs(batch_id);
CREATE INDEX IF NOT EXISTS idx_dns_change_logs_object ON tbl_dns_change_logs(object_id);
CREATE INDEX IF NOT EXISTS idx_dns_change_logs_time ON tbl_dns_change_logs(created_time);
CREATE TABLE IF NOT EXISTS tbl_dns_zone_policies (
  zone_id TEXT PRIMARY KEY,
  require_approval BOOLEAN NOT NULL DEFAULT FALSE,
  editor_role_ids JSONB NOT NULL DEFAULT '[]',
  env TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  updated_time BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tbl_dns_ttl_holds (
  id TEXT PRIMARY KEY,
  record_id TEXT NOT NULL,
  orig_ttl BIGINT NOT NULL,
  lowered_ttl BIGINT NOT NULL,
  restore_at BIGINT NOT NULL,
  batch_id TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'holding',
  restored_time BIGINT NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  created_time BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_dns_ttl_holds_status ON tbl_dns_ttl_holds(status, restore_at);
`

// Store 连接独立解析库。
type Store struct {
	db       *sql.DB
	nodeUser string
	nodePass string
	nodeDB   string
}

func Open(dsn, nodeUser, nodePass, nodeDB string) (*Store, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("未配置 dns.dsn")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接解析库失败: %w", err)
	}
	s := &Store{db: db, nodeUser: strings.TrimSpace(nodeUser), nodePass: nodePass, nodeDB: strings.TrimSpace(nodeDB)}
	if err := s.ensure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensure(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("初始化解析库表失败: %w", err)
	}
	now := time.Now().Unix()
	for _, name := range []string{SerialUpdateZones, SerialSyncZone} {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO serials(id, name, serial, parsing_range_id, updated_time)
VALUES($1,$2,1,'',$3)
ON CONFLICT (name) DO NOTHING`, uuid.NewString(), name, now); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO parsing_ranges(id, name, network_list, description, status, bind, created_time, updated_time)
VALUES($1,$2,'[]'::jsonb,'hdns 缺省范围，不参与来源匹配',$3,false,$4,$4)
ON CONFLICT (id) DO NOTHING`, DefaultRangeID, DefaultRangeName, StatusActive, now); err != nil {
		return err
	}
	s.grantNode(ctx)
	return nil
}

func (s *Store) grantNode(ctx context.Context) {
	if s.nodeUser == "" || s.nodePass == "" || s.nodeDB == "" {
		return
	}
	user := quoteIdent(s.nodeUser)
	pass := strings.ReplaceAll(s.nodePass, "'", "''")
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN
    CREATE ROLE %s LOGIN PASSWORD '%s';
  ELSE
    ALTER ROLE %s LOGIN PASSWORD '%s';
  END IF;
END $$`, strings.ReplaceAll(s.nodeUser, "'", "''"), user, pass, user, pass))
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`GRANT CONNECT ON DATABASE %s TO %s`, quoteIdent(s.nodeDB), user))
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, user))
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`GRANT SELECT ON zones, rrs, parsing_ranges, serials TO %s`, user))
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE ON coredns TO %s`, user))
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf(`GRANT USAGE, SELECT ON SEQUENCE coredns_id_seq TO %s`, user))
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func (s *Store) ListZones(ctx context.Context) ([]Zone, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, origin, ns, mbox, serial, ttl, status, updated_time FROM zones ORDER BY origin`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Zone
	for rows.Next() {
		var z Zone
		if err := rows.Scan(&z.ID, &z.Origin, &z.NS, &z.Mbox, &z.Serial, &z.TTL, &z.Status, &z.UpdatedTime); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	if out == nil {
		out = []Zone{}
	}
	return out, rows.Err()
}

func (s *Store) ListRanges(ctx context.Context, includeDefault bool) ([]Range, error) {
	q := `SELECT id, name, network_list, description, status, updated_time FROM parsing_ranges`
	if !includeDefault {
		q += ` WHERE name <> '` + DefaultRangeName + `'`
	}
	q += ` ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Range
	for rows.Next() {
		var r Range
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Name, &raw, &r.Description, &r.Status, &r.UpdatedTime); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.NetworkList)
		if r.NetworkList == nil {
			r.NetworkList = []string{}
		}
		out = append(out, r)
	}
	if out == nil {
		out = []Range{}
	}
	return out, rows.Err()
}

type RecordFilter struct {
	ZoneID   string
	RangeID  string
	Type     string
	Status   string
	Name     string
	Data     string
	Page     int
	PageSize int
}

func (s *Store) ListRecords(ctx context.Context, f RecordFilter) ([]Record, int, error) {
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
	if f.ZoneID != "" {
		add("r.zone_id=$%d", f.ZoneID)
	}
	if f.RangeID != "" {
		add("r.parsing_range_id=$%d", f.RangeID)
	}
	if f.Type != "" {
		add("r.type=$%d", strings.ToUpper(f.Type))
	}
	if f.Status != "" {
		st := NormalizeStatus(f.Status)
		if st == "" {
			return nil, 0, fmt.Errorf("状态无效")
		}
		add("r.status=$%d", st)
	}
	if strings.TrimSpace(f.Name) != "" {
		add("r.name ILIKE $%d", "%"+strings.TrimSpace(f.Name)+"%")
	}
	if strings.TrimSpace(f.Data) != "" {
		add("r.data ILIKE $%d", "%"+strings.TrimSpace(f.Data)+"%")
	}
	wh := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM rrs r WHERE `+wh, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	q := fmt.Sprintf(`SELECT r.id, r.name, r.type, r.data, r.ttl, r.zone_id, z.origin, r.parsing_range_id, p.name, p.network_list, r.description, r.status, r.updated_time
FROM rrs r
JOIN zones z ON z.id = r.zone_id
JOIN parsing_ranges p ON p.id = r.parsing_range_id
WHERE %s
ORDER BY r.updated_time DESC
LIMIT $%d OFFSET $%d`, wh, len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	if out == nil {
		out = []Record{}
	}
	return out, total, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(sc rowScanner) (Record, error) {
	var r Record
	var raw []byte
	err := sc.Scan(&r.ID, &r.Name, &r.Type, &r.Data, &r.TTL, &r.ZoneID, &r.ZoneOrigin, &r.ParsingRangeID, &r.ParsingRangeName, &raw, &r.Description, &r.Status, &r.UpdatedTime)
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal(raw, &r.NetworkList)
	if r.Type == "MX" {
		if host, pref, ok := DecodeMX(r.Data); ok {
			r.MailHost = host
			r.Preference = pref
		}
	}
	return r, nil
}

// LastChangeTime 最近一次 serial 变化时间，即节点应当重新加载的时间点。
func (s *Store) LastChangeTime(ctx context.Context) (int64, error) {
	var t int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(updated_time),0) FROM serials`).Scan(&t)
	return t, err
}

// ListNodes 节点心跳与同步状态。节点数据加载时间早于最近变更且已超过 SyncLagSeconds 视为未同步。
func (s *Store) ListNodes(ctx context.Context, now int64) ([]Node, error) {
	changed, err := s.LastChangeTime(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ip, name, updated_time, data_last_update_time FROM coredns ORDER BY ip, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.IP, &n.Name, &n.UpdatedTime, &n.DataUpdatedTime); err != nil {
			return nil, err
		}
		n.Online = now-n.UpdatedTime <= NodeOfflineSeconds
		n.ChangeTime = changed
		n.Synced = n.DataUpdatedTime >= changed || now-changed <= SyncLagSeconds
		out = append(out, n)
	}
	if out == nil {
		out = []Node{}
	}
	return out, rows.Err()
}

// WaitNode 在超时内等到 coredns 表出现该主机（IP 或名称）。
func (s *Store) WaitNode(ctx context.Context, host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	host = strings.TrimSpace(host)
	for {
		var n int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM coredns WHERE ip=$1 OR name=$1`, host).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待心跳超时，coredns 表未出现 %s", host)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func mapDup(err error, msg string) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return fmt.Errorf("%s", msg)
	}
	return err
}
