import { useMemo } from 'react'
import { useAppSelector } from '../store'

/** 配置类资源动作 URN（与后端 store/perms.go 对齐） */
export const Perm = {
  instances: {
    create: 'um.action.instances.create',
    edit: 'um.action.instances.edit',
    delete: 'um.action.instances.delete',
  },
  databases: {
    create: 'um.action.databases.create',
    edit: 'um.action.databases.edit',
    delete: 'um.action.databases.delete',
  },
  accounts: {
    create: 'um.action.accounts.create',
    edit: 'um.action.accounts.edit',
    delete: 'um.action.accounts.delete',
  },
  regions: {
    create: 'um.action.regions.create',
    edit: 'um.action.regions.edit',
    bind: 'um.action.regions.bind',
    delete: 'um.action.regions.delete',
  },
  environments: {
    create: 'um.action.environments.create',
    edit: 'um.action.environments.edit',
    delete: 'um.action.environments.delete',
  },
  monitorTags: {
    create: 'um.action.monitor-tags.create',
    edit: 'um.action.monitor-tags.edit',
    delete: 'um.action.monitor-tags.delete',
  },
  connectors: {
    create: 'um.action.connectors.create',
    edit: 'um.action.connectors.edit',
    delete: 'um.action.connectors.delete',
  },
  reviewRules: {
    create: 'um.action.review-rules.create',
    edit: 'um.action.review-rules.edit',
    delete: 'um.action.review-rules.delete',
  },
  args: {
    create: 'um.action.args.create',
    edit: 'um.action.args.edit',
    delete: 'um.action.args.delete',
  },
  scheduledJobs: {
    edit: 'um.action.scheduled-jobs.edit',
    trigger: 'um.action.scheduled-jobs.trigger',
    sidebar: 'um.sidebar.config-scheduled-jobs',
  },
  dns: {
    create: 'um.action.dns.create',
    edit: 'um.action.dns.edit',
    delete: 'um.action.dns.delete',
    import: 'um.action.dns.import',
    rollback: 'um.action.dns.rollback',
    policy: 'um.action.dns.policy',
    sidebar: 'um.sidebar.config-dns',
  },
  users: {
    create: 'um.action.users.create',
    edit: 'um.action.users.edit',
    delete: 'um.action.users.delete',
  },
  roles: {
    create: 'um.action.roles.create',
    edit: 'um.action.roles.edit',
    delete: 'um.action.roles.delete',
  },
  menus: {
    create: 'um.action.menus.create',
    edit: 'um.action.menus.edit',
    delete: 'um.action.menus.delete',
  },
  ldap: {
    edit: 'um.action.ldap.edit',
  },
  syncSlots: {
    collect: 'um.action.sync-slots.collect',
    delete: 'um.action.sync-slots.delete',
  },
  migration: {
    create: 'um.action.migration.create',
    edit: 'um.action.migration.edit',
    delete: 'um.action.migration.delete',
    approve: 'um.action.migration.approve',
  },
  schemaCompare: {
    create: 'um.action.schema-compare.create',
    delete: 'um.action.schema-compare.delete',
  },
  cdc: {
    create: 'um.action.cdc.create',
    edit: 'um.action.cdc.edit',
    delete: 'um.action.cdc.delete',
  },
  flashback: {
    create: 'um.action.flashback.create',
    edit: 'um.action.flashback.edit',
    cloudEdit: 'um.action.flashback-cloud.edit',
  },
  archive: {
    create: 'um.action.archive.create',
    edit: 'um.action.archive.edit',
    run: 'um.action.archive.run',
    stop: 'um.action.archive.stop',
    settingsEdit: 'um.action.archive-settings.edit',
  },
  sqlInsight: {
    collect: 'um.action.sql-insight.collect',
    awrGenerate: 'um.action.sql-insight.awr-generate',
    spaceCollect: 'um.action.sql-insight.space-collect',
    analyze: 'um.action.sql-insight.analyze',
    settings: 'um.sidebar.config-sql-insight',
  },
  todo: {
    create: 'um.action.todo.create',
    edit: 'um.action.todo.edit',
    follow: 'um.action.todo.follow',
    delete: 'um.action.todo.delete',
  },
  alert: {
    claim: 'um.action.alert.claim',
    resolve: 'um.action.alert.resolve',
    silence: 'um.action.alert.silence',
    ruleEdit: 'um.action.alert.rule.edit',
    ruleDelete: 'um.action.alert.rule.delete',
  },
  copilot: {
    chat: 'um.action.copilot.chat',
  },
  runbook: {
    execute: 'um.action.runbook.execute',
  },
  tenants: {
    create: 'um.action.tenants.create',
    edit: 'um.action.tenants.edit',
    disable: 'um.action.tenants.disable',
    delete: 'um.action.tenants.delete',
    members: 'um.action.tenants.members',
    grants: 'um.action.tenants.grants',
    impersonate: 'um.action.tenants.impersonate',
    sidebar: 'um.sidebar.tenants',
  },
  domains: {
    sync: 'um.action.domains.sync',
    assign: 'um.action.domains.assign',
    edit: 'um.action.domains.edit',
    delete: 'um.action.domains.delete',
    sidebar: 'um.sidebar.config-domains',
  },
} as const

function isAdminRole(role: unknown): boolean {
  const r = String(role || '').toLowerCase()
  return r === 'admin' || r === 'platform_admin' || r === 'super_admin'
}

/** 与后端 store.IsPlatformURN 对齐：仅这些走 platform_permissions。 */
function isPlatformUrn(urn: string): boolean {
  const u = urn.trim()
  if (!u) return false
  if (u.startsWith('um.action.tenants.') || u === 'um.sidebar.tenants') return true
  return (
    u === 'um.action.menus.create' ||
    u === 'um.action.menus.edit' ||
    u === 'um.action.menus.delete' ||
    u === 'um.action.users.create' ||
    u === 'um.action.users.edit' ||
    u === 'um.action.users.delete' ||
    u === 'um.action.ldap.edit'
  )
}

export function usePermission() {
  const user = useAppSelector((s) => s.auth.user)
  const tenantPerms = useMemo(() => new Set((user?.permissions as string[]) || []), [user?.permissions])
  const platformPerms = useMemo(
    () => new Set((user?.platform_permissions as string[]) || []),
    [user?.platform_permissions],
  )
  const permissions = useMemo(() => {
    const list = [...tenantPerms, ...platformPerms]
    return new Set(list)
  }, [tenantPerms, platformPerms])

  const isAdmin = isAdminRole(user?.role)

  function hasUrn(urn: string | undefined | null): boolean {
    if (!urn) return false
    if (isAdmin) return true
    if (isPlatformUrn(urn)) return platformPerms.has(urn)
    return tenantPerms.has(urn)
  }

  return { hasUrn, isAdmin, permissions }
}
