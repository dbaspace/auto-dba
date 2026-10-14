import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert, Breadcrumb, Button, Card, Col, Form, Input, InputNumber, Modal, Row, Select, Space, Switch, Table, Tabs, Tag, message,
} from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'
import { activeDeployHostIds, isAgentDeployActive, useAgentDeployTracker } from './agentDeployTracker'
import { ensureAgentForHost, endpointHost } from './ensureAgentForHost'
import {
  NodePrecheckPanel, agentMatchesHost, agentMismatchMessage, hostPrimaryAddr, resolveNodeHostInstanceId, useHostApprovers,
  type NodePrecheckResult,
} from './node-precheck/nodePrecheck'
import {
  ChangeCategoryTabs, DB_CHANGE_CATEGORIES, changeCategoryLabel, changeCategoryOf, filterChangesByCategory, pickChangeInCategory,
  type ChangeCategoryKey,
} from './changeCategories'
import DbReleasePanel, { type DbRelease } from './DbReleasePanel'
import { CONF_TEMPLATE_ENGINES, DbClusterConfModal, DbConfTemplatesCard } from './DbConfTemplates'
import './DbDeployWorkbench.css'

export type DbEngine = string

type Release = DbRelease

type Cluster = {
  id: string
  tenant_id?: string
  tenant_name?: string
  engine: string
  name: string
  environment: string
  topology: string
  deploy_kind?: string
  release_id?: string
  version?: string
  ha_enabled?: boolean
  ha_mode?: string
  controller_id?: string
  ha_cluster_id?: string
  ha_vip?: string
  ha_options_json?: string
  status: string
  software_base?: string
  data_root?: string
  cloud_vendor?: string
  cloud_instance_id?: string
  cloud_region?: string
  endpoint_host?: string
  endpoint_port?: number
  linked_instance_id?: string
}

type Node = {
  id: string
  role: string
  host: string
  port?: number
  status: string
  agent_id?: string
  host_instance_id?: string
  systemd_unit?: string
}

type DbChange = {
  id: string
  cluster_id: string
  type: string
  status: string
  log_text?: string
  created_by?: string
  created_at?: string
  updated_at?: string
  steps?: DbChangeStep[]
}

type DbChangeStep = {
  id: string; seq: number; step_key: string; title: string; status: string; log_text?: string
  started_at?: string; ended_at?: string
}

