import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Button, notification } from 'antd'
import api from '../../api/request'

export type AgentDeployRow = { id: string; host_instance_id: string; status: string; created_at?: string }

/** 超过该时长仍未结束的下发视为后端重启遗留，不再跟踪也不再占用主机（与后端判定一致）。 */
export const AGENT_DEPLOY_STALE_MS = 15 * 60 * 1000

export function isAgentDeployActive(row: AgentDeployRow, now = Date.now()): boolean {
  if (row.status !== 'pending' && row.status !== 'running') return false
  const t = row.created_at ? new Date(row.created_at).getTime() : NaN
  return Number.isNaN(t) || now - t < AGENT_DEPLOY_STALE_MS
}

/** 有进行中下发任务的主机 id，用于在目标主机下拉中置灰。 */
export function activeDeployHostIds(list: AgentDeployRow[]): Set<string> {
  const now = Date.now()
  return new Set(list.filter((d) => isAgentDeployActive(d, now)).map((d) => d.host_instance_id))
}

/**
 * 统一跟踪 Agent 下发任务：提交后 track(id)，列表刷新时 trackFromList(list) 续上页面刷新前的任务。
 * 每 2 秒轮询一次所有进行中的任务，结束后弹通知（带「查看日志」），不阻塞下一台的下发。
 */
export function useAgentDeployTracker<T extends AgentDeployRow>(opts: {
  basePath: string // 如 /sr-cluster/agents/deploys
  agentName: string // 如 Node Agent
  hostLabel: (hostInstanceId: string) => string
  onShowLog: (row: T) => void
  onFinished: (row: T) => void // 单个任务结束（刷新 Agent 列表等）
  refreshList: () => void
}) {
  const [tracking, setTracking] = useState<Record<string, number>>({})
  const optsRef = useRef(opts)
  optsRef.current = opts
  const inFlight = useRef(false)

  const track = useCallback((id: string, startedAt = Date.now()) => {
    if (!id) return
    setTracking((prev) => (prev[id] ? prev : { ...prev, [id]: startedAt }))
  }, [])

  const trackFromList = useCallback((list: AgentDeployRow[]) => {
    const now = Date.now()
    const add: Record<string, number> = {}
    for (const d of list) {
      if (!isAgentDeployActive(d, now)) continue
      const t = d.created_at ? new Date(d.created_at).getTime() : NaN
      add[d.id] = Number.isNaN(t) ? now : t
    }
    if (!Object.keys(add).length) return
    setTracking((prev) => {
      const next = { ...prev }
      let changed = false
      for (const [id, t] of Object.entries(add)) {
        if (!next[id]) { next[id] = t; changed = true }
      }
      return changed ? next : prev
    })
  }, [])

  const ids = Object.keys(tracking).sort().join(',')

  useEffect(() => {
    if (!ids) return
    const tick = async () => {
      if (inFlight.current) return
      inFlight.current = true
      const o = optsRef.current
      const done: string[] = []
      try {
        await Promise.all(Object.entries(tracking).map(async ([id, startedAt]) => {
          let row: T | undefined
          try {
            const res = await api.get(`${o.basePath}/${id}`)
            row = res.data?.data as T
          } catch {
            return
          }
          if (!row) return
          const host = o.hostLabel(row.host_instance_id)
          if (row.status !== 'pending' && row.status !== 'running') {
            done.push(id)
            const ok = row.status === 'success'
            const key = `agent-deploy-${id}`
            const r = row
            notification[ok ? 'success' : 'error']({
              key,
              message: `${host} ${o.agentName} 下发${ok ? '成功' : '失败'}`,
              description: ok ? '已登记，可用于集群绑定。' : '请查看日志定位原因。',
              duration: ok ? 6 : 0,
              btn: (
                <Button size="small" onClick={() => { notification.destroy(key); optsRef.current.onShowLog(r) }}>
                  查看日志
                </Button>
              ),
            })
            o.onFinished(row)
          } else if (Date.now() - startedAt > AGENT_DEPLOY_STALE_MS) {
            done.push(id)
            notification.warning({
              key: `agent-deploy-${id}`,
              message: `${host} ${o.agentName} 下发超过 15 分钟仍未结束`,
              description: '已停止自动跟踪，请稍后手动刷新查看下发记录。',
              duration: 0,
            })
          }
        }))
      } finally {
        inFlight.current = false
      }
      if (done.length) {
        setTracking((prev) => {
          const next = { ...prev }
          done.forEach((id) => { delete next[id] })
          return next
        })
      }
      o.refreshList()
    }
    const timer = setInterval(() => { void tick() }, 2000)
    return () => clearInterval(timer)
    // tracking 的内容由 ids 唯一决定
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ids])

  return { track, trackFromList, trackingCount: Object.keys(tracking).length }
}
