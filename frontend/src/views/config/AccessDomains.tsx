import React, { useEffect, useMemo, useState } from 'react'
import { Button, Input, Modal, Pagination, Select, Tabs, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'
import {
  parseInstanceTypes,
  type InstanceTypeOption,
} from '../../utils/instanceTypes'
import './Databases.css'

type TypeOption = InstanceTypeOption

interface DomainRow {
  id?: string
  tenant_id?: string
  tenant_name?: string
  instance_id?: string
  domain?: string
  env_code?: string
  db_type?: string
}

const OTHER = '__other__'

function typeKey(t?: string) {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}

export default function AccessDomains() {
  const user = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenant = (user as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = String(currentTenant?.id || '')
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([])
  const [activeType, setActiveType] = useState('')
  const [rows, setRows] = useState<DomainRow[]>([])
  const [q, setQ] = useState('')
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [page, setPage] = useState(1)
  const pageSize = 10
  const [tenants, setTenants] = useState<{ id: string; name?: string; code?: string }[]>([])
  const [assignRows, setAssignRows] = useState<DomainRow[]>([])
  const [assignTid, setAssignTid] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [selectedKeys, setSelectedKeys] = useState<React.Key[]>([])

  const knownTypes = useMemo(() => new Set(typeOptions.map((t) => typeKey(t.value))), [typeOptions])

  const tabs = useMemo(() => {
    const list = typeOptions.map((t) => ({ value: t.value, label: t.label }))
    if (rows.some((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))) {
      list.push({ value: OTHER, label: '其他' })
    }
    return list
  }, [typeOptions, rows, knownTypes])

  const filteredRows = useMemo(() => {
    let list = rows
    if (activeType === OTHER) {
      list = list.filter((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))
    } else if (activeType) {
      list = list.filter((r) => typeKey(r.db_type) === typeKey(activeType))
    }
    const kw = q.trim().toLowerCase()
    if (kw) {
      list = list.filter(
        (r) =>
          (r.domain || '').toLowerCase().includes(kw) ||
          (r.instance_id || '').toLowerCase().includes(kw) ||
          (r.env_code || '').toLowerCase().includes(kw),
      )
    }
    return list
  }, [rows, activeType, knownTypes, q])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page])

  useEffect(() => {
    setPage(1)
    setSelectedKeys([])
  }, [activeType, q])

  async function loadTypes() {
    const { data } = await api.get('/global-args')
    const raw = (data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    setTypeOptions(parseInstanceTypes(raw))
  }

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/access-domains')
      setRows(data.data || [])
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
      setActiveType(tabs[0]?.value || '')
    }
  }, [tabs, activeType])

  return (
    <div className="ops-page">
      {impersonating ? (
        <p className="muted" style={{ marginBottom: 8 }}>
          审计中：列表含全平台域名。可勾选后「批量指定租户」，或单行「指定租户」；一键同步按当前类型 Tab 过滤（「其他」则全量）。
        </p>
      ) : null}
      <Tabs
        className="inst-tabs"
        activeKey={activeType}
        onChange={(k) => {
          setActiveType(k)
          setPage(1)
          setSelectedKeys([])
        }}
        items={tabs.map((t) => ({
          key: t.value,
          label: `${t.label} (${rows.filter((r) =>
            t.value === OTHER
              ? r.db_type && !knownTypes.has(typeKey(r.db_type))
              : typeKey(r.db_type) === typeKey(t.value),
          ).length})`,
        }))}
      />
      <div className="toolbar" style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
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
      <div className="ops-table">
        <ResponsiveTable
          loading={loading}
          dataSource={pagedRows}
          rowKey={(r) => r.id || `${r.domain}-${r.db_type}`}
          pagination={false}
          scroll={{ x: 900 }}
          rowSelection={
            impersonating
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
            { title: '实例 ID', dataIndex: 'instance_id', key: 'instance_id', width: 280, ellipsis: true },
            {
              title: '操作',
              width: 120,
              fixed: 'right' as const,
              render: (_: unknown, row: DomainRow) =>
                impersonating ? (
                  <Auth urn={Perm.tenants.impersonate}>
                    <Button type="link" onClick={() => openAssign([row])}>
                      指定租户
                    </Button>
                  </Auth>
                ) : null,
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
            label: `${t.name || t.code || t.id}${t.id === currentTenantId ? '（当前审计）' : ''}`,
          }))}
        />
      </Modal>
    </div>
  )
}
