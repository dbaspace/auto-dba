import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Space, Table, message } from 'antd'
import api from '../../api/request'

interface EnvRow {
  id?: string
  code?: string
  name?: string
}

interface RegionRow {
  id?: string
  code?: string
  name?: string
  description?: string
}

export default function Regions() {
  const [rows, setRows] = useState<RegionRow[]>([])
  const [envs, setEnvs] = useState<EnvRow[]>([])
  const [bindMap, setBindMap] = useState<Record<string, EnvRow[]>>({})
  const [visible, setVisible] = useState(false)
  const [bindVisible, setBindVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [bindForm, setBindForm] = useState<{ id: string; name: string; env_ids: string[] }>({
    id: '',
    name: '',
    env_ids: [],
  })
  const [form] = Form.useForm()

  function envText(regionId?: string) {
    const list = bindMap[regionId || ''] || []
    return list.map((e) => e.name || e.code).join('、') || '—'
  }

  async function load() {
    const [r, e] = await Promise.all([api.get('/regions'), api.get('/environments')])
    const regionRows: RegionRow[] = r.data.data || []
    setRows(regionRows)
    setEnvs(e.data.data || [])
    const map: Record<string, EnvRow[]> = {}
    await Promise.all(
      regionRows.map(async (row) => {
        map[row.id || ''] = (await api.get(`/regions/${row.id}/envs`)).data.data || []
      }),
    )
    setBindMap(map)
  }

  function open(row?: RegionRow) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      code: row?.code || '',
      name: row?.name || '',
      description: row?.description || '',
    })
    setVisible(true)
  }

  function openBind(row: RegionRow) {
    setBindForm({
      id: row.id || '',
      name: row.name || row.code || '',
      env_ids: (bindMap[row.id || ''] || []).map((e) => e.id || ''),
    })
    setBindVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = { code: values.code, name: values.name, description: values.description }
      if (editingId) await api.put('/regions/' + editingId, payload)
      else await api.post('/regions', payload)
      message.success('已保存')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  async function saveBind() {
    setSaving(true)
    try {
      await api.put(`/regions/${bindForm.id}/envs`, { env_ids: bindForm.env_ids })
      message.success('已绑定')
      setBindVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function remove(row: RegionRow) {
    Modal.confirm({
      title: '提示',
      content: `删除区域「${row.name}」？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/regions/' + row.id)
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
          新增区域
        </Button>
      </div>
      <p className="hint">区域编码唯一。同一区域（如 beijing）请编辑后勾选多个环境，不要重复新增。</p>
      <div className="ops-table">
        <Table
          rowKey={(r) => r.id || r.code || ''}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '编码', dataIndex: 'code', width: 140 },
            { title: '名称', dataIndex: 'name', width: 160 },
            { title: '绑定环境', width: 220, render: (_, row) => envText(row.id) },
            { title: '说明', dataIndex: 'description', width: 180, ellipsis: true },
            {
              title: '操作',
              width: 220,
              fixed: 'right',
              render: (_, row) => (
                <Space size={0}>
                  <Button type="link" onClick={() => openBind(row)}>
                    绑定环境
                  </Button>
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
        title={editingId ? '编辑区域' : '新增区域'}
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
            <Input disabled={!!editingId} placeholder="如 default / beijing" />
          </Form.Item>
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="如 默认 / 北京" />
          </Form.Item>
          <Form.Item label="说明" name="description">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        open={bindVisible}
        title="绑定环境"
        width={480}
        destroyOnClose
        onCancel={() => setBindVisible(false)}
        footer={[
          <Button key="cancel" onClick={() => setBindVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={saving} onClick={saveBind}>
            保存
          </Button>,
        ]}
      >
        <p className="hint">区域「{bindForm.name}」可同时绑定测试、生产等多个环境。</p>
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          placeholder="选择环境"
          style={{ width: '100%' }}
          value={bindForm.env_ids}
          onChange={(v) => setBindForm((f) => ({ ...f, env_ids: v }))}
          options={envs.map((e) => ({ value: e.id, label: `${e.name} (${e.code})` }))}
        />
      </Modal>
    </div>
  )
}
