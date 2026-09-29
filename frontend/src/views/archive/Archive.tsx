import React, { useEffect, useState } from 'react'
import { Button, Card, Input, Modal, Select } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import './archive.css'

interface SimpleForm {
  name: string
  source_instance_id: string
  source_db: string
  table_name: string
  condition: string
}

function emptyForm(): SimpleForm {
  return { name: '', source_instance_id: '', source_db: '', table_name: '', condition: '' }
}

export default function Archive() {
  const [rows, setRows] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [visible, setVisible] = useState(false)
  const [form, setForm] = useState<SimpleForm>(emptyForm())

  function patch(p: Partial<SimpleForm>) {
    setForm((prev) => ({ ...prev, ...p }))
  }

  async function load() {
    setRows((await api.get('/archive/tasks')).data.data || [])
    setInstances((await api.get('/instances')).data.data || [])
  }

  async function create() {
    await api.post('/archive/tasks', form)
    setVisible(false)
    await load()
  }

  async function run(row: any) {
    await api.post(`/archive/tasks/${row.id}/run`)
    await load()
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const columns: ColumnsType<any> = [
    { title: '名称', dataIndex: 'name' },
    { title: '状态', dataIndex: 'status', width: 110 },
    { title: '源库', dataIndex: 'source_db' },
    { title: '表', dataIndex: 'table_name' },
    { title: '条件', dataIndex: 'condition' },
    {
      title: '操作',
      width: 120,
      render: (_, row) => (
        <Button type="link" onClick={() => run(row)}>
          运行
        </Button>
      ),
    },
  ]

  const labeled = (label: string, node: React.ReactNode) => (
    <div style={{ display: 'flex', alignItems: 'center', marginBottom: 16 }}>
      <div style={{ width: 90, textAlign: 'right', paddingRight: 12 }}>{label}</div>
      <div style={{ flex: 1 }}>{node}</div>
    </div>
  )

  return (
    <div className="page-card">
      <div className="archive-toolbar">
        <Button type="primary" onClick={() => setVisible(true)}>
          新建归档
        </Button>
      </div>
      <ResponsiveTable
        rowKey={(r) => r.id ?? r.name}
        columns={columns}
        dataSource={rows}
        pagination={false}
        style={{ marginTop: 16 }}
      />
      <Modal
        open={visible}
        title="归档任务"
        width={480}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={[
          <Button key="ok" type="primary" onClick={create}>
            创建
          </Button>,
        ]}
      >
        {labeled(
          '名称',
          <Input value={form.name} onChange={(e) => patch({ name: e.target.value })} />,
        )}
        {labeled(
          '实例',
          <Select
            style={{ width: '100%' }}
            value={form.source_instance_id || undefined}
            onChange={(v) => patch({ source_instance_id: v })}
            options={instances.map((i) => ({ label: i.name, value: i.id }))}
          />,
        )}
        {labeled(
          '库',
          <Input value={form.source_db} onChange={(e) => patch({ source_db: e.target.value })} />,
        )}
        {labeled(
          '表',
          <Input value={form.table_name} onChange={(e) => patch({ table_name: e.target.value })} />,
        )}
        {labeled(
          '条件',
          <Input
            value={form.condition}
            placeholder="created_at < now() - interval '90 day'"
            onChange={(e) => patch({ condition: e.target.value })}
          />,
        )}
      </Modal>
    </div>
  )
}
