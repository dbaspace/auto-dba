// CdcCreate 纯逻辑（无 JSX）：由旧 CdcCreate.vue <script setup> 迁移
// - 4 种任务模式的步骤常量
// - emptyForm / 类型
// - 校验函数
// - extraPayload / submitBody 构造
// - 标签映射等
import {
  CDC_INFRA_DEFAULTS,
  PAGE_TITLES,
  buildSrDynamicPartitionObject,
  collectSqlOverrides,
  decodeCdcExtra,
  filterShardDatabases,
  isMultiDbScope,
  normalizeKafkaBootstrap,
  normalizeSyncTableMode,
  positiveInt,
  resolveEffectivePartitionColumn,
  resolveSourceScope,
} from './cdc'

// ---- 步骤常量（与 Vue 一字不差，4 模式） ----
export const INIT_STEPS = ['选择源端', '选择表', '配置 Debezium', '配置目标端', '检测', 'SQL 预览', '确认并提交审批']
export const ADD_STEPS_AUTO = ['选择已有任务', '选择新增表', '配置目标端', 'SQL 预览', '确认并提交审批']
export const ADD_STEPS = ['选择已有任务', '选择新增表', '配置目标端', '确认并提交审批']
export const ALTER_STEPS = ['选择已有任务', '选择目标表', '执行方式', '确认并提交审批']
export const REMOVE_STEPS = ['选择已有任务', '选择移除表', '确认并提交审批']

// ---- 类型 ----
export interface CdcMapping {
  key: string
  source_tables: string[]
  target_table: string
  key_columns: string[]
  shard_keys: string[]
  partition_column: string
  is_shard_group: boolean
  shard_pattern: string
  source_db_scope: string
  source_db_names: string[]
  sync_table_mode: string
  open: boolean
  sr_dynamic_partition?: Record<string, string>
  [k: string]: any
}

export interface CdcForm {
  name: string
  description: string
  task_type: string
  env: string
  parent_id: string
  parent_ids: string[]
  source_scope_mode: string
  source_db_prefix: string
  source_db_names: string[]
  source_engine: string
  source_instance_id: string
  target_instance_id: string
  source_db: string
  target_db: string
  source_schema: string
  mysql_server_id: string
  tables: string[]
  table_map: Record<string, string>
  connector_id: string
  message_schema_mode: string
  debezium_mode: string
  snapshot_mode: string
  filter_delete_events: boolean
  kafka_bootstrap_servers: string
  routine_load_parallelism: number
  kafka_topic_partitions: number
  kafka_topic_replication_factor: number
  sr_auto_buckets: number
  sr_replication_num: number
  sr_dynamic_partition_enable: boolean
  sr_partition_column: string
  sr_dp_time_unit: string
  sr_dp_start: string
  sr_dp_end: string
  sr_dp_prefix: string
  publication_name: string
  slot_name: string
  connector_name: string
  topic_prefix: string
  auto_create_table: boolean
  key_model: string
  alter_schema_exec_mode: string
  kafka_default_offsets: string
  alter_columns: string
  alter_schema_tables: any[]
  pg_column_ack: boolean
  reviewer: string
  mappings: any[]
  [k: string]: any
}

export interface InfraDefaults {
  routine_load_parallelism: number
  kafka_topic_partitions: number
  kafka_topic_replication_factor: number
  sr_auto_buckets: number
  sr_replication_num: number
}

export interface SqlPreviewItem {
  table: string
  source: string
  ddl: string
  load: string
  skipped?: boolean
}

export interface PrecheckCheck {
  name?: string
  level?: string
  status?: string
  message?: string
  [k: string]: any
}

