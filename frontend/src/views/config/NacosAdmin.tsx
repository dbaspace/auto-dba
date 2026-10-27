import React, { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Modal, Select, Space, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import './MonitorTargets.css'

interface NacosRow {
  id?: string
  environment?: string
  console_url?: string
  username?: string
  password?: string
  updated_at?: string
}

interface EnvRow {
  code?: string
  name?: string
}

export default function NacosAdmin() {
  const [rows, setRows] = useState<NacosRow[]>([])
  const [envs, setEnvs] = useState<EnvRow[]>([])
  const [loading, setLoading] = useState(false)
  const [envFilter, setEnvFilter] = useState<string | undefined>()
  const [keyword, setKeyword] = useState('')
  const [visible, setVisible] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const envOptions = envs.map((e) => ({
    value: e.code,
    label: e.name ? `${e.name} (${e.code})` : e.code,
  }))

  async function load() {
    setLoading(true)
    try {
      const [listRes, envRes] = await Promise.all([
        api.get('/nacosadmin', { params: { environment: envFilter || undefined, keyword: keyword || undefined } }),
        api.get('/environments'),
      ])
      setRows(listRes.data.data || [])
      setEnvs(envRes.data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function openCreate() {
    setEditingId('')
    form.resetFields()
    form.setFieldsValue({ console_url: 'http://10.100.113.151:8080/next' })
    setVisible(true)
  }

  function openEdit(row: NacosRow) {
    setEditingId(row.id || '')
    form.setFieldsValue({ ...row, password: '' })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = { ...values }
      if (editingId) {
        if (!payload.password) delete payload.password
        await api.put(`/nacosadmin/${editingId}`, payload)
      } else {
        await api.post('/nacosadmin', payload)
      }
      message.success('已保存')
      setVisible(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  function remove(row: NacosRow) {
    Modal.confirm({
      title: '确认删除该 Nacos 配置？',
      onOk: async () => {
        await api.delete(`/nacosadmin/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const columns = [
    { title: '环境', dataIndex: 'environment', key: 'environment', width: 140 },
    { title: '控制台地址', dataIndex: 'console_url', key: 'console_url' },
    { title: '用户名', dataIndex: 'username', key: 'username', width: 140 },
    { title: '密码', dataIndex: 'password', key: 'password', width: 100 },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 190 },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_: unknown, row: NacosRow) => (
        <Space>
          <Button type="link" onClick={() => openEdit(row)}>
            编辑
          </Button>
          <Button type="link" danger onClick={() => remove(row)}>
            删除
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="ops-page">
      <Alert
        className="cred-warn"
        type="warning"
        showIcon
        message={
          <span>
            Nacos 控制台登录凭据仅存于本平台。环境取自 <Link to="/config/environments">配置中心 · 环境管理</Link>
            ，控制台地址与账号密码在此手工录入，供「平台运维 · Nacos 管理」代登。示例：
            http://10.100.113.151:8080/next
          </span>
        }
      />
      <div className="ops-title">
        <Space wrap>
          <Select
            allowClear
            placeholder="环境"
            style={{ width: 160 }}
            value={envFilter}
            onChange={setEnvFilter}
            options={envOptions}
          />
          <Input.Search
            allowClear
            placeholder="关键字"
            style={{ width: 200 }}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onSearch={() => load()}
          />
          <Button onClick={load}>刷新</Button>
          <Button type="primary" onClick={openCreate}>
            新建
          </Button>
        </Space>
      </div>
      <div className="ops-table">
        <ResponsiveTable rowKey="id" loading={loading} columns={columns as any} dataSource={rows} pagination={{ pageSize: 10 }} />
      </div>
      <Modal
        title={editingId ? '编辑 Nacos 配置' : '新建 Nacos 配置'}
        open={visible}
        onCancel={() => setVisible(false)}
        onOk={save}
        confirmLoading={saving}
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item name="environment" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择环境管理中的环境"
              options={envOptions}
            />
          </Form.Item>
          <Form.Item
            name="console_url"
            label="控制台地址"
            rules={[{ required: true }]}
            extra="含 /next 的控制台根地址，如 http://host:8080/next"
          >
            <Input placeholder="http://10.100.113.151:8080/next" />
          </Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={editingId ? [] : [{ required: true }]}
            extra={editingId ? '留空表示不修改密码' : undefined}
          >
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
