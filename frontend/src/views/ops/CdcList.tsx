import React, { useEffect, useMemo, useState } from 'react'
import {
  useNavigate,
  useSearchParams } from 'react-router-dom'
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Form,
  Input,
  Popover,
  Select,
  Space,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  message,
  Modal,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  DeleteOutlined,
  EyeOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  RedoOutlined,
  ReloadOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import { usePermission } from '../../hooks/usePermission'
import { navigateToWorkOrderCreate } from '../../utils/workOrderDraft'
import CdcReinitModal from './CdcReinitModal'
import CdcReinitBatchModal from './CdcReinitBatchModal'
import {
  approvalText,
  decodeCdcExtra,
  envTagColor,
  formatSourceScopeLabel,
  instAddr,
  instEnv,
  isValidCdcTaskID,
  statusTagColor,
  statusText,
  syncTableCountView,
  typeTagColor,
  typeText,
} from './cdc'

const { Title, Text } = Typography

const RUNNING_STATUSES = new Set([
  'init', 'running', 'paused', 'error', 'failed', 'creating', 'provisioning', 'configuring', 'reinitializing',
])
const TYPE_ALL = [
  { label: '初始化', value: 'init' },
  { label: '新增表', value: 'add_table' },
  { label: '移除表', value: 'remove_table' },
  { label: '源表加列', value: 'alter_schema' },
  { label: '删除任务', value: 'delete_task' },
]

const COL_STORE = 'auto-dba.cdc.listCols.v1'
const columnDefs = [
  { key: 'name', label: '任务名称', locked: true },
  { key: 'type', label: '任务类型' },
  { key: 'env', label: '环境' },
  { key: 'publication', label: 'Publication' },
  { key: 'source', label: '源端' },
  { key: 'target', label: '目标端' },
  { key: 'tables', label: '同步表数' },
  { key: 'debezium', label: 'Debezium 模式' },
  { key: 'status', label: '状态' },
  { key: 'schema', label: '结构差异' },
  { key: 'approval', label: '审批' },
  { key: 'created_by', label: '创建人' },
  { key: 'created_at', label: '创建时间' },
  { key: 'op', label: '操作', locked: true },
]
const DEFAULT_COLS = ['name', 'type', 'source', 'target', 'tables', 'debezium', 'status', 'schema', 'op']

type TabKey = 'running' | 'pending' | 'approval_result'

function loadCols(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(COL_STORE) || '[]')
    if (Array.isArray(raw) && raw.includes('name')) return raw
  } catch {
    /* ignore */
  }
  return [...DEFAULT_COLS]
}

