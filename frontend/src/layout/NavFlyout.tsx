import React, { useEffect, useRef, useState } from 'react'
import { AppstoreOutlined } from '@ant-design/icons'
import type { MenuNode } from '../store/authSlice'
import { resolveMenuIcon, menuIconTone } from '../components/MenuIcon'
import DbTypeIcon from '../components/DbTypeIcon'
import api from '../api/request'
import {
  INSTANCES_PATH,
  groupInstanceTypes,
  instanceTypeSearch,
  isInstancesPath,
  parseInstanceTypes,
  type InstanceTypeOption,
} from '../utils/instanceTypes'
import './NavFlyout.css'

interface Props {
  group: MenuNode | null
  open: boolean
  activePath: string
  activeSearch?: string
  onSelect: (path: string, title: string, search?: string) => void
  onClose: () => void
  /** 指针进入/离开弹层（用于与侧栏跨间隙 hover 关闭） */
  onHoverChange?: (inside: boolean) => void
  /** 侧栏右缘，用于定位 */
  anchorLeft: number
}

type FlyoutLink = { title: string; path: string; icon?: string }
type FlyoutSection = { title: string; icon?: string; links: FlyoutLink[] }

/** 一级菜单下直挂叶子组成的分组，默认沿用一级菜单名；此处按菜单 id 单独命名 */
const SECTION_TITLE_BY_GROUP: Record<string, string> = { 'm-db-config': '数据库配置' }

function leafIcon(n: MenuNode, fallback?: string): string | undefined {
  return n.icon || fallback
}

function flattenLeaves(node: MenuNode, fallbackIcon?: string): FlyoutLink[] {
  const out: FlyoutLink[] = []
  const walk = (n: MenuNode, fb?: string) => {
    if (n.path) {
      out.push({ title: n.title, path: n.path, icon: leafIcon(n, fb) })
    }
    const nextFb = n.icon || fb
    for (const c of n.children || []) walk(c, nextFb)
  }
  for (const c of node.children || []) walk(c, node.icon || fallbackIcon)
  return out
}

function renderIcon(name?: string, color?: string): React.ReactNode {
  return resolveMenuIcon(name, color) || <AppstoreOutlined style={color ? { color } : undefined} />
}

function dbTypeFromActiveSearch(search?: string) {
  const s = String(search || '')
  const q = s.startsWith('?') ? s.slice(1) : s
  return new URLSearchParams(q).get('db_type')?.trim() || ''
}

