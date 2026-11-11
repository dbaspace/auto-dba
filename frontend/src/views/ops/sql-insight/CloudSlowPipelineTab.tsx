import { useEffect, useState, type ReactNode } from 'react'
import { Button, Input, Pagination, Tag } from 'antd'
import { ArrowDownOutlined, ArrowRightOutlined, CloudServerOutlined, DatabaseOutlined, SearchOutlined, SendOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import api from '../../../api/request'

type PipelineItem = {
  instance_id: string
  name: string
  host: string
  port: number
  cloud_instance_id: string
  status: 'ok' | 'lag' | 'failed' | 'none'
  status_text: string
  collected: number
  collected_at: string
  kafka: boolean
  produced: number
  produced_at: string
  ingested: number
  ingested_at: string
  cursor_at: string
  delay_sec: number
  lag: number
  error: string
}

type Pipeline = {
  instances: number
  ok: number
  lag: number
  failed: number
  records: number
  items: PipelineItem[]
}

type Tone = 'green' | 'gray' | 'red' | 'orange'

const toneColor: Record<Tone, { fg: string; bg: string }> = {
  green: { fg: '#16a34a', bg: '#e8f7ee' },
  gray: { fg: '#98a2b3', bg: '#f2f4f7' },
  red: { fg: '#f04438', bg: '#fdeceb' },
  orange: { fg: '#f79009', bg: '#fff4e5' },
}

function beijingText(raw?: string) {
  if (!raw) return '—'
  const t = dayjs(raw)
  return t.isValid() ? t.format('YYYY-MM-DD HH:mm:ss') : raw
}

function delayText(sec: number) {
  if (!sec) return '—'
  if (sec < 60) return `${sec} 秒`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min} 分钟`
  return `${Math.floor(min / 60)} 小时 ${min % 60} 分钟`
}

function useNarrow(max = 900) {
  const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && window.matchMedia(`(max-width: ${max}px)`).matches)
  useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${max}px)`)
    const onChange = () => setNarrow(mq.matches)
    onChange()
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [max])
  return narrow
}

const statusColor: Record<PipelineItem['status'], string> = {
  ok: 'success',
  lag: 'warning',
  failed: 'error',
  none: 'default',
}

function stepTone(it: PipelineItem, at: string, short: boolean): Tone {
  if (it.status === 'failed') return 'red'
  if (short) return 'orange'
  if (!at) return 'gray'
  return 'green'
}

function FlowStep({ icon, title, value, extra, tone }: { icon: ReactNode; title: string; value: string; extra: string; tone: Tone }) {
  const color = toneColor[tone]
  return (
    <div style={{ flex: '0 1 220px', minWidth: 0, display: 'flex', alignItems: 'center', gap: 8, background: '#f7f8fa', borderRadius: 8, padding: '6px 10px' }}>
      <span style={{ width: 26, height: 26, borderRadius: '50%', background: color.bg, color: color.fg, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', fontSize: 14, flex: 'none' }}>
        {icon}
      </span>
      <div style={{ minWidth: 0 }}>
        <div style={{ color: '#667085', fontSize: 12, lineHeight: '16px' }}>{title}</div>
        <div style={{ fontSize: 14, fontWeight: 600, lineHeight: '18px' }}>{value}</div>
        <div style={{ color: '#667085', fontSize: 12, lineHeight: '16px' }}>{extra}</div>
      </div>
    </div>
  )
}

function FlowArrow({ dashed, color, label, down }: { dashed: boolean; color: string; label?: string; down: boolean }) {
  const Icon = down ? ArrowDownOutlined : ArrowRightOutlined
  const line = dashed ? 'dashed' : 'solid'
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 2, color, flex: 'none', fontSize: 12 }}>
      <div style={{ display: 'flex', flexDirection: down ? 'column' : 'row', alignItems: 'center' }}>
        <div style={down
          ? { height: 14, borderLeft: `1px ${line} ${color}` }
          : { width: 22, borderTop: `1px ${line} ${color}` }}
        />
        <Icon />
      </div>
      {label ? <span style={{ fontSize: 12, lineHeight: '16px', textAlign: 'center', maxWidth: 148, color: '#667085' }}>{label}</span> : null}
    </div>
  )
}

function arrowProps(it: PipelineItem, from: number, to: number, fromAt: string, toAt: string) {
  if (it.status === 'none') return { dashed: true, color: toneColor.gray.fg }
  const drop = to < from
  if (drop) return { dashed: false, color: toneColor.orange.fg }
  if (!it.kafka) return { dashed: true, color: toneColor.gray.fg }
  if (fromAt && toAt) return { dashed: false, color: '#1677ff' }
  return { dashed: true, color: toneColor.gray.fg }
}

