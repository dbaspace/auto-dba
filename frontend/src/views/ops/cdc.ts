const META_PREFIX = '-- auto-dba-cdc:'
/** 与前端路由静态段冲突的 id（如 /ops/cdc/create） */
const CDC_RESERVED_IDS = new Set(['create'])

export function isValidCdcTaskID(id: any): boolean {
  const s = String(id || '').trim()
  if (!s || /[/?#]/.test(s)) return false
  return !CDC_RESERVED_IDS.has(s.toLowerCase())
}

export function normalizeKafkaBootstrap(s: any): string {
  return String(s || '')
    .split(',')
    .map((part) => {
      const p = part.trim()
      if (!p || p.startsWith('[')) return p
      const m = p.match(/^(.*):(\d+):(\d+)$/)
      if (m && m[2] === m[3]) return `${m[1]}:${m[2]}`
      return p
    })
    .filter(Boolean)
    .join(',')
}

export function kafkaInstanceAddress(i: any): string {
  const host = String(i?.host || i?.domain_name || '').trim()
  const port = Number(i?.port)
  if (!host) return ''
  const cleaned = normalizeKafkaBootstrap(host)
  if (/:\d+$/.test(cleaned) || cleaned.includes(',')) return cleaned
  if (Number.isFinite(port) && port > 0) return `${cleaned}:${port}`
  return cleaned
}

export const TYPE_LABELS: Record<string, string> = {
  init: '初始化',
  add_table: '新增表',
  remove_table: '移除表',
  alter_schema: '源表加列',
  delete_task: '删除任务',
}

export const PAGE_TITLES: Record<string, string> = {
  init: '初始化同步',
  add_table: '新增同步表',
  alter_schema: '源表加列',
  remove_table: '移除同步表',
}

export const PAGE_HINTS: Record<string, string> = {
  init: '首次为某个 PG 数据库创建完整同步链路',
  add_table: '在已有同步任务基础上追加新表（继承 Connector / Debezium 模式）',
  alter_schema: '源端表新增列后，同步到 StarRocks 目标表并更新 Routine Load',
  remove_table: '从已有初始化任务的 Publication 中移除表并收缩同步范围',
}

export interface CdcExtra {
  tables?: string[]
  mappings?: any[]
  publication_name?: string
  debezium_mode?: string
  env?: string
  created_by?: string
  source_db_prefix?: string
  source_db_names?: string[]
  [k: string]: any
}

const STRING_ARRAY_KEYS = ['tables', 'source_db_names', 'parent_ids'] as const
const OBJECT_KEYS = ['mappings', 'table_map', 'sql_overrides', 'sr_dynamic_partition', 'alter_schema_tables'] as const
const BOOL_KEYS = ['auto_create_table', 'filter_delete_events', 'sr_dynamic_partition_enable', 'shard_db'] as const
const NUMBER_KEYS = [
  'kafka_topic_partitions',
  'kafka_topic_replication_factor',
  'sr_auto_buckets',
  'sr_replication_num',
  'routine_load_parallelism',
] as const

function asStringArray(v: any): string[] {
  if (Array.isArray(v)) return v.map((x) => String(x ?? '').trim()).filter(Boolean)
  if (v == null || v === '') return []
  if (typeof v === 'string') {
    const s = v.trim()
    if (!s) return []
    if (s.startsWith('[')) {
      try {
        const parsed = JSON.parse(s)
        if (Array.isArray(parsed)) return asStringArray(parsed)
      } catch {
        /* fall through to CSV */
      }
    }
    return s
      .split(/[,，]/)
      .map((p) => p.trim())
      .filter(Boolean)
  }
  return []
}

function asParsedJSON(v: any, fallback: any): any {
  if (v == null || v === '') return fallback
  if (typeof v !== 'string') return v
  const s = v.trim()
  if (!s || s === 'null') return fallback
  try {
    return JSON.parse(s)
  } catch {
    return fallback
  }
}

function asBool(v: any): boolean | undefined {
  if (typeof v === 'boolean') return v
  if (v == null || v === '') return undefined
  if (typeof v === 'number') return v !== 0
  const s = String(v).trim().toLowerCase()
  if (s === 'true' || s === '1' || s === 'yes' || s === 'on') return true
  if (s === 'false' || s === '0' || s === 'no' || s === 'off') return false
  return undefined
}

function asNumber(v: any): number | undefined {
  if (typeof v === 'number' && Number.isFinite(v)) return v
  if (v == null || v === '') return undefined
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

/** 后端 saveExtra 把 extra 全写成 string；前端消费前在此统一还原类型 */
export function normalizeCdcExtra(raw: any): CdcExtra {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {}
  const out: CdcExtra = { ...raw }

  for (const k of STRING_ARRAY_KEYS) {
    if (k in out) out[k] = asStringArray(out[k])
  }

  for (const k of OBJECT_KEYS) {
    if (!(k in out)) continue
    const fallback = k === 'mappings' || k === 'alter_schema_tables' ? [] : {}
    const parsed = asParsedJSON(out[k], fallback)
    if (k === 'mappings' || k === 'alter_schema_tables') {
      out[k] = Array.isArray(parsed) ? parsed : fallback
    } else {
      out[k] = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : fallback
    }
  }

  for (const k of BOOL_KEYS) {
    if (!(k in out)) continue
    const b = asBool(out[k])
    if (b === undefined) delete out[k]
    else out[k] = b
  }

  for (const k of NUMBER_KEYS) {
    if (!(k in out)) continue
    const n = asNumber(out[k])
    if (n === undefined) delete out[k]
    else out[k] = n
  }

  return out
}

export function syncTableCountView(extra: CdcExtra) {
  const pre = (extra as any)?.list_table_counts
  if (pre && typeof pre === 'object') {
    const source = Number(pre.source)
    const target = Number(pre.target)
    const shard = Number(pre.shard)
    if (Number.isFinite(source) || Number.isFinite(target) || Number.isFinite(shard)) {
      return {
        source: Number.isFinite(source) ? source : 0,
        target: Number.isFinite(target) ? target : 0,
        shard: Number.isFinite(shard) ? shard : 0,
      }
    }
  }
  const tables = Array.isArray(extra?.tables) ? extra.tables : []
  const maps = Array.isArray(extra?.mappings) ? extra.mappings : []
  const shard = maps.filter((m: any) => m.is_shard_group || (m.source_tables || []).length > 1).length
  const source = tables.length || maps.reduce((n: number, m: any) => n + (m.source_tables || []).length, 0)
  return {
    source,
    target: maps.length || tables.length,
    shard,
  }
}

export function encodeCdcExtra(userDesc: string, extra: CdcExtra): string {
  const text = String(userDesc || '').trim()
  const blob = META_PREFIX + JSON.stringify(extra || {})
  return text ? `${text}\n${blob}` : blob
}

export function decodeCdcExtra(description: any): { text: string; extra: CdcExtra } {
  const raw = String(description || '')
  const idx = raw.indexOf(META_PREFIX)
  if (idx < 0) return { text: raw.trim(), extra: {} }
  const text = raw.slice(0, idx).trim()
  try {
    const extra = JSON.parse(raw.slice(idx + META_PREFIX.length) || '{}')
    return { text, extra: normalizeCdcExtra(extra && typeof extra === 'object' ? extra : {}) }
  } catch {
    return { text, extra: {} }
  }
}

export function typeText(t: string): string {
  return TYPE_LABELS[t] || t || '—'
}

// AntD Tag 颜色（对齐 universe-fe SyncTaskTypeTag）
export function typeTagColor(t: string): string {
  return (
    { init: 'blue', add_table: 'green', remove_table: 'orange', alter_schema: 'purple', delete_task: 'red' }[
      t
    ] || 'default'
  )
}

export function envTagColor(e: string): string {
  if (e === 'prod' || e === '生产') return 'red'
  if (e === 'test' || e === '测试') return 'green'
  return 'blue'
}

export function statusText(s: string): string {
  return (
    {
      init: '编排中',
      creating: '编排中',
      provisioning: '编排中',
      configuring: '编排中',
      running: '运行中',
      paused: '已暂停',
      pending: '待审批',
      error: '异常',
      failed: '失败',
      approved: '已通过',
      rejected: '已驳回',
      canceled: '已撤销',
      cancelled: '已撤销',
      reinitializing: '重新初始化中',
    }[s] || s || '—'
  )
}

// AntD Tag status 颜色
export function statusTagColor(s: string): string {
  if (s === 'running') return 'success'
  if (s === 'reinitializing') return 'processing'
  if (s === 'paused' || s === 'pending' || s === 'init' || s === 'creating' || s === 'provisioning') return 'warning'
  if (s === 'error' || s === 'failed' || s === 'rejected') return 'error'
  return 'default'
}

export function approvalText(s: string): string {
  return (
    { pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销', cancelled: '已撤销', none: '—' }[
      s
    ] || s || '—'
  )
}

export function instLabel(i: any): string {
  if (!i) return '—'
  const addr = i.host ? `${i.host}:${i.port}` : ''
  return i.name && addr ? `${i.name} — ${addr}` : i.name || addr || i.id
}

export function instAddr(i: any): string {
  if (!i) return '—'
  return i.host ? `${i.host}:${i.port}` : i.name || '—'
}

function extraEndpoint(host: any, port: any): string {
  const h = String(host || '').trim()
  const p = Number(port || 0)
  if (h && p > 0) return `${h}:${p}`
  return h
}

/** 实例对不上时仍展示任务里记下的主机端口。 */
export function endpointLabel(instance: any, host: any, port: any, withName = false): string {
  const raw = extraEndpoint(host, port)
  if (!instance) return raw || '—'
  const label = withName ? instLabel(instance) : instAddr(instance)
  if (!raw || label.includes(raw)) return label
  return `${label}（${raw}）`
}

export function instEnv(i: any): string {
  return i?.idc || i?.env || i?.env_code || ''
}

export function formatSourceScopeLabel(dbName: string, prefix: string, dbNames?: string[]): string {
  const names = dbNames || []
  if (names.length > 1) {
    const pre = String(prefix || '').trim()
    return `${pre ? `${pre}*` : names[0]}（${names.length} 库）`
  }
  return String(dbName || names[0] || '—')
}

export function debeziumLabel(v: string): string {
  return { after_only: 'after_only', before_after: 'before_after' }[v] || v || 'after_only'
}

export function currentUsername(): string {
  try {
    return String(JSON.parse(localStorage.getItem('user') || '{}').username || '').trim()
  } catch {
    return ''
  }
}

export function sameUser(a: any, b: any): boolean {
  const x = String(a || '').trim().toLowerCase()
  const y = String(b || '').trim().toLowerCase()
  return Boolean(x && x === y)
}

export function createdByOf(task: any, extra: CdcExtra): string {
  return String(extra?.created_by || task?.created_by || '').trim()
}

export function assignedToOf(task: any, extra: CdcExtra): string {
  return String(extra?.assigned_to || extra?.reviewer || extra?.approver || task?.assigned_to || '').trim()
}

export function pipelineBundle(status: any): any {
  return status?.pipeline && typeof status.pipeline === 'object' ? status.pipeline : status || {}
}

export function connectorWorkerURL(extra: CdcExtra, detail: any): string {
  const raw = String(
    detail?.connect_base_url || detail?.worker_url || extra?.connect_base_url || extra?.worker_url || '',
  ).trim()
  if (!raw) return ''
  return /^https?:\/\//i.test(raw) ? raw : `http://${raw}`
}

/** 全链路节点状态文案：Connector 在 Connect 上 404 显示「缺失」 */
export function pipelineStateText(state: any): string {
  const st = String(state || '').toUpperCase()
  if (st === 'NOT_FOUND') return '缺失'
  return String(state || '')
}

// PipelineVisualization 节点色调（CSS class）
export function pipelineNodeTone(state: any): string {
  const st = String(state || '').toUpperCase()
  if (['UNREACHABLE', 'FAILED', 'MISSING', 'DEGRADED', 'NOT_FOUND', 'ERROR'].includes(st)) return 'is-error'
  if (['READY', 'RUNNING', 'OK', 'SUCCESS'].includes(st)) return 'is-ok'
  if (['PAUSED', 'SKIPPED'].includes(st)) return 'is-warn'
  if (st === 'UNKNOWN' || st === '') return ''
  return 'is-run'
}

/** 手动 DDL：不自动建表 / Routine Load（与详情横幅一致，不论是否配置目标端） */
export function isCdcManualDDL(extraOrTask?: { auto_create_table?: any } | null): boolean {
  return extraOrTask?.auto_create_table === false
}

/**
 * Debezium Connector 是否异常（对齐老项目 isDebeziumConnectorAbnormal）。
 * 用于决定是否展示「重新初始化」：仅 FAILED / NOT_FOUND(404) / task 全挂时为 true；
 * 探测未完成（无 connector）返回 false，不展示按钮。
 */
export function isDebeziumConnectorAbnormal(
  pipelineOrConnector?: {
    connector?: { state?: string; tasks?: { state?: string }[] } | null
    state?: string
    tasks?: { state?: string }[]
  } | null,
): boolean {
  if (!pipelineOrConnector) return false
  const conn =
    pipelineOrConnector.connector !== undefined
      ? pipelineOrConnector.connector
      : (pipelineOrConnector as { state?: string; tasks?: { state?: string }[] })
  if (!conn) return false
  const st = String(conn.state || '').toUpperCase()
  if (st === 'FAILED' || st === 'NOT_FOUND' || st === 'ERROR') return true
  const tasks = conn.tasks || []
  if (st === 'RUNNING' && tasks.length) {
    const running = tasks.filter((t) => String(t.state || '').toUpperCase() === 'RUNNING').length
    if (running === 0) return true
  }
  return false
}

/**
 * 列表「异常」判定，对齐老项目 isPipelineUnhealthy：
 * - Kafka MISSING/PARTIAL 不降级为异常（仅 UNREACHABLE/FAILED/ERROR）
 * - 手动 DDL 豁免 SR/RL
 */
export function isPipelineUnhealthy(
  pipeline?: {
    connector?: { state?: string; tasks?: { state?: string }[] } | null
    kafka?: { state?: string } | null
    slot?: { active?: boolean; state?: string } | null
    starrocks?: { state?: string } | null
    routine_loads?: { state?: string }[] | null
    healthy?: boolean
  } | null,
  opts?: { auto_create_table?: any },
): boolean {
  if (!pipeline) return false
  const slot = pipeline.slot
  if (slot && slot.active === false) return true

  const conn = pipeline.connector
  if (conn) {
    const st = String(conn.state || '').toUpperCase()
    if (st === 'FAILED' || st === 'NOT_FOUND' || st === 'ERROR') return true
    const tasks = conn.tasks || []
    if (st === 'RUNNING' && tasks.length) {
      const running = tasks.filter((t) => String(t.state || '').toUpperCase() === 'RUNNING').length
      if (running === 0) return true
    }
  }

  const kafkaSt = String(pipeline.kafka?.state || '').toUpperCase()
  if (['UNREACHABLE', 'FAILED', 'ERROR'].includes(kafkaSt)) return true
  // MISSING / PARTIAL：不刷列表异常（与老项目一致）

  if (isCdcManualDDL(opts)) return false

  const srSt = String(pipeline.starrocks?.state || '').toUpperCase()
  if (['UNREACHABLE', 'FAILED', 'ERROR', 'DEGRADED'].includes(srSt)) return true

  const loads = pipeline.routine_loads || []
  if (loads.length) {
    const bad = loads.some((l) => {
      const s = String(l.state || '').toUpperCase()
      return s === 'STOPPED' || s === 'FAILED' || s === 'CANCELLED' || s === 'CANCELED'
    })
    if (bad) return true
  }
  return false
}

export function formatLagBytes(n: any): string {
  const b = Number(n)
  if (!Number.isFinite(b) || b < 0) return '—'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} kB`
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`
  return `${(b / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

export function mappingSourceText(src: string): string {
  return (
    { add_table: '新增表', remove_table: '移除表', alter_schema: '源表加列', add_column: '源表加列', init: '初始化' }[
      src
    ] || '初始化'
  )
}

export function mappingSourceTagColor(src: string): string {
  return (
    { add_table: 'green', remove_table: 'orange', alter_schema: 'blue', add_column: 'blue', init: 'default' }[src] ||
    'default'
  )
}

export const LOG_STEP_OPTIONS = [
  { value: 'create', label: '创建任务' },
  { value: 'orchestrate', label: '编排' },
  { value: 'pg', label: 'PG Publication/Slot' },
  { value: 'kafka', label: 'Kafka Topic' },
  { value: 'debezium', label: 'Debezium Connector' },
  { value: 'starrocks', label: 'StarRocks 建表/Routine Load' },
  { value: 'manual', label: '人工操作' },
  { value: 'pause', label: '暂停' },
  { value: 'delete', label: '删除' },
  { value: 'reinit', label: '重新初始化' },
]

export function logStepText(step: string): string {
  return LOG_STEP_OPTIONS.find((s) => s.value === step)?.label || step || '—'
}

export function formatLogTime(s: any): string {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s).replace('T', ' ').replace('Z', '')
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export interface KeyModel {
  value: string
  label: string
  hint: string
  recommended?: boolean
  mechanism: string
  scenarios: string
  keywords: string[]
  keyColumnHint: string
}

export const KEY_MODELS: KeyModel[] = [
  {
    value: 'unique_key',
    label: '更新模型 UNIQUE KEY',
    hint: '以唯一键去重更新，CDC 常用',
    recommended: true,
    mechanism: '以唯一键为准，键冲突时新数据覆盖旧数据。',
    scenarios: 'CDC 常用覆盖更新；历史兼容场景。',
    keywords: ['覆盖更新', '实现简单', '历史兼容'],
    keyColumnHint: '唯一键（UNIQUE KEY 列），至少 1 列',
  },
  {
    value: 'primary_key',
    label: '主键模型 PRIMARY KEY',
    hint: '高频更新场景',
    mechanism: '以主键为唯一标识，写入冲突时标记删除并写入新文件。',
    scenarios: '实时高频更新：用户画像、订单状态。',
    keywords: ['实时更新', '高性能查询', '部分列更新'],
    keyColumnHint: '主键列，至少 1 列',
  },
  {
    value: 'duplicate_key',
    label: '明细模型 DUPLICATE KEY',
    hint: '只追加不更新',
    mechanism: '全量保留原始数据，相同 KEY 可重复存在。',
    scenarios: '行为日志、操作记录、时序追加写入。',
    keywords: ['存储原始日志', '保证完整性'],
    keyColumnHint: '排序键，可不选',
  },
  {
    value: 'aggregate_key',
    label: '聚合模型 AGGREGATE KEY',
    hint: '按维度预聚合',
    mechanism: '按 AGGREGATE KEY 分组，对指标列执行 SUM/MAX/MIN 等。',
    scenarios: 'PV/UV、GMV 等固定维度预聚合。',
    keywords: ['预聚合', '统计分析'],
    keyColumnHint: '聚合键至少 1 列',
  },
]

export function keyModelLabel(v: string): string {
  return KEY_MODELS.find((m) => m.value === v)?.label || v || '—'
}

export const CDC_DYNAMIC_PARTITION_DEFAULTS: Record<string, string> = {
  'dynamic_partition.enable': 'true',
  'dynamic_partition.time_unit': 'DAY',
  'dynamic_partition.start': '-3',
  'dynamic_partition.end': '3',
  'dynamic_partition.prefix': 'p',
}
export const SR_DP_TIME_UNITS = ['DAY', 'HOUR', 'WEEK', 'MONTH']

export const CDC_INFRA_DEFAULTS = {
  kafka_topic_partitions: 3,
  kafka_topic_replication_factor: 1,
  sr_auto_buckets: 10,
  sr_replication_num: 1,
  routine_load_parallelism: 3,
}

export function positiveInt(v: any, fallback: number): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0 || !Number.isInteger(n)) return fallback
  return n
}

export function isPartitionEligibleColumn(col: any): boolean {
  const hay = `${col?.data_type || ''} ${col?.udt_name || ''}`.toLowerCase()
  if (hay.includes('time') && !hay.includes('timestamp') && !hay.includes('datetime')) return false
  return hay.includes('date') || hay.includes('timestamp') || hay.includes('datetime')
}

export function resolveEffectivePartitionColumn(taskCol: any, mappingCol: any): string {
  const mapCol = String(mappingCol || '').trim()
  if (mapCol) return mapCol
  return String(taskCol || '').trim()
}

export function buildSrDynamicPartitionObject(form: any): Record<string, string> | undefined {
  const obj: Record<string, string> = {}
  const unit = String(form?.sr_dp_time_unit || '').trim()
  const start = form?.sr_dp_start
  const end = form?.sr_dp_end
  const prefix = String(form?.sr_dp_prefix || '').trim()
  if (unit) obj['dynamic_partition.time_unit'] = unit
  if (start !== undefined && start !== null && String(start).trim() !== '')
    obj['dynamic_partition.start'] = String(start).trim()
  if (end !== undefined && end !== null && String(end).trim() !== '') obj['dynamic_partition.end'] = String(end).trim()
  if (prefix) obj['dynamic_partition.prefix'] = prefix
  for (const row of form?.sr_dynamic_partition_extra || []) {
    const k = String(row.key || '').trim()
    const val = String(row.value ?? '').trim()
    if (!k.startsWith('dynamic_partition.') || !val) continue
    obj[k] = val
  }
  return Object.keys(obj).length ? obj : undefined
}

export function formatSrDynamicPartitionDisplay(obj: Record<string, string> | undefined): string {
  if (!obj || !Object.keys(obj).length) return '（平台默认）'
  return Object.entries(obj)
    .map(([k, v]) => `${k} = ${v}`)
    .join('； ')
}

export function validatePartitionColumnsForMappings(
  enable: boolean,
  autoCreate: boolean,
  taskColumn: any,
  mappings: any[],
): string {
  if (!enable || !autoCreate) return ''
  const taskCol = String(taskColumn || '').trim()
  for (const m of mappings || []) {
    if (!resolveEffectivePartitionColumn(taskCol, m.partition_column)) {
      return `表「${m.target_table || m.source_tables?.[0] || '—'}」须配置分区列，或在任务级填写默认分区键`
    }
  }
  return ''
}

export function isValidCdcNaming(v: any): boolean {
  return /^[a-zA-Z0-9_\-]+$/.test(String(v || '').trim())
}

const SHARD_SUFFIX_RE = /^(.*?)(_?\d+)$/
export function parseShardSuffix(name: any): { base: string; num: number } | null {
  const m = SHARD_SUFFIX_RE.exec(String(name || ''))
  if (!m?.[1]) return null
  const num = Number(String(m[2] || '').replace(/^_/, ''))
  if (Number.isNaN(num)) return null
  return { base: m[1], num }
}

export interface ShardGroup {
  base: string
  pattern: string
  targetTable: string
  members: string[]
}
export function groupShardTables(names: string[]): { groups: ShardGroup[]; singles: string[] } {
  const byBase = new Map<string, { name: string; num: number }[]>()
  const noSuffix: string[] = []
  for (const name of names || []) {
    const parsed = parseShardSuffix(name)
    if (!parsed) {
      noSuffix.push(name)
      continue
    }
    const arr = byBase.get(parsed.base) || []
    arr.push({ name, num: parsed.num })
    byBase.set(parsed.base, arr)
  }
  const groups: ShardGroup[] = []
  const singles = [...noSuffix]
  for (const [base, arr] of byBase) {
    if (arr.length < 2) {
      singles.push(arr[0].name)
      continue
    }
    groups.push({
      base,
      pattern: `${base}*`,
      targetTable: base.replace(/_+$/, ''),
      members: arr.sort((a, b) => a.num - b.num).map((x) => x.name),
    })
  }
  groups.sort((a, b) => a.base.localeCompare(b.base))
  singles.sort((a, b) => a.localeCompare(b))
  return { groups, singles }
}

export function matchGlob(pattern: string, name: string): boolean {
  const p = String(pattern || '').trim()
  const n = String(name || '')
  if (!p) return false
  const esc = p.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.')
  return new RegExp(`^${esc}$`).test(n)
}

export function syncTablesFromMappings(mappings: any[]): { tables: string[]; table_map: Record<string, string> } {
  const tables: string[] = []
  const tableMap: Record<string, string> = {}
  for (const m of mappings || []) {
    const tgt = String(m.target_table || '').trim()
    for (const src of m.source_tables || []) {
      if (!src) continue
      tables.push(src)
      if (tgt) tableMap[src] = tgt
    }
  }
  return { tables: [...new Set(tables)], table_map: tableMap }
}

export function normalizeSyncTableMode(v: any): string {
  const s = String(v || '').trim().toLowerCase()
  if (s === 'initialization' || s === 'initial' || s === 'init') return 'initialization'
  return 'incremental'
}
export function syncTableModeLabel(v: any): string {
  return normalizeSyncTableMode(v) === 'initialization' ? '初始化（写 snapshot）' : '增量'
}

export function formatDebeziumConfig(cfg: any): string {
  if (!cfg || typeof cfg !== 'object') return ''
  const keys = Object.keys(cfg).sort()
  const ordered: Record<string, any> = {}
  for (const k of keys) ordered[k] = cfg[k]
  return JSON.stringify(ordered, null, 2)
}

export function collectSqlOverrides(previews: any[]): Record<string, any> {
  const out: Record<string, any> = {}
  for (const it of previews || []) {
    const table = String(it?.table || '').trim()
    if (!table) continue
    const ddl = String(it.ddl || '').trim()
    const load = String(it.load || '').trim()
    if (!ddl && !load) continue
    out[table] = { create_table_sql: ddl, routine_load_sql: load }
  }
  return out
}

/** 分库名须以前缀开头，且去掉可选一个分隔 `_` 后后缀为非空纯数字（与后端 MatchNumericShardName 一致）。 */
export function matchNumericShardName(name: string, prefix: string): boolean {
  const n = String(name || '').trim()
  const pre = String(prefix || '').trim()
  if (!n || !pre) return false
  if (!n.startsWith(pre)) return false
  let rest = n.slice(pre.length)
  if (rest.startsWith('_')) rest = rest.slice(1)
  if (!rest) return false
  for (let i = 0; i < rest.length; i++) {
    const c = rest.charCodeAt(i)
    if (c < 48 || c > 57) return false
  }
  return true
}

export function filterShardDatabases(names: string[], prefix: string): string[] {
  const pre = String(prefix || '').trim()
  if (!pre) return []
  return (names || [])
    .filter((n) => matchNumericShardName(String(n || ''), pre))
    .sort((a, b) => a.localeCompare(b))
}

export function resolveSourceScope(opts: {
  mode: string
  representativeDb: string
  dbPrefix: string
  names: string[]
}): { mode: string; representative_db: string; db_prefix?: string; resolved_db_names: string[] } {
  const representative_db = String(opts.representativeDb || '').trim()
  if (opts.mode !== 'multi_db') {
    return { mode: 'single_db', representative_db, resolved_db_names: representative_db ? [representative_db] : [] }
  }
  const resolved = filterShardDatabases(opts.names, opts.dbPrefix)
  return {
    mode: 'multi_db',
    representative_db,
    db_prefix: String(opts.dbPrefix || '').trim(),
    resolved_db_names: resolved,
  }
}

export function shardDbPrefixHint(database: string, prefix: string): string {
  const db = String(database || '').trim().toLowerCase()
  const pre = String(prefix || '').trim().toLowerCase()
  if (!db || !pre) return ''
  if (db.startsWith(pre)) return ''
  let i = 0
  while (i < db.length && i < pre.length && db[i] === pre[i]) i += 1
  const ratio = i / Math.max(db.length, pre.length)
  if (ratio >= 0.72) return '逻辑库名未以分库前缀开头，但两者前缀较相似，请确认分库前缀是否正确'
  return '逻辑库名与分库前缀不一致，建议检查是否选错源库或前缀填写错误'
}

export function isMultiDbScope(mode: string, names: string[]): boolean {
  return mode === 'multi_db' && (names || []).length >= 2
}

export function parentTablesOf(row: any): string[] {
  const extra = decodeCdcExtra(row?.description).extra
  const tables = extra.tables
  if (Array.isArray(tables)) return tables.filter(Boolean)
  return String(tables || '')
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

export function countTasksSyncedTable(tableName: string, parents: any[]): number {
  const n = String(tableName || '').toLowerCase()
  let c = 0
  for (const p of parents || []) {
    if (parentTablesOf(p).some((t) => String(t).toLowerCase() === n)) c += 1
  }
  return c
}

export function tablesSyncedInAllParents(parents: any[]): string[] {
  if (!parents?.length) return []
  const lists = parents.map((p) => parentTablesOf(p).map((t) => String(t).toLowerCase()))
  if (lists.some((l) => !l.length)) return []
  const [first, ...rest] = lists
  const seen = new Set<string>()
  const out: string[] = []
  for (const t of first) {
    if (seen.has(t)) continue
    seen.add(t)
    if (rest.every((l) => l.includes(t))) out.push(t)
  }
  return out
}

export function resolveParentInstanceKey(row: any, inst: any): string {
  const host = String(inst?.host || inst?.domain_name || '').trim()
  if (host) return `${host}:${inst?.port ?? ''}`
  if (row?.source_host) return `${row.source_host}:${row.source_port ?? ''}`
  return String(row?.source_instance_id || '').trim()
}

export function validateAddTableParents(rows: any[], instOf: (id: string) => any): string {
  if (!rows?.length) return '请至少选择一个已有初始化任务'
  const keys = new Set(rows.map((r) => resolveParentInstanceKey(r, instOf?.(r.source_instance_id))).filter(Boolean))
  if (keys.size > 1) return '所选任务须属于同一源端实例，请取消不同实例的任务'
  return ''
}

export function pgTypeToStarRocksType(col: any): string {
  const dt = String(col?.data_type || col?.udt_name || '').toLowerCase()
  if (dt.includes('int8') || dt.includes('bigint')) return 'BIGINT'
  if (dt.includes('int4') || dt === 'int' || dt.includes('integer')) return 'INT'
  if (dt.includes('int2') || dt.includes('smallint')) return 'SMALLINT'
  if (dt.includes('bool')) return 'BOOLEAN'
  if (dt.includes('float8') || dt.includes('double')) return 'DOUBLE'
  if (dt.includes('float4') || dt === 'real') return 'FLOAT'
  if (dt.includes('numeric') || dt.includes('decimal')) return 'DECIMAL(38,10)'
  if (dt.includes('timestamp')) return 'DATETIME'
  if (dt === 'date') return 'DATE'
  if (dt.includes('json')) return 'JSON'
  return 'STRING'
}

export function computeColumnsToAdd(pgCols: any[], srNames: string[], primaryKeys: string[]): any[] {
  const sr = new Set((srNames || []).map((n) => String(n).toLowerCase()))
  const pks = new Set((primaryKeys || []).map((n) => String(n).toLowerCase()))
  return (pgCols || [])
    .filter((c) => {
      const name = String(c.name || '').toLowerCase()
      if (!name || pks.has(name) || c.is_primary_key) return false
      return !sr.has(name)
    })
    .map((c) => ({
      name: c.name,
      pg_type: c.data_type || '—',
      sr_type: pgTypeToStarRocksType(c),
      selected: true,
    }))
}

export function buildSqlPreview(form: any, mappingList: any[], kafkaBrokers: string): any[] {
  const db = form.target_db || 'target_db'
  const model = (form.key_model || 'unique_key').replace('_', ' ').toUpperCase()
  const mode = form.debezium_mode || 'after_only'
  const buckets = positiveInt(form.sr_auto_buckets, CDC_INFRA_DEFAULTS.sr_auto_buckets)
  const repl = positiveInt(form.sr_replication_num, CDC_INFRA_DEFAULTS.sr_replication_num)
  const rl = positiveInt(form.routine_load_parallelism, CDC_INFRA_DEFAULTS.routine_load_parallelism)
  const prefix = form.topic_prefix || form.name || 'cdc'
  const schema = form.source_schema || 'public'
  const env = String(form.env || '').trim()
  const sourceDb = String(form.source_db || '').trim()
  const defaultPrefix = [env, sourceDb].filter(Boolean).join('_')
  const rlNameOf = (tgt: string) => {
    const table = String(tgt || '').trim() || 'table'
    if (prefix && defaultPrefix && prefix !== defaultPrefix) return `rl_${prefix}_${table}`
    if (env || sourceDb) return `rl_${[env, sourceDb, table].filter(Boolean).join('_')}`
    return `rl_${table}`
  }
  const brokers = String(kafkaBrokers || form.kafka_bootstrap_servers || '').trim() || 'kafka:9092'
  const dpOn = !!form.sr_dynamic_partition_enable
  const dpObj = dpOn ? buildSrDynamicPartitionObject(form) || { ...CDC_DYNAMIC_PARTITION_DEFAULTS } : null
  const maps = (mappingList || []).length
    ? mappingList
    : (form.tables || []).map((table: string) => ({
        source_tables: [table],
        target_table: form.table_map?.[table] || table,
        key_columns: ['id'],
      }))
  const seen = new Set<string>()
  return maps
    .map((m: any) => {
      const tgt = m.target_table || m.source_tables?.[0] || 'example_table'
      if (seen.has(tgt)) return null
      seen.add(tgt)
      const keys = (m.key_columns || []).length ? m.key_columns : ['id']
      const shard = (m.shard_keys || []).length ? m.shard_keys : keys
      const source = (m.source_tables || []).join(', ')
      const keyCols = keys.map((k: string) => `  \`${k}\` BIGINT NOT NULL`).join(',\n')
      const partCol = dpOn ? resolveEffectivePartitionColumn(form.sr_partition_column, m.partition_column) : ''
      const topics = (m.source_tables || [tgt]).map((t: string) => `${prefix}.${schema}.${t}`).join(',')
      const jsonpaths =
        mode === 'before_after'
          ? `[${keys.map((k: string) => `"$.after.${k}"`).join(',')},"$.op"]`
          : `[${keys.map((k: string) => `"$.${k}"`).join(',')},"$.__op"]`
      const props = [`  "replication_num" = "${repl}"`, '  "enable_persistent_index" = "true"']
      if (partCol && dpObj) {
        Object.entries({ ...CDC_DYNAMIC_PARTITION_DEFAULTS, ...dpObj, ...(m.sr_dynamic_partition || {}) }).forEach(
          ([k, v]) => {
            props.push(`  "${k}" = "${v}"`)
          },
        )
      }
      const rlName = rlNameOf(tgt)
      const load = [
        `CREATE ROUTINE LOAD \`${db}\`.\`${rlName}\` ON \`${tgt}\``,
        `COLUMNS(${keys.join(', ')}, __op)`,
        `PROPERTIES (`,
        `  "format" = "json",`,
        `  "jsonpaths" = "${jsonpaths.replace(/"/g, '\\"')}",`,
        `  "desired_concurrent_number" = "${rl}"`,
        `)`,
        `FROM KAFKA (`,
        `  "kafka_broker_list" = "${brokers}",`,
        `  "kafka_topic" = "${topics}",`,
        `  "property.group.id" = "${rlName}",`,
        `  "property.kafka_default_offsets" = "OFFSET_BEGINNING"`,
        `);`,
      ]
      return {
        table: tgt,
        source,
        ddl: [
          `-- ${db}.${tgt}（按所选 KEY 列预览）`,
          `CREATE TABLE IF NOT EXISTS \`${db}\`.\`${tgt}\` (`,
          `${keyCols}`,
          `) ENGINE=OLAP`,
          `${model}(${keys.join(', ')})`,
          partCol ? `PARTITION BY RANGE(\`${partCol}\`) ()` : null,
          `DISTRIBUTED BY HASH(${shard.join(', ')}) BUCKETS ${buckets}`,
          `PROPERTIES (\n${props.join(',\n')}\n);`,
        ]
          .filter(Boolean)
          .join('\n'),
        load: load.join('\n'),
      }
    })
    .filter(Boolean)
}

export function snapshotLabel(v: string): string {
  return { initial: '全量 + 增量', never: '增量' }[v] || v || '—'
}

export function isSensitiveConfigKey(key: any): boolean {
  return /password|secret|token|credential/i.test(String(key || ''))
}

export function formatOperationType(source: string): string {
  return (
    { ui_save: '修改', restore: '恢复', add_table: '新增表', remove_table: '移除表', reinit: '重新初始化' }[source] ||
    (source ? '修改' : '—')
  )
}

// AntD Tag 颜色
export function operationTypeTagColor(source: string): string {
  return (
    { restore: 'orange', add_table: 'green', remove_table: 'red', ui_save: 'default', reinit: 'purple' }[source] ||
    'default'
  )
}