function fmtDuration(start?: string, end?: string): string {
  if (!start) return '—'
  const s = new Date(start).getTime()
  const e = end ? new Date(end).getTime() : Date.now()
  if (Number.isNaN(s) || Number.isNaN(e) || e < s) return '—'
  const sec = Math.round((e - s) / 1000)
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m${sec % 60}s`
  return `${Math.floor(sec / 3600)}h${Math.floor((sec % 3600) / 60)}m`
}

function fmtClock(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function lastLogLine(log?: string): string {
  const lines = String(log || '').trim().split('\n').map((l) => l.trim()).filter(Boolean)
  return lines[lines.length - 1] || ''
}

type Meta = {
  topologies?: { id: string; label: string; kind?: string }[]
  filter_presets?: { id: string; label: string }[]
  ha_modes?: string[]
  defaults?: { software_base?: string; data_root?: string; port?: number }
  codis_roles?: string[]
  notes?: string
  label?: string
  capability?: string
  capability_label?: string
  allow_deploy?: boolean
  allow_cloud?: boolean
  roles?: string[]
}

type RegInstance = {
  id: string
  name: string
  db_type?: string
  host?: string
  domain_name?: string
  domains?: string[]
  port?: number
  env_id?: string
  cloud_vendor?: string
  cloud_instance_id?: string
  cloud_region?: string
}

const CLOUD_VENDOR_OPTIONS = [
  { value: 'tencent', label: '腾讯云' },
  { value: 'aliyun', label: '阿里云' },
  { value: 'huawei', label: '华为云' },
  { value: 'aws', label: 'AWS' },
]

const cloudVendorText = (v?: string) => CLOUD_VENDOR_OPTIONS.find((o) => o.value === v)?.label || v || ''

function instanceTypeKey(t?: string) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}

function instanceEndpointHost(it: RegInstance) {
  const domain = (it.domains || []).find(Boolean) || it.domain_name
  if (domain) return domain.trim()
  return String(it.host || '').split(/[,，]/)[0].trim()
}

type RegNode = { role: string; host: string; port?: number; source?: string; agent_id?: string }

type InstanceProbe = {
  version?: string
  major?: string
  base_dir?: string
  data_dir?: string
  port?: number
  topology?: string
  connected_role?: string
  nodes?: RegNode[]
  warnings?: string[]
}

/** 实例登记的地址（逗号分隔多节点，可带 :port）→ 节点：第一个为主，其余为从。 */
function instanceFallbackNodes(it: RegInstance, defPort?: number): RegNode[] {
  const raw = String(it.host || it.domain_name || '')
  const addrs = raw.split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean).map((s) => {
    const m = s.match(/^(.+):(\d+)$/)
    return m ? { host: m[1], port: Number(m[2]) } : { host: s, port: it.port || defPort }
  })
  return addrs.map((a, i) => ({
    ...a,
    role: i === 0 ? 'primary' : 'replica',
    source: 'instance',
  }))
}

/** MySQL / PG 节点角色只有主、从：单机即 1 个主、无从库（旧值 single 视同主）。 */
const isPrimaryRole = (role?: string) => role === 'primary' || role === 'single'

function dbRoleText(role?: string): string {
  if (isPrimaryRole(role)) return '主'
  if (role === 'replica') return '从/备'
  return role || '-'
}

function registerNodesError(topology: string, nodes: RegNode[]): string {
  if (!nodes.length) return ''
  if (nodes.some((n) => !String(n.host || '').trim())) return '节点主机不能为空'
  const noPort = nodes.find((n) => !n.port)
  if (noPort) return `节点 ${noPort.host} 端口未填写`
  const primaries = nodes.filter((n) => isPrimaryRole(n.role)).length
  const replicas = nodes.filter((n) => n.role === 'replica').length
  if (topology === 'standalone' && (primaries !== 1 || replicas !== 0)) return '单机拓扑只能登记 1 个主节点'
  if (topology === 'master_replica') {
    if (primaries !== 1) return '主从拓扑须且仅能有 1 个主节点'
    if (!replicas) return '主从拓扑至少需要 1 个从节点，没有从库请选择单机'
  }
  return ''
}

const TOPO_LABEL: Record<string, string> = {
  standalone: '自建·单机/登记',
  master_replica: '自建·主从/主备',
  sentinel: '自建·Sentinel',
  codis: '自建·Codis',
  cloud_endpoint: '云托管·单入口',
  pd_tikv_tidb: '自建·PD/TiKV/TiDB',
  multi_node: '自建·多节点',
  replset: '自建·副本集',
  broker_cluster: '自建·Broker 集群',
}

const ENGINE_TITLE_FALLBACK: Record<string, string> = {
  mysql: 'MySQL',
  postgres: 'PostgreSQL',
  redis: 'Redis / Codis',
  tidb: 'TiDB',
  cockroachdb: 'CockroachDB',
  oceanbase: 'OceanBase',
  oracle: 'Oracle',
  sqlserver: 'SQL Server',
  clickhouse: 'ClickHouse',
  mongodb: 'MongoDB',
  cassandra: 'Cassandra',
  elasticsearch: 'Elasticsearch',
  kafka: 'Kafka',
}

const STATUS_COLOR: Record<string, string> = {
  draft: 'default',
  deploying: 'processing',
  ready: 'success',
  failed: 'error',
  ha_pending: 'warning',
  pending: 'default',
  running: 'processing',
  success: 'success',
  cancelled: 'default',
  stopped: 'default',
}

const DB_CHANGE_TYPE_LABEL: Record<string, string> = {
  cluster_deploy: '集群部署', ha_init: '高可用初始化', add_node: '扩容节点', remove_node: '下线节点',
  start_node: '启动节点', stop_node: '停止节点', restart_node: '重启节点',
}

type HAController = {
  id: string
  name: string
  engine: string
  kind: string
  base_url?: string
}

function compatibleHAController(mode: string, kind: string): boolean {
  const m = (mode || '').toLowerCase()
  const k = (kind || '').toLowerCase()
  if (m === 'orchestrator') return k === 'orchestrator'
  if (m === 'mha') return k === 'mha'
  if (m === 'proxysql_repl') return k === 'proxysql'
  if (m === 'pg_autofailover') return k === 'pg_autofailover'
  if (m === 'patroni') return k === 'patroni'
  if (m === 'repmgr') return k === 'repmgr'
  if (m === 'sentinel') return k === 'sentinel'
  return false
}

const HA_MODE_LABELS: Record<string, string> = {
  manual: '人工/未托管',
  async_repl: '异步/流复制（人工切换）',
  mha: 'MHA',
  orchestrator: 'Orchestrator',
  proxysql_repl: 'ProxySQL + 复制',
  pg_autofailover: 'pg_auto_failover',
  patroni: 'Patroni',
  repmgr: 'repmgr',
}

function needsHAController(mode: string): boolean {
  const m = (mode || '').toLowerCase()
  return m === 'mha' || m === 'orchestrator' || m === 'proxysql_repl' || m === 'pg_autofailover' || m === 'patroni' || m === 'repmgr' || m === 'sentinel'
}

export default function DbDeployWorkbench({ engine }: { engine: DbEngine }) {
  const navigate = useNavigate()
  const authUser = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((authUser as { impersonating?: boolean })?.impersonating)
  const currentTenant = (authUser as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = currentTenant?.id || ''
  const currentTenantLabel = currentTenant?.name || currentTenant?.code || '当前租户'
  const [meta, setMeta] = useState<Meta>({})
  const engineTitle = meta.label || ENGINE_TITLE_FALLBACK[engine] || engine
  const allowDeploy = meta.allow_deploy === true || (meta.allow_deploy === undefined && meta.capability === undefined && ['mysql', 'postgres', 'redis'].includes(engine))
  const isRegisterOnly = !allowDeploy
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [releases, setReleases] = useState<Release[]>([])
  const [agents, setAgents] = useState<{ id: string; name?: string; base_url: string; host_instance_id?: string }[]>([])
  const [agentDeploys, setAgentDeploys] = useState<{ id: string; host_instance_id: string; status: string; listen_port?: number; base_url?: string; log_text?: string; created_at?: string }[]>([])
  const [hosts, setHosts] = useState<{ id: string; name: string; host?: string; domain_name?: string; domains?: string[]; port?: number }[]>([])
  const [ossBind, setOssBind] = useState<{ oss_setting_id?: string; prefix?: string; oss_environment?: string }>({})
  const [ossOptions, setOssOptions] = useState<{ id: string; environment: string; endpoint?: string; bucket?: string }[]>([])
  const [selected, setSelected] = useState<Cluster | null>(null)
  const [nodes, setNodes] = useState<Node[]>([])
  const [changes, setChanges] = useState<DbChange[]>([])
  const [changeDetail, setChangeDetail] = useState<DbChange | null>(null)
  const [changeCat, setChangeCat] = useState<ChangeCategoryKey>('init')
  const [tab, setTab] = useState('clusters')
  const [createOpen, setCreateOpen] = useState(false)
  const [createMode, setCreateMode] = useState<'create' | 'register'>('create')
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [envList, setEnvList] = useState<{ id?: string; code?: string; name?: string }[]>([])
  const [regInstances, setRegInstances] = useState<RegInstance[]>([])
  const loadRegInstances = useCallback(async () => {
    try {
      const res = await api.get('/instances')
      const list: RegInstance[] = res.data?.data || []
      setRegInstances(list.filter((it) => instanceTypeKey(it.db_type) === instanceTypeKey(engine)))
    } catch (err: any) {
      message.error(err?.response?.data?.message || '加载资源配置实例失败')
    }
  }, [engine])
  const loadEnvironments = useCallback(async () => {
    try {
      const res = await api.get('/environments')
      const list: { id?: string; code?: string; name?: string }[] = res.data?.data || []
      setEnvList(list)
      setEnvOptions(list
        .map((e) => ({
          value: e.code || e.name || '',
          label: e.name && e.code && e.name !== e.code ? `${e.name} (${e.code})` : (e.name || e.code || ''),
        }))
        .filter((e) => e.value))
    } catch (err: any) {
      message.error(err?.response?.data?.message || '加载环境失败')
    }
  }, [])
  const [nodeOpen, setNodeOpen] = useState(false)
  const [confOpen, setConfOpen] = useState(false)
  const [nodePrecheck, setNodePrecheck] = useState<NodePrecheckResult | null>(null)
  const [nodePrecheckBusy, setNodePrecheckBusy] = useState(false)
  const [nodeSubmitBusy, setNodeSubmitBusy] = useState(false)
  const nodeApprovers = useHostApprovers()
  const ticketApprovers = useHostApprovers()
  const [watchTicketIds, setWatchTicketIds] = useState<string[]>([])
  const [agentOpen, setAgentOpen] = useState(false)
  const [deployModal, setDeployModal] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [deployLog, setDeployLog] = useState<{ id: string; status: string; log_text?: string; base_url?: string } | null>(null)
  const [topoFilter, setTopoFilter] = useState<string>('all')
  const [haControllers, setHaControllers] = useState<HAController[]>([])
  const [mhaEnvOk, setMhaEnvOk] = useState(false)
  const [form] = Form.useForm()
  const [probeState, setProbeState] = useState<{ status: 'idle' | 'running' | 'ok' | 'failed'; message?: string; warnings?: string[] }>({ status: 'idle' })
  const fillFromInstance = (it: RegInstance) => {
    const patch: Record<string, unknown> = {}
    if (!form.getFieldValue('name')) patch.name = it.name
    const envCode = envList.find((e) => e.id === it.env_id)?.code
    if (!form.getFieldValue('environment') && envCode) patch.environment = envCode
    form.setFieldsValue(patch)
  }
  const applyTopology = (topo: string) => {
    form.setFieldsValue({
      topology: topo,
      deploy_kind: 'self',
      ...(topo !== 'master_replica' ? { ha_enabled: false, controller_id: undefined } : {}),
    })
    if (topo !== 'master_replica') setMhaEnvOk(false)
  }
  const probeRegInstance = async (it: RegInstance) => {
    setProbeState({ status: 'running' })
    try {
      const res = await api.post('/db-deploy/instance-probe', { instance_id: it.id })
      if (form.getFieldValue('linked_instance_id') !== it.id) return
      if (!res.data?.ok) {
        const nodes = instanceFallbackNodes(it, meta.defaults?.port)
        if (nodes.length) applyTopology(nodes.length > 1 ? 'master_replica' : 'standalone')
        form.setFieldsValue({ nodes })
        setProbeState({ status: 'failed', message: res.data?.message || '探测失败' })
        return
      }
      const p: InstanceProbe = res.data.data || {}
      if (p.topology) applyTopology(p.topology)
      form.setFieldsValue({
        version: p.version || form.getFieldValue('version'),
        software_base: p.base_dir || undefined,
        ...(p.data_dir ? { data_root: p.data_dir } : {}),
        nodes: (p.nodes || []).map((n) => ({ role: n.role, host: n.host, port: n.port, source: n.source })),
      })
      setProbeState({ status: 'ok', warnings: p.warnings || [] })
    } catch (err: any) {
      setProbeState({ status: 'failed', message: err?.response?.data?.message || String(err) })
    }
  }
  const [nodeForm] = Form.useForm()
  const [ossForm] = Form.useForm()
  const [agentForm] = Form.useForm()
  const [deployForm] = Form.useForm()
  const [ticketForm] = Form.useForm()
  const [ticketDraft, setTicketDraft] = useState<{
    action: string
    title: string
    summary: string
    clusterId: string
    engine: string
    payload: Record<string, unknown>
  } | null>(null)
  const [ticketBusy, setTicketBusy] = useState(false)

  const submitDbTicket = async (
    eng: string,
    clusterId: string,
    action: string,
    payload: Record<string, unknown>,
    assignedTo?: string,
    description?: string,
  ) => {
    const res = await api.post('/db-deploy/tickets', {
      engine: eng,
      cluster_id: clusterId,
      action,
      assigned_to: assignedTo,
      description,
      payload,
    })
    const issue = res.data?.data?.issue
    const no = issue?.ticket_no || issue?.id || ''
    message.success(no ? `已提交审批工单 ${no}` : '已提交审批工单')
    if (issue?.id) {
      setWatchTicketIds((prev) => (prev.includes(issue.id) ? prev : [...prev, issue.id]))
      Modal.confirm({
        title: '工单已创建',
        content: `工单 ${no} 待审批通过后自动执行。是否打开工单详情？`,
        okText: '查看工单',
        cancelText: '关闭',
        onOk: () => navigate(`/ops/tickets/${issue.id}`),
      })
    }
    return issue
  }

  const loadTicketApprovers = async (
    draft: NonNullable<typeof ticketDraft>,
    nodeList: Node[],
  ) => {
    const ids = nodeList
      .map((n) => resolveNodeHostInstanceId(n, agents, hosts))
      .filter(Boolean)
    // 无主机（如纯云登记）时传 [] → 全局 sync_default_approver / 租户成员兜底
    const list = await ticketApprovers.load(ids)
    if (list.length > 0 && !ticketForm.getFieldValue('assigned_to')) {
      ticketForm.setFieldsValue({ assigned_to: list[0] })
    }
  }

  const openTicketDraft = async (draft: NonNullable<typeof ticketDraft>) => {
    ticketForm.resetFields()
    setTicketDraft(draft)
    let nodeList = nodes
    if (draft.clusterId && (!selected || selected.id !== draft.clusterId || nodes.length === 0)) {
      try {
        const res = await api.get(`/db-deploy/clusters/${draft.clusterId}`)
        nodeList = res.data?.nodes || []
        if (selected?.id === draft.clusterId) setNodes(nodeList)
      } catch {
        nodeList = []
      }
    }
    await loadTicketApprovers(draft, nodeList)
  }

  function assignClusterToCurrentTenant(row: Cluster, e?: React.MouseEvent) {
    e?.stopPropagation()
    if (!row.id || !currentTenantId) return
    const from = row.tenant_name || row.tenant_id || '其它租户'
    Modal.confirm({
      title: '划拨到当前租户',
      content: `将集群「${row.name}」（当前归属：${from}）划拨到「${currentTenantLabel}」？关联的资源实例、已登记库/账号一并迁移。`,
      okText: '划拨',
      onOk: async () => {
        await api.post(`/db-deploy/clusters/${row.id}/assign-tenant`, { tenant_id: currentTenantId })
        message.success('已划拨到 ' + currentTenantLabel)
        await load()
      },
    })
  }

  const load = useCallback(async () => {
    const [m, c, r, o, ossList, ag, dep, hostRes, haCtrl] = await Promise.all([
      api.get('/db-deploy/meta', { params: { engine } }),
      api.get('/db-deploy/clusters', { params: { engine: engine === 'redis' ? undefined : engine } }),
      api.get('/db-deploy/releases', { params: { engine: engine === 'redis' ? undefined : engine } }),
      api.get('/db-deploy/oss', { params: { engine: engine === 'redis' ? 'redis' : engine } }),
      api.get('/oss-settings').catch(() => ({ data: { data: [] } })),
      api.get('/db-deploy/agents').catch(() => ({ data: { data: [] } })),
      api.get('/db-deploy/agents/deploys', { params: { limit: 50 } }).catch(() => ({ data: { data: [] } })),
      api.get('/instances', { params: { db_type: 'host' } }).catch(() => ({ data: { data: [] } })),
      engine === 'mysql' || engine === 'postgres'
        ? api.get('/ha/controllers', { params: { engine } }).catch(() => ({ data: { data: [] } }))
        : Promise.resolve({ data: { data: [] } }),
    ])
    setMeta(m.data?.data || {})
    let list: Cluster[] = c.data?.data || []
    if (engine === 'redis') {
      list = list.filter((x) => x.engine === 'redis' || x.engine === 'codis')
    } else {
      list = list.filter((x) => x.engine === engine)
    }
    setClusters(list)
    setSelected((prev) => {
      if (!prev) return null
      const next = list.find((x) => x.id === prev.id) || null
      if (!next) setNodes([])
      return next
    })
    setHaControllers((haCtrl.data?.data || []) as HAController[])
    setMhaEnvOk(false)
    let rels: Release[] = r.data?.data || []
    if (engine === 'redis') {
      rels = rels.filter((x) => x.engine === 'redis' || x.engine === 'codis')
    } else {
      rels = rels.filter((x) => x.engine === engine)
    }
    setReleases(rels)
    setOssBind(o.data?.data || {})
    ossForm.setFieldsValue(o.data?.data || {})
    setOssOptions(ossList.data?.data || [])
    setAgents(ag.data?.data || [])
    setAgentDeploys(dep.data?.data || [])
    setHosts(hostRes.data?.data || [])
  }, [engine, ossForm])

  useEffect(() => { load().catch((e) => message.error(String(e))) }, [load])

  // 回到本页时刷新，便于审批通过后看到删除结果
  useEffect(() => {
    const onVis = () => {
      if (document.visibilityState === 'visible') void load().catch(() => {})
    }
    document.addEventListener('visibilitychange', onVis)
    window.addEventListener('focus', onVis)
    return () => {
      document.removeEventListener('visibilitychange', onVis)
      window.removeEventListener('focus', onVis)
    }
  }, [load])

  // 轮询本页提交的工单，终态后刷新集群列表（删除集群审批通过后无需手动 F5）
  useEffect(() => {
    if (watchTicketIds.length === 0) return
    let cancelled = false
    const done = new Set(['executed', 'rejected', 'canceled', 'cancelled', 'failed'])
    const tick = async () => {
      const still: string[] = []
      let finished = false
      await Promise.all(watchTicketIds.map(async (id) => {
        try {
          const { data } = await api.get(`/tickets/${id}`, { silent: true } as any)
          const st = String(data?.data?.status || data?.status || '')
          if (done.has(st)) finished = true
          else still.push(id)
        } catch {
          still.push(id)
        }
      }))
      if (cancelled) return
      if (finished) {
        message.success('关联工单已结束，已刷新集群列表')
        await load().catch(() => {})
      }
      setWatchTicketIds((prev) => {
        const next = still.filter((id) => prev.includes(id))
        return next.length === prev.length && next.every((id, i) => id === prev[i]) ? prev : next
      })
    }
    void tick()
    const timer = setInterval(() => { void tick() }, 4000)
    return () => { cancelled = true; clearInterval(timer) }
  }, [watchTicketIds, load])

  const linkedLookupTried = useRef(new Set<string>())
  useEffect(() => {
    const linked = selected?.linked_instance_id
    if (!linked || linkedLookupTried.current.has(linked) || regInstances.some((it) => it.id === linked)) return
    linkedLookupTried.current.add(linked)
    void loadRegInstances()
  }, [selected?.linked_instance_id, regInstances, loadRegInstances])

  // 轮询下发进度时只刷新 Agent 与下发记录，避免整页重载
  const loadAgentLists = useCallback(async () => {
    const [ag, dep] = await Promise.all([
      api.get('/db-deploy/agents').catch(() => null),
      api.get('/db-deploy/agents/deploys', { params: { limit: 50 } }).catch(() => null),
    ])
    if (ag) setAgents(ag.data?.data || [])
    if (dep) setAgentDeploys(dep.data?.data || [])
  }, [])
  const hostLabel = (id: string) => {
    const h = hosts.find((x) => x.id === id)
    return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
  }
  const deployTracker = useAgentDeployTracker<{ id: string; host_instance_id: string; status: string; log_text?: string; base_url?: string; created_at?: string }>({
    basePath: '/db-deploy/agents/deploys',
    agentName: 'DB Agent',
    hostLabel,
    onShowLog: (row) => setDeployLog(row),
    onFinished: () => { void loadAgentLists() },
    refreshList: () => { void loadAgentLists() },
  })
  const { trackFromList: trackDeploysFromList } = deployTracker
  useEffect(() => { trackDeploysFromList(agentDeploys) }, [agentDeploys, trackDeploysFromList])
  const deployingHostIds = useMemo(() => activeDeployHostIds(agentDeploys), [agentDeploys])

  const pollChange = useCallback(async (id: string) => {
    const res = await api.get(`/db-deploy/changes/${id}`)
    setChangeDetail(res.data?.data || null)
    return res.data?.data as DbChange
  }, [])

  useEffect(() => {
    if (!changeDetail?.id) return
    if (changeDetail.status !== 'running' && changeDetail.status !== 'pending') return
    const t = setInterval(() => { void pollChange(changeDetail.id) }, 2000)
    return () => clearInterval(t)
  }, [changeDetail?.id, changeDetail?.status, pollChange])

  // 工单详情「查看变更日志」跳转：?cluster=&change=
  const [searchParams] = useSearchParams()
  const urlChangeId = searchParams.get('change') || ''
  const urlClusterId = searchParams.get('cluster') || ''
  useEffect(() => {
    if (!urlChangeId) return
    setTab('changes')
    void pollChange(urlChangeId).catch(() => message.warning('变更单不存在或已删除'))
    if (urlClusterId) {
      void api.get('/db-deploy/changes', { params: { cluster_id: urlClusterId, limit: 50 } })
        .then((res) => setChanges(res.data?.data || []))
        .catch(() => {})
    }
  }, [urlChangeId, urlClusterId, pollChange])

  // 进入变更页且未选中变更时，自动打开最近一个执行中 / 失败的变更
  useEffect(() => {
    if (tab !== 'changes' || changeDetail || urlChangeId || !changes.length) return
    const target = changes.find((c) => c.status === 'running' || c.status === 'pending')
      || changes.find((c) => c.status === 'failed')
    if (target) void pollChange(target.id)
  }, [tab, changes, changeDetail, urlChangeId, pollChange])

  async function openCluster(row: Cluster) {
    setSelected(row)
    const res = await api.get(`/db-deploy/clusters/${row.id}`)
    setNodes(res.data?.nodes || [])
    setSelected(res.data?.data || row)
  }

  const afterChange = async (changeId?: string) => {
    setTab('changes')
    if (changeId) await pollChange(changeId)
    if (selected) {
      const res = await api.get('/db-deploy/changes', { params: { cluster_id: selected.id, limit: 50 } })
      setChanges(res.data?.data || [])
      await openCluster(selected)
    }
    await load()
  }

  // 任意入口打开变更时，分类子 Tab 跟随该变更，保证列表与详情一致
  useEffect(() => {
    if (changeDetail?.type) setChangeCat(changeCategoryOf(DB_CHANGE_CATEGORIES, changeDetail.type))
  }, [changeDetail?.id, changeDetail?.type])

  const selectChangeCategory = (key: ChangeCategoryKey) => {
    setChangeCat(key)
    const id = pickChangeInCategory(filterChangesByCategory(DB_CHANGE_CATEGORIES, changes, key))
    if (id) void pollChange(id)
    else setChangeDetail(null)
  }

  /** 打开变更页的指定分类（默认当前分类），并展示该分类最近一次（失败 / 执行中优先）变更日志 */
  const showClusterChangeLogs = async (clusterId: string, category: ChangeCategoryKey = changeCat) => {
    setTab('changes')
    setChangeCat(category)
    const res = await api.get('/db-deploy/changes', { params: { cluster_id: clusterId, limit: 50 } })
    const list: DbChange[] = res.data?.data || []
    setChanges(list)
    const target = pickChangeInCategory(filterChangesByCategory(DB_CHANGE_CATEGORIES, list, category))
    if (target) {
      await pollChange(target)
      return
    }
    setChangeDetail(null)
  }

  const nodeHostId = Form.useWatch('host_instance_id', nodeForm) as string | undefined
  const nodeHostInst = useMemo(() => hosts.find((h) => h.id === nodeHostId) || null, [hosts, nodeHostId])
  const nodeAgentOptions = useMemo(
    () => agents
      .filter((a) => agentMatchesHost(a, nodeHostInst))
      .map((a) => ({ value: a.id, label: `${a.name || a.id} (${a.base_url})` })),
    [agents, nodeHostInst],
  )

  const reloadDbAgents = useCallback(async () => {
    const res = await api.get('/db-deploy/agents').catch(() => ({ data: { data: [] } }))
    const list = res.data?.data || []
    setAgents(list)
    return list
  }, [])

  const onNodeHostChange = async (hostId?: string) => {
    const inst = hosts.find((h) => h.id === hostId) || null
    const host = hostPrimaryAddr(inst)
    let agentId: string | undefined
    const matched = agents.filter((a) => agentMatchesHost(a, inst))
    if (matched.length === 1) {
      agentId = matched[0].id
    } else if (host) {
      agentId = await ensureAgentForHost({
        agents,
        host,
        port: 8092,
        createPath: '/db-deploy/agents',
        namePrefix: 'db',
        allowCreate: true,
        reloadAgents: reloadDbAgents,
        silent: matched.length > 1,
      })
    }
    nodeForm.setFieldsValue({
      host,
      agent_id: agentId,
      assigned_to: undefined,
    })
    const list = await nodeApprovers.load(hostId)
    if (nodeForm.getFieldValue('host_instance_id') === hostId && list.length > 0) {
      nodeForm.setFieldsValue({ assigned_to: list[0] })
    }
  }

  const bindRegNodeAgent = async (index: number, hostRaw: string) => {
    const host = endpointHost(hostRaw) || String(hostRaw || '').trim()
    if (!host) return
    const id = await ensureAgentForHost({
      agents,
      host,
      port: 8092,
      createPath: '/db-deploy/agents',
      namePrefix: 'db',
      allowCreate: true,
      reloadAgents: reloadDbAgents,
    })
    if (id) form.setFieldValue(['nodes', index, 'agent_id'], id)
  }

  const buildAddNodePayload = (v: Record<string, any>): Record<string, unknown> => ({
    role: v.role,
    host: v.host,
    port: v.port,
    host_instance_id: v.host_instance_id,
    agent_id: v.agent_id,
  })

  const runNodePrecheck = async () => {
    if (!selected) return
    const v = await nodeForm.validateFields()
    setNodePrecheckBusy(true)
    try {
      const res = await api.post(`/db-deploy/clusters/${selected.id}/nodes/precheck`, {
        ...buildAddNodePayload(v),
        assigned_to: v.assigned_to,
      })
      setNodePrecheck(res.data?.data || null)
    } catch (e: any) {
      setNodePrecheck(null)
      message.error(e?.response?.data?.message || String(e))
    } finally {
      setNodePrecheckBusy(false)
    }
  }

  const submitAddNode = async () => {
    if (!selected || !nodePrecheck?.ok) return
    const v = await nodeForm.validateFields()
    setNodeSubmitBusy(true)
    try {
      await submitDbTicket(selected.engine || engine, selected.id, 'add_node', buildAddNodePayload(v), v.assigned_to, v.description)
      setNodeOpen(false)
    } catch (e: any) {
      message.error(e?.response?.data?.message || String(e))
    } finally {
      setNodeSubmitBusy(false)
    }
  }

  const filteredClusters = topoFilter === 'all'
    ? clusters
    : clusters.filter((c) => c.topology === topoFilter)

  const roleOptions = engine === 'redis'
    ? [
        { value: 'single', label: 'single' },
        { value: 'primary', label: 'primary' },
        { value: 'replica', label: 'replica' },
        { value: 'sentinel', label: 'sentinel' },
        ...(meta.codis_roles || []).map((r) => ({ value: r, label: r })),
      ]
    : [
        { value: 'primary', label: '主' },
        { value: 'replica', label: '从/备' },
      ]

  return (
    <div className="db-deploy-page">
      <Breadcrumb
        style={{ marginBottom: 12 }}
        items={[
          { title: <Link to="/ops/cluster-deploy">集群部署</Link> },
          { title: engineTitle },
        ]}
      />
      <Alert
        type={isRegisterOnly ? 'warning' : 'info'}
        showIcon
        style={{ marginBottom: 12 }}
        message={(
          <Space wrap>
            <span>{engineTitle}</span>
            {meta.capability_label && <Tag color={isRegisterOnly ? 'processing' : 'success'}>{meta.capability_label}</Tag>}
          </Space>
        )}
        description={meta.notes || (isRegisterOnly ? '本期仅支持登记已有或云单入口。' : '版本包预置配置中心 OSS；自建节点须绑定 DB Agent（:8092）；部署走变更单。')}
      />
      {impersonating ? (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={`审计中：列表含全平台集群。其它租户资源可划拨到「${currentTenantLabel}」，该租户成员即可运维。`}
        />
      ) : null}
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'clusters',
            label: '集群',
            children: (
              <Row gutter={16}>
                <Col xs={24} lg={selected ? 14 : 24}>
                  <Space style={{ marginBottom: 12 }} wrap>
                    <span>拓扑筛选</span>
                    <Select
                      style={{ width: 200 }}
                      value={topoFilter}
                      onChange={setTopoFilter}
                      options={
                        (meta.filter_presets && meta.filter_presets.length > 0)
                          ? meta.filter_presets.map((t) => ({ value: t.id, label: t.label }))
                          : [
                              { value: 'all', label: '全部' },
                              ...(meta.topologies || []).map((t) => ({ value: t.id, label: t.label })),
                            ]
                      }
                    />
                  </Space>
                  <Space style={{ marginBottom: 12 }} wrap>
                    {allowDeploy && (
                    <Button type="primary" onClick={() => {
                      setCreateMode('create')
                      form.resetFields()
                      form.setFieldsValue({
                        engine: engine === 'redis' ? 'redis' : engine,
                        deploy_kind: 'self',
                        topology: 'standalone',
                        software_base: meta.defaults?.software_base,
                        data_root: meta.defaults?.data_root,
                        ha_enabled: false,
                      })
                      setProbeState({ status: 'idle' })
                      void loadEnvironments()
                      setCreateOpen(true)
                    }}
                    >
                      新建集群
                    </Button>
                    )}
                    <Button type={allowDeploy ? 'default' : 'primary'} onClick={() => {
                      setCreateMode('register')
                      form.resetFields()
                      form.setFieldsValue({
                        engine: engine === 'redis' ? 'redis' : engine,
                        deploy_kind: 'self',
                        topology: 'standalone',
                        software_base: meta.defaults?.software_base,
                        data_root: meta.defaults?.data_root,
                        endpoint_port: meta.defaults?.port,
                      })
                      setProbeState({ status: 'idle' })
                      void loadEnvironments()
                      void loadRegInstances()
                      setCreateOpen(true)
                    }}
                    >
                      登记已有集群
                    </Button>
                  </Space>
                  <Table
                    rowKey="id"
                    size="small"
                    dataSource={filteredClusters}
                    onRow={(row) => ({ onClick: () => { void openCluster(row) }, style: { cursor: 'pointer' } })}
                    columns={[
                      { title: '名称', dataIndex: 'name' },
                      ...(impersonating
                        ? [{
                            title: '所属租户',
                            width: 120,
                            render: (_: unknown, row: Cluster) => {
                              const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                              const label = row.tenant_name || row.tenant_id || '—'
                              return same ? <Tag color="green">{label}</Tag> : <Tag color="orange">{label}</Tag>
                            },
                          }]
                        : []),
                      { title: '引擎', dataIndex: 'engine', width: 90 },
                      {
                        title: '部署',
                        width: 80,
                        render: (_: unknown, row: Cluster) => (
                          <Tag color={(row.deploy_kind || (row.topology === 'cloud_endpoint' ? 'cloud' : 'self')) === 'cloud' ? 'blue' : 'default'}>
                            {(row.deploy_kind === 'cloud' || row.topology === 'cloud_endpoint') ? '云' : '自建'}
                          </Tag>
                        ),
                      },
                      {
                        title: '拓扑',
                        dataIndex: 'topology',
                        width: 140,
                        render: (t: string) => TOPO_LABEL[t] || t,
                      },
                      { title: '版本', dataIndex: 'version', width: 100 },
                      {
                        title: '状态',
                        dataIndex: 'status',
                        width: 100,
                        render: (s: string) => <Tag color={STATUS_COLOR[s]}>{s}</Tag>,
                      },
                      { title: '环境', dataIndex: 'environment', width: 100 },
                      ...(impersonating
                        ? [{
                            title: '操作',
                            width: 140,
                            render: (_: unknown, row: Cluster) => (
                              row.tenant_id && row.tenant_id !== currentTenantId ? (
                                <Auth urn={Perm.tenants.impersonate}>
                                  <Button type="link" size="small" onClick={(e) => assignClusterToCurrentTenant(row, e)}>
                                    划拨到当前租户
                                  </Button>
                                </Auth>
                              ) : null
                            ),
                          }]
                        : []),
                    ]}
                  />
                </Col>
                {selected && (
                  <Col xs={24} lg={10}>
                    <Card
                      title={selected.name}
                      extra={(
                        <Space wrap size="small">
                          <Button type="link" size="small" onClick={() => setSelected(null)}>关闭</Button>
                        </Space>
                      )}
                      size="small"
                    >
                      <p>
                        {(selected.deploy_kind === 'cloud' || selected.topology === 'cloud_endpoint') ? '云托管' : '自建'}
                        {' · '}
                        {TOPO_LABEL[selected.topology] || selected.topology}
                        {' · '}
                        版本 {selected.version || '—'}
                        {' · '}
                        {selected.status}
                      </p>
                      {(selected.deploy_kind === 'cloud' || selected.topology === 'cloud_endpoint') ? (
                        <Card size="small" title="访问入口（厂商 HA 对用户仍是单入口）">
                          <p><b>Endpoint</b>：{selected.endpoint_host || nodes[0]?.host || '—'}:{selected.endpoint_port || nodes[0]?.port || meta.defaults?.port || '—'}</p>
                          <p><b>厂商</b>：{cloudVendorText(selected.cloud_vendor) || '—'} · <b>云实例 ID</b>：{selected.cloud_instance_id || '—'}</p>
                          <p>
                            <b>Region</b>：{selected.cloud_region || '—'} · <b>关联实例</b>：
                            {selected.linked_instance_id
                              ? (regInstances.find((it) => it.id === selected.linked_instance_id)?.name || selected.linked_instance_id)
                              : '—'}
                          </p>
                          <Alert type="info" showIcon message="云托管不走 Agent 装机/启停；切主与扩缩由云厂商控制台完成。" />
                        </Card>
                      ) : selected.topology === 'codis' ? (
                        <div className="codis-roles">
                          {(meta.codis_roles || ['codis_dashboard', 'codis_proxy', 'codis_server']).map((role) => (
                            <Card key={role} size="small" className="codis-role-card" title={role}>
                              <ul>
                                {nodes.filter((n) => n.role === role).map((n) => (
                                  <li key={n.id}>
                                    {n.host}:{n.port || '-'} · {n.status}
                                    {' '}
                                    <Button type="link" size="small" danger={n.status === 'ready'} onClick={async () => {
                                      const running = n.status === 'ready'
                                      try {
                                        const res = await api.post(`/db-deploy/clusters/${selected.id}/nodes/${n.id}/${running ? 'stop' : 'start'}`)
                                        await afterChange(res.data?.data?.change_id)
                                      } catch (err: any) {
                                        message.error(err?.response?.data?.message || String(err))
                                      }
                                    }}
                                    >
                                      {n.status === 'ready' ? '停止' : '启动'}
                                    </Button>
                                  </li>
                                ))}
                                {nodes.filter((n) => n.role === role).length === 0 && <li className="muted">暂无节点</li>}
                              </ul>
                            </Card>
                          ))}
                        </div>
                      ) : (
                        <Table
                          rowKey="id"
                          size="small"
                          pagination={false}
                          dataSource={nodes}
                          columns={[
                            {
                              title: '角色',
                              dataIndex: 'role',
                              width: 90,
                              render: (r: string) => (engine === 'mysql' || engine === 'postgres' ? dbRoleText(r) : r),
                            },
                            { title: '主机', dataIndex: 'host' },
                            { title: '端口', dataIndex: 'port', width: 60 },
                            {
                              title: '状态',
                              dataIndex: 'status',
                              width: 72,
                              render: (s: string) => <Tag color={STATUS_COLOR[s] || 'default'}>{s === 'ready' ? '运行中' : s === 'stopped' ? '已停止' : s}</Tag>,
                            },
                            {
                              title: '操作',
                              width: 200,
                              render: (_: unknown, row: Node) => (
                                <Space size={0} wrap>
                                  {row.status !== 'ready' && (selected.status === 'ready' || selected.status === 'ha_pending') && (
                                    <Button
                                      type="link"
                                      size="small"
                                      onClick={async (e) => {
                                        e.stopPropagation()
                                        try {
                                          const res = await api.post(`/db-deploy/clusters/${selected.id}/nodes/${row.id}/add`)
                                          message.success('已提交加入')
                                          await afterChange(res.data?.data?.change_id)
                                        } catch (err: any) {
                                          message.error(err?.response?.data?.message || String(err))
                                        }
                                      }}
                                    >
                                      加入
                                    </Button>
                                  )}
                                  <Button
                                    type="link"
                                    size="small"
                                    danger={row.status === 'ready'}
                                    onClick={async (e) => {
                                      e.stopPropagation()
                                      const running = row.status === 'ready'
                                      try {
                                        const res = await api.post(
                                          `/db-deploy/clusters/${selected.id}/nodes/${row.id}/${running ? 'stop' : 'start'}`,
                                        )
                                        message.success(running ? '已提交停止' : '已提交启动')
                                        await afterChange(res.data?.data?.change_id)
                                      } catch (err: any) {
                                        message.error(err?.response?.data?.message || String(err))
                                      }
                                    }}
                                  >
                                    {row.status === 'ready' ? '停止' : '启动'}
                                  </Button>
                                  <Button
                                    type="link"
                                    size="small"
                                    onClick={async (e) => {
                                      e.stopPropagation()
                                      try {
                                        const res = await api.post(`/db-deploy/clusters/${selected.id}/nodes/${row.id}/restart`)
                                        await afterChange(res.data?.data?.change_id)
                                      } catch (err: any) {
                                        message.error(err?.response?.data?.message || String(err))
                                      }
                                    }}
                                  >
                                    重启
                                  </Button>
                                  <Button
                                    type="link"
                                    size="small"
                                    danger
                                    onClick={async (e) => {
                                      e.stopPropagation()
                                      try {
                                        const res = await api.post(`/db-deploy/clusters/${selected.id}/nodes/${row.id}/remove`)
                                        message.success('已提交下线')
                                        await afterChange(res.data?.data?.change_id)
                                      } catch (err: any) {
                                        message.error(err?.response?.data?.message || String(err))
                                      }
                                    }}
                                  >
                                    下线
                                  </Button>
                                </Space>
                              ),
                            },
                          ]}
                        />
                      )}
                      <Space style={{ marginTop: 12 }} wrap>
                        {!(selected.deploy_kind === 'cloud' || selected.topology === 'cloud_endpoint') && (
                          <Button size="small" onClick={() => {
                            nodeForm.resetFields()
                            nodeForm.setFieldsValue({
                              role: engine === 'redis'
                                ? (selected.topology === 'standalone' ? 'single' : 'primary')
                                : (nodes.some((n) => isPrimaryRole(n.role)) ? 'replica' : 'primary'),
                              port: meta.defaults?.port,
                            })
                            setNodePrecheck(null)
                            void nodeApprovers.load(undefined)
                            setNodeOpen(true)
                          }}
                          >
                            添加节点
                          </Button>
                        )}
                        {CONF_TEMPLATE_ENGINES.includes(selected.engine || engine)
                          && !(selected.deploy_kind === 'cloud' || selected.topology === 'cloud_endpoint') && (
                          <Button size="small" onClick={() => setConfOpen(true)}>参数</Button>
                        )}
                        {allowDeploy && selected.status !== 'ready' && selected.status !== 'ha_pending'
                          && !(selected.deploy_kind === 'cloud' || selected.topology === 'cloud_endpoint') && (
                          <Button
                            size="small"
                            type="primary"
                            onClick={() => {
                              openTicketDraft({
                                action: 'deploy',
                                title: '提交部署审批',
                                summary: `将对集群 ${selected.name}（${selected.engine || engine}）执行装机部署。审批通过后自动编排；引擎未就绪时工单会标记执行失败并说明原因。`,
                                clusterId: selected.id,
                                engine: selected.engine || engine,
                                payload: {},
                              })
                            }}
                          >
                            开始部署
                          </Button>
                        )}
                        {selected.status === 'ha_pending' && selected.ha_enabled && (
                          <Button
                            size="small"
                            type="primary"
                            onClick={async () => {
                              try {
                                const res = await api.post(`/db-deploy/clusters/${selected.id}/ha-init`)
                                message.success('已提交 HA 接入')
                                await afterChange(res.data?.data?.change_id)
                                load()
                              } catch (err: any) {
                                message.error(err?.response?.data?.message || String(err))
                              }
                            }}
                          >
                            继续接入 HA
                          </Button>
                        )}
                        {selected.ha_cluster_id && (
                          <Button size="small" type="link" onClick={() => navigate('/ops/ha')}>
                            打开高可用维护
                          </Button>
                        )}
                        {selected.ha_enabled && (
                          <Tag color={selected.ha_cluster_id ? 'success' : 'warning'}>
                            HA {selected.ha_mode || ''}{selected.ha_cluster_id ? ' · 已绑定' : ' · 待接入'}
                          </Tag>
                        )}
                        <Button
                          size="small"
                          danger
                          onClick={() => {
                            openTicketDraft({
                              action: 'delete_cluster',
                              title: '提交删除集群审批',
                              summary: `将删除集群 ${selected.name} 及其节点登记。审批通过后执行。`,
                              clusterId: selected.id,
                              engine: selected.engine || engine,
                              payload: {},
                            })
                          }}
                        >
                          删除
                        </Button>
                      </Space>
                    </Card>
                  </Col>
                )}
              </Row>
            ),
          },
          {
            key: 'changes',
            label: '变更',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Space>
                  <Button
                    disabled={!selected}
                    onClick={() => selected && void showClusterChangeLogs(selected.id)}
                  >
                    刷新当前集群变更
                  </Button>
                  {!selected && <span className="muted">请先在「集群」选中集群</span>}
                </Space>
                <ChangeCategoryTabs
                  categories={DB_CHANGE_CATEGORIES}
                  changes={changes}
                  active={changeCat}
                  onChange={selectChangeCategory}
                />
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={filterChangesByCategory(DB_CHANGE_CATEGORIES, changes, changeCat)}
                  locale={{ emptyText: `暂无${changeCategoryLabel(DB_CHANGE_CATEGORIES, changeCat)}` }}
                  onRow={(row) => ({
                    onClick: () => { void pollChange(row.id) },
                    style: { cursor: 'pointer', background: changeDetail?.id === row.id ? 'var(--ant-color-primary-bg, #e6f4ff)' : undefined },
                  })}
                  columns={[
                    { title: '类型', dataIndex: 'type', width: 140, render: (t: string) => DB_CHANGE_TYPE_LABEL[t] || t },
                    {
                      title: '状态',
                      dataIndex: 'status',
                      width: 100,
                      render: (s: string) => <Tag color={STATUS_COLOR[s]}>{s}</Tag>,
                    },
                    {
                      title: '摘要',
                      dataIndex: 'log_text',
                      ellipsis: { showTitle: true },
                      render: (t: string) => lastLogLine(t) || '—',
                    },
                    { title: '开始时间', dataIndex: 'created_at', width: 140, render: (v: string) => fmtClock(v) },
                    {
                      title: '耗时',
                      width: 90,
                      render: (_: unknown, row: DbChange) => fmtDuration(
                        row.created_at,
                        row.status === 'running' || row.status === 'pending' ? undefined : row.updated_at,
                      ),
                    },
                    {
                      title: '操作',
                      width: 140,
                      render: (_: unknown, row: DbChange) => (
                        <Space>
                          {(row.status === 'failed' || row.status === 'cancelled') && (
                            <Button type="link" size="small" onClick={async (e) => {
                              e.stopPropagation()
                              await api.post(`/db-deploy/changes/${row.id}/retry`)
                              message.success('已重试')
                              await pollChange(row.id)
                            }}
                            >
                              重试
                            </Button>
                          )}
                          {(row.status === 'pending' || row.status === 'running') && (
                            <Button type="link" size="small" danger onClick={async (e) => {
                              e.stopPropagation()
                              await api.post(`/db-deploy/changes/${row.id}/cancel`)
                              message.success('已取消')
                              await pollChange(row.id)
                            }}
                            >
                              取消
                            </Button>
                          )}
                        </Space>
                      ),
                    },
                  ]}
                />
                {changeDetail && (
                  <Card
                    size="small"
                    title={`变更详情 · ${changeDetail.type} · ${changeDetail.id.slice(0, 8)}`}
                    extra={<Tag color={STATUS_COLOR[changeDetail.status]}>{changeDetail.status}</Tag>}
                  >
                    <Table
                      rowKey="id"
                      size="small"
                      pagination={false}
                      dataSource={changeDetail.steps || []}
                      columns={[
                        { title: '#', dataIndex: 'seq', width: 50 },
                        { title: '步骤', dataIndex: 'title', width: 220 },
                        {
                          title: '状态',
                          dataIndex: 'status',
                          width: 90,
                          render: (s: string) => <Tag color={STATUS_COLOR[s]}>{s}</Tag>,
                        },
                        { title: '开始', dataIndex: 'started_at', width: 120, render: (v: string) => fmtClock(v) },
                        {
                          title: '耗时',
                          width: 80,
                          render: (_: unknown, row: DbChangeStep) => fmtDuration(
                            row.started_at,
                            row.status === 'running' ? undefined : row.ended_at,
                          ),
                        },
                        {
                          title: '日志',
                          dataIndex: 'log_text',
                          render: (t: string, row: DbChangeStep) => (
                            <div className={`db-step-log-cell${row.status === 'failed' ? ' db-step-log-fail' : ''}`}>{t || '—'}</div>
                          ),
                        },
                      ]}
                    />
                    <div style={{ marginTop: 12, fontWeight: 600 }}>
                      {changeCategoryLabel(DB_CHANGE_CATEGORIES, changeCategoryOf(DB_CHANGE_CATEGORIES, changeDetail.type))}
                    </div>
                    <pre className="db-change-log">
                      {[
                        changeDetail.log_text ? `[变更] ${changeDetail.log_text}` : '',
                        ...(changeDetail.steps || []).map((s) => {
                          const body = (s.log_text || '').trim() || (s.status === 'pending' ? '（未执行）' : '—')
                          return `[#${s.seq} ${s.status}] ${s.title}\n${body}`
                        }),
                      ].filter(Boolean).join('\n\n') || '（暂无日志）'}
                    </pre>
                  </Card>
                )}
              </Space>
            ),
          },
          {
            key: 'releases',
            label: '版本目录',
            children: (
              <DbReleasePanel
                engine={engine}
                releases={releases}
                prefix={ossBind.prefix || ''}
                onChanged={() => { load().catch((e) => message.error(String(e))) }}
              />
            ),
          },
          {
            key: 'agents',
            label: 'DB Agent',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Alert
                  type="info"
                  showIcon
                  message="DB Agent 下发与登记（MySQL / PostgreSQL / Redis 自建节点）"
                  description={(
                    <>
                      SSH 下发
                      {' '}
                      <code>deploy/db-agent</code>
                      {' '}
                      到 host 实例，默认监听
                      {' '}
                      <code>:8092</code>
                      （API
                      {' '}
                      <code>/v1/db/*</code>
                      ）。
                      <strong>不是</strong>
                      {' '}
                      StarRocks Node Agent（
                      <code>:8091</code>
                      /
                      <code>/v1/sr/*</code>
                      ），也
                      <strong>不是</strong>
                      {' '}
                      配置中心平台 Agent（
                      <code>:8090</code>
                      ）。
                      下发前请在控制面执行
                      {' '}
                      <code>cd deploy/db-agent && ./build.sh</code>
                      。云托管单入口无需 Agent。对已有集群：下发
                      <strong>仅安装/重启 Agent 自身</strong>
                      ，不启停 MySQL/PG/Redis 等业务服务。
                    </>
                  )}
                />
                <Space wrap>
                  <Button type="primary" onClick={() => {
                    deployForm.resetFields()
                    deployForm.setFieldsValue({
                      listen_port: 8092,
                      remote_dir: '/home/sysadmin/auto-dba/db-agent',
                      escalate: false,
                    })
                    setDeployModal(true)
                  }}
                  >
                    SSH 下发
                  </Button>
                  <Button onClick={() => { agentForm.resetFields(); setAgentOpen(true) }}>手工登记</Button>
                  <Button onClick={() => { load() }}>刷新</Button>
                </Space>
                <Table
                  rowKey="id"
                  size="small"
                  title={() => '已登记 DB Agent'}
                  dataSource={agents}
                  columns={[
                    { title: '名称', dataIndex: 'name', width: 160 },
                    { title: 'Base URL', dataIndex: 'base_url' },
                    {
                      title: '来源',
                      width: 100,
                      render: (_: unknown, row: { id: string; host_instance_id?: string }) => (
                        row.host_instance_id ? <Tag color="blue">SSH 下发</Tag> : <Tag>手工</Tag>
                      ),
                    },
                    { title: 'ID', dataIndex: 'id', width: 240, ellipsis: true },
                    {
                      title: '操作',
                      width: 80,
                      render: (_: unknown, row: { id: string; name?: string; base_url?: string }) => (
                        <Button size="small" danger onClick={() => {
                          Modal.confirm({
                            title: '删除 Agent',
                            content: `确认删除「${row.name || row.base_url || row.id}」登记？删除后，该主机上的集群节点将无法通过 Agent 运维；在该主机重新下发 Agent 后会自动恢复绑定。`,
                            okButtonProps: { danger: true },
                            onOk: async () => {
                              try {
                                const res = await api.delete(`/db-deploy/agents/${row.id}`)
                                const affected: { name: string; detail?: string }[] = res.data?.affected_nodes || []
                                if (affected.length) {
                                  message.warning(
                                    `已删除。以下节点需重新下发 Agent 后才能运维：${affected
                                      .map((n) => (n.detail ? `${n.name}（${n.detail}）` : n.name))
                                      .join('、')}`,
                                    6,
                                  )
                                } else {
                                  message.success('已删除')
                                }
                                load()
                              } catch {
                                /* 错误已由全局拦截器提示 */
                              }
                            },
                          })
                        }}
                        >
                          删除
                        </Button>
                      ),
                    },
                  ]}
                />
                <Table
                  rowKey="id"
                  size="small"
                  title={() => '下发记录'}
                  dataSource={agentDeploys}
                  pagination={{ pageSize: 8 }}
                  columns={[
                    { title: '主机', dataIndex: 'host_instance_id', ellipsis: true, render: (id: string) => hostLabel(id) },
                    {
                      title: '状态',
                      dataIndex: 'status',
                      width: 110,
                      render: (s: string, row: { id: string; host_instance_id: string; status: string; created_at?: string }) => (
                        <Tag color={STATUS_COLOR[s]} icon={isAgentDeployActive(row) ? <SyncOutlined spin /> : undefined}>{s}</Tag>
                      ),
                    },
                    { title: '端口', dataIndex: 'listen_port', width: 70 },
                    { title: 'URL', dataIndex: 'base_url', ellipsis: true },
                    { title: '时间', dataIndex: 'created_at', width: 180 },
                    {
                      title: '日志',
                      width: 80,
                      render: (_: unknown, row: { id: string; status: string; log_text?: string; base_url?: string }) => (
                        <Button type="link" size="small" onClick={() => setDeployLog(row)}>查看</Button>
                      ),
                    },
                  ]}
                />
              </Space>
            ),
          },
          {
            key: 'settings',
            label: '设置',
            children: (
              <>
              <Card size="small" title="制品 OSS（引用配置中心）">
                <p className="muted">
                  当前绑定：{ossBind.oss_environment || '未选择'} · prefix={ossBind.prefix || '—'}
                  {' · '}
                  <Link to="/config/oss">去配置中心 OSS</Link>
                </p>
                <Form
                  form={ossForm}
                  layout="vertical"
                  style={{ maxWidth: 480 }}
                  onFinish={async (v) => {
                    const eng = engine === 'redis' ? 'redis' : engine
                    await api.put('/db-deploy/oss', { ...v, engine: eng })
                    if (engine === 'redis') {
                      await api.put('/db-deploy/oss', { ...v, engine: 'codis' }).catch(() => undefined)
                    }
                    message.success('已保存')
                    load()
                  }}
                >
                  <Form.Item name="oss_setting_id" label="配置中心 OSS" rules={[{ required: true }]}>
                    <Select
                      options={ossOptions.map((o) => ({
                        value: o.id,
                        label: `${o.environment} (${o.bucket || o.endpoint || o.id})`,
                      }))}
                      placeholder="选择环境 OSS"
                    />
                  </Form.Item>
                  <Form.Item name="prefix" label="制品 prefix" rules={[{ required: true }]}>
                    <Input placeholder={`db-artifacts/${engine}/`} />
                  </Form.Item>
                  <Button type="primary" htmlType="submit">保存</Button>
                </Form>
              </Card>
              {CONF_TEMPLATE_ENGINES.includes(engine) && <DbConfTemplatesCard engine={engine} />}
              </>
            ),
          },
        ].filter((it) => allowDeploy || (it.key !== 'releases' && it.key !== 'agents'))}
      />

      <DbClusterConfModal
        open={confOpen && !!selected}
        clusterId={selected?.id}
        clusterName={selected?.name}
        onClose={() => setConfOpen(false)}
      />

      <Modal
        title={createMode === 'register' ? '登记已有集群' : '新建集群'}
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => form.submit()}
        width={560}
        destroyOnClose
      >
        {createMode === 'register' && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="登记已有：自建可补节点后运维；云托管仅登记单入口（厂商 HA 仍是一个 endpoint），不装机、不启停。"
          />
        )}
        <Form
          form={form}
          layout="vertical"
          onFinish={async (v) => {
            try {
              const isCloud = v.topology === 'cloud_endpoint' || v.deploy_kind === 'cloud'
              const payload: Record<string, unknown> = {
                ...v,
                deploy_kind: isCloud ? 'cloud' : 'self',
                register_existing: createMode === 'register' || isCloud,
              }
              if (isCloud) {
                payload.topology = 'cloud_endpoint'
                payload.engine = engine === 'redis' ? 'redis' : (v.engine || engine)
                payload.ha_enabled = false
                delete payload.release_id
                delete payload.mha_user
                delete payload.mha_password
                delete payload.mha_ssh_user
              } else if (v.ha_enabled && (engine === 'mysql' || engine === 'postgres')) {
                if (needsHAController(v.ha_mode) && !v.controller_id) {
                  message.error('该 HA 模式须选择已登记的控制器')
                  return
                }
                if ((v.ha_mode === 'mha' ||
                  v.ha_mode === 'orchestrator' ||
                  v.ha_mode === 'proxysql_repl' ||
                  v.ha_mode === 'pg_autofailover' ||
                  v.ha_mode === 'patroni' ||
                  v.ha_mode === 'repmgr') && !mhaEnvOk) {
                  message.error('请先对所选控制器执行环境检查并通过')
                  return
                }
                const opts: Record<string, unknown> = {}
                if (v.mha_user) opts.user = v.mha_user
                if (v.mha_password) opts.password = v.mha_password
                if (v.mha_ssh_user) opts.ssh_user = v.mha_ssh_user
                if (v.mha_failover_script) opts.master_ip_failover_script = v.mha_failover_script
                if (v.mha_online_change_script) opts.master_ip_online_change_script = v.mha_online_change_script
                if (v.vip_hook_script) opts.vip_hook_script = v.vip_hook_script
                if (v.vip_hook_args) {
                  const args = String(v.vip_hook_args)
                    .split(/[\s,]+/)
                    .map((s: string) => s.trim())
                    .filter(Boolean)
                  if (args.length) opts.vip_hook_args = args
                }
                if (Object.keys(opts).length) {
                  payload.ha_options_json = JSON.stringify(opts)
                }
                delete payload.mha_user
                delete payload.mha_password
                delete payload.mha_ssh_user
                delete payload.mha_failover_script
                delete payload.mha_online_change_script
                delete payload.vip_hook_script
                delete payload.vip_hook_args
              } else {
                payload.ha_enabled = false
                delete payload.mha_user
                delete payload.mha_password
                delete payload.mha_ssh_user
                delete payload.mha_failover_script
                delete payload.mha_online_change_script
                delete payload.vip_hook_script
                delete payload.vip_hook_args
              }
              delete payload.nodes
              if (!isCloud && createMode === 'register') {
                const regNodes: RegNode[] = (v.nodes || []).filter((n: RegNode) => n && String(n.host || '').trim())
                const nodeErr = registerNodesError(v.topology, regNodes)
                if (nodeErr) {
                  message.error(nodeErr)
                  return
                }
                if (regNodes.length) {
                  payload.nodes = regNodes.map((n) => ({
                    role: n.role,
                    host: n.host.trim(),
                    port: n.port,
                    agent_id: n.agent_id,
                  }))
                }
              }
              const res = await api.post('/db-deploy/clusters', payload)
              if (res.data?.warning) message.warning(res.data.warning)
              message.success(isCloud || createMode === 'register' ? '已登记为 ready' : '已创建')
              setCreateOpen(false)
              await load()
              if (res.data?.data) await openCluster(res.data.data)
            } catch (e: any) {
              message.error(e?.response?.data?.message || String(e))
            }
          }}
        >
          <Form.Item name="deploy_kind" hidden><Input /></Form.Item>
          {engine === 'redis' && (
            <Form.Item noStyle shouldUpdate={(a, b) => a.topology !== b.topology}>
              {() => {
                const cloud = form.getFieldValue('topology') === 'cloud_endpoint'
                return (
                  <Form.Item name="engine" label="引擎" rules={[{ required: true }]} initialValue="redis">
                    <Select
                      disabled={cloud}
                      options={cloud
                        ? [{ value: 'redis', label: 'Redis（云单入口）' }]
                        : [{ value: 'redis', label: 'Redis' }, { value: 'codis', label: 'Codis（仅自建）' }]}
                    />
                  </Form.Item>
                )
              }}
            </Form.Item>
          )}
          {engine !== 'redis' && <Form.Item name="engine" hidden><Input /></Form.Item>}
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item
            name="environment"
            label="环境"
            rules={[{ required: true, message: '请选择环境' }]}
            extra={(
              <>
                来自
                {' '}
                <Link to="/config/environments">配置中心 · 环境管理</Link>
              </>
            )}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择环境"
              options={envOptions}
              notFoundContent="暂无环境，请先在配置中心 · 环境管理添加"
            />
          </Form.Item>
          <Form.Item name="topology" label="拓扑" rules={[{ required: true }]}>
            <Select
              options={(meta.topologies || [])
                .filter((t) => createMode === 'register' || t.kind !== 'cloud')
                .map((t) => ({ value: t.id, label: t.label }))}
              onChange={(val) => {
                if (val === 'cloud_endpoint') {
                  form.setFieldsValue({ deploy_kind: 'cloud', engine: engine === 'redis' ? 'redis' : engine, ha_enabled: false })
                  setMhaEnvOk(false)
                  void loadRegInstances()
                } else {
                  form.setFieldsValue({
                    deploy_kind: 'self',
                    ...(val !== 'master_replica' ? { ha_enabled: false, controller_id: undefined } : {}),
                  })
                  if (val !== 'master_replica') setMhaEnvOk(false)
                }
              }}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.topology !== b.topology}>
            {() => {
              const cloud = form.getFieldValue('topology') === 'cloud_endpoint'
              if (cloud) {
                return (
                  <>
                    <Form.Item
                      name="linked_instance_id"
                      label="从资源配置选择实例"
                      extra={(
                        <>
                          可选；选中后自动带出下列字段，仍可修改；不选则自定义填写。实例来自
                          {' '}
                          <Link to="/config/instances">资源配置</Link>
                        </>
                      )}
                    >
                      <Select
                        showSearch
                        allowClear
                        optionFilterProp="label"
                        placeholder="选择实例，或留空自定义"
                        notFoundContent="资源配置中暂无该类型实例"
                        options={regInstances.map((it) => {
                          const host = instanceEndpointHost(it)
                          const vendor = cloudVendorText(it.cloud_vendor)
                          return {
                            value: it.id,
                            label: [it.name, host ? `${host}${it.port ? `:${it.port}` : ''}` : '', vendor].filter(Boolean).join(' · '),
                          }
                        })}
                        onChange={(id?: string) => {
                          const it = regInstances.find((x) => x.id === id)
                          if (!it) return
                          const patch: Record<string, unknown> = {
                            endpoint_host: instanceEndpointHost(it) || undefined,
                            endpoint_port: it.port || meta.defaults?.port,
                            cloud_vendor: it.cloud_vendor || undefined,
                            cloud_instance_id: it.cloud_instance_id || undefined,
                            cloud_region: it.cloud_region || undefined,
                          }
                          form.setFieldsValue(patch)
                          fillFromInstance(it)
                        }}
                      />
                    </Form.Item>
                    <Form.Item name="endpoint_host" label="访问入口 Host/域名" rules={[{ required: true }]}><Input placeholder="vip.xxx.rds.aliyuncs.com" /></Form.Item>
                    <Form.Item name="endpoint_port" label="端口" rules={[{ required: true }]}><InputNumber style={{ width: '100%' }} /></Form.Item>
                    <Form.Item name="cloud_vendor" label="云厂商">
                      <Select allowClear placeholder="选择云厂商" options={CLOUD_VENDOR_OPTIONS} />
                    </Form.Item>
                    <Form.Item name="cloud_instance_id" label="云实例 ID"><Input /></Form.Item>
                    <Form.Item name="cloud_region" label="Region"><Input placeholder="cn-hangzhou / ap-guangzhou" /></Form.Item>
                    <Form.Item name="version" label="版本备注（可选）"><Input placeholder="如 8.0 / 16" /></Form.Item>
                  </>
                )
              }
              const regFromInstance = createMode === 'register' && (engine === 'mysql' || engine === 'postgres')
              return (
                <>
                  {regFromInstance && (
                    <>
                      <Form.Item
                        name="linked_instance_id"
                        label="从资源配置选择实例"
                        extra={(
                          <>
                            可选；选中后用实例账号只读连库，自动填写版本、目录、拓扑与节点，均可修改。实例来自
                            {' '}
                            <Link to="/config/instances">资源配置</Link>
                          </>
                        )}
                      >
                        <Select
                          showSearch
                          allowClear
                          optionFilterProp="label"
                          placeholder="选择实例，或留空手动填写"
                          notFoundContent="资源配置中暂无该类型实例"
                          options={regInstances.map((it) => ({
                            value: it.id,
                            label: [it.name, `${String(it.host || it.domain_name || '').trim()}${it.port ? `:${it.port}` : ''}`].filter(Boolean).join(' · '),
                          }))}
                          onChange={(id?: string) => {
                            const it = regInstances.find((x) => x.id === id)
                            if (!it) {
                              setProbeState({ status: 'idle' })
                              return
                            }
                            fillFromInstance(it)
                            void probeRegInstance(it)
                          }}
                        />
                      </Form.Item>
                      {probeState.status !== 'idle' && (
                        <div style={{ marginTop: -12, marginBottom: 16 }}>
                          <Space size={8} style={{ marginBottom: 6 }}>
                            {probeState.status === 'running' && <Tag icon={<SyncOutlined spin />} color="processing">探测中</Tag>}
                            {probeState.status === 'ok' && <Tag color="success">已探测并填写</Tag>}
                            {probeState.status === 'failed' && <Tag color="error">探测失败</Tag>}
                            <Button
                              size="small"
                              type="link"
                              disabled={probeState.status === 'running'}
                              onClick={() => {
                                const it = regInstances.find((x) => x.id === form.getFieldValue('linked_instance_id'))
                                if (it) void probeRegInstance(it)
                              }}
                            >
                              重新探测
                            </Button>
                          </Space>
                          {probeState.status === 'failed' && (
                            <Alert
                              type="error"
                              showIcon
                              message={probeState.message}
                              description="已按实例登记的主机生成节点，版本与目录请手动填写。"
                            />
                          )}
                          {probeState.status === 'ok' && !!probeState.warnings?.length && (
                            <Alert
                              type="warning"
                              showIcon
                              message="请核对以下信息"
                              description={<ul style={{ margin: 0, paddingLeft: 18 }}>{probeState.warnings.map((w) => <li key={w}>{w}</li>)}</ul>}
                            />
                          )}
                        </div>
                      )}
                      <Form.Item name="version" label="版本" extra="已有集群的实际版本；版本目录里有对应安装包时可在下方另选 DbRelease">
                        <Input placeholder={engine === 'mysql' ? '如 8.0.36' : '如 16.4'} />
                      </Form.Item>
                    </>
                  )}
                  <Form.Item
                    name="release_id"
                    label={regFromInstance ? '版本目录（可选）' : '版本（DbRelease）'}
                    rules={createMode === 'create' ? [{ required: true }] : []}
                  >
                    <Select
                      allowClear
                      options={releases.filter((r) => r.enabled && (r.status === undefined || r.status === 'ready')).map((r) => ({
                        value: r.id,
                        label: `${r.engine} ${r.version}${r.arch && r.arch !== 'x86_64' ? ` (${r.arch})` : ''}`,
                      }))}
                      placeholder="从版本目录选择（仅列出安装包已就绪的版本）"
                      notFoundContent="暂无就绪版本，请先到「版本目录」登记并上传 / 同步安装包"
                    />
                  </Form.Item>
                  <Form.Item name="software_base" label="软件目录">
                    <Input placeholder={regFromInstance ? '未探测到，请填写实际安装目录（如 /usr/lib/postgresql/15）' : undefined} />
                  </Form.Item>
                  <Form.Item name="data_root" label="数据目录"><Input /></Form.Item>
                  {regFromInstance && (
                    <Form.Item
                      label="节点"
                      extra="填 Host 失焦后自动匹配/登记 DB Agent（:8092）；提交时后端也会按地址补绑"
                    >
                      <Form.List name="nodes">
                        {(fields, { add, remove }) => (
                          <>
                            {fields.map((f) => (
                              <Space key={f.key} align="baseline" style={{ display: 'flex', marginBottom: 4 }} wrap>
                                <Form.Item name={[f.name, 'role']} noStyle rules={[{ required: true }]}>
                                  <Select
                                    style={{ width: 110 }}
                                    options={[
                                      { value: 'primary', label: '主' },
                                      { value: 'replica', label: '从/备' },
                                    ]}
                                  />
                                </Form.Item>
                                <Form.Item name={[f.name, 'host']} noStyle rules={[{ required: true, message: '主机必填' }]}>
                                  <Input
                                    style={{ width: 160 }}
                                    placeholder="主机 / IP"
                                    onBlur={(e) => void bindRegNodeAgent(f.name, e.target.value)}
                                  />
                                </Form.Item>
                                <Form.Item name={[f.name, 'port']} noStyle rules={[{ required: true, message: '端口必填' }]}>
                                  <InputNumber style={{ width: 100 }} placeholder="端口" min={1} max={65535} />
                                </Form.Item>
                                <Form.Item name={[f.name, 'agent_id']} noStyle>
                                  <Select
                                    allowClear
                                    showSearch
                                    optionFilterProp="label"
                                    style={{ width: 220 }}
                                    placeholder="DB Agent (:8092)"
                                    options={agents.map((a) => ({
                                      value: a.id,
                                      label: `${a.name || a.id} (${a.base_url})`,
                                    }))}
                                  />
                                </Form.Item>
                                <Button type="link" size="small" danger onClick={() => remove(f.name)}>删除</Button>
                              </Space>
                            ))}
                            <Button
                              size="small"
                              onClick={() => add({
                                role: fields.length ? 'replica' : 'primary',
                                port: meta.defaults?.port,
                              })}
                            >
                              添加节点
                            </Button>
                          </>
                        )}
                      </Form.List>
                    </Form.Item>
                  )}
                  {engine === 'mysql' || engine === 'postgres' ? (
                    <Form.Item noStyle shouldUpdate={(a, b) => a.topology !== b.topology}>
                      {() => {
                        const topo = form.getFieldValue('topology')
                        const haAllowed = topo === 'master_replica'
                        return (
                          <>
                            <Form.Item
                              name="ha_enabled"
                              label="启用 HA（自建·部署时接入）"
                              valuePropName="checked"
                              extra={haAllowed ? (engine === 'postgres' ? '支持 pg_autofailover / Patroni 等；须先登记控制器' : '须先在「高可用服务维护」登记对应架构控制器') : '仅主从拓扑可选 HA；当前拓扑不可启用'}
                            >
                              <Switch disabled={!haAllowed} onChange={(on) => { if (!on) setMhaEnvOk(false) }} />
                            </Form.Item>
                            <Form.Item noStyle shouldUpdate={(a, b) => a.ha_enabled !== b.ha_enabled || a.ha_mode !== b.ha_mode}>
                              {() => {
                                if (!form.getFieldValue('ha_enabled') || !haAllowed) return null
                                const mode = form.getFieldValue('ha_mode') || ''
                                const ctrls = haControllers.filter((c) => compatibleHAController(mode, c.kind))
                                const needCtrl = needsHAController(mode)
                                return (
                                  <>
                                    <Form.Item name="ha_mode" label="HA 模式" rules={[{ required: true }]}>
                                      <Select
                                        options={(meta.ha_modes || []).filter(Boolean).map((m) => ({
                                          value: m,
                                          label: HA_MODE_LABELS[m] || m,
                                        }))}
                                        onChange={() => {
                                          form.setFieldsValue({ controller_id: undefined })
                                          setMhaEnvOk(false)
                                        }}
                                      />
                                    </Form.Item>
                                    {needCtrl && (
                                      <Form.Item
                                        name="controller_id"
                                        label="HA 控制器"
                                        rules={[{ required: true, message: '请选择已登记控制器' }]}
                                        extra={ctrls.length === 0 ? '无匹配控制器，请先到高可用服务维护登记' : '须与模式兼容'}
                                      >
                                        <Select
                                          allowClear
                                          placeholder={ctrls.length ? '选择控制器' : '无可用控制器'}
                                          options={ctrls.map((c) => ({
                                            value: c.id,
                                            label: `${c.name} (${c.kind})`,
                                          }))}
                                          onChange={() => setMhaEnvOk(false)}
                                          disabled={ctrls.length === 0}
                                        />
                                      </Form.Item>
                                    )}
                                    {(mode === 'mha' ||
                                      mode === 'orchestrator' ||
                                      mode === 'proxysql_repl' ||
                                      mode === 'pg_autofailover' ||
                                      mode === 'patroni' ||
                                      mode === 'repmgr') && (
                                      <Space style={{ marginBottom: 12 }} wrap>
                                        <Button
                                          size="small"
                                          disabled={!form.getFieldValue('controller_id')}
                                          onClick={async () => {
                                            const cid = form.getFieldValue('controller_id')
                                            if (!cid) return
                                            try {
                                              const { data } = await api.post(`/ha/controllers/${cid}/env-check`, {})
                                              if (data?.data?.ok === false) {
                                                setMhaEnvOk(false)
                                                message.error(data?.data?.summary || '环境检查未通过')
                                                return
                                              }
                                              setMhaEnvOk(true)
                                              message.success(data?.data?.summary || '环境检查通过')
                                            } catch (e: any) {
                                              setMhaEnvOk(false)
                                              message.error(e?.response?.data?.message || String(e))
                                            }
                                          }}
                                        >
                                          {mode === 'mha'
                                            ? 'MHA 环境检查'
                                            : mode === 'orchestrator'
                                              ? 'Orchestrator 环境检查'
                                              : mode === 'proxysql_repl'
                                                ? 'ProxySQL 环境检查'
                                                : mode === 'pg_autofailover'
                                                  ? 'pg_autofailover 环境检查'
                                                  : mode === 'repmgr'
                                                    ? 'repmgr 环境检查'
                                                    : 'Patroni 环境检查'}
                                        </Button>
                                        <Tag color={mhaEnvOk ? 'success' : 'default'}>{mhaEnvOk ? '已通过' : '未检查'}</Tag>
                                      </Space>
                                    )}
                                    <Form.Item
                                      name="ha_vip"
                                      label="业务 VIP（推荐）"
                                      extra={
                                        <span>
                                          登记业务入口 VIP；切换钩子注入环境变量 <code>HA_VIP</code>。平台<strong>不</strong>自动漂移该地址。
                                        </span>
                                      }
                                      rules={[
                                        {
                                          pattern: /^$|^((25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(25[0-5]|2[0-4]\d|[01]?\d\d?)$/,
                                          message: '请填写合法 IPv4，或留空',
                                        },
                                      ]}
                                    >
                                      <Input placeholder="如 10.0.0.50" allowClear />
                                    </Form.Item>
                                    <Form.Item
                                      name="vip_hook_script"
                                      label="切换后 VIP 钩子脚本"
                                      extra="写入 HA 集群 Remark（vip_hook_*）；经 Platform Agent 白名单触发，不代写脚本内容"
                                    >
                                      <Input placeholder="/etc/auto-dba/hooks/vip.sh" allowClear />
                                    </Form.Item>
                                    <Form.Item name="vip_hook_args" label="钩子参数" extra="空格或逗号分隔，写入 vip_hook_args">
                                      <Input placeholder="--vip 10.0.0.50" allowClear />
                                    </Form.Item>
                                    {mode === 'mha' && (
                                      <>
                                        <Alert
                                          type="info"
                                          showIcon
                                          style={{ marginBottom: 12 }}
                                          message="MySQL · MHA 推荐路径"
                                          description="填写业务 VIP + failover 脚本 → 复制验收后自动下发 app.cnf → 切主走 HA 维护（钩子带 HA_VIP）。VIP 漂移脚本由现场维护。"
                                        />
                                        <Form.Item name="mha_user" label="MHA MySQL 用户" extra="写入 app.cnf，默认 mha"><Input placeholder="mha" /></Form.Item>
                                        <Form.Item name="mha_password" label="MHA MySQL 密码"><Input.Password placeholder="现场账号密码" /></Form.Item>
                                        <Form.Item name="mha_ssh_user" label="MHA SSH 用户"><Input placeholder="root" /></Form.Item>
                                        <Form.Item
                                          name="mha_failover_script"
                                          label="Failover 脚本"
                                          extra="写入 app.cnf 的 master_ip_failover_script（现场维护 VIP 漂移逻辑）"
                                        >
                                          <Input placeholder="/usr/local/bin/master_ip_failover" allowClear />
                                        </Form.Item>
                                        <Form.Item
                                          name="mha_online_change_script"
                                          label="在线切主脚本"
                                          extra="可选，对应 master_ip_online_change_script"
                                        >
                                          <Input placeholder="/usr/local/bin/master_ip_online_change" allowClear />
                                        </Form.Item>
                                      </>
                                    )}
                                    <div style={{ marginBottom: 12, fontSize: 12, opacity: 0.75 }}>
                                      需要 keepalived 托管浮动 IP 时，请前往{' '}
                                      <Link to="/ops/cluster-deploy/lvs">接入层代理 · LVS</Link>
                                      ，勿与本装机流水线混配 keepalived。
                                    </div>
                                  </>
                                )
                              }}
                            </Form.Item>
                          </>
                        )
                      }}
                    </Form.Item>
                  ) : null}
                </>
              )
            }}
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加节点（提交审批）"
        open={nodeOpen}
        onCancel={() => setNodeOpen(false)}
        destroyOnClose
        footer={[
          <Button key="cancel" onClick={() => setNodeOpen(false)}>取消</Button>,
          <Button key="check" loading={nodePrecheckBusy} onClick={() => { void runNodePrecheck() }}>基础校验</Button>,
          <Button
            key="submit"
            type="primary"
            loading={nodeSubmitBusy}
            disabled={!nodePrecheck?.ok || nodePrecheckBusy}
            onClick={() => { void submitAddNode() }}
          >
            提交工单
          </Button>,
        ]}
      >
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message="审批通过后登记节点；扩容编排按引擎实现进度启用。" />
        <Form
          form={nodeForm}
          layout="vertical"
          onValuesChange={(changed) => {
            setNodePrecheck(null)
            if ('host_instance_id' in changed) void onNodeHostChange(changed.host_instance_id)
          }}
        >
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={roleOptions} />
          </Form.Item>
          <Form.Item
            name="host_instance_id"
            label="主机（已登记主机）"
            rules={[{ required: true, message: '请选择主机' }]}
            extra={nodeHostInst ? `节点地址：${hostPrimaryAddr(nodeHostInst) || '—'}` : '主机来自实例管理中 db_type=host 的登记'}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择主机"
              notFoundContent="暂无主机实例，请先在实例管理登记主机"
              options={hosts.map((h) => ({ value: h.id, label: `${h.name} (${hostPrimaryAddr(h) || '无地址'})` }))}
            />
          </Form.Item>
          <Form.Item name="host" hidden rules={[{ required: true, message: '所选主机缺少 Host/域名' }]}><Input /></Form.Item>
          <Form.Item name="port" label="端口" extra="留空按角色默认端口（如 sentinel 26379）">
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="agent_id"
            label="DB Agent"
            dependencies={['host_instance_id']}
            rules={[
              { required: true, message: '自建装机必须绑定与主机同地址的 DB Agent（:8092）' },
              {
                validator: async (_, val) => {
                  if (!val || !nodeHostInst) return
                  const ag = agents.find((a) => a.id === val)
                  if (ag && !agentMatchesHost(ag, nodeHostInst)) throw new Error(agentMismatchMessage(ag, nodeHostInst))
                },
              },
            ]}
            extra={nodeHostInst && nodeAgentOptions.length === 0 ? (
              <span>
                该主机尚无 DB Agent（:8092），
                <Button type="link" size="small" style={{ padding: 0 }} onClick={() => { setNodeOpen(false); setTab('agents') }}>去下发 Agent</Button>
              </span>
            ) : '只列出与所选主机地址一致的 Agent'}
          >
            <Select
              showSearch
              optionFilterProp="label"
              disabled={!nodeHostInst}
              options={nodeAgentOptions}
              placeholder={nodeHostInst ? '选择 DB Agent' : '请先选择主机'}
            />
          </Form.Item>
          <Form.Item
            name="assigned_to"
            label="审批人"
            rules={[{ required: true, message: '请选择审批人' }]}
            extra={nodeHostInst && !nodeApprovers.loading && nodeApprovers.approvers.length === 0
              ? '该主机未配置审批人，请在实例管理为主机配置审批人'
              : '按所选主机的审批人配置加载'}
          >
            <Select
              showSearch
              allowClear
              loading={nodeApprovers.loading}
              disabled={!nodeHostInst}
              placeholder={nodeHostInst ? '选择审批人' : '请先选择主机'}
              options={nodeApprovers.approvers.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>
          <Form.Item name="description" label="事由"><Input.TextArea rows={2} /></Form.Item>
        </Form>
        <NodePrecheckPanel result={nodePrecheck} busy={nodePrecheckBusy} />
      </Modal>

      <Modal
        title={ticketDraft?.title || '提交审批'}
        open={!!ticketDraft}
        confirmLoading={ticketBusy}
        okText="提交工单"
        okButtonProps={{ danger: ticketDraft?.action === 'delete_cluster' }}
        onCancel={() => {
          setTicketDraft(null)
          ticketForm.resetFields()
          void ticketApprovers.load(undefined)
        }}
        onOk={async () => {
          if (!ticketDraft) return
          const v = await ticketForm.validateFields()
          setTicketBusy(true)
          try {
            await submitDbTicket(
              ticketDraft.engine,
              ticketDraft.clusterId,
              ticketDraft.action,
              ticketDraft.payload,
              v.assigned_to,
              v.description,
            )
            setTicketDraft(null)
            ticketForm.resetFields()
            void ticketApprovers.load(undefined)
            await load()
          } catch (e: any) {
            message.error(e?.response?.data?.message || String(e))
          } finally {
            setTicketBusy(false)
          }
        }}
        destroyOnClose
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={ticketDraft?.summary || ''} />
        <Form form={ticketForm} layout="vertical">
          <Form.Item
            name="assigned_to"
            label="审批人"
            rules={[{ required: true, message: '请选择审批人' }]}
            extra={
              ticketApprovers.loading
                ? '正在加载审批人…'
                : ticketApprovers.approvers.length === 0
                  ? '未加载到审批人：请配置全局 sync_default_approver，或确保本租户有成员'
                  : '按集群节点主机审批人加载；无主机时回退全局 sync_default_approver'
            }
          >
            <Select
              showSearch
              allowClear
              loading={ticketApprovers.loading}
              placeholder="选择审批人"
              options={ticketApprovers.approvers.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>
          <Form.Item name="description" label="事由"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>

      <Modal title="手工登记 DB Agent" open={agentOpen} onCancel={() => setAgentOpen(false)} onOk={() => agentForm.submit()} destroyOnClose>
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message="Base URL 须指向 DB Agent，默认端口 :8092（/v1/db/*），勿填 Node Agent :8091。" />
        <Form
          form={agentForm}
          layout="vertical"
          onFinish={async (v) => {
            try {
              await api.post('/db-deploy/agents', v)
              message.success('已登记')
              setAgentOpen(false)
              load()
            } catch (e: any) {
              message.error(e?.response?.data?.message || String(e))
            }
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]}><Input placeholder="http://10.x.x.x:8092" /></Form.Item>
          <Form.Item name="username" label="Basic 用户"><Input /></Form.Item>
          <Form.Item name="password" label="Basic 密码"><Input.Password /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title="SSH 下发 DB Agent"
        open={deployModal}
        confirmLoading={deployBusy}
        onCancel={() => setDeployModal(false)}
        onOk={async () => {
          const v = await deployForm.validateFields()
          setDeployBusy(true)
          try {
            const res = await api.post('/db-deploy/agents/deploy', {
              host_instance_id: v.host_instance_id,
              listen_port: v.listen_port || 8092,
              remote_dir: v.remote_dir,
              escalate: v.escalate ? 'sudo_su' : '',
            })
            const deployId = res.data?.data?.deploy_id as string
            message.success('已提交下发，进度见下发记录，完成后会通知')
            setDeployModal(false)
            if (deployId) deployTracker.track(deployId)
            void loadAgentLists()
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message)
          } finally {
            setDeployBusy(false)
          }
        }}
        destroyOnClose
        okText="开始下发"
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="下发前请确认控制面已构建制品；对已运行集群安全"
          description={(
            <>
              <div><code>cd deploy/db-agent && ./build.sh</code></div>
              <div style={{ marginTop: 6 }}>
                仅安装/重启 DB Agent，不启停业务库。监听端口请用 8092（约定 8090–8199），禁止填业务端口。
              </div>
            </>
          )}
        />
        <Form form={deployForm} layout="vertical" initialValues={{ listen_port: 8092, remote_dir: '/home/sysadmin/auto-dba/db-agent', escalate: false }}>
          <Form.Item
            name="host_instance_id"
            label="目标主机"
            rules={[{ required: true, message: '请选择 host 类型实例' }]}
            extra="须在实例管理登记类型为 host；依赖 terminal.ssh_identity_file。"
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder={hosts.length ? '选择主机' : '暂无 host 实例'}
              options={hosts.map((h) => ({
                value: h.id,
                disabled: deployingHostIds.has(h.id),
                label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}${deployingHostIds.has(h.id) ? '（下发中）' : ''}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="escalate" label="提权执行（sudo su -）" valuePropName="checked">
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item
            name="listen_port"
            label="监听端口"
            extra="默认 8092；勿与平台 8090、Node 8091 及业务端口冲突"
            rules={[{ required: true, type: 'number', min: 1024, max: 65535, message: '端口须在 1024–65535' }]}
          >
            <InputNumber min={1024} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="remote_dir" label="远程目录">
            <Input placeholder="/home/sysadmin/auto-dba/db-agent" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="下发日志"
        open={!!deployLog}
        onCancel={() => setDeployLog(null)}
        footer={<Button onClick={() => setDeployLog(null)}>关闭</Button>}
        width={720}
      >
        {deployLog && (
          <>
            <p>
              状态
              {' '}
              <Tag color={STATUS_COLOR[deployLog.status] || 'default'}>{deployLog.status}</Tag>
              {deployLog.base_url ? ` · ${deployLog.base_url}` : ''}
            </p>
            <pre className="db-change-log">{deployLog.log_text || '（无日志）'}</pre>
          </>
        )}
      </Modal>
    </div>
  )
}
