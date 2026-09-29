import React, { useState } from 'react'
import { Popover, Tooltip } from 'antd'
import {
  BgColorsOutlined,
  CheckOutlined,
  DesktopOutlined,
  MoonOutlined,
  SunOutlined,
} from '@ant-design/icons'
import { useAppearance } from '../theme/AppearanceProvider'
import { THEME_PRESETS, type ColorMode, type ThemePresetId } from '../theme/appearance'

interface Props {
  collapsed: boolean
  onOpenChange?: (open: boolean) => void
}

export default function AppearancePopover({ collapsed, onOpenChange }: Props) {
  const { preset, mode, setPreset, setMode } = useAppearance()
  const [open, setOpen] = useState(false)

  function handleOpen(next: boolean) {
    setOpen(next)
    onOpenChange?.(next)
  }

  const panel = (
    <div className="app-appearance-panel">
      <div className="app-appearance-section-title">外观主题</div>
      <div className="app-appearance-grid">
        {THEME_PRESETS.map((p) => {
          const active = preset === p.id
          return (
            <button
              key={p.id}
              type="button"
              className={`app-appearance-card${active ? ' is-active' : ''}`}
              onClick={() => setPreset(p.id as ThemePresetId)}
            >
              <div className="app-appearance-thumb">
                <div
                  className="app-appearance-thumb-sider"
                  style={{ background: p.thumbSider }}
                />
                <div className="app-appearance-thumb-main">
                  <div className="app-appearance-thumb-header" />
                  <div
                    className="app-appearance-thumb-body"
                    style={{ ['--thumb-accent' as string]: p.primary, borderLeftColor: p.primary }}
                  />
                </div>
              </div>
              <div className="app-appearance-card-label">{p.label}</div>
              {active ? (
                <span className="app-appearance-check">
                  <CheckOutlined />
                </span>
              ) : null}
            </button>
          )
        })}
      </div>
      <div className="app-appearance-section-title">显示模式</div>
      <div className="app-appearance-mode">
        {(
          [
            { id: 'light' as ColorMode, label: '浅色', icon: <SunOutlined /> },
            { id: 'dark' as ColorMode, label: '深色', icon: <MoonOutlined /> },
            { id: 'system' as ColorMode, label: '系统', icon: <DesktopOutlined /> },
          ] as const
        ).map((m) => (
          <button
            key={m.id}
            type="button"
            className={`app-appearance-mode-btn${mode === m.id ? ' is-active' : ''}`}
            onClick={() => setMode(m.id)}
          >
            {m.icon}
            <span>{m.label}</span>
          </button>
        ))}
      </div>
    </div>
  )

  const trigger = (
    <button type="button" className="app-sider-footer-btn" aria-label="外观主题">
      <BgColorsOutlined />
      <span className="app-sider-label">外观主题</span>
    </button>
  )

  return (
    <Popover
      content={panel}
      trigger="click"
      open={open}
      onOpenChange={handleOpen}
      placement="rightBottom"
      arrow={false}
      overlayInnerStyle={{ padding: 12 }}
    >
      {collapsed ? (
        <Tooltip title="外观主题" placement="right">
          {trigger}
        </Tooltip>
      ) : (
        trigger
      )}
    </Popover>
  )
}
