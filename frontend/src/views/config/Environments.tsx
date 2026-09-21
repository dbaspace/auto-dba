import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Space, Table, message } from 'antd'
import api from '../../api/request'

interface EnvRow {
  id?: string
  code?: string
  name?: string
  description?: string
  updated_at?: string
}

export default function Environments() {
  const [rows, setRows] = useState<EnvRow[]>([])
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [form] = Form.useForm()

  async function load() {
    const { data } = await api.get('/environments')
    setRows(data.data || [])
  }

  function open(row?: EnvRow) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      code: row?.code || '',
      name: row?.name || '',
      description: row?.description || '',
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = { code: values.code, name: values.name, description: values.description }
      if (editingId) await api.put('/environments/' + editingId, payload)
      else await api.post('/environments', payload)
      message.success('已保存')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function remove(row: EnvRow) {
    Modal.confirm({
      title: '提示',
      content: `删除环境「${row.name}」？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/environments/' + row.id)
        message.success('已删除')
        await load()
      },
    })
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <div className="ops-page">
      <div className="ops-title">
        <Button type="primary" onClick={() => open()}>
          新增环境
        </Button>
      </div>
      <p className="hint">环境可被多个区域绑定。实例上的环境编码会同步到 idc，供工单按环境筛选。</p>
      <div className="ops-table">
        <Table
          rowKey={(r) => r.id || r.code || ''}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '编码', dataIndex: 'code', width: 160 },
            { title: '名称', dataIndex: 'name', width: 180 },
            { title: '说明', dataIndex: 'description', ellipsis: true, width: 220 },
            { title: '更新时间', dataIndex: 'updated_at', width: 180 },
            {
              title: '操作',
              width: 160,
              fixed: 'right',
              render: (_, row) => (
                <Space size={0}>
                  <Button type="link" onClick={() => open(row)}>
                    编辑
                  </Button>
                  <Button type="link" danger onClick={() => remove(row)}>
                    删除
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </div>
      <Modal
        open={visible}
        title={editingId ? '编辑环境' : '新增环境'}
        width={480}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={[
          <Button key="cancel" onClick={() => setVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={saving} onClick={save}>
            保存
          </Button>,
        ]}
      >
        <Form form={form} labelCol={{ style: { width: 80 } }}>
          <Form.Item label="编码" name="code" rules={[{ required: true, message: '请填写编码' }]}>
            <Input disabled={!!editingId} placeholder="如 test / prod" />
          </Form.Item>
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="如 测试 / 生产" />
          </Form.Item>
          <Form.Item label="说明" name="description">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
