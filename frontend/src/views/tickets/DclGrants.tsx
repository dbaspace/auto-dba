import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { AutoComplete, Button, Checkbox, Form, Input, Select, Space, message } from 'antd'
import api from '../../api/request'
import DatabasePicker from '../../components/DatabasePicker'
import { dbLabel } from '../../utils/db'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import './DclGrants.css'

const PERM_SQL: Record<string, string> = { r: 'SELECT', c: 'INSERT', u: 'UPDATE', d: 'DELETE' }

function emptyForm() {
  return {
    env: '',
    db_type: '',
    instance_id: '',
    db_ids: [] as string[],
    username: '',
    permissions: [] as string[],
    assigned_to: '',
    description: '',
  }
}

function instLabel(i: any) {
  const addr = i.host ? `${i.host}:${i.port}` : i.domain_name || ''
  return i.name ? `${i.name} — ${addr}` : addr
}

export default function DclGrants() {
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [dbLoading, setDbLoading] = useState(false)
  const [approverLoading, setApproverLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [defaultApprover, setDefaultApprover] = useState('')
  const [antdForm] = Form.useForm()

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const extraEnvs = useMemo(() => {
    const set = new Set<string>()
    for (const i of instances) {
      if (i.idc && i.idc !== 'test' && i.idc !== 'prod') set.add(i.idc)
    }
    return [...set]
  }, [instances])

  const sourceOptions = useMemo(() => {
    if (!form.env || !form.db_type) return []
    return instances.filter(
      (i) => i.db_type === form.db_type && (!i.idc || i.idc === form.env || i.idc === 'default'),
    )
  }, [instances, form.env, form.db_type])

  const currentInst = useMemo(
    () => instances.find((i) => i.id === form.instance_id),
    [instances, form.instance_id],
  )

  const dbOptions = useMemo(
    () =>
      databases
        .filter((d) => d.instance_id === form.instance_id)
        .map((d) => ({ value: d.id, label: dbLabel(d) })),
    [databases, form.instance_id],
  )

  const accountOptions = useMemo(() => {
    const names: string[] = []
    if (currentInst?.username) names.push(currentInst.username)
    return names
  }, [currentInst])

  const approverOptions = useMemo(() => {
    const set = new Set<string>()
    if (defaultApprover) set.add(defaultApprover)
    for (const a of currentInst?.approvers || []) if (a) set.add(a)
    for (const id of form.db_ids || []) {
      const db = databases.find((d) => d.id === id)
      for (const a of db?.approvers || []) if (a) set.add(a)
    }
    return [...set]
  }, [defaultApprover, currentInst, form.db_ids, databases])

  const sourcePlaceholder = !form.env
    ? '请先选择环境'
    : !form.db_type
      ? '请先选择数据库类型'
      : '选择数据源(别名) 或搜索...'
  const accountPlaceholder = !form.instance_id
    ? '请先选择数据源(别名)'
    : !form.db_type
      ? '请先选择数据库类型'
      : '输入账号筛选，或直接选择/自定义输入'
  const approverPlaceholder = approverLoading
    ? '加载审批人…'
    : approverOptions.length
      ? '请选择审批人'
      : '暂无审批人，请配置 sync_default_approver'

  function onEnvOrTypeChange(patch: Partial<ReturnType<typeof emptyForm>>) {
    patchForm({ ...patch, instance_id: '', db_ids: [], username: '' })
  }

  async function loadDbs(instanceId: string) {
    if (!instanceId) {
      setDatabases([])
      return
    }
    setDbLoading(true)
    try {
      const { data } = await api.get('/databases', { params: { instance_id: instanceId } })
      setDatabases(data.data || [])
    } finally {
      setDbLoading(false)
    }
  }

  async function onSourceChange(instanceId: string) {
    const inst = instances.find((i) => i.id === instanceId)
    patchForm({ instance_id: instanceId || '', db_ids: [], username: inst?.username || '' })
    await loadDbs(instanceId)
    const f = formRef.current
    const opts = new Set<string>()
    if (defaultApprover) opts.add(defaultApprover)
    for (const a of inst?.approvers || []) if (a) opts.add(a)
    const first = [...opts][0]
    if (!f.assigned_to && first) patchForm({ assigned_to: first })
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'dcl',
    label: '权限申请',
    path: '/ops/dcl',
    getValues: () => ({ ...formRef.current }),
    applyValues: (d) => {
      const next = {
        ...emptyForm(),
        ...d,
        db_ids: Array.isArray(d.db_ids) ? d.db_ids : [],
        permissions: Array.isArray(d.permissions) ? d.permissions : [],
      }
      setForm(next)
      antdForm.setFieldsValue(next)
    },
    enabled: true,
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  function reset() {
    const next = emptyForm()
    if (defaultApprover) next.assigned_to = defaultApprover
    setForm(next)
    antdForm.resetFields()
    antdForm.setFieldsValue(next)
  }

  function buildGrantSql() {
    const privs = (form.permissions || []).map((p) => PERM_SQL[p]).filter(Boolean).join(', ')
    const dbs = (form.db_ids || [])
      .map((id) => databases.find((d) => d.id === id)?.db_name)
      .filter(Boolean)
    return `GRANT ${privs} ON ${dbs.join(', ')} TO ${form.username};`
  }

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    const username = String(form.username || '').trim()
    if (!accountOptions.includes(username) && !username.toLowerCase().endsWith('_user')) {
      message.warning('自定义账号须以 _user 结尾')
      return
    }
    const dbName = (form.db_ids || [])
      .map((id) => databases.find((d) => d.id === id)?.db_name)
      .filter(Boolean)
      .join(',')
    setSubmitting(true)
    try {
      await api.post('/dcl-grants', {
        instance_id: form.instance_id,
        db_name: dbName,
        assigned_to: form.assigned_to,
        description: form.description,
        sql_content: buildGrantSql(),
      })
      message.success('工单创建成功')
      clearOnSubmitSuccess()
      navigate('/ops/tickets')
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      const { data } = await api.get('/instances')
      setInstances(data.data || [])
      setApproverLoading(true)
      try {
        const args = (await api.get('/global-args')).data.data || []
        const def = args.find((a: any) => a.key === 'sync_default_approver')?.value || ''
        setDefaultApprover(def)
        setForm((f) => {
          if (!f.assigned_to && def) {
            const next = { ...f, assigned_to: def }
            antdForm.setFieldsValue({ assigned_to: def })
            return next
          }
          return f
        })
      } finally {
        setApproverLoading(false)
      }
      restoreDraft()
      if (formRef.current.instance_id) await loadDbs(formRef.current.instance_id)
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    antdForm.setFieldsValue(form)
  }, [form, antdForm])

  return (
    <div className="dcl-page order-page">
      <Form
        form={antdForm}
        layout="vertical"
        className="order-split dcl-split-form"
        initialValues={form}
        onSubmitCapture={(e) => e.preventDefault()}
      >
        <aside className="order-aside">
          <Form.Item label="环境" name="env" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              allowClear
              placeholder="请选择环境"
              value={form.env || undefined}
              onChange={(v) => onEnvOrTypeChange({ env: v || '' })}
              options={[
                { label: '测试', value: 'test' },
                { label: '生产', value: 'prod' },
                ...extraEnvs.map((e) => ({ label: e, value: e })),
              ]}
            />
          </Form.Item>
          <Form.Item label="数据库类型" name="db_type" rules={[{ required: true, message: '请选择数据库类型' }]}>
            <Select
              allowClear
              placeholder="请选择数据库类型"
              value={form.db_type || undefined}
              onChange={(v) => onEnvOrTypeChange({ db_type: v || '' })}
              options={[
                { label: 'MySQL', value: 'mysql' },
                { label: 'PostgreSQL', value: 'postgres' },
                { label: 'StarRocks', value: 'starrocks' },
              ]}
            />
          </Form.Item>
          <Form.Item label="数据源(别名)" name="instance_id" rules={[{ required: true, message: '请选择数据源' }]}>
            <Select
              showSearch
              allowClear
              disabled={!form.env || !form.db_type}
              placeholder={sourcePlaceholder}
              value={form.instance_id || undefined}
              onChange={(v) => onSourceChange(v || '')}
              optionFilterProp="label"
              options={sourceOptions.map((i) => ({ value: i.id, label: instLabel(i) }))}
            />
          </Form.Item>
          <Form.Item
            label="数据库账号"
            name="username"
            rules={[{ required: true, message: '请选择或输入数据库账号' }]}
            extra={<div className="extra">从列表选择已有账号；自定义输入时账号名须以 _user 结尾</div>}
          >
            <AutoComplete
              allowClear
              disabled={!form.instance_id || !form.db_type}
              placeholder={accountPlaceholder}
              value={form.username || undefined}
              onChange={(v) => patchForm({ username: v || '' })}
              options={accountOptions.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>
          <Form.Item
            label="账号权限"
            name="permissions"
            className="perm-item"
            rules={[{ type: 'array', required: true, min: 1, message: '请至少选择一项账号权限' }]}
          >
            <Checkbox.Group
              className="perm-group"
              value={form.permissions}
              onChange={(v) => patchForm({ permissions: v as string[] })}
              options={[
                { label: '读操作', value: 'r' },
                { label: '写操作', value: 'c' },
                { label: '更新操作', value: 'u' },
                { label: '删除操作', value: 'd' },
              ]}
            />
          </Form.Item>
          <Form.Item label="选择审批人" name="assigned_to" rules={[{ required: true, message: '请选择审批人' }]}>
            <Select
              showSearch
              allowClear
              loading={approverLoading}
              placeholder={approverPlaceholder}
              value={form.assigned_to || undefined}
              onChange={(v) => patchForm({ assigned_to: v || '' })}
              options={approverOptions.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>
          <Form.Item label="执行原因" name="description" rules={[{ required: true, message: '请输入执行原因' }]}>
            <Input.TextArea
              rows={3}
              placeholder="请输入执行原因"
              value={form.description}
              onChange={(e) => patchForm({ description: e.target.value })}
            />
          </Form.Item>
          <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <Form.Item className="aside-actions">
            <Space>
              <Button onClick={reset}>取消</Button>
              <Button onClick={() => saveDraft()}>保存草稿</Button>
              <Button type="primary" loading={submitting} onClick={submit}>
                提交申请
              </Button>
            </Space>
          </Form.Item>
        </aside>

        <main className="order-main">
          <div className="dcl-main-title">选择数据库</div>
          <Form.Item
            label="数据库"
            name="db_ids"
            className="db-item"
            rules={[{ type: 'array', required: true, min: 1, message: '请选择数据库' }]}
          >
            <DatabasePicker
              value={form.db_ids}
              onChange={(v) => patchForm({ db_ids: v })}
              options={dbOptions}
              loading={dbLoading}
              disabled={!form.env || !form.db_type || !form.instance_id}
            />
          </Form.Item>
        </main>
      </Form>
    </div>
  )
}
