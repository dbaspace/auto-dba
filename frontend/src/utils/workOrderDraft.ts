/** 仪表盘「我的草稿」：本机 localStorage，按用户 + 工单 type 分桶，每种类型最多 1 条 */
import { Modal } from 'antd'
import type { NavigateFunction } from 'react-router-dom'

export const WORK_ORDER_DRAFTS_BY_TYPE_KEY = 'auto-dba.workOrder.draftsByType.v1'

export interface WorkOrderCreateOption {
  type: string
  label: string
  path: string
}

export const WORK_ORDER_CREATE_OPTIONS: WorkOrderCreateOption[] = [
  { type: 'ddl', label: 'DDL工单', path: '/ops/ddl' },
  { type: 'dml', label: 'DML工单', path: '/ops/dml' },
  { type: 'kafka', label: 'Kafka工单', path: '/ops/kafka' },
  { type: 'redis', label: 'Redis工单', path: '/ops/redis' },
  { type: 'dcl', label: '权限申请', path: '/ops/dcl' },
  { type: 'createdb', label: '新增数据库', path: '/ops/createdb' },
  { type: 'dql', label: '数据导出', path: '/ops/dql' },
  { type: 'archive', label: '归档工单', path: '/ops/archive/apply' },
  { type: 'multi', label: '多语模板', path: '/ops/multilang' },
  { type: 'cdc', label: 'CDC同步任务', path: '/ops/cdc/create' },
  { type: 'migration', label: '数据迁移', path: '/ops/migration/create' },
]

const LEGACY_PAGE_DRAFTS: Record<string, { store: 'sessionStorage' | 'localStorage'; key: string }> = {
  ddl: { store: 'sessionStorage', key: 'auto-dba-ddl-draft' },
  dml: { store: 'sessionStorage', key: 'auto-dba-dml-draft' },
  createdb: { store: 'sessionStorage', key: 'auto-dba-createdb-draft' },
  kafka: { store: 'localStorage', key: 'auto-dba-kafka-draft' },
  redis: { store: 'localStorage', key: 'auto-dba-redis-draft' },
  dcl: { store: 'localStorage', key: 'auto-dba-dcl-draft' },
  dql: { store: 'sessionStorage', key: 'auto-dba-dql-draft' },
  archive: { store: 'localStorage', key: 'auto-dba-archive-draft' },
  cdc: { store: 'localStorage', key: 'auto-dba-cdc-create-draft' },
  migration: { store: 'localStorage', key: 'auto-dba-migration-draft' },
}

const IGNORE_MEANINGFUL_KEYS = new Set([
  'assigned_to', 'reviewer', 'assigned_approver', 'approver', 'scheduled_at', 'is_debug',
  'action', 'cleanup_policy', 'retention', 'partition', 'replication', 'step',
  'task_type', 'debezium_mode', 'message_schema_mode', 'key_model',
  'auto_create_table', 'shard_db', 'shard_table', 'start_mode', 'migration_mode',
  'schema_strategy', 'conflict_strategy', 'transfer_mode', 'batch_size', 'speed_limit',
  'run_mode', 'need_sensitive_data', 'row_count', 'columns',
])

export const DRAFTS_CHANGED_EVENT = 'work-order-drafts-changed'

export interface WorkOrderDraft {
  type: string
  label: string
  path: string
  hint: string
  values: Record<string, any>
  updatedAt: string
}

export function resolveDraftOwnerId(user: any): string {
  return String(user?.id || user?.username || '').trim()
}

export function currentDraftOwnerId(): string {
  try {
    return resolveDraftOwnerId(JSON.parse(localStorage.getItem('user') || '{}'))
  } catch {
    return ''
  }
}

export function draftsStorageKey(userId: string): string {
  return `${WORK_ORDER_DRAFTS_BY_TYPE_KEY}.${userId}`
}

