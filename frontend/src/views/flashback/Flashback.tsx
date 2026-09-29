import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  Button,
  Checkbox,
  Collapse,
  Col,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Pagination,
  Row,
  Select,
  Tag,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { dbLabel } from '../../utils/db'
import './flashback.css'

const PREFILL_KEY = 'auto-dba-dml-prefill'
const relChips = [
  { m: 5, label: '5m' },
  { m: 10, label: '10m' },
  { m: 60, label: '1h' },
  { m: 360, label: '6h' },
]

type FlashForm = {
  env: string
  instance_id: string
  db_name: string
  tables: string[]
  start_time: string
  end_time: string
  reviewer: string
  sql_types: string[]
  output_kind: string
  start_file: string
  start_pos: number
  stop_file: string
  stop_pos: number
}

function emptyForm(): FlashForm {
  return {
    env: '',
    instance_id: '',
    db_name: '',
    tables: [],
    start_time: '',
    end_time: '',
    reviewer: '',
    sql_types: [],
    output_kind: 'flashback',
    start_file: '',
    start_pos: 0,
    stop_file: '',
    stop_pos: 0,
  }
}

const DT_FMT = 'YYYY-MM-DD HH:mm:ss'

function pad(n: number) {
  return String(n).padStart(2, '0')
}
function toLocal(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(
    d.getMinutes(),
  )}:${pad(d.getSeconds())}`
}
function idcScope(idc: any) {
  const s = String(idc || '').toLowerCase().trim()
  if (!s || s === 'default' || s === 'unknown') return ''
  if (s.includes('test') || s.includes('dev') || s === 'uat') return 'test'
  return 'prod'
}
function statusText(s: any) {
  return (
    ({
      pending: '排队',
      init: '排队',
      running: '执行中',
      succeeded: '完成',
      executed: '完成',
      failed: '失败',
      cancelled: '已取消',
      canceled: '已取消',
      rejected: '已驳回',
    } as Record<string, string>)[s] ||
    s ||
    '—'
  )
}
function statusTag(s: any): string {
  if (s === 'succeeded' || s === 'executed') return 'success'
  if (s === 'running' || s === 'pending') return 'warning'
  if (s === 'failed' || s === 'rejected') return 'error'
  return 'default'
}
function approvalText(s: any) {
  return (
    ({
      pending: '待审批',
      approved: '已通过',
      rejected: '已驳回',
      canceled: '已撤销',
      cancelled: '已撤销',
    } as Record<string, string>)[s] ||
    (s ? s : '—')
  )
}
function fmtBytes(n: any) {
  const v = Number(n) || 0
  if (v < 1024) return v + ' B'
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + ' KB'
  if (v < 1024 * 1024 * 1024) return (v / (1024 * 1024)).toFixed(1) + ' MB'
  return (v / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}
function fmtTime(s: any) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s).replace('T', ' ').slice(0, 19)
  return toLocal(d)
}

export default function Flashback() {
  const navigate = useNavigate()
  const location = useLocation()
  const params = useParams()

  const page = useMemo(() => {
    if (params.id) return 'task'
    if (location.pathname.endsWith('/tasks')) return 'new'
    if (location.pathname.includes('/history')) return 'history'
    return 'dash'
  }, [params.id, location.pathname])

  const [wizardStep, setWizardStep] = useState(1)
  const [engine, setEngine] = useState<'postgres' | 'mysql'>('postgres')
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [tableOptions, setTableOptions] = useState<string[]>([])
  const [allRows, setAllRows] = useState<any[]>([])
  const [listLoading, setListLoading] = useState(false)
  const [prechecking, setPrechecking] = useState(false)
  const [creating, setCreating] = useState(false)
  const [precheck, setPrecheck] = useState<any>(null)
  const [keyword, setKeyword] = useState('')
  const [histStatus, setHistStatus] = useState('')
  const [histPage, setHistPage] = useState(1)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detail, setDetail] = useState<any>(null)
  const [sqls, setSqls] = useState<any[]>([])
  const [logs, setLogs] = useState<any[]>([])
  const [selectedSQLs, setSelectedSQLs] = useState<string[]>([])
  const [quickTable, setQuickTable] = useState('')
  const [quickAgo, setQuickAgo] = useState(10)
  const [selftesting, setSelftesting] = useState(false)
  const [selftest, setSelftest] = useState<any>(null)
  const [relMin, setRelMin] = useState(10)
  const [form, setForm] = useState<FlashForm>(emptyForm())
  const [defaultReviewer, setDefaultReviewer] = useState('')

  const listTimer = useRef<number>(0)
  const taskTimer = useRef<number>(0)
  const formRef = useRef<FlashForm>(form)
  formRef.current = form
  const engineRef = useRef(engine)
  engineRef.current = engine
  const detailRef = useRef<any>(detail)
  detailRef.current = detail
  const allRowsRef = useRef<any[]>(allRows)
  allRowsRef.current = allRows

  function patchForm(patch: Partial<FlashForm>) {
    setForm((prev) => ({ ...prev, ...patch }))
  }

  function isEngine(i: any) {
    const t = String(i.db_type || '').toLowerCase()
    return engine === 'postgres'
      ? t === 'postgres' || t === 'postgresql'
      : t.includes('mysql') || t === engine
  }

  const engineInstances = useMemo(() => instances.filter(isEngine), [instances, engine])
  const sourceOptions = useMemo(() => {
    return engineInstances.filter((i) => {
      if (!form.env) return true
      const scope = idcScope(i.idc)
      return !scope || scope === form.env
    })
  }, [engineInstances, form.env])
  const currentInst = useMemo(
    () => instances.find((i) => i.id === form.instance_id),
    [instances, form.instance_id],
  )
  const dbOptions = useMemo(
    () => databases.filter((d) => !d.instance_id || d.instance_id === form.instance_id),
    [databases, form.instance_id],
  )
  const reviewerOptions = useMemo(() => {
    const set = new Set<string>()
    for (const a of currentInst?.approvers || []) if (a) set.add(a)
    const db = dbOptions.find((d) => d.db_name === form.db_name)
    for (const a of db?.approvers || []) if (a) set.add(a)
    if (defaultReviewer) set.add(defaultReviewer)
    return [...set]
  }, [currentInst, dbOptions, form.db_name, defaultReviewer])
  const instMap = useMemo(() => {
    const m = new Map<string, any>()
    for (const i of instances) m.set(i.id, i)
    return m
  }, [instances])
  const recentTasks = useMemo(() => allRows.slice(0, 5), [allRows])

  const dash = useMemo(() => {
    const rows = allRows
    const startOfToday = new Date()
    startOfToday.setHours(0, 0, 0, 0)
    const days: { label: string; ok: number; fail: number }[] = []
    let max = 1
    for (let i = 6; i >= 0; i--) {
      const d = new Date(startOfToday)
      d.setDate(d.getDate() - i)
      const next = new Date(d)
      next.setDate(next.getDate() + 1)
      let ok = 0
      let fail = 0
      for (const r of rows) {
        const t = new Date(r.created_at).getTime()
        if (t >= d.getTime() && t < next.getTime()) {
          if (r.status === 'succeeded' || r.status === 'executed') ok++
          else if (r.status === 'failed') fail++
        }
      }
      max = Math.max(max, ok + fail)
      days.push({ label: `${d.getMonth() + 1}/${d.getDate()}`, ok, fail })
    }
    const succeeded = rows.filter((r) => r.status === 'succeeded' || r.status === 'executed').length
    const failed = rows.filter((r) => r.status === 'failed').length
    const pending = rows.filter(
      (r) => r.approval_status === 'pending' || r.status === 'pending' || r.status === 'running',
    ).length
    const today = rows.filter((r) => new Date(r.created_at) >= startOfToday).length
    const walBytes = rows.reduce((s, r) => s + Number(r.wal_bytes || 0), 0)
    return {
      total: rows.length,
      succeeded,
      failed,
      pending,
      today,
      walBytes,
      rate: rows.length ? Math.round((succeeded * 100) / rows.length) : 0,
      days,
      max,
      weekOk: days.reduce((s, d) => s + d.ok, 0),
      weekFail: days.reduce((s, d) => s + d.fail, 0),
    }
  }, [allRows])

  function instLabel(i: any) {
    const addr = i.host ? `${i.host}:${i.port}` : ''
    return i.name ? `${i.name} — ${addr}` : addr
  }
  function instText(row: any) {
    const i = instMap.get(row.instance_id)
    return i ? instLabel(i) : row.host ? `${row.host}:${row.port}` : row.instance_id || '—'
  }
  function tableText(row: any) {
    const t = row.tables || row.table_list || []
    return Array.isArray(t) && t.length ? t.join(', ') : '整库'
  }
  function rowStatusText(row: any) {
    const a = row?.approval_status
    if (a === 'pending') return '待审批'
    if (a === 'rejected') return '已驳回'
    if (a === 'canceled' || a === 'cancelled') return '已撤销'
    return statusText(row?.status)
  }
  function rowStatusTag(row: any): string {
    const a = row?.approval_status
    if (a === 'pending') return 'warning'
    if (a === 'rejected') return 'error'
    if (a === 'canceled' || a === 'cancelled') return 'default'
    return statusTag(row?.status)
  }
  function itemTag(it: any): string {
    if (it.status === 'passed' || it.ok) return 'success'
    if (it.status === 'warning') return 'warning'
    return 'error'
  }
  function pct(done: any, total: any) {
    const t = Number(total || 0)
    if (t <= 0) return detail?.status === 'succeeded' ? 100 : 0
    return Math.min(100, Math.round((Number(done || 0) * 100) / t))
  }
  function barH(n: number) {
    const m = dash.max || 1
    return `${Math.max(n ? 8 : 2, Math.round((n / m) * 100))}%`
  }

  const histFiltered = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    return allRows.filter((r) => {
      if (histStatus === 'awaiting_approval') {
        if (r.approval_status !== 'pending') return false
      } else if (histStatus && r.status !== histStatus && r.approval_status !== histStatus) return false
      if (!q) return true
      return `${instText(r)} ${r.db_name || ''} ${r.id || ''}`.toLowerCase().includes(q)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allRows, keyword, histStatus, instMap])
  const histRows = useMemo(
    () => histFiltered.slice((histPage - 1) * 10, histPage * 10),
    [histFiltered, histPage],
  )

  const addrText = useMemo(() => {
    const i = currentInst
    if (!i) return instances.length ? '请选择已配置的实例地址' : '还没有实例。请先到「配置 → 实例」添加。'
    const vendor = i.cloud_vendor || '自建'
    return `${i.name || i.id} · ${i.db_type} · ${i.host}:${i.port} · 用户 ${i.username || '—'} · vendor=${vendor} ${
      i.cloud_instance_id || ''
    } ${i.cloud_region || ''}`
  }, [currentInst, instances.length])

  function applyRel(m: number) {
    setRelMin(m)
    const end = new Date()
    patchForm({ end_time: toLocal(end), start_time: toLocal(new Date(end.getTime() - m * 60 * 1000)) })
  }
  function disableFuture(d: Dayjs) {
    return d.valueOf() > Date.now()
  }

  function payload() {
    const f = formRef.current
    return {
      instance_id: f.instance_id,
      database: f.db_name,
      db_name: f.db_name,
      tables: f.tables,
      target_time: f.start_time,
      start_time: f.start_time,
      end_time: f.end_time,
      sql_type: (f.sql_types || []).join(','),
      sql_types: f.sql_types,
      output_kind: f.output_kind,
      reviewer: f.reviewer,
      approver: f.reviewer,
      assigned_to: f.reviewer,
      start_file: f.start_file,
      start_pos: Number(f.start_pos || 0),
      stop_file: f.stop_file,
      stop_pos: Number(f.stop_pos || 0),
    }
  }

  function go(p: string) {
    if (p === 'new' && wizardStep > 1 && !precheck) setWizardStep(1)
    if (p !== 'task') stopTaskPoll()
    const map: Record<string, string> = {
      dash: '/ops/flashback/dashboard',
      new: '/ops/flashback/tasks',
      history: '/ops/flashback/history',
    }
    navigate(map[p] || '/ops/flashback/dashboard')
  }

  function onEngineChange(next: 'postgres' | 'mysql') {
    setEngine(next)
    patchForm({ instance_id: '', db_name: '', tables: [], reviewer: '' })
    setTableOptions([])
    setDatabases([])
    setPrecheck(null)
    setWizardStep(1)
  }
  function onEnvChange(v: string) {
    patchForm({ env: v || '', instance_id: '', db_name: '', tables: [], reviewer: '' })
    setTableOptions([])
    setPrecheck(null)
  }
  async function onInstChange(v: string) {
    patchForm({ instance_id: v, db_name: '', tables: [], reviewer: '' })
    setTableOptions([])
    setPrecheck(null)
    await loadDbs(v)
  }
  async function onDbChange(v: string) {
    patchForm({ db_name: v, tables: [] })
    setPrecheck(null)
    await loadTables(v)
  }

  const loadDbs = useCallback(async (instanceId: string) => {
    if (!instanceId) {
      setDatabases([])
      return
    }
    const [reg, live] = await Promise.all([
      api.get('/databases', { params: { instance_id: instanceId } }),
      api.get(`/instances/${instanceId}/databases`).catch(() => ({ data: { data: [] } })),
    ])
    const registered = reg.data.data || []
    const liveNames = (live.data.data || [])
      .map((d: any) =>
        typeof d === 'string' ? { db_name: d } : { db_name: d.name || d.db_name, system: d.system },
      )
      .filter((d: any) => d.db_name && !d.system)
    const seen = new Set(registered.map((d: any) => d.db_name))
    setDatabases([...registered, ...liveNames.filter((d: any) => !seen.has(d.db_name))])
  }, [])

  const loadTables = useCallback(
    async (dbName: string) => {
      setTableOptions([])
      const rec = databases.find((d) => d.id && d.db_name === dbName)
      if (rec?.id) {
        const { data } = await api.get(`/databases/${rec.id}/tables`)
        setTableOptions(data.data || [])
      }
    },
    [databases],
  )

  async function doPrecheck() {
    const f = formRef.current
    if (!f.instance_id) {
      message.warning('请选择实例')
      return
    }
    if (!f.db_name) {
      message.warning('请选择数据库')
      return
    }
    if (!f.start_time) {
      message.warning('请设置起始时间')
      return
    }
    if (!f.reviewer) {
      message.warning('请选择审核人')
      return
    }
    setPrechecking(true)
    try {
      const { data } = await api.post('/flashback/tasks/precheck', payload(), { timeout: 60000 })
      const items = (data.items || []).map((it: any) => ({
        ...it,
        ok: it.status === 'passed' || it.status === 'warning' || it.ok,
      }))
      setPrecheck({ ...data, ok: !!data.ok, items })
      setWizardStep(2)
      const level = data.ok ? 'success' : 'warning'
      message[level](data.message || (data.ok ? '预检查通过' : '预检查未通过'))
    } finally {
      setPrechecking(false)
    }
  }

  function goConfirm() {
    if (!formRef.current.reviewer) {
      message.warning('请先选择审核人')
      return
    }
    setWizardStep(3)
  }

  async function create() {
    if (!formRef.current.reviewer) {
      message.warning('请先选择审核人')
      return
    }
    setCreating(true)
    try {
      const { data } = await api.post('/flashback/tasks', payload(), { timeout: 60000 })
      message.success('已提交审批，通过后自动执行')
      setPrecheck(null)
      setWizardStep(1)
      await load()
      const row = data.data || data
      if (row?.issue_id) navigate('/ops/tickets/' + row.issue_id)
      else if (row?.id) openTask(row)
      else navigate('/ops/tickets?type=flashback')
    } finally {
      setCreating(false)
    }
  }

  function quickStart() {
    const f = formRef.current
    if (!f.instance_id) {
      message.warning('请先选择实例')
      return
    }
    if (!f.db_name) {
      message.warning('请填写数据库名')
      return
    }
    applyRel(Number(quickAgo) || 10)
    patchForm({
      tables: quickTable.trim() ? [quickTable.trim()] : [],
      reviewer: reviewerOptions[0] || f.reviewer,
    })
    setWizardStep(1)
    setPrecheck(null)
    go('new')
  }

  async function runSelftest() {
    const f = formRef.current
    if (engine !== 'postgres') {
      message.warning('DEFAULT 堆页自测仅支持 PostgreSQL 自建库')
      return
    }
    if (!f.instance_id || !f.db_name) {
      message.warning('请先选择实例与数据库')
      return
    }
    setSelftesting(true)
    setSelftest(null)
    try {
      const { data } = await api.post(
        '/flashback/tasks/selftest',
        { instance_id: f.instance_id, database: f.db_name },
        { timeout: 180000 },
      )
      setSelftest(data)
      if (data?.ok) message.success('DEFAULT 堆页补齐自测通过')
      else message.warning('自测未全部通过，请查看明细')
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '自测失败')
    } finally {
      setSelftesting(false)
    }
  }

  const load = useCallback(async () => {
    setListLoading(true)
    try {
      const [tasks, insts, args] = await Promise.all([
        api.get('/flashback/tasks'),
        api.get('/instances'),
        api.get('/global-args').catch(() => ({ data: { data: [] } })),
      ])
      setAllRows(tasks.data.data || [])
      setInstances(insts.data.data || [])
      const list = args.data.data || []
      const dr = String(list.find((a: any) => a.key === 'sync_default_approver')?.value || '').trim()
      setDefaultReviewer(dr)
    } finally {
      setListLoading(false)
    }
  }, [])

  function openTask(row: any) {
    if (!row?.id) return
    navigate(`/ops/flashback/history/${row.id}`)
  }

  const refreshTask = useCallback(async (id: string) => {
    const [s, l, t] = await Promise.all([
      api.get(`/flashback/tasks/${id}/sql`),
      api.get(`/flashback/tasks/${id}/logs`),
      api.get(`/flashback/tasks/${id}`),
    ])
    setSqls(s.data.data || [])
    setLogs(l.data.data || [])
    const d = t.data.data || detailRef.current
    setDetail(d)
    if (d && !(d.status === 'pending' || d.status === 'running')) stopTaskPoll()
  }, [])

  const loadTask = useCallback(
    async (id: string) => {
      if (!id) {
        stopTaskPoll()
        return
      }
      setDetail((prev: any) => ({ ...(prev || {}), id }))
      setDetailLoading(true)
      try {
        await refreshTask(id)
      } finally {
        setDetailLoading(false)
      }
      stopTaskPoll()
      const d = detailRef.current
      if (d && (d.status === 'pending' || d.status === 'running')) {
        taskTimer.current = window.setInterval(() => refreshTask(id).catch(() => {}), 3000)
      }
    },
    [refreshTask],
  )

  function reloadSQL() {
    if (detailRef.current?.id) refreshTask(detailRef.current.id)
  }

  function onSqlSel(_: any, rows: any[]) {
    setSelectedSQLs(rows.map((r) => r.statement).filter(Boolean))
  }

  function selectedText() {
    const stmts = selectedSQLs.length
      ? selectedSQLs
      : sqls.map((r) => r.statement).filter(Boolean)
    return (
      stmts.map((s) => String(s).replace(/;\s*$/, '') + ';').join('\n') + (stmts.length ? '\n' : '')
    )
  }

  async function copySQL() {
    const text = selectedText()
    if (!text) {
      message.warning('没有可复制的 SQL')
      return
    }
    await navigator.clipboard.writeText(text)
    message.success('已复制')
  }

  function downloadSQL() {
    const text = selectedText()
    if (!text) {
      message.warning('没有可下载的 SQL')
      return
    }
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `flashback-${detail?.id || 'sql'}.sql`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  function toDml(row: any) {
    if (!row) return
    const inst = instMap.get(row.instance_id)
    sessionStorage.setItem(
      PREFILL_KEY,
      JSON.stringify({
        env: form.env || idcScope(inst?.idc) || 'test',
        instance_type: inst?.db_type || engine,
        instance_id: row.instance_id,
        db_name: row.db_name || row.database,
        description: `闪回任务 ${row.id}`,
        sql_content: selectedText(),
      }),
    )
    navigate('/ops/dml')
  }

  function stopTaskPoll() {
    if (taskTimer.current) {
      window.clearInterval(taskTimer.current)
      taskTimer.current = 0
    }
  }

  // 详情：路由 id 变化时加载
  useEffect(() => {
    const id = String(params.id || '')
    if (id) loadTask(id)
    else stopTaskPoll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.id])

  // 初始化 + 列表轮询
  useEffect(() => {
    applyRel(10)
    load()
    listTimer.current = window.setInterval(() => {
      if (allRowsRef.current.some((r) => r.status === 'running' || r.status === 'pending')) load()
    }, 2000)
    return () => {
      if (listTimer.current) window.clearInterval(listTimer.current)
      stopTaskPoll()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const dtValue = (s: string): Dayjs | null => (s ? dayjs(s, DT_FMT) : null)

  // ============ 渲染各页 ============

  const recentColumns: ColumnsType<any> = [
    {
      title: '时间',
      width: 180,
      render: (_, row) => (
        <a className="link" onClick={() => openTask(row)}>
          {fmtTime(row.created_at)}
        </a>
      ),
    },
    { title: '表', ellipsis: true, render: (_, row) => tableText(row) },
    { title: '数据库', width: 140, render: (_, row) => row.db_name || row.database },
    {
      title: '状态',
      width: 100,
      render: (_, row) => <Tag color={rowStatusTag(row)}>{rowStatusText(row)}</Tag>,
    },
  ]

  const histColumns: ColumnsType<any> = [
    { title: '时间', width: 180, render: (_, row) => fmtTime(row.created_at) },
    { title: '模式', width: 90, render: () => <span className="badge online">online</span> },
    {
      title: '实例 / 地址',
      render: (_, row) => (
        <div>
          <div>{instText(row)}</div>
          <div className="meta">{row.host ? `${row.host}:${row.port}` : ''}</div>
        </div>
      ),
    },
    {
      title: '库 / 表',
      render: (_, row) => (
        <div>
          <div>{row.db_name || row.database}</div>
          <div className="meta">{tableText(row)}</div>
        </div>
      ),
    },
    { title: '闪回点', width: 180, render: (_, row) => row.start_time || row.target_time || '—' },
    {
      title: '状态',
      width: 110,
      render: (_, row) => <Tag color={rowStatusTag(row)}>{rowStatusText(row)}</Tag>,
    },
    {
      title: '审批',
      width: 100,
      render: (_, row) =>
        row.issue_id ? (
          <Button type="link" onClick={() => navigate('/ops/tickets/' + row.issue_id)}>
            {approvalText(row.approval_status)}
          </Button>
        ) : (
          <span className="meta">{approvalText(row.approval_status)}</span>
        ),
    },
    {
      title: '操作',
      width: 80,
      render: (_, row) => (
        <Button type="link" onClick={() => openTask(row)}>
          查看
        </Button>
      ),
    },
  ]

  const sqlColumns: ColumnsType<any> = [
    { title: '#', dataIndex: 'seq', width: 60 },
    { title: 'op', dataIndex: 'op', width: 80 },
    { title: '表', dataIndex: 'table_name', width: 140 },
    {
      title: 'SQL',
      render: (_, row) => <pre className="mono">{row.statement}</pre>,
    },
  ]

  return (
    <div className="fb-shell">
      {page === 'dash' && (
        <section className="dash">
          <div className="dash-kpis">
            <div className="card dash-kpi">
              <h3>总闪回次数</h3>
              <div className="num">{dash.total}</div>
              <p className="foot">今日 {dash.today} 条</p>
            </div>
            <div className="card dash-kpi">
              <h3>成功</h3>
              <div className="num">{dash.succeeded}</div>
              <p className="foot up">✓ 成功率 {dash.rate}%</p>
            </div>
            <div className="card dash-kpi">
              <h3>恢复数据量</h3>
              <div className="num">{fmtBytes(dash.walBytes)}</div>
              <p className="foot">累计 WAL / binlog</p>
            </div>
            <div className="card dash-kpi">
              <h3>待处理任务</h3>
              <div className="num">{dash.pending}</div>
              <p className={['foot', dash.pending ? 'down' : 'up'].join(' ')}>
                {dash.pending ? '队列积压' : '队列空闲'}
              </p>
            </div>
          </div>
          <div className="dash-mid">
            <div className="card">
              <div className="dash-hd">
                <h3>闪回趋势（近 7 日）</h3>
                <span className="meta">按日统计</span>
              </div>
              {dash.days.some((d) => d.ok || d.fail) ? (
                <div className="dash-bars">
                  {dash.days.map((d) => (
                    <div key={d.label} className="dash-col">
                      <div className="stack">
                        <div className="seg ok" style={{ height: barH(d.ok) }} />
                        {d.fail ? <div className="seg fail" style={{ height: barH(d.fail) }} /> : null}
                      </div>
                      <div className="lab">{d.label}</div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="empty">近 7 日暂无任务</div>
              )}
              <div className="dash-legend">
                <span>
                  <i className="ok" />
                  成功 {dash.weekOk}
                </span>
                <span>
                  <i className="fail" />
                  失败 {dash.weekFail}
                </span>
              </div>
            </div>
            <div className="card dash-quick">
              <div className="dash-hd">
                <h3>快速闪回</h3>
              </div>
              <Form
                layout="vertical"
                onSubmitCapture={(e) => {
                  e.preventDefault()
                  quickStart()
                }}
              >
                <Form.Item label="数据库类型">
                  <div className="chips">
                    <button
                      type="button"
                      className={'chip' + (engine === 'postgres' ? ' on' : '')}
                      onClick={() => onEngineChange('postgres')}
                    >
                      PG
                    </button>
                    <button
                      type="button"
                      className={'chip' + (engine === 'mysql' ? ' on' : '')}
                      onClick={() => onEngineChange('mysql')}
                    >
                      MySQL
                    </button>
                  </div>
                </Form.Item>
                <Form.Item label="实例">
                  <Select
                    showSearch
                    optionFilterProp="label"
                    placeholder="选择实例"
                    style={{ width: '100%' }}
                    value={form.instance_id || undefined}
                    onChange={(v) => onInstChange(v)}
                    options={engineInstances.map((i) => ({ label: instLabel(i), value: i.id }))}
                  />
                </Form.Item>
                <Form.Item label="数据库">
                  <Select
                    showSearch
                    optionFilterProp="label"
                    placeholder="业务库名"
                    style={{ width: '100%' }}
                    value={form.db_name || undefined}
                    onChange={(v) => onDbChange(v)}
                    options={dbOptions.map((d) => ({ label: dbLabel(d), value: d.db_name }))}
                  />
                </Form.Item>
                <Form.Item label="表">
                  <Input
                    placeholder="schema.table，可空=整库"
                    value={quickTable}
                    onChange={(e) => setQuickTable(e.target.value)}
                  />
                </Form.Item>
                <Form.Item label="回退到">
                  <Select
                    style={{ width: '100%' }}
                    value={quickAgo}
                    onChange={(v) => setQuickAgo(v)}
                    options={[
                      { value: 5, label: '5 分钟前' },
                      { value: 10, label: '10 分钟前' },
                      { value: 30, label: '30 分钟前' },
                      { value: 60, label: '1 小时前' },
                      { value: 360, label: '6 小时前' },
                    ]}
                  />
                </Form.Item>
                <Auth urn={Perm.flashback.edit}>
                  <Button className="go-btn" type="primary" htmlType="submit" style={{ background: '#1f8a4c', borderColor: '#1f8a4c' }}>
                    执行闪回
                  </Button>
                </Auth>
                {engine === 'postgres' && (
                  <Button
                    style={{ marginTop: 8, width: '100%' }}
                    loading={selftesting}
                    onClick={(e) => {
                      e.preventDefault()
                      runSelftest()
                    }}
                  >
                    DEFAULT 堆页自测
                  </Button>
                )}
              </Form>
              {selftest && (
                <div className="info tip" style={{ marginTop: 12 }}>
                  <strong>{selftest.ok ? '自测通过' : '自测未通过'}</strong>
                  {selftest.table ? <p>临时表 {selftest.table}</p> : null}
                  <ul style={{ margin: '8px 0 0', paddingLeft: 18 }}>
                    {(selftest.checks || []).map((c: any) => (
                      <li key={c.name} style={{ color: c.ok ? undefined : '#cf1322' }}>
                        {c.ok ? '✓' : '✗'} {c.name}：{c.detail}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              <p className="meta">
                将带入时间窗并进入预检查向导。实例地址来自「配置 → 实例」；云库还须关联云厂商/云实例
                ID，并在「多云密钥」配置 AK/SK。DEFAULT 自测会在目标库临时建表 DELETE（不改 FULL），验证堆页补齐后自动清理。
              </p>
            </div>
          </div>
          <div className="card">
            <div className="dash-hd">
              <h3>最近闪回任务</h3>
              <a
                className="link"
                onClick={(e) => {
                  e.preventDefault()
                  go('history')
                }}
              >
                查看全部
              </a>
            </div>
            {recentTasks.length ? (
              <ResponsiveTable
                rowKey={(r) => r.id}
                size="small"
                bordered
                pagination={false}
                columns={recentColumns}
                dataSource={recentTasks}
              />
            ) : (
              <div className="empty">还没有闪回任务。</div>
            )}
          </div>
        </section>
      )}

      {page === 'new' && (
        <section className="card">
          <p className="lead">
            连接地址从「配置 →
            实例」获取，这里只选实例并设定时间窗。云托管库请在实例上填写云厂商 / 云实例 ID /
            Region，并到「数据闪回 → 多云密钥」配置对应 AK/SK，才能从云平台下载 WAL。
          </p>
          <div className="steps">
            <div className={'step' + (wizardStep >= 1 ? ' on' : '')}>
              <b>1</b> 配置连接
            </div>
            <div className={'step' + (wizardStep >= 2 ? ' on' : '')}>
              <b>2</b> 预检查
            </div>
            <div className={'step' + (wizardStep >= 3 ? ' on' : '')}>
              <b>3</b> 提交审批
            </div>
          </div>

          {wizardStep === 1 && (
            <Form layout="vertical">
              <div className="field">
                <label>模式</label>
                <div className="chips">
                  <button
                    type="button"
                    className={'chip' + (engine === 'postgres' ? ' on' : '')}
                    onClick={() => onEngineChange('postgres')}
                  >
                    在线 · PostgreSQL
                  </button>
                  <button
                    type="button"
                    className={'chip' + (engine === 'mysql' ? ' on' : '')}
                    onClick={() => onEngineChange('mysql')}
                  >
                    在线 · MySQL
                  </button>
                </div>
              </div>
              <div className="split">
                <div>
                  <h3>连接配置</h3>
                  <Form.Item label="环境">
                    <Select
                      allowClear
                      placeholder="可选，用于筛选实例"
                      style={{ width: '100%' }}
                      value={form.env || undefined}
                      onChange={(v) => onEnvChange(v)}
                      options={[
                        { label: '测试', value: 'test' },
                        { label: '生产', value: 'prod' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="已配置实例 *" required>
                    <Select
                      showSearch
                      optionFilterProp="label"
                      placeholder="请选择实例"
                      style={{ width: '100%' }}
                      value={form.instance_id || undefined}
                      onChange={(v) => onInstChange(v)}
                      options={sourceOptions.map((i) => ({ label: instLabel(i), value: i.id }))}
                    />
                  </Form.Item>
                  <div className="addr">{addrText}</div>
                  <Form.Item label="数据库名 *" required>
                    <Select
                      showSearch
                      optionFilterProp="label"
                      placeholder="业务库名"
                      style={{ width: '100%' }}
                      value={form.db_name || undefined}
                      onChange={(v) => onDbChange(v)}
                      options={dbOptions.map((d) => ({ label: dbLabel(d), value: d.db_name }))}
                    />
                  </Form.Item>
                  <Form.Item label="目标表（空=整库；可多选）">
                    <Select
                      mode="tags"
                      showSearch
                      allowClear
                      placeholder="schema.table"
                      style={{ width: '100%' }}
                      value={form.tables}
                      onChange={(v) => patchForm({ tables: v })}
                      options={tableOptions.map((t) => ({ label: t, value: t }))}
                    />
                  </Form.Item>
                  {engine === 'postgres' && (
                    <div className="info tip">
                      <strong>生产默认保持 REPLICA IDENTITY DEFAULT</strong>
                      <p>
                        1）DEFAULT：WAL 通常只记主键；闪回用 FPW 与堆页死行补齐非主键列，请在 VACUUM/prune 前完成（闪回 SLA）。
                      </p>
                      <p>
                        2）高风险表可在变更窗临时 FULL（勿常态全库 FULL），结束后改回 DEFAULT：
                      </p>
                      <pre>{`ALTER TABLE schema.table REPLICA IDENTITY FULL;
