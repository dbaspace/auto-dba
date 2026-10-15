import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, Input, Modal, Pagination, Radio, Select, Space, Tabs, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useAppSelector } from '../../store'
import './Databases.css'

interface TypeOption {
  value: string
  label: string
}

interface InstanceRow {
  id?: string
  name?: string
  db_type?: string
  domains?: string[]
  domain_name?: string
  approvers?: string[]
}

interface RoleRow {
  id?: string
  name?: string
  code?: string
}

interface LiveDb {
  name: string
  system?: boolean
  registered?: boolean
}

interface DatabaseRow {
  id?: string
  tenant_id?: string
  tenant_name?: string
  db_name?: string
  db_names?: string[]
  instance_id?: string
  instance_name?: string
  domains?: string[]
  tags?: string[]
  visibility?: string
  access_users?: string[]
  role_ids?: string[]
  approvers?: string[]
}

const OTHER = '__other__'

function parseInstanceTypes(raw: string): TypeOption[] {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    const seen = new Set<string>()
    const out: TypeOption[] = []
    for (const it of arr) {
      const value = String(it?.value ?? '').trim()
      if (!value || seen.has(value) || value === 'kafka' || value === 'redis') continue
      seen.add(value)
      out.push({ value, label: String(it?.label || value).trim() || value })
    }
    return out
  } catch {
    return []
  }
}

function emptyMeta(): Partial<DatabaseRow> {
  return { visibility: 'global', access_users: [], role_ids: [], tags: [], domains: [] }
}

function defaultApprovers(inst?: InstanceRow): string[] {
  const fromInst = (inst?.approvers || []).map((s) => String(s).trim()).filter(Boolean)
  return fromInst.length ? fromInst : ['admin']
}

