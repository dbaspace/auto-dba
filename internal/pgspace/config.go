package pgspace

import (
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

const (
	pgSpaceAppName = "auto_dba_space_collector"

	pgSpaceEnabledKey           = "pg_space_enabled"
	pgSpaceDailyCronKey         = "pg_space_daily_cron"
	pgSpaceDefaultDailyCron     = "0 0 3 * * *"
	pgSpaceRetentionKey         = "pg_space_retention_days"
	pgSpaceDefaultRetentionDays = 90
	pgSpaceTopNKey              = "pg_space_top_n"
	pgSpaceDefaultTopN          = 50
	pgSpaceScanAllDatabasesKey  = "pg_space_scan_all_databases"
	pgSpaceStatementTimeoutKey  = "pg_space_statement_timeout_ms"
	pgSpaceDefaultStatementMs   = 8000
	pgSpaceMaxDatabasesKey      = "pg_space_max_databases"
	pgSpaceDefaultMaxDatabases  = 50
	pgSpaceMinDbSizeBytesKey    = "pg_space_min_db_size_bytes"
	pgSpaceShardStripRegexKey   = "pg_space_shard_strip_regex"
	pgSpaceDefaultShardRegex    = `_[0-9]+$`
	pgSpaceDefaultCapacityKey   = "pg_space_default_capacity_bytes"

	pgSpaceAdviceSevNone     = "none"
	pgSpaceAdviceSevWarning  = "warning"
	pgSpaceAdviceSevCritical = "critical"
	pgSpaceAdviceGrowthTopN  = 10

	defaultDB = "postgres"
)

func (s *Service) enabled() bool {
	v, err := s.Store.GetGlobalArg(pgSpaceEnabledKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "no":
		return false
	default:
		return true
	}
}

func (s *Service) scanAllDatabases() bool {
	v, err := s.Store.GetGlobalArg(pgSpaceScanAllDatabasesKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "no":
		return false
	default:
		return true
	}
}

func (s *Service) topN() int {
	return s.intArg(pgSpaceTopNKey, pgSpaceDefaultTopN)
}

func (s *Service) statementTimeoutMs() int {
	return s.intArg(pgSpaceStatementTimeoutKey, pgSpaceDefaultStatementMs)
}

func (s *Service) maxDatabases() int {
	return s.intArg(pgSpaceMaxDatabasesKey, pgSpaceDefaultMaxDatabases)
}

func (s *Service) minDbSizeBytes() int64 {
	return int64(s.intArg(pgSpaceMinDbSizeBytesKey, 0))
}

func (s *Service) shardRegex() string {
	v, err := s.Store.GetGlobalArg(pgSpaceShardStripRegexKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return pgSpaceDefaultShardRegex
	}
	return strings.TrimSpace(v)
}

func (s *Service) dailyCronSpec() string {
	v, err := s.Store.GetGlobalArg(pgSpaceDailyCronKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return pgSpaceDefaultDailyCron
	}
	return strings.TrimSpace(v)
}

func (s *Service) retentionDays() int {
	return s.intArg(pgSpaceRetentionKey, pgSpaceDefaultRetentionDays)
}

func (s *Service) defaultCapacityBytes() int64 {
	v, err := s.Store.GetGlobalArg(pgSpaceDefaultCapacityKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return 0
	}
	n, e := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if e != nil {
		return 0
	}
	return n
}

func (s *Service) intArg(key string, def int) int {
	v, err := s.Store.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	n, e := strconv.Atoi(strings.TrimSpace(v))
	if e != nil {
		return def
	}
	return n
}

// Service 表空间分析服务。
type Service struct {
	Store  *store.DB
	Secret string
}

// New 创建服务。
func New(db *store.DB, secret string) *Service {
	return &Service{Store: db, Secret: secret}
}
