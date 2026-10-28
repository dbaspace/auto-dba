import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Checkbox,
  DatePicker,
  Empty,
  Form,
  Input,
  Radio,
  Select,
  Space,
  Spin,
  Tabs,
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
  sourcePrimaryLabel,
  sourceSecondaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
  type DomainSource,
} from '../../utils/instanceSource'
import {
  filterAccessDomainsByGroup,
  unionApproverNames,
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
import './DdlOrder.css'

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
  data_source_ids: string[]
  db_name: string
  shard_db: boolean
  shard_db_prefix: string
  shard_table: boolean
  shard_table_prefix: string
  assigned_to: string
  description: string
  scheduled_at: string
  is_debug: string
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
    data_source_ids: [],
    db_name: '',
    shard_db: false,
    shard_db_prefix: '',
    shard_table: false,
    shard_table_prefix: '',
    assigned_to: '',
    description: '',
    scheduled_at: '',
    is_debug: 'yes',
    sql_content: '',
  }
}

function longestCommonPrefixLen(a: string, b: string): number {
  const n = Math.min(a.length, b.length)
  let i = 0
  for (; i < n; i++) {
    if (a[i] !== b[i]) break
  }
  return i
}

function getShardDbPrefixHint(database: string, prefix: string): string {
  const db = String(database || '').trim()
  const pre = String(prefix || '').trim()
  if (!db || !pre) return ''
  const dbL = db.toLowerCase()
  const preL = pre.toLowerCase()
  if (dbL.startsWith(preL)) return ''
  const lcp = longestCommonPrefixLen(dbL, preL)
  const maxLen = Math.max(dbL.length, preL.length)
  const ratio = maxLen ? lcp / maxLen : 0
  if (ratio >= 0.72) return '逻辑库名未以分库前缀开头，但两者前缀较相似，请确认分库前缀是否正确'
  return '逻辑库名与分库前缀不一致，建议检查是否选错源库或前缀填写错误'
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

export default function DdlOrder() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const [form, setForm] = useState<FormState>(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()

  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [envGroups, setEnvGroups] = useState<TicketEnvGroup[]>([])
  const envGroupsRef = useRef<TicketEnvGroup[]>([])
  const [approverHint, setApproverHint] = useState('')
  const [dbTypes, setDbTypes] = useState<DbType[]>(DEFAULT_DB_TYPES)
  const [dbRecords, setDbRecords] = useState<any[]>([])
  const [approvers, setApprovers] = useState<string[]>([])
  const [checkRows, setCheckRows] = useState<CheckRow[]>([])
  const [statementCount, setStatementCount] = useState(0)
  const [lastCheckFp, setLastCheckFp] = useState<string | null>(null)
  const [mainTab, setMainTab] = useState('sql')

  const [dsLoading, setDsLoading] = useState(false)
  const [dbLoading, setDbLoading] = useState(false)
  const [reviewerLoading, setReviewerLoading] = useState(false)
  const [checkLoading, setCheckLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [structureLoading, setStructureLoading] = useState(false)
  const [structureRows, setStructureRows] = useState<{ name: string }[]>([])
  const [structureHint, setStructureHint] = useState('')

  function patchForm(patch: Partial<FormState>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const isStarRocks = form.instance_type === 'starrocks'

  const dataSourceOptions = useMemo((): DomainSource[] => {
    return sourcesFromAccessDomains(
      filterAccessDomainsByGroup(accessDomains, {
        groupCode: form.env,
        groups: envGroups,
        dbType: form.instance_type,
      }),
    )
  }, [accessDomains, form.env, form.instance_type, envGroups])

  const selectedDomainSources = useMemo(
    () =>
      form.data_source_ids
        .map((k) => parseSourceKey(k))
        .filter((p): p is { instance_id: string; domain: string } => Boolean(p)),
    [form.data_source_ids],
  )

  const dbNameOptions = useMemo(() => {
    const domainsByInst = new Map<string, string[]>()
    for (const p of selectedDomainSources) {
      const list = domainsByInst.get(p.instance_id) || []
      list.push(p.domain)
      domainsByInst.set(p.instance_id, list)
    }
    const seen = new Set<string>()
    const out: { value: string; label: string }[] = []
    for (const d of dbRecords) {
      const name = d.db_name || ''
      if (!name || seen.has(name)) continue
      const domains = domainsByInst.get(d.instance_id) || []
      if (domains.length && !domains.some((dom) => filterDbsByDomain([d], dom).length)) continue
      seen.add(name)
      out.push({ value: name, label: dbLabel(d) })
    }
    return out
  }, [dbRecords, selectedDomainSources])

  const shardDbPrefixHint = getShardDbPrefixHint(form.db_name, form.shard_db_prefix)

  const checkFp = useMemo(
    () =>
      JSON.stringify({
        env: form.env,
        instance_type: form.instance_type,
        data_source_ids: [...form.data_source_ids].sort(),
        db_name: form.db_name,
        shard_db: form.shard_db,
        shard_db_prefix: String(form.shard_db_prefix || '').trim(),
        shard_table: form.shard_table,
        shard_table_prefix: String(form.shard_table_prefix || '').trim(),
        sql: String(form.sql_content || '').trim(),
      }),
    [form],
  )

  const checkHasError = checkRows.some((r) => String(r.error_level || '').toUpperCase() === 'ERROR')
  const fingerprintMatches = lastCheckFp != null && lastCheckFp === checkFp
  const submitBlockedByApprover = Boolean(approverHint) && approvers.length === 0
  const submitAllowed = fingerprintMatches && !checkHasError && !submitBlockedByApprover

  const submitDisabledReason = useMemo(() => {
    if (submitBlockedByApprover) return '所选数据源没有可用审核人，无法提交'
    if (lastCheckFp == null) return '请先点击「SQL检测」'
    if (!fingerprintMatches) return 'SQL 或表单与上次检测不一致，请重新检测后再提交'
    if (checkHasError) return 'SQL 检测存在错误，请修复后再提交'
    return ''
  }, [submitBlockedByApprover, lastCheckFp, fingerprintMatches, checkHasError])

  const dsPlaceholder = !form.env
    ? '请先选择环境'
    : !form.instance_type
      ? '请先选择实例类型'
      : '选择数据源(别名) 或搜索...'

  const dbPlaceholder = !form.data_source_ids.length
    ? '请先选择数据源(别名)'
    : dbLoading
      ? '加载逻辑库…'
      : '请选择'

  const reviewerPlaceholder = !form.db_name || !form.data_source_ids.length
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
      data_source_ids: [],
      db_name: '',
      assigned_to: '',
      shard_db: false,
      shard_table: false,
      shard_db_prefix: '',
      shard_table_prefix: '',
    })
    setDbRecords([])
    setApprovers([])
    setStructureRows([])
    setStructureHint('')
    resetCheck()
  }

  async function onDataSourcesChange(ids: string[]) {
    patchForm({ data_source_ids: ids, db_name: '', assigned_to: '' })
    setApprovers([])
    resetCheck()
    await loadDatabases(ids)
  }

  async function onDatabaseChange(v: string) {
    patchForm({ db_name: v, assigned_to: '' })
    resetCheck()
    await loadApprovers(formRef.current.data_source_ids, v)
  }

  async function loadDatabases(keys: string[]) {
    if (!keys.length) {
      setDbRecords([])
      return
    }
    setDbLoading(true)
    try {
      const parsed = keys.map((k) => parseSourceKey(k)).filter(Boolean) as { instance_id: string; domain: string }[]
      const instIds = [...new Set(parsed.map((p) => p.instance_id))]
      const lists = await Promise.all(
        instIds.map(async (id) => {
          const { data } = await api.get(`/tickets/instances/${id}/databases`)
          return (data.data || []).map((d: any) => ({ ...d, instance_id: d.instance_id || id }))
        }),
      )
      const records = lists.flat()
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

  async function loadApprovers(keys: string[], dbNameRaw: string) {
    const dbName = String(dbNameRaw || '').trim()
    const instIds = [
      ...new Set(
        keys
          .map((k) => parseSourceKey(k)?.instance_id)
          .filter((id): id is string => Boolean(id)),
      ),
    ]
    if (!instIds.length || !dbName) {
      setApprovers([])
      setApproverHint('')
      return
    }
    setReviewerLoading(true)
    try {
      const lists = await Promise.all(
        instIds.map(async (id) => {
          const { data } = await api.get('/tickets/target-approvers', {
            params: { instance_id: id, db_name: dbName },
          })
          return (data.data || []).map((a: any) => a.name || a).filter(Boolean) as string[]
        }),
      )
      const out = unionApproverNames(lists)
      setApprovers(out)
      if (instIds.length > 1) {
        const lines = instIds.map((id, i) => {
          const dom = keys.map((k) => parseSourceKey(k)).find((p) => p?.instance_id === id)?.domain || id
          const names = lists[i].length ? lists[i].join('、') : '未配置审核人'
          return `${dom}：${names}`
        })
        const same =
          lists.length > 0 &&
          lists.every((l) => {
            const a = [...l].map((s) => s.trim().toLowerCase()).filter(Boolean).sort().join('\0')
            const b = [...lists[0]].map((s) => s.trim().toLowerCase()).filter(Boolean).sort().join('\0')
            return a === b
          })
        if (out.length === 0) {
          setApproverHint('所选数据源没有可用审核人。\n' + lines.join('\n'))
        } else if (!same) {
          setApproverHint('以下为各数据源审核人（可选并集中任意一人）：\n' + lines.join('\n'))
        } else {
          setApproverHint('')
        }
      } else {
        setApproverHint('')
      }
      const cur = formRef.current
      if (cur.assigned_to && !out.includes(cur.assigned_to)) patchForm({ assigned_to: '' })
    } catch {
      setApprovers([])
      setApproverHint('')
      message.error('加载审核人失败')
    } finally {
      setReviewerLoading(false)
    }
  }

  function pickPrimary() {
    const f = formRef.current
    const dbName = String(f.db_name || '').trim()
    const sources = selectedDomainSources.length
      ? selectedDomainSources
      : f.data_source_ids.map((k) => parseSourceKey(k)).filter(Boolean) as {
          instance_id: string
          domain: string
        }[]
    const primary = sources[0]
    const hit =
      (primary &&
        dbRecords.find(
          (d) =>
            d.db_name === dbName &&
            d.instance_id === primary.instance_id &&
            filterDbsByDomain([d], primary.domain).length > 0,
        )) ||
      (primary && dbRecords.find((d) => d.db_name === dbName && d.instance_id === primary.instance_id)) ||
      dbRecords.find((d) => d.db_name === dbName)
    const instanceId = hit?.instance_id || primary?.instance_id || ''
    return {
      instance_id: instanceId,
      database_id: hit?.id || '',
      db_name: dbName,
      domain: primary?.domain || '',
    }
  }

  async function check() {
    setMainTab('sql')
    const f = formRef.current
    const sql = String(f.sql_content || '').trim()
    if (!sql) {
      message.error('请填写 SQL')
      return
    }
    if (!f.env || !f.instance_type || !f.data_source_ids.length || !f.db_name) {
      message.warning('检测前请先选择环境、实例类型、数据源与数据库')
      return
    }
    if (f.shard_db && !String(f.shard_db_prefix || '').trim()) {
      message.warning('已勾选分库时请填写分库前缀后再检测')
      return
    }
    if (f.shard_table && !String(f.shard_table_prefix || '').trim()) {
      message.warning('已勾选分表时请填写分表前缀后再检测')
      return
    }
    const fp = checkFp
    setCheckLoading(true)
    try {
      const primary = pickPrimary()
      const { data } = await api.post('/tickets/check-sql', {
        sql_content: sql,
        type: 'ddl',
        db_type: f.instance_type,
        instance_id: primary.instance_id || '',
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
      if (data.constraint_check_skipped) message.warning('平台已关闭 DDL 约束检查，仅做提交门禁')
      else if (data.ok) message.success(n > 0 ? `检测通过，将按 ${n} 条语句执行` : '检测通过')
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
    setMainTab('sql')
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

  async function fetchStructure() {
    setMainTab('structure')
    setStructureRows([])
    setStructureHint('')
    const f = formRef.current
    if (!f.data_source_ids.length || !f.db_name) {
      setStructureHint('请先选择数据源与数据库')
      return
    }
    const rec = pickPrimary()
    if (!rec.database_id) {
      setStructureHint('未找到已登记库，无法拉取表结构')
      return
    }
    setStructureLoading(true)
    try {
      const { data } = await api.get(`/databases/${rec.database_id}/tables`)
      const tables = data.data || []
      const rows = tables.map((name: string) => ({ name }))
      setStructureRows(rows)
      if (!rows.length) setStructureHint(data.message || '暂无表结构数据')
    } catch (e: any) {
      setStructureHint(e.response?.data?.message || '拉取表结构失败')
    } finally {
      setStructureLoading(false)
    }
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'ddl',
    label: 'DDL工单',
    path: '/ops/ddl',
    getValues: () => ({ ...formRef.current }),
    applyValues: (d) => {
      const next: FormState = {
        ...emptyForm(),
        ...d,
        data_source_ids: Array.isArray(d.data_source_ids) ? d.data_source_ids : [],
      } as FormState
      formRef.current = next
      setForm(next)
      antdForm.setFieldsValue(next)
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
      message.warning('请完善表单必填项（含分库/分表前缀）后再提交')
      return
    }
    const f = formRef.current
    const sql = String(f.sql_content || '').trim()
    if (!sql) {
      message.error('请填写 SQL')
      return
    }
    if (submitBlockedByApprover) {
      message.warning('所选数据源没有可用审核人，无法提交')
      return
    }
    if (!submitAllowed) {
      message.warning(submitDisabledReason)
      return
    }
    const rec = pickPrimary()
    if (!rec.instance_id || !rec.domain) {
      message.warning('未解析到有效数据源，请重新选择数据源后再提交')
      return
    }
    const dataSources = (
      selectedDomainSources.length
        ? selectedDomainSources
        : (f.data_source_ids.map((k) => parseSourceKey(k)).filter(Boolean) as {
            instance_id: string
            domain: string
          }[])
    ).map((p) => ({ instance_id: p.instance_id, domain: p.domain }))
    const instanceIds = [...new Set(dataSources.map((d) => d.instance_id))]
    const extra = {
      env: f.env,
      instance_type: f.instance_type,
      data_source_ids: instanceIds,
      data_sources: dataSources,
      domain: rec.domain,
      shard_db: f.shard_db,
      shard_db_prefix: String(f.shard_db_prefix || '').trim(),
      shard_table: f.shard_table,
      shard_table_prefix: String(f.shard_table_prefix || '').trim(),
      is_debug: f.is_debug,
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/tickets', {
        type: 'ddl',
        db_type: f.instance_type,
        instance_id: rec.instance_id,
        database_id: rec.database_id,
        db_name: rec.db_name,
        domain: rec.domain,
        sql_content: sql,
        description: f.description,
        assigned_to: f.assigned_to,
        scheduled_at: f.scheduled_at || '',
        need_rollback: JSON.stringify(extra),
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
      restoreDraft()
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
      const nextKeys = (cur.data_source_ids || []).filter((k) => validKeys.has(k))
      if (nextKeys.length !== (cur.data_source_ids || []).length) {
        patchForm({ data_source_ids: nextKeys })
      }
      await loadDatabases(nextKeys)
      await loadApprovers(nextKeys, formRef.current.db_name)
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    antdForm.setFieldsValue(form)
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

  const structureColumns: ColumnsType<{ name: string }> = [{ title: '表名', dataIndex: 'name' }]

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

            <Form.Item
              label="数据源(别名)"
              name="data_source_ids"
              rules={[{ type: 'array', required: true, min: 1, message: '请选择数据源' }]}
            >
              <Select
                mode="multiple"
                showSearch
                allowClear
                className="ds-select"
                disabled={!form.env || !form.instance_type}
                loading={dsLoading}
                placeholder={dsPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.data_source_ids}
                onChange={(v) => onDataSourcesChange(v as string[])}
                options={dataSourceOptions.map((s) => ({
                  value: s.key,
                  label: sourcePrimaryLabel(s),
                  optlabel: sourcePrimaryLabel(s),
                  secondary: sourceSecondaryLabel(s),
                }))}
                optionRender={(opt) => (
                  <div className="ds-opt">
                    <span>{(opt.data as any).optlabel}</span>
                    {(opt.data as any).secondary ? (
                      <span className="muted">{(opt.data as any).secondary}</span>
                    ) : null}
                  </div>
                )}
              />
            </Form.Item>

            <Form.Item label="数据库" name="db_name" rules={[{ required: true, message: '请选择数据库' }]}>
              <Select
                showSearch
                allowClear
                disabled={!form.data_source_ids.length}
                loading={dbLoading}
                placeholder={dbPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.db_name || undefined}
                onChange={(v) => onDatabaseChange(v || '')}
                options={dbNameOptions.map((d) => ({ value: d.value, label: d.label }))}
              />
            </Form.Item>

            {!isStarRocks && (
              <>
                <Form.Item label="是否批量">
                  <div className="batch-row">
                    <Checkbox
                      checked={form.shard_db}
                      onChange={(e) =>
                        patchForm({ shard_db: e.target.checked, ...(e.target.checked ? {} : { shard_db_prefix: '' }) })
                      }
                    >
                      分库
                    </Checkbox>
                    <Checkbox
                      checked={form.shard_table}
                      onChange={(e) =>
                        patchForm({
                          shard_table: e.target.checked,
                          ...(e.target.checked ? {} : { shard_table_prefix: '' }),
                        })
                      }
                    >
                      分表
                    </Checkbox>
                  </div>
                </Form.Item>

                {form.shard_db && shardDbPrefixHint && (
                  <Alert type="warning" showIcon className="hint-alert" message={shardDbPrefixHint} />
                )}

                {form.shard_db && (
                  <Form.Item
                    label="分库前缀"
                    name="shard_db_prefix"
                    rules={[
                      {
                        validator: (_, v) =>
                          formRef.current.shard_db && !String(v || '').trim()
                            ? Promise.reject(new Error('勾选分库时请填写分库前缀'))
                            : Promise.resolve(),
                      },
                    ]}
                  >
                    <Input
                      allowClear
                      placeholder="库名前缀匹配如 db_pay_0"
                      value={form.shard_db_prefix}
                      onChange={(e) => patchForm({ shard_db_prefix: e.target.value })}
                    />
                  </Form.Item>
                )}
                {form.shard_table && (
                  <Form.Item
                    label="分表前缀"
                    name="shard_table_prefix"
                    rules={[
                      {
                        validator: (_, v) =>
                          formRef.current.shard_table && !String(v || '').trim()
                            ? Promise.reject(new Error('勾选分表时请填写分表前缀'))
                            : Promise.resolve(),
                      },
                    ]}
                  >
                    <Input
                      allowClear
                      placeholder="表名前缀，如 tbl_order_"
                      value={form.shard_table_prefix}
                      onChange={(e) => patchForm({ shard_table_prefix: e.target.value })}
                    />
                  </Form.Item>
                )}
              </>
            )}

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
                disabled={!form.db_name || !form.data_source_ids.length}
                loading={reviewerLoading}
                placeholder={reviewerPlaceholder}
                style={{ width: '100%' }}
                optionFilterProp="label"
                value={form.assigned_to || undefined}
                onChange={(v) => patchForm({ assigned_to: v || '' })}
                options={approvers.map((a) => ({ value: a, label: a }))}
              />
              {approverHint ? (
                <div
                  className="form-extra"
                  style={{ whiteSpace: 'pre-wrap', color: submitBlockedByApprover ? '#cf1322' : '#666' }}
                >
                  {approverHint}
                </div>
              ) : null}
            </Form.Item>

            <Form.Item label="描述" name="description" rules={[{ required: true, message: '请输入描述' }]}>
              <Input
                allowClear
                placeholder="如：业务上线"
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

            <Form.Item label="是否调测" name="is_debug" rules={[{ required: true, message: '请选择' }]}>
              <Radio.Group value={form.is_debug} onChange={(e) => patchForm({ is_debug: e.target.value })}>
                <Radio value="yes">是</Radio>
                <Radio value="no">否</Radio>
              </Radio.Group>
            </Form.Item>

            <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
            {statementCount > 0 ? (
              <p className="hint">将按 {statementCount} 条语句执行（按分号拆分）</p>
            ) : null}
            <Form.Item className="aside-actions">
              <Space wrap>
                <Button onClick={fetchStructure}>获取表结构</Button>
                <Tooltip title={submitting || submitAllowed ? '' : submitDisabledReason} placement="top">
                  <span className="submit-wrap">
                    <Button type="primary" loading={submitting} disabled={!submitAllowed} onClick={submit}>
                      提交
                    </Button>
                  </span>
                </Tooltip>
                <Button onClick={() => saveDraft()}>保存草稿</Button>
              </Space>
            </Form.Item>
          </Form>
        </aside>

        <main className="order-main">
          <Tabs
            activeKey={mainTab}
            onChange={setMainTab}
            items={[
              {
                key: 'sql',
                label: '填写SQL',
                children: (
                  <>
                    <SqlEditor
                      className="sql-editor"
                      minRows={16}
                      placeholder="ALTER TABLE t ADD COLUMN c varchar(32);"
                      value={form.sql_content}
                      onChange={(v) => patchForm({ sql_content: v })}
                    />
                    <div className="sql-actions">
                      <Button type="primary" loading={checkLoading} disabled={checkLoading} onClick={check}>
                        SQL检测
                      </Button>
                      <Button onClick={beautify}>SQL美化</Button>
                    </div>
                    <p className="hint">
                      编辑框内可检测、美化 SQL
                      {isStarRocks ? '；StarRocks 使用 MySQL 兼容语法' : ''}
                    </p>
                    {!isStarRocks && (form.shard_db || form.shard_table) && (
                      <Alert
                        type="info"
                        showIcon
                        className="hint-alert"
                        message="分库/分表模式下仅做语法与规范校验，不校验表是否存在于单个库"
                      />
                    )}
                    {statementCount > 0 ? (
                      <Alert
                        type="info"
                        showIcon
                        className="hint-alert"
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
                  </>
                ),
              },
              {
                key: 'structure',
                label: '表结构详情',
                children: (
                  <Spin spinning={structureLoading}>
                    {structureRows.length ? (
                      <ResponsiveTable
                        dataSource={structureRows}
                        columns={structureColumns}
                        size="small"
                        bordered
                        rowKey="name"
                        pagination={false}
                      />
                    ) : (
                      <Empty description={structureHint || '暂无表结构数据'} />
                    )}
                  </Spin>
                ),
              },
              {
                key: 'index',
                label: '索引详情',
                children: <Empty description="暂无索引数据" />,
              },
            ]}
          />
        </main>
      </div>
    </div>
  )
}
