import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, Dropdown, Input, Modal, Pagination, Popover, Radio, Select, Space, Tabs, Tag, message } from 'antd'
import { SettingOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm, usePermission } from '../../hooks/usePermission'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useAppSelector } from '../../store'
import { splitTypeTabsByDataCount } from '../../utils/typeTabs'
import { filterNamesByMembers } from '../../utils/tenantMembers'
import './Databases.css'

interface TypeOption {
  value: string
  label: string
}

interface InstanceRow {
  id?: string
  name?: string
  db_type?: string
  idc?: string
  domains?: string[]
  domain_name?: string
  approvers?: string[]
  tenant_id?: string
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


/** 从 tags 的 idc= 解析环境；无则回退实例 idc */
function resolveRowEnv(row: DatabaseRow, inst?: InstanceRow): string {
  for (const raw of row.tags || []) {
    const t = String(raw || '').trim()
    const i = t.indexOf('=')
    if (i <= 0) continue
    if (t.slice(0, i).trim().toLowerCase() === 'idc') {
      const v = t.slice(i + 1).trim()
      if (v) return v
    }
  }
  return String(inst?.idc || '').trim()
}

/**
 * 已登记库是明细：每行只展示该库自己的绑定域名。
 * 优先 domains，其次 tags 的 domain=；仅当库本身未绑定时才回退实例主域名（不含实例 domains 全集）。
 */
function resolveRowDomains(row: DatabaseRow, inst?: InstanceRow): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  const add = (raw?: string) => {
    const v = String(raw || '').trim()
    if (!v) return
    const key = v.toLowerCase()
    if (seen.has(key)) return
    seen.add(key)
    out.push(v)
  }
  for (const d of row.domains || []) add(d)
  for (const raw of row.tags || []) {
    const t = String(raw || '').trim()
    const i = t.indexOf('=')
    if (i <= 0) continue
    if (t.slice(0, i).trim().toLowerCase() === 'domain') add(t.slice(i + 1))
  }
  if (!out.length && inst) {
    add(inst.domain_name)
  }
  return out
}

const COLUMN_STORAGE_KEY = 'auto-dba-databases-visible-columns'
const FIXED_COLUMNS = new Set(['db_name', 'actions'])
const COLUMN_OPTIONS: { key: string; label: string; auditOnly?: boolean }[] = [
  { key: 'env', label: '环境' },
  { key: 'domains', label: '绑定域名' },
  { key: 'db_name', label: '库名' },
  { key: 'tenant', label: '所属租户', auditOnly: true },
  { key: 'tags', label: '标签' },
  { key: 'visibility', label: '范围' },
  { key: 'access_users', label: '可访问用户' },
  { key: 'role_ids', label: '绑定角色' },
  { key: 'instance', label: '实例' },
  { key: 'approvers', label: '审批人' },
  { key: 'actions', label: '操作' },
]
const DEFAULT_COLUMNS = COLUMN_OPTIONS.map((c) => c.key)

