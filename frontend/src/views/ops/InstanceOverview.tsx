import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Input, Select, Space, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useNavigate } from 'react-router-dom'
import api from '../../api/request'
import './InstanceOverview.css'

type Sample = {
  reachable?: boolean
  partial?: boolean
  error_kind?: string
  error?: string
  version?: string
  role?: string
  uptime_sec?: number
  conn_used?: number
  conn_max?: number
  mem_used?: number
  mem_max?: number
  lag_bytes?: number
  lag_known?: boolean
  lag_seconds?: number
  lag_sec_known?: boolean
  hit_rate?: number
  hit_known?: boolean
  collected_at?: string
}

type Row = {
  id: string
  name: string
  db_type: string
  host: string
  port: number
  env_id: string
  env_name: string
  collectable: boolean
  sample?: Sample
}

type CollectState = { status?: string; done?: number; total?: number; message?: string }

type EnvOpt = { id: string; name?: string; code?: string }

const STALE_MS = 5 * 60 * 1000
const LAG_BYTES = 16 * 1024 * 1024

function roleText(role?: string) {
  if (role === 'primary') return '主库'
  if (role === 'replica') return '从库'
  return '—'
}

function ratio(used?: number, max?: number) {
  if (used == null && max == null) return '—'
  const u = used ?? 0
  if (!max) return `${u}/不限`
  return `${u}/${max}`
}

