import React, { useEffect, useMemo, useState } from 'react'
import { Button, Input, Space, Table, Tabs, message } from 'antd'
import { Link } from 'react-router-dom'
import type { EChartsCoreOption } from 'echarts'
import api from '../../../api/request'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'
import ChartBox from '../../dashboard/ChartBox'
import type { SnapshotFilter } from './types'

const spreadColors = ['#f6d36b', '#f5b942', '#f08c2e', '#e85d4c', '#d4380d', '#a8071a']

function beijingText(raw?: string) {
  const text = String(raw || '').trim()
  if (!text) return '—'
  const t = Date.parse(text)
  if (!Number.isFinite(t)) return text
  const parts = Object.fromEntries(new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(new Date(t)).map((p) => [p.type, p.value]))
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}

function bucketParts(bucket: string) {
  const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(bucket)
  if (!m) return null
  return { date: `${m[1]}-${m[2]}-${m[3]}`, short: `${m[4]}:${m[5]}`, day: `${m[2]}-${m[3]} ${m[4]}:${m[5]}` }
}

function trendOption(trend: { bucket?: string; count?: number }[]): EChartsCoreOption {
  const rows = trend.map((p) => ({ bucket: String(p.bucket || ''), count: Number(p.count) || 0 }))
  const first = bucketParts(rows[0]?.bucket || '')
  const last = bucketParts(rows[rows.length - 1]?.bucket || '')
  const crossDay = Boolean(first && last && first.date !== last.date)
  const labels = rows.map((p) => {
    const part = bucketParts(p.bucket)
    if (!part) return p.bucket
    return crossDay ? part.day : part.short
  })
  const step = Math.max(0, Math.ceil(labels.length / 6) - 1)
  return {
    grid: { left: 40, right: 12, top: 16, bottom: 28 },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = Array.isArray(params) ? params[0] : params
        const row = rows[item?.dataIndex] || { bucket: item?.name, count: item?.value }
        return `${row.bucket}<br/>条数 ${row.count}`
      },
    },
    xAxis: {
      type: 'category',
      data: labels,
      axisTick: { show: false },
      axisLabel: { fontSize: 11, interval: step, hideOverlap: true },
    },
    yAxis: { type: 'value', minInterval: 1, axisLabel: { fontSize: 11 } },
    series: [{
      type: 'bar',
      data: rows.map((p) => p.count),
      barMaxWidth: 8,
      itemStyle: { color: '#5b8ff9' },
    }],
  }
}

function spreadOption(spread: { label?: string; count?: number }[]): EChartsCoreOption {
  const rows = spread.map((s) => ({ label: String(s.label || ''), count: Number(s.count) || 0 }))
  return {
    grid: { left: 72, right: 36, top: 8, bottom: 8 },
    tooltip: { trigger: 'axis', formatter: (params: any) => {
      const item = Array.isArray(params) ? params[0] : params
      return `${item?.name}<br/>条数 ${item?.value ?? 0}`
    } },
    xAxis: { type: 'value', minInterval: 1, splitLine: { show: false }, axisLabel: { show: false } },
    yAxis: {
      type: 'category',
      inverse: true,
      data: rows.map((r) => r.label),
      axisTick: { show: false },
      axisLabel: { fontSize: 12 },
    },
    series: [{
      type: 'bar',
      barMaxWidth: 16,
      data: rows.map((r, i) => ({ value: r.count, itemStyle: { color: spreadColors[i % spreadColors.length] } })),
      label: { show: true, position: 'right', fontSize: 12, formatter: '{c}' },
    }],
  }
}

