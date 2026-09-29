import React, { useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { CI_TYPES } from './services/cmdbApi'

type TagDef = {
  id?: string
  key?: string
  name?: string
  values?: string[]
  description?: string
  required?: boolean
  ci_types?: string[]
  scope?: string
  sort_order?: number
}

export default function CmdbTags() {
  const [rows, setRows] = useState<TagDef[]>([])
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [form] = Form.useForm()

  async function load() {
    const { data } = await api.get('/cmdb/tags')
    setRows(data.data || [])
  }

  function open(row?: TagDef) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      key: row?.key || '',
      name: row?.name || '',
      values: row?.values || [],
      description: row?.description || '',
      required: !!row?.required,
      ci_types: row?.ci_types || [],
      scope: row?.scope || 'general',
      sort_order: row?.sort_order ?? 0,
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = {
        key: String(values.key || '').trim(),
        name: String(values.name || '').trim(),
        values: (values.values || []).map((v: string) => String(v).trim()).filter(Boolean),
        description: values.description || '',
        required: !!values.required,
        ci_types: values.ci_types || [],
        scope: values.scope || 'general',
        sort_order: Number(values.sort_order) || 0,
      }
      if (editingId) await api.put('/cmdb/tags/' + editingId, payload)
      else await api.post('/cmdb/tags', payload)
      message.success('已保存')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  return (
    <div className="ops-page">
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => open()}>
          新建标签
        </Button>
        <Button
          onClick={async () => {
            await api.post('/cmdb/tags/sync-monitor')
            message.success('已从监控标签同步')
            await load()
          }}
        >
          同步监控标签
        </Button>
      </Space>
      <ResponsiveTable
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: 'Key', dataIndex: 'key', width: 140 },
          { title: '名称', dataIndex: 'name', width: 140 },
          {
            title: '候选值',
            dataIndex: 'values',
            render: (vs: string[]) => (
              <Space wrap>
                {(vs || []).map((v) => (
                  <Tag key={v}>{v}</Tag>
                ))}
              </Space>
            ),
          },
          { title: '范围', dataIndex: 'scope', width: 100 },
          {
            title: '适用类型',
            dataIndex: 'ci_types',
            render: (t: string[]) => (t?.length ? t.join(', ') : '全部'),
          },
          {
            title: '操作',
            width: 140,
            render: (_: unknown, row: TagDef) => (
              <Space>
                <Button type="link" size="small" onClick={() => open(row)}>
                  编辑
                </Button>
                <Button
                  type="link"
                  danger
                  size="small"
                  onClick={() =>
                    Modal.confirm({
                      title: '删除标签？',
                      onOk: async () => {
                        await api.delete('/cmdb/tags/' + row.id)
                        message.success('已删除')
                        await load()
                      },
                    })
                  }
                >
                  删除
                </Button>
              </Space>
            ),
          },
        ]}
      />
      <Modal open={visible} title={editingId ? '编辑标签' : '新建标签'} onCancel={() => setVisible(false)} onOk={() => void save()} confirmLoading={saving} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="key" label="Key" rules={[{ required: true }]}>
            <Input disabled={!!editingId} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="values" label="候选值">
            <Select mode="tags" tokenSeparators={[',']} />
          </Form.Item>
          <Form.Item name="ci_types" label="适用 CI 类型">
            <Select mode="multiple" options={CI_TYPES} allowClear />
          </Form.Item>
          <Form.Item name="scope" label="范围">
            <Select
              options={[
                { value: 'general', label: '通用' },
                { value: 'monitor', label: '监控' },
              ]}
            />
          </Form.Item>
          <Form.Item name="required" label="必填" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="sort_order" label="排序">
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
