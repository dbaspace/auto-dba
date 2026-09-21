const META_PREFIX = '-- auto-dba-cdc:'
const TASK_ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function isValidCdcTaskID(id) {
  return TASK_ID_RE.test(String(id || '').trim())
}

export function normalizeKafkaBootstrap(s) {
  return String(s || '').split(',').map((part) => {
    const p = part.trim()
    if (!p || p.startsWith('[')) return p
    const m = p.match(/^(.*):(\d+):(\d+)$/)
    if (m && m[2] === m[3]) return `${m[1]}:${m[2]}`
    return p
  }).filter(Boolean).join(',')
}

export function kafkaInstanceAddress(i) {
  const host = String(i?.host || i?.domain_name || '').trim()
  const port = Number(i?.port)
  if (!host) return ''
  const cleaned = normalizeKafkaBootstrap(host)
  if (/:\d+$/.test(cleaned) || cleaned.includes(',')) return cleaned
  if (Number.isFinite(port) && port > 0) return `${cleaned}:${port}`
  return cleaned
}



export const TYPE_LABELS = {
  init: '初始化',
  add_table: '新增表',
  remove_table: '移除表',
  alter_schema: '源表加列',
  delete_task: '删除任务',
}

export const PAGE_TITLES = {
  init: '初始化同步',
  add_table: '新增同步表',
  alter_schema: '源表加列',
  remove_table: '移除同步表',
}

export const PAGE_HINTS = {
  init: '首次为某个 PG 数据库创建完整同步链路',
  add_table: '在已有同步任务基础上追加新表（继承 Connector / Debezium 模式）',
  alter_schema: '源端表新增列后，同步到 StarRocks 目标表并更新 Routine Load',
  remove_table: '从已有初始化任务的 Publication 中移除表并收缩同步范围',
}

export const KEY_MODELS = [
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

export const CDC_DYNAMIC_PARTITION_DEFAULTS = {
  'dynamic_partition.enable': 'true',
  'dynamic_partition.time_unit': 'DAY',
  'dynamic_partition.start': '-3',
  'dynamic_partition.end': '3',
  'dynamic_partition.prefix': 'p',
}

export const SR_DP_TIME_UNITS = ['DAY', 'HOUR', 'WEEK', 'MONTH']

export function isPartitionEligibleColumn(col) {
  const hay = `${col?.data_type || ''} ${col?.udt_name || ''}`.toLowerCase()
  if (hay.includes('time') && !hay.includes('timestamp') && !hay.includes('datetime')) return false
  return hay.includes('date') || hay.includes('timestamp') || hay.includes('datetime')
}

export function resolveEffectivePartitionColumn(taskCol, mappingCol) {
  const mapCol = String(mappingCol || '').trim()
  if (mapCol) return mapCol
  return String(taskCol || '').trim()
}

export function buildSrDynamicPartitionObject(form) {
  const obj = {}
  const unit = String(form?.sr_dp_time_unit || '').trim()
  const start = form?.sr_dp_start
  const end = form?.sr_dp_end
  const prefix = String(form?.sr_dp_prefix || '').trim()
  if (unit) obj['dynamic_partition.time_unit'] = unit
  if (start !== undefined && start !== null && String(start).trim() !== '') obj['dynamic_partition.start'] = String(start).trim()
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

export function formatSrDynamicPartitionDisplay(obj) {
  if (!obj || !Object.keys(obj).length) return '（平台默认）'
  return Object.entries(obj).map(([k, v]) => `${k} = ${v}`).join('； ')
}

export function validatePartitionColumnsForMappings(enable, autoCreate, taskColumn, mappings) {
  if (!enable || !autoCreate) return ''
  const taskCol = String(taskColumn || '').trim()
  for (const m of mappings || []) {
    if (!resolveEffectivePartitionColumn(taskCol, m.partition_column)) {
      return `表「${m.target_table || m.source_tables?.[0] || '—'}」须配置分区列，或在任务级填写默认分区键`
    }
  }
  return ''
}

export function isValidCdcNaming(v) {
  return /^[a-zA-Z0-9_\-]+$/.test(String(v || '').trim())
}

export function pipelineNodeTone(state) {
  const st = String(state || '').toUpperCase()
  if (['UNREACHABLE', 'FAILED', 'MISSING', 'DEGRADED', 'NOT_FOUND', 'ERROR'].includes(st)) return 'is-error'
  if (['READY', 'RUNNING', 'OK', 'SUCCESS'].includes(st)) return 'is-ok'
  if (['PAUSED', 'SKIPPED'].includes(st)) return 'is-warn'
  if (st === 'UNKNOWN' || st === '') return ''
  return 'is-run'
}

export function formatLagBytes(n) {
  const b = Number(n)
  if (!Number.isFinite(b) || b < 0) return '—'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} kB`
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`
  return `${(b / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

export function syncTableCountView(extra) {
  const tables = extra?.tables || []
  const maps = extra?.mappings || []
  const shard = maps.filter((m) => m.is_shard_group || (m.source_tables || []).length > 1).length
  const source = tables.length || maps.reduce((n, m) => n + (m.source_tables || []).length, 0)
  return {
    source,
    target: maps.length || tables.length,
    shard,
  }
}

export function currentUsername() {
  try {
    return String(JSON.parse(localStorage.getItem('user') || '{}').username || '').trim()
  } catch {
    return ''
  }
}

export function sameUser(a, b) {
  const x = String(a || '').trim().toLowerCase()
  const y = String(b || '').trim().toLowerCase()
  return Boolean(x && x === y)
}

export function createdByOf(task, extra) {
  return String(extra?.created_by || task?.created_by || '').trim()
}

export function assignedToOf(task, extra) {
  return String(extra?.assigned_to || extra?.reviewer || extra?.approver || task?.assigned_to || '').trim()
}

export function pipelineBundle(status) {
  return status?.pipeline && typeof status.pipeline === 'object' ? status.pipeline : (status || {})
}

export function mappingSourceText(src) {
  return { add_table: '新增表', remove_table: '移除表', alter_schema: '源表加列', add_column: '源表加列', init: '初始化' }[src] || '初始化'
}

export function mappingSourceTag(src) {
  return { add_table: 'success', remove_table: 'warning', alter_schema: 'info', add_column: 'info', init: '' }[src] || ''
}

export function connectorWorkerURL(extra, detail) {
  const raw = String(detail?.connect_base_url || detail?.worker_url || extra?.connect_base_url || extra?.worker_url || '').trim()
  if (!raw) return ''
  return /^https?:\/\//i.test(raw) ? raw : `http://${raw}`
}

export function runningTaskCount(tasks) {
  const list = Array.isArray(tasks) ? tasks : []
  return list.filter((t) => String(t?.state || '').toUpperCase() === 'RUNNING').length
}

/** 与全局参数 sync_kafka_* / sync_sr_* 默认一致 */
export const CDC_INFRA_DEFAULTS = {
  kafka_topic_partitions: 3,
  kafka_topic_replication_factor: 1,
  sr_auto_buckets: 10,
  sr_replication_num: 1,
  routine_load_parallelism: 3,
}

export function positiveInt(v, fallback) {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0 || !Number.isInteger(n)) return fallback
  return n
}

