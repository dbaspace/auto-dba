import { useEffect, useRef } from 'react'
import { useAppSelector } from '../store'

/**
 * Tab 保活时，当前 Tab 从后台切回前台时重新拉取数据。
 * match: 精确 path（忽略 query），或自定义匹配函数。
 */
export function useReloadOnTabActive(
  match: string | ((activeKey: string) => boolean),
  reload: () => void | Promise<void>,
) {
  const activeKey = useAppSelector((s) => s.tabs.activeKey)
  const reloadRef = useRef(reload)
  reloadRef.current = reload

  const matched =
    typeof match === 'function'
      ? match(activeKey)
      : activeKey.split('?')[0] === match

  useEffect(() => {
    if (!matched) return
    void reloadRef.current()
  }, [matched, activeKey])
}

/** 切回 Tab 时适合重挂载刷新的页面（表单/查询编辑器除外，以免丢输入） */
export function shouldRemountOnTabActivate(path: string): boolean {
  const p = (path || '').split('?')[0]
  if (!p) return false
  // 保活表单 / 编辑器
  if (
    p === '/ops/query' ||
    p === '/ops/kafka-query' ||
    p === '/ops/redis-query' ||
    p === '/ops/ddl' ||
    p === '/ops/dml' ||
    p === '/ops/dql' ||
    p === '/ops/dcl' ||
    p === '/ops/createdb' ||
    p === '/ops/resource-apply' ||
    p === '/ops/kafka' ||
    p === '/ops/redis' ||
    p.startsWith('/ops/cdc/create') ||
    p.startsWith('/ops/migration/create') ||
    p.startsWith('/ops/terminal')
  ) {
    return false
  }
  // 工作台、工单、配置、CMDB、告警、运维列表等
  if (p === '/ops/dashboard' || p === '/metadata/database/search/dashboard') return true
  if (p === '/ops/tickets' || /^\/ops\/tickets\/[^/]+$/.test(p)) return true
  if (p.startsWith('/config/')) return true
  if (p.startsWith('/cmdb/')) return true
  if (p.startsWith('/ops/alerts')) return true
  if (p.startsWith('/ops/todo')) return true
  if (
    p === '/ops/cdc' ||
    p === '/ops/migration' ||
    p === '/ops/slots' ||
    p.startsWith('/ops/archive') ||
    p.startsWith('/ops/backup') ||
    p.startsWith('/ops/flashback') ||
    p.startsWith('/ops/db-monitor') ||
    p === '/ops/sessions' ||
    p === '/ops/ha' ||
    p === '/ops/sr' ||
    p.startsWith('/ops/sr-cluster') ||
    p.startsWith('/ops/cluster-deploy')
  ) {
    return true
  }
  return false
}
