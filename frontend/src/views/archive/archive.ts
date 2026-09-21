export const STRATEGY_OPTIONS = [
  { label: '直接删除', value: 'delete_only' },
  { label: '备份到目标实例', value: 'backup_target' },
  { label: '归档到 OSS', value: 'archive_oss' },
  { label: '自定义脚本', value: 'custom_script' },
]

export const RUN_MODE_OPTIONS = [
  { label: '立即执行', value: 'immediate' },
  { label: '定时任务', value: 'scheduled' },
]

export const SCRIPT_TYPE_OPTIONS = [
  { label: 'Shell', value: 'shell' },
  { label: 'Python 3+', value: 'python' },
  { label: 'Go', value: 'go' },
  { label: 'SQL', value: 'sql' },
]

export const PG_DELETE_MODE_OPTIONS = [
  { label: '自动（按预估数据量）', value: 'auto' },
  { label: 'CTE', value: 'cte' },
  { label: 'Shard', value: 'shard' },
]

export const SCRIPT_TEMPLATES: Record<string, string> = {
  sql: '-- 在此编写 SQL\nDELETE FROM t WHERE create_time < ...',
  python: 'def archive_delete():\n    # 删除逻辑\n    return deleted  # 整数行数；失败请 raise\n',
  go: `package main

// 填写区：host/dbname/tables 为空时用源端；删除条件 create_time < today-retainDays
var (
	host       = ""
	port       = 5432
	user       = ""
	password   = ""
	dbname     = ""
	tables     = ""
	retainDays = 30
	limit      = 1000
)

func main() {
}
`,
  shell: '# 在此编写 Shell 脚本\necho "archive"',
}

const META_RE = /^-- auto-dba-archive:([a-z_]+),([a-z_]+)\s*\n?/
const SCHED_RE = /^-- auto-dba-schedule:([^\n]*)\n?/m
const EXTRA_RE = /^-- auto-dba-extra:([^\n]*)\n?/m

export function strategyLabel(v: any): string {
  return STRATEGY_OPTIONS.find((x) => x.value === v)?.label || '备份到目标实例'
}

export function runModeLabel(v: any): string {
  return RUN_MODE_OPTIONS.find((x) => x.value === v)?.label || '立即执行'
}

function stripMeta(raw: any): string {
  return String(raw || '').replace(META_RE, '').replace(SCHED_RE, '').replace(EXTRA_RE, '').trim()
}

function compactExtra(extra: any): Record<string, any> {
  if (!extra || typeof extra !== 'object') return {}
  const out: Record<string, any> = {}
  for (const [k, v] of Object.entries(extra)) {
    if (v == null) continue
    const s = typeof v === 'string' ? v.trim() : v
    if (s === '' || s === false) continue
    out[k] = s
  }
  return out
}

export function encodeCondition(
  sql: any,
  strategy: any,
  runMode: any,
  schedule: any,
  extra?: any,
): string {
  const body = stripMeta(sql)
  let out = `-- auto-dba-archive:${strategy || 'backup_target'},${runMode || 'immediate'}\n`
  if ((runMode || 'immediate') === 'scheduled' && String(schedule || '').trim()) {
    out += `-- auto-dba-schedule:${String(schedule).trim()}\n`
  }
  const packed = compactExtra(extra)
  if (Object.keys(packed).length) {
    out += `-- auto-dba-extra:${JSON.stringify(packed)}\n`
  }
  return out + body
}

export function decodeMeta(condition: any): {
  strategy: string
  run_mode: string
  schedule_expr: string
  sql: string
  extra: Record<string, any>
} {
  const raw = String(condition || '')
  const m = raw.match(META_RE)
  const s = raw.match(SCHED_RE)
  const e = raw.match(EXTRA_RE)
  let extra: Record<string, any> = {}
  try {
    extra = e?.[1] ? JSON.parse(e[1]) : {}
  } catch {
    extra = {}
  }
  if (!extra || typeof extra !== 'object') extra = {}
  return {
    strategy: m?.[1] || extra.strategy || 'backup_target',
    run_mode: m?.[2] || extra.run_mode || 'immediate',
    schedule_expr: (s?.[1] || extra.schedule_expr || '').trim(),
    sql: stripMeta(raw),
    extra,
  }
}

export function isStockScript(content: any): boolean {
  const t = String(content || '').replace(/\s+$/g, '')
  if (!t.trim()) return true
  return Object.values(SCRIPT_TEMPLATES).some((tpl) => t === String(tpl).replace(/\s+$/g, ''))
}

export function isApproved(row: any): boolean {
  const a = row?.approval_status
  if (a === 'pending' || a === 'rejected' || a === 'canceled') return false
  if (!a && !row?.issue_id) return true
  return a === 'approved'
}

export function canRun(row: any): boolean {
  if (!row) return false
  if (row.status === 'running' || row.status === 'paused') return false
  return isApproved(row)
}

export function statusText(s: any): string {
  return (
    ({
      running: '执行中',
      executed: '成功',
      success: '成功',
      failed: '失败',
      paused: '已停用',
      pending: '待审批',
      scheduled: '定时待执行',
      rejected: '已驳回',
      canceled: '已撤销',
      approved: '已通过',
      init: '待执行',
    } as Record<string, string>)[s] ||
    s ||
    '-'
  )
}

export function statusType(s: any): string {
  if (s === 'executed' || s === 'success' || s === 'approved') return 'success'
  if (s === 'failed' || s === 'rejected') return 'danger'
  if (s === 'running' || s === 'pending') return 'warning'
  if (s === 'paused' || s === 'scheduled') return 'info'
  return 'info'
}

export function formatTime(v: any): string {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(
    d.getMinutes(),
  )}:${p(d.getSeconds())}`
}

export function sourceTarget(row: any, instances: any[] = []): string {
  const inst = instances.find((i) => i.id === row.source_instance_id)
  const host = inst ? `${inst.name || inst.host || inst.domain_name}` : (row.source_host || '')
  const db = row.source_db || ''
  const table = row.table_name || row.source_table || ''
  const meta = decodeMeta(row.condition)
  const extra = meta.extra || {}
  const src = [host, db].filter(Boolean).join(' / ') + (table && table !== 'script' ? `.${table}` : '')
  if (meta.strategy === 'custom_script') return `${host || src || '-'}（自定义脚本）`
  if (extra.oss_bucket) return `${src || '-'} → OSS:${extra.oss_bucket}`
  const tgt = [extra.target_host, extra.target_db, extra.target_table].filter(Boolean).join('/')
  if (tgt) return `${src || '-'} → ${tgt}`
  return src || '-'
}

export function scheduleLabel(row: any): string {
  const { run_mode, schedule_expr } = decodeMeta(row.condition)
  const mode = row.schedule_expr ? 'scheduled' : run_mode
  if (mode === 'scheduled') return row.schedule_expr || schedule_expr || '定时任务'
  return '立即执行'
}

export function approvalText(s: any): string {
  return (
    ({ pending: '待审批', approved: '已通过', rejected: '已驳回', canceled: '已撤销', none: '—' } as Record<
      string,
      string
    >)[s] ||
    s ||
    '—'
  )
}