export default function Databases() {
  const user = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenant = (user as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = String(currentTenant?.id || '')
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([])
  const [activeType, setActiveType] = useState('')
  const [instances, setInstances] = useState<InstanceRow[]>([])
  const [rows, setRows] = useState<DatabaseRow[]>([])
  const [dbNameQ, setDbNameQ] = useState('')
  const [visible, setVisible] = useState(false)
  const [form, setForm] = useState<DatabaseRow>({})
  const [liveDbs, setLiveDbs] = useState<LiveDb[]>([])
  const [dbLoading, setDbLoading] = useState(false)
  const [liveHint, setLiveHint] = useState('')
  const [showSystemDb, setShowSystemDb] = useState(false)
  const [dbSearch, setDbSearch] = useState('')
  const [userOptions, setUserOptions] = useState<string[]>(['admin'])
  const [roles, setRoles] = useState<RoleRow[]>([])
  const [usedTags, setUsedTags] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [page, setPage] = useState(1)
  const pageSize = 10
  const [tenants, setTenants] = useState<{ id: string; name?: string; code?: string }[]>([])
  const [assignRows, setAssignRows] = useState<DatabaseRow[]>([])
  const [assignTid, setAssignTid] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [selectedKeys, setSelectedKeys] = useState<React.Key[]>([])

  const knownTypes = useMemo(() => new Set(typeOptions.map((t) => t.value)), [typeOptions])

  function instanceType(id?: string) {
    return instances.find((i) => i.id === id)?.db_type || ''
  }

  function matchType(dbType: string, tab: string) {
    if (tab === OTHER) return !!dbType && !knownTypes.has(dbType)
    return dbType === tab
  }

  const visibleDbs = useMemo(() => {
    if (showSystemDb) return liveDbs
    return liveDbs.filter((d) => !d.system || d.registered)
  }, [liveDbs, showSystemDb])

  const matchedSelectable = useMemo(() => {
    const q = dbSearch.trim().toLowerCase()
    return visibleDbs.filter((d) => {
      if (d.registered) return false
      if (!q) return true
      return d.name.toLowerCase().includes(q)
    })
  }, [visibleDbs, dbSearch])

  const selectedDbNames = useMemo(() => new Set(form.db_names || []), [form.db_names])
  const matchedSelectedCount = matchedSelectable.filter((d) => selectedDbNames.has(d.name)).length
  const matchedAllChecked = matchedSelectable.length > 0 && matchedSelectedCount === matchedSelectable.length
  const matchedIndeterminate = matchedSelectedCount > 0 && matchedSelectedCount < matchedSelectable.length

  const instanceDomains = useMemo(() => {
    const inst = instances.find((i) => i.id === form.instance_id)
    if (!inst) return [] as string[]
    const list = [...(inst.domains || [])]
    if (inst.domain_name && !list.includes(inst.domain_name)) list.unshift(inst.domain_name)
    return [...new Set(list.filter(Boolean))]
  }, [instances, form.instance_id])

  const tabs = useMemo(() => {
    const list = typeOptions.map((t) => ({ value: t.value, label: t.label }))
    const hasOther = instances.some((i) => i.db_type && !knownTypes.has(i.db_type))
    if (hasOther) list.push({ value: OTHER, label: '其他' })
    return list
  }, [typeOptions, instances, knownTypes])

  const tabInstances = useMemo(() => {
    if (!activeType) return instances
    return instances.filter((i) => matchType(i.db_type || '', activeType))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instances, activeType, knownTypes])

  const formInstances = useMemo(() => {
    if (form.id) {
      const cur = instances.find((i) => i.id === form.instance_id)
      return cur ? [cur] : tabInstances
    }
    return tabInstances
  }, [form.id, form.instance_id, instances, tabInstances])

  const filteredRows = useMemo(() => {
    const q = dbNameQ.trim().toLowerCase()
    return rows.filter((r) => {
      const dbType = instanceType(r.instance_id)
      if (activeType && !matchType(dbType, activeType)) return false
      if (q && !String(r.db_name || '').toLowerCase().includes(q)) return false
      return true
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, activeType, dbNameQ, instances, knownTypes])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  useEffect(() => {
    setPage(1)
  }, [activeType, dbNameQ])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(filteredRows.length / pageSize) || 1)
    if (page > maxPage) setPage(maxPage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredRows, pageSize])

  function typeCount(value: string) {
    return rows.filter((r) => matchType(instanceType(r.instance_id), value)).length
  }

  function ensureActiveTab(list: { value: string }[], cur: string) {
    const names = list.map((t) => t.value)
    if (!names.includes(cur)) return names[0] || ''
    return cur
  }

  function instanceName(id?: string, fallback?: string) {
    return fallback || instances.find((i) => i.id === id)?.name || id || ''
  }

  function roleNames(ids?: string[]) {
    return (ids || []).map((id) => roles.find((r) => r.id === id)?.name || id).join(',')
  }

  async function loadAclOptions(tenantId?: string) {
    const tid = tenantId || currentTenantId
    const { data } = await api.get('/tenant-acl-options', { params: tid ? { tenant_id: tid } : {} })
    const payload = data.data || {}
    const names = (payload.users || []).map((u: any) => u.username || u.name).filter(Boolean)
    setUserOptions([...new Set(names as string[])])
    setRoles(payload.roles || [])
  }

  async function loadMeta() {
    const [instRes, argRes] = await Promise.all([api.get('/instances'), api.get('/global-args')])
    setInstances(instRes.data.data || [])
    await loadAclOptions(currentTenantId)
    const raw = (argRes.data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    setTypeOptions(parseInstanceTypes(raw))
  }

  async function load() {
    const { data } = await api.get('/databases')
    const list: DatabaseRow[] = data.data || []
    setRows(list)
    setUsedTags((prev) => {
      const tags = new Set(prev)
      for (const row of list) {
        for (const t of row.tags || []) if (t) tags.add(t)
      }
      return [...tags]
    })
  }

  async function loadLiveDbs(id?: string) {
    setLiveDbs([])
    setLiveHint('')
    if (!id) return
    setDbLoading(true)
    try {
      const { data } = await api.get(`/instances/${id}/databases`)
      setLiveDbs(data.data || [])
      setLiveHint(data.live ? '' : data.message || '未能拉取实例库，可手动输入库名')
    } catch (e: any) {
      setLiveHint(e.response?.data?.message || '未能拉取实例库，可手动输入库名')
    } finally {
      setDbLoading(false)
    }
  }

  function onInstanceChange(id?: string) {
    const inst = instances.find((i) => i.id === id)
    setDbSearch('')
    setForm((f) => ({
      ...f,
      instance_id: id,
      db_names: [],
      domains: [],
      approvers: f.id ? f.approvers : defaultApprovers(inst),
    }))
    void loadAclOptions(inst?.tenant_id || currentTenantId)
    loadLiveDbs(id)
  }

  function selectMatchedDbs() {
    const names = matchedSelectable.map((d) => d.name)
    setForm((f) => ({ ...f, db_names: [...new Set([...(f.db_names || []), ...names])] }))
  }

  function deselectMatchedDbs() {
    const remove = new Set(matchedSelectable.map((d) => d.name))
    setForm((f) => ({ ...f, db_names: (f.db_names || []).filter((n) => !remove.has(n)) }))
  }

  function open(row?: DatabaseRow) {
    setDbSearch('')
    if (row) {
      setForm({
        ...emptyMeta(),
        ...row,
        approvers: [...(row.approvers || [])],
        access_users: [...(row.access_users || [])],
        role_ids: [...(row.role_ids || [])],
        tags: [...(row.tags || [])],
        domains: [...(row.domains || [])],
        visibility: row.visibility === 'private' ? 'private' : 'global',
      })
      setLiveDbs([])
      setLiveHint('')
      const inst = instances.find((i) => i.id === row.instance_id)
      void loadAclOptions(row.tenant_id || inst?.tenant_id || currentTenantId)
    } else {
      setForm({
        instance_id: '',
        db_names: [],
        approvers: defaultApprovers(),
        ...emptyMeta(),
      })
      void loadAclOptions(currentTenantId)
      loadLiveDbs()
    }
    setVisible(true)
  }

  function payloadMeta() {
    const approvers = (form.approvers || []).map((s) => String(s).trim()).filter(Boolean)
    const visibility = form.visibility === 'private' ? 'private' : 'global'
    const tags = (form.tags || []).map((s) => String(s).trim()).filter(Boolean)
    const domains = (form.domains || []).map((s) => String(s).trim()).filter(Boolean)
    const access_users = visibility === 'private'
      ? (form.access_users || []).map((s) => String(s).trim()).filter(Boolean)
      : []
    const role_ids = visibility === 'private' ? [...(form.role_ids || [])] : []
    return { approvers, visibility, access_users, role_ids, tags, domains }
  }

  async function save() {
    const meta = payloadMeta()
    if (!meta.domains.length) {
      message.warning('请绑定域名')
      return
    }
    if (!meta.approvers.length) {
      message.warning('请至少选择一名审批人')
      return
    }
    if (meta.visibility === 'private' && !meta.access_users.length && !meta.role_ids.length) {
      message.warning('私有库请至少选择可访问用户或绑定角色')
      return
    }
    setSaving(true)
    try {
      if (form.id) {
        await api.put('/databases/' + form.id, { ...form, ...meta })
        message.success('已保存')
      } else {
        const dbNames = (form.db_names || []).map((s) => String(s).trim()).filter(Boolean)
        if (!form.instance_id || !dbNames.length) {
          message.warning('请选择实例并至少选择一个库')
          return
        }
        const { data } = await api.post('/databases/batch', {
          instance_id: form.instance_id,
          db_names: dbNames,
          ...meta,
        })
        const created = data.data?.created || []
        const skipped = data.data?.skipped || []
        message.success(
          skipped.length
            ? `已登记 ${created.length} 个库，跳过已存在 ${skipped.length} 个`
            : `已登记 ${created.length} 个库`,
        )
      }
      const savedType = instanceType(form.instance_id)
      setVisible(false)
      await load()
      if (savedType) setActiveType(savedType)
    } finally {
      setSaving(false)
    }
  }

  function openAssign(rows: DatabaseRow[]) {
    const list = rows.filter((r) => r.id)
    if (!list.length) {
      message.warning('请先勾选库')
      return
    }
    setAssignTid(currentTenantId)
    setAssignRows(list)
  }

  async function confirmAssign() {
    if (!assignRows.length || !assignTid) {
      message.warning('请选择目标租户')
      return
    }
    const ids = assignRows.map((r) => r.id!).filter(Boolean)
    setAssigning(true)
    try {
      const { data } = await api.post('/databases/assign-tenant', { tenant_id: assignTid, ids })
      const label =
        tenants.find((t) => t.id === assignTid)?.name || tenants.find((t) => t.id === assignTid)?.code || assignTid
      const okCount = Number(data.ok_count || 0)
      const failCount = Number(data.fail_count || 0)
      const failed: { id?: string; db_name?: string; message?: string }[] = data.failed || []
      if (failCount === 0) {
        message.success(`已划拨 ${okCount} 个库到 ${label}`)
      } else {
        Modal.warning({
          title: `划拨完成：成功 ${okCount}，失败 ${failCount}`,
          content: (
            <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
              {failed.map((f) => (
                <li key={f.id || f.db_name}>
                  {f.db_name || f.id}：{f.message || '失败'}
                </li>
              ))}
            </ul>
          ),
        })
      }
      setAssignRows([])
      setSelectedKeys([])
      await load()
      await loadMeta()
    } finally {
      setAssigning(false)
    }
  }

  function remove(row: DatabaseRow) {
    Modal.confirm({
      title: '提示',
      content: '删除该登记库？',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/databases/' + row.id)
        await load()
      },
    })
  }

  useEffect(() => {
    ;(async () => {
      await loadMeta()
      await load()
      if (impersonating) {
        try {
          const { data } = await api.get('/tenants')
          setTenants((data.data || []).filter((t: { status?: string }) => t.status !== 'disabled'))
        } catch {
          setTenants([])
        }
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    setActiveType((cur) => ensureActiveTab(tabs, cur))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabs])

  const visibility = form.visibility === 'private' ? 'private' : 'global'

  return (
    <div className="ops-page">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">库名：</span>
          <Input
            value={dbNameQ}
            placeholder="请输入库名"
            allowClear
            onChange={(e) => setDbNameQ(e.target.value)}
            onPressEnter={load}
          />
        </div>
        <div className="toolbar" style={{ margin: 0, flexShrink: 0 }}>
          <Button type="primary" onClick={load}>
            查询
          </Button>
          <Button type="primary" onClick={() => open()}>
            登记库
          </Button>
          {impersonating ? (
            <Auth urn={Perm.tenants.impersonate}>
              <Button
                disabled={!selectedKeys.length}
                onClick={() => {
                  const set = new Set(selectedKeys.map(String))
                  openAssign(filteredRows.filter((r) => r.id && set.has(r.id)))
                }}
              >
                批量指定租户{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
              </Button>
            </Auth>
          ) : null}
        </div>
      </div>
      {impersonating ? (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={`审计中：可指定库的归属租户（单一归属）。实例地址可留在原租户；跨租户共用请用授权（只读），不要多租户共编同一行。`}
        />
      ) : null}
      <Tabs
        className="inst-tabs"
        activeKey={activeType}
        onChange={(k) => {
          setActiveType(k)
          setDbNameQ('')
          setPage(1)
          setSelectedKeys([])
        }}
        items={tabs.map((t) => ({
          key: t.value,
          label: (
            <span className="tab-label">
              <DbTypeIcon type={t.value} size={16} />
              <span>
                {t.label} ({typeCount(t.value)})
              </span>
            </span>
          ),
        }))}
      />
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.db_name || ''}
          dataSource={pagedRows}
          bordered
          pagination={false}
          scroll={{ y: 'calc(100% - 47px)' }}
          style={{ height: '100%' }}
          rowSelection={
            impersonating
              ? {
                  selectedRowKeys: selectedKeys,
                  onChange: (keys) => setSelectedKeys(keys),
                  getCheckboxProps: (row: DatabaseRow) => ({ disabled: !row.id }),
                }
              : undefined
          }
          columns={[
            { title: '库名', dataIndex: 'db_name', width: 140 },
            ...(impersonating
              ? [
                  {
                    title: '所属租户',
                    width: 120,
                    render: (_: unknown, row: DatabaseRow) => {
                      const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                      const label = row.tenant_name || row.tenant_id || '—'
                      return same ? <Tag color="green">{label}</Tag> : <Tag color="orange">{label}</Tag>
                    },
                  },
                ]
              : []),
            {
              title: '绑定域名',
              width: 180,
              render: (_, row) => (row.domains || []).join(', ') || '—',
            },
            {
              title: '标签',
              width: 140,
              render: (_, row) => {
                const list = row.tags || []
                if (!list.length) return '—'
                return list.map((t) => (
                  <Tag key={t} className="cell-tag">
                    {t}
                  </Tag>
                ))
              },
            },
            {
              title: '范围',
              width: 80,
              render: (_, row) => (row.visibility === 'private' ? '私有' : '全局'),
            },
            {
              title: '可访问用户',
              width: 140,
              render: (_, row) =>
                row.visibility === 'private' ? (row.access_users || []).join(',') || '—' : '全部',
            },
            {
              title: '绑定角色',
              width: 140,
              render: (_, row) => (row.visibility === 'private' ? roleNames(row.role_ids) || '—' : '—'),
            },
            { title: '实例', width: 120, render: (_, row) => instanceName(row.instance_id, row.instance_name) },
            {
              title: '审批人',
              width: 120,
              render: (_, row) => (row.approvers || []).join(','),
            },
            {
              title: '操作',
              width: impersonating ? 280 : 160,
              fixed: 'right',
              render: (_, row) => (
                <Space size={0}>
                  {impersonating ? (
                    <Auth urn={Perm.tenants.impersonate}>
                      <Button type="link" onClick={() => openAssign([row])}>
                        指定租户
                      </Button>
                    </Auth>
                  ) : null}
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
        <span className="pager-total">共 {filteredRows.length} 条</span>
        <Pagination
          current={page}
          pageSize={pageSize}
          total={filteredRows.length}
          showSizeChanger={false}
          onChange={(p) => setPage(p)}
        />
      </div>

      <Modal
        open={assignRows.length > 0}
        title={assignRows.length > 1 ? `批量指定库归属租户（${assignRows.length}）` : '指定库归属租户'}
        okText="划拨"
        confirmLoading={assigning}
        onCancel={() => !assigning && setAssignRows([])}
        onOk={confirmAssign}
      >
        <p style={{ marginBottom: 12 }}>
          {assignRows.length === 1 ? (
            <>
              将库「{assignRows[0].db_name}」（实例{' '}
              {instanceName(assignRows[0].instance_id, assignRows[0].instance_name)}
              ）划到指定租户。实例地址不迁；该库账号随库走。其它租户要用请走授权（只读）。
            </>
          ) : (
            <>
              将以下 {assignRows.length} 个库划到同一目标租户。实例地址不迁；各库账号随库走。其它租户要用请走授权（只读）。
              <ul style={{ margin: '8px 0 0', paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
                {assignRows.map((r) => (
                  <li key={r.id}>
                    {r.db_name}（{instanceName(r.instance_id, r.instance_name)}）
                  </li>
                ))}
              </ul>
            </>
          )}
        </p>
        <Select
          style={{ width: '100%' }}
          showSearch
          optionFilterProp="label"
          value={assignTid || undefined}
          onChange={(v) => setAssignTid(v)}
          options={tenants.map((t) => ({
            value: t.id,
            label: `${t.name || t.code || t.id}${t.id === currentTenantId ? '（当前审计）' : ''}`,
          }))}
        />
      </Modal>
      <Modal
        open={visible}
        title="登记库"
        width={640}
        destroyOnClose
        centered
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
        <div className="db-form">
          <div className="fi">
            <label>实例</label>
            <Select
              value={form.instance_id || undefined}
              showSearch
              disabled={!!form.id}
              style={{ width: '100%' }}
              placeholder="请选择实例"
              optionFilterProp="label"
              options={formInstances.map((i) => ({ value: i.id as string, label: i.name }))}
              onChange={(v) => onInstanceChange(v)}
            />
          </div>
          <div className="fi">
            <label>域名</label>
            <div className="fi-body">
              <Select
                mode="multiple"
                value={form.domains || []}
                showSearch
                allowClear
                disabled={!form.instance_id}
                placeholder={
                  form.instance_id
                    ? instanceDomains.length
                      ? '输入关键字搜索并选择域名，不默认填充'
                      : '该实例尚未配置域名'
                    : '请先选择实例'
                }
                style={{ width: '100%' }}
                optionFilterProp="label"
                options={instanceDomains.map((d) => ({ value: d, label: d }))}
                onChange={(v) => setForm((f) => ({ ...f, domains: v }))}
              />
              <div className="type-hint">从该实例已配置的域名中按需选择，至少选一个，支持搜索</div>
            </div>
          </div>
          {form.id ? (
            <div className="fi">
              <label>库名</label>
              <Input value={form.db_name || ''} disabled />
            </div>
          ) : (
            <div className="fi">
              <label>库名</label>
              <div className="fi-body">
                <Select
                  mode="tags"
                  value={form.db_names || []}
                  showSearch
                  allowClear
                  loading={dbLoading}
                  disabled={!form.instance_id}
                  placeholder="输入关键字搜索库名，可多选，也可输入后回车"
                  style={{ width: '100%' }}
                  optionFilterProp="label"
                  searchValue={dbSearch}
                  onSearch={setDbSearch}
                  onDropdownVisibleChange={(open) => {
                    if (!open) setDbSearch('')
                  }}
                  dropdownRender={(menu) => (
                    <>
                      <div className="db-select-match-bar" onMouseDown={(e) => e.preventDefault()}>
                        <Checkbox
                          checked={matchedAllChecked}
                          indeterminate={matchedIndeterminate}
                          disabled={!form.instance_id || matchedSelectable.length === 0}
                          onChange={(e) => {
                            if (e.target.checked) selectMatchedDbs()
                            else deselectMatchedDbs()
                          }}
                        >
                          全选当前匹配 ({matchedSelectable.length})
                        </Checkbox>
                      </div>
                      {menu}
                    </>
                  )}
                  options={visibleDbs.map((d) => ({
                    value: d.name,
                    label: d.registered ? `${d.name}（已登记）` : d.name,
                    disabled: d.registered,
                  }))}
                  onChange={(v) => setForm((f) => ({ ...f, db_names: v }))}
                />
                <div className="type-hint">
                  <Checkbox checked={showSystemDb} onChange={(e) => setShowSystemDb(e.target.checked)}>
                    显示系统库
                  </Checkbox>
                  <span>下拉内可全选当前搜索结果</span>
                  {liveHint && <span className="live-hint">{liveHint}</span>}
                </div>
              </div>
            </div>
          )}
          <div className="fi">
            <label>标签</label>
            <div className="fi-body">
              <Select
                mode="tags"
                value={form.tags || []}
                showSearch
                maxTagCount="responsive"
                placeholder="输入后回车，同名库用标签区分"
                style={{ width: '100%' }}
                optionFilterProp="label"
                options={usedTags.map((t) => ({ value: t, label: t }))}
                onChange={(v) => setForm((f) => ({ ...f, tags: v }))}
              />
              <div className="type-hint">工单选库时会展示为「库名 [标签]」，用于区分相同库名</div>
            </div>
          </div>
          <div className="fi">
            <label>可见范围</label>
            <div className="fi-body">
              <Radio.Group
                value={visibility}
                onChange={(e) => setForm((f) => ({ ...f, visibility: e.target.value }))}
              >
                <Radio.Button value="global">全局</Radio.Button>
                <Radio.Button value="private">私有</Radio.Button>
              </Radio.Group>
              <div className="type-hint">
                全局：本租户内可见。私有：仅可访问用户，以及绑定角色下的本租户成员可见
              </div>
            </div>
          </div>
          {visibility === 'private' && (
            <>
              <div className="fi">
                <label>可访问用户</label>
                <Select
                  mode="multiple"
                  value={form.access_users || []}
                  showSearch
                  maxTagCount="responsive"
                  placeholder="选择本库所属租户成员"
                  style={{ width: '100%' }}
                  optionFilterProp="label"
                  options={userOptions.map((u) => ({ value: u, label: u }))}
                  onChange={(v) => setForm((f) => ({ ...f, access_users: v }))}
                />
              </div>
              <div className="fi">
                <label>绑定角色</label>
                <Select
                  mode="multiple"
                  value={form.role_ids || []}
                  showSearch
                  maxTagCount="responsive"
                  placeholder="绑定后，本租户内该角色成员可见"
                  style={{ width: '100%' }}
                  optionFilterProp="label"
                  options={roles.map((r) => ({ value: r.id as string, label: r.name || r.code }))}
                  onChange={(v) => setForm((f) => ({ ...f, role_ids: v }))}
                />
              </div>
            </>
          )}
          <div className="fi">
            <label>审批人</label>
            <Select
              mode="multiple"
              value={form.approvers || []}
              showSearch
              maxTagCount="responsive"
              placeholder="仅限本库所属租户成员"
              style={{ width: '100%' }}
              optionFilterProp="label"
              options={userOptions.map((u) => ({ value: u, label: u }))}
              onChange={(v) => setForm((f) => ({ ...f, approvers: v }))}
            />
          </div>
        </div>
      </Modal>
    </div>
  )
}
