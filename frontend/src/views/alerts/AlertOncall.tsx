import React, { useCallback, useEffect, useState } from 'react'
import { Button, Card, Drawer, Form, Input, Select, Space, Switch, Tag, Typography, message } from 'antd'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'

type Row = {
  id: string
  name: string
  weekday: number
  start_hm: string
  end_hm: string
  usernames: string[]
  enabled: boolean
}

const WEEKDAYS = [
  { value: 0, label: '周日' },
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' },
]

export default function AlertOncall() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.alert.ruleEdit)
  const [rows, setRows] = useState<Row[]>([])
  const [users, setUsers] = useState<{ value: string; label: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Row | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [res, uRes] = await Promise.all([
        api.get('/alert-oncall'),
        api.get('/users').catch(() => ({ data: { data: [] } })),
      ])
      setRows(res.data?.data || [])
      const list = (uRes.data?.data || []) as { username?: string; display_name?: string }[]
      setUsers(list.map((u) => ({
        value: u.username || '',
        label: u.display_name ? `${u.display_name} (${u.username})` : (u.username || ''),
      })).filter((x) => x.value))
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
        weekday: Number(v.weekday),
        start_hm: v.start_hm,
        end_hm: v.end_hm,
        usernames: v.usernames || [],
        enabled: v.enabled !== false,
      }
      if (editing) await api.put(`/alert-oncall/${editing.id}`, body)
      else await api.post('/alert-oncall', body)
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
    { title: '星期', dataIndex: 'weekday', width: 80, render: (v: number) => WEEKDAYS.find((w) => w.value === v)?.label || v },
    { title: '时段', width: 140, render: (_: unknown, r: Row) => `${r.start_hm} ~ ${r.end_hm}` },
    { title: '值班人', dataIndex: 'usernames', render: (v: string[]) => (v || []).map((u) => <Tag key={u}>{u}</Tag>) },
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
            await api.delete(`/alert-oncall/${r.id}`)
            message.success('已删除')
            await load()
          }}>删除</Button>
        </Space>
      ) : null,
    },
  ]

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>值班排班</Typography.Title>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        当前时段值班人会收到告警邮件，并优先作为企微 touser（需用户名与企微账号一致，或使用全局 WecomToUser）。
      </Typography.Paragraph>
      <Card size="small" extra={canEdit ? (
        <Button type="primary" onClick={() => {
          setEditing(null)
          form.resetFields()
          form.setFieldsValue({ weekday: 1, start_hm: '09:00', end_hm: '18:00', enabled: true })
          setOpen(true)
        }}>新建值班槽</Button>
      ) : null}>
        <ResponsiveTable rowKey="id" loading={loading} dataSource={rows} columns={columns} pagination={{ pageSize: 20 }} />
      </Card>
      <Drawer open={open} onClose={() => setOpen(false)} title={editing ? '编辑值班' : '新建值班'} width={480}
        extra={<Button type="primary" loading={saving} onClick={() => void submit()}>保存</Button>}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="weekday" label="星期" rules={[{ required: true }]}>
            <Select options={WEEKDAYS} />
          </Form.Item>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="start_hm" label="开始" rules={[{ required: true }]} style={{ width: 140 }}>
              <Input placeholder="09:00" />
            </Form.Item>
            <Form.Item name="end_hm" label="结束" rules={[{ required: true }]} style={{ width: 140 }}>
              <Input placeholder="18:00" />
            </Form.Item>
          </Space>
          <Form.Item name="usernames" label="值班人" rules={[{ required: true, message: '请选择值班人' }]}>
            <Select mode="multiple" options={users} showSearch optionFilterProp="label" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}
