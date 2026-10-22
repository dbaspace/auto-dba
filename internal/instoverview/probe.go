package instoverview

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/redisquery"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// Sample 是一次点采样，不是监控曲线的区间平均。
type Sample struct {
	Reachable   bool    `json:"reachable"`
	Partial     bool    `json:"partial,omitempty"`
	ErrorKind   string  `json:"error_kind,omitempty"`
	Error       string  `json:"error,omitempty"`
	Version     string  `json:"version,omitempty"`
	Role        string  `json:"role,omitempty"`
	UptimeSec   int64   `json:"uptime_sec,omitempty"`
	ConnUsed    int64   `json:"conn_used,omitempty"`
	ConnMax     int64   `json:"conn_max,omitempty"`
	MemUsed     int64   `json:"mem_used,omitempty"`
	MemMax      int64   `json:"mem_max,omitempty"`
	LagBytes    int64   `json:"lag_bytes,omitempty"`
	LagKnown    bool    `json:"lag_known,omitempty"`
	LagSeconds  int64   `json:"lag_seconds,omitempty"`
	LagSecKnown bool    `json:"lag_sec_known,omitempty"`
	HitRate     float64 `json:"hit_rate,omitempty"`
	HitKnown    bool    `json:"hit_known,omitempty"`
	// DataBytes 是当前账号可见的非模板库文件合计，不含 WAL。
	DataBytes        int64   `json:"data_bytes,omitempty"`
	DataKnown        bool    `json:"data_known,omitempty"`
	StorageRate      float64 `json:"storage_rate,omitempty"`
	StorageRateKnown bool    `json:"storage_rate_known,omitempty"`
	CollectedAt      string  `json:"collected_at,omitempty"`
}

// Classify 把连库失败分成连不上、认证失败、超时、权限不足。
func Classify(err error) (kind, msg string) {
	if err == nil {
		return "", ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(s, "timeout") || strings.Contains(s, "i/o timeout") || strings.Contains(s, "deadline"):
		return "timeout", "超时"
	case strings.Contains(s, "password") || strings.Contains(s, "authentication") || strings.Contains(s, "28p01") || strings.Contains(s, "wrongpass") || strings.Contains(s, "noauth") || strings.Contains(s, "invalid username"):
		return "auth", "认证失败"
	case strings.Contains(s, "permission denied") || strings.Contains(s, "42501") || strings.Contains(s, "noperm") || strings.Contains(s, "insufficient"):
		return "permission", "权限不足"
	default:
		return "connect", "连不上"
	}
}

// Probe 采集一台实例。timeout 由调用方放进 ctx。
func Probe(ctx context.Context, inst *store.Instance, secretKey string) Sample {
	now := time.Now().Format(time.RFC3339)
	if inst == nil {
		return Sample{ErrorKind: "connect", Error: "连不上", CollectedAt: now}
	}
	pass, err := secret.Decrypt(secretKey, inst.Password)
	if err != nil {
		return Sample{ErrorKind: "auth", Error: "认证失败", CollectedAt: now}
	}
	switch store.NormalizeDBType(inst.DBType) {
	case "postgres":
		s := probePostgres(ctx, inst, secretKey, pass)
		s.CollectedAt = now
		return s
	case "redis":
		s := probeRedis(ctx, inst, pass)
		s.CollectedAt = now
		return s
	default:
		return Sample{ErrorKind: "skip", Error: "暂不采集", CollectedAt: now}
	}
}

func probePostgres(ctx context.Context, inst *store.Instance, secretKey, pass string) Sample {
	_ = pass
	db, err := dbmig.OpenInstance(ctx, inst, secretKey, "")
	if err != nil {
		kind, msg := Classify(err)
		return Sample{ErrorKind: kind, Error: msg}
	}
	defer db.Close()
	s, err := scanPostgres(ctx, db, false)
	if err == nil {
		s.Reachable = true
		fillDataBytes(ctx, db, &s)
		return s
	}
	kind, msg := Classify(err)
	if kind != "permission" {
		return Sample{ErrorKind: kind, Error: msg}
	}
	s, err2 := scanPostgres(ctx, db, true)
	if err2 != nil {
		k2, m2 := Classify(err2)
		return Sample{ErrorKind: k2, Error: m2}
	}
	s.Reachable = true
	s.Partial = true
	s.ErrorKind = "permission"
	s.Error = msg
	fillDataBytes(ctx, db, &s)
	return s
}

