import React, { useMemo } from 'react'
import { useAppSelector } from '../store'
import './UserWatermark.css'

function truthy(v: unknown) {
  if (v === true || v === 1) return true
  const s = String(v ?? '').trim().toLowerCase()
  return s === '1' || s === 'true' || s === 'yes' || s === 'on'
}

/** 全局用户水印：由全局参数 user_watermark_enabled 控制 */
export default function UserWatermark() {
  const user = useAppSelector((s) => s.auth.user)
  const enabled = truthy(user?.user_watermark_enabled)
  const text = useMemo(() => {
    return String(user?.username || '').trim()
  }, [user?.username])

  if (!enabled || !text) return null

  return (
    <div className="user-watermark" aria-hidden="true">
      {Array.from({ length: 48 }).map((_, i) => (
        <span key={i}>{text}</span>
      ))}
    </div>
  )
}
