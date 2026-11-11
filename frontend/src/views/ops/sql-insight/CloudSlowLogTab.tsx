import React, { useEffect, useMemo, useState } from 'react'
import { Button, Modal, Table, Tabs, Typography, message } from 'antd'
import type { EChartsCoreOption } from 'echarts'
import api from '../../../api/request'
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

const slowPage = {
  defaultPageSize: 10,
  pageSizeOptions: ['10', '20', '30', '50', '100'],
  showSizeChanger: true,
}

function csvCell(value: unknown) {
  const text = String(value ?? '')
  if (/[",\n\r]/.test(text)) return `"${text.replace(/"/g, '""')}"`
  return text
}

function downloadCsv(filename: string, headers: string[], rows: unknown[][]) {
  const body = [headers, ...rows].map((line) => line.map(csvCell).join(',')).join('\r\n')
  const blob = new Blob([`\uFEFF${body}`], { type: 'text/csv;charset=utf-8' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = filename
  link.click()
  URL.revokeObjectURL(link.href)
}

function SlowSqlText({ sql, onView }: { sql: string; onView: (sql: string) => void }) {
  const [cut, setCut] = useState(false)
  if (!sql) return null
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', maxWidth: 480, minWidth: 0 }}>
      <Typography.Paragraph
        style={{ marginBottom: 0, flex: 1, minWidth: 0 }}
        ellipsis={{ rows: 1, onEllipsis: (next) => setCut((prev) => (prev === next ? prev : next)) }}
      >
        {sql}
      </Typography.Paragraph>
      {cut ? (
        <Button type="link" size="small" style={{ paddingInline: 4 }} onClick={() => onView(sql)}>查看</Button>
      ) : null}
    </div>
  )
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
  const [note, setNote] = useState('')
  const [dbFilter, setDbFilter] = useState<string[]>([])
  const [viewSql, setViewSql] = useState('')
  const [slowTab, setSlowTab] = useState('list')

  async function load() {
    if (!filter.instanceId) {
      setRows([])
      setStats([])
      setTrend([])
      setSpread([])
      setNote('请先选择 PostgreSQL 实例')
      return
    }
    const { data } = await api.get('/cloud/slow-logs', {
      params: { instance_id: filter.instanceId, from: filter.startTime, to: filter.endTime },
      silent: true,
    })
    setDbFilter([])
    setRows(data.data || [])
    setStats(data.stats || [])
    setTrend(data.trend || [])
    setSpread(data.spread || [])
    setNote(data.message || '')
  }

  useEffect(() => {
    if (active) void load().catch((e) => {
      setNote(e?.response?.data?.message || '加载失败')
    })
  }, [active, filter.instanceId, filter.startTime, filter.endTime])

  const countChart = useMemo(() => trendOption(trend), [trend])
  const durationChart = useMemo(() => spreadOption(spread), [spread])
  const dbOptions = useMemo(() => {
    const names = new Set<string>()
    rows.forEach((row) => {
      const name = String(row.db_name || '').trim()
      if (name) names.add(name)
    })
    return [...names].sort().map((name) => ({ text: name, value: name }))
  }, [rows])
  const listRows = useMemo(() => {
    if (!dbFilter.length) return rows
    const picked = new Set(dbFilter)
    return rows.filter((row) => picked.has(String(row.db_name || '')))
  }, [rows, dbFilter])

  const exportCsv = () => {
    if (slowTab === 'stats') {
      if (!stats.length) {
        message.warning('没有可导出的数据')
        return
      }
      downloadCsv('cloud-slow-stats.csv', ['最后执行', '次数', '总耗时 ms', '平均 ms', '库', '用户', '客户端', 'SQL'], stats.map((row) => [
        beijingText(row.last_time), row.calls, row.total_ms, row.avg_ms, row.db_name, row.db_user, row.client, row.sql_text,
      ]))
      return
    }
    if (!listRows.length) {
      message.warning('没有可导出的数据')
      return
    }
    downloadCsv('cloud-slow-log.csv', ['时间', '库', '用户', '客户端', '耗时 ms', 'SQL'], listRows.map((row) => [
      beijingText(row.log_time), row.db_name, row.db_user, row.client, row.duration_ms, row.sql_text,
    ]))
  }

  return (
    <div>
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
        activeKey={slowTab}
        onChange={setSlowTab}
        tabBarExtraContent={<Button onClick={exportCsv}>导出</Button>}
        items={[
          {
            key: 'list',
            label: '慢 SQL 列表',
            children: (
              <Table
                rowKey="id"
                dataSource={rows}
                size="small"
                pagination={slowPage}
                onChange={(_pagination, filters) => {
                  const next = filters.db_name
                  setDbFilter(Array.isArray(next) ? next.map(String) : [])
                }}
                columns={[
                  { title: '时间', dataIndex: 'log_time', width: 180, render: (v) => beijingText(v) },
                  {
                    title: '库',
                    dataIndex: 'db_name',
                    width: 120,
                    filters: dbOptions,
                    filteredValue: dbFilter,
                    onFilter: (value, record) => String(record.db_name || '') === String(value),
                  },
                  { title: '用户', dataIndex: 'db_user', width: 100 },
                  { title: '客户端', dataIndex: 'client', width: 140 },
                  {
                    title: '耗时 ms',
                    dataIndex: 'duration_ms',
                    width: 110,
                    sorter: (a, b) => (Number(a.duration_ms) || 0) - (Number(b.duration_ms) || 0),
                  },
                  { title: 'SQL', dataIndex: 'sql_text', width: 480, render: (v) => <SlowSqlText sql={String(v || '')} onView={setViewSql} /> },
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
                pagination={slowPage}
                columns={[
                  { title: '最后执行', dataIndex: 'last_time', width: 180, render: (v) => beijingText(v) },
                  { title: '次数', dataIndex: 'calls', width: 80 },
                  { title: '总耗时 ms', dataIndex: 'total_ms', width: 110 },
                  { title: '平均 ms', dataIndex: 'avg_ms', width: 90 },
                  { title: '库', dataIndex: 'db_name', width: 120 },
                  { title: '用户', dataIndex: 'db_user', width: 100 },
                  { title: '客户端', dataIndex: 'client', width: 140 },
                  { title: 'SQL', dataIndex: 'sql_text', width: 480, render: (v) => <SlowSqlText sql={String(v || '')} onView={setViewSql} /> },
                ]}
              />
            ),
          },
        ]}
      />
      <Modal
        title="SQL"
        open={Boolean(viewSql)}
        onCancel={() => setViewSql('')}
        footer={<Button onClick={() => setViewSql('')}>关闭</Button>}
        width={760}
      >
        <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 480, overflow: 'auto' }}>{viewSql}</pre>
      </Modal>
    </div>
  )
}
