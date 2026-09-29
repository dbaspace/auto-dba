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
} as const

function isAdminRole(role: unknown): boolean {
  const r = String(role || '').toLowerCase()
  return r === 'admin' || r === 'platform_admin' || r === 'super_admin'
}

export function usePermission() {
  const user = useAppSelector((s) => s.auth.user)
  const permissions = useMemo(() => {
    const list = (user?.permissions as string[]) || []
    return new Set(list)
  }, [user?.permissions])

  const isAdmin = isAdminRole(user?.role)

  function hasUrn(urn: string | undefined | null): boolean {
    if (!urn) return false
    if (isAdmin) return true
    return permissions.has(urn)
  }

  return { hasUrn, isAdmin, permissions }
}
