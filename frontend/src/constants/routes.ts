import type { MenuNode } from '../store/authSlice'

export const PAGE_ROUTES = [
  { path: '/ops/dashboard', title: '仪表盘' },
  { path: '/ops/query', title: '数据库查询' },
  { path: '/ops/kafka-query', title: 'Kafka 查询' },
  { path: '/ops/redis-query', title: 'Redis 查询' },
  { path: '/ops/tickets', title: '工单列表' },
  { path: '/ops/ddl', title: 'DDL 工单' },
  { path: '/ops/dml', title: 'DML 工单' },
  { path: '/ops/dql', title: '数据导出' },
  { path: '/ops/kafka', title: 'Kafka 工单' },
  { path: '/ops/dcl', title: '权限申请' },
  { path: '/ops/createdb', title: '新增数据库' },
  { path: '/ops/resource-apply', title: '平台资源申请' },
  { path: '/ops/slots', title: '同步列表' },
  { path: '/ops/cdc', title: 'CDC 同步任务' },
  { path: '/ops/cdc/create', title: '创建 CDC' },
  { path: '/ops/migration', title: '数据迁移' },
  { path: '/ops/migration/create', title: '新建迁移任务' },
  { path: '/ops/migration/settings', title: '迁移设置' },
  { path: '/ops/flashback', title: '数据闪回' },
  { path: '/ops/flashback/dashboard', title: '闪回仪表盘' },
  { path: '/ops/flashback/tasks', title: '闪回任务' },
  { path: '/ops/flashback/history', title: '闪回历史' },
  { path: '/ops/flashback/cloud', title: '闪回多云密钥' },
  { path: '/ops/archive', title: '归档管理' },
  { path: '/ops/archive/dashboard', title: '归档控制台' },
  { path: '/ops/archive/tasks', title: '归档任务管理' },
  { path: '/ops/archive/runs', title: '归档执行记录' },
  { path: '/ops/archive/monitor', title: '归档监控告警' },
  { path: '/ops/archive/settings', title: '归档系统设置' },
  { path: '/ops/archive/masking', title: '敏感数据脱敏' },
  { path: '/ops/archive/report', title: '归档报表' },
  { path: '/ops/backup', title: '数据备份' },
  { path: '/ops/backup/policies', title: '备份策略' },
  { path: '/ops/backup/runs', title: '备份执行记录' },
  { path: '/ops/backup/restore', title: '备份恢复' },
  { path: '/ops/backup/settings', title: '备份存储' },
  { path: '/k8s/clusters', title: 'K8s 集群管理' },
  { path: '/k8s/overview', title: 'K8s 集群概览' },
  { path: '/k8s/nodes', title: 'K8s 节点' },
  { path: '/k8s/workloads', title: 'K8s 工作负载' },
  { path: '/k8s/network', title: 'K8s 网络' },
  { path: '/k8s/storage', title: 'K8s 存储' },
  { path: '/k8s/resources', title: 'K8s 资源浏览器' },
  { path: '/k8s/operators', title: 'K8s Operator' },
  { path: '/k8s/db-clusters', title: 'K8s 数据库服务' },
  { path: '/k8s/apps', title: 'K8s Helm 应用' },
  { path: '/ops/sql-insight', title: 'SQL 洞察' },
  { path: '/ops/cloud/postgres', title: '云 PostgreSQL' },
  { path: '/ops/cloud/mysql', title: '云 MySQL' },
  { path: '/ops/cloud/redis', title: '云 Redis' },
  { path: '/ops/cloud/hosts', title: '云主机' },
  { path: '/ops/cloud/policies', title: '云自愈策略' },
  { path: '/config/cloud-accounts', title: '云账号' },
  { path: '/ops/cloud/accounts', title: '云账号' },
  { path: '/ops/instance-overview', title: '实例总览' },
  { path: '/ops/redis-rdb', title: 'Redis 内存分析' },
  { path: '/ops/db-monitor/postgres', title: 'Postgres监控' },
  { path: '/ops/db-monitor/mysql', title: 'MySQL监控' },
  { path: '/ops/db-monitor/redis', title: 'Redis监控' },
  { path: '/ops/db-monitor/starrocks', title: 'Starrocks监控' },
  { path: '/ops/db-monitor/clickhouse', title: 'ClickHouse监控' },
  { path: '/ops/db-monitor/kafka', title: 'Kafka监控' },
  { path: '/ops/db-monitor/tidb', title: 'TiDB监控' },
  { path: '/ops/db-monitor/cockroachdb', title: 'CockroachDB监控' },
  { path: '/ops/db-monitor/cassandra', title: 'Cassandra监控' },
  { path: '/ops/db-monitor/oceanbase', title: 'OceanBase监控' },
  { path: '/ops/db-monitor/host', title: '主机监控' },
  { path: '/ops/db-monitor/mongodb', title: 'MongoDB监控' },
  { path: '/ops/db-monitor/oracle', title: 'Oracle监控' },
  { path: '/ops/db-monitor/elasticsearch', title: 'ES监控' },
  { path: '/ops/db-monitor/sqlserver', title: 'SQLServer监控' },
  { path: '/ops/db-monitor/debezium', title: 'Debezium监控' },
  { path: '/ops/db-monitor/dolphinscheduler', title: '海豚监控' },
  { path: '/ops/sessions', title: '会话管理' },
  { path: '/ops/multilang', title: '多语模版' },
  { path: '/ops/dolphin', title: '海豚管理' },
  { path: '/ops/nacos', title: 'Nacos 管理' },
  { path: '/ops/sr', title: 'SR控制台' },
  { path: '/ops/cluster-deploy', title: '集群部署' },
  { path: '/ops/cluster-deploy/haproxy', title: 'HAProxy 部署' },
  { path: '/ops/cluster-deploy/nginx', title: 'Nginx 部署' },
  { path: '/ops/cluster-deploy/lvs', title: 'LVS 部署' },
  { path: '/ops/sr-cluster', title: 'StarRocks 部署' },
  { path: '/ops/terminal', title: '主机终端' },
  { path: '/ops/ha', title: '高可用服务维护' },
  { path: '/ops/todo', title: '待办跟进' },
  { path: '/ops/alerts', title: '活跃告警' },
  { path: '/ops/alerts/activity', title: '实时动态' },
  { path: '/ops/alerts/history', title: '历史告警' },
  { path: '/ops/alerts/rules', title: '规则管理' },
  { path: '/ops/alerts/silences', title: '静默管理' },
  { path: '/ops/alerts/routes', title: '通知路由' },
  { path: '/ops/alerts/inhibitions', title: '抑制规则' },
  { path: '/ops/alerts/oncall', title: '值班排班' },
  { path: '/cmdb/assets', title: '资产目录' },
  { path: '/cmdb/topology', title: '关系拓扑' },
  { path: '/cmdb/tags', title: 'CMDB 标签' },
  { path: '/cmdb/discover', title: '发现登记' },
  { path: '/cmdb/import-export', title: '导入导出' },
  { path: '/cmdb/audit', title: '变更审计' },
  { path: '/cmdb/sync', title: '同步任务' },
  { path: '/config/instances', title: '实例' },
  { path: '/config/regions', title: '区域管理' },
  { path: '/config/environments', title: '环境管理' },
  { path: '/dns/records', title: '解析记录' },
  { path: '/dns/zones', title: '解析域与范围' },
  { path: '/dns/nodes', title: '解析节点' },
  { path: '/dns/changes', title: 'DNS 变更记录' },
  { path: '/dns/tools', title: '解析测试' },
  { path: '/dns/inspect', title: '解析体检' },
  { path: '/config/tags', title: '标签管理' },
  { path: '/config/connectors', title: '连接器' },
  { path: '/config/agents', title: '平台 Agent' },
  { path: '/config/oss', title: '对象存储' },
  { path: '/config/monitor-targets', title: '监控目标注册' },
  { path: '/config/fe-admin', title: 'FE管理' },
  { path: '/config/ds-admin', title: '海豚配置' },
  { path: '/config/nacos-admin', title: 'Nacos 配置' },
  { path: '/config/scheduled-jobs', title: '定时任务' },
  { path: '/config/databases', title: '已登记库' },
  { path: '/config/domains', title: '域名管理' },
  { path: '/config/accounts', title: '账号管理' },
  { path: '/config/review-rules', title: '审核规则' },
  { path: '/config/sql-insight', title: 'SQL 洞察设置' },
  { path: '/config/args', title: '全局参数' },
  { path: '/config/menus', title: '菜单管理' },
  { path: '/config/roles', title: '角色授权' },
  { path: '/config/users', title: '用户管理' },
  { path: '/config/tenants', title: '租户管理' },
  { path: '/config/login-logs', title: '登录日志' },
  { path: '/config/login-sessions', title: '登录会话' },
  { path: '/config/query-audits', title: '查询审计' },
  { path: '/config/client-access', title: '客户端访问授权' },
  { path: '/config/my-access-tokens', title: '我的访问令牌' },
  { path: '/config/ldap', title: '认证配置' },
]