export default function CloudSlowLogTab({ filter, active }: { filter: SnapshotFilter; active: boolean }) {
  const [rows, setRows] = useState<any[]>([])
  const [stats, setStats] = useState<any[]>([])
  const [trend, setTrend] = useState<any[]>([])
  const [spread, setSpread] = useState<any[]>([])
  const [cursor, setCursor] = useState<any>(null)
  const [note, setNote] = useState('')
  const [db, setDb] = useState('')
  const [q, setQ] = useState('')
  const [minMs, setMinMs] = useState('')
  const [nowMs, setNowMs] = useState(() => Date.now())

  async function load() {
    if (!filter.instanceId) {
      setRows([])
      setStats([])
      setTrend([])
      setSpread([])
      setCursor(null)
      setNote('请先选择 PostgreSQL 实例')
      return
    }
    const { data } = await api.get('/cloud/slow-logs', {
      params: { instance_id: filter.instanceId, from: filter.startTime, to: filter.endTime, db, q, min_ms: minMs || undefined },
      silent: true,
    })
    setRows(data.data || [])
    setStats(data.stats || [])
    setTrend(data.trend || [])
    setSpread(data.spread || [])
    setCursor(data.cursor || null)
    setNote(data.message || '')
  }

  useEffect(() => {
    if (active) void load().catch((e) => {
      setNote(e?.response?.data?.message || '加载失败')
    })
  }, [active, filter.instanceId, filter.startTime, filter.endTime])

  const syncedAt = cursor?.updated_at ? Date.parse(cursor.updated_at) : Number.NaN
  const coolUntil = Number.isFinite(syncedAt) ? syncedAt + 5 * 60 * 1000 : 0
  const cooling = coolUntil > nowMs
  useEffect(() => {
    if (!cooling) return
    const timer = window.setTimeout(() => setNowMs(Date.now()), Math.max(coolUntil - Date.now(), 0) + 200)
    return () => window.clearTimeout(timer)
  }, [cooling, coolUntil])

  const countChart = useMemo(() => trendOption(trend), [trend])
  const durationChart = useMemo(() => spreadOption(spread), [spread])

  return (
    <div>
      <Space style={{ marginBottom: 12 }} wrap>
        <span>已发到 Kafka {beijingText(cursor?.produced_at)} 已入库 {beijingText(cursor?.ingested_at)} 游标 {beijingText(cursor?.cursor_at)} {cursor?.status || ''} {cursor?.last_count ?? ''} {cursor?.error || ''}</span>
        {cursor?.gap_from ? <span>缺口 {beijingText(cursor.gap_from)} ~ {beijingText(cursor.gap_to)}</span> : null}
        <Input placeholder="库名" value={db} onChange={(e) => setDb(e.target.value)} style={{ width: 120 }} />
        <Input placeholder="SQL" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 180 }} />
        <Input placeholder="最小耗时 ms" value={minMs} onChange={(e) => setMinMs(e.target.value)} style={{ width: 120 }} />
        <Button onClick={() => void load().catch((e) => setNote(e?.response?.data?.message || '加载失败'))}>查询</Button>
        <Auth urn={Perm.sqlInsight.cloudSlowSync}>
          <Button
            type="primary"
            disabled={cooling || !filter.instanceId}
            onClick={() => api.post('/cloud/slow-logs/sync', { instance_id: filter.instanceId || '' }).then(({ data }) => {
              message.success(data?.message || '已发出')
              setNowMs(Date.now())
              void load()
            }).catch((e) => message.error(e?.response?.data?.message || '同步失败'))}
          >立即同步</Button>
        </Auth>
        <Link to="/config/sql-insight?tab=cloud">同步设置</Link>
      </Space>
      {note && <p>{note}</p>}
      {(trend.length > 0 || spread.length > 0) && (
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 12 }}>
          <div style={{ flex: '2 1 420px', minWidth: 0 }}>
            <div style={{ marginBottom: 4 }}>慢查询次数</div>
            <ChartBox option={countChart} height="220px" />
          </div>
          <div style={{ flex: '1 1 280px', minWidth: 0 }}>
            <div style={{ marginBottom: 4 }}>耗时分布</div>
            <ChartBox option={durationChart} height="220px" />
          </div>
        </div>
      )}
      <Tabs
        items={[
          {
            key: 'list',
            label: '慢 SQL 列表',
            children: (
              <Table
                rowKey="id"
                dataSource={rows}
                size="small"
                columns={[
                  { title: '时间', dataIndex: 'log_time', width: 180, render: (v) => beijingText(v) },
                  { title: '库', dataIndex: 'db_name', width: 120 },
                  { title: '用户', dataIndex: 'db_user', width: 100 },
                  { title: '客户端', dataIndex: 'client', width: 140 },
                  { title: '耗时 ms', dataIndex: 'duration_ms', width: 100 },
                  { title: 'SQL', dataIndex: 'sql_text' },
                ]}
              />
            ),
          },
          {
            key: 'stats',
            label: '慢 SQL 统计',
            children: (
              <Table
                rowKey="fingerprint"
                dataSource={stats}
                size="small"
                columns={[
                  { title: '最后执行', dataIndex: 'last_time', width: 180, render: (v) => beijingText(v) },
                  { title: '次数', dataIndex: 'calls', width: 80 },
                  { title: '总耗时 ms', dataIndex: 'total_ms', width: 110 },
                  { title: '平均 ms', dataIndex: 'avg_ms', width: 90 },
                  { title: '库', dataIndex: 'db_name', width: 120 },
                  { title: '用户', dataIndex: 'db_user', width: 100 },
                  { title: '客户端', dataIndex: 'client', width: 140 },
                  { title: 'SQL', dataIndex: 'sql_text' },
                ]}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
