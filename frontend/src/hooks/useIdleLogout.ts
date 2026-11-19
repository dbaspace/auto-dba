import { useEffect, useRef, useState } from 'react'
import { message } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useAppDispatch } from '../store'
import { clearAuth } from '../store/authSlice'
import { resetTabs } from '../store/tabsSlice'
import api, { beginLogout } from '../api/request'
import { clearSystemNoticeDismissals } from '../utils/systemNoticeSession'
import { clearQueryWorkspaces } from '../views/query/querySession'

const LAST_ACTIVITY_KEY = 'last_activity_at'
const CHECK_MS = 15_000
const THROTTLE_MS = 1_000
const REPORT_MS = 60_000

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
  const [liveMin, setLiveMin] = useState<number | null>(null)
  const fromUser = Number(timeoutMin)
  const mins = liveMin == null ? fromUser : liveMin
  const enabled = Number.isFinite(mins) && mins > 0
  const loggingOut = useRef(false)
  const seenEnabled = useRef(false)
  const wasOff = useRef(false)

  useEffect(() => {
    let stop = false
    const pull = async () => {
      try {
        const { data } = await api.get('/auth/idle-timeout', { silent: true })
        const n = Number(data?.login_idle_timeout_min)
        if (!stop && Number.isFinite(n)) setLiveMin(n)
      } catch {
        /* 401 由请求拦截器退出；其它失败沿用当前分钟数 */
      }
    }
    void pull()
    const timer = window.setInterval(pull, REPORT_MS)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    let lastReport = 0
    const report = () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return
      const now = Date.now()
      if (now - lastReport < REPORT_MS) return
      lastReport = now
      void api.post('/auth/sessions/touch', {}, { silent: true }).catch(() => {})
    }
    const events = ['mousemove', 'mousedown', 'keydown', 'scroll', 'touchstart'] as const
    for (const ev of events) {
      window.addEventListener(ev, report, { passive: true })
    }
    return () => {
      for (const ev of events) {
        window.removeEventListener(ev, report)
      }
    }
  }, [])

  useEffect(() => {
    if (!enabled) {
      if (seenEnabled.current) wasOff.current = true
      return
    }
    const restarted = wasOff.current
    wasOff.current = false
    seenEnabled.current = true

    loggingOut.current = false
    const limitMs = mins * 60_000
    let lastTouch = 0

    const logout = () => {
      if (loggingOut.current) return
      loggingOut.current = true
      beginLogout()
      clearLastActivity()
      dispatch(clearAuth())
      dispatch(resetTabs({}))
      clearQueryWorkspaces()
      clearSystemNoticeDismissals()
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

    // 关闭后再打开，从现在重新计时。只是把分钟数改长或改短时，仍用原来的最后活动时间。
    if (restarted) touchLastActivity()
    const existing = readLastActivity()
    if (!restarted && existing && Date.now() - existing > limitMs) {
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
