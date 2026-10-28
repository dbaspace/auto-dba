import api from '../api/request'
import { engineTypeKey, type AccessDomainRow } from './instanceSource'

export type TicketEnvGroup = {
  code: string
  label: string
  codes: string[]
}

type Payload = {
  mode?: string
  groups?: TicketEnvGroup[]
}

export async function loadTicketEnvGroups(): Promise<TicketEnvGroup[]> {
  try {
    const { data } = await api.get('/tickets/env-groups')
    const payload = (data?.data || data) as Payload
    const groups = Array.isArray(payload?.groups) ? payload.groups : []
    return groups
      .map((g) => ({
        code: String(g?.code || '').trim(),
        label: String(g?.label || g?.code || '').trim(),
        codes: (g?.codes || []).map((c) => String(c || '').trim()).filter(Boolean),
      }))
      .filter((g) => g.code)
  } catch {
    return []
  }
}

export function codesOfGroup(groupCode: string, groups: TicketEnvGroup[]): string[] {
  const want = groupCode.trim().toLowerCase()
  if (!want) return []
  const g = groups.find((x) => x.code.toLowerCase() === want)
  return g?.codes || []
}

/** 草稿或链接里的具体编码（beijing）归到所属档；已经是档位编码则原样返回。 */
export function resolveTicketEnvValue(saved: string, groups: TicketEnvGroup[]): string {
  const v = String(saved || '').trim()
  if (!v) return ''
  const asGroup = groups.find((g) => g.code.toLowerCase() === v.toLowerCase())
  if (asGroup) return asGroup.code
  const owner = groups.find((g) => g.codes.some((c) => c.toLowerCase() === v.toLowerCase()))
  return owner?.code || ''
}

export function envCodeInGroup(envCode: string, groupCode: string, groups: TicketEnvGroup[]): boolean {
  const code = String(envCode || '').trim().toLowerCase()
  if (!groupCode.trim()) return true
  if (!code) return false
  return codesOfGroup(groupCode, groups).some((c) => c.toLowerCase() === code)
}

/** 申请页按环境档过滤域名行。全等匹配仍留给其他页面。 */
export function filterAccessDomainsByGroup(
  rows: AccessDomainRow[],
  opts: { groupCode?: string; groups?: TicketEnvGroup[]; dbType?: string } = {},
): AccessDomainRow[] {
  const groupCode = String(opts.groupCode || '').trim()
  const groups = opts.groups || []
  const codes = new Set(codesOfGroup(groupCode, groups).map((c) => c.toLowerCase()))
  const wantType = engineTypeKey(opts.dbType)
  return (rows || []).filter((r) => {
    if (groupCode && !codes.has(String(r.env_code || '').trim().toLowerCase())) return false
    if (wantType && engineTypeKey(r.db_type) !== wantType) return false
    return true
  })
}

export function envCodeForSource(rows: AccessDomainRow[], instanceId: string, domain: string): string {
  const id = String(instanceId || '').trim()
  const dom = String(domain || '').trim().toLowerCase()
  const hit = (rows || []).find(
    (r) => String(r.instance_id || '').trim() === id && String(r.domain || '').trim().toLowerCase() === dom,
  )
  return String(hit?.env_code || '').trim()
}

/** 多实例 DDL：各数据源审核人并集（大小写不敏感去重，保留首次写法）。 */
export function unionApproverNames(lists: string[][]): string[] {
  const norm = (s: string) => s.trim().toLowerCase()
  const display = new Map<string, string>()
  for (const list of lists) {
    for (const n of list || []) {
      const k = norm(n)
      if (!k || display.has(k)) continue
      display.set(k, n.trim())
    }
  }
  return [...display.values()]
}

export type DbRecordForApprover = {
  instance_id?: string
  db_name?: string
  approvers?: string[] | string
}

function hasApprovers(d: DbRecordForApprover): boolean {
  const a = d.approvers
  if (Array.isArray(a)) return a.some((x) => String(x || '').trim())
  return String(a || '').trim().length > 0
}

/** db_marketing_064 / db_marketing_0 → db_marketing_ ，便于分库族匹配 */
export function dbNameFamilyPrefix(dbName: string): string {
  const s = String(dbName || '').trim()
  if (!s) return ''
  const m = s.match(/^(.*_)[^_]+$/)
  return m ? m[1] : s
}

/**
 * 为某实例解析用于拉取库级审核人的库名。
 * 1) 同实例上库名与表单逻辑库一致；
 * 2) 分库时按 shard 前缀 / 库名族前缀匹配该实例登记库（优先有审批人的）；
 * 3) 否则返回空串，后端回退实例审批人。
 */
export function resolveDbNameForApprover(
  instanceId: string,
  formDbName: string,
  dbRecords: DbRecordForApprover[],
  opts?: { shardDb?: boolean; shardDbPrefix?: string },
): string {
  const iid = String(instanceId || '').trim()
  const want = String(formDbName || '').trim()
  if (!iid) return want

  const onInst = (dbRecords || []).filter(
    (d) => String(d.instance_id || '').trim() === iid && String(d.db_name || '').trim(),
  )

  if (want) {
    const exact = onInst.find((d) => String(d.db_name || '').trim() === want)
    if (exact) return want
  }

  if (opts?.shardDb) {
    const prefix = String(opts.shardDbPrefix || '').trim()
    const prefixes = [prefix, dbNameFamilyPrefix(prefix), dbNameFamilyPrefix(want)]
      .map((p) => p.trim())
      .filter(Boolean)
    const seen = new Set<string>()
    for (const p of prefixes) {
      const key = p.toLowerCase()
      if (seen.has(key)) continue
      seen.add(key)
      const matches = onInst.filter((d) => String(d.db_name || '').trim().toLowerCase().startsWith(key))
      const withAppr = matches.find((d) => hasApprovers(d))
      if (withAppr) return String(withAppr.db_name || '').trim()
      if (matches[0]) return String(matches[0].db_name || '').trim()
    }
  }

  return ''
}
