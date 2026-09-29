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
}

export type CmdbGraph = {
  nodes: CmdbCI[]
  edges: { src_type: string; src_id: string; rel_type: string; dst_type: string; dst_id: string; source: string; remark?: string }[]
}

export const CI_TYPES = [
  { value: 'instance', label: '实例' },
  { value: 'database', label: '已登记库' },
  { value: 'host', label: '主机' },
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
