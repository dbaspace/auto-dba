import { Tabs } from 'antd'

export type ChangeCategoryKey = 'init' | 'run' | 'upgrade' | 'scale'

export type ChangeCategory = { key: ChangeCategoryKey; label: string; types: string[] }

/** StarRocks 集群变更分类；配置下发归扩缩容。 */
export const SR_CHANGE_CATEGORIES: ChangeCategory[] = [
  { key: 'init', label: '初始化日志', types: ['cluster_init', 'cluster_reinit'] },
  { key: 'run', label: '运行日志', types: ['start_node', 'stop_node', 'restart_node'] },
  { key: 'upgrade', label: '升级日志', types: ['upgrade', 'upgrade_rollback'] },
  { key: 'scale', label: '扩缩容日志', types: ['add_fe', 'add_be', 'remove_fe', 'remove_be', 'decommission_be', 'recommission_be', 'config_apply', 'config_rollback'] },
]

/** MySQL / PG / Redis 等部署工作台变更分类（暂无升级类变更）。 */
export const DB_CHANGE_CATEGORIES: ChangeCategory[] = [
  { key: 'init', label: '初始化日志', types: ['cluster_deploy', 'ha_init'] },
  { key: 'run', label: '运行日志', types: ['start_node', 'stop_node', 'restart_node'] },
  { key: 'upgrade', label: '升级日志', types: [] },
  { key: 'scale', label: '扩缩容日志', types: ['add_node', 'remove_node'] },
]

/** 变更所属分类；未知类型归「运行日志」，避免被漏掉。 */
export function changeCategoryOf(categories: ChangeCategory[], type?: string): ChangeCategoryKey {
  return categories.find((c) => c.types.includes(type || ''))?.key || 'run'
}

export function changeCategoryLabel(categories: ChangeCategory[], key: ChangeCategoryKey): string {
  return categories.find((c) => c.key === key)?.label || '变更日志'
}

export function filterChangesByCategory<T extends { type: string }>(
  categories: ChangeCategory[], list: T[], key: ChangeCategoryKey,
): T[] {
  return list.filter((c) => changeCategoryOf(categories, c.type) === key)
}

/** 分类内最值得看的一条：失败 / 执行中优先，否则最新一条（列表按时间倒序）。 */
export function pickChangeInCategory<T extends { id: string; status: string }>(list: T[]): string {
  return list.find((c) => c.status === 'failed' || c.status === 'running' || c.status === 'pending')?.id
    || list[0]?.id
    || ''
}

export function ChangeCategoryTabs<T extends { type: string }>({
  categories, changes, active, onChange,
}: {
  categories: ChangeCategory[]
  changes: T[]
  active: ChangeCategoryKey
  onChange: (key: ChangeCategoryKey) => void
}) {
  return (
    <Tabs
      size="small"
      activeKey={active}
      onChange={(k) => onChange(k as ChangeCategoryKey)}
      style={{ marginBottom: -8 }}
      items={categories.map((c) => ({
        key: c.key,
        label: `${c.label} (${filterChangesByCategory(categories, changes, c.key).length})`,
      }))}
    />
  )
}
