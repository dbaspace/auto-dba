export type PermissionOption = { label: string; value: string; title?: string }

const BASE_OPTIONS: PermissionOption[] = [
  { label: '读操作', value: 'r' },
  { label: '写操作', value: 'c' },
  { label: '更新操作', value: 'u' },
  { label: '删除操作', value: 'd' },
]

const STARROCKS_EXTRA: PermissionOption[] = [
  {
    label: '结构变更',
    value: 'ddl',
    title: '建表/视图/物化视图/函数/PIPE，修改与删除表、视图、物化视图，并包含删除数据库权限',
  },
  { label: '导出', value: 'export', title: '对库内所有表执行 EXPORT' },
]

const LABELS: Record<string, string> = Object.fromEntries(
  [...BASE_OPTIONS, ...STARROCKS_EXTRA].map((o) => [o.value, o.label]),
)

/** 按实例类型返回账号权限选项；StarRocks 额外提供结构变更与导出。 */
export function permissionOptions(dbType: string): PermissionOption[] {
  return dbType === 'starrocks' ? [...BASE_OPTIONS, ...STARROCKS_EXTRA] : BASE_OPTIONS
}

/** 剔除当前实例类型不支持的已选权限。 */
export function filterPermissions(dbType: string, perms: string[]): string[] {
  const allowed = new Set(permissionOptions(dbType).map((o) => o.value))
  return (perms || []).filter((p) => allowed.has(p))
}

export function permissionLabel(code: string): string {
  return LABELS[code] || code
}

type SrObject = { clause: string; order: string[] }

// 与后端 grant_starrocks.go 保持一致
const SR_OBJECTS: SrObject[] = [
  { clause: 'ALL TABLES IN DATABASE', order: ['DELETE', 'DROP', 'INSERT', 'SELECT', 'ALTER', 'EXPORT', 'UPDATE'] },
  { clause: 'ALL VIEWS IN DATABASE', order: ['SELECT', 'DROP'] },
  { clause: 'ALL MATERIALIZED VIEWS IN DATABASE', order: ['ALTER', 'REFRESH', 'DROP', 'SELECT'] },
  {
    clause: 'DATABASE',
    order: ['CREATE TABLE', 'DROP', 'ALTER', 'CREATE VIEW', 'CREATE FUNCTION', 'CREATE MATERIALIZED VIEW', 'CREATE PIPE'],
  },
]

const SR_PERM_PRIVS: Record<string, string[][]> = {
  r: [['SELECT'], ['SELECT'], ['SELECT'], []],
  c: [['INSERT'], [], [], []],
  u: [['UPDATE'], [], [], []],
  d: [['DELETE'], [], [], []],
  export: [['EXPORT'], [], [], []],
  ddl: [
    ['DROP', 'ALTER'],
    ['DROP'],
    ['ALTER', 'REFRESH', 'DROP'],
    ['CREATE TABLE', 'DROP', 'ALTER', 'CREATE VIEW', 'CREATE FUNCTION', 'CREATE MATERIALIZED VIEW', 'CREATE PIPE'],
  ],
}

export function starRocksGrantStatements(username: string, db: string, perms: string[]): string[] {
  const user = String(username || '').trim()
  const d = String(db || '').trim()
  if (!user || !d) return []
  const want = SR_OBJECTS.map(() => new Set<string>())
  for (const p of perms || []) {
    const privs = SR_PERM_PRIVS[p]
    if (!privs) continue
    privs.forEach((list, i) => list.forEach((x) => want[i].add(x)))
  }
  const ident = `'${user.replace(/'/g, "''")}'@'%'`
  const out: string[] = []
  SR_OBJECTS.forEach((obj, i) => {
    const privs = obj.order.filter((x) => want[i].has(x))
    if (privs.length) out.push(`GRANT ${privs.join(', ')} ON ${obj.clause} ${d} TO USER ${ident};`)
  })
  return out
}

export function previewStarRocksGrantSql(dbNames: string[], username: string, perms: string[]): string {
  const body = (dbNames || []).flatMap((db) => starRocksGrantStatements(username, db, perms))
  if (!body.length) return ''
  return ['-- 账号不存在时执行前自动 CREATE USER … IDENTIFIED BY（随机密码，执行后通知申请人）', ...body].join('\n')
}
