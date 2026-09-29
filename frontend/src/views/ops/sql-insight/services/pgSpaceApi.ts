import api from '../../../../api/request'

export function formatBytes(n?: number | null): string {
  if (n == null || !Number.isFinite(n)) return '-'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = Math.abs(n)
  let i = 0
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024
    i++
  }
  return `${n < 0 ? '-' : ''}${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`
}

export async function fetchPgSpaceSummary(instanceId: string, topN = 20) {
  const { data } = await api.get('/pg-space/summary', {
    params: { instance_id: instanceId, top_n: topN },
  })
  return data?.data || null
}

export async function fetchPgSpaceRealtime(instanceId: string, dbName: string, topN = 20) {
  const { data } = await api.get('/pg-space/realtime', {
    params: {
      instance_id: instanceId,
      db_name: dbName || undefined,
      top_n: topN,
    },
    timeout: 120000,
  })
  return data?.data || null
}

export async function fetchPgSpaceDatabases(instanceId: string) {
  const { data } = await api.get('/pg-space/databases', {
    params: { instance_id: instanceId },
  })
  return data?.data || { databases: [] }
}

export async function fetchPgSpaceSnapshots(params: Record<string, any>) {
  const { data } = await api.get('/pg-space/snapshots', { params })
  return data?.data || { list: [], total: 0 }
}

export async function fetchPgSpaceTrend(params: Record<string, any>) {
  const { data } = await api.get('/pg-space/trend', { params })
  return data?.data || null
}

export async function fetchPgSpaceAdvice(instanceId: string) {
  const { data } = await api.get('/pg-space/advice', {
    params: { instance_id: instanceId },
  })
  return data?.data || null
}

export async function fetchPgSpaceAdviceSummary() {
  const { data } = await api.get('/pg-space/advice/summary')
  return data?.data || null
}

export async function startPgSpaceCollect(instanceId?: string) {
  const { data } = await api.post('/pg-space/collect', {
    instance_id: instanceId || undefined,
  })
  return data?.data || null
}

export async function fetchPgSpaceCollectStatus(jobId: string) {
  const { data } = await api.get('/pg-space/collect/status', { params: { job_id: jobId } })
  return data?.data || null
}

export async function fetchPgSpaceCollectJobs(limit = 20) {
  const { data } = await api.get('/pg-space/collect/jobs', { params: { limit } })
  const payload = data?.data
  if (Array.isArray(payload)) return payload
  if (payload && typeof payload === 'object' && Array.isArray((payload as any).list)) return (payload as any).list
  return []
}

export async function pausePgSpaceCollect(jobId: string) {
  const { data } = await api.post('/pg-space/collect/pause', { job_id: jobId })
  return data
}

export async function resumePgSpaceCollect(jobId: string) {
  const { data } = await api.post('/pg-space/collect/resume', { job_id: jobId })
  return data
}

export async function cancelPgSpaceCollect(jobId: string) {
  const { data } = await api.post('/pg-space/collect/cancel', { job_id: jobId })
  return data
}

export function isCollectActive(state?: string) {
  const s = String(state || '').toLowerCase()
  return ['pending', 'running', 'paused', 'processing'].includes(s)
}

