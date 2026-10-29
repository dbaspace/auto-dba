import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  AutoComplete,
  Button,
  Dropdown,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  message,
  type MenuProps,
} from 'antd'
import api, { abortAuthReissue, beginAuthReissue, finishAuthReissue, getAuthEpoch } from '../../api/request'
import Auth from '../../components/Auth'
import { Perm, usePermission } from '../../hooks/usePermission'
import { useAppDispatch } from '../../store'
import { setUser } from '../../store/authSlice'

const RESOURCE_LABELS: Record<string, string> = {
  tbl_instances: '实例',
  tbl_databases: '已登记库',
  tbl_db_accounts: '数据库账号',
  tbl_issues: '工单',
  tbl_todos: '待办',
  tbl_cdc_tasks: 'CDC 任务',
  tbl_sr_clusters: 'SR 集群',
  tbl_db_deploy_clusters: '部署集群',
  tbl_proxy_services: '接入层代理',
  tbl_migration_tasks: '迁移任务',
  tbl_flashback_tasks: '闪回任务',
  tbl_archive_tasks: '归档任务',
  tbl_backup_policies: '备份策略',
  tbl_ha_clusters: 'HA 集群',
  tbl_alert_rules: '告警规则',
  tbl_alerts: '告警',
}

function formatResourceSummary(resources?: Record<string, number>) {
  if (!resources) return ''
  return Object.entries(resources)
    .filter(([, n]) => n > 0)
    .map(([k, n]) => `${RESOURCE_LABELS[k] || k} ${n}`)
    .join('、')
}

type Tenant = {
  id: string
  code: string
  name: string
  status: string
  plan?: string
  description?: string
}

function isSystemTenant(t: Tenant) {
  const code = String(t.code || '').toLowerCase()
  return t.id === 'tenant-default' || code === 'default' || code === 'dba'
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
  { value: 'domain', label: '域名' },
  { value: 'sr_cluster', label: 'SR集群' },
  { value: 'db_deploy_cluster', label: '部署集群' },
]

