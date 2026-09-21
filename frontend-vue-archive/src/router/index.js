import { createRouter, createWebHashHistory } from 'vue-router'
import { collectPaths, pathAllowed } from '../constants/routes'

const routes = [
  { path: '/login', name: 'Login', component: () => import('../views/Login.vue'), meta: { public: true } },
  { path: '/403', name: 'Forbidden', component: () => import('../views/Forbidden.vue'), meta: { public: true } },
  {
    path: '/',
    component: () => import('../layout/AppLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', redirect: '/ops/dashboard' },
      { path: 'ops/dashboard', name: 'Dashboard', component: () => import('../views/dashboard/Dashboard.vue'), meta: { title: '仪表盘' } },
      { path: 'metadata/database/search/dashboard', redirect: '/ops/dashboard' },
      { path: 'ops/query', name: 'Query', component: () => import('../views/query/CommonQuery.vue'), meta: { title: '数据库查询' } },
      { path: 'ops/kafka-query', name: 'KafkaQuery', component: () => import('../views/query/KafkaQuery.vue'), meta: { title: 'Kafka 查询' } },
      { path: 'ops/redis-query', name: 'RedisQuery', component: () => import('../views/query/RedisQuery.vue'), meta: { title: 'Redis 查询' } },
      { path: 'query/kafka', redirect: '/ops/kafka-query' },
      { path: 'query/redis', redirect: '/ops/redis-query' },
      { path: 'metadata/database/search/common/:db?/:table?', redirect: '/ops/query' },
      { path: 'ops/tickets', name: 'Tickets', component: () => import('../views/tickets/TicketList.vue'), meta: { title: '工单列表' } },
      { path: 'ops/tickets/:id', name: 'TicketDetail', component: () => import('../views/tickets/TicketDetail.vue'), meta: { title: '工单审核' } },
      { path: 'ops/ddl', name: 'DDL', component: () => import('../views/tickets/DdlOrder.vue'), meta: { title: 'DDL 工单' } },
      { path: 'metadata/database/search/serviceOperations/ddl-order', redirect: '/ops/ddl' },
      { path: 'database/search/serviceOperations/ddl-order', redirect: '/ops/ddl' },
      { path: 'ops/dml', name: 'DML', component: () => import('../views/tickets/DmlOrder.vue'), meta: { title: 'DML 工单' } },
      { path: 'metadata/database/search/serviceOperations/dml-order', redirect: '/ops/dml' },
      { path: 'database/search/serviceOperations/dml-order', redirect: '/ops/dml' },
      { path: 'ops/dql', name: 'DQL', component: () => import('../views/tickets/DqlExport.vue'), meta: { title: '数据导出' } },
      { path: 'metadata/database/work-order/dql', redirect: '/ops/dql' },
      { path: 'database/work-order/dql', redirect: '/ops/dql' },
      { path: 'ops/createdb', name: 'CreateDB', component: () => import('../views/tickets/CreateDb.vue'), meta: { title: '新增数据库' } },
      { path: 'metadata/database/search/serviceOperations/createdb', redirect: '/ops/createdb' },
      { path: 'database/search/serviceOperations/createdb', redirect: '/ops/createdb' },
      { path: 'ops/dcl', name: 'DCL', component: () => import('../views/tickets/DclGrants.vue'), meta: { title: '权限申请' } },
      { path: 'ops/kafka', name: 'Kafka', component: () => import('../views/ops/KafkaOrder.vue'), meta: { title: 'Kafka 工单' } },
      { path: 'ops/slots', name: 'Slots', component: () => import('../views/ops/SlotList.vue'), meta: { title: '同步列表' } },
      { path: 'ops/cdc', name: 'CDC', component: () => import('../views/ops/CdcList.vue'), meta: { title: 'CDC 同步任务' } },
      { path: 'ops/cdc/create', name: 'CDCCreate', component: () => import('../views/ops/CdcCreate.vue'), meta: { title: '创建 CDC' } },
      { path: 'ops/cdc/:id', name: 'CDCDetail', component: () => import('../views/ops/CdcDetail.vue'), meta: { title: 'CDC 任务详情' } },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask', redirect: (to) => ({ path: '/ops/cdc', query: to.query }) },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask/create', redirect: (to) => ({ path: '/ops/cdc/create', query: to.query }) },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask/:id', redirect: (to) => `/ops/cdc/${to.params.id}` },
      { path: 'database/search/serviceOperations/cdcSyncTask', redirect: (to) => ({ path: '/ops/cdc', query: to.query }) },
      { path: 'database/search/serviceOperations/cdcSyncTask/create', redirect: (to) => ({ path: '/ops/cdc/create', query: to.query }) },
      { path: 'database/search/serviceOperations/cdcSyncTask/:id', redirect: (to) => `/ops/cdc/${to.params.id}` },
      { path: 'ops/migration', name: 'Migration', component: () => import('../views/ops/Migration.vue'), meta: { title: '数据迁移' } },
      { path: 'ops/migration/create', name: 'MigrationCreate', component: () => import('../views/ops/MigrationCreate.vue'), meta: { title: '新建迁移任务' } },
      { path: 'ops/migration/settings', name: 'MigrationSettings', component: () => import('../views/ops/MigrationSettings.vue'), meta: { title: '迁移设置' } },
      { path: 'ops/migration/:id', name: 'MigrationDetail', component: () => import('../views/ops/MigrationDetail.vue'), meta: { title: '迁移任务详情' } },
      { path: 'metadata/database/search/serviceOperations/migration', redirect: '/ops/migration' },
      { path: 'metadata/database/search/serviceOperations/migration/create', redirect: '/ops/migration/create' },
      { path: 'metadata/database/search/serviceOperations/migration/settings', redirect: '/ops/migration/settings' },
      { path: 'metadata/database/search/serviceOperations/migration/:id', redirect: (to) => `/ops/migration/${to.params.id}` },
      { path: 'database/search/serviceOperations/migration', redirect: '/ops/migration' },
      { path: 'database/search/serviceOperations/migration/create', redirect: '/ops/migration/create' },
      { path: 'database/search/serviceOperations/migration/settings', redirect: '/ops/migration/settings' },
      { path: 'database/search/serviceOperations/migration/:id', redirect: (to) => `/ops/migration/${to.params.id}` },
      { path: 'ops/flashback', redirect: '/ops/flashback/dashboard' },
      { path: 'ops/flashback/dashboard', name: 'FlashbackDash', component: () => import('../views/ops/Flashback.vue'), meta: { title: '闪回仪表盘' } },
      { path: 'ops/flashback/tasks', name: 'FlashbackTasks', component: () => import('../views/ops/Flashback.vue'), meta: { title: '闪回任务' } },
      { path: 'ops/flashback/history', name: 'FlashbackHistory', component: () => import('../views/ops/Flashback.vue'), meta: { title: '闪回历史' } },
      { path: 'ops/flashback/history/:id', name: 'FlashbackTask', component: () => import('../views/ops/Flashback.vue'), meta: { title: '闪回任务详情' } },
      { path: 'ops/flashback/cloud', name: 'FlashbackCloud', component: () => import('../views/ops/FlashbackCloud.vue'), meta: { title: '闪回多云密钥' } },
      { path: 'metadata/database/search/serviceOperations/flashback', redirect: '/ops/flashback/dashboard' },
      { path: 'database/search/serviceOperations/flashback', redirect: '/ops/flashback/dashboard' },
      { path: 'ops/archive', redirect: '/ops/archive/dashboard' },
      { path: 'ops/archive/dashboard', name: 'ArchiveDash', component: () => import('../views/archive/Dashboard.vue'), meta: { title: '归档控制台' } },
      { path: 'ops/archive/apply', name: 'ArchiveOrder', component: () => import('../views/archive/ArchiveOrder.vue'), meta: { title: '归档任务' } },
      { path: 'ops/archive/tasks', name: 'ArchiveTasks', component: () => import('../views/archive/TaskList.vue'), meta: { title: '任务管理' } },
      { path: 'ops/archive/runs', name: 'ArchiveRuns', component: () => import('../views/archive/RunHistory.vue'), meta: { title: '归档执行记录' } },
      { path: 'ops/archive/monitor', name: 'ArchiveMonitor', component: () => import('../views/archive/Monitor.vue'), meta: { title: '归档监控' } },
      { path: 'ops/archive/settings', name: 'ArchiveSettings', component: () => import('../views/archive/Settings.vue'), meta: { title: '归档设置' } },
      { path: 'ops/archive/masking', name: 'ArchiveMasking', component: () => import('../views/archive/Masking.vue'), meta: { title: '敏感数据脱敏' } },
      { path: 'ops/archive/report', name: 'ArchiveReport', component: () => import('../views/archive/Report.vue'), meta: { title: '归档报表' } },
      { path: 'metadata/database/search/archiverManager', redirect: '/ops/archive/dashboard' },
      { path: 'metadata/database/search/archiverManager/archiveDashboard', redirect: '/ops/archive/dashboard' },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiveDashboard', redirect: '/ops/archive/dashboard' },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiveTask', redirect: '/ops/archive/apply' },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiverList', redirect: '/ops/archive/tasks' },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archivetaskMonitor', redirect: '/ops/archive/monitor' },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archivetaskSetting', redirect: '/ops/archive/settings' },
      { path: 'ops/sql-insight', name: 'Insight', component: () => import('../views/ops/SqlInsight.vue'), meta: { title: 'SQL 洞察' } },
      { path: 'metadata/database/search/serviceOperations/activitysnapshot', redirect: '/ops/sql-insight' },
      { path: 'database/search/serviceOperations/activitysnapshot', redirect: '/ops/sql-insight' },
      { path: 'ops/sessions', name: 'Sessions', component: () => import('../views/ops/Sessions.vue'), meta: { title: '会话管理' } },
      { path: 'metadata/database/search/serviceOperations/process', redirect: '/ops/sessions' },
      { path: 'database/search/serviceOperations/process', redirect: '/ops/sessions' },
      { path: 'ops/multilang', name: 'Multilang', component: () => import('../views/ops/Multilang.vue'), meta: { title: '多语模版' } },
      { path: 'ops/dolphin', name: 'Dolphin', component: () => import('../views/ops/ExternalStatus.vue'), props: { kind: 'dolphin' }, meta: { title: '海豚管理' } },
      { path: 'ops/sr', name: 'SR', component: () => import('../views/ops/ExternalStatus.vue'), props: { kind: 'sr' }, meta: { title: 'SR 节点' } },
      { path: 'config/instances', name: 'Instances', component: () => import('../views/config/Instances.vue'), meta: { title: '实例' } },
      { path: 'config/regions', name: 'Regions', component: () => import('../views/config/Regions.vue'), meta: { title: '区域管理' } },
      { path: 'config/environments', name: 'Environments', component: () => import('../views/config/Environments.vue'), meta: { title: '环境管理' } },
      { path: 'config/connectors', name: 'Connectors', component: () => import('../views/config/Connectors.vue'), meta: { title: '连接器' } },
      { path: 'metadata/database/manage/connector', redirect: '/config/connectors' },
      { path: 'config/databases', name: 'Databases', component: () => import('../views/config/Databases.vue'), meta: { title: '已登记库' } },
      { path: 'config/accounts', name: 'Accounts', component: () => import('../views/config/Accounts.vue'), meta: { title: '账号管理' } },
      { path: 'metadata/database/manage/account', redirect: '/config/accounts' },
      { path: 'database/manage/account', redirect: '/config/accounts' },
      { path: 'config/review-rules', name: 'ReviewRules', component: () => import('../views/config/ReviewRules.vue'), meta: { title: '审核规则' } },
      { path: 'metadata/database/manage/goinception', redirect: '/config/review-rules' },
      { path: 'database/manage/goinception', redirect: '/config/review-rules' },
      { path: 'config/args', name: 'Args', component: () => import('../views/config/GlobalArgs.vue'), meta: { title: '全局参数' } },
      { path: 'config/menus', name: 'Menus', component: () => import('../views/config/Menus.vue'), meta: { title: '菜单管理' } },
      { path: 'config/roles', name: 'Roles', component: () => import('../views/config/Roles.vue'), meta: { title: '角色授权' } },
      { path: 'config/users', name: 'Users', component: () => import('../views/config/Users.vue'), meta: { title: '用户管理' } },
      { path: 'config/login-logs', name: 'LoginLogs', component: () => import('../views/config/LoginLogs.vue'), meta: { title: '登录日志' } },
      { path: 'config/ldap', name: 'Ldap', component: () => import('../views/config/Ldap.vue'), meta: { title: 'LDAP 配置' } },
    ],
  },
]

const router = createRouter({ history: createWebHashHistory(), routes })

function allowedPaths() {
  try {
    const u = JSON.parse(localStorage.getItem('user') || '{}')
    return collectPaths(u.menus || [])
  } catch {
    return []
  }
}

function firstAllowed() {
  const paths = allowedPaths()
  if (paths.includes('/ops/dashboard')) return '/ops/dashboard'
  return paths[0] || '/ops/dashboard'
}

router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (to.meta.public) {
    if (to.path === '/login' && token) return { path: firstAllowed() }
    return true
  }
  if (!token) return { path: '/login', query: { redirect: to.fullPath } }
  if (to.path === '/') return { path: firstAllowed() }
  const allowed = allowedPaths()
  if (allowed.length && !pathAllowed(to.path, allowed)) {
    return { path: '/403' }
  }
})

export default router
