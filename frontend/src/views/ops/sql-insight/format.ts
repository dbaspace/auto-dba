/** SQL 洞察共用格式化工具 */
import { message } from 'antd'

export function pad(n: number) {
  return String(n).padStart(2, '0')
}

export function formatTime(v: any) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return String(v)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function formatDuration(seconds: any) {
  if (seconds == null || !Number.isFinite(Number(seconds))) return '-'
  const s = Math.max(0, Number(seconds))
  if (s < 1) return `${s.toFixed(2)}s`
  if (s < 60) return `${s.toFixed(1)}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
}

export function durationClass(v: any) {
  const s = v ?? 0
  if (s >= 10) return 'dur-danger'
  if (s >= 1) return 'dur-warn'
  return ''
}

export function stateColor(state: any): string {
  const s = String(state || '').toLowerCase()
  if (s.includes('active')) return 'success'
  if (s.includes('idle in transaction')) return 'warning'
  if (s.includes('abort')) return 'error'
  return 'default'
}

export function truncate(text: any, n: number) {
  if (!text) return '-'
  return text.length > n ? `${text.slice(0, n)}…` : text
}

export async function copyText(text: any) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(String(text))
    message.success('已复制')
  } catch {
    message.error('复制失败')
  }
}
