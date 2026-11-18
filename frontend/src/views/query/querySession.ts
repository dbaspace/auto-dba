const WORKSPACE_PREFIX = 'auto-dba-query-workspace.'
const LEGACY_WORKSPACE_KEY = 'auto-dba-query-workspace'

export function queryWorkspaceKey(userId: string, tenantId: string) {
  return `${WORKSPACE_PREFIX}${userId}.${tenantId}`
}

function legacySessionWorkspaceKey(tenantId: string) {
  return WORKSPACE_PREFIX + tenantId
}

export function queryFavoriteKey(tenantId: string) {
  return `auto-dba-query-favorites.${tenantId}`
}

export function queryHistoryKey(tenantId: string) {
  return `auto-dba-query-history.${tenantId}`
}

/** 读当前用户的查询页签。本机只有旧的 sessionStorage 时迁到 localStorage。 */
export function readQueryWorkspace(userId: string, tenantId: string): string | null {
  if (!userId || !tenantId) return null
  const key = queryWorkspaceKey(userId, tenantId)
  const local = localStorage.getItem(key)
  if (local != null) return local
  const legacy = sessionStorage.getItem(legacySessionWorkspaceKey(tenantId))
  if (legacy == null) return null
  try {
    localStorage.setItem(key, legacy)
  } catch {
    /* 迁不过去也先用这份内容 */
  }
  return legacy
}

export function writeQueryWorkspace(userId: string, tenantId: string, raw: string) {
  if (!userId || !tenantId) return
  localStorage.setItem(queryWorkspaceKey(userId, tenantId), raw)
}

/** 退出登录时清掉会话里的旧页签，避免下一个账号读到上一份 SQL。按用户存在 localStorage 的页签保留。 */
export function clearQueryWorkspaces() {
  const drop: string[] = []
  for (let i = 0; i < sessionStorage.length; i++) {
    const key = sessionStorage.key(i)
    if (!key) continue
    if (key === LEGACY_WORKSPACE_KEY || key.startsWith(WORKSPACE_PREFIX)) drop.push(key)
  }
  drop.forEach((key) => sessionStorage.removeItem(key))
}
