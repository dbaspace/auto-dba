export type InsightTabKey = 'sql' | 'analysis' | 'visual' | 'awr' | 'space' | 'smart'

export interface SnapshotFilter {
  instanceId: string
  startTime: string
  endTime: string
  /** 物理实例 host/port，供 AWR / 智能分析等按 hub 契约过滤 */
  host?: string
  port?: number
}

