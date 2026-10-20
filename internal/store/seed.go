package store

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func (d *DB) seed() error {
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_users`).Scan(&n); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if n == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := d.Exec(
			`INSERT INTO tbl_users(id, username, display_name, role, role_id, password_hash, created_at) VALUES(?,?,?,?,?,?,?)`,
			uuid.NewString(), "admin", "管理员", "admin", "", string(hash), now,
		); err != nil {
			return err
		}
		log.Printf("seed: default user admin/admin123 created")
	}

	mysqlVars, err := defaultGoInceptionVariablesJSON()
	if err != nil {
		return err
	}
	reviewRules, err := defaultTicketReviewRulesJSON()
	if err != nil {
		return err
	}
	defaults := []GlobalArg{
		{Key: "sync_approval_enabled", Value: "true", Description: "CDC 创建/删除是否走审批"},
		{Key: "sync_default_approver", Value: "admin", Description: "建库与同步默认审批人"},
		{Key: ArgIssueGlobalApproverWhitelist, Value: "", Description: "工单/迁移超级审核白名单：登录名，逗号或换行分隔；可不等于 assigned_to 也能看待办并审批"},
		{Key: "sync_sr_routine_load_desired_concurrent", Value: "3", Description: "CDC Routine Load 默认并发"},
		{Key: "sync_kafka_topic_partitions", Value: "3", Description: "CDC Kafka Topic 默认分区数"},
		{Key: "sync_kafka_topic_replication_factor", Value: "1", Description: "CDC Kafka Topic 默认副本数"},
		{Key: "sync_sr_auto_buckets", Value: "10", Description: "CDC StarRocks 自动建表默认分桶数"},
		{Key: "sync_sr_replication_num", Value: "1", Description: "CDC StarRocks 自动建表默认副本数"},
		{Key: "sync_sr_dynamic_partition_defaults", Value: `{"dynamic_partition.enable":"true","dynamic_partition.time_unit":"DAY","dynamic_partition.start":"-3","dynamic_partition.end":"3","dynamic_partition.prefix":"p"}`, Description: "CDC StarRocks 动态分区 PROPERTIES 默认值（JSON）"},
		{Key: "sync_shard_fanout_on_approve", Value: "true", Description: "分库任务审批后是否拆子任务"},
		{Key: "ticket_exclude_cdc_when_empty", Value: "true", Description: "工单列表无筛选时排除 cdc_sync"},
		{Key: "ddl_forbid_drop_database", Value: "true", Description: "DDL 禁止 DROP DATABASE"},
		{Key: ArgTicketDDLLongStmtTimeoutMinutes, Value: "360", Description: "DDL 工单长耗时语句（CREATE INDEX CONCURRENTLY 等）单条执行超时（分钟）"},
		{Key: "dml_require_where", Value: "true", Description: "UPDATE/DELETE 必须带 WHERE"},
		{Key: "login_idle_timeout_min", Value: "30", Description: "登录闲置超时（分钟），无操作自动退出；0 表示关闭"},
		{Key: ArgQueryDefaultLimit, Value: "1000", Description: "数据库查询默认条数限制：SQL 无 limit 时自动追加"},
		{Key: ArgQueryMaxLimit, Value: "10000", Description: "数据库查询最大条数限制：用户 limit 不得超过此值"},
		{Key: "query_data_mask_columns", Value: "", Description: "数据库查询结果脱敏列名：逗号分隔或 JSON 数组，命中列值显示为 ***"},
		{Key: ArgUserWatermarkEnabled, Value: "false", Description: "是否开启用户水印：true/false；开启后页面展示当前用户标识水印"},
		{Key: "migration_approval_enabled", Value: "true", Description: "数据迁移是否走审批"},
		{Key: "migration_approver_user_ids", Value: "admin", Description: "数据迁移审核人登录名，逗号分隔"},
		{Key: ArgMigrationOSSEndpoint, Value: "", Description: "迁移 OSS 访问地址"},
		{Key: ArgMigrationOSSBucket, Value: "", Description: "迁移 OSS 存储桶名称"},
		{Key: ArgMigrationOSSPrefix, Value: "migration", Description: "迁移 OSS 文件路径前缀"},
		{Key: ArgMigrationOSSAccessKey, Value: "", Description: "迁移 OSS AccessKey ID"},
		{Key: ArgMigrationOSSSecretKey, Value: "", Description: "迁移 OSS AccessKey Secret"},
		{Key: ArgMigrationOSSRetentionDays, Value: "7", Description: "迁移 OSS 临时文件保留天数"},
		{Key: ArgMigrationOSSChunkSizeMB, Value: "100", Description: "迁移 OSS 单文件分片大小(MB)"},
		{Key: ArgMigrationMaxConcurrentTasks, Value: "3", Description: "迁移最大并发任务数"},
		{Key: ArgMigrationTableParallelism, Value: "4", Description: "迁移默认多表并行度"},
		{Key: ArgMigrationBatchSize, Value: "10000", Description: "迁移默认批次大小"},
		{Key: ArgMigrationSingleTableTimeout, Value: "30", Description: "单表迁移超时(分钟)"},
		{Key: ArgMigrationDefaultSpeedLimit, Value: "0", Description: "迁移默认限速(行/秒)，0 表示不限速"},
		{Key: ArgMigrationScheduleTolerance, Value: "10", Description: "定时触发容忍窗口(分钟)"},
		{Key: "ticket_instance_types", Value: `[{"value":"mysql","label":"MySQL"},{"value":"postgres","label":"PostgreSQL"},{"value":"tidb","label":"TiDB"},{"value":"cockroachdb","label":"CockroachDB"},{"value":"oceanbase","label":"OceanBase"},{"value":"cassandra","label":"Cassandra"},{"value":"oracle","label":"Oracle"},{"value":"elasticsearch","label":"Elasticsearch"},{"value":"mongodb","label":"MongoDB"},{"value":"sqlserver","label":"SqlServer"},{"value":"starrocks","label":"StarRocks"},{"value":"clickhouse","label":"ClickHouse"},{"value":"kafka","label":"Kafka"},{"value":"redis","label":"Redis"}]`, Description: "实例可选类型：JSON [{value,label}]，仅 value 必填"},
		{Key: GlobalArgGoInceptionVariables, Value: mysqlVars, Description: descGoInceptionVariables},
		{Key: GlobalArgTicketReviewRules, Value: reviewRules, Description: descTicketReviewRules},
		{Key: "awr_prometheus_base_url", Value: "", Description: "Prometheus 地址（必填，为空则不生成报告）"},
		{Key: "awr_prometheus_auth_token", Value: "", Description: "可选鉴权 token"},
		{Key: "awr_prom_instance_label", Value: "id", Description: "库指标关联标签名"},
		{Key: "awr_prom_host_label", Value: "instance", Description: "主机指标关联标签名"},
		{Key: "awr_enabled", Value: "true", Description: "AWR 总开关"},
		{Key: "awr_daily_cron", Value: "0 30 1 * * *", Description: "AWR 每日生成 cron（6 段含秒）"},
		{Key: "awr_host_metrics_enabled", Value: "false", Description: "主机(node_exporter)指标开关"},
		{Key: "awr_cloud_metrics_enabled", Value: "false", Description: "云监控(qce)资源补充开关（默认关闭）"},
		{Key: "awr_retention_days", Value: "30", Description: "AWR 报告保留天数"},
		{Key: "awr_push_enabled", Value: "false", Description: "AWR 每日推送开关"},
		{Key: "awr_push_receiver", Value: "", Description: "AWR 推送接收人 username（逗号分隔）"},
		{Key: "pg_space_enabled", Value: "true", Description: "表空间定时采集开关"},
		{Key: "pg_space_daily_cron", Value: "0 0 3 * * *", Description: "表空间每日采集 Cron（6 段含秒）"},
		{Key: "pg_space_retention_days", Value: "90", Description: "表空间快照保留天数"},
		{Key: "sql_insight_analysis_cron_enabled", Value: "false", Description: "智能分析定时开关"},
		{Key: "sql_insight_analysis_cron", Value: "0 0 3 * * *", Description: "智能分析每日 Cron（6 段含秒）"},
		{Key: "cmdb_sync_auto_apply", Value: "false", Description: "CMDB 外部同步是否自动入库（默认关，仅产出候选）"},
		{Key: "cmdb_webhook_url", Value: "", Description: "CMDB CI/关系变更 Webhook URL（空则关闭）"},
	}
	for _, a := range defaults {
		var exists int
		_ = d.QueryRow(`SELECT COUNT(1) FROM tbl_global_args WHERE key=?`, a.Key).Scan(&exists)
		if exists == 0 {
			if _, err := d.Exec(
				`INSERT INTO tbl_global_args(key, value, description, updated_at) VALUES(?,?,?,?)`,
				a.Key, a.Value, a.Description, now,
			); err != nil {
				return err
			}
		}
	}

	if err := d.EnsureMigrationSettings(); err != nil {
		return err
	}
	if err := d.EnsureFlashbackCloudSettings(); err != nil {
		return err
	}

	if err := d.ensureRegionEnvCatalog(now); err != nil {
		return err
	}

	var instN int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_instances`).Scan(&instN); err != nil {
		return err
	}
	if instN == 0 {
		id := uuid.NewString()
		if _, err := d.Exec(
			`INSERT INTO tbl_instances(id, name, domain_name, port, db_type, idc, region_id, env_id, host, username, password, approvers, description, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, "示例 PostgreSQL", "127.0.0.1", 5432, "postgres", "test", "r-default", "e-test", "127.0.0.1",
			"postgres", "", `["admin"]`, "本地种子实例，请按实际环境修改", now, now,
		); err != nil {
			return err
		}
		dbID := uuid.NewString()
		if _, err := d.Exec(
			`INSERT INTO tbl_databases(id, instance_id, db_name, approvers, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
			dbID, id, "appdb", `["admin"]`, now, now,
		); err != nil {
			return err
		}
		log.Printf("seed: sample instance 127.0.0.1:5432 / appdb with approver=admin")
	}
	return d.seedRBAC(now)
}