func fillDataBytes(ctx context.Context, db *sql.DB, s *Sample) {
	if s == nil || db == nil {
		return
	}
	var n sql.NullFloat64
	err := db.QueryRowContext(ctx, `
SELECT COALESCE(sum(pg_database_size(d.datname)), 0)
FROM pg_database d
WHERE NOT d.datistemplate
  AND d.datallowconn
  AND has_database_privilege(current_user, d.datname, 'CONNECT')`).Scan(&n)
	if err != nil || !n.Valid {
		return
	}
	s.DataKnown = true
	s.DataBytes = int64(n.Float64)
}

func scanPostgres(ctx context.Context, db *sql.DB, basic bool) (Sample, error) {
	var s Sample
	var inRecovery bool
	if basic {
		err := db.QueryRowContext(ctx, `
SELECT current_setting('server_version'),
       pg_is_in_recovery(),
       EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))::bigint`).Scan(&s.Version, &inRecovery, &s.UptimeSec)
		if err != nil {
			return Sample{}, err
		}
		s.Role = pgRole(inRecovery)
		return s, nil
	}
	var lag sql.NullFloat64
	err := db.QueryRowContext(ctx, `
SELECT current_setting('server_version'),
       pg_is_in_recovery(),
       EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))::bigint,
       current_setting('max_connections')::int,
       (SELECT count(*)::int FROM pg_stat_activity),
       CASE WHEN pg_is_in_recovery()
            THEN pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn())
            ELSE NULL END`).Scan(&s.Version, &inRecovery, &s.UptimeSec, &s.ConnMax, &s.ConnUsed, &lag)
	if err != nil {
		return Sample{}, err
	}
	s.Role = pgRole(inRecovery)
	if inRecovery && lag.Valid {
		s.LagKnown = true
		s.LagBytes = int64(lag.Float64)
	}
	return s, nil
}

func pgRole(inRecovery bool) string {
	if inRecovery {
		return "replica"
	}
	return "primary"
}

func probeRedis(ctx context.Context, inst *store.Instance, pass string) Sample {
	info, err := redisquery.InfoSection(ctx, inst, redisquery.Options{Password: pass}, "")
	if err != nil {
		kind, msg := Classify(err)
		return Sample{ErrorKind: kind, Error: msg}
	}
	s := Sample{Reachable: true, Version: info["redis_version"]}
	s.UptimeSec = atoi64(info["uptime_in_seconds"])
	s.ConnUsed = atoi64(info["connected_clients"])
	s.ConnMax = atoi64(info["maxclients"])
	s.MemUsed = atoi64(info["used_memory"])
	s.MemMax = atoi64(info["maxmemory"])
	role := strings.ToLower(strings.TrimSpace(info["role"]))
	if role == "slave" || role == "replica" {
		s.Role = "replica"
		master := atoi64(info["master_repl_offset"])
		slave := atoi64(info["slave_repl_offset"])
		if master > 0 || slave > 0 {
			s.LagKnown = true
			s.LagBytes = master - slave
			if s.LagBytes < 0 {
				s.LagBytes = 0
			}
		}
		if v, ok := info["master_last_io_seconds_ago"]; ok && v != "" && !strings.EqualFold(v, "-1") {
			s.LagSecKnown = true
			s.LagSeconds = atoi64(v)
		}
	} else {
		s.Role = "primary"
	}
	hits := atoi64(info["keyspace_hits"])
	misses := atoi64(info["keyspace_misses"])
	if hits+misses > 0 {
		s.HitKnown = true
		s.HitRate = float64(hits) / float64(hits+misses)
	}
	return s
}

func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func Unsupported(dbType string) bool {
	switch store.NormalizeDBType(dbType) {
	case "postgres", "redis":
		return false
	default:
		return true
	}
}

func SkipSample() Sample {
	return Sample{ErrorKind: "skip", Error: "暂不采集", CollectedAt: time.Now().Format(time.RFC3339)}
}
