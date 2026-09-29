import api from '../../../../api/request'
import { asArray } from './asArray'

export async function fetchAnalysisRuns(params: Record<string, any> = {}) {
  const { data } = await api.get('/sql-insight/analysis/runs', { params })
  return data
}

export async function fetchAnalysisRun(id: string) {
  const { data } = await api.get(`/sql-insight/analysis/runs/${id}`)
  return data?.data || null
}

export async function createAnalysisRun(body: Record<string, any>) {
  const { data } = await api.post('/sql-insight/analysis/runs', body)
  return data?.data || null
}

export async function fetchAnalysisFindings(params: Record<string, any>) {
  const { data } = await api.get('/sql-insight/analysis/findings', { params })
  return data
}

export async function feedbackFinding(id: string, body: { feedback: string; feedback_note?: string }) {
  const { data } = await api.post(`/sql-insight/analysis/findings/${id}/feedback`, body)
  return data
}

export async function fetchAnalysisMetrics() {
  const { data } = await api.get('/sql-insight/analysis/metrics')
  return asArray(data?.data)
}

export async function fetchAnalysisRules() {
  const { data } = await api.get('/sql-insight/analysis/rules')
  return asArray(data?.data)
}

export async function fetchAnalysisSummary(params: Record<string, any> = {}) {
  const { data } = await api.get('/sql-insight/analysis/summary', { params })
  return data?.data || null
}

export const SCENARIO_TAG_OPTS = [
  { value: 'promo', label: '大促 / 营销高峰' },
  { value: 'expected_load_up', label: '预期负载上升' },
  { value: 'expected_load_down', label: '预期负载下跌' },
  { value: 'maintenance', label: '变更 / 发布 / 割接' },
  { value: 'migration', label: '迁移 / 扩容中' },
  { value: 'capacity_test', label: '压测' },
  { value: 'known_issue', label: '已知问题排查中' },
]