export default function CdcList() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [allRows, setAllRows] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [driftById, setDriftById] = useState<Record<string, any>>({})
  const [tab, setTab] = useState<TabKey>('running')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [filterEnv, setFilterEnv] = useState('')
  const [filterType, setFilterType] = useState('init')
  const [filterQ, setFilterQ] = useState('')
  const [visibleCols, setVisibleCols] = useState<string[]>(loadCols())
  const [reinitTask, setReinitTask] = useState<any>(null)
  const [batchOpen, setBatchOpen] = useState(false)
  const { isAdmin } = usePermission()

  const instMap = useMemo(() => {
    const m = new Map<string, any>()
    for (const i of instances) m.set(i.id, i)
    return m
  }, [instances])

  const taskTypeOptions = tab === 'running' ? [{ label: '初始化', value: 'init' }] : TYPE_ALL

  function colOn(key: string) {
    return visibleCols.includes(key)
  }
  function toggleCol(key: string, on: boolean) {
    const next = new Set(visibleCols)
    if (on) next.add(key)
    else next.delete(key)
    next.add('name')
    next.add('op')
    const arr = [...next]
    setVisibleCols(arr)
    localStorage.setItem(COL_STORE, JSON.stringify(arr))
  }

  function instOf(id: string) {
    return instMap.get(id)
  }
  function extraOf(row: any) {
    return decodeCdcExtra(row?.description).extra
  }
  function driftOf(row: any) {
    return driftById[row.id]
  }
  function sourceScopeOf(row: any) {
    const extra = extraOf(row)
    return formatSourceScopeLabel(row.source_db, extra.source_db_prefix || '', extra.source_db_names)
  }
  function countOf(row: any) {
    return syncTableCountView(extraOf(row))
  }
  function rowEnv(row: any) {
    return extraOf(row).env || instEnv(instOf(row.source_instance_id))
  }
  function isPending(row: any) {
    return row.approval_status === 'pending' || row.status === 'pending'
  }
  function isApprovalResult(row: any) {
    return (
      row.approval_status === 'approved' ||
      row.approval_status === 'rejected' ||
      row.approval_status === 'canceled'
    )
  }
  function isRunning(row: any) {
    return RUNNING_STATUSES.has(row.status) && !isPending(row)
  }

  const pendingCount = allRows.filter(isPending).length
  const abnormalCount = allRows.filter(
    (r) => isRunning(r) && (r.status === 'error' || r.status === 'failed' || r._pipeline?.healthy === false),
  ).length
  const runningCount = allRows.filter(isRunning).length

  const filtered = useMemo(() => {
    return allRows.filter((r) => {
      if (tab === 'pending' && !isPending(r)) return false
      if (tab === 'approval_result' && !isApprovalResult(r)) return false
      if (tab === 'running' && !isRunning(r)) return false
      if (filterType && r.task_type !== filterType) return false
      if (filterEnv && rowEnv(r) !== filterEnv) return false
      if (filterQ && !String(r.name || '').toLowerCase().includes(filterQ.trim().toLowerCase())) return false
      return true
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allRows, tab, filterType, filterEnv, filterQ, instMap])

  const pageRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filtered.slice(start, start + pageSize)
  }, [filtered, page, pageSize])

  const runningLabel = tab === 'running' ? `运行列表 (${filtered.length || runningCount})` : '运行列表'

  const emptyText =
    ({ running: '暂无运行中的任务', pending: '暂无待审核任务', approval_result: '暂无审批结果' } as Record<TabKey, string>)[
      tab
    ]

  const pageDriftCount = pageRows.filter((r) => {
    const d = driftOf(r)
    return r.task_type === 'init' && d?.state === 'WARN' && Number(d.drift_table_count || 0) > 0
  }).length

  async function loadDriftCache() {
    const { data } = await api.get('/cdc-sync/schema-alerts', { silent: true }).catch(() => ({ data: {} as any }))
    const list = data.data || []
    const m: Record<string, any> = {}
    for (const it of list) {
      if (it?.init_task_id) m[it.init_task_id] = it
    }
    setDriftById(m)
  }

  async function probeRunningPipelines(rows: any[]) {
    const targets = rows.filter((r) => isRunning(r) && r.task_type === 'init').slice(0, 20)
    const chunk = 4
    for (let i = 0; i < targets.length; i += chunk) {
      const part = targets.slice(i, i + chunk)
      // eslint-disable-next-line no-await-in-loop
      await Promise.all(
        part.map(async (row) => {
          if (!isValidCdcTaskID(row.id)) return
          const { data } = await api
            .get(`/cdc-sync/tasks/${row.id}/status`, { silent: true })
            .catch(() => ({ data: {} as any }))
          const pipe = data.data || {}
          row._pipeline = { healthy: pipe.healthy, unhealthy_reason: pipe.unhealthy_reason }
        }),
      )
    }
    setAllRows((prev) => [...prev])
  }

  async function load() {
    setLoading(true)
    setLoadError('')
    try {
      const [tasksRes, inst, envRes] = await Promise.all([
        api.get('/cdc-sync/tasks'),
        api.get('/instances'),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
      ])
      const rows = tasksRes.data.data || []
      setAllRows(rows)
      setInstances(inst.data.data || [])
      await loadDriftCache()
      void probeRunningPipelines(rows)
      const envs = envRes.data.data || []
      let opts = envs
        .map((e: any) => ({ value: e.code || e.name, label: e.name || e.code }))
        .filter((e: any) => e.value)
      if (!opts.length) opts = [{ value: 'test', label: 'test' }, { value: 'prod', label: 'prod' }]
      setEnvOptions(opts)
    } catch (e: any) {
      setLoadError(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  async function act(row: any, kind: string) {
    if (isPending(row) && (kind === 'resume' || kind === 'retry' || kind === 'restart' || kind === 'pause')) {
      message.warning('CDC 任务须审批通过后才能执行')
      return
    }
    const run = async () => {
      const { data } = await api.post(`/cdc-sync/tasks/${row.id}/${kind}`)
      if (data?.data?.status === 'reinitializing') message.success('已提交，正在重新初始化')
      else message.info(data.message || '已更新')
      await load()
    }
    if (kind === 'pause') {
      Modal.confirm({
        title: `确认暂停「${row.name || row.id}」？`,
        okText: '暂停',
        cancelText: '取消',
        onOk: run,
      })
      return
    }
    if (kind === 'restart') {
      Modal.confirm({
        title: `确认重启「${row.name || row.id}」？`,
        content: '重启将重置 Routine Load 与 Debezium Connector，期间会短暂中断。',
        okText: '重启',
        cancelText: '取消',
        onOk: run,
      })
      return
    }
    await run()
  }

  function remove(row: any) {
    let reviewer = ''
    Modal.confirm({
      title: '删除同步任务',
      content: (
        <div>
          <p>
            删除任务「{row.name}」将提交审批，通过后清理 Publication / Slot / Connector（保留 StarRocks 表）。请填写审核人：
          </p>
          <Input placeholder="审核人" onChange={(e) => (reviewer = e.target.value)} />
        </div>
      ),
      okText: '提交审批',
      cancelText: '取消',
      async onOk() {
        if (!reviewer.trim()) {
          message.warning('审核人必填')
          throw new Error('reviewer required')
        }
        const { data } = await api.post(`/cdc-sync/tasks/${row.id}/delete`, { reviewer, approver: reviewer })
        message.success('已提交删除审批')
        if (data.data?.issue_id) navigate('/ops/tickets/' + data.data.issue_id)
        else await load()
      },
    })
  }

  async function refreshTasks() {
    const { data } = await api.get('/cdc-sync/tasks', { silent: true }).catch(() => ({ data: null as any }))
    if (!data) return
    setAllRows((prev) => {
      const pipes = new Map(prev.map((r) => [r.id, r._pipeline]))
      return (data.data || []).map((r: any) => ({ ...r, _pipeline: pipes.get(r.id) }))
    })
  }

  // 初始化 & tab 切换
  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const hasReinitializing = allRows.some((r) => r.status === 'reinitializing')
  useEffect(() => {
    if (!hasReinitializing) return
    const id = window.setInterval(() => void refreshTasks(), 3000)
    return () => window.clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasReinitializing])

  useEffect(() => {
    const t = searchParams.get('tab')
    if (t === 'pending' || t === 'approval_result' || t === 'running') {
      setTab(t)
      setPage(1)
      setFilterType(t === 'running' ? 'init' : '')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(filtered.length / pageSize) || 1)
    if (page > maxPage) setPage(maxPage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filtered, pageSize])

  function onTabChange(key: string) {
    setTab(key as TabKey)
    setPage(1)
    setFilterType(key === 'running' ? 'init' : '')
  }

  function goCreate(type: string) {
    if (type === 'init') {
      navigateToWorkOrderCreate(navigate, 'cdc', { type })
      return
    }
    navigate(`/ops/cdc/create?type=${type}`)
  }

  const columns: ColumnsType<any> = []
  if (colOn('name')) {
    columns.push({
      title: '任务名称',
      key: 'name',
      width: 200,
      fixed: 'left',
      ellipsis: true,
      render: (_: any, row: any) => (
        <a onClick={() => navigate('/ops/cdc/' + row.id)}>{row.name || '(未命名)'}</a>
      ),
    })
  }
  if (colOn('type')) {
    columns.push({
      title: '任务类型',
      key: 'type',
      width: 110,
      render: (_: any, row: any) => <Tag color={typeTagColor(row.task_type)}>{typeText(row.task_type)}</Tag>,
    })
  }
  if (colOn('env')) {
    columns.push({
      title: '环境',
      key: 'env',
      width: 90,
      render: (_: any, row: any) => <Tag color={envTagColor(rowEnv(row))}>{rowEnv(row) || '—'}</Tag>,
    })
  }
  if (colOn('publication')) {
    columns.push({
      title: 'Publication',
      key: 'publication',
      width: 150,
      ellipsis: true,
      render: (_: any, row: any) => extraOf(row).publication_name || '—',
    })
  }
  if (colOn('source')) {
    columns.push({
      title: '源端',
      key: 'source',
      width: 180,
      ellipsis: true,
      render: (_: any, row: any) => (
        <div style={{ lineHeight: 1.4 }}>
          <div>{instAddr(instOf(row.source_instance_id))}</div>
          <div style={{ color: '#8c8c8c', fontSize: 12 }}>{sourceScopeOf(row)}</div>
        </div>
      ),
    })
  }
  if (colOn('target')) {
    columns.push({
      title: '目标端',
      key: 'target',
      width: 180,
      ellipsis: true,
      render: (_: any, row: any) => (
        <div style={{ lineHeight: 1.4 }}>
          <div>{row.target_instance_id ? instAddr(instOf(row.target_instance_id)) : '未配置'}</div>
          <div style={{ color: '#8c8c8c', fontSize: 12 }}>
            {row.target_db || (row.target_instance_id ? '—' : '未配置')}
          </div>
        </div>
      ),
    })
  }
  if (colOn('tables')) {
    columns.push({
      title: '同步表数',
      key: 'tables',
      width: 150,
      render: (_: any, row: any) => {
        const c = countOf(row)
        return (
          <Tooltip title="源端：任务配置表数，与 Publication 一致；目标：映射组/目标表数；分表：分表归并映射组数">
            <span>
              源端 <b>{c.source}</b>
              <span style={{ color: '#c0c4cc' }}> · </span>目标 <b>{c.target}</b>
              {c.shard ? (
                <>
                  <span style={{ color: '#c0c4cc' }}> · </span>分表 <b>{c.shard}</b>
                </>
              ) : null}
            </span>
          </Tooltip>
        )
      },
    })
  }
  if (colOn('debezium')) {
    columns.push({
      title: 'Debezium 模式',
      key: 'debezium',
      width: 130,
      render: (_: any, row: any) => extraOf(row).debezium_mode || 'after_only',
    })
  }
  if (colOn('status')) {
    columns.push({
      title: '状态',
      key: 'status',
      width: 100,
      render: (_: any, row: any) => {
        const unhealthy = row._pipeline?.healthy === false && row.status !== 'reinitializing'
        return (
          <Tag color={unhealthy ? 'error' : statusTagColor(row.status)}>
            {unhealthy ? '异常' : statusText(row.status)}
          </Tag>
        )
      },
    })
  }
  if (tab === 'running' && colOn('schema')) {
    columns.push({
      title: '结构差异',
      key: 'schema',
      width: 120,
      render: (_: any, row: any) => {
        const d = driftOf(row)
        if (d?.state === 'WARN' && d.drift_table_count) {
          return <Tag color="warning">{d.drift_table_count} 张表有差异</Tag>
        }
        if (d?.state === 'ERROR') return <Tag color="error">检查失败</Tag>
        if (d?.state === 'OK' || (d?.state === 'WARN' && !d.drift_table_count)) return <Tag color="success">一致</Tag>
        return <span style={{ color: '#c0c4cc' }}>未检查</span>
      },
    })
  }
  if (colOn('approval')) {
    columns.push({
      title: '审批',
      key: 'approval',
      width: 100,
      render: (_: any, row: any) =>
        row.issue_id && approvalText(row.approval_status) !== '—' ? (
          <a onClick={() => navigate('/ops/tickets/' + row.issue_id)}>{approvalText(row.approval_status)}</a>
        ) : (
          <span style={{ color: '#c0c4cc' }}>{approvalText(row.approval_status)}</span>
        ),
    })
  }
  if (colOn('created_by')) {
    columns.push({
      title: '创建人',
      key: 'created_by',
      width: 110,
      ellipsis: true,
      render: (_: any, row: any) => extraOf(row).created_by || '—',
    })
  }
  if (colOn('created_at')) {
    columns.push({ title: '创建时间', key: 'created_at', dataIndex: 'created_at', width: 180 })
  }
  if (colOn('op')) {
    columns.push({
      title: '操作',
      key: 'op',
      width: 360,
      fixed: 'right',
      render: (_: any, row: any) => (
        <Space size={0} wrap={false} className="cdc-row-ops">
          <Button
            type="link"
            size="small"
            className="!px-1"
            icon={<EyeOutlined />}
            onClick={() => navigate('/ops/cdc/' + row.id)}
          >
            详情
          </Button>
          {row.status === 'running' && !isPending(row) && (
            <Button
              type="link"
              size="small"
              className="!px-1"
              icon={<PauseCircleOutlined />}
              onClick={() => act(row, 'pause')}
            >
              暂停
            </Button>
          )}
          {row.status === 'paused' && !isPending(row) && (
            <Button
              type="link"
              size="small"
              className="!px-1"
              icon={<PlayCircleOutlined />}
              onClick={() => act(row, 'resume')}
            >
              恢复
            </Button>
          )}
          {row.status === 'running' && !isPending(row) && (
            <Button
              type="link"
              size="small"
              className="!px-1"
              icon={<ReloadOutlined />}
              onClick={() => act(row, 'restart')}
            >
              重启
            </Button>
          )}
          {(row.status === 'error' || row.status === 'failed') && !isPending(row) && (
            <Button
              type="link"
              size="small"
              className="!px-1"
              icon={<RedoOutlined />}
              onClick={() => act(row, 'retry')}
            >
              重试
            </Button>
          )}
          {row.can_reinit && !isPending(row) && (
            <Tooltip title="Debezium Connector 在 Connect 上缺失，按原配置补齐资源">
              <Button
                type="link"
                size="small"
                className="!px-1"
                icon={<SyncOutlined />}
                onClick={() => setReinitTask(row)}
              >
                重新初始化
              </Button>
            </Tooltip>
          )}
          {row.task_type === 'init' && !isPending(row) && row.status !== 'reinitializing' && (
            <Button
              type="link"
              size="small"
              className="!px-1"
              danger
              icon={<DeleteOutlined />}
              onClick={() => remove(row)}
            >
              删除
            </Button>
          )}
        </Space>
      ),
    })
  }

  const scrollX = columns.reduce((sum, c) => sum + (Number(c.width) || 120), 0)

  const columnPicker = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      {columnDefs.map((c) => (
        <Checkbox
          key={c.key}
          checked={visibleCols.includes(c.key)}
          disabled={c.locked}
          onChange={(e) => toggleCol(c.key, e.target.checked)}
        >
          {c.label}
        </Checkbox>
      ))}
    </div>
  )

  return (
    <div className="ops-page" style={{ overflow: 'auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <Title level={4} style={{ margin: 0 }}>
          CDC 同步任务
        </Title>
        <Space>
          {isAdmin && (
            <Button icon={<SyncOutlined />} onClick={() => setBatchOpen(true)}>
              按 Debezium 服务重新初始化
            </Button>
          )}
          <Button onClick={() => goCreate('remove_table')}>移除同步表</Button>
          <Button onClick={() => goCreate('alter_schema')}>源表加列</Button>
          <Button onClick={() => goCreate('add_table')}>新增同步表</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => goCreate('init')}>
            初始化同步
          </Button>
        </Space>
      </div>

      {loadError && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message="CDC 接口不可用" description={loadError} />
      )}

      <Tabs
        activeKey={tab}
        onChange={onTabChange}
        items={[
          {
            key: 'running',
            label: (
              <span>
                <span style={{ color: abnormalCount ? '#cf1322' : undefined }}>{runningLabel}</span>
                {abnormalCount ? <Badge count={abnormalCount} style={{ marginLeft: 6 }} /> : null}
              </span>
            ),
          },
          {
            key: 'pending',
            label: (
              <span>
                待审核列表
                {pendingCount ? <Badge count={pendingCount} color="blue" style={{ marginLeft: 6 }} /> : null}
              </span>
            ),
          },
          { key: 'approval_result', label: '审批结果列表' },
        ]}
      />

      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <Form
          layout="inline"
          onSubmitCapture={(e) => {
            e.preventDefault()
            setPage(1)
          }}
        >
          <Form.Item label="环境">
            <Select
              allowClear
              placeholder="全部"
              style={{ width: 140 }}
              value={filterEnv || undefined}
              onChange={(v) => setFilterEnv(v || '')}
              options={envOptions}
            />
          </Form.Item>
          <Form.Item label="任务类型">
            <Select
              allowClear={tab !== 'running'}
              disabled={tab === 'running'}
              placeholder={tab === 'running' ? '初始化' : '全部'}
              style={{ width: 130 }}
              value={filterType || undefined}
              onChange={(v) => setFilterType(v || '')}
              options={taskTypeOptions}
            />
          </Form.Item>
          <Form.Item label="任务名称">
            <Input
              allowClear
              placeholder="模糊搜索"
              style={{ width: 160 }}
              value={filterQ}
              onChange={(e) => setFilterQ(e.target.value)}
            />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" disabled={loading} onClick={() => setPage(1)}>
                查询
              </Button>
              <Button icon={<ReloadOutlined />} disabled={loading} onClick={load}>
                刷新
              </Button>
            </Space>
          </Form.Item>
        </Form>
        <Popover content={columnPicker} trigger="click" placement="bottomRight">
          <Button>设置列</Button>
        </Popover>
      </div>

      {tab === 'running' && pageDriftCount > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ margin: '12px 0' }}
          message={`本页有 ${pageDriftCount} 个运行中的初始化任务存在表结构差异`}
          description="PostgreSQL 已有、StarRocks 目标表尚未同步的字段需通过「源表加列」处理。点击任务名称进入详情查看明细。"
        />
      )}
      {tab !== 'running' && (
        <p className="hint" style={{ marginTop: 12 }}>
          任务类型选「全部」可查看 init / 新增表 / 移除表 / 源表加列 全部审批申请记录
        </p>
      )}

      <ResponsiveTable
        rowKey="id"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={pageRows}
        scroll={{ x: scrollX }}
        locale={{ emptyText }}
        style={{ marginTop: 12 }}
        pagination={{
          current: page,
          pageSize,
          total: filtered.length,
          showSizeChanger: true,
          pageSizeOptions: ['10', '20', '50'],
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, ps) => {
            setPage(p)
            setPageSize(ps)
          },
        }}
      />
      <CdcReinitModal
        open={!!reinitTask}
        task={reinitTask}
        onClose={() => setReinitTask(null)}
        onSubmitted={() => void refreshTasks()}
      />
      <CdcReinitBatchModal open={batchOpen} onClose={() => setBatchOpen(false)} onSubmitted={() => void refreshTasks()} />
    </div>
  )
}