function isPlainObject(v: any): boolean {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

export function serializeDraftValues(input: any): Record<string, any> {
  if (!input) return {}
  try {
    return JSON.parse(
      JSON.stringify(input, (_key, value) => {
        if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value.toISOString()
        return value
      }),
    )
  } catch {
    return {}
  }
}

function normalizeDraft(raw: any): WorkOrderDraft | null {
  if (!raw || typeof raw !== 'object') return null
  const type = String(raw.type || '').trim()
  const path = String(raw.path || '').trim()
  if (!type || !path) return null
  return {
    type,
    label: String(raw.label || type),
    path,
    hint: String(raw.hint || ''),
    values: isPlainObject(raw.values) ? raw.values : {},
    updatedAt: String(raw.updatedAt || ''),
  }
}

function parseMapRaw(raw: string | null): Record<string, WorkOrderDraft> {
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Record<string, WorkOrderDraft> = {}
    for (const [k, v] of Object.entries(parsed)) {
      const d = normalizeDraft(v)
      if (d) out[d.type || k] = d
    }
    return out
  } catch {
    return {}
  }
}

function optionOf(type: string): WorkOrderCreateOption | null {
  return WORK_ORDER_CREATE_OPTIONS.find((o) => o.type === type) || null
}

function readLegacyValues(type: string): Record<string, any> | null {
  const spec = LEGACY_PAGE_DRAFTS[type]
  if (!spec) return null
  try {
    const raw = window[spec.store]?.getItem(spec.key)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
}

function removeLegacy(type: string) {
  const spec = LEGACY_PAGE_DRAFTS[type]
  if (!spec) return
  try {
    window[spec.store]?.removeItem(spec.key)
  } catch {
    /* ignore */
  }
}

/** 退出登录时清掉各工单页共用的旧草稿键（不按用户分桶）。 */
export function clearSharedLegacyDrafts() {
  Object.keys(LEGACY_PAGE_DRAFTS).forEach(removeLegacy)
}

function notifyDraftsChanged() {
  try {
    window.dispatchEvent(new CustomEvent(DRAFTS_CHANGED_EVENT))
  } catch {
    /* ignore */
  }
}

function migrateLegacyIntoUser(userId: string) {
  try {
    const key = draftsStorageKey(userId)
    const map = parseMapRaw(localStorage.getItem(key))
    let changed = false
    for (const opt of WORK_ORDER_CREATE_OPTIONS) {
      if (map[opt.type]) {
        removeLegacy(opt.type)
        continue
      }
      const values = readLegacyValues(opt.type)
      if (!values) continue
      map[opt.type] = {
        type: opt.type,
        label: opt.label,
        path: opt.path,
        hint: '',
        values: serializeDraftValues(values),
        updatedAt: new Date().toISOString(),
      }
      changed = true
      removeLegacy(opt.type)
    }
    if (changed) localStorage.setItem(key, JSON.stringify(map))
  } catch {
    /* ignore */
  }
}

function readMap(userId: string): Record<string, WorkOrderDraft> {
  const uid = String(userId || '').trim()
  if (!uid) return {}
  try {
    migrateLegacyIntoUser(uid)
    return parseMapRaw(localStorage.getItem(draftsStorageKey(uid)))
  } catch {
    return {}
  }
}

function writeMap(userId: string, map: Record<string, WorkOrderDraft>): boolean {
  const uid = String(userId || '').trim()
  if (!uid) return false
  try {
    const key = draftsStorageKey(uid)
    if (!Object.keys(map).length) localStorage.removeItem(key)
    else localStorage.setItem(key, JSON.stringify(map))
    notifyDraftsChanged()
    return true
  } catch {
    return false
  }
}

function isFilled(val: any): boolean {
  if (val == null || val === '') return false
  if (typeof val === 'boolean') return false
  if (typeof val === 'number') return false
  if (Array.isArray(val)) return val.length > 0
  if (typeof val === 'object')
    return Object.entries(val).some(([k, v]) => !IGNORE_MEANINGFUL_KEYS.has(k) && isFilled(v))
  return String(val).trim() !== ''
}

export function hasMeaningfulDraftValues(values: any): boolean {
  const v = isPlainObject(values) ? values : {}
  const merged = isPlainObject(v.form) ? { ...v, ...v.form } : v
  for (const [k, val] of Object.entries(merged)) {
    if (IGNORE_MEANINGFUL_KEYS.has(k) || k === 'form' || k === 'mappings') continue
    if (isFilled(val)) return true
  }
  if (Array.isArray(merged.mappings) && merged.mappings.length) return true
  return false
}

export function loadWorkOrderDraft(type: string, userId = currentDraftOwnerId()): WorkOrderDraft | null {
  const t = String(type || '').trim()
  if (!t || !String(userId || '').trim()) return null
  return readMap(userId)[t] || null
}

export function loadAllWorkOrderDrafts(userId = currentDraftOwnerId()): WorkOrderDraft[] {
  if (!String(userId || '').trim()) return []
  return Object.values(readMap(userId)).sort((a, b) => {
    const ta = a.updatedAt || ''
    const tb = b.updatedAt || ''
    return ta < tb ? 1 : ta > tb ? -1 : 0
  })
}

export function hasWorkOrderDraft(type: string, userId = currentDraftOwnerId()): boolean {
  return !!loadWorkOrderDraft(type, userId)
}

export function saveWorkOrderDraft(
  draft: Partial<WorkOrderDraft>,
  userId = currentDraftOwnerId(),
  opts: { force?: boolean } = {},
): boolean {
  const uid = String(userId || '').trim()
  const type = String(draft.type || '').trim()
  const path = String(draft.path || '').trim()
  if (!uid || !type || !path) return false
  const values = serializeDraftValues(draft.values)
  const map = readMap(uid)
  if (!opts.force && !hasMeaningfulDraftValues(values)) {
    if (!map[type]) return true
    delete map[type]
    removeLegacy(type)
    return writeMap(uid, map)
  }
  map[type] = {
    type,
    label: String(draft.label || type),
    path,
    hint: String(draft.hint || ''),
    values,
    updatedAt: new Date().toISOString(),
  }
  removeLegacy(type)
  return writeMap(uid, map)
}

/** 有 type 清一条；无 type 清空该用户全部 */
export function clearWorkOrderDraft(type?: string, userId = currentDraftOwnerId()) {
  const uid = String(userId || '').trim()
  if (!uid) return
  try {
    const t = String(type || '').trim()
    if (!t) {
      localStorage.removeItem(draftsStorageKey(uid))
      Object.keys(LEGACY_PAGE_DRAFTS).forEach(removeLegacy)
      notifyDraftsChanged()
      return
    }
    removeLegacy(t)
    const map = readMap(uid)
    if (!(t in map)) {
      notifyDraftsChanged()
      return
    }
    delete map[t]
    writeMap(uid, map)
  } catch {
    /* ignore */
  }
}

export function summarizeWorkOrderDraft(draft: WorkOrderDraft | null): string {
  if (!draft) return ''
  const v = draft.values || {}
  const nested = isPlainObject(v.form) ? v.form : {}
  const candidates = [
    v.sql_content, v.sql, v.statement, v.name, v.description, v.topic, v.username,
    nested.sql_content, nested.sql, nested.name, nested.description, nested.topic, nested.username,
  ]
  for (const c of candidates) {
    const s = String(c ?? '').trim()
    if (s) return s.length > 120 ? `${s.slice(0, 120)}…` : s
  }
  return '（已填写部分字段）'
}

/**
 * 该工单类型在当前用户本机已有草稿时拦截「再开一张同类型」。
 * @returns true 可继续空白新建；false 已拦截
 */
export function allowOpenTypeOrRestore(type: string, label: string, onRestore?: () => void): Promise<boolean> {
  const t = String(type || '').trim()
  const uid = currentDraftOwnerId()
  if (!t || !uid || !hasWorkOrderDraft(t, uid)) return Promise.resolve(true)
  const draft = loadWorkOrderDraft(t, uid)
  const name = draft?.label || label || t
  return new Promise<boolean>((resolve) => {
    Modal.confirm({
      title: '该类型已有草稿',
      content: `本机当前账号已有「${name}」草稿，不能再开一张同类型工单。请继续编辑该草稿，或先在「我的草稿」中清空后再新建。`,
      okText: '继续编辑',
      cancelText: '关闭',
      onOk() {
        onRestore?.()
        resolve(false)
      },
      onCancel() {
        resolve(false)
      },
    })
  })
}

export async function navigateToWorkOrderCreate(
  navigate: NavigateFunction,
  type: string,
  query?: Record<string, any>,
) {
  const opt = optionOf(type)
  const path = opt?.path || `/ops/${type}`
  const search = query ? '?' + new URLSearchParams(query as Record<string, string>).toString() : ''
  const go = () => navigate(path + search)
  if (!opt) {
    go()
    return
  }
  const ok = await allowOpenTypeOrRestore(opt.type, opt.label, go)
  if (ok) go()
}
