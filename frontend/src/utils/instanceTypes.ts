export type InstanceTypeOption = {
  value: string
  label: string
}

export type InstanceTypeGroup = {
  id: string
  title: string
  values: string[]
}

export const INSTANCES_PATH = '/config/instances'

/** 与诊断菜单引擎分组对齐；未知类型归入「其他」 */
export const INSTANCE_TYPE_GROUPS: InstanceTypeGroup[] = [
  {
    id: 'rdbms',
    title: '关系型数据库',
    values: ['mysql', 'postgres', 'oracle', 'sqlserver', 'oceanbase'],
  },
  {
    id: 'dist',
    title: '分布式 SQL',
    values: ['tidb', 'cockroachdb'],
  },
  {
    id: 'nosql',
    title: '分析与 NoSQL',
    values: ['starrocks', 'clickhouse', 'redis', 'mongodb', 'cassandra', 'elasticsearch'],
  },
  {
    id: 'infra',
    title: '中间件与基础设施',
    values: ['kafka', 'host'],
  },
]

export function isInstancesPath(path?: string) {
  const p = String(path || '').replace(/\/+$/, '') || '/'
  return p === INSTANCES_PATH
}

export function parseInstanceTypes(raw: string): InstanceTypeOption[] {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    const seen = new Set<string>()
    const out: InstanceTypeOption[] = []
    for (const it of arr) {
      const value = String(it?.value ?? '').trim()
      if (!value || seen.has(value)) continue
      seen.add(value)
      out.push({ value, label: String(it?.label || value).trim() || value })
    }
    return out
  } catch {
    return []
  }
}

export function groupInstanceTypes(
  opts: InstanceTypeOption[],
): { title: string; items: InstanceTypeOption[] }[] {
  const byValue = new Map<string, InstanceTypeOption>()
  for (const o of opts) {
    const key = String(o.value || '').trim().toLowerCase()
    if (!key || byValue.has(key)) continue
    byValue.set(key, o)
  }

  const used = new Set<string>()
  const groups: { title: string; items: InstanceTypeOption[] }[] = []

  for (const g of INSTANCE_TYPE_GROUPS) {
    const items: InstanceTypeOption[] = []
    for (const v of g.values) {
      const hit = byValue.get(v)
      if (!hit) continue
      items.push(hit)
      used.add(v)
    }
    if (items.length) groups.push({ title: g.title, items })
  }

  const other: InstanceTypeOption[] = []
  for (const [key, opt] of byValue) {
    if (!used.has(key)) other.push(opt)
  }
  if (other.length) groups.push({ title: '其他', items: other })

  return groups
}

export function instanceTypeSearch(dbType: string) {
  const v = String(dbType || '').trim()
  return v ? `?db_type=${encodeURIComponent(v)}` : ''
}

export function dbTypeFromSearch(search?: string) {
  const s = String(search || '')
  const q = s.startsWith('?') ? s.slice(1) : s
  return new URLSearchParams(q).get('db_type')?.trim() || ''
}
