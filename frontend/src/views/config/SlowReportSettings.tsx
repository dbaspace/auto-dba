import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Checkbox, Drawer, Form, Input, InputNumber, Select, Space, Switch, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

interface Rule {
  id: string
  name: string
  enabled: boolean
  env_ids: string[]
  tenant_id: string
  instance_ids: string[]
  db_names: string[]
  cron_expr: string
  recipients: string
  top_n: number
  last_sent_at: string
  last_error: string
}

const emptyRule = (): Rule => ({
  id: '',
  name: '',
  enabled: true,
  env_ids: [],
  tenant_id: '',
  instance_ids: [],
  db_names: [],
  cron_expr: '0 9 * * 1',
  recipients: '',
  top_n: 20,
  last_sent_at: '',
  last_error: '',
})

interface OptInst {
  id: string
  name: string
  env_name: string
  databases: string[]
}

interface PickOption {
  value: string
  label: string
}

function FilterMultiSelect({
  value,
  onChange,
  options,
  placeholder,
  notFoundContent,
  loading,
  allowCustom,
}: {
  value: string[]
  onChange: (v: string[]) => void
  options: PickOption[]
  placeholder?: string
  notFoundContent?: string
  loading?: boolean
  allowCustom?: boolean
}) {
  const [kw, setKw] = useState('')
  const inputRef = useRef<any>(null)
  const q = kw.trim().toLowerCase()
  const matched = options.filter((o) => !q || o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q))
  const picked = new Set(value)
  const matchedValues = matched.map((o) => o.value)
  const allChecked = matchedValues.length > 0 && matchedValues.every((v) => picked.has(v))
  const indeterminate = !allChecked && matchedValues.some((v) => picked.has(v))

  function selectMatched() {
    const next = new Set(value)
    matchedValues.forEach((v) => next.add(v))
    onChange(Array.from(next))
  }

  function clearMatched() {
    const drop = new Set(matchedValues)
    onChange(value.filter((v) => !drop.has(v)))
  }

  function invertMatched() {
    const drop = new Set(matchedValues)
    const next = value.filter((v) => !drop.has(v))
    matchedValues.forEach((v) => {
      if (!picked.has(v)) next.push(v)
    })
    onChange(next)
  }

  return (
    <Select
      mode={allowCustom ? 'tags' : 'multiple'}
      showSearch={false}
      value={value}
      loading={loading}
      placeholder={placeholder}
      notFoundContent={notFoundContent}
      options={matched}
      menuItemSelectedIcon={null}
      optionRender={(opt) => {
        const v = String(opt.value ?? '')
        return (
          <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, width: '100%' }}>
            <Checkbox checked={picked.has(v)} style={{ pointerEvents: 'none', flexShrink: 0 }} />
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{opt.label}</span>
          </span>
        )
      }}
      onChange={onChange}
      onDropdownVisibleChange={(open) => {
        if (!open) setKw('')
      }}
      dropdownRender={(menu) => (
        <div>
          <div
            style={{ padding: 8 }}
            onMouseDown={(e) => {
              e.preventDefault()
              e.stopPropagation()
              queueMicrotask(() => inputRef.current?.focus({ preventScroll: true }))
            }}
          >
            <Input
              ref={inputRef}
              allowClear
              size="small"
              value={kw}
              placeholder={allowCustom ? '输入关键字过滤，回车可添加未列出的库名' : '输入关键字过滤'}
              onChange={(e) => setKw(e.target.value)}
              onMouseDown={(e) => e.stopPropagation()}
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => {
                e.stopPropagation()
                if (!allowCustom || e.key !== 'Enter') return
                const text = kw.trim()
                if (!text) return
                if (!value.includes(text)) onChange([...value, text])
                setKw('')
              }}
            />
          </div>
          <div
            style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '0 8px 8px' }}
            onMouseDown={(e) => e.preventDefault()}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <Checkbox
              checked={allChecked}
              indeterminate={indeterminate}
              disabled={matchedValues.length === 0}
              onChange={(e) => (e.target.checked ? selectMatched() : clearMatched())}
            >
              全选{q ? '当前匹配' : ''} ({matchedValues.length})
            </Checkbox>
            <Button type="link" size="small" disabled={matchedValues.length === 0} onClick={invertMatched}>
              反选
            </Button>
          </div>
          {menu}
        </div>
      )}
    />
  )
}