function bytesText(n?: number) {
  if (n == null) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function uptimeText(sec?: number) {
  if (!sec) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  if (d > 0) return `${d}天${h}小时`
  const m = Math.floor((sec % 3600) / 60)
  if (h > 0) return `${h}小时${m}分`
  return `${m}分`
}

function lagText(s?: Sample) {
  if (!s || s.role !== 'replica') return '—'
  const parts: string[] = []
  if (s.lag_known) parts.push(bytesText(s.lag_bytes || 0))
  if (s.lag_sec_known) parts.push(`${s.lag_seconds || 0}s`)
  return parts.length ? parts.join(' / ') : '—'
}

function stale(s?: Sample) {
  if (!s?.collected_at) return false
  const t = Date.parse(s.collected_at)
  return Number.isFinite(t) && Date.now() - t > STALE_MS
}

function versionMixKeys(rows: Row[]) {
  const groups = new Map<string, Set<string>>()
  for (const row of rows) {
    const v = row.sample?.version?.trim()
    if (!row.sample?.reachable || !v) continue
    const key = `${row.env_id}|${row.db_type}`
    const set = groups.get(key) || new Set<string>()
    set.add(v)
    groups.set(key, set)
  }
  const mixed = new Set<string>()
  groups.forEach((set, key) => {
    if (set.size > 1) mixed.add(key)
  })
  return mixed
}

function anomalyTags(row: Row, mixed: Set<string>) {
  const s = row.sample
  const tags: string[] = []
  if (!s) return tags
  if (s.error_kind === 'skip') return ['暂不采集']
  if (!s.reachable) tags.push(s.error || '不可达')
  else if (s.error_kind === 'permission') tags.push('权限不足')
  if (s.conn_max && s.conn_used != null && s.conn_used / s.conn_max >= 0.8) tags.push('连接偏高')
  if (s.mem_max && s.mem_used != null && s.mem_used / s.mem_max >= 0.8) tags.push('内存偏高')
  if ((s.lag_known && (s.lag_bytes || 0) >= LAG_BYTES) || (s.lag_sec_known && (s.lag_seconds || 0) >= 10)) {
    tags.push('复制延迟')
  }
  if (s.version && mixed.has(`${row.env_id}|${row.db_type}`)) tags.push('版本不一致')
  return tags
}

function csvCell(v: string) {
  if (/[",\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`
  return v
}

export default function InstanceOverview() {
  const navigate = useNavigate()
  const [dbType, setDbType] = useState('postgres')
  const [env, setEnv] = useState<string | undefined>()
  const [q, setQ] = useState('')
  const [envs, setEnvs] = useState<EnvOpt[]>([])
  const [rows, setRows] = useState<Row[]>([])
  const [collect, setCollect] = useState<CollectState>({})
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/instance-overview', {
        params: { db_type: dbType, env: env || '', q },
      })
      setRows((data?.data || []) as Row[])
      setCollect((data?.collect || {}) as CollectState)
    } catch {
      /* 拦截器已提示 */
    } finally {
      setLoading(false)
    }
  }, [dbType, env, q])

  useEffect(() => {
    api
      .get('/environments')
      .then(({ data }) => setEnvs((data?.data || []) as EnvOpt[]))
      .catch(() => setEnvs([]))
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (collect.status !== 'running') return
    const timer = window.setInterval(() => {
      void load()
    }, 1000)
    return () => window.clearInterval(timer)
  }, [collect.status, load])

  const refresh = async () => {
    try {
      const { data } = await api.post('/instance-overview/collect', { db_type: dbType, env: env || '', q })
      setCollect((data?.collect || {}) as CollectState)
      if (data?.collect?.message) message.info(data.collect.message)
      void load()
    } catch {
      /* 拦截器已提示 */
    }
  }

  const mixed = useMemo(() => versionMixKeys(rows), [rows])
  const sorted = useMemo(() => {
    return [...rows].sort((a, b) => {
      const da = anomalyTags(a, mixed).filter((t) => t !== '暂不采集').length
      const db = anomalyTags(b, mixed).filter((t) => t !== '暂不采集').length
      if (da !== db) return db - da
      return (a.name || a.host).localeCompare(b.name || b.host)
    })
  }, [rows, mixed])

  const summary = useMemo(() => {
    let down = 0
    let primary = 0
    let replica = 0
    const versions = new Set<string>()
    for (const row of rows) {
      const s = row.sample
      if (s?.reachable === false && s.error_kind !== 'skip') down++
      if (s?.role === 'primary') primary++
      if (s?.role === 'replica') replica++
      if (s?.version) versions.add(s.version)
    }
    return { total: rows.length, down, primary, replica, versions: versions.size }
  }, [rows])

  const showRedis = dbType === 'redis'
  const showRuntime = dbType === 'postgres' || dbType === 'redis'

  const columns: ColumnsType<Row> = [
    { title: '实例名', dataIndex: 'name', width: 180, ellipsis: true },
    {
      title: '地址',
      width: 200,
      ellipsis: true,
      render: (_, row) => `${row.host || '—'}:${row.port || ''}`,
    },
    { title: '环境', dataIndex: 'env_name', width: 140, ellipsis: true },
    {
      title: '版本',
      width: 140,
      render: (_, row) => {
        const v = row.sample?.version
        const mixedTag = v && mixed.has(`${row.env_id}|${row.db_type}`)
        return (
          <span>
            {v || '—'}
            {mixedTag ? <Tag color="orange">不一致</Tag> : null}
          </span>
        )
      },
    },
    { title: '角色', width: 80, render: (_, row) => roleText(row.sample?.role) },
    {
      title: '状态',
      width: 160,
      render: (_, row) => {
        const tags = anomalyTags(row, mixed)
        if (!row.sample) return '未采集'
        if (!tags.length) return <Tag color="green">正常</Tag>
        return (
          <Space size={4} wrap>
            {tags.map((t) => (
              <Tag key={t} color={t === '暂不采集' ? 'default' : 'red'}>
                {t}
              </Tag>
            ))}
          </Space>
        )
      },
    },
  ]
  if (showRuntime) {
    columns.push(
      { title: '运行时间', width: 110, render: (_, row) => uptimeText(row.sample?.uptime_sec) },
      { title: '连接', width: 110, render: (_, row) => (row.sample ? ratio(row.sample.conn_used, row.sample.conn_max) : '—') },
    )
  }
  if (showRedis) {
    columns.push(
      { title: '内存', width: 160, render: (_, row) => (row.sample ? `${bytesText(row.sample.mem_used)}/${row.sample.mem_max ? bytesText(row.sample.mem_max) : '不限'}` : '—') },
      {
        title: '命中率',
        width: 90,
        render: (_, row) => (row.sample?.hit_known ? `${Math.round((row.sample.hit_rate || 0) * 1000) / 10}%` : '—'),
      },
    )
  }
  if (showRuntime) {
    columns.push({ title: '从库延迟', width: 140, render: (_, row) => lagText(row.sample) })
  }
  columns.push(
    {
      title: '采集时间',
      width: 170,
      render: (_, row) => row.sample?.collected_at?.replace('T', ' ').slice(0, 19) || '—',
    },
    {
      title: '操作',
      width: 150,
      fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          {row.collectable ? (
            <Button
              type="link"
              size="small"
              onClick={() => navigate(`/ops/db-monitor/${row.db_type}?instance=${encodeURIComponent(row.id)}`)}
            >
              监控
            </Button>
          ) : null}
          <Button type="link" size="small" onClick={() => navigate(`/config/instances?db_type=${encodeURIComponent(row.db_type)}`)}>
            配置
          </Button>
        </Space>
      ),
    },
  )

  const exportCsv = () => {
    const header = ['实例名', '地址', '环境', '类型', '版本', '角色', '状态', '连接', '内存', '从库延迟', '采集时间']
    const lines = [header.join(',')]
    for (const row of sorted) {
      const tags = anomalyTags(row, mixed)
      lines.push(
        [
          row.name,
          `${row.host}:${row.port}`,
          row.env_name,
          row.db_type,
          row.sample?.version || '',
          roleText(row.sample?.role),
          tags.join(' ') || (row.sample ? '正常' : '未采集'),
          row.sample ? ratio(row.sample.conn_used, row.sample.conn_max) : '',
          row.sample?.mem_used != null ? bytesText(row.sample.mem_used) : '',
          lagText(row.sample),
          row.sample?.collected_at || '',
        ]
          .map((v) => csvCell(String(v)))
          .join(','),
      )
    }
    const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'instance-overview.csv'
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const running = collect.status === 'running'
  const progress = running && collect.total ? `${collect.done || 0}/${collect.total}` : ''

  return (
    <div className="ops-page instance-overview">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">类型</span>
          <Select
            style={{ width: 140 }}
            value={dbType}
            onChange={setDbType}
            options={[
              { value: 'postgres', label: 'PostgreSQL' },
              { value: 'redis', label: 'Redis' },
              { value: 'other', label: '其它' },
            ]}
          />
        </div>
        <div className="filter-item">
          <span className="label">环境</span>
          <Select
            allowClear
            placeholder="全部环境"
            style={{ width: 180 }}
            value={env}
            onChange={setEnv}
            options={[
              { value: 'unset', label: '未设置环境' },
              ...envs.map((e) => ({ value: e.id, label: e.name || e.code || e.id })),
            ]}
          />
        </div>
        <div className="filter-item">
          <span className="label">名称或地址</span>
          <Input allowClear style={{ width: 220 }} value={q} onChange={(e) => setQ(e.target.value)} placeholder="实例名 / 主机" />
        </div>
        <Space>
          <Button type="primary" loading={running} onClick={() => void refresh()}>
            {running ? `采集 ${progress}` : '刷新'}
          </Button>
          <Button onClick={exportCsv}>导出</Button>
        </Space>
      </div>
      <div className="instance-overview-summary">
        <span>实例 {summary.total}</span>
        <span>不可达 {summary.down}</span>
        <span>主库 {summary.primary}</span>
        <span>从库 {summary.replica}</span>
        <span>版本种类 {summary.versions}</span>
        <span className="instance-overview-note">数字是当前一次采样，不是监控图里的区间平均。超过约 5 分钟会变淡。</span>
      </div>
      <Table
        rowKey="id"
        size="small"
        loading={loading && !running}
        dataSource={sorted}
        columns={columns}
        scroll={{ x: 1400 }}
        pagination={{ defaultPageSize: 20, showSizeChanger: true, pageSizeOptions: ['20', '50', '100'], showTotal: (n) => `共 ${n} 条` }}
        rowClassName={(row) => (stale(row.sample) ? 'instance-overview-stale' : '')}
      />
    </div>
  )
}
