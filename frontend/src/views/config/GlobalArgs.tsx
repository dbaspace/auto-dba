import React, { useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, Modal, Pagination, Space, Tooltip, message } from 'antd'
import { EyeOutlined } from '@ant-design/icons'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './GlobalArgs.css'

interface ArgRow {
  key?: string
  value?: string
  description?: string
  updated_at?: string
}

const PAGE_SIZE = 10
const VALUE_PREVIEW_LEN = 48

function truncateValue(raw?: string): string {
  const v = String(raw ?? '')
  if (!v) return '-'
  if (v.length <= VALUE_PREVIEW_LEN) return v
  return v.slice(0, VALUE_PREVIEW_LEN) + '…'
}

export default function GlobalArgs() {
  const [rows, setRows] = useState<ArgRow[]>([])
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [visible, setVisible] = useState(false)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [viewRow, setViewRow] = useState<ArgRow | null>(null)
  const [form] = Form.useForm()

  const filtered = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) return rows
    return rows.filter((r) => `${r.key} ${r.value} ${r.description}`.toLowerCase().includes(s))
  }, [rows, q])

  const pageRows = useMemo(
    () => filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE),
    [filtered, page],
  )

  async function load() {
    const { data } = await api.get('/global-args')
    setRows(data.data || [])
  }

  function open(row?: ArgRow) {
    setEditing(!!row)
    form.setFieldsValue({
      key: row?.key || '',
      value: row?.value || '',
      description: row?.description || '',
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      if (editing) await api.put('/global-args', { ...values })
      else await api.post('/global-args', { ...values })
      message.success('已保存')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function remove(row: ArgRow) {
    Modal.confirm({
      title: '提示',
      content: `删除参数「${row.key}」？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/global-args/' + encodeURIComponent(row.key || ''))
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
      <p className="hint">CDC / 建库 / 工单约束与实例类型等。修改后立即生效。</p>
      <div className="toolbar">
        <span className="label">关键词：</span>
        <Input
          allowClear
          placeholder="搜索键 / 值 / 说明"
          style={{ width: 260 }}
          value={q}
          onChange={(e) => {
            setQ(e.target.value)
            setPage(1)
          }}
        />
        <Button type="primary" onClick={() => open()}>
          新增参数
        </Button>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.key || ''}
          dataSource={pageRows}
          bordered
          pagination={false}
          scroll={{ y: 'calc(100% - 47px)' }}
          style={{ height: '100%' }}
          columns={[
            { title: '键', dataIndex: 'key', width: 260, ellipsis: true },
            {
              title: '值',
              dataIndex: 'value',
              width: 240,
              render: (v: string | undefined, row) => {
                const full = String(v ?? '')
                const needView = full.length > VALUE_PREVIEW_LEN
                return (
                  <div className={`ga-value-cell${needView ? ' has-view' : ''}`}>
                    {needView ? (
                      <Tooltip title="查看完整值">
                        <Button
                          type="text"
                          size="small"
                          className="ga-value-view"
                          icon={<EyeOutlined />}
                          onClick={() => setViewRow(row)}
                          aria-label="查看完整值"
                        />
                      </Tooltip>
                    ) : null}
                    <div className="ga-value-preview" title={needView ? full : undefined}>
                      {truncateValue(v)}
                    </div>
                  </div>
                )
              },
            },
            { title: '说明', dataIndex: 'description', width: 200, ellipsis: true },
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
      <div className="pager">
        <span className="pager-total">共 {filtered.length} 条</span>
        <Pagination
          current={page}
          pageSize={PAGE_SIZE}
          total={filtered.length}
          showSizeChanger={false}
          showQuickJumper={false}
          onChange={(p) => setPage(p)}
        />
      </div>
      <Modal
        open={visible}
        title={editing ? '编辑参数' : '新增参数'}
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
        <Form form={form} labelCol={{ style: { width: 80 } }}>
          <Form.Item label="键" name="key" rules={[{ required: true, message: '请填写键' }]}>
            <Input disabled={editing} placeholder="如 ticket_instance_types" />
          </Form.Item>
          <Form.Item label="值" name="value">
            <Input.TextArea rows={4} placeholder="字符串或 JSON" />
          </Form.Item>
          <Form.Item label="说明" name="description">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        open={!!viewRow}
        title={viewRow?.key ? `参数值 · ${viewRow.key}` : '参数值'}
        width={640}
        destroyOnClose
        onCancel={() => setViewRow(null)}
        footer={[
          <Button key="close" type="primary" onClick={() => setViewRow(null)}>
            关闭
          </Button>,
        ]}
      >
        <Input.TextArea
          className="ga-value-full"
          readOnly
          autoSize={{ minRows: 6, maxRows: 18 }}
          value={viewRow?.value || ''}
        />
      </Modal>
    </div>
  )
}
