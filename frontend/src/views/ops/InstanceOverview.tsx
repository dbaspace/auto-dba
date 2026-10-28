import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Progress, Select, Space, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { EChartsCoreOption } from 'echarts'
import { useNavigate } from 'react-router-dom'
import api from '../../api/request'
import ChartBox from '../dashboard/ChartBox'
import { groupInstanceTypes, parseInstanceTypes, type InstanceTypeOption } from '../../utils/instanceTypes'
import './InstanceOverview.css'

type CpuPoint = { t?: number; v?: number }

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
  data_bytes?: number
  data_known?: boolean
  storage_rate?: number
  storage_rate_known?: boolean
  cpu_rate?: number
  cpu_known?: boolean
  cpu_series?: CpuPoint[]
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

function memSizeGb(max?: number) {
  if (!max) return '—'
  const gb = max / 1024 ** 3
  const rounded = Math.round(gb * 100) / 100
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(2)
}

function usedStorage(s?: Sample) {
  if (!s?.data_known) return '—'
  return bytesText(s.data_bytes || 0)
}

function storageRateText(s?: Sample) {
  if (!s?.storage_rate_known) return '—'
  return (s.storage_rate || 0).toFixed(2)
}

function approxTotal(s?: Sample) {
  if (!s?.data_known || !s.storage_rate_known || !s.storage_rate) return '—'
  return bytesText((s.data_bytes || 0) / (s.storage_rate / 100))
}

function usagePct(used?: number, max?: number) {
  if (!max || used == null) return null
  return (used / max) * 100
}

function rateLevel(pct: number): 'ok' | 'warn' | 'danger' {
  if (pct >= 80) return 'danger'
  if (pct >= 60) return 'warn'
  return 'ok'
}

function RateCell({ pct, extra }: { pct: number | null; extra?: string }) {
  if (pct == null || !Number.isFinite(pct)) return <span className="ov-muted">{extra || '—'}</span>
  const lv = rateLevel(pct)
  const width = Math.max(0, Math.min(100, pct))
  return (
    <div className={`ov-metric ov-metric-${lv}`}>
      <div className="ov-bar" aria-hidden>
        <span style={{ width: `${width}%` }} />
      </div>
      <span className="ov-metric-num">{Math.round(pct)}%</span>
      {extra ? <span className="ov-metric-extra">{extra}</span> : null}
    </div>
  )
}

function cpuSparkOption(series?: CpuPoint[]): EChartsCoreOption | null {
  const pts = (series || []).filter((p) => p.t != null && p.v != null && Number.isFinite(p.v))
  if (!pts.length) return null
  const xs = pts.map((p) => (p.t as number) * 1000)
  const ys = pts.map((p) => Math.round((p.v as number) * 10) / 10)
  const last = ys[ys.length - 1] ?? 0
  const color = last >= 80 ? '#cf1322' : last >= 60 ? '#d48806' : '#389e0d'
  return {
    animation: false,
    grid: { left: 2, right: 2, top: 4, bottom: 2 },
    xAxis: { type: 'time', show: false },
    yAxis: { type: 'value', show: false, min: 0, max: Math.max(100, ...ys) },
    tooltip: {
      trigger: 'axis',
      appendToBody: true,
      formatter: (params: unknown) => {
        const arr = Array.isArray(params) ? params : [params]
        const p = arr[0] as { value?: [number, number] }
        const pair = p?.value
        if (!pair || pair.length < 2) return ''
        const d = new Date(pair[0])
        const hh = String(d.getHours()).padStart(2, '0')
        const mm = String(d.getMinutes()).padStart(2, '0')
        const ss = String(d.getSeconds()).padStart(2, '0')
        return `${hh}:${mm}:${ss}<br/>CPU ${pair[1]}%`
      },
    },
    series: [
      {
        type: 'line',
        data: xs.map((t, i) => [t, ys[i]]),
        showSymbol: false,
        smooth: true,
        lineStyle: { width: 1.5, color },
        areaStyle: { color, opacity: 0.18 },
      },
    ],
  }
}

