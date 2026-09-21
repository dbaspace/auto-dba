import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Card, Input, Select, Space, message } from 'antd'
import api from '../api/request'
import { useAppDispatch } from '../store'
import { setUser } from '../store/authSlice'
import loginBg from '../assets/login-bg.jpg'
import BrandMark from '../components/BrandMark'

const MODE_LABEL: Record<string, string> = {
  platform: '平台',
  ldap: 'LDAP',
  sso: 'SSO',
}

const LOOPBACK_HOSTS = new Set(['localhost', '127.0.0.1', '::1', '[::1]'])

/** 当前页与 SSO 回调配置在 loopback 上混用 localhost / 127.0.0.1 时返回 true */
function isLoopbackHostMismatch(pageOrigin: string, callbackURL: string): boolean {
  try {
    const page = new URL(pageOrigin)
    const cb = new URL(callbackURL)
    const ph = page.hostname.toLowerCase()
    const ch = cb.hostname.toLowerCase()
    if (!LOOPBACK_HOSTS.has(ph) || !LOOPBACK_HOSTS.has(ch)) {
      return false
    }
    return ph !== ch
  } catch {
    return false
  }
}

export default function Login() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const dispatch = useAppDispatch()
  const [loading, setLoading] = useState(false)
  const [ssoLoading, setSsoLoading] = useState(false)
  const [authHint, setAuthHint] = useState('')
  const [authModes, setAuthModes] = useState<string[]>(['platform'])
  const [ssoGrantMode, setSsoGrantMode] = useState('')
  const [ssoCallbackURL, setSsoCallbackURL] = useState('')
  const [authMode, setAuthMode] = useState('platform')
  const [error, setError] = useState('')
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('admin')
  const [requireCaptcha, setRequireCaptcha] = useState(false)
  const [captchaId, setCaptchaId] = useState('')
  const [captchaImg, setCaptchaImg] = useState('')
  const [captchaCode, setCaptchaCode] = useState('')
  const [retryAfter, setRetryAfter] = useState(0)
  const timerRef = useRef<number>(0)
  const ticketHandled = useRef(false)

  const showModeSelect = authModes.length > 1
  const ssoAuthorize = authMode === 'sso' && ssoGrantMode === 'authorization_code'

  const modeOptions = useMemo(
    () => authModes.map((m) => ({ value: m, label: MODE_LABEL[m] || m })),
    [authModes],
  )

  function finishLogin(data: any) {
    localStorage.setItem('token', data.token)
    localStorage.setItem('user', JSON.stringify(data.user))
    localStorage.setItem('last_activity_at', String(Date.now()))
    dispatch(setUser(data.user))
    message.success('登录成功')
    setRequireCaptcha(false)
    startCooldown(0)
    const first = (data.user?.menus || []).flatMap(function walk(n: any): string[] {
      return n.path ? [n.path] : (n.children || []).flatMap(walk)
    })[0]
    navigate(params.get('redirect') || first || '/403', { replace: true })
  }

  useEffect(() => {
    api
      .get('/info')
      .then(({ data }) => {
        const modes = Array.isArray(data.auth_modes) ? data.auth_modes.filter(Boolean) : ['platform']
        const list = modes.length ? modes : ['platform']
        setAuthModes(list)
        const grant = String(data.sso_grant_mode || '')
        setSsoGrantMode(grant)
        const callbackURL = String(data.sso_callback_url || '').trim()
        setSsoCallbackURL(callbackURL)
        const priority = Array.isArray(data.auth_priority) ? data.auth_priority : []
        const preferred =
          priority.find((m: string) => list.includes(m)) || list.find((m) => m !== 'platform') || list[0]
        setAuthMode(preferred)
        if (list.includes('sso') && grant === 'authorization_code') {
          setAuthHint('SSO 可输入账号密码登录，或使用下方跳转授权登录')
          if (callbackURL && isLoopbackHostMismatch(window.location.origin, callbackURL)) {
            message.warning('请统一使用 127.0.0.1 或 localhost 打开页面（与认证配置中的 SSO 回调一致）')
          }
        } else if (list.length > 1) {
          setAuthHint(`可选认证方式：${list.map((m: string) => MODE_LABEL[m] || m).join(' / ')}`)
        } else {
          setAuthHint('')
        }
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    const ticket = params.get('sso_ticket')
    if (!ticket || ticketHandled.current) return
    ticketHandled.current = true
    setSsoLoading(true)
    setError('')
    api
      .get('/auth/sso/ticket', { params: { ticket } })
      .then(({ data }) => finishLogin(data))
      .catch((e: any) => {
        setError(e?.response?.data?.message || e?.message || 'SSO 登录失败')
      })
      .finally(() => setSsoLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  useEffect(() => {
    const err = params.get('error')
    if (err && !params.get('sso_ticket')) {
      setError(err)
    }
  }, [params])

  useEffect(() => {
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current)
    }
  }, [])

  const startCooldown = useCallback((sec: number) => {
    if (timerRef.current) window.clearInterval(timerRef.current)
    const n = Math.max(0, Math.floor(sec || 0))
    setRetryAfter(n)
    if (n <= 0) return
    timerRef.current = window.setInterval(() => {
      setRetryAfter((prev) => {
        if (prev <= 1) {
          window.clearInterval(timerRef.current)
          timerRef.current = 0
          return 0
        }
        return prev - 1
      })
    }, 1000)
  }, [])

  async function loadCaptcha() {
    try {
      const { data } = await api.get('/auth/captcha')
      setCaptchaId(data.captcha_id || '')
      setCaptchaImg(data.image_base64 || '')
      setCaptchaCode('')
    } catch {
      message.error('验证码加载失败')
    }
  }

  function startSSOAuthorize() {
    if (ssoCallbackURL && isLoopbackHostMismatch(window.location.origin, ssoCallbackURL)) {
      try {
        const cb = new URL(ssoCallbackURL)
        const q = new URLSearchParams()
        const redirect = params.get('redirect') || ''
        if (redirect) q.set('redirect', redirect)
        message.info('已切换到与 SSO 回调一致的地址，请再次点击 SSO 登录')
        const qs = q.toString()
        window.location.replace(`${cb.origin}/#/login${qs ? `?${qs}` : ''}`)
        return
      } catch {
        message.warning('请统一使用 127.0.0.1 或 localhost 打开页面（与认证配置中的 SSO 回调一致）')
        return
      }
    }
    const frontend = window.location.origin
    const redirect = params.get('redirect') || ''
    const q = new URLSearchParams()
    q.set('frontend', frontend)
    if (redirect) q.set('redirect', redirect)
    window.location.href = `/api/v1/auth/sso/login?${q.toString()}`
  }

  async function onSubmit() {
    if (retryAfter > 0) {
      setError(`请等待 ${Math.ceil(retryAfter / 60)} 分钟后再试`)
      return
    }
    if (!username.trim()) {
      setError('请输入用户名')
      return
    }
    if (!password && authMode === 'sso' && ssoAuthorize) {
      // 授权码模式未填密码时，跳转 IdP
      startSSOAuthorize()
      return
    }
    setLoading(true)
    setError('')
    try {
      localStorage.removeItem('token')
      const payload: Record<string, string> = {
        username,
        password,
        auth_mode: authMode || 'platform',
      }
      if (requireCaptcha) {
        payload.captcha_id = captchaId
        payload.captcha_code = captchaCode
      }
      const { data } = await api.post('/auth/login', payload)
      finishLogin(data)
    } catch (e: any) {
      const body = e?.response?.data || {}
      const msg = body.message || e?.message || '登录失败'
      setError(msg)
      if (body.require_captcha || (body.fail_count || 0) >= 3) {
        setRequireCaptcha(true)
        await loadCaptcha()
      }
      if (body.retry_after_sec > 0) {
        startCooldown(body.retry_after_sec)
      }
    } finally {
      setLoading(false)
    }
  }

  const cooldownHint =
    retryAfter > 0
      ? `认证失败次数过多，请 ${Math.ceil(retryAfter / 60)} 分钟后再试（剩余 ${retryAfter} 秒）`
      : ''

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        padding: 24,
        background: `linear-gradient(180deg, rgba(12,24,48,0.38), rgba(12,24,48,0.52)), url(${loginBg}) center / cover no-repeat`,
      }}
    >
      <Card style={{ width: 'min(400px, 100%)', borderRadius: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <BrandMark size={56} />
          <h2 style={{ margin: 0, textAlign: 'center' }}>auto-dba</h2>
        </div>
        <p style={{ color: '#909399', fontSize: 13, textAlign: 'center' }}>运维自助独立工作台</p>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            onSubmit()
          }}
        >
          {showModeSelect ? (
            <Select
              style={{ width: '100%', marginBottom: 12 }}
              value={authMode}
              options={modeOptions}
              onChange={(v) => {
                setAuthMode(v)
                if (v === 'sso' && (username === 'admin' || password === 'admin')) {
                  setUsername('')
                  setPassword('')
                }
              }}
              disabled={retryAfter > 0 || ssoLoading}
            />
          ) : null}
          <Input
            style={{ marginBottom: 12 }}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="用户名"
            disabled={retryAfter > 0 || ssoLoading}
          />
          <Input.Password
            style={{ marginBottom: 12 }}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="密码"
            disabled={retryAfter > 0 || ssoLoading}
          />
          {requireCaptcha ? (
            <div style={{ marginBottom: 12 }}>
              <Space.Compact style={{ width: '100%' }}>
                <Input
                  value={captchaCode}
                  onChange={(e) => setCaptchaCode(e.target.value)}
                  placeholder="请输入验证码"
                  disabled={retryAfter > 0}
                />
                <Button
                  style={{ width: 150, padding: 0, height: 32 }}
                  onClick={loadCaptcha}
                  disabled={retryAfter > 0}
                  title="点击刷新验证码"
                >
                  {captchaImg ? (
                    <img src={captchaImg} alt="captcha" style={{ height: 30, display: 'block', margin: '0 auto' }} />
                  ) : (
                    '获取验证码'
                  )}
                </Button>
              </Space.Compact>
            </div>
          ) : null}
          <Button
            type="primary"
            block
            loading={loading || ssoLoading}
            disabled={retryAfter > 0}
            onClick={onSubmit}
          >
            {retryAfter > 0 ? `请稍候（${retryAfter}s）` : '登录'}
          </Button>
          <div style={{ textAlign: 'right', marginTop: 8 }}>
            <Link to="/forgot-password" style={{ fontSize: 13 }}>
              忘记密码？
            </Link>
          </div>
          {ssoAuthorize ? (
            <Button
              type="link"
              block
              style={{ marginTop: 4 }}
              disabled={retryAfter > 0 || loading || ssoLoading}
              onClick={startSSOAuthorize}
            >
              跳转 SSO 授权登录
            </Button>
          ) : null}
        </form>
        {(error || cooldownHint) && (
          <p style={{ color: '#c0352b', fontSize: 13, marginTop: 8, textAlign: 'center' }}>
            {cooldownHint || error}
          </p>
        )}
        <p style={{ color: '#909399', fontSize: 13, textAlign: 'center', marginTop: 8 }}>
          {authHint || '默认账号 admin / admin'}
        </p>
      </Card>
    </div>
  )
}
