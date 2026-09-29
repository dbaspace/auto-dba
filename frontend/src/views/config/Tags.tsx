import React, { useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Select, Space, Tag, message } from 'antd'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

interface TagRow {
  id?: string
  key?: string
  name?: string
  values?: string[]
  description?: string
  sort_order?: number
  updated_at?: string
}

export default function Tags() {
  const [rows, setRows] = useState<TagRow[]>([])
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [form] = Form.useForm()

  async function load() {
    const { data } = await api.get('/monitor-tags')
    setRows(data.data || [])
  }

  function open(row?: TagRow) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      key: row?.key || '',
      name: row?.name || '',
      values: row?.values || [],
      description: row?.description || '',
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
        sort_order: Number(values.sort_order) || 0,
      }
      if (editingId) await api.put('/monitor-tags/' + editingId, payload)
      else await api.post('/monitor-tags', payload)
      message.success('已保存')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function remove(row: TagRow) {
    Modal.confirm({
      title: '提示',
      content: `删除标签「${row.key}」？监控目标注册中已保存的标签值不会自动清除。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/monitor-tags/' + row.id)
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
          新增标签
        </Button>
      </div>
      <p className="hint">
        定义监控目标可用的标签名与候选值，供{' '}
        <Link to="/config/monitor-targets">监控目标注册</Link> 的「选择标签名」使用。
        若填写了候选值，注册时下拉仅显示这些值；<code>idc</code>/<code>env</code>{' '}
        候选值为空时才会回退到环境管理编码。
      </p>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.key || ''}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ x: 'max-content', y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '标签名', dataIndex: 'key', width: 140 },
            { title: '显示名', dataIndex: 'name', width: 140 },
            {
              title: '候选值',
              dataIndex: 'values',
              width: 320,
              render: (vals: string[] | undefined) =>
                vals?.length ? (
                  <Space size={[4, 4]} wrap>
                    {vals.map((v) => (
                      <Tag key={v}>{v}</Tag>
                    ))}
                  </Space>
                ) : (
                  <span style={{ color: '#999' }}>无（可自定义输入）</span>
                ),
            },
            { title: '说明', dataIndex: 'description', ellipsis: true, width: 200 },
            { title: '排序', dataIndex: 'sort_order', width: 72 },
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
        title={editingId ? '编辑标签' : '新增标签'}
        width={560}
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
        <Form form={form} layout="vertical">
          <Form.Item
            name="key"
            label="标签名"
            rules={[
              { required: true, message: '请输入标签名' },
              { pattern: /^[A-Za-z_][\w.-]*$/, message: '以字母或下划线开头，可含数字、.、-' },
            ]}
          >
            <Input placeholder="如 app、domain、idc" disabled={!!editingId} />
          </Form.Item>
          <Form.Item name="name" label="显示名" rules={[{ required: true, message: '请输入显示名' }]}>
            <Input placeholder="界面展示名称" />
          </Form.Item>
          <Form.Item name="values" label="候选值" tooltip="可选；为空时注册目标时需手动输入标签值">
            <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="输入后回车添加，可多个" />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Form.Item name="sort_order" label="排序" initialValue={0}>
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
