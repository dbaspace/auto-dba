import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Dropdown, Input, Modal, Pagination, Select, Tabs, Tag, message } from 'antd'
import dayjs from 'dayjs'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm, usePermission } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'
import {
  parseInstanceTypes,
  type InstanceTypeOption,
} from '../../utils/instanceTypes'
import { splitTypeTabsByDataCount } from '../../utils/typeTabs'
import './Databases.css'

type TypeOption = InstanceTypeOption

interface DomainRow {
  id?: string
  tenant_id?: string
  tenant_name?: string
  instance_id?: string
  instance_name?: string
  domain?: string
  env_code?: string
  db_type?: string
  created_at?: string
  approvers?: string[]
}

const OTHER = '__other__'

function typeKey(t?: string) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}

function domainCreatedAt(v?: string) {
  const t = Date.parse(v || '')
  return Number.isNaN(t) ? 0 : t
}

function formatDomainTime(v?: string) {
  if (!v) return '—'
  const d = dayjs(v)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : v
}

export default function AccessDomains() {
  const user = useAppSelector((s) => s.auth.user)
  const { hasUrn } = usePermission()
  const canDelete = hasUrn(Perm.domains.delete)
  const canEdit = hasUrn(Perm.domains.edit)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenant = (user as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = String(currentTenant?.id || '')
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([])
  const [activeType, setActiveType] = useState('')
  const [rows, setRows] = useState<DomainRow[]>([])
  const [q, setQ] = useState('')
  const [envFilter, setEnvFilter] = useState('')
  const [tenantFilter, setTenantFilter] = useState('')
  const [envCatalog, setEnvCatalog] = useState<{ code: string; name: string }[]>([])
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [page, setPage] = useState(1)
  const pageSize = 10
  const [tenants, setTenants] = useState<{ id: string; name?: string; code?: string }[]>([])
  const [assignRows, setAssignRows] = useState<DomainRow[]>([])
  const [assignTid, setAssignTid] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [selectedKeys, setSelectedKeys] = useState<React.Key[]>([])
  const [deleting, setDeleting] = useState(false)
  const [batchMode, setBatchMode] = useState<'set' | 'revoke' | ''>('')
  const [batchRows, setBatchRows] = useState<DomainRow[]>([])
  const [batchNames, setBatchNames] = useState<string[]>([])
  const [memberOptions, setMemberOptions] = useState<{ value: string; label: string }[]>([])
  const [memberLoading, setMemberLoading] = useState(false)
  const [memberError, setMemberError] = useState('')
  const [savingApprovers, setSavingApprovers] = useState(false)

  const knownTypes = useMemo(() => new Set(typeOptions.map((t) => typeKey(t.value))), [typeOptions])

  const tabs = useMemo(() => {
    const list = typeOptions.map((t) => ({ value: t.value, label: t.label }))
    if (rows.some((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))) {
      list.push({ value: OTHER, label: '其他' })
    }
    return list
  }, [typeOptions, rows, knownTypes])

  function typeCount(value: string) {
    return rows.filter((r) =>
      value === OTHER
        ? !!(r.db_type && !knownTypes.has(typeKey(r.db_type)))
        : typeKey(r.db_type) === typeKey(value),
    ).length
  }

  const { visible: visibleTypeTabs, more: moreTypeTabs } = useMemo(
    () => splitTypeTabsByDataCount(tabs, typeCount, activeType),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tabs, rows, knownTypes, activeType],
  )

  const envFilterOptions = useMemo(() => {
    const codes = new Map<string, string>()
    for (const e of envCatalog) {
      if (e.code) codes.set(e.code, e.name || e.code)
    }
    for (const r of rows) {
      const code = String(r.env_code || '').trim()
      if (code && !codes.has(code)) codes.set(code, code)
    }
    return [...codes.entries()]
      .sort((a, b) => a[0].localeCompare(b[0], 'zh-CN'))
      .map(([value, label]) => ({ value, label: label !== value ? `${label}（${value}）` : value }))
  }, [envCatalog, rows])

  const tenantFilterOptions = useMemo(() => {
    const map = new Map<string, string>()
    for (const t of tenants) {
      const id = String(t.id || '').trim()
      if (!id) continue
      const base = String(t.name || t.code || id).trim() || id
      map.set(id, id === currentTenantId ? `${base}（当前）` : base)
    }
    for (const r of rows) {
      const id = String(r.tenant_id || '').trim()
      if (!id || map.has(id)) continue
      const base = String(r.tenant_name || id).trim() || id
      map.set(id, id === currentTenantId ? `${base}（当前）` : base)
    }
    return [...map.entries()]
      .sort((a, b) => a[1].localeCompare(b[1], 'zh-CN'))
      .map(([value, label]) => ({ value, label }))
  }, [tenants, rows, currentTenantId])

  const filteredRows = useMemo(() => {
    let list = rows
    if (activeType === OTHER) {
      list = list.filter((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))
    } else if (activeType) {
      list = list.filter((r) => typeKey(r.db_type) === typeKey(activeType))
    }
    if (envFilter) {
      list = list.filter((r) => String(r.env_code || '').trim() === envFilter)
    }
    if (tenantFilter) {
      list = list.filter((r) => String(r.tenant_id || '').trim() === tenantFilter)
    }
    const kw = q.trim().toLowerCase()
    if (kw) {
      list = list.filter(
        (r) =>
          (r.domain || '').toLowerCase().includes(kw) ||
          (r.instance_id || '').toLowerCase().includes(kw) ||
          (r.instance_name || '').toLowerCase().includes(kw) ||
          (r.env_code || '').toLowerCase().includes(kw) ||
          (r.approvers || []).some((a) => a.toLowerCase().includes(kw)),
      )
    }
    return [...list].sort((a, b) => {
      const d = domainCreatedAt(b.created_at) - domainCreatedAt(a.created_at)
      if (d !== 0) return d
      return String(a.domain || '').localeCompare(String(b.domain || ''))
    })
  }, [rows, activeType, knownTypes, q, envFilter, tenantFilter])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page])

  useEffect(() => {
    setPage(1)
    setSelectedKeys([])
  }, [activeType, q, envFilter, tenantFilter])

  async function loadTypes() {
    const { data } = await api.get('/global-args')
    const raw = (data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    setTypeOptions(parseInstanceTypes(raw))
  }

  async function load() {
    setLoading(true)
    try {
      const [domRes, envRes] = await Promise.all([
        api.get('/access-domains'),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
      ])
      setRows(domRes.data.data || [])
      setEnvCatalog(
        (envRes.data.data || [])
          .map((e: any) => ({ code: String(e.code || '').trim(), name: String(e.name || e.code || '').trim() }))
          .filter((e: { code: string }) => e.code),
      )
      setSelectedKeys([])
    } finally {
      setLoading(false)
    }
  }

  async function syncAll() {
    setSyncing(true)
    try {
      const body: { db_type?: string } = {}
      if (activeType && activeType !== OTHER) body.db_type = typeKey(activeType)
      const { data } = await api.post('/access-domains/sync', body)
      const r = data.data || {}
      message.success(
        `同步完成：新建 ${r.created || 0}，更新 ${r.updated || 0}，删除 ${r.deleted || 0}` +
          (body.db_type ? `（仅 ${body.db_type}）` : '（全部类型）'),
      )
      setPage(1)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  function openAssign(list: DomainRow[]) {
    const next = list.filter((r) => r.id)
    if (!next.length) {
      message.warning('请先勾选域名')
      return
    }
    setAssignTid(currentTenantId)
    setAssignRows(next)
  }

  async function confirmAssign() {
    if (!assignRows.length || !assignTid) {
      message.warning('请选择目标租户')
      return
    }
    const ids = assignRows.map((r) => r.id!).filter(Boolean)
    setAssigning(true)
    try {
      const label =
        tenants.find((t) => t.id === assignTid)?.name ||
        tenants.find((t) => t.id === assignTid)?.code ||
        assignTid
      // 单条走 /:id/assign-tenant（与已启动后端兼容）；多条走批量接口
      if (ids.length === 1) {
        await api.post(`/access-domains/${ids[0]}/assign-tenant`, { tenant_id: assignTid })
        message.success(`已划拨到 ${label}`)
      } else {
        const { data } = await api.post('/access-domains/assign-tenant', { tenant_id: assignTid, ids })
        const okCount = Number(data.ok_count || 0)
        const failCount = Number(data.fail_count || 0)
        const failed: { id?: string; domain?: string; db_type?: string; message?: string }[] = data.failed || []
        if (failCount === 0) {
          message.success(`已划拨 ${okCount} 个域名到 ${label}`)
        } else {
          Modal.warning({
            title: `划拨完成：成功 ${okCount}，失败 ${failCount}`,
            content: (
              <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
                {failed.map((f) => (
                  <li key={f.id || f.domain}>
                    {f.domain || f.id}
                    {f.db_type ? `（${f.db_type}）` : ''}：{f.message || '失败'}
                  </li>
                ))}
              </ul>
            ),
          })
        }
      }
      setAssignRows([])
      setSelectedKeys([])
      await load()
    } finally {
      setAssigning(false)
    }
  }

  function removeRows(list: DomainRow[]) {
    const next = list.filter((r) => r.id)
    if (!next.length) {
      message.warning('请先勾选域名')
      return
    }
    Modal.confirm({
      title: next.length > 1 ? `删除 ${next.length} 个域名` : '删除域名',
      okText: '删除',
      okButtonProps: { danger: true },
      content: (
        <div>
          <p style={{ marginBottom: 8 }}>
            只删除域名记录，不删除实例和已登记库。关联实例的域名列表会去掉这一条，下次一键同步不会再加回来。
          </p>
          <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
            {next.map((r) => (
              <li key={r.id}>
                {r.domain}（{r.db_type || '—'}）
              </li>
            ))}
          </ul>
        </div>
      ),
      onOk: async () => {
        setDeleting(true)
        try {
          const ids = next.map((r) => r.id!).filter(Boolean)
          if (ids.length === 1) {
            await api.delete(`/access-domains/${ids[0]}`)
            message.success('已删除')
          } else {
            const { data } = await api.post('/access-domains/batch-delete', { ids })
            const okCount = Number(data.ok_count || 0)
            const failCount = Number(data.fail_count || 0)
            const failed: { id?: string; domain?: string; db_type?: string; message?: string }[] = data.failed || []
            if (failCount === 0) {
              message.success(`已删除 ${okCount} 个域名`)
            } else {
              Modal.warning({
                title: `删除完成：成功 ${okCount}，失败 ${failCount}`,
                content: (
                  <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
                    {failed.map((f) => (
                      <li key={f.id || f.domain}>
                        {f.domain || f.id}
                        {f.db_type ? `（${f.db_type}）` : ''}：{f.message || '失败'}
                      </li>
                    ))}
                  </ul>
                ),
              })
            }
          }
          setSelectedKeys([])
          await load()
        } finally {
          setDeleting(false)
        }
      },
    })
  }

  const redisTab = typeKey(activeType) === 'redis'

  function selectedDomainRows() {
    const set = new Set(selectedKeys.map(String))
    return filteredRows.filter((r) => r.id && set.has(r.id))
  }

  async function loadTenantMembers(tid: string) {
    if (!tid) {
      setMemberOptions([])
      setMemberError('该域名没有所属租户，无法列出成员')
      return
    }
    setMemberLoading(true)
    setMemberError('')
    try {
      const { data } = await api.get(`/tenants/${tid}/members`, { silent: true })
      const opts: { value: string; label: string }[] = []
      const seen = new Set<string>()
      for (const m of data?.data || []) {
        if (String(m?.status || '').toLowerCase() !== 'active') continue
        const username = String(m?.username || '').trim()
        const key = username.toLowerCase()
        if (!username || seen.has(key)) continue
        seen.add(key)
        const display = String(m?.display_name || '').trim()
        opts.push({ value: username, label: display ? `${display}（${username}）` : username })
      }
      setMemberOptions(opts)
    } catch (e: any) {
      setMemberOptions([])
      setMemberError(e?.response?.data?.message || e?.message || '加载租户成员失败')
    } finally {
      setMemberLoading(false)
    }
  }

  function openApproverBatch(mode: 'set' | 'revoke') {
    const picked = selectedDomainRows()
    if (!picked.length) {
      message.warning('请先勾选域名')
      return
    }
    const tids = new Set(picked.map((r) => String(r.tenant_id || '').trim()))
    const tid = [...tids][0]
    if (tids.size !== 1 || !tid) {
      message.warning('请选择同一租户下的域名')
      return
    }
    setBatchMode(mode)
    setBatchRows(picked)
    setBatchNames([])
    setMemberError('')
    if (mode === 'set') {
      void loadTenantMembers(tid)
      return
    }
    const seen = new Set<string>()
    const opts: { value: string; label: string }[] = []
    for (const row of picked) {
      for (const raw of row.approvers || []) {
        const name = String(raw || '').trim()
        const key = name.toLowerCase()
        if (!name || seen.has(key)) continue
        seen.add(key)
        opts.push({ value: name, label: name })
      }
    }
    setMemberOptions(opts)
  }

  async function saveApproverBatch() {
    if (!batchRows.length || !batchMode) return
    const approvers = [...new Set(batchNames.map((s) => String(s || '').trim()).filter(Boolean))]
    if (!approvers.length) {
      message.warning(batchMode === 'set' ? '请选择要设置的审核人' : '请选择要撤销的审核人')
      return
    }
    setSavingApprovers(true)
    try {
      const path = batchMode === 'set' ? '/access-domains/batch-approvers' : '/access-domains/batch-remove-approvers'
      const { data } = await api.post(path, { ids: batchRows.map((r) => r.id), approvers })
      const parts = [`已更新 ${data?.ok_count || 0} 个域名`]
      if (data?.skip_count) parts.push(`跳过 ${data.skip_count} 个`)
      if ((data?.dropped || []).length) parts.push(`已忽略非本租户成员：${data.dropped.join('、')}`)
      message.success(parts.join('，'))
      setBatchMode('')
      setBatchRows([])
      await load()
    } finally {
      setSavingApprovers(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      await loadTypes()
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
    if (!tabs.length) return
    if (!tabs.some((t) => t.value === activeType)) {
      setActiveType(visibleTypeTabs[0]?.value || tabs[0]?.value || '')
    }
  }, [tabs, activeType, visibleTypeTabs])

  return (
    <div className="ops-page">
      {impersonating ? (
        <p className="muted" style={{ marginBottom: 8 }}>
          租户模式：列表含全平台域名。可勾选后「批量指定租户」或「批量删除」；一键同步按当前类型 Tab 过滤（「其他」则全量）。
        </p>
      ) : null}
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
                  label: `${t.label} (${t.count})`,
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
          label: `${t.label} (${t.count})`,
        }))}
      />
      <div className="toolbar" style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="全部环境"
          style={{ width: 180 }}
          value={envFilter || undefined}
          onChange={(v) => setEnvFilter(v || '')}
          options={envFilterOptions}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="全部租户"
          style={{ width: 200 }}
          value={tenantFilter || undefined}
          onChange={(v) => setTenantFilter(v || '')}
          options={tenantFilterOptions}
        />
        <Input
          value={q}
          allowClear
          placeholder="搜索域名/实例/环境"
          style={{ width: 240 }}
          onChange={(e) => setQ(e.target.value)}
        />
        <Button onClick={() => void load()} loading={loading}>
          刷新
        </Button>
        <Auth urn={Perm.domains.sync}>
          <Button type="primary" loading={syncing} onClick={() => void syncAll()}>
            一键同步{activeType && activeType !== OTHER ? `（${typeKey(activeType)}）` : ''}
          </Button>
        </Auth>
        {redisTab && canEdit ? (
          <Auth urn={Perm.domains.edit}>
            <Button disabled={!selectedKeys.length} onClick={() => openApproverBatch('set')}>
              批量设置审核人{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
            </Button>
            <Button disabled={!selectedKeys.length} onClick={() => openApproverBatch('revoke')}>
              批量撤销审核人{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
            </Button>
          </Auth>
        ) : null}
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
        <Auth urn={Perm.domains.delete}>
          <Button
            danger
            loading={deleting}
            disabled={!selectedKeys.length}
            onClick={() => {
              const set = new Set(selectedKeys.map(String))
              removeRows(filteredRows.filter((r) => r.id && set.has(r.id)))
            }}
          >
            批量删除{selectedKeys.length ? `（${selectedKeys.length}）` : ''}
          </Button>
        </Auth>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          loading={loading}
          dataSource={pagedRows}
          rowKey={(r) => r.id || `${r.domain}-${r.db_type}`}
          pagination={false}
          scroll={{ x: 900 }}
          rowSelection={
            impersonating || canDelete || (redisTab && canEdit)
              ? {
                  selectedRowKeys: selectedKeys,
                  onChange: (keys) => setSelectedKeys(keys),
                  getCheckboxProps: (row: DomainRow) => ({ disabled: !row.id }),
                }
              : undefined
          }
          columns={[
            { title: '域名', dataIndex: 'domain', key: 'domain', width: 220 },
            {
              title: '类型',
              dataIndex: 'db_type',
              key: 'db_type',
              width: 100,
              render: (v: string) => <Tag>{v || '—'}</Tag>,
            },
            { title: '环境', dataIndex: 'env_code', key: 'env_code', width: 100, render: (v: string) => v || '—' },
            {
              title: '所属租户',
              key: 'tenant',
              width: 140,
              render: (_: unknown, row: DomainRow) => {
                const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                const label = row.tenant_name || row.tenant_id || '—'
                return same ? <Tag color="green">{label}</Tag> : label
              },
            },
            {
              title: '实例',
              key: 'instance',
              width: 220,
              ellipsis: true,
              render: (_: unknown, row: DomainRow) => row.instance_name || row.instance_id || '—',
            },
            {
              title: '创建时间',
              dataIndex: 'created_at',
              key: 'created_at',
              width: 180,
              render: (v: string) => formatDomainTime(v),
            },
            ...(redisTab
              ? [
                  {
                    title: '审核人',
                    key: 'approvers',
                    width: 180,
                    ellipsis: true,
                    render: (_: unknown, row: DomainRow) => (row.approvers || []).join(',') || '—',
                  },
                ]
              : []),
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
        title={assignRows.length > 1 ? `批量指定域名归属租户（${assignRows.length}）` : '指定域名归属租户'}
        okText="划拨"
        confirmLoading={assigning}
        onCancel={() => !assigning && setAssignRows([])}
        onOk={confirmAssign}
      >
        <p style={{ marginBottom: 12 }}>
          {assignRows.length === 1 ? (
            <>
              将域名「{assignRows[0].domain}」（{assignRows[0].db_type}）划到指定租户。同地址不同引擎互不影响。
            </>
          ) : (
            <>
              将以下 {assignRows.length} 个域名划到同一目标租户。同地址不同引擎互不影响。
              <ul style={{ margin: '8px 0 0', paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
                {assignRows.map((r) => (
                  <li key={r.id}>
                    {r.domain}（{r.db_type}）
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
            label: `${t.name || t.code || t.id}${t.id === currentTenantId ? '（当前工作租户）' : ''}`,
          }))}
        />
      </Modal>
      <Modal
        open={!!batchMode}
        title={batchMode === 'revoke' ? `批量撤销审核人（${batchRows.length}）` : `批量设置审核人（${batchRows.length}）`}
        okText="保存"
        confirmLoading={savingApprovers}
        onCancel={() => !savingApprovers && setBatchMode('')}
        onOk={() => void saveApproverBatch()}
      >
        <p style={{ marginBottom: 12 }}>
          {batchMode === 'revoke'
            ? '从各域名现有名单中去掉所选审核人。某个域名上没有这个人则跳过该域名，名单可以撤空。一键同步不会覆盖这里填写的内容。'
            : '把所选审核人追加到各域名现有名单。不是该租户成员的名字会被忽略。一键同步不会覆盖这里填写的内容。'}
        </p>
        <ul style={{ margin: '0 0 12px', paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
          {batchRows.map((r) => (
            <li key={r.id}>{r.domain}</li>
          ))}
        </ul>
        {memberError ? (
          <Alert type="error" showIcon style={{ marginBottom: 12 }} message={memberError} />
        ) : null}
        <Select
          mode="multiple"
          showSearch
          allowClear
          optionFilterProp="label"
          style={{ width: '100%' }}
          placeholder={
            batchMode === 'revoke'
              ? '选择要撤销的审核人'
              : memberLoading
                ? '正在加载租户成员'
                : '选择要追加的审核人'
          }
          loading={memberLoading}
          value={batchNames}
          onChange={(v) => setBatchNames(v)}
          options={memberOptions}
          notFoundContent={memberLoading ? '正在加载' : batchMode === 'revoke' ? '所选域名还没有审核人' : '该租户暂无活跃成员'}
        />
      </Modal>
    </div>
  )
}