export default function NavFlyout({
  group,
  open,
  activePath,
  activeSearch,
  onSelect,
  onClose,
  onHoverChange,
  anchorLeft,
}: Props) {
  const panelRef = useRef<HTMLDivElement>(null)
  const [instanceTypes, setInstanceTypes] = useState<InstanceTypeOption[]>([])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (panelRef.current?.contains(t)) return
      const sider = (t as HTMLElement)?.closest?.('.app-sider')
      if (sider) return
      onClose()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('mousedown', onDown)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('mousedown', onDown)
    }
  }, [open, onClose])

  useEffect(() => {
    if (!open || !group) return
    const hasInstances = (group.children || []).some((c) => {
      if (isInstancesPath(c.path)) return true
      return (c.children || []).some((x) => isInstancesPath(x.path))
    })
    if (!hasInstances) {
      setInstanceTypes([])
      return
    }
    let cancelled = false
    api
      .get('/global-args')
      .then((res) => {
        if (cancelled) return
        const args = res.data?.data || []
        const raw = args.find((a: { key?: string }) => a.key === 'ticket_instance_types')?.value || ''
        setInstanceTypes(parseInstanceTypes(raw))
      })
      .catch(() => {
        if (!cancelled) setInstanceTypes([])
      })
    return () => {
      cancelled = true
    }
  }, [open, group])

  if (!open || !group) return null

  const directSectionTitle = SECTION_TITLE_BY_GROUP[group.id || ''] || group.title
  const sections = (group.children || []).map((child) => {
    if (child.children?.length) {
      return {
        title: child.title,
        icon: child.icon,
        links: flattenLeaves(child, group.icon),
      }
    }
    if (child.path) {
      return {
        title: directSectionTitle,
        icon: group.icon,
        links: [{ title: child.title, path: child.path, icon: leafIcon(child, group.icon) }],
      }
    }
    return null
  })

  const leafOnly = (group.children || []).every((c) => c.path && !c.children?.length)
  const blocks: FlyoutSection[] = leafOnly
    ? [
        {
          title: directSectionTitle,
          icon: group.icon,
          links: (group.children || [])
            .filter((c) => c.path)
            .map((c) => ({
              title: c.title,
              path: c.path as string,
              icon: leafIcon(c, group.icon),
            })),
        },
      ]
    : (sections.filter(Boolean) as FlyoutSection[])

  const merged: FlyoutSection[] = []
  for (const b of blocks) {
    const exist = merged.find((m) => m.title === b.title)
    if (exist) {
      for (const l of b.links) {
        if (!exist.links.some((x) => x.path === l.path)) exist.links.push(l)
      }
    } else merged.push({ ...b, links: [...b.links] })
  }

  const activeDbType = isInstancesPath(activePath) ? dbTypeFromActiveSearch(activeSearch) : ''
  const showTypeTags = instanceTypes.length > 0

  const total = merged.reduce((n, s) => {
    let count = 0
    let hasInst = false
    for (const l of s.links) {
      if (isInstancesPath(l.path) && showTypeTags) {
        hasInst = true
        continue
      }
      count += 1
    }
    if (hasInst) count += instanceTypes.length
    return n + count
  }, 0)

  return (
    <div
      ref={panelRef}
      className="nav-flyout"
      style={{ left: anchorLeft }}
      role="dialog"
      onMouseEnter={() => onHoverChange?.(true)}
      onMouseLeave={() => onHoverChange?.(false)}
      aria-label={group.title}
    >
      <div className="nav-flyout-head">
        <span className="nav-flyout-head-badge">{renderIcon(group.icon)}</span>
        <div className="nav-flyout-head-text">
          <div className="nav-flyout-head-title">{group.title}</div>
          <div className="nav-flyout-head-sub">共 {total} 项功能</div>
        </div>
      </div>
      <div className="nav-flyout-body">
        {merged.map((sec) => {
          const normalLinks = sec.links.filter((l) => !(isInstancesPath(l.path) && showTypeTags))
          const hasInstances = sec.links.some((l) => isInstancesPath(l.path))
          const showInstBlock = hasInstances && showTypeTags

          return (
            <React.Fragment key={sec.title}>
              {showInstBlock ? (
                <div className="nav-flyout-section nav-flyout-section-instances">
                  <div className="nav-flyout-section-title">
                    <span className="nav-flyout-section-bar" aria-hidden />
                    <span>实例管理</span>
                  </div>
                  {groupInstanceTypes(instanceTypes).map((g) => (
                    <div key={g.title} className="nav-flyout-type-group">
                      <div className="nav-flyout-type-group-title">{g.title}</div>
                      <div className="nav-flyout-type-tags">
                        {g.items.map((t) => {
                          const active = activeDbType === t.value
                          return (
                            <button
                              key={t.value}
                              type="button"
                              className={`nav-flyout-type-tag${active ? ' active' : ''}`}
                              onClick={() => onSelect(INSTANCES_PATH, t.label, instanceTypeSearch(t.value))}
                            >
                              <DbTypeIcon type={t.value} size={16} />
                              <span className="nav-flyout-type-tag-text">{t.label}</span>
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}
              {normalLinks.length || !showInstBlock ? (
                <div className="nav-flyout-section nav-flyout-section-menus">
                  <div className="nav-flyout-section-title">
                    <span className="nav-flyout-section-bar" aria-hidden />
                    <span>{sec.title}</span>
                  </div>
                  {(normalLinks.length ? normalLinks : sec.links).length ? (
                    <div
                      className={`nav-flyout-links${
                        (normalLinks.length ? normalLinks : sec.links).length >= 4 ? ' cols-3' : ''
                      }`}
                    >
                      {(normalLinks.length ? normalLinks : sec.links).map((l) => {
                        const active = activePath === l.path || activePath.startsWith(l.path + '/')
                        const iconName = l.icon || sec.icon || group.icon
                        const tone = menuIconTone(iconName)
                        return (
                          <button
                            key={l.path}
                            type="button"
                            className={`nav-flyout-link${active ? ' active' : ''}`}
                            onClick={() => onSelect(l.path, l.title)}
                          >
                            <span
                              className="nav-flyout-link-ico"
                              style={{ color: tone.color, background: tone.background }}
                            >
                              {renderIcon(iconName, tone.color)}
                            </span>
                            <span className="nav-flyout-link-text">{l.title}</span>
                          </button>
                        )
                      })}
                    </div>
                  ) : null}
                </div>
              ) : null}
            </React.Fragment>
          )
        })}
      </div>
    </div>
  )
}
