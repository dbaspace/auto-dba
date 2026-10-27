import React, { useMemo } from 'react'
import { Navigate, Route, Routes, useParams } from 'react-router-dom'
import Dashboard from '../views/dashboard/Dashboard'
import CdcList from '../views/ops/CdcList'
import CdcDetail from '../views/ops/CdcDetail'
import CdcCreate from '../views/ops/CdcCreate'
import TicketList from '../views/tickets/TicketList'
import TicketDetail from '../views/tickets/TicketDetail'
import DclGrants from '../views/tickets/DclGrants'
import ResourceApply from '../views/tickets/ResourceApply'
import CreateDb from '../views/tickets/CreateDb'
import DdlOrder from '../views/tickets/DdlOrder'
import DmlOrder from '../views/tickets/DmlOrder'
import DqlExport from '../views/tickets/DqlExport'
import KafkaOrder from '../views/ops/KafkaOrder'
import RedisOrder from '../views/ops/RedisOrder'
import SlotList from '../views/ops/SlotList'
import Multilang from '../views/ops/Multilang'
import DolphinOps from '../views/ops/DolphinOps'
import NacosOps from '../views/ops/NacosOps'
import SrNodes from '../views/ops/SrNodes'
import ClusterDeployHub from '../views/ops/ClusterDeployHub'
import ClusterDeployEngine from '../views/ops/ClusterDeployEngine'
import Sessions from '../views/ops/Sessions'
import MysqlSlow from '../views/ops/MysqlSlow'
import SqlInsight from '../views/ops/SqlInsight'
import CloudPostgres from '../views/ops/cloud/CloudPostgres'
import CloudPolicies from '../views/ops/cloud/CloudPolicies'
import CloudAccounts from '../views/ops/cloud/CloudAccounts'
import InstanceOverview from '../views/ops/InstanceOverview'
import DbMonitor from '../views/ops/DbMonitor'
import RedisRdb from '../views/ops/redis-rdb/RedisRdb'
import Migration from '../views/ops/Migration'
import MigrationCreate from '../views/ops/MigrationCreate'
import MigrationDetail from '../views/ops/MigrationDetail'
import MigrationSettings from '../views/ops/MigrationSettings'
import MigrationSchemaCompare from '../views/ops/MigrationSchemaCompare'
import Flashback from '../views/flashback/Flashback'
import FlashbackCloud from '../views/flashback/FlashbackCloud'
import Environments from '../views/config/Environments'
import SqlExecutors from '../views/config/SqlExecutors'
import DnsRecords from '../views/dns/DnsRecords'
import DnsZones from '../views/dns/DnsZones'
import DnsNodes from '../views/dns/DnsNodes'
import DnsChanges from '../views/dns/DnsChanges'
import DnsTools from '../views/dns/DnsTools'
import DnsInspect from '../views/dns/DnsInspect'
import Regions from '../views/config/Regions'
import Users from '../views/config/Users'
import Tenants from '../views/config/Tenants'
import Roles from '../views/config/Roles'
import GlobalArgs from '../views/config/GlobalArgs'
import SqlInsightSettings from '../views/config/SqlInsightSettings'
import LoginLogs from '../views/config/LoginLogs'
import LoginSessions from '../views/config/LoginSessions'
import QueryAudits from '../views/config/QueryAudits'
import ClientAccess from '../views/config/ClientAccess'
import MyAccessTokens from '../views/config/MyAccessTokens'
import Connectors from '../views/config/Connectors'
import MonitorTargets from '../views/config/MonitorTargets'
import FeAdmin from '../views/config/FeAdmin'
import DsAdmin from '../views/config/DsAdmin'
import NacosAdmin from '../views/config/NacosAdmin'
import ScheduledJobs from '../views/config/ScheduledJobs'
import AuthConfig from '../views/config/AuthConfig'
import ReviewRules from '../views/config/ReviewRules'
import Instances from '../views/config/Instances'
import Databases from '../views/config/Databases'
import AccessDomains from '../views/config/AccessDomains'
import Accounts from '../views/config/Accounts'
import Menus from '../views/config/Menus'
import RunHistory from '../views/archive/RunHistory'
import Masking from '../views/archive/Masking'
import Report from '../views/archive/Report'
import ArchiveDashboard from '../views/archive/Dashboard'
import ArchiveOrder from '../views/archive/ArchiveOrder'
import TaskList from '../views/archive/TaskList'
import ArchiveMonitor from '../views/archive/Monitor'
import ArchiveSettings from '../views/archive/Settings'
import KafkaQuery from '../views/query/KafkaQuery'
import RedisQuery from '../views/query/RedisQuery'
import CommonQuery from '../views/query/CommonQuery'
import Terminal from '../views/ops/Terminal'
import HaMaintain from '../views/ops/HaMaintain'
import BackupPolicies from '../views/backup/Policies'
import BackupRuns from '../views/backup/Runs'
import BackupRestore from '../views/backup/Restore'
import BackupSettings from '../views/backup/Settings'
import K8sClusters from '../views/k8s/Clusters'
import K8sOverview from '../views/k8s/Overview'
import K8sWorkloads from '../views/k8s/Workloads'
import { K8sNetwork, K8sNodes, K8sStorage } from '../views/k8s/ResourceList'
import K8sResources from '../views/k8s/Resources'
import K8sOperators from '../views/k8s/Operators'
import K8sDBClusters from '../views/k8s/DBClusters'
import K8sApps from '../views/k8s/Apps'
import PlatformAgent from '../views/config/PlatformAgent'
import OssUpload from '../views/config/OssUpload'
import CmdbAssets from '../views/cmdb/Assets'
import CmdbTopology from '../views/cmdb/Topology'
import CmdbTags from '../views/cmdb/Tags'
import CmdbDiscover from '../views/cmdb/Discover'
import CmdbImportExport from '../views/cmdb/ImportExport'
import CmdbAudit from '../views/cmdb/Audit'
import CmdbSync from '../views/cmdb/Sync'
import TodoFollowUp from '../views/todo/TodoFollowUp'
import ActiveAlerts from '../views/alerts/ActiveAlerts'
import AlertActivity from '../views/alerts/AlertActivity'
import AlertHistory from '../views/alerts/AlertHistory'
import AlertRules from '../views/alerts/AlertRules'
import AIOpsKnowledge from '../views/aiops/Knowledge'
import AiAssistant from '../views/aiops/AiAssistant'
import AIOpsIncidents from '../views/aiops/Incidents'
import AlertRoutes from '../views/alerts/AlertRoutes'
import AlertInhibitions from '../views/alerts/AlertInhibitions'
import AlertOncall from '../views/alerts/AlertOncall'
import AlertSilences from '../views/alerts/AlertSilences'
import MenuPagePlaceholder from '../views/MenuPagePlaceholder'

