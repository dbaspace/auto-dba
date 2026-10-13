import React, { useEffect, useMemo, useState } from 'react'
import { Button, Input, InputNumber, Modal, Radio, Select, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { useLocation, useNavigate } from 'react-router-dom'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppDispatch, useAppSelector } from '../../store'
import { tabKey, updateTabMeta } from '../../store/tabsSlice'
import {
  dbTypeFromSearch,
  instanceTypeSearch,
  parseInstanceTypes,
  type InstanceTypeOption,
} from '../../utils/instanceTypes'
import './Instances.css'

type TypeOption = InstanceTypeOption

interface InstanceRow {
  id?: string
  tenant_id?: string
  tenant_name?: string
  name?: string
  db_type?: string
  region_id?: string
  env_id?: string
  host?: string
  port?: number
  domains?: string[]
  domain_name?: string
  tags?: string[]
  username?: string
  password?: string
  password_set?: boolean
  cloud_vendor?: string
  cloud_instance_id?: string
  cloud_region?: string
  visibility?: string
  access_users?: string[]
  role_ids?: string[]
  approvers?: string[]
  description?: string
}

interface MetaRow {
  id?: string
  name?: string
  code?: string
}

const OTHER = '__other__'
const REQUIRED_PASSWORD_TYPES = new Set([
  'mysql', 'postgres', 'postgresql', 'tidb', 'starrocks', 'clickhouse',
])
const OPTIONAL_PASSWORD_TYPES = new Set([
  'kafka', 'redis', 'elasticsearch', 'mongodb', 'cockroachdb', 'cockroach',
  'oceanbase', 'cassandra', 'oracle', 'sqlserver', 'mssql',
])

function typeKey(t?: string) {
  const n = String(t || '').toLowerCase()
  if (n === 'cockroach' || n === 'cockroadchdb') return 'cockroachdb'
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  return n
}

function isHostType(dbType?: string) {
  const t = typeKey(dbType)
  return t === 'host' || t === 'os' || t === '主机'
}

function defaultPort(dbType?: string) {
  const map: Record<string, number> = {
    mysql: 3306, postgres: 5432, postgresql: 5432, tidb: 4000,
    starrocks: 9030, clickhouse: 8123, kafka: 9092, redis: 6379,
    cockroachdb: 26257, cockroach: 26257, oceanbase: 2881, cassandra: 9042,
    oracle: 1521, elasticsearch: 9200, mongodb: 27017, sqlserver: 1433, mssql: 1433,
    host: 22, os: 22,
  }
  return map[String(dbType || '').toLowerCase()] || 5432
}

function cloudVendorText(v?: string) {
  return ({ tencent: '腾讯云', aliyun: '阿里云', huawei: '华为云', aws: 'AWS' } as Record<string, string>)[String(v || '')] || (v ? v : '自建')
}

function domainList(row?: InstanceRow): string[] {
  const list = Array.isArray(row?.domains) ? row!.domains!.filter(Boolean) : []
  if (list.length) return list
  return row?.domain_name ? [row.domain_name] : []
}

/** 多节点主机：统一为英文逗号分隔 */
function normalizeHost(raw?: string) {
  return String(raw || '')
    .trim()
    .replace(/，/g, ',')
    .replace(/,+/g, ',')
    .replace(/^,|,$/g, '')
}

