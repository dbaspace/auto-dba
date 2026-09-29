import React, { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './backup.css'

const METHOD_LABELS: Record<string, string> = {
  logical_full: '逻辑全量',
  physical_full: '物理全量',
  physical_incr: '物理增量',
}

export default function BackupPolicies() {
  const [rows, setRows] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [storages, setStorages] = useState<any[]>([])
  const [agents, setAgents] = useState<any[]>([])
  const [methodOpts, setMethodOpts] = useState<{ value: string; label: string }[]>([
    { value: 'logical_full', label: '逻辑全量' },
    { value: 'physical_full', label: '物理全量' },
    { value: 'physical_incr', label: '物理增量 (MySQL)' },
  ])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<any>(null)
  const [form] = Form.useForm()
  const methodWatch = Form.useWatch('method', form)

  const instMap = useMemo(() => {
    const m: Record<string, any> = {}
    instances.forEach((i) => {
      m[i.id] = i
    })
    return m
  }, [instances])

  async function load() {
    setLoading(true)
    try {
      const [p, i, s, a, meta] = await Promise.all([
        api.get('/backup/policies'),
        api.get('/instances'),
        api.get('/backup/storages'),
        api.get('/platform/agents', { params: { scope: 'backup' } }),
        api.get('/backup/meta').catch(() => ({ data: { data: null } })),
      ])
      setRows(p.data.data || [])
      setInstances((i.data.data || []).filter((x: any) => {
        const t = String(x.db_type || '').toLowerCase()
        return t.includes('postgres') || t.includes('mysql') || t === 'tidb' || t === 'oceanbase'
      }))
      setStorages(s.data.data || [])
      setAgents(a.data.data || [])
      const md = meta?.data?.data
      if (md?.methods?.length) {
        const labels = md.method_labels || METHOD_LABELS
        setMethodOpts(md.methods.map((m: string) => ({ value: m, label: labels[m] || m })))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function openCreate() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({
      method: 'logical_full',
      retention_days: 7,
      parallel: 1,
      compress: false,
      databases: [],
    })
    setOpen(true)
  }

  function openEdit(row: any) {
    setEditing(row)
    form.setFieldsValue({
      name: row.name,
      instance_id: row.instance_id,
      method: row.method || 'logical_full',
      databases: row.databases || [],
      schedule_expr: row.schedule_expr,
      retention_days: row.retention_days,
      parallel: row.parallel,
      compress: !!row.compress,
      storage_id: row.storage_id,
      agent_id: row.agent_id || undefined,
    })
    setOpen(true)
  }

  async function onSave() {
    const v = await form.validateFields()
    const body = {
      name: v.name,
      instance_id: v.instance_id,
      databases: v.databases || [],
      schedule_expr: (v.schedule_expr || '').trim(),
      retention_days: v.retention_days || 7,
      parallel: v.parallel || 1,
      compress: !!v.compress,
      storage_id: v.storage_id,
      agent_id: v.agent_id || '',
      method: v.method || 'logical_full',
    }
    if (editing) {
      await api.patch(`/backup/policies/${editing.id}`, body)
      message.success('已更新')
    } else {
      await api.post('/backup/policies', body)
      message.success('已创建')
    }
    setOpen(false)
    await load()
  }

  async function run(row: any) {
    await api.post(`/backup/policies/${row.id}/run`)
    message.success('已触发备份，请到执行记录查看')
  }

  async function toggle(row: any, enabled: boolean) {
    await api.post(`/backup/policies/${row.id}/enabled`, { enabled })
    message.success(enabled ? '已启用' : '已停用')
    await load()
  }

  function remove(row: any) {
    Modal.confirm({
      title: '删除策略',
      content: `确认删除「${row.name}」？历史执行记录保留。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete(`/backup/policies/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const columns: ColumnsType<any> = [
    { title: '策略', dataIndex: 'name', minWidth: 140, ellipsis: true },
    {
      title: '实例',
      minWidth: 160,
      ellipsis: true,
      render: (_, r) => r.instance_name || instMap[r.instance_id]?.name || r.instance_id,
    },
    { title: '引擎', dataIndex: 'engine', width: 100 },
    {
      title: '方式',
      dataIndex: 'method',
      width: 110,
      render: (v) => METHOD_LABELS[v] || v || '逻辑全量',
    },
    {
      title: '库',
      minWidth: 120,
      ellipsis: true,
      render: (_, r) => (r.databases || []).join(', ') || '-',
    },
    { title: '调度', dataIndex: 'schedule_expr', width: 120, render: (v) => v || '仅手动' },
    {
      title: '启用',
      width: 80,
      render: (_, r) => (
        <Switch checked={!!r.enabled} onChange={(v) => toggle(r, v)} size="small" />
      ),
    },
    {
      title: '操作',
      width: 220,
      render: (_, r) => (
        <Space size="small" wrap>
          <Button type="link" size="small" onClick={() => run(r)}>
            立即执行
          </Button>
          <Button type="link" size="small" onClick={() => openEdit(r)}>
            编辑
          </Button>
          <Button type="link" size="small" danger onClick={() => remove(r)}>
            删除
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="backup-page">
      <div className="backup-toolbar">
        <h2>备份策略</h2>
        <Button type="primary" onClick={openCreate}>
          新建策略
        </Button>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 15 }} />
      <Modal
        title={editing ? '编辑策略' : '新建策略'}
        open={open}
        onCancel={() => setOpen(false)}
        onOk={onSave}
        width={560}
        destroyOnClose
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="如：prod-pg-nightly" />
          </Form.Item>
          <Form.Item name="instance_id" label="实例" rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={instances.map((i) => ({
                value: i.id,
                label: `${i.name} (${i.db_type})`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="method"
            label="备份方式"
            rules={[{ required: true }]}
            extra={
              String(methodWatch || '').startsWith('physical')
                ? '物理备份必须选择 Agent；PG 仅支持物理全量（pg_basebackup）；MySQL 支持全量/增量（xtrabackup）'
                : '逻辑全量可内嵌或走 Agent'
            }
          >
            <Select options={methodOpts} />
          </Form.Item>
          <Form.Item
            name="databases"
            label="数据库"
            extra="逻辑备份：MySQL 必填；物理备份可忽略（整实例）"
          >
            <Select mode="tags" placeholder="输入库名回车" tokenSeparators={[',']} />
          </Form.Item>
          <Form.Item name="storage_id" label="存储" rules={[{ required: true }]}>
            <Select
              options={storages.map((s) => ({
                value: s.id,
                label: `${s.name} (${s.kind})`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="agent_id"
            label="平台 Agent"
            rules={
              String(methodWatch || '').startsWith('physical')
                ? [{ required: true, message: '物理备份必须绑定平台 Agent' }]
                : undefined
            }
            extra="须在「配置中心 → 平台 Agent」勾选归属「备份控制」。物理备份必填；逻辑备份空则走平台内嵌（需 allow_embedded）"
          >
            <Select allowClear options={agents.map((a) => ({ value: a.id, label: a.name }))} />
          </Form.Item>
          <Form.Item name="schedule_expr" label="调度" extra="留空仅手动；支持 HH:MM 或 5 段 cron">
            <Input placeholder="02:30 或 0 2 * * *" />
          </Form.Item>
          <Space size="large">
            <Form.Item name="retention_days" label="保留天数">
              <InputNumber min={1} max={365} />
            </Form.Item>
            <Form.Item name="parallel" label="并行度">
              <InputNumber min={1} max={16} />
            </Form.Item>
            <Form.Item name="compress" label="压缩" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}