// ---- emptyForm ----
export function emptyForm(infraDefaults: InfraDefaults = { ...CDC_INFRA_DEFAULTS }): CdcForm {
  return {
    name: '',
    description: '',
    task_type: 'init',
    env: '',
    parent_id: '',
    parent_ids: [],
    source_scope_mode: 'single_db',
    source_db_prefix: '',
    source_db_names: [],
    source_engine: 'postgres',
    source_instance_id: '',
    target_instance_id: '',
    source_db: '',
    target_db: '',
    source_schema: 'public',
    mysql_server_id: '5400',
    tables: [],
    table_map: {},
    connector_id: '',
    message_schema_mode: 'without_schema',
    debezium_mode: 'after_only',
    snapshot_mode: 'initial',
    filter_delete_events: false,
    kafka_bootstrap_servers: '',
    routine_load_parallelism: infraDefaults.routine_load_parallelism,
    kafka_topic_partitions: infraDefaults.kafka_topic_partitions,
    kafka_topic_replication_factor: infraDefaults.kafka_topic_replication_factor,
    sr_auto_buckets: infraDefaults.sr_auto_buckets,
    sr_replication_num: infraDefaults.sr_replication_num,
    sr_dynamic_partition_enable: false,
    sr_partition_column: '',
    sr_dp_time_unit: '',
    sr_dp_start: '',
    sr_dp_end: '',
    sr_dp_prefix: '',
    publication_name: '',
    slot_name: '',
    connector_name: '',
    topic_prefix: '',
    auto_create_table: true,
    key_model: 'unique_key',
    alter_schema_exec_mode: 'sql_only',
    kafka_default_offsets: 'OFFSET_BEGINNING',
    alter_columns: '',
    alter_schema_tables: [],
    pg_column_ack: false,
    reviewer: '',
    mappings: [],
  }
}

// ---- infra 默认值 ----
export function applyInfraDefaults(args: any[]): InfraDefaults {
  const list = args || []
  const n = (key: string, fallback: number) =>
    positiveInt(list.find((a) => a.key === key)?.value, fallback)
  return {
    routine_load_parallelism: n('sync_sr_routine_load_desired_concurrent', CDC_INFRA_DEFAULTS.routine_load_parallelism),
    kafka_topic_partitions: n('sync_kafka_topic_partitions', CDC_INFRA_DEFAULTS.kafka_topic_partitions),
    kafka_topic_replication_factor: n('sync_kafka_topic_replication_factor', CDC_INFRA_DEFAULTS.kafka_topic_replication_factor),
    sr_auto_buckets: n('sync_sr_auto_buckets', CDC_INFRA_DEFAULTS.sr_auto_buckets),
    sr_replication_num: n('sync_sr_replication_num', CDC_INFRA_DEFAULTS.sr_replication_num),
  }
}

// ---- 类型规整 ----
export function matchType(a: any, b: any): boolean {
  const n = (t: any) => {
    t = String(t || '').toLowerCase()
    if (['postgresql', 'pg', 'pgsql'].includes(t)) return 'postgres'
    if (t === 'sr') return 'starrocks'
    return t
  }
  return n(a) === n(b)
}

export function instanceID(v: any): string {
  if (v && typeof v === 'object') return String(v.id || v.instance_id || v.value || '').trim()
  return String(v || '').trim()
}

// ---- 步骤计算 ----
export function stepsForType(taskType: string, autoCreateTable: boolean): string[] {
  if (taskType === 'alter_schema') return ALTER_STEPS
  if (taskType === 'remove_table') return REMOVE_STEPS
  if (taskType === 'add_table') return autoCreateTable ? ADD_STEPS_AUTO : ADD_STEPS
  return INIT_STEPS
}

export function needParentType(taskType: string): boolean {
  return taskType === 'add_table' || taskType === 'alter_schema' || taskType === 'remove_table'
}

export function isSqlStepFor(taskType: string, autoCreateTable: boolean, step: number): boolean {
  if (taskType === 'init') return step === 5
  if (taskType === 'add_table') return autoCreateTable && step === 3
  return false
}

