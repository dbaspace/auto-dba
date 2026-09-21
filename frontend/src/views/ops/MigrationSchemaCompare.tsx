import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Button,
  Card,
  Col,
  Drawer,
  Input,
  Popconfirm,
  Progress,
  Row,
  Select,
  Space,
  Table,
  Tag,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import './Migration.css'

type Inst = { id: string; name?: string; db_type?: string; host?: string; port?: number; env?: string }

function isPG(t?: string) {
  const n = String(t || '').toLowerCase()
  return n === 'postgres' || n === 'postgresql' || n === 'pg' || n === 'pgsql'
}
function isMySQLFamily(t?: string) {
  const n = String(t || '').toLowerCase()
  return n === 'mysql' || n === 'tidb' || n === 'oceanbase'
}
function sameFamily(a?: string, b?: string) {
  if (isPG(a) && isPG(b)) return true
  if (isMySQLFamily(a) && isMySQLFamily(b)) return true
  return false
}
function familyBadgeLabel(t?: string) {
  if (isPG(t)) return 'PostgreSQL'
  if (isMySQLFamily(t)) {
    const n = String(t || '').toLowerCase()
    if (n === 'tidb') return 'TiDB'
    if (n === 'oceanbase') return 'OceanBase'
    return 'MySQL'
  }
  return t || '未选'
}
function instLabel(i: Inst) {
  return `${i.name || i.id} (${i.db_type || '?'}) ${i.host || ''}:${i.port || ''}`
}

type SideState = {
  instanceId: string
  db: string
  schema: string
  dbs: string[]
  tables: string[]
  loadingDBs: boolean
  loadingTables: boolean
}

const emptySide = (): SideState => ({
  instanceId: '',
  db: '',
  schema: 'public',
  dbs: [],
  tables: [],
  loadingDBs: false,
  loadingTables: false,
})

type DiffRow = {
  table: string
  status: string
  message?: string
  diffs?: { kind: string; name?: string; left?: string; right?: string; message: string }[]
  fix_sql?: string
}

type CompareSummary = {
  total?: number
  match?: number
  differ?: number
  left_only?: number
  right_only?: number
  has_fix_sql?: boolean
  fix_table_count?: number
}

type CompareTask = {
  id: string
  status: string
  left_instance_id: string
  right_instance_id: string
  left_instance_name?: string
  right_instance_name?: string
  left_db: string
  right_db: string
  left_schema?: string
  right_schema?: string
  tables?: string[]
  engine?: string
  summary?: CompareSummary
  items?: DiffRow[]
  fix_sql?: string
  error_message?: string
  progress?: number
  progress_message?: string
  created_by?: string
  created_at?: string
  finished_at?: string
}

const statusColor: Record<string, string> = {
  match: 'success',
  differ: 'error',
  left_only: 'warning',
  right_only: 'warning',
  pending: 'default',
  running: 'processing',
  completed: 'success',
  failed: 'error',
}
const statusLabel: Record<string, string> = {
  match: '一致',
  differ: '不一致',
  left_only: '仅源端',
  right_only: '仅目标端',
  pending: '排队中',
  running: '执行中',
  completed: '已完成',
  failed: '失败',
}

const kindLabel: Record<string, string> = {
  column: '列',
  constraint: '约束',
  foreign_key: '外键',
  index: '索引',
  comment: '注释',
  rls: 'RLS',
  policy: '策略',
  trigger: '触发器',
  function: '函数/例程',
}

function endpointSummary(t: CompareTask) {
  const L = `源 ${t.left_instance_name || t.left_instance_id}/${t.left_db}`
  const R = `目标 ${t.right_instance_name || t.right_instance_id}/${t.right_db}`
  return `${L} → ${R}`
}

function hasMeaningfulFixSQL(sql?: string) {
  const s = (sql || '').trim()
  if (!s) return false
  if (s.startsWith('-- 仅目标') || s.startsWith('-- 无需') || s.startsWith('-- 无修复')) return false
  return /CREATE |ALTER /i.test(s)
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success('已复制到剪贴板')
  } catch {
    message.error('复制失败')
  }
}

