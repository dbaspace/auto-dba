import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { useAppDispatch, useAppSelector } from '../../store'
import { closeTab, openTab, tabKey } from '../../store/tabsSlice'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Drawer,
  Input,
  Modal,
  Pagination,
  Radio,
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
  Tooltip,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import CdcDebeziumConfigTab from './CdcDebeziumConfigTab'
import CdcReinitModal from './CdcReinitModal'
import PipelineVisualization, { PipelineNode } from './PipelineVisualization'
import ShardSourceTablesGrid from './ShardSourceTablesGrid'
import {
  filterPublicationDisplayRows,
  groupPublishedTablesByMappings,
  type PublicationDisplayRow,
} from './cdcPublication'
import {
  approvalText,
  assignedToOf,
  connectorWorkerURL,
  createdByOf,
  currentUsername,
  decodeCdcExtra,
  envTagColor,
  formatLagBytes,
  formatLogTime,
  formatSourceScopeLabel,
  endpointLabel,
  isValidCdcTaskID,
  keyModelLabel,
  LOG_STEP_OPTIONS,
  logStepText,
  isDebeziumConnectorAbnormal,
  mappingSourceTagColor,
  mappingSourceText,
  pipelineBundle,
  pipelineNodeTone,
  pipelineStateText,
  sameUser,
  statusTagColor,
  statusText,
  syncTableCountView,
  typeTagColor,
  typeText,
} from './cdc'
import './cdc.css'

