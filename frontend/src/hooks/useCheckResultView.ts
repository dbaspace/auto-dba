import { useEffect, useMemo, useState } from 'react'
import { useCheckResultPagination } from './useCheckResultPagination'

export type CheckLevelFilter = 'all' | 'ok' | 'warn' | 'error'

export function normalizeCheckLevel(lv: string): 'ok' | 'warn' | 'error' | 'other' {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return 'error'
  if (x === 'WARN' || x === 'WARNING') return 'warn'
  if (x === 'INFO' || x === 'OK' || x === 'PASS') return 'ok'
  return 'other'
}

/**
 * SQL 检测结果：按错误等级过滤 + 视口分页。
 * 新检测结果写入时重置为「全部」。
 */
export function useCheckResultView<T extends { error_level?: string }>(rows: T[], reservePx = 400) {
  const [levelFilter, setLevelFilter] = useState<CheckLevelFilter>('all')

  // Reset filter when a new check result set arrives (length / first row identity).
  const rowsSig = `${rows.length}:${String(rows[0]?.error_level || '')}:${String((rows[0] as { message?: string })?.message || '').slice(0, 40)}`
  useEffect(() => {
    setLevelFilter('all')
  }, [rowsSig])

  const counts = useMemo(() => {
    let ok = 0
    let warn = 0
    let error = 0
    for (const r of rows) {
      const n = normalizeCheckLevel(String(r.error_level || ''))
      if (n === 'ok') ok++
      else if (n === 'warn') warn++
      else if (n === 'error') error++
    }
    return { ok, warn, error, total: rows.length }
  }, [rows])

  const filteredRows = useMemo(() => {
    if (levelFilter === 'all') return rows
    return rows.filter((r) => normalizeCheckLevel(String(r.error_level || '')) === levelFilter)
  }, [rows, levelFilter])

  const { pagination, rowNo, pageSize, current } = useCheckResultPagination(filteredRows.length, reservePx)

  return {
    filteredRows,
    levelFilter,
    setLevelFilter,
    counts,
    pagination,
    rowNo,
    pageSize,
    current,
  }
}
