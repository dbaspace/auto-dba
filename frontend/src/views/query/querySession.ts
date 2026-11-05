const WORKSPACE_PREFIX = 'auto-dba-query-workspace.'
const LEGACY_WORKSPACE_KEY = 'auto-dba-query-workspace'

export function queryWorkspaceKey(tenantId: string) {
  return WORKSPACE_PREFIX + tenantId
}

export function queryFavoriteKey(tenantId: string) {
  return `auto-dba-query-favorites.${tenantId}`
}

export function queryHistoryKey(tenantId: string) {
  return `auto-dba-query-history.${tenantId}`
}

/** 退出登录时清掉各租户的查询页签，避免下一个账号读到上一份 SQL。 */
export function clearQueryWorkspaces() {
  const drop: string[] = []
  for (let i = 0; i < sessionStorage.length; i++) {
    const key = sessionStorage.key(i)
    if (!key) continue
    if (key === LEGACY_WORKSPACE_KEY || key.startsWith(WORKSPACE_PREFIX)) drop.push(key)
  }
  drop.forEach((key) => sessionStorage.removeItem(key))
}
