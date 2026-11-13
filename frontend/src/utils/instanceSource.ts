/** 工单数据源：按域名管理（access-domains）或实例 domains 展开（非 host） */

export type DomainSource = {
  key: string
  instance_id: string
  domain: string
  name: string
}

export type AccessDomainRow = {
  id?: string
  instance_id?: string
  instance_name?: string
  domain?: string
  env_code?: string
  db_type?: string
}

/** 与后端 NormalizeEngineType / AccessDomains.typeKey 对齐 */
export function engineTypeKey(t?: string) {
  const n = String(t || '')
    .trim()
    .toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'mssql') return 'sqlserver'
  if (n === 'cockroach' || n === 'cockroadchdb') return 'cockroachdb'
  return n
}

function domainKey(s: string) {
  return String(s || '').trim().replace(/\.+$/, '').toLowerCase()
}

function instanceDomains(inst: any): string[] {
  const list = Array.isArray(inst?.domains)
    ? (inst.domains as unknown[]).map((t) => String(t).trim()).filter(Boolean)
    : []
  if (list.length) {
    const seen = new Set<string>()
    const out: string[] = []
    for (const d of list) {
      const k = domainKey(d)
      if (!k || seen.has(k)) continue
      seen.add(k)
      out.push(d)
    }
    return out
  }
  const single = String(inst?.domain_name || '').trim()
  return single ? [single] : []
}

export function sourceKey(instanceId: string, domain: string) {
  return `${instanceId}::${domain}`
}

export function parseSourceKey(key: string): { instance_id: string; domain: string } | null {
  const s = String(key || '')
  const i = s.indexOf('::')
  if (i <= 0) return null
  const instance_id = s.slice(0, i).trim()
  const domain = s.slice(i + 2).trim()
  if (!instance_id || !domain) return null
  return { instance_id, domain }
}

/** 一实例多域名 → 多条数据源选项；无域名则跳过（不展示纯 host） */
export function expandDomainSources(instances: any[]): DomainSource[] {
  const out: DomainSource[] = []
  for (const inst of instances || []) {
    const id = String(inst?.id || '').trim()
    if (!id) continue
    const name = String(inst?.name || '').trim()
    for (const domain of instanceDomains(inst)) {
      out.push({
        key: sourceKey(id, domain),
        instance_id: id,
        domain,
        name,
      })
    }
  }
  return out
}

/** 从域名管理列表构建工单数据源选项（可见性已由 /access-domains 按租户过滤） */
export function sourcesFromAccessDomains(rows: AccessDomainRow[]): DomainSource[] {
  const out: DomainSource[] = []
  const seen = new Set<string>()
  for (const row of rows || []) {
    const instance_id = String(row?.instance_id || '').trim()
    const domain = String(row?.domain || '').trim()
    if (!instance_id || !domain) continue
    const key = sourceKey(instance_id, domain)
    if (seen.has(key)) continue
    seen.add(key)
    out.push({
      key,
      instance_id,
      domain,
      name: String(row?.instance_name || '').trim(),
    })
  }
  return out
}

/** 按环境码 / 引擎类型过滤域名行 */
export function filterAccessDomainRows(
  rows: AccessDomainRow[],
  opts: { env?: string; dbType?: string } = {},
): AccessDomainRow[] {
  const env = String(opts.env || '').trim()
  const wantType = engineTypeKey(opts.dbType)
  return (rows || []).filter((r) => {
    if (env && String(r.env_code || '').trim() !== env) return false
    if (wantType && engineTypeKey(r.db_type) !== wantType) return false
    return true
  })
}

export function sourcePrimaryLabel(row: DomainSource) {
  return row.domain
}

export function sourceSecondaryLabel(row: DomainSource) {
  return row.name || ''
}

/** 库自身绑定的域名：domains 数组 + tags 中 domain=；均无则视为未绑定 */
export function dbBoundDomains(db: any): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  const add = (raw: unknown) => {
    const v = String(raw || '').trim()
    if (!v) return
    const k = domainKey(v)
    if (!k || seen.has(k)) return
    seen.add(k)
    out.push(v)
  }
  if (Array.isArray(db?.domains)) {
    for (const d of db.domains) add(d)
  }
  if (Array.isArray(db?.tags)) {
    for (const raw of db.tags) {
      const t = String(raw || '').trim()
      const i = t.indexOf('=')
      if (i <= 0) continue
      if (t.slice(0, i).trim().toLowerCase() === 'domain') add(t.slice(i + 1))
    }
  }
  return out
}

/**
 * 已登记库用于导出数据源的域名。
 * 先 domains，再标签 domain=；库本身没绑定时才用实例主域名 domain_name。
 */
export function registeredDbDomains(db: any, inst?: { domain_name?: string } | null): string[] {
  const bound = dbBoundDomains(db)
  if (bound.length) return bound
  const primary = String(inst?.domain_name || '').trim()
  return primary ? [primary] : []
}

/**
 * 从查询进入导出时补选数据源。已有选中值则保持。
 * 一条域名直接选中；多条时优先和当前环境下拉选项重合的，否则第一条。没有绑定域名则不选。
 */
export function pickRegisteredExportDomain(
  domains: string[],
  optionDomains: string[] = [],
  current = '',
): string {
  const cur = String(current || '').trim()
  if (cur) return cur
  const list = (domains || []).map((d) => String(d || '').trim()).filter(Boolean)
  if (!list.length) return ''
  if (list.length === 1) return list[0]
  const opts = new Set((optionDomains || []).map((d) => domainKey(d)).filter(Boolean))
  return list.find((d) => opts.has(domainKey(d))) || list[0]
}

/**
 * 库记录是否匹配指定域名（大小写不敏感）。
 * 未绑定任何域名时视为匹配（与后端提交校验：空 domains 放行一致）。
 */
export function dbHasDomain(db: any, domain: string): boolean {
  const want = domainKey(domain)
  if (!want) return false
  const list = dbBoundDomains(db)
  if (!list.length) return true
  return list.some((d) => domainKey(d) === want)
}

export function filterDbsByDomain(dbs: any[], domain: string): any[] {
  const want = String(domain || '').trim()
  if (!want) return []
  return (dbs || []).filter((d) => dbHasDomain(d, want))
}