export default function Instances() {
  const location = useLocation()
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const activeTabKey = useAppSelector((s) => s.tabs.activeKey)
  const user = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenant = (user as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = String(currentTenant?.id || '')
  const currentTenantLabel = currentTenant?.name || currentTenant?.code || '当前租户'
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([])
  const [typesReady, setTypesReady] = useState(false)
  const [regions, setRegions] = useState<MetaRow[]>([])
  const [regionEnvs, setRegionEnvs] = useState<MetaRow[]>([])
  const [allEnvs, setAllEnvs] = useState<MetaRow[]>([])
  const [rows, setRows] = useState<InstanceRow[]>([])
  const [q, setQ] = useState('')
  const [visible, setVisible] = useState(false)
  const [testing, setTesting] = useState(false)
  const [form, setForm] = useState<InstanceRow>({})
  const [approverText, setApproverText] = useState('admin')
  const [activeType, setActiveType] = useState('')
  const [userOptions, setUserOptions] = useState<string[]>(['admin'])
  const [roles, setRoles] = useState<MetaRow[]>([])

  const dbTypeKey = typeKey(form.db_type)
  const hostType = isHostType(form.db_type)
  const passwordRequired = !hostType && REQUIRED_PASSWORD_TYPES.has(String(form.db_type || '').toLowerCase())
  const showPassword = !hostType && (passwordRequired || OPTIONAL_PASSWORD_TYPES.has(String(form.db_type || '').toLowerCase()))
  const showCloud = (() => {
    if (hostType) return false
    const t = String(form.db_type || '').toLowerCase()
    return t === 'mysql' || t === 'postgres' || t === 'postgresql'
  })()
  const visibility = form.visibility === 'private' ? 'private' : hostType ? (form.visibility || 'private') : 'global'

  const cloudIdPlaceholder = (() => {
    const v = String(form.cloud_vendor || '')
    if (v === 'tencent') return '如 postgres-xxxx'
    if (v === 'aliyun') return '如 pgm-xxxx / rr-xxxx'
    if (v === 'huawei') return '云数据库实例 ID'
    if (v === 'aws') return '如 db-xxxx'
    return '云平台实例 ID'
  })()

  const passwordLabel = (() => {
    const t = dbTypeKey
    if (t === 'mysql') return 'MySQL 密码'
    if (t === 'postgres' || t === 'postgresql') return 'PostgreSQL 密码'
    if (t === 'tidb') return 'TiDB 密码'
    if (t === 'starrocks') return 'StarRocks 密码'
    if (t === 'clickhouse') return 'ClickHouse 密码'
    if (t === 'kafka') return '密码（可选）'
    if (t === 'redis') return '密码（可选）'
    return passwordRequired ? '密码' : '密码（可选）'
  })()
  const passwordPlaceholder = (() => {
    if (form.id && form.password_set) return '已加密保存，不填则不修改'
    return passwordRequired ? '请输入密码' : '无认证则留空'
  })()
  const passwordHint = passwordRequired
    ? '密码入库加密存储，接口不回传明文'
    : '可选。Kafka / Redis 等无 SASL 或无密码时可留空'

  const knownTypes = useMemo(() => new Set(typeOptions.map((t) => typeKey(t.value))), [typeOptions])

  const tabs = useMemo(() => {
    const list = typeOptions.map((t) => ({ value: t.value, label: t.label }))
    if (rows.some((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))) {
      list.push({ value: OTHER, label: '其他' })
    }
    return list
  }, [typeOptions, rows, knownTypes])

  const filteredRows = useMemo(() => {
    if (activeType === OTHER) {
      return rows.filter((r) => r.db_type && !knownTypes.has(typeKey(r.db_type)))
    }
    if (!activeType) return rows
    return rows.filter((r) => typeKey(r.db_type) === typeKey(activeType) || r.db_type === activeType)
  }, [rows, activeType, knownTypes])

  const usedTags = useMemo(() => {
    const set = new Set<string>()
    for (const r of rows) {
      for (const t of r.tags || []) {
        const s = String(t).trim()
        if (s) set.add(s)
      }
    }
    return [...set].sort()
  }, [rows])

  /** 用 typeKey 匹配；cur 非空但不在 list 时返回空串，禁止回退到 names[0] */
  function resolveActiveTab(list: { value: string }[], cur: string) {
    if (!cur) return ''
    const hit = list.find((t) => t.value === cur || typeKey(t.value) === typeKey(cur))
    return hit?.value || ''
  }

  function applyTypeToTab(typeValue: string, typeLabel: string) {
    const nextSearch = typeValue === OTHER ? '' : instanceTypeSearch(typeValue)
    const title = typeValue === OTHER ? '其他' : typeLabel
    const curKey = tabKey(location.pathname, location.search || '')
    dispatch(updateTabMeta({ key: curKey, search: nextSearch, title }))
    const to = location.pathname + (nextSearch || '')
    const thisIsActive = activeTabKey === curKey
    if (thisIsActive && location.pathname + (location.search || '') !== to) {
      navigate(to, { replace: true })
    }
  }

  async function loadTypes() {
    try {
      const args = (await api.get('/global-args')).data.data || []
      const raw = args.find((a: any) => a.key === 'ticket_instance_types')?.value || ''
      const opts = parseInstanceTypes(raw)
      setTypeOptions(opts)
      return opts
    } finally {
      setTypesReady(true)
    }
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
    const [r, e] = await Promise.all([api.get('/regions'), api.get('/environments')])
    setRegions(r.data.data || [])
    setAllEnvs(e.data.data || [])
    await loadAclOptions(currentTenantId)
  }

  function regionName(id?: string) {
    return regions.find((r) => r.id === id)?.name || '—'
  }
  function envName(id?: string) {
    return allEnvs.find((e) => e.id === id)?.name || '—'
  }

  async function loadRegionEnvs(regionId?: string) {
    if (!regionId) {
      setRegionEnvs([])
      return
    }
    setRegionEnvs((await api.get(`/regions/${regionId}/envs`)).data.data || [])
  }

  async function load() {
    const { data } = await api.get('/instances', { params: { q } })
    setRows(data.data || [])
  }

  function open(row?: InstanceRow) {
    const tabType = activeType !== OTHER ? activeType : ''
    const creatingHost = isHostType(tabType)
    const selfName = String((user as { username?: string })?.username || '').trim()
    const next: InstanceRow = row
      ? {
          ...row,
          domains: domainList(row),
          tags: [...(row.tags || [])],
          password: '',
          access_users: [...(row.access_users || [])],
          role_ids: [...(row.role_ids || [])],
          visibility: row.visibility || (isHostType(row.db_type) ? 'private' : 'global'),
        }
      : {
          db_type: tabType,
          port: defaultPort(tabType),
          region_id: '',
          env_id: '',
          host: '',
          domains: [],
          tags: [],
          password: '',
          username: creatingHost ? 'sysadmin' : '',
          visibility: creatingHost ? 'private' : 'global',
          access_users: creatingHost && selfName ? [selfName] : [],
          role_ids: [],
          approvers: selfName ? [selfName] : [],
          cloud_vendor: '',
          cloud_instance_id: '',
          cloud_region: '',
        }
    const cur = String(next.db_type || '').trim()
    if (cur && !typeOptions.some((t) => t.value === cur)) {
      setTypeOptions((prev) => [...prev, { value: cur, label: cur }])
    }
    setForm(next)
    setApproverText((next.approvers || []).join(','))
    setVisible(true)
    void loadAclOptions(row?.tenant_id || currentTenantId)
    loadRegionEnvs(next.region_id)
  }

  function onRegionChange(regionId?: string) {
    setForm((f) => ({ ...f, region_id: regionId, env_id: '' }))
    loadRegionEnvs(regionId)
  }

  async function save() {
    const host = normalizeHost(form.host)
    let domains = (form.domains || []).map((s) => String(s).trim()).filter(Boolean)
    if (isHostType(form.db_type) && !domains.length && host) {
      domains = [host]
    }
    const payload: InstanceRow = {
      ...form,
      host,
      domains,
      domain_name: domains[0] || '',
      tags: (form.tags || []).map((s) => String(s).trim()).filter(Boolean),
      approvers: approverText.split(',').map((s) => s.trim()).filter(Boolean),
    }
    if (isHostType(payload.db_type)) {
      payload.visibility = payload.visibility === 'global' ? 'global' : 'private'
      payload.access_users = payload.visibility === 'private' ? [...(payload.access_users || [])] : []
      payload.role_ids = payload.visibility === 'private' ? [...(payload.role_ids || [])] : []
      if (payload.visibility === 'private' && !payload.access_users.length && !payload.role_ids.length) {
        message.warning('主机私有可见时须指定可访问用户或绑定角色')
        return
      }
      if (!String(payload.username || '').trim()) payload.username = 'sysadmin'
    } else {
      payload.visibility = 'global'
      payload.access_users = []
      payload.role_ids = []
    }
    if (!payload.host) {
      message.warning('请填写连接主机')
      return
    }
    if (!domains.length) {
      message.warning('请至少添加一个域名')
      return
    }
    if (passwordRequired && !payload.id && !String(payload.password || '').trim()) {
      message.warning('请填写密码')
      return
    }
    const t = String(payload.db_type || '').toLowerCase()
    if (payload.cloud_vendor && (t === 'postgres' || t === 'postgresql')) {
      if (!String(payload.cloud_instance_id || '').trim()) {
        message.warning('云实例须填写云实例 ID，用于从云平台下载 WAL')
        return
      }
      if (!String(payload.cloud_region || '').trim()) {
        message.warning('云实例须填写云 Region')
        return
      }
    }
    if (!String(payload.password || '').trim()) delete payload.password
    if (payload.id) await api.put('/instances/' + payload.id, payload)
    else await api.post('/instances', payload)
    message.success('已保存')
    setVisible(false)
    if (payload.db_type) setActiveType(payload.db_type)
    await load()
  }

  async function testConnection() {
    const host = normalizeHost(form.host)
    if (!host) {
      message.warning('请填写连接主机')
      return
    }
    if (!form.port || form.port <= 0) {
      message.warning('请填写端口')
      return
    }
    if (!String(form.db_type || '').trim()) {
      message.warning('请选择实例类型')
      return
    }
    if (passwordRequired && !String(form.username || '').trim()) {
      message.warning('请填写账号')
      return
    }
    if (passwordRequired && !form.id && !String(form.password || '').trim()) {
      message.warning('请填写密码')
      return
    }
    if (passwordRequired && form.id && !String(form.password || '').trim() && !form.password_set) {
      message.warning('请填写密码')
      return
    }
    setTesting(true)
    try {
      const body: Record<string, unknown> = {
        db_type: form.db_type,
        host,
        port: form.port,
        username: String(form.username || '').trim(),
      }
      if (form.id) body.id = form.id
      if (String(form.password || '').trim()) body.password = form.password
      const { data } = await api.post('/instances/test-connection', body)
      if (data?.ok) message.success('连接成功')
      else message.error(data?.message || '连接失败')
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '连接失败')
    } finally {
      setTesting(false)
    }
  }

  function assignToCurrentTenant(row: InstanceRow) {
    if (!row.id || !currentTenantId) return
    const from = row.tenant_name || row.tenant_id || '其它租户'
    Modal.confirm({
      title: '划拨到当前租户',
      content: `将实例「${row.name || row.id}」（当前归属：${from}）划拨到「${currentTenantLabel}」？下属已登记库与账号一并迁移。`,
      okText: '划拨',
      onOk: async () => {
        await api.post(`/instances/${row.id}/assign-tenant`, { tenant_id: currentTenantId })
        message.success('已划拨到 ' + currentTenantLabel)
        await load()
      },
    })
  }

  function remove(row: InstanceRow) {
    const host = isHostType(row.db_type)
    Modal.confirm({
      title: '提示',
      content: host
        ? '删除该主机？若主机上仍有 Agent 或集群节点，需先下线节点、删除 Agent 后再删除主机。'
        : '删除实例及其已登记库？',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.delete('/instances/' + row.id, { silent: true })
        } catch (e: any) {
          Modal.error({
            title: host ? '主机仍被使用，无法删除' : '删除失败',
            width: 560,
            content: <div style={{ whiteSpace: 'pre-wrap' }}>{e?.response?.data?.message || e?.message || '删除失败'}</div>,
          })
          return
        }
        await load()
      },
    })
  }

  useEffect(() => {
    Promise.all([load(), loadTypes(), loadMeta()])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    // 类型未就绪时禁止回退/改 URL，避免 postgres 被误判后落到 mysql
    if (!typesReady) return
    if (!tabs.length) return
    const fromQuery = dbTypeFromSearch(location.search)
    const matched = resolveActiveTab(tabs, fromQuery || activeType)
    if (fromQuery && !matched) {
      // URL 指定了类型但暂不在 tabs（或别名未入表）：保留 query，勿 fallback
      if (fromQuery !== activeType) setActiveType(fromQuery)
      return
    }
    const next = matched || tabs[0]?.value || ''
    if (next && next !== activeType) setActiveType(next)
    if (!next) return
    const label = tabs.find((t) => t.value === next)?.label || next
    const wantSearch = next === OTHER ? '' : instanceTypeSearch(next)
    const curSearch = location.search || ''
    if (curSearch !== wantSearch) {
      applyTypeToTab(next, label)
      return
    }
    const curKey = tabKey(location.pathname, curSearch)
    dispatch(updateTabMeta({ key: curKey, title: label }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabs, location.search, typesReady])

  return (
    <div className="ops-page">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">关键词：</span>
          <Input value={q} placeholder="请输入名称/主机" onChange={(e) => setQ(e.target.value)} onPressEnter={load} />
        </div>
        <div className="toolbar" style={{ margin: 0, flexShrink: 0 }}>
          <Button type="primary" onClick={load}>
            查询
          </Button>
          <Auth urn={Perm.instances.create}>
            <Button type="primary" onClick={() => open()}>
              新增实例
            </Button>
          </Auth>
        </div>
      </div>
      {impersonating ? (
        <div className="type-hint" style={{ margin: '0 0 8px', color: 'var(--ant-color-text-secondary, #666)' }}>
          审计中：列表含全平台实例。其它租户资源可「划拨到当前租户（{currentTenantLabel}）」后供该租户成员使用。
        </div>
      ) : null}
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.name || ''}
          dataSource={filteredRows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          locale={{
            emptyText: impersonating
              ? '暂无实例。可在其它类型 Tab 查看，或先在默认租户建好后于本页划拨。'
              : '暂无数据',
          }}
          columns={[
            { title: '名称', dataIndex: 'name' },
            ...(impersonating
              ? [
                  {
                    title: '所属租户',
                    width: 120,
                    render: (_: unknown, row: InstanceRow) => {
                      const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                      const label = row.tenant_name || row.tenant_id || '—'
                      return same ? (
                        <Tag color="green">{label}</Tag>
                      ) : (
                        <Tag color="orange">{label}</Tag>
                      )
                    },
                  },
                ]
              : []),
            { title: '区域', width: 120, render: (_, row) => regionName(row.region_id) },
            { title: '环境', width: 120, render: (_, row) => envName(row.env_id) },
            { title: '连接主机', width: 160, render: (_, row) => `${row.host}:${row.port}` },
            {
              title: '域名',
              width: 200,
              render: (_, row) => {
                const list = domainList(row)
                if (!list.length) return <span className="muted">—</span>
                return list.map((d) => (
                  <Tag key={d} className="domain-tag">
                    {d}
                  </Tag>
                ))
              },
            },
            {
              title: '标签',
              width: 180,
              render: (_, row) => {
                const list = row.tags || []
                if (!list.length) return <span className="muted">—</span>
                return list.map((t) => (
                  <Tag key={t} color="default" className="domain-tag">
                    {t}
                  </Tag>
                ))
              },
            },
            { title: '云厂商', width: 100, render: (_, row) => cloudVendorText(row.cloud_vendor) },
            {
              title: '云实例 ID',
              dataIndex: 'cloud_instance_id',
              width: 140,
              ellipsis: true,
              render: (_, row) => row.cloud_instance_id || '—',
            },
            {
              title: '云 Region',
              dataIndex: 'cloud_region',
              width: 130,
              render: (_, row) => row.cloud_region || '—',
            },
            { title: '审批人', dataIndex: 'approvers', width: 120, render: (_, row) => (row.approvers || []).join(',') || '—' },
            {
              title: '操作',
              width: impersonating ? 300 : 220,
              fixed: 'right' as const,
              render: (_, row) => (
                <>
                  {impersonating && row.tenant_id && row.tenant_id !== currentTenantId ? (
                    <Auth urn={Perm.tenants.impersonate}>
                      <Button type="link" onClick={() => assignToCurrentTenant(row)}>
                        划拨到当前租户
                      </Button>
                    </Auth>
                  ) : null}
                  {isHostType(row.db_type) && (
                    <Button
                      type="link"
                      disabled={!String(row.host || row.domain_name || '').trim()}
                      onClick={async () => {
                        try {
                          const res = await api.post(`/instances/${row.id}/open-terminal`)
                          const url = res.data?.data?.terminal_url as string
                          if (!url) {
                            message.error(res.data?.message || '未返回终端地址')
                            return
                          }
                          window.open(url, '_blank', 'noopener,noreferrer')
                        } catch (e: any) {
                          message.error(e?.response?.data?.message || '打开终端失败')
                        }
                      }}
                    >
                      终端
                    </Button>
                  )}
                  <Auth urn={Perm.instances.edit}>
                    <Button type="link" onClick={() => open(row)}>
                      编辑
                    </Button>
                  </Auth>
                  <Auth urn={Perm.instances.delete}>
                    <Button type="link" danger onClick={() => remove(row)}>
                      删除
                    </Button>
                  </Auth>
                </>
              ),
            },
          ]}
        />
      </div>
      <Modal
        open={visible}
        title={form.id ? '编辑实例' : '新增实例'}
        width={620}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={[
          <Button key="test" loading={testing} onClick={testConnection}>
            测试连接
          </Button>,
          <Button key="cancel" onClick={() => setVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" onClick={save}>
            保存
          </Button>,
        ]}
      >
        <div className="inst-form">
          <div className="fi">
            <label>名称</label>
            <Input value={form.name || ''} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </div>
          <div className="fi">
            <label>类型</label>
            <div className="fi-body">
              <Select
                value={form.db_type || undefined}
                disabled={!form.id}
                placeholder="当前 Tab 类型"
                style={{ width: '100%' }}
                options={typeOptions.map((t) => ({ value: t.value, label: t.label }))}
                onChange={(v) => setForm((f) => ({ ...f, db_type: v }))}
              />
              {!typeOptions.length && <div className="type-hint">请先在「全局参数」配置 ticket_instance_types</div>}
            </div>
          </div>
          <div className="fi">
            <label>区域</label>
            <Select
              value={form.region_id || undefined}
              showSearch
              allowClear
              placeholder="请选择区域"
              style={{ width: '100%' }}
              optionFilterProp="label"
              options={regions.map((r) => ({ value: r.id, label: r.name }))}
              onChange={(v) => onRegionChange(v)}
            />
          </div>
          <div className="fi">
            <label>环境</label>
            <Select
              value={form.env_id || undefined}
              showSearch
              allowClear
              disabled={!form.region_id}
              placeholder={form.region_id ? '请选择环境' : '请先选择区域'}
              style={{ width: '100%' }}
              optionFilterProp="label"
              options={regionEnvs.map((e) => ({ value: e.id, label: e.name }))}
              onChange={(v) => setForm((f) => ({ ...f, env_id: v }))}
            />
          </div>
          <div className="fi">
            <label>连接主机</label>
            <div className="fi-body">
              <Input
                value={form.host || ''}
                placeholder="实际连接 IP / 主机名，多节点用英文逗号分隔"
                onChange={(e) => setForm((f) => ({ ...f, host: e.target.value }))}
              />
              <div className="type-hint">多节点集群可填多个地址，连接时按顺序尝试</div>
            </div>
          </div>
          <div className="fi">
            <label>{hostType ? 'SSH 端口' : '端口'}</label>
            <InputNumber min={1} value={form.port} onChange={(v) => setForm((f) => ({ ...f, port: v ?? undefined }))} />
          </div>
          <div className="fi">
            <label>域名</label>
            <div className="fi-body">
              <Select
                mode="tags"
                value={form.domains || []}
                placeholder={hostType ? '可选；留空则用连接主机作为标识' : '回车添加，一个主机可绑多个域名'}
                style={{ width: '100%' }}
                tokenSeparators={[',']}
                onChange={(v) => setForm((f) => ({ ...f, domains: v }))}
              />
              <div className="type-hint">
                {hostType ? '主机类型可省略域名，保存时自动用连接主机填充' : '同一连接主机可绑定多个域名，输入后回车生成标签'}
              </div>
            </div>
          </div>
          <div className="fi">
            <label>标签</label>
            <div className="fi-body">
              <Select
                mode="tags"
                value={form.tags || []}
                placeholder="输入后回车，可自定义任意标签"
                style={{ width: '100%' }}
                options={[
                  ...allEnvs
                    .map((e) => String(e.code || '').trim())
                    .filter(Boolean)
                    .map((code) => ({ value: `idc=${code}`, label: `idc=${code}` })),
                  ...usedTags
                    .filter((t) => !/^idc=/i.test(t))
                    .map((t) => ({ value: t, label: t })),
                ]}
                onChange={(v) => setForm((f) => ({ ...f, tags: v }))}
              />
              <div className="type-hint">
                可选。规范：`idc` 须等于所选环境编码（保存时自动同步）；Redis 等可补 `domain=`
              </div>
            </div>
          </div>
          <div className="fi">
            <label>{hostType ? 'SSH 用户' : '账号'}</label>
            <div className="fi-body">
              <Input
                value={form.username || ''}
                placeholder={hostType ? '默认 sysadmin' : ''}
                onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
              />
              {hostType && <div className="type-hint">登录目标机的系统用户；私钥使用平台全局 terminal 配置</div>}
            </div>
          </div>
          {hostType && (
            <>
              <div className="fi">
                <label>可见范围</label>
                <div className="fi-body">
                  <Radio.Group
                    value={visibility === 'global' ? 'global' : 'private'}
                    onChange={(e) => setForm((f) => ({ ...f, visibility: e.target.value }))}
                  >
                    <Radio.Button value="global">全局</Radio.Button>
                    <Radio.Button value="private">私有</Radio.Button>
                  </Radio.Group>
                  <div className="type-hint">私有：仅本租户授权成员/角色可在「主机终端」看到并操作；平台特权角色除外</div>
                </div>
              </div>
              {visibility !== 'global' && (
                <>
                  <div className="fi">
                    <label>可访问用户</label>
                    <Select
                      mode="multiple"
                      value={form.access_users || []}
                      showSearch
                      maxTagCount="responsive"
                      placeholder="选择所属租户成员"
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
            </>
          )}
          {showPassword && (
            <div className="fi">
              <label>{passwordLabel}</label>
              <div className="fi-body">
                <Input.Password
                  value={form.password || ''}
                  autoComplete="new-password"
                  placeholder={passwordPlaceholder}
                  onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                />
                <div className="type-hint">{passwordHint}</div>
              </div>
            </div>
          )}
          {showCloud && (
            <div className="fi">
              <label>云厂商</label>
              <div className="fi-body">
                <Select
                  value={form.cloud_vendor ?? ''}
                  allowClear
                  placeholder="空=自建"
                  style={{ width: '100%' }}
                  options={[
                    { value: '', label: '自建' },
                    { value: 'tencent', label: '腾讯云' },
                    { value: 'aliyun', label: '阿里云' },
                    { value: 'huawei', label: '华为云' },
                    { value: 'aws', label: 'AWS' },
                  ]}
                  onChange={(v) => setForm((f) => ({ ...f, cloud_vendor: v ?? '' }))}
                />
                <div className="type-hint">
                  闪回下载 WAL 时按厂商匹配「数据闪回 → 多云密钥」中的 AK/SK。连库仍用上方主机/账密。
                </div>
              </div>
            </div>
          )}
          {showCloud && (
            <div className="fi">
              <label>云实例 ID</label>
              <Input
                value={form.cloud_instance_id || ''}
                placeholder={cloudIdPlaceholder}
                onChange={(e) => setForm((f) => ({ ...f, cloud_instance_id: e.target.value }))}
              />
            </div>
          )}
          {showCloud && (
            <div className="fi">
              <label>云 Region</label>
              <Input
                value={form.cloud_region || ''}
                placeholder="如 ap-guangzhou / cn-hangzhou"
                onChange={(e) => setForm((f) => ({ ...f, cloud_region: e.target.value }))}
              />
            </div>
          )}
          <div className="fi">
            <label>审批人</label>
            <Select
              mode="multiple"
              value={approverText
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean)}
              showSearch
              maxTagCount="responsive"
              placeholder="仅限所属租户成员"
              style={{ width: '100%' }}
              optionFilterProp="label"
              options={userOptions.map((u) => ({ value: u, label: u }))}
              onChange={(v) => setApproverText((v || []).join(','))}
            />
          </div>
          <div className="fi">
            <label>说明</label>
            <Input value={form.description || ''} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
          </div>
        </div>
      </Modal>
    </div>
  )
}
