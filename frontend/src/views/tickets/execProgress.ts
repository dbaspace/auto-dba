export type ExecProgress = {
  total?: number
  done?: number
  failed?: number
  current_instance?: string
  current_db?: string
  stmt_index?: number
  stmt_total?: number
  session_id?: number
  phase?: string
  env?: string
  executor?: string
  job_id?: string
  executor_url?: string
  target_user?: string
  last_get_at?: string
  get_failures?: number
  last_error?: string
}

export type ProgressView = {
  title: string
  indeterminate: boolean
  percent: number
  sub: string
}

/** DDL 执行中的进度。单库按语句，多库按已结束库数，并把当前库的语句比例算进去。 */
export function ddlProgressView(p?: ExecProgress | null): ProgressView | null {
  if (!p || (p.phase !== 'preparing' && p.phase !== 'running')) return null
  if (p.phase === 'preparing') {
    return { title: '正在统计分库', indeterminate: true, percent: 0, sub: '' }
  }
  const total = Number(p.total || 0)
  const done = Number(p.done || 0)
  const stmtIndex = Number(p.stmt_index || 0)
  const stmtTotal = Number(p.stmt_total || 0)
  const failed = Number(p.failed || 0)
  const cur = [p.current_instance, p.current_db].filter(Boolean).join(' / ')
  let percent = 0
  let title = ''
  let sub = ''
  if (total <= 1) {
    title = `执行中 第 ${stmtIndex}/${stmtTotal || 0} 条`
    percent = stmtTotal > 0 ? Math.min(100, Math.round((stmtIndex / stmtTotal) * 100)) : 0
    if (cur) sub = cur
  } else {
    title = `执行中 ${done}/${total} 库`
    let frac = done
    if (stmtTotal > 0 && stmtIndex > 0 && done < total) frac += stmtIndex / stmtTotal
    percent = Math.min(100, Math.round((frac / Math.max(total, 1)) * 100))
    if (cur) sub = `当前：${cur} · 第 ${stmtIndex}/${stmtTotal || 0} 条`
  }
  if (failed > 0) sub = sub ? `${sub} · 失败 ${failed} 个库` : `失败 ${failed} 个库`
  if (p.session_id) sub = sub ? `${sub} · 当前会话 ${p.session_id}` : `当前会话 ${p.session_id}`
  const where = [p.env ? `环境 ${p.env}` : '', p.executor ? `执行器 ${p.executor}` : ''].filter(Boolean).join(' · ')
  if (where) sub = sub ? `${sub} · ${where}` : where
  return { title, indeterminate: false, percent, sub }
}
