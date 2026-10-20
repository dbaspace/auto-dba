import api from '../../../api/request'

export type K8sCluster = {
  id: string
  name: string
  tenant_id?: string
  env_id?: string
  region_id?: string
  access_mode: string
  auth_type: string
  api_server?: string
  credential_set?: boolean
  agent_id?: string
  impersonate_enabled?: boolean
  version?: string
  node_count?: number
  status: string
  last_probe_at?: string
  last_probe_error?: string
  visibility?: string
  remark?: string
}

export async function listClusters() {
  const r = await api.get('/k8s/clusters')
  return (r.data.data || []) as K8sCluster[]
}

export async function createCluster(body: Record<string, unknown>) {
  const r = await api.post('/k8s/clusters', body)
  return r.data.data as K8sCluster
}

export async function updateCluster(id: string, body: Record<string, unknown>) {
  const r = await api.patch(`/k8s/clusters/${id}`, body)
  return r.data.data as K8sCluster
}

export async function deleteCluster(id: string) {
  await api.delete(`/k8s/clusters/${id}`)
}

export async function probeCluster(id: string) {
  const r = await api.post(`/k8s/clusters/${id}/probe`)
  return r.data.data
}

export async function getOverview(id: string) {
  const r = await api.get(`/k8s/clusters/${id}/overview`)
  return r.data.data
}

export async function listResources(clusterId: string, gvr: string, params?: Record<string, string>) {
  const r = await api.get(`/k8s/clusters/${clusterId}/resources/${gvr}`, { params })
  return r.data.data
}

export async function getResource(clusterId: string, gvr: string, ns: string, name: string, yaml = true) {
  const r = await api.get(`/k8s/clusters/${clusterId}/resources/${gvr}/${ns || '_'}/${name}`, {
    params: yaml ? { yaml: '1' } : {},
  })
  return r.data.data
}

export async function getPodLog(clusterId: string, ns: string, name: string, params?: Record<string, string>) {
  const r = await api.get(`/k8s/clusters/${clusterId}/pods/${ns}/${name}/log`, { params })
  return r.data.data
}

export async function scaleWorkload(clusterId: string, kind: string, ns: string, name: string, replicas: number) {
  await api.post(`/k8s/clusters/${clusterId}/workloads/${kind}/${ns}/${name}/scale`, { replicas })
}

export async function restartWorkload(clusterId: string, kind: string, ns: string, name: string) {
  await api.post(`/k8s/clusters/${clusterId}/workloads/${kind}/${ns}/${name}/restart`)
}

export async function applyYAML(clusterId: string, yaml: string, dryRun: boolean) {
  const r = await api.post(`/k8s/clusters/${clusterId}/apply`, { yaml, dry_run: dryRun })
  return r.data.data
}

export async function detectOperators(clusterId: string) {
  const r = await api.post(`/k8s/clusters/${clusterId}/operators/detect`)
  return r.data.data || []
}

export async function listDBClusters(clusterId?: string) {
  const r = await api.get('/k8s/db-clusters', { params: clusterId ? { cluster_id: clusterId } : {} })
  return r.data.data || []
}

export async function createDBCluster(clusterId: string, body: Record<string, unknown>) {
  const r = await api.post(`/k8s/clusters/${clusterId}/db-clusters`, body)
  return r.data.data
}

export async function multiSummary() {
  const r = await api.get('/k8s/summary')
  return r.data.data
}

export async function assignNamespace(clusterId: string, body: { namespace: string; tenant_id: string }) {
  const r = await api.post(`/k8s/clusters/${clusterId}/namespaces/assign`, body)
  return r.data.data
}

const STORAGE_KEY = 'keeplus.k8s.picker'

export function loadPicker(): { clusterId?: string; ns?: string } {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
  } catch {
    return {}
  }
}

export function savePicker(v: { clusterId?: string; ns?: string }) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(v))
}