function InstanceFlow({ it, narrow }: { it: PipelineItem; narrow: boolean }) {
  const kafkaTone: Tone = it.status === 'failed' ? 'red' : !it.kafka || !it.produced_at ? 'gray' : 'green'
  const cloud = arrowProps(it, it.collected, it.produced, it.collected_at, it.produced_at)
  const sink = arrowProps(it, it.produced, it.ingested, it.produced_at, it.ingested_at)
  const cloudLabel = `游标 ${beijingText(it.cursor_at)}`
  const delayLabel = !it.ingested_at && it.ingested > 0 ? '已追上' : delayText(it.delay_sec)
  const sinkLabel = `延迟 ${delayLabel} · 完整性 ${it.ingested}/${it.collected} · 落后 ${it.lag} 条${it.error ? ` · ${it.error}` : ''}`
  return (
    <div style={{ display: 'flex', flexDirection: narrow ? 'column' : 'row', alignItems: narrow ? 'stretch' : 'center', gap: narrow ? 6 : 8 }}>
      <FlowStep
        icon={<CloudServerOutlined />}
        title="云平台采集"
        value={`${it.collected} 条`}
        extra={beijingText(it.collected_at)}
        tone={stepTone(it, it.collected_at, false)}
      />
      <FlowArrow dashed={cloud.dashed} color={cloud.color} label={cloudLabel} down={narrow} />
      <FlowStep
        icon={<SendOutlined />}
        title="Kafka 发送"
        value={it.kafka ? `${it.produced} 条` : '直写元库'}
        extra={beijingText(it.produced_at)}
        tone={kafkaTone}
      />
      <FlowArrow dashed={sink.dashed} color={sink.color} label={sinkLabel} down={narrow} />
      <FlowStep
        icon={<DatabaseOutlined />}
        title="元库入库"
        value={`${it.ingested} 条`}
        extra={beijingText(it.ingested_at)}
        tone={stepTone(it, it.ingested_at, it.ingested < it.collected)}
      />
    </div>
  )
}

export default function CloudSlowPipelineTab({ active }: { active: boolean }) {
  const [data, setData] = useState<Pipeline | null>(null)
  const [note, setNote] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<'all' | PipelineItem['status']>('all')
  const narrow = useNarrow()

  async function load() {
    const { data: body } = await api.get('/cloud/slow-logs/pipeline', { silent: true })
    setData(body.data || null)
    setNote('')
  }

  useEffect(() => {
    if (active) {
      void load().catch((e) => setNote(e?.response?.data?.message || '加载失败'))
    }
  }, [active])

  const summary = data || { instances: 0, ok: 0, lag: 0, failed: 0, records: 0, items: [] }
  const q = keyword.trim().toLowerCase()
  const filtered = summary.items.filter((it) => {
    if (statusFilter !== 'all' && it.status !== statusFilter) return false
    if (!q) return true
    return `${it.name} ${it.host} ${it.port} ${it.status_text}`.toLowerCase().includes(q)
  })
  const maxPage = Math.max(1, Math.ceil(filtered.length / pageSize) || 1)
  const current = Math.min(page, maxPage)
  const pageItems = filtered.slice((current - 1) * pageSize, current * pageSize)

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="搜索实例名称、主机或状态"
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value)
            setPage(1)
          }}
          style={{ width: 280 }}
        />
        <Button onClick={() => void load().catch((e) => setNote(e?.response?.data?.message || '加载失败'))}>刷新</Button>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {([
            ['all', '全部', summary.instances],
            ['ok', '正常', summary.ok],
            ['lag', '告警', summary.lag],
            ['failed', '失败', summary.failed],
          ] as const).map(([key, label, n]) => {
            const on = statusFilter === key
            return (
              <button
                key={key}
                type="button"
                onClick={() => {
                  setStatusFilter(key)
                  setPage(1)
                }}
                style={{
                  border: on ? '1px solid #1677ff' : '1px solid #d0d5dd',
                  background: on ? '#1677ff' : '#fff',
                  color: on ? '#fff' : '#344054',
                  borderRadius: 16,
                  padding: '4px 12px',
                  cursor: 'pointer',
                }}
              >
                {label} {n}
              </button>
            )
          })}
          <button
            type="button"
            style={{ border: '1px solid #d0d5dd', background: '#fff', color: '#344054', borderRadius: 16, padding: '4px 12px', cursor: 'default' }}
          >
            记录 {summary.records}
          </button>
        </div>
      </div>
      {note && <p>{note}</p>}
      {!note && summary.items.length === 0 && <p>当前没有已关联云实例的 PostgreSQL。</p>}
      {!note && summary.items.length > 0 && filtered.length === 0 && <p>没有匹配的实例。</p>}
      {pageItems.map((it) => (
        <div key={it.instance_id} style={{ border: '1px solid #e5e7eb', borderRadius: 10, padding: '10px 12px', marginBottom: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
            <strong>{it.name}</strong>
            <span style={{ color: '#667085' }}>{it.host}{it.port ? `:${it.port}` : ''}</span>
            <Tag color={statusColor[it.status] || 'default'}>{it.status_text}</Tag>
          </div>
          <InstanceFlow it={it} narrow={narrow} />
        </div>
      ))}
      {filtered.length > 0 && (
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 4 }}>
          <Pagination
            current={current}
            pageSize={pageSize}
            total={filtered.length}
            pageSizeOptions={['10', '20', '30', '50', '100']}
            showSizeChanger
            showTotal={(n) => `共 ${n} 条`}
            onChange={(next, size) => {
              setPage(next)
              setPageSize(size)
            }}
          />
        </div>
      )}
    </div>
  )
}
