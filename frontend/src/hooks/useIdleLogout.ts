import { useEffect, useRef } from 'react'
import { message } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useAppDispatch } from '../store'
import { clearAuth } from '../store/authSlice'
import { resetTabs } from '../store/tabsSlice'

const LAST_ACTIVITY_KEY = 'last_activity_at'
const CHECK_MS = 15_000
const THROTTLE_MS = 1_000

function readLastActivity(): number {
  const raw = localStorage.getItem(LAST_ACTIVITY_KEY)
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? n : 0
}

export function touchLastActivity() {
  localStorage.setItem(LAST_ACTIVITY_KEY, String(Date.now()))
}

function clearLastActivity() {
  localStorage.removeItem(LAST_ACTIVITY_KEY)
}

/**
 * 登录闲置超时：无操作达到 timeoutMin 分钟后清登录态并跳转登录页。
 * timeoutMin <= 0 表示关闭。
 */
export default function useIdleLogout(timeoutMin: number | undefined | null) {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const mins = Number(timeoutMin)
  const enabled = Number.isFinite(mins) && mins > 0
  const loggingOut = useRef(false)

  useEffect(() => {
    if (!enabled) return

    loggingOut.current = false
    const limitMs = mins * 60_000
    let lastTouch = 0

    const logout = () => {
      if (loggingOut.current) return
      loggingOut.current = true
      clearLastActivity()
      dispatch(clearAuth())
      dispatch(resetTabs({}))
      sessionStorage.removeItem('auto-dba-query-workspace')
      message.warning('登录已超时，请重新登录')
      navigate('/login', { replace: true })
    }

    const check = () => {
      const last = readLastActivity()
      if (!last) {
        touchLastActivity()
        return
      }
      if (Date.now() - last > limitMs) logout()
    }

    const onActivity = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
        return
      }
      const now = Date.now()
      const last = readLastActivity()
      if (last && now - last > limitMs) {
        logout()
        return
      }
      if (now - lastTouch < THROTTLE_MS) return
      lastTouch = now
      touchLastActivity()
    }

    // 挂载时若已超时则立即退出，避免刷新续命
    const existing = readLastActivity()
    if (existing && Date.now() - existing > limitMs) {
      logout()
      return
    }
    if (!existing) touchLastActivity()

    const events = ['mousemove', 'mousedown', 'keydown', 'scroll', 'touchstart', 'visibilitychange'] as const
    for (const ev of events) {
      window.addEventListener(ev, onActivity, { passive: true })
    }
    const timer = window.setInterval(check, CHECK_MS)

    return () => {
      for (const ev of events) {
        window.removeEventListener(ev, onActivity)
      }
      window.clearInterval(timer)
    }
  }, [dispatch, navigate, enabled, mins])
}

export { clearLastActivity, LAST_ACTIVITY_KEY }
