import React from 'react'
import { createHashRouter, Navigate, useParams } from 'react-router-dom'
import AppLayout from '../layout/AppLayout'
import Login from '../views/Login'
import SelectTenant from '../views/SelectTenant'
import ForgotPassword from '../views/ForgotPassword'
import ResetPassword from '../views/ResetPassword'
import Forbidden from '../views/Forbidden'
import RequireAuth from './RequireAuth'
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
import SrNodes from '../views/ops/SrNodes'
import Sessions from '../views/ops/Sessions'
import SqlInsight from '../views/ops/SqlInsight'
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
import Regions from '../views/config/Regions'
import Users from '../views/config/Users'
import Roles from '../views/config/Roles'
import GlobalArgs from '../views/config/GlobalArgs'
import SqlInsightSettings from '../views/config/SqlInsightSettings'
import LoginLogs from '../views/config/LoginLogs'
import LoginSessions from '../views/config/LoginSessions'
import QueryAudits from '../views/config/QueryAudits'
import Connectors from '../views/config/Connectors'
import MonitorTargets from '../views/config/MonitorTargets'
import FeAdmin from '../views/config/FeAdmin'
import DsAdmin from '../views/config/DsAdmin'
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
import ClusterDeployHub from '../views/ops/ClusterDeployHub'
import ClusterDeployEngine from '../views/ops/ClusterDeployEngine'
import BackupPolicies from '../views/backup/Policies'
import BackupRuns from '../views/backup/Runs'
import BackupSettings from '../views/backup/Settings'
import PlatformAgent from '../views/config/PlatformAgent'
import OssUpload from '../views/config/OssUpload'
import CmdbAssets from '../views/cmdb/Assets'
import CmdbTopology from '../views/cmdb/Topology'
import CmdbTags from '../views/cmdb/Tags'
import CmdbDiscover from '../views/cmdb/Discover'
import CmdbImportExport from '../views/cmdb/ImportExport'
import CmdbAudit from '../views/cmdb/Audit'
import CmdbSync from '../views/cmdb/Sync'

/** 旧路径带 :id 时转发到新路径 */
function RedirectWithId({ toPrefix }: { toPrefix: string }) {
  const { id } = useParams()
  return <Navigate to={`${toPrefix}/${id || ''}`} replace />
}