function RedirectWithId({ toPrefix }: { toPrefix: string }) {
  const { id } = useParams()
  return <Navigate to={`${toPrefix}/${id || ''}`} replace />
}

/** 单个 Tab 面板：用固定 location 匹配路由，隐藏时组件仍挂载保活 */
export function TabPagePanel({ pathname, search }: { pathname: string; search?: string }) {
  const loc = useMemo(
    () => ({
      pathname,
      search: search || '',
      hash: '',
      state: null as unknown,
      key: pathname + (search || ''),
    }),
    [pathname, search],
  )

  return (
    <Routes location={loc}>
      <Route path="/ops/dashboard" element={<Dashboard />} />
      <Route path="/metadata/database/search/dashboard" element={<Navigate to="/ops/dashboard" replace />} />

      <Route path="/ops/query" element={<CommonQuery />} />
      <Route path="/ops/kafka-query" element={<KafkaQuery />} />
      <Route path="/ops/redis-query" element={<RedisQuery />} />
      <Route path="/query/kafka" element={<Navigate to="/ops/kafka-query" replace />} />
      <Route path="/query/redis" element={<Navigate to="/ops/redis-query" replace />} />
      <Route path="/metadata/database/search/common/:db?/:table?" element={<Navigate to="/ops/query" replace />} />

      <Route path="/ops/tickets" element={<TicketList />} />
      <Route path="/ops/tickets/:id" element={<TicketDetail />} />
      <Route path="/ops/ddl" element={<DdlOrder />} />
      <Route path="/metadata/database/search/serviceOperations/ddl-order" element={<Navigate to="/ops/ddl" replace />} />
      <Route path="/database/search/serviceOperations/ddl-order" element={<Navigate to="/ops/ddl" replace />} />
      <Route path="/ops/dml" element={<DmlOrder />} />
      <Route path="/metadata/database/search/serviceOperations/dml-order" element={<Navigate to="/ops/dml" replace />} />
      <Route path="/database/search/serviceOperations/dml-order" element={<Navigate to="/ops/dml" replace />} />
      <Route path="/ops/dql" element={<DqlExport />} />
      <Route path="/metadata/database/work-order/dql" element={<Navigate to="/ops/dql" replace />} />
      <Route path="/database/work-order/dql" element={<Navigate to="/ops/dql" replace />} />
      <Route path="/ops/createdb" element={<CreateDb />} />
      <Route path="/metadata/database/search/serviceOperations/createdb" element={<Navigate to="/ops/createdb" replace />} />
      <Route path="/database/search/serviceOperations/createdb" element={<Navigate to="/ops/createdb" replace />} />
      <Route path="/ops/dcl" element={<DclGrants />} />
      <Route path="/ops/resource-apply" element={<ResourceApply />} />
      <Route path="/ops/kafka" element={<KafkaOrder />} />
      <Route path="/ops/redis" element={<RedisOrder />} />
      <Route path="/ops/slots" element={<SlotList />} />

      <Route path="/ops/cdc" element={<CdcList />} />
      <Route path="/ops/cdc/create" element={<CdcCreate />} />
      <Route path="/ops/cdc/:id" element={<CdcDetail />} />
      <Route path="/metadata/database/search/serviceOperations/cdcSyncTask" element={<Navigate to="/ops/cdc" replace />} />
      <Route path="/metadata/database/search/serviceOperations/cdcSyncTask/create" element={<Navigate to="/ops/cdc/create" replace />} />
      <Route path="/metadata/database/search/serviceOperations/cdcSyncTask/:id" element={<RedirectWithId toPrefix="/ops/cdc" />} />
      <Route path="/database/search/serviceOperations/cdcSyncTask" element={<Navigate to="/ops/cdc" replace />} />
      <Route path="/database/search/serviceOperations/cdcSyncTask/create" element={<Navigate to="/ops/cdc/create" replace />} />
      <Route path="/database/search/serviceOperations/cdcSyncTask/:id" element={<RedirectWithId toPrefix="/ops/cdc" />} />

      <Route path="/ops/migration" element={<Migration />} />
      <Route path="/ops/migration/create" element={<MigrationCreate />} />
      <Route path="/ops/migration/settings" element={<MigrationSettings />} />
      <Route path="/ops/migration/compare" element={<MigrationSchemaCompare />} />
      <Route path="/ops/migration/:id" element={<MigrationDetail />} />
      <Route path="/metadata/database/search/serviceOperations/migration" element={<Navigate to="/ops/migration" replace />} />
      <Route path="/metadata/database/search/serviceOperations/migration/create" element={<Navigate to="/ops/migration/create" replace />} />
      <Route path="/metadata/database/search/serviceOperations/migration/settings" element={<Navigate to="/ops/migration/settings" replace />} />
      <Route path="/metadata/database/search/serviceOperations/migration/:id" element={<RedirectWithId toPrefix="/ops/migration" />} />
      <Route path="/database/search/serviceOperations/migration" element={<Navigate to="/ops/migration" replace />} />
      <Route path="/database/search/serviceOperations/migration/create" element={<Navigate to="/ops/migration/create" replace />} />
      <Route path="/database/search/serviceOperations/migration/settings" element={<Navigate to="/ops/migration/settings" replace />} />
      <Route path="/database/search/serviceOperations/migration/:id" element={<RedirectWithId toPrefix="/ops/migration" />} />

      <Route path="/ops/flashback" element={<Navigate to="/ops/flashback/dashboard" replace />} />
      <Route path="/ops/flashback/dashboard" element={<Flashback />} />
      <Route path="/ops/flashback/tasks" element={<Flashback />} />
      <Route path="/ops/flashback/history" element={<Flashback />} />
      <Route path="/ops/flashback/history/:id" element={<Flashback />} />
      <Route path="/ops/flashback/cloud" element={<FlashbackCloud />} />
      <Route path="/metadata/database/search/serviceOperations/flashback" element={<Navigate to="/ops/flashback/dashboard" replace />} />
      <Route path="/database/search/serviceOperations/flashback" element={<Navigate to="/ops/flashback/dashboard" replace />} />

      <Route path="/ops/archive" element={<Navigate to="/ops/archive/dashboard" replace />} />
      <Route path="/ops/archive/dashboard" element={<ArchiveDashboard />} />
      <Route path="/ops/archive/apply" element={<ArchiveOrder />} />
      <Route path="/ops/archive/tasks" element={<TaskList />} />
      <Route path="/ops/archive/runs" element={<RunHistory />} />
      <Route path="/ops/archive/monitor" element={<ArchiveMonitor />} />
      <Route path="/ops/archive/settings" element={<ArchiveSettings />} />
      <Route path="/ops/archive/masking" element={<Masking />} />
      <Route path="/ops/archive/report" element={<Report />} />
      <Route path="/metadata/database/search/archiverManager" element={<Navigate to="/ops/archive/dashboard" replace />} />
      <Route path="/metadata/database/search/archiverManager/archiveDashboard" element={<Navigate to="/ops/archive/dashboard" replace />} />
      <Route path="/metadata/database/search/serviceOperations/archiverManager/archiveDashboard" element={<Navigate to="/ops/archive/dashboard" replace />} />
      <Route path="/metadata/database/search/serviceOperations/archiverManager/archiveTask" element={<Navigate to="/ops/archive/apply" replace />} />
      <Route path="/metadata/database/search/serviceOperations/archiverManager/archiverList" element={<Navigate to="/ops/archive/tasks" replace />} />
      <Route path="/metadata/database/search/serviceOperations/archiverManager/archivetaskMonitor" element={<Navigate to="/ops/archive/monitor" replace />} />
      <Route path="/metadata/database/search/serviceOperations/archiverManager/archivetaskSetting" element={<Navigate to="/ops/archive/settings" replace />} />

      <Route path="/ops/backup" element={<Navigate to="/ops/backup/policies" replace />} />
      <Route path="/ops/backup/policies" element={<BackupPolicies />} />
      <Route path="/ops/backup/runs" element={<BackupRuns />} />
      <Route path="/ops/backup/restore" element={<BackupRestore />} />
      <Route path="/ops/backup/settings" element={<BackupSettings />} />

      <Route path="/k8s/clusters" element={<K8sClusters />} />
      <Route path="/k8s/overview" element={<K8sOverview />} />
      <Route path="/k8s/nodes" element={<K8sNodes />} />
      <Route path="/k8s/workloads" element={<K8sWorkloads />} />
      <Route path="/k8s/network" element={<K8sNetwork />} />
      <Route path="/k8s/storage" element={<K8sStorage />} />
      <Route path="/k8s/resources" element={<K8sResources />} />
      <Route path="/k8s/operators" element={<K8sOperators />} />
      <Route path="/k8s/db-clusters" element={<K8sDBClusters />} />
      <Route path="/k8s/apps" element={<K8sApps />} />

      <Route path="/ops/sql-insight" element={<SqlInsight />} />
      <Route path="/ops/cloud/postgres" element={<CloudPostgres engine="postgres" />} />
      <Route path="/ops/cloud/mysql" element={<CloudPostgres engine="mysql" />} />
      <Route path="/ops/cloud/redis" element={<CloudPostgres engine="redis" />} />
      <Route path="/ops/cloud/hosts" element={<CloudPostgres engine="host" />} />
      <Route path="/ops/cloud/policies" element={<CloudPolicies />} />
      <Route path="/ops/cloud/accounts" element={<Navigate to="/config/cloud-accounts" replace />} />
      <Route path="/config/cloud-accounts" element={<CloudAccounts />} />
      <Route path="/ops/instance-overview" element={<InstanceOverview />} />
      <Route path="/ops/redis-rdb" element={<RedisRdb />} />
      <Route path="/ops/db-monitor" element={<Navigate to="/ops/db-monitor/postgres" replace />} />
      <Route path="/ops/db-monitor/postgres" element={<DbMonitor engine="postgres" />} />
      <Route path="/ops/db-monitor/mysql" element={<DbMonitor engine="mysql" />} />
      <Route path="/ops/db-monitor/redis" element={<DbMonitor engine="redis" />} />
      <Route path="/ops/db-monitor/starrocks" element={<DbMonitor engine="starrocks" />} />
      <Route path="/ops/db-monitor/clickhouse" element={<DbMonitor engine="clickhouse" />} />
      <Route path="/ops/db-monitor/kafka" element={<DbMonitor engine="kafka" />} />
      <Route path="/ops/db-monitor/tidb" element={<DbMonitor engine="tidb" />} />
      <Route path="/ops/db-monitor/cockroachdb" element={<DbMonitor engine="cockroachdb" />} />
      <Route path="/ops/db-monitor/cassandra" element={<DbMonitor engine="cassandra" />} />
      <Route path="/ops/db-monitor/oceanbase" element={<DbMonitor engine="oceanbase" />} />
      <Route path="/ops/db-monitor/host" element={<DbMonitor engine="host" />} />
      <Route path="/ops/db-monitor/mongodb" element={<DbMonitor engine="mongodb" />} />
      <Route path="/ops/db-monitor/oracle" element={<DbMonitor engine="oracle" />} />
      <Route path="/ops/db-monitor/elasticsearch" element={<DbMonitor engine="elasticsearch" />} />
      <Route path="/ops/db-monitor/sqlserver" element={<DbMonitor engine="sqlserver" />} />
      <Route path="/ops/db-monitor/debezium" element={<DbMonitor engine="debezium" />} />
      <Route path="/ops/db-monitor/dolphinscheduler" element={<DbMonitor engine="dolphinscheduler" />} />
      <Route path="/metadata/database/search/serviceOperations/activitysnapshot" element={<Navigate to="/ops/sql-insight" replace />} />
      <Route path="/database/search/serviceOperations/activitysnapshot" element={<Navigate to="/ops/sql-insight" replace />} />
      <Route path="/ops/sessions" element={<Sessions />} />
      <Route path="/ops/mysql-slow" element={<MysqlSlow />} />
      <Route path="/metadata/database/search/serviceOperations/process" element={<Navigate to="/ops/sessions" replace />} />
      <Route path="/database/search/serviceOperations/process" element={<Navigate to="/ops/sessions" replace />} />
      <Route path="/ops/multilang" element={<Multilang />} />
      <Route path="/ops/dolphin" element={<DolphinOps />} />
      <Route path="/ops/nacos" element={<NacosOps />} />
      <Route path="/ops/sr" element={<SrNodes />} />
      <Route path="/ops/cluster-deploy" element={<ClusterDeployHub />} />
      <Route path="/ops/cluster-deploy/:engine" element={<ClusterDeployEngine />} />
      <Route path="/ops/sr-cluster" element={<Navigate to="/ops/cluster-deploy/starrocks" replace />} />
      <Route path="/ops/terminal" element={<Terminal />} />
      <Route path="/ops/ha" element={<HaMaintain />} />

      <Route path="/cmdb/assets" element={<CmdbAssets />} />
      <Route path="/cmdb/topology" element={<CmdbTopology />} />
      <Route path="/cmdb/tags" element={<CmdbTags />} />
      <Route path="/cmdb/discover" element={<CmdbDiscover />} />
      <Route path="/cmdb/import-export" element={<CmdbImportExport />} />
      <Route path="/cmdb/audit" element={<CmdbAudit />} />
      <Route path="/cmdb/sync" element={<CmdbSync />} />

      <Route path="/config/instances" element={<Instances />} />
      <Route path="/config/regions" element={<Regions />} />
      <Route path="/config/environments" element={<Environments />} />
      <Route path="/config/sql-executors" element={<SqlExecutors />} />
      <Route path="/dns" element={<Navigate to="/dns/records" replace />} />
      <Route path="/dns/records" element={<DnsRecords />} />
      <Route path="/dns/zones" element={<DnsZones />} />
      <Route path="/dns/nodes" element={<DnsNodes />} />
      <Route path="/dns/changes" element={<DnsChanges />} />
      <Route path="/dns/tools" element={<DnsTools />} />
      <Route path="/dns/inspect" element={<DnsInspect />} />
      <Route path="/config/dns" element={<Navigate to="/dns/records" replace />} />
      <Route path="/config/tags" element={<Navigate to="/cmdb/tags" replace />} />
      <Route path="/config/connectors" element={<Connectors />} />
      <Route path="/config/agents" element={<PlatformAgent />} />
      <Route path="/config/oss" element={<OssUpload />} />
      <Route path="/config/monitor-targets" element={<MonitorTargets />} />
      <Route path="/config/fe-admin" element={<FeAdmin />} />
      <Route path="/config/ds-admin" element={<DsAdmin />} />
      <Route path="/config/nacos-admin" element={<NacosAdmin />} />
      <Route path="/config/scheduled-jobs" element={<ScheduledJobs />} />
      <Route path="/metadata/database/manage/connector" element={<Navigate to="/config/connectors" replace />} />
      <Route path="/config/databases" element={<Databases />} />
      <Route path="/config/domains" element={<AccessDomains />} />
      <Route path="/config/accounts" element={<Accounts />} />
      <Route path="/metadata/database/manage/account" element={<Navigate to="/config/accounts" replace />} />
      <Route path="/database/manage/account" element={<Navigate to="/config/accounts" replace />} />
      <Route path="/config/review-rules" element={<ReviewRules />} />
      <Route path="/config/sql-insight" element={<SqlInsightSettings />} />
      <Route path="/metadata/database/manage/goinception" element={<Navigate to="/config/review-rules" replace />} />
      <Route path="/database/manage/goinception" element={<Navigate to="/config/review-rules" replace />} />
      <Route path="/config/args" element={<GlobalArgs />} />
      <Route path="/config/menus" element={<Menus />} />
      <Route path="/config/roles" element={<Roles />} />
      <Route path="/config/users" element={<Users />} />
      <Route path="/config/tenants" element={<Tenants />} />
      <Route path="/config/login-logs" element={<LoginLogs />} />
      <Route path="/config/login-sessions" element={<LoginSessions />} />
      <Route path="/config/query-audits" element={<QueryAudits />} />
      <Route path="/config/client-access" element={<ClientAccess />} />
      <Route path="/config/my-access-tokens" element={<MyAccessTokens />} />
      <Route path="/config/ldap" element={<AuthConfig />} />

      <Route path="/ops/todo" element={<TodoFollowUp />} />
      <Route path="/ops/alerts" element={<ActiveAlerts />} />
      <Route path="/ops/alerts/activity" element={<AlertActivity />} />
      <Route path="/ops/alerts/history" element={<AlertHistory />} />
      <Route path="/ops/alerts/rules" element={<AlertRules />} />
      <Route path="/ops/alerts/silences" element={<AlertSilences />} />
      <Route path="/ops/alerts/routes" element={<AlertRoutes />} />
      <Route path="/ops/alerts/inhibitions" element={<AlertInhibitions />} />
      <Route path="/ops/alerts/oncall" element={<AlertOncall />} />
      <Route path="/ops/aiops/knowledge" element={<AIOpsKnowledge />} />
      <Route path="/ops/ai-assistant" element={<AiAssistant />} />
      <Route path="/ops/aiops/incidents" element={<AIOpsIncidents />} />

      <Route path="*" element={<MenuPagePlaceholder pathname={pathname} />} />
    </Routes>
  )
}
