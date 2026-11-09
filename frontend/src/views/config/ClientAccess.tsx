import React, { useEffect, useMemo, useState } from 'react'
import { Button, Input, InputNumber, Modal, Pagination, Popconfirm, Select, Space, Tabs, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './LoginLogs.css'

type PlatformUser = { id?: string; username?: string; display_name?: string; status?: string }

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
  source_cidrs?: string
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
  const [formUser, setFormUser] = useState<string | undefined>()
  const [formDays, setFormDays] = useState<number | null>(90)
  const [formRemark, setFormRemark] = useState('')
  const [formSources, setFormSources] = useState('')
  const [editID, setEditID] = useState('')
  const [editSources, setEditSources] = useState('')
  const [userOptionsLoading, setUserOptionsLoading] = useState(false)
  const [platformUsers, setPlatformUsers] = useState<PlatformUser[]>([])

  const userSelectOptions = useMemo(
    () =>
      platformUsers
        .filter((u) => String(u.status || 'active').toLowerCase() !== 'disabled')
        .filter((u) => String(u.username || '').trim())
        .map((u) => {
          const name = String(u.username || '').trim()
          const display = String(u.display_name || '').trim()
          return {
            value: name,
            label: display ? `${name}（${display}）` : name,
          }
        }),
    [platformUsers],
  )

  async function load(nextPage = page, override?: { username?: string; status?: string }) {
    const u = override?.username !== undefined ? override.username : username
    const st = override?.status !== undefined ? override.status : status
    setLoading(true)
    try {
      const { data } = await api.get('/client-access/grants', {
        params: {
          username: String(u || '').trim() || undefined,
          status: String(st || '').trim() || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '查询失败')
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function ensurePlatformUsers() {
    if (platformUsers.length) return
    setUserOptionsLoading(true)
    try {
      const { data } = await api.get('/users')
      setPlatformUsers(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载用户列表失败')
    } finally {
      setUserOptionsLoading(false)
    }
  }

  function openCreate() {
    setFormUser(undefined)
    setFormRemark('')
    setFormDays(90)
    setFormSources('')
    setOpen(true)
    void ensurePlatformUsers()
  }

  async function createGrant() {
    const username = String(formUser || '').trim()
    if (!username) {
      message.warning('请选择登录用户名')
      return
    }
    if (!formSources.trim()) {
      message.warning('请填写来源地址')
      return
    }
    try {
      await api.post('/client-access/grants', {
        username,
        days: formDays || 0,
        remark: formRemark.trim(),
        scopes: 'query',
        source_cidrs: formSources.trim(),
      })
      message.success('已开通授权，请对方登录后在「我的访问令牌」中自行签发')
      setOpen(false)
      setFormUser(undefined)
      setFormRemark('')
      load(1)
      setPage(1)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '开通失败')
    }
  }

  async function saveSources() {
    if (!editID) return
    if (!editSources.trim()) {
      message.warning('请填写来源地址')
      return
    }
    try {
      await api.put('/client-access/grants/' + editID + '/sources', { source_cidrs: editSources.trim() })
      message.success('已更新来源地址')
      setEditID('')
      void load(page)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '保存失败')
    }
  }

  async function revoke(id?: string) {
    if (!id) return
    try {
      await api.delete('/client-access/grants/' + id)
      message.success('已吊销，可在状态「已吊销」中查看')
      setPage(1)
      // 吊销后当前若筛「有效」会变空，自动切到已吊销列表
      setStatus('revoked')
      void load(1, { status: 'revoked' })
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
          onPressEnter={() => {
            setPage(1)
            void load(1)
          }}
          style={{ width: 160 }}
        />
        <Select
          allowClear
          placeholder="状态（空=全部）"
          value={status || undefined}
          onChange={(v) => {
            const next = v || ''
            setStatus(next)
            setPage(1)
            void load(1, { status: next })
          }}
          style={{ width: 140 }}
          options={[
            { value: 'active', label: '有效' },
            { value: 'revoked', label: '已吊销' },
          ]}
        />
        <Button
          type="primary"
          onClick={() => {
            setPage(1)
            void load(1)
          }}
        >
          查询
        </Button>
        <Button type="primary" onClick={openCreate}>
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
          { title: '来源地址', dataIndex: 'source_cidrs', width: 180, ellipsis: true, render: (v: string) => v || '未设置' },
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
            width: 140,
            render: (_: any, r: GrantRow) =>
              r.status === 'active' ? (
                <Space size={0}>
                  <Button
                    type="link"
                    size="small"
                    onClick={() => {
                      setEditID(r.id || '')
                      setEditSources(r.source_cidrs || '')
                    }}
                  >
                    来源
                  </Button>
                  <Popconfirm title="确认吊销？关联 PAT 将立即失效" onConfirm={() => revoke(r.id)}>
                    <Button type="link" danger size="small">
                      吊销
                    </Button>
                  </Popconfirm>
                </Space>
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
      <Modal
        title="开通客户端访问授权"
        open={open}
        onCancel={() => {
          setOpen(false)
          setFormUser(undefined)
        }}
        onOk={createGrant}
        okText="开通"
      >
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Select
            showSearch
            allowClear
            style={{ width: '100%' }}
            placeholder="登录用户名"
            loading={userOptionsLoading}
            value={formUser}
            onChange={(v) => setFormUser(v)}
            options={userSelectOptions}
            optionFilterProp="label"
            filterOption={(input, option) => {
              const q = input.trim().toLowerCase()
              if (!q) return true
              const label = String(option?.label ?? '').toLowerCase()
              const value = String(option?.value ?? '').toLowerCase()
              return label.includes(q) || value.includes(q)
            }}
            notFoundContent={userOptionsLoading ? '加载中…' : '无匹配用户'}
          />
          <InputNumber
            style={{ width: '100%' }}
            min={0}
            placeholder="有效天数（0 或不填表示不过期）"
            value={formDays as any}
            onChange={(v) => setFormDays(v)}
          />
          <Input.TextArea placeholder="备注" value={formRemark} onChange={(e) => setFormRemark(e.target.value)} rows={2} />
          <Input.TextArea
            placeholder={'来源地址，每行一个 IP 或网段\n例如 61.49.49.50 或 10.0.0.0/8'}
            value={formSources}
            onChange={(e) => setFormSources(e.target.value)}
            rows={3}
          />
        </Space>
      </Modal>
      <Modal title="修改来源地址" open={!!editID} onCancel={() => setEditID('')} onOk={saveSources} okText="保存">
        <Input.TextArea
          placeholder={'每行一个 IP 或网段\n例如 61.49.49.50 或 10.0.0.0/8'}
          value={editSources}
          onChange={(e) => setEditSources(e.target.value)}
          rows={4}
        />
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