function CpuSparkCell({ sample }: { sample?: Sample }) {
  const option = useMemo(() => cpuSparkOption(sample?.cpu_series), [sample?.cpu_series])
  if (!sample?.cpu_known || !option) return <span className="ov-muted">—</span>
  const last = sample.cpu_rate
  const lv = last != null && Number.isFinite(last) ? rateLevel(last) : 'ok'
  return (
    <div className={`ov-cpu ov-cpu-${lv}`}>
      <div className="ov-cpu-chart">
        <ChartBox option={option} height="36px" />
      </div>
      <span className="ov-cpu-num">{last != null && Number.isFinite(last) ? `${Math.round(last)}%` : ''}</span>
    </div>
  )
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

function anomalyTags(row: Row) {
  const s = row.sample
  const tags: string[] = []
  if (!s) return tags
  if (s.error_kind === 'skip') return ['暂不采集']
  if (!s.reachable) tags.push(s.error || '不可达')
  else if (s.error_kind === 'permission') tags.push('权限不足')
  if (s.conn_max && s.conn_used != null && s.conn_used / s.conn_max >= 0.8) tags.push('连接偏高')
  if (s.mem_max && s.mem_used != null && s.mem_used / s.mem_max >= 0.8) tags.push('内存偏高')
  if (s.storage_rate_known && (s.storage_rate || 0) >= 80) tags.push('存储偏高')
  if (s.cpu_known && (s.cpu_rate || 0) >= 80) tags.push('CPU偏高')
  if ((s.lag_known && (s.lag_bytes || 0) >= LAG_BYTES) || (s.lag_sec_known && (s.lag_seconds || 0) >= 10)) {
    tags.push('复制延迟')
  }
  return tags
}

function normEngine(t: string) {
  const n = t.trim().toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}

const FALLBACK_TYPES: InstanceTypeOption[] = [
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'redis', label: 'Redis' },
]

