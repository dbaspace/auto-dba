import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  DatePicker,
  Form,
  Input,
  Select,
  Tooltip,
  message,
} from 'antd'
import CheckResultFilterBar from '../../components/CheckResultFilterBar'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import { dbLabel } from '../../utils/db'
import { formatSqlContent } from '../../utils/formatSql'
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
import {
  filterAccessDomainsByGroup,
  loadTicketEnvGroups,
  resolveTicketEnvValue,
  type TicketEnvGroup,
} from '../../utils/ticketEnv'
import DbTypeIcon from '../../components/DbTypeIcon'
import SqlEditor from '../../components/SqlEditor'
import { useCheckResultView } from '../../hooks/useCheckResultView'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import { useAppDispatch, useAppSelector } from '../../store'
import { goTicketListAndClose } from './goTicketList'
import {
  countNonEmptyLines,
  lineVsStatementHint,
  shouldHintLineVsStatement,
} from './sqlStatementCount'
import './DmlOrder.css'

const PREFILL_KEY = 'auto-dba-dml-prefill'

interface DbType {
  value: string
  label: string
}

interface CheckRow {
  stage: string
  error_level: string
  message: string
  sql: string
  affected_rows: string | number
}

interface FormState {
  env: string
  instance_type: string
  instance_id: string
  domain: string
  db_name: string
  assigned_to: string
  description: string
  scheduled_at: string
  sql_content: string
}

const DEFAULT_DB_TYPES: DbType[] = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
]

function emptyForm(): FormState {
  return {
    env: '',
    instance_type: '',
    instance_id: '',
    domain: '',
    db_name: '',
    assigned_to: '',
    description: '',
    scheduled_at: '',
    sql_content: '',
  }
}

function levelLabel(lv: string): string {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return '错误'
  if (x === 'WARN' || x === 'WARNING') return '警告'
  if (x === 'INFO' || x === 'OK') return '通过'
  return lv || '—'
}

function levelClass(lv: string): string {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return 'lv-error'
  if (x === 'WARN' || x === 'WARNING') return 'lv-warn'
  if (x === 'INFO' || x === 'OK') return 'lv-ok'
  return ''
}

function readJson(key: string): Record<string, any> | null {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const d = JSON.parse(raw)
    return d && typeof d === 'object' ? d : null
  } catch {
    sessionStorage.removeItem(key)
    return null
  }
}

