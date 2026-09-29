package awr

import (
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

const (
	awrPromBaseURLKey         = "awr_prometheus_base_url"
	awrPromAuthTokenKey       = "awr_prometheus_auth_token"
	awrPromPartialResponseKey = "awr_prometheus_partial_response"
	awrPromDedupKey           = "awr_prometheus_dedup"
	awrPromDBLabelKey         = "awr_prom_instance_label"
	awrPromHostLabelKey       = "awr_prom_host_label"
	awrPromCloudLabelKey      = "awr_prom_cloud_label"

	awrDailyCronKey     = "awr_daily_cron"
	awrDefaultDailyCron = "0 30 1 * * *"
	awrEnabledKey       = "awr_enabled"
	awrHostMetricsKey   = "awr_host_metrics_enabled"
	awrCloudMetricsKey  = "awr_cloud_metrics_enabled"
	awrRetentionKey     = "awr_retention_days"
	awrDefaultRetention = 30
	awrPushEnabledKey   = "awr_push_enabled"
	awrPushReceiverKey  = "awr_push_receiver"

	awrTopSQLLimit       = 10
	awrSummaryMaxDomains = 100000
	awrMaxSeriesPoints   = 288
)

// Service AWR 报告服务。
type Service struct {
	Store  *store.DB
	Secret string
}

// New 创建服务。
func New(db *store.DB, secret string) *Service {
	return &Service{Store: db, Secret: secret}
}

func (s *Service) enabled() bool {
	v, err := s.Store.GetGlobalArg(awrEnabledKey)
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

func (s *Service) dailyCronSpec() string {
	v, err := s.Store.GetGlobalArg(awrDailyCronKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return awrDefaultDailyCron
	}
	return strings.TrimSpace(v)
}

func (s *Service) retentionDays() int {
	return s.intArg(awrRetentionKey, awrDefaultRetention)
}

func (s *Service) hostMetricsEnabled() bool {
	v, err := s.Store.GetGlobalArg(awrHostMetricsKey)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

// cloudMetricsEnabled 云监控资源补充；默认关闭（auto-dba 优先 self_pg）。
func (s *Service) cloudMetricsEnabled() bool {
	v, err := s.Store.GetGlobalArg(awrCloudMetricsKey)
	if err != nil || strings.TrimSpace(v) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

func (s *Service) pushEnabled() bool {
	v, err := s.Store.GetGlobalArg(awrPushEnabledKey)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

func (s *Service) boolArg(key string, def bool) bool {
	v, err := s.Store.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
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

func (s *Service) floatArg(key string, def float64) float64 {
	v, err := s.Store.GetGlobalArg(key)
	if err != nil {
		return def
	}
	if f, e := strconv.ParseFloat(strings.TrimSpace(v), 64); e == nil {
		return f
	}
	return def
}

func (s *Service) stringArg(key, def string) string {
	v, err := s.Store.GetGlobalArg(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}
