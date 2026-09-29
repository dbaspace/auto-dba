import React, { useCallback, useRef, useState } from 'react'
import { Alert } from 'antd'
import api from '../../../api/request'

export type HostInstLite = {
  id: string
  name: string
  host?: string
  domain_name?: string
  domains?: string[]
}

export type AgentLite = { id: string; name?: string; base_url: string; host_instance_id?: string }

export type NodePrecheckItem = { name: string; ok: boolean; level?: string; message?: string }
export type NodePrecheckResult = { ok: boolean; summary?: string; items?: NodePrecheckItem[] }

function normHost(s?: string): string {
  let v = String(s || '').trim().toLowerCase()
  if (v.startsWith('[') && v.endsWith(']')) v = v.slice(1, -1)
  return v.replace(/\.$/, '')
}

/** Agent base_url 中的主机名（小写，不含端口）。 */
export function agentHost(baseUrl?: string): string {
  const raw = String(baseUrl || '').trim()
  if (!raw) return ''
  try {
    return normHost(new URL(raw.includes('://') ? raw : `http://${raw}`).hostname)
  } catch {
    return ''
  }
}

/** 主机实例可匹配的地址：host / domain_name / domains。 */
export function hostAddrs(inst?: HostInstLite | null): string[] {
  if (!inst) return []
  const out = [inst.host, inst.domain_name, ...(inst.domains || [])].map(normHost).filter(Boolean)
  return Array.from(new Set(out))
}

export function hostPrimaryAddr(inst?: HostInstLite | null): string {
  if (!inst) return ''
  return String(inst.host || '').trim() || String(inst.domain_name || '').trim()
}

/** 与后端 nodecheck.AgentMatchesHost 一致。 */
export function agentMatchesHost(agent?: AgentLite | null, inst?: HostInstLite | null): boolean {
  if (!agent || !inst) return false
  if (agent.host_instance_id && agent.host_instance_id === inst.id) return true
  const h = agentHost(agent.base_url)
  return !!h && hostAddrs(inst).includes(h)
}

export function agentMismatchMessage(agent: AgentLite, inst: HostInstLite): string {
  return `Agent 地址 ${agentHost(agent.base_url) || agent.base_url} 与所选主机 ${hostPrimaryAddr(inst)} 不一致`
}

/** 集群节点所在主机实例：节点登记 → 节点 Agent 登记 → 按地址匹配主机。 */
export function resolveNodeHostInstanceId(
  node: { host?: string; host_instance_id?: string; agent_id?: string },
  agents: AgentLite[],
  hosts: HostInstLite[],
): string {
  if (node.host_instance_id) return node.host_instance_id
  const ag = node.agent_id ? agents.find((a) => a.id === node.agent_id) : undefined
  if (ag?.host_instance_id) return ag.host_instance_id
  const h = normHost(node.host)
  if (!h) return ''
  return hosts.find((inst) => hostAddrs(inst).includes(h))?.id || ''
}

async function fetchApprovers(instanceId?: string): Promise<string[]> {
  const { data } = await api.get('/tickets/target-approvers', { params: instanceId ? { instance_id: instanceId } : {} })
  return ((data?.data || []) as any[]).map((a) => a?.name || a).filter(Boolean) as string[]
}

/**
 * 按主机实例加载审批人（/tickets/target-approvers）。
 * 传数组时合并多个主机的审批人；空数组取全局兜底列表；undefined 清空。
 */
export function useHostApprovers() {
  const [approvers, setApprovers] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const seq = useRef(0)
  const load = useCallback(async (instanceIds?: string | string[]): Promise<string[]> => {
    const my = ++seq.current
    if (instanceIds === undefined || instanceIds === '') {
      setApprovers([])
      return []
    }
    const ids = Array.from(new Set((Array.isArray(instanceIds) ? instanceIds : [instanceIds]).filter(Boolean)))
    setLoading(true)
    try {
      const lists = ids.length > 0 ? await Promise.all(ids.map((id) => fetchApprovers(id))) : [await fetchApprovers()]
      const list = Array.from(new Set(lists.flat()))
      if (my === seq.current) setApprovers(list)
      return list
    } catch {
      if (my === seq.current) setApprovers([])
      return []
    } finally {
      if (my === seq.current) setLoading(false)
    }
  }, [])
  return { approvers, loading, load }
}

const ITEM_LABEL: Record<string, string> = {
  cluster: '集群状态',
  host_instance: '主机实例',
  agent_match: 'Agent 与主机一致',
  agent_online: 'Agent 在线',
  duplicate: '重复节点',
  role_rule: '角色规则',
  fe_count: 'FE 数量',
  agent_precheck: 'Agent 环境预检',
  approver: '审批人',
  'agent:extra_disk': 'Agent 预检 · 磁盘概况',
  'agent:data_disk_format': 'Agent 预检 · 将格式化磁盘',
  'agent:data_root_nonempty': 'Agent 预检 · 数据目录非空',
}

function itemLabel(name: string): string {
  if (ITEM_LABEL[name]) return ITEM_LABEL[name]
  if (name.startsWith('agent:')) return `Agent 预检 · ${name.slice(6)}`
  return name
}

export function NodePrecheckPanel({ result, busy }: { result: NodePrecheckResult | null; busy?: boolean }) {
  if (busy) return <Alert type="info" showIcon message="正在执行基础校验…" style={{ marginBottom: 12 }} />
  if (!result) {
    return <Alert type="info" showIcon message="请先点击「基础校验」，全部通过后才能提交工单" style={{ marginBottom: 12 }} />
  }
  const hasWarn = (result.items || []).some((it) => it.level === 'warn')
  return (
    <Alert
      type={result.ok ? (hasWarn ? 'warning' : 'success') : 'error'}
      showIcon
      style={{ marginBottom: 12 }}
      message={result.summary || (result.ok ? '基础校验通过' : '基础校验未通过')}
      description={(
        <ul style={{ margin: '8px 0 0', paddingLeft: 18, maxHeight: 220, overflow: 'auto' }}>
          {(result.items || []).map((it, i) => (
            <li
              key={`${it.name}-${i}`}
              style={{ color: !it.ok ? '#a61d24' : it.level === 'warn' ? '#ad6800' : undefined }}
            >
              {!it.ok ? '✗' : it.level === 'warn' ? '!' : '✓'}
              {' '}
              {itemLabel(it.name)}
              {it.message ? ` — ${it.message}` : ''}
            </li>
          ))}
        </ul>
      )}
    />
  )
}
