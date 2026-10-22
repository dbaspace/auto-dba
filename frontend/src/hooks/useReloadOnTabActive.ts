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

/** 页签切走后保持挂载。新数据用页面上的刷新，避免来回切换重新请求。 */
export function shouldRemountOnTabActivate(_path: string): boolean {
  return false
}
