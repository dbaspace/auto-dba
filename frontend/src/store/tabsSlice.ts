import { createSlice, PayloadAction } from '@reduxjs/toolkit'
import type { MenuNode } from './authSlice'
import { PAGE_ROUTES, collectPaths, firstAllowed, pathAllowed } from '../constants/routes'

export const HOME_PATH = '/ops/dashboard'
export const MAX_CLOSABLE_TABS = 12

export interface AppTab {
  key: string
  path: string
  search: string
  title: string
  closable: boolean
}

interface TabsState {
  tabs: AppTab[]
  activeKey: string
}

function normPath(path: string): string {
  if (!path) return '/'
  const p = path.startsWith('/') ? path : `/${path}`
  return p.length > 1 && p.endsWith('/') ? p.slice(0, -1) : p
}

function tabKey(path: string, search = ''): string {
  const p = normPath(path)
  const s = search && search !== '?' ? search : ''
  return s ? `${p}${s.startsWith('?') ? s : `?${s}`}` : p
}

/** 展示名：仪表盘 → 工作台；其余优先菜单标题 */
export function titleForPath(path: string, menus?: MenuNode[]): string {
  const p = normPath(path)
  if (p === HOME_PATH || p === '/metadata/database/search/dashboard') return '工作台'
  const exactRoute = PAGE_ROUTES.find((r) => r.path === p)
  if (exactRoute) return exactRoute.title
  const fromMenu = findMenuTitle(menus, p)
  if (fromMenu) return fromMenu
  const hit = PAGE_ROUTES.find((r) => p.startsWith(r.path + '/'))
  if (hit) {
    if (p !== hit.path && p.startsWith(hit.path + '/')) {
      const rest = p.slice(hit.path.length + 1)
      if (rest && !rest.includes('/')) return `${hit.title}`
    }
    return hit.title
  }
  return p
}

function findMenuTitle(nodes: MenuNode[] | undefined, path: string): string {
  let best = ''
  let bestLen = -1
  const walk = (list: MenuNode[] | undefined) => {
    for (const n of list || []) {
      if (n.path && (n.path === path || path.startsWith(n.path + '/'))) {
        if (n.path.length > bestLen) {
          bestLen = n.path.length
          best = n.title === '仪表盘' && n.path === HOME_PATH ? '工作台' : n.title
        }
      }
      if (n.children?.length) walk(n.children)
    }
  }
  walk(nodes)
  return best
}

export function makeHomeTab(menus?: MenuNode[]): AppTab {
  const allowed = collectPaths(menus)
  let path = HOME_PATH
  if (allowed.length && !pathAllowed(HOME_PATH, allowed)) {
    path = firstAllowed(allowed)
  }
  return {
    key: tabKey(path),
    path: normPath(path),
    search: '',
    title: path === HOME_PATH ? '工作台' : titleForPath(path, menus),
    closable: false,
  }
}

const TABS_STORAGE_PREFIX = 'auto-dba-app-tabs:'

/** 最近一次成功读写所用的 userId（登出后 localStorage.user 已空，clear 仍指向该桶） */
let lastTabsOwnerId = ''

function currentTabsOwnerId(): string {
  try {
    const id = String(JSON.parse(localStorage.getItem('user') || '{}').id || '').trim()
    if (id) {
      lastTabsOwnerId = id
      return id
    }
  } catch {
    /* ignore */
  }
  return ''
}

function tabsStorageKey(userId: string): string {
  return `${TABS_STORAGE_PREFIX}${userId || 'anon'}`
}

function isAppTab(v: unknown): v is AppTab {
  if (!v || typeof v !== 'object') return false
  const t = v as Record<string, unknown>
  return (
    typeof t.key === 'string' &&
    typeof t.path === 'string' &&
    typeof t.search === 'string' &&
    typeof t.title === 'string' &&
    typeof t.closable === 'boolean'
  )
}

function defaultTabsState(menus?: MenuNode[]): TabsState {
  const home = makeHomeTab(menus)
  return { tabs: [home], activeKey: home.key }
}

