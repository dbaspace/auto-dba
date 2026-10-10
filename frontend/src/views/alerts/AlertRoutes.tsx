import React, { useCallback, useEffect, useState } from 'react'
import { Button, Card, Drawer, Form, Input, InputNumber, Select, Space, Switch, Tag, Typography, message } from 'antd'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'

type RouteRow = {
  id: string
  name: string
  matchers: string
  channels: string[]
  group_by: string[]
  continue: boolean
  priority: number
  enabled: boolean
}

const CHANNELS = [
  { value: 'webhook', label: 'Webhook' },
  { value: 'wecom', label: '企业微信' },
  { value: 'phone', label: '电话' },
  { value: 'email', label: '邮件' },
]

export default function AlertRoutes() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.alert.ruleEdit)
  const [rows, setRows] = useState<RouteRow[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<RouteRow | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/alert-routes')
      setRows(res.data?.data || [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const submit = async () => {
    const v = await form.validateFields()
    setSaving(true)
    try {
      const body = {
        name: v.name,
        matchers: v.matchers || '',
        channels: v.channels || [],
        group_by: String(v.group_by_text || '').split(/[,;\s]+/).map((s: string) => s.trim()).filter(Boolean),
        continue: !!v.continue,
        priority: Number(v.priority ?? 100),
        enabled: v.enabled !== false,
      }
      if (editing) await api.put(`/alert-routes/${editing.id}`, body)
      else await api.post('/alert-routes', body)
      message.success('已保存')
      setOpen(false)
      await load()
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '保存失败'
      message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const columns = [
    { title: '名称', dataIndex: 'name', render: (v: string, r: RouteRow) => (
      <Space>{v}{!r.enabled && <Tag>停用</Tag>}</Space>
    ) },
    { title: '优先级', dataIndex: 'priority', width: 80 },
    { title: 'Matchers', dataIndex: 'matchers', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v || '(默认)'}</Typography.Text> },
    { title: '渠道', dataIndex: 'channels', render: (v: string[]) => (v || []).map((c) => <Tag key={c}>{c}</Tag>) },
    { title: '分组', dataIndex: 'group_by', width: 140, render: (v: string[]) => (v || []).join(', ') || '—' },
    {
      title: '操作', width: 120,
      render: (_: unknown, r: RouteRow) => canEdit ? (
        <Space size={0}>
          <Button type="link" size="small" onClick={() => {
            setEditing(r)
            form.setFieldsValue({ ...r, group_by_text: (r.group_by || []).join(',') })
            setOpen(true)
          }}>编辑</Button>
          <Button type="link" size="small" danger onClick={async () => {
            await api.delete(`/alert-routes/${r.id}`)
            message.success('已删除')
            await load()
          }}>删除</Button>
        </Space>
      ) : null,
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>通知路由</Typography.Title>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        按 matchers 选择外发渠道与分组键；空 matchers 为默认路由。优先级越小越先匹配，未勾选 continue 则停止。
      </Typography.Paragraph>
      <Card size="small" extra={canEdit ? (
        <Button type="primary" onClick={() => {
          setEditing(null)
          form.resetFields()
          form.setFieldsValue({ priority: 100, channels: ['webhook'], enabled: true })
          setOpen(true)
        }}>新建路由</Button>
      ) : null}>
        <ResponsiveTable rowKey="id" loading={loading} dataSource={rows} columns={columns} pagination={{ pageSize: 20 }} />
      </Card>
      <Drawer open={open} onClose={() => setOpen(false)} title={editing ? '编辑路由' : '新建路由'} width={480}
        extra={<Button type="primary" loading={saving} onClick={() => void submit()}>保存</Button>}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="matchers" label="Matchers" extra="如 severity=P0, service=mysql；空=默认"><Input /></Form.Item>
          <Form.Item name="channels" label="渠道" rules={[{ required: true }]}>
            <Select mode="multiple" options={CHANNELS} />
          </Form.Item>
          <Form.Item name="group_by_text" label="分组标签" extra="逗号分隔，如 service,cluster">
            <Input placeholder="service" />
          </Form.Item>
          <Form.Item name="priority" label="优先级"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="continue" label="继续匹配后续" valuePropName="checked"><Switch /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}