export default function CdcDetail() {
  const navigate = useNavigate()
  const location = useLocation()
  const params = useParams()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const id = String(params.id || '')

  const backToCdcList = (opts?: { replace?: boolean }) => {
    const detailKey = tabKey(location.pathname, location.search)
    dispatch(openTab({ path: '/ops/cdc', menus, title: 'CDC 同步任务' }))
    dispatch(closeTab(detailKey))
    navigate('/ops/cdc', opts?.replace ? { replace: true } : undefined)
  }

  const [loading, setLoading] = useState(false)
  const [task, setTask] = useState<any>({})
  const [instances, setInstances] = useState<any[]>([])
  const [tab, setTab] = useState('overview')
  const [pipeline, setPipeline] = useState<any>({})
  const [logs, setLogs] = useState<any[]>([])
  const [logStep, setLogStep] = useState('')
  const [logLevel, setLogLevel] = useState('')
  const [logQ, setLogQ] = useState('')
  const [logPage, setLogPage] = useState(1)
  const [logPageSize, setLogPageSize] = useState(50)
  const [logTotal, setLogTotal] = useState(0)
  const [logDetailOpen, setLogDetailOpen] = useState(false)
  const [logDetail, setLogDetail] = useState<any>({})
  const [schemaAlerts, setSchemaAlerts] = useState<any>(null)
  const [schemaScanning, setSchemaScanning] = useState(false)
  const [pipelineLoading, setPipelineLoading] = useState(false)
  const [pipelineRefreshedAt, setPipelineRefreshedAt] = useState('')
  const [approveOpen, setApproveOpen] = useState(false)
  const [rejectOpen, setRejectOpen] = useState(false)
  const [approveComment, setApproveComment] = useState('')
  const [rejectComment, setRejectComment] = useState('')
  const [approving, setApproving] = useState(false)
  const [tableRows, setTableRows] = useState<any[]>([])
  const [tableQ, setTableQ] = useState('')
  const [showDeletedMaps, setShowDeletedMaps] = useState(false)
  const [tablePage, setTablePage] = useState(1)
  const [tablePageSize, setTablePageSize] = useState(10)
  const [ddlOpen, setDdlOpen] = useState(false)
  const [ddlSQL, setDdlSQL] = useState('')
  const [ddlTarget, setDdlTarget] = useState('')
  const [runtimeOpen, setRuntimeOpen] = useState(false)
  const [runtime, setRuntime] = useState<any>({})
  const [publication, setPublication] = useState<any>({})
  const [pubOpen, setPubOpen] = useState(false)
  const [pubMode, setPubMode] = useState<'add' | 'remove'>('add')
  const [pubTables, setPubTables] = useState('')
  const [pubReviewer, setPubReviewer] = useState('')
  const [pubSubmitting, setPubSubmitting] = useState(false)
  const [pubSelected, setPubSelected] = useState<string[]>([])
  const [availablePubTables, setAvailablePubTables] = useState<string[]>([])
  const [syncedTables, setSyncedTables] = useState<string[]>([])
  const [pubKeyword, setPubKeyword] = useState('')
  const [diffOpen, setDiffOpen] = useState(false)
  const [diffTarget, setDiffTarget] = useState<any>(null)
  const [diffCols, setDiffCols] = useState<any[]>([])
  const [diffReviewer, setDiffReviewer] = useState('')
  const [diffSubmitting, setDiffSubmitting] = useState(false)
  const [migrateSQL, setMigrateSQL] = useState('')
  const [diffOffset, setDiffOffset] = useState('OFFSET_BEGINNING')
  const [reinitOpen, setReinitOpen] = useState(false)

  const pollRef = useRef<number>(0)
  const tabRef = useRef(tab)
  tabRef.current = tab
  const tablesLoadedRef = useRef(false)
  const publicationLoadedRef = useRef(false)
  const pipelineLoadedRef = useRef(false)

  const extra = useMemo(() => decodeCdcExtra(task.description).extra, [task])
  const note = useMemo(() => decodeCdcExtra(task.description).text, [task])
  const pending = task.approval_status === 'pending' || task.status === 'pending'
  const errorStep = pipeline.error_step || extra.error_step || ''
  const sourceScopeLabel = formatSourceScopeLabel(task.source_db, extra.source_db_prefix, extra.source_db_names)
  const source = instances.find((i) => i.id === task.source_instance_id)
  const target = instances.find((i) => i.id === task.target_instance_id)
  const isInit = task.task_type === 'init'
  const countView = syncTableCountView(extra)
  const workerURL = connectorWorkerURL(extra, {})
  const createdBy = createdByOf(task, extra)
  const assignedTo = assignedToOf(task, extra)
  const me = currentUsername()
  const isApprover = pending && task.issue_id && (!assignedTo || sameUser(me, assignedTo))
  const isCreatorOnly = pending && task.issue_id && sameUser(me, createdBy) && !isApprover
  const showApprovalBar = pending && task.issue_id && (isApprover || isCreatorOnly || !me)
  const isDeleteApproval = task.task_type === 'delete_task'
  const tableDriftCount = Number(schemaAlerts?.drift_table_count || 0)
  const schemaHasDrift = schemaAlerts?.state === 'WARN' || schemaAlerts?.state === 'ERROR'
  const showSchemaBanner =
    isInit &&
    !pending &&
    (schemaHasDrift || schemaAlerts?.state === 'OK' || schemaAlerts?.state === 'SKIPPED' || !!schemaAlerts)
  const pipe = pipelineBundle(pipeline)

  function taskID() {
    const tid = task.id || id
    return isValidCdcTaskID(tid) ? String(tid).trim() : ''
  }

  const visibleTableRows = useMemo(() => {
    const q = tableQ.trim().toLowerCase()
    return (tableRows || []).filter((r) => {
      if (!q) return true
      const blob = `${(r.source_tables || []).join(' ')} ${r.target_table || ''} ${(r.topic_names || []).join(' ')}`.toLowerCase()
      return blob.includes(q)
    })
  }, [tableRows, tableQ])
  const pagedTableRows = useMemo(() => {
    const start = (tablePage - 1) * tablePageSize
    return visibleTableRows.slice(start, start + tablePageSize)
  }, [visibleTableRows, tablePage, tablePageSize])

  const approvalBarTitle = isCreatorOnly
    ? isDeleteApproval
      ? '撤回删除申请'
      : '撤回待审批'
    : isDeleteApproval
    ? '删除审批'
    : '审批操作'
  const approvalBarHint = isCreatorOnly
    ? isDeleteApproval
      ? '您是申请人：点击「撤回」并填写原因后，取消本次删除申请，任务恢复原有运行状态。'
      : '您是创建人：点击「撤回」并填写原因后，任务进入「已驳回」。'
    : isDeleteApproval
    ? assignedTo
      ? `删除审批人为「${assignedTo}」。通过后将清理 Publication / Slot / Connector / Routine Load 等资源；驳回则保留任务。`
      : '通过后执行删除并清理资源；驳回则取消删除申请。'
    : assignedTo
    ? `本任务指定审批人为「${assignedTo}」，请在本区域完成通过/驳回。`
    : '请在本区域完成审批；列表页仅支持查询与进入详情。'

  const schemaBannerType: 'error' | 'warning' | 'success' | 'info' = (() => {
    const st = String(schemaAlerts?.state || '').toUpperCase()
    if (st === 'ERROR') return 'error'
    if (st === 'WARN' && tableDriftCount) return 'warning'
    if (st === 'OK') return 'success'
    return 'info'
  })()
  const schemaBannerTitle = (() => {
    const st = String(schemaAlerts?.state || '').toUpperCase()
    if (st === 'ERROR') return schemaAlerts?.message || '表结构巡检失败'
    if (st === 'WARN' && tableDriftCount) return schemaAlerts?.message || '表结构巡检有差异'
    if (st === 'OK') return '源端与目标表结构一致'
    if (st === 'SKIPPED') return '未执行结构检查'
    return '表结构巡检'
  })()

  const pipelineNodes: PipelineNode[] = useMemo(() => {
    const p = pipe || {}
    const pg = p.pg_source || p.pg || {}
    const pub = p.publication || {}
    const slot = p.slot || {}
    const conn = p.connector || {}
    const kafka = p.kafka || {}
    const sr = p.starrocks || {}
    const rls = p.routine_loads || []
    const runningRL = rls.filter((j: any) => String(j.state || '').toUpperCase() === 'RUNNING').length
    const lag = slot.replication_lag_bytes ?? slot.lag_bytes
    const manual = extra.auto_create_table === false
    return [
      {
        id: 'pg',
        name: 'PG Source',
        detail:
          pg.host && pg.port
            ? `${pg.host}:${pg.port}`
            : pub.name || slot.name
            ? '源端可达（Publication/Slot 已探测）'
            : pg.message || '等待探测',
        tone: pipelineNodeTone(pg.state || (pub.name || slot.name ? 'READY' : '')),
        extras: [],
        message: pg.message || '',
      },
      {
        id: 'publication',
        name: 'Publication',
        detail: pub.name || extra.publication_name || '未上报',
        tone: pipelineNodeTone(pub.exists ? 'READY' : pub.state || 'UNKNOWN'),
        extras: [
          { label: 'Publication 表', value: String(pub.table_count ?? 0) },
          { label: '存在', value: pub.exists ? '是' : '否' },
        ],
        message: pub.message || '',
      },
      {
        id: 'slot',
        name: 'Replication Slot',
        detail: slot.name || extra.slot_name || '未上报',
        tone: pipelineNodeTone(slot.active ? 'READY' : slot.state || 'UNKNOWN'),
        extras: [
          { label: '状态', value: slot.active ? 'active' : 'inactive' },
          { label: 'lag', value: formatLagBytes(lag) },
        ],
        message: slot.message || slot.restart_lsn || '',
      },
      {
        id: 'debezium',
        name: 'Debezium Connector',
        detail: conn.name || extra.connector_name || '未上报',
        tone: pipelineNodeTone(conn.state),
        extras: [
          { label: '状态', value: pipelineStateText(conn.state) || '—' },
          { label: '模式', value: extra.debezium_mode || conn.debezium_mode || '—' },
          ...((conn.tasks || []).length
            ? [
                {
                  label: 'tasks',
                  value: `${(conn.tasks || []).filter((t: any) => String(t.state).toUpperCase() === 'RUNNING').length}/${conn.tasks.length} RUNNING`,
                },
              ]
            : []),
        ],
        message: conn.message || '',
        ready: Boolean(conn.name || extra.connector_name),
      },
      {
        id: 'kafka',
        name: 'Kafka Topics',
        detail: 'Kafka 消息总线',
        tone: pipelineNodeTone(kafka.state),
        extras: [
          { label: 'state', value: kafka.state || '—' },
          { label: 'topics', value: String(kafka.topic_count ?? (p.kafka_topics || []).length) },
          ...(kafka.all_exist != null ? [{ label: 'all_exist', value: kafka.all_exist ? '是' : '否' }] : []),
        ],
        message: kafka.message || '',
      },
      {
        id: 'starrocks',
        name: 'StarRocks',
        detail: manual
          ? '目标端未配置（手动模式，无需建表）'
          : sr.host && sr.port
          ? `${sr.host}:${sr.port}`
          : sr.message || task.target_db || '等待',
        tone: manual ? 'is-ok' : pipelineNodeTone(sr.state),
        extras: manual
          ? [{ label: '说明', value: '手动 DDL' }]
          : [
              { label: 'state', value: sr.state || '—' },
              { label: '配置表数', value: String(pub.table_count ?? rls.length) },
            ],
      },
      {
        id: 'routine_load',
        name: 'Routine Load',
        detail: '导入任务（依赖目标表）',
        tone: manual ? 'is-ok' : pipelineNodeTone(sr.state),
        extras: rls.length
          ? [
              { label: '运行/总数', value: `${runningRL}/${rls.length}` },
              {
                label: '同步任务',
                value:
                  rls
                    .slice(0, 2)
                    .map((j: any) => j.name || j.table)
                    .join(', ') + (rls.length > 2 ? '…' : ''),
              },
            ]
          : [{ label: '状态', value: '等待' }],
      },
    ]
  }, [pipe, extra, task])

  const publishedTableNames = useMemo(() => {
    const live: string[] = (publication.tables || []).map((n: any) => String(n).trim()).filter(Boolean)
    if (live.length) return live
    const rec = publication.task_tables || extra.tables || []
    return (Array.isArray(rec) ? rec : []).map((n: any) => String(n).trim()).filter(Boolean)
  }, [publication, extra])

  const pubDisplayRows = useMemo(
    () => groupPublishedTablesByMappings(publishedTableNames, tableRows),
    [publishedTableNames, tableRows],
  )

  const pubFilteredRows = useMemo(
    () => filterPublicationDisplayRows(pubDisplayRows, pubKeyword),
    [pubDisplayRows, pubKeyword],
  )


  function tableQuery() {
    return {
      include_deleted: showDeletedMaps ? '1' : undefined,
      include_schema_drift: isInit ? '1' : undefined,
    }
  }

  const loadLogs = useCallback(
    async (page = logPage, size = logPageSize) => {
      const tid = taskID()
      if (!tid) return
      const { data } = await api
        .get(`/cdc-sync/tasks/${tid}/logs`, {
          params: {
            step: logStep,
            level: logLevel,
            log_level: logLevel,
            q: logQ,
            page,
            page_size: size,
          },
          silent: true,
        })
        .catch(() => ({ data: {} as any }))
      const body = data.data
      const list = Array.isArray(body) ? body : body?.list || []
      setLogs(list)
      setLogTotal(body?.pagination?.total ?? data.total ?? list.length)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [logStep, logLevel, logQ, logPage, logPageSize, task.id],
  )

  async function loadSchemaAlerts(refresh = false) {
    const tid = taskID()
    if (!tid) return
    if (refresh) setSchemaScanning(true)
    const { data } = refresh
      ? await api.post(`/cdc-sync/tasks/${tid}/schema-alerts/scan`).catch(() => ({ data: {} as any }))
      : await api.get(`/cdc-sync/tasks/${tid}/schema-alerts`).catch(() => ({ data: {} as any }))
    const alerts = data.data || null
    setSchemaAlerts(alerts)
    setSchemaScanning(false)
    if (!alerts?.items) return
    const by = new Map((alerts.items || []).map((it: any) => [String(it.target_table || '').toLowerCase(), it]))
    setTableRows((prev) =>
      prev.map((row) => {
        const it: any = by.get(String(row.target_table || '').toLowerCase())
        if (!it) return row
        return { ...row, schema_has_drift: !!it.has_drift, schema_missing_columns: it.missing_columns || [] }
      }),
    )
  }

  async function loadPublication(tid?: string) {
    const id0 = tid || task.id || id
    if (!isValidCdcTaskID(id0)) return
    const [{ data }, synced] = await Promise.all([
      api.get(`/cdc-sync/tasks/${id0}/publication`, { silent: true }).catch(() => ({ data: {} as any })),
      api.get(`/cdc-sync/tasks/${id0}/synced-tables`, { silent: true }).catch(() => ({ data: {} as any })),
    ])
    setPublication(data.data || {})
    setSyncedTables(synced.data.data || [])
    publicationLoadedRef.current = true
  }

  function mergeRLFromPipeline(rowsIn: any[], pipeObj: any) {
    const rls = pipeObj?.routine_loads || pipeline.routine_loads || []
    if (!rls.length) return rowsIn
    const by = new Map(rls.map((j: any) => [String(j.table || '').toLowerCase(), j]))
    return rowsIn.map((row) => {
      const hit: any = by.get(String(row.target_table || '').toLowerCase())
      if (!hit) return row
      return {
        ...row,
        routine_load_name: row.routine_load_name || hit.name,
        routine_load_status: hit.state || row.routine_load_status,
      }
    })
  }

  async function loadPipelineStatus(tid: string, showSpin = false) {
    if (showSpin) setPipelineLoading(true)
    try {
      const { data } = await api.get(`/cdc-sync/tasks/${tid}/status`, { silent: true }).catch(() => ({ data: {} as any }))
      const pipeObj = data.data || {}
      setPipeline(pipeObj)
      setPipelineRefreshedAt(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
      pipelineLoadedRef.current = true
      return pipeObj
    } finally {
      if (showSpin) setPipelineLoading(false)
    }
  }

  async function reloadTables() {
    if (!id) return
    const { data } = await api
      .get(`/cdc-sync/tasks/${id}/tables`, { params: tableQuery(), silent: true })
      .catch(() => ({ data: {} as any }))
    const raw = data.data || []
    let rows0 = raw.map((x: any) =>
      typeof x === 'string' ? { name: x, target_table: x, source_tables: [x] } : { name: (x.source_tables || [])[0], ...x },
    )
    rows0 = mergeRLFromPipeline(rows0, pipelineBundle(pipeline))
    if (schemaAlerts?.items?.length) {
      const by = new Map(
        (schemaAlerts.items || []).map((it: any) => [String(it.target_table || '').toLowerCase(), it]),
      )
      rows0 = rows0.map((row) => {
        const it: any = by.get(String(row.target_table || '').toLowerCase())
        if (!it) return row
        return { ...row, schema_has_drift: !!it.has_drift, schema_missing_columns: it.missing_columns || [] }
      })
    }
    setTableRows(rows0)
    tablesLoadedRef.current = true
  }

  /** 对齐 universe-fe：首屏只等任务本体，重请求后台/按 Tab 懒加载 */
  const load = useCallback(async () => {
    const tid = isValidCdcTaskID(id) ? id : ''
    if (!tid) {
      window.clearInterval(pollRef.current)
      return
    }
    tablesLoadedRef.current = false
    publicationLoadedRef.current = false
    pipelineLoadedRef.current = false
    setLoading(true)
    try {
      const { data } = await api.get(`/cdc-sync/tasks/${tid}`)
      const t0 = data.data || {}
      setTask(t0)
      setLoading(false)
      void api
        .get('/instances')
        .then((res) => setInstances(res.data.data || []))
        .catch(() => {})
      void loadPipelineStatus(tid)
      if (t0.task_type === 'init' || t0.parent_id) void loadSchemaAlerts(false)
    } catch {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  // 仅编排中 / 待审批轮询；稳定 running 停轮询（对齐 universe-fe）
  const PROVISIONING = useMemo(
    () => new Set(['init', 'creating', 'provisioning', 'configuring', 'reinitializing', 'adding', 'removing', 'altering']),
    [],
  )
  useEffect(() => {
    window.clearInterval(pollRef.current)
    const should = pending || PROVISIONING.has(String(task.status || ''))
    if (!should || !taskID()) return
    pollRef.current = window.setInterval(() => {
      const tid = taskID()
      if (!tid) {
        window.clearInterval(pollRef.current)
        return
      }
      void loadPipelineStatus(tid)
      api
        .get(`/cdc-sync/tasks/${tid}`, { silent: true })
        .then(({ data }) => setTask((prev: any) => data.data || prev))
        .catch(() => {})
      if (tabRef.current === 'logs') void loadLogs()
    }, 10000)
    return () => window.clearInterval(pollRef.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.status, task.approval_status, task.id, pending])

  useEffect(() => {
    if (!isValidCdcTaskID(id)) {
      backToCdcList({ replace: true })
      return
    }
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  async function refreshPipeline() {
    const tid = taskID()
    if (!tid) return
    await loadPipelineStatus(tid, true)
  }

  async function ensureTablesLoaded() {
    if (tablesLoadedRef.current || !id) return
    await reloadTables()
  }

  async function ensurePublicationLoaded() {
    if (publicationLoadedRef.current) return
    await loadPublication(id)
  }

  async function ensurePipelineLoaded() {
    if (pipelineLoadedRef.current) return
    const tid = taskID()
    if (!tid) return
    await loadPipelineStatus(tid, true)
  }

  async function showMappingDDL(row: any) {
    setDdlTarget(row.target_table || '映射')
    setDdlSQL('加载中…')
    setDdlOpen(true)
    const rid = row.id || row.target_table
    const { data } = await api
      .get(`/cdc-sync/tasks/${task.id}/tables/${encodeURIComponent(rid)}/ddl`)
      .catch(() => ({ data: {} as any }))
    const d = data.data || {}
    setDdlSQL([d.create_table_sql || '', d.routine_load_sql ? `-- Routine Load\n${d.routine_load_sql}` : ''].filter(Boolean).join('\n\n') || '暂无 DDL')
  }

  async function openConnectorRuntime() {
    setRuntimeOpen(true)
    const { data } = await api.get(`/cdc-sync/tasks/${task.id}/connector/status`).catch(() => ({ data: {} as any }))
    const d = data.data || {}
    const conn = d.connector || {}
    setRuntime({
      name: conn.name || extra.connector_name,
      state: conn.state || d.worst_state,
      worker_id: conn.worker_id,
      tasks: conn.tasks || d.tasks || [],
    })
  }

  async function openPub(mode: 'add' | 'remove', prefillTables?: string[]) {
    setPubMode(mode)
    setPubReviewer('')
    setPubOpen(true)
    if (mode === 'add') {
      setPubTables('')
      setPubSelected([])
      const { data } = await api
        .get(`/cdc-sync/tasks/${task.id}/add-table/available-tables`)
        .catch(() => ({ data: {} as any }))
      setAvailablePubTables(data.data || [])
    } else {
      const pref = (prefillTables || []).map((t) => String(t).trim()).filter(Boolean)
      setPubSelected(pref)
      setPubTables(pref.length ? pref.join(', ') : '')
      setAvailablePubTables(syncedTables.length ? syncedTables : publishedTableNames)
    }
  }

  async function submitPub() {
    const tables = [
      ...new Set([...pubSelected, ...pubTables.split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean)]),
    ]
    if (!tables.length) {
      message.warning('请填写表名')
      return
    }
    if (!pubReviewer) {
      message.warning('请填写审核人')
      return
    }
    setPubSubmitting(true)
    try {
      const body = { tables, reviewer: pubReviewer, approver: pubReviewer }
      const { data } =
        pubMode === 'add'
          ? await api.post(`/cdc-sync/tasks/${task.id}/publication/tables`, body)
          : await api.delete(`/cdc-sync/tasks/${task.id}/publication/tables`, { data: body })
      message.success('已提交审批')
      setPubOpen(false)
      if (data.data?.issue_id) navigate('/ops/tickets/' + data.data.issue_id)
      else load()
    } finally {
      setPubSubmitting(false)
    }
  }

  async function diffOne(row: any) {
    setDiffTarget(row)
    setDiffCols([])
    setMigrateSQL('')
    setDiffReviewer('')
    setDiffOpen(true)
    try {
      const { data } = await api.post(`/cdc-sync/tasks/${task.id}/schema-diff`, {
        target_table: row.target_table,
        source_tables: row.source_tables || [row.target_table],
      })
      const item = (data.data?.items || [])[0]
      setDiffCols((item?.columns_to_add || []).map((c: any) => ({ ...c, selected: c.selected !== false })))
      if (item?.warnings?.length) message.warning(item.warnings.join('；'))
    } catch (e: any) {
      message.warning(e?.response?.data?.message || '列对比失败')
    }
  }

  async function loadMigrateSQL() {
    const cols = diffCols.filter((c) => c.selected)
    const { data } = await api.post(`/cdc-sync/tasks/${task.id}/schema-migrate-sql`, {
      target_table: diffTarget?.target_table,
      columns: cols,
    })
    setMigrateSQL(data.data?.sql || '')
  }

  async function submitAlter() {
    if (!diffReviewer) {
      message.warning('请填写审核人')
      return
    }
    const cols = diffCols.filter((c) => c.selected)
    setDiffSubmitting(true)
    try {
      const { data } = await api.post('/cdc-sync/tasks', {
        name: `${task.name}_加列`,
        task_type: 'alter_schema',
        parent_id: task.id,
        source_instance_id: task.source_instance_id,
        target_instance_id: task.target_instance_id,
        source_db: task.source_db,
        target_db: task.target_db,
        reviewer: diffReviewer,
        approver: diffReviewer,
        description: '源表加列',
        tables: [diffTarget.target_table],
        alter_schema_exec_mode: 'auto',
        kafka_default_offsets: diffOffset,
        alter_schema_tables: [
          {
            target_table: diffTarget.target_table,
            source_tables: diffTarget.source_tables || [diffTarget.target_table],
            columns: cols,
          },
        ],
      })
      message.success('已提交加列审批')
      setDiffOpen(false)
      if (data.data?.issue_id) navigate('/ops/tickets/' + data.data.issue_id)
    } finally {
      setDiffSubmitting(false)
    }
  }

  async function act(kind: string) {
    const { data } = await api.post(`/cdc-sync/tasks/${task.id}/${kind}`)
    if (data?.data?.status === 'reinitializing') message.success('已提交，正在重新初始化')
    else message.info(data.message || '已更新')
    await load()
  }

  async function approveTicket() {
    if (!task.issue_id) return
    setApproving(true)
    try {
      await api.put(`/tickets/${task.issue_id}/approve`, { comment: approveComment || undefined })
      message.success(isDeleteApproval ? '删除审批已通过，任务已清理' : '已通过审批')
      setApproveOpen(false)
      if (isDeleteApproval) {
        backToCdcList()
        return
      }
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '审批失败')
    } finally {
      setApproving(false)
    }
  }

  async function rejectTicket() {
    if (!task.issue_id) return
    if (!String(rejectComment || '').trim()) {
      message.warning(isCreatorOnly ? '请填写撤回原因' : '请填写驳回意见')
      return
    }
    setApproving(true)
    try {
      await api.put(`/tickets/${task.issue_id}/reject`, { reason: rejectComment.trim() })
      message.success('操作成功')
      setRejectOpen(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '驳回失败')
    } finally {
      setApproving(false)
    }
  }

  const pubColumns: ColumnsType<PublicationDisplayRow> = [
    {
      title: '源表',
      key: 'src',
      render: (_, row) =>
        row.kind === 'shard' ? (
          <div>
            <Tag color="purple">分表 {row.tables.length}</Tag>
            {row.shard_pattern ? <code style={{ marginLeft: 4 }}>{row.shard_pattern}</code> : null}
            <ShardSourceTablesGrid tables={row.tables} />
          </div>
        ) : (
          <div>
            <Tag color="blue">单表</Tag>{' '}
            <code>{row.tables[0] || '—'}</code>
          </div>
        ),
    },
    {
      title: '目标表',
      key: 'target',
      width: 180,
      render: (_, row) => <code>{row.target_table || row.tables[0] || '—'}</code>,
    },
    {
      title: '操作',
      key: 'op',
      width: 120,
      render: (_, row) => (
        <Button
          type="link"
          size="small"
          danger
          disabled={pending}
          onClick={() => openPub('remove', row.tables)}
        >
          移除
        </Button>
      ),
    },
  ]

  const mapColumns: ColumnsType<any> = [
    {
      title: '源表',
      key: 'src',
      width: 360,
      onCell: () => ({ className: 'cdc-map-src-cell' }),
      render: (_, row) => {
        const srcs: string[] = row.source_tables?.length ? row.source_tables : row.name ? [row.name] : []
        const isShard = !!(row.is_shard_group || srcs.length > 1)
        return isShard ? (
          <div className="cdc-map-src-cell">
            <div className="cdc-map-src-meta">
              <Tag color="orange">分表 {srcs.length}</Tag>
              {row.shard_pattern ? <code>{row.shard_pattern}</code> : null}
            </div>
            <ShardSourceTablesGrid tables={srcs} />
          </div>
        ) : (
          <div className="cdc-map-src-meta">
            <Tag color="blue">单表</Tag>
            <code>{srcs[0] || '—'}</code>
          </div>
        )
      },
    },
    {
      title: '目标表',
      key: 'tgt',
      width: 140,
      ellipsis: true,
      render: (_, row) => (
        <Tooltip title={row.target_table}>
          <span>
            <code
              style={{
                color: row.status === 'deleted' ? '#909399' : undefined,
                fontSize: 11,
              }}
            >
              {row.target_table}
            </code>
            {row.status === 'deleted' && <Tag style={{ marginLeft: 4 }}>已移除</Tag>}
          </span>
        </Tooltip>
      ),
    },
    {
      title: 'KEY 列',
      key: 'keys',
      width: 100,
      render: (_, row) => {
        const ks = row.key_columns || row.primary_keys || []
        if (!ks.length) return <span style={{ color: '#c0c4cc' }}>—</span>
        return (
          <div className="cdc-map-key-wrap">
            {ks.map((k: string) => (
              <Tag key={k} style={{ margin: 0 }}>
                {k}
              </Tag>
            ))}
          </div>
        )
      },
    },
    {
      title: 'Kafka Topic',
      key: 'topic',
      width: 200,
      ellipsis: true,
      render: (_, row) => {
        const topics: string[] = row.topic_names || []
        if (!topics.length) return <span style={{ color: '#c0c4cc' }}>—</span>
        const tip =
          topics.length > 1 ? (
            <span style={{ whiteSpace: 'pre-wrap' }}>{topics.join('\n')}</span>
          ) : (
            topics[0]
          )
        return (
          <Tooltip title={tip}>
            <div className="cdc-map-topic-wrap">
              <code>{topics[0]}</code>
              {topics.length > 1 ? <span style={{ color: '#8c8c8c' }}> +{topics.length - 1}</span> : null}
            </div>
          </Tooltip>
        )
      },
    },
    {
      title: '结构差异',
      key: 'drift',
      width: 100,
      render: (_, row) =>
        row.schema_has_drift ? (
          <Tag color="warning">缺列 {(row.schema_missing_columns || []).length || ''}</Tag>
        ) : schemaAlerts?.state === 'OK' ? (
          <Tag color="success">一致</Tag>
        ) : (
          <span style={{ color: '#c0c4cc' }}>—</span>
        ),
    },
    {
      title: 'Routine Load',
      key: 'rl',
      width: 160,
      ellipsis: true,
      render: (_, row) => {
        const name = row.routine_load_name || '—'
        return (
          <Tooltip title={name === '—' ? undefined : name}>
            <span className="cdc-map-rl-cell">
              <code>{name}</code>
            </span>
          </Tooltip>
        )
      },
    },
    {
      title: '来源',
      key: 'source',
      width: 80,
      render: (_, row) => <Tag color={mappingSourceTagColor(row.source)}>{mappingSourceText(row.source)}</Tag>,
    },
    {
      title: '操作',
      key: 'op',
      width: 180,
      fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          <Button
            type="link"
            size="small"
            disabled={!isInit || pending || row.status === 'deleted'}
            onClick={() => diffOne(row)}
          >
            + 同步新字段
          </Button>
          <Button type="link" size="small" onClick={() => showMappingDDL(row)}>
            查看 DDL
          </Button>
        </Space>
      ),
    },
  ]

  const descItems = (
    items: { label: string; children: React.ReactNode; span?: number }[],
  ) => items.map((it, i) => <Descriptions.Item key={i} label={it.label} span={it.span}>{it.children}</Descriptions.Item>)

  return (
    <div className="cdc-shell cdc-detail" style={{ overflow: 'auto' }}>
      <div className="cdc-back">
        <Button type="link" onClick={() => backToCdcList()}>
          ← 返回列表
        </Button>
      </div>
      <div className="cdc-tag-row">
        {task.task_type && <Tag color={typeTagColor(task.task_type)}>{typeText(task.task_type)}</Tag>}
        {task.status && !pending && <Tag color={statusTagColor(task.status)}>{statusText(task.status)}</Tag>}
        {extra.env && <Tag color={envTagColor(extra.env)}>{extra.env}</Tag>}
        {task.approval_status && (
          <Tag color={task.approval_status === 'approved' ? 'success' : task.approval_status === 'rejected' ? 'error' : 'warning'}>
            {approvalText(task.approval_status)}
          </Tag>
        )}
        {task.parent_id && (
          <span className="muted">
            关联：
            <a onClick={() => navigate('/ops/cdc/' + task.parent_id)}>{task.parent_id}</a>
          </span>
        )}
        <div className="ops">
          {task.status === 'paused' && !pending && (
            <Button type="primary" onClick={() => act('resume')}>
              恢复
            </Button>
          )}
          {(task.status === 'error' || task.status === 'failed') && !pending && (
            <Button type="primary" onClick={() => act('retry')}>
              重试
            </Button>
          )}
          {task.can_reinit && !pending && isDebeziumConnectorAbnormal(pipe) && (
            <Button onClick={() => setReinitOpen(true)}>重新初始化</Button>
          )}
        </div>
      </div>
      <CdcReinitModal open={reinitOpen} task={task} onClose={() => setReinitOpen(false)} onSubmitted={load} />

      {task.status === 'reinitializing' && (
        <Alert
          type="info"
          showIcon
          className="cdc-banner"
          message="正在重新初始化"
          description="后台正在按原配置补齐 Publication / Slot / Kafka Topic / Connector / StarRocks，完成后状态变为运行中；进度见「日志」页签（步骤：重新初始化）。"
        />
      )}

      {pending && (
        <Alert
          type="warning"
          showIcon
          className="cdc-banner"
          message="任务待审批"
          description={
            <span>
              审批通过后才会开始编排同步链路。
              {task.issue_id && (
                <Button type="link" onClick={() => navigate('/ops/tickets/' + task.issue_id)}>
                  查看工单 #{task.issue_id}
                </Button>
              )}
            </span>
          }
        />
      )}
      {(errorStep || extra.error_message) && task.status !== 'reinitializing' && (
        <Alert
          type="error"
          showIcon
          className="cdc-banner"
          message={errorStep === 'reinit' ? '重新初始化失败' : errorStep ? `任务异常（${errorStep}）` : '任务异常'}
          description={
            errorStep === 'reinit'
              ? `${extra.error_message || ''}${extra.error_message ? '。' : ''}可点击「重试」按默认参数（快照沿用原配置、恢复 Routine Load、沿用当前绑定）重新发起；需要换参数请点「重新初始化」。`
              : extra.error_message || '可在操作列点击「重试」从该失败步骤起点续跑。'
          }
        />
      )}
      {extra.auto_create_table === false && !task.target_instance_id && (
        <Alert
          type="info"
          showIcon
          className="cdc-banner"
          message="手动 DDL 模式，未配置目标端"
          description="该任务关闭了自动建表且未配置目标端 StarRocks，系统不会自动创建目标表与 Routine Load；请在目标库手动建表后自行配置导入。全链路状态中 StarRocks / Routine Load 节点的未就绪属预期。"
        />
      )}
      {extra.auto_create_table === false && task.target_instance_id && (
        <Alert type="info" showIcon className="cdc-banner" message="手动 DDL：本任务不会自动建表 / Routine Load" />
      )}

      {showApprovalBar && (
        <div className={`cdc-approval-bar ${isCreatorOnly ? 'is-creator' : 'is-approver'}`}>
          <div>
            <div className="bar-title">{approvalBarTitle}</div>
            <p className="bar-hint">{approvalBarHint}</p>
          </div>
          <div className="ops">
            {!isCreatorOnly && (
              <Button type="primary" loading={approving} onClick={() => setApproveOpen(true)}>
                通过
              </Button>
            )}
            <Button danger loading={approving} onClick={() => setRejectOpen(true)}>
              {isCreatorOnly ? '撤回' : '驳回'}
            </Button>
          </div>
        </div>
      )}

      {showSchemaBanner && (
        <Alert
          className="cdc-banner"
          type={schemaBannerType}
          showIcon
          message={schemaBannerTitle}
          description={
            <div>
              {schemaAlerts?.message && schemaAlerts.state !== 'OK' && <div>{schemaAlerts.message}</div>}
              {schemaAlerts?.drift_table_count ? (
                <div className="muted mt-1">共 {schemaAlerts.drift_table_count} 张目标表存在结构差异</div>
              ) : null}
              {schemaAlerts?.checked_at && <div className="muted">上次检查：{schemaAlerts.checked_at}</div>}
            </div>
          }
          action={
            <Space wrap size="small">
              <Button size="small" loading={schemaScanning} onClick={() => loadSchemaAlerts(true)}>
                立即检查
              </Button>
              <Button size="small" onClick={() => setTab('tables')}>
                去表映射
              </Button>
              <Button
                size="small"
                onClick={() => navigate(`/ops/cdc/create?type=alter_schema&parent_id=${task.id}`)}
              >
                源表加列向导
              </Button>
            </Space>
          }
        />
      )}

      <Tabs
        activeKey={tab}
        onChange={(k) => {
          setTab(k)
          if (k === 'logs') void loadLogs()
          if (k === 'tables') void ensureTablesLoaded()
          if (k === 'publication') {
            void ensurePublicationLoaded()
            void ensureTablesLoaded()
          }
          if (k === 'pipeline') void ensurePipelineLoaded()
        }}
        items={[
          { key: 'overview', label: '基本信息' },
          { key: 'pipeline', label: '全链路状态' },
          {
            key: 'tables',
            label: (
              <Badge count={tableDriftCount || 0} dot={schemaHasDrift && !tableDriftCount} size="small">
                <span>表映射明细</span>
              </Badge>
            ),
          },
          ...(isInit ? [{ key: 'publication', label: 'Publication 表' }] : []),
          { key: 'debezium', label: 'Debezium 配置' },
          { key: 'logs', label: '操作日志' },
        ]}
      />

      {tab === 'overview' && (
        <div className="overview-stack">
          <Card size="small" title="源端 / 目标端" className="cdc-card small">
            <Descriptions column={2} size="small">
              {descItems([
                { label: '源端 PostgreSQL', children: endpointLabel(source, extra.source_host, extra.source_port, true) },
                {
                  label: '目标端 StarRocks',
                  children: task.target_instance_id || extra.target_host
                    ? endpointLabel(target, extra.target_host, extra.target_port, true)
                    : '未配置',
                },
                { label: 'PG 数据库', children: sourceScopeLabel },
                { label: '目标数据库', children: task.target_db || '未配置' },
                { label: 'Schema', children: extra.source_schema || 'public' },
                { label: '环境', children: <Tag color={envTagColor(extra.env)}>{extra.env || '—'}</Tag> },
                { label: 'StarRocks 分桶数 BUCKETS', children: <strong>{extra.sr_auto_buckets || 10}</strong> },
                { label: 'StarRocks 表副本数', children: <strong>{extra.sr_replication_num || 1}</strong> },
              ])}
            </Descriptions>
          </Card>
          <Card size="small" title="同步配置" className="cdc-card small">
            <Descriptions column={2} size="small">
              {descItems([
                {
                  label: 'Debezium 模式',
                  children: (
                    <Tag color={extra.debezium_mode === 'before_after' ? 'warning' : 'default'}>
                      {extra.debezium_mode || 'after_only'}
                    </Tag>
                  ),
                },
                { label: '快照模式', children: extra.snapshot_mode || 'initial' },
                { label: '过滤删除事件', children: extra.filter_delete_events !== false ? '开启' : '关闭' },
                { label: '表模型', children: keyModelLabel(extra.table_model || extra.key_model) },
                {
                  label: '同步表数',
                  children: (
                    <span>
                      源端 <span className="c-src">{countView.source}</span>
                      <span className="muted"> · </span>目标 <span className="c-tgt">{countView.target}</span>
                      {countView.shard ? (
                        <>
                          <span className="muted"> · </span>分表 <span className="c-shard">{countView.shard}</span>
                        </>
                      ) : null}
                    </span>
                  ),
                },
                { label: '动态分区', children: extra.sr_dynamic_partition_enable ? '已开启' : '未开启' },
              ])}
            </Descriptions>
          </Card>
          <Card
            size="small"
            title={
              <div className="card-head">
                <span>Debezium 集群</span>
                <a onClick={() => setTab('debezium')}>完整配置详见「Debezium 配置」Tab</a>
              </div>
            }
            className="cdc-card small"
          >
            <Descriptions column={2} size="small">
              {descItems([
                { label: 'Connector ID', children: extra.connector_id || '—' },
                {
                  label: 'Worker URL',
                  children: workerURL ? (
                    <a href={workerURL} target="_blank" rel="noreferrer">
                      {workerURL}
                    </a>
                  ) : (
                    '—'
                  ),
                },
                {
                  label: 'Kafka 消息格式',
                  children: extra.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema',
                },
                { label: 'Kafka Bootstrap', children: <code>{extra.kafka_bootstrap_servers || '—'}</code> },
              ])}
            </Descriptions>
          </Card>
          <Card size="small" title="命名信息" className="cdc-card small">
            <Descriptions column={2} size="small">
              {descItems([
                { label: 'Publication', children: <code>{extra.publication_name || '—'}</code> },
                { label: 'Slot', children: <code>{extra.slot_name || '—'}</code> },
                { label: 'Topic 前缀', span: 2, children: <code>{extra.topic_prefix || '—'}</code> },
              ])}
            </Descriptions>
          </Card>
          <Card size="small" title="任务信息" className="cdc-card small">
            <Descriptions column={2} size="small">
              {descItems([
                { label: '任务类型', children: typeText(task.task_type) },
                {
                  label: '关联任务',
                  children: task.parent_id ? (
                    <a onClick={() => navigate('/ops/cdc/' + task.parent_id)}>{task.parent_id}</a>
                  ) : (
                    '—'
                  ),
                },
                { label: '任务描述', span: 2, children: note || '—' },
                { label: '创建人', children: createdBy || '—' },
                { label: '创建时间', children: task.created_at || '—' },
                { label: '审核人', children: assignedTo || '—' },
                {
                  label: '审批状态',
                  children: task.issue_id ? (
                    <a onClick={() => navigate('/ops/tickets/' + task.issue_id)}>{approvalText(task.approval_status)}</a>
                  ) : (
                    approvalText(task.approval_status)
                  ),
                },
              ])}
            </Descriptions>
          </Card>
        </div>
      )}

      {tab === 'pipeline' && (
        <Card size="small" className="cdc-card wide">
          <div className="pipeline-toolbar mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="font-medium text-sm">全链路状态</span>
              <span className="text-xs text-gray-500">
                {pending || ['running', 'init', 'creating', 'configuring', 'provisioning'].includes(task.status)
                  ? `自动刷新 8s · 上次更新 ${pipelineRefreshedAt || '—'}`
                  : `已停止自动刷新 · 上次更新 ${pipelineRefreshedAt || '—'}`}
              </span>
            </div>
            <Button size="small" loading={pipelineLoading} onClick={refreshPipeline}>
              手动刷新
            </Button>
          </div>
          {pipeline.healthy === false && (
            <Alert type="warning" showIcon className="mb" message={pipeline.unhealthy_reason || '链路未完全就绪'} />
          )}
          <PipelineVisualization nodes={pipelineNodes} onOpenConnector={openConnectorRuntime} />
          {(pipe.routine_loads || []).length > 0 && (
            <ResponsiveTable
              rowKey={(r: any) => r.name || r.table}
              size="small"
              className="mt"
              style={{ marginTop: 12 }}
              dataSource={pipe.routine_loads}
              pagination={false}
              locale={{ emptyText: '无 Routine Load' }}
              columns={[
                { title: '目标表', dataIndex: 'table' },
                { title: 'Routine Load', dataIndex: 'name' },
                { title: '状态', dataIndex: 'state', width: 140 },
              ]}
            />
          )}
        </Card>
      )}

      {tab === 'tables' && (
        <Card
          size="small"
          className="cdc-card wide"
          title={
            <div className="card-head">
              <div className="map-title">
                <span>共 {visibleTableRows.length} 组映射</span>
                {isInit && task.status === 'running' && (
                  <Switch
                    size="small"
                    checkedChildren="显示已移除"
                    unCheckedChildren="隐藏已移除"
                    checked={showDeletedMaps}
                    onChange={(v) => {
                      setShowDeletedMaps(v)
                      setTimeout(reloadTables, 0)
                    }}
                  />
                )}
              </div>
              <div className="map-tools">
                <Input
                  allowClear
                  size="small"
                  placeholder="搜索表"
                  style={{ width: 180 }}
                  value={tableQ}
                  onChange={(e) => setTableQ(e.target.value)}
                />
                {isInit && !pending && (
                  <Button size="small" loading={schemaScanning} onClick={() => loadSchemaAlerts(true)}>
                    检测结构差异
                  </Button>
                )}
                <Button size="small" onClick={reloadTables}>
                  刷新
                </Button>
              </div>
            </div>
          }
        >
          <ResponsiveTable
            className="cdc-map-table"
            rowKey={(r) => r.target_table || r.name}
            size="small"
            columns={mapColumns}
            dataSource={pagedTableRows}
            pagination={false}
            scroll={{ x: 1320 }}
            locale={{ emptyText: '暂无映射表' }}
          />
          <div className="cdc-pager">
            <Pagination
              current={tablePage}
              pageSize={tablePageSize}
              total={visibleTableRows.length}
              pageSizeOptions={['10', '20', '50']}
              showSizeChanger
              showTotal={(t) => `共 ${t} 条`}
              onChange={(p, s) => {
                setTablePage(p)
                setTablePageSize(s)
              }}
            />
          </div>
        </Card>
      )}

      {tab === 'publication' && (
        <Card size="small" className="cdc-card wide">
          <div className="card-head" style={{ marginBottom: 12 }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 12 }}>
              <span>
                Publication{' '}
                <code>{publication.publication_name || extra.publication_name || '—'}</code>
              </span>
              {(publication.slot_name || extra.slot_name) && (
                <span style={{ color: '#8c8c8c', fontSize: 12 }}>
                  Slot: <code>{publication.slot_name || extra.slot_name}</code>
                </span>
              )}
              <span style={{ color: '#8c8c8c', fontSize: 12 }}>
                已发布 {publishedTableNames.length} 张表
                {pubDisplayRows.length > 0 && pubDisplayRows.length < publishedTableNames.length
                  ? `（${pubDisplayRows.length} 组）`
                  : ''}
                {pubKeyword.trim()
                  ? ` · 显示 ${pubFilteredRows.length} / ${pubDisplayRows.length} 组`
                  : ''}
              </span>
            </div>
            <Space wrap>
              <Input.Search
                allowClear
                placeholder="搜索源表、目标表或分表模式"
                style={{ width: 220 }}
                value={pubKeyword}
                onChange={(e) => setPubKeyword(e.target.value)}
              />
              <Button
                onClick={() => {
                  void loadPublication()
                  void reloadTables()
                }}
              >
                刷新
              </Button>
              {!pending && (
                <Button type="primary" onClick={() => openPub('add')}>
                  添加表
                </Button>
              )}
              {!pending && (
                <Button danger onClick={() => openPub('remove')}>
                  移除表
                </Button>
              )}
            </Space>
          </div>
          {publication.message && <Alert type="warning" showIcon className="mb" message={publication.message} />}
          <ResponsiveTable
            rowKey="key"
            size="small"
            dataSource={pubFilteredRows}
            columns={pubColumns}
            pagination={{ pageSize: 15, showSizeChanger: false }}
            locale={{
              emptyText:
                pubDisplayRows.length > 0 && pubKeyword.trim()
                  ? '无匹配的已发布表'
                  : '暂无 Publication 表',
            }}
          />
        </Card>
      )}

      {tab === 'debezium' && (
        <Card size="small" className="cdc-card wide">
          {task.id && (
            <CdcDebeziumConfigTab
              taskId={task.id}
              extra={extra}
              pending={pending}
              onUpdated={load}
              onReinit={task.task_type === 'init' ? () => setReinitOpen(true) : undefined}
            />
          )}
        </Card>
      )}

      {tab === 'logs' && (
        <Card size="small" className="cdc-card">
          <div className="log-filters">
            <Select
              allowClear
              placeholder="步骤"
              style={{ width: 200 }}
              value={logStep || undefined}
              onChange={(v) => {
                setLogStep(v || '')
                setTimeout(() => loadLogs(1, logPageSize), 0)
              }}
              options={LOG_STEP_OPTIONS.map((s) => ({ label: s.label, value: s.value }))}
            />
            <Select
              allowClear
              placeholder="级别"
              style={{ width: 100 }}
              value={logLevel || undefined}
              onChange={(v) => {
                setLogLevel(v || '')
                setTimeout(() => loadLogs(1, logPageSize), 0)
              }}
              options={[
                { label: 'INFO', value: 'INFO' },
                { label: 'WARN', value: 'WARN' },
                { label: 'ERROR', value: 'ERROR' },
              ]}
            />
            <Input
              allowClear
              placeholder="搜索内容"
              style={{ width: 180 }}
              value={logQ}
              onChange={(e) => setLogQ(e.target.value)}
              onPressEnter={() => loadLogs(1, logPageSize)}
            />
            <Button type="primary" onClick={() => loadLogs(1, logPageSize)}>
              筛选
            </Button>
            <Button onClick={() => loadLogs()}>刷新</Button>
          </div>
          <ResponsiveTable
            rowKey={(r) => r.id || `${r.created_at || r.at}-${r.step}`}
            dataSource={logs}
            pagination={false}
            locale={{ emptyText: '暂无操作日志' }}
            columns={[
              { title: '时间', width: 180, render: (_, r) => formatLogTime(r.created_at || r.at) },
              {
                title: '级别',
                width: 80,
                render: (_, r) => {
                  const lv = r.level || r.log_level
                  return <Tag color={lv === 'ERROR' ? 'error' : lv === 'WARN' ? 'warning' : 'default'}>{lv}</Tag>
                },
              },
              { title: '步骤', width: 200, render: (_, r) => logStepText(r.step) },
              { title: '消息', dataIndex: 'message', ellipsis: true },
              {
                title: '详情',
                width: 80,
                render: (_, r) => (
                  <Button
                    type="link"
                    onClick={() => {
                      setLogDetail(r)
                      setLogDetailOpen(true)
                    }}
                  >
                    查看
                  </Button>
                ),
              },
            ]}
          />
          <div className="log-pager">
            <Pagination
              current={logPage}
              pageSize={logPageSize}
              total={logTotal}
              pageSizeOptions={['20', '50', '100']}
              showSizeChanger
              showTotal={(t) => `共 ${t} 条`}
              onChange={(p, s) => {
                setLogPage(p)
                setLogPageSize(s)
                loadLogs(p, s)
              }}
            />
          </div>
        </Card>
      )}

      {/* 追加/移除表 */}
      <Modal
        title={pubMode === 'add' ? '追加同步表' : '移除同步表'}
        open={pubOpen}
        width={560}
        onCancel={() => setPubOpen(false)}
        okText="提交审批"
        cancelText="取消"
        confirmLoading={pubSubmitting}
        onOk={submitPub}
        destroyOnClose
      >
        {availablePubTables.length > 0 && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ marginBottom: 6 }}>{pubMode === 'add' ? '可选表' : '选择要移除的表'}</div>
            <Checkbox.Group
              value={pubSelected}
              onChange={(v) => setPubSelected(v as string[])}
              options={availablePubTables.map((t) => ({ label: t, value: t }))}
            />
          </div>
        )}
        <div style={{ marginBottom: 12 }}>
          <div style={{ marginBottom: 6 }}>{pubMode === 'add' ? '或手动输入表名（逗号分隔）' : '或手动输入表名（逗号分隔）'}</div>
          <Input.TextArea rows={3} placeholder="如 orders, users" value={pubTables} onChange={(e) => setPubTables(e.target.value)} />
        </div>
        <div>
          <div style={{ marginBottom: 6 }}>审核人</div>
          <Input placeholder="审批通过后执行" value={pubReviewer} onChange={(e) => setPubReviewer(e.target.value)} />
        </div>
      </Modal>

      {/* 源表加列 */}
      <Modal
        title="源表加列"
        open={diffOpen}
        width={720}
        onCancel={() => setDiffOpen(false)}
        destroyOnClose
        footer={[
          <Button key="cancel" onClick={() => setDiffOpen(false)}>
            取消
          </Button>,
          <Button key="preview" onClick={loadMigrateSQL}>
            预览迁移 SQL
          </Button>,
          <Button
            key="submit"
            type="primary"
            loading={diffSubmitting}
            disabled={!diffCols.some((c) => c.selected)}
            onClick={submitAlter}
          >
            提交加列审批
          </Button>,
        ]}
      >
        <Alert type="info" showIcon className="mb" message="对比 PG 有而 StarRocks 无的列；提交后走 alter_schema 审批。" />
        <ResponsiveTable
          rowKey="name"
          size="small"
          dataSource={diffCols}
          pagination={false}
          locale={{ emptyText: '无待加列或尚未对比' }}
          columns={[
            {
              width: 48,
              render: (_, row) => (
                <Checkbox
                  checked={row.selected}
                  onChange={(e) =>
                    setDiffCols((prev) => prev.map((c) => (c === row ? { ...c, selected: e.target.checked } : c)))
                  }
                />
              ),
            },
            { title: '列名', dataIndex: 'name' },
            { title: 'PG', dataIndex: 'pg_type', width: 140 },
            { title: 'SR', dataIndex: 'sr_type', width: 140 },
          ]}
        />
        {migrateSQL && <Input.TextArea rows={8} readOnly value={migrateSQL} className="cfg-pre mt" style={{ marginTop: 12 }} />}
        <div style={{ marginTop: 12 }}>
          <div style={{ marginBottom: 6 }}>重建 Routine Load 位点</div>
          <Radio.Group value={diffOffset} onChange={(e) => setDiffOffset(e.target.value)}>
            <Radio value="OFFSET_BEGINNING">OFFSET_BEGINNING</Radio>
            <Radio value="OFFSET_END">OFFSET_END</Radio>
          </Radio.Group>
        </div>
        <div style={{ marginTop: 12 }}>
          <div style={{ marginBottom: 6 }}>审核人</div>
          <Input placeholder="审批通过后执行" value={diffReviewer} onChange={(e) => setDiffReviewer(e.target.value)} />
        </div>
      </Modal>

      <Modal title={(ddlTarget || '映射') + ' DDL'} open={ddlOpen} width={720} footer={<Button onClick={() => setDdlOpen(false)}>关闭</Button>} onCancel={() => setDdlOpen(false)}>
        <pre className="cfg-pre">{ddlSQL || '暂无 DDL'}</pre>
      </Modal>

      <Drawer title="Debezium 运行状态" width={480} open={runtimeOpen} onClose={() => setRuntimeOpen(false)}>
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="Connector">{runtime.name || extra.connector_name || '—'}</Descriptions.Item>
          <Descriptions.Item label="状态">{runtime.state || runtime.worst_state || '—'}</Descriptions.Item>
          <Descriptions.Item label="Worker">{runtime.worker_id || workerURL || '—'}</Descriptions.Item>
        </Descriptions>
        {(runtime.tasks || []).length > 0 && (
          <ResponsiveTable
            rowKey="id"
            size="small"
            style={{ marginTop: 12 }}
            dataSource={runtime.tasks}
            pagination={false}
            columns={[
              { title: 'Task', dataIndex: 'id', width: 80 },
              { title: '状态', dataIndex: 'state' },
              { title: 'Worker', dataIndex: 'worker_id' },
            ]}
          />
        )}
      </Drawer>

      <Modal
        title={isDeleteApproval ? '确认通过删除' : '审批通过'}
        open={approveOpen}
        width={480}
        onCancel={() => setApproveOpen(false)}
        okText={isDeleteApproval ? '确认通过删除' : '确认通过'}
        cancelText="取消"
        confirmLoading={approving}
        onOk={approveTicket}
        destroyOnClose
      >
        <div style={{ marginBottom: 6 }}>审批意见（可选）</div>
        <Input.TextArea rows={3} placeholder="可留空" value={approveComment} onChange={(e) => setApproveComment(e.target.value)} />
      </Modal>

      <Modal title="日志详情" open={logDetailOpen} width={720} footer={null} onCancel={() => setLogDetailOpen(false)} destroyOnClose>
        <div className="log-detail-meta muted">
          {formatLogTime(logDetail.created_at || logDetail.at)} · {logStepText(logDetail.step)}
          <Tag
            color={
              (logDetail.level || logDetail.log_level) === 'ERROR'
                ? 'error'
                : (logDetail.level || logDetail.log_level) === 'WARN'
                ? 'warning'
                : 'default'
            }
          >
            {logDetail.level || logDetail.log_level}
          </Tag>
          {logDetail.operator && <span> · {logDetail.operator}</span>}
        </div>
        <p className="log-detail-msg">{logDetail.message}</p>
        <pre className="log-detail-pre">{logDetail.detail || '（无 detail）'}</pre>
      </Modal>

      <Modal
        title={isCreatorOnly ? '撤回待审批' : '审批驳回'}
        open={rejectOpen}
        width={480}
        onCancel={() => setRejectOpen(false)}
        okText={isCreatorOnly ? '确认撤回' : '确认驳回'}
        okButtonProps={{ danger: true }}
        cancelText="取消"
        confirmLoading={approving}
        onOk={rejectTicket}
        destroyOnClose
      >
        <div style={{ marginBottom: 6 }}>{isCreatorOnly ? '撤回原因' : '驳回意见'}</div>
        <Input.TextArea
          rows={4}
          placeholder={isCreatorOnly ? '请填写撤回原因/说明（必填）' : '必填'}
          value={rejectComment}
          onChange={(e) => setRejectComment(e.target.value)}
        />
      </Modal>
    </div>
  )
}
