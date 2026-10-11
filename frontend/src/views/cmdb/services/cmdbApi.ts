import api from '../../../api/request'

export type CmdbCI = {
  id: string
  type: string
  name: string
  status?: string
  env_id?: string
  region_id?: string
  tags?: string[]
  attrs?: Record<string, unknown>
  updated_at?: string
  edit_path?: string
  health_badges?: string[]
  app_tags?: string[]
}

export type CmdbGraph = {
  nodes: CmdbCI[]
  edges: { src_type: string; src_id: string; rel_type: string; dst_type: string; dst_id: string; source: string; remark?: string }[]
}

export type CmdbImpact = {
  root: CmdbCI
  direction: string
  depth: number
  summary: {
    upstream: number
    downstream: number
    by_type: Record<string, number>
    app_tags?: string[]
    biz_tags?: string[]
  }
  nodes: CmdbCI[]
  edges: CmdbGraph['edges']
}

export type CmdbSnapshot = {
  id: string
  ci_type: string
  ci_id: string
  hash: string
  payload: Record<string, unknown>
  is_baseline: boolean
  actor?: string
  created_at: string
}

export type CmdbDrift = {
  ci_type: string
  ci_id: string
  name?: string
  field: string
  baseline: unknown
  current: unknown
  baseline_at?: string
}

export const CI_TYPES = [
  { value: 'instance', label: '实例' },
  { value: 'database', label: '已登记库' },
  { value: 'host', label: '主机' },
  { value: 'sr_cluster', label: 'StarRocks 集群' },
  { value: 'db_deploy_cluster', label: '部署集群' },
  { value: 'proxy_service', label: '代理服务' },
  { value: 'db_account', label: '数据库账号' },
  { value: 'cdc_task', label: 'CDC 任务' },
  { value: 'region', label: '区域' },
  { value: 'environment', label: '环境' },
  { value: 'connector', label: '连接器' },
  { value: 'monitor_job', label: '监控 Job' },
  { value: 'monitor_group', label: '监控 Target' },
  { value: 'ha_cluster', label: 'HA 集群' },
  { value: 'backup_policy', label: '备份策略' },
]

export async function listCIs(params: Record<string, string | number | undefined>) {
  const { data } = await api.get('/cmdb/cis', { params })
  return (data?.data || []) as CmdbCI[]
}

export async function getCI(type: string, id: string) {
  const { data } = await api.get(`/cmdb/cis/${type}/${encodeURIComponent(id)}`)
  return data as { data: CmdbCI; tags?: string[]; tag_map?: Record<string, string> }
}

export async function getGraph(type: string, id: string, depth = 1) {
  const { data } = await api.get(`/cmdb/cis/${type}/${encodeURIComponent(id)}/graph`, { params: { depth } })
  return (data?.data || { nodes: [], edges: [] }) as CmdbGraph
}

export async function getImpact(type: string, id: string, direction = 'down', depth = 2) {
  const { data } = await api.get(`/cmdb/cis/${type}/${encodeURIComponent(id)}/impact`, { params: { direction, depth } })
  return data?.data as CmdbImpact
}

export async function listSnapshots(type: string, id: string) {
  const { data } = await api.get(`/cmdb/cis/${type}/${encodeURIComponent(id)}/snapshots`)
  return (data?.data || []) as CmdbSnapshot[]
}

export async function createSnapshot(type: string, id: string, baseline = false) {
  const { data } = await api.post(`/cmdb/cis/${type}/${encodeURIComponent(id)}/snapshots`, null, {
    params: baseline ? { baseline: 1 } : undefined,
  })
  return data?.data as CmdbSnapshot
}

export async function listDrifts() {
  const { data } = await api.get('/cmdb/drifts')
  return (data?.data || []) as CmdbDrift[]
}

export async function listByApp(tag: string) {
  const { data } = await api.get('/cmdb/by-app', { params: { tag } })
  return (data?.data || []) as CmdbCI[]
}
