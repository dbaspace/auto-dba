import React, { useEffect, useMemo, useState } from 'react'
import { Alert, AutoComplete, Button, Form, Input, InputNumber, Modal, Select, Space, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import './MonitorTargets.css'

interface FeRow {
  id?: string
  task_name?: string
  environment?: string
  domain?: string
  port?: number
  fe_node?: string
  username?: string
  password?: string
  updated_at?: string
}

interface PreviewItem {
  task_name?: string
  env?: string
  domain?: string
  endpoints?: string[]
}

interface EnvRow {
  code?: string
  name?: string
}

function splitFeNodes(s?: string): string[] {
  return String(s || '')
    .split(/[,，;\n]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function joinFeNodes(arr: unknown): string {
  if (Array.isArray(arr)) return arr.map((x) => String(x || '').trim()).filter(Boolean).join(',')
  return String(arr || '').trim()
}

export default function FeAdmin() {
  const [rows, setRows] = useState<FeRow[]>([])
  const [envs, setEnvs] = useState<EnvRow[]>([])
  const [preview, setPreview] = useState<PreviewItem[]>([])
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [envFilter, setEnvFilter] = useState<string | undefined>()
  const [keyword, setKeyword] = useState('')
  const [visible, setVisible] = useState(false)
  const [readonly, setReadonly] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()
  const watchTask = Form.useWatch('task_name', form) as string | undefined

  async function load() {
    setLoading(true)
    try {
      const [listRes, envRes, prevRes] = await Promise.all([
        api.get('/feadmin', { params: { environment: envFilter || undefined, keyword: keyword || undefined } }),
        api.get('/environments'),
        api.get('/monitor-targets/preview', { params: { kind: 'starrocks-fe' } }),
      ])
      setRows(listRes.data.data || [])
      setEnvs(envRes.data.data || [])
      setPreview(prevRes.data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const taskOptions = useMemo(() => {
    const names = Array.from(new Set(preview.map((p) => p.task_name).filter(Boolean))) as string[]
    return names.map((n) => ({ value: n, label: n }))
  }, [preview])

  const domainOptions = useMemo(() => {
    const names = Array.from(new Set(preview.map((p) => p.domain).filter(Boolean))) as string[]
    return names.map((n) => ({ value: n, label: n }))
  }, [preview])

  const feNodeOptions = useMemo(() => {
    const hit = preview.find((p) => p.task_name === watchTask)
    const nodes = new Set<string>()
    if (hit) {
      ;(hit.endpoints || []).forEach((e) => nodes.add(e))
    } else {
      preview.forEach((p) => (p.endpoints || []).forEach((e) => nodes.add(e)))
    }
    return Array.from(nodes).map((n) => ({ value: n, label: n }))
  }, [preview, watchTask])

  function applyPreview(taskName?: string) {
    const hit = preview.find((p) => p.task_name === taskName) || preview[0]
    if (!hit) return
    const eps = hit.endpoints || []
    form.setFieldsValue({
      task_name: hit.task_name,
      environment: hit.env || form.getFieldValue('environment'),
      domain: hit.domain || form.getFieldValue('domain'),
      fe_nodes: eps.length ? eps : form.getFieldValue('fe_nodes'),
      port: form.getFieldValue('port') || 8030,
    })
  }

  async function syncFromMonitor() {
    setSyncing(true)
    try {
      const { data } = await api.post('/feadmin/sync-from-monitor')
      const d = data?.data || {}
      message.success(
        `已从监控同步：新建 ${d.created || 0}，更新拓扑 ${d.updated || 0}` +
          (d.skipped ? `，跳过 ${d.skipped}` : '') +
          '。请编辑补充账号密码。',
      )
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  function openCreate() {
    setEditingId('')
    setReadonly(false)
    form.resetFields()
    form.setFieldsValue({ port: 8030, username: 'root', fe_nodes: [] })
    if (preview[0]) applyPreview(preview[0].task_name)
    setVisible(true)
  }

  function openEdit(row: FeRow, viewOnly = false) {
    setEditingId(row.id || '')
    setReadonly(viewOnly)
    form.setFieldsValue({
      ...row,
      password: '',
      fe_nodes: splitFeNodes(row.fe_node),
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload: Record<string, unknown> = {
        ...values,
        fe_node: joinFeNodes(values.fe_nodes),
      }
      delete payload.fe_nodes
      if (editingId) {
        if (!payload.password) delete payload.password
        await api.put(`/feadmin/${editingId}`, payload)
      } else {
        if (!payload.password) delete payload.password
        await api.post('/feadmin', payload)
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

  function remove(row: FeRow) {
    Modal.confirm({
      title: '确认删除该 FE 凭据？',
      onOk: async () => {
        await api.delete(`/feadmin/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const columns = [
    { title: '任务名', dataIndex: 'task_name', key: 'task_name' },
    { title: '环境', dataIndex: 'environment', key: 'environment', width: 120 },
    { title: '域名', dataIndex: 'domain', key: 'domain' },
    { title: '端口', dataIndex: 'port', key: 'port', width: 80 },
    {
      title: 'FE 节点',
      dataIndex: 'fe_node',
      key: 'fe_node',
      render: (v: string) => (
        <Space size={[4, 4]} wrap>
          {splitFeNodes(v).map((n) => (
            <Tag key={n}>{n}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '用户名',
      dataIndex: 'username',
      key: 'username',
      width: 120,
      render: (v: string, row: FeRow) => (
        <Space size={4}>
          <span>{v || '-'}</span>
          {!row.password ? <Tag color="warning">待补凭据</Tag> : null}
        </Space>
      ),
    },
    { title: '密码', dataIndex: 'password', key: 'password', width: 100, render: (v: string) => v || '（未设）' },
    {
      title: '操作',
      key: 'action',
      width: 200,
      render: (_: unknown, row: FeRow) => (
        <Space>
          <Button type="link" onClick={() => openEdit(row, true)}>
            查看
          </Button>
          <Button type="link" onClick={() => openEdit(row, false)}>
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
            请先在 <Link to="/config/monitor-targets">监控目标注册</Link> 登记 StarRocks FE 目标组（label 含 fe），再点「从监控同步」拉取拓扑（含全部 FE 节点），最后编辑补充账号密码。
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
            options={envs.map((e) => ({ value: e.code, label: e.name ? `${e.name} (${e.code})` : e.code }))}
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
          <Button type="primary" loading={syncing} onClick={() => void syncFromMonitor()}>
            从监控同步
          </Button>
          <Button onClick={openCreate}>新建</Button>
        </Space>
      </div>
      <div className="ops-table">
        <ResponsiveTable rowKey="id" loading={loading} columns={columns as any} dataSource={rows} pagination={{ pageSize: 10 }} />
      </div>
      <Modal
        title={readonly ? '查看 FE 凭据' : editingId ? '编辑 FE 凭据' : '新建 FE 凭据'}
        open={visible}
        onCancel={() => setVisible(false)}
        onOk={readonly ? () => setVisible(false) : save}
        okText={readonly ? '关闭' : '保存'}
        confirmLoading={saving}
        width={640}
        destroyOnClose
      >
        <Form form={form} layout="vertical" disabled={readonly}>
          <Form.Item name="task_name" label="任务名">
            <AutoComplete
              allowClear
              options={taskOptions}
              onChange={(v) => applyPreview(v)}
              placeholder="可从监控注册预填，也可手输"
            />
          </Form.Item>
          <Form.Item name="environment" label="环境" rules={[{ required: true }]}>
            <Select
              showSearch
              options={envs.map((e) => ({ value: e.code, label: e.name ? `${e.name} (${e.code})` : e.code }))}
            />
          </Form.Item>
          <Form.Item name="domain" label="域名" rules={[{ required: true }]}>
            <AutoComplete options={domainOptions} placeholder="可手输" />
          </Form.Item>
          <Form.Item name="port" label="端口">
            <InputNumber style={{ width: '100%' }} min={0} />
          </Form.Item>
          <Form.Item
            name="fe_nodes"
            label="FE 节点"
            rules={[{ required: true, type: 'array', min: 1, message: '请至少选择一个 FE 节点' }]}
            extra="可从监控任务带出多个 FE；也可手输 host:port 后回车添加"
          >
            <Select
              mode="tags"
              style={{ width: '100%' }}
              placeholder="host:port，可多选"
              options={feNodeOptions}
              tokenSeparators={[',', '，', ';', ' ']}
            />
          </Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true }]} extra="同步后默认可为 root，请按现场修改">
            <Input />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            extra={editingId ? '留空表示不修改密码；未设密码请在此补充' : '可稍后编辑补充'}
          >
            <Input.Password placeholder={editingId ? '******' : '可选，同步后补全'} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