func (d *DB) seedRBAC(now string) error {
	adminRole, err := d.ensureRole("admin", "管理员", "系统管理员，拥有全部功能权限。", now)
	if err != nil {
		return err
	}
	opRole, err := d.ensureRole("operator", "运维", "运维人员，可管理业务配置，不含用户/角色/菜单等权限治理。", now)
	if err != nil {
		return err
	}
	if _, err := d.ensureRole("viewer", "只读", "只读人员，仅可查看已授权页面，不可新建、编辑或删除。", now); err != nil {
		return err
	}
	if _, err := d.ensureRole("developer", "开发者", "开发人员，业务功能权限与运维相当，不含用户/角色等权限治理。", now); err != nil {
		return err
	}
	if err := d.seedMenus(now); err != nil {
		return err
	}
	if err := d.ensureDashboardMenu(now); err != nil {
		return err
	}
	if err := d.ensureDataQueryMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureRedisInstanceType(); err != nil {
		return err
	}
	if err := d.ensureQueryInstanceTypes(); err != nil {
		return err
	}
	if err := d.ensureHostInstanceType(); err != nil {
		return err
	}
	if err := d.ensureDqlMenu(now); err != nil {
		return err
	}
	if err := d.ensureRedisTicketMenu(now); err != nil {
		return err
	}
	if err := d.ensureResourceApplyMenu(now); err != nil {
		return err
	}
	if err := d.ensureSchemaCompareMenu(now); err != nil {
		return err
	}
	if err := d.ensureFlashbackMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureArchiveMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureBackupMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureK8sMenus(now); err != nil {
		return err
	}
	if err := d.ensureRegionEnvMenus(now); err != nil {
		return err
	}
	if err := d.ensureMonitorTagMenu(now); err != nil {
		return err
	}
	if err := d.EnsureDefaultMonitorLabelTags(); err != nil {
		return err
	}
	if err := d.ensureConnectorMenu(now); err != nil {
		return err
	}
	if err := d.ensureMonitorOpsMenus(now); err != nil {
		return err
	}
	if err := d.ensureConfigCenterGroups(now); err != nil {
		return err
	}
	if err := d.ensurePlatformAgentMenu(now); err != nil {
		return err
	}
	if err := d.ensureMigrationOSSMenu(now); err != nil {
		return err
	}
	if err := d.ensureSrClusterMenu(now); err != nil {
		return err
	}
	if err := d.ensureDbMonitorMenu(now); err != nil {
		return err
	}
	if err := d.ensureSqlInsightActionMenus(now); err != nil {
		return err
	}
	if err := d.ensureAlertMenus(now); err != nil {
		return err
	}
	if err := d.ensureTodoMenus(now); err != nil {
		return err
	}
	if err := d.ensureTopLevelMenuSort(now); err != nil {
		return err
	}
	if err := d.ensureTenantMenus(now); err != nil {
		return err
	}
	if err := d.EnsureDefaultAlertRules(); err != nil {
		return err
	}
	if err := d.ensureDbConfigMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureAccessDomainsMenu(now); err != nil {
		return err
	}
	if err := d.ensureLoginLogsMenu(now); err != nil {
		return err
	}
	if err := d.ensureLoginSessionsMenu(now); err != nil {
		return err
	}
	if err := d.ensureScheduledJobsMenu(now); err != nil {
		return err
	}
	if err := d.ensureScheduledJobsActionMenus(now); err != nil {
		return err
	}
	if err := d.ensureQueryAuditsMenu(now); err != nil {
		return err
	}
	if err := d.ensureLdapMenu(now); err != nil {
		return err
	}
	if err := d.ensureUserPermissionMenus(now); err != nil {
		return err
	}
	if err := d.ensureTerminalMenu(now); err != nil {
		return err
	}
	if err := d.ensureHAMenu(now); err != nil {
		return err
	}
	if err := d.ensureCmdbMenuTree(now); err != nil {
		return err
	}
	if err := d.EnsureDefaultCmdbTagDefs(); err != nil {
		return err
	}
	if err := d.backfillMenuMeta(); err != nil {
		return err
	}
	menus, err := d.ListMenus()
	if err != nil {
		return err
	}
	var allIDs, opIDs []string
	denyOp := map[string]bool{"/config/menus": true, "/config/roles": true, "/config/users": true, "/config/args": true, "/config/login-logs": true, "/config/query-audits": true, "/config/ldap": true}
	denyOpID := map[string]bool{"m-rbac": true}
	for _, m := range menus {
		allIDs = append(allIDs, m.ID)
		if !denyOp[m.Path] && !denyOpID[m.ID] {
			opIDs = append(opIDs, m.ID)
		}
	}
	if ids, _ := d.ListRoleMenuIDs(adminRole.ID); len(ids) == 0 {
		if err := d.ReplaceRoleMenus(adminRole.ID, allIDs); err != nil {
			return err
		}
	}
	if ids, _ := d.ListRoleMenuIDs(opRole.ID); len(ids) == 0 {
		if err := d.ReplaceRoleMenus(opRole.ID, opIDs); err != nil {
			return err
		}
	}
	if _, err := d.Exec(`UPDATE tbl_users SET role_id=?, role=? WHERE username=? AND (role_id IS NULL OR role_id='')`,
		adminRole.ID, "admin", "admin"); err != nil {
		return err
	}
	return nil
}

func defaultMenuIcon(id, path string) string {
	switch id {
	case "m-dashboard":
		return "dashboard"
	case "m-data-query":
		return "search"
	case "m-query":
		return "search"
	case "m-query-kafka":
		return "share"
	case "m-query-redis":
		return "coin"
	case "m-tickets":
		return "tickets"
	case "m-tickets-list":
		return "list"
	case "m-tickets-ddl":
		return "edit"
	case "m-tickets-dml":
		return "document"
	case "m-tickets-dql":
		return "download"
	case "m-tickets-kafka":
		return "share"
	case "m-tickets-redis":
		return "coin"
	case "m-tickets-resource-apply":
		return "appstore"
	case "m-tickets-dcl":
		return "key"
	case "m-tickets-createdb":
		return "plus"
	case "m-sync":
		return "refresh"
	case "m-sync-slots":
		return "connection"
	case "m-sync-cdc":
		return "upload"
	case "m-sync-mig":
		return "sort"
	case "m-sync-schema-cmp":
		return "document"
	case "m-safe":
		return "lock"
	case "m-safe-fb":
		return "clock"
	case "m-safe-fb-dash":
		return "histogram"
	case "m-safe-fb-tasks":
		return "list"
	case "m-safe-fb-hist":
		return "clock"
	case "m-safe-fb-cloud":
		return "key"
	case "m-safe-arc":
		return "folder"
	case "m-safe-arc-dash":
		return "histogram"
	case "m-safe-arc-apply":
		return "document"
	case "m-safe-arc-tasks":
		return "list"
	case "m-safe-arc-runs":
		return "clock"
	case "m-safe-arc-mon":
		return "warning"
	case "m-safe-arc-set":
		return "setting"
	case "m-safe-arc-mask":
		return "lock"
	case "m-safe-arc-rpt":
		return "databoard"
	case "m-diag":
		return "monitor"
	case "m-diag-perf":
		return "search"
	case "m-diag-rdbms":
		return "coin"
	case "m-diag-dist":
		return "monitor"
	case "m-diag-nosql":
		return "databoard"
	case "m-diag-infra":
		return "connection"
	case "m-diag-sql":
		return "search"
	case "m-diag-dbmon":
		return "monitor"
	case "m-diag-dbmon-mysql":
		return "coin"
	case "m-diag-dbmon-redis":
		return "connection"
	case "m-diag-dbmon-starrocks":
		return "histogram"
	case "m-diag-dbmon-clickhouse":
		return "databoard"
	case "m-diag-dbmon-kafka":
		return "files"
	case "m-diag-dbmon-tidb":
		return "coin"
	case "m-diag-dbmon-cockroach":
		return "monitor"
	case "m-diag-dbmon-cassandra":
		return "databoard"
	case "m-diag-dbmon-oceanbase":
		return "histogram"
	case "m-diag-dbmon-host":
		return "monitor"
	case "m-diag-dbmon-mongodb":
		return "coin"
	case "m-diag-dbmon-oracle":
		return "databoard"
	case "m-diag-dbmon-es":
		return "search"
	case "m-diag-dbmon-sqlserver":
		return "monitor"
	case "m-diag-dbmon-debezium":
		return "connection"
	case "m-diag-dbmon-dolphin":
		return "histogram"
	case "m-diag-sess":
		return "cpu"
	case "m-plat":
		return "grid"
	case "m-plat-ml":
		return "files"
	case "m-plat-ds":
		return "histogram"
	case "m-plat-sr":
		return "coin"
	case "m-plat-term":
		return "monitor"
	case "m-plat-ha":
		return "safety"
	case "m-cmdb":
		return "coin"
	case "m-cmdb-assets":
		return "list"
	case "m-cmdb-topo":
		return "share"
	case "m-cmdb-tags":
		return "tag"
	case "m-cmdb-discover":
		return "search"
	case "m-cmdb-io":
		return "upload"
	case "m-cmdb-audit":
		return "audit"
	case "m-cmdb-sync":
		return "refresh"
	case "m-rbac":
		return "lock"
	case "m-cfg":
		return "setting"
	case "m-db-config":
		return "coin"
	case "m-cfg-inst":
		return "monitor"
	case "m-cfg-region":
		return "location"
	case "m-cfg-env":
		return "grid"
	case "m-cfg-tags":
		return "tag"
	case "m-cfg-base":
		return "setting"
	case "m-cfg-access":
		return "tag"
	case "m-cfg-obs":
		return "monitor"
	case "m-cfg-conn":
		return "connection"
	case "m-cfg-agent":
		return "connection"
	case "m-cfg-oss":
		return "upload"
	case "m-cfg-monitor":
		return "monitor"
	case "m-cfg-fe-admin":
		return "coin"
	case "m-cfg-ds-admin":
		return "histogram"
	case "m-cfg-scheduled-jobs":
		return "clock"
	case "m-cfg-db":
		return "coin"
	case "m-cfg-domains":
		return "share"
	case "m-cfg-accounts":
		return "user"
	case "m-cfg-review":
		return "document"
	case "m-cfg-args":
		return "setting"
	case "m-cfg-menus":
		return "menu"
	case "m-cfg-roles":
		return "team"
	case "m-cfg-users":
		return "user"
	case "m-cfg-login-logs":
		return "login"
	case "m-cfg-login-sessions":
		return "team"
	case "m-cfg-ldap":
		return "safety"
	case "m-cfg-query-audits":
		return "audit"
	}
	switch path {
	case "/ops/dashboard":
		return "dashboard"
	case "/ops/tickets":
		return "list"
	}
	return ""
}

func (d *DB) ensureRole(code, name, desc, now string) (*Role, error) {
	r, err := d.GetRoleByCode(code)
	if err != nil {
		return nil, err
	}
	if r != nil {
		return r, nil
	}
	r = &Role{ID: uuid.NewString(), Name: name, Code: code, Description: desc, CreatedAt: now}
	if err := d.CreateRole(r); err != nil {
		return nil, err
	}
	return r, nil
}

type seedMenu struct {
	ID, Parent, Title, Path, URN, Perm, Icon string
	Sort                                     int
}