export const CLUSTER_DEPLOY_ENGINE_LABELS: Record<string, string> = {
  haproxy: 'HAProxy', nginx: 'Nginx', lvs: 'LVS',
  starrocks: 'StarRocks', clickhouse: 'ClickHouse',
  mysql: 'MySQL', postgres: 'PostgreSQL', oceanbase: 'OceanBase', oracle: 'Oracle', sqlserver: 'SQL Server',
  tidb: 'TiDB', cockroachdb: 'CockroachDB',
  redis: 'Redis', mongodb: 'MongoDB', cassandra: 'Cassandra', elasticsearch: 'Elasticsearch',
  kafka: 'Kafka',
}

/** 集群部署引擎工作台 /ops/cluster-deploy/<engine> 的 Tab 名，如「StarRocks 部署」；非引擎页返回空串 */
export function clusterDeployEngineTitle(path: string): string {
  const m = /^\/ops\/cluster-deploy\/([^/]+)\/?$/.exec(path || '')
  if (!m) return ''
  const id = m[1].toLowerCase()
  const label = CLUSTER_DEPLOY_ENGINE_LABELS[id] || id.charAt(0).toUpperCase() + id.slice(1)
  return `${label} 部署`
}

export function collectPaths(nodes: MenuNode[] | undefined, out: string[] = []): string[] {
  for (const n of nodes || []) {
    if (n.path) out.push(n.path)
    if (n.children?.length) collectPaths(n.children, out)
  }
  return out
}

