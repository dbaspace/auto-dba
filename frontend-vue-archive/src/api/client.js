import axios from 'axios'
import { ElMessage } from 'element-plus'

const client = axios.create({ baseURL: '/api/v1', timeout: 30000 })

function isBenignCdcMiss(url, status) {
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
      } catch { /* keep default */ }
    }
    if (status === 401) {
      localStorage.removeItem('token')
      localStorage.removeItem('user')
      if (location.hash.includes('/login')) {
        ElMessage.error(msg)
      } else {
        location.hash = '#/login'
      }
    } else if (!err.config?.silent && !isBenignCdcMiss(err.config?.url, status)) {
      ElMessage.error(msg)
    }
    return Promise.reject(err)
  }
)

export default client