export function encodeCdcExtra(userDesc, extra) {
  const text = String(userDesc || '').trim()
  const blob = META_PREFIX + JSON.stringify(extra || {})
  return text ? `${text}\n${blob}` : blob
}

export function decodeCdcExtra(description) {
  const raw = String(description || '')
  const idx = raw.indexOf(META_PREFIX)
  if (idx < 0) return { text: raw.trim(), extra: {} }
  const text = raw.slice(0, idx).trim()
  try {
    const extra = JSON.parse(raw.slice(idx + META_PREFIX.length) || '{}')
    return { text, extra: extra && typeof extra === 'object' ? extra : {} }
  } catch {
    return { text, extra: {} }
  }
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
]

export function logStepText(step) {
  return LOG_STEP_OPTIONS.find((s) => s.value === step)?.label || step || '—'
}

export function formatLogTime(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return String(s).replace('T', ' ').replace('Z', '')
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function typeText(t) {
  return TYPE_LABELS[t] || t || '—'
}

export function typeTag(t) {
  return { init: '', add_table: 'success', remove_table: 'warning', alter_schema: 'info', delete_task: 'danger' }[t] || 'info'
}

export function envTag(e) {
  if (e === 'prod' || e === '生产') return 'danger'
  if (e === 'test' || e === '测试') return 'success'
  return 'info'
}

export function statusText(s) {
  return {
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
  }[s] || s || '—'
}

export function statusTag(s) {
  if (s === 'running') return 'success'
  if (s === 'paused' || s === 'pending' || s === 'init' || s === 'creating' || s === 'provisioning') return 'warning'
  if (s === 'error' || s === 'failed' || s === 'rejected') return 'danger'
  return 'info'
}

export function approvalText(s) {
  return { pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销', cancelled: '已撤销', none: '—' }[s] || s || '—'
}

export function instLabel(i) {
  if (!i) return '—'
  const addr = i.host ? `${i.host}:${i.port}` : ''
  return i.name && addr ? `${i.name} — ${addr}` : i.name || addr || i.id
}

export function instAddr(i) {
  if (!i) return '—'
  return i.host ? `${i.host}:${i.port}` : i.name || '—'
}

export function instEnv(i) {
  return i?.idc || i?.env || i?.env_code || ''
}

export function keyModelLabel(v) {
  return KEY_MODELS.find((m) => m.value === v)?.label || v || '—'
}

const SHARD_SUFFIX_RE = /^(.*?)(_?\d+)$/

export function parseShardSuffix(name) {
  const m = SHARD_SUFFIX_RE.exec(String(name || ''))
  if (!m?.[1]) return null
  const num = Number(String(m[2] || '').replace(/^_/, ''))
  if (Number.isNaN(num)) return null
  return { base: m[1], num }
}

export function groupShardTables(names) {
  const byBase = new Map()
  const noSuffix = []
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
  const groups = []
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

export function matchGlob(pattern, name) {
  const p = String(pattern || '').trim()
  const n = String(name || '')
  if (!p) return false
  const esc = p.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.')
  return new RegExp(`^${esc}$`).test(n)
}

export function syncTablesFromMappings(mappings) {
  const tables = []
  const tableMap = {}
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

export function debeziumLabel(v) {
  return { after_only: 'after_only', before_after: 'before_after' }[v] || v || 'after_only'
}

export function snapshotLabel(v) {
  return { initial: '全量 + 增量', never: '增量' }[v] || v || '—'
}

export function normalizeSyncTableMode(v) {
  const s = String(v || '').trim().toLowerCase()
  if (s === 'initialization' || s === 'initial' || s === 'init') return 'initialization'
  return 'incremental'
}

export function syncTableModeLabel(v) {
  return normalizeSyncTableMode(v) === 'initialization' ? '初始化（写 snapshot）' : '增量'
}

export function isSensitiveConfigKey(key) {
  return /password|secret|token|credential/i.test(String(key || ''))
}

export function formatOperationType(source) {
  return { ui_save: '修改', restore: '恢复', add_table: '新增表', remove_table: '移除表' }[source] || (source ? '修改' : '—')
}

export function operationTypeTag(source) {
  return { restore: 'warning', add_table: 'success', remove_table: 'danger', ui_save: '' }[source] || ''
}

export function formatDebeziumConfig(cfg) {
  if (!cfg || typeof cfg !== 'object') return ''
  const keys = Object.keys(cfg).sort()
  const ordered = {}
  for (const k of keys) ordered[k] = cfg[k]
  return JSON.stringify(ordered, null, 2)
}

export function collectSqlOverrides(previews) {
  const out = {}
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

export function buildSqlPreview(form, mappingList, kafkaBrokers) {
  const db = form.target_db || 'target_db'
  const model = (form.key_model || 'unique_key').replace('_', ' ').toUpperCase()
  const mode = form.debezium_mode || 'after_only'
  const buckets = positiveInt(form.sr_auto_buckets, CDC_INFRA_DEFAULTS.sr_auto_buckets)
  const repl = positiveInt(form.sr_replication_num, CDC_INFRA_DEFAULTS.sr_replication_num)
  const rl = positiveInt(form.routine_load_parallelism, CDC_INFRA_DEFAULTS.routine_load_parallelism)
  const prefix = form.topic_prefix || form.name || 'cdc'
  const schema = form.source_schema || 'public'
  const brokers = String(kafkaBrokers || form.kafka_bootstrap_servers || '').trim() || 'kafka:9092'
  const dpOn = !!form.sr_dynamic_partition_enable
  const dpObj = dpOn ? (buildSrDynamicPartitionObject(form) || { ...CDC_DYNAMIC_PARTITION_DEFAULTS }) : null
  const maps = (mappingList || []).length
    ? mappingList
    : (form.tables || []).map((table) => ({
      source_tables: [table],
      target_table: form.table_map?.[table] || table,
      key_columns: ['id'],
    }))
  const seen = new Set()
  return maps.map((m) => {
    const tgt = m.target_table || m.source_tables?.[0] || 'example_table'
    if (seen.has(tgt)) return null
    seen.add(tgt)
    const keys = (m.key_columns || []).length ? m.key_columns : ['id']
    const shard = (m.shard_keys || []).length ? m.shard_keys : keys
    const source = (m.source_tables || []).join(', ')
    const keyCols = keys.map((k) => `  \`${k}\` BIGINT NOT NULL`).join(',\n')
    const partCol = dpOn ? resolveEffectivePartitionColumn(form.sr_partition_column, m.partition_column) : ''
    const topics = (m.source_tables || [tgt]).map((t) => `${prefix}.${schema}.${t}`).join(',')
    const jsonpaths = mode === 'before_after'
      ? `[${keys.map((k) => `"$.after.${k}"`).join(',')},"$.op"]`
      : `[${keys.map((k) => `"$.${k}"`).join(',')},"$.__op"]`
    const props = [`  "replication_num" = "${repl}"`, '  "enable_persistent_index" = "true"']
    if (partCol && dpObj) {
      Object.entries({ ...CDC_DYNAMIC_PARTITION_DEFAULTS, ...dpObj, ...(m.sr_dynamic_partition || {}) }).forEach(([k, v]) => {
        props.push(`  "${k}" = "${v}"`)
      })
    }
    const load = [
      `CREATE ROUTINE LOAD \`${db}\`.\`${tgt}_rl\` ON \`${tgt}\``,
      `COLUMNS(${keys.join(', ')}, __op)`,
      `PROPERTIES (`,
      `  "format" = "json",`,
      `  "jsonpaths" = "${jsonpaths.replace(/"/g, '\\"')}",`,
      `  "desired_concurrent_number" = "${rl}"`,
      `)`,
      `FROM KAFKA (`,
      `  "kafka_broker_list" = "${brokers}",`,
      `  "kafka_topic" = "${topics}",`,
      `  "property.group.id" = "${tgt}_rl",`,
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
      ].filter(Boolean).join('\n'),
      load: load.join('\n'),
    }
  }).filter(Boolean)
}

export function filterShardDatabases(names, prefix) {
  const pre = String(prefix || '').trim().toLowerCase()
  if (!pre) return []
  return (names || []).filter((n) => String(n || '').toLowerCase().startsWith(pre)).sort((a, b) => a.localeCompare(b))
}

export function resolveSourceScope({ mode, representativeDb, dbPrefix, names }) {
  const representative_db = String(representativeDb || '').trim()
  if (mode !== 'multi_db') {
    return { mode: 'single_db', representative_db, resolved_db_names: representative_db ? [representative_db] : [] }
  }
  const resolved = filterShardDatabases(names, dbPrefix)
  return { mode: 'multi_db', representative_db, db_prefix: String(dbPrefix || '').trim(), resolved_db_names: resolved }
}

export function formatSourceScopeLabel(dbName, prefix, dbNames) {
  const names = dbNames || []
  if (names.length > 1) {
    const pre = String(prefix || '').trim()
    return `${pre ? `${pre}*` : names[0]}（${names.length} 库）`
  }
  return String(dbName || names[0] || '—')
}

export function shardDbPrefixHint(database, prefix) {
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

export function pgTypeToStarRocksType(col) {
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

export function computeColumnsToAdd(pgCols, srNames, primaryKeys) {
  const sr = new Set((srNames || []).map((n) => String(n).toLowerCase()))
  const pks = new Set((primaryKeys || []).map((n) => String(n).toLowerCase()))
  return (pgCols || []).filter((c) => {
    const name = String(c.name || '').toLowerCase()
    if (!name || pks.has(name) || c.is_primary_key) return false
    return !sr.has(name)
  }).map((c) => ({
    name: c.name,
    pg_type: c.data_type || '—',
    sr_type: pgTypeToStarRocksType(c),
    selected: true,
  }))
}

export function parentInstanceKey(row) {
  if (!row) return ''
  if (row.source_host) return `${row.source_host}:${row.source_port ?? ''}`
  return String(row.source_instance_id || '').trim()
}

export function parentTablesOf(row) {
  const extra = decodeCdcExtra(row?.description).extra
  const tables = extra.tables
  if (Array.isArray(tables)) return tables.filter(Boolean)
  return String(tables || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

export function countTasksSyncedTable(tableName, parents) {
  const n = String(tableName || '').toLowerCase()
  let c = 0
  for (const p of parents || []) {
    if (parentTablesOf(p).some((t) => String(t).toLowerCase() === n)) c += 1
  }
  return c
}

export function isMultiDbScope(mode, names) {
  return mode === 'multi_db' && (names || []).length >= 2
}

export function tablesSyncedInAllParents(parents) {
  if (!parents?.length) return []
  const lists = parents.map((p) => parentTablesOf(p).map((t) => String(t).toLowerCase()))
  if (lists.some((l) => !l.length)) return []
  const [first, ...rest] = lists
  const seen = new Set()
  const out = []
  for (const t of first) {
    if (seen.has(t)) continue
    seen.add(t)
    if (rest.every((l) => l.includes(t))) out.push(t)
  }
  return out
}

export function resolveParentInstanceKey(row, inst) {
  const host = String(inst?.host || inst?.domain_name || '').trim()
  if (host) return `${host}:${inst?.port ?? ''}`
  return parentInstanceKey(row)
}

export function validateAddTableParents(rows, instOf) {
  if (!rows?.length) return '请至少选择一个已有初始化任务'
  const keys = new Set(rows.map((r) => resolveParentInstanceKey(r, instOf?.(r.source_instance_id))).filter(Boolean))
  if (keys.size > 1) return '所选任务须属于同一源端实例，请取消不同实例的任务'
  return ''
}
