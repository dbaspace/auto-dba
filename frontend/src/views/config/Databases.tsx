import React, { useEffect, useMemo, useState } from 'react'
import { Button, Checkbox, Input, Modal, Pagination, Radio, Select, Space, Table, Tabs, Tag, message } from 'antd'
import api from '../../api/request'
import DbTypeIcon from '../../components/DbTypeIcon'
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
  db_name?: string
  db_names?: string[]
  instance_id?: string
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
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([])
  const [activeType, setActiveType] = useState('')
  const [instances, setInstances] = useState<InstanceRow[]>([])
  const [rows, setRows] = useState<DatabaseRow[]>([])
  const [instanceId, setInstanceId] = useState<string>('')
  const [visible, setVisible] = useState(false)
  const [form, setForm] = useState<DatabaseRow>({})
  const [liveDbs, setLiveDbs] = useState<LiveDb[]>([])
  const [dbLoading, setDbLoading] = useState(false)
  const [liveHint, setLiveHint] = useState('')
  const [showSystemDb, setShowSystemDb] = useState(false)
  const [userOptions, setUserOptions] = useState<string[]>(['admin'])
  const [roles, setRoles] = useState<RoleRow[]>([])
  const [usedTags, setUsedTags] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [page, setPage] = useState(1)
  const pageSize = 10

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
    return rows.filter((r) => {
      const dbType = instanceType(r.instance_id)
      if (activeType && !matchType(dbType, activeType)) return false
      if (instanceId && r.instance_id !== instanceId) return false
      return true
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, activeType, instanceId, instances, knownTypes])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  useEffect(() => {
    setPage(1)
  }, [activeType, instanceId])

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

  function instanceName(id?: string) {
    return instances.find((i) => i.id === id)?.name || id || ''
  }

  function roleNames(ids?: string[]) {
    return (ids || []).map((id) => roles.find((r) => r.id === id)?.name || id).join(',')
  }

  async function loadMeta() {
    const [instRes, apprRes, roleRes, argRes] = await Promise.all([
      api.get('/instances'),
      api.get('/create-db/approvers'),
      api.get('/role-options'),
      api.get('/global-args'),
    ])
    setInstances(instRes.data.data || [])
    const names = (apprRes.data.data || []).map((a: any) => a.name || a).filter(Boolean)
    setUserOptions([...new Set(['admin', ...names])])
    setRoles(roleRes.data.data || [])
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
    setForm((f) => ({
      ...f,
      instance_id: id,
      db_names: [],
      domains: [],
      approvers: f.id ? f.approvers : defaultApprovers(inst),
    }))
    loadLiveDbs(id)
  }

  function open(row?: DatabaseRow) {
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
    } else {
      const pref = instanceId && matchType(instanceType(instanceId), activeType) ? instanceId : ''
      const inst = instances.find((i) => i.id === pref)
      setForm({
        instance_id: pref,
        db_names: [],
        approvers: defaultApprovers(inst),
        ...emptyMeta(),
      })
      loadLiveDbs(pref)
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
          <span className="label">实例：</span>
          <Select
            value={instanceId || undefined}
            allowClear
            showSearch
            placeholder="请选择实例"
            optionFilterProp="label"
            style={{ minWidth: 220 }}
            options={tabInstances.map((i) => ({ value: i.id as string, label: i.name }))}
            onChange={(v) => setInstanceId(v || '')}
          />
        </div>
        <div className="toolbar" style={{ margin: 0, flexShrink: 0 }}>
          <Button type="primary" onClick={load}>
            查询
          </Button>
          <Button type="primary" onClick={() => open()}>
            登记库
          </Button>
        </div>
      </div>
      <Tabs
        className="inst-tabs"
        activeKey={activeType}
        onChange={(k) => {
          setActiveType(k)
          if (instanceId && !matchType(instanceType(instanceId), k)) {
            setInstanceId('')
          }
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
        <Table
          rowKey={(r) => r.id || r.db_name || ''}
          dataSource={pagedRows}
          bordered
          pagination={false}
          scroll={{ y: 'calc(100% - 47px)' }}
          style={{ height: '100%' }}
          columns={[
            { title: '库名', dataIndex: 'db_name', width: 140 },
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
            { title: '实例', width: 120, render: (_, row) => instanceName(row.instance_id) },
            {
              title: '审批人',
              width: 120,
              render: (_, row) => (row.approvers || []).join(','),
            },
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
              <div className="type-hint">全局：所有人可见。私有：仅可访问用户，以及绑定角色下的用户可见</div>
            </div>
          </div>
          {visibility === 'private' && (
            <>
              <div className="fi">
                <label>可访问用户</label>
                <Select
                  mode="tags"
                  value={form.access_users || []}
                  showSearch
                  maxTagCount="responsive"
                  placeholder="选择或输入用户名"
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
                  placeholder="绑定后，该角色下用户可见"
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
              mode="tags"
              value={form.approvers || []}
              showSearch
              maxTagCount="responsive"
              placeholder="可多选，也可输入后回车"
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
