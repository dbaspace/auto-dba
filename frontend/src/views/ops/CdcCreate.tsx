import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  useLocation,
  useNavigate,
  useSearchParams } from 'react-router-dom'
import { useAppDispatch, useAppSelector } from '../../store'
import { closeTab, openTab, tabKey } from '../../store/tabsSlice'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  Collapse,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popover,
  Radio,
  Row,
  Select,
  Steps,
  Switch,
  Tag,
  Tooltip,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { SearchOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import CdcTableMappingPanel from './CdcTableMappingPanel'
import type { Mapping } from './CdcTableMappingPanel'
import './cdc.css'
import './CdcCreate.css'
import {
  CDC_DYNAMIC_PARTITION_DEFAULTS,
  CDC_INFRA_DEFAULTS,
  KEY_MODELS,
  PAGE_HINTS,
  PAGE_TITLES,
  SR_DP_TIME_UNITS,
  buildSqlPreview,
  buildSrDynamicPartitionObject,
  countTasksSyncedTable,
  decodeCdcExtra,
  filterShardDatabases,
  formatDebeziumConfig,
  formatSourceScopeLabel,
  formatSrDynamicPartitionDisplay,
  groupShardTables,
  instEnv,
  instLabel,
  keyModelLabel,
  matchGlob,
  normalizeKafkaBootstrap,
  normalizeSyncTableMode,
  parentTablesOf,
  positiveInt,
  resolveEffectivePartitionColumn,
  resolveSourceScope,
  shardDbPrefixHint,
  snapshotLabel,
  syncTableModeLabel,
  syncTablesFromMappings,
  tablesSyncedInAllParents,
  validateAddTableParents,
  validatePartitionColumnsForMappings,
} from './cdc'
import {
  ADD_STEPS,
  ADD_STEPS_AUTO,
  ALTER_STEPS,
  INIT_STEPS,
  REMOVE_STEPS,
  applyInfraDefaults,
  emptyForm,
  envTagColor,
  extraPayload,
  instanceID,
  matchType,
  needParentType,
  newMapping as makeMapping,
  restoreMappings as restoreMappingsLogic,
  statusTagColor,
  statusTextLabel,
  stepsForType,
  submitBody as buildSubmitBody,
  typeTagColor,
  typeTextTag,
  validateDebeziumStep,
  validateInitStep0,
  validateTableStep,
  validateTargetStep,
} from './cdcCreateLogic'
import type { CdcForm, CdcMapping, InfraDefaults, PrecheckCheck, SqlPreviewItem } from './cdcCreateLogic'

const dpDefaults = CDC_DYNAMIC_PARTITION_DEFAULTS

export default function CdcCreate() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const [searchParams] = useSearchParams()

  const [step, setStep] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [prechecking, setPrechecking] = useState(false)
  const [precheckOk, setPrecheckOk] = useState(false)
  const [precheckMsg, setPrecheckMsg] = useState('')
  const [precheckChecks, setPrecheckChecks] = useState<PrecheckCheck[]>([])
  const [loadingDbs, setLoadingDbs] = useState(false)
  const [loadingTables, setLoadingTables] = useState(false)
  const [tableHint, setTableHint] = useState('')
  const [tableQ, setTableQ] = useState('')
  const [manualTables, setManualTables] = useState('')
  const [sqlPreviews, setSqlPreviews] = useState<SqlPreviewItem[]>([])
  const [debeziumPreview, setDebeziumPreview] = useState<any>(null)
  const [instances, setInstances] = useState<any[]>([])
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [connectors, setConnectors] = useState<any[]>([])
  const [parentTasks, setParentTasks] = useState<any[]>([])
  const [sourceDbs, setSourceDbs] = useState<string[]>([])
  const [tables, setTables] = useState<string[]>([])
  const [registeredDbs, setRegisteredDbs] = useState<any[]>([])
  const [defaultReviewers, setDefaultReviewers] = useState<string[]>([])
  const [mappings, setMappings] = useState<CdcMapping[]>([])
  const [groupShards, setGroupShards] = useState(true)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [shardOpen, setShardOpen] = useState(false)
  const [shardPattern, setShardPattern] = useState('')
  const [shardPatternType, setShardPatternType] = useState('glob')
  const [shardDbScope, setShardDbScope] = useState('all')
  const [shardTarget, setShardTarget] = useState('')
  const [parentMappingRows, setParentMappingRows] = useState<any[]>([])
  const [syncedByParent, setSyncedByParent] = useState<Record<string, string[]>>({})
  const [discoverLoading, setDiscoverLoading] = useState(false)
  const [discoveredTables, setDiscoveredTables] = useState<string[]>([])
  const [diffLoading, setDiffLoading] = useState(false)
  const [diffWarning, setDiffWarning] = useState('')
  const [alterDiffItems, setAlterDiffItems] = useState<any[]>([])
  const [migrateSQL, setMigrateSQL] = useState('')

  const [infraDefaults, setInfraDefaults] = useState<InfraDefaults>({ ...CDC_INFRA_DEFAULTS })
  const [form, setForm] = useState<CdcForm>(() => emptyForm())

  // ---- refs 镜像（供闭包/异步读取最新值） ----
  const formRef = useRef(form)
  formRef.current = form
  const mappingsRef = useRef(mappings)
  mappingsRef.current = mappings
  const stepRef = useRef(step)
  stepRef.current = step
  const sqlPreviewsRef = useRef(sqlPreviews)
  sqlPreviewsRef.current = sqlPreviews
  const debeziumPreviewRef = useRef(debeziumPreview)
  debeziumPreviewRef.current = debeziumPreview
  const instancesRef = useRef(instances)
  instancesRef.current = instances
  const sourceDbsRef = useRef(sourceDbs)
  sourceDbsRef.current = sourceDbs
  const tablesRef = useRef(tables)
  tablesRef.current = tables
  const parentTasksRef = useRef(parentTasks)
  parentTasksRef.current = parentTasks
  const parentMappingRowsRef = useRef(parentMappingRows)
  parentMappingRowsRef.current = parentMappingRows
  const alterDiffItemsRef = useRef(alterDiffItems)
  alterDiffItemsRef.current = alterDiffItems
  const infraDefaultsRef = useRef(infraDefaults)
  infraDefaultsRef.current = infraDefaults
  const precheckOkRef = useRef(precheckOk)
  precheckOkRef.current = precheckOk

  const didInit = useRef(false)
  const discoverTimer = useRef<number>(0)

  function patchForm(next: Partial<CdcForm>) {
    setForm((prev) => ({ ...prev, ...next }))
  }

  // ---- 派生量 ----
  const mode = form.task_type
  const pageTitle = PAGE_TITLES[form.task_type] || '创建 CDC 任务'
  const pageHint = PAGE_HINTS[form.task_type] || ''
  const needParent = needParentType(form.task_type)
  const steps = stepsForType(form.task_type, form.auto_create_table)
  const targetOptional = form.auto_create_table === false
  const isSqlStep =
    form.task_type === 'init'
      ? step === 5
      : form.task_type === 'add_table'
        ? form.auto_create_table && step === 3
        : false
  const isConfirmStep = step === steps.length - 1
  const isWideStep =
    (form.task_type === 'init' && step === 1) ||
    (form.task_type === 'add_table' && step === 1) ||
    (form.task_type === 'alter_schema' && step === 1)
  const nextDisabled = form.task_type === 'init' && step === 4 && !precheckOk

  const resolvedShardDbs = useMemo(() => {
    if (form.source_scope_mode !== 'multi_db') return form.source_db ? [form.source_db] : []
    return filterShardDatabases(sourceDbs, form.source_db_prefix)
  }, [form.source_scope_mode, form.source_db, form.source_db_prefix, sourceDbs])

  const shardBundles = useMemo(() => groupShardTables(tables), [tables])
  const visibleShardGroups = useMemo(() => {
    if (!groupShards) return []
    const q = tableQ.trim().toLowerCase()
    return shardBundles.groups.filter(
      (g) => !q || g.base.toLowerCase().includes(q) || g.members.some((n) => n.toLowerCase().includes(q)),
    )
  }, [groupShards, tableQ, shardBundles])
  const visibleSingleTables = useMemo(() => {
    const q = tableQ.trim().toLowerCase()
    const names = groupShards ? shardBundles.singles : tables
    return names.filter((t) => !q || t.toLowerCase().includes(q))
  }, [groupShards, tableQ, shardBundles, tables])

  const shardPreview = useMemo(() => {
    if (discoveredTables.length || discoverLoading) return discoveredTables
    if (shardPatternType === 'regex') return []
    return tables.filter((t) => matchGlob(shardPattern, t))
  }, [discoveredTables, discoverLoading, shardPatternType, tables, shardPattern])

  const sourceInst = instances.find((i) => String(i.id) === instanceID(form.source_instance_id))
  const targetInst = instances.find((i) => String(i.id) === instanceID(form.target_instance_id))
  const parentName = parentTasks.find((t) => t.id === form.parent_id)?.name || form.parent_id || '—'

  const selectedParents = useMemo(() => {
    const multi = form.task_type === 'add_table' || form.task_type === 'alter_schema'
    const ids = multi && form.parent_ids.length ? form.parent_ids : form.parent_id ? [form.parent_id] : []
    return parentTasks.filter((t) => ids.includes(t.id))
  }, [form.task_type, form.parent_ids, form.parent_id, parentTasks])

  const parentTables = useMemo(() => {
    const p = selectedParents[0] || parentTasks.find((t) => t.id === form.parent_id)
    if (!p) return []
    const fromSynced = syncedByParent[p.id]
    if (fromSynced?.length) return fromSynced
    return parentTablesOf(p)
  }, [selectedParents, parentTasks, form.parent_id, syncedByParent])

  const parentMappings = useMemo(() => {
    const live = (parentMappingRows || []).filter((m) => m.status !== 'deleted')
    if (live.length) return live
    const p = selectedParents[0] || parentTasks.find((t) => t.id === form.parent_id)
    const extra = decodeCdcExtra(p?.description).extra
    const maps = extra.mappings || []
    if (maps.length) return maps
    return parentTables.map((t) => ({ target_table: extra.table_map?.[t] || t, source_tables: [t] }))
  }, [parentMappingRows, selectedParents, parentTasks, form.parent_id, parentTables])

  const canAutoAlter = useMemo(() => {
    const p = selectedParents[0] || parentTasks.find((t) => t.id === form.parent_id)
    if (!p) return false
    const extra = decodeCdcExtra(p.description).extra
    return extra.auto_create_table !== false && !!(p.target_instance_id && p.target_db)
  }, [selectedParents, parentTasks, form.parent_id])

  const syncedInAllParents = useMemo(() => {
    const ids = selectedParents.map((p) => p.id)
    if (!ids.length) return new Set(tablesSyncedInAllParents(selectedParents))
    const maps = ids.map((id) => new Set((syncedByParent[id] || []).map((n) => String(n).toLowerCase())))
    if (maps.some((s) => !s.size) && !Object.keys(syncedByParent).length) {
      return new Set(tablesSyncedInAllParents(selectedParents))
    }
    const first = maps[0]
    const out: string[] = []
    for (const n of first) {
      if (maps.every((s) => s.has(n))) out.push(n)
    }
    return new Set(out)
  }, [selectedParents, syncedByParent])

  const scopePrefixHint =
    form.source_scope_mode === 'multi_db' ? shardDbPrefixHint(form.source_db, form.source_db_prefix) : ''
  const sourceScopeLabel = formatSourceScopeLabel(form.source_db, form.source_db_prefix, resolvedShardDbs)

  const alterDiffRows = useMemo(() => {
    const rows: any[] = []
    for (const it of alterDiffItems) {
      for (const c of it.columns_to_add || []) rows.push({ ...c, target_table: it.target_table })
    }
    return rows
  }, [alterDiffItems])

  const parentCandidates = useMemo(() => {
    return parentTasks.filter((t) => {
      if (t.task_type !== 'init') return false
      if (form.task_type === 'add_table' && t.status !== 'running') return false
      if (!['running', 'paused', 'init', 'creating', 'provisioning'].includes(t.status)) return false
      if (!form.env) return true
      const extra = decodeCdcExtra(t.description).extra
      const env = extra.env || instEnv(instances.find((i) => i.id === t.source_instance_id))
      return env === form.env
    })
  }, [parentTasks, form.task_type, form.env, instances])

  const sourceInstances = useMemo(() => {
    const eng = form.source_engine === 'mysql' ? 'mysql' : 'postgres'
    return instances.filter((i) => {
      if (eng === 'mysql') {
        if (!(matchType(i.db_type, 'mysql') || matchType(i.db_type, 'tidb') || matchType(i.db_type, 'oceanbase') || matchType(i.db_type, 'mariadb'))) {
          return false
        }
      } else if (!matchType(i.db_type, 'postgres')) {
        return false
      }
      return !form.env || instEnv(i) === form.env || !instEnv(i)
    })
  }, [instances, form.env, form.source_engine])
  const targetInstances = useMemo(() => {
    const list = instances.filter(
      (i) => matchType(i.db_type, 'starrocks') && (!form.env || instEnv(i) === form.env || !instEnv(i)),
    )
    return list.length
      ? list
      : instances.filter((i) => !matchType(i.db_type, 'postgres') && !matchType(i.db_type, 'kafka'))
  }, [instances, form.env])
  const filteredConnectors = useMemo(
    () =>
      connectors.filter(
        (c) =>
          (!form.env || c.env === form.env) &&
          (c.message_schema_mode || 'without_schema') === form.message_schema_mode,
      ),
    [connectors, form.env, form.message_schema_mode],
  )
  const selectedConnector = connectors.find((c) => c.id === form.connector_id)
  const connectorKafkaHost = normalizeKafkaBootstrap(selectedConnector?.kafka_host)

  const reviewerOptions = useMemo(() => {
    const set = new Set(defaultReviewers)
    for (const a of sourceInst?.approvers || []) if (a) set.add(a)
    const db = registeredDbs.find((d) => d.db_name === form.source_db)
    for (const a of db?.approvers || []) if (a) set.add(a)
    return [...set]
  }, [defaultReviewers, sourceInst, registeredDbs, form.source_db])

  const filteredTables = useMemo(() => {
    const q = tableQ.trim().toLowerCase()
    if (!q) return tables
    return tables.filter((t) => t.toLowerCase().includes(q))
  }, [tableQ, tables])

  const selectableCount = visibleShardGroups.length + visibleSingleTables.length
  const selectedSelectableCount =
    visibleShardGroups.filter((g) => isGroupSelected(g)).length +
    visibleSingleTables.filter((t) => isTableSelected(t)).length
  const allSelectableChecked = selectableCount > 0 && selectedSelectableCount === selectableCount
  const allSelectableIndeterminate = selectedSelectableCount > 0 && selectedSelectableCount < selectableCount

  const debeziumConfigText = formatDebeziumConfig(debeziumPreview?.config)

  // ---- payload deps 组装（供 precheck / preview / submit） ----
  function buildDeps() {
    return {
      form: formRef.current,
      mappings: mappingsRef.current,
      resolvedShardDbs:
        formRef.current.source_scope_mode !== 'multi_db'
          ? formRef.current.source_db
            ? [formRef.current.source_db]
            : []
          : filterShardDatabases(sourceDbsRef.current, formRef.current.source_db_prefix),
      alterDiffRows: (() => {
        const rows: any[] = []
        for (const it of alterDiffItemsRef.current) {
          for (const c of it.columns_to_add || []) rows.push({ ...c, target_table: it.target_table })
        }
        return rows
      })(),
      parentMappings: (() => {
        const live = (parentMappingRowsRef.current || []).filter((m) => m.status !== 'deleted')
        if (live.length) return live
        const ids = formRef.current.parent_ids.length
          ? formRef.current.parent_ids
          : formRef.current.parent_id
            ? [formRef.current.parent_id]
            : []
        const p = parentTasksRef.current.find((t) => ids.includes(t.id))
        const extra = decodeCdcExtra(p?.description).extra
        const maps = extra.mappings || []
        if (maps.length) return maps
        return parentTablesOf(p).map((t) => ({ target_table: extra.table_map?.[t] || t, source_tables: [t] }))
      })(),
      infraDefaults: infraDefaultsRef.current,
      debeziumPreview: debeziumPreviewRef.current,
      sqlPreviews: sqlPreviewsRef.current,
    }
  }
  function extraPayloadNow() {
    return extraPayload(buildDeps())
  }
  function submitBodyNow() {
    return buildSubmitBody(buildDeps())
  }

  // ---- 表选择/映射操作 ----
  function isTableSelected(name: string) {
    return mappings.some((m) => !m.is_shard_group && m.source_tables.includes(name))
  }
  function isGroupSelected(g: { members: string[] }) {
    const set = new Set(g.members)
    return mappings.some(
      (m) =>
        m.is_shard_group &&
        m.source_tables.length === g.members.length &&
        m.source_tables.every((t) => set.has(t)),
    )
  }
  function syncMappingTables(list: CdcMapping[]) {
    const synced = syncTablesFromMappings(list)
    patchForm({ tables: synced.tables, table_map: synced.table_map })
  }
  function applyMappings(list: CdcMapping[]) {
    setMappings(list)
    syncMappingTables(list)
  }
  function newMapping(opts: {
    source_tables: string[]
    target_table?: string
    is_shard_group?: boolean
    shard_pattern?: string
  }): CdcMapping {
    return makeMapping(opts, formRef.current.task_type, formRef.current.snapshot_mode)
  }

  function toggleSingle(name: string, checked: boolean) {
    let next: CdcMapping[]
    if (checked) {
      if (isTableSelected(name)) return
      next = mappings.filter((m) => !(m.is_shard_group && m.source_tables.includes(name)))
      next = [...next, newMapping({ source_tables: [name], target_table: name, is_shard_group: false })]
    } else {
      next = mappings.filter((m) => !(m.source_tables.length === 1 && m.source_tables[0] === name))
    }
    applyMappings(next)
  }

  function toggleGroup(g: { members: string[]; targetTable: string; pattern: string }, checked: boolean) {
    let next: CdcMapping[]
    if (checked) {
      next = mappings.filter((m) => !m.source_tables.some((t) => g.members.includes(t)))
      next = [
        ...next,
        newMapping({
          source_tables: g.members,
          target_table: g.targetTable,
          is_shard_group: true,
          shard_pattern: g.pattern,
        }),
      ]
    } else {
      next = mappings.filter((m) => !(m.is_shard_group && m.shard_pattern === g.pattern))
    }
    applyMappings(next)
  }

  function toggleAllSelectable(checked: boolean) {
    if (checked) {
      let next = mappings.slice()
      for (const g of visibleShardGroups) {
        const set = new Set(g.members)
        const already = next.some(
          (m) => m.is_shard_group && m.source_tables.length === g.members.length && m.source_tables.every((t) => set.has(t)),
        )
        if (already) continue
        next = next.filter((m) => !m.source_tables.some((t) => g.members.includes(t)))
        next = [
          ...next,
          newMapping({
            source_tables: g.members,
            target_table: g.targetTable,
            is_shard_group: true,
            shard_pattern: g.pattern,
          }),
        ]
      }
      for (const t of visibleSingleTables) {
        const already = next.some((m) => !m.is_shard_group && m.source_tables.includes(t))
        if (already) continue
        next = next.filter((m) => !(m.is_shard_group && m.source_tables.includes(t)))
        next = [...next, newMapping({ source_tables: [t], target_table: t, is_shard_group: false })]
      }
      applyMappings(next)
      return
    }
    const groups = new Set(visibleShardGroups.map((g) => g.pattern))
    const singles = new Set(visibleSingleTables)
    const next = mappings.filter((m) => {
      if (m.is_shard_group && groups.has(m.shard_pattern)) return false
      if (!m.is_shard_group && m.source_tables.length === 1 && singles.has(m.source_tables[0])) return false
      return true
    })
    applyMappings(next)
  }

  function toggleExpand(base: string) {
    setExpandedGroups((prev) => {
      const n = new Set(prev)
      if (n.has(base)) n.delete(base)
      else n.add(base)
      return n
    })
  }

  function mergeManualTables() {
    const extra = String(manualTables || '')
      .split(/[\s,，]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (!extra.length) return
    const set = new Set([...tables, ...form.tables, ...extra])
    setTables([...set])
    let next = mappings.slice()
    for (const name of extra) {
      if (next.some((m) => !m.is_shard_group && m.source_tables.includes(name))) continue
      next = next.filter((m) => !(m.is_shard_group && m.source_tables.includes(name)))
      next = [...next, newMapping({ source_tables: [name], target_table: name, is_shard_group: false })]
    }
    applyMappings(next)
  }

  function restoreMappings(raw: any) {
    const list = restoreMappingsLogic(raw, formRef.current.tables, formRef.current.table_map)
    applyMappings(list)
  }

  // ---- 分表匹配弹窗 ----
  function scheduleDiscover() {
    window.clearTimeout(discoverTimer.current)
    discoverTimer.current = window.setTimeout(runDiscover, 280)
  }
  async function runDiscover() {
    const pattern = shardPatternRef.current.trim()
    const f = formRef.current
    if (!pattern || !instanceID(f.source_instance_id)) {
      setDiscoveredTables([])
      return
    }
    const resolved =
      f.source_scope_mode !== 'multi_db'
        ? f.source_db
          ? [f.source_db]
          : []
        : filterShardDatabases(sourceDbsRef.current, f.source_db_prefix)
    const dbs =
      shardDbScopeRef.current === 'representative' || !resolved.length
        ? f.source_db
          ? [f.source_db]
          : []
        : resolved
    if (!dbs.length) {
      setDiscoveredTables(
        tablesRef.current.filter((t) => (shardPatternTypeRef.current === 'regex' ? false : matchGlob(pattern, t))),
      )
      return
    }
    setDiscoverLoading(true)
    try {
      const { data } = await api.post('/cdc-sync/discover/tables', {
        instance_id: instanceID(f.source_instance_id),
        db_name: f.source_db,
        db_names: dbs,
        schema: f.source_schema || 'public',
        pattern,
        pattern_type: shardPatternTypeRef.current,
      })
      setDiscoveredTables(data.data?.tables || [])
    } catch {
      setDiscoveredTables(
        shardPatternTypeRef.current === 'regex' ? [] : tablesRef.current.filter((t) => matchGlob(pattern, t)),
      )
    } finally {
      setDiscoverLoading(false)
    }
  }
  const shardPatternRef = useRef(shardPattern)
  shardPatternRef.current = shardPattern
  const shardPatternTypeRef = useRef(shardPatternType)
  shardPatternTypeRef.current = shardPatternType
  const shardDbScopeRef = useRef(shardDbScope)
  shardDbScopeRef.current = shardDbScope

  function confirmShard() {
    const members = shardPreview
    if (!members.length) return
    const target =
      shardTarget.trim() ||
      String(shardPattern)
        .replace(/\*+$/, '')
        .replace(/_+$/, '') ||
      members[0]
    let next = mappings.filter((m) => !m.source_tables.some((t) => members.includes(t)))
    next = [
      ...next,
      newMapping({
        source_tables: members,
        target_table: target,
        is_shard_group: true,
        shard_pattern: shardPattern.trim(),
      }),
    ]
    applyMappings(next)
    setShardOpen(false)
    setShardPattern('')
    setShardTarget('')
  }

  // ---- Debezium 模式切换（含 before_after 提示） ----
  function onDebeziumModeChange(v: string) {
    const needWarn = v === 'before_after' && form.debezium_mode !== 'before_after'
    patchForm({ debezium_mode: v })
    if (!needWarn) return
    Modal.info({
      title: '请确认 Before + After 模式',
      content: 'Before+After 模式会增加 Kafka 消息体积和 StarRocks 存储，请仅在审计/对比等明确需求下使用。',
      okText: '我知道了',
    })
  }

  function onNamingEdited() {
    setPrecheckOk(false)
    setPrecheckMsg('命名已修改，请重新检测')
  }

  // ---- 导航 ----
  function goList(query?: Record<string, any>) {
    const createKey = tabKey(location.pathname, location.search)
    const q =
      query && typeof query === 'object' && query.tab
        ? `?${new URLSearchParams(query as Record<string, string>).toString()}`
        : ''
    dispatch(openTab({ path: '/ops/cdc', menus, title: 'CDC 同步任务' }))
    dispatch(closeTab(createKey))
    navigate(`/ops/cdc${q}`)
  }

  function resetMode(type: string) {
    const keep = emptyForm(infraDefaultsRef.current)
    keep.task_type = PAGE_TITLES[type] ? type : 'init'
    if (keep.task_type === 'add_table') keep.auto_create_table = false
    setForm(keep)
    formRef.current = keep
    setStep(0)
    setPrecheckOk(false)
    setPrecheckMsg('')
    setPrecheckChecks([])
    setTables([])
    setSourceDbs([])
    setSqlPreviews([])
    setDebeziumPreview(null)
    setMappings([])
    setAlterDiffItems([])
    setDiffWarning('')
    setMigrateSQL('')
  }

  // ---- 环境/实例/库/表加载 ----
  function onEnvChange(v: string) {
    patchForm({
      env: v,
      source_instance_id: '',
      target_instance_id: '',
      source_db: '',
      connector_id: '',
      tables: [],
    })
    setSourceDbs([])
    setTables([])
    setMappings([])
  }

  async function onSourceInstChange(v: string) {
    patchForm({ source_instance_id: v, source_db: '', tables: [] })
    setTables([])
    setMappings([])
    await loadSourceDbs(v)
  }

  function onSourceDbChange(v: string) {
    patchForm({ source_db: v, tables: [] })
    setTables([])
    setMappings([])
    // pickDefaultReviewer 依赖 reviewerOptions（下一次渲染），此处交由 effect 处理
    syncResolvedShardDbs(v, formRef.current.source_scope_mode, formRef.current.source_db_prefix)
  }

  function onScopeModeChange(v: string) {
    const patch: Partial<CdcForm> = { source_scope_mode: v }
    if (v !== 'multi_db') {
      patch.source_db_prefix = ''
      patch.source_db_names = []
    }
    patchForm(patch)
    syncResolvedShardDbs(formRef.current.source_db, v, v !== 'multi_db' ? '' : formRef.current.source_db_prefix)
  }

  function syncResolvedShardDbs(db: string, mode: string, prefix: string) {
    const scope = resolveSourceScope({
      mode,
      representativeDb: db,
      dbPrefix: prefix,
      names: sourceDbsRef.current,
    })
    patchForm({ source_db_names: scope.resolved_db_names })
  }

  async function loadSourceDbs(instId?: string) {
    const id = instId ?? formRef.current.source_instance_id
    if (!id) {
      setSourceDbs([])
      setRegisteredDbs([])
      return
    }
    setLoadingDbs(true)
    try {
      const [live, reg] = await Promise.all([
        api.get(`/instances/${id}/databases`).catch(() => ({ data: { data: [] } })),
        api.get('/databases', { params: { instance_id: id } }).catch(() => ({ data: { data: [] } })),
      ])
      setRegisteredDbs(reg.data.data || [])
      let dbs = (live.data.data || [])
        .filter((d: any) => !d.system)
        .map((d: any) => d.name || d.db_name)
        .filter(Boolean)
      if (!dbs.length) dbs = (reg.data.data || []).map((d: any) => d.db_name).filter(Boolean)
      setSourceDbs(dbs)
      sourceDbsRef.current = dbs
      syncResolvedShardDbs(
        formRef.current.source_db,
        formRef.current.source_scope_mode,
        formRef.current.source_db_prefix,
      )
    } catch {
      setSourceDbs([])
      setRegisteredDbs([])
    } finally {
      setLoadingDbs(false)
    }
  }

  async function loadTables() {
    const f = formRef.current
    if (!f.source_instance_id || !f.source_db) {
      setTableHint('请先选择源端实例和数据库')
      return
    }
    setLoadingTables(true)
    setTableHint('')
    try {
      if (f.task_type === 'add_table' && f.parent_id) {
        const { data } = await api.get(`/cdc-sync/tasks/${f.parent_id}/add-table/available-tables`)
        setTables(data.data || [])
      } else {
        const { data } = await api.get(
          `/migration/instances/${f.source_instance_id}/databases/${encodeURIComponent(f.source_db)}/tables`,
        )
        const list = (data.data || [])
          .map((t: any) => (typeof t === 'string' ? t : t.table_name || t.name))
          .filter(Boolean)
        setTables(list)
        if (!list.length) setTableHint('未拉到表，可在下方手动补充表名')
      }
    } catch (e: any) {
      setTables([])
      setTableHint(e?.response?.data?.message || '无法连接实例拉表，可手动补充表名')
    } finally {
      setLoadingTables(false)
    }
  }

  // ---- 父任务选择 ----
  function onParentRowClick(row: any) {
    if (!row) return
    if (form.task_type === 'add_table' || form.task_type === 'alter_schema') {
      toggleParent(row, !form.parent_ids.includes(row.id))
    } else {
      onPickParent(row)
    }
  }

  function toggleParent(row: any, checked: boolean) {
    if (!row) return
    const nextSet = new Set(form.parent_ids)
    if (checked) nextSet.add(row.id)
    else nextSet.delete(row.id)
    const selected = parentTasks.filter((t) => nextSet.has(t.id))
    const err = validateAddTableParents(selected, (id: string) => instances.find((i) => i.id === id))
    if (checked && err) {
      message.warning(err)
      return
    }
    const ids = [...nextSet]
    patchForm({ parent_ids: ids, parent_id: ids[0] || '' })
    const ref = selected[0] || (checked ? row : null)
    if (ref) onPickParent(ref, ids)
  }

  function onPickParent(row: any, parentIdsOverride?: string[]) {
    if (!row) return
    const isMulti = form.task_type === 'add_table' || form.task_type === 'alter_schema'
    const extra = decodeCdcExtra(row.description).extra
    const suffix = form.task_type === 'alter_schema' ? '加列' : form.task_type === 'remove_table' ? '移除表' : '加表'
    const patch: Partial<CdcForm> = {
      parent_id: row.id,
      source_instance_id: row.source_instance_id,
      target_instance_id: row.target_instance_id,
      source_db: row.source_db,
      target_db: row.target_db,
      env: extra.env || form.env,
      source_schema: extra.source_schema || 'public',
      connector_id: extra.connector_id || '',
      debezium_mode: extra.debezium_mode || 'after_only',
      message_schema_mode: extra.message_schema_mode || 'without_schema',
      key_model: extra.key_model || 'unique_key',
      auto_create_table: form.task_type === 'add_table' ? false : extra.auto_create_table !== false,
      sr_dynamic_partition_enable: extra.sr_dynamic_partition_enable === true,
      sr_partition_column: extra.sr_partition_column || '',
      kafka_bootstrap_servers: extra.kafka_bootstrap_servers || '',
      routine_load_parallelism: positiveInt(extra.routine_load_parallelism, infraDefaults.routine_load_parallelism),
      kafka_topic_partitions: positiveInt(extra.kafka_topic_partitions, infraDefaults.kafka_topic_partitions),
      kafka_topic_replication_factor: positiveInt(
        extra.kafka_topic_replication_factor,
        infraDefaults.kafka_topic_replication_factor,
      ),
      sr_auto_buckets: positiveInt(extra.sr_auto_buckets, infraDefaults.sr_auto_buckets),
      sr_replication_num: positiveInt(extra.sr_replication_num, infraDefaults.sr_replication_num),
      publication_name: extra.publication_name || '',
      slot_name: extra.slot_name || '',
      connector_name: extra.connector_name || '',
      topic_prefix: extra.topic_prefix || '',
    }
    if (!form.name) patch.name = `${row.name}_${suffix}`
    if (isMulti) patch.parent_ids = parentIdsOverride && parentIdsOverride.length ? parentIdsOverride : [row.id]
    // canAutoAlter 需基于新 parent 计算
    const autoAlterOk = extra.auto_create_table !== false && !!(row.target_instance_id && row.target_db)
    if (!autoAlterOk && form.task_type === 'alter_schema') patch.alter_schema_exec_mode = 'sql_only'
    patchForm(patch)
    formRef.current = { ...formRef.current, ...patch }
    void loadSourceDbs(row.source_instance_id)
    void loadParentExtras(patch.parent_ids || (form.parent_ids.length ? form.parent_ids : [row.id]))
  }

  async function loadParentExtras(idsArg?: string[]) {
    const ids = idsArg && idsArg.length ? idsArg : formRef.current.parent_ids.length ? formRef.current.parent_ids : formRef.current.parent_id ? [formRef.current.parent_id] : []
    setParentMappingRows([])
    setSyncedByParent({})
    if (!ids.length) return
    try {
      const [maps, synced] = await Promise.all([
        api.get(`/cdc-sync/tasks/${ids[0]}/tables`).catch(() => ({ data: {} as any })),
        api.post('/cdc-sync/tasks/synced-tables/batch', { parent_ids: ids }).catch(() => ({ data: {} as any })),
      ])
      setParentMappingRows(maps.data.data || [])
      setSyncedByParent(synced.data.data || {})
    } catch {
      setParentMappingRows([])
      setSyncedByParent({})
    }
  }

  // ---- alter_schema 列级 diff ----
  function toggleAlterMapping(m: any) {
    const name = m.target_table
    const set = new Set(form.tables)
    if (set.has(name)) set.delete(name)
    else set.add(name)
    patchForm({ tables: [...set] })
    formRef.current = { ...formRef.current, tables: [...set] }
    void loadSchemaDiff()
  }

  async function loadSchemaDiff() {
    const f = formRef.current
    if (!f.parent_id || !f.tables.length) {
      setAlterDiffItems([])
      setDiffWarning('')
      return
    }
    setDiffLoading(true)
    try {
      const items: any[] = []
      const warns: string[] = []
      const maps = parentMappings.filter((x: any) => f.tables.includes(x.target_table))
      for (const m of maps) {
        // eslint-disable-next-line no-await-in-loop
        const { data } = await api.post(`/cdc-sync/tasks/${f.parent_id}/schema-diff`, {
          target_table: m.target_table,
          source_tables: m.source_tables || [m.target_table],
        })
        const row = (data.data?.items || [])[0] || { target_table: m.target_table, columns_to_add: [], warnings: [] }
        items.push(row)
        for (const w of row.warnings || []) warns.push(w)
        if (row.error) warns.push(row.error)
      }
      setAlterDiffItems(items)
      setDiffWarning([...new Set(warns)].join('；'))
    } catch (e: any) {
      setDiffWarning(e?.response?.data?.message || '列对比失败')
    } finally {
      setDiffLoading(false)
    }
  }

  function toggleAlterDiffRow(target_table: string, name: string, selected: boolean) {
    setAlterDiffItems((prev) =>
      prev.map((it) =>
        it.target_table === target_table
          ? {
              ...it,
              columns_to_add: (it.columns_to_add || []).map((c: any) =>
                c.name === name ? { ...c, selected } : c,
              ),
            }
          : it,
      ),
    )
  }

  // ---- SQL / Debezium 预览 ----
  async function refreshSql() {
    const local = buildSqlPreview(
      formRef.current,
      mappingsRef.current,
      formRef.current.kafka_bootstrap_servers || connectorKafkaHost,
    )
    setSqlPreviews(local)
    sqlPreviewsRef.current = local
    try {
      const { data } = await api.post('/cdc-sync/tasks/preview-target-sql', {
        ...submitBodyNow(),
        ...extraPayloadNow(),
      })
      const items = data.data?.items || []
      if (items.length) {
        const next = items.map((it: any) => ({
          table: it.table,
          source: it.source,
          ddl: it.ddl || '',
          load: it.load || '',
          skipped: it.skipped,
        }))
        setSqlPreviews(next)
        sqlPreviewsRef.current = next
      } else {
        const sql = data.data?.sql || ''
        if (sql) {
          const next = [{ table: formRef.current.target_db || 'target', source: formRef.current.tables.join(','), ddl: sql, load: '' }]
          setSqlPreviews(next)
          sqlPreviewsRef.current = next
        }
      }
    } catch {
      /* keep local preview */
    }
    return loadDebeziumPreview()
  }

  async function loadDebeziumPreview() {
    try {
      const { data } = await api.post('/cdc-sync/tasks/debezium-preview', {
        ...submitBodyNow(),
        ...extraPayloadNow(),
      })
      const preview = data.data || null
      setDebeziumPreview(preview)
      debeziumPreviewRef.current = preview
    } catch (e: any) {
      setDebeziumPreview(null)
      debeziumPreviewRef.current = null
      message.warning(e?.response?.data?.message || 'Debezium 配置预览失败')
    }
  }

  // ---- 草稿 ----
  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'cdc',
    label: 'CDC同步任务',
    path: '/ops/cdc/create',
    enabled: formRef.current.task_type === 'init',
    getValues: () => ({ form: { ...formRef.current, mappings: mappingsRef.current }, step: stepRef.current }),
    applyValues: (v: any) => {
      const d = v.form || v
      const nextForm = { ...emptyForm(infraDefaultsRef.current), ...d }
      setForm(nextForm)
      formRef.current = nextForm
      restoreMappings(d.mappings || nextForm.mappings)
      if (Number.isFinite(v.step)) {
        setStep(v.step)
        stepRef.current = v.step
      } else if (Number.isFinite(d.step)) {
        setStep(d.step)
        stepRef.current = d.step
      }
    },
  })

  // 表单/映射/步骤变化时自动存草稿（init）
  useEffect(() => {
    if (form.task_type === 'init') schedulePersist()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form, mappings, step])

  // ---- 预检 ----
  async function runPrecheck() {
    const f = formRef.current
    if (!instanceID(f.source_instance_id)) {
      message.warning('请先选择源端实例')
      return
    }
    if (f.auto_create_table && !instanceID(f.target_instance_id)) {
      message.warning('自动建表开启时必须选择目标端实例')
      return
    }
    setPrechecking(true)
    try {
      const { data } = await api.post('/cdc-sync/tasks/precheck', { ...submitBodyNow(), ...extraPayloadNow() })
      const payload = data.data || {}
      if (payload.sql_previews?.length) {
        const next = payload.sql_previews.map((it: any) => ({
          table: it.table,
          source: it.source,
          ddl: it.ddl || '',
          load: it.load || '',
          skipped: it.skipped,
        }))
        setSqlPreviews(next)
        sqlPreviewsRef.current = next
      }
      setPrecheckChecks(payload.checks || [])
      const ok = !!(data.ready ?? data.ok)
      setPrecheckOk(ok)
      precheckOkRef.current = ok
      const msg = data.message || (ok ? '预检通过' : '预检未通过')
      setPrecheckMsg(msg)
      const naming = payload.naming || {}
      const namingPatch: Partial<CdcForm> = {}
      if (naming.publication_name && !f.publication_name) namingPatch.publication_name = naming.publication_name
      if (naming.slot_name && !f.slot_name) namingPatch.slot_name = naming.slot_name
      if (naming.connector_name && !f.connector_name) namingPatch.connector_name = naming.connector_name
      if (naming.topic_prefix && !f.topic_prefix) namingPatch.topic_prefix = naming.topic_prefix
      if (Object.keys(namingPatch).length) patchForm(namingPatch)
      if (ok) message.success(msg)
      else message.warning(msg)
    } catch (e: any) {
      setPrecheckOk(false)
      precheckOkRef.current = false
      setPrecheckChecks([])
      const msg = e?.response?.data?.message || '预检失败'
      setPrecheckMsg(msg)
      message.warning(msg)
    } finally {
      setPrechecking(false)
    }
  }

  // ---- 下一步/上一步/提交 ----
  async function next() {
    const f = formRef.current
    if (f.task_type === 'init' && step === 0) {
      const err = validateInitStep0(f, resolvedShardDbs)
      if (err) {
        message.warning(err)
        return
      }
      await loadTables()
    }
    if (f.task_type === 'init' && step === 1) {
      syncMappingTables(mappingsRef.current)
      const err = validateTableStep(f, mappingsRef.current)
      if (err) {
        message.warning(err)
        return
      }
    }
    if (f.task_type === 'init' && step === 2) {
      const err = validateDebeziumStep(f)
      if (err) {
        message.warning(err)
        return
      }
    }
    if (f.task_type === 'init' && step === 3) {
      const err = validateTargetStep(f)
      if (err) {
        message.warning(err)
        return
      }
      const partErr = validatePartitionColumnsForMappings(
        f.sr_dynamic_partition_enable,
        f.auto_create_table,
        f.sr_partition_column,
        mappingsRef.current,
      )
      if (partErr) {
        message.warning(partErr)
        return
      }
    }
    if (f.task_type === 'init' && step === 4) {
      if (!precheckOkRef.current) {
        message.warning('检测未通过，请先点击「重新检测」')
        return
      }
    }
    if (needParent && step === 0) {
      if (f.task_type === 'add_table' && !f.parent_ids.length && !f.parent_id) {
        message.warning('请选择已有初始化任务')
        return
      }
      if (f.task_type !== 'add_table' && !f.parent_id) {
        message.warning('请选择已有初始化任务')
        return
      }
    }
    if (f.task_type === 'add_table' && step === 0) await loadTables()
    if (f.task_type === 'add_table' && step === 1) {
      syncMappingTables(mappingsRef.current)
      const err = validateTableStep(f, mappingsRef.current)
      if (err) {
        message.warning(err)
        return
      }
    }
    if (f.task_type === 'alter_schema' && step === 1) {
      if (!f.tables.length) {
        message.warning('请至少选择一张目标表')
        return
      }
      const rows: any[] = []
      for (const it of alterDiffItemsRef.current) for (const c of it.columns_to_add || []) rows.push(c)
      if (!rows.some((c) => c.selected)) {
        message.warning('请至少勾选一列待加列')
        return
      }
    }
    if (f.task_type === 'remove_table' && step === 1 && !f.tables.length) {
      message.warning('请至少选择一张要移除的表')
      return
    }
    if (f.task_type === 'alter_schema' && step === 2 && !f.pg_column_ack) {
      message.warning('请确认已在 PG 加列')
      return
    }
    const nextStep = Math.min(step + 1, steps.length - 1)
    setStep(nextStep)
    stepRef.current = nextStep
    const nextIsSql =
      f.task_type === 'init'
        ? nextStep === 5
        : f.task_type === 'add_table'
          ? f.auto_create_table && nextStep === 3
          : false
    const nextIsConfirm = nextStep === steps.length - 1
    if (f.task_type === 'init' && nextStep === 4) void runPrecheck()
    if (nextIsSql) await refreshSql()
    else if (nextIsConfirm) {
      await loadDebeziumPreview()
      if (f.task_type === 'alter_schema') await loadAlterMigrateSQL()
    }
  }

  async function loadAlterMigrateSQL() {
    const f = formRef.current
    if (!f.parent_id || !f.tables.length) {
      setMigrateSQL('')
      return
    }
    try {
      const rows: any[] = []
      for (const it of alterDiffItemsRef.current) for (const c of it.columns_to_add || []) rows.push({ ...c, target_table: it.target_table })
      const { data } = await api.post(`/cdc-sync/tasks/${f.parent_id}/schema-migrate-sql`, {
        kafka_default_offsets: f.kafka_default_offsets,
        items: f.tables.map((tgt) => ({
          target_table: tgt,
          columns: rows.filter((c) => c.selected && c.target_table === tgt),
        })),
      })
      setMigrateSQL(data.data?.sql || '')
    } catch {
      setMigrateSQL('')
    }
  }

  function prev() {
    const f = formRef.current
    if (f.task_type === 'init' && (step === 4 || step === 5)) {
      setPrecheckOk(false)
      precheckOkRef.current = false
      setPrecheckMsg('')
    }
    const nextStep = Math.max(step - 1, 0)
    setStep(nextStep)
    stepRef.current = nextStep
  }

  async function submit() {
    const f = formRef.current
    if (!f.name) {
      message.warning('请填写任务名称')
      return
    }
    if (!f.source_instance_id) {
      message.warning('源端实例不能为空')
      return
    }
    if (f.task_type === 'init' && f.auto_create_table && !f.target_instance_id) {
      message.warning('自动建表开启时必须选择目标端实例')
      return
    }
    if (needParent && !f.parent_id && !f.parent_ids.length) {
      message.warning('请选择父任务')
      return
    }
    if (!f.reviewer) {
      message.warning('请选择审核人')
      return
    }
    setSubmitting(true)
    try {
      const body = submitBodyNow()
      if (f.task_type === 'add_table' && f.parent_ids.length > 1) {
        const { data: pre } = await api
          .post('/cdc-sync/tasks/batch-add-table/precheck', { ...body, parent_ids: f.parent_ids })
          .catch(() => ({ data: {} as any }))
        if (pre.data?.failed) message.warning(`批量预检：${pre.data.failed} 个父任务未通过，仍将提交可通过的任务`)
      }
      if ((f.task_type === 'add_table' || f.task_type === 'alter_schema') && f.parent_ids.length > 1) {
        const path =
          f.task_type === 'alter_schema' ? '/cdc-sync/tasks/batch-alter-schema' : '/cdc-sync/tasks/batch-add-table'
        const { data } = await api.post(path, { ...body, parent_ids: f.parent_ids })
        clearOnSubmitSuccess()
        const created = data.data?.created || 0
        const skipped = data.data?.skipped || 0
        const label = f.task_type === 'alter_schema' ? '加列' : '加表'
        message.success(`已提交 ${created} 条${label}任务${skipped ? `，跳过 ${skipped} 个` : ''}`)
        goList({ tab: 'pending' })
        return
      }
      await api.post('/cdc-sync/tasks', body)
      clearOnSubmitSuccess()
      message.success('已提交审批，通过后自动执行')
      goList({ tab: 'pending' })
    } finally {
      setSubmitting(false)
    }
  }

  // ---- 默认审核人 ----
  useEffect(() => {
    if (form.reviewer && reviewerOptions.includes(form.reviewer)) return
    const first = reviewerOptions[0]
    if (first && first !== form.reviewer) patchForm({ reviewer: first })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reviewerOptions])

  // ---- message_schema_mode 变化：清理不匹配的 connector ----
  useEffect(() => {
    if (form.connector_id && !filteredConnectors.some((c) => c.id === form.connector_id)) {
      patchForm({ connector_id: '' })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.message_schema_mode])

  // ---- 分表弹窗打开/类型切换：触发 discover ----
  useEffect(() => {
    if (shardOpen) scheduleDiscover()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shardOpen, shardPatternType])

  // ---- route type 变化：重置模式 ----
  useEffect(() => {
    if (!didInit.current) return
    const t = String(searchParams.get('type') || 'init')
    resetMode(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams.get('type')])

  // ---- 初始化加载 ----
  useEffect(() => {
    if (didInit.current) return
    didInit.current = true
    const type = String(searchParams.get('type') || 'init')
    resetMode(type)
    void (async () => {
      const [instRes, envRes, connRes, taskRes, argRes] = await Promise.all([
        api.get('/cdc-sync/instances').catch(() => api.get('/instances')),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
        api.get('/connectors').catch(() => ({ data: { data: [] } })),
        api
          .get('/cdc-sync/tasks/init-list')
          .catch(() => api.get('/cdc-sync/tasks', { params: { task_type: 'init' } })),
        api.get('/global-args').catch(() => ({ data: { data: [] } })),
      ])
      setInstances(instRes.data.data || [])
      instancesRef.current = instRes.data.data || []
      const envs = envRes.data.data || []
      let opts = envs
        .map((e: any) => ({ value: e.code || e.name, label: e.name ? `${e.name} (${e.code})` : e.code }))
        .filter((e: any) => e.value)
      if (!opts.length) {
        opts = [
          { value: 'test', label: '测试 (test)' },
          { value: 'prod', label: '生产 (prod)' },
        ]
      }
      setEnvOptions(opts)
      setConnectors(connRes.data.data || [])
      const inits = (taskRes.data.data || []).filter((t: any) => t.task_type === 'init')
      setParentTasks(inits)
      parentTasksRef.current = inits
      const args = argRes.data.data || []
      const infra = applyInfraDefaults(args)
      setInfraDefaults(infra)
      infraDefaultsRef.current = infra
      patchForm({
        routine_load_parallelism: infra.routine_load_parallelism,
        kafka_topic_partitions: infra.kafka_topic_partitions,
        kafka_topic_replication_factor: infra.kafka_topic_replication_factor,
        sr_auto_buckets: infra.sr_auto_buckets,
        sr_replication_num: infra.sr_replication_num,
      })
      const set = new Set<string>()
      String(args.find((a: any) => a.key === 'sync_default_approver')?.value || '')
        .split(/[,，\s]+/)
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((u) => set.add(u))
      setDefaultReviewers([...set])

      const preParent = String(searchParams.get('parent_id') || '').trim()
      if (preParent && inits.some((t: any) => t.id === preParent)) {
        const row = inits.find((t: any) => t.id === preParent)
        if (row) onPickParent(row)
      }

      if (formRef.current.task_type !== 'init') return
      if (!restoreDraft()) return
      if (formRef.current.source_instance_id) await loadSourceDbs()
      if (formRef.current.source_instance_id && formRef.current.source_db) await loadTables()
      if (stepRef.current >= 5) void refreshSql()
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ---- 映射面板 onChange 适配（React 版 panel 用 onChange） ----
  function onMappingsChange(next: Mapping[]) {
    applyMappings(next as CdcMapping[])
  }

  // ====================== 渲染 ======================
  return (
    <div className="cdc-shell cdc-create ops-page">
      <div className="cdc-back">
        <Button type="link" onClick={() => goList()}>
          ← 返回列表
        </Button>
      </div>
      <div className="cdc-title-row" style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 8 }}>
        <div className="cdc-title-left" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <h2 style={{ margin: 0 }}>{pageTitle}</h2>
          <Tag color={typeTagColor(form.task_type)}>{typeTextTag(form.task_type)}</Tag>
        </div>
        <div className="cdc-hint muted">{pageHint}</div>
      </div>

      <Steps className="cdc-steps" current={step} items={steps.map((s) => ({ title: s }))} style={{ marginBottom: 16 }} />

      <div className={`cdc-form${isWideStep ? ' cdc-form-wide' : ''}`}>
        {/* ===== init: 选择源端 ===== */}
        {mode === 'init' && step === 0 && (
          <>
            <Card size="small" className="cdc-card small" title={<span className="card-title">基本信息</span>}>
              <Form layout="vertical">
                <Form.Item label="任务名称" required>
                  <Input
                    maxLength={100}
                    showCount
                    placeholder="例：prod_order_db"
                    value={form.name}
                    onChange={(e) => patchForm({ name: e.target.value })}
                  />
                </Form.Item>
                <Form.Item label="任务描述">
                  <Input.TextArea
                    rows={2}
                    maxLength={300}
                    showCount
                    placeholder="可选，描述同步范围、负责人、关联工单等"
                    value={form.description}
                    onChange={(e) => patchForm({ description: e.target.value })}
                  />
                </Form.Item>
                <Form.Item label="环境" required>
                  <Select
                    showSearch
                    placeholder="请选择环境"
                    style={{ width: 220 }}
                    value={form.env || undefined}
                    onChange={onEnvChange}
                    options={envOptions}
                    optionFilterProp="label"
                  />
                </Form.Item>
              </Form>
            </Card>
            <Card
              size="small"
              className="cdc-card small"
              title={<span className="card-title">源端 {form.source_engine === 'mysql' ? 'MySQL' : 'PostgreSQL'}</span>}
            >
              <Form layout="vertical">
                <Form.Item label="源引擎" required>
                  <Select
                    style={{ width: 220 }}
                    value={form.source_engine || 'postgres'}
                    onChange={(v) =>
                      patchForm({
                        source_engine: v,
                        source_instance_id: '',
                        source_db: '',
                        source_schema: v === 'mysql' ? '' : 'public',
                        mysql_server_id: form.mysql_server_id || '5400',
                      })
                    }
                    options={[
                      { value: 'postgres', label: 'PostgreSQL' },
                      { value: 'mysql', label: 'MySQL / TiDB / OceanBase' },
                    ]}
                  />
                </Form.Item>
                {form.source_engine === 'mysql' ? (
                  <Form.Item
                    label="Debezium server.id"
                    required
                    extra="须与源端 @@server_id 及其它同源 CDC 任务不冲突"
                  >
                    <Input
                      style={{ width: 220 }}
                      value={form.mysql_server_id}
                      onChange={(e) => patchForm({ mysql_server_id: e.target.value })}
                      placeholder="5400"
                    />
                  </Form.Item>
                ) : null}
                <Form.Item label="实例" required>
                  <Select
                    showSearch
                    disabled={!form.env}
                    placeholder={
                      form.env
                        ? form.source_engine === 'mysql'
                          ? '搜索 / 选择 MySQL 实例'
                          : '搜索 / 选择 PG 实例'
                        : '请先选择环境'
                    }
                    style={{ width: '100%' }}
                    value={form.source_instance_id || undefined}
                    onChange={onSourceInstChange}
                    optionFilterProp="label"
                    options={sourceInstances.map((i) => ({ value: String(i.id), label: instLabel(i) }))}
                  />
                </Form.Item>
                <Form.Item label="同步范围">
                  <Radio.Group
                    optionType="button"
                    value={form.source_scope_mode}
                    onChange={(e) => onScopeModeChange(e.target.value)}
                  >
                    <Radio.Button value="single_db">单库</Radio.Button>
                    <Radio.Button value="multi_db">多分库</Radio.Button>
                  </Radio.Group>
                  <div className="form-extra">
                    多分库：同一实例内同构分库一次 init，各库独立 Publication / Slot / Connector，共享 Topic 前缀。
                  </div>
                </Form.Item>
                <Form.Item label={form.source_scope_mode === 'multi_db' ? '代表库' : '数据库'} required>
                  <Select
                    showSearch
                    mode="tags"
                    maxCount={1}
                    disabled={!form.source_instance_id}
                    loading={loadingDbs}
                    placeholder={form.source_instance_id ? '选择库（可手动输入）' : '请先选择实例'}
                    style={{ width: 320 }}
                    value={form.source_db ? [form.source_db] : []}
                    onChange={(v: string[]) => onSourceDbChange(v[v.length - 1] || '')}
                    options={sourceDbs.map((d) => ({ value: d, label: d }))}
                  />
                  {form.source_scope_mode === 'multi_db' && (
                    <div className="form-extra">代表库用于探测表结构，同构分库字段应一致。</div>
                  )}
                </Form.Item>
                {form.source_scope_mode === 'multi_db' && (
                  <>
                    <Form.Item label="分库前缀">
                      <Input
                        allowClear
                        placeholder="如 db_order_master_"
                        style={{ width: 320 }}
                        value={form.source_db_prefix}
                        onChange={(e) => patchForm({ source_db_prefix: e.target.value })}
                        onBlur={() =>
                          syncResolvedShardDbs(form.source_db, form.source_scope_mode, form.source_db_prefix)
                        }
                      />
                    </Form.Item>
                    {scopePrefixHint && (
                      <Alert type="warning" showIcon className="mb" message={scopePrefixHint} />
                    )}
                    <Form.Item label="匹配到的分库">
                      {resolvedShardDbs.length ? (
                        <div className="shard-db-tags">
                          {resolvedShardDbs.map((n) => (
                            <Tag key={n} color={n === form.source_db ? 'blue' : 'default'}>
                              {n}
                            </Tag>
                          ))}
                        </div>
                      ) : (
                        <div className="form-extra warn">
                          填写前缀后将从实例库列表自动匹配；有效多分库需 ≥ 2 个库。
                        </div>
                      )}
                    </Form.Item>
                  </>
                )}
                {form.source_engine !== 'mysql' ? (
                  <Form.Item label="Schema" required>
                    <Select
                      showSearch
                      mode="tags"
                      maxCount={1}
                      placeholder="public"
                      style={{ width: 220 }}
                      value={form.source_schema ? [form.source_schema] : []}
                      onChange={(v: string[]) => patchForm({ source_schema: v[v.length - 1] || '' })}
                      options={[{ value: 'public', label: 'public' }]}
                    />
                  </Form.Item>
                ) : null}
              </Form>
            </Card>
          </>
        )}

        {/* ===== init: 选择表 ===== */}
        {mode === 'init' && step === 1 && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="多张 PG 分表 → 1 张 StarRocks 目标表"
              description="点击「分表匹配」批量归并；勾选单表则一对一映射。同一组分表只会写入一张 StarRocks 目标表。"
            />
            <Alert
              type="error"
              showIcon
              className="mb"
              message="请为每张映射配置 KEY 列"
              description="点击右侧映射卡片展开后，在「KEY 列」中可多选并调整字段顺序；KEY 及其顺序会决定自动建表时的数据分布（分桶/排序键）与后续 SQL 查询性能，请谨慎选择。"
            />
            {resolvedShardDbs.length > 1 && (
              <div className="scope-summary">
                <div>
                  <strong>当前范围（多分库）：</strong>
                  代表库 <code>{form.source_db || '—'}</code> · Schema <code>{form.source_schema || 'public'}</code>
                  {form.source_db_prefix && (
                    <>
                      {' '}
                      · 前缀 <code>{form.source_db_prefix}</code>
                    </>
                  )}{' '}
                  · 已匹配 {resolvedShardDbs.length} 个库
                </div>
                <div className="shard-db-tags mt">
                  {resolvedShardDbs.map((n) => (
                    <Tag key={n} color="blue">
                      {n}
                    </Tag>
                  ))}
                </div>
                <div className="form-extra">
                  逻辑表选一次即覆盖全部分库；字段以代表库为准，请确保各分库表结构一致。
                </div>
              </div>
            )}
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <Card
                  size="small"
                  className="cdc-card map-card"
                  title={
                    <div className="card-head">
                      <span className="card-title">源端表列表</span>
                      <Button type="link" onClick={() => setShardOpen(true)}>
                        分表匹配
                      </Button>
                    </div>
                  }
                >
                  <div className="tbl-bar">
                    <Input
                      allowClear
                      placeholder="搜索表名"
                      prefix={<SearchOutlined />}
                      style={{ flex: 1 }}
                      value={tableQ}
                      onChange={(e) => setTableQ(e.target.value)}
                    />
                    <Checkbox
                      checked={allSelectableChecked}
                      indeterminate={allSelectableIndeterminate}
                      onChange={(e) => toggleAllSelectable(e.target.checked)}
                    >
                      全选可选（{selectableCount}）
                    </Checkbox>
                    <Switch
                      checked={groupShards}
                      checkedChildren="分表归类"
                      unCheckedChildren="平铺"
                      onChange={setGroupShards}
                    />
                    <Button loading={loadingTables} onClick={loadTables}>
                      刷新
                    </Button>
                  </div>
                  {form.sr_dynamic_partition_enable && (
                    <Alert
                      type="info"
                      showIcon
                      className="mb"
                      message="已开启动态分区"
                      description="请在右侧映射中为每张表选择 DATE/DATETIME 分区列；留空则使用任务默认分区键。"
                    />
                  )}
                  {tableHint && <Alert type="info" showIcon className="mb" message={tableHint} />}
                  <div className="src-list">
                    {visibleShardGroups.map((g) => (
                      <div key={'g-' + g.base} className="src-row">
                        <Checkbox checked={isGroupSelected(g)} onChange={(e) => toggleGroup(g, e.target.checked)}>
                          <span className="src-name">{g.targetTable || g.base}</span>
                          <Tag color="warning">分表 {g.members.length}</Tag>
                        </Checkbox>
                        <Button type="link" className="expand-btn" onClick={() => toggleExpand(g.base)}>
                          {expandedGroups.has(g.base) ? '收起' : '展开'}
                        </Button>
                        {expandedGroups.has(g.base) && (
                          <div className="src-members">
                            {g.members.map((n) => (
                              <div key={n} className="muted">
                                {n}
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    ))}
                    {visibleSingleTables.map((t) => (
                      <div key={t} className="src-row">
                        <Checkbox checked={isTableSelected(t)} onChange={(e) => toggleSingle(t, e.target.checked)}>
                          {t}
                        </Checkbox>
                      </div>
                    ))}
                    {!visibleShardGroups.length && !visibleSingleTables.length && (
                      <div className="tree-empty">暂无表</div>
                    )}
                  </div>
                  <Form layout="vertical">
                    <Form.Item label="手动补充表名" className="mt">
                      <Input.TextArea
                        rows={2}
                        placeholder="连不上库时可手动填写，逗号或换行分隔"
                        value={manualTables}
                        onChange={(e) => setManualTables(e.target.value)}
                        onBlur={mergeManualTables}
                      />
                    </Form.Item>
                  </Form>
                </Card>
              </Col>
              <Col xs={24} md={12}>
                <Card
                  size="small"
                  className="cdc-card map-card"
                  title={
                    <div className="card-head">
                      <span className="card-title">已选同步映射</span>
                      <span className="muted">共 {mappings.length} 组</span>
                    </div>
                  }
                >
                  <CdcTableMappingPanel
                    mappings={mappings as Mapping[]}
                    onChange={onMappingsChange}
                    instanceId={form.source_instance_id}
                    dbName={form.source_db}
                    schema={form.source_schema || 'public'}
                  />
                </Card>
              </Col>
            </Row>
          </>
        )}

        {/* ===== init: Debezium ===== */}
        {mode === 'init' && step === 2 && (
          <Card size="small" className="cdc-card small" title={<span className="card-title">Debezium 配置</span>}>
            <Form layout="vertical">
              <Form.Item label="Kafka 消息格式" required>
                <Radio.Group
                  value={form.message_schema_mode}
                  onChange={(e) => patchForm({ message_schema_mode: e.target.value })}
                >
                  <Radio value="without_schema">不带 Schema</Radio>
                  <Radio value="with_schema">带 Schema</Radio>
                </Radio.Group>
                <div className="form-extra">先确认 Connect 集群是否带 Schema，再选择同类型的 Debezium 地址。</div>
              </Form.Item>
              <Form.Item label="Debezium 集群" required>
                <Select
                  showSearch
                  placeholder={form.env ? '选择当前环境的 Debezium 地址' : '请先选择环境'}
                  style={{ width: '100%' }}
                  value={form.connector_id || undefined}
                  onChange={(v) => patchForm({ connector_id: v })}
                  optionFilterProp="label"
                  options={filteredConnectors.map((c) => ({
                    value: c.id,
                    label: c.dbz_host,
                    kafka: normalizeKafkaBootstrap(c.kafka_host) || '—',
                  }))}
                  optionRender={(opt) => (
                    <div>
                      <div className="opt-main">{opt.data.label}</div>
                      <div className="opt-sub">Kafka：{opt.data.kafka}</div>
                    </div>
                  )}
                />
                {form.env && !filteredConnectors.length && (
                  <div className="form-extra warn">当前环境暂无匹配集群，请先在「配置 → 连接器」维护。</div>
                )}
              </Form.Item>
              {selectedConnector &&
                form.message_schema_mode === 'with_schema' &&
                form.debezium_mode === 'after_only' && (
                  <Alert
                    type="warning"
                    showIcon
                    className="mb"
                    message="Debezium 集群消息格式"
                    description="所选集群为带 Schema，当前任务模版为 after_only 扁平 JSON，请确认 Connect 集群与 StarRocks 消费端兼容。"
                  />
                )}
              {selectedConnector && (
                <Descriptions column={1} bordered size="small" className="mb">
                  <Descriptions.Item label="Debezium 地址">{selectedConnector.dbz_host || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Kafka 消息格式">
                    <Tag color={form.message_schema_mode === 'with_schema' ? 'blue' : 'default'}>
                      {form.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema'}
                    </Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="Kafka 地址（连接器配置）">
                    {connectorKafkaHost ? (
                      <code>{connectorKafkaHost}</code>
                    ) : (
                      <span className="muted">未配置（可在高级配置中手动覆盖）</span>
                    )}
                  </Descriptions.Item>
                </Descriptions>
              )}
              {selectedConnector && (
                <div className="form-extra mb">
                  创建任务时按连接器解析 Kafka；上表来自「配置 → 连接器」，无需额外选择。
                </div>
              )}
              <Form.Item label="Debezium 模式" required>
                <Radio.Group
                  className="mode-radios"
                  value={form.debezium_mode}
                  onChange={(e) => onDebeziumModeChange(e.target.value)}
                >
                  <div className="mode-opt">
                    <Radio value="after_only">
                      仅 After
                      <Tag color="blue" className="ml">
                        推荐
                      </Tag>
                    </Radio>
                    <div className="opt-sub">输出变更后的数据，适合大多数同步场景，消息更紧凑。</div>
                  </div>
                  <div className="mode-opt">
                    <Radio value="before_after">Before + After</Radio>
                    <div className="opt-sub">
                      输出变更前后完整数据，适合审计与对比；
                      <span className="warn-text">消息体积与存储成本会显著上升</span>。
                    </div>
                  </div>
                </Radio.Group>
              </Form.Item>
              <Form.Item
                label={
                  <span>
                    同步模式
                    <Tooltip title="对应 Debezium snapshot.mode：决定首次是否全量同步历史数据">
                      <span className="qmark">?</span>
                    </Tooltip>
                  </span>
                }
                required
              >
                <Radio.Group value={form.snapshot_mode} onChange={(e) => patchForm({ snapshot_mode: e.target.value })}>
                  <Radio value="initial">全量 + 增量</Radio>
                  <Radio value="never">增量</Radio>
                </Radio.Group>
                <div className="form-extra">
                  {form.snapshot_mode === 'never'
                    ? '增量：仅从当前位点开始消费新数据，不回拉历史。'
                    : '全量 + 增量：先同步表的历史全量数据，再进入增量同步。'}
                </div>
              </Form.Item>
              <Form.Item
                label={
                  <span>
                    过滤删除事件
                    <Tooltip title="开启后 Debezium 将过滤 DELETE 事件；关闭则保留删除事件同步。后端据此选择 Connector 配置模版。">
                      <span className="qmark">?</span>
                    </Tooltip>
                  </span>
                }
              >
                <Switch
                  checked={form.filter_delete_events}
                  checkedChildren="开"
                  unCheckedChildren="关"
                  onChange={(v) => patchForm({ filter_delete_events: v })}
                />
              </Form.Item>
              <Collapse
                className="cdc-adv"
                ghost
                items={[
                  {
                    key: 'advanced',
                    label: '高级配置',
                    children: (
                      <>
                        <Form.Item
                          label={
                            <span>
                              Kafka Bootstrap 覆盖
                              <Tooltip title="默认使用连接器表中 Kafka 地址，一般无需填写">
                                <span className="qmark">?</span>
                              </Tooltip>
                            </span>
                          }
                        >
                          <Input
                            placeholder="留空则使用连接器配置中的 Kafka 地址"
                            value={form.kafka_bootstrap_servers}
                            onChange={(e) => patchForm({ kafka_bootstrap_servers: e.target.value })}
                          />
                        </Form.Item>
                        <Form.Item label="Routine Load 并发数">
                          <InputNumber
                            min={1}
                            max={32}
                            style={{ width: '100%' }}
                            value={form.routine_load_parallelism}
                            onChange={(v) => patchForm({ routine_load_parallelism: Number(v) || 0 })}
                          />
                          <div className="form-extra">
                            当前后端仅接受该字段，实际并发由全局参数 sync_sr_routine_load_desired_concurrent
                            决定，填写暂不生效。
                          </div>
                        </Form.Item>
                        <div className="infra-title">Kafka / StarRocks 建表参数</div>
                        <div className="form-extra mb">
                          对应全局参数默认值；提交后覆盖任务级配置。未传或 ≤0 时后端仍用全局默认。
                        </div>
                        <Form.Item
                          label={
                            <span>
                              Kafka Topic 分区数
                              <Tooltip title="Debezium topic.creation.default.partitions">
                                <span className="qmark">?</span>
                              </Tooltip>
                            </span>
                          }
                        >
                          <InputNumber
                            min={1}
                            max={128}
                            style={{ width: '100%' }}
                            value={form.kafka_topic_partitions}
                            onChange={(v) => patchForm({ kafka_topic_partitions: Number(v) || 0 })}
                          />
                        </Form.Item>
                        <Form.Item
                          label={
                            <span>
                              Kafka Topic 副本数
                              <Tooltip title="Debezium topic.creation.default.replication.factor">
                                <span className="qmark">?</span>
                              </Tooltip>
                            </span>
                          }
                        >
                          <InputNumber
                            min={1}
                            max={5}
                            style={{ width: '100%' }}
                            value={form.kafka_topic_replication_factor}
                            onChange={(v) => patchForm({ kafka_topic_replication_factor: Number(v) || 0 })}
                          />
                        </Form.Item>
                        <Form.Item
                          label={
                            <span>
                              StarRocks 分桶数 BUCKETS
                              <Tooltip title="自动建表 DDL 中 DISTRIBUTED BY HASH ... BUCKETS N">
                                <span className="qmark">?</span>
                              </Tooltip>
                            </span>
                          }
                        >
                          <InputNumber
                            min={1}
                            max={1024}
                            style={{ width: '100%' }}
                            value={form.sr_auto_buckets}
                            onChange={(v) => patchForm({ sr_auto_buckets: Number(v) || 0 })}
                          />
                        </Form.Item>
                        <Form.Item
                          label={
                            <span>
                              StarRocks 表副本数
                              <Tooltip title='建表 PROPERTIES("replication_num" = "N")'>
                                <span className="qmark">?</span>
                              </Tooltip>
                            </span>
                          }
                        >
                          <InputNumber
                            min={1}
                            max={5}
                            style={{ width: '100%' }}
                            value={form.sr_replication_num}
                            onChange={(v) => patchForm({ sr_replication_num: Number(v) || 0 })}
                          />
                        </Form.Item>
                      </>
                    ),
                  },
                ]}
              />
            </Form>
          </Card>
        )}

        {/* ===== init: 目标端 ===== */}
        {mode === 'init' && step === 3 && (
          <Card size="small" className="cdc-card small" title={<span className="card-title">目标端 StarRocks</span>}>
            <Form layout="vertical">
              <Form.Item label="自动建表">
                <Switch
                  checked={form.auto_create_table}
                  checkedChildren="开"
                  unCheckedChildren="关"
                  onChange={(v) => patchForm({ auto_create_table: v })}
                />
              </Form.Item>
              <Form.Item
                label={
                  <span>
                    实例
                    {targetOptional && <span className="muted label-opt">（选填）</span>}
                  </span>
                }
                required={!targetOptional}
              >
                <Select
                  showSearch
                  allowClear
                  placeholder="搜索 / 选择 StarRocks 实例"
                  style={{ width: '100%' }}
                  value={form.target_instance_id || undefined}
                  onChange={(v) => patchForm({ target_instance_id: v || '' })}
                  optionFilterProp="label"
                  options={targetInstances.map((i) => ({ value: String(i.id), label: instLabel(i) }))}
                />
                {targetOptional && <div className="form-extra">手动 DDL 模式：可不填目标端，后续自行建表</div>}
              </Form.Item>
              <Form.Item
                label={
                  <span>
                    目标数据库
                    {targetOptional && <span className="muted label-opt">（选填）</span>}
                  </span>
                }
                required={!targetOptional}
              >
                <Input
                  placeholder="例：cdc_prod_order"
                  value={form.target_db}
                  onChange={(e) => patchForm({ target_db: e.target.value })}
                />
              </Form.Item>
              <Form.Item label="表模型" required>
                <Radio.Group
                  className="key-models"
                  value={form.key_model}
                  onChange={(e) => patchForm({ key_model: e.target.value })}
                >
                  {KEY_MODELS.map((m) => (
                    <div key={m.value} className="key-opt">
                      <div className="key-opt-row">
                        <Radio value={m.value}>
                          {m.label}
                          {m.recommended && (
                            <Tag color="blue" className="ml">
                              推荐
                            </Tag>
                          )}
                        </Radio>
                        <Popover
                          placement="right"
                          trigger="click"
                          content={
                            <div className="key-pop" style={{ width: 320 }}>
                              <div>
                                <b>核心机制</b>
                                <div>{m.mechanism}</div>
                              </div>
                              <div className="mt">
                                <b>适用场景</b>
                                <div>{m.scenarios}</div>
                              </div>
                              <div className="mt">
                                <b>关键词</b>
                                <div className="key-tags">
                                  {m.keywords.map((k) => (
                                    <Tag key={k}>{k}</Tag>
                                  ))}
                                </div>
                              </div>
                              <div className="mt">
                                <b>KEY 列要求</b>
                                <div>{m.keyColumnHint}</div>
                              </div>
                            </div>
                          }
                        >
                          <Button type="link" size="small">
                            说明
                          </Button>
                        </Popover>
                      </div>
                      <div className="opt-sub">{m.hint}</div>
                    </div>
                  ))}
                </Radio.Group>
              </Form.Item>
              <div className="dp-block">
                <div className="infra-title">动态分区（StarRocks）</div>
                <Form.Item label="启用动态分区">
                  <Switch
                    checked={form.sr_dynamic_partition_enable}
                    disabled={targetOptional}
                    checkedChildren="开"
                    unCheckedChildren="关"
                    onChange={(v) => patchForm({ sr_dynamic_partition_enable: v })}
                  />
                  <div className="form-extra">
                    关闭时分区键与参数不会写入 DDL；仅创建任务时生效，创建后不可修改。
                  </div>
                </Form.Item>
                {form.sr_dynamic_partition_enable && (
                  <>
                    <Form.Item
                      label={
                        <span>
                          任务默认分区键
                          <Tooltip title="多表使用同一日期列时只填此项；单表不同列请在「选择表」步骤按表指定">
                            <span className="qmark">?</span>
                          </Tooltip>
                        </span>
                      }
                    >
                      <Input
                        placeholder="例：report_date、created_at"
                        value={form.sr_partition_column}
                        onChange={(e) => patchForm({ sr_partition_column: e.target.value })}
                      />
                    </Form.Item>
                    <div className="form-extra mb">
                      以下参数可选，未填则使用平台默认（time_unit=
                      {dpDefaults['dynamic_partition.time_unit']}，start=
                      {dpDefaults['dynamic_partition.start']}，end=
                      {dpDefaults['dynamic_partition.end']}，prefix=
                      {dpDefaults['dynamic_partition.prefix']}）
                    </div>
                    <Form.Item label="time_unit">
                      <Select
                        allowClear
                        placeholder={`默认 ${dpDefaults['dynamic_partition.time_unit']}`}
                        style={{ width: '100%' }}
                        value={form.sr_dp_time_unit || undefined}
                        onChange={(v) => patchForm({ sr_dp_time_unit: v || '' })}
                        options={SR_DP_TIME_UNITS.map((u) => ({ value: u, label: u }))}
                      />
                    </Form.Item>
                    <Form.Item label="start（保留历史分区数，负整数）">
                      <Input
                        placeholder={dpDefaults['dynamic_partition.start']}
                        value={form.sr_dp_start}
                        onChange={(e) => patchForm({ sr_dp_start: e.target.value })}
                      />
                    </Form.Item>
                    <Form.Item label="end（预创建未来分区数）">
                      <Input
                        placeholder={dpDefaults['dynamic_partition.end']}
                        value={form.sr_dp_end}
                        onChange={(e) => patchForm({ sr_dp_end: e.target.value })}
                      />
                    </Form.Item>
                    <Form.Item label="prefix">
                      <Input
                        placeholder={dpDefaults['dynamic_partition.prefix']}
                        value={form.sr_dp_prefix}
                        onChange={(e) => patchForm({ sr_dp_prefix: e.target.value })}
                      />
                    </Form.Item>
                    <Alert
                      type="info"
                      showIcon
                      message="分区列"
                      description="请在「选择表」步骤为每张映射选择分区列；留空则使用上方任务默认分区键。"
                    />
                  </>
                )}
              </div>
            </Form>
          </Card>
        )}

        {/* ===== init: 检测 ===== */}
        {mode === 'init' && step === 4 && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="创建前检测"
              description="将按当前向导配置调用预检接口，校验 PG / Debezium / Kafka / StarRocks 等前置条件。全部必选项（level=required）须为 passed 且 ready=true 后，方可进入「SQL 预览」。"
            />
            <Card
              size="small"
              className="cdc-card small"
              title={
                <div className="card-head">
                  <span className="card-title">检测结果</span>
                  <Button type="primary" loading={prechecking} onClick={runPrecheck}>
                    重新检测
                  </Button>
                </div>
              }
            >
              {precheckMsg && (
                <div className={`precheck-box ${precheckOk ? 'ok' : 'bad'}`}>{precheckMsg}</div>
              )}
              {precheckChecks.length > 0 && (
                <ResponsiveTable
                  className="mt"
                  size="small"
                  rowKey={(r: PrecheckCheck, i) => `${r.name || ''}_${i}`}
                  dataSource={precheckChecks}
                  pagination={false}
                  columns={
                    [
                      { title: '检查项', dataIndex: 'name', width: 160 },
                      { title: '级别', width: 90, render: (_: any, row: PrecheckCheck) => row.level || '—' },
                      {
                        title: '状态',
                        width: 90,
                        render: (_: any, row: PrecheckCheck) => (
                          <Tag
                            color={
                              row.status === 'passed' ? 'success' : row.status === 'failed' ? 'error' : 'warning'
                            }
                          >
                            {row.status}
                          </Tag>
                        ),
                      },
                      { title: '说明', dataIndex: 'message', ellipsis: true },
                    ] as ColumnsType<PrecheckCheck>
                  }
                />
              )}
            </Card>
            <Card size="small" className="cdc-card small" title={<span className="card-title">命名（将用于创建）</span>}>
              <Form layout="vertical">
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item label="Publication">
                      <Input
                        maxLength={128}
                        className="mono"
                        placeholder="例：test_db_dba（环境_库名）"
                        value={form.publication_name}
                        onChange={(e) => patchForm({ publication_name: e.target.value })}
                        onBlur={onNamingEdited}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="Slot">
                      <Input
                        maxLength={128}
                        className="mono"
                        placeholder="例：slot_test_db_dba_a1b2c3"
                        value={form.slot_name}
                        onChange={(e) => patchForm({ slot_name: e.target.value })}
                        onBlur={onNamingEdited}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="Connector">
                      <Input
                        maxLength={128}
                        className="mono"
                        placeholder="例：test_db_dba"
                        value={form.connector_name}
                        onChange={(e) => patchForm({ connector_name: e.target.value })}
                        onBlur={onNamingEdited}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="Topic 前缀">
                      <Input
                        maxLength={128}
                        className="mono"
                        placeholder="例：test_db_dba"
                        value={form.topic_prefix}
                        onChange={(e) => patchForm({ topic_prefix: e.target.value })}
                        onBlur={onNamingEdited}
                      />
                    </Form.Item>
                  </Col>
                </Row>
                <div className="form-extra">
                  未填写时 Publication / Connector / Topic 前缀默认为「环境_库名」；Slot 默认为
                  slot_环境_库名_6位随机hex。输入框中的值为预检与创建的提交依据；result.naming
                  仅作首次建议。重新检测按当前输入校验且不会重置为默认名。Topic 前缀不能包含英文句点。
                </div>
              </Form>
            </Card>
          </>
        )}

        {/* ===== init / add_table: SQL 预览 ===== */}
        {isSqlStep && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="目标端 SQL 预览（编排时将执行）"
              description="可直接改 DDL / Routine Load；提交时会带上 sql_overrides，审批通过后按你改过的 SQL 执行。若 Routine Load 的 kafka_topic 与当前 Topic 前缀不一致，后端会忽略该覆盖。"
            />
            {sqlPreviews.map((item, idx) => (
              <Card
                key={item.table || idx}
                size="small"
                className="cdc-card small"
                title={
                  <span className="card-title">
                    {item.source} → {item.table}
                  </span>
                }
              >
                <Form layout="vertical">
                  <Form.Item label="建表 DDL">
                    <Input.TextArea
                      rows={8}
                      className="sql-box"
                      value={item.ddl}
                      onChange={(e) => {
                        const next = sqlPreviews.map((x, i) => (i === idx ? { ...x, ddl: e.target.value } : x))
                        setSqlPreviews(next)
                        sqlPreviewsRef.current = next
                      }}
                    />
                  </Form.Item>
                  <Form.Item label="Routine Load">
                    <Input.TextArea
                      rows={6}
                      className="sql-box"
                      value={item.load}
                      onChange={(e) => {
                        const next = sqlPreviews.map((x, i) => (i === idx ? { ...x, load: e.target.value } : x))
                        setSqlPreviews(next)
                        sqlPreviewsRef.current = next
                      }}
                    />
                  </Form.Item>
                </Form>
              </Card>
            ))}
            {debeziumPreview && (
              <Card
                size="small"
                className="cdc-card small"
                title={
                  <span className="card-title">
                    Debezium Connector（{debeziumPreview.template || form.debezium_mode}）
                  </span>
                }
              >
                {(debeziumPreview.notes || []).map((n: string, i: number) => (
                  <Alert key={i} type="info" className="mb-sm" message={n} />
                ))}
                <Input.TextArea rows={18} readOnly className="sql-box" value={debeziumConfigText} />
              </Card>
            )}
          </>
        )}

        {/* ===== init / add_table / alter / remove: 确认 ===== */}
        {isConfirmStep && (
          <>
            <Alert
              type="success"
              showIcon
              className="mb"
              message="请确认配置后提交审批"
              description="初始化同步、新增同步表、源表加列、移除同步表创建后都须审批通过才会执行。可在列表「待审核」或工单中心查看。提交后超过 8 小时未审核完成，系统将自动取消。"
            />
            <Card size="small" className="cdc-card small" title={<span className="card-title">任务信息</span>}>
              <Descriptions column={2} bordered size="small">
                <Descriptions.Item label="任务名称">{form.name || '—'}</Descriptions.Item>
                <Descriptions.Item label="环境">
                  <Tag color={envTagColor(form.env)}>{form.env || '—'}</Tag>
                </Descriptions.Item>
                <Descriptions.Item label="任务描述" span={2}>
                  {form.description || '—'}
                </Descriptions.Item>
                {needParent && selectedParents.length > 1 ? (
                  <Descriptions.Item label="父任务" span={2}>
                    {selectedParents.map((p) => p.name).join('、')}
                  </Descriptions.Item>
                ) : needParent ? (
                  <Descriptions.Item label="父任务" span={2}>
                    {parentName}
                  </Descriptions.Item>
                ) : null}
                <Descriptions.Item label="审核人" span={2}>
                  <Select
                    showSearch
                    mode="tags"
                    maxCount={1}
                    placeholder="审批通过后执行"
                    style={{ width: 320 }}
                    value={form.reviewer ? [form.reviewer] : []}
                    onChange={(v: string[]) => patchForm({ reviewer: v[v.length - 1] || '' })}
                    options={reviewerOptions.map((a) => ({ value: a, label: a }))}
                  />
                </Descriptions.Item>
              </Descriptions>
            </Card>
            <Card size="small" className="cdc-card small" title={<span className="card-title">源端 / 目标端</span>}>
              <Descriptions column={2} bordered size="small">
                <Descriptions.Item label="PG 实例">{instLabel(sourceInst)}</Descriptions.Item>
                <Descriptions.Item label="StarRocks 实例">
                  {form.target_instance_id
                    ? instLabel(targetInst)
                    : form.auto_create_table
                      ? '—'
                      : '未填写（手动 DDL）'}
                </Descriptions.Item>
                <Descriptions.Item label="源库 / Schema">
                  {sourceScopeLabel} / {form.source_schema || 'public'}
                </Descriptions.Item>
                <Descriptions.Item label="目标库">
                  {form.target_db || (form.auto_create_table ? '—' : '未填写（手动 DDL）')}
                </Descriptions.Item>
                <Descriptions.Item label="自动建表">
                  {form.auto_create_table ? '是' : '否（手动 DDL）'}
                </Descriptions.Item>
                <Descriptions.Item label="表模型">{keyModelLabel(form.key_model)}</Descriptions.Item>
                <Descriptions.Item label="动态分区">
                  {form.sr_dynamic_partition_enable ? '已开启' : '未开启'}
                </Descriptions.Item>
                {form.sr_dynamic_partition_enable && (
                  <Descriptions.Item label="任务默认分区键">{form.sr_partition_column || '—'}</Descriptions.Item>
                )}
                {form.sr_dynamic_partition_enable && (
                  <Descriptions.Item label="dynamic_partition 参数" span={2}>
                    {formatSrDynamicPartitionDisplay(buildSrDynamicPartitionObject(form))}
                  </Descriptions.Item>
                )}
              </Descriptions>
            </Card>
            {mode === 'init' && (
              <Card size="small" className="cdc-card small" title={<span className="card-title">Debezium 配置</span>}>
                <Descriptions column={2} bordered size="small">
                  <Descriptions.Item label="Debezium 集群 ID">{form.connector_id || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Debezium 地址">{selectedConnector?.dbz_host || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Kafka 消息格式">
                    {form.message_schema_mode === 'with_schema' ? '带 Schema' : '不带 Schema'}
                  </Descriptions.Item>
                  <Descriptions.Item label="Kafka 地址" span={2}>
                    {form.kafka_bootstrap_servers
                      ? `${normalizeKafkaBootstrap(form.kafka_bootstrap_servers)}（覆盖）`
                      : connectorKafkaHost || '—（由连接器配置解析）'}
                  </Descriptions.Item>
                  <Descriptions.Item label="Debezium 模式">
                    {form.debezium_mode === 'before_after' ? 'Before + After' : '仅 After（推荐）'}
                  </Descriptions.Item>
                  <Descriptions.Item label="同步模式">{snapshotLabel(form.snapshot_mode)}</Descriptions.Item>
                  <Descriptions.Item label="过滤删除事件">
                    {form.filter_delete_events ? '开启' : '关闭'}
                  </Descriptions.Item>
                  <Descriptions.Item label="Routine Load 并发">
                    {form.routine_load_parallelism}{' '}
                    <span className="muted">（当前后端全局参数生效，此处仅作记录）</span>
                  </Descriptions.Item>
                  <Descriptions.Item label="Kafka Topic 分区数">{form.kafka_topic_partitions}</Descriptions.Item>
                  <Descriptions.Item label="Kafka Topic 副本数">
                    {form.kafka_topic_replication_factor}
                  </Descriptions.Item>
                  <Descriptions.Item label="StarRocks BUCKETS">{form.sr_auto_buckets}</Descriptions.Item>
                  <Descriptions.Item label="StarRocks 表副本数">{form.sr_replication_num}</Descriptions.Item>
                </Descriptions>
              </Card>
            )}
            {mode === 'init' && (
              <Card
                size="small"
                className="cdc-card small"
                title={
                  <div className="card-head">
                    <span className="card-title">命名预览（将用于创建）</span>
                    <span className="muted">与任务名称无关；修改请返回「检测」步骤</span>
                  </div>
                }
              >
                <Descriptions column={2} bordered size="small">
                  <Descriptions.Item label="Publication">{form.publication_name || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Slot">{form.slot_name || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Connector">{form.connector_name || '—'}</Descriptions.Item>
                  <Descriptions.Item label="Topic 前缀">{form.topic_prefix || '—'}</Descriptions.Item>
                </Descriptions>
              </Card>
            )}
            {mappings.length > 0 && (
              <Card
                size="small"
                className="cdc-card small"
                title={
                  <div className="card-head">
                    <span className="card-title">表映射摘要</span>
                    <span className="muted">
                      共 {mappings.length} 组（单表 {mappings.filter((m) => !m.is_shard_group).length} / 分表{' '}
                      {mappings.filter((m) => m.is_shard_group).length}）
                    </span>
                  </div>
                }
              >
                <div className="map-summary">
                  {mappings.map((m) => (
                    <div key={m.key} className="map-summary-row">
                      <Tag color={m.is_shard_group ? 'warning' : 'default'}>
                        {m.is_shard_group ? `分表 ${m.source_tables.length}` : '单表'}
                      </Tag>
                      <code>{m.is_shard_group ? m.shard_pattern || m.source_tables[0] : m.source_tables[0]}</code>
                      <span className="map-arrow">→</span>
                      <code>{m.target_table}</code>
                      <span className="muted">
                        KEY {m.key_columns?.length ? m.key_columns.join(', ') : '未配置'}
                      </span>
                      {m.shard_keys?.length ? (
                        <span className="muted">· 分桶键：{m.shard_keys.join(', ')}</span>
                      ) : null}
                      {form.sr_dynamic_partition_enable && (
                        <span className="muted">
                          · 生效分区列：
                          {resolveEffectivePartitionColumn(form.sr_partition_column, m.partition_column) || '—'}
                        </span>
                      )}
                      <span className="muted">· {syncTableModeLabel(m.sync_table_mode)}</span>
                    </div>
                  ))}
                </div>
              </Card>
            )}
            {mode === 'alter_schema' && migrateSQL && (
              <Card
                size="small"
                className="cdc-card small"
                title={<span className="card-title">迁移 SQL（PAUSE / STOP / ALTER / 重建 RL）</span>}
              >
                <Input.TextArea rows={10} readOnly className="sql-box" value={migrateSQL} />
              </Card>
            )}
            {mode === 'alter_schema' && (
              <Card size="small" className="cdc-card small" title={<span className="card-title">加列摘要</span>}>
                <Descriptions column={1} bordered size="small">
                  <Descriptions.Item label="执行">
                    {form.alter_schema_exec_mode === 'auto' ? '自动执行' : '仅生成 SQL'}
                  </Descriptions.Item>
                  {alterDiffItems.map((it) => (
                    <Descriptions.Item key={it.target_table} label={it.target_table}>
                      {(it.columns_to_add || [])
                        .filter((c: any) => c.selected)
                        .map((c: any) => `${c.name} ${c.sr_type}`)
                        .join('，') || '无待加列'}
                    </Descriptions.Item>
                  ))}
                </Descriptions>
              </Card>
            )}
            {mode === 'remove_table' && (
              <Card size="small" className="cdc-card small" title={<span className="card-title">将移除的表</span>}>
                <div className="shard-db-tags">
                  {form.tables.map((t) => (
                    <Tag key={t} color="warning">
                      {t}
                    </Tag>
                  ))}
                </div>
              </Card>
            )}
          </>
        )}

        {/* ===== add_table / alter / remove: 选择已有任务 ===== */}
        {needParent && step === 0 && (
          <Card
            size="small"
            className="cdc-card small"
            title={
              <span className="card-title">
                {form.task_type === 'add_table' || form.task_type === 'alter_schema'
                  ? '选择已有初始化任务（可多选同一源端）'
                  : '选择已有初始化任务'}
              </span>
            }
          >
            <Form layout="vertical">
              <Form.Item label="环境">
                <Select
                  allowClear
                  showSearch
                  placeholder="全部"
                  style={{ width: 220 }}
                  value={form.env || undefined}
                  onChange={(v) => patchForm({ env: v || '' })}
                  options={envOptions}
                  optionFilterProp="label"
                />
              </Form.Item>
            </Form>
            {(form.task_type === 'add_table' || form.task_type === 'alter_schema') && (
              <Alert
                type="info"
                showIcon
                className="mb"
                message="批量父任务"
                description={
                  form.task_type === 'alter_schema'
                    ? '可勾选同一源端实例下的多个 init 任务；提交后共享一张工单，每个父任务各生成一条加列子任务。'
                    : '可勾选同一源端实例下的多个 init 任务；提交后共享一张工单，每个父任务各生成一条加表子任务，已同步的表会自动跳过。'
                }
              />
            )}
            <ResponsiveTable
              size="small"
              rowKey="id"
              dataSource={parentCandidates}
              pagination={false}
              locale={{ emptyText: '暂无可用的初始化任务' }}
              onRow={(row) => ({ onClick: () => onParentRowClick(row) })}
              rowClassName={(row) =>
                form.parent_id === row.id || form.parent_ids.includes(row.id) ? 'ant-table-row-selected' : ''
              }
              columns={[
                {
                  title: '',
                  width: 48,
                  render: (_: any, row: any) =>
                    form.task_type === 'add_table' || form.task_type === 'alter_schema' ? (
                      <Checkbox
                        checked={form.parent_ids.includes(row.id)}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => toggleParent(row, e.target.checked)}
                      />
                    ) : (
                      <Radio
                        checked={form.parent_id === row.id}
                        onClick={(e) => e.stopPropagation()}
                        onChange={() => onPickParent(row)}
                      />
                    ),
                },
                { title: '任务名称', dataIndex: 'name', ellipsis: true, width: 180 },
                { title: '源端', ellipsis: true, render: (_: any, row: any) => row.source_db || '—' },
                { title: '目标端', ellipsis: true, render: (_: any, row: any) => row.target_db || '—' },
                {
                  title: '状态',
                  width: 100,
                  render: (_: any, row: any) => (
                    <Tag color={statusTagColor(row.status)}>{statusTextLabel(row.status)}</Tag>
                  ),
                },
              ]}
            />
          </Card>
        )}

        {/* ===== add_table: 选择新增表 ===== */}
        {mode === 'add_table' && step === 1 && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="多张 PG 分表 → 1 张 StarRocks 目标表"
              description="点击「分表匹配」批量归并；勾选单表则一对一映射。"
            />
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <Card
                  size="small"
                  className="cdc-card map-card"
                  title={
                    <div className="card-head">
                      <span className="card-title">源端表列表</span>
                      <Button type="link" onClick={() => setShardOpen(true)}>
                        分表匹配
                      </Button>
                    </div>
                  }
                >
                  <div className="tbl-bar">
                    <Input
                      allowClear
                      placeholder="搜索表名"
                      prefix={<SearchOutlined />}
                      style={{ flex: 1 }}
                      value={tableQ}
                      onChange={(e) => setTableQ(e.target.value)}
                    />
                    <Checkbox
                      checked={allSelectableChecked}
                      indeterminate={allSelectableIndeterminate}
                      onChange={(e) => toggleAllSelectable(e.target.checked)}
                    >
                      全选可选（{selectableCount}）
                    </Checkbox>
                    <Switch
                      checked={groupShards}
                      checkedChildren="分表归类"
                      unCheckedChildren="平铺"
                      onChange={setGroupShards}
                    />
                    <Button loading={loadingTables} onClick={loadTables}>
                      刷新
                    </Button>
                  </div>
                  <div className="src-list">
                    {visibleShardGroups.map((g) => (
                      <div key={'ag-' + g.base} className="src-row">
                        <Checkbox checked={isGroupSelected(g)} onChange={(e) => toggleGroup(g, e.target.checked)}>
                          <span className="src-name">{g.targetTable || g.base}</span>
                          <Tag color="warning">分表 {g.members.length}</Tag>
                        </Checkbox>
                      </div>
                    ))}
                    {visibleSingleTables.map((t) => {
                      const syncedAll = syncedInAllParents.has(t.toLowerCase())
                      const cnt = countTasksSyncedTable(t, selectedParents)
                      return (
                        <div key={'a-' + t} className="src-row">
                          <Checkbox
                            checked={isTableSelected(t)}
                            disabled={syncedAll}
                            onChange={(e) => toggleSingle(t, e.target.checked)}
                          >
                            {t}
                            {syncedAll ? (
                              <Tag>全部任务已同步</Tag>
                            ) : cnt ? (
                              <Tag color="blue">已在 {cnt} 个任务同步</Tag>
                            ) : null}
                          </Checkbox>
                        </div>
                      )
                    })}
                  </div>
                </Card>
              </Col>
              <Col xs={24} md={12}>
                <Card
                  size="small"
                  className="cdc-card map-card"
                  title={
                    <div className="card-head">
                      <span className="card-title">已选同步映射</span>
                      <span className="muted">共 {mappings.length} 组</span>
                    </div>
                  }
                >
                  <CdcTableMappingPanel
                    mappings={mappings as Mapping[]}
                    onChange={onMappingsChange}
                    instanceId={form.source_instance_id}
                    dbName={form.source_db}
                    schema={form.source_schema || 'public'}
                  />
                </Card>
              </Col>
            </Row>
          </>
        )}

        {/* ===== add_table: 配置目标端 ===== */}
        {mode === 'add_table' && step === 2 && (
          <Card size="small" className="cdc-card small" title={<span className="card-title">配置目标端</span>}>
            <Form layout="vertical">
              <Form.Item label="自动建表">
                <Switch
                  checked={form.auto_create_table}
                  checkedChildren="开"
                  unCheckedChildren="关"
                  onChange={(v) => patchForm({ auto_create_table: v })}
                />
              </Form.Item>
            </Form>
            <Descriptions column={2} bordered size="small">
              <Descriptions.Item label="继承目标实例">{instLabel(targetInst)}</Descriptions.Item>
              <Descriptions.Item label="目标库">{form.target_db || '—'}</Descriptions.Item>
              <Descriptions.Item label="表模型">{keyModelLabel(form.key_model)}</Descriptions.Item>
              <Descriptions.Item label="动态分区">
                {form.sr_dynamic_partition_enable ? '已开启（继承）' : '未开启'}
              </Descriptions.Item>
              {form.sr_dynamic_partition_enable && (
                <Descriptions.Item label="分区键">{form.sr_partition_column || '—'}</Descriptions.Item>
              )}
            </Descriptions>
          </Card>
        )}

        {/* ===== alter: 列级 diff ===== */}
        {mode === 'alter_schema' && step === 1 && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="列级差异"
              description="左侧选择目标映射，右侧对比「PG 有而 StarRocks 无」的非主键列。连不上目标端时仍会列出 PG 候选列。"
            />
            <Row gutter={16}>
              <Col xs={24} md={8}>
                <Card size="small" className="cdc-card map-card" title={<span className="card-title">目标映射</span>}>
                  <div className="src-list">
                    {parentMappings.map((m: any) => (
                      <div
                        key={m.target_table}
                        className={`src-row alter-map${form.tables.includes(m.target_table) ? ' active' : ''}`}
                        onClick={() => toggleAlterMapping(m)}
                      >
                        <Checkbox
                          checked={form.tables.includes(m.target_table)}
                          onClick={(e) => e.stopPropagation()}
                          onChange={() => toggleAlterMapping(m)}
                        >
                          <span className="src-name">{m.target_table}</span>
                          <div className="muted">{(m.source_tables || []).join(', ')}</div>
                        </Checkbox>
                      </div>
                    ))}
                    {!parentMappings.length && <div className="tree-empty">父任务暂无映射</div>}
                  </div>
                </Card>
              </Col>
              <Col xs={24} md={16}>
                <Card
                  size="small"
                  className="cdc-card map-card"
                  title={
                    <div className="card-head">
                      <span className="card-title">待加列</span>
                      <Button type="link" loading={diffLoading} onClick={loadSchemaDiff}>
                        重新对比
                      </Button>
                    </div>
                  }
                >
                  {diffWarning && <Alert type="warning" showIcon className="mb" message={diffWarning} />}
                  <ResponsiveTable
                    size="small"
                    rowKey={(r: any) => `${r.target_table}_${r.name}`}
                    dataSource={alterDiffRows}
                    pagination={false}
                    locale={{ emptyText: '请选择映射后自动对比' }}
                    columns={[
                      {
                        title: '',
                        width: 48,
                        render: (_: any, row: any) => (
                          <Checkbox
                            checked={!!row.selected}
                            onChange={(e) => toggleAlterDiffRow(row.target_table, row.name, e.target.checked)}
                          />
                        ),
                      },
                      { title: '目标表', dataIndex: 'target_table' },
                      { title: '列名', dataIndex: 'name' },
                      { title: 'PG 类型', dataIndex: 'pg_type', width: 120 },
                      { title: 'SR 类型', dataIndex: 'sr_type', width: 140 },
                    ]}
                  />
                </Card>
              </Col>
            </Row>
          </>
        )}

        {/* ===== remove: 选择移除表 ===== */}
        {mode === 'remove_table' && step === 1 && (
          <>
            <Alert
              type="warning"
              showIcon
              className="mb"
              message="将从 Publication 移除所选表"
              description="审批通过后会 ALTER PUBLICATION DROP TABLE，并收缩 Connector table.include.list。不会删除 StarRocks 目标表。"
            />
            <Card
              size="small"
              className="cdc-card small"
              title={<span className="card-title">选择要移除的同步表</span>}
            >
              <Checkbox.Group
                className="tbl-list"
                value={form.tables}
                onChange={(v) => patchForm({ tables: v as string[] })}
              >
                {parentTables.map((t) => (
                  <Checkbox key={t} value={t}>
                    {t}
                  </Checkbox>
                ))}
              </Checkbox.Group>
            </Card>
          </>
        )}

        {/* ===== alter: 执行方式 ===== */}
        {mode === 'alter_schema' && step === 2 && (
          <>
            <Alert
              type="info"
              showIcon
              className="mb"
              message="PostgreSQL 前置"
              description="请确认已在 PostgreSQL 对所有相关源表执行 ADD COLUMN。本系统不会在源库加列。"
            />
            <Card size="small" className="cdc-card small">
              <Form layout="vertical">
                <Form.Item>
                  <Checkbox
                    checked={form.pg_column_ack}
                    onChange={(e) => patchForm({ pg_column_ack: e.target.checked })}
                  >
                    我已在 PostgreSQL 完成 ADD COLUMN
                  </Checkbox>
                </Form.Item>
                <Form.Item label="执行方式">
                  <Radio.Group
                    className="col-radios"
                    value={form.alter_schema_exec_mode}
                    onChange={(e) => patchForm({ alter_schema_exec_mode: e.target.value })}
                  >
                    <Radio value="sql_only">仅生成 SQL（下一步预览后手动执行）</Radio>
                    <Radio value="auto" disabled={!canAutoAlter}>
                      自动执行 alter_schema
                    </Radio>
                  </Radio.Group>
                  {!canAutoAlter && (
                    <div className="form-extra">父任务未开启自动建表或未配置目标端，只能生成 SQL。</div>
                  )}
                </Form.Item>
                {form.alter_schema_exec_mode === 'auto' && (
                  <Form.Item label="重建 Routine Load 位点">
                    <Radio.Group
                      value={form.kafka_default_offsets}
                      onChange={(e) => patchForm({ kafka_default_offsets: e.target.value })}
                    >
                      <Radio value="OFFSET_BEGINNING">OFFSET_BEGINNING（从头消费）</Radio>
                      <Radio value="OFFSET_END">OFFSET_END（从最新位点）</Radio>
                    </Radio.Group>
                  </Form.Item>
                )}
              </Form>
            </Card>
          </>
        )}
      </div>

      {/* ===== 分表匹配弹窗 ===== */}
      <Modal
        open={shardOpen}
        title="分表匹配"
        width={560}
        destroyOnClose
        onCancel={() => setShardOpen(false)}
        footer={[
          <Button key="cancel" onClick={() => setShardOpen(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" disabled={!shardPreview.length} onClick={confirmShard}>
            加入映射
          </Button>,
        ]}
      >
        <Alert
          type="info"
          showIcon
          className="mb"
          message="多张 PG 分表 → 1 张 StarRocks 目标表"
          description="匹配规则将归并为同一映射组写入一张目标表。"
        />
        <Form layout="vertical">
          <Form.Item label="匹配类型">
            <Radio.Group value={shardPatternType} onChange={(e) => setShardPatternType(e.target.value)}>
              <Radio value="glob">glob（默认，尾部 * 按数字分表收窄）</Radio>
              <Radio value="regex">regex</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label={shardPatternType === 'regex' ? '匹配规则（regex）' : '匹配规则（glob）'}>
            <Input
              placeholder={shardPatternType === 'regex' ? '如 ^tv_app_market_[0-9]+$' : '如 tv_app_market_*'}
              value={shardPattern}
              onChange={(e) => {
                setShardPattern(e.target.value)
                scheduleDiscover()
              }}
            />
          </Form.Item>
          {resolvedShardDbs.length > 1 && (
            <Form.Item label="匹配范围">
              <Radio.Group
                value={shardDbScope}
                onChange={(e) => {
                  setShardDbScope(e.target.value)
                  scheduleDiscover()
                }}
              >
                <Radio value="all">全部分库</Radio>
                <Radio value="representative">仅代表库 {form.source_db}</Radio>
              </Radio.Group>
            </Form.Item>
          )}
          <Form.Item label="目标表名">
            <Input
              placeholder="默认同规则去掉 *"
              value={shardTarget}
              onChange={(e) => setShardTarget(e.target.value)}
            />
          </Form.Item>
          <div className="muted">
            {discoverLoading
              ? '正在匹配…'
              : `匹配到 ${shardPreview.length} 张：${shardPreview.slice(0, 8).join(', ')}${
                  shardPreview.length > 8 ? '…' : ''
                }`}
          </div>
        </Form>
      </Modal>

      {/* ===== 导航栏 ===== */}
      <div className={`cdc-nav${isWideStep ? ' cdc-nav-wide' : ''}`}>
        <Button onClick={() => goList()}>取消</Button>
        <div className="cdc-nav-right">
          {mode === 'init' && <Button onClick={() => saveDraft()}>保存草稿</Button>}
          {step > 0 && <Button onClick={prev}>上一步</Button>}
          {step < steps.length - 1 ? (
            <Button type="primary" disabled={nextDisabled} onClick={next}>
              下一步
            </Button>
          ) : mode === 'alter_schema' && form.alter_schema_exec_mode === 'sql_only' ? (
            <Button onClick={() => goList()}>返回列表</Button>
          ) : (
            <Button type="primary" loading={submitting} disabled={!form.reviewer} onClick={submit}>
              {mode === 'init' ? '确认创建' : '确认提交'}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
