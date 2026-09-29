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

export async function fetchRedisRdbInstances(q?: string) {
  const { data } = await api.get('/redis-rdb/instances', { params: { q: q || undefined } })
  return (data?.data?.instances || []) as any[]
}

export async function fetchRedisRdbOnline(instanceId: string) {
  const { data } = await api.get('/redis-rdb/online', {
    params: { instance_id: instanceId },
    timeout: 60000,
  })
  return data?.data || null
}

export async function uploadRedisRdb(file: File, instanceId?: string, note?: string) {
  const fd = new FormData()
  fd.append('file', file)
  if (instanceId) fd.append('instance_id', instanceId)
  if (note) fd.append('note', note)
  const { data } = await api.post('/redis-rdb/upload', fd, {
    timeout: 300000,
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return data?.data || null
}

export async function fetchRedisRdbAgents() {
  const { data } = await api.get('/redis-rdb/agents')
  return (data?.data?.agents || []) as any[]
}

export async function fetchRedisRdbViaAgent(instanceId: string, agentId: string, note?: string) {
  const { data } = await api.post(
    '/redis-rdb/fetch',
    { instance_id: instanceId, agent_id: agentId, note: note || undefined },
    { timeout: 120000 },
  )
  return data?.data || null
}

export async function fetchRedisRdbJobs() {
  const { data } = await api.get('/redis-rdb/jobs')
  return (data?.data?.jobs || []) as any[]
}

export async function fetchRedisRdbJob(id: string) {
  const { data } = await api.get(`/redis-rdb/jobs/${id}`)
  return data?.data || null
}

export async function fetchRedisRdbReport(id: string) {
  const { data } = await api.get(`/redis-rdb/jobs/${id}/report`)
  return data?.data || null
}

export async function deleteRedisRdbJob(id: string) {
  await api.delete(`/redis-rdb/jobs/${id}`)
}