// ---- 映射工厂 ----
export function newMapping(
  opts: { source_tables: string[]; target_table?: string; is_shard_group?: boolean; shard_pattern?: string },
  taskType: string,
  snapshotMode: string,
): CdcMapping {
  return {
    key: `m_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
    source_tables: [...opts.source_tables],
    target_table: opts.target_table || opts.source_tables[0] || '',
    key_columns: [],
    shard_keys: [],
    partition_column: '',
    is_shard_group: !!opts.is_shard_group,
    shard_pattern: opts.shard_pattern || '',
    source_db_scope: 'all',
    source_db_names: [],
    sync_table_mode:
      taskType === 'add_table'
        ? 'incremental'
        : normalizeSyncTableMode(snapshotMode === 'never' ? 'incremental' : 'initialization'),
    open: false,
  }
}

export function restoreMappings(raw: any, formTables: string[], tableMap: Record<string, string>): CdcMapping[] {
  const list = Array.isArray(raw) ? raw : []
  let mappings: CdcMapping[] = list.map((m: any) => ({
    key: m.key || `m_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
    source_tables: [...(m.source_tables || [])],
    target_table: m.target_table || m.source_tables?.[0] || '',
    key_columns: [...(m.key_columns || [])],
    shard_keys: [...(m.shard_keys || [])],
    partition_column: m.partition_column || '',
    is_shard_group: !!m.is_shard_group,
    shard_pattern: m.shard_pattern || '',
    source_db_scope: m.source_db_scope || 'all',
    source_db_names: [...(m.source_db_names || [])],
    sync_table_mode: normalizeSyncTableMode(m.sync_table_mode || m.sync_mode),
    open: m.open !== false,
  }))
  if (!mappings.length && (formTables || []).length) {
    mappings = (formTables || []).map((t) => ({
      key: `m_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
      source_tables: [t],
      target_table: tableMap?.[t] || t,
      key_columns: [],
      shard_keys: [],
      partition_column: '',
      is_shard_group: false,
      shard_pattern: '',
      source_db_scope: 'all',
      source_db_names: [],
      sync_table_mode: 'incremental',
      open: false,
    }))
  }
  return mappings
}

// ---- 已解析分库 ----
export function resolvedShardDbsOf(form: CdcForm, sourceDbs: string[]): string[] {
  if (form.source_scope_mode !== 'multi_db') return form.source_db ? [form.source_db] : []
  return filterShardDatabases(sourceDbs, form.source_db_prefix)
}

export function syncResolvedShardDbs(form: CdcForm, sourceDbs: string[]): string[] {
  const scope = resolveSourceScope({
    mode: form.source_scope_mode,
    representativeDb: form.source_db,
    dbPrefix: form.source_db_prefix,
    names: sourceDbs,
  })
  return scope.resolved_db_names
}

// ---- alter diff 行 ----
export function alterDiffRowsOf(alterDiffItems: any[]): any[] {
  const rows: any[] = []
  for (const it of alterDiffItems || []) {
    for (const c of it.columns_to_add || []) {
      rows.push({ ...c, target_table: it.target_table })
    }
  }
  return rows
}

// ---- 父任务映射 ----
export function parentMappingsOf(
  parentMappingRows: any[],
  parent: any,
  parentTables: string[],
): any[] {
  const live = (parentMappingRows || []).filter((m) => m.status !== 'deleted')
  if (live.length) return live
  const extra = decodeCdcExtra(parent?.description).extra
  const maps = extra.mappings || []
  if (maps.length) return maps
  return (parentTables || []).map((t) => ({ target_table: extra.table_map?.[t] || t, source_tables: [t] }))
}

export function canAutoAlterOf(parent: any): boolean {
  if (!parent) return false
  const extra = decodeCdcExtra(parent.description).extra
  return extra.auto_create_table !== false && !!(parent.target_instance_id && parent.target_db)
}

// ---- payload 构造依赖 ----
export interface PayloadDeps {
  form: CdcForm
  mappings: CdcMapping[]
  resolvedShardDbs: string[]
  alterDiffRows: any[]
  parentMappings: any[]
  infraDefaults: InfraDefaults
  debeziumPreview: any
  sqlPreviews: SqlPreviewItem[]
}

export function extraPayload(deps: PayloadDeps): Record<string, any> {
  const { form, mappings, resolvedShardDbs, alterDiffRows, parentMappings, infraDefaults, debeziumPreview, sqlPreviews } =
    deps
  const multi = isMultiDbScope(form.source_scope_mode, resolvedShardDbs)
  const selectedCols = (alterDiffRows || []).filter((c) => c.selected)
  const alterTables = form.tables.map((tgt) => ({
    target_table: tgt,
    source_tables: parentMappings.find((m) => m.target_table === tgt)?.source_tables || [tgt],
    columns: selectedCols
      .filter((c) => c.target_table === tgt)
      .map((c) => ({ name: c.name, pg_type: c.pg_type, sr_type: c.sr_type, selected: true })),
  }))
  const overrides = collectSqlOverrides(sqlPreviews)
  return {
    env: form.env,
    source_engine: form.source_engine || 'postgres',
    mysql_server_id: form.source_engine === 'mysql' ? form.mysql_server_id || '5400' : undefined,
    source_instance_id: instanceID(form.source_instance_id),
    target_instance_id: instanceID(form.target_instance_id),
    source_schema: form.source_engine === 'mysql' ? form.source_schema || form.source_db || '' : form.source_schema,
    source_scope_mode: form.source_scope_mode,
    source_db_prefix: form.source_db_prefix,
    source_db_names: multi ? resolvedShardDbs : form.source_db ? [form.source_db] : [],
    shard_db: multi,
    representative_db: form.source_db,
    tables: form.tables,
    table_map: form.table_map,
    mappings: mappings.map((m) => ({
      source_tables: m.source_tables,
      target_table: m.target_table,
      key_columns: m.key_columns,
      shard_keys: m.shard_keys || [],
      partition_column: m.partition_column || '',
      sr_dynamic_partition: m.sr_dynamic_partition,
      is_shard_group: m.is_shard_group,
      shard_pattern: m.shard_pattern,
      source_db_scope: m.source_db_scope || 'all',
      source_db_names: (m.source_db_scope === 'explicit' ? m.source_db_names : []) || [],
      sync_table_mode: normalizeSyncTableMode(m.sync_table_mode),
    })),
    connector_id: form.connector_id,
    message_schema_mode: form.message_schema_mode,
    debezium_mode: form.debezium_mode,
    snapshot_mode: form.snapshot_mode,
    filter_delete_events: form.filter_delete_events,
    kafka_bootstrap_servers: normalizeKafkaBootstrap(form.kafka_bootstrap_servers),
    routine_load_parallelism: positiveInt(form.routine_load_parallelism, infraDefaults.routine_load_parallelism),
    kafka_topic_partitions: positiveInt(form.kafka_topic_partitions, infraDefaults.kafka_topic_partitions),
    kafka_topic_replication_factor: positiveInt(
      form.kafka_topic_replication_factor,
      infraDefaults.kafka_topic_replication_factor,
    ),
    sr_auto_buckets: positiveInt(form.sr_auto_buckets, infraDefaults.sr_auto_buckets),
    sr_replication_num: positiveInt(form.sr_replication_num, infraDefaults.sr_replication_num),
    sr_dynamic_partition_enable: !!form.sr_dynamic_partition_enable,
    sr_partition_column: form.sr_partition_column,
    sr_dynamic_partition: buildSrDynamicPartitionObject(form),
    auto_create_table: form.auto_create_table,
    key_model: form.key_model,
    publication_name: form.publication_name || '',
    slot_name: form.slot_name || '',
    connector_name: form.connector_name || '',
    topic_prefix: form.topic_prefix || debeziumPreview?.topic_prefix || '',
    debezium_template: debeziumPreview?.template || '',
    debezium_config: debeziumPreview?.config || undefined,
    alter_columns: selectedCols.map((c) => c.name).join(',') || form.alter_columns,
    alter_schema_tables: form.task_type === 'alter_schema' ? alterTables : undefined,
    alter_schema_exec_mode: form.alter_schema_exec_mode,
    kafka_default_offsets: form.kafka_default_offsets,
    sql_overrides: Object.keys(overrides).length ? overrides : undefined,
    parent_ids: form.parent_ids,
  }
}

export function submitBody(deps: PayloadDeps): Record<string, any> {
  const { form } = deps
  const extra = extraPayload(deps)
  return {
    name: form.name,
    task_name: form.name,
    task_type: form.task_type,
    parent_id: form.parent_id,
    parent_task_id: form.parent_id,
    parent_ids: form.parent_ids,
    source_instance_id: instanceID(form.source_instance_id),
    target_instance_id: instanceID(form.target_instance_id),
    source_db: form.source_db,
    source_db_name: form.source_db,
    target_db: form.target_db,
    target_db_name: form.target_db,
    description: form.description,
    reviewer: form.reviewer,
    approver: form.reviewer,
    assigned_to: form.reviewer,
    ...extra,
  }
}

// ---- 校验：进入下一步（返回 '' 表示通过，否则为错误提示） ----
export function validateInitStep0(form: CdcForm, resolvedShardDbs: string[]): string {
  if (!form.name) return '请输入任务名称'
  if (!form.env) return '请选择环境'
  if (!instanceID(form.source_instance_id)) return '请选择源端实例'
  if (!form.source_db) return '请选择数据库'
  if (form.source_engine !== 'mysql' && !form.source_schema) return '请输入 Schema'
  if (form.source_engine === 'mysql') {
    const sid = Number(form.mysql_server_id)
    if (!Number.isFinite(sid) || sid <= 0) return '请填写有效的 MySQL Debezium server.id'
  }
  if (form.source_scope_mode === 'multi_db' && resolvedShardDbs.length < 2) {
    return '多分库至少匹配 2 个库，请检查分库前缀'
  }
  return ''
}

export function validateTableStep(form: CdcForm, mappings: CdcMapping[]): string {
  if (!form.tables.length) return '请至少选择一张表'
  if (form.key_model !== 'duplicate_key' && mappings.some((m) => !m.key_columns.length)) {
    return '存在映射未配置 KEY 列，请展开映射或点击「加载全部字段」后勾选 KEY 列'
  }
  return ''
}

export function validateDebeziumStep(form: CdcForm): string {
  if (!form.message_schema_mode) return '请完善 Debezium 配置'
  if (!form.connector_id) return '请完善 Debezium 配置'
  if (!form.debezium_mode) return '请完善 Debezium 配置'
  if (!form.snapshot_mode) return '请完善 Debezium 配置'
  return ''
}

export function validateTargetStep(form: CdcForm): string {
  if (form.auto_create_table) {
    if (!instanceID(form.target_instance_id)) return '请选择目标端 StarRocks 实例'
    if (!form.target_db) return '自动建表开启时，目标库必填'
  }
  return ''
}

// ---- 标签映射 ----
export function typeTextTag(v: string): string {
  return { init: '初始化', add_table: '新增表', remove_table: '移除表', alter_schema: '源表加列' }[v] || v || '—'
}

export function typeTagColor(v: string): string {
  return (
    { init: 'blue', add_table: 'green', remove_table: 'orange', alter_schema: 'purple' }[v] || 'default'
  )
}

export function envTagColor(e: string): string {
  if (e === 'prod' || e === '生产') return 'red'
  if (e === 'test' || e === '测试') return 'green'
  return 'blue'
}

export function statusTagColor(s: string): string {
  if (s === 'running') return 'success'
  if (s === 'paused' || s === 'pending' || s === 'init' || s === 'creating' || s === 'provisioning') return 'warning'
  if (s === 'error' || s === 'failed' || s === 'rejected') return 'error'
  return 'default'
}

export function statusTextLabel(s: string): string {
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
    }[s] || s || '—'
  )
}

// ---- 校验：父任务归属同一实例 ----
export function typeAllowed(type: string): boolean {
  return !!PAGE_TITLES[type]
}

export function resolveEffectivePartitionColumnSafe(taskCol: any, mapCol: any): string {
  return resolveEffectivePartitionColumn(taskCol, mapCol)
}
