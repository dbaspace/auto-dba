import React, { useCallback, useEffect, useState } from 'react'
import { Button, Card, Drawer, Form, Input, Space, Switch, Tag, Typography, message } from 'antd'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'

type Row = {
  id: string
  name: string
  source_matchers: string
  target_matchers: string
  equal_labels: string
  enabled: boolean
}

export default function AlertInhibitions() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.alert.ruleEdit)
  const [rows, setRows] = useState<Row[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Row | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/alert-inhibitions')
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
        source_matchers: v.source_matchers,
        target_matchers: v.target_matchers,
        equal_labels: v.equal_labels || '',
        enabled: v.enabled !== false,
      }
      if (editing) await api.put(`/alert-inhibitions/${editing.id}`, body)
      else await api.post('/alert-inhibitions', body)
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
    { title: '名称', dataIndex: 'name', render: (v: string, r: Row) => (
      <Space>{v}{!r.enabled && <Tag>停用</Tag>}</Space>
    ) },
    { title: '源（压制方）', dataIndex: 'source_matchers', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    { title: '目标（被压制）', dataIndex: 'target_matchers', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    { title: 'Equal', dataIndex: 'equal_labels', width: 120, render: (v: string) => v || '—' },
    {
      title: '操作', width: 120,
      render: (_: unknown, r: Row) => canEdit ? (
        <Space size={0}>
          <Button type="link" size="small" onClick={() => {
            setEditing(r)
            form.setFieldsValue(r)
            setOpen(true)
          }}>编辑</Button>
          <Button type="link" size="small" danger onClick={async () => {
            await api.delete(`/alert-inhibitions/${r.id}`)
            message.success('已删除')
            await load()
          }}>删除</Button>
        </Space>
      ) : null,
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>抑制规则</Typography.Title>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        当源告警 firing 时，匹配目标的告警仍落库，但跳过外发。equal 标签要求源与目标取值相同（如 instance）。
      </Typography.Paragraph>
      <Card size="small" extra={canEdit ? (
        <Button type="primary" onClick={() => {
          setEditing(null)
          form.resetFields()
          form.setFieldsValue({ enabled: true, equal_labels: 'instance' })
          setOpen(true)
        }}>新建抑制</Button>
      ) : null}>
        <ResponsiveTable rowKey="id" loading={loading} dataSource={rows} columns={columns} pagination={{ pageSize: 20 }} />
      </Card>
      <Drawer open={open} onClose={() => setOpen(false)} title={editing ? '编辑抑制' : '新建抑制'} width={480}
        extra={<Button type="primary" loading={saving} onClick={() => void submit()}>保存</Button>}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="source_matchers" label="源 Matchers" rules={[{ required: true }]}
            extra="如 alertname=HostDown"><Input /></Form.Item>
          <Form.Item name="target_matchers" label="目标 Matchers" rules={[{ required: true }]}
            extra="如 alertname=DiskHigh"><Input /></Form.Item>
          <Form.Item name="equal_labels" label="Equal 标签" extra="逗号分隔"><Input placeholder="instance" /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}