/** 从 localStorage 恢复；校验失败或无登录用户则回退工作台 */
export function loadTabsState(menus?: MenuNode[]): TabsState {
  const fallback = defaultTabsState(menus)
  const uid = currentTabsOwnerId()
  if (!uid) return fallback
  try {
    const raw = localStorage.getItem(tabsStorageKey(uid))
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as { tabs?: unknown; activeKey?: unknown }
    if (!Array.isArray(parsed.tabs) || !parsed.tabs.length) return fallback
    const tabs = parsed.tabs.filter(isAppTab).map((t) => ({
      key: t.key,
      path: normPath(t.path),
      search: typeof t.search === 'string' ? t.search : '',
      title: t.title || titleForPath(t.path, menus),
      closable: !!t.closable,
    }))
    if (!tabs.length) return fallback

    const home = makeHomeTab(menus)
    if (!tabs.some((t) => !t.closable && t.path === home.path)) {
      tabs.unshift(home)
    }
    // 工作台固定首位
    const pin = tabs.findIndex((t) => !t.closable)
    if (pin > 0) {
      const [h] = tabs.splice(pin, 1)
      tabs.unshift(h)
    }

    const activeKey =
      typeof parsed.activeKey === 'string' && tabs.some((t) => t.key === parsed.activeKey)
        ? parsed.activeKey
        : tabs[0].key
    return { tabs, activeKey }
  } catch {
    return fallback
  }
}

/** 仅在已登录（有 user.id）时写入，避免登出后把空态写回用户桶 */
export function saveTabsState(state: TabsState): void {
  const uid = currentTabsOwnerId()
  if (!uid) return
  try {
    localStorage.setItem(
      tabsStorageKey(uid),
      JSON.stringify({ tabs: state.tabs, activeKey: state.activeKey }),
    )
  } catch {
    /* quota / private mode */
  }
}

export function clearTabsState(): void {
  try {
    if (lastTabsOwnerId) localStorage.removeItem(tabsStorageKey(lastTabsOwnerId))
    const uid = currentTabsOwnerId()
    if (uid && uid !== lastTabsOwnerId) localStorage.removeItem(tabsStorageKey(uid))
  } catch {
    /* ignore */
  }
}

const initialState: TabsState = loadTabsState()

type OpenPayload = {
  path: string
  search?: string
  title?: string
  menus?: MenuNode[]
  /** 跳过 pathAllowed（仅 RequireAuth 已通过后的同步） */
  force?: boolean
}

