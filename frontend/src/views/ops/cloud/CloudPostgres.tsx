import React, { useEffect, useMemo, useState } from 'react'
import { Button, Collapse, Form, Input, InputNumber, Modal, Popover, Radio, Select, Space, Switch, Table, message } from 'antd'
import { Link } from 'react-router-dom'
import api from '../../../api/request'
import Auth from '../../../components/Auth'
import { Perm, usePermission } from '../../../hooks/usePermission'

const actions = [
  { value: 'create', label: '资源申请' },
  { value: 'scale_spec', label: '规格变配' },
  { value: 'scale_disk', label: '磁盘扩容' },
  { value: 'restart', label: '重启' },
  { value: 'switchover', label: '主备切换' },
  { value: 'modify_param', label: '修改参数' },
  { value: 'register_platform', label: '同步到平台资源' },
  { value: 'rebind_account', label: '改绑云账号' },
]

function memoryText(memoryMB?: number) {
  const mem = Number(memoryMB || 0)
  if (mem >= 1024 && mem % 1024 === 0) return `${mem / 1024}GB`
  return `${mem}MB`
}

function specText(cpu?: number, memoryMB?: number) {
  return `${Number(cpu || 0)}核 / ${memoryText(memoryMB)}`
}

const diskTypeLabel: Record<string, string> = {
  CLOUD_BASIC: '普通云硬盘',
  CLOUD_PREMIUM: '高性能云硬盘',
  CLOUD_SSD: 'SSD 云硬盘',
  CLOUD_HSSD: '增强型 SSD',
  CLOUD_BSSD: '通用型 SSD',
  CLOUD_TSSD: '极速型 SSD',
  LOCAL_BASIC: '本地盘',
  LOCAL_SSD: '本地 SSD',
  LOCAL_NVME: '本地 NVMe',
}

function diskLine(kind: string, gb?: number, type?: string) {
  const size = Number(gb || 0)
  if (size <= 0) return ''
  const t = String(type || '').trim()
  return `${kind} ${size}GB${t ? ` ${diskTypeLabel[t] || t}` : ''}`
}

function systemDiskText(gb?: number, type?: string) {
  return diskLine('系统盘', gb, type)
}

const switchClock = /^([01]\d|2[0-3]):[0-5]\d:[0-5]\d$/

function storageLimitMessage(disk: number, max: number) {
  if (max > 0 && disk > max) return `当前磁盘 ${disk}GB，该规格最大支持 ${max}GB`
  return ''
}

const emptyCatalog = { zones: [], versions: [], vpcs: [], subnets: [], security_groups: [], projects: [], errors: {} as Record<string, string> }

