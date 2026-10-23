import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Button, Empty, Spin, Tag } from 'antd'
import dayjs from 'dayjs'
import api from '../../api/request'

export interface ActivityInstance {
  id?: string
  name?: string
  db_type?: string
  host?: string
  port?: number
  version?: string
}

export interface ActivityLink {
  label: string
  path: string
}

export interface ActivityEvent {
  id: string
  kind: 'alert' | 'recover' | 'change' | 'inspect' | string
  sub?: string
  time: number
  severity?: string
  title: string
  metric?: string
  detail?: string
  actor?: string
  badge?: string
  instance?: ActivityInstance
  links?: ActivityLink[]
}

export interface ActivitySummary {
  total: number
  firing: number
}

const KIND_TEXT: Record<string, string> = { alert: '告警', recover: '恢复', change: '变更', inspect: '巡检' }
const KIND_COLOR: Record<string, string> = { alert: 'red', recover: 'green', change: 'blue', inspect: 'default' }

const DB_BADGE: Record<string, { text: string; bg: string }> = {
  postgres: { text: 'PG', bg: '#1677ff' },
  postgresql: { text: 'PG', bg: '#1677ff' },
  mysql: { text: 'MY', bg: '#fa8c16' },
  redis: { text: 'RD', bg: '#f5222d' },
  starrocks: { text: 'SR', bg: '#722ed1' },
  tidb: { text: 'TD', bg: '#13c2c2' },
  mongodb: { text: 'MG', bg: '#52c41a' },
  clickhouse: { text: 'CH', bg: '#faad14' },
  kafka: { text: 'KF', bg: '#595959' },
  oracle: { text: 'OR', bg: '#cf1322' },
  elasticsearch: { text: 'ES', bg: '#fadb14' },
}

function dotColor(ev: ActivityEvent): string {
  if (ev.kind === 'recover') return '#52c41a'
  if (ev.kind === 'alert') return ev.severity === 'P0' || ev.severity === 'P1' ? '#ff4d4f' : '#fa8c16'
  return '#91caff'
}

