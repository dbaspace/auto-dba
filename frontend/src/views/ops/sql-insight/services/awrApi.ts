import api from '../../../../api/request'

export async function fetchAwrReports(params: Record<string, any>) {
  const { data } = await api.get('/awr-reports', { params })
  return data
}

export async function fetchAwrDetail(params: Record<string, any>) {
  const { data } = await api.get('/awr-reports/detail', { params })
  return data?.data || null
}

export async function exportAwrReport(params: Record<string, any>) {
  const res = await api.get('/awr-reports/export', {
    params,
    responseType: 'blob',
  })
  return res.data
}

export async function startAwrGenerate(body: Record<string, any> = {}) {
  const { data } = await api.post('/awr-reports/generate', body)
  return data?.data || null
}

export async function fetchAwrGenerateStatus(jobId: string) {
  const { data } = await api.get('/awr-reports/generate/status', { params: { job_id: jobId } })
  return data?.data || null
}

export async function fetchAwrGenerateJobs(limit = 20) {
  const { data } = await api.get('/awr-reports/generate/jobs', { params: { limit } })
  const payload = data?.data
  if (Array.isArray(payload)) return payload
  if (payload && typeof payload === 'object' && Array.isArray((payload as any).list)) return (payload as any).list
  return []
}

export async function pauseAwrGenerate(jobId: string) {
  const { data } = await api.post('/awr-reports/generate/pause', { job_id: jobId })
  return data
}

export async function resumeAwrGenerate(jobId: string) {
  const { data } = await api.post('/awr-reports/generate/resume', { job_id: jobId })
  return data
}

export async function cancelAwrGenerate(jobId: string) {
  const { data } = await api.post('/awr-reports/generate/cancel', { job_id: jobId })
  return data
}

export async function fetchAwrSummary(reportDate: string) {
  const { data } = await api.get('/awr-reports/summary', { params: { report_date: reportDate } })
  return data?.data || null
}

export function parseAwrSections(raw: any): any {
  if (!raw) return null
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw)
    } catch {
      return null
    }
  }
  return raw
}
