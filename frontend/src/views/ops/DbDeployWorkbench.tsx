import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Breadcrumb, Button, Card, Col, Form, Input, InputNumber, Modal, Row, Select, Space, Switch, Table, Tabs, Tag, message,
} from 'antd'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import api from '../../api/request'
import {
  NodePrecheckPanel, agentMatchesHost, agentMismatchMessage, hostPrimaryAddr, useHostApprovers,
  type NodePrecheckResult,
} from './node-precheck/nodePrecheck'
import './DbDeployWorkbench.css'

export type DbEngine = string

type Release = {
  id: string
  engine: string
  version: string
  object_key: string
  checksum: string
  arch?: string
  enabled: boolean
}

type Cluster = {
  id: string
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

const INIT_CHANGE_TYPES = ['cluster_deploy', 'ha_init']

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
  const [tab, setTab] = useState('clusters')
  const [createOpen, setCreateOpen] = useState(false)
  const [createMode, setCreateMode] = useState<'create' | 'register'>('create')
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [nodeOpen, setNodeOpen] = useState(false)
  const [nodePrecheck, setNodePrecheck] = useState<NodePrecheckResult | null>(null)
  const [nodePrecheckBusy, setNodePrecheckBusy] = useState(false)
  const [nodeSubmitBusy, setNodeSubmitBusy] = useState(false)
  const nodeApprovers = useHostApprovers()
  const [agentOpen, setAgentOpen] = useState(false)
  const [deployModal, setDeployModal] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [deployLog, setDeployLog] = useState<{ id: string; status: string; log_text?: string; base_url?: string } | null>(null)
  const [topoFilter, setTopoFilter] = useState<string>('all')
  const [haControllers, setHaControllers] = useState<HAController[]>([])
  const [mhaEnvOk, setMhaEnvOk] = useState(false)
  const [form] = Form.useForm()
  const [relForm] = Form.useForm()
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

  const openTicketDraft = (draft: NonNullable<typeof ticketDraft>) => {
    ticketForm.resetFields()
    setTicketDraft(draft)
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

  const showClusterChangeLogs = async (
    clusterId: string,
    opts?: { types?: string[]; excludeTypes?: string[]; emptyHint?: string },
  ) => {
    setTab('changes')
    const res = await api.get('/db-deploy/changes', { params: { cluster_id: clusterId, limit: 50 } })
    const list: DbChange[] = res.data?.data || []
    setChanges(list)
    let filtered = list
    if (opts?.types?.length) {
      const allow = new Set(opts.types)
      filtered = list.filter((c) => allow.has(c.type))
    } else if (opts?.excludeTypes?.length) {
      const deny = new Set(opts.excludeTypes)
      filtered = list.filter((c) => !deny.has(c.type))
    }
    const target = filtered.find((c) => c.status === 'failed' || c.status === 'running' || c.status === 'pending')?.id
      || filtered[0]?.id
    if (target) {
      await pollChange(target)
      return
    }
    setChangeDetail(null)
    if (opts?.emptyHint) message.info(opts.emptyHint)
  }

  const nodeHostId = Form.useWatch('host_instance_id', nodeForm) as string | undefined
  const nodeHostInst = useMemo(() => hosts.find((h) => h.id === nodeHostId) || null, [hosts, nodeHostId])
  const nodeAgentOptions = useMemo(
    () => agents
      .filter((a) => agentMatchesHost(a, nodeHostInst))
      .map((a) => ({ value: a.id, label: `${a.name || a.id} (${a.base_url})` })),
    [agents, nodeHostInst],
  )

  const onNodeHostChange = async (hostId?: string) => {
    const inst = hosts.find((h) => h.id === hostId) || null
    const matched = agents.filter((a) => agentMatchesHost(a, inst))
    nodeForm.setFieldsValue({
      host: hostPrimaryAddr(inst),
      agent_id: matched.length === 1 ? matched[0].id : undefined,
      assigned_to: undefined,
    })
    const list = await nodeApprovers.load(hostId)
    if (nodeForm.getFieldValue('host_instance_id') === hostId && list.length > 0) {
      nodeForm.setFieldsValue({ assigned_to: list[0] })
    }
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
        { value: 'single', label: '单机' },
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
                    ]}
                  />
                </Col>
                {selected && (
                  <Col xs={24} lg={10}>
                    <Card
                      title={selected.name}
                      extra={(
                        <Space wrap size="small">
                          <Button type="link" size="small" onClick={() => {
                            void showClusterChangeLogs(selected.id, {
                              types: INIT_CHANGE_TYPES,
                              emptyHint: '暂无初始化/部署日志',
                            })
                          }}
                          >
                            部署日志
                          </Button>
                          <Button type="link" size="small" onClick={() => {
                            void showClusterChangeLogs(selected.id, {
                              excludeTypes: INIT_CHANGE_TYPES,
                              emptyHint: '暂无运行变更日志',
                            })
                          }}
                          >
                            运行日志
                          </Button>
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
                          <p><b>厂商</b>：{selected.cloud_vendor || '—'} · <b>云实例 ID</b>：{selected.cloud_instance_id || '—'}</p>
                          <p><b>Region</b>：{selected.cloud_region || '—'} · <b>关联实例</b>：{selected.linked_instance_id || '—'}</p>
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
                            { title: '角色', dataIndex: 'role', width: 90 },
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
                              role: selected.topology === 'standalone' ? 'single' : 'primary',
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
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={changes}
                  onRow={(row) => ({
                    onClick: () => { void pollChange(row.id) },
                    style: { cursor: 'pointer', background: changeDetail?.id === row.id ? 'var(--ant-color-primary-bg, #e6f4ff)' : undefined },
                  })}
                  columns={[
                    { title: '类型', dataIndex: 'type', width: 140 },
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
                      {INIT_CHANGE_TYPES.includes(changeDetail.type) ? '部署日志' : '运行日志'}
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
              <>
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="请先将安装包上传到配置中心 OSS，再在此登记 object_key 与 sha256"
                />
                <Button type="primary" style={{ marginBottom: 12 }} onClick={() => { relForm.resetFields(); relForm.setFieldsValue({ engine: engine === 'redis' ? 'redis' : engine, arch: 'x86_64', enabled: true }); setReleaseOpen(true) }}>
                  登记版本
                </Button>
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={releases}
                  columns={[
                    { title: '引擎', dataIndex: 'engine', width: 90 },
                    { title: '版本', dataIndex: 'version', width: 120 },
                    { title: 'object_key', dataIndex: 'object_key', ellipsis: true },
                    { title: 'checksum', dataIndex: 'checksum', ellipsis: true, width: 140 },
                    {
                      title: '启用',
                      dataIndex: 'enabled',
                      width: 70,
                      render: (v: boolean) => (v ? '是' : '否'),
                    },
                    {
                      title: '操作',
                      width: 160,
                      render: (_: unknown, r: Release) => (
                        <Space>
                          <Button
                            size="small"
                            onClick={async () => {
                              try {
                                await api.post(`/db-deploy/releases/${r.id}/presign`)
                                message.success('签发成功（URL 不下发到界面）')
                              } catch (e: any) {
                                message.error(e?.response?.data?.message || String(e))
                              }
                            }}
                          >
                            试签发
                          </Button>
                          <Button size="small" danger onClick={async () => { await api.delete(`/db-deploy/releases/${r.id}`); load() }}>删除</Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </>
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
                      。云托管单入口无需 Agent。
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
                    { title: '状态', dataIndex: 'status', width: 90, render: (s: string) => <Tag color={STATUS_COLOR[s]}>{s}</Tag> },
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
            ),
          },
        ].filter((it) => allowDeploy || (it.key !== 'releases' && it.key !== 'agents'))}
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
              const res = await api.post('/db-deploy/clusters', payload)
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
          <Form.Item name="environment" label="环境" rules={[{ required: true }]}><Input placeholder="环境标识" /></Form.Item>
          <Form.Item name="topology" label="拓扑" rules={[{ required: true }]}>
            <Select
              options={(meta.topologies || [])
                .filter((t) => createMode === 'register' || t.kind !== 'cloud')
                .map((t) => ({ value: t.id, label: t.label }))}
              onChange={(val) => {
                if (val === 'cloud_endpoint') {
                  form.setFieldsValue({ deploy_kind: 'cloud', engine: engine === 'redis' ? 'redis' : engine, ha_enabled: false })
                  setMhaEnvOk(false)
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
                    <Form.Item name="endpoint_host" label="访问入口 Host/域名" rules={[{ required: true }]}><Input placeholder="vip.xxx.rds.aliyuncs.com" /></Form.Item>
                    <Form.Item name="endpoint_port" label="端口" rules={[{ required: true }]}><InputNumber style={{ width: '100%' }} /></Form.Item>
                    <Form.Item name="cloud_vendor" label="云厂商"><Input placeholder="aliyun / tencent / huawei / aws" /></Form.Item>
                    <Form.Item name="cloud_instance_id" label="云实例 ID"><Input /></Form.Item>
                    <Form.Item name="cloud_region" label="Region"><Input placeholder="cn-hangzhou / ap-guangzhou" /></Form.Item>
                    <Form.Item name="linked_instance_id" label="关联配置中心实例 ID" extra="可选，便于监控/闪回复用云字段"><Input /></Form.Item>
                    <Form.Item name="version" label="版本备注（可选）"><Input placeholder="如 8.0 / 16" /></Form.Item>
                  </>
                )
              }
              return (
                <>
                  <Form.Item
                    name="release_id"
                    label="版本（DbRelease）"
                    rules={createMode === 'create' ? [{ required: true }] : []}
                  >
                    <Select
                      allowClear
                      options={releases.filter((r) => r.enabled).map((r) => ({
                        value: r.id,
                        label: `${r.engine} ${r.version}`,
                      }))}
                      placeholder="从版本目录选择"
                    />
                  </Form.Item>
                  <Form.Item name="software_base" label="软件目录"><Input /></Form.Item>
                  <Form.Item name="data_root" label="数据目录"><Input /></Form.Item>
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

      <Modal title="登记版本" open={releaseOpen} onCancel={() => setReleaseOpen(false)} onOk={() => relForm.submit()} destroyOnClose>
        <Form
          form={relForm}
          layout="vertical"
          onFinish={async (v) => {
            try {
              await api.post('/db-deploy/releases', v)
              message.success('已登记')
              setReleaseOpen(false)
              load()
            } catch (e: any) {
              message.error(e?.response?.data?.message || String(e))
            }
          }}
        >
          <Form.Item name="engine" label="引擎" rules={[{ required: true }]}>
            <Select options={
              engine === 'redis'
                ? [{ value: 'redis', label: 'redis' }, { value: 'codis', label: 'codis' }]
                : [{ value: engine, label: engine }]
            }
            />
          </Form.Item>
          <Form.Item name="version" label="版本号" rules={[{ required: true }]}><Input placeholder="8.0.36 / 16.4 / 7.2.4" /></Form.Item>
          <Form.Item name="object_key" label="OSS object_key" rules={[{ required: true }]}><Input placeholder="db-artifacts/mysql/8.0.36/x86_64/xxx.tar.gz" /></Form.Item>
          <Form.Item name="checksum" label="sha256" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="arch" label="架构"><Input /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
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
        onCancel={() => { setTicketDraft(null); ticketForm.resetFields() }}
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
          <Form.Item name="assigned_to" label="审批人" extra="留空则使用全局 sync_default_approver">
            <Input placeholder="默认取全局同步审批人" />
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
            message.success('已提交下发')
            setDeployModal(false)
            if (deployId) {
              for (let i = 0; i < 60; i++) {
                await new Promise((r) => setTimeout(r, 2000))
                const dRes = await api.get(`/db-deploy/agents/deploys/${deployId}`)
                const depRow = dRes.data?.data
                if (depRow && depRow.status !== 'pending' && depRow.status !== 'running') {
                  setDeployLog(depRow)
                  await load()
                  if (depRow.status === 'success') message.success('下发成功，已登记 DB Agent')
                  else message.error('下发失败，请查看日志')
                  break
                }
              }
            } else {
              await load()
            }
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
          message="下发前请确认控制面已构建制品"
          description={<code>cd deploy/db-agent && ./build.sh</code>}
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
                label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="escalate" label="提权执行（sudo su -）" valuePropName="checked">
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item name="listen_port" label="监听端口" extra="默认 8092；勿与平台 Agent 8090、StarRocks Node Agent 8091 冲突">
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
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
