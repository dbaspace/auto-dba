import React, { useCallback, useEffect, useState } from 'react'
import { Tooltip } from 'antd'
import { DoubleLeftOutlined, DoubleRightOutlined } from '@ant-design/icons'
import './CollapsibleSide.css'

function readCollapsed(key: string): boolean {
  try {
    return sessionStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

function writeCollapsed(key: string, v: boolean) {
  try {
    sessionStorage.setItem(key, v ? '1' : '0')
  } catch {
    /* ignore */
  }
}

export interface CollapsibleSideProps {
  storageKey: string
  /** 展开时宽度；收起时为 0 */
  width?: number | string
  title?: React.ReactNode
  className?: string
  style?: React.CSSProperties
  children: React.ReactNode
  onCollapsedChange?: (collapsed: boolean) => void
}

/** 查询页左侧实例/树面板：右缘悬浮收起/展开，折叠偏好写入 sessionStorage */
export default function CollapsibleSide({
  storageKey,
  width = 260,
  title,
  className = '',
  style,
  children,
  onCollapsedChange,
}: CollapsibleSideProps) {
  const [collapsed, setCollapsed] = useState(() => readCollapsed(storageKey))

  useEffect(() => {
    onCollapsedChange?.(collapsed)
  }, [collapsed, onCollapsedChange])

  const setCollapsedPersist = useCallback(
    (next: boolean) => {
      setCollapsed(next)
      writeCollapsed(storageKey, next)
    },
    [storageKey],
  )

  const toggleBtn = (
    <Tooltip title={collapsed ? '展开侧栏' : '收起侧栏'} placement="right">
      <button
        type="button"
        className={`collapsible-side-float-btn${collapsed ? ' is-collapsed' : ''}`}
        aria-label={collapsed ? '展开侧栏' : '收起侧栏'}
        onClick={() => setCollapsedPersist(!collapsed)}
      >
        {collapsed ? <DoubleRightOutlined /> : <DoubleLeftOutlined />}
      </button>
    </Tooltip>
  )

  if (collapsed) {
    return (
      <div className="collapsible-side-collapsed">
        {toggleBtn}
      </div>
    )
  }

  const w = typeof width === 'number' ? `${width}px` : width

  return (
    <aside
      className={`collapsible-side ${className}`.trim()}
      style={{ ...style, width: w, flexShrink: 0 }}
    >
      {title ? (
        <div className="collapsible-side-head">
          <span className="collapsible-side-title">{title}</span>
        </div>
      ) : null}
      <div className="collapsible-side-body">{children}</div>
      {toggleBtn}
    </aside>
  )
}