-- 变更窗结束
ALTER TABLE schema.table REPLICA IDENTITY DEFAULT;`}</pre>
                    </div>
                  )}
                </div>
                <div>
                  <h3>闪回目标</h3>
                  <Form.Item label="起始时间 *（须早于误操作 COMMIT）" required>
                    <DatePicker
                      showTime
                      format={DT_FMT}
                      style={{ width: '100%' }}
                      disabledDate={disableFuture}
                      value={dtValue(form.start_time)}
                      onChange={(d) => patchForm({ start_time: d ? d.format(DT_FMT) : '' })}
                    />
                  </Form.Item>
                  <div className="chips">
                    {relChips.map((c) => (
                      <button
                        key={c.m}
                        type="button"
                        className={'chip' + (relMin === c.m ? ' on' : '')}
                        onClick={() => applyRel(c.m)}
                      >
                        {c.label}
                      </button>
                    ))}
                  </div>
                  <Form.Item label="结束时间（须晚于误操作 COMMIT）" style={{ marginTop: 12 }}>
                    <DatePicker
                      showTime
                      format={DT_FMT}
                      style={{ width: '100%' }}
                      disabledDate={disableFuture}
                      value={dtValue(form.end_time)}
                      onChange={(d) => patchForm({ end_time: d ? d.format(DT_FMT) : '' })}
                    />
                  </Form.Item>
                  <Form.Item label="输出">
                    <Select
                      style={{ width: '100%' }}
                      value={form.output_kind}
                      onChange={(v) => patchForm({ output_kind: v })}
                      options={[
                        { value: 'flashback', label: 'Undo SQL（反向闪回）' },
                        { value: 'original', label: '原始 Redo SQL' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="SQL 类型">
                    <div className="chips">
                      {(['insert', 'update', 'delete', 'ddl'] as const).map((t) => (
                        <label key={t} className={'chip' + (form.sql_types.includes(t) ? ' on' : '')}>
                          <input
                            type="checkbox"
                            checked={form.sql_types.includes(t)}
                            onChange={(e) => {
                              const set = new Set(form.sql_types)
                              if (e.target.checked) set.add(t)
                              else set.delete(t)
                              patchForm({ sql_types: [...set] })
                            }}
                          />{' '}
                          {t}
                        </label>
                      ))}
                    </div>
                  </Form.Item>
                  <Form.Item label="审核人" required>
                    <Select
                      mode="tags"
                      showSearch
                      maxTagCount={1}
                      placeholder="审批通过后执行"
                      style={{ width: '100%' }}
                      value={form.reviewer ? [form.reviewer] : []}
                      onChange={(v) => patchForm({ reviewer: (v as string[]).slice(-1)[0] || '' })}
                      options={reviewerOptions.map((a) => ({ label: a, value: a }))}
                    />
                  </Form.Item>
                  <Collapse
                    items={[
                      {
                        key: 'adv',
                        label: '高级位点',
                        children: (
                          <Row gutter={12}>
                            <Col span={12}>
                              <Form.Item label="start_file">
                                <Input
                                  value={form.start_file}
                                  onChange={(e) => patchForm({ start_file: e.target.value })}
                                />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item label="start_pos">
                                <InputNumber
                                  style={{ width: '100%' }}
                                  value={form.start_pos}
                                  onChange={(v) => patchForm({ start_pos: Number(v || 0) })}
                                />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item label="stop_file">
                                <Input
                                  value={form.stop_file}
                                  onChange={(e) => patchForm({ stop_file: e.target.value })}
                                />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item label="stop_pos">
                                <InputNumber
                                  style={{ width: '100%' }}
                                  value={form.stop_pos}
                                  onChange={(v) => patchForm({ stop_pos: Number(v || 0) })}
                                />
                              </Form.Item>
                            </Col>
                          </Row>
                        ),
                      },
                    ]}
                  />
                </div>
              </div>
              <div className="actions">
                <Button type="primary" loading={prechecking} onClick={doPrecheck}>
                  下一步：预检查
                </Button>
              </div>
            </Form>
          )}

          {wizardStep === 2 && (
            <div>
              <div className="info">
                {precheck?.ok ? '预检查通过' : '预检查未通过'} · {precheck?.host || ''}:
                {precheck?.port || ''} · {precheck?.parse_mode || ''} · {precheck?.server_version || ''}
              </div>
              <div className="checks">
                {(precheck?.items || []).map((it: any) => (
                  <div key={it.code || it.name} className="check">
                    <Tag color={itemTag(it)}>{it.status || (it.ok ? 'passed' : 'failed')}</Tag>
                    <div>
                      <strong>{it.name}</strong>
                      <div className="meta">{it.message}</div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="actions">
                <Button onClick={() => setWizardStep(1)}>返回修改</Button>
                <Button type="primary" disabled={!precheck?.ok} onClick={goConfirm}>
                  下一步：提交审批
                </Button>
              </div>
            </div>
          )}

          {wizardStep === 3 && (
            <div>
              <div className="addr">
                实例 <strong>{instText({ instance_id: form.instance_id })}</strong> · 库 {form.db_name} · 表{' '}
                {form.tables.length ? form.tables.join(', ') : '整库'}
                <br />
                时间窗 {form.start_time} → {form.end_time || '现在'} ·{' '}
                {form.output_kind === 'original' ? '原始 Redo SQL' : 'Undo SQL'}
                <br />
                审核人 {form.reviewer || '—'} · 审批通过后按当前配置自动执行
              </div>
              <div className="actions">
                <Button onClick={() => setWizardStep(2)}>返回预检查</Button>
                <Auth urn={Perm.flashback.edit}>
                  <Button type="primary" loading={creating} disabled={!form.reviewer} onClick={create}>
                    提交审批
                  </Button>
                </Auth>
              </div>
            </div>
          )}
        </section>
      )}

      {page === 'history' && (
        <section className="card">
          <div className="toolbar">
            <div className="grow">
              <p className="lead">任务地址来自已配置实例，不在这里改连接。</p>
            </div>
            <Auth urn={Perm.flashback.create}>
              <Button type="primary" onClick={() => go('new')}>
                新建任务
              </Button>
            </Auth>
          </div>
          <div className="toolbar">
            <Select
              allowClear
              placeholder="全部状态"
              style={{ width: 140 }}
              value={histStatus || undefined}
              onChange={(v) => setHistStatus(v || '')}
              options={[
                { value: 'awaiting_approval', label: '待审批' },
                { value: 'pending', label: '排队' },
                { value: 'running', label: '执行中' },
                { value: 'succeeded', label: '成功' },
                { value: 'failed', label: '失败' },
                { value: 'rejected', label: '已驳回' },
              ]}
            />
            <Input
              allowClear
              placeholder="库名 / 实例 / 任务 ID"
              style={{ width: 260 }}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onPressEnter={() => setHistPage(1)}
            />
            <Button onClick={load}>刷新</Button>
          </div>
          <ResponsiveTable
            rowKey={(r) => r.id}
            size="small"
            bordered
            loading={listLoading}
            columns={histColumns}
            dataSource={histRows}
            pagination={false}
            locale={{ emptyText: '还没有历史任务' }}
          />
          <div className="pager">
            <span>共 {histFiltered.length} 条</span>
            <Pagination
              current={histPage}
              pageSize={10}
              total={histFiltered.length}
              showSizeChanger={false}
              onChange={(p) => setHistPage(p)}
            />
          </div>
        </section>
      )}

      {page === 'task' && (
        <section className="card">
          <div className="toolbar">
            <div className="grow">
              <p className="mono">{detail?.id}</p>
            </div>
            {detail && <Tag color={rowStatusTag(detail)}>{rowStatusText(detail)}</Tag>}
            {detail?.issue_id && (
              <Button type="link" onClick={() => navigate('/ops/tickets/' + detail.issue_id)}>
                {approvalText(detail.approval_status)}
              </Button>
            )}
            <Button onClick={() => go('history')}>返回历史</Button>
          </div>
          {detail && (
            <div className="split">
              <div className="addr">
                实例 {instText(detail)} · {detail.host}:{detail.port}
                <br />
                库 {detail.db_name || detail.database} / {tableText(detail)}
                <br />
                时间 {detail.start_time || detail.target_time} → {detail.end_time || '现在'}
                {detail.error_message && <div className="err">{detail.error_message}</div>}
              </div>
              <div className="meters">
                <div>
                  <div className="meta">
                    获取日志 {detail.log_done || 0}/{detail.log_total || 0}
                  </div>
                  <div className="track">
                    <div className="bar" style={{ width: pct(detail.log_done, detail.log_total) + '%' }} />
                  </div>
                </div>
                <div>
                  <div className="meta">
                    解析 {detail.parse_done || 0}/{detail.parse_total || 0}
                  </div>
                  <div className="track">
                    <div
                      className="bar parse"
                      style={{ width: pct(detail.parse_done, detail.parse_total) + '%' }}
                    />
                  </div>
                </div>
                <div className="meta">变更数 {detail.change_count ?? 0}</div>
              </div>
            </div>
          )}
          <div className="actions">
            <Button onClick={reloadSQL}>刷新 SQL</Button>
            <Button onClick={copySQL}>复制选中 SQL</Button>
            <Button onClick={downloadSQL}>下载选中 .sql</Button>
            <Auth urn={Perm.flashback.edit}>
              <Button
                type="primary"
                disabled={detail?.status !== 'succeeded' || !selectedSQLs.length}
                onClick={() => toDml(detail)}
              >
                转 DML 工单
              </Button>
            </Auth>
            <span className="meta">{selectedSQLs.length ? `已选 ${selectedSQLs.length} 条` : '未选'}</span>
          </div>
          <ResponsiveTable
            rowKey={(r) => r.seq ?? r.statement}
            bordered
            size="small"
            scroll={{ y: 360 }}
            pagination={false}
            columns={sqlColumns}
            dataSource={sqls}
            rowSelection={{ type: 'checkbox', onChange: onSqlSel }}
          />
          <h3>运行日志</h3>
          <div className="logs">
            {logs.length ? (
              logs.map((l, i) => (
                <p key={i} className={'log-line ' + String(l.level || '').toLowerCase()}>
                  [{fmtTime(l.created_at)}] {l.level} {l.message}
                </p>
              ))
            ) : (
              <p className="log-line">暂无日志</p>
            )}
          </div>
        </section>
      )}

      {detailLoading && page === 'task' && null}
    </div>
  )
}