export default function Tenants() {
  const { hasUrn } = usePermission()
  const dispatch = useAppDispatch()
  const [list, setList] = useState<Tenant[]>([])
  const [listQ, setListQ] = useState('')
  const [loading, setLoading] = useState(false)
  const [grantSelectedKeys, setGrantSelectedKeys] = useState<React.Key[]>([])
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
  const [users, setUsers] = useState<{ id: string; username: string; display_name?: string; status?: string }[]>([])
  const [planOptions, setPlanOptions] = useState<{ value: string; label: string }[]>([
    { value: 'free', label: '免费' },
    { value: 'standard', label: '标准' },
    { value: 'enterprise', label: '企业' },
  ])
  const [form] = Form.useForm()
  const [memberForm] = Form.useForm()
  const memberOptsLoaded = useRef(false)
  const grantTargetMode = Form.useWatch('grant_target_mode', grantForm)

  const targetTenantOptions = useMemo(
    () =>
      list
        .filter((t) => t.id !== current?.id && t.status !== 'disabled')
        .map((t) => ({ value: t.id, label: `${t.name || t.code}（${t.code}）` })),
    [list, current?.id],
  )

  const targetUserOptions = useMemo(
    () =>
      users
        .filter((u) => String(u.status || 'active').toLowerCase() !== 'disabled')
        .map((u) => ({
          value: u.id,
          label: u.display_name ? `${u.display_name}（${u.username}）` : u.username || u.id,
        })),
    [users],
  )

  const userLabelById = useMemo(() => {
    const m: Record<string, string> = {}
    for (const u of users) {
      m[u.id] = u.display_name ? `${u.display_name}（${u.username}）` : u.username || u.id
    }
    return m
  }, [users])

  const filteredList = useMemo(() => {
    const kw = listQ.trim().toLowerCase()
    if (!kw) return list
    return list.filter((t) => {
      const code = String(t.code || '').toLowerCase()
      const name = String(t.name || '').toLowerCase()
      const desc = String(t.description || '').toLowerCase()
      return code.includes(kw) || name.includes(kw) || desc.includes(kw)
    })
  }, [list, listQ])

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
  }, [])

  async function ensureUsersAndRoles() {
    if (memberOptsLoaded.current) return
    const [u, r] = await Promise.all([
      api.get('/users', { silent: true }),
      api.get('/roles', { silent: true }),
    ])
    setUsers(u.data?.data || [])
    setRoles(r.data?.data || [])
    memberOptsLoaded.current = true
  }

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
    const [, membersRes] = await Promise.all([
      ensureUsersAndRoles(),
      api.get(`/tenants/${t.id}/members`),
    ])
    setMembers(membersRes.data.data || [])
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
    setGrantSelectedKeys([])
    grantForm.setFieldsValue({
      grant_target_mode: 'tenant',
      resource_type: 'instance',
      resource_ids: [],
      to_tenant_id: undefined,
      to_user_id: undefined,
      permission: 'read',
    })
    const [, grantsRes] = await Promise.all([
      ensureUsersAndRoles(),
      api.get(`/tenants/${t.id}/grants`),
    ])
    const rows = grantsRes.data.data || []
    setGrants(rows)
    await loadGrantResources(t.id, 'instance')
    await warmGrantLabels(t.id, rows)
  }

  async function saveGrant() {
    if (!current) return
    const v = await grantForm.validateFields()
    const ids = (Array.isArray(v.resource_ids) ? v.resource_ids : []).map((x: string) => String(x || '').trim()).filter(Boolean)
    if (!ids.length) {
      message.warning('请选择资源')
      return
    }
    const typ = v.resource_type || 'instance'
    const mode = v.grant_target_mode === 'user' ? 'user' : 'tenant'
    const payload: Record<string, unknown> = {
      resource_type: typ,
      resource_ids: ids,
      permission: v.permission || 'read',
    }
    if (mode === 'user') {
      payload.to_user_id = v.to_user_id
    } else {
      payload.to_tenant_id = v.to_tenant_id
    }
    try {
      const { data } = await api.post(`/tenants/${current.id}/grants/batch`, payload)
      const created = Number(data.created || 0)
      const skipped = Number(data.skipped || 0)
      const failed = Array.isArray(data.failed) ? data.failed : []
      if (failed.length) {
        message.warning(`成功 ${created}，跳过 ${skipped}，失败 ${failed.length}`)
      } else if (created === 0 && skipped > 0) {
        message.info(
          mode === 'user'
            ? `所选资源对该用户均已覆盖（已有用户授权或所属租户已授权，跳过 ${skipped}）`
            : `所选资源均已授权（跳过 ${skipped}）`,
        )
      } else {
        message.success(`已授权 ${created} 项${skipped ? `，跳过已覆盖 ${skipped}` : ''}`)
      }
      grantForm.setFieldsValue({
        grant_target_mode: mode,
        resource_type: typ,
        resource_ids: [],
        to_tenant_id: mode === 'tenant' ? undefined : v.to_tenant_id,
        to_user_id: mode === 'user' ? undefined : v.to_user_id,
        permission: 'read',
      })
      const listRes = await api.get(`/tenants/${current.id}/grants`)
      setGrants(listRes.data.data || [])
      await loadGrantResources(current.id, typ)
      await warmGrantLabels(current.id, listRes.data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '批量授权失败')
    }
  }

  async function revokeSelectedGrants() {
    if (!current || !grantSelectedKeys.length) return
    const ids = grantSelectedKeys.map(String)
    Modal.confirm({
      title: '撤销授权',
      content: `确认撤销已选 ${ids.length} 条授权？`,
      okText: '撤销',
      okButtonProps: { danger: true },
      async onOk() {
        const { data } = await api.post(`/tenants/${current.id}/grants/batch-delete`, { ids })
        const deleted = Number(data.deleted || 0)
        const failed = Array.isArray(data.failed) ? data.failed : []
        if (failed.length) {
          message.warning(`已撤销 ${deleted}，失败 ${failed.length}`)
        } else {
          message.success(`已撤销 ${deleted} 条`)
        }
        setGrantSelectedKeys([])
        const listRes = await api.get(`/tenants/${current.id}/grants`)
        setGrants(listRes.data.data || [])
      },
    })
  }

  async function openAudit(t: Tenant) {
    setCurrent(t)
    setAuditOpen(true)
    const { data } = await api.get(`/tenants/${t.id}/audit-logs`)
    setAudits(data.data || [])
  }

  async function impersonate(t: Tenant) {
    const epoch = beginAuthReissue()
    try {
      const { data } = await api.post(`/tenants/${t.id}/impersonate`)
      if (epoch !== getAuthEpoch()) {
        abortAuthReissue()
        return
      }
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      if (!finishAuthReissue(epoch)) return
      message.success(`已进入租户模式：${t.name}（下次可直接用顶栏「进入租户模式」）`)
      location.hash = '#/'
      location.reload()
    } catch (e: any) {
      abortAuthReissue()
      message.error(e?.response?.data?.message || '进入租户模式失败')
    }
  }

  function currentSessionTenantId() {
    try {
      const u = JSON.parse(localStorage.getItem('user') || '{}')
      return String(u?.current_tenant?.id || u?.tenant_id || '')
    } catch {
      return ''
    }
  }

  /** 删除后同步顶栏租户列表；若当前会话租户被删则自动切到仍可用的租户（优先默认） */
  async function refreshAuthAfterTenantChange(removedId?: string) {
    try {
      const { data } = await api.get('/auth/me', { silent: true })
      const u = data?.data || data?.user || {}
      if (!u || typeof u !== 'object') return
      let tenants: Tenant[] = Array.isArray(u.tenants) ? [...u.tenants] : []
      if (removedId) {
        tenants = tenants.filter((t) => t?.id !== removedId)
        u.tenants = tenants
      }
      const curId = String(u.current_tenant?.id || u.tenant_id || '')
      const currentGone = Boolean(removedId) && (curId === removedId || !u.current_tenant)
      if (currentGone) {
        const next =
          tenants.find((t) => t.id === 'tenant-default' || t.code === 'default') || tenants[0]
        if (next?.id) {
          const epoch = beginAuthReissue()
          try {
            const sw = await api.post('/tenants/switch', { tenant_id: next.id })
            if (epoch !== getAuthEpoch()) {
              abortAuthReissue()
              return
            }
            localStorage.setItem('token', sw.data.token)
            localStorage.setItem('user', JSON.stringify(sw.data.user))
            if (!finishAuthReissue(epoch)) return
            dispatch(setUser(sw.data.user))
            message.success(`已切换到 ${sw.data.user?.current_tenant?.name || next.name}`)
            location.hash = '#/'
            location.reload()
          } catch (e: any) {
            abortAuthReissue()
            throw e
          }
          return
        }
        localStorage.removeItem('token')
        localStorage.setItem('user', JSON.stringify({ ...u, current_tenant: undefined }))
        location.hash = '#/select-tenant'
        location.reload()
        return
      }
      localStorage.setItem('user', JSON.stringify(u))
      dispatch(setUser(u))
    } catch {
      /* ignore */
    }
  }

  function confirmDelete(t: Tenant) {
    Modal.confirm({
      title: `删除租户「${t.name}」？`,
      content: '仅当该租户无成员且未绑定业务资源时可删除。有资源时请先划拨释放。',
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.delete(`/tenants/${t.id}`, { silent: true })
          message.success('已删除')
          if (currentSessionTenantId() === t.id) {
            await refreshAuthAfterTenantChange(t.id)
            return
          }
          await load()
          await refreshAuthAfterTenantChange(t.id)
        } catch (err: any) {
          const data = err?.response?.data || {}
          const msg = String(data.message || '删除失败')
          const summary = formatResourceSummary(data.resources)
          if (summary) {
            message.error(`${msg}：${summary}`)
          } else if (data.members) {
            message.error(`${msg}（当前 ${data.members} 人）`)
          } else {
            message.error(msg)
          }
          throw err
        }
      },
    })
  }

  function moreMenu(r: Tenant): MenuProps {
    const items: MenuProps['items'] = [
      {
        key: 'audit',
        label: '审计',
        onClick: ({ domEvent }) => {
          domEvent.stopPropagation()
          openAudit(r)
        },
      },
    ]
    if (hasUrn(Perm.tenants.impersonate)) {
      items.push({
        key: 'impersonate',
        label: '进入租户模式',
        title: '也可在顶栏点「进入租户模式」快捷进入',
        onClick: ({ domEvent }) => {
          domEvent.stopPropagation()
          impersonate(r)
        },
      })
    }
    if (hasUrn(Perm.tenants.delete) && !isSystemTenant(r)) {
      items.push({ type: 'divider' })
      items.push({
        key: 'delete',
        danger: true,
        label: '删除',
        onClick: ({ domEvent }) => {
          domEvent.stopPropagation()
          confirmDelete(r)
        },
      })
    }
    return { items }
  }

  return (
    <div style={{ padding: 16, width: '100%', maxWidth: '100%', overflow: 'hidden' }}>
      <Space style={{ marginBottom: 16 }} wrap>
        <Auth urn={Perm.tenants.create}>
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            新建租户
          </Button>
        </Auth>
        <Button onClick={load}>刷新</Button>
        <Input.Search
          allowClear
          placeholder="搜索编码/名称"
          style={{ width: 240 }}
          value={listQ}
          onChange={(e) => setListQ(e.target.value)}
        />
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={filteredList}
        tableLayout="fixed"
        style={{ width: '100%' }}
        pagination={{ pageSize: 10, showSizeChanger: true }}
        columns={[
          { title: '编码', dataIndex: 'code', width: 120, ellipsis: true },
          { title: '名称', dataIndex: 'name', width: 140, ellipsis: true },
          {
            title: '状态',
            dataIndex: 'status',
            width: 90,
            render: (s: string) => (
              <Tag color={s === 'active' ? 'green' : 'default'}>{s}</Tag>
            ),
          },
          {
            title: '套餐',
            dataIndex: 'plan',
            width: 90,
            ellipsis: true,
            render: (v: string) => planLabel(v),
          },
          {
            title: '说明',
            dataIndex: 'description',
            ellipsis: { showTitle: false },
            render: (v: string) =>
              v ? (
                <Tooltip title={v} placement="topLeft">
                  <span>{v}</span>
                </Tooltip>
              ) : (
                '-'
              ),
          },
          {
            title: '操作',
            width: 240,
            render: (_: unknown, r: Tenant) => (
              <Space size={4} wrap={false}>
                <Auth urn={Perm.tenants.edit}>
                  <Button type="link" size="small" style={{ paddingInline: 4 }} onClick={() => openEdit(r)}>
                    编辑
                  </Button>
                </Auth>
                <Auth urn={Perm.tenants.members}>
                  <Button type="link" size="small" style={{ paddingInline: 4 }} onClick={() => openMembers(r)}>
                    成员
                  </Button>
                </Auth>
                <Auth urn={Perm.tenants.grants}>
                  <Button type="link" size="small" style={{ paddingInline: 4 }} onClick={() => openGrants(r)}>
                    授权
                  </Button>
                </Auth>
                <Dropdown menu={moreMenu(r)} trigger={['click']}>
                  <Button type="link" size="small" style={{ paddingInline: 4 }}>
                    更多
                  </Button>
                </Dropdown>
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
          setGrantSelectedKeys([])
        }}
        footer={null}
        width={1000}
        destroyOnClose
      >
        <p style={{ marginBottom: 12, color: '#6b7280', fontSize: 13 }}>
          从「{current?.name || '资源方租户'}」选出资源，授权给目标租户或指定用户只读使用（授权仅控制可见与使用，不授予工单审批权；审批人须为资源所属租户成员）。若用户所属租户已有该资源授权，再授权给该用户会自动跳过。下方表格为已创建的授权记录。
        </p>
        <Form
          form={grantForm}
          layout="inline"
          style={{
            marginBottom: 12,
            display: 'flex',
            flexWrap: 'nowrap',
            alignItems: 'center',
            gap: 8,
            overflowX: 'auto',
          }}
          onFinish={saveGrant}
        >
          <Form.Item name="grant_target_mode" initialValue="tenant" style={{ marginInlineEnd: 0 }}>
            <Select
              style={{ width: 120 }}
              options={[
                { value: 'tenant', label: '目标租户' },
                { value: 'user', label: '指定用户' },
              ]}
              onChange={(mode) => {
                grantForm.setFieldsValue({
                  to_tenant_id: undefined,
                  to_user_id: undefined,
                  grant_target_mode: mode,
                })
                if (mode === 'user') void ensureUsersAndRoles()
              }}
            />
          </Form.Item>
          <Form.Item name="resource_type" rules={[{ required: true }]} initialValue="instance" style={{ marginInlineEnd: 0 }}>
            <Select
              style={{ width: 120 }}
              options={GRANT_TYPES}
              onChange={(typ) => {
                grantForm.setFieldsValue({ resource_ids: [] })
                if (current) void loadGrantResources(current.id, typ)
              }}
            />
          </Form.Item>
          <Form.Item
            name="resource_ids"
            rules={[{ required: true, type: 'array', min: 1, message: '请选择资源' }]}
            style={{ marginInlineEnd: 0 }}
          >
            <Select
              mode="multiple"
              style={{ width: 260 }}
              showSearch
              optionFilterProp="label"
              maxTagCount="responsive"
              loading={grantResLoading}
              placeholder={grantResLoading ? '加载中…' : grantResources.length ? '选择资源（可多选）' : '该类型下暂无资源'}
              options={grantResources.map((r) => ({ value: r.id, label: r.label }))}
              notFoundContent={grantResLoading ? '加载中…' : '暂无可选资源'}
              dropdownRender={(menu) => (
                <>
                  <div
                    style={{
                      padding: '4px 8px',
                      display: 'flex',
                      gap: 4,
                      borderBottom: '1px solid #f0f0f0',
                    }}
                    onMouseDown={(e) => e.preventDefault()}
                  >
                    <Button
                      type="link"
                      size="small"
                      disabled={!grantResources.length}
                      onClick={() => grantForm.setFieldsValue({ resource_ids: grantResources.map((r) => r.id) })}
                    >
                      全选
                    </Button>
                    <Button type="link" size="small" onClick={() => grantForm.setFieldsValue({ resource_ids: [] })}>
                      清空
                    </Button>
                  </div>
                  {menu}
                </>
              )}
            />
          </Form.Item>
          {grantTargetMode === 'user' ? (
            <Form.Item name="to_user_id" rules={[{ required: true, message: '请选择目标用户' }]} style={{ marginInlineEnd: 0 }}>
              <Select
                style={{ width: 220 }}
                showSearch
                optionFilterProp="label"
                placeholder="目标用户"
                options={targetUserOptions}
              />
            </Form.Item>
          ) : (
            <Form.Item name="to_tenant_id" rules={[{ required: true, message: '请选择目标租户' }]} style={{ marginInlineEnd: 0 }}>
              <Select
                style={{ width: 200 }}
                showSearch
                optionFilterProp="label"
                placeholder="目标租户"
                options={targetTenantOptions}
              />
            </Form.Item>
          )}
          <Form.Item name="permission" initialValue="read" style={{ marginInlineEnd: 0 }}>
            <Select style={{ width: 90 }} options={[{ value: 'read', label: 'read' }]} />
          </Form.Item>
          <Button type="primary" htmlType="submit">
            授权
          </Button>
        </Form>
        <div style={{ marginBottom: 8 }}>
          <Button danger disabled={!grantSelectedKeys.length} onClick={() => void revokeSelectedGrants()}>
            {grantSelectedKeys.length ? `撤销（${grantSelectedKeys.length}）` : '撤销'}
          </Button>
        </div>
        <Table
          rowKey="id"
          size="small"
          dataSource={grants}
          rowSelection={{
            selectedRowKeys: grantSelectedKeys,
            onChange: (keys) => setGrantSelectedKeys(keys),
          }}
          locale={{ emptyText: '暂无授权记录，请选择资源与目标后点击授权' }}
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
              title: '授权对象',
              key: 'grant_target',
              ellipsis: true,
              render: (_: unknown, row: { to_tenant_id?: string; to_user_id?: string }) => {
                if (row.to_user_id) {
                  return `用户 · ${userLabelById[row.to_user_id] || row.to_user_id}`
                }
                const tid = row.to_tenant_id || ''
                const t = list.find((x) => x.id === tid)
                return `租户 · ${t ? t.name || t.code : tid || '—'}`
              },
            },
            { title: '权限', dataIndex: 'permission', width: 80 },
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