const router = createHashRouter([
  { path: '/login', element: <Login /> },
  { path: '/select-tenant', element: <SelectTenant /> },
  { path: '/forgot-password', element: <ForgotPassword /> },
  { path: '/reset-password', element: <ResetPassword /> },
  { path: '/403', element: <Forbidden /> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <AppLayout />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <Navigate to="/ops/dashboard" replace /> },
      { path: 'ops/dashboard', element: <Dashboard /> },
      { path: 'metadata/database/search/dashboard', element: <Navigate to="/ops/dashboard" replace /> },

      { path: 'ops/query', element: <CommonQuery /> },
      { path: 'ops/kafka-query', element: <KafkaQuery /> },
      { path: 'ops/redis-query', element: <RedisQuery /> },
      { path: 'query/kafka', element: <Navigate to="/ops/kafka-query" replace /> },
      { path: 'query/redis', element: <Navigate to="/ops/redis-query" replace /> },
      { path: 'metadata/database/search/common/:db?/:table?', element: <Navigate to="/ops/query" replace /> },

      { path: 'ops/tickets', element: <TicketList /> },
      { path: 'ops/tickets/:id', element: <TicketDetail /> },
      { path: 'ops/ddl', element: <DdlOrder /> },
      { path: 'metadata/database/search/serviceOperations/ddl-order', element: <Navigate to="/ops/ddl" replace /> },
      { path: 'database/search/serviceOperations/ddl-order', element: <Navigate to="/ops/ddl" replace /> },
      { path: 'ops/dml', element: <DmlOrder /> },
      { path: 'metadata/database/search/serviceOperations/dml-order', element: <Navigate to="/ops/dml" replace /> },
      { path: 'database/search/serviceOperations/dml-order', element: <Navigate to="/ops/dml" replace /> },
      { path: 'ops/dql', element: <DqlExport /> },
      { path: 'metadata/database/work-order/dql', element: <Navigate to="/ops/dql" replace /> },
      { path: 'database/work-order/dql', element: <Navigate to="/ops/dql" replace /> },
      { path: 'ops/createdb', element: <CreateDb /> },
      { path: 'metadata/database/search/serviceOperations/createdb', element: <Navigate to="/ops/createdb" replace /> },
      { path: 'database/search/serviceOperations/createdb', element: <Navigate to="/ops/createdb" replace /> },
      { path: 'ops/dcl', element: <DclGrants /> },
      { path: 'ops/resource-apply', element: <ResourceApply /> },
      { path: 'ops/kafka', element: <KafkaOrder /> },
      { path: 'ops/redis', element: <RedisOrder /> },
      { path: 'ops/slots', element: <SlotList /> },

      { path: 'ops/cdc', element: <CdcList /> },
      { path: 'ops/cdc/create', element: <CdcCreate /> },
      { path: 'ops/cdc/:id', element: <CdcDetail /> },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask', element: <Navigate to="/ops/cdc" replace /> },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask/create', element: <Navigate to="/ops/cdc/create" replace /> },
      { path: 'metadata/database/search/serviceOperations/cdcSyncTask/:id', element: <RedirectWithId toPrefix="/ops/cdc" /> },
      { path: 'database/search/serviceOperations/cdcSyncTask', element: <Navigate to="/ops/cdc" replace /> },
      { path: 'database/search/serviceOperations/cdcSyncTask/create', element: <Navigate to="/ops/cdc/create" replace /> },
      { path: 'database/search/serviceOperations/cdcSyncTask/:id', element: <RedirectWithId toPrefix="/ops/cdc" /> },

      { path: 'ops/migration', element: <Migration /> },
      { path: 'ops/migration/create', element: <MigrationCreate /> },
      { path: 'ops/migration/settings', element: <MigrationSettings /> },
      { path: 'ops/migration/compare', element: <MigrationSchemaCompare /> },
      { path: 'ops/migration/:id', element: <MigrationDetail /> },
      { path: 'metadata/database/search/serviceOperations/migration', element: <Navigate to="/ops/migration" replace /> },
      { path: 'metadata/database/search/serviceOperations/migration/create', element: <Navigate to="/ops/migration/create" replace /> },
      { path: 'metadata/database/search/serviceOperations/migration/settings', element: <Navigate to="/ops/migration/settings" replace /> },
      { path: 'metadata/database/search/serviceOperations/migration/:id', element: <RedirectWithId toPrefix="/ops/migration" /> },
      { path: 'database/search/serviceOperations/migration', element: <Navigate to="/ops/migration" replace /> },
      { path: 'database/search/serviceOperations/migration/create', element: <Navigate to="/ops/migration/create" replace /> },
      { path: 'database/search/serviceOperations/migration/settings', element: <Navigate to="/ops/migration/settings" replace /> },
      { path: 'database/search/serviceOperations/migration/:id', element: <RedirectWithId toPrefix="/ops/migration" /> },

      { path: 'ops/flashback', element: <Navigate to="/ops/flashback/dashboard" replace /> },
      { path: 'ops/flashback/dashboard', element: <Flashback /> },
      { path: 'ops/flashback/tasks', element: <Flashback /> },
      { path: 'ops/flashback/history', element: <Flashback /> },
      { path: 'ops/flashback/history/:id', element: <Flashback /> },
      { path: 'ops/flashback/cloud', element: <FlashbackCloud /> },
      { path: 'metadata/database/search/serviceOperations/flashback', element: <Navigate to="/ops/flashback/dashboard" replace /> },
      { path: 'database/search/serviceOperations/flashback', element: <Navigate to="/ops/flashback/dashboard" replace /> },

      { path: 'ops/archive', element: <Navigate to="/ops/archive/dashboard" replace /> },
      { path: 'ops/archive/dashboard', element: <ArchiveDashboard /> },
      { path: 'ops/archive/apply', element: <ArchiveOrder /> },
      { path: 'ops/archive/tasks', element: <TaskList /> },
      { path: 'ops/archive/runs', element: <RunHistory /> },
      { path: 'ops/archive/monitor', element: <ArchiveMonitor /> },
      { path: 'ops/archive/settings', element: <ArchiveSettings /> },
      { path: 'ops/archive/masking', element: <Masking /> },
      { path: 'ops/archive/report', element: <Report /> },
      { path: 'metadata/database/search/archiverManager', element: <Navigate to="/ops/archive/dashboard" replace /> },
      { path: 'metadata/database/search/archiverManager/archiveDashboard', element: <Navigate to="/ops/archive/dashboard" replace /> },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiveDashboard', element: <Navigate to="/ops/archive/dashboard" replace /> },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiveTask', element: <Navigate to="/ops/archive/apply" replace /> },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archiverList', element: <Navigate to="/ops/archive/tasks" replace /> },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archivetaskMonitor', element: <Navigate to="/ops/archive/monitor" replace /> },
      { path: 'metadata/database/search/serviceOperations/archiverManager/archivetaskSetting', element: <Navigate to="/ops/archive/settings" replace /> },

      { path: 'ops/backup', element: <Navigate to="/ops/backup/policies" replace /> },
      { path: 'ops/backup/policies', element: <BackupPolicies /> },
      { path: 'ops/backup/runs', element: <BackupRuns /> },
      { path: 'ops/backup/settings', element: <BackupSettings /> },

      { path: 'ops/sql-insight', element: <SqlInsight /> },
      { path: 'ops/redis-rdb', element: <RedisRdb /> },
      { path: 'ops/db-monitor', element: <Navigate to="/ops/db-monitor/postgres" replace /> },
      { path: 'ops/db-monitor/postgres', element: <DbMonitor engine="postgres" /> },
      { path: 'ops/db-monitor/mysql', element: <DbMonitor engine="mysql" /> },
      { path: 'ops/db-monitor/redis', element: <DbMonitor engine="redis" /> },
      { path: 'ops/db-monitor/starrocks', element: <DbMonitor engine="starrocks" /> },
      { path: 'ops/db-monitor/clickhouse', element: <DbMonitor engine="clickhouse" /> },
      { path: 'ops/db-monitor/kafka', element: <DbMonitor engine="kafka" /> },
      { path: 'ops/db-monitor/tidb', element: <DbMonitor engine="tidb" /> },
      { path: 'ops/db-monitor/cockroachdb', element: <DbMonitor engine="cockroachdb" /> },
      { path: 'ops/db-monitor/cassandra', element: <DbMonitor engine="cassandra" /> },
      { path: 'ops/db-monitor/oceanbase', element: <DbMonitor engine="oceanbase" /> },
      { path: 'ops/db-monitor/host', element: <DbMonitor engine="host" /> },
      { path: 'ops/db-monitor/mongodb', element: <DbMonitor engine="mongodb" /> },
      { path: 'ops/db-monitor/oracle', element: <DbMonitor engine="oracle" /> },
      { path: 'ops/db-monitor/elasticsearch', element: <DbMonitor engine="elasticsearch" /> },
      { path: 'ops/db-monitor/sqlserver', element: <DbMonitor engine="sqlserver" /> },
      { path: 'ops/db-monitor/debezium', element: <DbMonitor engine="debezium" /> },
      { path: 'ops/db-monitor/dolphinscheduler', element: <DbMonitor engine="dolphinscheduler" /> },
      { path: 'metadata/database/search/serviceOperations/activitysnapshot', element: <Navigate to="/ops/sql-insight" replace /> },
      { path: 'database/search/serviceOperations/activitysnapshot', element: <Navigate to="/ops/sql-insight" replace /> },
      { path: 'ops/sessions', element: <Sessions /> },
      { path: 'metadata/database/search/serviceOperations/process', element: <Navigate to="/ops/sessions" replace /> },
      { path: 'database/search/serviceOperations/process', element: <Navigate to="/ops/sessions" replace /> },
      { path: 'ops/multilang', element: <Multilang /> },
      { path: 'ops/dolphin', element: <DolphinOps /> },
      { path: 'ops/sr', element: <SrNodes /> },
      { path: 'ops/cluster-deploy', element: <ClusterDeployHub /> },
      { path: 'ops/cluster-deploy/:engine', element: <ClusterDeployEngine /> },
      { path: 'ops/sr-cluster', element: <Navigate to="/ops/cluster-deploy/starrocks" replace /> },
      { path: 'ops/terminal', element: <Terminal /> },
      { path: 'ops/ha', element: <HaMaintain /> },

      { path: 'cmdb/assets', element: <CmdbAssets /> },
      { path: 'cmdb/topology', element: <CmdbTopology /> },
      { path: 'cmdb/tags', element: <CmdbTags /> },
      { path: 'cmdb/discover', element: <CmdbDiscover /> },
      { path: 'cmdb/import-export', element: <CmdbImportExport /> },
      { path: 'cmdb/audit', element: <CmdbAudit /> },
      { path: 'cmdb/sync', element: <CmdbSync /> },

      { path: 'config/instances', element: <Instances /> },
      { path: 'config/regions', element: <Regions /> },
      { path: 'config/environments', element: <Environments /> },
      { path: 'config/tags', element: <Navigate to="/cmdb/tags" replace /> },
      { path: 'config/connectors', element: <Connectors /> },
      { path: 'config/agents', element: <PlatformAgent /> },
      { path: 'config/oss', element: <OssUpload /> },
      { path: 'config/monitor-targets', element: <MonitorTargets /> },
      { path: 'config/fe-admin', element: <FeAdmin /> },
      { path: 'config/ds-admin', element: <DsAdmin /> },
      { path: 'metadata/database/manage/connector', element: <Navigate to="/config/connectors" replace /> },
      { path: 'config/databases', element: <Databases /> },
      { path: 'config/domains', element: <AccessDomains /> },
      { path: 'config/accounts', element: <Accounts /> },
      { path: 'metadata/database/manage/account', element: <Navigate to="/config/accounts" replace /> },
      { path: 'database/manage/account', element: <Navigate to="/config/accounts" replace /> },
      { path: 'config/review-rules', element: <ReviewRules /> },
      { path: 'config/sql-insight', element: <SqlInsightSettings /> },
      { path: 'metadata/database/manage/goinception', element: <Navigate to="/config/review-rules" replace /> },
      { path: 'database/manage/goinception', element: <Navigate to="/config/review-rules" replace /> },
      { path: 'config/args', element: <GlobalArgs /> },
      { path: 'config/menus', element: <Menus /> },
      { path: 'config/roles', element: <Roles /> },
      { path: 'config/users', element: <Users /> },
      { path: 'config/login-logs', element: <LoginLogs /> },
      { path: 'config/login-sessions', element: <LoginSessions /> },
      { path: 'config/query-audits', element: <QueryAudits /> },
      { path: 'config/ldap', element: <AuthConfig /> },
      { path: '*', element: null },
    ],
  },
  { path: '*', element: <Navigate to="/" replace /> },
])

export default router
