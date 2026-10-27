import React, { useEffect, useState } from 'react'
import { Button, Input, InputNumber, Modal, Pagination, Popconfirm, Select, Space, Tabs, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './LoginLogs.css'

interface GrantRow {
  id?: string
  user_id?: string
  username?: string
  display_name?: string
  tenant_id?: string
  scopes?: string
  expires_at?: string
  created_by?: string
  status?: string
  remark?: string
  created_at?: string
}

interface AuditRow {
  id?: string
  username?: string
  client_type?: string
  method?: string
  path?: string
  status?: number
  latency_ms?: number
  message?: string
  ip?: string
  created_at?: string
}

const PAGE_SIZE = 10

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function GrantsPanel() {
  const [rows, setRows] = useState<GrantRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [status, setStatus] = useState('active')
  const [page, setPage] = useState(1)
  const [open, setOpen] = useState(false)
  const [formUser, setFormUser] = useState('')
  const [formDays, setFormDays] = useState<number | null>(90)
  const [formRemark, setFormRemark] = useState('')

  async function load(nextPage = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/client-access/grants', {
        params: {
          username: username.trim() || undefined,
          status: status || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function createGrant() {
    if (!formUser.trim()) {
      message.warning('请填写用户名')
      return
    }
    try {
      await api.post('/client-access/grants', {
        username: formUser.trim(),
        days: formDays || 0,
        remark: formRemark.trim(),
        scopes: 'query',
      })
      message.success('已开通授权')
      setOpen(false)
      setFormUser('')
      setFormRemark('')
      load(1)
      setPage(1)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '开通失败')
    }
  }

  async function revoke(id?: string) {
    if (!id) return
    try {
      await api.delete('/client-access/grants/' + id)
      message.success('已吊销')
      load(page)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '吊销失败')
    }
  }

  return (
    <>
      <p className="hint">
        为用户开通本地 AI / CLI / MCP 访问授权后，对方可签发 PAT，仅可调用查询白名单接口；浏览器登录与项目间调用不受影响。
      </p>
      <div className="toolbar">
        <Input
          allowClear
          placeholder="用户名"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          style={{ width: 160 }}
        />
        <Select
          allowClear
          placeholder="状态"
          value={status || undefined}
          onChange={(v) => setStatus(v || '')}
          style={{ width: 120 }}
          options={[
            { value: 'active', label: '有效' },
            { value: 'revoked', label: '已吊销' },
          ]}
        />
        <Button type="primary" onClick={() => { setPage(1); load(1) }}>
          查询
        </Button>
        <Button type="primary" onClick={() => setOpen(true)}>
          开通授权
        </Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        pagination={false}
        columns={[
          { title: '用户', dataIndex: 'username', render: (_: any, r: GrantRow) => r.display_name ? `${r.username}（${r.display_name}）` : r.username },
          { title: '范围', dataIndex: 'scopes', width: 100 },
          {
            title: '状态',
            dataIndex: 'status',
            width: 90,
            render: (v: string) => (v === 'active' ? <Tag color="green">有效</Tag> : <Tag>已吊销</Tag>),
          },
          { title: '过期', dataIndex: 'expires_at', width: 170, render: fmtTime },
          { title: '签发人', dataIndex: 'created_by', width: 100 },
          { title: '创建时间', dataIndex: 'created_at', width: 170, render: fmtTime },
          { title: '备注', dataIndex: 'remark', ellipsis: true },
          {
            title: '操作',
            width: 90,
            render: (_: any, r: GrantRow) =>
              r.status === 'active' ? (
                <Popconfirm title="确认吊销？关联 PAT 将立即失效" onConfirm={() => revoke(r.id)}>
                  <Button type="link" danger size="small">
                    吊销
                  </Button>
                </Popconfirm>
              ) : (
                '-'
              ),
          },
        ]}
      />
      <Pagination
        style={{ marginTop: 12, textAlign: 'right' }}
        current={page}
        pageSize={PAGE_SIZE}
        total={total}
        onChange={(p) => {
          setPage(p)
          load(p)
        }}
      />
      <Modal title="开通客户端访问授权" open={open} onCancel={() => setOpen(false)} onOk={createGrant} okText="开通">
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Input placeholder="用户名" value={formUser} onChange={(e) => setFormUser(e.target.value)} />
          <InputNumber
            style={{ width: '100%' }}
            min={0}
            placeholder="有效天数（0 或不填表示不过期）"
            value={formDays as any}
            onChange={(v) => setFormDays(v)}
          />
          <Input.TextArea placeholder="备注" value={formRemark} onChange={(e) => setFormRemark(e.target.value)} rows={2} />
        </Space>
      </Modal>
    </>
  )
}

function ApiAuditsPanel() {
  const [rows, setRows] = useState<AuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [clientType, setClientType] = useState('')
  const [page, setPage] = useState(1)

  async function load(nextPage = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/client-access/api-audits', {
        params: {
          username: username.trim() || undefined,
          client_type: clientType || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <>
      <p className="hint">记录本地 AI / CLI / MCP 通道的接口访问（含拒绝）。查询 SQL 明细见「查询审计」。</p>
      <div className="toolbar">
        <Input allowClear placeholder="用户名" value={username} onChange={(e) => setUsername(e.target.value)} style={{ width: 160 }} />
        <Select
          allowClear
          placeholder="客户端"
          value={clientType || undefined}
          onChange={(v) => setClientType(v || '')}
          style={{ width: 140 }}
          options={[
            { value: 'mcp', label: 'mcp' },
            { value: 'cli', label: 'cli' },
            { value: 'local_ai', label: 'local_ai' },
            { value: 'machine', label: 'machine' },
          ]}
        />
        <Button type="primary" onClick={() => { setPage(1); load(1) }}>
          查询
        </Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        pagination={false}
        columns={[
          { title: '时间', dataIndex: 'created_at', width: 170, render: fmtTime },
          { title: '用户', dataIndex: 'username', width: 110 },
          { title: '客户端', dataIndex: 'client_type', width: 90 },
          { title: '方法', dataIndex: 'method', width: 80 },
          { title: '路径', dataIndex: 'path', ellipsis: true },
          {
            title: '状态',
            dataIndex: 'status',
            width: 80,
            render: (v: number) => (v >= 200 && v < 400 ? <Tag color="green">{v}</Tag> : <Tag color="red">{v}</Tag>),
          },
          { title: '耗时ms', dataIndex: 'latency_ms', width: 80 },
          { title: '说明', dataIndex: 'message', ellipsis: true },
          { title: 'IP', dataIndex: 'ip', width: 120 },
        ]}
      />
      <Pagination
        style={{ marginTop: 12, textAlign: 'right' }}
        current={page}
        pageSize={PAGE_SIZE}
        total={total}
        onChange={(p) => {
          setPage(p)
          load(p)
        }}
      />
    </>
  )
}

export default function ClientAccess() {
  return (
    <div className="ops-page">
      <Tabs
        items={[
          { key: 'grants', label: '访问授权', children: <GrantsPanel /> },
          { key: 'api', label: '接口审计', children: <ApiAuditsPanel /> },
        ]}
      />
    </div>
  )
}