export function pathAllowed(path: string, allowed: string[]): boolean {
  if (!path || path === '/' || path === '/403') return true
  if (path === '/ops/dashboard' || path === '/metadata/database/search/dashboard') return true
  if (path === '/ops/query' || path.startsWith('/metadata/database/search/common')) return true
  if (path === '/ops/kafka-query' || path === '/query/kafka') return true
  if (path === '/ops/redis-query' || path === '/query/redis') return true
  if (path === '/ops/dql' || path.includes('/work-order/dql')) return true
  if (path === '/ops/resource-apply') return true
  if (path.includes('/serviceOperations/ddl-order')) return true
  if (path.includes('/serviceOperations/dml-order')) return true
  if (path.includes('/serviceOperations/createdb')) return true
  if (path === '/config/accounts' || path.includes('/manage/account')) return true
  if (path === '/config/review-rules' || path.includes('/manage/goinception')) return true
  if (path === '/config/connectors' || path.startsWith('/metadata/database/manage/connector')) return true
  if (path === '/config/monitor-targets' || path === '/config/fe-admin' || path === '/config/ds-admin' || path === '/config/nacos-admin') return true
  if (path.startsWith('/ops/archive') || path.startsWith('/metadata/database/search/archiverManager')) return true
  if (path.startsWith('/ops/backup')) return true
  if (path.startsWith('/k8s/')) return true
  if (path === '/ops/sql-insight' || path.includes('/serviceOperations/activitysnapshot')) return true
  if (path === '/ops/redis-rdb') return true
  if (path === '/ops/db-monitor' || path.startsWith('/ops/db-monitor/')) return true
  if (path === '/ops/sessions' || path.includes('/serviceOperations/process')) return true
  if (path === '/ops/slots') return true
  if (path === '/ops/cdc' || path.startsWith('/ops/cdc/') || path.includes('/serviceOperations/cdcSyncTask')) return true
  if (path === '/ops/migration' || path.startsWith('/ops/migration/') || path.includes('/serviceOperations/migration')) return true
  if (path === '/ops/flashback' || path.startsWith('/ops/flashback/') || path.includes('/serviceOperations/flashback')) return true
  if (path === '/ops/cluster-deploy' || path.startsWith('/ops/cluster-deploy/') || path === '/ops/sr-cluster' || path.startsWith('/ops/sr-cluster/')) return true
  if (allowed.includes(path)) return true
  return allowed.some((p) => p && (path === p || path.startsWith(p + '/')))
}

export function firstAllowed(allowed: string[]): string {
  if (allowed.includes('/ops/dashboard')) return '/ops/dashboard'
  return allowed[0] || '/ops/dashboard'
}
