import api from '../../api/request'

export type QueryTreeCache = {
  tenantId: string
  databases: any[]
  instances: any[]
  envOptions: string[]
}

const STORAGE_PREFIX = 'auto-dba.query-tree.'

let cache: QueryTreeCache | null = null

function storageKey(tenantId: string) {
  return STORAGE_PREFIX + tenantId
}

function validCache(tenantId: string, parsed: QueryTreeCache | null): QueryTreeCache | null {
  if (!parsed || parsed.tenantId !== tenantId) return null
  if (!Array.isArray(parsed.databases) || !Array.isArray(parsed.instances)) return null
  if (!Array.isArray(parsed.envOptions)) parsed.envOptions = []
  return parsed
}

export function readQueryTreeCache(tenantId: string): QueryTreeCache | null {
  if (!tenantId) return null
  if (cache?.tenantId === tenantId) return cache
  try {
    const raw = sessionStorage.getItem(storageKey(tenantId))
    if (!raw) return null
    const parsed = validCache(tenantId, JSON.parse(raw) as QueryTreeCache)
    if (!parsed) return null
    cache = parsed
    return parsed
  } catch {
    return null
  }
}

export function writeQueryTreeCache(entry: QueryTreeCache) {
  if (!entry.tenantId) return
  cache = entry
  try {
    sessionStorage.setItem(storageKey(entry.tenantId), JSON.stringify(entry))
  } catch {
    /* 配额满时仍保留本次内存缓存 */
  }
}

function dbTags(db: any): string[] {
  const raw = db?.tags
  if (Array.isArray(raw)) return raw.map((t) => String(t || '').trim()).filter(Boolean)
  if (typeof raw === 'string') {
    const s = raw.trim()
    if (!s) return []
    try {
      const parsed = JSON.parse(s)
      if (Array.isArray(parsed)) return dbTags({ tags: parsed })
    } catch { /* 不是 JSON */ }
    return s.split(/[,，]/).map((t) => t.trim()).filter(Boolean)
  }
  return []
}

function resolveDbEnv(db: any, inst?: any): string {
  for (const raw of dbTags(db)) {
    const i = raw.indexOf('=')
    if (i <= 0) continue
    if (raw.slice(0, i).trim().toLowerCase() === 'idc') {
      const v = raw.slice(i + 1).trim()
      if (v) return v
    }
  }
  return String(inst?.idc || '').trim()
}

function engineName(t: any): string {
  return String(t || '').toLowerCase().trim()
}

export function buildQueryTreeCache(tenantId: string, payload: any): QueryTreeCache {
  const instances = Array.isArray(payload?.instances) ? payload.instances : []
  const instById: Record<string, any> = {}
  for (const inst of instances) instById[inst.id] = inst
  const databases = (Array.isArray(payload?.databases) ? payload.databases : []).filter((db: any) => {
    const t = engineName(instById[db.instance_id]?.db_type)
    return t !== 'kafka' && t !== 'redis'
  })
  const envSet = new Set<string>()
  for (const db of databases) {
    const env = resolveDbEnv(db, instById[db.instance_id])
    if (env) envSet.add(env)
  }
  const envOptions = [...envSet].sort((a, b) => a.localeCompare(b, 'zh-CN', { numeric: true, sensitivity: 'base' }))
  return { tenantId, databases, instances, envOptions }
}

let inflight: { tenantId: string; promise: Promise<QueryTreeCache | null> } | null = null
let cacheGen = 0

function loadQueryCatalog(tenantId: string): Promise<QueryTreeCache | null> {
  if (inflight?.tenantId === tenantId) return inflight.promise
  const gen = cacheGen
  const promise = api.get('/query/catalog').then(({ data }) => {
    if (gen !== cacheGen) return null
    const entry = buildQueryTreeCache(tenantId, data?.data || {})
    writeQueryTreeCache(entry)
    return entry
  }).catch(() => null).finally(() => {
    if (inflight?.promise === promise) inflight = null
  })
  inflight = { tenantId, promise }
  return promise
}

/** 登录后预取。已有缓存或已有在途请求时不再另发。 */
export function prefetchQueryCatalog(tenantId: string): Promise<QueryTreeCache | null> {
  if (!tenantId) return Promise.resolve(null)
  const hit = readQueryTreeCache(tenantId)
  if (hit) return Promise.resolve(hit)
  return loadQueryCatalog(tenantId)
}

/** 打开查询页后的后台更新。在途请求则共用那一次。 */
export function refreshQueryCatalog(tenantId: string): Promise<QueryTreeCache | null> {
  if (!tenantId) return Promise.resolve(null)
  return loadQueryCatalog(tenantId)
}

export function clearQueryTreeCache() {
  cache = null
  cacheGen += 1
  inflight = null
  try {
    const drop: string[] = []
    for (let i = 0; i < sessionStorage.length; i++) {
      const key = sessionStorage.key(i)
      if (key && key.startsWith(STORAGE_PREFIX)) drop.push(key)
    }
    drop.forEach((key) => sessionStorage.removeItem(key))
  } catch {
    /* 无会话存储时忽略 */
  }
}
