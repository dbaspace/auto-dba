export type AlertItem = {
  id: string
  fingerprint: string
  rule_id: string
  severity: string
  title: string
  service: string
  cluster: string
  labels: Record<string, string>
  annotations: Record<string, string>
  status: string
  handle: string
  owner: string
  cur_value: string
  threshold: string
  aggregate_count: number
  started_at: string
  resolved_at: string
  pending_since: string
  last_notified_at?: string
  notify_count?: number
  created_at: string
  updated_at: string
}

export type AlertTier = {
  op: '>=' | '>' | '<=' | '<' | string
  value: number
  severity: string
  for?: string
}

export type AlertNotifyPolicy = {
  max_fire_per_window: number
  window: string
  repeat_interval: string
  repeat_on_handles: string[]
  inherit_global?: boolean
}

export type AlertEvent = {
  id: string
  alert_id: string
  action: string
  detail: string
  operator: string
  created_at: string
}

export type AlertScope = {
  mode: 'global' | 'instances' | 'cluster' | 'tags' | string
  matcher_key?: string
  instance_ids?: string[]
  cluster?: { idc: string; domain: string }
  tags?: string[]
  matcher?: string
}

export type AlertExprTemplate = {
  id: string
  name: string
  expr: string
  metric_expr?: string
  tiers?: AlertTier[]
  service: string
  matcher_key: string
  severity: string
  for_duration: string
  summary: string
}

export type AlertRule = {
  id: string
  name: string
  expr: string
  metric_expr?: string
  severity: string
  for_duration: string
  labels: Record<string, string>
  annotations: Record<string, string>
  cluster: string
  scope: AlertScope
  tiers?: AlertTier[]
  notify_policy?: AlertNotifyPolicy
  compiled_expr?: string
  compiled_at?: string
  compile_error?: string
  enabled: boolean
  health: number
  triggered_7d: number
  noise_rate: number
  created_by: string
  created_at: string
  updated_at: string
}

export type AlertSilence = {
  id: string
  name: string
  matchers: string
  starts_at: string
  ends_at: string
  reason: string
  status: string
  created_by: string
  affected?: number
}

export type AlertStats = {
  active: number
  unclaimed: number
  p0: number
  mtta: string
  mttr: string
}

export const SEV_COLOR: Record<string, string> = {
  P0: 'red',
  P1: 'orange',
  P2: 'blue',
  P3: 'default',
}

export const HANDLE_LABEL: Record<string, { label: string; color: string }> = {
  unclaimed: { label: '待认领', color: 'gold' },
  acknowledged: { label: '已认领', color: 'processing' },
  processing: { label: '处理中', color: 'purple' },
  silenced: { label: '已静默', color: 'default' },
  resolved: { label: '已恢复', color: 'success' },
}

export const SILENCE_LABEL: Record<string, { label: string; color: string }> = {
  active: { label: '生效中', color: 'success' },
  pending: { label: '待生效', color: 'warning' },
  expired: { label: '已过期', color: 'default' },
}

export function durationText(startedAt?: string) {
  if (!startedAt) return '—'
  const ms = Date.now() - new Date(startedAt).getTime()
  if (Number.isNaN(ms) || ms < 0) return '—'
  const mins = Math.floor(ms / 60000)
  if (mins < 60) return `${mins}m`
  const h = Math.floor(mins / 60)
  const m = mins % 60
  if (h < 48) return `${h}h ${m}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}
