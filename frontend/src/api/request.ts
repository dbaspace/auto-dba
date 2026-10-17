import axios, { AxiosRequestConfig } from 'axios'
import { message } from 'antd'

// 扩展 config：支持 silent（静默错误）
declare module 'axios' {
  export interface AxiosRequestConfig {
    silent?: boolean
  }
}

const client = axios.create({ baseURL: '/api/v1', timeout: 30000 })

// 主动退出或已提示过会话失效后，后续并发 401 不再弹窗
let authEnded = false

export function beginLogout() {
  authEnded = true
}

function onLoginPage() {
  return location.hash.startsWith('#/login') || location.hash === '' || location.hash === '#/'
}

function isBenignCdcMiss(url: string | undefined, status: number | undefined): boolean {
  if (status !== 404) return false
  return /\/cdc-sync\/tasks\/(undefined|null|create)(\/|$)/.test(String(url || ''))
}

client.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('token')
  if (token) {
    cfg.headers.Authorization = `Bearer ${token}`
    if (authEnded && !onLoginPage()) authEnded = false
  }
  try {
    const u = JSON.parse(localStorage.getItem('user') || '{}')
    const tid = u?.current_tenant?.id || u?.tenant_id
    if (tid) cfg.headers['X-Tenant-Id'] = tid
  } catch {
    /* ignore */
  }
  return cfg
})

client.interceptors.response.use(
  (res) => res,
  async (err) => {
    const status = err.response?.status
    let msg = err.response?.data?.message || err.message || '请求失败'
    const data = err.response?.data
    if (data instanceof Blob) {
      try {
        const parsed = JSON.parse(await data.text())
        if (parsed?.message) msg = parsed.message
      } catch {
        /* keep default */
      }
    }
    if (status === 401) {
      const code = err.response?.data?.code
      if (code === 'session_revoked') {
        msg = err.response?.data?.message || '会话已失效，请重新登录'
      } else if (/idle|未操作|idle timeout/i.test(String(msg))) {
        msg = msg || '长时间未操作，请重新登录'
      }
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      sessionStorage.removeItem('auto-dba-query-workspace')
      const isLoginCall = /\/auth\/login/.test(String(err.config?.url || ''))
      if (isLoginCall) {
        message.error(msg)
      } else if (!authEnded && !onLoginPage()) {
        authEnded = true
        message.error(msg)
        location.hash = '#/login'
      }
    } else if (!(err.config as AxiosRequestConfig)?.silent && !isBenignCdcMiss(err.config?.url, status)) {
      message.error(msg)
    }
    return Promise.reject(err)
  },
)

export default client
