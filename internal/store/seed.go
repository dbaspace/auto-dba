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
		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := d.Exec(
			`INSERT INTO tbl_users(id, username, display_name, role, role_id, password_hash, created_at) VALUES(?,?,?,?,?,?,?)`,
			uuid.NewString(), "admin", "管理员", "admin", "", string(hash), now,
		); err != nil {
			return err
		}
		log.Printf("seed: default user admin/admin created")
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
		{Key: "sync_sr_routine_load_desired_concurrent", Value: "3", Description: "CDC Routine Load 默认并发"},
		{Key: "sync_kafka_topic_partitions", Value: "3", Description: "CDC Kafka Topic 默认分区数"},
		{Key: "sync_kafka_topic_replication_factor", Value: "1", Description: "CDC Kafka Topic 默认副本数"},
		{Key: "sync_sr_auto_buckets", Value: "10", Description: "CDC StarRocks 自动建表默认分桶数"},
		{Key: "sync_sr_replication_num", Value: "1", Description: "CDC StarRocks 自动建表默认副本数"},
		{Key: "sync_sr_dynamic_partition_defaults", Value: `{"dynamic_partition.enable":"true","dynamic_partition.time_unit":"DAY","dynamic_partition.start":"-3","dynamic_partition.end":"3","dynamic_partition.prefix":"p"}`, Description: "CDC StarRocks 动态分区 PROPERTIES 默认值（JSON）"},
		{Key: "sync_shard_fanout_on_approve", Value: "true", Description: "分库任务审批后是否拆子任务"},
		{Key: "ticket_exclude_cdc_when_empty", Value: "true", Description: "工单列表无筛选时排除 cdc_sync"},
		{Key: "ddl_forbid_drop_database", Value: "true", Description: "DDL 禁止 DROP DATABASE"},
		{Key: "dml_require_where", Value: "true", Description: "UPDATE/DELETE 必须带 WHERE"},
		{Key: "login_idle_timeout_min", Value: "30", Description: "登录闲置超时（分钟），无操作自动退出；0 表示关闭"},
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
	if err := d.ensureSchemaCompareMenu(now); err != nil {
		return err
	}
	if err := d.ensureFlashbackMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureArchiveMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureRegionEnvMenus(now); err != nil {
		return err
	}
	if err := d.ensureConnectorMenu(now); err != nil {
		return err
	}
	if err := d.ensureDbConfigMenuTree(now); err != nil {
		return err
	}
	if err := d.ensureLoginLogsMenu(now); err != nil {
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
	case "m-diag-sql":
		return "search"
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
	case "m-cfg-conn":
		return "connection"
	case "m-cfg-db":
		return "coin"
	case "m-cfg-accounts":
		return "user"
	case "m-cfg-review":
		return "document"
	case "m-cfg-args":
		return "setting"
	case "m-cfg-menus":
		return "menu"
	case "m-cfg-roles":
		return "user"
	case "m-cfg-users":
		return "user-filled"
	case "m-cfg-login-logs":
		return "clock"
	case "m-cfg-ldap":
		return "key"
	case "m-cfg-query-audits":
		return "search"
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
		{"m-query-kafka", "m-data-query", "Kafka", "/ops/kafka-query", "um.sidebar.ops-query-kafka", "查看 Kafka", "share", 2},
		{"m-query-redis", "m-data-query", "Redis", "/ops/redis-query", "um.sidebar.ops-query-redis", "查看 Redis", "coin", 3},
		{"m-tickets", "", "工单", "", "um.sidebar.tickets", "查看工单", "tickets", 10},
		{"m-tickets-list", "m-tickets", "工单列表", "/ops/tickets", "um.sidebar.ops-tickets", "查看工单列表", "list", 11},
		{"m-tickets-ddl", "m-tickets", "DDL 工单", "/ops/ddl", "um.sidebar.ops-ddl", "查看 DDL 工单", "edit", 12},
		{"m-tickets-dml", "m-tickets", "DML 工单", "/ops/dml", "um.sidebar.ops-dml", "查看 DML 工单", "document", 13},
		{"m-tickets-dql", "m-tickets", "DQL 工单", "/ops/dql", "um.sidebar.ops-dql", "查看数据导出", "download", 17},
		{"m-tickets-kafka", "m-tickets", "Kafka 工单", "/ops/kafka", "um.sidebar.ops-kafka", "查看 Kafka 工单", "share", 14},
		{"m-tickets-redis", "m-tickets", "Redis 工单", "/ops/redis", "um.sidebar.ops-redis", "查看 Redis 工单", "coin", 15},
		{"m-tickets-dcl", "m-tickets", "权限申请", "/ops/dcl", "um.sidebar.ops-dcl", "查看权限申请", "key", 16},
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
		{"m-diag-sql", "m-diag", "SQL 洞察", "/ops/sql-insight", "um.sidebar.ops-sql-insight", "查看 SQL 洞察", "search", 41},
		{"m-diag-sess", "m-diag", "会话管理", "/ops/sessions", "um.sidebar.ops-sessions", "查看会话管理", "cpu", 42},
		{"m-plat", "", "平台运维", "", "um.sidebar.platform", "查看平台运维", "grid", 50},
		{"m-plat-ml", "m-plat", "多语模版", "/ops/multilang", "um.sidebar.ops-multilang", "查看多语模版", "files", 51},
		{"m-plat-ds", "m-plat", "海豚管理", "/ops/dolphin", "um.sidebar.ops-dolphin", "查看海豚管理", "histogram", 52},
		{"m-plat-sr", "m-plat", "SR 节点", "/ops/sr", "um.sidebar.ops-sr", "查看 SR 节点", "coin", 53},
		{"m-plat-term", "m-plat", "主机终端", "/ops/terminal", "um.sidebar.ops-terminal", "查看主机终端", "monitor", 54},
		{"m-rbac", "", "用户权限", "", "um.sidebar.rbac", "查看用户权限", "lock", 55},
		{"m-cfg-users", "m-rbac", "用户管理", "/config/users", "um.sidebar.config-users", "查看用户管理", "user-filled", 56},
		{"m-cfg-roles", "m-rbac", "角色授权", "/config/roles", "um.sidebar.config-roles", "查看角色授权", "user", 57},
		{"m-cfg-menus", "m-rbac", "菜单管理", "/config/menus", "um.sidebar.config-menus", "查看菜单管理", "menu", 58},
		{"m-cfg-login-logs", "m-rbac", "登录日志", "/config/login-logs", "um.sidebar.config-login-logs", "查看登录日志", "clock", 59},
		{"m-cfg-ldap", "m-rbac", "认证配置", "/config/ldap", "um.sidebar.config-ldap", "查看认证配置", "key", 60},
		{"m-cfg-query-audits", "m-rbac", "查询审计", "/config/query-audits", "um.sidebar.config-query-audits", "查看查询审计", "search", 61},
		{"m-cfg", "", "配置中心", "", "um.sidebar.config", "查看配置中心", "setting", 2},
		{"m-db-config", "", "数据库配置", "", "um.sidebar.db-config", "查看数据库配置", "coin", 3},
		{"m-cfg-inst", "m-db-config", "实例管理", "/config/instances", "um.sidebar.config-instances", "查看实例管理", "monitor", 1},
		{"m-cfg-db", "m-db-config", "已登记库", "/config/databases", "um.sidebar.config-databases", "查看已登记库", "coin", 2},
		{"m-cfg-accounts", "m-db-config", "账号管理", "/config/accounts", "um.sidebar.config-accounts", "查看账号管理", "user", 3},
		{"m-cfg-review", "m-db-config", "审核规则", "/config/review-rules", "um.sidebar.config-review-rules", "查看审核规则", "document", 4},
		{"m-cfg-args", "m-cfg", "全局参数", "/config/args", "um.sidebar.config-args", "查看全局参数", "setting", 73},
		{"m-cfg-region", "m-cfg", "区域管理", "/config/regions", "um.sidebar.config-regions", "查看区域管理", "location", 74},
		{"m-cfg-env", "m-cfg", "环境管理", "/config/environments", "um.sidebar.config-environments", "查看环境管理", "grid", 75},
		{"m-cfg-conn", "m-cfg", "连接器", "/config/connectors", "um.sidebar.config-connectors", "查看连接器", "connection", 76},
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
		{"m-query-kafka", "m-data-query", "Kafka", "/ops/kafka-query", "um.sidebar.ops-query-kafka", "查看 Kafka", "share", 2},
		{"m-query-redis", "m-data-query", "Redis", "/ops/redis-query", "um.sidebar.ops-query-redis", "查看 Redis", "coin", 3},
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

func (d *DB) ensureDbConfigMenuTree(now string) error {
	parent := &Menu{
		ID: "m-db-config", ParentID: "", Title: "数据库配置", Path: "",
		URN: "um.sidebar.db-config", PermissionDesc: "查看数据库配置", Icon: "coin",
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
		{"m-cfg-accounts", "m-db-config", "账号管理", "/config/accounts", "um.sidebar.config-accounts", "查看账号管理", "user", 3},
		{"m-cfg-review", "m-db-config", "审核规则", "/config/review-rules", "um.sidebar.config-review-rules", "查看审核规则", "document", 4},
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

func (d *DB) ensureLoginLogsMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-login-logs", ParentID: "m-rbac", Title: "登录日志", Path: "/config/login-logs",
		URN: "um.sidebar.config-login-logs", PermissionDesc: "查看登录日志", Icon: "clock",
		SortOrder: 59, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := d.EnsureMenu(m); err != nil {
		return err
	}
	return d.grantMenuToRoleCode(m.ID, "admin")
}

func (d *DB) ensureQueryAuditsMenu(now string) error {
	m := &Menu{
		ID: "m-cfg-query-audits", ParentID: "m-rbac", Title: "查询审计", Path: "/config/query-audits",
		URN: "um.sidebar.config-query-audits", PermissionDesc: "查看查询审计", Icon: "search",
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
		URN: "um.sidebar.config-ldap", PermissionDesc: "查看认证配置", Icon: "key",
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
		{"m-cfg-users", "m-rbac", "用户管理", "/config/users", "um.sidebar.config-users", "查看用户管理", "user-filled", 56},
		{"m-cfg-roles", "m-rbac", "角色授权", "/config/roles", "um.sidebar.config-roles", "查看角色授权", "user", 57},
		{"m-cfg-menus", "m-rbac", "菜单管理", "/config/menus", "um.sidebar.config-menus", "查看菜单管理", "menu", 58},
		{"m-cfg-login-logs", "m-rbac", "登录日志", "/config/login-logs", "um.sidebar.config-login-logs", "查看登录日志", "clock", 59},
		{"m-cfg-ldap", "m-rbac", "认证配置", "/config/ldap", "um.sidebar.config-ldap", "查看认证配置", "key", 60},
		{"m-cfg-query-audits", "m-rbac", "查询审计", "/config/query-audits", "um.sidebar.config-query-audits", "查看查询审计", "search", 61},
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