export default function SlowReportSettings() {
  const [rules, setRules] = useState<Rule[]>([])
  const [envs, setEnvs] = useState<any[]>([])
  const [tenants, setTenants] = useState<any[]>([])
  const [optionInst, setOptionInst] = useState<OptInst[]>([])
  const [optLoading, setOptLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<Rule>(emptyRule())
  const [saving, setSaving] = useState(false)

  async function load() {
    const { data } = await api.get('/sql-insight/slow-reports')
    setRules(data.data || [])
  }

  useEffect(() => {
    load().catch((e) => message.error(e?.response?.data?.message || '加载周报规则失败'))
    api.get('/environments').then(({ data }) => setEnvs(data.data || [])).catch(() => {})
    api.get('/tenants').then(({ data }) => setTenants(data.data || [])).catch(() => {})
  }, [])

  const envKey = form.env_ids.join(',')
  useEffect(() => {
    if (!open || !envKey) {
      setOptionInst([])
      return
    }
    let cancel = false
    setOptLoading(true)
    api.get('/sql-insight/slow-reports/options', {
      params: { env_ids: envKey, tenant_id: form.tenant_id || '' },
    }).then(({ data }) => {
      if (!cancel) setOptionInst(data.data || [])
    }).catch((e) => {
      if (!cancel) {
        setOptionInst([])
        message.error(e?.response?.data?.message || '加载实例失败')
      }
    }).finally(() => {
      if (!cancel) setOptLoading(false)
    })
    return () => { cancel = true }
  }, [open, envKey, form.tenant_id])

  const dbOptions = useMemo(() => {
    const src = form.instance_ids.length
      ? optionInst.filter((i) => form.instance_ids.includes(i.id))
      : optionInst
    const names = new Set<string>()
    src.forEach((i) => (i.databases || []).forEach((n) => names.add(n)))
    return Array.from(names).sort()
  }, [optionInst, form.instance_ids])

  function patch(p: Partial<Rule>) {
    setForm((cur) => ({ ...cur, ...p }))
  }

  async function save() {
    setSaving(true)
    try {
      const body = { ...form, tenant_id: form.tenant_id || '' }
      if (form.id) await api.put('/sql-insight/slow-reports/' + form.id, body)
      else {
        const { data } = await api.post('/sql-insight/slow-reports', body)
        setForm(data.data || body)
      }
      message.success('已保存')
      setOpen(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function previewNow() {
    try {
      const { data } = await api.post('/sql-insight/slow-reports/preview', form)
      const page = window.open('', '_blank')
      if (!page) {
        message.error('浏览器拦截了新标签页')
        return
      }
      page.document.open()
      page.document.write(data.html || '')
      page.document.close()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '预览失败')
    }
  }

  async function sendNow(id: string) {
    try {
      const { data } = await api.post('/sql-insight/slow-reports/' + id + '/send')
      if (data.message) message.warning('周报已保存。' + data.message)
      else message.success('已发送')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '发送失败')
    }
  }

  const columns: ColumnsType<Rule> = [
    { title: '名称', dataIndex: 'name', render: (v) => v || '慢 SQL 周报' },
    { title: '启用', dataIndex: 'enabled', width: 80, render: (v) => (v ? <Tag color="green">开</Tag> : <Tag>关</Tag>) },
    { title: '收件人', dataIndex: 'recipients', ellipsis: true },
    { title: '上次发送', dataIndex: 'last_sent_at', width: 180 },
    { title: '错误', dataIndex: 'last_error', ellipsis: true },
    {
      title: '操作',
      width: 220,
      render: (_, row) => (
        <Space>
          <Button type="link" onClick={() => { setForm({ ...emptyRule(), ...row }); setOpen(true) }}>编辑</Button>
          <Button type="link" onClick={() => sendNow(row.id)}>立即发送</Button>
          <Button type="link" danger onClick={() => {
            api.delete('/sql-insight/slow-reports/' + row.id).then(load).catch((e) => message.error(e?.response?.data?.message || '删除失败'))
          }}>删除</Button>
        </Space>
      ),
    },
  ]

  return (
    <div>
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => { setForm(emptyRule()); setOpen(true) }}>新建规则</Button>
        <Link to="/ops/slow-sql-report">查看已生成周报</Link>
        <Link to="/config/scheduled-jobs">发送时间在定时任务中设置</Link>
      </Space>
      <ResponsiveTable rowKey="id" columns={columns} dataSource={rules} pagination={false} />
      <Drawer title={form.id ? '编辑周报规则' : '新建周报规则'} open={open} width={560} onClose={() => setOpen(false)}
        extra={<Space>
          <Button onClick={previewNow}>预览</Button>
          <Button type="primary" loading={saving} onClick={save}>保存</Button>
        </Space>}>
        <Form layout="vertical">
          <Form.Item label="名称">
            <Input value={form.name} onChange={(e) => patch({ name: e.target.value })} placeholder="慢 SQL 周报" />
          </Form.Item>
          <Form.Item label="启用">
            <Switch checked={form.enabled} onChange={(v) => patch({ enabled: v })} />
          </Form.Item>
          <Form.Item label="环境" required>
            <Select mode="multiple" value={form.env_ids} onChange={(v) => patch({ env_ids: v, instance_ids: [] })}
              options={envs.map((e) => ({ value: e.id, label: e.name || e.code }))} />
          </Form.Item>
          <Form.Item label="业务线（租户）">
            <Select allowClear value={form.tenant_id || undefined} onChange={(v) => patch({ tenant_id: v || '', instance_ids: [] })}
              options={tenants.map((t) => ({ value: t.id, label: t.name || t.code }))} />
          </Form.Item>
          <Form.Item label="实例" extra={form.env_ids.length ? `当前可选 ${optionInst.length} 个实例。不选则包含这些实例。全选和反选只作用于过滤后的结果。` : '请先选择环境'}>
            <FilterMultiSelect
              loading={optLoading}
              placeholder={form.env_ids.length ? '选择实例' : '请先选择环境'}
              notFoundContent={form.env_ids.length ? '没有匹配的实例' : '请先选择环境'}
              value={form.instance_ids}
              onChange={(v) => patch({ instance_ids: v })}
              options={optionInst.map((i) => ({ value: i.id, label: i.env_name ? `${i.name} · ${i.env_name}` : i.name }))}
            />
          </Form.Item>
          <Form.Item label="库名" extra="随上面的实例列出已登记库和慢日志里出现过的库。多个环境共用同一云实例时必须填写。全选和反选只作用于过滤后的结果。">
            <FilterMultiSelect
              allowCustom
              loading={optLoading}
              placeholder={form.env_ids.length ? '选择或输入库名' : '请先选择环境'}
              notFoundContent={form.env_ids.length ? '没有匹配的库，可在过滤框回车添加' : '请先选择环境'}
              value={form.db_names}
              onChange={(v) => patch({ db_names: v })}
              options={dbOptions.map((n) => ({ value: n, label: n }))}
            />
          </Form.Item>
          <Form.Item label="收件人" extra="多个邮箱用逗号或分号分隔">
            <Input.TextArea rows={3} value={form.recipients} onChange={(e) => patch({ recipients: e.target.value })} />
          </Form.Item>
          <Form.Item label="排名条数">
            <InputNumber min={1} max={100} value={form.top_n} onChange={(v) => patch({ top_n: Number(v) || 20 })} />
          </Form.Item>
        </Form>
      </Drawer>
    </div>
  )
}