function csvCell(v: string) {
  if (/[",\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`
  return v
}

export default function InstanceOverview() {
  const navigate = useNavigate()
  const [dbType, setDbType] = useState('postgres')
  const [env, setEnv] = useState<string | undefined>()
  const [q, setQ] = useState('')
  const [typeOptions, setTypeOptions] = useState<InstanceTypeOption[]>(FALLBACK_TYPES)
  const [envs, setEnvs] = useState<EnvOpt[]>([])
  const [rows, setRows] = useState<Row[]>([])
  const [collect, setCollect] = useState<CollectState>({})
  const [loading, setLoading] = useState(false)
  const [sampledAt, setSampledAt] = useState('')
  const wasRunning = useRef(false)

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
    api
      .get('/global-args')
      .then(({ data }) => {
        const raw = ((data?.data || []) as { key?: string; value?: string }[]).find((a) => a.key === 'ticket_instance_types')?.value || ''
        const opts = parseInstanceTypes(raw)
        if (!opts.length) return
        setTypeOptions(opts)
        setDbType((cur) => opts.find((o) => o.value === cur || normEngine(o.value) === normEngine(cur))?.value || opts[0].value)
      })
      .catch(() => {})
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

  useEffect(() => {
    if (collect.status === 'running') {
      wasRunning.current = true
      return
    }
    if (!wasRunning.current || collect.status !== 'done') return
    wasRunning.current = false
    const now = new Date()
    const hh = String(now.getHours()).padStart(2, '0')
    const mm = String(now.getMinutes()).padStart(2, '0')
    setSampledAt(`${hh}:${mm}`)
  }, [collect.status])

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

  const sorted = useMemo(() => {
    return [...rows].sort((a, b) => {
      const da = anomalyTags(a).filter((t) => t !== '暂不采集').length
      const db = anomalyTags(b).filter((t) => t !== '暂不采集').length
      if (da !== db) return db - da
      return (a.name || a.host).localeCompare(b.name || b.host)
    })
  }, [rows])

  const summary = useMemo(() => {
    let alert = 0
    let ok = 0
    let primary = 0
    let replica = 0
    const versions = new Set<string>()
    for (const row of rows) {
      const s = row.sample
      const tags = anomalyTags(row).filter((t) => t !== '暂不采集')
      if (tags.length) alert++
      if (s?.reachable) ok++
      if (s?.role === 'primary') primary++
      if (s?.role === 'replica') replica++
      if (s?.version) versions.add(s.version)
    }
    return { total: rows.length, alert, ok, primary, replica, versions: versions.size }
  }, [rows])

  const engine = normEngine(dbType)
  const showRedis = engine === 'redis'
  const showRuntime = engine === 'postgres' || engine === 'redis'
  const typeGroups = useMemo(
    () =>
      groupInstanceTypes(typeOptions).map((g) => ({
        label: g.title,
        options: g.items.map((t) => ({ value: t.value, label: t.label })),
      })),
    [typeOptions],
  )

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
      render: (_, row) => row.sample?.version || '—',
    },
    {
      title: '角色',
      width: 88,
      render: (_, row) => {
        const role = row.sample?.role
        if (role === 'primary') return <Tag className="ov-role ov-role-primary">主库</Tag>
        if (role === 'replica') return <Tag className="ov-role ov-role-replica">从库</Tag>
        return '—'
      },
    },
    {
      title: '状态',
      width: 180,
      render: (_, row) => {
        const tags = anomalyTags(row)
        if (!row.sample) return <span className="ov-status muted">未采集</span>
        if (!tags.length) return <span className="ov-status ok">正常</span>
        return (
          <Space size={4} wrap>
            {tags.map((t) => (
              <span key={t} className={`ov-status ${t === '暂不采集' ? 'muted' : !row.sample?.reachable ? 'danger' : 'warn'}`}>
                {t}
              </span>
            ))}
          </Space>
        )
      },
    },
  ]
  if (showRuntime) {
    columns.push(
      { title: '运行时间', width: 110, render: (_, row) => uptimeText(row.sample?.uptime_sec) },
      {
        title: '连接',
        width: 200,
        onCell: (row) => {
          const pct = usagePct(row.sample?.conn_used, row.sample?.conn_max)
          return { className: pct == null ? '' : `ov-cell ov-cell-${rateLevel(pct)}` }
        },
        render: (_, row) => (
          <RateCell
            pct={usagePct(row.sample?.conn_used, row.sample?.conn_max)}
            extra={row.sample ? ratio(row.sample.conn_used, row.sample.conn_max) : undefined}
          />
        ),
      },
    )
  }
  if (showRedis) {
    columns.push(
      { title: '内存大小GB', width: 110, render: (_, row) => memSizeGb(row.sample?.mem_max) },
      {
        title: '使用率',
        width: 170,
        onCell: (row) => {
          const pct = usagePct(row.sample?.mem_used, row.sample?.mem_max)
          return { className: pct == null ? '' : `ov-cell ov-cell-${rateLevel(pct)}` }
        },
        render: (_, row) => <RateCell pct={usagePct(row.sample?.mem_used, row.sample?.mem_max)} />,
      },
      {
        title: 'CPU',
        width: 160,
        onCell: (row) => {
          const pct = row.sample?.cpu_known ? row.sample.cpu_rate ?? 0 : null
          return { className: pct == null ? '' : `ov-cell ov-cell-${rateLevel(pct)}` }
        },
        render: (_, row) => <CpuSparkCell sample={row.sample} />,
      },
      {
        title: '命中率',
        width: 90,
        render: (_, row) => (row.sample?.hit_known ? `${Math.round((row.sample.hit_rate || 0) * 1000) / 10}%` : '—'),
      },
    )
  }
  if (engine === 'postgres') {
    columns.push(
      { title: '已用存储', width: 110, render: (_, row) => usedStorage(row.sample) },
      {
        title: '使用率',
        width: 170,
        onCell: (row) => {
          const pct = row.sample?.storage_rate_known ? row.sample.storage_rate ?? 0 : null
          return { className: pct == null ? '' : `ov-cell ov-cell-${rateLevel(pct)}` }
        },
        render: (_, row) => <RateCell pct={row.sample?.storage_rate_known ? row.sample.storage_rate ?? 0 : null} />,
      },
      { title: '大约总量', width: 110, render: (_, row) => approxTotal(row.sample) },
    )
  }
  if (showRuntime) {
    columns.push({ title: '从库延迟', width: 140, render: (_, row) => lagText(row.sample) })
  }
  columns.push(
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
    const header = [
      '实例名',
      '地址',
      '环境',
      '类型',
      '版本',
      '角色',
      '状态',
      '连接',
      '已用存储',
      '存储使用率',
      '大约总量',
      '内存大小GB',
      '使用率',
      'CPU',
      '从库延迟',
    ]
    const lines = [header.join(',')]
    for (const row of sorted) {
      const tags = anomalyTags(row)
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
          usedStorage(row.sample),
          storageRateText(row.sample),
          approxTotal(row.sample),
          memSizeGb(row.sample?.mem_max),
          (() => {
            const pct = usagePct(row.sample?.mem_used, row.sample?.mem_max)
            return pct == null ? '' : `${Math.round(pct)}%`
          })(),
          row.sample?.cpu_known && row.sample.cpu_rate != null ? `${Math.round(row.sample.cpu_rate)}%` : '',
          lagText(row.sample),
        ]
          .map((v) => csvCell(String(v)))
          .join(','),
      )
    }
    let latest = ''
    for (const row of sorted) {
      const t = row.sample?.collected_at || ''
      if (t > latest) latest = t
    }
    const stamp = latest ? latest.replace('T', ' ').slice(0, 19) : '未采集'
    lines.push('')
    lines.push(['采集时间', stamp].map((v) => csvCell(v)).join(','))
    const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'instance-overview.csv'
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const running = collect.status === 'running'
  const sampleDone = collect.done || 0
  const sampleTotal = collect.total || 0
  const samplePercent = sampleTotal ? Math.round((sampleDone / sampleTotal) * 100) : 0

  return (
    <div className="ops-page instance-overview">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">类型</span>
          <Select
            style={{ width: 180 }}
            popupMatchSelectWidth={false}
            value={dbType}
            onChange={setDbType}
            options={typeGroups}
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
          <Button type="primary" loading={running} disabled={running} onClick={() => void refresh()}>
            刷新
          </Button>
          {running ? (
            <span className="ov-collect">
              <Progress percent={samplePercent} size="small" showInfo={false} />
              <span>
                正在采样 {sampleDone}/{sampleTotal}
              </span>
            </span>
          ) : sampledAt ? (
            <span className="ov-collect-done">采样完成 {sampledAt}</span>
          ) : null}
          <Button onClick={exportCsv}>导出</Button>
        </Space>
      </div>
      <div className="instance-overview-cards">
        <div className="ov-card">
          <div className="label">实例总数</div>
          <div className="value">{summary.total}<span className="unit">个</span></div>
        </div>
        <div className="ov-card ok">
          <div className="label">运行正常</div>
          <div className="value">{summary.ok}</div>
        </div>
        <div className="ov-card warn">
          <div className="label">告警中</div>
          <div className="value">{summary.alert}</div>
        </div>
        <div className="ov-card">
          <div className="label">主库</div>
          <div className="value">{summary.primary}</div>
        </div>
        <div className="ov-card">
          <div className="label">从库</div>
          <div className="value">{summary.replica}</div>
        </div>
        <div className="ov-card">
          <div className="label">版本种类</div>
          <div className="value">{summary.versions}<span className="unit">种</span></div>
        </div>
      </div>
      <div className="instance-overview-note">
        {running
          ? '正在采样，汇总和表格会随结果更新，完成后才是本轮最终数字'
          : `数字是当前一次采样，不是监控图里的区间平均。超过约 5 分钟会变淡。低于 60% 为绿色，60% 起为黄色，80% 起为红色。${
              engine === 'postgres'
                ? ' 已用存储是各库文件合计。使用率是云监控最近值。大约总量 = 已用存储 / 使用率。'
                : engine === 'redis'
                  ? ' Redis CPU 为近 5 分钟 Prometheus 曲线（redis_exporter），旁注为末点占用。'
                  : ''
            }`}
      </div>
      <div className="instance-overview-table">
        <Table
          rowKey="id"
          size="small"
          loading={loading && !running}
          dataSource={sorted}
          columns={columns}
          scroll={{ x: 1560 }}
          pagination={{ defaultPageSize: 20, showSizeChanger: true, pageSizeOptions: ['20', '50', '100'], showTotal: (n) => `共 ${n} 条` }}
          rowClassName={(row) => (stale(row.sample) ? 'instance-overview-stale' : '')}
        />
      </div>
    </div>
  )
}
