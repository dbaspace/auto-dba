import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Space, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  assignNamespace,
  createCluster,
  deleteCluster,
  listClusters,
  probeCluster,
  updateCluster,
  type K8sCluster,
} from './services/k8sApi'
import './k8s.css'

export default function K8sClusters() {
  const [rows, setRows] = useState<K8sCluster[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [assignOpen, setAssignOpen] = useState(false)
  const [editing, setEditing] = useState<K8sCluster | null>(null)
  const [form] = Form.useForm()
  const [assignForm] = Form.useForm()

  async function load() {
    setLoading(true)
    try {
      setRows(await listClusters())
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<K8sCluster> = [
    { title: '名称', dataIndex: 'name' },
    { title: '状态', dataIndex: 'status', width: 110, render: (v) => <Tag color={statusColor(v)}>{v}</Tag> },
    { title: '版本', dataIndex: 'version', width: 140 },
    { title: '节点', dataIndex: 'node_count', width: 80 },
    { title: '接入', dataIndex: 'access_mode', width: 90 },
    { title: '最近探测', dataIndex: 'last_probe_at', width: 180 },
    {
      title: '操作',
      width: 280,
      render: (_, row) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => onProbe(row)}>探测</Button>
          <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
          <Button size="small" onClick={() => openAssign(row)}>划拨 NS</Button>
          <Button size="small" danger onClick={() => onDelete(row)}>删除</Button>
        </Space>
      ),
    },
  ]

  function openCreate() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ access_mode: 'direct', auth_type: 'kubeconfig', visibility: 'global' })
    setOpen(true)
  }

  function openEdit(row: K8sCluster) {
    setEditing(row)
    form.setFieldsValue({
      name: row.name,
      env_id: row.env_id,
      region_id: row.region_id,
      access_mode: row.access_mode,
      auth_type: row.auth_type,
      api_server: row.api_server,
      agent_id: row.agent_id,
      impersonate_enabled: row.impersonate_enabled,
      visibility: row.visibility,
      remark: row.remark,
      credential: '',
    })
    setOpen(true)
  }

  function openAssign(row: K8sCluster) {
    setEditing(row)
    assignForm.resetFields()
    setAssignOpen(true)
  }

  async function onSave() {
    const v = await form.validateFields()
    try {
      if (editing) {
        await updateCluster(editing.id, v)
        message.success('已更新')
      } else {
        await createCluster(v)
        message.success('已纳管')
      }
      setOpen(false)
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '失败')
    }
  }

  async function onProbe(row: K8sCluster) {
    try {
      const r = await probeCluster(row.id)
      message.success(r?.ok ? `探测成功 ${r.version || ''}` : `探测失败: ${r?.error || ''}`)
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '探测失败')
    }
  }

  async function onDelete(row: K8sCluster) {
    Modal.confirm({
      title: `删除集群 ${row.name}?`,
      onOk: async () => {
        await deleteCluster(row.id)
        message.success('已删除')
        load()
      },
    })
  }

  async function onAssign() {
    const v = await assignForm.validateFields()
    if (!editing) return
    try {
      await assignNamespace(editing.id, v)
      message.success('已划拨')
      setAssignOpen(false)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '失败')
    }
  }

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>K8s 集群管理</h3>
        <Button type="primary" onClick={openCreate}>纳管集群</Button>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} />

      <Modal title={editing ? '编辑集群' : '纳管集群'} open={open} onOk={onSave} onCancel={() => setOpen(false)} width={720} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="access_mode" label="接入方式">
            <Select options={[{ value: 'direct', label: '直连' }, { value: 'agent', label: 'Agent 隧道' }]} />
          </Form.Item>
          <Form.Item name="auth_type" label="认证">
            <Select options={[{ value: 'kubeconfig', label: 'kubeconfig' }, { value: 'token', label: 'Token' }]} />
          </Form.Item>
          <Form.Item name="api_server" label="API Server"><Input placeholder="https://...（token 模式必填）" /></Form.Item>
          <Form.Item name="credential" label={editing ? '凭据（留空不改）' : 'kubeconfig / token'} rules={editing ? [] : [{ required: true }]}>
            <Input.TextArea rows={8} placeholder="粘贴 kubeconfig YAML 或 Bearer Token" />
          </Form.Item>
          <Form.Item name="ca_data" label="CA（可选）"><Input.TextArea rows={2} /></Form.Item>
          <Form.Item name="agent_id" label="Agent ID"><Input placeholder="access_mode=agent 时填写" /></Form.Item>
          <Form.Item name="env_id" label="环境 ID"><Input /></Form.Item>
          <Form.Item name="region_id" label="区域 ID"><Input /></Form.Item>
          <Form.Item name="remark" label="备注"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal title="划拨命名空间" open={assignOpen} onOk={onAssign} onCancel={() => setAssignOpen(false)} destroyOnClose>
        <Form form={assignForm} layout="vertical">
          <Form.Item name="namespace" label="Namespace" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="tenant_id" label="目标租户 ID" rules={[{ required: true }]}><Input /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

function statusColor(s: string) {
  if (s === 'healthy') return 'green'
  if (s === 'degraded') return 'orange'
  if (s === 'unreachable') return 'red'
  return 'default'
}