function pgCreateError(v: any, specs: any[]) {
  if (!v.account_id) return '请选择已绑定的云账号'
  if (!v.region) return '请选择地域'
  if (!v.zone || !v.standby_zone) return '请选择主可用区和备可用区'
  if (!v.vpc_id || !v.subnet_id) return '请选择私有网络和子网'
  if (!v.db_major_version) return '请选择数据库版本'
  if (!v.spec_code || !v.cpu || !v.memory_mb) return '请选择规格'
  const disk = Number(v.disk_gb || 0)
  if (disk < 10 || disk % 10 !== 0) return '磁盘按 10GB 步进，且不能小于 10GB'
  const spec = specs.find((s) => s.spec_code === v.spec_code)
  const limit = storageLimitMessage(disk, Number(spec?.max_storage_gb || 0))
  if (limit) return limit
  if (v.charge_type !== 'PREPAID' && v.charge_type !== 'POSTPAID_BY_HOUR') return '请选择计费模式'
  if (v.charge_type === 'PREPAID') {
    const period = Number(v.period || 0)
    if (!((period >= 1 && period <= 12) || period === 24 || period === 36)) return '包年包月时长只支持 1 到 12 个月，或 24、36 个月'
  }
  if (v.charset !== 'UTF8' && v.charset !== 'LATIN1') return '字符集只支持 UTF8 或 LATIN1'
  if (v.sync_mode !== 'Async' && v.sync_mode !== 'Semi-sync') return '请选择数据复制方式'
  const admin = String(v.admin_user || '')
  if (!/^[A-Za-z_][A-Za-z0-9_]{0,15}$/.test(admin) || admin.toLowerCase() === 'postgres' || admin.toLowerCase().startsWith('pg_')) {
    return '管理员账号需要 1-16 位字母、数字或下划线，不能以数字或 pg_ 开头，也不能是 postgres'
  }
  const password = String(v.password_enc || '')
  if (password !== String(v.password_confirm || '')) return '两次输入的密码不一致'
  if (password.length < 8 || password.length > 32 || password.startsWith('/') || !/[a-z]/.test(password) || !/[A-Z]/.test(password) || !/[0-9]/.test(password) || !/[`()~!@#$%^&*\-+=_|{}\[\]:;'<>,.?/]/.test(password)) {
    return '管理员密码需要 8-32 位，包含大写、小写、数字和特殊字符，且不能以 / 开头'
  }
  return ''
}

function intranetLabel(row: { intranet?: string; port?: number }) {
  const host = String(row.intranet || '').trim()
  if (!host) return ''
  const port = Number(row.port || 0)
  return port > 0 ? `${host}:${port}` : host
}

function kafkaCount(v?: number) {
  return v ? v : '—'
}

function kafkaClusterLabel(v?: string) {
  switch (String(v || '').trim().toLowerCase()) {
    case 'profession':
      return '专业版'
    case 'premium':
      return '高级版'
    case 'standard':
      return '标准版'
    default:
      return v || '—'
  }
}

const reconcileLabel: Record<string, string> = {
  linked: '已关联',
  cloud_only: '云上未关联',
  missing: '云上已不存在',
  status_changed: '状态变化',
  conflict: '冲突',
}

type Caps = { scale_disk_down?: boolean; switchover?: boolean; parameters?: boolean; restart?: boolean; create?: boolean; slow_log?: boolean }

const titles: Record<string, string> = {
  postgres: '云 PostgreSQL',
  mysql: '云 MySQL',
  redis: '云 Redis',
  kafka: '云 Kafka',
  host: '云主机',
}

function AccountCell({
  accountId,
  accountName,
  vendor,
  accounts,
  loading,
  disabled,
  onChange,
}: {
  accountId?: string
  accountName?: string
  vendor?: string
  accounts: any[]
  loading?: boolean
  disabled?: boolean
  onChange: (id: string) => void
}) {
  const { hasUrn } = usePermission()
  if (!hasUrn(Perm.cloud.submit)) return <>{accountName || accountId || '—'}</>
  const opts = accounts
    .filter((a) => !vendor || a.vendor === vendor)
    .map((a) => ({ value: a.id, label: a.name }))
  if (accountId && !opts.some((o) => o.value === accountId)) {
    opts.unshift({ value: accountId, label: accountName || accountId })
  }
  return (
    <Select
      size="small"
      style={{ width: 180 }}
      value={accountId || undefined}
      placeholder="选择归属账号"
      loading={loading}
      disabled={disabled}
      options={opts}
      onChange={onChange}
    />
  )
}

export default function CloudPostgres({ engine = 'postgres' }: { engine?: string }) {
  const title = titles[engine] || '云资源'
  const [rows, setRows] = useState<any[]>([])
  const [health, setHealth] = useState<any[]>([])
  const [queued, setQueued] = useState(0)
  const [caps, setCaps] = useState<Record<string, Caps>>({})
  const [reconcile, setReconcile] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [open, setOpen] = useState(false)
  const [quote, setQuote] = useState<any>(null)
  const [specs, setSpecs] = useState<any[]>([])
  const [specError, setSpecError] = useState('')
  const [catalog, setCatalog] = useState<any>(emptyCatalog)
  const [accounts, setAccounts] = useState<any[]>([])
  const [params, setParams] = useState<any[]>([])
  const [assigning, setAssigning] = useState<string>('')
  const [syncOpen, setSyncOpen] = useState(false)
  const [syncIds, setSyncIds] = useState<string[]>([])
  const [syncing, setSyncing] = useState(false)
  const [form] = Form.useForm()
  const [specPick, setSpecPick] = useState<string>()
  const action = Form.useWatch('action', form)
  const beforeCPU = Number(Form.useWatch('before_cpu', form) || 0)
  const beforeMem = Number(Form.useWatch('before_memory_mb', form) || 0)
  const beforeDisk = Number(Form.useWatch('before_disk_gb', form) || 0)
  const targetCPU = Number(Form.useWatch('cpu', form) || 0)
  const targetMem = Number(Form.useWatch('memory_mb', form) || 0)
  const switchTag = Number(Form.useWatch('switch_tag', form) || 0)
  const chargeType = Form.useWatch('charge_type', form)
  const pickedAccount = Form.useWatch('account_id', form)
  const pickedVpc = Form.useWatch('vpc_id', form)
  const pgCreate = engine === 'postgres' && action === 'create'
  const accountRegions: string[] = accounts.find((a) => a.id === pickedAccount)?.regions || []

  function specMax(cpu: number, mem: number) {
    const spec = specs.find((s) => Number(s.cpu) === Number(cpu) && Number(s.memory_mb) === Number(mem))
    return Number(spec?.max_storage_gb || 0)
  }

  async function load() {
    const { data } = await api.get('/cloud/postgres', { params: { engine, reconcile: reconcile || undefined } })
    setRows(data.data || [])
    setCaps(data.caps || {})
    const h = await api.get('/cloud/postgres/health', { params: { engine } })
    setHealth(h.data.data || [])
    setQueued(h.data.queued || 0)
  }

  async function loadAccounts() {
    const { data } = await api.get('/cloud/accounts', { params: { vendor: 'tencent' } })
    const list = data.data || []
    setAccounts(list)
    return list as any[]
  }

  useEffect(() => {
    void load().catch((e) => message.error(e?.response?.data?.message || '加载失败'))
    void loadAccounts().catch(() => {})
  }, [reconcile, engine])

  function reportSync(data: any) {
    const r = data?.result
    const tips: string[] = []
    if (r?.skipped?.length) tips.push(`跳过：${r.skipped.join('；')}`)
    if (r?.errors?.length) tips.push(`失败：${r.errors.join('；')}`)
    if (tips.length) message.warning(`已同步 ${r?.synced || 0} 个账号。${tips.join('。')}`)
    else message.success('已同步')
  }

  async function runSync(accountIds: string[]) {
    setSyncing(true)
    try {
      const { data } = await api.post('/cloud/postgres/sync', { engine, account_ids: accountIds })
      reportSync(data)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  async function clickSync() {
    if (syncing) return
    let list = accounts
    try {
      list = await loadAccounts()
    } catch {
      /* 沿用已加载的账号 */
    }
    const ready = list.filter((a) => a.has_readonly)
    if (ready.length === 0) {
      message.warning(
        list.length
          ? `云账号 ${list.map((a) => a.name).join('、')} 未登记只读密钥，请到配置中心 · 云账号登记后再同步`
          : '没有可用的云账号，请到配置中心 · 云账号登记后再同步',
      )
      return
    }
    if (ready.length === 1 && list.length === 1) {
      void runSync([ready[0].id])
      return
    }
    setSyncIds([])
    setSyncOpen(true)
  }

  function onSyncPopover(next: boolean) {
    if (syncing) return
    if (!next) {
      setSyncOpen(false)
      return
    }
    void clickSync()
  }

  function confirmSync() {
    const ids = syncIds
    setSyncOpen(false)
    void runSync(ids)
  }

  async function assignAccount(row: any, accountId: string) {
    if (!accountId || accountId === row.account_id) return
    if (row.instance_id) {
      setAssigning(row.id)
      try {
        await api.post('/cloud/postgres/tickets', {
          action: 'rebind_account',
          account_id: accountId,
          cloud_instance_id: row.cloud_instance_id,
          vendor: row.vendor || 'tencent',
          engine,
          region: row.region,
          name: row.name,
        })
        message.success('已提交改绑工单，审批通过后才会更新平台实例')
      } catch (e: any) {
        message.error(e?.response?.data?.message || '提交失败')
      } finally {
        setAssigning('')
      }
      return
    }
    setAssigning(row.id)
    try {
      await api.put(`/cloud/postgres/resources/${row.id}/account`, { account_id: accountId })
      message.success('已指定归属账号，同云实例的重复行已合并')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '修改失败')
    } finally {
      setAssigning('')
    }
  }

  async function loadPurchase(accountId?: string, region?: string, vpcId?: string) {
    if (engine !== 'postgres' || !accountId || !region) {
      setCatalog(emptyCatalog)
      return
    }
    try {
      const { data } = await api.get('/cloud/postgres/purchase-options', { params: { account_id: accountId, region, vpc_id: vpcId || undefined, engine } })
      setCatalog({ ...emptyCatalog, ...(data.data || {}), errors: data.data?.errors || {} })
    } catch (e: any) {
      setCatalog({ ...emptyCatalog, errors: { zones: e?.response?.data?.message || '购买选项加载失败' } })
    }
  }

  async function loadSpecs(accountId?: string, region?: string, zone?: string) {
    if (!accountId || !String(zone || '').trim()) {
      setSpecs([])
      setSpecError('')
      return
    }
    try {
      const { data } = await api.get('/cloud/postgres/specs', { params: { account_id: accountId, region, zone, engine }, silent: true })
      setSpecs(data.data || [])
      setSpecError('')
    } catch (e: any) {
      setSpecs([])
      setSpecError(e?.response?.data?.message || '规格加载失败')
    }
  }

  async function loadParams(row: any) {
    if (!row?.cloud_instance_id) {
      setParams([])
      return
    }
    const { data } = await api.get(`/cloud/postgres/${row.cloud_instance_id}/parameters`, { params: { vendor: row.vendor || 'tencent', engine } })
    setParams(data.data || [])
  }

  function openAction(row: any, next: string) {
    setQuote(null)
    setSpecs([])
    setCatalog(emptyCatalog)
    setParams([])
    setSpecPick(undefined)
    form.setFieldsValue({
      action: next,
      account_id: row?.account_id,
      cloud_instance_id: row?.cloud_instance_id,
      region: row?.region,
      zone: row?.zone,
      standby_zone: undefined,
      vendor: row?.vendor || 'tencent',
      engine,
      name: next === 'create' ? undefined : row?.name,
      instance_type: row?.version,
      cpu: undefined,
      memory_mb: undefined,
      spec_code: undefined,
      disk_gb: next === 'scale_disk' ? undefined : (next === 'create' && engine === 'postgres' ? 100 : next === 'create' && engine === 'kafka' ? 200 : row?.disk_gb),
      before_cpu: row?.cpu,
      before_memory_mb: row?.memory_mb,
      before_disk_gb: row?.disk_gb,
      charge_type: 'POSTPAID_BY_HOUR',
      period: 1,
      charset: 'UTF8',
      sync_mode: 'Semi-sync',
      instance_count: 1,
      db_major_version: undefined,
      vpc_id: undefined,
      subnet_id: undefined,
      security_group_id: undefined,
      project_id: undefined,
      tag_key: undefined,
      tag_value: undefined,
      need_support_tde: false,
      deletion_protection: false,
      password_enc: undefined,
      password_confirm: undefined,
      admin_user: undefined,
      switch_tag: 0,
      switch_start: undefined,
      switch_end: undefined,
      param_name: undefined,
      param_value: undefined,
    })
    setOpen(true)
    if (next === 'create') {
      api.get('/cloud/accounts', { params: { vendor: 'tencent' } })
        .then(({ data }) => setAccounts(data.data || []))
        .catch(() => setAccounts([]))
    }
    if (engine !== 'kafka' && (next === 'scale_spec' || next === 'create' || (engine === 'postgres' && next === 'scale_disk'))) {
      void loadSpecs(row?.account_id, row?.region, row?.zone)
    }
    if (next === 'modify_param') {
      void loadParams(row).catch((e) => message.error(e?.response?.data?.message || '参数加载失败'))
    }
  }

  function allow(row: any, key: keyof Caps) {
    const c = caps[row?.vendor || 'tencent']
    if (!c) {
      if (key === 'restart') return engine !== 'redis'
      return key !== 'switchover' && key !== 'parameters'
    }
    if (key === 'restart') return c.restart !== false
    return !!c[key]
  }

  useEffect(() => { setPage(1) }, [keyword, reconcile, engine])

  const shown = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((r) => [r.name, r.cloud_instance_id, r.account_name, r.status, r.instance_name, r.region, intranetLabel(r)]
      .some((v) => String(v || '').toLowerCase().includes(q)))
  }, [rows, keyword])

  const showSpec = (action === 'create' || action === 'scale_spec') && engine !== 'kafka'
  const showDisk = action === 'create' || action === 'scale_disk' || action === 'scale_spec'

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0 }}>{title}</h2>
        <Popover
          title="选择要同步的云账号"
          trigger="click"
          placement="bottomLeft"
          open={syncOpen && !syncing}
          onOpenChange={onSyncPopover}
          content={
            <div style={{ width: 320 }}>
              <Select
                mode="multiple"
                style={{ width: '100%' }}
                placeholder="至少选择一个已登记只读密钥的账号"
                value={syncIds}
                onChange={setSyncIds}
                getPopupContainer={(node) => node.parentElement || document.body}
                options={accounts.map((a) => ({
                  value: a.id,
                  label: a.has_readonly ? a.name : `${a.name}（未登记只读密钥）`,
                  disabled: !a.has_readonly,
                }))}
              />
              <div style={{ marginTop: 12, textAlign: 'right' }}>
                <Space>
                  <Button size="small" onClick={() => setSyncOpen(false)}>取消</Button>
                  <Button size="small" type="primary" disabled={syncIds.length === 0} onClick={confirmSync}>确定</Button>
                </Space>
              </div>
            </div>
          }
        >
          <Button loading={syncing}>立即同步</Button>
        </Popover>
        <Auth urn={Perm.cloud.submit}>
          {(caps.tencent?.create !== false) && <Button type="primary" onClick={() => openAction(null, 'create')}>资源申请</Button>}
        </Auth>
        {engine === 'postgres' && <Link to="/ops/sql-insight">慢日志见 SQL 洞察 · 云慢日志</Link>}
      </Space>
      <Space style={{ marginBottom: 12 }} wrap>
        <span>同步健康</span>
        {health.map((h) => (
          <span key={h.account_id}>
            {h.name}：{h.status === 'no_key' ? '缺只读密钥(跳过)' : ({ ok: '正常', error: '失败', skipped: '未勾选', running: '执行中' } as Record<string, string>)[h.status] || h.status || '未同步'} {h.status === 'no_key' ? '' : (h.error || '')} {h.cursor_at || ''}
            {h.cursor_at ? ` 落后 ${h.lag_sec ?? 0} 秒` : ''}
          </span>
        ))}
        <span>排队 {queued}</span>
        <Select allowClear placeholder="对账状态" style={{ width: 160 }} value={reconcile || undefined} onChange={(v) => setReconcile(v || '')}
          options={Object.entries(reconcileLabel).map(([value, label]) => ({ value, label }))} />
        <Input
          allowClear
          style={{ width: 280 }}
          placeholder="搜索名称、云实例、账号、平台实例、内网地址"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
      </Space>
      <Table
        rowKey="id"
        dataSource={shown}
        scroll={{ x: engine === 'kafka' ? 1970 : 1300 }}
        pagination={{
          current: page,
          pageSize,
          showSizeChanger: true,
          pageSizeOptions: ['10', '20', '30', '50', '100'],
          showTotal: (n) => `共 ${n} 条`,
          onChange: (nextPage, nextSize) => {
            setPage(nextSize === pageSize ? nextPage : 1)
            setPageSize(nextSize)
          },
        }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '云实例', dataIndex: 'cloud_instance_id' },
          { title: '内网地址', width: 200, render: (_, r) => intranetLabel(r) || '—' },
          { title: '地域', dataIndex: 'region', render: (v) => v || '—' },
          {
            title: '账号',
            dataIndex: 'account_id',
            width: 200,
            render: (v, row) => (
              <AccountCell
                accountId={v}
                accountName={row.account_name}
                vendor={row.vendor}
                accounts={accounts}
                loading={assigning === row.id}
                disabled={!!assigning && assigning !== row.id}
                onChange={(id) => void assignAccount(row, id)}
              />
            ),
          },
          { title: '状态', dataIndex: 'status' },
          ...(engine === 'kafka' ? [] : [{ title: '规格', render: (_: unknown, r: { cpu?: number; memory_mb?: number }) => specText(r.cpu, r.memory_mb) }]),
          {
            title: '磁盘 GB',
            dataIndex: 'disk_gb',
            render: (v, r) => {
              if (engine === 'kafka') return v || '—'
              if (engine !== 'host') return v
              const lines = [
                systemDiskText(r.system_disk_gb, r.system_disk_type),
                ...(Array.isArray(r.data_disks) ? r.data_disks.map((d: { gb?: number; type?: string }) => diskLine('数据盘', d.gb, d.type)) : []),
              ].filter(Boolean)
              if (!lines.length) return v || '—'
              return (
                <div>
                  {lines.map((line, i) => (
                    <div key={i} style={{ whiteSpace: 'nowrap' }}>{line}</div>
                  ))}
                </div>
              )
            },
          },
          ...(engine === 'kafka'
            ? [
                { title: 'Topic 上限', dataIndex: 'kafka_topic_limit', width: 110, render: (v: number) => kafkaCount(v) },
                { title: 'Topic 使用', dataIndex: 'kafka_topic_used', width: 110, render: (v: number) => kafkaCount(v) },
                { title: '分区上限', dataIndex: 'kafka_partition_limit', width: 110, render: (v: number) => kafkaCount(v) },
                { title: '分区已用', dataIndex: 'kafka_partition_used', width: 110, render: (v: number) => kafkaCount(v) },
                { title: '版本', dataIndex: 'version', width: 100, render: (v: string) => v || '—' },
                { title: '集群类型', dataIndex: 'kafka_instance_type', width: 110, render: (v: string) => kafkaClusterLabel(v) },
              ]
            : []),
          {
            title: '平台实例',
            dataIndex: 'instance_name',
            render: (v, r) => r.instance_id
              ? <Link to="/config/instances">{v || r.instance_id}</Link>
              : '—',
          },
          {
            title: '对账',
            dataIndex: 'reconcile',
            width: 280,
            render: (v, r) => r.reconcile_note || reconcileLabel[v] || v || '—',
          },
          { title: '同步时间', dataIndex: 'synced_at' },
          {
            title: '操作',
            render: (_, row) => (
              <Auth urn={Perm.cloud.submit}>
                <Space>
                  {engine !== 'kafka' && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'scale_spec')}>变配</Button>}
                  {allow(row, 'restart') && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'restart')}>重启</Button>}
                  {allow(row, 'switchover') && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'switchover')}>切换</Button>}
                  {allow(row, 'parameters') && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'modify_param')}>参数</Button>}
                  {!row.instance_id && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'register_platform')}>同步到平台</Button>}
                </Space>
              </Auth>
            ),
          },
        ]}
      />
      <Modal title="提交云变更工单" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} width={pgCreate ? 860 : 720}>
        <Form form={form} layout="vertical" onFinish={async (v) => {
          if (engine === 'postgres' && v.action === 'create') {
            const reason = pgCreateError(v, specs)
            if (reason) {
              message.error(reason)
              return
            }
          }
          if (engine === 'kafka' && v.action === 'create') {
            if (!String(v.name || '').trim() || !String(v.zone || '').trim() || !String(v.vpc_id || '').trim() || !String(v.subnet_id || '').trim() || Number(v.disk_gb || 0) <= 0) {
              message.error('请填写名称、可用区 ID、磁盘、VPC 和子网')
              return
            }
          }
          const flexible = engine === 'postgres' || engine === 'mysql'
          if (flexible && v.action === 'scale_spec') {
            const cpu = Number(v.cpu || 0)
            const mem = Number(v.memory_mb || 0)
            const beforeCpu = Number(v.before_cpu || 0)
            const beforeMem = Number(v.before_memory_mb || 0)
            const beforeDiskGB = Number(v.before_disk_gb || 0)
            const disk = Number(v.disk_gb || 0)
            if ((cpu > 0) !== (mem > 0)) {
              message.error('请同时填写核数和内存')
              return
            }
            const specChanged = cpu > 0 && (cpu !== beforeCpu || mem !== beforeMem)
            const diskChanged = disk > 0 && disk !== beforeDiskGB
            if (specChanged && ((beforeCpu > 0 && cpu < beforeCpu) || (beforeMem > 0 && mem < beforeMem))) {
              message.error('目标规格不能小于当前规格')
              return
            }
            if (disk > 0 && beforeDiskGB > 0 && disk < beforeDiskGB) {
              message.error('磁盘只能扩容，目标不能小于当前值')
              return
            }
            if (!specChanged && !diskChanged) {
              message.error('没有需要执行的变更')
              return
            }
            if (engine === 'postgres') {
              const useCPU = specChanged ? cpu : beforeCpu
              const useMem = specChanged ? mem : beforeMem
              const useDisk = disk > 0 ? disk : beforeDiskGB
              const spec = specs.find((s) => Number(s.cpu) === useCPU && Number(s.memory_mb) === useMem)
              const limit = storageLimitMessage(useDisk, Number(spec?.max_storage_gb || 0))
              if (limit) {
                message.error(limit)
                return
              }
            }
          }
          if (v.action === 'scale_disk' && Number(v.disk_gb) <= Number(v.before_disk_gb || 0)) {
            message.error('磁盘只能扩大，目标必须大于当前值')
            return
          }
          if (engine === 'postgres' && v.action === 'scale_disk') {
            const cpu = v.action === 'scale_disk' ? Number(v.before_cpu || 0) : Number(v.cpu || 0)
            const mem = v.action === 'scale_disk' ? Number(v.before_memory_mb || 0) : Number(v.memory_mb || 0)
            const disk = Number(v.disk_gb || (v.action === 'scale_spec' ? v.before_disk_gb : 0) || 0)
            const spec = specs.find((s) => Number(s.cpu) === cpu && Number(s.memory_mb) === mem)
            const limit = storageLimitMessage(disk, Number(spec?.max_storage_gb || 0))
            if (limit) {
              message.error(limit)
              return
            }
          }
          if (engine === 'postgres' && (v.action === 'scale_spec' || v.action === 'scale_disk') && Number(v.switch_tag) === 1) {
            const start = String(v.switch_start || '').trim()
            const end = String(v.switch_end || '').trim()
            if (!switchClock.test(start) || !switchClock.test(end) || start >= end) {
              message.error('指定切换时间需要开始早于结束，格式为 HH:mm:ss')
              return
            }
          }
          const body: any = { ...v, params: v.param_name ? [{ name: v.param_name, value: v.param_value }] : undefined }
          delete body.password_confirm
          delete body.tag_key
          delete body.tag_value
          if (engine === 'postgres' && v.action === 'create' && String(v.tag_key || '').trim()) {
            body.tags = [{ key: String(v.tag_key).trim(), value: String(v.tag_value || '').trim() }]
          }
          await api.post('/cloud/postgres/tickets', body)
          message.success('已提交工单，审批通过后才会执行')
          setOpen(false)
        }}>
          <Form.Item name="action" label="动作"><Select options={actions} disabled /></Form.Item>
          <Form.Item name="vendor" hidden><Input /></Form.Item>
          <Form.Item name="engine" hidden><Input /></Form.Item>
          {engine === 'host' && action === 'scale_spec' && (
            <Form.Item name="instance_type" label="机型" rules={[{ required: true, message: '请选择机型' }]} extra="规格列表的 Label 含机型编码">
              <Select
                options={specs.map((s) => ({ value: String(s.label || '').split(' · ')[0] || `${s.cpu}-${s.memory_mb}`, label: s.label }))}
                onChange={(v) => {
                  const spec = specs.find((s) => String(s.label || '').startsWith(String(v)))
                  form.setFieldsValue({ instance_type: v, cpu: spec?.cpu, memory_mb: spec?.memory_mb })
                }}
              />
            </Form.Item>
          )}
          <Form.Item name="before_cpu" hidden><InputNumber /></Form.Item>
          <Form.Item name="before_memory_mb" hidden><InputNumber /></Form.Item>
          <Form.Item name="before_disk_gb" hidden><InputNumber /></Form.Item>
          {action === 'create' ? (
            <Form.Item name="account_id" label="云账号" rules={[{ required: true, message: '请选择已绑定的云账号' }]}>
              <Select
                options={accounts.map((a) => ({ value: a.id, label: a.name }))}
                onChange={(id) => {
                  const acc = accounts.find((a) => a.id === id)
                  const region = acc?.regions?.[0] || ''
                  form.setFieldsValue({
                    region,
                    vpc_id: undefined,
                    subnet_id: undefined,
                    zone: undefined,
                    standby_zone: undefined,
                    spec_code: undefined,
                    cpu: undefined,
                    memory_mb: undefined,
                    db_major_version: undefined,
                  })
                  setSpecPick(undefined)
                  if (engine === 'postgres') void loadPurchase(id, region)
                  if (engine !== 'kafka') void loadSpecs(id, region, '')
                }}
              />
            </Form.Item>
          ) : (
            <Form.Item name="account_id" label="云账号"><Input disabled /></Form.Item>
          )}
          {action !== 'create' && <Form.Item name="cloud_instance_id" label="云实例 ID"><Input disabled /></Form.Item>}
          {pgCreate && (
            <>
              <Form.Item name="charge_type" label="计费模式" rules={[{ required: true, message: '请选择计费模式' }]}>
                <Radio.Group>
                  <Radio value="PREPAID">包年包月</Radio>
                  <Radio value="POSTPAID_BY_HOUR">按量计费</Radio>
                </Radio.Group>
              </Form.Item>
              {chargeType === 'PREPAID' && (
                <Space align="start">
                  <Form.Item name="period" label="购买时长" rules={[{ required: true, message: '请选择购买时长' }]}>
                    <Select
                      style={{ width: 160 }}
                      options={[
                        { value: 1, label: '1 个月' },
                        { value: 2, label: '2 个月' },
                        { value: 5, label: '5 个月' },
                        { value: 12, label: '1 年' },
                        { value: 24, label: '2 年' },
                        { value: 36, label: '3 年' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="其他月数" extra="1 到 12，或 24、36">
                    <InputNumber min={1} max={36} onChange={(n) => { if (n) form.setFieldValue('period', n) }} />
                  </Form.Item>
                </Space>
              )}
              <Form.Item name="region" label="地域" extra={pickedAccount && accountRegions.length === 0 ? '云账号没有配置地域' : undefined} rules={[{ required: true, message: '请选择地域' }]}>
                <Select
                  options={accountRegions.map((r) => ({ value: r, label: r }))}
                  placeholder="选择云账号已配置的地域"
                  onChange={(region) => {
                    form.setFieldsValue({ vpc_id: undefined, subnet_id: undefined, zone: undefined, standby_zone: undefined, spec_code: undefined, cpu: undefined, memory_mb: undefined })
                    setSpecPick(undefined)
                    void loadPurchase(form.getFieldValue('account_id'), region)
                    void loadSpecs(form.getFieldValue('account_id'), region, '')
                  }}
                />
              </Form.Item>
              <Form.Item name="vpc_id" label="私有网络" extra={catalog.errors?.vpcs || ((catalog.vpcs || []).length === 0 ? '该地域没有私有网络，请先在云上准备' : undefined)} rules={[{ required: true, message: '请选择私有网络' }]}>
                <Select
                  options={(catalog.vpcs || []).map((v: any) => ({ value: v.id, label: v.name }))}
                  placeholder="选择私有网络"
                  onChange={(vpc) => {
                    form.setFieldValue('subnet_id', undefined)
                    void loadPurchase(form.getFieldValue('account_id'), form.getFieldValue('region'), vpc)
                  }}
                />
              </Form.Item>
              <Form.Item name="subnet_id" label="子网" extra={catalog.errors?.subnets || (pickedVpc && (catalog.subnets || []).length === 0 ? '该私有网络没有子网，请先在云上准备' : undefined)} rules={[{ required: true, message: '请选择子网' }]}>
                <Select
                  options={(catalog.subnets || []).map((s: any) => ({ value: s.id, label: `${s.name}${s.cidr ? ` ${s.cidr}` : ''}${s.zone ? ` ${s.zone}` : ''}${s.available_ip ? ` 可用 ${s.available_ip}` : ''}` }))}
                  placeholder="选择子网"
                />
              </Form.Item>
              <Form.Item label="数据库配置" extra="引擎 PostgreSQL，架构高可用版，硬盘类型本地 SSD">
                <Input disabled value="PostgreSQL / 高可用版 / 本地 SSD" />
              </Form.Item>
              <Space align="start" style={{ display: 'flex' }}>
                <Form.Item name="zone" label="主可用区" style={{ minWidth: 240 }} rules={[{ required: true, message: '请选择主可用区' }]}>
                  <Select
                    options={(catalog.zones || []).map((z: any) => ({ value: z.id, label: `${z.name} ${z.id}` }))}
                    placeholder={catalog.errors?.zones || '选择主可用区'}
                    onChange={(zone) => void loadSpecs(form.getFieldValue('account_id'), form.getFieldValue('region'), zone)}
                  />
                </Form.Item>
                <Form.Item name="standby_zone" label="备可用区" style={{ minWidth: 240 }} rules={[{ required: true, message: '请选择备可用区' }]}>
                  <Select options={(catalog.zones || []).map((z: any) => ({ value: z.id, label: `${z.name} ${z.id}` }))} placeholder="选择备可用区" />
                </Form.Item>
              </Space>
              <Form.Item name="db_major_version" label="数据库版本" extra={catalog.errors?.versions} rules={[{ required: true, message: '请选择数据库版本' }]}>
                <Select options={(catalog.versions || []).map((v: any) => ({ value: v.major, label: v.label || v.major }))} placeholder="选择数据库版本" />
              </Form.Item>
              <Form.Item name="spec_code" hidden><Input /></Form.Item>
              <Form.Item label="规格" extra={specError || (specMax(targetCPU, targetMem) ? `最大存储 ${specMax(targetCPU, targetMem)}GB` : '只列出云厂商返回的规格')}>
                <Select
                  value={specPick}
                  placeholder={specs.length ? '选择规格' : '该可用区没有返回规格'}
                  options={specs.map((s) => ({ value: s.spec_code || `${s.cpu}-${s.memory_mb}`, label: s.label || `${s.cpu}核 ${memoryText(s.memory_mb)}` }))}
                  onChange={(key) => {
                    setSpecPick(key)
                    const spec = specs.find((s) => (s.spec_code || `${s.cpu}-${s.memory_mb}`) === key)
                    if (!spec) {
                      form.setFieldsValue({ cpu: undefined, memory_mb: undefined, spec_code: undefined })
                      return
                    }
                    form.setFieldsValue({ cpu: spec.cpu, memory_mb: spec.memory_mb, spec_code: spec.spec_code })
                    const disk = Number(form.getFieldValue('disk_gb') || 0)
                    const limit = storageLimitMessage(disk, Number(spec.max_storage_gb || 0))
                    if (limit) message.error(limit)
                  }}
                />
              </Form.Item>
              <Space align="start">
                <Form.Item name="cpu" label="CPU"><InputNumber disabled /></Form.Item>
                <Form.Item name="memory_mb" label="内存 MB"><InputNumber disabled /></Form.Item>
              </Space>
              <Form.Item name="disk_gb" label="磁盘 GB" extra="本地 SSD，按 10GB 步进，默认 100GB" rules={[{ required: true, message: '请填写磁盘' }]}>
                <InputNumber min={10} step={10} max={specMax(targetCPU, targetMem) || undefined} />
              </Form.Item>
              <Form.Item name="name" label="实例名" extra="不填则创建后再命名"><Input /></Form.Item>
              <Form.Item name="sync_mode" label="数据复制方式" rules={[{ required: true, message: '请选择数据复制方式' }]}>
                <Radio.Group>
                  <Radio value="Async">异步</Radio>
                  <Radio value="Semi-sync">半同步</Radio>
                </Radio.Group>
              </Form.Item>
              <Form.Item name="charset" label="字符集" rules={[{ required: true, message: '请选择字符集' }]}>
                <Radio.Group>
                  <Radio value="UTF8">UTF8</Radio>
                  <Radio value="LATIN1">LATIN1</Radio>
                </Radio.Group>
              </Form.Item>
              <Form.Item name="admin_user" label="管理员账号" rules={[{ required: true, message: '请填写管理员账号' }]}><Input /></Form.Item>
              <Form.Item name="password_enc" label="管理员密码" extra="只在创建时填写，保存前会加密" rules={[{ required: true, message: '请填写管理员密码' }]}><Input.Password /></Form.Item>
              <Form.Item name="password_confirm" label="确认密码" rules={[{ required: true, message: '请再次填写密码' }]}><Input.Password /></Form.Item>
              <Collapse items={[{
                key: 'advanced',
                label: '高级配置',
                children: (
                  <>
                    <Form.Item name="security_group_id" label="安全组" extra={catalog.errors?.security_groups || '不选则使用云上默认安全组'}>
                      <Select allowClear options={(catalog.security_groups || []).map((g: any) => ({ value: g.id, label: `${g.name} ${g.id}` }))} placeholder="选择安全组" />
                    </Form.Item>
                    <Space align="start">
                      <Form.Item name="tag_key" label="标签键"><Input /></Form.Item>
                      <Form.Item name="tag_value" label="标签值"><Input /></Form.Item>
                    </Space>
                    <Form.Item name="project_id" label="项目" extra={catalog.errors?.projects}>
                      <Select allowClear options={(catalog.projects || []).map((p: any) => ({ value: p.id, label: p.name }))} placeholder="默认项目" />
                    </Form.Item>
                    <Form.Item name="need_support_tde" label="数据透明加密" valuePropName="checked"><Switch /></Form.Item>
                    <Form.Item name="deletion_protection" label="删除保护" valuePropName="checked"><Switch /></Form.Item>
                    <Form.Item name="instance_count" label="购买数量"><InputNumber min={1} max={10} /></Form.Item>
                  </>
                ),
              }]} />
            </>
          )}
          {!pgCreate && <Form.Item name="region" label="地域"><Input /></Form.Item>}
          {!pgCreate && (action === 'create' || action === 'scale_spec') && (
            <Form.Item name="zone" label={engine === 'kafka' ? '可用区 ID' : '可用区'} extra={engine === 'kafka' ? '填写腾讯云可用区数字 ID，例如 100003' : undefined} rules={engine === 'kafka' && action === 'create' ? [{ required: true, message: '请填写可用区 ID' }] : undefined}>
              <Input onBlur={() => { if (engine !== 'kafka') void loadSpecs(form.getFieldValue('account_id'), form.getFieldValue('region'), form.getFieldValue('zone')) }} />
            </Form.Item>
          )}
          {!pgCreate && action !== 'create' && action !== 'scale_spec' && <Form.Item name="zone" hidden><Input /></Form.Item>}
          {!pgCreate && action === 'create' && <Form.Item name="name" label="实例名" rules={engine === 'kafka' ? [{ required: true, message: '请填写实例名' }] : undefined}><Input /></Form.Item>}
          {engine === 'kafka' && action === 'create' && (
            <>
              <Form.Item name="vpc_id" label="VPC" rules={[{ required: true, message: '请填写 VPC' }]}><Input placeholder="vpc-xxxxxxxx" /></Form.Item>
              <Form.Item name="subnet_id" label="子网" rules={[{ required: true, message: '请填写子网' }]}><Input placeholder="subnet-xxxxxxxx" /></Form.Item>
            </>
          )}
          {action === 'scale_spec' && (
            <Form.Item label="原规格">
              <Input disabled value={beforeCPU || beforeMem ? specText(beforeCPU, beforeMem) : '—'} />
            </Form.Item>
          )}
          {showSpec && !pgCreate && (
            <Form.Item label={action === 'scale_spec' ? '目标规格' : '规格'} extra={action === 'scale_spec' && (engine === 'postgres' || engine === 'mysql') ? '不选或与原规格相同则不调整规格' : '只列出云厂商返回的规格'}>
              <Select
                allowClear={action === 'scale_spec' && (engine === 'postgres' || engine === 'mysql')}
                value={specPick}
                placeholder={specs.length ? '选择规格' : '该可用区没有返回规格'}
                options={specs.map((s) => ({ value: `${s.cpu}-${s.memory_mb}`, label: s.label || `${s.cpu}核 ${s.memory_mb}MB` }))}
                onChange={(key) => {
                  setSpecPick(key)
                  const spec = specs.find((s) => `${s.cpu}-${s.memory_mb}` === key)
                  if (!spec) {
                    form.setFieldsValue({ cpu: undefined, memory_mb: undefined })
                    return
                  }
                  form.setFieldsValue({ cpu: spec.cpu, memory_mb: spec.memory_mb })
                  const disk = Number(form.getFieldValue('disk_gb') || form.getFieldValue('before_disk_gb') || 0)
                  const limit = storageLimitMessage(disk, Number(spec.max_storage_gb || 0))
                  if (limit) message.error(limit)
                }}
              />
            </Form.Item>
          )}
          {showSpec && !pgCreate && <Form.Item name="cpu" label="CPU" rules={[{ required: action === 'create' || (action === 'scale_spec' && engine !== 'postgres' && engine !== 'mysql'), message: '请选择规格' }]}><InputNumber min={1} disabled /></Form.Item>}
          {showSpec && !pgCreate && <Form.Item name="memory_mb" label="内存 MB" rules={[{ required: action === 'create' || (action === 'scale_spec' && engine !== 'postgres' && engine !== 'mysql'), message: '请选择规格' }]}><InputNumber min={1} disabled /></Form.Item>}
          {showDisk && !pgCreate && (
            <Form.Item
              name="disk_gb"
              label="磁盘 GB"
              extra={
                action === 'scale_disk'
                  ? `当前 ${beforeDisk || 0} GB，目标必须更大${specMax(beforeCPU, beforeMem) ? `，当前规格最大存储 ${specMax(beforeCPU, beforeMem)}GB` : ''}`
                  : action === 'scale_spec'
                    ? `当前 ${beforeDisk || 0} GB，不改则不扩容${(() => {
                        const changed = targetCPU > 0 && targetMem > 0 && (targetCPU !== beforeCPU || targetMem !== beforeMem)
                        const max = specMax(changed ? targetCPU : beforeCPU, changed ? targetMem : beforeMem)
                        return max ? `，该规格最大存储 ${max}GB` : ''
                      })()}`
                    : undefined
              }
              rules={action === 'scale_disk' || (engine === 'kafka' && action === 'create') ? [{ required: true, message: '请填写磁盘' }] : undefined}
            >
              <InputNumber min={action === 'scale_disk' ? (beforeDisk || 0) + 1 : (action === 'scale_spec' ? beforeDisk || 0 : 0)} />
            </Form.Item>
          )}
          {!pgCreate && engine !== 'kafka' && action === 'create' && <Form.Item name="admin_user" label="管理员账号"><Input /></Form.Item>}
          {!pgCreate && engine !== 'kafka' && action === 'create' && <Form.Item name="password_enc" label="管理员密码" extra="只在创建时填写，保存前会加密"><Input.Password /></Form.Item>}
          {action === 'modify_param' && (
            <>
              <Table
                size="small"
                rowKey="name"
                dataSource={params}
                pagination={{ pageSize: 8 }}
                columns={[
                  { title: '参数', dataIndex: 'name' },
                  { title: '当前值', dataIndex: 'value' },
                ]}
              />
              <Form.Item name="param_name" label="要修改的参数" rules={[{ required: true, message: '请填写参数名' }]}><Input /></Form.Item>
              <Form.Item name="param_value" label="目标值" rules={[{ required: true, message: '请填写参数值' }]}><Input /></Form.Item>
            </>
          )}
          {engine === 'postgres' && (action === 'scale_spec' || action === 'scale_disk') && (
            <>
              <Form.Item
                name="switch_tag"
                label="切换时间"
                extra="调整过程中可能迁移数据，期间访问不受影响；迁移完成后按所选时间切换，会有秒级闪断。"
              >
                <Radio.Group>
                  <Radio value={0}>升级完成时</Radio>
                  <Radio value={1}>指定时间</Radio>
                  <Radio value={2}>维护时间内</Radio>
                </Radio.Group>
              </Form.Item>
              {switchTag === 1 && (
                <Space align="start">
                  <Form.Item name="switch_start" label="开始" rules={[{ required: true, message: '请填写开始时间' }]}>
                    <Input placeholder="02:00:00" style={{ width: 140 }} />
                  </Form.Item>
                  <Form.Item name="switch_end" label="结束" rules={[{ required: true, message: '请填写结束时间' }]}>
                    <Input placeholder="06:00:00" style={{ width: 140 }} />
                  </Form.Item>
                </Space>
              )}
            </>
          )}
          <Button onClick={async () => {
            const v = form.getFieldsValue()
            if (engine === 'postgres' && v.action === 'create') {
              const reason = pgCreateError(v, specs)
              if (reason) {
                message.error(reason)
                return
              }
            }
            try {
              const body = { ...v }
              delete body.password_confirm
              delete body.tag_key
              delete body.tag_value
              if (String(v.tag_key || '').trim()) body.tags = [{ key: String(v.tag_key).trim(), value: String(v.tag_value || '').trim() }]
              const { data } = await api.post('/cloud/postgres/quote', body)
              setQuote(data)
            } catch (e: any) {
              message.error(e?.response?.data?.message || '询价失败')
            }
          }}>询价 / 预检</Button>
          {quote && <p>{quote.impact}；费用差额 {quote.data?.amount ?? '—'}；{quote.blip ? '会造成闪断' : '默认不闪断'}</p>}
        </Form>
      </Modal>
    </div>
  )
}