async function downloadExport(taskId: string, format: 'csv' | 'json' | 'sql') {
  const res = await api.get(`/migration/schema-compare/tasks/${taskId}/export`, {
    params: { format },
    responseType: 'blob',
  })
  const mime =
    format === 'json'
      ? 'application/json'
      : format === 'sql'
        ? 'application/sql;charset=utf-8'
        : 'text/csv;charset=utf-8'
  const blob = new Blob([res.data], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `schema-compare-${taskId.slice(0, 8)}.${format}`
  a.click()
  URL.revokeObjectURL(url)
}

export default function MigrationSchemaCompare() {
  const [instances, setInstances] = useState<Inst[]>([])
  const [left, setLeft] = useState<SideState>(emptySide)
  const [right, setRight] = useState<SideState>(emptySide)
  const [selectedTables, setSelectedTables] = useState<string[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [tasks, setTasks] = useState<CompareTask[]>([])
  const [loadingTasks, setLoadingTasks] = useState(false)
  const [detail, setDetail] = useState<CompareTask | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailFilter, setDetailFilter] = useState<string | undefined>()
  const pollRef = useRef<number | null>(null)
  const detailPollRef = useRef<number | null>(null)

  const leftInst = instances.find((i) => i.id === left.instanceId)
  const rightInst = instances.find((i) => i.id === right.instanceId)
  const showSchema = isPG(leftInst?.db_type) || isPG(rightInst?.db_type)

  const leftInstanceOptions = useMemo(
    () => instances.map((i) => ({ value: i.id, label: instLabel(i) })),
    [instances],
  )
  const rightInstanceOptions = useMemo(() => {
    const list = leftInst
      ? instances.filter((i) => sameFamily(leftInst.db_type, i.db_type))
      : instances
    return list.map((i) => ({ value: i.id, label: instLabel(i) }))
  }, [instances, leftInst])

  // 源端切换族后，若目标端已选且不同族，清空目标端
  useEffect(() => {
    if (!right.instanceId) return
    if (!leftInst) return
    const cur = instances.find((i) => i.id === right.instanceId)
    if (cur && !sameFamily(leftInst.db_type, cur.db_type)) {
      setRight(emptySide())
      setSelectedTables([])
      message.info('源端类型已变更，已清空不同族的目标端选择')
    }
  }, [leftInst?.id, leftInst?.db_type, right.instanceId, instances])

  const tableOptions = useMemo(() => {
    const set = new Set<string>()
    left.tables.forEach((t) => set.add(t))
    right.tables.forEach((t) => set.add(t))
    return [...set].sort().map((t) => ({ label: t, value: t }))
  }, [left.tables, right.tables])

  const loadTasks = useCallback(async () => {
    setLoadingTasks(true)
    try {
      const { data } = await api.get('/migration/schema-compare/tasks', { params: { limit: 50 } })
      setTasks(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载任务失败')
    } finally {
      setLoadingTasks(false)
    }
  }, [])

  useEffect(() => {
    ;(async () => {
      const { data } = await api.get('/instances')
      const list = (data.data || []).filter((i: Inst) => isPG(i.db_type) || isMySQLFamily(i.db_type))
      setInstances(list)
    })()
    loadTasks()
  }, [loadTasks])

  // 列表轮询：存在 pending/running 时每 2s 刷新
  useEffect(() => {
    const busy = tasks.some((t) => t.status === 'pending' || t.status === 'running')
    if (pollRef.current) {
      window.clearInterval(pollRef.current)
      pollRef.current = null
    }
    if (!busy) return
    pollRef.current = window.setInterval(() => {
      loadTasks()
    }, 2000)
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current)
    }
  }, [tasks, loadTasks])

  // 详情轮询
  useEffect(() => {
    if (detailPollRef.current) {
      window.clearInterval(detailPollRef.current)
      detailPollRef.current = null
    }
    if (!detailOpen || !detail) return
    if (detail.status !== 'pending' && detail.status !== 'running') return
    detailPollRef.current = window.setInterval(async () => {
      try {
        const { data } = await api.get(`/migration/schema-compare/tasks/${detail.id}`)
        setDetail(data.data)
        if (data.data?.status === 'completed' || data.data?.status === 'failed') {
          loadTasks()
        }
      } catch {
        /* ignore */
      }
    }, 2000)
    return () => {
      if (detailPollRef.current) window.clearInterval(detailPollRef.current)
    }
  }, [detailOpen, detail?.id, detail?.status, loadTasks])

  async function loadDBs(side: 'left' | 'right', instanceId: string) {
    const setSide = side === 'left' ? setLeft : setRight
    if (!instanceId) {
      setSide((s) => ({ ...s, instanceId: '', db: '', dbs: [], tables: [] }))
      return
    }
    setSide((s) => ({ ...s, instanceId, db: '', dbs: [], tables: [], loadingDBs: true }))
    try {
      const { data } = await api.get(`/migration/instances/${instanceId}/databases`)
      const dbs = (data.data || [])
        .map((d: any) => (typeof d === 'string' ? d : d.name || d.database || d.db_name))
        .filter(Boolean)
      setSide((s) => ({ ...s, dbs, loadingDBs: false }))
    } catch (e: any) {
      setSide((s) => ({ ...s, loadingDBs: false }))
      message.error(e?.response?.data?.message || '加载库列表失败')
    }
  }

  async function loadTables(side: 'left' | 'right', instanceId: string, db: string) {
    const setSide = side === 'left' ? setLeft : setRight
    setSide((s) => ({ ...s, db, tables: [], loadingTables: true }))
    if (!instanceId || !db) {
      setSide((s) => ({ ...s, loadingTables: false }))
      return
    }
    try {
      const { data } = await api.get(
        `/migration/instances/${instanceId}/databases/${encodeURIComponent(db)}/tables`,
        { params: { names_only: 1 } },
      )
      const tables = (data.data || [])
        .map((t: any) => t.table_name || t.name || (typeof t === 'string' ? t : ''))
        .filter(Boolean)
      setSide((s) => ({ ...s, tables, loadingTables: false }))
    } catch (e: any) {
      setSide((s) => ({ ...s, loadingTables: false }))
      message.error(e?.response?.data?.message || '加载表列表失败')
    }
  }

  async function runCompare() {
    if (!left.instanceId || !right.instanceId || !left.db || !right.db) {
      message.warning('请选择源端与目标端实例与库')
      return
    }
    if (!sameFamily(leftInst?.db_type, rightInst?.db_type)) {
      message.warning('源端与目标端数据库类型须同属一类（PostgreSQL 互比，或 MySQL 族互比）')
      return
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/migration/schema-compare/tasks', {
        left_instance_id: left.instanceId,
        right_instance_id: right.instanceId,
        left_db: left.db,
        right_db: right.db,
        left_schema: left.schema || 'public',
        right_schema: right.schema || 'public',
        tables: selectedTables,
      })
      message.success(data.message || '任务已创建')
      await loadTasks()
      if (data.data?.id) {
        const det = await api.get(`/migration/schema-compare/tasks/${data.data.id}`)
        setDetail(det.data.data)
        setDetailOpen(true)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || '创建任务失败')
    } finally {
      setSubmitting(false)
    }
  }

  async function openDetail(row: CompareTask) {
    try {
      const { data } = await api.get(`/migration/schema-compare/tasks/${row.id}`)
      setDetail(data.data)
      setDetailFilter(undefined)
      setDetailOpen(true)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载详情失败')
    }
  }

  async function removeTask(id: string) {
    try {
      await api.delete(`/migration/schema-compare/tasks/${id}`)
      message.success('已删除')
      if (detail?.id === id) setDetailOpen(false)
      loadTasks()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '删除失败')
    }
  }

  const detailRows = useMemo(() => {
    const items = detail?.items || []
    if (!detailFilter) return items
    return items.filter((r) => r.status === detailFilter)
  }, [detail, detailFilter])

  const taskColumns: ColumnsType<CompareTask> = [
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (s: string) => <Tag color={statusColor[s] || 'default'}>{statusLabel[s] || s}</Tag>,
    },
    {
      title: '对比端点',
      ellipsis: true,
      render: (_: any, row: CompareTask) => endpointSummary(row),
    },
    {
      title: '进度',
      width: 280,
      render: (_: any, row: CompareTask) =>
        row.status === 'pending' || row.status === 'running' ? (
          <Progress percent={row.progress || 0} size="small" status="active" />
        ) : row.status === 'completed' ? (
          <span className="schema-compare-progress-text">
            一致 {row.summary?.match ?? 0} · 差异 {row.summary?.differ ?? 0} · 仅源{' '}
            {row.summary?.left_only ?? 0} · 仅目标 {row.summary?.right_only ?? 0}
            {row.summary?.has_fix_sql ? (
              <Tag color="blue" style={{ marginLeft: 6 }}>
                有修复SQL
                {row.summary?.fix_table_count ? ` ${row.summary.fix_table_count}` : ''}
              </Tag>
            ) : null}
          </span>
        ) : (
          <span className="muted">{row.error_message || '—'}</span>
        ),
    },
    { title: '创建人', dataIndex: 'created_by', width: 100, render: (v) => v || '—' },
    { title: '创建时间', dataIndex: 'created_at', width: 180 },
    {
      title: '操作',
      width: 280,
      fixed: 'right',
      render: (_: any, row: CompareTask) => (
        <Space size={4} className="mig-ops">
          <Button type="link" size="small" onClick={() => openDetail(row)}>
            查看
          </Button>
          {row.status === 'completed' && (
            <>
              <Button type="link" size="small" onClick={() => downloadExport(row.id, 'csv')}>
                CSV
              </Button>
              <Button type="link" size="small" onClick={() => downloadExport(row.id, 'json')}>
                JSON
              </Button>
              {row.summary?.has_fix_sql && (
                <Button type="link" size="small" onClick={() => downloadExport(row.id, 'sql')}>
                  SQL
                </Button>
              )}
            </>
          )}
          {(row.status === 'completed' || row.status === 'failed') && (
            <Popconfirm title="确认删除该任务？" onConfirm={() => removeTask(row.id)}>
              <Button type="link" size="small" danger>
                删除
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]

  const diffColumns: ColumnsType<DiffRow> = [
    { title: '表名', dataIndex: 'table', width: 160 },
    {
      title: '结果',
      dataIndex: 'status',
      width: 100,
      render: (s: string) => <Tag color={statusColor[s] || 'default'}>{statusLabel[s] || s}</Tag>,
    },
    { title: '说明', dataIndex: 'message', width: 120 },
    {
      title: '差异明细',
      render: (_: any, row: DiffRow) => {
        if (!row.diffs?.length) return '—'
        return (
          <div className="schema-diff-detail">
            {row.diffs.map((d, i) => (
              <div key={i} className="schema-diff-line">
                <Tag>{kindLabel[d.kind] || d.kind}</Tag>
                {d.name ? <code>{d.name}</code> : null} {d.message}
                {(d.left || d.right) && (
                  <div className="muted">
                    {d.left != null && d.left !== '' && <div>源: {d.left}</div>}
                    {d.right != null && d.right !== '' && <div>目标: {d.right}</div>}
                  </div>
                )}
              </div>
            ))}
          </div>
        )
      },
    },
    {
      title: '修复 SQL',
      width: 120,
      render: (_: any, row: DiffRow) => {
        const sql = (row.fix_sql || '').trim()
        if (!sql) return '—'
        return (
          <Space size={0} direction="vertical">
            {hasMeaningfulFixSQL(sql) ? <Tag color="blue">可修复</Tag> : <span className="muted">注释</span>}
            <Button type="link" size="small" onClick={() => copyText(sql)}>
              复制
            </Button>
          </Space>
        )
      },
    },
  ]

  function renderSide(
    side: 'left' | 'right',
    state: SideState,
    title: string,
    instanceOptions: { value: string; label: string }[],
  ) {
    const setSide = side === 'left' ? setLeft : setRight
    const inst = side === 'left' ? leftInst : rightInst
    const typeTag = (
      <Tag color={isPG(inst?.db_type) ? 'geekblue' : isMySQLFamily(inst?.db_type) ? 'green' : 'default'}>
        {familyBadgeLabel(inst?.db_type)}
      </Tag>
    )
    return (
      <Card
        title={
          <Space size={8}>
            <span>{title}</span>
            {typeTag}
          </Space>
        }
        size="small"
      >
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <div>
            <div className="muted mb-xs">实例</div>
            <Select
              showSearch
              allowClear
              optionFilterProp="label"
              style={{ width: '100%' }}
              placeholder={side === 'right' && leftInst ? '同族实例' : '选择实例'}
              value={state.instanceId || undefined}
              options={instanceOptions}
              onChange={(id) => loadDBs(side, id || '')}
            />
          </div>
          <div>
            <div className="muted mb-xs">库</div>
            <Select
              showSearch
              optionFilterProp="label"
              style={{ width: '100%' }}
              placeholder="选择库"
              loading={state.loadingDBs}
              value={state.db || undefined}
              options={state.dbs.map((d) => ({ value: d, label: d }))}
              onChange={(db) => loadTables(side, state.instanceId, db)}
            />
          </div>
          {showSchema && isPG(inst?.db_type) && (
            <div>
              <div className="muted mb-xs">Schema</div>
              <Input
                value={state.schema}
                onChange={(e) => setSide((s) => ({ ...s, schema: e.target.value }))}
                placeholder="public"
              />
            </div>
          )}
        </Space>
      </Card>
    )
  }

  return (
    <div className="mig-page schema-compare-page">
      <div className="ops-title">
        <h1>表结构对比</h1>
      </div>

      <Row gutter={16} className="mb">
        <Col span={12}>{renderSide('left', left, '源端', leftInstanceOptions)}</Col>
        <Col span={12}>{renderSide('right', right, '目标端', rightInstanceOptions)}</Col>
      </Row>

      <Card size="small" className="mb">
        <Space wrap style={{ width: '100%' }} align="start">
          <div style={{ minWidth: 320, flex: 1 }}>
            <div className="muted mb-xs">
              对比表（可多选；不选则对比同名表交集，上限 200）。差异修复 SQL 按「源→目标」生成（只增不删）。
              PostgreSQL 含 RLS/Policy；MySQL 族（含 TiDB/OceanBase）对齐列/索引/约束/外键/注释/触发器/例程，不含
              RLS。同族才可互比；实例类型取自实例配置，目标端将按源端自动筛选。
            </div>
            <Select
              mode="multiple"
              allowClear
              showSearch
              maxTagCount="responsive"
              style={{ width: '100%' }}
              placeholder="不选 = 自动取源端与目标端同名表"
              options={tableOptions}
              value={selectedTables}
              onChange={setSelectedTables}
            />
          </div>
          <Button type="primary" loading={submitting} onClick={runCompare} style={{ marginTop: 22 }}>
            开始对比
          </Button>
        </Space>
      </Card>

      <Card
        size="small"
        title="对比任务"
        extra={
          <Button size="small" onClick={loadTasks} loading={loadingTasks}>
            刷新
          </Button>
        }
      >
        <Table
          rowKey="id"
          size="small"
          bordered
          loading={loadingTasks}
          pagination={{ pageSize: 20 }}
          dataSource={tasks}
          columns={taskColumns}
          scroll={{ x: 1100 }}
          locale={{ emptyText: '暂无任务' }}
        />
      </Card>

      <Drawer
        title={detail ? `任务详情 · ${detail.id.slice(0, 8)}` : '任务详情'}
        width={960}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
        extra={
          detail?.status === 'completed' ? (
            <Space>
              <Button size="small" onClick={() => downloadExport(detail.id, 'csv')}>
                导出 CSV
              </Button>
              <Button size="small" onClick={() => downloadExport(detail.id, 'json')}>
                导出 JSON
              </Button>
              {hasMeaningfulFixSQL(detail.fix_sql) && (
                <>
                  <Button size="small" onClick={() => copyText(detail.fix_sql || '')}>
                    复制修复 SQL
                  </Button>
                  <Button size="small" type="primary" onClick={() => downloadExport(detail.id, 'sql')}>
                    导出 SQL
                  </Button>
                </>
              )}
            </Space>
          ) : null
        }
      >
        {detail && (
          <>
            <div className="mb schema-compare-summary">
              <Tag color={statusColor[detail.status]}>{statusLabel[detail.status] || detail.status}</Tag>
              <span className="muted">{endpointSummary(detail)}</span>
            </div>
            {(detail.status === 'pending' || detail.status === 'running') && (
              <div className="mb">
                <Progress percent={detail.progress || 0} status="active" />
                <div className="muted">{detail.progress_message || '执行中…'}</div>
              </div>
            )}
            {detail.status === 'failed' && (
              <div className="mb" style={{ color: '#cf1322' }}>
                {detail.error_message || '执行失败'}
              </div>
            )}
            {detail.status === 'completed' && (
              <>
                <div className="mb schema-compare-summary">
                  <Tag>共 {detail.summary?.total ?? 0}</Tag>
                  <Tag color="success">一致 {detail.summary?.match ?? 0}</Tag>
                  <Tag color="error">不一致 {detail.summary?.differ ?? 0}</Tag>
                  <Tag color="warning">仅源端 {detail.summary?.left_only ?? 0}</Tag>
                  <Tag color="warning">仅目标端 {detail.summary?.right_only ?? 0}</Tag>
                  {detail.summary?.has_fix_sql ? (
                    <Tag color="blue">修复 SQL {detail.summary?.fix_table_count ?? 0} 表</Tag>
                  ) : (
                    <Tag>无需修复 SQL</Tag>
                  )}
                  <Select
                    allowClear
                    placeholder="按结果筛选"
                    style={{ width: 140, marginLeft: 8 }}
                    value={detailFilter}
                    onChange={setDetailFilter}
                    options={[
                      { value: 'match', label: '一致' },
                      { value: 'differ', label: '不一致' },
                      { value: 'left_only', label: '仅源端' },
                      { value: 'right_only', label: '仅目标端' },
                    ]}
                  />
                </div>
                {hasMeaningfulFixSQL(detail.fix_sql) && (
                  <Card
                    size="small"
                    className="mb"
                    title="源→目标 修复 SQL（请人工审核后执行）"
                    extra={
                      <Space>
                        <Button size="small" onClick={() => copyText(detail.fix_sql || '')}>
                          复制全部
                        </Button>
                        <Button size="small" onClick={() => downloadExport(detail.id, 'sql')}>
                          下载 .sql
                        </Button>
                      </Space>
                    }
                  >
                    <pre className="schema-fix-sql-block">{detail.fix_sql}</pre>
                  </Card>
                )}
                <Table
                  rowKey={(r) => r.table}
                  size="small"
                  bordered
                  pagination={{ pageSize: 50 }}
                  dataSource={detailRows}
                  columns={diffColumns}
                  expandable={{
                    rowExpandable: (r) => !!(r.fix_sql || '').trim(),
                    expandedRowRender: (r) => (
                      <pre className="schema-fix-sql-block small">{r.fix_sql}</pre>
                    ),
                  }}
                />
              </>
            )}
          </>
        )}
      </Drawer>
    </div>
  )
}
