import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  message,
} from 'antd'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import ResponsiveTable from '../../components/ResponsiveTable'
import './HaMaintain.css'

type HACluster = {
  id: string
  name: string
  engine: string
  env_id: string
  mode: string
  controller_id?: string
  maint_status: string
  vip?: string
  remark?: string
  members?: HAMember[]
  updated_at?: string
}

type HAMember = {
  id?: string
  instance_id: string
  role: string
  priority?: number
  enabled?: boolean
  instance_name?: string
  host?: string
  port?: number
}

type Topology = {
  cluster_id: string
  name: string
  engine: string
  maint_status: string
  probed_at: string
  members: Array<{
    instance_id: string
    instance_name: string
    host: string
    port: number
    role: string
    registered_role: string
    healthy: boolean
    lag_seconds?: number
    detail?: string
    error?: string
  }>
  warnings?: string[]
}

type ModeOpt = { code: string; label: string; hint?: string }
type KindOpt = { code: string; label: string; engine: string }

const ENGINE_OPTS = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'redis', label: 'Redis' },
]

const ACTION_OPTS = [
  { value: 'enter_maint', label: '进入维护' },
  { value: 'exit_maint', label: '退出维护' },
  { value: 'switchover', label: '计划内切换' },
  { value: 'failover_dead', label: '主宕机强制切换' },
]

const actionLabel = (v: string) => ACTION_OPTS.find((a) => a.value === v)?.label || v

/** 与后端 HAModesForEngine / HAControllerKinds 对齐；meta 接口 404 时兜底，避免建集群表单空白 */
const FALLBACK_HA_META = {
  modes: {
    mysql: [
      { code: 'manual', label: '人工/未托管', hint: '仅登记拓扑，切换按手册执行' },
      { code: 'async_repl', label: '异步复制（人工切换）', hint: '主从复制，无托管工具' },
      { code: 'orchestrator', label: '复制 + Orchestrator', hint: 'graceful-master-takeover' },
      { code: 'mha', label: '复制 + MHA', hint: 'Master High Availability' },
      { code: 'proxysql_repl', label: 'ProxySQL + 复制', hint: '流量入口由 ProxySQL 转发' },
      { code: 'mgr', label: 'MGR / InnoDB Cluster', hint: '组复制共识' },
      { code: 'pxc', label: 'PXC / Galera', hint: '同步多主' },
      { code: 'shared_storage', label: 'DRBD + Pacemaker', hint: '共享存储' },
      { code: 'k8s_operator', label: 'K8s MySQL Operator', hint: '云原生 Operator' },
    ],
    postgres: [
      { code: 'manual', label: '人工/未托管', hint: '仅登记拓扑' },
      { code: 'async_repl', label: '流复制（人工切换）', hint: '原生 streaming replication' },
      { code: 'pg_autofailover', label: 'pg_auto_failover', hint: 'Monitor + Keeper，pg_autoctl 编排' },
      { code: 'patroni', label: 'Patroni', hint: 'DCS 选主 + REST switchover/failover' },
      { code: 'repmgr', label: 'repmgr', hint: '流复制 + repmgr；计划切换经 Agent' },
      { code: 'k8s_operator', label: 'K8s PG Operator', hint: 'CloudNativePG 等' },
    ],
    redis: [
      { code: 'manual', label: '人工/未托管', hint: '仅登记拓扑' },
      { code: 'sentinel', label: 'Redis Sentinel', hint: 'SENTINEL FAILOVER' },
      { code: 'cluster', label: 'Redis Cluster', hint: '分片集群，不按单主从切换' },
    ],
  } as Record<string, ModeOpt[]>,
  defaults: { mysql: 'manual', postgres: 'manual', redis: 'manual' } as Record<string, string>,
  controller_kinds: [
    { code: 'orchestrator', label: 'Orchestrator', engine: 'mysql' },
    { code: 'mha', label: 'MHA', engine: 'mysql' },
    { code: 'proxysql', label: 'ProxySQL', engine: 'mysql' },
    { code: 'pg_autofailover', label: 'pg_auto_failover', engine: 'postgres' },
    { code: 'patroni', label: 'Patroni', engine: 'postgres' },
    { code: 'repmgr', label: 'repmgr', engine: 'postgres' },
    { code: 'sentinel', label: 'Sentinel', engine: 'redis' },
  ] as KindOpt[],
}

