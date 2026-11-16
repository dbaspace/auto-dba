import { Alert, Table, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'

type Storage = {
  size_bytes: number
  used_bytes: number
  rate: number
  has_prev: boolean
  growth_bytes?: number
  daily_bytes?: number
  days_to_full?: number
  level: 'ok' | 'warn' | 'danger' | 'muted'
  advice: string
  capacity_known?: boolean
  rate_current?: boolean
}

type Metric = {
  known: boolean
  reason?: string
  avg: number
  max: number
  level: 'ok' | 'warn' | 'danger' | 'muted'
  advice: string
}

export type PGTrendItem = {
  id: string
  name: string
  app?: string
  domain?: string
  host: string
  env_name: string
  storage?: Storage
  qps?: Metric
  tps?: Metric
  locks?: Metric
  deadlocks?: Metric
  cpu?: Metric
  slots?: { known: boolean; active: number; total: number; max?: number; level: string; advice: string; reason?: string }
  slot_wal?: Metric
  level: 'ok' | 'warn' | 'danger' | 'muted'
  advice: string
}

export type PGTrendResp = {
  days: number
  items: PGTrendItem[]
  summary: { instances?: number; hosts?: number; danger?: number; conclusions: string[] }
  note?: string
  generated_at?: string
}

function sizeText(n?: number) {
  if (n == null || Number.isNaN(n)) return '—'
  const abs = Math.abs(n)
  const sign = n < 0 ? '-' : ''
  if (abs >= 1024 ** 4) return `${sign}${(abs / 1024 ** 4).toFixed(2)} TB`
  if (abs >= 1024 ** 3) return `${sign}${(abs / 1024 ** 3).toFixed(1)} GB`
  if (abs >= 1024 ** 2) return `${sign}${(abs / 1024 ** 2).toFixed(0)} MB`
  return `${sign}${Math.round(abs)} B`
}

function storageTimeout(d?: Storage) {
  return d?.advice?.includes('查询超时') ? <span className="ov-muted">{d.advice}</span> : null
}

function rateText(d?: Storage) {
  const timeout = storageTimeout(d)
  if (d?.rate_current) {
    const cls = d.rate >= 80 ? 'danger' : d.rate >= 60 ? 'warn' : 'ok'
    const text = `${d.rate.toFixed(1)}%`
    return (
      <Tooltip title="当前磁盘使用率（腾讯云）">
        <span className={`ov-trend-${cls}`}>{text}</span>
      </Tooltip>
    )
  }
  if (!d || (!d.capacity_known && !d.used_bytes && d.level === 'muted')) return timeout || <span className="ov-muted">无样本</span>
  if (!d.capacity_known) {
    return (
      <Tooltip title={`已用 ${sizeText(d.used_bytes)}，没有购买容量`}>
        <span className="ov-muted">—</span>
      </Tooltip>
    )
  }
  const cls = d.rate >= 80 ? 'danger' : d.rate >= 60 ? 'warn' : 'ok'
  return (
    <Tooltip title={`已用 ${sizeText(d.used_bytes)} / ${sizeText(d.size_bytes)}`}>
      <span className={`ov-trend-${cls}`}>{`${Math.round(d.rate)}%`}</span>
    </Tooltip>
  )
}

function growthText(d?: Storage) {
  const timeout = storageTimeout(d)
  if (!d || (d.level === 'muted' && !d.has_prev && !d.used_bytes)) return timeout || <span className="ov-muted">无样本</span>
  if (!d.has_prev) return timeout || <span className="ov-muted">无对照</span>
  const g = d.growth_bytes || 0
  return `${g > 0 ? '+' : ''}${sizeText(g)}`
}

function dailyText(d?: Storage) {
  if (!d?.has_prev) return '—'
  const v = d.daily_bytes || 0
  return `${v > 0 ? '+' : ''}${sizeText(v)}/天`
}

function fullText(d?: Storage) {
  if (!d) return '—'
  if (!d.capacity_known) return <span className="ov-muted">{d.has_prev ? '无容量' : '—'}</span>
  if (d.days_to_full == null) return <span className="ov-muted">{d.has_prev ? '不增长' : '—'}</span>
  const days = d.days_to_full
  const text = days < 1 ? '不足 1 天' : `约 ${Math.round(days)} 天`
  const cls = days <= 14 ? 'danger' : days <= 30 ? 'warn' : 'ok'
  return <span className={`ov-trend-${cls}`}>{text}</span>
}

function numText(v: number) {
  const abs = Math.abs(v)
  if (abs >= 100) return Math.round(v).toLocaleString()
  if (abs >= 10) return v.toFixed(1)
  if (abs >= 0.01) return v.toFixed(2)
  if (abs === 0) return '0'
  return v.toFixed(4)
}

function slotText(s?: PGTrendItem['slots']) {
  if (!s?.known) return <span className="ov-muted">{s?.reason || '没有样本'}</span>
  const active = Math.round(s.active)
  const total = Math.round(s.total)
  const text = s.max == null ? `${active}/${total}` : `${active}/${total}/${Math.round(s.max)}`
  const cls = s.level === 'warn' || s.level === 'danger' ? s.level : 'ok'
  return <span className={`ov-trend-${cls}`}>{text}</span>
}

function walText(m?: Metric) {
  if (!m?.known) return <span className="ov-muted">{m?.reason || '没有样本'}</span>
  return sizeText(m.max)
}

function metricText(m: Metric | undefined, kind: 'rate' | 'count' | 'cpu', which: 'avg' | 'max') {
  if (!m?.known) return <span className="ov-muted">{m?.reason || '没有样本'}</span>
  const v = which === 'avg' ? m.avg : m.max
  if (kind === 'cpu') {
    const cls = which === 'max' && (m.level === 'warn' || m.level === 'danger') ? m.level : 'ok'
    return <span className={`ov-trend-${cls}`}>{`${Math.round(v * 100)}%`}</span>
  }
  const text = kind === 'count' ? Math.round(v).toLocaleString() : `${numText(v)}/s`
  if (which === 'max' && (m.level === 'warn' || m.level === 'danger')) {
    return <span className={`ov-trend-${m.level}`}>{text}</span>
  }
  return text
}

export default function PgStorageReportBody({ resp, scrollY, showNote = true }: { resp: PGTrendResp; scrollY?: number; showNote?: boolean }) {
  const days = resp.days || 7
  const columns: ColumnsType<PGTrendItem> = [
    { title: '实例名', dataIndex: 'name', width: 140, fixed: 'left', ellipsis: true },
    { title: '地址', dataIndex: 'host', width: 130, ellipsis: true },
    { title: '环境', dataIndex: 'env_name', width: 90, ellipsis: true },
    {
      title: '库文件',
      children: [
        { title: '使用率', width: 80, render: (_: unknown, row: PGTrendItem) => rateText(row.storage) },
        { title: `${days} 天增长`, width: 110, render: (_: unknown, row: PGTrendItem) => growthText(row.storage) },
        { title: '日均', width: 110, render: (_: unknown, row: PGTrendItem) => dailyText(row.storage) },
        { title: '预计写满', width: 100, render: (_: unknown, row: PGTrendItem) => fullText(row.storage) },
      ],
    },
    {
      title: 'QPS',
      children: [
        { title: '平均', width: 90, render: (_: unknown, row: PGTrendItem) => metricText(row.qps, 'rate', 'avg') },
        { title: '峰值', width: 90, render: (_: unknown, row: PGTrendItem) => metricText(row.qps, 'rate', 'max') },
      ],
    },
    {
      title: 'TPS',
      children: [
        { title: '平均', width: 90, render: (_: unknown, row: PGTrendItem) => metricText(row.tps, 'rate', 'avg') },
        { title: '峰值', width: 90, render: (_: unknown, row: PGTrendItem) => metricText(row.tps, 'rate', 'max') },
      ],
    },
    { title: '锁峰值', width: 90, render: (_: unknown, row: PGTrendItem) => metricText(row.locks, 'count', 'max') },
    { title: '死锁峰值', width: 100, render: (_: unknown, row: PGTrendItem) => metricText(row.deadlocks, 'rate', 'max') },
    { title: '槽', width: 120, render: (_: unknown, row: PGTrendItem) => slotText(row.slots) },
    { title: '槽最大日志', width: 120, render: (_: unknown, row: PGTrendItem) => walText(row.slot_wal) },
    {
      title: 'CPU',
      children: [
        { title: '平均', width: 80, render: (_: unknown, row: PGTrendItem) => metricText(row.cpu, 'cpu', 'avg') },
        { title: '峰值', width: 80, render: (_: unknown, row: PGTrendItem) => metricText(row.cpu, 'cpu', 'max') },
      ],
    },
    {
      title: '建议',
      width: 280,
      render: (_, row) => <span className={`ov-trend-${row.level}`}>{row.advice}</span>,
    },
  ]
  const n = resp.summary?.instances ?? resp.summary?.hosts ?? resp.items?.length ?? 0
  return (
    <>
      {showNote ? (
        <div className="ov-trend-note">
          {resp.note || '使用率是当前腾讯云磁盘使用率。库文件不含 WAL，写满天数按购买磁盘估算。QPS、TPS 按 Grafana 的 hostname 精确汇总。QPS、TPS、CPU 的峰值都是 1 分钟分辨率上的最大值。CPU 来自云监控。'}
        </div>
      ) : null}
      <Alert
        type={resp.summary?.danger ? 'warning' : 'info'}
        showIcon
        style={{ marginBottom: 12 }}
        message={`共分析 ${n} 台 PostgreSQL`}
        description={
          <ul className="ov-trend-conclusions">
            {(resp.summary?.conclusions || []).map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        }
      />
      <Table
        rowKey="id"
        size="small"
        bordered
        dataSource={resp.items || []}
        columns={columns}
        scroll={{ x: 2100, y: scrollY }}
        pagination={{ defaultPageSize: 20, showSizeChanger: { getPopupContainer: () => document.body }, showTotal: (total) => `共 ${total} 台` }}
      />
    </>
  )
}
