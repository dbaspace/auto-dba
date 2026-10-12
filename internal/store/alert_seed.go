package store

import (
	"log"
	"strings"
)

// DefaultAlertRules 与 dbmonitor 主机 / MySQL / PG 面板同源指标的基线规则。
// Expr 为模板，含 __SCOPE__；默认 scope=global，生产环境建议改为实例/集群/标签。
// 多档规则用 MetricExpr + Tiers（Prom 查指标，Ruler 取最高命中档）。
func DefaultAlertRules() []AlertRule {
	global := DefaultAlertScope()
	notify := DefaultAlertNotifyPolicy()
	hostDiskMetric := `(node_filesystem_size_bytes{mountpoint="/",fstype!="rootfs",__SCOPE__} - node_filesystem_avail_bytes{mountpoint="/",fstype!="rootfs",__SCOPE__}) / clamp_min(node_filesystem_size_bytes{mountpoint="/",fstype!="rootfs",__SCOPE__}, 1)`
	mysqlConnMetric := `mysql_global_status_threads_connected{__SCOPE__} / clamp_min(mysql_global_variables_max_connections{__SCOPE__}, 1)`
	return []AlertRule{
		{
			ID: "rule-seed-host-disk", Name: "主机磁盘使用率", Severity: AlertSeverityP2, ForDuration: "10m",
			MetricExpr: hostDiskMetric, Expr: hostDiskMetric,
			Tiers: []AlertTier{
				{Op: ">=", Value: 0.85, Severity: AlertSeverityP2, For: "10m"},
				{Op: ">=", Value: 0.95, Severity: AlertSeverityP1, For: "5m"},
			},
			Labels: map[string]string{"team": "DBA", "service": "host", "alertname": "HostDisk"},
			Annotations: map[string]string{
				"summary": "主机根分区磁盘使用率过高", "threshold": "≥85% P2 / ≥95% P1",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-host-cpu-85", Name: "主机 CPU 使用率过高", Severity: AlertSeverityP2, ForDuration: "5m",
			Expr: `(1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",__SCOPE__}[5m]))) >= 0.85`,
			Labels: map[string]string{"team": "DBA", "service": "host", "alertname": "HostCPUHigh"},
			Annotations: map[string]string{
				"summary": "主机 CPU 使用率 ≥ 85%", "threshold": "≥ 85%",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-mysql-conn", Name: "MySQL 连接数使用率", Severity: AlertSeverityP1, ForDuration: "5m",
			MetricExpr: mysqlConnMetric, Expr: mysqlConnMetric,
			Tiers: []AlertTier{
				{Op: ">=", Value: 0.8, Severity: AlertSeverityP1, For: "5m"},
				{Op: ">=", Value: 0.9, Severity: AlertSeverityP0, For: "3m"},
			},
			Labels: map[string]string{"team": "DBA", "service": "mysql", "alertname": "MySQLConn"},
			Annotations: map[string]string{
				"summary": "MySQL 连接数 / max_connections 过高", "threshold": "≥80% P1 / ≥90% P0",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-mysql-running", Name: "MySQL 活动连接数过高", Severity: AlertSeverityP1, ForDuration: "5m",
			Expr: `mysql_global_status_threads_running{__SCOPE__} >= 64`,
			Labels: map[string]string{"team": "DBA", "service": "mysql", "alertname": "MySQLThreadsRunningHigh"},
			Annotations: map[string]string{
				"summary": "MySQL threads_running（活动连接）≥ 64", "threshold": "≥ 64",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-pg-conn-80", Name: "PostgreSQL 连接数使用率过高", Severity: AlertSeverityP1, ForDuration: "5m",
			Expr: `sum by (instance) (pg_stat_activity_count{__SCOPE__}) / clamp_min(max by (instance) (pg_settings_max_connections{__SCOPE__}), 1) >= 0.8`,
			Labels: map[string]string{"team": "DBA", "service": "postgres", "alertname": "PGConnHigh"},
			Annotations: map[string]string{
				"summary": "PostgreSQL 连接数 / max_connections ≥ 80%", "threshold": "≥ 80%",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		// 实例存储：单规则多档（对齐 Grafana；非主机 node_filesystem）
		{
			ID: "rule-seed-pg-storage", Name: "PostgreSQL 实例存储使用率", Severity: AlertSeverityP1, ForDuration: "5m",
			MetricExpr: `max by (d_b_instance_name) (qce_postgres_storagerate_max{__SCOPE__}) / 100`,
			Expr:      `max by (d_b_instance_name) (qce_postgres_storagerate_max{__SCOPE__}) / 100`,
			Tiers: []AlertTier{
				{Op: ">=", Value: 0.8, Severity: AlertSeverityP1, For: "5m"},
				{Op: ">=", Value: 0.9, Severity: AlertSeverityP0, For: "3m"},
			},
			Labels: map[string]string{"team": "DBA", "service": "postgres", "alertname": "PGStorage"},
			Annotations: map[string]string{
				"summary": "腾讯云 PostgreSQL 实例存储使用率过高", "threshold": "≥80% P1 / ≥90% P0",
			},
			Scope:        AlertScope{Mode: AlertScopeModeGlobal, MatcherKey: AlertMatcherKeyDBInstanceName},
			NotifyPolicy: notify,
			Enabled:      true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-pg-cache-hit", Name: "PostgreSQL 缓存命中率过低", Severity: AlertSeverityP2, ForDuration: "10m",
			MetricExpr: `min by (instance) (sum by (instance) (rate(pg_stat_database_blks_hit{__SCOPE__,datname!~"template.*|postgres"}[5m])) / clamp_min(sum by (instance) (rate(pg_stat_database_blks_hit{__SCOPE__,datname!~"template.*|postgres"}[5m]) + rate(pg_stat_database_blks_read{__SCOPE__,datname!~"template.*|postgres"}[5m])), 1e-9))`,
			Expr:      `min by (instance) (sum by (instance) (rate(pg_stat_database_blks_hit{__SCOPE__,datname!~"template.*|postgres"}[5m])) / clamp_min(sum by (instance) (rate(pg_stat_database_blks_hit{__SCOPE__,datname!~"template.*|postgres"}[5m]) + rate(pg_stat_database_blks_read{__SCOPE__,datname!~"template.*|postgres"}[5m])), 1e-9))`,
			Tiers: []AlertTier{
				{Op: "<=", Value: 0.95, Severity: AlertSeverityP2, For: "10m"},
				{Op: "<=", Value: 0.90, Severity: AlertSeverityP1, For: "5m"},
			},
			Labels: map[string]string{"team": "DBA", "service": "postgres", "alertname": "PGCacheHitLow", "metric_key": "cache_hit_ratio"},
			Annotations: map[string]string{
				"summary": "PostgreSQL 缓冲命中率过低", "threshold": "≤95% P2 / ≤90% P1", "si_metric": "cache_hit_ratio",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-pg-repl-lag", Name: "PostgreSQL 复制延迟过高", Severity: AlertSeverityP1, ForDuration: "5m",
			// postgres_exporter 常见指标；现场无此 series 时规则不触发，可按 exporter 改写。
			MetricExpr: `max by (instance) (pg_replication_lag{__SCOPE__})`,
			Expr:      `max by (instance) (pg_replication_lag{__SCOPE__})`,
			Tiers: []AlertTier{
				{Op: ">=", Value: 30, Severity: AlertSeverityP1, For: "5m"},
				{Op: ">=", Value: 300, Severity: AlertSeverityP0, For: "3m"},
			},
			Labels: map[string]string{"team": "DBA", "service": "postgres", "alertname": "PGReplicationLag", "metric_key": "replication_lag"},
			Annotations: map[string]string{
				"summary": "PostgreSQL 主从复制延迟过高", "threshold": "≥30s P1 / ≥300s P0", "si_metric": "replication_lag",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
		{
			ID: "rule-seed-pg-deadlocks", Name: "PostgreSQL 死锁速率过高", Severity: AlertSeverityP2, ForDuration: "5m",
			Expr: `sum by (instance) (rate(pg_stat_database_deadlocks{__SCOPE__}[5m])) >= 0.01`,
			Labels: map[string]string{"team": "DBA", "service": "postgres", "alertname": "PGDeadlocks", "metric_key": "deadlocks_ps"},
			Annotations: map[string]string{
				"summary": "PostgreSQL 死锁速率 ≥ 0.01/s", "threshold": "≥ 0.01/s", "si_metric": "deadlocks_ps",
			},
			Scope: global, NotifyPolicy: notify, Enabled: true, CreatedBy: "system",
		},
	}
}

// AlertExprTemplate 预置 PromQL 样例（非白名单；选用后可改）。
type AlertExprTemplate struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Expr       string      `json:"expr"`
	MetricExpr string      `json:"metric_expr,omitempty"`
	Tiers      []AlertTier `json:"tiers,omitempty"`
	Service    string      `json:"service"`
	MatcherKey string      `json:"matcher_key"`
	Severity   string      `json:"severity"`
	For        string      `json:"for_duration"`
	Summary    string      `json:"summary"`
}

// ListAlertExprTemplates 从种子规则导出模板列表，供前端「选用模板」。
func ListAlertExprTemplates() []AlertExprTemplate {
	rules := DefaultAlertRules()
	out := make([]AlertExprTemplate, 0, len(rules))
	for _, r := range rules {
		mk := r.Scope.MatcherKey
		if mk == "" {
			mk = AlertMatcherKeyAuto
		}
		svc := ""
		if r.Labels != nil {
			svc = r.Labels["service"]
		}
		sum := ""
		if r.Annotations != nil {
			sum = r.Annotations["summary"]
		}
		out = append(out, AlertExprTemplate{
			ID: r.ID, Name: r.Name, Expr: r.Expr, MetricExpr: r.MetricExpr, Tiers: r.Tiers,
			Service: svc, MatcherKey: mk,
			Severity: r.Severity, For: r.ForDuration, Summary: sum,
		})
	}
	return out
}

var obsoleteAlertSeedIDs = []string{
	"rule-seed-pg-storage-80",
	"rule-seed-pg-storage-90",
	"rule-seed-host-disk-85",
	"rule-seed-host-disk-95",
	"rule-seed-mysql-conn-80",
	"rule-seed-mysql-conn-90",
}

// EnsureDefaultAlertRules 幂等写入默认规则：已存在的 ID 跳过内容覆盖（不改用户阈值）；
// 若系统种子仍无 __SCOPE__，仅升级模板与编译字段以便作用域绑定。
// 系统种子的旧「过高/危急」拆分规则会被移除，改由单指标多档承接。
func (d *DB) EnsureDefaultAlertRules() error {
	n := 0
	upgraded := 0
	for _, base := range DefaultAlertRules() {
		exist, err := d.GetAlertRule(base.ID)
		if err != nil {
			return err
		}
		if exist == nil {
			r := base
			if strings.TrimSpace(r.TenantID) == "" {
				r.TenantID = DefaultTenantID
			}
			if err := d.CreateAlertRule(&r); err != nil {
				return err
			}
			n++
			continue
		}
		// 系统种子：补齐多档 / 作用域模板，不覆盖用户自建规则
		if exist.CreatedBy == "system" {
			need := false
			if !strings.Contains(exist.Expr, AlertScopeToken) && !strings.Contains(exist.MetricExpr, AlertScopeToken) {
				exist.Expr = base.Expr
				exist.MetricExpr = base.MetricExpr
				need = true
			}
			if len(base.Tiers) > 0 && len(exist.Tiers) == 0 {
				exist.Tiers = base.Tiers
				exist.MetricExpr = base.MetricExpr
				exist.Expr = base.Expr
				exist.Annotations = base.Annotations
				exist.NotifyPolicy = base.NotifyPolicy
				need = true
			}
			if exist.Scope.Mode == "" {
				exist.Scope = DefaultAlertScope()
				need = true
			}
			if need {
				if err := d.SaveAlertRule(exist); err != nil {
					return err
				}
				upgraded++
				continue
			}
		}
		if exist.CompiledExpr == "" {
			_ = d.CompileAlertRuleScope(exist)
			_ = d.saveAlertRuleCompiled(exist)
		}
	}
	removed := 0
	for _, id := range obsoleteAlertSeedIDs {
		exist, err := d.GetAlertRule(id)
		if err != nil {
			return err
		}
		if exist != nil && exist.CreatedBy == "system" {
			if err := d.DeleteAlertRule(id); err != nil {
				return err
			}
			removed++
		}
	}
	if n > 0 || upgraded > 0 || removed > 0 {
		log.Printf("seed: default alert rules ensured (+%d upgraded=%d removed_obsolete=%d)", n, upgraded, removed)
	}
	return nil
}