export default function HaMaintain() {
  const [tab, setTab] = useState('observe')
  const [engine, setEngine] = useState<string | undefined>()
  const [clusters, setClusters] = useState<HACluster[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<HACluster | null>(null)
  const [topo, setTopo] = useState<Topology | null>(null)
  const [topoLoading, setTopoLoading] = useState(false)
  const [tasks, setTasks] = useState<any[]>([])
  const [envs, setEnvs] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [changeOpen, setChangeOpen] = useState(false)
  const [form] = Form.useForm()
  const [changeForm] = Form.useForm()
  const [ctrlForm] = Form.useForm()
  const [precheck, setPrecheck] = useState<any>(null)
  const [modeMeta, setModeMeta] = useState<Record<string, ModeOpt[]>>({})
  const [modeDefaults, setModeDefaults] = useState<Record<string, string>>({})
  const [kindMeta, setKindMeta] = useState<KindOpt[]>([])
  const [controllers, setControllers] = useState<any[]>([])
  const [ctrlOpen, setCtrlOpen] = useState(false)
  const [platAgents, setPlatAgents] = useState<any[]>([])
  const [ctrlSource, setCtrlSource] = useState<'manual' | 'agent'>('manual')
  const [candidateOpts, setCandidateOpts] = useState<{ value: string; label: string }[]>([])
  const [dbDeployClusters, setDbDeployClusters] = useState<{ id: string; name: string }[]>([])
  const [dbDeployId, setDbDeployId] = useState<string | undefined>()
  const [mhaConf, setMhaConf] = useState<any>(null)
  const [mhaOptsForm] = Form.useForm()
  const [mhaBusy, setMhaBusy] = useState(false)
  const [mgrStatus, setMgrStatus] = useState<any>(null)
  const [mhaReleases, setMhaReleases] = useState<any[]>([])
  const [mhaOss, setMhaOss] = useState<any>(null)
  const [ossSettings, setOssSettings] = useState<any[]>([])
  const [installReleaseId, setInstallReleaseId] = useState<string>()
  const [installLog, setInstallLog] = useState('')
  const [relForm] = Form.useForm()
  const [artForm] = Form.useForm()
  const [ossForm] = Form.useForm()
  const [relOpen, setRelOpen] = useState(false)
  const [artOpen, setArtOpen] = useState(false)
  const [artReleaseId, setArtReleaseId] = useState<string>()
  const [pgCtrlState, setPgCtrlState] = useState<any>(null)
  const [pgafReleases, setPgafReleases] = useState<any[]>([])
  const [pgafOss, setPgafOss] = useState<any>(null)
  const [pgafOssForm] = Form.useForm()
  const [pgafRelForm] = Form.useForm()
  const [pgafArtForm] = Form.useForm()
  const [pgafRelOpen, setPgafRelOpen] = useState(false)
  const [pgafArtOpen, setPgafArtOpen] = useState(false)
  const [pgafArtReleaseId, setPgafArtReleaseId] = useState<string>()
  const [pgafInstallId, setPgafInstallId] = useState<string>()
  const [pgafInstallLog, setPgafInstallLog] = useState('')
  const [patroniReleases, setPatroniReleases] = useState<any[]>([])
  const [patroniOss, setPatroniOss] = useState<any>(null)
  const [patroniOssForm] = Form.useForm()
  const [patroniRelForm] = Form.useForm()
  const [patroniArtForm] = Form.useForm()
  const [patroniRelOpen, setPatroniRelOpen] = useState(false)
  const [patroniArtOpen, setPatroniArtOpen] = useState(false)
  const [patroniArtReleaseId, setPatroniArtReleaseId] = useState<string>()
  const [patroniInstallId, setPatroniInstallId] = useState<string>()
  const [patroniInstallLog, setPatroniInstallLog] = useState('')
  const createEngine = Form.useWatch('engine', form) || 'mysql'
  const createMode = Form.useWatch('mode', form)
  const ctrlKind = Form.useWatch('kind', ctrlForm)
  const ctrlAgentId = Form.useWatch('agent_id', ctrlForm)

  const loadClusters = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/ha/clusters', { params: { engine } })
      setClusters(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载集群失败')
    } finally {
      setLoading(false)
    }
  }, [engine])

  const loadTasks = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/tasks')
      setTasks(data.data || [])
    } catch {
      /* ignore */
    }
  }, [])

  const loadControllers = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/controllers')
      setControllers(data.data || [])
    } catch {
      /* ignore */
    }
  }, [])

  const loadPlatAgents = useCallback(async () => {
    try {
      const { data } = await api.get('/platform/agents', { params: { scope: 'ha' } })
      setPlatAgents(data.data || [])
    } catch {
      setPlatAgents([])
    }
  }, [])

  const loadMhaReleases = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/mha/releases')
      setMhaReleases(data.data || [])
    } catch {
      setMhaReleases([])
    }
  }, [])

  const loadMhaOss = useCallback(async () => {
    try {
      const [{ data: oss }, { data: settings }] = await Promise.all([
        api.get('/ha/mha/oss'),
        api.get('/oss-settings'),
      ])
      setMhaOss(oss.data || {})
      setOssSettings(settings.data || [])
      ossForm.setFieldsValue({
        oss_setting_id: oss.data?.oss_setting_id || undefined,
        prefix: oss.data?.prefix || 'mha-artifacts/',
      })
    } catch {
      /* ignore */
    }
  }, [ossForm])

  const loadMgrStatus = async (clusterId: string) => {
    try {
      const { data } = await api.get(`/ha/clusters/${clusterId}/mha/manager/status`)
      setMgrStatus(data.data)
    } catch (e: any) {
      setMgrStatus({ active: false, message: e?.response?.data?.message || '无法获取状态' })
    }
  }

  const loadPgCtrlState = async (row: HACluster) => {
    setPgCtrlState(null)
    try {
      if (row.mode === 'pg_autofailover') {
        const { data } = await api.get(`/ha/clusters/${row.id}/pgaf/state`)
        setPgCtrlState({ kind: 'pgaf', ...data.data })
      } else if (row.mode === 'patroni') {
        const { data } = await api.get(`/ha/clusters/${row.id}/patroni/cluster`)
        setPgCtrlState({ kind: 'patroni', members: data.data?.members || [] })
      }
    } catch (e: any) {
      setPgCtrlState({ kind: row.mode, error: e?.response?.data?.message || '状态拉取失败' })
    }
  }

  const loadPgafReleases = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/pgaf/releases')
      setPgafReleases(data.data || [])
    } catch {
      setPgafReleases([])
    }
  }, [])

  const loadPgafOss = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/pgaf/oss')
      setPgafOss(data.data || {})
      pgafOssForm.setFieldsValue({
        oss_setting_id: data.data?.oss_setting_id || undefined,
        prefix: data.data?.prefix || 'pgaf-artifacts/',
      })
    } catch {
      /* ignore */
    }
  }, [pgafOssForm])

  const loadPatroniReleases = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/patroni/releases')
      setPatroniReleases(data.data || [])
    } catch {
      setPatroniReleases([])
    }
  }, [])

  const loadPatroniOss = useCallback(async () => {
    try {
      const { data } = await api.get('/ha/patroni/oss')
      setPatroniOss(data.data || {})
      patroniOssForm.setFieldsValue({
        oss_setting_id: data.data?.oss_setting_id || undefined,
        prefix: data.data?.prefix || 'patroni-artifacts/',
      })
    } catch {
      /* ignore */
    }
  }, [patroniOssForm])

  useEffect(() => {
    loadClusters()
    loadTasks()
    loadControllers()
    loadPlatAgents()
    api.get('/environments').then((r) => setEnvs(r.data.data || [])).catch(() => {})
    api.get('/ha/meta/modes').then((r) => {
      const d = r.data.data || {}
      setModeMeta(Object.keys(d.modes || {}).length ? d.modes : FALLBACK_HA_META.modes)
      setModeDefaults(Object.keys(d.defaults || {}).length ? d.defaults : FALLBACK_HA_META.defaults)
      setKindMeta((d.controller_kinds || []).length ? d.controller_kinds : FALLBACK_HA_META.controller_kinds)
    }).catch(() => {
      setModeMeta(FALLBACK_HA_META.modes)
      setModeDefaults(FALLBACK_HA_META.defaults)
      setKindMeta(FALLBACK_HA_META.controller_kinds)
    })
  }, [loadClusters, loadTasks, loadControllers, loadPlatAgents])

  useEffect(() => {
    loadMhaReleases()
    loadMhaOss()
    loadPgafReleases()
    loadPgafOss()
    loadPatroniReleases()
    loadPatroniOss()
  }, [loadMhaReleases, loadMhaOss, loadPgafReleases, loadPgafOss, loadPatroniReleases, loadPatroniOss])

  const loadTopology = async (id: string) => {
    setTopoLoading(true)
    setTopo(null)
    try {
      const { data } = await api.get(`/ha/clusters/${id}/topology`)
      setTopo(data.data)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '拓扑探测失败')
    } finally {
      setTopoLoading(false)
    }
  }

  const onSelectCluster = async (row: HACluster) => {
    setSelected(row)
    const { data } = await api.get(`/ha/clusters/${row.id}`)
    setSelected(data.data)
    await loadTopology(row.id)
    if (data.data?.mode === 'mha') {
      await loadMhaConf(row.id)
      await loadMgrStatus(row.id)
      setPgCtrlState(null)
    } else if (data.data?.mode === 'pg_autofailover' || data.data?.mode === 'patroni') {
      setMhaConf(null)
      setMgrStatus(null)
      await loadPgCtrlState(data.data)
    } else {
      setMhaConf(null)
      setMgrStatus(null)
      setPgCtrlState(null)
    }
  }

  const openCreate = async () => {
    form.resetFields()
    form.setFieldsValue({ engine: 'mysql', mode: modeDefaults.mysql || 'manual', members: [{ role: 'primary' }, { role: 'replica' }] })
    setDbDeployId(undefined)
    setCreateOpen(true)
    await loadCandidates('mysql')
  }

  const loadCandidates = async (eng: string) => {
    try {
      const { data } = await api.get('/ha/candidates', { params: { engine: eng } })
      const inst = (data.data?.instances || []).map((i: any) => ({ value: i.instance_id, label: i.label }))
      setCandidateOpts(inst)
      setInstances(data.data?.instances || [])
      // 可导入的部署集群（按 cluster 去重）
      const seen = new Map<string, string>()
      for (const n of data.data?.deploy_nodes || []) {
        if (n.dbdeploy_cluster && !seen.has(n.dbdeploy_cluster)) {
          seen.set(n.dbdeploy_cluster, n.cluster_name || n.dbdeploy_cluster)
        }
      }
      setDbDeployClusters([...seen.entries()].map(([id, name]) => ({ id, name })))
    } catch {
      setCandidateOpts([])
      setDbDeployClusters([])
      // fallback
      try {
        const { data } = await api.get('/instances', { params: { db_type: eng === 'postgres' ? 'postgres' : eng } })
        const list = data.data || []
        setInstances(list)
        setCandidateOpts(list.map((i: any) => ({
          value: i.id,
          label: `${i.name} (${i.host || i.domain_name}:${i.port})`,
        })))
      } catch {
        setInstances([])
      }
    }
  }

  const importDbDeploy = async (id: string) => {
    setDbDeployId(id)
    try {
      const { data } = await api.post('/ha/candidates/import-dbdeploy', { dbdeploy_cluster_id: id })
      const d = data.data || {}
      form.setFieldsValue({
        name: d.cluster_name || form.getFieldValue('name'),
        mode: 'mha',
        controller_id: d.controller_id || form.getFieldValue('controller_id'),
        members: (d.members || []).map((m: any) => ({
          instance_id: m.instance_id,
          role: m.role,
        })),
        dbdeploy_cluster_id: id,
      })
      // 刷新候选以便展示新登记实例
      await loadCandidates('mysql')
      message.success(`已从部署集群导入 ${ (d.members || []).length } 个成员`)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '导入失败')
    }
  }

  const loadInstances = async (eng: string) => {
    await loadCandidates(eng)
  }

  const submitCreate = async () => {
    const values = await form.validateFields()
    const members = (values.members || []).filter((m: any) => m?.instance_id)
    if (values.mode === 'mha') {
      const p = members.filter((m: any) => m.role === 'primary').length
      const r = members.filter((m: any) => m.role === 'replica').length
      if (p !== 1 || r < 1) {
        message.error('MHA 必须恰好 1 个 primary，且至少 1 个 replica')
        return
      }
    }
    try {
      await api.post('/ha/clusters', {
        ...values,
        members,
        dbdeploy_cluster_id: values.dbdeploy_cluster_id || dbDeployId,
      })
      message.success('集群已创建')
      setCreateOpen(false)
      loadClusters()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '创建失败')
    }
  }

  const loadMhaConf = async (clusterId: string) => {
    try {
      const { data } = await api.get(`/ha/clusters/${clusterId}/mha/conf`)
      setMhaConf(data.data)
      mhaOptsForm.setFieldsValue({
        ...(data.data?.options || {}),
        conf_path: data.data?.conf_path,
        content: data.data?.content,
      })
    } catch (e: any) {
      setMhaConf(null)
      message.error(e?.response?.data?.message || '加载 MHA 配置失败')
    }
  }

  const openChange = () => {
    if (!selected) {
      message.warning('请先选择集群')
      return
    }
    changeForm.resetFields()
    changeForm.setFieldsValue({ cluster_id: selected.id, action: 'enter_maint' })
    setPrecheck(null)
    setChangeOpen(true)
  }

  const runPrecheck = async () => {
    const values = await changeForm.validateFields(['cluster_id', 'action', 'target_instance_id'])
    try {
      const { data } = await api.post('/ha/precheck', values)
      setPrecheck(data.data)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '预检失败')
    }
  }

  const submitChange = async () => {
    const values = await changeForm.validateFields()
    if (values.action === 'failover_dead' && !precheck?.ok) {
      message.error('强制切换必须先通过预检')
      return
    }
    try {
      const { data } = await api.post('/ha/changes', values)
      message.success('变更单已提交')
      setChangeOpen(false)
      loadTasks()
      if (data.data?.issue?.id) {
        message.info(`工单号 ${data.data.issue.ticket_no}`)
      }
    } catch (e: any) {
      const msg = e?.response?.data?.message || '提交失败'
      message.error(msg)
      if (e?.response?.data?.data) setPrecheck(e.response.data.data)
    }
  }

  const replicaOptions = useMemo(() => {
    const ms = selected?.members || []
    return ms
      .filter((m) => m.role !== 'primary')
      .map((m) => ({
        value: m.instance_id,
        label: `${m.instance_name || m.instance_id} (${m.role})`,
      }))
  }, [selected])

  const modeLabel = (eng: string, code: string) =>
    (modeMeta[eng] || []).find((m) => m.code === code)?.label || code

  const createModeOpts = (modeMeta[createEngine] || []).map((m) => ({ value: m.code, label: m.label }))
  const createModeHint = (modeMeta[createEngine] || []).find((m) => m.code === createMode)?.hint
  const roleOpts = (() => {
    const mgrLike = createEngine === 'mysql' && (createMode === 'mgr' || createMode === 'pxc')
    const opts = [
      { value: 'primary', label: mgrLike ? '主/成员' : 'primary' },
      { value: 'replica', label: mgrLike ? '成员' : 'replica' },
    ]
    if (createEngine === 'redis' && createMode === 'sentinel') {
      opts.push({ value: 'sentinel', label: 'sentinel' })
    }
    return opts
  })()

  const submitController = async () => {
    const values = await ctrlForm.validateFields()
    const payload: any = {
      name: values.name,
      engine: values.engine,
      kind: values.kind,
      remark: values.remark || '',
    }
    if (ctrlSource === 'agent') {
      payload.agent_id = values.agent_id
      payload.base_url = '' // 后端从 Agent 回填
    } else {
      payload.base_url = values.base_url
      payload.username = values.username || ''
      payload.password = values.password || ''
    }
    try {
      await api.post('/ha/controllers', payload)
      message.success('控制器已登记')
      setCtrlOpen(false)
      loadControllers()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '登记失败')
    }
  }

  const agentName = (id?: string) => {
    if (!id) return '-'
    const a = platAgents.find((x) => x.id === id)
    return a ? a.name : id.slice(0, 8)
  }

  return (
    <div className="ha-page">
      <div className="ha-toolbar">
        <Space wrap>
          <Select
            allowClear
            placeholder="引擎"
            style={{ width: 140 }}
            options={ENGINE_OPTS}
            value={engine}
            onChange={setEngine}
          />
          <Button onClick={loadClusters} loading={loading}>
            刷新
          </Button>
          <Button type="primary" onClick={openCreate}>
            登记集群
          </Button>
          <Button onClick={openChange} disabled={!selected}>
            发起变更
          </Button>
        </Space>
      </div>

      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'observe',
            label: '观测',
            children: (
              <div className="ha-split">
                <div className="ha-list">
                  <ResponsiveTable
                    rowKey="id"
                    loading={loading}
                    dataSource={clusters}
                    size="small"
                    pagination={false}
                    onRow={(row) => ({
                      onClick: () => onSelectCluster(row),
                      className: selected?.id === row.id ? 'ha-row-active' : undefined,
                    })}
                    columns={[
                      { title: '名称', dataIndex: 'name', minWidth: 140 },
                      {
                        title: '引擎',
                        dataIndex: 'engine',
                        width: 100,
                        render: (v) => <Tag>{v}</Tag>,
                      },
                      {
                        title: '模式',
                        dataIndex: 'mode',
                        minWidth: 140,
                        render: (v, r) => modeLabel(r.engine, v),
                      },
                      {
                        title: '状态',
                        dataIndex: 'maint_status',
                        width: 100,
                        render: (v) =>
                          v === 'maintaining' ? <Tag color="orange">维护中</Tag> : <Tag color="green">正常</Tag>,
                      },
                      { title: 'VIP', dataIndex: 'vip', width: 120 },
                    ]}
                  />
                </div>
                <div className="ha-detail">
                  {!selected && <Alert type="info" showIcon message="选择左侧集群查看拓扑" />}
                  {selected && (
                    <>
                      <div className="ha-detail-head">
                        <strong>{selected.name}</strong>
                        <span className="ha-muted">{modeLabel(selected.engine, selected.mode)}</span>
                        <Button size="small" loading={topoLoading} onClick={() => loadTopology(selected.id)}>
                          重新探测
                        </Button>
                      </div>
                      {selected.maint_status === 'maintaining' && (
                        <Alert type="warning" showIcon message="集群处于维护模式" style={{ marginBottom: 12 }} />
                      )}
                      {topo?.warnings?.map((w) => (
                        <Alert key={w} type="warning" showIcon message={w} style={{ marginBottom: 8 }} />
                      ))}
                      <Table
                        rowKey="instance_id"
                        size="small"
                        loading={topoLoading}
                        pagination={false}
                        dataSource={topo?.members || []}
                        columns={[
                          { title: '实例', dataIndex: 'instance_name', render: (v, r) => v || r.instance_id },
                          { title: '地址', render: (_, r) => `${r.host}:${r.port}` },
                          {
                            title: '探测角色',
                            dataIndex: 'role',
                            render: (v, r) => (
                              <Space>
                                <Tag color={v === 'primary' ? 'blue' : 'default'}>{v}</Tag>
                                {r.registered_role && r.registered_role !== v && (
                                  <Tag>登记:{r.registered_role}</Tag>
                                )}
                              </Space>
                            ),
                          },
                          {
                            title: '健康',
                            dataIndex: 'healthy',
                            width: 80,
                            render: (v) => (v ? <Tag color="green">OK</Tag> : <Tag color="red">异常</Tag>),
                          },
                          {
                            title: '延迟(s)',
                            dataIndex: 'lag_seconds',
                            width: 90,
                            render: (v) => (v == null ? '-' : Number(v).toFixed(1)),
                          },
                          { title: '详情', dataIndex: 'detail', ellipsis: true },
                          { title: '错误', dataIndex: 'error', ellipsis: true },
                        ]}
                      />
                      {topo?.probed_at && (
                        <div className="ha-muted">探测时间 {topo.probed_at}</div>
                      )}
                      {pgCtrlState && (
                        <Alert
                          style={{ marginTop: 12 }}
                          type={pgCtrlState.error ? 'warning' : 'info'}
                          showIcon
                          message={
                            pgCtrlState.kind === 'patroni'
                              ? 'Patroni /cluster'
                              : 'pg_autoctl show state'
                          }
                          description={
                            pgCtrlState.error ? (
                              pgCtrlState.error
                            ) : pgCtrlState.kind === 'patroni' ? (
                              <ul style={{ margin: 0, paddingLeft: 18 }}>
                                {(pgCtrlState.members || []).map((m: any) => (
                                  <li key={m.name}>
                                    {m.name} · {m.role} · {m.state} · {m.host}
                                  </li>
                                ))}
                              </ul>
                            ) : (
                              <pre style={{ margin: 0, maxHeight: 200, overflow: 'auto', fontSize: 12 }}>
                                {typeof pgCtrlState.raw === 'string'
                                  ? pgCtrlState.raw
                                  : JSON.stringify(pgCtrlState.state || pgCtrlState, null, 2)}
                              </pre>
                            )
                          }
                          action={
                            selected ? (
                              <Button size="small" onClick={() => loadPgCtrlState(selected)}>
                                刷新
                              </Button>
                            ) : null
                          }
                        />
                      )}
                      {selected?.mode === 'pg_autofailover' && (
                        <Space wrap style={{ marginTop: 12 }}>
                          <Button
                            size="small"
                            onClick={async () => {
                              try {
                                const { data } = await api.get(`/ha/clusters/${selected.id}/pgaf/service/status`)
                                message.info(
                                  data.data?.active
                                    ? `unit ${data.data?.unit || ''} 运行中`
                                    : `unit ${data.data?.unit || ''} 未运行（${data.data?.state || ''}）`,
                                )
                              } catch (e: any) {
                                message.error(e?.response?.data?.message || '查询失败')
                              }
                            }}
                          >
                            Keeper/Monitor 服务状态
                          </Button>
                          {(['start', 'stop', 'restart'] as const).map((act) => (
                            <Button
                              key={act}
                              size="small"
                              danger={act === 'stop'}
                              onClick={async () => {
                                try {
                                  await api.post(`/ha/clusters/${selected.id}/pgaf/service/${act}`, {})
                                  message.success(`已${act}`)
                                } catch (e: any) {
                                  message.error(e?.response?.data?.message || `${act} 失败`)
                                }
                              }}
                            >
                              服务 {act}
                            </Button>
                          ))}
                        </Space>
                      )}
                    </>
                  )}
                </div>
              </div>
            ),
          },
          {
            key: 'change',
            label: '变更',
            children: (
              <div className="ha-change">
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message="维护进出、计划内切换与强制切换均走变更单审批。MySQL·MHA/Orchestrator/ProxySQL、PostgreSQL·pg_autofailover/Patroni/repmgr 审批通过后可由控制器执行。自动 failover 仍由现场守护进程负责。"
                />
                <Button type="primary" onClick={openChange} disabled={!selected}>
                  对当前集群发起变更
                </Button>
                {!selected && <div className="ha-muted">请先在「观测」中选择集群</div>}
                {selected?.mode === 'mha' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 MHA 模式：请先安装版本并在「MHA 配置」下发 app.cnf；强制切换仅在主不可达时使用。"
                  />
                )}
                {selected?.mode === 'orchestrator' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 Orchestrator：计划切换 graceful-master-takeover；强制切换 force-master-failover（控制器备注可填 cluster_alias）。"
                  />
                )}
                {selected?.mode === 'proxysql_repl' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 ProxySQL+复制：计划切换将更新 writer hostgroup；请确认目标节点已可写。控制器备注可配 writer_hostgroup/admin_port。"
                  />
                )}
                {selected?.mode === 'pg_autofailover' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 pg_auto_failover：请绑定 Monitor 侧平台 Agent；可查看 Monitor 状态、systemd 启停并发起计划/强制切换。"
                  />
                )}
                {selected?.mode === 'patroni' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 Patroni：控制器 Base URL 填 REST 地址；计划切换走 /switchover，强制切换走 /failover。代装请用「PG·Patroni 版本」并配置 package_agent_url。"
                  />
                )}
                {selected?.mode === 'repmgr' && (
                  <Alert
                    type="success"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="当前为 repmgr：绑定平台 Agent 后计划切换将执行 repmgr standby switchover。"
                  />
                )}
              </div>
            ),
          },
          {
            key: 'mha',
            label: 'MHA 配置',
            children: (
              <div className="ha-mha-conf">
                {!selected || selected.mode !== 'mha' ? (
                  <Alert type="info" showIcon message="请选择 MySQL · MHA 模式的集群以管理 app.cnf（模板生成 / 编辑 / 下发到 Manager Agent）" />
                ) : (
                  <Space direction="vertical" style={{ width: '100%' }} size="middle">
                    <Alert
                      type="warning"
                      showIcon
                      message="密码等敏感项写入 conf 后会下发到 Manager 主机；请使用专用 mha 账号。下发后建议勾选验证 check_repl，并重启 manager。"
                    />
                    <Space wrap align="center">
                      <Tag color={mgrStatus?.active ? 'green' : 'default'}>
                        manager {mgrStatus?.active ? '运行中' : '未运行'}
                        {mgrStatus?.unit ? ` · ${mgrStatus.unit}` : ''}
                      </Tag>
                      <Button size="small" onClick={() => loadMgrStatus(selected.id)}>刷新状态</Button>
                      <Button
                        size="small"
                        loading={mhaBusy}
                        onClick={async () => {
                          setMhaBusy(true)
                          try {
                            await api.post(`/ha/clusters/${selected.id}/mha/manager/start`, {})
                            message.success('已启动')
                            await loadMgrStatus(selected.id)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '启动失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        启动
                      </Button>
                      <Button
                        size="small"
                        loading={mhaBusy}
                        onClick={async () => {
                          setMhaBusy(true)
                          try {
                            await api.post(`/ha/clusters/${selected.id}/mha/manager/stop`, {})
                            message.success('已停止')
                            await loadMgrStatus(selected.id)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '停止失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        停止
                      </Button>
                      <Button
                        size="small"
                        type="primary"
                        loading={mhaBusy}
                        onClick={async () => {
                          setMhaBusy(true)
                          try {
                            await api.post(`/ha/clusters/${selected.id}/mha/manager/restart`, {})
                            message.success('已重启')
                            await loadMgrStatus(selected.id)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '重启失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        重启 manager
                      </Button>
                    </Space>
                    <Space wrap>
                      <Select
                        style={{ minWidth: 220 }}
                        placeholder="选择 MHA 版本安装"
                        value={installReleaseId}
                        onChange={setInstallReleaseId}
                        options={mhaReleases.filter((r) => r.enabled !== false).map((r) => ({
                          value: r.id,
                          label: `${r.version} (${r.arch || 'x86_64'})`,
                        }))}
                      />
                      <Button
                        loading={mhaBusy}
                        disabled={!installReleaseId}
                        onClick={async () => {
                          setMhaBusy(true)
                          setInstallLog('')
                          try {
                            const { data } = await api.post(`/ha/clusters/${selected.id}/mha/packages/install`, {
                              release_id: installReleaseId,
                            }, { timeout: 300000 })
                            setInstallLog(data.data?.log || '')
                            message.success('安装完成')
                            await loadMgrStatus(selected.id)
                          } catch (e: any) {
                            setInstallLog(e?.response?.data?.data?.log || '')
                            message.error(e?.response?.data?.message || '安装失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        安装到 Manager + 成员
                      </Button>
                    </Space>
                    {installLog && (
                      <Input.TextArea rows={6} value={installLog} readOnly style={{ fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12 }} />
                    )}
                    <Space wrap>
                      <Button onClick={() => loadMhaConf(selected.id)}>重新加载</Button>
                      <Button
                        loading={mhaBusy}
                        onClick={async () => {
                          const v = await mhaOptsForm.validateFields()
                          setMhaBusy(true)
                          try {
                            const { content, conf_path, ...options } = v
                            const { data } = await api.post(`/ha/clusters/${selected.id}/mha/conf/preview`, options)
                            mhaOptsForm.setFieldsValue({
                              content: data.data?.content,
                              conf_path: data.data?.conf_path || conf_path,
                            })
                            message.success('已按选项重新生成模板')
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '生成失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        按选项生成模板
                      </Button>
                      <Button
                        type="primary"
                        loading={mhaBusy}
                        onClick={async () => {
                          const v = await mhaOptsForm.validateFields()
                          setMhaBusy(true)
                          try {
                            const { content, conf_path, ...options } = v
                            await api.put(`/ha/clusters/${selected.id}/mha/conf`, {
                              content,
                              conf_path,
                              options,
                            })
                            message.success('已保存')
                            await loadMhaConf(selected.id)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '保存失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        保存
                      </Button>
                      <Button
                        danger
                        loading={mhaBusy}
                        onClick={async () => {
                          const v = await mhaOptsForm.validateFields()
                          setMhaBusy(true)
                          try {
                            const { data } = await api.post(`/ha/clusters/${selected.id}/mha/conf/push`, {
                              content: v.content,
                              conf_path: v.conf_path,
                              verify_repl: !!v.verify_repl,
                            })
                            const d = data.data || {}
                            if (d.repl_ok === false) {
                              message.warning(`已下发，但 check_repl 未通过：${d.repl_detail || ''}`)
                            } else {
                              message.success(d.repl_ok ? `已下发并验证：${d.repl_detail || 'ok'}` : '已下发到 Agent')
                            }
                            Modal.confirm({
                              title: '是否重启 masterha_manager？',
                              content: 'conf 变更后需重启 manager 才生效。',
                              onOk: async () => {
                                await api.post(`/ha/clusters/${selected.id}/mha/manager/restart`, {})
                                message.success('已重启 manager')
                                await loadMgrStatus(selected.id)
                              },
                            })
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '下发失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        下发到 Agent
                      </Button>
                    </Space>
                    <Form form={mhaOptsForm} layout="vertical" initialValues={{ ssh_port: 22, ping_interval: 3, verify_repl: true }}>
                      <Space wrap style={{ width: '100%' }}>
                        <Form.Item name="conf_path" label="conf 路径" style={{ minWidth: 280 }}>
                          <Input placeholder="/etc/mha/app.cnf" />
                        </Form.Item>
                        <Form.Item name="user" label="MySQL user" style={{ width: 140 }}>
                          <Input />
                        </Form.Item>
                        <Form.Item name="password" label="MySQL password" style={{ width: 160 }}>
                          <Input.Password placeholder="空则模板写 CHANGE_ME" />
                        </Form.Item>
                        <Form.Item name="ssh_user" label="ssh_user" style={{ width: 120 }}>
                          <Input />
                        </Form.Item>
                        <Form.Item name="ssh_port" label="ssh_port" style={{ width: 100 }}>
                          <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                        </Form.Item>
                        <Form.Item name="ping_interval" label="ping_interval" style={{ width: 120 }}>
                          <InputNumber min={1} style={{ width: '100%' }} />
                        </Form.Item>
                        <Form.Item name="manager_workdir" label="manager_workdir" style={{ minWidth: 220 }}>
                          <Input />
                        </Form.Item>
                        <Form.Item name="master_binlog_dir" label="master_binlog_dir" style={{ minWidth: 200 }}>
                          <Input />
                        </Form.Item>
                        <Form.Item name="master_ip_failover_script" label="failover 脚本" style={{ minWidth: 260 }}>
                          <Input placeholder="可选 VIP/DNS 脚本" />
                        </Form.Item>
                        <Form.Item name="master_ip_online_change_script" label="online change 脚本" style={{ minWidth: 260 }}>
                          <Input placeholder="可选在线切 VIP" />
                        </Form.Item>
                        <Form.Item name="verify_repl" label="下发后验证" valuePropName="checked">
                          <Switch checkedChildren="check_repl" unCheckedChildren="跳过" />
                        </Form.Item>
                      </Space>
                      <Form.Item name="content" label="app.cnf 正文（可改）" rules={[{ required: true }]}>
                        <Input.TextArea rows={18} style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }} />
                      </Form.Item>
                    </Form>
                    {mhaConf?.gen_error && <Alert type="error" showIcon message={mhaConf.gen_error} />}
                  </Space>
                )}
              </div>
            ),
          },
          {
            key: 'mha-versions',
            label: 'MHA 版本',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Alert
                  type="info"
                  showIcon
                  message="登记 manager/node 分包（rpm/deb），绑定配置中心 OSS 后可经 Manager Agent 代装。与 db-deploy 的 tar 装包路径独立。"
                />
                <Form form={ossForm} layout="inline">
                  <Form.Item name="oss_setting_id" label="OSS" rules={[{ required: true }]}>
                    <Select
                      style={{ minWidth: 260 }}
                      options={ossSettings.map((s) => ({
                        value: s.id,
                        label: `${s.environment || s.name || s.id} · ${s.bucket || ''}`,
                      }))}
                      placeholder="选择配置中心 OSS"
                    />
                  </Form.Item>
                  <Form.Item name="prefix" label="prefix">
                    <Input style={{ width: 180 }} placeholder="mha-artifacts/" />
                  </Form.Item>
                  <Button
                    type="primary"
                    onClick={async () => {
                      const v = await ossForm.validateFields()
                      try {
                        await api.put('/ha/mha/oss', v)
                        message.success('已保存 OSS 绑定')
                        loadMhaOss()
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || '保存失败')
                      }
                    }}
                  >
                    保存 OSS
                  </Button>
                  {mhaOss?.oss_bucket && (
                    <span className="ha-muted">当前 bucket: {mhaOss.oss_bucket}</span>
                  )}
                </Form>
                <Space>
                  <Button
                    type="primary"
                    onClick={() => {
                      relForm.resetFields()
                      relForm.setFieldsValue({ channel: 'stable', arch: 'x86_64', enabled: true })
                      setRelOpen(true)
                    }}
                  >
                    登记版本
                  </Button>
                  <Button onClick={loadMhaReleases}>刷新</Button>
                </Space>
                <ResponsiveTable
                  rowKey="id"
                  dataSource={mhaReleases}
                  pagination={false}
                  expandable={{
                    expandedRowRender: (r: any) => (
                      <Table
                        size="small"
                        pagination={false}
                        rowKey="id"
                        dataSource={r.artifacts || []}
                        columns={[
                          { title: '角色', dataIndex: 'role', width: 100 },
                          { title: '格式', dataIndex: 'pack_format', width: 80 },
                          { title: 'object_key', dataIndex: 'object_key', ellipsis: true },
                          { title: 'sha256', dataIndex: 'sha256', width: 120, ellipsis: true },
                        ]}
                      />
                    ),
                  }}
                  columns={[
                    { title: '版本', dataIndex: 'version', width: 120 },
                    { title: '渠道', dataIndex: 'channel', width: 90 },
                    { title: '架构', dataIndex: 'arch', width: 90 },
                    { title: 'OS', dataIndex: 'os', width: 100 },
                    {
                      title: '制品',
                      width: 100,
                      render: (_, r) => `${(r.artifacts || []).length} 个`,
                    },
                    {
                      title: '启用',
                      dataIndex: 'enabled',
                      width: 80,
                      render: (v) => (v === false ? '否' : '是'),
                    },
                    {
                      title: '操作',
                      width: 260,
                      render: (_, r) => (
                        <Space wrap>
                          <Button
                            size="small"
                            onClick={() => {
                              setArtReleaseId(r.id)
                              artForm.resetFields()
                              artForm.setFieldsValue({ role: 'manager', pack_format: 'rpm' })
                              setArtOpen(true)
                            }}
                          >
                            添加制品
                          </Button>
                          <Button
                            size="small"
                            onClick={async () => {
                              try {
                                const { data } = await api.post(`/ha/mha/releases/${r.id}/presign?role=manager`)
                                Modal.info({
                                  title: 'presign manager',
                                  width: 640,
                                  content: (
                                    <div style={{ wordBreak: 'break-all' }}>
                                      <div>key: {data.data?.object_key}</div>
                                      <div>sha256: {data.data?.sha256}</div>
                                      <div>url: {data.data?.url}</div>
                                    </div>
                                  ),
                                })
                              } catch (e: any) {
                                message.error(e?.response?.data?.message || 'presign 失败')
                              }
                            }}
                          >
                            试签发
                          </Button>
                          <Button
                            size="small"
                            danger
                            onClick={async () => {
                              await api.delete(`/ha/mha/releases/${r.id}`)
                              loadMhaReleases()
                            }}
                          >
                            删除
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </Space>
            ),
          },
          {
            key: 'pgaf-versions',
            label: 'PG·pgaf 版本',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Alert
                  type="info"
                  showIcon
                  message="登记 pg_auto_failover 的 monitor/keeper 分包（rpm/deb），绑定 OSS 后可经 Monitor Agent 代装。与 MHA 制品目录独立。"
                />
                <Form form={pgafOssForm} layout="inline">
                  <Form.Item name="oss_setting_id" label="OSS" rules={[{ required: true }]}>
                    <Select
                      style={{ minWidth: 260 }}
                      options={ossSettings.map((s) => ({
                        value: s.id,
                        label: `${s.environment || s.name || s.id} · ${s.bucket || ''}`,
                      }))}
                    />
                  </Form.Item>
                  <Form.Item name="prefix" label="prefix">
                    <Input style={{ width: 180 }} placeholder="pgaf-artifacts/" />
                  </Form.Item>
                  <Button
                    type="primary"
                    onClick={async () => {
                      const v = await pgafOssForm.validateFields()
                      try {
                        await api.put('/ha/pgaf/oss', v)
                        message.success('已保存')
                        loadPgafOss()
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || '保存失败')
                      }
                    }}
                  >
                    保存 OSS
                  </Button>
                  {pgafOss?.oss_bucket && <span className="ha-muted">bucket: {pgafOss.oss_bucket}</span>}
                </Form>
                <Space>
                  <Button
                    type="primary"
                    onClick={() => {
                      pgafRelForm.resetFields()
                      pgafRelForm.setFieldsValue({ channel: 'stable', arch: 'x86_64' })
                      setPgafRelOpen(true)
                    }}
                  >
                    登记版本
                  </Button>
                  <Button onClick={loadPgafReleases}>刷新</Button>
                  {selected?.mode === 'pg_autofailover' && (
                    <>
                      <Select
                        style={{ minWidth: 200 }}
                        placeholder="选版本安装到 Monitor+成员"
                        value={pgafInstallId}
                        onChange={setPgafInstallId}
                        options={pgafReleases.filter((r) => r.enabled !== false).map((r) => ({
                          value: r.id,
                          label: r.version,
                        }))}
                      />
                      <Button
                        loading={mhaBusy}
                        disabled={!pgafInstallId}
                        onClick={async () => {
                          if (!selected) return
                          setMhaBusy(true)
                          setPgafInstallLog('')
                          try {
                            const { data } = await api.post(
                              `/ha/clusters/${selected.id}/pgaf/packages/install`,
                              { release_id: pgafInstallId },
                              { timeout: 300000 },
                            )
                            setPgafInstallLog(data.data?.log || '')
                            message.success('安装完成')
                          } catch (e: any) {
                            setPgafInstallLog(e?.response?.data?.data?.log || '')
                            message.error(e?.response?.data?.message || '安装失败')
                          } finally {
                            setMhaBusy(false)
                          }
                        }}
                      >
                        安装
                      </Button>
                    </>
                  )}
                </Space>
                {pgafInstallLog && (
                  <Input.TextArea rows={5} value={pgafInstallLog} readOnly style={{ fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12 }} />
                )}
                <ResponsiveTable
                  rowKey="id"
                  dataSource={pgafReleases}
                  pagination={false}
                  expandable={{
                    expandedRowRender: (r: any) => (
                      <Table
                        size="small"
                        pagination={false}
                        rowKey="id"
                        dataSource={r.artifacts || []}
                        columns={[
                          { title: '角色', dataIndex: 'role', width: 100 },
                          { title: '格式', dataIndex: 'pack_format', width: 80 },
                          { title: 'object_key', dataIndex: 'object_key', ellipsis: true },
                          { title: 'sha256', dataIndex: 'sha256', width: 120, ellipsis: true },
                        ]}
                      />
                    ),
                  }}
                  columns={[
                    { title: '版本', dataIndex: 'version', width: 120 },
                    { title: '渠道', dataIndex: 'channel', width: 90 },
                    { title: '架构', dataIndex: 'arch', width: 90 },
                    {
                      title: '操作',
                      width: 220,
                      render: (_, r) => (
                        <Space>
                          <Button
                            size="small"
                            onClick={() => {
                              setPgafArtReleaseId(r.id)
                              pgafArtForm.resetFields()
                              pgafArtForm.setFieldsValue({ role: 'monitor', pack_format: 'rpm' })
                              setPgafArtOpen(true)
                            }}
                          >
                            添加制品
                          </Button>
                          <Button
                            size="small"
                            danger
                            onClick={async () => {
                              await api.delete(`/ha/pgaf/releases/${r.id}`)
                              loadPgafReleases()
                            }}
                          >
                            删除
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </Space>
            ),
          },
          {
            key: 'patroni-versions',
            label: 'PG·Patroni 版本',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Alert
                  type="info"
                  showIcon
                  message="登记 Patroni rpm/deb 制品并经平台 Agent 代装。控制器备注可填 package_agent_url（REST:8008 与 Agent 端口不同时必填）。不代装 etcd。"
                />
                <Form form={patroniOssForm} layout="inline">
                  <Form.Item name="oss_setting_id" label="OSS" rules={[{ required: true }]}>
                    <Select
                      style={{ minWidth: 260 }}
                      options={ossSettings.map((s) => ({
                        value: s.id,
                        label: `${s.environment || s.name || s.id} · ${s.bucket || ''}`,
                      }))}
                    />
                  </Form.Item>
                  <Form.Item name="prefix" label="前缀">
                    <Input style={{ width: 180 }} placeholder="patroni-artifacts/" />
                  </Form.Item>
                  <Button
                    type="primary"
                    onClick={async () => {
                      const v = await patroniOssForm.validateFields()
                      try {
                        await api.put('/ha/patroni/oss', v)
                        message.success('已保存')
                        loadPatroniOss()
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || '保存失败')
                      }
                    }}
                  >
                    保存 OSS
                  </Button>
                  {patroniOss?.oss_bucket && <span className="ha-muted">bucket: {patroniOss.oss_bucket}</span>}
                </Form>
                <Space wrap>
                  <Button
                    type="primary"
                    onClick={() => {
                      patroniRelForm.resetFields()
                      patroniRelForm.setFieldsValue({ channel: 'stable', arch: 'x86_64' })
                      setPatroniRelOpen(true)
                    }}
                  >
                    登记版本
                  </Button>
                  <Select
                    style={{ minWidth: 220 }}
                    placeholder="选择版本代装"
                    value={patroniInstallId}
                    onChange={setPatroniInstallId}
                    options={patroniReleases.filter((r) => r.enabled !== false).map((r) => ({
                      value: r.id,
                      label: r.version,
                    }))}
                  />
                  <Button
                    disabled={!patroniInstallId || selected?.mode !== 'patroni'}
                    onClick={async () => {
                      if (!selected || !patroniInstallId) return
                      try {
                        const { data } = await api.post(
                          `/ha/clusters/${selected.id}/patroni/packages/install`,
                          { release_id: patroniInstallId },
                        )
                        setPatroniInstallLog(data.data?.log || JSON.stringify(data.data))
                        message.success('代装完成')
                      } catch (e: any) {
                        setPatroniInstallLog(e?.response?.data?.data?.log || e?.response?.data?.message || '')
                        message.error(e?.response?.data?.message || '代装失败')
                      }
                    }}
                  >
                    对当前 Patroni 集群代装
                  </Button>
                </Space>
                {patroniInstallLog && (
                  <Input.TextArea rows={5} value={patroniInstallLog} readOnly style={{ fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12 }} />
                )}
                <ResponsiveTable
                  rowKey="id"
                  dataSource={patroniReleases}
                  pagination={false}
                  columns={[
                    { title: '版本', dataIndex: 'version' },
                    { title: '渠道', dataIndex: 'channel', width: 100 },
                    { title: '架构', dataIndex: 'arch', width: 100 },
                    {
                      title: '制品',
                      render: (_: any, r: any) => (r.artifacts || []).map((a: any) => a.role).join(', ') || '-',
                    },
                    {
                      title: '操作',
                      width: 200,
                      render: (_: any, r: any) => (
                        <Space>
                          <Button
                            size="small"
                            onClick={() => {
                              setPatroniArtReleaseId(r.id)
                              patroniArtForm.resetFields()
                              patroniArtForm.setFieldsValue({ role: 'patroni', pack_format: 'rpm' })
                              setPatroniArtOpen(true)
                            }}
                          >
                            添加制品
                          </Button>
                          <Button
                            size="small"
                            danger
                            onClick={async () => {
                              await api.delete(`/ha/patroni/releases/${r.id}`)
                              loadPatroniReleases()
                            }}
                          >
                            删除
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </Space>
            ),
          },
          {
            key: 'govern',
            label: '治理',
            children: (
              <ResponsiveTable
                rowKey="id"
                dataSource={tasks}
                pagination={{ pageSize: 20 }}
                columns={[
                  { title: '集群', dataIndex: 'cluster_name', minWidth: 140 },
                  { title: '引擎', dataIndex: 'engine', width: 90 },
                  {
                    title: '操作',
                    dataIndex: 'action',
                    width: 140,
                    render: (v) => actionLabel(v),
                  },
                  { title: '状态', dataIndex: 'status', width: 100 },
                  { title: '操作人', dataIndex: 'operator', width: 120 },
                  { title: '时间', dataIndex: 'created_at', width: 180 },
                  {
                    title: '工单',
                    dataIndex: 'issue_id',
                    width: 100,
                    render: (id) => (id ? <Link to={`/ops/tickets/${id}`}>查看</Link> : '-'),
                  },
                ]}
              />
            ),
          },
          {
            key: 'controller',
            label: '控制器',
            children: (
              <div>
                <Button type="primary" style={{ marginBottom: 12 }} onClick={() => {
                  ctrlForm.resetFields()
                  ctrlForm.setFieldsValue({ engine: 'mysql', kind: 'orchestrator' })
                  setCtrlSource('manual')
                  setCtrlOpen(true)
                  loadPlatAgents()
                }}>
                  登记控制器
                </Button>
                <ResponsiveTable
                  rowKey="id"
                  dataSource={controllers}
                  pagination={false}
                  columns={[
                    { title: '名称', dataIndex: 'name', minWidth: 140 },
                    { title: '引擎', dataIndex: 'engine', width: 90 },
                    { title: '类型', dataIndex: 'kind', width: 120 },
                    { title: 'Base URL', dataIndex: 'base_url', minWidth: 200 },
                    {
                      title: '平台 Agent',
                      width: 140,
                      render: (_, r) => (r.agent_id ? agentName(r.agent_id) : '-'),
                    },
                    {
                      title: '操作',
                      width: 160,
                      render: (_, r) => (
                        <Space>
                          {(r.kind === 'mha' ||
                            r.kind === 'orchestrator' ||
                            r.kind === 'proxysql' ||
                            r.kind === 'pg_autofailover' ||
                            r.kind === 'patroni' ||
                            r.kind === 'repmgr') && (
                            <Button
                              type="link"
                              size="small"
                              onClick={async () => {
                                try {
                                  const { data } = await api.post(`/ha/controllers/${r.id}/env-check`)
                                  const d = data.data || {}
                                  Modal.info({
                                    title: d.ok ? '环境检查通过' : '环境检查未通过',
                                    width: 560,
                                    content: (
                                      <div>
                                        <div style={{ marginBottom: 8 }}>{d.summary}</div>
                                        <ul style={{ margin: 0, paddingLeft: 18 }}>
                                          {(d.checks || []).map((c: any) => (
                                            <li key={c.name}>{c.ok ? '✓' : '✗'} {c.name}: {c.detail}</li>
                                          ))}
                                        </ul>
                                      </div>
                                    ),
                                  })
                                } catch (e: any) {
                                  message.error(e?.response?.data?.message || '检查失败')
                                }
                              }}
                            >
                              环境检查
                            </Button>
                          )}
                          <Button
                            type="link"
                            danger
                            size="small"
                            onClick={async () => {
                              await api.delete(`/ha/controllers/${r.id}`)
                              loadControllers()
                            }}
                          >
                            删除
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </div>
            ),
          },
        ]}
      />

      <Modal
        title="登记 HA 集群"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={submitCreate}
        width={720}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="如 order-mysql-ha" />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="engine" label="引擎" rules={[{ required: true }]} style={{ width: 180 }}>
              <Select
                options={ENGINE_OPTS}
                onChange={(v) => {
                  loadInstances(v)
                  form.setFieldsValue({
                    mode: modeDefaults[v] || 'manual',
                    members: [{ role: 'primary' }],
                    controller_id: undefined,
                  })
                }}
              />
            </Form.Item>
            <Form.Item name="env_id" label="环境" style={{ width: 180 }}>
              <Select
                allowClear
                options={envs.map((e) => ({ value: e.id, label: e.name || e.code }))}
              />
            </Form.Item>
            <Form.Item name="mode" label="模式" rules={[{ required: true }]} style={{ width: 240 }}>
              <Select options={createModeOpts} optionLabelProp="label" />
            </Form.Item>
          </Space>
          {createModeHint && <div className="ha-muted" style={{ marginTop: -8, marginBottom: 8 }}>{createModeHint}</div>}
          <Form.Item
            noStyle
            shouldUpdate={(p, c) => p.mode !== c.mode || p.engine !== c.engine}
          >
            {() => {
              const mode = form.getFieldValue('mode')
              const needCtrl = ['orchestrator', 'mha', 'proxysql_repl', 'pg_autofailover', 'patroni', 'repmgr', 'sentinel'].includes(mode)
              return (
                <Form.Item
                  name="controller_id"
                  label={needCtrl ? '控制器（必填）' : '控制器（可选）'}
                  rules={needCtrl ? [{ required: true, message: '该模式必须绑定控制器' }] : undefined}
                  extra={
                    needCtrl ? (
                      <span className="ha-muted">
                        {mode === 'mha'
                          ? 'MHA 需绑定 kind=mha 的 Manager Agent；平台可启停 systemd 托管的 masterha_manager'
                          : mode === 'pg_autofailover'
                            ? 'pg_auto_failover 需绑定 Monitor 侧平台 Agent（kind=pg_autofailover）'
                            : mode === 'patroni'
                              ? 'Patroni 填 REST Base URL（如 http://patroni:8008）'
                              : '自动切换模式须绑定兼容控制器'}
                      </span>
                    ) : undefined
                  }
                >
                  <Select
                    allowClear={!needCtrl}
                    placeholder={
                      mode === 'mha'
                        ? '选择 MHA Manager Agent'
                        : mode === 'pg_autofailover'
                          ? '选择 pg_auto_failover Monitor Agent'
                          : mode === 'patroni'
                            ? '选择 Patroni REST 控制器'
                            : mode === 'repmgr'
                              ? '选择 repmgr Agent'
                              : mode === 'proxysql_repl'
                                ? '选择 ProxySQL 控制器'
                                : '选择兼容的 HA 控制器'
                    }
                    options={controllers
                      .filter((c) => c.engine === createEngine)
                      .filter((c) => {
                        if (mode === 'orchestrator') return c.kind === 'orchestrator'
                        if (mode === 'mha') return c.kind === 'mha'
                        if (mode === 'proxysql_repl') return c.kind === 'proxysql'
                        if (mode === 'pg_autofailover') return c.kind === 'pg_autofailover'
                        if (mode === 'patroni') return c.kind === 'patroni'
                        if (mode === 'repmgr') return c.kind === 'repmgr'
                        if (mode === 'sentinel') return c.kind === 'sentinel'
                        return true
                      })
                      .map((c) => ({ value: c.id, label: `${c.name} (${c.kind})` }))}
                  />
                </Form.Item>
              )
            }}
          </Form.Item>
          <Form.Item name="vip" label="VIP">
            <Input placeholder="可选" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="dbdeploy_cluster_id" hidden><Input /></Form.Item>
          {createEngine === 'mysql' && createMode === 'mha' && (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 12 }}
              message="MHA 须 1 主 ≥1 从。可从「集群部署」已就绪的 MySQL 主从导入成员（自动登记实例）。"
              action={
                <Select
                  allowClear
                  placeholder="从集群部署导入"
                  style={{ width: 220 }}
                  options={dbDeployClusters.map((c) => ({ value: c.id, label: c.name }))}
                  onChange={(id) => { if (id) void importDbDeploy(id) }}
                />
              }
            />
          )}
          <Form.List name="members">
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                    <Form.Item
                      {...field}
                      name={[field.name, 'instance_id']}
                      rules={[{ required: true, message: '选实例' }]}
                    >
                      <Select
                        showSearch
                        optionFilterProp="label"
                        style={{ width: 320 }}
                        placeholder="实例（已登记 / 部署导入）"
                        options={candidateOpts.length ? candidateOpts : instances.map((i: any) => ({
                          value: i.id || i.instance_id,
                          label: i.label || `${i.name} (${i.host || i.domain_name}:${i.port})`,
                        }))}
                      />
                    </Form.Item>
                    <Form.Item {...field} name={[field.name, 'role']} initialValue="replica">
                      <Select style={{ width: 140 }} options={roleOpts} />
                    </Form.Item>
                    <Button type="link" danger onClick={() => remove(field.name)}>
                      删除
                    </Button>
                  </Space>
                ))}
                <Button type="dashed" onClick={() => add({ role: 'replica' })} block>
                  添加成员
                </Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>

      <Modal
        title="发起 HA 变更"
        open={changeOpen}
        onCancel={() => setChangeOpen(false)}
        onOk={submitChange}
        width={640}
        destroyOnClose
        okText="提交变更单"
      >
        <Form form={changeForm} layout="vertical">
          <Form.Item name="cluster_id" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="action" label="操作" rules={[{ required: true }]}>
            <Select
              options={
                selected?.mode === 'mha' ||
                selected?.mode === 'orchestrator' ||
                selected?.mode === 'pg_autofailover' ||
                selected?.mode === 'patroni'
                  ? ACTION_OPTS
                  : ACTION_OPTS.filter((a) => a.value !== 'failover_dead')
              }
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, c) => p.action !== c.action}>
            {() => {
              const act = changeForm.getFieldValue('action')
              if (act === 'switchover') {
                return (
                  <Form.Item
                    name="target_instance_id"
                    label="新主实例"
                    rules={[{ required: true, message: '请选择目标从库' }]}
                  >
                    <Select options={replicaOptions} placeholder="选择提升目标" />
                  </Form.Item>
                )
              }
              if (act === 'failover_dead') {
                return (
                  <>
                    <Alert
                      type="error"
                      showIcon
                      style={{ marginBottom: 12 }}
                      message="主宕机强制切换可能丢未落盘事务。须确认主已不可达，并完成预检后再提交审批。"
                    />
                    <Form.Item name="target_instance_id" label="新主实例（可选）">
                      <Select allowClear options={replicaOptions} placeholder="留空则由控制器自动选择" />
                    </Form.Item>
                  </>
                )
              }
              return null
            }}
          </Form.Item>
          <Form.Item name="assigned_to" label="审批人">
            <Input placeholder="默认取全局同步审批人" />
          </Form.Item>
          <Form.Item name="description" label="事由">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Button onClick={runPrecheck} style={{ marginBottom: 12 }}>
            运行预检
          </Button>
          {precheck && (
            <Alert
              type={precheck.ok ? 'success' : 'error'}
              showIcon
              message={precheck.summary}
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {(precheck.checks || []).map((c: any) => (
                    <li key={c.name}>
                      {c.ok ? '✓' : '✗'} {c.name}: {c.message}
                    </li>
                  ))}
                </ul>
              }
            />
          )}
        </Form>
      </Modal>

      <Modal
        title="登记 HA 控制器"
        open={ctrlOpen}
        onCancel={() => setCtrlOpen(false)}
        onOk={submitController}
        destroyOnClose
      >
        <Form form={ctrlForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="engine" label="引擎" rules={[{ required: true }]}>
            <Select
              options={ENGINE_OPTS}
              onChange={(v) => {
                const first = kindMeta.find((k) => k.engine === v)
                ctrlForm.setFieldValue('kind', first?.code)
                if (first?.code !== 'mha') setCtrlSource('manual')
              }}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, c) => p.engine !== c.engine}>
            {() => (
              <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
                <Select
                  options={kindMeta
                    .filter((k) => k.engine === (ctrlForm.getFieldValue('engine') || 'mysql'))
                    .map((k) => ({ value: k.code, label: k.label }))}
                  onChange={(v) => {
                    if (v !== 'mha' && v !== 'pg_autofailover') setCtrlSource('manual')
                  }}
                />
              </Form.Item>
            )}
          </Form.Item>
          {ctrlKind === 'mha' || ctrlKind === 'pg_autofailover' ? (
            <Form.Item label="接入方式">
              <Select
                value={ctrlSource}
                onChange={(v) => {
                  setCtrlSource(v)
                  if (v === 'agent') {
                    ctrlForm.setFieldsValue({ base_url: undefined, username: undefined, password: undefined })
                  } else {
                    ctrlForm.setFieldsValue({ agent_id: undefined })
                  }
                }}
                options={[
                  { value: 'manual', label: '手动 Base URL' },
                  { value: 'agent', label: '选用平台 Agent' },
                ]}
              />
            </Form.Item>
          ) : null}
          {(ctrlKind === 'mha' || ctrlKind === 'pg_autofailover') && ctrlSource === 'agent' ? (
            <Form.Item
              name="agent_id"
              label="平台 Agent"
              rules={[{ required: true, message: '请选择归属含「高可用」的平台 Agent' }]}
              extra={
                <>
                  请在 <Link to="/config/agents">配置中心 → 平台 Agent</Link> 勾选归属「高可用」后下发/登记。
                  {platAgents.length === 0 ? ' 当前无可选 Agent。' : ''}
                </>
              }
            >
              <Select
                showSearch
                optionFilterProp="label"
                placeholder="选择平台 Agent"
                options={platAgents.map((a) => ({
                  value: a.id,
                  label: `${a.name} — ${a.base_url || '-'}`,
                }))}
                onChange={(id) => {
                  const a = platAgents.find((x) => x.id === id)
                  if (a) ctrlForm.setFieldValue('base_url', a.base_url)
                }}
              />
            </Form.Item>
          ) : (
            <Form.Item
              noStyle
              shouldUpdate={(p, c) => p.kind !== c.kind || p.engine !== c.engine}
            >
              {() => {
                const kind = ctrlForm.getFieldValue('kind')
                const ph =
                  kind === 'mha'
                    ? 'http://mha-manager-agent:8088'
                    : kind === 'pg_autofailover'
                      ? 'http://pg-monitor-agent:8090'
                      : kind === 'patroni'
                        ? 'http://patroni-host:8008'
                        : kind === 'sentinel'
                          ? 'sentinel-host:26379 或 redis://host:26379'
                          : 'http://orchestrator:3000'
                return (
                  <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]}>
                    <Input placeholder={ph} />
                  </Form.Item>
                )
              }}
            </Form.Item>
          )}
          {!(
            (ctrlKind === 'mha' || ctrlKind === 'pg_autofailover') &&
            ctrlSource === 'agent'
          ) ? (
            <>
              <Form.Item name="username" label="用户名">
                <Input />
              </Form.Item>
              <Form.Item name="password" label="密码">
                <Input.Password />
              </Form.Item>
            </>
          ) : null}
          {ctrlAgentId && ctrlSource === 'agent' ? (
            <Form.Item label="Base URL（自动带出）">
              <Input disabled value={platAgents.find((a) => a.id === ctrlAgentId)?.base_url || ''} />
            </Form.Item>
          ) : null}
          <Form.Item
            noStyle
            shouldUpdate={(p, c) => p.kind !== c.kind}
          >
            {() => {
              const kind = ctrlForm.getFieldValue('kind')
              if (kind === 'mha') {
                return (
                  <Form.Item
                    name="remark"
                    label="app.cnf 路径"
                    extra="写入控制器备注；支持纯路径，或 JSON 字段 app_conf（如 /etc/mha/app1.cnf），默认 /etc/mha/app.cnf"
                  >
                    <Input placeholder="/etc/mha/app.cnf" />
                  </Form.Item>
                )
              }
              if (kind === 'pg_autofailover') {
                return (
                  <Form.Item
                    name="remark"
                    label="formation / monitor"
                    extra='JSON：{"formation":"default","group":"0","monitor":"postgres://autoctl_node@monitor:5432/pg_auto_failover"} 或纯 monitor URI / pgdata 路径'
                  >
                    <Input.TextArea
                      rows={3}
                      placeholder='{"formation":"default","group":"0","monitor":"postgres://..."}'
                    />
                  </Form.Item>
                )
              }
              return (
                <Form.Item name="remark" label="备注">
                  <Input />
                </Form.Item>
              )
            }}
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="登记 MHA 版本"
        open={relOpen}
        onCancel={() => setRelOpen(false)}
        onOk={async () => {
          const v = await relForm.validateFields()
          try {
            await api.post('/ha/mha/releases', v)
            message.success('已登记')
            setRelOpen(false)
            loadMhaReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '登记失败')
          }
        }}
        destroyOnClose
      >
        <Form form={relForm} layout="vertical">
          <Form.Item name="version" label="版本号" rules={[{ required: true }]}>
            <Input placeholder="0.58" />
          </Form.Item>
          <Form.Item name="channel" label="渠道">
            <Input placeholder="stable" />
          </Form.Item>
          <Form.Item name="arch" label="架构">
            <Input placeholder="x86_64" />
          </Form.Item>
          <Form.Item name="os" label="OS">
            <Input placeholder="el7 / ubuntu20.04" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加制品"
        open={artOpen}
        onCancel={() => setArtOpen(false)}
        onOk={async () => {
          const v = await artForm.validateFields()
          try {
            await api.post(`/ha/mha/releases/${artReleaseId}/artifacts`, v)
            message.success('已添加')
            setArtOpen(false)
            loadMhaReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '添加失败')
          }
        }}
        destroyOnClose
      >
        <Form form={artForm} layout="vertical">
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={[{ value: 'manager' }, { value: 'node' }]} />
          </Form.Item>
          <Form.Item name="pack_format" label="包格式" rules={[{ required: true }]}>
            <Select options={[{ value: 'rpm' }, { value: 'deb' }]} />
          </Form.Item>
          <Form.Item name="object_key" label="object_key" rules={[{ required: true }]}>
            <Input placeholder="mha-artifacts/0.58/mha4mysql-manager-0.58-0.el7.noarch.rpm" />
          </Form.Item>
          <Form.Item name="sha256" label="sha256" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="filename" label="文件名">
            <Input />
          </Form.Item>
          <Form.Item name="size_bytes" label="大小(字节)">
            <InputNumber style={{ width: '100%' }} min={0} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title="登记 pg_auto_failover 版本"
        open={pgafRelOpen}
        onCancel={() => setPgafRelOpen(false)}
        onOk={async () => {
          const v = await pgafRelForm.validateFields()
          try {
            await api.post('/ha/pgaf/releases', v)
            message.success('已登记')
            setPgafRelOpen(false)
            loadPgafReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '登记失败')
          }
        }}
        destroyOnClose
      >
        <Form form={pgafRelForm} layout="vertical">
          <Form.Item name="version" label="版本号" rules={[{ required: true }]}>
            <Input placeholder="2.1" />
          </Form.Item>
          <Form.Item name="channel" label="渠道"><Input /></Form.Item>
          <Form.Item name="arch" label="架构"><Input /></Form.Item>
          <Form.Item name="os" label="OS"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加 pgaf 制品"
        open={pgafArtOpen}
        onCancel={() => setPgafArtOpen(false)}
        onOk={async () => {
          const v = await pgafArtForm.validateFields()
          try {
            await api.post(`/ha/pgaf/releases/${pgafArtReleaseId}/artifacts`, v)
            message.success('已添加')
            setPgafArtOpen(false)
            loadPgafReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '添加失败')
          }
        }}
        destroyOnClose
      >
        <Form form={pgafArtForm} layout="vertical">
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={[{ value: 'monitor' }, { value: 'keeper' }]} />
          </Form.Item>
          <Form.Item name="pack_format" label="包格式" rules={[{ required: true }]}>
            <Select options={[{ value: 'rpm' }, { value: 'deb' }]} />
          </Form.Item>
          <Form.Item name="object_key" label="object_key" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="sha256" label="sha256" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="登记 Patroni 版本"
        open={patroniRelOpen}
        onCancel={() => setPatroniRelOpen(false)}
        onOk={async () => {
          const v = await patroniRelForm.validateFields()
          try {
            await api.post('/ha/patroni/releases', v)
            message.success('已登记')
            setPatroniRelOpen(false)
            loadPatroniReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '登记失败')
          }
        }}
        destroyOnClose
      >
        <Form form={patroniRelForm} layout="vertical">
          <Form.Item name="version" label="版本号" rules={[{ required: true }]}>
            <Input placeholder="3.2.2" />
          </Form.Item>
          <Form.Item name="channel" label="渠道"><Input /></Form.Item>
          <Form.Item name="arch" label="架构"><Input /></Form.Item>
          <Form.Item name="os" label="OS"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title="添加 Patroni 制品"
        open={patroniArtOpen}
        onCancel={() => setPatroniArtOpen(false)}
        onOk={async () => {
          const v = await patroniArtForm.validateFields()
          try {
            await api.post(`/ha/patroni/releases/${patroniArtReleaseId}/artifacts`, v)
            message.success('已添加')
            setPatroniArtOpen(false)
            loadPatroniReleases()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '添加失败')
          }
        }}
        destroyOnClose
      >
        <Form form={patroniArtForm} layout="vertical">
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={[{ value: 'patroni' }]} />
          </Form.Item>
          <Form.Item name="pack_format" label="包格式" rules={[{ required: true }]}>
            <Select options={[{ value: 'rpm' }, { value: 'deb' }]} />
          </Form.Item>
          <Form.Item name="object_key" label="object_key" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="sha256" label="sha256" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
