import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Alert, Button, Form, Input, Select, Space, Switch, message } from 'antd'
import api from '../../api/request'
import { useAppDispatch, useAppSelector } from '../../store'
import { goTicketListAndClose } from './goTicketList'
import {
  envCodeInGroup,
  filterAccessDomainsByGroup,
  loadTicketEnvGroups,
  resolveTicketEnvValue,
  type TicketEnvGroup,
} from '../../utils/ticketEnv'
import {
  filterDbsByDomain,
  parseSourceKey,
  pickRegisteredExportDomain,
  registeredDbDomains,
  sourceKey,
  sourcePrimaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
  type DomainSource,
} from '../../utils/instanceSource'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useTicketSubmitFreeze } from '../../hooks/useTicketSubmitFreeze'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import './DqlExport.css'

const SENSITIVE =
  /phone|mobile|email|mail|id_card|idcard|身份证|手机|邮箱|password|passwd|secret|token|card_no|bank|身份证号/i

/** 导出 SQL 无 LIMIT 时自动追加的默认行数（前端常量，非全局参数） */
const DQL_AUTO_LIMIT = 500000

const DEFAULT_DB_TYPES = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
]

/** DQL 库下拉：数据源已选定域名，不再重复「 @ 域名」。标签仍跟在库名后。 */
function dqlDbOptionLabel(d: any): string {
  const name = String(d?.db_name || d?.name || '').trim()
  const tags = (Array.isArray(d?.tags) ? d.tags : [])
    .map((t: any) => String(t || '').trim())
    .filter(Boolean)
  if (!name) return ''
  if (!tags.length) return name
  return `${name} [${tags.join(',')}]`
}

function emptyForm() {
  return {
    db_type: '',
    env: '',
    domain: '',
    database_id: '',
    instance_id: '',
    db_name: '',
    sql_content: '',
    assigned_to: '',
    need_sensitive_data: false,
    dba_assigned_to: '',
    security_assigned_to: '',
    description: '',
  }
}

