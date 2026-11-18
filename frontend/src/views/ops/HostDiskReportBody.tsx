import { Alert, Table, Tooltip } from 'antd'
import type { ColumnsType } from 'antd/es/table'

export type DiskTrend = {
  size_bytes: number
  used_bytes: number
  rate: number
  has_prev: boolean
  prev_used_bytes?: number
  growth_bytes?: number
  daily_bytes?: number
  days_to_full?: number
  new_devices?: number
  level: 'ok' | 'warn' | 'danger' | 'muted'
  advice: string
}

export type HostPressure = {
  cpu_avg?: number
  cpu_peak?: number
  cpu_peak_at?: string
  io_avg?: number
  io_peak?: number
  io_peak_at?: string
  net_in_avg?: number
  net_in_peak?: number
  net_in_peak_at?: string
  net_out_avg?: number
  net_out_peak?: number
  conn_avg?: number
  conn_peak?: number
}

export type TrendItem = {
  id: string
  name: string
  app?: string
  domain?: string
  host: string
  env_name: string
  data?: DiskTrend
  system?: DiskTrend
  pressure?: HostPressure
}

export type TrendResp = {
  days: number
  items: TrendItem[]
  summary: { hosts: number; conclusions: string[] }
  generated_at?: string
  report_id?: string
  save_error?: string
}

export type ReportScope = {
  domain?: string
  host?: string
  env_name?: string
  env?: string
  host_count?: number
  days?: number
  start?: string
  end?: string
}

export function parseReportScope(scope?: string | ReportScope): ReportScope {
  if (!scope) return {}
  if (typeof scope !== 'string') return scope
  try {
    return JSON.parse(scope) as ReportScope
  } catch {
    return {}
  }
}