function relTime(sec: number): string {
  const diff = Math.max(0, dayjs().unix() - sec)
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

function dayKey(sec: number): string {
  return dayjs.unix(sec).format('YYYY-MM-DD')
}

function dayLabel(sec: number): string {
  const d = dayjs.unix(sec)
  const today = dayjs().startOf('day')
  if (d.isSame(today, 'day')) return `今天 · ${d.format('MM-DD')}`
  if (d.isSame(today.subtract(1, 'day'), 'day')) return `昨天 · ${d.format('MM-DD')}`
  return d.format('YYYY-MM-DD')
}

function mergeById(base: ActivityEvent[], extra: ActivityEvent[]): ActivityEvent[] {
  const seen = new Set(base.map((e) => e.id))
  const add = extra.filter((e) => !seen.has(e.id))
  return [...add, ...base].sort((a, b) => b.time - a.time || a.id.localeCompare(b.id))
}

export default function ActivityTimeline({
  since, until, kinds, severity, instanceId, compact, poll, relatedAt, refreshKey, onSummary,
}: {
  since?: number
  until?: number
  kinds?: string
  severity?: string
  instanceId?: string
  compact?: boolean
  poll?: boolean
  relatedAt?: number
  refreshKey?: number
  onSummary?: (s: ActivitySummary) => void
}) {
  const navigate = useNavigate()
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [pending, setPending] = useState<ActivityEvent[]>([])
  const [nextBefore, setNextBefore] = useState(0)
  const [loading, setLoading] = useState(false)
  const [errors, setErrors] = useState<string[]>([])
  const boxRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)
  const newestRef = useRef(0)
  const summaryRef = useRef(onSummary)
  summaryRef.current = onSummary

  const query = useCallback(async (extra: Record<string, number | undefined>) => {
    const { data } = await api.get('/alerts/activity', {
      params: {
        since: since || undefined,
        until: until || undefined,
        kinds: kinds || undefined,
        severity: severity || undefined,
        instance_id: instanceId || undefined,
        limit: compact ? 30 : 50,
        ...extra,
      },
    })
    return data as { data?: ActivityEvent[]; summary?: ActivitySummary; next_before?: number; errors?: string[] }
  }, [since, until, kinds, severity, instanceId, compact])

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const data = await query({})
      const list = data.data || []
      setEvents(list)
      setPending([])
      setNextBefore(data.next_before || 0)
      setErrors(data.errors || [])
      newestRef.current = list[0]?.time || 0
      if (data.summary) summaryRef.current?.(data.summary)
    } catch (e: any) {
      setErrors([e?.response?.data?.message || e?.message || '加载失败'])
    } finally {
      setLoading(false)
    }
  }, [query])

  useEffect(() => { void reload() }, [reload, refreshKey])

  useEffect(() => {
    if (!poll) return
    const timer = window.setInterval(() => {
      if (document.hidden || !newestRef.current) return
      query({ since: newestRef.current }).then((data) => {
        const fresh = (data.data || []).filter((e) => !events.some((x) => x.id === e.id) && e.time >= newestRef.current)
        if (!fresh.length) return
        newestRef.current = Math.max(newestRef.current, ...fresh.map((e) => e.time))
        if (stickRef.current) {
          setEvents((prev) => mergeById(prev, fresh))
        } else {
          setPending((prev) => mergeById(prev, fresh))
        }
      }).catch(() => undefined)
    }, 30000)
    return () => window.clearInterval(timer)
  }, [poll, query, events])

  async function loadOlder() {
    if (!nextBefore) return
    setLoading(true)
    try {
      const data = await query({ before: nextBefore })
      setEvents((prev) => mergeById(data.data || [], prev))
      setNextBefore(data.next_before || 0)
    } catch (e: any) {
      setErrors([e?.response?.data?.message || '加载失败'])
    } finally {
      setLoading(false)
    }
  }

  function showPending() {
    setEvents((prev) => mergeById(prev, pending))
    setPending([])
    stickRef.current = true
    boxRef.current?.scrollTo({ top: 0 })
  }

  let lastDay = ''
  return (
    <div>
      {errors.length > 0 && <Alert type="warning" showIcon style={{ marginBottom: 8 }} message={errors.join('；')} />}
      <div
        className="act-scroll"
        ref={boxRef}
        style={{ maxHeight: compact ? 420 : 'calc(100vh - 230px)' }}
        onScroll={(e) => { stickRef.current = e.currentTarget.scrollTop < 48 }}
      >
        {pending.length > 0 && <div className="act-new" onClick={showPending}>有 {pending.length} 条新动态</div>}
        {loading && events.length === 0 && <div style={{ textAlign: 'center', padding: 24 }}><Spin /></div>}
        {!loading && events.length === 0 && <Empty description="这个时间范围内没有动态" />}
        {events.map((ev) => {
          const day = dayKey(ev.time)
          const showDay = day !== lastDay
          lastDay = day
          const related = !!relatedAt && ev.kind === 'change' && ev.time >= relatedAt - 1800 && ev.time <= relatedAt
          const db = DB_BADGE[(ev.instance?.db_type || '').toLowerCase()]
          const addr = ev.instance?.host ? `${ev.instance.host}${ev.instance.port ? ':' + ev.instance.port : ''}` : ''
          return (
            <React.Fragment key={ev.id}>
              {showDay && <div className="act-day">{dayLabel(ev.time)}</div>}
              <div className="act-row">
                <div className="act-time">
                  <div className="hm">{dayjs.unix(ev.time).format('HH:mm')}</div>
                  <div className="rel">{relTime(ev.time)}</div>
                </div>
                <div className="act-rail"><div className="act-dot" style={{ background: dotColor(ev) }} /></div>
                <div className={`act-card${related ? ' related' : ''}`}>
                  <div className="act-card-title">
                    <Tag color={KIND_COLOR[ev.kind] || 'default'}>{KIND_TEXT[ev.kind] || ev.kind}</Tag>
                    <span>{ev.title}</span>
                    {ev.metric && <span className="metric">{ev.metric}</span>}
                    {ev.badge && <Tag>{ev.badge}</Tag>}
                    {related && <Tag color="orange">可能相关</Tag>}
                  </div>
                  {ev.detail && <div className="detail">{ev.detail}</div>}
                  <div className="act-card-foot">
                    <div className="act-inst">
                      {ev.instance?.name || ev.instance?.id ? (
                        <>
                          <span className="act-badge" style={{ background: db?.bg || '#8c8c8c' }}>{db?.text || 'DB'}</span>
                          <span>{ev.instance.name || ev.instance.id}</span>
                          {addr && <span>{addr}</span>}
                          {ev.instance.version && <span>v{ev.instance.version}</span>}
                        </>
                      ) : <span>{ev.actor ? `操作人 ${ev.actor}` : '未关联实例'}</span>}
                    </div>
                    <span>
                      {(ev.links || []).map((l) => (
                        <Button key={l.label} type="link" size="small" onClick={() => navigate(l.path)}>{l.label}</Button>
                      ))}
                    </span>
                  </div>
                </div>
              </div>
            </React.Fragment>
          )
        })}
        {nextBefore > 0 && (
          <div className="act-more"><Button loading={loading} onClick={() => void loadOlder()}>加载更早</Button></div>
        )}
      </div>
    </div>
  )
}
