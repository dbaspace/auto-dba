import React, { useEffect, useMemo, useState } from 'react'
import { AutoComplete, Button, Form, Input, Modal, Select, Space, Table, Tag, message } from 'antd'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'

type Tenant = {
  id: string
  code: string
  name: string
  status: string
  plan?: string
  description?: string
}

type Member = {
  tenant_id: string
  user_id: string
  username?: string
  display_name?: string
  role_ids: string[]
  status: string
}

type GrantResourceOpt = { id: string; label: string }

const GRANT_TYPES = [
  { value: 'instance', label: '实例' },
  { value: 'database', label: '已登记库' },
  { value: 'sr_cluster', label: 'SR集群' },
  { value: 'db_deploy_cluster', label: '部署集群' },
]

export default function Tenants() {
  const [list, setList] = useState<Tenant[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [memberOpen, setMemberOpen] = useState(false)
  const [grantOpen, setGrantOpen] = useState(false)
  const [auditOpen, setAuditOpen] = useState(false)
  const [current, setCurrent] = useState<Tenant | null>(null)
  const [grants, setGrants] = useState<any[]>([])
  const [audits, setAudits] = useState<any[]>([])
  const [grantResources, setGrantResources] = useState<GrantResourceOpt[]>([])
  const [grantLabelMap, setGrantLabelMap] = useState<Record<string, string>>({})
  const [grantResLoading, setGrantResLoading] = useState(false)
  const [editForm] = Form.useForm()
  const [grantForm] = Form.useForm()
  const [members, setMembers] = useState<Member[]>([])
  const [roles, setRoles] = useState<{ id: string; name: string; code: string }[]>([])
  const [users, setUsers] = useState<{ id: string; username: string }[]>([])
  const [planOptions, setPlanOptions] = useState<{ value: string; label: string }[]>([
    { value: 'free', label: '免费' },
    { value: 'standard', label: '标准' },
    { value: 'enterprise', label: '企业' },
  ])
  const [form] = Form.useForm()
  const [memberForm] = Form.useForm()

  const targetTenantOptions = useMemo(
    () =>
      list
        .filter((t) => t.id !== current?.id && t.status !== 'disabled')
        .map((t) => ({ value: t.id, label: `${t.name || t.code}（${t.code}）` })),
    [list, current?.id],
  )

  const planLabel = (v?: string) => planOptions.find((p) => p.value === v)?.label || v || '-'

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/tenants')
      setList(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    api.get('/tenants/plan-options', { silent: true }).then((r) => {
      if (Array.isArray(r.data?.data) && r.data.data.length) setPlanOptions(r.data.data)
    }).catch(() => {})
    api.get('/roles', { silent: true }).then((r) => setRoles(r.data?.data || [])).catch(() => {})
    api.get('/users', { silent: true }).then((r) => setUsers(r.data?.data || [])).catch(() => {})
  }, [])

  async function onCreate() {
    const v = await form.validateFields()
    await api.post('/tenants', v)
    message.success('已创建')
    setCreateOpen(false)
    form.resetFields()
    load()
  }

  async function openMembers(t: Tenant) {
    setCurrent(t)
    setMemberOpen(true)
    const { data } = await api.get(`/tenants/${t.id}/members`)
    setMembers(data.data || [])
  }

  async function saveMember() {
    if (!current) return
    const v = await memberForm.validateFields()
    await api.put(`/tenants/${current.id}/members`, v)
    message.success('已保存成员')
    memberForm.resetFields()
    const { data } = await api.get(`/tenants/${current.id}/members`)
    setMembers(data.data || [])
  }

  async function removeMember(userId: string) {
    if (!current) return
    await api.delete(`/tenants/${current.id}/members/${userId}`)
    message.success('已移除')
    const { data } = await api.get(`/tenants/${current.id}/members`)
    setMembers(data.data || [])
  }

  async function openEdit(t: Tenant) {
    setCurrent(t)
    editForm.setFieldsValue({ name: t.name, status: t.status, plan: t.plan || 'standard', description: t.description })
    setEditOpen(true)
  }

  async function onEdit() {
    if (!current) return
    const v = await editForm.validateFields()
    await api.put(`/tenants/${current.id}`, v)
    message.success('已更新')
    setEditOpen(false)
    load()
  }

  async function loadGrantResources(tenantId: string, type: string) {
    if (!tenantId || !type) {
      setGrantResources([])
      return
    }
    setGrantResLoading(true)
    try {
      const { data } = await api.get(`/tenants/${tenantId}/grant-resources`, { params: { type } })
      const opts: GrantResourceOpt[] = data.data || []
      setGrantResources(opts)
      setGrantLabelMap((prev) => {
        const next = { ...prev }
        for (const r of opts) next[r.id] = r.label
        return next
      })
    } catch (e: any) {
      setGrantResources([])
      message.error(e?.response?.data?.message || '加载可授权资源失败')
    } finally {
      setGrantResLoading(false)
    }
  }

  async function warmGrantLabels(tenantId: string, rows: { resource_type?: string }[]) {
    const types = [...new Set(rows.map((g) => g.resource_type).filter(Boolean))] as string[]
    for (const typ of types) {
      try {
        const { data } = await api.get(`/tenants/${tenantId}/grant-resources`, { params: { type: typ } })
        const opts: GrantResourceOpt[] = data.data || []
        setGrantLabelMap((prev) => {
          const next = { ...prev }
          for (const r of opts) next[r.id] = r.label
          return next
        })
      } catch {
        /* ignore */
      }
    }
  }

  async function openGrants(t: Tenant) {
    setCurrent(t)
    setGrantOpen(true)
    setGrantLabelMap({})
    grantForm.setFieldsValue({ resource_type: 'instance', resource_id: undefined, to_tenant_id: undefined, permission: 'read' })
    const { data } = await api.get(`/tenants/${t.id}/grants`)
    const rows = data.data || []
    setGrants(rows)
    await loadGrantResources(t.id, 'instance')
    await warmGrantLabels(t.id, rows)
  }

  async function saveGrant() {
    if (!current) return
    const v = await grantForm.validateFields()
    await api.post(`/tenants/${current.id}/grants`, v)
    message.success('已授权')
    const typ = v.resource_type || 'instance'
    grantForm.setFieldsValue({ resource_type: typ, resource_id: undefined, to_tenant_id: undefined, permission: 'read' })
    const { data } = await api.get(`/tenants/${current.id}/grants`)
    setGrants(data.data || [])
    await loadGrantResources(current.id, typ)
  }

  async function removeGrant(id: string) {
    if (!current) return
    await api.delete(`/tenants/${current.id}/grants/${id}`)
    const { data } = await api.get(`/tenants/${current.id}/grants`)
    setGrants(data.data || [])
  }

  async function openAudit(t: Tenant) {
    setCurrent(t)
    setAuditOpen(true)
    const { data } = await api.get(`/tenants/${t.id}/audit-logs`)
    setAudits(data.data || [])
  }

  async function impersonate(t: Tenant) {
    const { data } = await api.post(`/tenants/${t.id}/impersonate`)
    localStorage.setItem('token', data.token)
    localStorage.setItem('user', JSON.stringify(data.user))
    message.success(`已进入租户审计：${t.name}`)
    location.hash = '#/'
    location.reload()
  }

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 16 }}>
        <Auth urn={Perm.tenants.create}>
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            新建租户
          </Button>
        </Auth>
        <Button onClick={load}>刷新</Button>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={list}
        columns={[
          { title: '编码', dataIndex: 'code' },
          { title: '名称', dataIndex: 'name' },
          {
            title: '状态',
            dataIndex: 'status',
            render: (s: string) => (
              <Tag color={s === 'active' ? 'green' : 'default'}>{s}</Tag>
            ),
          },
          {
            title: '套餐',
            dataIndex: 'plan',
            render: (v: string) => planLabel(v),
          },
          { title: '说明', dataIndex: 'description', ellipsis: true },
          {
            title: '操作',
            render: (_: unknown, r: Tenant) => (
              <Space>
                <Auth urn={Perm.tenants.edit}>
                  <Button type="link" onClick={() => openEdit(r)}>
                    编辑
                  </Button>
                </Auth>
                <Auth urn={Perm.tenants.members}>
                  <Button type="link" onClick={() => openMembers(r)}>
                    成员
                  </Button>
                </Auth>
                <Auth urn={Perm.tenants.grants}>
                  <Button type="link" onClick={() => openGrants(r)}>
                    授权
                  </Button>
                </Auth>
                <Button type="link" onClick={() => openAudit(r)}>
                  审计
                </Button>
                <Auth urn={Perm.tenants.impersonate}>
                  <Button type="link" onClick={() => impersonate(r)}>
                    进入审计
                  </Button>
                </Auth>
              </Space>
            ),
          },
        ]}
      />

      <Modal title="新建租户" open={createOpen} onOk={onCreate} onCancel={() => setCreateOpen(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="code" label="编码" rules={[{ required: true }]}>
            <Input placeholder="如 bu-a" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="plan"
            label="套餐"
            initialValue="standard"
            rules={[
              { required: true, message: '请选择或填写套餐' },
              {
                pattern: /^[a-z0-9._-]{1,64}$/,
                message: '小写字母/数字/.-_，最长 64',
              },
            ]}
            extra="可选：免费 / 标准 / 企业，也可自定义如 bu-finance"
          >
            <AutoComplete
              options={planOptions.map((p) => ({ value: p.value, label: `${p.label}（${p.value}）` }))}
              placeholder="选择或输入自定义套餐"
              filterOption={(input, option) =>
                String(option?.value || '')
                  .toLowerCase()
                  .includes(input.toLowerCase()) ||
                String(option?.label || '')
                  .toLowerCase()
                  .includes(input.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`成员 · ${current?.name || ''}`}
        open={memberOpen}
        onCancel={() => setMemberOpen(false)}
        footer={null}
        width={720}
        destroyOnClose
      >
        <Auth urn={Perm.tenants.members}>
          <Form form={memberForm} layout="inline" style={{ marginBottom: 12 }} onFinish={saveMember}>
            <Form.Item name="user_id" rules={[{ required: true, message: '选择用户' }]}>
              <Select
                style={{ width: 200 }}
                placeholder="用户"
                options={users.map((u) => ({ value: u.id, label: u.username }))}
                showSearch
                optionFilterProp="label"
              />
            </Form.Item>
            <Form.Item name="role_ids" rules={[{ required: true, message: '选择角色' }]}>
              <Select
                mode="multiple"
                style={{ width: 260 }}
                placeholder="角色"
                options={roles.map((r) => ({ value: r.id, label: `${r.name}(${r.code})` }))}
              />
            </Form.Item>
            <Button type="primary" htmlType="submit">
              添加/更新
            </Button>
          </Form>
        </Auth>
        <Table
          rowKey="user_id"
          size="small"
          dataSource={members}
          columns={[
            { title: '用户', dataIndex: 'username', width: 120 },
            { title: '显示名', dataIndex: 'display_name', width: 120 },
            {
              title: '角色',
              render: (_: unknown, m: Member) => {
                const ids = Array.isArray(m.role_ids) ? m.role_ids.filter(Boolean) : []
                if (!ids.length) return '-'
                const nameOf = (id: string) => {
                  const r = roles.find((x) => x.id === id)
                  return r?.name || r?.code || id
                }
                return (
                  <Space size={[4, 4]} wrap>
                    {ids.map((id) => (
                      <Tag key={id}>{nameOf(id)}</Tag>
                    ))}
                  </Space>
                )
              },
            },
            { title: '状态', dataIndex: 'status', width: 90 },
            {
              title: '操作',
              width: 80,
              render: (_: unknown, m: Member) => (
                <Button type="link" danger onClick={() => removeMember(m.user_id)}>
                  移除
                </Button>
              ),
            },
          ]}
        />
      </Modal>

      <Modal title={`编辑 · ${current?.name || ''}`} open={editOpen} onOk={onEdit} onCancel={() => setEditOpen(false)} destroyOnClose>
        <Form form={editForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select options={[{ value: 'active', label: 'active' }, { value: 'disabled', label: 'disabled' }]} />
          </Form.Item>
          <Form.Item name="plan" label="套餐" rules={[{ required: true }]}>
            <AutoComplete options={planOptions.map((p) => ({ value: p.value, label: `${p.label}（${p.value}）` }))} />
          </Form.Item>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`跨租户授权 · ${current?.name || ''}`}
        open={grantOpen}
        onCancel={() => {
          setGrantOpen(false)
          setGrantResources([])
          setGrantLabelMap({})
        }}
        footer={null}
        width={880}
        destroyOnClose
      >
        <p style={{ marginBottom: 12, color: '#6b7280', fontSize: 13 }}>
          从「{current?.name || '资源方租户'}」选出资源，授权给目标租户只读使用。下方表格为已创建的授权记录（不是资源目录）。
        </p>
        <Form form={grantForm} layout="inline" style={{ marginBottom: 12, rowGap: 8 }} onFinish={saveGrant}>
          <Form.Item name="resource_type" rules={[{ required: true }]} initialValue="instance">
            <Select
              style={{ width: 130 }}
              options={GRANT_TYPES}
              onChange={(typ) => {
                grantForm.setFieldsValue({ resource_id: undefined })
                if (current) void loadGrantResources(current.id, typ)
              }}
            />
          </Form.Item>
          <Form.Item name="resource_id" rules={[{ required: true, message: '请选择资源' }]}>
            <Select
              style={{ width: 280 }}
              showSearch
              optionFilterProp="label"
              loading={grantResLoading}
              placeholder={grantResLoading ? '加载中…' : grantResources.length ? '选择资源' : '该类型下暂无资源'}
              options={grantResources.map((r) => ({ value: r.id, label: r.label }))}
              notFoundContent={grantResLoading ? '加载中…' : '暂无可选资源'}
            />
          </Form.Item>
          <Form.Item name="to_tenant_id" rules={[{ required: true, message: '请选择目标租户' }]}>
            <Select
              style={{ width: 220 }}
              showSearch
              optionFilterProp="label"
              placeholder="目标租户"
              options={targetTenantOptions}
            />
          </Form.Item>
          <Form.Item name="permission" initialValue="read">
            <Select style={{ width: 100 }} options={[{ value: 'read', label: 'read' }]} />
          </Form.Item>
          <Button type="primary" htmlType="submit">
            授权
          </Button>
        </Form>
        <Table
          rowKey="id"
          size="small"
          dataSource={grants}
          locale={{ emptyText: '暂无授权记录，请选择资源与目标租户后点击授权' }}
          columns={[
            {
              title: '类型',
              dataIndex: 'resource_type',
              width: 120,
              render: (v: string) => GRANT_TYPES.find((t) => t.value === v)?.label || v,
            },
            {
              title: '资源',
              dataIndex: 'resource_id',
              ellipsis: true,
              render: (id: string) => grantLabelMap[id] || id,
            },
            {
              title: '目标租户',
              dataIndex: 'to_tenant_id',
              ellipsis: true,
              render: (id: string) => {
                const t = list.find((x) => x.id === id)
                return t ? `${t.name || t.code}` : id || '—'
              },
            },
            { title: '权限', dataIndex: 'permission', width: 80 },
            {
              title: '操作',
              width: 80,
              render: (_: unknown, g: { id: string }) => (
                <Button type="link" danger onClick={() => removeGrant(g.id)}>
                  撤销
                </Button>
              ),
            },
          ]}
        />
      </Modal>

      <Modal title={`审计 · ${current?.name || ''}`} open={auditOpen} onCancel={() => setAuditOpen(false)} footer={null} width={800} destroyOnClose>
        <Table
          rowKey="id"
          size="small"
          dataSource={audits}
          columns={[
            { title: '时间', dataIndex: 'created_at', width: 200 },
            { title: '动作', dataIndex: 'action' },
            { title: '详情', dataIndex: 'detail', ellipsis: true },
            { title: '操作者', dataIndex: 'actor_user_id', ellipsis: true },
          ]}
        />
      </Modal>
    </div>
  )
}
