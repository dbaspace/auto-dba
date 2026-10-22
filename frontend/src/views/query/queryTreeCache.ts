export type QueryTreeCache = {
  tenantId: string
  databases: any[]
  instances: any[]
  envOptions: string[]
}

let cache: QueryTreeCache | null = null

export function readQueryTreeCache(tenantId: string): QueryTreeCache | null {
  if (!tenantId || !cache || cache.tenantId !== tenantId) return null
  return cache
}

export function writeQueryTreeCache(entry: QueryTreeCache) {
  if (!entry.tenantId) return
  cache = entry
}

export function clearQueryTreeCache() {
  cache = null
}