export default function DmlOrder() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const [searchParams] = useSearchParams()
  const [form, setForm] = useState<FormState>(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()

  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [envGroups, setEnvGroups] = useState<TicketEnvGroup[]>([])
  const envGroupsRef = useRef<TicketEnvGroup[]>([])
  const [dbTypes, setDbTypes] = useState<DbType[]>(DEFAULT_DB_TYPES)
  const [dbRecords, setDbRecords] = useState<any[]>([])
  const [approvers, setApprovers] = useState<string[]>([])
  const [checkRows, setCheckRows] = useState<CheckRow[]>([])
  const [statementCount, setStatementCount] = useState(0)
  const [lastCheckFp, setLastCheckFp] = useState<string | null>(null)

  const [dsLoading, setDsLoading] = useState(false)
  const [dbLoading, setDbLoading] = useState(false)
  const [reviewerLoading, setReviewerLoading] = useState(false)
  const [checkLoading, setCheckLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  function patchForm(patch: Partial<FormState>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const dataSourceOptions = useMemo((): DomainSource[] => {
    return sourcesFromAccessDomains(
      filterAccessDomainsByGroup(accessDomains, {
        groupCode: form.env,
        groups: envGroups,
        dbType: form.instance_type,
      }),
    )
  }, [accessDomains, form.env, form.instance_type, envGroups])

  const filteredDbRecords = useMemo(
    () => (form.domain ? filterDbsByDomain(dbRecords, form.domain) : []),
    [dbRecords, form.domain],
  )

  const dbNameOptions = useMemo(() => {
    const seen = new Set<string>()
    const out: { value: string; label: string }[] = []
    for (const d of filteredDbRecords) {
      const name = d.db_name || ''
      if (!name || seen.has(name)) continue
      seen.add(name)
      out.push({ value: name, label: dbLabel({ ...d, domains: [form.domain].filter(Boolean) }) })
    }
    return out
  }, [filteredDbRecords, form.domain])

  const selectedSourceKey = form.instance_id && form.domain ? sourceKey(form.instance_id, form.domain) : undefined

  /** 仅环境 / 类型 / 数据源 / 库 / SQL 参与指纹，改审核人/描述/定时不失效 */
  const checkFp = useMemo(
    () =>
      JSON.stringify({
        env: form.env,
        instance_type: form.instance_type,
        instance_id: form.instance_id,
        domain: form.domain,
        db_name: form.db_name,
        sql: String(form.sql_content || '').trim(),
      }),
    [form],
  )

  const checkHasError = checkRows.some((r) => String(r.error_level || '').toUpperCase() === 'ERROR')
  const fingerprintMatches = lastCheckFp != null && lastCheckFp === checkFp
  const submitAllowed = fingerprintMatches && !checkHasError

  const submitDisabledReason = useMemo(() => {
    if (lastCheckFp == null) return '请先点击「SQL检测」'
    if (!fingerprintMatches) return 'SQL 或表单与上次检测不一致，请重新检测后再提交'
    if (checkHasError) return 'SQL 检测存在错误，请修复后再提交'
    return ''
  }, [lastCheckFp, fingerprintMatches, checkHasError])

  const dsPlaceholder = !form.env
    ? '请先选择环境'
    : !form.instance_type
      ? '请先选择实例类型'
      : '请选择数据源或搜索...'

  const dbPlaceholder = !form.instance_id
    ? '请先选择数据源'
    : dbLoading
      ? '加载逻辑库…'
      : '请选择'

  const reviewerPlaceholder = !form.db_name || !form.instance_id
    ? '请先选择数据源与数据库'
    : reviewerLoading
      ? '加载审核人…'
      : '请选择（可选）'

  function resetCheck() {
    setLastCheckFp(null)
    setCheckRows([])
    setStatementCount(0)
  }

  function onEnvOrTypeChange(patch: Partial<FormState>) {
    patchForm({
      ...patch,
      instance_id: '',
      domain: '',
      db_name: '',
      assigned_to: '',
    })
    setDbRecords([])
    setApprovers([])
    resetCheck()
  }

  async function onDataSourceChange(v: string) {
    const parsed = parseSourceKey(v || '')
    if (!parsed) {
      patchForm({ instance_id: '', domain: '', db_name: '', assigned_to: '' })
      setDbRecords([])
      setApprovers([])
      resetCheck()
      return
    }
    patchForm({
      instance_id: parsed.instance_id,
      domain: parsed.domain,
      db_name: '',
      assigned_to: '',
    })
    setApprovers([])
    resetCheck()
    await loadDatabases(parsed.instance_id)
  }

  async function onDatabaseChange(v: string) {
    patchForm({ db_name: v, assigned_to: '' })
    resetCheck()
    await loadApprovers(formRef.current.instance_id, v)
  }

  async function loadDatabases(id: string) {
    if (!id) {
      setDbRecords([])
      return
    }
    setDbLoading(true)
    try {
      const { data } = await api.get(`/tickets/instances/${id}/databases`)
      const records = (data.data || []).map((d: any) => ({ ...d, instance_id: d.instance_id || id }))
      setDbRecords(records)
      const names = new Set<string>()
      for (const d of records) {
        if (d.db_name) names.add(d.db_name)
      }
      const cur = formRef.current
      if (cur.db_name && !names.has(cur.db_name)) {
        patchForm({ db_name: '', assigned_to: '' })
        setApprovers([])
      }
    } catch (e: any) {
      setDbRecords([])
      message.error(e.response?.data?.message || '加载逻辑库失败')
    } finally {
      setDbLoading(false)
    }
  }

  async function loadApprovers(id: string, dbNameRaw: string) {
    const dbName = String(dbNameRaw || '').trim()
    if (!id || !dbName) {
      setApprovers([])
      return
    }
    setReviewerLoading(true)
    try {
      const { data } = await api.get('/tickets/target-approvers', {
        params: { instance_id: id, db_name: dbName },
      })
      const out = (data.data || []).map((a: any) => a.name || a).filter(Boolean)
      const uniq = [...new Set(out)] as string[]
      setApprovers(uniq)
      const cur = formRef.current
      if (cur.assigned_to && !uniq.includes(cur.assigned_to)) patchForm({ assigned_to: '' })
    } catch {
      setApprovers([])
      message.error('加载审核人失败')
    } finally {
      setReviewerLoading(false)
    }
  }

  function pickPrimary() {
    const f = formRef.current
    const dbName = String(f.db_name || '').trim()
    const scoped = f.domain ? filterDbsByDomain(dbRecords, f.domain) : []
    const hit = scoped.find((d) => d.db_name === dbName) || dbRecords.find((d) => d.db_name === dbName)
    return {
      instance_id: f.instance_id || hit?.instance_id || '',
      domain: f.domain || '',
      database_id: hit?.id || '',
      db_name: dbName,
    }
  }

  async function check() {
    const f = formRef.current
    const sql = String(f.sql_content || '').trim()
    if (!sql) {
      message.error('请填写 SQL')
      return
    }
    if (!f.env || !f.instance_type || !f.instance_id || !f.domain || !f.db_name) {
      message.warning('检测前请先选择环境、实例类型、数据源（域名）与数据库')
      return
    }
    const fp = checkFp
    setCheckLoading(true)
    try {
      const { data } = await api.post('/tickets/check-sql', {
        sql_content: sql,
        type: 'dml',
        db_type: f.instance_type,
        instance_id: f.instance_id,
        db_name: f.db_name,
      })
      const parsed = Number(data.statement_count)
      const n = Number.isFinite(parsed) && parsed > 0 ? parsed : (data.statements || []).length
      setStatementCount(n)
      setCheckRows(
        (data.statements || []).map((r: any) => ({
          stage: r.stage || 'parse',
          error_level: r.error_level || '',
          message: r.message || '',
          sql: r.sql || sql,
          affected_rows: r.affected_rows ?? '—',
        })),
      )
      if (data.ok) message.success(n > 0 ? `检测通过，将按 ${n} 条语句执行` : '检测通过')
      else message.warning('检测未通过')
    } catch (e: any) {
      const msg = e.response?.data?.message || e.message || 'SQL 检测请求失败'
      message.error(msg)
      setStatementCount(0)
      setCheckRows([{ stage: '—', error_level: 'ERROR', message: msg, sql, affected_rows: '—' }])
    } finally {
      setCheckLoading(false)
      setLastCheckFp(fp)
    }
  }

  function beautify() {
    const sql = String(formRef.current.sql_content || '').trim()
    const result = formatSqlContent(sql)
    if (!result.ok) {
      message.warning(result.error || 'SQL 美化失败')
      return
    }
    if (result.unchanged) {
      message.info('SQL 已是规范格式')
      return
    }
    patchForm({ sql_content: result.text })
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'dml',
    label: 'DML工单',
    path: '/ops/dml',
    getValues: () => ({ ...formRef.current }),
    applyValues: (d) => {
      const next: FormState = {
        ...emptyForm(),
        ...d,
        instance_id: d.instance_id || '',
        domain: d.domain || '',
        sql_content: d.sql_content || d.sql || '',
      } as FormState
      formRef.current = next
      setForm(next)
      antdForm.setFieldsValue({
        ...next,
        instance_id: next.instance_id && next.domain ? sourceKey(next.instance_id, next.domain) : undefined,
      })
      if (next.instance_id) void loadDatabases(next.instance_id)
    },
    enabled: true,
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      message.warning('请完善表单必填项后再提交')
      return
    }
    const f = formRef.current
    const sql = String(f.sql_content || '').trim()
    if (!sql) {
      message.error('请填写 SQL')
      return
    }
    if (!submitAllowed) {
      message.warning(submitDisabledReason)
      return
    }
    const rec = pickPrimary()
    if (!rec.instance_id) {
      message.warning('未解析到有效数据源，请重新选择数据源后再提交')
      return
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/tickets', {
        type: 'dml',
        db_type: f.instance_type,
        instance_id: rec.instance_id,
        domain: rec.domain,
        database_id: rec.database_id,
        db_name: rec.db_name,
        sql_content: sql,
        description: f.description,
        assigned_to: f.assigned_to,
        scheduled_at: f.scheduled_at || '',
        need_rollback: JSON.stringify({
          env: f.env,
          instance_type: f.instance_type,
          domain: rec.domain,
        }),
      })
      message.success(data.data?.ticket_no ? `提交成功，工单号 ${data.data.ticket_no}` : '提交成功')
      clearOnSubmitSuccess()
      goTicketListAndClose(dispatch, navigate, location, menus)
    } catch (e: any) {
      message.error(e.response?.data?.message || '提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  function applyIncoming(): boolean {
    const q = searchParams
    const fromQuery: Record<string, string> = {
      env: String(q.get('env') || '').trim(),
      instance_type: String(q.get('instance_type') || q.get('db_type') || '').trim(),
      instance_id: String(q.get('instance_id') || '').trim(),
      db_name: String(q.get('db_name') || q.get('database') || '').trim(),
      description: String(q.get('description') || '').trim(),
      sql_content: String(q.get('sql') || q.get('sql_content') || '').trim(),
    }
    const hasQuery = Object.values(fromQuery).some(Boolean)
    const prefill = readJson(PREFILL_KEY)
    const src = hasQuery ? fromQuery : prefill
    if (!src) return false
    const next: FormState = {
      ...emptyForm(),
      ...src,
      instance_id: src.instance_id || '',
      domain: src.domain || '',
      sql_content: src.sql_content || src.sql || '',
    } as FormState
    formRef.current = next
    setForm(next)
    antdForm.setFieldsValue({
      ...next,
      instance_id: next.instance_id && next.domain ? sourceKey(next.instance_id, next.domain) : undefined,
    })
    if (prefill) sessionStorage.removeItem(PREFILL_KEY)
    return true
  }

  async function loadMeta() {
    setDsLoading(true)
    try {
      const [domRes, argRes] = await Promise.all([
        api.get('/access-domains'),
        api.get('/global-args').catch(() => ({ data: { data: [] } })),
      ])
      const domains: AccessDomainRow[] = domRes.data.data || []
      setAccessDomains(domains)
      const groups = await loadTicketEnvGroups()
      envGroupsRef.current = groups
      setEnvGroups(groups)
      const mapped = resolveTicketEnvValue(formRef.current.env, groups)
      if (mapped !== formRef.current.env) {
        formRef.current = { ...formRef.current, env: mapped }
        patchForm({ env: mapped })
      }
      const args = argRes.data.data || argRes.data || []
      const raw =
        (Array.isArray(args) ? args : []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
      let nextTypes = DEFAULT_DB_TYPES
      try {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed) && parsed.length) {
          nextTypes = parsed
            .filter(
              (t: any) =>
                t.value && !['kafka', 'redis', 'mongodb', 'elasticsearch', 'cassandra'].includes(t.value),
            )
            .map((t: any) => ({ value: t.value, label: t.label || t.value }))
        }
      } catch {
        /* keep default */
      }
      setDbTypes(nextTypes)
      const cur = formRef.current
      if (cur.instance_type && !nextTypes.some((t) => t.value === cur.instance_type)) {
        patchForm({ instance_type: '' })
      }
      return domains
    } finally {
      setDsLoading(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      const incoming = applyIncoming()
      if (!incoming) restoreDraft()
      const domains = (await loadMeta()) || []
      const cur = formRef.current
      const validKeys = new Set(
        sourcesFromAccessDomains(
          filterAccessDomainsByGroup(domains, {
            groupCode: cur.env,
            groups: envGroupsRef.current,
            dbType: cur.instance_type,
          }),
        ).map((s) => s.key),
      )
      const curKey = cur.instance_id && cur.domain ? sourceKey(cur.instance_id, cur.domain) : ''
      if (curKey && !validKeys.has(curKey)) {
        patchForm({ instance_id: '', domain: '', db_name: '', assigned_to: '' })
      }
      await loadDatabases(formRef.current.instance_id)
      await loadApprovers(formRef.current.instance_id, formRef.current.db_name)
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    antdForm.setFieldsValue({
      ...form,
      instance_id: form.instance_id && form.domain ? sourceKey(form.instance_id, form.domain) : undefined,
    })
  }, [form, antdForm])

  const {
    filteredRows: checkViewRows,
    levelFilter,
    setLevelFilter,
    counts: checkCounts,
    pagination: checkPagination,
    rowNo: checkRowNo,
  } = useCheckResultView(checkRows)

  const sqlLineApprox = useMemo(() => countNonEmptyLines(form.sql_content), [form.sql_content])
  const showStmtHint =
    statementCount > 0 && shouldHintLineVsStatement(sqlLineApprox, statementCount)

  const checkColumns: ColumnsType<CheckRow> = [
    { title: '序号', width: 64, render: (_: unknown, __: unknown, index: number) => checkRowNo(index) },
    { title: '阶段', dataIndex: 'stage', width: 100 },
    {
      title: '错误等级',
      width: 100,
      render: (_, row) => <span className={levelClass(row.error_level)}>{levelLabel(row.error_level)}</span>,
    },
    { title: '错误信息', dataIndex: 'message', ellipsis: true },
    { title: 'SQL语句', dataIndex: 'sql', ellipsis: true },
    { title: '影响行数', dataIndex: 'affected_rows', width: 90 },
    { title: '操作', width: 70, render: () => '—' },
  ]

  return (
    <div className="order-page">
      <div className="order-split">
        <aside className="order-aside">
          <Form form={antdForm} layout="vertical" initialValues={form} onSubmitCapture={(e) => e.preventDefault()}>
            <Form.Item label="环境" name="env" rules={[{ required: true, message: '请选择环境' }]}>
              <Select
                showSearch
                allowClear
                placeholder="请选择"
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.env || undefined}
                onChange={(v) => onEnvOrTypeChange({ env: v || '' })}
                options={envGroups.map((e) => ({ value: e.code, label: e.label }))}
              />
            </Form.Item>

            <Form.Item label="实例类型" name="instance_type" rules={[{ required: true, message: '请选择实例类型' }]}>
              <Select
                showSearch
                allowClear
                disabled={!form.env}
                placeholder={form.env ? '请选择' : '请先选择环境'}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.instance_type || undefined}
                onChange={(v) => onEnvOrTypeChange({ instance_type: v || '' })}
                options={dbTypes.map((t) => ({
                  value: t.value,
                  label: (
                    <span className="type-opt">
                      <DbTypeIcon type={t.value} size={16} />
                      <span>{t.label}</span>
                    </span>
                  ),
                  title: t.label,
                }))}
              />
            </Form.Item>

            <Form.Item label="数据源" name="instance_id" rules={[{ required: true, message: '请选择数据源（域名）' }]}>
              <Select
                showSearch
                allowClear
                disabled={!form.env || !form.instance_type}
                loading={dsLoading}
                placeholder={dsPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={selectedSourceKey}
                onChange={(v) => onDataSourceChange(v || '')}
                options={dataSourceOptions.map((i) => ({
                  value: i.key,
                  label: sourcePrimaryLabel(i),
                  optlabel: sourcePrimaryLabel(i),
                  secondary: sourceSecondaryLabel(i),
                }))}
                optionRender={(opt) => (
                  <div className="ds-opt">
                    <span>{(opt.data as any).optlabel}</span>
                    <span className="muted">{(opt.data as any).secondary}</span>
                  </div>
                )}
              />
            </Form.Item>

            <Form.Item label="数据库" name="db_name" rules={[{ required: true, message: '请选择数据库' }]}>
              <Select
                showSearch
                allowClear
                disabled={!form.instance_id}
                loading={dbLoading}
                placeholder={dbPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.db_name || undefined}
                onChange={(v) => onDatabaseChange(v || '')}
                options={dbNameOptions.map((d) => ({ value: d.value, label: d.label }))}
              />
            </Form.Item>

            <Form.Item
              label={
                <span>
                  审核人 <span className="optional">(可选)</span>
                </span>
              }
            >
              <Select
                showSearch
                allowClear
                disabled={!form.db_name || !form.instance_id}
                loading={reviewerLoading}
                placeholder={reviewerPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.assigned_to || undefined}
                onChange={(v) => patchForm({ assigned_to: v || '' })}
                options={approvers.map((a) => ({ value: a, label: a }))}
              />
            </Form.Item>

            <Form.Item label="描述" name="description" rules={[{ required: true, message: '请输入描述' }]}>
              <Input
                allowClear
                placeholder="如：业务数据修复"
                value={form.description}
                onChange={(e) => patchForm({ description: e.target.value })}
              />
            </Form.Item>

            <Form.Item label="定时执行">
              <DatePicker
                showTime={{ format: 'HH:mm' }}
                format="YYYY-MM-DD HH:mm"
                placeholder="年/月/日 --:--"
                style={{ width: '100%' }}
                value={form.scheduled_at ? dayjs(form.scheduled_at) : null}
                onChange={(d: Dayjs | null) =>
                  patchForm({ scheduled_at: d ? d.format('YYYY-MM-DD HH:mm:ss') : '' })
                }
              />
            </Form.Item>

            <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
            {statementCount > 0 ? (
              <p className="hint">将按 {statementCount} 条语句执行（按分号拆分）</p>
            ) : null}
            <Form.Item className="aside-actions">
              <Tooltip title={submitting || submitAllowed ? '' : submitDisabledReason} placement="top">
                <span className="submit-wrap">
                  <Button type="primary" style={{ width: '100%' }} loading={submitting} disabled={!submitAllowed} onClick={submit}>
                    提交
                  </Button>
                </span>
              </Tooltip>
            </Form.Item>
            <Form.Item className="aside-actions draft-item">
              <Button style={{ width: '100%' }} onClick={() => saveDraft()}>
                保存草稿
              </Button>
            </Form.Item>
          </Form>
        </aside>

        <main className="order-main">
          <div className="sql-title">填写 SQL</div>
          <SqlEditor
            className="sql-editor"
            minRows={16}
            placeholder="UPDATE t SET c=1 WHERE id=1;"
            value={form.sql_content}
            onChange={(v) => patchForm({ sql_content: v })}
          />
          <div className="sql-actions">
            <Button type="primary" loading={checkLoading} disabled={checkLoading} onClick={check}>
              SQL检测
            </Button>
            <Button onClick={beautify}>SQL美化</Button>
          </div>
          <p className="hint">编辑框内可检测、美化 SQL</p>
          {statementCount > 0 ? (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 12 }}
              message={`将按 ${statementCount} 条语句执行（按分号拆分，与检测结果行数可能不同）`}
              description={showStmtHint ? lineVsStatementHint(sqlLineApprox, statementCount) : undefined}
            />
          ) : null}
          <CheckResultFilterBar
            value={levelFilter}
            onChange={setLevelFilter}
            counts={checkCounts}
          />
          <ResponsiveTable
            dataSource={checkViewRows}
            columns={checkColumns}
            size="small"
            bordered
            rowKey={(_, i) => String(i)}
            pagination={checkPagination}
            locale={{ emptyText: levelFilter === 'all' ? '暂无检测结果' : '当前状态下无记录' }}
          />
        </main>
      </div>
    </div>
  )
}
