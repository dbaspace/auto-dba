import React from 'react'
import ReactDOM from 'react-dom/client'
import { Provider } from 'react-redux'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { RouterProvider } from 'react-router-dom'
import 'dayjs/locale/zh-cn'
import { store } from './store'
import router from './router'
import './styles/global.css'

function loginHashURL(query: string): string {
  const q = query.startsWith('?') || query.startsWith('#') ? query.replace(/^[#?]/, '') : query
  const origin = window.location.origin
  return q ? `${origin}/#/login?${q}` : `${origin}/#/login`
}

/** IdP 回调到 path /auth/callback（非 hash）；换票后再进 HashRouter 登录页。 */
async function handleSSOFrontendCallback(): Promise<boolean> {
  if (window.location.pathname !== '/auth/callback') {
    return false
  }
  const params = new URLSearchParams(window.location.search)
  const err = params.get('error')
  const errDesc = params.get('error_description')
  if (err) {
    const msg = errDesc ? `${err}: ${errDesc}` : err
    window.location.replace(loginHashURL(`error=${encodeURIComponent(msg)}`))
    return true
  }
  const code = params.get('code') || ''
  const state = params.get('state') || ''
  const redirectURI = `${window.location.origin}${window.location.pathname}`
  try {
    const res = await fetch('/api/v1/auth/sso/exchange', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        code,
        state,
        redirect_uri: redirectURI,
      }),
    })
    const data = (await res.json().catch(() => ({}))) as {
      ticket?: string
      redirect?: string
      message?: string
    }
    if (!res.ok || !data.ticket) {
      const msg = data.message || `SSO 换票失败 (${res.status})`
      window.location.replace(loginHashURL(`error=${encodeURIComponent(msg)}`))
      return true
    }
    const q = new URLSearchParams()
    q.set('sso_ticket', data.ticket)
    if (data.redirect) {
      q.set('redirect', data.redirect)
    }
    window.location.replace(loginHashURL(q.toString()))
  } catch (e) {
    const msg = e instanceof Error ? e.message : 'SSO 换票网络错误'
    window.location.replace(loginHashURL(`error=${encodeURIComponent(msg)}`))
  }
  return true
}

function mountApp() {
  ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
    <React.StrictMode>
      <Provider store={store}>
        <ConfigProvider
          locale={zhCN}
          theme={{
            token: {
              colorPrimary: '#4f5fb8',
              colorLink: '#2563eb',
              borderRadius: 2,
              fontSize: 12,
            },
          }}
        >
          <RouterProvider router={router} />
        </ConfigProvider>
      </Provider>
    </React.StrictMode>,
  )
}

void handleSSOFrontendCallback().then((handled) => {
  if (!handled) {
    mountApp()
  }
})
