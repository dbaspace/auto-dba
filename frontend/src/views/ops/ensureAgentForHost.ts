import api from '../../api/request'
import { message } from 'antd'

export type AgentLite = { id: string; name?: string; base_url: string }

function endpointHost(ep: string): string {
  const s = (ep || '').trim()
  if (!s) return ''
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end > 0) return s.slice(1, end)
  }
  const idx = s.lastIndexOf(':')
  if (idx > 0 && !s.includes('://')) {
    const maybePort = s.slice(idx + 1)
    if (/^\d+$/.test(maybePort)) return s.slice(0, idx)
  }
  return s
}

/** 从 Agent base_url 解析主机（忽略端口与 scheme） */
export function agentUrlHost(baseUrl: string): string {
  const s = (baseUrl || '').trim()
  if (!s) return ''
  try {
    const u = new URL(s.includes('://') ? s : `http://${s}`)
    return (u.hostname || '').replace(/^\[|\]$/g, '')
  } catch {
    return endpointHost(s.replace(/^https?:\/\//, ''))
  }
}

export function matchAgentByHost(agents: AgentLite[], host: string): AgentLite | undefined {
  const h = endpointHost(host) || host.trim()
  if (!h) return undefined
  return agents.find((a) => agentUrlHost(a.base_url) === h)
}

export type EnsureAgentOpts = {
  agents: AgentLite[]
  host: string
  /** 默认端口，如 8091 / 8092 */
  port: number
  /** 创建 API，如 /sr-cluster/agents、/db-deploy/agents */
  createPath: string
  /** Agent 名称前缀，如 sr / db */
  namePrefix: string
  /** false 时仅匹配不创建（平台 Agent） */
  allowCreate?: boolean
  /** 创建成功后刷新列表 */
  reloadAgents?: () => Promise<AgentLite[]>
  silent?: boolean
}

/**
 * 按 Host 匹配已有 Agent；未命中且允许创建时登记 http://host:port 并返回新 id。
 */
export async function ensureAgentForHost(opts: EnsureAgentOpts): Promise<string | undefined> {
  const host = endpointHost(opts.host) || String(opts.host || '').trim()
  if (!host) return undefined
  const hit = matchAgentByHost(opts.agents, host)
  if (hit?.id) return hit.id
  if (opts.allowCreate === false) {
    if (!opts.silent) {
      message.warning(`主机 ${host} 尚无匹配的 Agent，请先在配置中心登记或下发`)
    }
    return undefined
  }
  const baseURL = `http://${host}:${opts.port}`
  try {
    const { data } = await api.post(opts.createPath, {
      name: `${opts.namePrefix}-${host}`,
      base_url: baseURL,
    })
    const created = data?.data as AgentLite | undefined
    if (opts.reloadAgents) {
      const list = await opts.reloadAgents()
      const again = matchAgentByHost(list, host)
      if (again?.id) return again.id
    }
    if (created?.id) return created.id
  } catch (e: any) {
    if (!opts.silent) {
      message.warning(
        e?.response?.data?.message ||
          `自动登记 Agent ${baseURL} 失败，请使用「按主机登记」或到 Agent Tab 手工登记`,
      )
    }
  }
  return undefined
}

export { endpointHost }
