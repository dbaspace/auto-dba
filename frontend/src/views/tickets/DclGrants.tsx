import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Alert, AutoComplete, Button, Checkbox, Form, Input, Select, Space, message } from 'antd'
import api from '../../api/request'
import DatabasePicker from '../../components/DatabasePicker'
import { dbLabel } from '../../utils/db'
import {
  filterAccessDomainsByGroup,
  loadTicketEnvGroups,
  resolveTicketEnvValue,
  type TicketEnvGroup,
} from '../../utils/ticketEnv'
import {
  filterDbsByDomain,
  parseSourceKey,
  sourceKey,
  sourcePrimaryLabel,
  sourceSecondaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
  type DomainSource,
} from '../../utils/instanceSource'
import { useTicketSubmitFreeze } from '../../hooks/useTicketSubmitFreeze'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import { useAppDispatch, useAppSelector } from '../../store'
import { goTicketListAndClose } from './goTicketList'
import './DclGrants.css'

const PERM_SQL: Record<string, string> = { r: 'SELECT', c: 'INSERT', u: 'UPDATE', d: 'DELETE' }

function pgQuoteIdent(name: string) {
  return `"${String(name || '').replace(/"/g, '""')}"`
}

function grantPrivs(perms: string[]) {
  return (perms || []).map((p) => PERM_SQL[p]).filter(Boolean).join(', ')
}

