import React, { useEffect, useMemo, useState } from 'react'
import { Button, Card, Col, Radio, Row, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import { formatTime, statusText, statusType } from './archive.ts'
import './archive.css'

const tagColor: Record<string, string> = {
  success: 'success',
  danger: 'error',
  warning: 'warning',
  info: 'default',
}

const weekdayIndex: Record<string, number> = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 }

function shanghaiParts(d: Date) {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    weekday: 'short',
  }).formatToParts(d)
  const get = (type: string) => parts.find((p) => p.type === type)?.value || ''
  return {
    y: Number(get('year')),
    m: Number(get('month')),
    day: Number(get('day')),
    weekday: weekdayIndex[get('weekday')] ?? 0,
  }
}

function shanghaiMidnight(y: number, m: number, day: number) {
  return Date.UTC(y, m - 1, day) - 8 * 3600 * 1000
}

function rangeStart(range: string, now = new Date()) {
  const p = shanghaiParts(now)
  if (range === 'month') return shanghaiMidnight(p.y, p.m, 1)
  if (range === 'week') {
    const delta = p.weekday === 0 ? 6 : p.weekday - 1
    const civil = new Date(Date.UTC(p.y, p.m - 1, p.day))
    civil.setUTCDate(civil.getUTCDate() - delta)
    return shanghaiMidnight(civil.getUTCFullYear(), civil.getUTCMonth() + 1, civil.getUTCDate())
  }
  return shanghaiMidnight(p.y, p.m, p.day)
}

function runAt(row: any): number {
  const raw = row?.finished_at || row?.started_at || row?.created_at
  const t = new Date(raw).getTime()
  return Number.isNaN(t) ? 0 : t
}

function isSuccess(status: string) {
  return status === 'executed' || status === 'success'
}

export default function Report() {
  const [range, setRange] = useState('week')
  const [runs, setRuns] = useState<any[]>([])
  const [tasks, setTasks] = useState<any[]>([])

  const running = useMemo(() => tasks.filter((t) => t.status === 'running').length, [tasks])
  const taskByID = useMemo(() => {
    const m = new Map<string, any>()
    tasks.forEach((t) => {
      if (t?.id) m.set(t.id, t)
    })
    return m
  }, [tasks])

  const scoped = useMemo(() => {
    const start = rangeStart(range)
    return runs.filter((row) => runAt(row) >= start)
  }, [runs, range])

  const kpis = useMemo(() => {
    const taskIDs = new Set<string>()
    let success = 0
    let failed = 0
    scoped.forEach((row) => {
      if (row.task_id) taskIDs.add(row.task_id)
      if (isSuccess(row.status)) success += 1
      else if (row.status === 'failed') failed += 1
    })
    const rate = success + failed > 0 ? `${Math.round((success / (success + failed)) * 100)}%` : '-'
    return [
      { label: '总任务', value: taskIDs.size, bg: '#1677ff', mark: '☰' },
      { label: '成功率', value: rate, bg: '#52c41a', mark: '✓' },
      { label: '失败', value: failed, bg: '#f5222d', mark: '!' },
      { label: '执行中', value: running, bg: '#fa8c16', mark: '▶' },
    ]
  }, [scoped, running])

  const rows = useMemo(
    () =>
      scoped.map((row) => {
        const task = taskByID.get(row.task_id) || {}
        return {
          ...row,
          name: row.task_name || row.name || task.name || '-',
          source_db: task.source_db || '-',
          table_name: task.table_name || '-',
          at: row.finished_at || row.started_at || row.created_at,
          volume: row.volume || `${row.deleted_rows || 0} 行`,
        }
      }),
    [scoped, taskByID],
  )

  async function load() {
    try {
      const [runRes, taskRes] = await Promise.all([api.get('/archive/runs'), api.get('/archive/tasks')])
      setRuns(runRes.data.data || [])
      setTasks(taskRes.data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    }
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<any> = [
    { title: '任务', dataIndex: 'name', ellipsis: true },
    { title: '源库', dataIndex: 'source_db', width: 140 },
    { title: '表', dataIndex: 'table_name', width: 180, ellipsis: true },
    {
      title: '状态',
      width: 100,
      render: (_, row) => (
        <Tag color={tagColor[statusType(row.status)] || 'default'}>{statusText(row.status)}</Tag>
      ),
    },
    { title: '时间', width: 180, render: (_, row) => formatTime(row.at) },
    { title: '归档量', dataIndex: 'volume', width: 90 },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="toolbar mb">
          <Radio.Group optionType="button" value={range} onChange={(e) => setRange(e.target.value)}>
            <Radio.Button value="today">今日</Radio.Button>
            <Radio.Button value="week">本周</Radio.Button>
            <Radio.Button value="month">本月</Radio.Button>
          </Radio.Group>
          <Button onClick={load}>刷新</Button>
        </div>
        <Row gutter={16} className="mb">
          {kpis.map((k) => (
            <Col key={k.label} xs={24} sm={12} lg={6}>
              <div className="kpi">
                <div className="kpi-ico" style={{ background: k.bg }}>
                  {k.mark}
                </div>
                <div>
                  <div className="kpi-num">{k.value}</div>
                  <div className="kpi-label">{k.label}</div>
                </div>
              </div>
            </Col>
          ))}
        </Row>
        <Card bordered={false}>
          <div style={{ marginBottom: 12 }}>任务明细</div>
          <ResponsiveTable
            rowKey={(r) => r.id ?? r.task_id ?? r.name}
            dataSource={rows}
            columns={columns}
            pagination={false}
            locale={{ emptyText: '暂无数据' }}
          />
        </Card>
      </div>
    </div>
  )
}
