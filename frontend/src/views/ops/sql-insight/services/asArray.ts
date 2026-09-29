/** 把接口 data 规范成数组，避免 antd Table dataSource.some 报错。 */
export function asArray(v: unknown): any[] {
  if (Array.isArray(v)) return v
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>
    if (Array.isArray(o.list)) return o.list
    if (Array.isArray(o.data)) return o.data
    if (Array.isArray(o.items)) return o.items
    if (Array.isArray(o.rows)) return o.rows
  }
  return []
}

export function asTotal(v: unknown, fallback = 0): number {
  if (typeof v === 'number' && Number.isFinite(v)) return v
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>
    const t = o.total ?? (o.pagination as any)?.total
    if (typeof t === 'number' && Number.isFinite(t)) return t
  }
  return fallback
}