/** 与后端模板对齐的只读预览（FOR ROLE 提交时替换为真实库 owner） */
function previewGrantSql(dbType: string, dbNames: string[], username: string, permissions: string[]) {
  const user = String(username || '').trim()
  const perms = permissions || []
  const privs = grantPrivs(perms)
  if (!user || !dbNames.length || !privs) return ''
  if (dbType === 'postgres') {
    const u = pgQuoteIdent(user)
    const owner = pgQuoteIdent('$DB_OWNER$')
    const parts: string[] = [
      '-- FOR ROLE 提交时按库查询 pg_database.datdba 替换；$DB_OWNER$ 仅为预览占位',
      '-- 账号不存在时执行前自动 CREATE USER … ENCRYPTED PASSWORD',
    ]
    for (const db of dbNames) {
      const d = String(db || '').trim()
      if (!d) continue
      parts.push(
        [
          `-- \\c ${d}`,
          `GRANT CONNECT ON DATABASE ${pgQuoteIdent(d)} TO ${u};`,
          `GRANT ${privs} ON ALL TABLES IN SCHEMA public TO ${u};`,
          `GRANT USAGE ON SCHEMA public TO ${u};`,
          `ALTER DEFAULT PRIVILEGES FOR ROLE ${owner} IN SCHEMA public GRANT ${privs} ON TABLES TO ${u};`,
          `GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO ${u};`,
          `ALTER DEFAULT PRIVILEGES FOR ROLE ${owner} IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO ${u};`,
        ].join('\n'),
      )
    }
    return parts.join('\n\n')
  }
  return dbNames
    .map((db) => {
      const d = String(db || '').trim().replace(/`/g, '``')
      const u = user.replace(/'/g, "''")
      return `GRANT ${privs} ON \`${d}\`.* TO '${u}'@'%';`
    })
    .filter(Boolean)
    .join('\n')
}

function emptyForm() {
  return {
    env: '',
    db_type: '',
    instance_id: '',
    domain: '',
    db_ids: [] as string[],
    username: '',
    permissions: [] as string[],
    assigned_to: '',
    description: '',
  }
}

export default function DclGrants() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const ticketFreeze = useTicketSubmitFreeze()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [instances, setInstances] = useState<any[]>([])
  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [envGroups, setEnvGroups] = useState<TicketEnvGroup[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [dbLoading, setDbLoading] = useState(false)
  const [approverLoading, setApproverLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [defaultApprover, setDefaultApprover] = useState('')
  const [antdForm] = Form.useForm()

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const sourceOptions = useMemo((): DomainSource[] => {
    if (!form.env || !form.db_type) return []
    return sourcesFromAccessDomains(
      filterAccessDomainsByGroup(accessDomains, {
        groupCode: form.env,
        groups: envGroups,
        dbType: form.db_type,
      }),
    )
  }, [accessDomains, form.env, form.db_type, envGroups])

  const currentInst = useMemo(
    () => instances.find((i) => i.id === form.instance_id),
    [instances, form.instance_id],
  )

  const sourceSelectValue =
    form.instance_id && form.domain ? sourceKey(form.instance_id, form.domain) : undefined

  const dbOptions = useMemo(() => {
    let list = databases.filter((d) => d.instance_id === form.instance_id)
    if (form.domain) list = filterDbsByDomain(list, form.domain)
    return list.map((d) => ({ value: d.id, label: dbLabel(d) }))
  }, [databases, form.instance_id, form.domain])

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
      : '选择数据源(域名) 或搜索...'
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
    patchForm({ ...patch, instance_id: '', domain: '', db_ids: [], username: '' })
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

  async function onSourceChange(key: string) {
    const parsed = parseSourceKey(key)
    const instanceId = parsed?.instance_id || ''
    const domain = parsed?.domain || ''
    const inst = instances.find((i) => i.id === instanceId)
    patchForm({
      instance_id: instanceId,
      domain,
      db_ids: [],
      username: inst?.username || '',
    })
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
      formRef.current = next
      setForm(next)
      antdForm.setFieldsValue({
        ...next,
        instance_id: next.instance_id && next.domain ? sourceKey(next.instance_id, next.domain) : undefined,
      })
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

  const selectedDbNames = useMemo(
    () =>
      (form.db_ids || [])
        .map((id) => databases.find((d) => d.id === id)?.db_name)
        .filter(Boolean) as string[],
    [form.db_ids, databases],
  )

  const previewSql = useMemo(
    () => previewGrantSql(form.db_type, selectedDbNames, form.username, form.permissions),
    [form.db_type, form.username, form.permissions, selectedDbNames],
  )

  async function submit() {
    if (ticketFreeze.enabled) {
      message.warning(ticketFreeze.message)
      return
    }
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
    if (!form.domain) {
      message.warning('请选择带域名的数据源')
      return
    }
    if (!form.permissions?.length) {
      message.warning('请选择账号权限')
      return
    }
    const dbName = selectedDbNames.join(',')
    setSubmitting(true)
    try {
      await api.post('/dcl-grants', {
        instance_id: form.instance_id,
        domain: form.domain,
        db_name: dbName,
        username,
        permissions: form.permissions,
        assigned_to: form.assigned_to,
        description: form.description,
      })
      message.success('工单创建成功')
      clearOnSubmitSuccess()
      goTicketListAndClose(dispatch, navigate, location, menus)
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      const [instRes, domRes] = await Promise.all([
        api.get('/instances'),
        api.get('/access-domains').catch(() => ({ data: { data: [] } })),
      ])
      const domains: AccessDomainRow[] = domRes.data.data || []
      const instList = instRes.data.data || []
      setInstances(instList)
      setAccessDomains(domains)
      const groups = await loadTicketEnvGroups()
      setEnvGroups(groups)
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
      const mapped = resolveTicketEnvValue(formRef.current.env, groups)
      if (mapped !== formRef.current.env) {
        formRef.current = { ...formRef.current, env: mapped }
        patchForm({ env: mapped })
      }
      const cur = formRef.current
      const validKeys = new Set(
        sourcesFromAccessDomains(
          filterAccessDomainsByGroup(domains, { groupCode: cur.env, groups, dbType: cur.db_type }),
        ).map((s) => s.key),
      )
      const curKey = cur.instance_id && cur.domain ? sourceKey(cur.instance_id, cur.domain) : ''
      if (curKey && !validKeys.has(curKey)) {
        formRef.current = { ...cur, instance_id: '', domain: '', db_ids: [] }
        patchForm({ instance_id: '', domain: '', db_ids: [] })
      } else if (formRef.current.instance_id) {
        await loadDbs(formRef.current.instance_id)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    antdForm.setFieldsValue({
      ...form,
      instance_id: sourceSelectValue,
    })
  }, [form, antdForm, sourceSelectValue])

  return (
    <div className="dcl-page order-page">
      {ticketFreeze.enabled ? (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={ticketFreeze.message} />
      ) : null}
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
              showSearch
              optionFilterProp="label"
              placeholder="请选择环境"
              value={form.env || undefined}
              onChange={(v) => onEnvOrTypeChange({ env: v || '' })}
              options={envGroups.map((e) => ({ label: e.label, value: e.code }))}
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
              value={sourceSelectValue}
              onChange={(v) => onSourceChange(v || '')}
              optionFilterProp="label"
              options={sourceOptions.map((s) => ({
                value: s.key,
                label: sourcePrimaryLabel(s),
                secondary: sourceSecondaryLabel(s),
              }))}
              optionRender={(opt) => (
                <div className="ds-opt">
                  <span>{(opt.data as any).label}</span>
                  {(opt.data as any).secondary ? (
                    <span className="muted">{(opt.data as any).secondary}</span>
                  ) : null}
                </div>
              )}
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
              <Button type="primary" loading={submitting} disabled={ticketFreeze.enabled} onClick={submit}>
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
          {previewSql ? (
            <div className="dcl-sql-preview">
              <div className="dcl-main-title">授权 SQL 预览</div>
              <pre className="dcl-sql-pre">{previewSql}</pre>
              {form.db_type === 'postgres' ? (
                <p className="hint">
                  审批通过后：账号不存在则 CREATE USER … ENCRYPTED PASSWORD
                  并通知密码；按库查询 owner 后执行 CONNECT / USAGE / 表与序列授权 /
                  ALTER DEFAULT PRIVILEGES（无 schema CREATE）。
                </p>
              ) : (
                <p className="hint">审批通过后将按库执行 GRANT；账号须事先存在。</p>
              )}
            </div>
          ) : null}
        </main>
      </Form>
    </div>
  )
}