function localClock(raw: string) {
  const d = new Date(raw)
  if (!raw || Number.isNaN(d.getTime())) return (raw || '').replace('T', ' ').slice(0, 16)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function reportLabel(createdAt: string, scope?: string | ReportScope) {
  const parsed = parseReportScope(scope)
  const time = localClock(createdAt)
  const domain = parsed.domain || '全部域名'
  const host = parsed.host || '全部地址'
  const env = parsed.env_name || (parsed.env === 'unset' ? '未设置环境' : parsed.env) || '全部环境'
  const n = parsed.host_count ?? 0
  return `${time} · ${domain} / ${host} / ${env} · ${n} 台`
}

function sizeText(n?: number) {
  if (n == null) return '—'
  const abs = Math.abs(n)
  const sign = n < 0 ? '-' : ''
  if (abs >= 1024 ** 4) return `${sign}${(abs / 1024 ** 4).toFixed(2)} TB`
  if (abs >= 1024 ** 3) return `${sign}${(abs / 1024 ** 3).toFixed(1)} GB`
  return `${sign}${(abs / 1024 ** 2).toFixed(0)} MB`
}

function growthText(d?: DiskTrend) {
  if (!d) return '—'
  if (!d.has_prev) return <span className="ov-muted">无对照</span>
  const g = d.growth_bytes || 0
  return `${g > 0 ? '+' : ''}${sizeText(g)}`
}

function dailyText(d?: DiskTrend) {
  if (!d?.has_prev) return '—'
  const v = d.daily_bytes || 0
  return `${v > 0 ? '+' : ''}${sizeText(v)}/天`
}

function fullText(d?: DiskTrend) {
  if (!d) return '—'
  if (d.days_to_full == null) return <span className="ov-muted">{d.has_prev ? '不增长' : '—'}</span>
  const days = d.days_to_full
  const text = days < 1 ? '不足 1 天' : `约 ${Math.round(days)} 天`
  const cls = days <= 14 ? 'danger' : days <= 30 ? 'warn' : 'ok'
  return <span className={`ov-trend-${cls}`}>{text}</span>
}

function numText(n: number | undefined, fmt: (n: number) => string) {
  if (n == null || Number.isNaN(n)) return '—'
  return fmt(n)
}

function pctText(n?: number) {
  return numText(n, (v) => `${Math.round(v)}%`)
}

function connText(n?: number) {
  return numText(n, (v) => String(Math.round(v)))
}

function peakClock(raw?: string) {
  if (!raw) return ''
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function peakCell(text: string, at?: string) {
  const tip = peakClock(at)
  if (!tip || text === '—') return text
  return (
    <Tooltip title={tip}>
      <span>{text}</span>
    </Tooltip>
  )
}

function byteRateText(n?: number) {
  return numText(n, (v) => {
    const abs = Math.abs(v)
    const sign = v < 0 ? '-' : ''
    if (abs >= 1024 ** 3) return `${sign}${(abs / 1024 ** 3).toFixed(1)} GB/s`
    if (abs >= 1024 ** 2) return `${sign}${(abs / 1024 ** 2).toFixed(1)} MB/s`
    if (abs >= 1024) return `${sign}${(abs / 1024).toFixed(1)} KB/s`
    return `${sign}${abs.toFixed(0)} B/s`
  })
}

function rateText(d?: DiskTrend) {
  if (!d) return <span className="ov-muted">无样本</span>
  const cls = d.rate >= 80 ? 'danger' : d.rate >= 60 ? 'warn' : 'ok'
  return (
    <Tooltip title={`已用 ${sizeText(d.used_bytes)} / ${sizeText(d.size_bytes)}`}>
      <span className={`ov-trend-${cls}`}>{`${Math.round(d.rate)}%`}</span>
    </Tooltip>
  )
}

const LEVEL_ORDER = { danger: 0, warn: 1, ok: 2, muted: 3 } as const

function adviceCell(row: TrendItem) {
  const parts = [
    row.data ? { label: '数据盘', d: row.data } : null,
    row.system ? { label: '系统盘', d: row.system } : null,
  ].filter(Boolean) as { label: string; d: DiskTrend }[]
  if (!parts.length && !row.pressure) return <span className="ov-muted">没有 node_exporter 样本</span>
  parts.sort((a, b) => LEVEL_ORDER[a.d.level] - LEVEL_ORDER[b.d.level])
  const extra: string[] = []
  const cpu = row.pressure?.cpu_peak
  const io = row.pressure?.io_peak
  if (cpu != null && cpu >= 85) extra.push(`CPU 峰值 ${Math.round(cpu)}%，不低于 85%`)
  if (io != null && io >= 80) extra.push(`IO util 峰值 ${Math.round(io)}%，不低于 80%`)
  return (
    <div className="ov-trend-advice">
      {parts.map((p) => (
        <div key={p.label} className={`ov-trend-${p.d.level}`}>
          {p.label}：{p.d.advice}
          {p.d.new_devices ? `（${p.d.new_devices} 块盘为新挂载，未计入增长）` : ''}
        </div>
      ))}
      {extra.map((line) => (
        <div key={line} className="ov-trend-warn">{line}</div>
      ))}
    </div>
  )
}

export default function HostDiskReportBody({ resp, scrollY, showNote = true }: { resp: TrendResp; scrollY?: number; showNote?: boolean }) {
  const days = resp.days || 7
  const diskCols = (key: 'data' | 'system', title: string) => ({
    title,
    children: [
      { title: '使用率', width: 80, render: (_: unknown, row: TrendItem) => rateText(row[key]) },
      { title: `${days} 天增长`, width: 110, render: (_: unknown, row: TrendItem) => growthText(row[key]) },
      { title: '日均', width: 110, render: (_: unknown, row: TrendItem) => dailyText(row[key]) },
      { title: '预计写满', width: 100, render: (_: unknown, row: TrendItem) => fullText(row[key]) },
    ],
  })
  const columns: ColumnsType<TrendItem> = [
    { title: '实例名', dataIndex: 'name', width: 140, fixed: 'left', ellipsis: true },
    { title: '应用', dataIndex: 'app', width: 120, ellipsis: true, render: (v?: string) => v || '—' },
    { title: '环境', dataIndex: 'env_name', width: 90, ellipsis: true },
    diskCols('data', '数据盘'),
    diskCols('system', '系统盘（/）'),
    {
      title: 'CPU',
      children: [
        { title: '均值', width: 72, render: (_: unknown, row: TrendItem) => pctText(row.pressure?.cpu_avg) },
        { title: '峰值', width: 72, render: (_: unknown, row: TrendItem) => peakCell(pctText(row.pressure?.cpu_peak), row.pressure?.cpu_peak_at) },
      ],
    },
    {
      title: 'IO util',
      children: [
        { title: '均值', width: 72, render: (_: unknown, row: TrendItem) => pctText(row.pressure?.io_avg) },
        { title: '峰值', width: 72, render: (_: unknown, row: TrendItem) => peakCell(pctText(row.pressure?.io_peak), row.pressure?.io_peak_at) },
      ],
    },
    {
      title: '网络',
      children: [
        { title: '入站均值', width: 100, render: (_: unknown, row: TrendItem) => byteRateText(row.pressure?.net_in_avg) },
        { title: '入站峰值', width: 100, render: (_: unknown, row: TrendItem) => peakCell(byteRateText(row.pressure?.net_in_peak), row.pressure?.net_in_peak_at) },
        { title: '出站均值', width: 100, render: (_: unknown, row: TrendItem) => byteRateText(row.pressure?.net_out_avg) },
        { title: '出站峰值', width: 100, render: (_: unknown, row: TrendItem) => byteRateText(row.pressure?.net_out_peak) },
      ],
    },
    {
      title: '连接数',
      children: [
        { title: '均值', width: 72, render: (_: unknown, row: TrendItem) => connText(row.pressure?.conn_avg) },
        { title: '峰值', width: 72, render: (_: unknown, row: TrendItem) => connText(row.pressure?.conn_peak) },
      ],
    },
    { title: '建议', width: 320, render: (_, row) => adviceCell(row) },
  ]
  const danger = (resp.items || []).some((it) => it.data?.level === 'danger' || it.system?.level === 'danger')
  return (
    <>
      {showNote ? (
        <div className="ov-trend-note">
          对比 Prometheus 中 node_exporter 的当前用量和 {days} 天前用量。只有两次都采到的盘才计入增长，新挂载的盘不算成增长。预计写满天数 = 剩余容量 ÷ 日均增长。
        </div>
      ) : null}
      <Alert
        type={danger ? 'warning' : 'info'}
        showIcon
        style={{ marginBottom: 12 }}
        message={`共分析 ${resp.summary?.hosts ?? 0} 台主机`}
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
        scroll={{ x: 2360, y: scrollY }}
        pagination={{ defaultPageSize: 20, showSizeChanger: { getPopupContainer: () => document.body }, showTotal: (n) => `共 ${n} 台` }}
      />
    </>
  )
}
