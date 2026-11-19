import React, { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Pagination,
  Select,
  Space,
  Tabs,
  Tag,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useAppSelector } from '../../store'
import { permissionLabel, permissionOptions } from '../../utils/dbPermissions'
import './Accounts.css'

const defaultTabs = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
]

function emptyForm() {
  return {
    id: '',
    instance_id: '',
    db_name: '',
    username: '',
    password: '',
    permissions: [] as string[],
    description: '',
  }
}

function fmtTime(v: any) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function domainText(row: any) {
  const host = row.domain_name || row.host
  if (!host) return row.instance_name || '—'
  return row.port ? `${host}:${row.port}` : host
}

function instLabel(i: any) {
  const addr = i.host ? `${i.host}:${i.port}` : i.domain_name || ''
  return i.name ? (addr ? `${i.name} (${addr})` : i.name) : addr
}

function permText(list: any[]) {
  return (list || []).map((v) => permissionLabel(String(v))).join('， ') || '—'
}

export default function Accounts() {
  const user = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenantId = (user as { current_tenant?: { id?: string } })?.current_tenant?.id || ''
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [envOptions, setEnvOptions] = useState<string[]>([])
  const [tabs, setTabs] = useState(defaultTabs)
  const [activeType, setActiveType] = useState('mysql')
  const [idc, setIdc] = useState('')
  const [dbName, setDbName] = useState('')
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formVisible, setFormVisible] = useState(false)
  const [viewVisible, setViewVisible] = useState(false)
  const [viewing, setViewing] = useState<any>(null)
  const [plainPassword, setPlainPassword] = useState('')
  const [pwdLoading, setPwdLoading] = useState(false)
  const [instDbs, setInstDbs] = useState<any[]>([])
  const [form, setForm] = useState(emptyForm())
  const [antdForm] = Form.useForm()

  const formInstances = useMemo(
    () => instances.filter((i) => String(i.db_type || '').toLowerCase() === activeType),
    [instances, activeType],
  )

  const filteredRows = useMemo(() => {
    const kw = String(q || '').trim().toLowerCase()
    return rows.filter((r) => {
      if (activeType && String(r.db_type || '').toLowerCase() !== activeType) return false
      if (idc && r.idc !== idc) return false
      if (dbName && r.db_name !== dbName) return false
      if (kw && !`${r.username} ${r.db_name} ${r.description || ''}`.toLowerCase().includes(kw)) return false
      return true
    })
  }, [rows, activeType, idc, dbName, q])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  useEffect(() => {
    setPage(1)
  }, [activeType, idc, dbName, q])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(filteredRows.length / pageSize) || 1)
    setPage((current) => (current > maxPage ? maxPage : current))
  }, [filteredRows.length, pageSize])

  const dbNameOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of rows) {
      if (activeType && String(r.db_type || '').toLowerCase() !== activeType) continue
      if (r.db_name) set.add(r.db_name)
    }
    for (const d of databases) {
      const inst = instances.find((i) => i.id === d.instance_id)
      if (inst && String(inst.db_type || '').toLowerCase() === activeType && d.db_name) set.add(d.db_name)
    }
    return [...set]
  }, [rows, databases, instances, activeType])

  function typeCount(t: string) {
    return rows.filter((r) => String(r.db_type || '').toLowerCase() === t).length
  }

  function typeLabel(t: string) {
    return tabs.find((x) => x.value === t)?.label || t || '—'
  }

  function onInstChange(instance_id: string) {
    setForm((f) => ({ ...f, instance_id, db_name: '' }))
    antdForm.setFieldsValue({ instance_id, db_name: '' })
    setInstDbs(databases.filter((d) => d.instance_id === instance_id))
  }

  function open(row?: any) {
    if (row) {
      const next = {
        id: row.id,
        instance_id: row.instance_id,
        db_name: row.db_name,
        username: row.username,
        password: '',
        permissions: [...(row.permissions || [])],
        description: row.description || '',
      }
      setForm(next)
      antdForm.setFieldsValue(next)
      setInstDbs(databases.filter((d) => d.instance_id === row.instance_id))
    } else {
      const next = emptyForm()
      setForm(next)
      antdForm.setFieldsValue(next)
      setInstDbs([])
    }
    setFormVisible(true)
  }

  async function save() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    setSaving(true)
    try {
      const payload = {
        instance_id: form.instance_id,
        db_name: form.db_name,
        username: form.username,
        password: form.password,
        permissions: form.permissions,
        description: form.description,
      }
      if (form.id) {
        await api.put('/db-accounts/' + form.id, payload)
        message.success('已保存')
      } else {
        await api.post('/db-accounts', payload)
        message.success('已登记')
      }
      setFormVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function view(row: any) {
    setViewing(row)
    setPlainPassword('')
    setViewVisible(true)
  }

  async function revealPwd() {
    if (!viewing?.id) return
    setPwdLoading(true)
    try {
      const { data } = await api.get('/db-accounts/' + viewing.id, { params: { view_credential: 1 } })
      setPlainPassword(data.real_password || '')
      if (!data.real_password) message.warning('未存储密码')
    } catch (e: any) {
      const code = e?.response?.data?.code
      if (code === 'step_up_required') {
        Modal.confirm({
          title: '二次验证',
          content: (
            <Input.Password
              id="step-up-pwd"
              placeholder="请输入登录密码（LDAP/SSO 账号可留空并确认）"
              autoComplete="current-password"
            />
          ),
          onOk: async () => {
            const el = document.getElementById('step-up-pwd') as HTMLInputElement | null
            const password = el?.value || ''
            try {
              await api.post('/auth/step-up', password ? { password } : { confirm: true })
              message.success('验证成功')
              await revealPwd()
            } catch (err: any) {
              message.error(err?.response?.data?.message || '验证失败')
              return Promise.reject(err)
            }
          },
        })
      } else {
        message.error(e?.response?.data?.message || '无法查看密码')
      }
    } finally {
      setPwdLoading(false)
    }
  }

  async function copyPwd() {
    try {
      await navigator.clipboard.writeText(plainPassword)
      message.success('已复制')
    } catch {
      message.warning('复制失败，请手动选择')
    }
  }

  function remove(row: any) {
    Modal.confirm({
      title: '删除确认',
      content: `确定删除账号 ${row.username}？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/db-accounts/' + row.id)
        message.success('已删除')
        await load()
      },
    })
  }

  async function load() {
    setLoading(true)
    try {
      const [accRes, instRes, dbRes, envRes, argRes] = await Promise.all([
        api.get('/db-accounts'),
        api.get('/instances'),
        api.get('/databases'),
        api.get('/environments'),
        api.get('/global-args').catch(() => ({ data: { data: [] } })),
      ])
      const accList = accRes.data.data || []
      const instList = instRes.data.data || instRes.data || []
      setRows(accList)
      setInstances(instList)
      setDatabases(dbRes.data.data || dbRes.data || [])
      const envRows = envRes.data.data || envRes.data || []
      const codes = new Set(envRows.map((e: any) => e.code).filter(Boolean))
      for (const i of instList) if (i.idc) codes.add(i.idc)
      for (const r of accList) if (r.idc) codes.add(r.idc)
      setEnvOptions([...codes] as string[])
      const args = argRes.data.data || argRes.data || []
      const raw =
        (Array.isArray(args) ? args : []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
      let nextTabs = defaultTabs
      try {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed) && parsed.length) {
          nextTabs = parsed
            .filter((t: any) => t.value && t.value !== 'kafka' && t.value !== 'redis')
            .map((t: any) => ({ value: t.value, label: t.label || t.value }))
        }
      } catch {
        /* keep default */
      }
      setTabs(nextTabs)
      if (!nextTabs.some((t) => t.value === activeType) && nextTabs[0]) {
        setActiveType(nextTabs[0].value)
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="ops-page">
      <p className="hint">新增数据库工单审批通过后会自动写入此处；密码以 AES-256-GCM 加密存储，查看时再解密。</p>
      {impersonating ? (
        <p className="hint" style={{ color: 'var(--ant-color-text-secondary, #666)' }}>
          租户模式：列表含全平台账号（含所属租户）。划拨请在实例页操作，账号随实例租户归属。
        </p>
      ) : null}
      <Tabs
        activeKey={activeType}
        onChange={setActiveType}
        className="inst-tabs"
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
      <div className="ops-filters">
        <div className="filter-item">
          <Select
            allowClear
            showSearch
            placeholder="选择环境"
            style={{ width: 160 }}
            value={idc || undefined}
            onChange={(v) => setIdc(v || '')}
            options={envOptions.map((e) => ({ value: e, label: e }))}
          />
        </div>
        <div className="filter-item">
          <Select
            allowClear
            showSearch
            placeholder="选择数据库"
            style={{ width: 220 }}
            value={dbName || undefined}
            onChange={(v) => setDbName(v || '')}
            options={dbNameOptions.map((n) => ({ value: n, label: n }))}
          />
        </div>
        <div className="filter-item">
          <Input
            allowClear
            placeholder="搜索用户名、库名、描述"
            style={{ width: 240 }}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
        <div className="toolbar" style={{ margin: 0, flexShrink: 0 }}>
          <Auth urn={Perm.accounts.create}>
            <Button type="primary" onClick={() => open()}>
              登记账号
            </Button>
          </Auth>
        </div>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || `${r.username}-${r.db_name}`}
          dataSource={pagedRows}
          loading={loading}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            {
              title: '账号类型',
              width: 120,
              render: (_, row) => <Tag color="processing">{typeLabel(row.db_type)}</Tag>,
            },
            ...(impersonating
              ? [
                  {
                    title: '所属租户',
                    width: 120,
                    render: (_: unknown, row: any) => {
                      const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                      const label = row.tenant_name || row.tenant_id || '—'
                      return same ? <Tag color="green">{label}</Tag> : <Tag color="orange">{label}</Tag>
                    },
                  },
                ]
              : []),
            {
              title: '用户名',
              render: (_, row) => (
                <Button type="link" onClick={() => view(row)}>
                  {row.username}
                </Button>
              ),
            },
            {
              title: '数据库',
              render: (_, row) => <Tag color="processing">{row.db_name || '—'}</Tag>,
            },
            {
              title: '域名',
              render: (_, row) => domainText(row),
            },
            {
              title: 'IDC',
              width: 100,
              render: (_, row) => (row.idc ? <Tag color="success">{row.idc}</Tag> : '—'),
            },
            { title: '描述', dataIndex: 'description', ellipsis: true },
            {
              title: '创建时间',
              width: 170,
              render: (_, row) => fmtTime(row.created_at),
            },
            {
              title: '操作',
              width: 180,
              fixed: 'right',
              render: (_, row) => (
                <Space size={0}>
                  <Button type="link" onClick={() => view(row)}>
                    查看
                  </Button>
                  <Auth urn={Perm.accounts.edit}>
                    <Button type="link" onClick={() => open(row)}>
                      编辑
                    </Button>
                  </Auth>
                  <Auth urn={Perm.accounts.delete}>
                    <Button type="link" danger onClick={() => remove(row)}>
                      删除
                    </Button>
                  </Auth>
                </Space>
              ),
            },
          ]}
        />
      </div>
      <div className="pager" style={{ gap: 12, alignItems: 'center' }}>
        <span className="pager-total">共 {filteredRows.length} 条</span>
        <Pagination
          current={page}
          pageSize={pageSize}
          total={filteredRows.length}
          showSizeChanger
          pageSizeOptions={['10', '20', '30', '50', '100']}
          onChange={(p, ps) => {
            setPage(p)
            if (ps && ps !== pageSize) setPageSize(ps)
          }}
        />
      </div>

      <Modal
        open={formVisible}
        title={form.id ? '编辑账号' : '登记账号'}
        width={560}
        destroyOnClose
        onCancel={() => setFormVisible(false)}
        footer={[
          <Button key="c" onClick={() => setFormVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={saving} onClick={save}>
            保存
          </Button>,
        ]}
      >
        <Form
          form={antdForm}
          labelCol={{ style: { width: 100 } }}
          initialValues={form}
          onValuesChange={(_, all) => setForm((f) => ({ ...f, ...all }))}
        >
          {!form.id ? (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              message="只把已有账号登记到平台，不在数据库上创建用户，也不提交工单。"
            />
          ) : null}
          <Form.Item name="instance_id" label="实例" rules={[{ required: true, message: '请选择实例' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              disabled={!!form.id}
              style={{ width: '100%' }}
              onChange={onInstChange}
              options={formInstances.map((i) => ({ value: i.id, label: instLabel(i) }))}
            />
          </Form.Item>
          <Form.Item name="db_name" label="数据库" rules={[{ required: true, message: '请选择数据库' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              style={{ width: '100%' }}
              options={instDbs.map((d) => ({ value: d.db_name, label: d.db_name }))}
            />
          </Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input disabled={!!form.id} />
          </Form.Item>
          <Form.Item
            name="password"
            label={form.id ? '新密码' : '密码'}
            rules={form.id ? [] : [{ required: true, whitespace: true, message: '请填写该账号在数据库里的现有密码' }]}
          >
            <Input.Password placeholder={form.id ? '留空则不修改' : '填写该账号在数据库里的现有密码'} />
          </Form.Item>
          <Form.Item
            name="permissions"
            label="账号权限"
            rules={[{ type: 'array', required: true, min: 1, message: '请选择账号权限' }]}
          >
            <Select
              mode="multiple"
              style={{ width: '100%' }}
              optionFilterProp="label"
              options={permissionOptions(
                String(formInstances.find((i) => i.id === form.instance_id)?.db_type || ''),
              )}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer open={viewVisible} title="账号详情" width={520} onClose={() => setViewVisible(false)}>
        {viewing ? (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="账号名称">
              <div className="pwd-row">
                <b>{viewing.username}</b>
                {plainPassword ? <code className="pwd">{plainPassword}</code> : null}
                {!plainPassword ? (
                  <Button type="link" loading={pwdLoading} onClick={revealPwd}>
                    查看密码
                  </Button>
                ) : (
                  <>
                    <Button type="link" onClick={() => setPlainPassword('')}>
                      隐藏
                    </Button>
                    <Button type="link" onClick={copyPwd}>
                      复制
                    </Button>
                  </>
                )}
              </div>
            </Descriptions.Item>
            <Descriptions.Item label="数据库">{viewing.db_name || '—'}</Descriptions.Item>
            <Descriptions.Item label="账号权限">{permText(viewing.permissions)}</Descriptions.Item>
            <Descriptions.Item label="域名">{domainText(viewing)}</Descriptions.Item>
            <Descriptions.Item label="数据库类型">{typeLabel(viewing.db_type)}</Descriptions.Item>
            <Descriptions.Item label="IDC">{viewing.idc || '—'}</Descriptions.Item>
            <Descriptions.Item label="描述">{viewing.description || '—'}</Descriptions.Item>
            <Descriptions.Item label="创建时间">{fmtTime(viewing.created_at)}</Descriptions.Item>
            <Descriptions.Item label="创建人">{viewing.created_by || '—'}</Descriptions.Item>
            <Descriptions.Item label="更新时间">{fmtTime(viewing.updated_at)}</Descriptions.Item>
            <Descriptions.Item label="更新人">{viewing.updated_by || '—'}</Descriptions.Item>
          </Descriptions>
        ) : null}
      </Drawer>
    </div>
  )
}
