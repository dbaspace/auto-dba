/** 工单数据源：按实例登记的 domains 展开（非 host） */

export type DomainSource = {
  key: string
  instance_id: string
  domain: string
  name: string
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

export function sourcePrimaryLabel(row: DomainSource) {
  return row.domain
}

export function sourceSecondaryLabel(row: DomainSource) {
  return row.name || ''
}

/** 库记录是否绑定了指定域名（大小写不敏感） */
export function dbHasDomain(db: any, domain: string): boolean {
  const want = domainKey(domain)
  if (!want) return false
  const list = Array.isArray(db?.domains) ? db.domains : []
  return list.some((d: unknown) => domainKey(String(d)) === want)
}

export function filterDbsByDomain(dbs: any[], domain: string): any[] {
  const want = String(domain || '').trim()
  if (!want) return []
  return (dbs || []).filter((d) => dbHasDomain(d, want))
}
