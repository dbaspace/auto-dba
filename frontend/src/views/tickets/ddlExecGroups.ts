export type DdlExecTab = 'all' | 'ok' | 'fail'

export interface DdlExecGroup {
  key: string
  instanceId: string
  label: string
  path: string
  ok: boolean
  error: string
  rolledBack: number
  rows: {
    key: string
    sql: string
    status: string
    error: string
    start: string
    end: string
    cost: string
    affected: string | number
  }[]
}

export interface DdlExecStats {
  totalSQL: number
  successSQL: number
  rolledBackSQL: number
  instTotal: number
  instOk: number
  instFail: number
  /** 后端计划条数（SplitSQL）；有值时摘要优先用此口径 */
  statementCount?: number
  /** 明细是否已抽样收敛 */
  compacted?: boolean
  /** 摘要数字是否来自后端 success_count（而非 records 长度） */
  countsFromBackend?: boolean
}

export const NO_DETAIL_ERROR = '执行失败但未记录错误详情，请联系管理员查看后端日志'

function fmtTimeMs(v: any): string {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  const p = (n: number) => String(n).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${ms}`
}

function costMs(start: string, end: string): string {
  if (!start || !end) return '-'
  const a = Date.parse(start)
  const b = Date.parse(end)
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return '-'
  return String(b - a)
}

/** 整单已失败：工单 failed 或 execute_result.success === false */
export function isTicketExecFailed(opts: { execFailed?: boolean; execSuccess?: boolean }): boolean {
  if (opts.execFailed === true) return true
  if (opts.execSuccess === false) return true
  return false
}

export function buildDdlExecGroups(
  records: any[] | undefined,
  opts: {
    env?: string
    envByInstanceId?: Record<string, string>
    fallbackDb?: string
    fallbackHost?: string
    execMessage?: string
    execFailed?: boolean
    execSuccess?: boolean
    successCount?: number
    statementCount?: number
    compacted?: boolean
  },
): { groups: DdlExecGroup[]; stats: DdlExecStats } {
  const recs = Array.isArray(records) ? records : []
  const fallbackEnv = String(opts.env || '-').trim() || '-'
  const envById = opts.envByInstanceId || {}
  const ticketFailed = isTicketExecFailed(opts)
  const map = new Map<string, any[]>()
  const order: string[] = []

  for (const r of recs) {
    const iid = String(r.instance_id || '').trim()
    const host = String(r.host || opts.fallbackHost || '').trim()
    const db = String(r.db || opts.fallbackDb || '').trim()
    const key = iid || `${host}|${db}` || 'unknown'
    if (!map.has(key)) {
      map.set(key, [])
      order.push(key)
    }
    map.get(key)!.push(r)
  }

  // 无 records 但有整单消息或已失败时，仍展示一个占位块，避免失败却只显示「暂无数据」
  if (!order.length && (opts.execMessage || ticketFailed)) {
    order.push('gate')
    map.set('gate', [
      {
        sql: '(执行)',
        status: 'failed',
        error_message: opts.execMessage || NO_DETAIL_ERROR,
        host: opts.fallbackHost || '',
        db: opts.fallbackDb || '',
      },
    ])
  }

  const groups: DdlExecGroup[] = order.map((key) => {
    const list = map.get(key) || []
    const first = list[0] || {}
    const host = String(first.host || opts.fallbackHost || '').trim()
    const port = first.port ? String(first.port) : ''
    const db = String(first.db || opts.fallbackDb || '').trim() || '-'
    const name = String(first.instance_name || '').trim()
    const hostPort = host ? (port ? `${host}:${port}` : host) : name || key
    const label = db && hostPort ? `${db} - ${hostPort}` : hostPort || db || key
    const iid = String(first.instance_id || '').trim()
    const env = (iid && String(envById[iid] || '').trim()) || fallbackEnv
    const path = `${env} / ${db}${hostPort ? ` · ${hostPort}` : ''}`
    const failed = list.filter((r) => String(r.status || '').toLowerCase() === 'failed')
    const rolledBack = list.filter((r) => String(r.status || '').toLowerCase() === 'rolled_back').length
    // 有 failed / rolled_back，或整单已失败时，实例不得标成功（防御脏明细全 success）
    const ok = list.length > 0 && failed.length === 0 && rolledBack === 0 && !ticketFailed
    const error =
      (failed[0] && String(failed[0].error_message || '').trim()) ||
      (!ok ? String(opts.execMessage || '').trim() || NO_DETAIL_ERROR : '') ||
      ''

    const rows = list.map((r, i) => {
      const start = r.started_at || ''
      const end = r.ended_at || ''
      return {
        key: `${key}-${i}`,
        sql: r.sql || '-',
        status: String(r.status || '').toLowerCase(),
        error: r.error_message ? String(r.error_message) : '-',
        start: fmtTimeMs(start),
        end: fmtTimeMs(end),
        cost: costMs(start, end),
        affected: r.affected_row ?? 0,
      }
    })

    return {
      key,
      instanceId: String(first.instance_id || ''),
      label,
      path,
      ok,
      error,
      rolledBack,
      rows,
    }
  })

  let successSQL = recs.filter((r) => String(r.status || '').toLowerCase() === 'success').length
  if (!recs.length && groups.length) {
    successSQL = 0
  }
  let totalSQL = recs.length || groups.reduce((n, g) => n + g.rows.length, 0)
  const backendSuccess = Number(opts.successCount)
  const backendPlanned = Number(opts.statementCount)
  let countsFromBackend = false
  if (Number.isFinite(backendSuccess) && backendSuccess >= 0) {
    successSQL = backendSuccess
    countsFromBackend = true
  }
  if (Number.isFinite(backendPlanned) && backendPlanned > 0) {
    totalSQL = backendPlanned
    countsFromBackend = true
  } else if (countsFromBackend && successSQL > totalSQL) {
    totalSQL = successSQL
  }

  const instOk = groups.filter((g) => g.ok).length
  const instFail = groups.filter((g) => !g.ok).length

  return {
    groups,
    stats: {
      totalSQL,
      successSQL,
      rolledBackSQL: groups.reduce((n, g) => n + g.rolledBack, 0),
      instTotal: groups.length,
      instOk,
      instFail,
      statementCount: Number.isFinite(backendPlanned) && backendPlanned > 0 ? backendPlanned : undefined,
      compacted: Boolean(opts.compacted),
      countsFromBackend,
    },
  }
}
