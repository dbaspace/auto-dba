import React, { useCallback, useEffect, useState } from 'react'
import {
  Button, Card, DatePicker, Drawer, Form, Input, Tabs, Tag, Typography, message,
} from 'antd'
import dayjs, { type Dayjs } from 'dayjs'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'
import { AlertSilence, SILENCE_LABEL } from './types'

export default function AlertSilences() {
  const { hasUrn } = usePermission()
  const [tab, setTab] = useState('active')
  const [rows, setRows] = useState<AlertSilence[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/alert-silences', { params: { tab } })
      setRows(res.data?.data || [])
    } finally {
      setLoading(false)
    }
  }, [tab])

  useEffect(() => { void load() }, [load])

  const submit = async () => {
    const v = await form.validateFields()
    setSaving(true)
    try {
      const range = v.range as [Dayjs, Dayjs]
      await api.post('/alert-silences', {
        name: v.name,
        matchers: v.matchers,
        reason: v.reason,
        starts_at: range[0].toISOString(),
        ends_at: range[1].toISOString(),
      })
      message.success('静默已创建')
      setOpen(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  const columns = [
    {
      title: '规则名', key: 'name',
      render: (_: unknown, s: AlertSilence) => (
        <div>
          <div style={{ fontWeight: 600 }}>{s.name}</div>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>{s.reason}</Typography.Text>
        </div>
      ),
    },
    {
      title: '匹配范围', dataIndex: 'matchers',
      render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text>,
    },
    {
      title: '生效时间', width: 200,
      render: (_: unknown, s: AlertSilence) => (
        <div style={{ fontFamily: 'monospace', fontSize: 12 }}>
          {dayjs(s.starts_at).format('MM-DD HH:mm')}
          <br />→ {dayjs(s.ends_at).format('MM-DD HH:mm')}
        </div>
      ),
    },
    { title: '创建人', dataIndex: 'created_by', width: 100 },
    {
      title: '状态', width: 100, dataIndex: 'status',
      render: (v: string) => {
        const h = SILENCE_LABEL[v] || { label: v, color: 'default' }
        return <Tag color={h.color}>{h.label}</Tag>
      },
    },
    {
      title: '影响告警', width: 90, dataIndex: 'affected',
      render: (v: number) => <span style={{ fontWeight: 700, color: v > 0 ? '#dc2626' : undefined }}>{v ?? 0}</span>,
    },
    {
      title: '操作', width: 100,
      render: (_: unknown, s: AlertSilence) => (
        hasUrn(Perm.alert.silence) && s.status === 'active' ? (
          <Button type="link" size="small" danger onClick={async () => {
            await api.post(`/alert-silences/${s.id}/cancel`)
            message.success('已取消静默')
            await load()
          }}>取消</Button>
        ) : null
      ),
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>静默管理</Typography.Title>
      <Card
        size="small"
        extra={hasUrn(Perm.alert.silence) ? (
          <Button type="primary" onClick={() => {
            form.resetFields()
            form.setFieldsValue({
              range: [dayjs(), dayjs().add(2, 'hour')],
            })
            setOpen(true)
          }}>新建静默</Button>
        ) : null}
      >
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'active', label: '生效中' },
            { key: 'pending', label: '待生效' },
            { key: 'expired', label: '已过期' },
          ]}
        />
        <ResponsiveTable rowKey="id" loading={loading} dataSource={rows} columns={columns} pagination={{ pageSize: 20 }} />
      </Card>

      <Drawer
        width={480}
        open={open}
        onClose={() => setOpen(false)}
        title="新建静默规则"
        extra={<Button type="primary" loading={saving} onClick={() => void submit()}>提交</Button>}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="静默名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item name="reason" label="静默原因" rules={[{ required: true, message: '请填写原因' }]}>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item
            name="matchers"
            label="匹配条件"
            rules={[{ required: true, message: '请填写匹配条件' }]}
            extra="格式：service=order-service, env=prod（精确匹配）"
          >
            <Input placeholder="service=mysql, cluster=bj-prod-01" />
          </Form.Item>
          <Form.Item name="range" label="生效时间" rules={[{ required: true, message: '请选择时间' }]}>
            <DatePicker.RangePicker showTime style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}
