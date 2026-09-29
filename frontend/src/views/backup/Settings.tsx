import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './backup.css'

/** 数据备份 · 存储池（平台 Agent 已迁至配置中心） */
export default function BackupSettings() {
  const [storages, setStorages] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [stOpen, setStOpen] = useState(false)
  const [stForm] = Form.useForm()

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/backup/storages')
      setStorages(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  async function saveStorage() {
    const v = await stForm.validateFields()
    await api.post('/backup/storages', {
      name: v.name,
      kind: v.kind,
      local_path: v.local_path || '',
      endpoint: v.endpoint || '',
      bucket: v.bucket || '',
      prefix: v.prefix || '',
      access_key: v.access_key || '',
      secret_key: v.secret_key || '',
      remark: v.remark || '',
    })
    message.success('已添加存储')
    setStOpen(false)
    stForm.resetFields()
    await load()
  }

  function delStorage(row: any) {
    Modal.confirm({
      title: '删除存储',
      content: `确认删除「${row.name}」？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete(`/backup/storages/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const stCols: ColumnsType<any> = [
    { title: '名称', dataIndex: 'name', minWidth: 140 },
    {
      title: '类型',
      width: 90,
      render: (_, r) => <Tag>{r.kind}</Tag>,
    },
    {
      title: '路径/桶',
      minWidth: 200,
      ellipsis: true,
      render: (_, r) => (r.kind === 'local' ? r.local_path || '-' : `${r.endpoint}/${r.bucket}`),
    },
    {
      title: '操作',
      width: 100,
      render: (_, r) => (
        <Button type="link" danger size="small" onClick={() => delStorage(r)}>
          删除
        </Button>
      ),
    },
  ]

  const kind = Form.useWatch('kind', stForm)

  return (
    <div className="backup-page">
      <div className="backup-toolbar">
        <h2>备份存储</h2>
        <span className="backup-hint">
          平台 Agent 请在 <Link to="/config/agents">配置中心 → 平台 Agent</Link> 定义（勾选「备份控制」归属后可被策略引用）。
        </span>
        <Button
          type="primary"
          onClick={() => {
            stForm.resetFields()
            stForm.setFieldsValue({ kind: 'local' })
            setStOpen(true)
          }}
        >
          添加存储
        </Button>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={stCols} dataSource={storages} />

      <Modal title="添加存储" open={stOpen} onCancel={() => setStOpen(false)} onOk={saveStorage} destroyOnClose>
        <Form form={stForm} layout="vertical" initialValues={{ kind: 'local' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select options={[{ value: 'local', label: '本地目录' }, { value: 's3', label: 'S3 兼容' }]} />
          </Form.Item>
          {kind === 'local' ? (
            <Form.Item name="local_path" label="本地路径" extra="空则使用平台 backup.workdir">
              <Input placeholder="/data/auto-dba-backup" />
            </Form.Item>
          ) : (
            <>
              <Form.Item name="endpoint" label="Endpoint" rules={[{ required: true }]}>
                <Input placeholder="https://s3.example.com" />
              </Form.Item>
              <Form.Item name="bucket" label="Bucket" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="prefix" label="前缀">
                <Input placeholder="backups/" />
              </Form.Item>
              <Form.Item name="access_key" label="AccessKey">
                <Input />
              </Form.Item>
              <Form.Item name="secret_key" label="SecretKey">
                <Input.Password />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </div>
  )
}
