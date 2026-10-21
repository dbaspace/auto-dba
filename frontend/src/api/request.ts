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

export type NdjsonEvent = { type?: string; step?: any; data?: any; message?: string }

/** 按行读取 NDJSON，每收到一行就回调。用于助手执行过程。timeoutMs 默认 6 分钟。external 用于手动停止当前这一条。 */
export async function streamNdjson(
  url: string,
  body: unknown,
  onEvent: (ev: NdjsonEvent) => void,
  timeoutMs = 360000,
  external?: AbortSignal,
) {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Accept: 'application/x-ndjson',
  }
  const token = localStorage.getItem('token')
  if (token) headers.Authorization = `Bearer ${token}`
  try {
    const u = JSON.parse(localStorage.getItem('user') || '{}')
    const tid = u?.current_tenant?.id || u?.tenant_id
    if (tid) headers['X-Tenant-Id'] = tid
  } catch {
    /* ignore */
  }
  const ctrl = new AbortController()
  const timer = setTimeout(() => {
    if (!ctrl.signal.aborted) ctrl.abort(new DOMException('回答生成超时', 'TimeoutError'))
  }, timeoutMs)
  const onExternal = () => {
    if (ctrl.signal.aborted) return
    const reason = external?.reason
    ctrl.abort(reason instanceof DOMException ? reason : new DOMException('已停止', 'AbortError'))
  }
  if (external) {
    if (external.aborted) onExternal()
    else external.addEventListener('abort', onExternal)
  }
  try {
    const res = await fetch(`/api/v1${url}`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
      signal: ctrl.signal,
    })
    if (!res.ok) {
      let msg = res.statusText || '请求失败'
      try {
        const parsed = await res.json()
        if (parsed?.message) msg = parsed.message
      } catch {
        /* keep status text */
      }
      throw new Error(msg)
    }
    if (!res.body) throw new Error('没有响应流')
    const reader = res.body.getReader()
    const dec = new TextDecoder()
    let buf = ''
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop() || ''
      for (const line of lines) {
        const text = line.trim()
        if (!text) continue
        onEvent(JSON.parse(text))
      }
    }
    if (buf.trim()) onEvent(JSON.parse(buf.trim()))
  } catch (e) {
    if (ctrl.signal.aborted) {
      const reason = ctrl.signal.reason
      const msg = reason instanceof Error && reason.message ? reason.message : '回答生成超时'
      if (/without reason|aborted/i.test(msg) && msg !== '已停止' && msg !== '回答生成超时') {
        throw new Error('回答生成超时')
      }
      throw new Error(msg)
    }
    throw e
  } finally {
    clearTimeout(timer)
    external?.removeEventListener('abort', onExternal)
  }
}

export default client
