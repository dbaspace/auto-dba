import { createElement } from 'react'
import { message, Modal } from 'antd'
import api from '../../api/request'

export const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'TXT', 'MX']

export const TTL_OPTIONS = [
  { label: '30 秒', value: 30 },
  { label: '1 分钟', value: 60 },
  { label: '5 分钟', value: 300 },
  { label: '10 分钟', value: 600 },
  { label: '30 分钟', value: 1800 },
  { label: '1 小时', value: 3600 },
  { label: '12 小时', value: 43200 },
  { label: '24 小时', value: 86400 },
]

export const STATUS_OPTIONS = [
  { value: 'Active', label: '运行' },
  { value: 'Removed', label: '停止' },
]

export const SOURCE_TEXT: Record<string, string> = {
  ui: '页面',
  import: '导入',
  rollback: '回滚',
  ticket: '工单',
  ttl: 'TTL 预降',
}

export const ACTION_TEXT: Record<string, string> = { create: '新增', update: '修改', delete: '删除' }
export const OBJECT_TEXT: Record<string, string> = { zone: '解析域', range: '解析范围', record: '记录' }

export interface Zone {
  id: string
  origin: string
  status: string
  ttl?: number
  updated_time?: number
}

export interface Range {
  id: string
  name: string
  network_list: string[]
  description: string
  status: string
}

export interface HostRef {
  addr: string
  instance_id: string
  instance_name: string
  db_type: string
}

export interface TTLHold {
  id: string
  record_id: string
  record_name?: string
  record_type?: string
  zone_origin?: string
  orig_ttl: number
  lowered_ttl: number
  restore_at: number
  batch_id: string
  actor: string
  status: string
  restored_time: number
  message: string
  created_time: number
}

export interface Rec {
  id: string
  name: string
  type: string
  data: string
  mail_host?: string
  preference?: number
  ttl: number
  zone_id: string
  zone_origin?: string
  parsing_range_id: string
  parsing_range_name?: string
  network_list?: string[]
  description: string
  status: string
  updated_time?: number
  instances?: HostRef[]
  ttl_hold?: TTLHold
}

export interface Issue {
  level: 'error' | 'warn' | 'info'
  code: string
  message: string
  record_ids?: string[]
  names?: string[]
}

export interface AppliedItem {
  seq: number
  action: string
  object: string
  id: string
  summary: string
}

export interface ZonePolicy {
  zone_id: string
  require_approval: boolean
  editor_role_ids: string[]
  env: string
  updated_by?: string
  updated_time?: number
}

export interface DnsOp {
  action: string
  object: string
  id?: string
  after?: any
  expect?: any
}

export function errMsg(e: any, fallback = '操作失败'): string {
  return e?.response?.data?.message || e?.message || fallback
}

export function fmtTime(sec?: number): string {
  if (!sec) return '-'
  const d = new Date(sec * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function recordValue(r: { type: string; data: string; mail_host?: string; preference?: number }): string {
  if (r.type === 'MX') {
    if (r.mail_host) return `${r.preference ?? 0} ${r.mail_host}`
    const [h, p] = String(r.data || '').split('|')
    if (p !== undefined) return `${p} ${h}`
  }
  return r.data
}

export const ISSUE_COLOR: Record<string, string> = { error: 'red', warn: 'orange', info: 'blue' }
export const ISSUE_TEXT: Record<string, string> = { error: '错误', warn: '提示', info: '信息' }

/** 统一处理写接口返回：转审批、校验提示、成功。返回 true 表示已落库或已提交审批。 */
export function handleWriteResult(data: any, okText = '已保存'): boolean {
  if (data?.pending_approval) {
    const no = data?.ticket?.ticket_no || data?.ticket?.id || ''
    Modal.info({
      title: '已提交审批',
      content: `该解析域需要审批，已生成 DNS 变更工单 ${no}，审批通过后自动执行。`,
    })
    return true
  }
  const warns: Issue[] = data?.warnings || []
  if (warns.length) {
    message.warning({ content: `${okText}，校验提示：${warns.map((w) => w.message).join('；')}`, duration: 6 })
  } else {
    message.success(okText)
  }
  return true
}

/** 写接口失败：校验错误、冲突逐条展示。 */
export function handleWriteError(e: any, fallback = '保存失败') {
  const d = e?.response?.data
  const lines: string[] = (d?.issues || []).map((i: Issue) => i.message).concat(d?.conflicts || [])
  if (lines.length) {
    Modal.error({
      title: d?.conflicts?.length ? '数据已被修改，无法执行' : '校验未通过',
      content: createElement('div', { style: { whiteSpace: 'pre-wrap' } }, lines.join('\n')),
    })
    return
  }
  message.error(errMsg(e, fallback))
}

export async function loadZones(): Promise<Zone[]> {
  const { data } = await api.get('/dns/zones')
  return data.data || []
}

export async function loadRanges(includeDefault = false): Promise<Range[]> {
  const { data } = await api.get('/dns/ranges', { params: includeDefault ? { include_default: 1 } : {} })
  return data.data || []
}

export async function downloadExport(params: Record<string, string>) {
  const res = await api.get('/dns/export', { params, responseType: 'blob' })
  const dispo = String(res.headers?.['content-disposition'] || '')
  const m = /filename="?([^"]+)"?/.exec(dispo)
  const name = m?.[1] || `dns-export.${params.format === 'zone' ? 'zone' : 'csv'}`
  const url = URL.createObjectURL(res.data)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}
