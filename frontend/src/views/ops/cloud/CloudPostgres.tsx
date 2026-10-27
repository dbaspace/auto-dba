import React, { useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Select, Space, Table, message } from 'antd'
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
  const [open, setOpen] = useState(false)
  const [quote, setQuote] = useState<any>(null)
  const [specs, setSpecs] = useState<any[]>([])
  const [accounts, setAccounts] = useState<any[]>([])
  const [params, setParams] = useState<any[]>([])
  const [assigning, setAssigning] = useState<string>('')
  const [syncOpen, setSyncOpen] = useState(false)
  const [syncIds, setSyncIds] = useState<string[]>([])
  const [syncing, setSyncing] = useState(false)
  const [form] = Form.useForm()
  const action = Form.useWatch('action', form)
  const beforeDisk = Number(Form.useWatch('before_disk_gb', form) || 0)

  async function load() {
    const { data } = await api.get('/cloud/postgres', { params: { engine, reconcile: reconcile || undefined } })
    setRows(data.data || [])
    setCaps(data.caps || {})
    const h = await api.get('/cloud/postgres/health', { params: { engine } })
    setHealth(h.data.data || [])
    setQueued(h.data.queued || 0)
  }

  async function loadAccounts() {
    const { data } = await api.get('/cloud/accounts', { params: { mine: 1, vendor: 'tencent' } })
    setAccounts(data.data || [])
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
      setSyncOpen(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '同步失败')
    } finally {
      setSyncing(false)
    }
  }

  function clickSync() {
    const ready = accounts.filter((a) => a.has_readonly)
    if (ready.length === 0) {
      message.warning('没有已登记只读密钥的云账号，请到配置中心 · 云账号登记后再同步')
      return
    }
    if (ready.length === 1) {
      void runSync([ready[0].id])
      return
    }
    setSyncIds([])
    setSyncOpen(true)
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

  async function loadSpecs(accountId?: string, region?: string, zone?: string) {
    if (!accountId) {
      setSpecs([])
      return
    }
    const { data } = await api.get('/cloud/postgres/specs', { params: { account_id: accountId, region, zone, engine } })
    setSpecs(data.data || [])
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
    setParams([])
    form.setFieldsValue({
      action: next,
      account_id: row?.account_id,
      cloud_instance_id: row?.cloud_instance_id,
      region: row?.region,
      zone: row?.zone,
      vendor: row?.vendor || 'tencent',
      engine,
      name: row?.name,
      instance_type: row?.version,
      cpu: undefined,
      memory_mb: undefined,
      disk_gb: next === 'scale_disk' ? undefined : row?.disk_gb,
      before_cpu: row?.cpu,
      before_memory_mb: row?.memory_mb,
      before_disk_gb: row?.disk_gb,
      param_name: undefined,
      param_value: undefined,
    })
    setOpen(true)
    if (next === 'create') {
      api.get('/cloud/accounts', { params: { mine: 1, vendor: 'tencent' } })
        .then(({ data }) => setAccounts(data.data || []))
        .catch(() => setAccounts([]))
    }
    if (next === 'scale_spec' || next === 'create') {
      void loadSpecs(row?.account_id, row?.region, row?.zone).catch((e) => message.error(e?.response?.data?.message || '规格加载失败'))
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

  const showSpec = action === 'create' || action === 'scale_spec'
  const showDisk = action === 'create' || action === 'scale_disk' || action === 'scale_spec'

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0 }}>{title}</h2>
        <Button loading={syncing} onClick={clickSync}>立即同步</Button>
        <Auth urn={Perm.cloud.submit}>
          {(caps.tencent?.create !== false) && <Button type="primary" onClick={() => openAction(null, 'create')}>资源申请</Button>}
        </Auth>
        {engine === 'postgres' && <Link to="/ops/sql-insight">慢日志见 SQL 洞察 · 云慢日志</Link>}
      </Space>
      <Space style={{ marginBottom: 12 }} wrap>
        <span>同步健康</span>
        {health.map((h) => (
          <span key={h.account_id}>
            {h.name}：{h.status === 'no_key' ? '缺只读密钥(跳过)' : (h.status || '未同步')} {h.status === 'no_key' ? '' : (h.error || '')} {h.cursor_at || ''}
            {h.cursor_at ? ` 落后 ${h.lag_sec ?? 0} 秒` : ''}
          </span>
        ))}
        <span>排队 {queued}</span>
        <Select allowClear placeholder="对账状态" style={{ width: 160 }} value={reconcile || undefined} onChange={(v) => setReconcile(v || '')}
          options={Object.entries(reconcileLabel).map(([value, label]) => ({ value, label }))} />
      </Space>
      <Table
        rowKey="id"
        dataSource={rows}
        scroll={{ x: 1100 }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '云实例', dataIndex: 'cloud_instance_id' },
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
          { title: '规格', render: (_, r) => `${r.cpu || 0}核 / ${r.memory_mb || 0}MB` },
          { title: '磁盘 GB', dataIndex: 'disk_gb' },
          {
            title: '平台实例',
            dataIndex: 'instance_name',
            render: (v, r) => r.instance_id
              ? <Link to="/config/instances">{v || r.instance_id}</Link>
              : '—',
          },
          { title: '对账', dataIndex: 'reconcile', render: (v) => reconcileLabel[v] || v || '—' },
          { title: '同步时间', dataIndex: 'synced_at' },
          {
            title: '操作',
            render: (_, row) => (
              <Auth urn={Perm.cloud.submit}>
                <Space>
                  <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'scale_spec')}>变配</Button>
                  {engine !== 'redis' && engine !== 'host' && <Button size="small" disabled={row.reconcile === 'conflict'} onClick={() => openAction(row, 'scale_disk')}>扩盘</Button>}
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
      <Modal
        title="选择要同步的云账号"
        open={syncOpen}
        confirmLoading={syncing}
        okButtonProps={{ disabled: syncIds.length === 0 }}
        onCancel={() => setSyncOpen(false)}
        onOk={() => void runSync(syncIds)}
      >
        <Select
          mode="multiple"
          style={{ width: '100%' }}
          placeholder="至少选择一个已登记只读密钥的账号"
          value={syncIds}
          onChange={setSyncIds}
          options={accounts.filter((a) => a.has_readonly).map((a) => ({ value: a.id, label: a.name }))}
        />
      </Modal>
      <Modal title="提交云变更工单" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} width={720}>
        <Form form={form} layout="vertical" onFinish={async (v) => {
          if (v.action === 'scale_disk' && Number(v.disk_gb) <= Number(v.before_disk_gb || 0)) {
            message.error('磁盘只能扩大，目标必须大于当前值')
            return
          }
          await api.post('/cloud/postgres/tickets', { ...v, params: v.param_name ? [{ name: v.param_name, value: v.param_value }] : undefined })
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
                  form.setFieldValue('region', region)
                  void loadSpecs(id, region, form.getFieldValue('zone'))
                }}
              />
            </Form.Item>
          ) : (
            <Form.Item name="account_id" label="云账号"><Input disabled /></Form.Item>
          )}
          {action !== 'create' && <Form.Item name="cloud_instance_id" label="云实例 ID"><Input disabled /></Form.Item>}
          <Form.Item name="region" label="地域"><Input /></Form.Item>
          {(action === 'create' || action === 'scale_spec') && <Form.Item name="zone" label="可用区"><Input onBlur={() => void loadSpecs(form.getFieldValue('account_id'), form.getFieldValue('region'), form.getFieldValue('zone'))} /></Form.Item>}
          {action === 'create' && <Form.Item name="name" label="实例名"><Input /></Form.Item>}
          {showSpec && (
            <Form.Item label="规格" extra="只列出云厂商返回的规格">
              <Select
                placeholder={specs.length ? '选择规格' : '暂无规格，请先填写账号和可用区'}
                options={specs.map((s) => ({ value: `${s.cpu}-${s.memory_mb}`, label: s.label || `${s.cpu}核 ${s.memory_mb}MB` }))}
                onChange={(key) => {
                  const spec = specs.find((s) => `${s.cpu}-${s.memory_mb}` === key)
                  if (spec) form.setFieldsValue({ cpu: spec.cpu, memory_mb: spec.memory_mb })
                }}
              />
            </Form.Item>
          )}
          {showSpec && <Form.Item name="cpu" label="CPU" rules={[{ required: action === 'scale_spec' || action === 'create', message: '请选择规格' }]}><InputNumber min={1} disabled /></Form.Item>}
          {showSpec && <Form.Item name="memory_mb" label="内存 MB" rules={[{ required: action === 'scale_spec' || action === 'create', message: '请选择规格' }]}><InputNumber min={1} disabled /></Form.Item>}
          {showDisk && (
            <Form.Item
              name="disk_gb"
              label="磁盘 GB"
              extra={action === 'scale_disk' ? `当前 ${beforeDisk || 0} GB，目标必须更大` : undefined}
              rules={action === 'scale_disk' ? [{ required: true, message: '请填写目标磁盘' }] : undefined}
            >
              <InputNumber min={action === 'scale_disk' ? (beforeDisk || 0) + 1 : 0} />
            </Form.Item>
          )}
          {action === 'create' && <Form.Item name="admin_user" label="管理员账号"><Input /></Form.Item>}
          {action === 'create' && <Form.Item name="password_enc" label="管理员密码" extra="只在创建时填写，保存前会加密"><Input.Password /></Form.Item>}
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
          <Button onClick={async () => {
            const v = form.getFieldsValue()
            const { data } = await api.post('/cloud/postgres/quote', v)
            setQuote(data)
          }}>询价 / 预检</Button>
          {quote && <p>{quote.impact}；费用差额 {quote.data?.amount ?? '—'}；{quote.blip ? '会造成闪断' : '默认不闪断'}</p>}
        </Form>
      </Modal>
    </div>
  )
}
