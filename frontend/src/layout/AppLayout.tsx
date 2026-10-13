import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Layout, Tooltip } from 'antd'
import { useLocation, useNavigate } from 'react-router-dom'
import { CloseOutlined, MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons'
import api from '../api/request'
import { menuIconTone, resolveMenuIcon } from '../components/MenuIcon'
import { BrandWordmark } from '../components/BrandMark'
import { useAppDispatch, useAppSelector } from '../store'
import { setUser, MenuNode } from '../store/authSlice'
import {
  activateTab,
  closeTab,
  HOME_PATH,
  makeHomeTab,
  openTab,
  pruneUnauthorizedTabs,
  reorderTabs,
  tabKey,
  titleForPath,
  updateTabMeta,
} from '../store/tabsSlice'
import { collectPaths, pathAllowed } from '../constants/routes'
import { isInstancesPath } from '../utils/instanceTypes'
import UserAccountMenu from './UserAccountMenu'
import TenantSwitcher from '../components/TenantSwitcher'
import UserWatermark from '../components/UserWatermark'
import useIdleLogout from '../hooks/useIdleLogout'
import NavFlyout from './NavFlyout'
import AppearancePopover from './AppearancePopover'
import { TabPagePanel } from '../router/appPages'
import { readSiderCollapsed, writeSiderCollapsed } from '../theme/appearance'
import './NavFlyout.css'

const { Sider, Header, Content } = Layout

const SIDER_WIDTH = 220
const SIDER_COLLAPSED_WIDTH = 64
/** 离开侧栏/弹层后关闭延迟，避免移向弹层时闪关 */
const FLYOUT_CLOSE_MS = 400
/** 悬停切换/打开延迟，掠过邻项不误切 */
const FLYOUT_SWITCH_MS = 250

function topLevelMenus(nodes: MenuNode[] | undefined): MenuNode[] {
  return (nodes || []).filter((n) => n.title)
}

function findTopForPath(nodes: MenuNode[] | undefined, path: string): MenuNode | null {
  for (const n of nodes || []) {
    if (n.path && (n.path === path || path.startsWith(n.path + '/'))) return n
    if (n.children?.length) {
      const hit = findInTree(n.children, path)
      if (hit) return n
    }
  }
  return null
}

function findInTree(nodes: MenuNode[] | undefined, path: string): boolean {
  for (const n of nodes || []) {
    if (n.path && (n.path === path || path.startsWith(n.path + '/'))) return true
    if (n.children?.length && findInTree(n.children, path)) return true
  }
  return false
}

function displayTitle(n: MenuNode): string {
  if (n.path === HOME_PATH || n.id === 'm-dashboard') return '工作台'
  if (n.title === '仪表盘' && (!n.children || !n.children.length)) return '工作台'
  return n.title
}

export default function AppLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const user = useAppSelector((s) => s.auth.user)
  const menus = useAppSelector((s) => s.auth.menus)
  const tabs = useAppSelector((s) => s.tabs.tabs)
  const activeKey = useAppSelector((s) => s.tabs.activeKey)
  const activeTab = useMemo(() => tabs.find((t) => t.key === activeKey), [tabs, activeKey])
  const hideUserWatermark = (activeTab?.path || location.pathname) === '/ops/dolphin'

  useIdleLogout(user?.login_idle_timeout_min as number | undefined)

  const path = location.pathname
  const search = location.search || ''
  const tops = useMemo(() => topLevelMenus(menus), [menus])
  const activeTop = useMemo(() => findTopForPath(menus, path), [menus, path])

  const [flyoutId, setFlyoutId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState(() => readSiderCollapsed())
  const flyoutCloseTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const flyoutOpenTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const flyoutIdRef = useRef<string | null>(null)
  flyoutIdRef.current = flyoutId
  const flyoutGroup = useMemo(
    () => tops.find((t) => (t.id || t.title) === flyoutId) || null,
    [tops, flyoutId],
  )

  const siderWidth = collapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH
  const flyoutAnchor = siderWidth + 8

  const cancelFlyoutOpen = useCallback(() => {
    if (flyoutOpenTimer.current) {
      clearTimeout(flyoutOpenTimer.current)
      flyoutOpenTimer.current = null
    }
  }, [])

  const cancelFlyoutClose = useCallback(() => {
    if (flyoutCloseTimer.current) {
      clearTimeout(flyoutCloseTimer.current)
      flyoutCloseTimer.current = null
    }
  }, [])

  const scheduleFlyoutClose = useCallback(() => {
    cancelFlyoutOpen()
    cancelFlyoutClose()
    flyoutCloseTimer.current = setTimeout(() => {
      flyoutCloseTimer.current = null
      setFlyoutId(null)
    }, FLYOUT_CLOSE_MS)
  }, [cancelFlyoutClose, cancelFlyoutOpen])

  const scheduleFlyoutOpen = useCallback(
    (id: string) => {
      cancelFlyoutClose()
      if (flyoutIdRef.current === id) {
        cancelFlyoutOpen()
        return
      }
      cancelFlyoutOpen()
      flyoutOpenTimer.current = setTimeout(() => {
        flyoutOpenTimer.current = null
        setFlyoutId(id)
      }, FLYOUT_SWITCH_MS)
    },
    [cancelFlyoutClose, cancelFlyoutOpen],
  )

  const closeFlyout = useCallback(() => {
    cancelFlyoutOpen()
    cancelFlyoutClose()
    setFlyoutId(null)
  }, [cancelFlyoutClose, cancelFlyoutOpen])

  useEffect(
    () => () => {
      cancelFlyoutOpen()
      cancelFlyoutClose()
    },
    [cancelFlyoutClose, cancelFlyoutOpen],
  )

  const openPage = useCallback(
    (toPath: string, title?: string, opts?: { replace?: boolean; search?: string }) => {
      if (!toPath || !toPath.startsWith('/')) return
      const allowed = collectPaths(menus)
      if (allowed.length && !pathAllowed(toPath, allowed)) {
        navigate('/403')
        return
      }
      const nextSearch = opts?.search || ''
      const normalizedSearch =
        nextSearch && !nextSearch.startsWith('?') && nextSearch !== '' ? `?${nextSearch}` : nextSearch
      // 实例页：同 path 只保留一个 Tab，就地改 search/title，避免双面板互抢回跳 mysql
      if (isInstancesPath(toPath)) {
        const existing = tabs.find((t) => isInstancesPath(t.path))
        if (existing) {
          dispatch(
            updateTabMeta({
              key: existing.key,
              search: normalizedSearch,
              title: title || existing.title,
            }),
          )
          const nextKey = tabKey(toPath, normalizedSearch)
          dispatch(activateTab(nextKey))
          const to = toPath + (normalizedSearch || '')
          if (toPath !== path || (normalizedSearch || '') !== (search || '')) {
            navigate(to, { replace: opts?.replace ?? true })
          }
          closeFlyout()
          return
        }
      }
      dispatch(openTab({ path: toPath, search: normalizedSearch, title, menus }))
      const to = toPath + (normalizedSearch || '')
      if (toPath !== path || (normalizedSearch || '') !== (search || '')) {
        navigate(to, { replace: opts?.replace })
      }
      closeFlyout()
    },
    [closeFlyout, dispatch, menus, navigate, path, search, tabs],
  )

  useEffect(() => {
    dispatch(pruneUnauthorizedTabs({ menus }))
  }, [menus, dispatch])

  useEffect(() => {
    if (!path || path === '/403' || path === '/login') return
    const allowed = collectPaths(menus)
    if (allowed.length && !pathAllowed(path, allowed)) return
    const key = tabKey(path, search)
    const exists = tabs.some((t) => t.key === key)
    if (exists) {
      if (activeKey !== key) dispatch(activateTab(key))
      return
    }
    // 实例页单 Tab：URL db_type 变化时就地改 meta，勿再 openTab 出一个 MySQL 面板
    if (isInstancesPath(path)) {
      const existing = tabs.find((t) => isInstancesPath(t.path))
      if (existing) {
        dispatch(updateTabMeta({ key: existing.key, search }))
        dispatch(activateTab(key))
        return
      }
    }
    dispatch(
      openTab({
        path,
        search,
        title: titleForPath(path, menus),
        menus,
      }),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, search, menus, dispatch])

  useEffect(() => {
    let mounted = true
    async function refreshMe() {
      try {
        const { data } = await api.get('/auth/me')
        const u = data.data || {}
        if (!mounted) return
        localStorage.setItem('user', JSON.stringify(u))
        dispatch(setUser(u))
        dispatch(pruneUnauthorizedTabs({ menus: u.menus || [] }))
      } catch {
        /* ignore */
      }
    }
    // 始终拉一次 /auth/me，避免 seed 新增菜单后仍沿用 localStorage 旧 menus
    refreshMe()
    const handler = () => refreshMe()
    window.addEventListener('menus-changed', handler)
    return () => {
      mounted = false
      window.removeEventListener('menus-changed', handler)
    }
  }, [dispatch])

  function toggleCollapsed() {
    setCollapsed((c) => {
      const next = !c
      writeSiderCollapsed(next)
      return next
    })
    closeFlyout()
  }

  /** 有子菜单或是空目录（无 path）时走弹层；空目录弹层里提示暂无页面 */
  function opensFlyout(n: MenuNode) {
    return !!n.children?.length || !n.path
  }

  function onTopEnter(n: MenuNode) {
    if (opensFlyout(n)) {
      scheduleFlyoutOpen(n.id || n.title)
    }
  }

  function onTopClick(n: MenuNode) {
    if (opensFlyout(n)) {
      cancelFlyoutOpen()
      cancelFlyoutClose()
      setFlyoutId((cur) => ((n.id || n.title) === cur ? cur : n.id || n.title))
      return
    }
    if (n.path) openPage(n.path, displayTitle(n))
  }

  function onTabClick(key: string) {
    const tab = tabs.find((t) => t.key === key)
    if (!tab) return
    dispatch(activateTab(key))
    const to = tab.path + (tab.search || '')
    if (location.pathname + (location.search || '') !== to) {
      navigate(to)
    }
  }

  function onTabClose(e: React.MouseEvent, key: string) {
    e.stopPropagation()
    const idx = tabs.findIndex((t) => t.key === key)
    const tab = tabs[idx]
    if (!tab || !tab.closable) return
    const wasActive = activeKey === key
    let nextTo = ''
    if (wasActive) {
      const next = tabs[idx + 1] || tabs[idx - 1] || tabs.find((t) => !t.closable) || makeHomeTab(menus)
      nextTo = next.path + (next.search || '')
    }
    dispatch(closeTab(key))
    if (wasActive && nextTo) navigate(nextTo)
  }

  const dragTabKeyRef = useRef<string | null>(null)
  const [dragOverKey, setDragOverKey] = useState<string | null>(null)

  function onTabDragStart(e: React.DragEvent, key: string, closable: boolean) {
    if (!closable) {
      e.preventDefault()
      return
    }
    dragTabKeyRef.current = key
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', key)
    try {
      e.dataTransfer.setData('application/x-app-tab', key)
    } catch {
      /* ignore */
    }
  }

  function onTabDragOver(e: React.DragEvent, key: string, closable: boolean) {
    if (!dragTabKeyRef.current || !closable) return
    if (dragTabKeyRef.current === key) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    if (dragOverKey !== key) setDragOverKey(key)
  }

  function onTabDrop(e: React.DragEvent, toKey: string, closable: boolean) {
    e.preventDefault()
    e.stopPropagation()
    const fromKey = dragTabKeyRef.current || e.dataTransfer.getData('text/plain')
    setDragOverKey(null)
    dragTabKeyRef.current = null
    if (!fromKey || !closable || fromKey === toKey) return
    dispatch(reorderTabs({ fromKey, toKey }))
  }

  function onTabDragEnd() {
    dragTabKeyRef.current = null
    setDragOverKey(null)
  }

  function goHome() {
    const home = makeHomeTab(menus)
    openPage(home.path, home.title)
  }

  const collapseBtn = (
    <button type="button" className="app-sider-footer-btn" onClick={toggleCollapsed} aria-label={collapsed ? '展开菜单' : '收起菜单'}>
      {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
      <span className="app-sider-label">收起菜单</span>
    </button>
  )

  return (
    <Layout style={{ height: '100%', width: '100%', overflow: 'hidden', position: 'relative' }}>
      {!hideUserWatermark && <UserWatermark />}
      <Sider
        width={SIDER_WIDTH}
        collapsedWidth={SIDER_COLLAPSED_WIDTH}
        collapsed={collapsed}
        trigger={null}
        collapsible
        className={`app-sider${collapsed ? ' is-collapsed' : ''}`}
        style={{
          background: 'var(--app-sider-bg)',
          overflow: 'hidden',
          borderRight: '1px solid var(--app-border)',
          height: '100%',
        }}
      >
        <div className="app-sider-brand" onClick={goHome} title="KeePlus">
          <BrandWordmark collapsed={collapsed} />
        </div>
        <div
          className="app-sider-menu"
          style={{ padding: '8px 0' }}
          onMouseLeave={scheduleFlyoutClose}
        >
          {tops.map((n) => {
            const id = n.id || n.title
            const title = displayTitle(n)
            const selected =
              !!activeTop &&
              (activeTop.id || activeTop.title) === id &&
              (!flyoutId || flyoutId === id)
            const flyoutOpen = flyoutId === id
            const icoTone = menuIconTone(n.icon)
            const btn = (
              <button
                type="button"
                className={`app-sider-top-item${selected ? ' active' : ''}${flyoutOpen ? ' flyout-open' : ''}`}
                onMouseEnter={() => onTopEnter(n)}
                onClick={() => onTopClick(n)}
              >
                <span className="app-sider-top-ico" style={{ color: icoTone.color }}>
                  {resolveMenuIcon(n.icon, icoTone.color)}
                </span>
                <span className="app-sider-label">{title}</span>
              </button>
            )
            return (
              <div key={id}>
                {collapsed ? (
                  <Tooltip title={title} placement="right">
                    {btn}
                  </Tooltip>
                ) : (
                  btn
                )}
              </div>
            )
          })}
        </div>
        <div className="app-sider-footer">
          <AppearancePopover collapsed={collapsed} onOpenChange={(open) => open && closeFlyout()} />
          {collapsed ? (
            <Tooltip title="展开菜单" placement="right">
              {collapseBtn}
            </Tooltip>
          ) : (
            collapseBtn
          )}
        </div>
      </Sider>

      <NavFlyout
        group={flyoutGroup}
        open={!!flyoutGroup}
        activePath={path}
        activeSearch={search}
        anchorLeft={flyoutAnchor}
        onClose={closeFlyout}
        onHoverChange={(inside) => {
          if (inside) {
            cancelFlyoutOpen()
            cancelFlyoutClose()
          } else {
            scheduleFlyoutClose()
          }
        }}
        onSelect={(p, title, nextSearch) => openPage(p, title, { search: nextSearch })}
      />

      <Layout style={{ minWidth: 0, minHeight: 0, background: 'var(--app-page-bg)' }}>
        <Header
          style={{
            height: 'var(--app-header-height)',
            lineHeight: 'var(--app-header-height)',
            background: 'var(--app-header-bg)',
            display: 'flex',
            alignItems: 'center',
            gap: 16,
            padding: '0 16px',
            flexShrink: 0,
            borderBottom: '1px solid var(--app-border)',
          }}
        >
          <div style={{ flex: 1 }} />
          <TenantSwitcher />
          <UserAccountMenu user={user} />
        </Header>

        <div className="app-tab-bar">
          {tabs.map((t) => (
            <button
              key={t.key}
              type="button"
              className={`app-tab${t.key === activeKey ? ' active' : ''}${dragOverKey === t.key ? ' drag-over' : ''}${t.closable ? ' is-draggable' : ' is-pinned'}`}
              draggable={t.closable}
              onDragStart={(e) => onTabDragStart(e, t.key, t.closable)}
              onDragOver={(e) => onTabDragOver(e, t.key, t.closable)}
              onDrop={(e) => onTabDrop(e, t.key, t.closable)}
              onDragEnd={onTabDragEnd}
              onClick={() => onTabClick(t.key)}
              title={t.closable ? `${t.title}（可拖动排序）` : t.title}
            >
              <span className="app-tab-title">{t.title}</span>
              {t.closable ? (
                <span
                  className="app-tab-close"
                  role="button"
                  tabIndex={0}
                  aria-label="关闭"
                  draggable={false}
                  onClick={(e) => onTabClose(e, t.key)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') onTabClose(e as unknown as React.MouseEvent, t.key)
                  }}
                >
                  <CloseOutlined style={{ fontSize: 10 }} />
                </span>
              ) : null}
            </button>
          ))}
        </div>

        <Content
          style={{
            flex: '1 1 0',
            minHeight: 0,
            minWidth: 0,
            display: 'flex',
            flexDirection: 'column',
            padding: 0,
            background: 'var(--app-content-bg)',
          }}
        >
          <div className="app-tab-panels">
            {tabs.map((t) => (
              <div key={t.key} className={`app-tab-panel${t.key === activeKey ? ' active' : ''}`}>
                <TabPagePanel pathname={t.path} search={t.search} />
              </div>
            ))}
          </div>
        </Content>
      </Layout>
    </Layout>
  )
}