function loadVisibleColumns(): string[] {
  try {
    const arr = JSON.parse(localStorage.getItem(COLUMN_STORAGE_KEY) || 'null')
    if (Array.isArray(arr)) {
      const known = new Set(DEFAULT_COLUMNS)
      const keys = arr.map(String).filter((k) => known.has(k))
      return [...new Set([...keys, ...FIXED_COLUMNS])]
    }
  } catch {
    /* ignore */
  }
  return DEFAULT_COLUMNS
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
  const [initLoading, setInitLoading] = useState(true)
  const [dbNameQ, setDbNameQ] = useState('')
  const [domainFilter, setDomainFilter] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [envCatalog, setEnvCatalog] = useState<{ code: string; name: string }[]>([])
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
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.databases.edit)
  const canDelete = hasUrn(Perm.databases.delete)
  const [batchMode, setBatchMode] = useState<'approvers' | 'access' | null>(null)
  const [batchRows, setBatchRows] = useState<DatabaseRow[]>([])
  const [batchUsers, setBatchUsers] = useState<string[]>([])
  const [batchOptions, setBatchOptions] = useState<string[]>([])
  const [batchSaving, setBatchSaving] = useState(false)
  const [visibleColumns, setVisibleColumns] = useState<string[]>(loadVisibleColumns)

  function updateVisibleColumns(keys: string[]) {
    const next = [...new Set([...keys, ...FIXED_COLUMNS])]
    setVisibleColumns(next)
    localStorage.setItem(COLUMN_STORAGE_KEY, JSON.stringify(next))
  }

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

  function typeCount(value: string) {
    return rows.filter((r) => matchType(instanceType(r.instance_id), value)).length
  }

  const { visible: visibleTypeTabs, more: moreTypeTabs } = useMemo(
    () => splitTypeTabsByDataCount(tabs, typeCount, activeType),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tabs, rows, instances, knownTypes, activeType],
  )

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

  const envFilterOptions = useMemo(() => {
    const codes = new Map<string, string>()
    for (const e of envCatalog) {
      if (e.code) codes.set(e.code, e.name || e.code)
    }
    for (const r of rows) {
      const inst = instances.find((i) => i.id === r.instance_id)
      const code = resolveRowEnv(r, inst)
      if (code && !codes.has(code)) codes.set(code, code)
    }
    return [...codes.entries()]
      .sort((a, b) => a[0].localeCompare(b[0], 'zh-CN'))
      .map(([value, label]) => ({ value, label: label !== value ? `${label}（${value}）` : value }))
  }, [envCatalog, rows, instances])

  const domainFilterOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of rows) {
      const inst = instances.find((i) => i.id === r.instance_id)
      for (const d of resolveRowDomains(r, inst)) set.add(d)
    }
    return [...set]
      .sort((a, b) => a.localeCompare(b, 'zh-CN'))
      .map((value) => ({ value, label: value }))
  }, [rows, instances])

  const filteredRows = useMemo(() => {
    const q = dbNameQ.trim().toLowerCase()
    const dom = domainFilter.trim().toLowerCase()
    return rows.filter((r) => {
      const inst = instances.find((i) => i.id === r.instance_id)
      const dbType = inst?.db_type || ''
      if (activeType && !matchType(dbType, activeType)) return false
      if (q && !String(r.db_name || '').toLowerCase().includes(q)) return false
      if (envFilter) {
        if (resolveRowEnv(r, inst) !== envFilter) return false
      }
      if (dom) {
        const domains = resolveRowDomains(r, inst)
        if (!domains.some((d) => d.toLowerCase() === dom || d.toLowerCase().includes(dom))) return false
      }
      return true
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, activeType, dbNameQ, domainFilter, envFilter, instances, knownTypes])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  useEffect(() => {
    setPage(1)
    setSelectedKeys([])
  }, [activeType, dbNameQ, domainFilter, envFilter])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(filteredRows.length / pageSize) || 1)
    if (page > maxPage) setPage(maxPage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filteredRows, pageSize])

  function instanceName(id?: string, fallback?: string) {
    return fallback || instances.find((i) => i.id === id)?.name || id || ''
  }

  function roleNames(ids?: string[]) {
    return (ids || []).map((id) => roles.find((r) => r.id === id)?.name || id).join(',')
  }

  function aclParams(tenantId?: string) {
    const tid = tenantId || currentTenantId
    return { params: tid ? { tenant_id: tid } : {} }
  }

  function applyAclOptions(payload: any) {
    const names = (payload?.users || []).map((u: any) => u.username || u.name).filter(Boolean)
    setUserOptions([...new Set(names as string[])])
    setRoles(payload?.roles || [])
  }

  async function loadAclOptions(tenantId?: string) {
    const { data } = await api.get('/tenant-acl-options', aclParams(tenantId))
    applyAclOptions(data.data || {})
  }

  async function loadInitial() {
    const [instRes, argRes, envRes, dbRes] = await Promise.all([
      api.get('/instances'),
      api.get('/global-args'),
      api.get('/environments').catch(() => ({ data: { data: [] } })),
      api.get('/databases'),
    ])
    setInstances(instRes.data.data || [])
    setEnvCatalog(
      (envRes.data.data || [])
        .map((e: any) => ({ code: String(e.code || '').trim(), name: String(e.name || e.code || '').trim() }))
        .filter((e: { code: string }) => e.code),
    )
    const raw = (argRes.data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    setTypeOptions(parseInstanceTypes(raw))
    applyRows(dbRes.data.data || [])
  }

  async function load() {
    const { data } = await api.get('/databases')
    applyRows(data.data || [])
  }

  function applyRows(list: DatabaseRow[]) {
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

  async function open(row?: DatabaseRow) {
    setDbSearch('')
    if (row) {
      const inst = instances.find((i) => i.id === row.instance_id)
      const tid = row.tenant_id || inst?.tenant_id || currentTenantId
      let members: string[] = []
      try {
        const { data } = await api.get('/tenant-acl-options', aclParams(tid))
        applyAclOptions(data.data || {})
        members = ((data.data || {}).users || []).map((u: any) => u.username || u.name).filter(Boolean)
      } catch {
        members = []
      }
      const ap = filterNamesByMembers(row.approvers, members)
      const au = filterNamesByMembers(row.access_users, members)
      const dropped = [...new Set([...ap.dropped, ...au.dropped])]
      if (dropped.length) {
        message.warning(`已忽略非本租户成员：${dropped.join('、')}（保存后从名单去掉；设置其它人会追加到现有合法审核人）`)
      }
      setForm({
        ...emptyMeta(),
        ...row,
        approvers: ap.kept,
        access_users: au.kept,
        role_ids: [...(row.role_ids || [])],
        tags: [...(row.tags || [])],
        domains: [...(row.domains || [])],
        visibility: row.visibility === 'private' ? 'private' : 'global',
      })
      setLiveDbs([])
      setLiveHint('')
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
    const { kept: approvers } = filterNamesByMembers(form.approvers, userOptions)
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

  /** 按勾选 key 从全量 rows 解析；跨页勾选依赖 preserveSelectedRowKeys。 */
  function resolveSelectedRows(keys: React.Key[] = selectedKeys): DatabaseRow[] | null {
    const want = [...new Set(keys.map(String).filter(Boolean))]
    if (!want.length) {
      message.warning('请先勾选库')
      return null
    }
    const byID = new Map(rows.filter((r) => r.id).map((r) => [String(r.id), r]))
    const list: DatabaseRow[] = []
    const missing: string[] = []
    for (const id of want) {
      const row = byID.get(id)
      if (row) list.push(row)
      else missing.push(id)
    }
    if (missing.length) {
      message.warning(`有 ${missing.length} 条勾选已不在当前列表，请重新勾选后再操作`)
      return null
    }
    return list
  }

  function openAssign(rows?: DatabaseRow[]) {
    const list = (rows || resolveSelectedRows() || []).filter((r) => r.id)
    if (!list.length) {
      if (!rows) return
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
      await loadInitial()
    } finally {
      setAssigning(false)
    }
  }

  function rowTenantId(row: DatabaseRow) {
    const inst = instances.find((i) => i.id === row.instance_id)
    return String(row.tenant_id || inst?.tenant_id || currentTenantId || '')
  }

  function sameList(lists: string[][]) {
    if (!lists.length) return [] as string[]
    const key = (l: string[]) => [...l].sort().join('\u0000')
    const first = key(lists[0])
    return lists.every((l) => key(l) === first) ? [...lists[0]] : []
  }

  async function openBatch(mode: 'approvers' | 'access') {
    const list = resolveSelectedRows()
    if (!list?.length) return
    const tids = new Set(list.map(rowTenantId))
    if (tids.size > 1) {
      message.warning('请选择同一租户下的库')
      return
    }
    if (mode === 'access') {
      const globals = list.filter((r) => r.visibility !== 'private')
      if (globals.length) {
        message.warning(`仅可对私有库批量设置可访问用户，所选含全局库：${globals.map((r) => r.db_name).join('、')}`)
        return
      }
    }
    const tid = [...tids][0]
    let names: string[] = []
    try {
      const { data } = await api.get('/tenant-acl-options', { params: tid ? { tenant_id: tid } : {} })
      names = ((data.data || {}).users || []).map((u: any) => u.username || u.name).filter(Boolean)
      setBatchOptions([...new Set(names as string[])])
    } catch {
      return
    }
    if (mode === 'approvers') {
      const orphan = new Set<string>()
      for (const r of list) {
        for (const n of filterNamesByMembers(r.approvers, names).dropped) orphan.add(n)
      }
      if (orphan.size) {
        message.warning(`已忽略非本租户成员：${[...orphan].join('、')}（批量追加不会提交这些人）`)
      }
      // 追加语义：选择器表示要追加的人，不预填现有名单
      setBatchUsers([])
    } else {
      const same = sameList(list.map((r) => r.access_users || []))
      setBatchUsers(filterNamesByMembers(same, names).kept)
    }
    setBatchRows(list)
    setBatchMode(mode)
  }

  function closeBatch() {
    if (batchSaving) return
    setBatchMode(null)
    setBatchRows([])
    setBatchUsers([])
  }

  async function confirmBatch() {
    if (!batchMode || !batchRows.length) return
    const { kept: users, dropped: localDropped } = filterNamesByMembers(batchUsers, batchOptions)
    if (localDropped.length) {
      message.warning(`已忽略非本租户成员：${localDropped.join('、')}`)
    }
    if (batchMode === 'access' && !users.length) {
      const noRole = batchRows.filter((r) => !(r.role_ids || []).length)
      if (noRole.length) {
        message.warning(`私有库须至少保留一个可访问用户或角色：${noRole.map((r) => r.db_name).join('、')}`)
        return
      }
    }
    const ids = batchRows.map((r) => r.id!).filter(Boolean)
    const url = batchMode === 'approvers' ? '/databases/batch-approvers' : '/databases/batch-access-users'
    const body = batchMode === 'approvers' ? { ids, approvers: users } : { ids, access_users: users }
    const label = batchMode === 'approvers' ? '审核人' : '可访问用户'
    setBatchSaving(true)
    try {
      const { data } = await api.post(url, body)
      const okCount = Number(data.ok_count || 0)
      const failCount = Number(data.fail_count || 0)
      const failed: { id?: string; db_name?: string; message?: string }[] = data.failed || []
      const dropped: string[] = Array.isArray(data.dropped) ? data.dropped.map(String) : []
      const selected = ids.length
      const verb = batchMode === 'approvers' ? '追加' : '设置'
      if (failCount === 0 && okCount === selected) {
        message.success(
          `已为 ${okCount} 个库${verb}${label}` +
            (dropped.length ? `（已忽略非成员：${dropped.join('、')}）` : ''),
        )
      } else {
        Modal.warning({
          title: `${verb}${label}完成：勾选 ${selected}，成功 ${okCount}，失败 ${failCount}`,
          content: (
            <div>
              {dropped.length ? <p style={{ marginBottom: 8 }}>已忽略非本租户成员：{dropped.join('、')}</p> : null}
              <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
                {failed.map((f) => (
                  <li key={f.id || f.db_name}>
                    {f.db_name || f.id}：{f.message || '失败'}
                  </li>
                ))}
              </ul>
            </div>
          ),
        })
      }
      setBatchMode(null)
      setBatchRows([])
      setBatchUsers([])
      setSelectedKeys([])
      await load()
    } finally {
      setBatchSaving(false)
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

  function removeSelected() {
    const next = resolveSelectedRows()
    if (!next?.length) return
    Modal.confirm({
      title: `删除 ${next.length} 个登记库`,
      okText: '删除',
      okButtonProps: { danger: true },
      content: (
        <div>
          <p style={{ marginBottom: 8 }}>只删除已登记库，不删除实例。</p>
          <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
            {next.map((r) => (
              <li key={r.id}>{r.db_name || r.id}</li>
            ))}
          </ul>
        </div>
      ),
      onOk: async () => {
        const { data } = await api.post('/databases/batch-delete', { ids: next.map((r) => r.id) })
        const okCount = Number(data.ok_count || 0)
        const failCount = Number(data.fail_count || 0)
        const failed: { id?: string; db_name?: string; message?: string }[] = data.failed || []
        if (failCount === 0) {
          message.success(`已删除 ${okCount} 个库`)
        } else {
          Modal.warning({
            title: `删除完成：成功 ${okCount}，失败 ${failCount}`,
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
        setSelectedKeys([])
        await load()
      },
    })
  }

  useEffect(() => {
    loadInitial().finally(() => setInitLoading(false))
    if (impersonating) {
      api
        .get('/tenants')
        .then(({ data }) => setTenants((data.data || []).filter((t: { status?: string }) => t.status !== 'disabled')))
        .catch(() => setTenants([]))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (initLoading) return
    setActiveType((cur) => {
      if (tabs.some((t) => t.value === cur)) return cur
      return visibleTypeTabs[0]?.value || tabs[0]?.value || ''
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initLoading, tabs, visibleTypeTabs])

  const visibility = form.visibility === 'private' ? 'private' : 'global'

  return (
    <div className="ops-page">
      <div className="ops-filters db-filters">
        <div className="filter-item filter-item-fixed">
          <span className="label">环境：</span>
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="全部环境"
            style={{ width: 160 }}
            popupMatchSelectWidth={false}
            dropdownStyle={{ minWidth: 160, maxWidth: 220 }}
            value={envFilter || undefined}
            onChange={(v) => setEnvFilter(v || '')}
            options={envFilterOptions}
          />
        </div>
        <div className="filter-item filter-item-fixed">
          <span className="label">域名：</span>
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="全部域名"
            style={{ width: 240 }}
            popupMatchSelectWidth={false}
            dropdownStyle={{ minWidth: 240, maxWidth: 360 }}
            value={domainFilter || undefined}
            onChange={(v) => setDomainFilter(v || '')}
            options={domainFilterOptions}
          />
        </div>
        <div className="filter-item filter-item-fixed">
          <span className="label">库名：</span>
          <Input
            value={dbNameQ}
            placeholder="请输入库名"
            allowClear
            style={{ width: 180 }}
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
              <Button disabled={!selectedKeys.length} onClick={() => openAssign()}>
                批量指定租户{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
              </Button>
            </Auth>
          ) : null}
          {canDelete ? (
            <Button danger disabled={!selectedKeys.length} onClick={removeSelected}>
              批量删除{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
            </Button>
          ) : null}
          {canEdit ? (
            <>
              <Button disabled={!selectedKeys.length} onClick={() => openBatch('approvers')}>
                批量设置审核人{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
              </Button>
              <Button disabled={!selectedKeys.length} onClick={() => openBatch('access')}>
                批量设置可访问用户{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
              </Button>
            </>
          ) : null}
          <Popover
            trigger="click"
            placement="bottomRight"
            content={
              <div className="db-col-setting">
                {COLUMN_OPTIONS.filter((c) => !c.auditOnly || impersonating).map((c) => (
                  <Checkbox
                    key={c.key}
                    checked={visibleColumns.includes(c.key)}
                    disabled={FIXED_COLUMNS.has(c.key)}
                    onChange={(e) =>
                      updateVisibleColumns(
                        e.target.checked
                          ? [...visibleColumns, c.key]
                          : visibleColumns.filter((k) => k !== c.key),
                      )
                    }
                  >
                    {c.label}
                  </Checkbox>
                ))}
                <div className="db-col-setting-footer">
                  <Button size="small" type="link" onClick={() => updateVisibleColumns(DEFAULT_COLUMNS)}>
                    恢复默认
                  </Button>
                </div>
              </div>
            }
          >
            <Button
              className="db-col-setting-btn"
              icon={<SettingOutlined />}
              title="自定义列表字段"
              aria-label="自定义列表字段"
            />
          </Popover>
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
      {initLoading ? null : (
      <Tabs
        className="inst-tabs inst-tabs-fixed6"
        activeKey={activeType}
        onChange={(k) => {
          setActiveType(k)
          setPage(1)
          setSelectedKeys([])
        }}
        tabBarExtraContent={
          moreTypeTabs.length ? (
            <Dropdown
              trigger={['click']}
              menu={{
                selectedKeys: moreTypeTabs.some((t) => t.value === activeType) ? [activeType] : [],
                items: moreTypeTabs.map((t) => ({
                  key: t.value,
                  label: (
                    <span className="tab-label">
                      <DbTypeIcon type={t.value} size={16} />
                      <span>
                        {t.label} ({t.count})
                      </span>
                    </span>
                  ),
                })),
                onClick: ({ key }) => {
                  setActiveType(String(key))
                  setPage(1)
                  setSelectedKeys([])
                },
              }}
            >
              <Button type="text" className="inst-tabs-more-btn" aria-label="更多类型">
                …
              </Button>
            </Dropdown>
          ) : null
        }
        items={visibleTypeTabs.map((t) => ({
          key: t.value,
          label: (
            <span className="tab-label">
              <DbTypeIcon type={t.value} size={16} />
              <span>
                {t.label} ({t.count})
              </span>
            </span>
          ),
        }))}
      />
      )}
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.db_name || ''}
          dataSource={pagedRows}
          loading={initLoading}
          bordered
          pagination={false}
          scroll={{ y: 'calc(100% - 47px)' }}
          style={{ height: '100%' }}
          rowSelection={
            impersonating || canEdit || canDelete
              ? {
                  selectedRowKeys: selectedKeys,
                  preserveSelectedRowKeys: true,
                  onChange: (keys) => setSelectedKeys(keys),
                  getCheckboxProps: (row: DatabaseRow) => ({ disabled: !row.id }),
                }
              : undefined
          }
          columns={([
            {
              key: 'env',
              title: '环境',
              width: 110,
              ellipsis: true,
              render: (_, row) => {
                const inst = instances.find((i) => i.id === row.instance_id)
                return resolveRowEnv(row, inst) || '—'
              },
            },
            {
              key: 'domains',
              title: '绑定域名',
              width: 200,
              ellipsis: true,
              render: (_, row) => {
                const inst = instances.find((i) => i.id === row.instance_id)
                const list = resolveRowDomains(row, inst)
                return list.length ? list.join(', ') : '—'
              },
            },
            { key: 'db_name', title: '库名', dataIndex: 'db_name', width: 140, ellipsis: true },
            ...(impersonating
              ? [
                  {
                    key: 'tenant',
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
              key: 'tags',
              title: '标签',
              width: 140,
              ellipsis: true,
              render: (_, row) => (row.tags || []).join(', ') || '—',
            },
            {
              key: 'visibility',
              title: '范围',
              width: 80,
              render: (_, row) => (row.visibility === 'private' ? '私有' : '全局'),
            },
            {
              key: 'access_users',
              title: '可访问用户',
              width: 140,
              ellipsis: true,
              render: (_, row) =>
                row.visibility === 'private' ? (row.access_users || []).join(',') || '—' : '全部',
            },
            {
              key: 'role_ids',
              title: '绑定角色',
              width: 140,
              ellipsis: true,
              render: (_, row) => (row.visibility === 'private' ? roleNames(row.role_ids) || '—' : '—'),
            },
            {
              key: 'instance',
              title: '实例',
              width: 120,
              ellipsis: true,
              render: (_, row) => instanceName(row.instance_id, row.instance_name),
            },
            {
              key: 'approvers',
              title: '审批人',
              width: 120,
              ellipsis: true,
              render: (_, row) => (row.approvers || []).join(','),
            },
            {
              key: 'actions',
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
          ] as ColumnsType<DatabaseRow>).filter((c) => visibleColumns.includes(String(c.key)))}
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
        open={!!batchMode}
        title={`${batchMode === 'approvers' ? '批量设置审核人' : '批量设置可访问用户'}（${batchRows.length}）`}
        okText="保存"
        confirmLoading={batchSaving}
        onCancel={closeBatch}
        onOk={confirmBatch}
        destroyOnClose
      >
        <p style={{ marginBottom: 8 }}>
          {batchMode === 'approvers' ? (
            <>
              将所选审核人<strong>追加</strong>到各库现有名单（并集）；已不在本租户的会自动去掉；不会删除仍有效的审核人。
              留空表示不追加新人（仍会清理各库上的非本租户成员）。候选人仅限该租户成员。
            </>
          ) : (
            <>
              以下库的可访问用户将被<strong>覆盖</strong>为所选名单，候选人仅限该租户成员。库的可见范围与绑定角色不变。
            </>
          )}
        </p>
        <ul style={{ margin: '0 0 12px', paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
          {batchRows.map((r) => (
            <li key={r.id}>
              {r.db_name}（{instanceName(r.instance_id, r.instance_name)}）
            </li>
          ))}
        </ul>
        <Select
          mode="multiple"
          style={{ width: '100%' }}
          placeholder={batchMode === 'approvers' ? '选择要追加的审核人' : '选择可访问用户'}
          value={batchUsers}
          onChange={(v) => setBatchUsers(v)}
          options={batchOptions.map((u) => ({ value: u, label: u }))}
          showSearch
          optionFilterProp="label"
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
                placeholder="如 alias=账单ODS ，回车添加"
                style={{ width: '100%' }}
                optionFilterProp="label"
                options={usedTags.map((t) => ({ value: t, label: t }))}
                onChange={(v) => setForm((f) => ({ ...f, tags: v }))}
              />
              <div className="type-hint">
                格式 <code>key=value</code>。约定：
                <code>alias=</code>（或兼容 <code>label=</code>）为查询树短名，同环境同库名时用来区分实例/业务；
                <code>idc=</code> 环境；
                <code>domain=</code> 访问域名。
                查询树只显示 alias/label，不会把 idc、domain 整串挂在库名后。
              </div>
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