function parseTables(sql: string) {
  const out: string[] = []
  const re = /\b(?:from|join)\s+([`"']?[\w.]+[`"']?)/gi
  let m: RegExpExecArray | null
  while ((m = re.exec(String(sql || '')))) {
    const name = String(m[1] || '').replace(/[`"']/g, '')
    if (name && !out.includes(name)) out.push(name)
  }
  return out
}

function stripSQLComments(sql: string) {
  return String(sql || '')
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/--[^\n]*/g, ' ')
}

/** 解析 SQL 中最后一个 LIMIT 行数；LIMIT a,b 取 b；无 LIMIT 返回 null */
function parseLimitFromSQL(sql: string): number | null {
  const s = stripSQLComments(sql)
  const re = /\blimit\s+(\d+)(?:\s*,\s*(\d+))?/gi
  let last: number | null = null
  let m: RegExpExecArray | null
  while ((m = re.exec(s))) {
    const n = m[2] != null ? Number(m[2]) : Number(m[1])
    if (Number.isFinite(n) && n >= 0) last = n
  }
  return last
}

/** 无 LIMIT 时在末尾自动补充；已有 LIMIT 则原样返回 */
export function ensureSqlHasLimit(sql: string, autoN = DQL_AUTO_LIMIT): string {
  if (parseLimitFromSQL(sql) != null) return sql
  const raw = String(sql || '')
  const s = raw.trimEnd()
  if (!s.trim()) return raw
  const semi = /;\s*$/.test(s)
  const body = semi ? s.replace(/;\s*$/, '').trimEnd() : s
  return `${body}\nLIMIT ${autoN}${semi ? ';' : ''}`
}

export default function DqlExport() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const ticketFreeze = useTicketSubmitFreeze()

  const leaveToList = () => goTicketListAndClose(dispatch, navigate, location, menus)
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()
  const [saving, setSaving] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [envGroups, setEnvGroups] = useState<TicketEnvGroup[]>([])
  const [envCatalogReady, setEnvCatalogReady] = useState(false)
  const envGroupsRef = useRef<TicketEnvGroup[]>([])
  const [dbTypes, setDbTypes] = useState(DEFAULT_DB_TYPES)
  const [approvers, setApprovers] = useState<string[]>([])
  const [extraApprovers, setExtraApprovers] = useState<string[]>([])
  /** 草稿带来的行数提示；仅当 SQL 无 LIMIT 时参与估算 */
  const [draftRowHint, setDraftRowHint] = useState(0)
  const [resultCols, setResultCols] = useState<string[]>([])
  const resultColsRef = useRef(resultCols)
  resultColsRef.current = resultCols

  const rowCount = useMemo(() => {
    const lim = parseLimitFromSQL(form.sql_content)
    if (lim != null) return lim
    // 无 LIMIT：保守按需审批，避免漏审
    return Math.max(draftRowHint, 501)
  }, [form.sql_content, draftRowHint])
  const rowCountRef = useRef(rowCount)
  rowCountRef.current = rowCount

  const needStrictApproval = !!(form.need_sensitive_data || rowCount > 500)

  // 动态监听：停顿后若无 LIMIT 则自动补全（失焦/提交仍会再兜底）
  useEffect(() => {
    const sql = String(form.sql_content || '')
    if (!sql.trim() || parseLimitFromSQL(sql) != null) return
    const t = window.setTimeout(() => {
      const cur = String(antdForm.getFieldValue('sql_content') || formRef.current.sql_content || '')
      if (!cur.trim() || parseLimitFromSQL(cur) != null) return
      const ensured = ensureSqlHasLimit(cur)
      if (ensured !== cur) {
        antdForm.setFieldsValue({ sql_content: ensured })
        setForm((f) => ({ ...f, sql_content: ensured }))
      }
    }, 800)
    return () => window.clearTimeout(t)
  }, [form.sql_content, antdForm])

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => {
      const next = { ...f, ...patch }
      antdForm.setFieldsValue(next)
      return next
    })
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'dql',
    label: '数据导出',
    path: '/ops/dql',
    getValues: () => ({
      ...formRef.current,
      row_count: rowCountRef.current,
      columns: resultColsRef.current,
    }),
    applyValues: (d) => {
      const next = {
        ...emptyForm(),
        database_id: d.database_id || '',
        instance_id: d.instance_id || '',
        domain: d.domain || '',
        db_name: d.db_name || '',
        env: d.env || '',
        db_type: String(d.db_type || '').toLowerCase(),
        sql_content: d.sql_content || d.sql || '',
        assigned_to: d.assigned_to || '',
        need_sensitive_data: !!d.need_sensitive_data,
        dba_assigned_to: d.dba_assigned_to || '',
        security_assigned_to: d.security_assigned_to || '',
        description: d.description || '',
      }
      formRef.current = next
      setForm(next)
      antdForm.setFieldsValue(next)
      setDraftRowHint(Number(d.row_count || 0))
      setResultCols(Array.isArray(d.columns) ? d.columns : [])
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, rowCount, resultCols, schedulePersist])

  // 不需要加强审批时清空 DBA/安全，避免误提交
  useEffect(() => {
    if (needStrictApproval) return
    if (!form.dba_assigned_to && !form.security_assigned_to) return
    patchForm({ dba_assigned_to: '', security_assigned_to: '' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [needStrictApproval])

  const dataSourceOptions = useMemo((): DomainSource[] => {
    return sourcesFromAccessDomains(
      filterAccessDomainsByGroup(accessDomains, {
        groupCode: form.env,
        groups: envGroups,
        dbType: form.db_type,
      }),
    )
  }, [accessDomains, form.env, form.db_type, envGroups])

  const selectedSourceKey = form.instance_id && form.domain ? sourceKey(form.instance_id, form.domain) : undefined

  const sourceSelectOptions = useMemo(() => {
    const opts = dataSourceOptions.map((i) => ({
      value: i.key,
      label: sourcePrimaryLabel(i),
    }))
    if (selectedSourceKey && form.domain && !opts.some((o) => o.value === selectedSourceKey)) {
      opts.unshift({ value: selectedSourceKey, label: form.domain })
    }
    return opts
  }, [dataSourceOptions, selectedSourceKey, form.domain])

  const dbOptions = useMemo(() => {
    if (!form.instance_id || !form.domain) return []
    const scoped = databases.filter((d) => d.instance_id === form.instance_id)
    const matched = filterDbsByDomain(scoped, form.domain)
    return matched.length ? matched : scoped
  }, [databases, form.instance_id, form.domain])

  const dbSelectOptions = useMemo(() => {
    const opts = dbOptions.map((d) => ({ value: d.id, label: dqlDbOptionLabel(d) }))
    if (form.database_id && form.db_name && !opts.some((o) => o.value === form.database_id)) {
      opts.unshift({ value: form.database_id, label: form.db_name })
    }
    return opts
  }, [dbOptions, form.database_id, form.db_name])

  const parsedTables = useMemo(() => parseTables(form.sql_content), [form.sql_content])

  const matchMask = useMemo(() => {
    const cols = resultCols.filter((c) => SENSITIVE.test(c))
    const sqlHits = [...String(form.sql_content || '').matchAll(/[`"'.\w]+/g)]
      .map((m) => m[0].replace(/[`"']/g, ''))
      .filter((n) => SENSITIVE.test(n))
    const names = [...new Set([...cols, ...sqlHits])]
    if (!names.length) return []
    const table = parsedTables[0] || ''
    return [{ key: names.join(','), table, columns: names }]
  }, [resultCols, form.sql_content, parsedTables])

  function onFilterChange(patch: Partial<ReturnType<typeof emptyForm>>) {
    if ('db_type' in patch || 'env' in patch) {
      patchForm({ ...patch, domain: '', instance_id: '', database_id: '', db_name: '' })
      setApprovers([])
      return
    }
    patchForm(patch)
  }

  function onDataSourceChange(v: string) {
    const parsed = parseSourceKey(v || '')
    if (!parsed) {
      patchForm({ instance_id: '', domain: '', database_id: '', db_name: '' })
      setApprovers([])
      return
    }
    patchForm({
      instance_id: parsed.instance_id,
      domain: parsed.domain,
      database_id: '',
      db_name: '',
    })
    setApprovers([])
  }

  async function loadApprovers(instanceId: string, dbName: string) {
    if (!instanceId) {
      setApprovers([])
      return
    }
    try {
      const { data } = await api.get('/tickets/target-approvers', {
        params: { instance_id: instanceId, db_name: dbName },
      })
      const list = (data.data || []).map((a: any) => a.name || a).filter(Boolean)
      setApprovers(list)
      if (!formRef.current.assigned_to && list[0]) {
        patchForm({ assigned_to: list[0] })
      }
    } catch {
      setApprovers([])
    }
  }

  async function onDbChange(
    databaseId: string,
    opts?: { databases?: any[]; instances?: any[] },
  ) {
    const dbList = opts?.databases || databases
    const instList = opts?.instances || instances
    const localInstMap: Record<string, any> = {}
    for (const i of instList) localInstMap[i.id] = i
    const db = dbList.find((d) => d.id === databaseId) || null
    const instanceId = db?.instance_id || formRef.current.instance_id || ''
    const dbName = db?.db_name || formRef.current.db_name || ''
    const inst = localInstMap[instanceId]
    const patch: Partial<ReturnType<typeof emptyForm>> = {
      database_id: databaseId,
      instance_id: instanceId,
      db_name: dbName,
    }
    if (inst) {
      if (!formRef.current.db_type) patch.db_type = String(inst.db_type || '').toLowerCase()
      if (!formRef.current.env) patch.env = resolveTicketEnvValue(inst.idc || '', envGroupsRef.current)
    }
    patchForm(patch)
    await loadApprovers(instanceId, dbName)
  }

  function applyDraft() {
    restoreDraft()
  }

  async function loadMeta(): Promise<{ instances: any[]; databases: any[]; accessDomains: any[] }> {
    const groupsPromise = loadTicketEnvGroups().then((groups) => {
      envGroupsRef.current = groups
      setEnvGroups(groups)
      const mapped = resolveTicketEnvValue(formRef.current.env, groups)
      if (mapped !== formRef.current.env) {
        formRef.current = { ...formRef.current, env: mapped }
        patchForm({ env: mapped })
      }
      setEnvCatalogReady(true)
      return groups
    })
    const [instRes, dbRes, domainRes, argRes, apprRes] = await Promise.all([
      api.get('/instances'),
      api.get('/databases'),
      api.get('/access-domains').catch(() => ({ data: { data: [] } })),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
      api.get('/create-db/approvers', { params: { scope: 'tenant' } }).catch(() => ({ data: { data: [] } })),
      groupsPromise,
    ])
    const instList = instRes.data.data || []
    const dbList = dbRes.data.data || []
    setInstances(instList)
    setDatabases(dbList)
    setAccessDomains(domainRes.data.data || [])
    const raw = (argRes.data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length) {
        setDbTypes(
          parsed
            .filter(
              (t: any) =>
                t.value && !['kafka', 'redis', 'mongodb', 'elasticsearch', 'cassandra'].includes(t.value),
            )
            .map((t: any) => ({ value: t.value, label: t.label || t.value })),
        )
      }
    } catch {
      /* keep default */
    }
    setExtraApprovers([
      ...new Set<string>((apprRes.data.data || []).map((a: any) => a.name || a).filter(Boolean)),
    ])
    return { instances: instList, databases: dbList, accessDomains: domainRes.data.data || [] }
  }

  async function submit() {
    if (ticketFreeze.enabled) {
      message.warning(ticketFreeze.message)
      return
    }
    const curSQL = String(antdForm.getFieldValue('sql_content') ?? formRef.current.sql_content ?? '')
    const withLimit = ensureSqlHasLimit(curSQL)
    if (withLimit !== curSQL) {
      antdForm.setFieldsValue({ sql_content: withLimit })
      patchForm({ sql_content: withLimit })
    }
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    const f = { ...formRef.current, sql_content: withLimit }
    if (!f.domain || !f.instance_id) {
      message.error('请选择数据源（域名）')
      return
    }
    if (!f.sql_content.trim()) {
      message.error('SQL语句不能为空')
      return
    }
    if (parseLimitFromSQL(withLimit) == null) {
      message.error('导出 SQL 必须包含 LIMIT，禁止提交')
      return
    }
    const hasDBA = !!f.dba_assigned_to
    const hasSec = !!f.security_assigned_to
    const strict = !!(f.need_sensitive_data || (parseLimitFromSQL(withLimit) ?? rowCount) > 500)
    if (strict) {
      if (hasDBA !== hasSec || !hasDBA) {
        message.error('请同步设置 DBA审批人 和 安全审批人')
        return
      }
    }
    setSaving(true)
    try {
      const { data } = await api.post('/tickets', {
        type: 'dql',
        instance_id: f.instance_id,
        domain: f.domain,
        database_id: f.database_id,
        db_name: f.db_name,
        sql_content: withLimit,
        description: f.description,
        assigned_to: f.assigned_to,
        need_sensitive_data: f.need_sensitive_data,
        dba_assigned_to: strict ? f.dba_assigned_to : '',
        security_assigned_to: strict ? f.security_assigned_to : '',
        row_count: parseLimitFromSQL(withLimit) ?? rowCount,
      })
      message.success('工单创建成功 ' + (data.data?.ticket_no || ''))
      clearOnSubmitSuccess()
      leaveToList()
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      applyDraft()
      const meta = await loadMeta()
      if (cancelled) return
      const mapped = resolveTicketEnvValue(formRef.current.env, envGroupsRef.current)
      if (mapped !== formRef.current.env) {
        formRef.current = { ...formRef.current, env: mapped }
        patchForm({ env: mapped })
      }
      const picked = (meta.databases || []).find((d: any) => d.id === formRef.current.database_id)
      const pickedInst = (meta.instances || []).find((i: any) => i.id === (picked?.instance_id || formRef.current.instance_id))
      if (
        formRef.current.env &&
        pickedInst &&
        !envCodeInGroup(pickedInst.idc || '', formRef.current.env, envGroupsRef.current)
      ) {
        formRef.current = { ...formRef.current, database_id: '', instance_id: '', db_name: '' }
        patchForm({ database_id: '', instance_id: '', db_name: '' })
      }
      if (formRef.current.database_id && !String(formRef.current.domain || '').trim()) {
        const db = (meta.databases || []).find((d: any) => d.id === formRef.current.database_id)
        const inst = (meta.instances || []).find((i: any) => i.id === (db?.instance_id || formRef.current.instance_id))
        const options = sourcesFromAccessDomains(filterAccessDomainsByGroup(meta.accessDomains || [], {
          groupCode: formRef.current.env,
          groups: envGroupsRef.current,
          dbType: formRef.current.db_type,
        }))
        const domain = pickRegisteredExportDomain(
          registeredDbDomains(db, inst),
          options.map((s) => s.domain),
        )
        if (domain) {
          const instanceId = String(db?.instance_id || formRef.current.instance_id || '')
          formRef.current = { ...formRef.current, domain, instance_id: instanceId || formRef.current.instance_id }
          patchForm({ domain, instance_id: formRef.current.instance_id })
        }
      }
      // setForm 异步，草稿恢复后用 antdForm 同步取值，避免跳过审批人加载
      const af = antdForm.getFieldsValue(true) || {}
      const f = { ...formRef.current, ...af }
      if (f.database_id) {
        await onDbChange(String(f.database_id), meta)
      } else if (f.instance_id) {
        await loadApprovers(String(f.instance_id), String(f.db_name || ''))
      }
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 库/实例变化时刷新审批人（手动改选或其它恢复路径）
  const approverKeyRef = useRef('')
  useEffect(() => {
    const instanceId = String(form.instance_id || '').trim()
    const dbName = String(form.db_name || '').trim()
    const databaseId = String(form.database_id || '').trim()
    const key = `${databaseId}|${instanceId}|${dbName}`
    if (!instanceId || key === approverKeyRef.current) return
    approverKeyRef.current = key
    void loadApprovers(instanceId, dbName)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.database_id, form.instance_id, form.db_name])

  return (
    <div className="dql-page order-page">
      {ticketFreeze.enabled ? (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={ticketFreeze.message} />
      ) : null}
      <Form
        form={antdForm}
        layout="vertical"
        className="order-split dql-split-form"
        initialValues={form}
        onValuesChange={(_, all) => setForm((f) => ({ ...f, ...all }))}
        onSubmitCapture={(e) => e.preventDefault()}
      >
        <aside className="order-aside">
          <Form.Item name="db_type" label="数据库类型" rules={[{ required: true, message: '请选择数据库类型' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择数据库类型"
              onChange={(v) => onFilterChange({ db_type: v })}
              options={dbTypes.map((t) => ({
                value: t.value,
                label: (
                  <span className="type-opt">
                    <DbTypeIcon type={t.value} size={16} />
                    <span>{t.label}</span>
                  </span>
                ),
              }))}
            />
          </Form.Item>
          <Form.Item name="env" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              showSearch
              placeholder={envCatalogReady ? '请选择环境' : '加载环境…'}
              onChange={(v) => onFilterChange({ env: v })}
              options={envGroups.map((e) => ({ value: e.code, label: e.label }))}
              {...(envCatalogReady ? {} : { labelRender: () => '' })}
            />
          </Form.Item>
          <Form.Item label="数据源" required>
            <Select
              showSearch
              allowClear
              optionFilterProp="label"
              disabled={!form.env || !form.db_type}
              placeholder={!form.env ? '请先选择环境' : !form.db_type ? '请先选择数据库类型' : '请选择数据源（域名）'}
              value={selectedSourceKey}
              onChange={(v) => onDataSourceChange(v || '')}
              options={sourceSelectOptions}
            />
          </Form.Item>
          <Form.Item name="database_id" label="数据库" rules={[{ required: true, message: '请选择数据库' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              disabled={!form.instance_id}
              placeholder={form.instance_id ? '请选择数据库' : '请先选择数据源'}
              onChange={(v) => onDbChange(v)}
              options={dbSelectOptions}
            />
          </Form.Item>
          <Form.Item
            name="assigned_to"
            label="选择导出审批人"
            rules={[{ required: true, message: '请选择审批人' }]}
            extra={<div className="form-extra">如果没有审批人，请联系运维配置。</div>}
          >
            <Select
              showSearch
              placeholder="请选择审批人"
              options={approvers.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>

          <Form.Item
            name="need_sensitive_data"
            label="原文导出敏感字段"
            valuePropName="checked"
            extra={
              matchMask.length ? (
                <div className="form-extra">
                  检测到敏感字段：{matchMask.map((m) => m.columns.join(', ')).join('；')}（默认加密导出）
                </div>
              ) : undefined
            }
          >
            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
          </Form.Item>
          {needStrictApproval ? (
            <>
              <Form.Item
                name="dba_assigned_to"
                label="DBA审批人"
                rules={[{ required: true, message: '请选择DBA审批人' }]}
              >
                <Select
                  allowClear
                  showSearch
                  placeholder="请选择审批人"
                  options={extraApprovers.map((a) => ({ value: a, label: a }))}
                />
              </Form.Item>
              <Form.Item
                name="security_assigned_to"
                label="安全审批人"
                rules={[{ required: true, message: '请选择安全审批人' }]}
              >
                <Select
                  allowClear
                  showSearch
                  placeholder="请选择审批人"
                  options={extraApprovers.map((a) => ({ value: a, label: a }))}
                />
              </Form.Item>
            </>
          ) : null}

          <Form.Item
            name="description"
            label="导出原因"
            rules={[{ required: true, message: '请输入导出原因' }]}
          >
            <Input.TextArea rows={2} placeholder="请输入导出原因" />
          </Form.Item>
          <Form.Item className="aside-actions">
            <Space>
              <Button type="primary" loading={saving} disabled={ticketFreeze.enabled} onClick={submit}>
                提交申请
              </Button>
              <Button onClick={() => saveDraft()}>保存草稿</Button>
              <Button onClick={leaveToList}>取消</Button>
            </Space>
          </Form.Item>
        </aside>

        <main className="order-main">
          <div className="dql-main-title">填写SQL</div>
          <Form.Item
            name="sql_content"
            required
            className="dql-sql-item"
            rules={[
              { required: true, message: 'SQL语句不能为空' },
              {
                validator: async (_, v) => {
                  if (parseLimitFromSQL(String(v || '')) == null) {
                    return Promise.reject(new Error('导出 SQL 必须包含 LIMIT'))
                  }
                },
              },
            ]}
          >
            <Input.TextArea
              placeholder={`SELECT ... LIMIT ${DQL_AUTO_LIMIT}`}
              className="dql-sql-editor"
              rows={12}
              onBlur={() => {
                const cur = String(antdForm.getFieldValue('sql_content') || '')
                const ensured = ensureSqlHasLimit(cur)
                if (ensured !== cur) {
                  antdForm.setFieldsValue({ sql_content: ensured })
                  patchForm({ sql_content: ensured })
                  void antdForm.validateFields(['sql_content']).catch(() => undefined)
                }
              }}
            />
          </Form.Item>
          <p className="dql-sql-hint">
            编辑框内填写只读导出 SQL。必须包含 LIMIT：失焦或提交时若未写将自动补充 LIMIT {DQL_AUTO_LIMIT}（可改数字）；无 LIMIT 禁止提交。预估行数仅作审批参考。
          </p>
        </main>
      </Form>
    </div>
  )
}
