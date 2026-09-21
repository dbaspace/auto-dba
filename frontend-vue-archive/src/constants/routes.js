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
  { path: '/ops/sql-insight', title: 'SQL 洞察' },
  { path: '/ops/sessions', title: '会话管理' },
  { path: '/ops/multilang', title: '多语模版' },
  { path: '/ops/dolphin', title: '海豚管理' },
  { path: '/ops/sr', title: 'SR 节点' },
  { path: '/config/instances', title: '实例' },
  { path: '/config/regions', title: '区域管理' },
  { path: '/config/environments', title: '环境管理' },
  { path: '/config/connectors', title: '连接器' },
  { path: '/config/databases', title: '已登记库' },
  { path: '/config/accounts', title: '账号管理' },
  { path: '/config/review-rules', title: '审核规则' },
  { path: '/config/args', title: '全局参数' },
  { path: '/config/menus', title: '菜单管理' },
  { path: '/config/roles', title: '角色授权' },
  { path: '/config/users', title: '用户管理' },
  { path: '/config/login-logs', title: '登录日志' },
  { path: '/config/ldap', title: 'LDAP 配置' },
]

export function collectPaths(nodes, out = []) {
  for (const n of nodes || []) {
    if (n.path) out.push(n.path)
    if (n.children?.length) collectPaths(n.children, out)
  }
  return out
}

export function pathAllowed(path, allowed) {
  if (!path || path === '/' || path === '/403') return true
  if (path === '/ops/dashboard' || path === '/metadata/database/search/dashboard') return true
  if (path === '/ops/query' || path.startsWith('/metadata/database/search/common')) return true
  if (path === '/ops/kafka-query' || path === '/query/kafka') return true
  if (path === '/ops/redis-query' || path === '/query/redis') return true
  if (path === '/ops/dql' || path.includes('/work-order/dql')) return true
  if (path.includes('/serviceOperations/ddl-order')) return true
  if (path.includes('/serviceOperations/dml-order')) return true
  if (path.includes('/serviceOperations/createdb')) return true
  if (path === '/config/accounts' || path.includes('/manage/account')) return true
  if (path === '/config/review-rules' || path.includes('/manage/goinception')) return true
  if (path === '/config/connectors' || path.startsWith('/metadata/database/manage/connector')) return true
  if (path.startsWith('/ops/archive') || path.startsWith('/metadata/database/search/archiverManager')) return true
  if (path === '/ops/sql-insight' || path.includes('/serviceOperations/activitysnapshot')) return true
  if (path === '/ops/sessions' || path.includes('/serviceOperations/process')) return true
  if (path === '/ops/slots') return true
  if (path === '/ops/cdc' || path.startsWith('/ops/cdc/') || path.includes('/serviceOperations/cdcSyncTask')) return true
  if (path === '/ops/migration' || path.startsWith('/ops/migration/') || path.includes('/serviceOperations/migration')) return true
  if (path === '/ops/flashback' || path.startsWith('/ops/flashback/') || path.includes('/serviceOperations/flashback')) return true
  if (allowed.includes(path)) return true
  return allowed.some((p) => p && (path === p || path.startsWith(p + '/')))
}