const tabsSlice = createSlice({
  name: 'tabs',
  initialState,
  reducers: {
    resetTabs(state, action: PayloadAction<{ menus?: MenuNode[] }>) {
      clearTabsState()
      const home = makeHomeTab(action.payload?.menus)
      state.tabs = [home]
      state.activeKey = home.key
    },
    openTab(state, action: PayloadAction<OpenPayload>) {
      const { menus, force, title } = action.payload
      const path = normPath(action.payload.path)
      const search = action.payload.search || ''
      if (path === '/403' || path === '/login') return

      const allowed = collectPaths(menus)
      if (!force && allowed.length && !pathAllowed(path, allowed)) return

      const key = tabKey(path, search)
      const existing = state.tabs.find((t) => t.key === key)
      if (existing) {
        state.activeKey = existing.key
        if (title) existing.title = title
        return
      }

      const home = makeHomeTab(menus)
      const isPinned = normPath(path) === home.path && !(search && search !== '?')
      const tab: AppTab = {
        key,
        path,
        search: search && !search.startsWith('?') ? `?${search}` : search,
        title: title || titleForPath(path, menus),
        closable: !isPinned,
      }

      if (tab.closable) {
        const closables = state.tabs.filter((t) => t.closable)
        if (closables.length >= MAX_CLOSABLE_TABS) {
          const drop = closables[0]
          state.tabs = state.tabs.filter((t) => t.key !== drop.key)
        }
      }

      state.tabs.push(tab)
      state.activeKey = key
    },
    activateTab(state, action: PayloadAction<string>) {
      if (state.tabs.some((t) => t.key === action.payload)) {
        state.activeKey = action.payload
      }
    },
    /** 就地更新当前 Tab 的 search/title（如实例页切换库类型） */
    updateTabMeta(
      state,
      action: PayloadAction<{ key: string; search?: string; title?: string }>,
    ) {
      const idx = state.tabs.findIndex((t) => t.key === action.payload.key)
      if (idx < 0) return
      const tab = state.tabs[idx]
      const nextSearch =
        action.payload.search !== undefined
          ? action.payload.search && !action.payload.search.startsWith('?') && action.payload.search !== ''
            ? `?${action.payload.search}`
            : action.payload.search || ''
          : tab.search
      const nextTitle = action.payload.title !== undefined ? action.payload.title : tab.title
      const nextKey = tabKey(tab.path, nextSearch)

      if (nextKey !== tab.key) {
        const collide = state.tabs.findIndex((t) => t.key === nextKey)
        if (collide >= 0) {
          state.tabs.splice(idx, 1)
          const target = state.tabs.find((t) => t.key === nextKey)
          if (target && action.payload.title !== undefined) target.title = nextTitle
          state.activeKey = nextKey
          return
        }
        tab.key = nextKey
        tab.search = nextSearch
        tab.title = nextTitle
        if (state.activeKey === action.payload.key) state.activeKey = nextKey
        return
      }
      tab.search = nextSearch
      tab.title = nextTitle
    },
    closeTab(state, action: PayloadAction<string>) {
      const key = action.payload
      const idx = state.tabs.findIndex((t) => t.key === key)
      if (idx < 0) return
      const tab = state.tabs[idx]
      if (!tab.closable) return
      state.tabs.splice(idx, 1)
      if (state.activeKey === key) {
        const next = state.tabs[idx] || state.tabs[idx - 1] || state.tabs[0]
        state.activeKey = next?.key || state.tabs[0]?.key
      }
    },
    /** 拖拽重排：工作台（不可关闭）永久固定在 index 0 */
    reorderTabs(state, action: PayloadAction<{ fromKey: string; toKey: string }>) {
      const { fromKey, toKey } = action.payload
      if (!fromKey || !toKey || fromKey === toKey) return
      const fromIdx = state.tabs.findIndex((t) => t.key === fromKey)
      const toIdx = state.tabs.findIndex((t) => t.key === toKey)
      if (fromIdx < 0 || toIdx < 0) return
      const from = state.tabs[fromIdx]
      const to = state.tabs[toIdx]
      if (!from.closable || !to.closable) return
      // 禁止落到固定 Tab 之前
      if (toIdx === 0 && !to.closable) return
      const next = state.tabs.slice()
      const [moved] = next.splice(fromIdx, 1)
      let insertAt = next.findIndex((t) => t.key === toKey)
      if (insertAt < 0) return
      if (fromIdx < toIdx) insertAt += 1
      // 保证首位仍是不可关闭的工作台
      const pinnedIdx = next.findIndex((t) => !t.closable)
      if (pinnedIdx >= 0 && insertAt <= pinnedIdx) {
        insertAt = pinnedIdx + 1
      }
      if (insertAt < 1) insertAt = 1
      next.splice(insertAt, 0, moved)
      // 若工作台不在首位则纠正
      const pin = next.findIndex((t) => !t.closable)
      if (pin > 0) {
        const [home] = next.splice(pin, 1)
        next.unshift(home)
      }
      state.tabs = next
    },
    closeOtherTabs(state, action: PayloadAction<string | undefined>) {
      const keep = action.payload || state.activeKey
      state.tabs = state.tabs.filter((t) => !t.closable || t.key === keep)
      if (!state.tabs.some((t) => t.key === state.activeKey)) {
        state.activeKey = state.tabs.find((t) => t.key === keep)?.key || state.tabs[0]?.key
      }
    },
    /** 菜单权限变更后剔除无权限 Tab */
    pruneUnauthorizedTabs(state, action: PayloadAction<{ menus?: MenuNode[] }>) {
      const menus = action.payload.menus
      const allowed = collectPaths(menus)
      const home = makeHomeTab(menus)
      state.tabs = state.tabs.filter((t) => {
        if (!t.closable && t.path === home.path) return true
        if (!allowed.length) return true
        return pathAllowed(t.path, allowed)
      })
      if (!state.tabs.some((t) => t.path === home.path && !t.closable)) {
        state.tabs.unshift(home)
      }
      if (!state.tabs.some((t) => t.key === state.activeKey)) {
        state.activeKey = home.key
      }
    },
  },
})

export const { resetTabs, openTab, activateTab, updateTabMeta, closeTab, closeOtherTabs, pruneUnauthorizedTabs, reorderTabs } =
  tabsSlice.actions
export { normPath, tabKey }
export default tabsSlice.reducer
