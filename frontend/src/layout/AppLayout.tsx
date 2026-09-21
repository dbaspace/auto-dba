import React, { useEffect, useMemo, useState } from 'react'
import { Layout, Menu } from 'antd'
import type { MenuProps } from 'antd'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import api from '../api/request'
import { resolveMenuIcon } from '../components/MenuIcon'
import { useAppDispatch, useAppSelector } from '../store'
import { setUser, MenuNode } from '../store/authSlice'
import { firstAllowed } from '../constants/routes'
import UserAccountMenu from './UserAccountMenu'
import UserWatermark from '../components/UserWatermark'
import useIdleLogout from '../hooks/useIdleLogout'

const { Sider, Header, Content } = Layout

function buildMenuItems(nodes: MenuNode[]): MenuProps['items'] {
  return (nodes || [])
    .map((n) => {
      if (n.children?.length) {
        return {
          key: n.id || n.title,
          icon: resolveMenuIcon(n.icon),
          label: n.title,
          children: buildMenuItems(n.children),
        }
      }
      if (n.path) {
        return {
          key: n.path,
          icon: resolveMenuIcon(n.icon),
          label: n.title,
        }
      }
      return null
    })
    .filter(Boolean) as MenuProps['items']
}

/** menus 指纹：变化时 remount Menu，避免 defaultOpenKeys 陈旧 */
function menusFingerprint(nodes: MenuNode[] | undefined): string {
  const parts: string[] = []
  const walk = (list: MenuNode[] | undefined) => {
    for (const n of list || []) {
      parts.push(`${n.id || ''}|${n.path || ''}|${n.title || ''}`)
      if (n.children?.length) walk(n.children)
    }
  }
  walk(nodes)
  return String(parts.length) + ':' + parts.join(';')
}

function findMenuTrail(nodes: MenuNode[] | undefined, path: string, trail: MenuNode[] = []): MenuNode[] {
  if (!path) return []
  let best: MenuNode[] = []
  for (const n of nodes || []) {
    const next = [...trail, n]
    if (n.path && (n.path === path || path.startsWith(n.path + '/'))) {
      const deeper = findMenuTrail(n.children, path, next)
      const hit = deeper.length ? deeper : next
      if (hit.length > best.length) best = hit
      continue
    }
    if (n.children?.length) {
      const hit = findMenuTrail(n.children, path, next)
      if (hit.length > best.length) best = hit
    }
  }
  return best
}

function firstPath(nodes: MenuNode[] | undefined): string {
  for (const n of nodes || []) {
    if (n.path) return n.path
    const c = firstPath(n.children)
    if (c) return c
  }
  return '/403'
}

function collectOpenKeys(nodes: MenuNode[] | undefined, path: string, acc: string[] = []): string[] {
  for (const n of nodes || []) {
    if (n.children?.length) {
      const hit = findMenuTrail([n], path)
      if (hit.length) acc.push(n.id || n.title)
      collectOpenKeys(n.children, path, acc)
    }
  }
  return acc
}

export default function AppLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const user = useAppSelector((s) => s.auth.user)
  const menus = useAppSelector((s) => s.auth.menus)

  useIdleLogout(user?.login_idle_timeout_min as number | undefined)

  const path = location.pathname
  const activeMenu = useMemo(() => {
    if (path.startsWith('/ops/flashback/history/')) return '/ops/flashback/history'
    return path
  }, [path])

  const menuItems = useMemo(() => buildMenuItems(menus), [menus])
  const menuKey = useMemo(() => menusFingerprint(menus), [menus])
  const routeOpenKeys = useMemo(() => collectOpenKeys(menus, activeMenu), [menus, activeMenu])
  const [openKeys, setOpenKeys] = useState<string[]>([])

  // menus / 当前路由变化时，合并路径所需的展开项（保留用户手动展开）
  useEffect(() => {
    setOpenKeys((prev) => Array.from(new Set([...prev, ...routeOpenKeys])))
  }, [routeOpenKeys, menuKey])

  useEffect(() => {
    let mounted = true
    async function refreshMe() {
      try {
        const { data } = await api.get('/auth/me')
        const u = data.data || {}
        if (!mounted) return
        localStorage.setItem('user', JSON.stringify(u))
        dispatch(setUser(u))
      } catch {
        /* ignore */
      }
    }
    refreshMe()
    const handler = () => refreshMe()
    window.addEventListener('menus-changed', handler)
    return () => {
      mounted = false
      window.removeEventListener('menus-changed', handler)
    }
  }, [dispatch])

  function goPath(index: string) {
    if (!index || !index.startsWith('/')) return
    if (index === path) return
    navigate(index)
  }

  function goHome() {
    navigate(firstPath(menus))
  }

  return (
    <Layout style={{ height: '100%', width: '100%', overflow: 'hidden', position: 'relative' }}>
      <UserWatermark />
      <Sider
        width={220}
        className="app-sider"
        style={{
          background: '#fff',
          overflow: 'hidden',
          borderRight: '1px solid #f0f0f0',
          height: '100%',
        }}
      >
        <div
          onClick={goHome}
          style={{
            height: 56,
            color: '#1f2937',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontWeight: 700,
            letterSpacing: '0.5px',
            cursor: 'pointer',
            flexShrink: 0,
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          auto-dba
        </div>
        <div className="app-sider-menu">
          <Menu
            key={menuKey}
            theme="light"
            mode="inline"
            selectedKeys={[activeMenu]}
            openKeys={openKeys}
            onOpenChange={(keys) => setOpenKeys(keys as string[])}
            items={menuItems}
            style={{ background: '#fff', borderRight: 'none' }}
            onClick={({ key }) => goPath(String(key))}
          />
        </div>
      </Sider>
      <Layout style={{ minWidth: 0, minHeight: 0 }}>
        <Header
          style={{
            height: 56,
            lineHeight: '56px',
            background: '#fff',
            display: 'flex',
            alignItems: 'center',
            gap: 16,
            padding: '0 16px',
            flexShrink: 0,
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          <div style={{ flex: 1 }} />
          <UserAccountMenu user={user} />
        </Header>
        <Content style={{ flex: '1 1 0', minHeight: 0, minWidth: 0, overflow: 'auto', padding: 8, background: '#fff' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}

export { firstAllowed }
