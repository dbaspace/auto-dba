import axios, { AxiosRequestConfig } from 'axios'
import { message } from 'antd'

// 扩展 config：支持 silent（静默错误）
declare module 'axios' {
  export interface AxiosRequestConfig {
    silent?: boolean
  }
}

const client = axios.create({ baseURL: '/api/v1', timeout: 30000 })

function isBenignCdcMiss(url: string | undefined, status: number | undefined): boolean {
  if (status !== 404) return false
  return /\/cdc-sync\/tasks\/(undefined|null|create)(\/|$)/.test(String(url || ''))
}

client.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('token')
  if (token) cfg.headers.Authorization = `Bearer ${token}`
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
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      sessionStorage.removeItem('auto-dba-query-workspace')
      if (location.hash.includes('/login')) {
        message.error(msg)
      } else {
        location.hash = '#/login'
      }
    } else if (!(err.config as AxiosRequestConfig)?.silent && !isBenignCdcMiss(err.config?.url, status)) {
      message.error(msg)
    }
    return Promise.reject(err)
  },
)

export default client