func (d *DB) seedMenus(now string) error {
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_menus`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	items := []seedMenu{
		{"m-dashboard", "", "仪表盘", "/ops/dashboard", "um.sidebar.ops-dashboard", "查看仪表盘", "dashboard", 1},
		{"m-data-query", "", "数据查询", "", "um.sidebar.data-query", "查看数据查询", "search", 5},
		{"m-query", "m-data-query", "数据库", "/ops/query", "um.sidebar.ops-query", "查看数据库", "search", 1},
		{"m-query-kafka", "m-data-query", "Kafka 查询", "/ops/kafka-query", "um.sidebar.ops-query-kafka", "查看 Kafka", "share", 2},
		{"m-query-redis", "m-data-query", "Redis 查询", "/ops/redis-query", "um.sidebar.ops-query-redis", "查看 Redis", "coin", 3},
		{"m-tickets", "", "工单申请", "", "um.sidebar.tickets", "查看工单申请", "tickets", 10},
		{"m-tickets-list", "m-tickets", "工单列表", "/ops/tickets", "um.sidebar.ops-tickets", "查看工单列表", "list", 11},
		{"m-tickets-ddl", "m-tickets", "DDL 工单", "/ops/ddl", "um.sidebar.ops-ddl", "查看 DDL 工单", "edit", 12},
		{"m-tickets-dml", "m-tickets", "DML 工单", "/ops/dml", "um.sidebar.ops-dml", "查看 DML 工单", "document", 13},
		{"m-tickets-dql", "m-tickets", "DQL 工单", "/ops/dql", "um.sidebar.ops-dql", "查看数据导出", "download", 17},
		{"m-tickets-kafka", "m-tickets", "Kafka 工单", "/ops/kafka", "um.sidebar.ops-kafka", "查看 Kafka 工单", "share", 14},
		{"m-tickets-redis", "m-tickets", "Redis 工单", "/ops/redis", "um.sidebar.ops-redis", "查看 Redis 工单", "coin", 15},
		{"m-tickets-dcl", "m-tickets", "权限申请", "/ops/dcl", "um.sidebar.ops-dcl", "查看权限申请", "key", 16},
		{"m-tickets-resource-apply", "m-tickets", "平台资源申请", "/ops/resource-apply", "um.sidebar.ops-resource-apply", "查看平台资源申请", "appstore", 18},
		{"m-tickets-createdb", "m-tickets", "新增数据库", "/ops/createdb", "um.sidebar.ops-createdb", "查看新增数据库", "plus", 17},
		{"m-sync", "", "同步与迁移", "", "um.sidebar.sync", "查看同步与迁移", "refresh", 20},
		{"m-sync-slots", "m-sync", "同步列表", "/ops/slots", "um.sidebar.ops-slots", "查看同步列表", "connection", 21},
		{"m-sync-cdc", "m-sync", "CDC 同步任务", "/ops/cdc", "um.sidebar.ops-cdc", "查看 CDC 同步", "upload", 22},
		{"m-sync-mig", "m-sync", "数据迁移", "/ops/migration", "um.sidebar.ops-migration", "查看数据迁移", "sort", 23},
		{"m-sync-schema-cmp", "m-sync", "表结构对比", "/ops/migration/compare", "um.sidebar.ops-migration-compare", "查看表结构对比", "document", 24},
		{"m-safe", "", "数据安全", "", "um.sidebar.safe", "查看数据安全", "lock", 30},
		{"m-safe-fb", "m-safe", "数据闪回", "", "um.sidebar.ops-flashback", "查看数据闪回", "clock", 31},
		{"m-safe-fb-dash", "m-safe-fb", "仪表盘", "/ops/flashback/dashboard", "um.sidebar.ops-flashback-dashboard", "查看闪回仪表盘", "histogram", 31},
		{"m-safe-fb-tasks", "m-safe-fb", "闪回任务", "/ops/flashback/tasks", "um.sidebar.ops-flashback-tasks", "查看闪回任务", "list", 32},
		{"m-safe-fb-hist", "m-safe-fb", "历史记录", "/ops/flashback/history", "um.sidebar.ops-flashback-history", "查看闪回历史", "clock", 33},
		{"m-safe-fb-cloud", "m-safe-fb", "多云密钥", "/ops/flashback/cloud", "um.sidebar.ops-flashback-cloud", "查看闪回多云密钥", "key", 34},
		{"m-safe-arc", "m-safe", "归档管理", "", "um.sidebar.ops-archive", "查看归档管理", "folder", 32},
		{"m-safe-arc-dash", "m-safe-arc", "控制台", "/ops/archive/dashboard", "um.sidebar.ops-archive-dashboard", "查看归档控制台", "histogram", 33},
		{"m-safe-arc-apply", "m-safe-arc", "归档任务", "/ops/archive/apply", "um.sidebar.ops-archive-apply", "提交归档工单", "document", 33},
		{"m-safe-arc-tasks", "m-safe-arc", "任务管理", "/ops/archive/tasks", "um.sidebar.ops-archive-tasks", "查看归档任务", "list", 34},
		{"m-safe-arc-runs", "m-safe-arc", "执行记录", "/ops/archive/runs", "um.sidebar.ops-archive-runs", "查看归档执行记录", "clock", 35},
		{"m-safe-arc-mon", "m-safe-arc", "监控告警", "/ops/archive/monitor", "um.sidebar.ops-archive-monitor", "查看归档监控", "warning", 36},
		{"m-safe-arc-set", "m-safe-arc", "系统设置", "/ops/archive/settings", "um.sidebar.ops-archive-settings", "查看归档设置", "setting", 37},
		{"m-safe-arc-mask", "m-safe-arc", "敏感数据脱敏", "/ops/archive/masking", "um.sidebar.ops-archive-masking", "查看数据脱敏", "lock", 38},
		{"m-safe-arc-rpt", "m-safe-arc", "归档报表", "/ops/archive/report", "um.sidebar.ops-archive-report", "查看归档报表", "databoard", 39},
		{"m-diag", "", "诊断", "", "um.sidebar.diag", "查看诊断", "monitor", 40},
		{"m-diag-perf", "m-diag", "性能诊断", "", "um.sidebar.diag-perf", "查看性能诊断", "search", 10},
		{"m-diag-sql", "m-diag-perf", "SQL 洞察", "/ops/sql-insight", "um.sidebar.ops-sql-insight", "查看 SQL 洞察", "search", 1},
		{"m-diag-sess", "m-diag-perf", "会话管理", "/ops/sessions", "um.sidebar.ops-sessions", "查看会话管理", "cpu", 2},
		{"m-diag-mysql-slow", "m-diag-perf", "MySQL 慢 SQL", "/ops/mysql-slow", "um.sidebar.ops-mysql-slow", "查看 MySQL Top SQL digest", "histogram", 3},
		{"m-diag-rdbms", "m-diag", "关系型数据库", "", "um.sidebar.diag-rdbms", "查看关系型数据库监控", "coin", 20},
		{"m-diag-dbmon-mysql", "m-diag-rdbms", "MySQL监控", "/ops/db-monitor/mysql", "um.sidebar.ops-db-monitor-mysql", "查看 MySQL 监控", "coin", 1},
		{"m-diag-dbmon", "m-diag-rdbms", "Postgres监控", "/ops/db-monitor/postgres", "um.sidebar.ops-db-monitor-postgres", "查看 Postgres 监控", "monitor", 2},
		{"m-diag-dbmon-oracle", "m-diag-rdbms", "Oracle监控", "/ops/db-monitor/oracle", "um.sidebar.ops-db-monitor-oracle", "查看 Oracle 监控", "databoard", 3},
		{"m-diag-dbmon-sqlserver", "m-diag-rdbms", "SQLServer监控", "/ops/db-monitor/sqlserver", "um.sidebar.ops-db-monitor-sqlserver", "查看 SQLServer 监控", "monitor", 4},
		{"m-diag-dbmon-oceanbase", "m-diag-rdbms", "OceanBase监控", "/ops/db-monitor/oceanbase", "um.sidebar.ops-db-monitor-oceanbase", "查看 OceanBase 监控", "histogram", 5},
		{"m-diag-dist", "m-diag", "分布式 SQL", "", "um.sidebar.diag-dist", "查看分布式 SQL 监控", "monitor", 30},
		{"m-diag-dbmon-tidb", "m-diag-dist", "TiDB监控", "/ops/db-monitor/tidb", "um.sidebar.ops-db-monitor-tidb", "查看 TiDB 监控", "coin", 1},
		{"m-diag-dbmon-cockroach", "m-diag-dist", "CockroachDB监控", "/ops/db-monitor/cockroachdb", "um.sidebar.ops-db-monitor-cockroach", "查看 CockroachDB 监控", "monitor", 2},
		{"m-diag-nosql", "m-diag", "分析与 NoSQL", "", "um.sidebar.diag-nosql", "查看分析与 NoSQL 监控", "databoard", 40},
		{"m-diag-dbmon-starrocks", "m-diag-nosql", "Starrocks监控", "/ops/db-monitor/starrocks", "um.sidebar.ops-db-monitor-starrocks", "查看 StarRocks 监控", "histogram", 1},
		{"m-diag-dbmon-clickhouse", "m-diag-nosql", "ClickHouse监控", "/ops/db-monitor/clickhouse", "um.sidebar.ops-db-monitor-clickhouse", "查看 ClickHouse 监控", "databoard", 2},
		{"m-diag-dbmon-redis", "m-diag-nosql", "Redis监控", "/ops/db-monitor/redis", "um.sidebar.ops-db-monitor-redis", "查看 Redis 监控", "connection", 3},
		{"m-diag-redis-rdb", "m-diag-nosql", "Redis内存分析", "/ops/redis-rdb", "um.sidebar.ops-redis-rdb", "查看 Redis RDB 内存分析", "search", 4},
		{"m-diag-dbmon-mongodb", "m-diag-nosql", "MongoDB监控", "/ops/db-monitor/mongodb", "um.sidebar.ops-db-monitor-mongodb", "查看 MongoDB 监控", "coin", 5},
		{"m-diag-dbmon-cassandra", "m-diag-nosql", "Cassandra监控", "/ops/db-monitor/cassandra", "um.sidebar.ops-db-monitor-cassandra", "查看 Cassandra 监控", "databoard", 6},
		{"m-diag-dbmon-es", "m-diag-nosql", "ES监控", "/ops/db-monitor/elasticsearch", "um.sidebar.ops-db-monitor-es", "查看 Elasticsearch 监控", "search", 7},
		{"m-diag-infra", "m-diag", "中间件与基础设施", "", "um.sidebar.diag-infra", "查看中间件与基础设施监控", "connection", 50},
		{"m-diag-dbmon-kafka", "m-diag-infra", "Kafka监控", "/ops/db-monitor/kafka", "um.sidebar.ops-db-monitor-kafka", "查看 Kafka 监控", "files", 1},
		{"m-diag-dbmon-debezium", "m-diag-infra", "Debezium监控", "/ops/db-monitor/debezium", "um.sidebar.ops-db-monitor-debezium", "查看 Debezium 监控", "connection", 2},
		{"m-diag-dbmon-host", "m-diag-infra", "主机监控", "/ops/db-monitor/host", "um.sidebar.ops-db-monitor-host", "查看主机监控", "monitor", 3},
		{"m-diag-dbmon-dolphin", "m-diag-infra", "海豚监控", "/ops/db-monitor/dolphinscheduler", "um.sidebar.ops-db-monitor-dolphin", "查看海豚监控", "histogram", 4},
		{"m-plat", "", "平台运维", "", "um.sidebar.platform", "查看平台运维", "grid", 50},
		{"m-plat-ml", "m-plat", "多语模版", "/ops/multilang", "um.sidebar.ops-multilang", "查看多语模版", "files", 51},
		{"m-plat-ds", "m-plat", "海豚管理", "/ops/dolphin", "um.sidebar.ops-dolphin", "查看海豚管理", "histogram", 52},
		{"m-plat-sr", "m-plat", "SR控制台", "/ops/sr", "um.sidebar.ops-sr", "查看 SR 控制台", "coin", 52},
		{"m-plat-term", "m-plat", "主机终端", "/ops/terminal", "um.sidebar.ops-terminal", "查看主机终端", "monitor", 54},
		{"m-plat-ha", "m-plat", "高可用服务维护", "/ops/ha", "um.sidebar.ops-ha", "查看高可用服务维护", "safety", 55},
		{"m-rbac", "", "用户权限", "", "um.sidebar.rbac", "查看用户权限", "lock", 55},
		{"m-cfg-users", "m-rbac", "用户管理", "/config/users", "um.sidebar.config-users", "查看用户管理", "user", 56},
		{"m-cfg-roles", "m-rbac", "角色授权", "/config/roles", "um.sidebar.config-roles", "查看角色授权", "team", 57},
		{"m-cfg-menus", "m-rbac", "菜单管理", "/config/menus", "um.sidebar.config-menus", "查看菜单管理", "menu", 58},
		{"m-cfg-login-sessions", "m-rbac", "登录会话", "/config/login-sessions", "um.sidebar.config-login-sessions", "查看登录会话并强制下线", "team", 58},
		{"m-cfg-login-logs", "m-rbac", "登录日志", "/config/login-logs", "um.sidebar.config-login-logs", "查看登录日志", "login", 59},
		{"m-cfg-ldap", "m-rbac", "认证配置", "/config/ldap", "um.sidebar.config-ldap", "查看认证配置", "safety", 60},
		{"m-cfg-query-audits", "m-rbac", "查询审计", "/config/query-audits", "um.sidebar.config-query-audits", "查看查询审计", "audit", 61},
		{"m-cfg", "", "配置中心", "", "um.sidebar.config", "查看配置中心", "setting", 2},
		{"m-cfg-base", "m-cfg", "基础设置", "", "um.sidebar.config-base", "查看基础设置", "setting", 10},
		{"m-cfg-access", "m-cfg", "标签与连接", "", "um.sidebar.config-access", "查看标签与连接", "tag", 20},
		{"m-cfg-obs", "m-cfg", "监控运维", "", "um.sidebar.config-obs", "查看监控运维", "monitor", 30},
		{"m-cfg-args", "m-cfg-base", "全局参数", "/config/args", "um.sidebar.config-args", "查看全局参数", "setting", 1},
		{"m-cfg-region", "m-cfg-base", "区域管理", "/config/regions", "um.sidebar.config-regions", "查看区域管理", "location", 2},
		{"m-cfg-env", "m-cfg-base", "环境管理", "/config/environments", "um.sidebar.config-environments", "查看环境管理", "grid", 3},
		{"m-cfg-tags", "m-cfg-access", "标签管理", "/config/tags", "um.sidebar.config-tags", "查看标签管理", "tag", 1},
		{"m-cfg-conn", "m-cfg-access", "连接器", "/config/connectors", "um.sidebar.config-connectors", "查看连接器", "connection", 2},
		{"m-cfg-agent", "m-cfg-access", "平台 Agent", "/config/agents", "um.sidebar.config-agents", "查看平台 Agent", "connection", 3},
		{"m-cfg-oss", "m-cfg-access", "对象存储", "/config/oss", "um.sidebar.config-oss", "查看对象存储配置与上传", "upload", 4},
		{"m-cfg-monitor", "m-cfg-obs", "监控目标注册", "/config/monitor-targets", "um.sidebar.config-monitor-targets", "查看监控目标注册", "monitor", 1},
		{"m-cfg-fe-admin", "m-cfg-obs", "FE管理", "/config/fe-admin", "um.sidebar.config-fe-admin", "查看 FE 管理", "coin", 2},
		{"m-cfg-ds-admin", "m-cfg-obs", "海豚配置", "/config/ds-admin", "um.sidebar.config-ds-admin", "查看海豚配置", "histogram", 3},
		{"m-cfg-scheduled-jobs", "m-cfg-obs", "定时任务", "/config/scheduled-jobs", URNSidebarScheduledJobs, "查看平台定时任务", "clock", 4},
		{"m-db-config", "", "资源配置", "", "um.sidebar.db-config", "查看资源配置", "coin", 3},
		{"m-cfg-inst", "m-db-config", "实例管理", "/config/instances", "um.sidebar.config-instances", "查看实例管理", "monitor", 1},
		{"m-cfg-db", "m-db-config", "已登记库", "/config/databases", "um.sidebar.config-databases", "查看已登记库", "coin", 2},
		{"m-cfg-domains", "m-db-config", "域名管理", "/config/domains", URNSidebarConfigDomains, "查看域名管理", "share", 3},
		{"m-cfg-accounts", "m-db-config", "账号管理", "/config/accounts", "um.sidebar.config-accounts", "查看账号管理", "user", 4},
		{"m-cfg-review", "m-db-config", "审核规则", "/config/review-rules", "um.sidebar.config-review-rules", "查看审核规则", "document", 5},
		{"m-cfg-sql-insight", "m-db-config", "SQL 洞察设置", "/config/sql-insight", "um.sidebar.config-sql-insight", "查看 SQL 洞察设置", "setting", 6},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.CreateMenu(m); err != nil {
			return err
		}
	}
	log.Printf("seed: default menu tree created")
	return nil
}

func (d *DB) ensureDashboardMenu(now string) error {
	exist, err := d.GetMenu("m-dashboard")
	if err != nil {
		return err
	}
	if exist == nil {
		m := &Menu{
			ID: "m-dashboard", Title: "仪表盘", Path: "/ops/dashboard",
			URN: "um.sidebar.ops-dashboard", PermissionDesc: "查看仪表盘", Icon: "dashboard",
			SortOrder: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := d.CreateMenu(m); err != nil {
			return err
		}
		log.Printf("seed: dashboard menu created")
	} else if exist.SortOrder != 1 || exist.Path != "/ops/dashboard" {
		exist.Path = "/ops/dashboard"
		exist.SortOrder = 1
		exist.Title = "仪表盘"
		exist.Enabled = true
		if exist.URN == "" {
			exist.URN = "um.sidebar.ops-dashboard"
		}
		if exist.PermissionDesc == "" {
			exist.PermissionDesc = "查看仪表盘"
		}
		if exist.Icon == "" {
			exist.Icon = "dashboard"
		}
		if err := d.UpdateMenu(exist); err != nil {
			return err
		}
	}
	return d.GrantMenuToAllRoles("m-dashboard")
}

func (d *DB) ensureDqlMenu(now string) error {
	m := &Menu{
		ID: "m-tickets-dql", ParentID: "m-tickets", Title: "DQL 工单", Path: "/ops/dql",
		URN: "um.sidebar.ops-dql", PermissionDesc: "查看数据导出", Icon: "download",
		SortOrder: 17, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.GrantMenuFollowingParent(m.ID, m.ParentID)
}

func (d *DB) ensureRedisTicketMenu(now string) error {
	m := &Menu{
		ID: "m-tickets-redis", ParentID: "m-tickets", Title: "Redis 工单", Path: "/ops/redis",
		URN: "um.sidebar.ops-redis", PermissionDesc: "查看 Redis 工单", Icon: "coin",
		SortOrder: 15, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.GrantMenuFollowingParent(m.ID, m.ParentID)
}

func (d *DB) ensureResourceApplyMenu(now string) error {
	m := &Menu{
		ID: "m-tickets-resource-apply", ParentID: "m-tickets", Title: "平台资源申请", Path: "/ops/resource-apply",
		URN: "um.sidebar.ops-resource-apply", PermissionDesc: "查看平台资源申请", Icon: "appstore",
		SortOrder: 18, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	// 已有「工单申请」的角色跟进授权；内置角色再兜底灌入，避免侧栏缺失 / 403
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	return d.GrantMenuToAllRoles(m.ID)
}

// EnsureResourceApplyMenu 供运维/启动补菜单（幂等）。
func (d *DB) EnsureResourceApplyMenu(now string) error {
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339)
	}
	return d.ensureResourceApplyMenu(now)
}

func (d *DB) ensureSchemaCompareMenu(now string) error {
	m := &Menu{
		ID: "m-sync-schema-cmp", ParentID: "m-sync", Title: "表结构对比", Path: "/ops/migration/compare",
		URN: "um.sidebar.ops-migration-compare", PermissionDesc: "查看表结构对比", Icon: "document",
		SortOrder: 24, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.GrantMenuFollowingParent(m.ID, m.ParentID)
}

func (d *DB) ensureDataQueryMenuTree(now string) error {
	parent := &Menu{
		ID: "m-data-query", ParentID: "", Title: "数据查询", Path: "",
		URN: "um.sidebar.data-query", PermissionDesc: "查看数据查询", Icon: "search",
		SortOrder: 5, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	if err := d.GrantMenuToAllRoles(parent.ID); err != nil {
		return err
	}
	items := []seedMenu{
		{"m-query", "m-data-query", "数据库", "/ops/query", "um.sidebar.ops-query", "查看数据库", "search", 1},
		{"m-query-kafka", "m-data-query", "Kafka 查询", "/ops/kafka-query", "um.sidebar.ops-query-kafka", "查看 Kafka", "share", 2},
		{"m-query-redis", "m-data-query", "Redis 查询", "/ops/redis-query", "um.sidebar.ops-query-redis", "查看 Redis", "coin", 3},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureQueryInstanceTypes() error {
	raw, err := d.GetGlobalArg("ticket_instance_types")
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		log.Printf("seed: ticket_instance_types JSON 无法解析，跳过补齐查询类型: %v", err)
		return nil
	}
	have := map[string]struct{}{}
	cleaned := make([]map[string]any, 0, len(items))
	changed := false
	for _, it := range items {
		v := strings.ToLower(strings.TrimSpace(fmt.Sprint(it["value"])))
		if v == "cockroadchdb" {
			v = "cockroachdb"
			it["value"] = "cockroachdb"
			it["label"] = "CockroachDB"
			changed = true
		}
		if v == "" {
			changed = true
			continue
		}
		if _, ok := have[v]; ok {
			changed = true
			continue
		}
		have[v] = struct{}{}
		cleaned = append(cleaned, it)
	}
	items = cleaned
	extras := []map[string]any{
		{"value": "cockroachdb", "label": "CockroachDB"},
		{"value": "oceanbase", "label": "OceanBase"},
		{"value": "cassandra", "label": "Cassandra"},
		{"value": "oracle", "label": "Oracle"},
		{"value": "elasticsearch", "label": "Elasticsearch"},
		{"value": "mongodb", "label": "MongoDB"},
		{"value": "sqlserver", "label": "SqlServer"},
	}
	added := 0
	for _, it := range extras {
		v := strings.ToLower(fmt.Sprint(it["value"]))
		if _, ok := have[v]; ok {
			continue
		}
		items = append(items, it)
		have[v] = struct{}{}
		added++
		changed = true
	}
	if !changed {
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := d.SetGlobalArg("ticket_instance_types", string(b), "实例可选类型：JSON [{value,label}]，仅 value 必填"); err != nil {
		return err
	}
	log.Printf("seed: ticket_instance_types 已更新（新增 %d）", added)
	return nil
}

func (d *DB) ensureRedisInstanceType() error {
	raw, err := d.GetGlobalArg("ticket_instance_types")
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		log.Printf("seed: ticket_instance_types JSON 无法解析，跳过补 Redis: %v", err)
		return nil
	}
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(it["value"])), "redis") {
			return nil
		}
	}
	items = append(items, map[string]any{"value": "redis", "label": "Redis"})
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := d.SetGlobalArg("ticket_instance_types", string(b), "实例可选类型：JSON [{value,label}]，仅 value 必填"); err != nil {
		return err
	}
	log.Printf("seed: ticket_instance_types 已补充 redis")
	return nil
}

func (d *DB) ensureHostInstanceType() error {
	raw, err := d.GetGlobalArg("ticket_instance_types")
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		log.Printf("seed: ticket_instance_types JSON 无法解析，跳过补主机类型: %v", err)
		return nil
	}
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(it["value"])), "host") {
			return nil
		}
	}
	items = append(items, map[string]any{"value": "host", "label": "主机"})
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := d.SetGlobalArg("ticket_instance_types", string(b), "实例可选类型：JSON [{value,label}]，仅 value 必填"); err != nil {
		return err
	}
	log.Printf("seed: ticket_instance_types 已补充 host")
	return nil
}

func (d *DB) ensureFlashbackMenuTree(now string) error {
	items := []seedMenu{
		{"m-safe-fb", "m-safe", "数据闪回", "", "um.sidebar.ops-flashback", "查看数据闪回", "clock", 31},
		{"m-safe-fb-dash", "m-safe-fb", "仪表盘", "/ops/flashback/dashboard", "um.sidebar.ops-flashback-dashboard", "查看闪回仪表盘", "histogram", 31},
		{"m-safe-fb-tasks", "m-safe-fb", "闪回任务", "/ops/flashback/tasks", "um.sidebar.ops-flashback-tasks", "查看闪回任务", "list", 32},
		{"m-safe-fb-hist", "m-safe-fb", "历史记录", "/ops/flashback/history", "um.sidebar.ops-flashback-history", "查看闪回历史", "clock", 33},
		{"m-safe-fb-cloud", "m-safe-fb", "多云密钥", "/ops/flashback/cloud", "um.sidebar.ops-flashback-cloud", "查看闪回多云密钥", "key", 34},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
		if it.Parent == "m-safe-fb" {
			if err := d.GrantMenuToAllRoles(it.ID); err != nil {
				return err
			}
		}
	}
	if err := d.GrantMenuToAllRoles("m-safe-fb"); err != nil {
		return err
	}
	log.Printf("seed: flashback menu tree ensured")
	return nil
}

func (d *DB) ensureArchiveMenuTree(now string) error {
	items := []seedMenu{
		{"m-safe-arc", "m-safe", "归档管理", "", "um.sidebar.ops-archive", "查看归档管理", "folder", 32},
		{"m-safe-arc-dash", "m-safe-arc", "控制台", "/ops/archive/dashboard", "um.sidebar.ops-archive-dashboard", "查看归档控制台", "histogram", 33},
		{"m-safe-arc-apply", "m-safe-arc", "归档任务", "/ops/archive/apply", "um.sidebar.ops-archive-apply", "提交归档工单", "document", 33},
		{"m-safe-arc-tasks", "m-safe-arc", "任务管理", "/ops/archive/tasks", "um.sidebar.ops-archive-tasks", "查看归档任务", "list", 34},
		{"m-safe-arc-runs", "m-safe-arc", "执行记录", "/ops/archive/runs", "um.sidebar.ops-archive-runs", "查看归档执行记录", "clock", 35},
		{"m-safe-arc-mon", "m-safe-arc", "监控告警", "/ops/archive/monitor", "um.sidebar.ops-archive-monitor", "查看归档监控", "warning", 36},
		{"m-safe-arc-set", "m-safe-arc", "系统设置", "/ops/archive/settings", "um.sidebar.ops-archive-settings", "查看归档设置", "setting", 37},
		{"m-safe-arc-mask", "m-safe-arc", "敏感数据脱敏", "/ops/archive/masking", "um.sidebar.ops-archive-masking", "查看数据脱敏", "lock", 38},
		{"m-safe-arc-rpt", "m-safe-arc", "归档报表", "/ops/archive/report", "um.sidebar.ops-archive-report", "查看归档报表", "databoard", 39},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
		if it.Parent == "m-safe-arc" {
			if err := d.GrantMenuToAllRoles(it.ID); err != nil {
				return err
			}
		}
	}
	if err := d.GrantMenuToAllRoles("m-safe-arc"); err != nil {
		return err
	}
	log.Printf("seed: archive menu tree ensured")
	return nil
}

func (d *DB) ensureBackupMenuTree(now string) error {
	items := []seedMenu{
		{"m-safe-bak", "m-safe", "数据备份", "", "um.sidebar.ops-backup", "查看数据备份", "folder", 40},
		{"m-safe-bak-pol", "m-safe-bak", "备份策略", "/ops/backup/policies", "um.sidebar.ops-backup-policies", "查看备份策略", "list", 41},
		{"m-safe-bak-runs", "m-safe-bak", "执行记录", "/ops/backup/runs", "um.sidebar.ops-backup-runs", "查看备份执行记录", "clock", 42},
		{"m-safe-bak-restore", "m-safe-bak", "备份恢复", "/ops/backup/restore", "um.sidebar.ops-backup-restore", "发起逻辑/物理全量恢复工单", "rollback", 43},
		{"m-safe-bak-set", "m-safe-bak", "备份存储", "/ops/backup/settings", "um.sidebar.ops-backup-settings", "查看备份存储", "setting", 44},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
		if it.Parent == "m-safe-bak" {
			if err := d.GrantMenuToAllRoles(it.ID); err != nil {
				return err
			}
		}
	}
	if err := d.GrantMenuToAllRoles("m-safe-bak"); err != nil {
		return err
	}
	log.Printf("seed: backup menu tree ensured")
	return nil
}

func (d *DB) ensureRegionEnvCatalog(now string) error {
	if exist, _ := d.GetRegion("r-default"); exist == nil {
		if err := d.CreateRegion(&Region{ID: "r-default", Code: "default", Name: "默认", Description: "默认区域", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
	}
	if exist, _ := d.GetEnvironment("e-test"); exist == nil {
		if err := d.CreateEnvironment(&Environment{ID: "e-test", Code: "test", Name: "测试", Description: "测试环境", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
	}
	if exist, _ := d.GetEnvironment("e-prod"); exist == nil {
		if err := d.CreateEnvironment(&Environment{ID: "e-prod", Code: "prod", Name: "生产", Description: "生产环境", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
	}
	bound, err := d.ListRegionEnvs("r-default")
	if err != nil {
		return err
	}
	if len(bound) == 0 {
		if err := d.SetRegionEnvs("r-default", []string{"e-test", "e-prod"}); err != nil {
			return err
		}
	}
	_, err = d.Exec(`UPDATE tbl_instances SET region_id='r-default', env_id=CASE WHEN idc IN ('prod','production') THEN 'e-prod' ELSE 'e-test' END, idc=CASE WHEN idc IN ('prod','production') THEN 'prod' ELSE 'test' END WHERE region_id IS NULL OR region_id=''`)
	return err
}

func (d *DB) ensureRegionEnvMenus(now string) error {
	items := []seedMenu{
		{"m-cfg-region", "m-cfg", "区域管理", "/config/regions", "um.sidebar.config-regions", "查看区域管理", "location", 67},
		{"m-cfg-env", "m-cfg", "环境管理", "/config/environments", "um.sidebar.config-environments", "查看环境管理", "grid", 68},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: region/env menus ensured")
	return nil
}

func (d *DB) ensureMonitorTagMenu(now string) error {
	items := []seedMenu{
		{"m-cfg-tags", "m-cfg", "标签管理", "/config/tags", "um.sidebar.config-tags", "查看标签管理", "tag", 69},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: monitor tag menu ensured")
	return nil
}

// ensureConfigCenterGroups 将配置中心叶子菜单归入专业分组，供侧栏 Flyout 分栏展示。
func (d *DB) ensureConfigCenterGroups(now string) error {
	groups := []seedMenu{
		{"m-cfg-base", "m-cfg", "基础设置", "", "um.sidebar.config-base", "查看基础设置", "setting", 10},
		{"m-cfg-access", "m-cfg", "标签与连接", "", "um.sidebar.config-access", "查看标签与连接", "tag", 20},
		{"m-cfg-obs", "m-cfg", "监控运维", "", "um.sidebar.config-obs", "查看监控运维", "monitor", 30},
	}
	for _, it := range groups {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	leaves := []struct {
		id, parent string
		sort       int
	}{
		{"m-cfg-args", "m-cfg-base", 1},
		{"m-cfg-region", "m-cfg-base", 2},
		{"m-cfg-env", "m-cfg-base", 3},
		{"m-cfg-tags", "m-cfg-access", 1},
		{"m-cfg-conn", "m-cfg-access", 2},
		{"m-cfg-agent", "m-cfg-access", 3},
		{"m-cfg-oss", "m-cfg-access", 4},
		{"m-cfg-monitor", "m-cfg-obs", 1},
		{"m-cfg-fe-admin", "m-cfg-obs", 2},
		{"m-cfg-ds-admin", "m-cfg-obs", 3},
		{"m-cfg-scheduled-jobs", "m-cfg-obs", 4},
	}
	for _, leaf := range leaves {
		exist, err := d.GetMenu(leaf.id)
		if err != nil {
			return err
		}
		if exist == nil {
			continue
		}
		exist.ParentID = leaf.parent
		exist.SortOrder = leaf.sort
		exist.UpdatedAt = now
		if err := d.UpdateMenu(exist); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(leaf.id, leaf.parent); err != nil {
			return err
		}
	}
	log.Printf("seed: config center groups ensured")
	return nil
}

// ensurePlatformAgentMenu 配置中心 · 平台 Agent（平台级执行面，归属 backup/rdb/ha）。
func (d *DB) ensurePlatformAgentMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-agent", ParentID: "m-cfg-access", Title: "平台 Agent", Path: "/config/agents",
		URN: "um.sidebar.config-agents", PermissionDesc: "查看平台 Agent", Icon: "connection",
		SortOrder: 3, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	log.Printf("seed: platform agent menu ensured")
	return nil
}

// ensureMigrationOSSMenu 配置中心 · OSS 配置（数据迁移 OSS 中转等引用）。
func (d *DB) ensureMigrationOSSMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-oss", ParentID: "m-cfg-access", Title: "对象存储", Path: "/config/oss",
		URN: "um.sidebar.config-oss", PermissionDesc: "查看对象存储配置与上传", Icon: "upload",
		SortOrder: 4, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	log.Printf("seed: migration oss menu ensured")
	return nil
}

// ensureSrClusterMenu 平台运维 · 集群部署（按引擎类型交付；StarRocks 为第一种）。
func (d *DB) ensureSrClusterMenu(now string) error {
	parent := &Menu{
		ID: "m-plat", ParentID: "", Title: "平台运维", Path: "",
		URN: "um.sidebar.platform", PermissionDesc: "查看平台运维", Icon: "grid",
		SortOrder: 50, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	m := &Menu{
		ID: "m-plat-sr-cluster", ParentID: "m-plat", Title: "集群部署", Path: "/ops/cluster-deploy",
		URN: "um.sidebar.ops-cluster-deploy", PermissionDesc: "查看集群部署", Icon: "coin",
		SortOrder: 53, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	// 将原「SR 节点」菜单更名为控制台并调整排序，与集群部署区分
	if exist, err := d.GetMenu("m-plat-sr"); err == nil && exist != nil {
		exist.SortOrder = 52
		exist.Title = "SR控制台"
		exist.UpdatedAt = now
		_ = d.UpdateMenu(exist)
	}
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	log.Printf("seed: cluster-deploy menu ensured")
	return nil
}

func (d *DB) ensureDbConfigMenuTree(now string) error {
	parent := &Menu{
		ID: "m-db-config", ParentID: "", Title: "资源配置", Path: "",
		URN: "um.sidebar.db-config", PermissionDesc: "查看资源配置", Icon: "coin",
		SortOrder: 3, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	if err := d.GrantMenuToAllRoles(parent.ID); err != nil {
		return err
	}
	items := []seedMenu{
		{"m-cfg-inst", "m-db-config", "实例管理", "/config/instances", "um.sidebar.config-instances", "查看实例管理", "monitor", 1},
		{"m-cfg-db", "m-db-config", "已登记库", "/config/databases", "um.sidebar.config-databases", "查看已登记库", "coin", 2},
		{"m-cfg-domains", "m-db-config", "域名管理", "/config/domains", URNSidebarConfigDomains, "查看域名管理", "share", 3},
		{"m-cfg-accounts", "m-db-config", "账号管理", "/config/accounts", "um.sidebar.config-accounts", "查看账号管理", "user", 4},
		{"m-cfg-review", "m-db-config", "审核规则", "/config/review-rules", "um.sidebar.config-review-rules", "查看审核规则", "document", 5},
		{"m-cfg-sql-insight", "m-db-config", "SQL 洞察设置", "/config/sql-insight", "um.sidebar.config-sql-insight", "查看 SQL 洞察设置", "setting", 6},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
			return err
		}
	}
	return nil
}

// ensureAccessDomainsMenu 资源配置 · 域名管理 + 同步/划拨动作（幂等）。
func (d *DB) ensureAccessDomainsMenu(now string) error {
	if err := d.ensureDbConfigMenuTree(now); err != nil {
		return err
	}
	actions := []seedMenu{
		{"m-act-domains-sync", "m-cfg-domains", "同步域名", "", URNDomainsSync, "一键同步实例域名", "refresh", 90},
		{"m-act-domains-assign", "m-cfg-domains", "划拨域名", "", URNDomainsAssign, "划拨域名归属租户", "share", 91},
		{"m-act-domains-edit", "m-cfg-domains", "编辑域名", "", URNDomainsEdit, "编辑域名资源", "edit", 92},
	}
	for _, it := range actions {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
			return err
		}
	}
	log.Printf("seed: access domains menu ensured")
	return nil
}

func (d *DB) ensureTerminalMenu(now string) error {
	parent := &Menu{
		ID: "m-plat", ParentID: "", Title: "平台运维", Path: "",
		URN: "um.sidebar.platform", PermissionDesc: "查看平台运维", Icon: "grid",
		SortOrder: 50, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	m := &Menu{
		ID: "m-plat-term", ParentID: "m-plat", Title: "主机终端", Path: "/ops/terminal",
		URN: "um.sidebar.ops-terminal", PermissionDesc: "查看主机终端", Icon: "monitor",
		SortOrder: 54, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.GrantMenuFollowingParent(m.ID, m.ParentID)
}

func (d *DB) ensureHAMenu(now string) error {
	parent := &Menu{
		ID: "m-plat", ParentID: "", Title: "平台运维", Path: "",
		URN: "um.sidebar.platform", PermissionDesc: "查看平台运维", Icon: "grid",
		SortOrder: 50, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	m := &Menu{
		ID: "m-plat-ha", ParentID: "m-plat", Title: "高可用服务维护", Path: "/ops/ha",
		URN: "um.sidebar.ops-ha", PermissionDesc: "查看高可用服务维护", Icon: "safety",
		SortOrder: 55, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.GrantMenuFollowingParent(m.ID, m.ParentID)
}

func (d *DB) ensureCmdbMenuTree(now string) error {
	items := []seedMenu{
		{"m-cmdb", "", "CMDB", "", "um.sidebar.cmdb", "查看 CMDB", "coin", 6},
		{"m-cmdb-assets", "m-cmdb", "资产目录", "/cmdb/assets", "um.sidebar.cmdb-assets", "查看资产目录", "list", 1},
		{"m-cmdb-topo", "m-cmdb", "关系拓扑", "/cmdb/topology", "um.sidebar.cmdb-topology", "查看关系拓扑", "share", 2},
		{"m-cmdb-tags", "m-cmdb", "标签管理", "/cmdb/tags", "um.sidebar.cmdb-tags", "查看 CMDB 标签", "tag", 3},
		{"m-cmdb-discover", "m-cmdb", "发现登记", "/cmdb/discover", "um.sidebar.cmdb-discover", "查看发现登记", "search", 4},
		{"m-cmdb-io", "m-cmdb", "导入导出", "/cmdb/import-export", "um.sidebar.cmdb-io", "查看导入导出", "upload", 5},
		{"m-cmdb-audit", "m-cmdb", "变更审计", "/cmdb/audit", "um.sidebar.cmdb-audit", "查看变更审计", "audit", 6},
		{"m-cmdb-sync", "m-cmdb", "同步任务", "/cmdb/sync", "um.sidebar.cmdb-sync", "查看同步任务", "refresh", 7},
		{"m-act-cmdb-edit", "m-cmdb", "编辑 CMDB", "", URNCmdbEdit, "编辑 CMDB", "edit", 90},
		{"m-act-cmdb-import", "m-cmdb", "导入导出权限", "", URNCmdbImport, "CMDB 导入导出", "upload", 91},
		{"m-act-cmdb-sync", "m-cmdb", "同步权限", "", URNCmdbSync, "CMDB 外部同步", "refresh", 92},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	// 原配置中心标签页指向 CMDB 标签
	if exist, err := d.GetMenu("m-cfg-tags"); err == nil && exist != nil {
		exist.Path = "/cmdb/tags"
		exist.UpdatedAt = now
		_ = d.UpdateMenu(exist)
	}
	log.Printf("seed: cmdb menu tree ensured")
	return nil
}

func (d *DB) ensureLoginLogsMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-login-logs", ParentID: "m-rbac", Title: "登录日志", Path: "/config/login-logs",
		URN: "um.sidebar.config-login-logs", PermissionDesc: "查看登录日志", Icon: "login",
		SortOrder: 59, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

func (d *DB) ensureLoginSessionsMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-login-sessions", ParentID: "m-rbac", Title: "登录会话", Path: "/config/login-sessions",
		URN: "um.sidebar.config-login-sessions", PermissionDesc: "查看登录会话并强制下线", Icon: "team",
		SortOrder: 58, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	// 跟父菜单「用户权限」已授权的角色一并开通；并保证 admin 有权
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

// ensureScheduledJobsMenu 配置中心 · 监控运维 · 定时任务。
func (d *DB) ensureScheduledJobsMenu(now string) error {
	if err := d.ensureConfigCenterGroups(now); err != nil {
		return err
	}
	m := &Menu{
		ID: "m-cfg-scheduled-jobs", ParentID: "m-cfg-obs", Title: "定时任务", Path: "/config/scheduled-jobs",
		URN: URNSidebarScheduledJobs, PermissionDesc: "查看平台定时任务", Icon: "clock",
		SortOrder: 4, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

func (d *DB) ensureScheduledJobsActionMenus(now string) error {
	if err := d.ensureScheduledJobsMenu(now); err != nil {
		return err
	}
	items := []seedMenu{
		{"m-act-scheduled-jobs-edit", "m-cfg-scheduled-jobs", "编辑定时任务", "", URNScheduledJobsEdit, "启停与修改平台定时任务调度", "edit", 90},
		{"m-act-scheduled-jobs-trigger", "m-cfg-scheduled-jobs", "触发定时任务", "", URNScheduledJobsTrigger, "手动立即执行平台定时任务", "thunderbolt", 91},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: scheduled-jobs action menus ensured")
	return nil
}

func (d *DB) ensureQueryAuditsMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-query-audits", ParentID: "m-rbac", Title: "查询审计", Path: "/config/query-audits",
		URN: "um.sidebar.config-query-audits", PermissionDesc: "查看查询审计", Icon: "audit",
		SortOrder: 61, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

func (d *DB) ensureLdapMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-ldap", ParentID: "m-rbac", Title: "认证配置", Path: "/config/ldap",
		URN: "um.sidebar.config-ldap", PermissionDesc: "查看认证配置", Icon: "safety",
		SortOrder: 60, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

// ensureUserPermissionMenus 将用户/角色/菜单/登录日志/LDAP 挂到顶级「用户权限」下（幂等改 parent）。
func (d *DB) ensureUserPermissionMenus(now string) error {
	parent := &Menu{
		ID: "m-rbac", ParentID: "", Title: "用户权限", Path: "",
		URN: "um.sidebar.rbac", PermissionDesc: "查看用户权限", Icon: "lock",
		SortOrder: 55, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	if err := d.grantMenuToRoleCode(parent.ID, "admin"); err != nil {
		return err
	}
	items := []seedMenu{
		{"m-cfg-users", "m-rbac", "用户管理", "/config/users", "um.sidebar.config-users", "查看用户管理", "user", 56},
		{"m-cfg-roles", "m-rbac", "角色授权", "/config/roles", "um.sidebar.config-roles", "查看角色授权", "team", 57},
		{"m-cfg-menus", "m-rbac", "菜单管理", "/config/menus", "um.sidebar.config-menus", "查看菜单管理", "menu", 58},
		{"m-cfg-login-sessions", "m-rbac", "登录会话", "/config/login-sessions", "um.sidebar.config-login-sessions", "查看登录会话并强制下线", "team", 58},
		{"m-cfg-login-logs", "m-rbac", "登录日志", "/config/login-logs", "um.sidebar.config-login-logs", "查看登录日志", "login", 59},
		{"m-cfg-ldap", "m-rbac", "认证配置", "/config/ldap", "um.sidebar.config-ldap", "查看认证配置", "safety", 60},
		{"m-cfg-query-audits", "m-rbac", "查询审计", "/config/query-audits", "um.sidebar.config-query-audits", "查看查询审计", "audit", 61},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
			return err
		}
	}
	// 配置中心紧挨工作台之后（sort=2）
	cfg := &Menu{
		ID: "m-cfg", ParentID: "", Title: "配置中心", Path: "",
		URN: "um.sidebar.config", PermissionDesc: "查看配置中心", Icon: "setting",
		SortOrder: 2, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(cfg); err != nil {
		return err
	}
	log.Printf("seed: user-permission menus ensured under m-rbac")
	return nil
}

func (d *DB) grantMenuToRoleCode(menuID, roleCode string) error {
	r, err := d.GetRoleByCode(roleCode)
	if err != nil || r == nil {
		return err
	}
	ids, err := d.ListRoleMenuIDs(r.ID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == menuID {
			return nil
		}
	}
	_, err = d.Exec(`INSERT INTO tbl_role_menus(role_id, menu_id) VALUES(?,?)`, r.ID, menuID)
	return err
}

func (d *DB) ensureConnectorMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-conn", ParentID: "m-cfg", Title: "连接器", Path: "/config/connectors",
		URN: "um.sidebar.config-connectors", PermissionDesc: "查看连接器", Icon: "connection",
		SortOrder: 69, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
		return err
	}
	log.Printf("seed: connector menu ensured")
	return nil
}

func (d *DB) ensureMonitorOpsMenus(now string) error {
	items := []seedMenu{
		{"m-cfg-monitor", "m-cfg", "监控目标注册", "/config/monitor-targets", "um.sidebar.config-monitor-targets", "查看监控目标注册", "monitor", 70},
		{"m-cfg-fe-admin", "m-cfg", "FE管理", "/config/fe-admin", "um.sidebar.config-fe-admin", "查看 FE 管理", "coin", 71},
		{"m-cfg-ds-admin", "m-cfg", "海豚配置", "/config/ds-admin", "um.sidebar.config-ds-admin", "查看海豚配置", "histogram", 72},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	// rename SR console menu title if present
	if exist, err := d.GetMenu("m-plat-sr"); err == nil && exist != nil && exist.Title != "SR控制台" {
		exist.Title = "SR控制台"
		exist.UpdatedAt = now
		_ = d.UpdateMenu(exist)
	}
	log.Printf("seed: monitor/fe/ds menus ensured")
	return nil
}

func (d *DB) ensureSqlInsightActionMenus(now string) error {
	items := []seedMenu{
		{"m-act-si-collect", "m-diag-sql", "手动采集活动快照", "", URNSqlInsightCollect, "手动采集 PG 活动会话快照", "upload", 90},
		{"m-act-si-awr-gen", "m-diag-sql", "手动生成 AWR 报告", "", URNSqlInsightAwrGenerate, "手动触发生成 / 管理 AWR 生成任务", "histogram", 91},
		{"m-act-si-space-collect", "m-diag-sql", "手动采集表空间", "", URNSqlInsightSpaceCollect, "手动采集表空间快照 / 管理采集任务", "download", 92},
		{"m-act-si-analyze", "m-diag-sql", "手动智能分析", "", URNSqlInsightAnalyze, "手动触发智能分析运行", "search", 93},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: sql-insight action menus ensured")
	return nil
}

func (d *DB) ensureDbMonitorMenu(now string) error {
	items := []struct {
		ID, Parent, Title, Path, URN, Perm, Icon string
		Sort                                     int
	}{
		{"m-diag-perf", "m-diag", "性能诊断", "", "um.sidebar.diag-perf", "查看性能诊断", "search", 10},
		{"m-diag-sql", "m-diag-perf", "SQL 洞察", "/ops/sql-insight", "um.sidebar.ops-sql-insight", "查看 SQL 洞察", "search", 1},
		{"m-diag-sess", "m-diag-perf", "会话管理", "/ops/sessions", "um.sidebar.ops-sessions", "查看会话管理", "cpu", 2},
		{"m-diag-mysql-slow", "m-diag-perf", "MySQL 慢 SQL", "/ops/mysql-slow", "um.sidebar.ops-mysql-slow", "查看 MySQL Top SQL digest", "histogram", 3},
		{"m-diag-rdbms", "m-diag", "关系型数据库", "", "um.sidebar.diag-rdbms", "查看关系型数据库监控", "coin", 20},
		{"m-diag-dbmon-mysql", "m-diag-rdbms", "MySQL监控", "/ops/db-monitor/mysql", "um.sidebar.ops-db-monitor-mysql", "查看 MySQL 监控", "coin", 1},
		{"m-diag-dbmon", "m-diag-rdbms", "Postgres监控", "/ops/db-monitor/postgres", "um.sidebar.ops-db-monitor-postgres", "查看 Postgres 监控", "monitor", 2},
		{"m-diag-dbmon-oracle", "m-diag-rdbms", "Oracle监控", "/ops/db-monitor/oracle", "um.sidebar.ops-db-monitor-oracle", "查看 Oracle 监控", "databoard", 3},
		{"m-diag-dbmon-sqlserver", "m-diag-rdbms", "SQLServer监控", "/ops/db-monitor/sqlserver", "um.sidebar.ops-db-monitor-sqlserver", "查看 SQLServer 监控", "monitor", 4},
		{"m-diag-dbmon-oceanbase", "m-diag-rdbms", "OceanBase监控", "/ops/db-monitor/oceanbase", "um.sidebar.ops-db-monitor-oceanbase", "查看 OceanBase 监控", "histogram", 5},
		{"m-diag-dist", "m-diag", "分布式 SQL", "", "um.sidebar.diag-dist", "查看分布式 SQL 监控", "monitor", 30},
		{"m-diag-dbmon-tidb", "m-diag-dist", "TiDB监控", "/ops/db-monitor/tidb", "um.sidebar.ops-db-monitor-tidb", "查看 TiDB 监控", "coin", 1},
		{"m-diag-dbmon-cockroach", "m-diag-dist", "CockroachDB监控", "/ops/db-monitor/cockroachdb", "um.sidebar.ops-db-monitor-cockroach", "查看 CockroachDB 监控", "monitor", 2},
		{"m-diag-nosql", "m-diag", "分析与 NoSQL", "", "um.sidebar.diag-nosql", "查看分析与 NoSQL 监控", "databoard", 40},
		{"m-diag-dbmon-starrocks", "m-diag-nosql", "Starrocks监控", "/ops/db-monitor/starrocks", "um.sidebar.ops-db-monitor-starrocks", "查看 StarRocks 监控", "histogram", 1},
		{"m-diag-dbmon-clickhouse", "m-diag-nosql", "ClickHouse监控", "/ops/db-monitor/clickhouse", "um.sidebar.ops-db-monitor-clickhouse", "查看 ClickHouse 监控", "databoard", 2},
		{"m-diag-dbmon-redis", "m-diag-nosql", "Redis监控", "/ops/db-monitor/redis", "um.sidebar.ops-db-monitor-redis", "查看 Redis 监控", "connection", 3},
		{"m-diag-redis-rdb", "m-diag-nosql", "Redis内存分析", "/ops/redis-rdb", "um.sidebar.ops-redis-rdb", "查看 Redis RDB 内存分析", "search", 4},
		{"m-diag-dbmon-mongodb", "m-diag-nosql", "MongoDB监控", "/ops/db-monitor/mongodb", "um.sidebar.ops-db-monitor-mongodb", "查看 MongoDB 监控", "coin", 5},
		{"m-diag-dbmon-cassandra", "m-diag-nosql", "Cassandra监控", "/ops/db-monitor/cassandra", "um.sidebar.ops-db-monitor-cassandra", "查看 Cassandra 监控", "databoard", 6},
		{"m-diag-dbmon-es", "m-diag-nosql", "ES监控", "/ops/db-monitor/elasticsearch", "um.sidebar.ops-db-monitor-es", "查看 Elasticsearch 监控", "search", 7},
		{"m-diag-infra", "m-diag", "中间件与基础设施", "", "um.sidebar.diag-infra", "查看中间件与基础设施监控", "connection", 50},
		{"m-diag-dbmon-kafka", "m-diag-infra", "Kafka监控", "/ops/db-monitor/kafka", "um.sidebar.ops-db-monitor-kafka", "查看 Kafka 监控", "files", 1},
		{"m-diag-dbmon-debezium", "m-diag-infra", "Debezium监控", "/ops/db-monitor/debezium", "um.sidebar.ops-db-monitor-debezium", "查看 Debezium 监控", "connection", 2},
		{"m-diag-dbmon-host", "m-diag-infra", "主机监控", "/ops/db-monitor/host", "um.sidebar.ops-db-monitor-host", "查看主机监控", "monitor", 3},
		{"m-diag-dbmon-dolphin", "m-diag-infra", "海豚监控", "/ops/db-monitor/dolphinscheduler", "um.sidebar.ops-db-monitor-dolphin", "查看海豚监控", "histogram", 4},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(m.ID, m.ParentID); err != nil {
			return err
		}
	}
	log.Printf("seed: typed db-monitor menus ensured")
	return nil
}

func (d *DB) backfillMenuMeta() error {
	list, err := d.ListMenus()
	if err != nil {
		return err
	}
	for i := range list {
		m := list[i]
		changed := false
		if strings.TrimSpace(m.URN) == "" {
			m.URN = defaultMenuURN(m.Path, m.ID)
			changed = true
		}
		if strings.TrimSpace(m.PermissionDesc) == "" {
			m.PermissionDesc = "查看" + m.Title
			changed = true
		}
		if strings.TrimSpace(m.Icon) == "" {
			if ic := defaultMenuIcon(m.ID, m.Path); ic != "" {
				m.Icon = ic
				changed = true
			}
		}
		if changed {
			if err := d.UpdateMenu(&m); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) ensureTenantMenus(now string) error {
	parent := "m-rbac"
	items := []seedMenu{
		{"m-cfg-tenants", parent, "租户管理", "/config/tenants", URNSidebarTenants, "查看租户管理", "cluster", 55},
		{"m-act-tenants-create", "m-cfg-tenants", "新建租户", "", URNTenantsCreate, "创建租户", "plus", 90},
		{"m-act-tenants-edit", "m-cfg-tenants", "编辑租户", "", URNTenantsEdit, "编辑租户资料与状态", "edit", 91},
		{"m-act-tenants-disable", "m-cfg-tenants", "停用租户", "", URNTenantsDisable, "停用租户", "stop", 92},
		{"m-act-tenants-delete", "m-cfg-tenants", "删除租户", "", URNTenantsDelete, "删除无成员且无绑定资源的租户", "delete", 93},
		{"m-act-tenants-members", "m-cfg-tenants", "管理成员", "", URNTenantsMembers, "增删改租户成员与角色", "team", 94},
		{"m-act-tenants-grants", "m-cfg-tenants", "资源授权", "", URNTenantsGrants, "跨租户资源授权", "share", 95},
		{"m-act-tenants-impersonate", "m-cfg-tenants", "进入租户审计", "", URNTenantsImpersonate, "平台管理员进入租户上下文（审计）", "eye", 96},
	}
	admin, _ := d.GetRoleByCode("admin")
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if admin != nil {
			if _, err := d.Exec(
				`INSERT INTO tbl_role_menus(role_id, menu_id) VALUES(?,?) ON CONFLICT DO NOTHING`,
				admin.ID, it.ID,
			); err != nil {
				return err
			}
		}
	}
	log.Printf("seed: tenant menus ensured")
	return nil
}

func (d *DB) ensureAlertMenus(now string) error {
	items := []seedMenu{
		{"m-alert", "", "告警管理", "", "um.sidebar.alerts", "查看告警管理", "bell", 70},
		{"m-alert-active", "m-alert", "活跃告警", "/ops/alerts", "um.sidebar.ops-alerts", "查看活跃告警", "bell", 1},
		{"m-alert-history", "m-alert", "历史告警", "/ops/alerts/history", "um.sidebar.ops-alerts-history", "查看历史告警", "clock", 2},
		{"m-alert-rules", "m-alert", "规则管理", "/ops/alerts/rules", "um.sidebar.ops-alerts-rules", "查看告警规则", "setting", 3},
		{"m-alert-silences", "m-alert", "静默管理", "/ops/alerts/silences", "um.sidebar.ops-alerts-silences", "查看静默规则", "mute", 4},
		{"m-alert-routes", "m-alert", "通知路由", "/ops/alerts/routes", "um.sidebar.ops-alerts-routes", "查看通知路由", "share", 5},
		{"m-alert-inhibitions", "m-alert", "抑制规则", "/ops/alerts/inhibitions", "um.sidebar.ops-alerts-inhibitions", "查看抑制规则", "stop", 6},
		{"m-alert-oncall", "m-alert", "值班排班", "/ops/alerts/oncall", "um.sidebar.ops-alerts-oncall", "查看值班排班", "team", 7},
		{"m-alert-aiops-kb", "m-alert", "AIOps 知识库", "/ops/aiops/knowledge", URNSidebarAIOpsKB, "查看 AIOps 知识库", "book", 8},
		{"m-alert-aiops-incidents", "m-alert", "AIOps 关联", "/ops/aiops/incidents", URNSidebarAIOpsIncidents, "查看告警关联事故", "cluster", 9},
		{"m-act-alert-claim", "m-alert-active", "认领告警", "", URNAlertClaim, "认领 / 转派 / 处理中", "user", 90},
		{"m-act-alert-resolve", "m-alert-active", "关闭告警", "", URNAlertResolve, "恢复 / 误报关闭", "check", 91},
		{"m-act-alert-silence", "m-alert-active", "静默告警", "", URNAlertSilence, "新建 / 取消静默", "pause", 92},
		{"m-act-alert-rule-edit", "m-alert-rules", "编辑规则", "", URNAlertRuleEdit, "新建 / 编辑 / 启停规则", "edit", 90},
		{"m-act-alert-rule-del", "m-alert-rules", "删除规则", "", URNAlertRuleDelete, "删除告警规则", "delete", 91},
		{"m-act-copilot-chat", "m-alert-aiops-kb", "Copilot 对话", "", URNCopilotChat, "使用 DBA Copilot", "robot", 90},
		{"m-act-runbook-exec", "m-alert-active", "执行 Runbook", "", URNRunbookExecute, "执行受控 Runbook 动作", "thunderbolt", 93},
	}
	// 若已有「告警管理」目录（含手工空目录），子页面挂到其下，避免出现两个同名目录。
	parentID := "m-alert"
	if list, err := d.ListMenus(); err == nil {
		for _, m := range list {
			if m.Title == "告警管理" && strings.TrimSpace(m.Path) == "" && !IsActionMenu(m) {
				parentID = m.ID
				break
			}
		}
	}
	for i := range items {
		if items[i].ID == "m-alert" {
			items[i].ID = parentID
		}
		if items[i].Parent == "m-alert" {
			items[i].Parent = parentID
		}
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if it.Parent == "" {
			if err := d.GrantMenuToAllRoles(it.ID); err != nil {
				return err
			}
		} else if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: alert menus ensured under %s", parentID)
	return nil
}

func (d *DB) ensureTodoMenus(now string) error {
	parentID := "m-todo"
	if list, err := d.ListMenus(); err == nil {
		for _, m := range list {
			if m.Title == "待办跟进" && strings.TrimSpace(m.Path) == "" && !IsActionMenu(m) {
				parentID = m.ID
				break
			}
		}
	}
	parent := &Menu{
		ID: parentID, ParentID: "", Title: "待办跟进", Path: "",
		URN: "um.sidebar.todo", PermissionDesc: "查看待办跟进", Icon: "todo-list",
		SortOrder: 65, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(parent); err != nil {
		return err
	}
	if err := d.GrantMenuToAllRoles(parent.ID); err != nil {
		return err
	}
	page := &Menu{
		ID: "m-todo-board", ParentID: parentID, Title: "待办看板", Path: "/ops/todo",
		URN: "um.sidebar.ops-todo", PermissionDesc: "查看待办看板", Icon: "todo-list",
		SortOrder: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	// 若已有挂在该目录下的 /ops/todo 页面，沿用其 id，避免重复
	if list, err := d.ListMenus(); err == nil {
		for _, m := range list {
			if strings.TrimSpace(m.Path) == "/ops/todo" {
				page.ID = m.ID
				page.ParentID = parentID
				break
			}
		}
	}
	if err := d.EnsureMenu(page); err != nil {
		return err
	}
	if err := d.GrantMenuFollowingParent(page.ID, parentID); err != nil {
		return err
	}
	log.Printf("seed: todo menus ensured under %s", parentID)
	return nil
}

func (d *DB) ensureK8sMenus(now string) error {
	items := []seedMenu{
		{"m-k8s", "", "容器管理", "", "um.sidebar.k8s", "查看容器管理", "cluster", 45},
		{"m-k8s-clusters", "m-k8s", "集群管理", "/k8s/clusters", URNSidebarK8sClusters, "查看 K8s 集群", "cloud", 1},
		{"m-k8s-overview", "m-k8s", "集群概览", "/k8s/overview", URNSidebarK8sOverview, "查看集群概览", "dashboard", 2},
		{"m-k8s-nodes", "m-k8s", "节点", "/k8s/nodes", URNSidebarK8sNodes, "查看节点", "server", 3},
		{"m-k8s-workloads", "m-k8s", "工作负载", "/k8s/workloads", URNSidebarK8sWorkloads, "查看工作负载", "appstore", 4},
		{"m-k8s-network", "m-k8s", "网络", "/k8s/network", URNSidebarK8sNetwork, "查看网络", "global", 5},
		{"m-k8s-storage", "m-k8s", "存储", "/k8s/storage", URNSidebarK8sStorage, "查看存储", "database", 6},
		{"m-k8s-resources", "m-k8s", "资源浏览器", "/k8s/resources", URNSidebarK8sResources, "浏览 CRD", "code", 7},
		{"m-k8s-operators", "m-k8s", "Operator 中心", "/k8s/operators", URNSidebarK8sOperators, "查看 Operator", "tool", 8},
		{"m-k8s-db", "m-k8s", "数据库服务", "/k8s/db-clusters", URNSidebarK8sDBClusters, "查看数据库服务", "database", 9},
		{"m-k8s-apps", "m-k8s", "Helm 应用", "/k8s/apps", URNSidebarK8sApps, "查看 Helm 应用", "appstore", 10},
	}
	for _, it := range items {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: it.Path,
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if it.Parent != "" {
			if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
				return err
			}
		}
		if err := d.GrantMenuToAllRoles(it.ID); err != nil {
			return err
		}
	}
	actions := []seedMenu{
		{"m-act-k8s-cluster-create", "m-k8s-clusters", "纳管集群", "", URNK8sClusterCreate, "纳管 K8s 集群", "", 1},
		{"m-act-k8s-cluster-edit", "m-k8s-clusters", "编辑集群", "", URNK8sClusterEdit, "编辑 K8s 集群", "", 2},
		{"m-act-k8s-cluster-delete", "m-k8s-clusters", "删除集群", "", URNK8sClusterDelete, "删除 K8s 集群", "", 3},
		{"m-act-k8s-cluster-assign", "m-k8s-clusters", "划拨命名空间", "", URNK8sClusterAssign, "划拨命名空间", "", 4},
		{"m-act-k8s-pod-log", "m-k8s-workloads", "查看 Pod 日志", "", URNK8sPodLog, "查看 Pod 日志", "", 1},
		{"m-act-k8s-pod-exec", "m-k8s-workloads", "Pod 终端", "", URNK8sPodExec, "进入 Pod 终端", "", 2},
		{"m-act-k8s-wl-scale", "m-k8s-workloads", "扩缩容", "", URNK8sWorkloadScale, "工作负载扩缩", "", 3},
		{"m-act-k8s-wl-restart", "m-k8s-workloads", "重启", "", URNK8sWorkloadRestart, "工作负载重启", "", 4},
		{"m-act-k8s-wl-rollback", "m-k8s-workloads", "回滚", "", URNK8sWorkloadRollback, "工作负载回滚", "", 5},
		{"m-act-k8s-wl-delete", "m-k8s-workloads", "删除资源", "", URNK8sWorkloadDelete, "删除工作负载", "", 6},
		{"m-act-k8s-yaml-apply", "m-k8s-resources", "YAML Apply", "", URNK8sYAMLApply, "应用 YAML", "", 1},
		{"m-act-k8s-node-drain", "m-k8s-nodes", "Drain 节点", "", URNK8sNodeDrain, "驱逐节点", "", 1},
		{"m-act-k8s-secret-view", "m-k8s-storage", "查看 Secret 明文", "", URNK8sSecretView, "查看 Secret", "", 1},
		{"m-act-k8s-quota-edit", "m-k8s-overview", "编辑配额", "", URNK8sQuotaEdit, "编辑 ResourceQuota", "", 1},
		{"m-act-k8s-db-create", "m-k8s-db", "创建数据库集群", "", URNK8sDBCreate, "创建数据库服务", "", 1},
		{"m-act-k8s-db-scale", "m-k8s-db", "扩缩数据库", "", URNK8sDBScale, "扩缩数据库服务", "", 2},
		{"m-act-k8s-db-upgrade", "m-k8s-db", "升级数据库", "", URNK8sDBUpgrade, "升级数据库服务", "", 3},
		{"m-act-k8s-db-switch", "m-k8s-db", "切换主库", "", URNK8sDBSwitchover, "数据库切换", "", 4},
		{"m-act-k8s-db-delete", "m-k8s-db", "删除数据库集群", "", URNK8sDBDelete, "删除数据库服务", "", 5},
	}
	for _, it := range actions {
		m := &Menu{
			ID: it.ID, ParentID: it.Parent, Title: it.Title, Path: "",
			URN: it.URN, PermissionDesc: it.Perm, Icon: it.Icon, SortOrder: it.Sort, Enabled: true,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := d.EnsureMenu(m); err != nil {
			return err
		}
		if err := d.GrantMenuFollowingParent(it.ID, it.Parent); err != nil {
			return err
		}
	}
	log.Printf("seed: k8s menus ensured")
	return nil
}

// ensureTopLevelMenuSort 回写一级目录 sort_order，与侧栏正常顺序一致。
func (d *DB) ensureTopLevelMenuSort(now string) error {
	tops := []struct {
		id    string
		title string
		urn   string
		icon  string
		sort  int
	}{
		{"m-dashboard", "仪表盘", "um.sidebar.ops-dashboard", "dashboard", 1},
		{"m-cfg", "配置中心", "um.sidebar.config", "setting", 2},
		{"m-db-config", "资源配置", "um.sidebar.db-config", "coin", 3},
		{"m-data-query", "数据查询", "um.sidebar.data-query", "search", 5},
		{"m-cmdb", "CMDB", "um.sidebar.cmdb", "coin", 6},
		{"m-tickets", "工单申请", "um.sidebar.tickets", "tickets", 10},
		{"m-sync", "同步与迁移", "um.sidebar.sync", "refresh", 20},
		{"m-safe", "数据安全", "um.sidebar.safe", "lock", 30},
		{"m-diag", "诊断", "um.sidebar.diag", "monitor", 40},
		{"m-k8s", "容器管理", "um.sidebar.k8s", "cluster", 45},
		{"m-plat", "平台运维", "um.sidebar.platform", "grid", 50},
		{"m-rbac", "用户权限", "um.sidebar.rbac", "lock", 55},
		{"m-alert", "告警管理", "um.sidebar.alerts", "bell", 70},
	}
	for _, it := range tops {
		exist, err := d.GetMenu(it.id)
		if err != nil {
			return err
		}
		if exist == nil {
			continue
		}
		m := *exist
		m.ParentID = ""
		m.Title = it.title
		if it.urn != "" {
			m.URN = it.urn
		}
		if it.icon != "" {
			m.Icon = it.icon
		}
		m.SortOrder = it.sort
		m.Enabled = true
		m.UpdatedAt = now
		if err := d.UpdateMenu(&m); err != nil {
			return err
		}
	}
	// 待办目录可能是手工 id，按标题回写序号
	if list, err := d.ListMenus(); err == nil {
		for i := range list {
			m := list[i]
			if m.Title == "待办跟进" && strings.TrimSpace(m.Path) == "" && strings.TrimSpace(m.ParentID) == "" {
				m.SortOrder = 65
				m.UpdatedAt = now
				if err := d.UpdateMenu(&m); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}
