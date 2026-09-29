import api from '../../../../api/request'

export type PanelResult = {
  key: string
  title: string
  unit: string
  section: string
  kind: string
  span?: string
  value?: number | null
  text?: string
  series?: {
    name: string
    labels?: Record<string, string>
    points: { t: number; v: number }[]
    max?: number | null
    current?: number | null
  }[]
  error?: string
}

export type CatalogSection = {
  name: string
  title: string
  panels: { key: string; title: string; unit: string; kind: string; span?: string }[]
}

export type MonitorInstance = {
  id: string
  name: string
  host: string
  port: number
  db_type?: string
  idc?: string
  region_id?: string
  region_code?: string
  region_name?: string
  env_id?: string
  domain_name?: string
  domains?: string[]
  tags?: string[]
}

export async function fetchDbMonitorCatalog(engine = 'postgres') {
  const { data } = await api.get('/db-monitor/catalog', { params: { engine } })
  return (data?.data || []) as CatalogSection[]
}

export async function fetchDbMonitorInstances(dbType = 'postgres') {
  const { data } = await api.get('/db-monitor/instances', { params: { db_type: dbType } })
  return (data?.data || []) as MonitorInstance[]
}

export type MonitorCluster = {
  id: string
  idc: string
  env_name?: string
  domain: string
  fe_endpoints?: string[]
  be_endpoints?: string[]
  job_name?: string
}

export async function fetchDbMonitorClusters(engine = 'starrocks') {
  const { data } = await api.get('/db-monitor/clusters', { params: { engine } })
  return (data?.data || []) as MonitorCluster[]
}

export async function fetchDbMonitorOverview(instanceId?: string, includeSettings = false, engine = 'postgres') {
  const { data } = await api.get('/db-monitor/overview', {
    params: {
      instance_id: instanceId || undefined,
      include_settings: includeSettings ? '1' : undefined,
      engine,
    },
    timeout: 60000,
  })
  return (data?.data || []) as PanelResult[]
}

/** 预热后端 selector 缓存（忽略结果） */
export async function warmDbMonitorSelectors(instanceId: string, engine = 'postgres') {
  if (!instanceId) return
  try {
    await fetchDbMonitorOverview(instanceId, false, engine)
  } catch {
    /* ignore */
  }
}

export async function fetchDbMonitorQuery(params: {
  instance_id?: string
  engine?: string
  idc?: string
  domain?: string
  cluster_id?: string
  fe_instances?: string[]
  be_instances?: string[]
  start: string
  end: string
  step?: number
  sections?: string[]
}) {
  const { data } = await api.post('/db-monitor/query', params, { timeout: 120000 })
  return (data?.data || []) as PanelResult[]
}

export async function fetchDbMonitorPanels(params: {
  instance_id?: string
  engine?: string
  section?: string
  start?: string
  end?: string
  step?: number
  keys?: string
}) {
  const { data } = await api.get('/db-monitor/panels', {
    params,
    timeout: 120000,
  })
  return (data?.data || []) as PanelResult[]
}

/** 从 tags / idc 字段 / 区域 解析 idc 值 */
export function resolveInstanceIdc(it: MonitorInstance): string {
  for (const raw of it.tags || []) {
    const t = String(raw || '').trim()
    const i = t.indexOf('=')
    if (i <= 0) continue
    if (t.slice(0, i).trim().toLowerCase() === 'idc') {
      const v = t.slice(i + 1).trim()
      if (v) return v
    }
  }
  const fromField = String(it.idc || '').trim()
  if (fromField) return fromField
  const code = String(it.region_code || '').trim()
  if (code) return code
  return String(it.region_name || '').trim()
}

/** Grafana 式 domain：优先 tags domain=，其次 domains / domain_name / host */
export function resolveInstanceDomain(it: MonitorInstance): string {
  for (const raw of it.tags || []) {
    const t = String(raw || '').trim()
    const i = t.indexOf('=')
    if (i <= 0) continue
    if (t.slice(0, i).trim().toLowerCase() === 'domain') {
      const v = t.slice(i + 1).trim()
      if (v) return v
    }
  }
  for (const d of it.domains || []) {
    const v = String(d || '').trim()
    if (v) return v
  }
  const dn = String(it.domain_name || '').trim()
  if (dn) return dn
  return String(it.host || '').trim()
}

/** 实例标签选项：仅取 tags 中显式定义的 db= 值，未定义则不返回 */
export function resolveInstanceTagLabels(it: MonitorInstance): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const raw of it.tags || []) {
    const t = String(raw || '').trim()
    if (!t) continue
    const i = t.indexOf('=')
    if (i <= 0) continue
    const k = t.slice(0, i).trim().toLowerCase()
    const v = t.slice(i + 1).trim()
    if (k !== 'db' || !v || seen.has(v)) continue
    seen.add(v)
    out.push(v)
  }
  return out
}

/** 仅按 db= 标签匹配 */
export function instanceHasTag(it: MonitorInstance, tag: string): boolean {
  const want = String(tag || '').trim()
  if (!want) return true
  return resolveInstanceTagLabels(it).includes(want)
}
