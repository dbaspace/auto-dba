import React, { useCallback, useEffect, useState } from 'react'
import { Button, Card, Col, DatePicker, Drawer, Row, Select, Space, Statistic, Tag, Typography, message } from 'antd'
import dayjs, { type Dayjs } from 'dayjs'
import api from '../../api/request'
import ResponsiveTable from '../../components/ResponsiveTable'
import { type AlertHistoryRow, HANDLE_LABEL, SEV_COLOR, alertLocation, durationText } from './types'

export default function AlertHistory() {
  const [rows, setRows] = useState<AlertHistoryRow[]>([])
  const [loading, setLoading] = useState(false)
  const [severity, setSeverity] = useState<string>()
  const [range, setRange] = useState<[Dayjs, Dayjs] | null>([dayjs().subtract(7, 'day'), dayjs()])
  const [report, setReport] = useState('')
  const [reportOpen, setReportOpen] = useState(false)
  const [reportLoading, setReportLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/alerts', {
        params: {
          tab: 'history',
          severity,
          since: range?.[0]?.toISOString(),
        },
      })
      setRows(res.data?.data || [])
    } finally {
      setLoading(false)
    }
  }, [severity, range])

  useEffect(() => { void load() }, [load])

  const columns = [
    { title: '级别', dataIndex: 'severity', width: 70, render: (v: string) => <Tag color={SEV_COLOR[v]}>{v}</Tag> },
    {
      title: '告警内容', key: 't',
      render: (_: unknown, a: AlertHistoryRow) => (
        <div>
          <div style={{ fontWeight: 600 }}>
            {a.kind === 'resolve' ? <Tag color="green">恢复</Tag> : <Tag color="orange">通知</Tag>}
            {a.title}
          </div>
          <Typography.Text type="secondary" ellipsis={{ tooltip: a.id }} style={{ fontSize: 12, maxWidth: '100%', display: 'inline-block' }}>
            告警 {a.id.slice(0, 8)}{a.cluster ? ` · ${a.cluster}` : ''}
          </Typography.Text>
        </div>
      ),
    },
    {
      title: '主机 / 目录', key: 'loc', width: 200,
      render: (_: unknown, a: AlertHistoryRow) => {
        const { host, mount } = alertLocation(a.labels)
        if (!host && !mount) return '—'
        const text = [host, mount].filter(Boolean).join(' · ')
        return <Typography.Text ellipsis={{ tooltip: text }} style={{ maxWidth: 190 }}>{text}</Typography.Text>
      },
    },
    { title: '服务', dataIndex: 'service', width: 120, render: (v: string) => v ? <Tag>{v}</Tag> : '—' },
    {
      title: '时间', dataIndex: 'event_at', width: 150,
      render: (v: string) => v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—',
    },
    {
      title: '第几次', width: 80,
      render: (_: unknown, a: AlertHistoryRow) => a.kind === 'notify' && a.notify_seq ? `第 ${a.notify_seq} 次` : '—',
    },
    { title: '持续', width: 90, render: (_: unknown, a: AlertHistoryRow) => durationText(a.started_at) },
    {
      title: '恢复时间', width: 150,
      render: (_: unknown, a: AlertHistoryRow) => a.resolved_at ? dayjs(a.resolved_at).format('YYYY-MM-DD HH:mm') : '—',
    },
    { title: '负责人', dataIndex: 'owner', width: 100, render: (v: string) => v || '—' },
    {
      title: '状态', width: 90,
      render: (_: unknown, a: AlertHistoryRow) => {
        const h = HANDLE_LABEL[a.handle] || { label: a.handle, color: 'default' }
        return <Tag color={h.color}>{h.label}</Tag>
      },
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>历史告警</Typography.Title>
      <Row gutter={12} style={{ marginBottom: 16 }}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="历史记录" value={rows.length} suffix="条" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="时间范围" value={range ? `${range[0].format('MM-DD')} ~ ${range[1].format('MM-DD')}` : '全部'} /></Card></Col>
      </Row>
      <Card size="small">
        <Space wrap style={{ marginBottom: 12 }}>
          <DatePicker.RangePicker value={range} onChange={(v) => setRange(v as [Dayjs, Dayjs] | null)} />
          <Select
            allowClear placeholder="全部级别" style={{ width: 120 }}
            options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))}
            value={severity} onChange={setSeverity}
          />
          <Button onClick={() => void load()}>查询</Button>
          <Button loading={reportLoading} onClick={async () => {
            setReportLoading(true)
            try {
              const res = await api.post('/alerts/postmortem', {
                since: range?.[0]?.toISOString(),
                until: range?.[1]?.toISOString(),
              })
              const md = res.data?.data?.markdown || ''
              setReport(md)
              setReportOpen(true)
              message.success('复盘已生成')
            } catch (e: unknown) {
              const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '生成失败'
              message.error(msg)
            } finally {
              setReportLoading(false)
            }
          }}>生成复盘</Button>
        </Space>
        <ResponsiveTable rowKey="event_id" loading={loading} dataSource={rows} columns={columns} pagination={{ pageSize: 20 }} />
      </Card>
      <Drawer open={reportOpen} onClose={() => setReportOpen(false)} title="告警复盘报告" width={720}
        extra={<Button onClick={() => {
          void navigator.clipboard.writeText(report)
          message.success('已复制 Markdown')
        }}>复制</Button>}>
        <Typography.Paragraph>
          <pre style={{ whiteSpace: 'pre-wrap', fontSize: 13, margin: 0 }}>{report}</pre>
        </Typography.Paragraph>
      </Drawer>
    </div>
  )
}
