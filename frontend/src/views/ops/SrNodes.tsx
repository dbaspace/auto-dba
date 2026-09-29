import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Empty, Select, Space, Spin, Tabs, message } from 'antd'
import { RollbackOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import SrFeHtmlView from './SrFeHtmlView'
import './ExternalStatus.css'
import './DolphinOps.css'
import './SrNodes.css'

interface FeRow {
  id?: string
  task_name?: string
  environment?: string
  domain?: string
  fe_node?: string
  username?: string
  port?: number
}

interface PreviewItem {
  task_name?: string
  env?: string
  domain?: string
  endpoints?: string[]
}

interface FePanel {
  tab_key?: string
  tab_label?: string
  title?: string
  html?: string
  body?: string
  url?: string
  ok?: boolean
  status_code?: number
  error?: string
  content_type?: string
}

const FE_TABS = [
  { key: 'system', label: 'system' },
  { key: 'backends', label: 'backends' },
  { key: 'logs', label: 'logs' },
  { key: 'queries', label: 'queries' },
  { key: 'sessions', label: 'sessions' },
  { key: 'variables', label: 'variables' },
  { key: 'ha', label: 'ha' },
  { key: 'help', label: 'help' },
] as const

type FeTabKey = (typeof FE_TABS)[number]['key']

function splitFeNodes(feNode: string | undefined): string[] {
  if (!feNode) return []
  return [...new Set(feNode.split(/[,，;\n]+/).map((s) => s.trim()).filter(Boolean))]
}

function normalizeFEBaseUrl(raw: string): string {
  let s = raw.trim()
  if (!/^https?:\/\//i.test(s)) s = `http://${s}`
  return s.replace(/\/+$/, '')
}

function extractBodyInnerHtml(fullHtml: string): string {
  if (!fullHtml.trim()) return ''
  try {
    const doc = new DOMParser().parseFromString(fullHtml, 'text/html')
    const body = doc.body
    if (!body) return fullHtml
    body.querySelectorAll('nav').forEach((el) => el.remove())
    body.querySelectorAll('script').forEach((el) => el.remove())
    body.querySelectorAll('link[rel="stylesheet"]').forEach((el) => el.remove())
    return body.innerHTML.trim() || fullHtml
  } catch {
    return fullHtml
  }
}

function jsonArrayToHtmlTable(arr: Record<string, unknown>[]): string {
  if (!arr.length) return ''
  const keys = Object.keys(arr[0])
  const ths = keys.map((k) => `<th>${escapeHtml(k)}</th>`).join('')
  const rows = arr
    .map((row) => {
      const tds = keys.map((k) => `<td>${escapeHtml(String(row[k] ?? ''))}</td>`).join('')
      return `<tr>${tds}</tr>`
    })
    .join('')
  return `<table class="table table-hover table-bordered table-striped table-condensed" data-toggle="table"><thead><tr>${ths}</tr></thead><tbody>${rows}</tbody></table>`
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function bodyToEmbedHtml(raw: string, contentType?: string): string {
  const text = (raw || '').trim()
  if (!text) return ''
  if (contentType?.includes('application/json')) {
    try {
      const arr = JSON.parse(text)
      if (Array.isArray(arr) && arr.length) return jsonArrayToHtmlTable(arr)
    } catch {
      /* fallthrough */
    }
  }
  const inner = extractBodyInnerHtml(text)
  if (inner) return inner
  if (text.startsWith('<')) return text
  return `<pre class="sr-fe-pre">${escapeHtml(text)}</pre>`
}

function renderPanelHtml(panel: FePanel | undefined): string {
  if (!panel) return ''
  return bodyToEmbedHtml(panel.html || panel.body || '', panel.content_type)
}

function seedAuthCookie() {
  const token = localStorage.getItem('token') || ''
  if (token) {
    document.cookie = `token=${encodeURIComponent(token)}; path=/; SameSite=Lax`
  }
}

function withAccessToken(path: string) {
  const token = localStorage.getItem('token') || ''
  if (!token) return path
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}access_token=${encodeURIComponent(token)}`
}

function beProxyURL(host: string) {
  return withAccessToken(`/api/v1/starrocks-be-proxy/${encodeURIComponent(host)}/mem_tracker`)
}

export default function SrNodes() {
  const [mode, setMode] = useState<'fe' | 'be'>('fe')
  const [rows, setRows] = useState<FeRow[]>([])
  const [bePreview, setBePreview] = useState<PreviewItem[]>([])
  const [loading, setLoading] = useState(false)

  const [feDomain, setFeDomain] = useState<string>()
  const [feNode, setFeNode] = useState<string>()
  const [feAdminId, setFeAdminId] = useState<string>()
  const [feTab, setFeTab] = useState<FeTabKey>('system')
  const [fePanels, setFePanels] = useState<Record<string, FePanel>>({})
  const [feLoading, setFeLoading] = useState(false)
  const [feError, setFeError] = useState<string | null>(null)
  const [subPath, setSubPath] = useState<string | null>(null)
  const [subPathHistory, setSubPathHistory] = useState<string[]>([])
  const [subHtml, setSubHtml] = useState<string | null>(null)

  const [domain, setDomain] = useState<string>()
  const [beHost, setBeHost] = useState<string>()
  const fePanelsRef = useRef(fePanels)
  fePanelsRef.current = fePanels

  useEffect(() => {
    void (async () => {
      setLoading(true)
      try {
        seedAuthCookie()
        const [feRes, beRes] = await Promise.all([
          api.get('/feadmin'),
          api.get('/monitor-targets/preview', { params: { kind: 'starrocks-be' } }),
        ])
        const list: FeRow[] = feRes.data.data || []
        setRows(list)
        const preview: PreviewItem[] = beRes.data.data || []
        setBePreview(preview)
        const domains = Array.from(
          new Set(preview.map((p) => String(p.domain || '').trim()).filter(Boolean)),
        )
        if (domains[0]) setDomain(domains[0])
        const feDomains = Array.from(
          new Set(list.map((r) => String(r.domain || '').trim()).filter(Boolean)),
        )
        if (feDomains[0]) {
          setFeDomain(feDomains[0])
          const matched = list.filter((r) => String(r.domain || '').trim() === feDomains[0])
          if (matched[0]?.id) setFeAdminId(matched[0].id)
          const nodes = splitFeNodes(matched[0]?.fe_node)
          if (nodes[0]) setFeNode(nodes[0])
        }
      } catch (e: any) {
        message.error(e?.response?.data?.message || e.message || '加载失败')
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  const feDomainOptions = useMemo(() => {
    const set = new Set<string>()
    for (const r of rows) {
      const d = String(r.domain || '').trim()
      if (d) set.add(d)
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((d) => ({ value: d, label: d }))
  }, [rows])

  const feNodeOptions = useMemo(() => {
    if (!feDomain) return []
    const nodes = new Set<string>()
    for (const r of rows) {
      if (String(r.domain || '').trim() !== feDomain) continue
      for (const n of splitFeNodes(r.fe_node)) nodes.add(n)
    }
    return Array.from(nodes)
      .sort((a, b) => a.localeCompare(b))
      .map((n) => ({ value: n, label: n }))
  }, [rows, feDomain])

  const resetSubNav = useCallback(() => {
    setSubPath(null)
    setSubPathHistory([])
    setSubHtml(null)
  }, [])

  const onFeDomainChange = useCallback(
    (value: string | undefined) => {
      setFeDomain(value)
      setFeNode(undefined)
      setFeAdminId(undefined)
      setFePanels({})
      setFeError(null)
      resetSubNav()
      if (!value) return
      const matched = rows.filter((r) => String(r.domain || '').trim() === value)
      if (matched[0]?.id) setFeAdminId(matched[0].id)
      const nodes = splitFeNodes(matched[0]?.fe_node)
      if (nodes.length === 1) setFeNode(nodes[0])
    },
    [rows, resetSubNav],
  )

  const onFeNodeChange = useCallback(
    (value: string | undefined) => {
      setFeNode(value)
      setFePanels({})
      setFeError(null)
      resetSubNav()
      if (!value || !feDomain) {
        setFeAdminId(undefined)
        return
      }
      const hit = rows.find(
        (r) =>
          String(r.domain || '').trim() === feDomain && splitFeNodes(r.fe_node).includes(value),
      )
      setFeAdminId(hit?.id)
    },
    [rows, feDomain, resetSubNav],
  )

  const fetchFePanel = useCallback(
    async (nodeAddress: string, tabKey: string, adminId?: string, force?: boolean) => {
      if (!force && fePanelsRef.current[tabKey]?.ok) return
      setFeLoading(true)
      setFeError(null)
      try {
        const res = await api.post('/starrocks-web/page', {
          kind: 'fe',
          fe_admin_id: adminId,
          be_base_url: normalizeFEBaseUrl(nodeAddress),
          tab: tabKey,
        })
        const data = res.data?.data || {}
        const panels: FePanel[] = data.panels || []
        if (!panels.length) {
          setFeError('未返回页面数据')
          return
        }
        setFePanels((prev) => {
          const next = { ...prev }
          for (const p of panels) {
            if (p.tab_key) next[p.tab_key] = p
          }
          return next
        })
        const cur = panels.find((p) => p.tab_key === tabKey) || panels[0]
        if (cur && cur.ok === false) {
          setFeError(cur.error || `上游拒绝访问（HTTP ${cur.status_code || '?'}）`)
        }
      } catch (e: any) {
        setFeError(e?.response?.data?.message || e.message || '请求失败')
      } finally {
        setFeLoading(false)
      }
    },
    [],
  )

  const parseSubPathForApi = useCallback(
    (rawSubPath: string): { tab: string; path: string } => {
      const qIdx = rawSubPath.indexOf('?path=')
      if (qIdx >= 0) {
        const tabPart = rawSubPath.substring(0, qIdx).replace(/^\/+/, '') || feTab
        const pathPart = decodeURIComponent(rawSubPath.substring(qIdx + 6))
        return { tab: tabPart || feTab, path: pathPart }
      }
      if (rawSubPath.startsWith('?path=')) {
        return { tab: feTab, path: decodeURIComponent(rawSubPath.substring(6)) }
      }
      if (rawSubPath.startsWith('path=')) {
        return { tab: feTab, path: decodeURIComponent(rawSubPath.substring(5)) }
      }
      return { tab: feTab, path: rawSubPath }
    },
    [feTab],
  )

  const fetchSubPath = useCallback(
    async (nodeAddress: string, subPathRaw: string, adminId?: string) => {
      const { tab: apiTab, path: apiPath } = parseSubPathForApi(subPathRaw)
      setFeLoading(true)
      setFeError(null)
      try {
        const res = await api.post('/starrocks-web/page', {
          kind: 'fe',
          fe_admin_id: adminId,
          be_base_url: normalizeFEBaseUrl(nodeAddress),
          tab: apiTab || 'system',
          path: apiPath,
        })
        const data = res.data?.data || {}
        const panels: FePanel[] = data.panels || []
        const cur = panels[0]
        if (!cur) {
          setSubHtml(null)
          setFeError('未返回页面数据')
          return
        }
        if (cur.ok === false) {
          setSubHtml(null)
          setFeError(cur.error || `HTTP ${cur.status_code || '?'}`)
          return
        }
        setSubHtml(bodyToEmbedHtml(cur.html || cur.body || '', cur.content_type))
      } catch (e: any) {
        setSubHtml(null)
        setFeError(e?.response?.data?.message || e.message || '请求失败')
      } finally {
        setFeLoading(false)
      }
    },
    [parseSubPathForApi],
  )

  const navigateToSubPath = useCallback(
    (newPath: string) => {
      if (!feNode) return
      setSubPathHistory((prev) => [...prev, subPath ?? '/'])
      setSubPath(newPath)
      void fetchSubPath(feNode, newPath, feAdminId)
    },
    [feNode, feAdminId, subPath, fetchSubPath],
  )

  const goBack = useCallback(() => {
    if (!feNode) return
    if (subPathHistory.length > 0) {
      const prev = subPathHistory[subPathHistory.length - 1]
      setSubPathHistory((h) => h.slice(0, -1))
      if (prev === '/' || prev == null) {
        resetSubNav()
        return
      }
      setSubPath(prev)
      void fetchSubPath(feNode, prev, feAdminId)
      return
    }
    resetSubNav()
  }, [feNode, feAdminId, subPathHistory, fetchSubPath, resetSubNav])

  const onEmbedNavigate = useCallback(
    (href: string) => {
      let targetPath = href
      if (targetPath.startsWith('?')) {
        const currentBase = subPath ?? `/${feTab}`
        const basePath = currentBase.split('?')[0]
        targetPath = `${basePath}${targetPath}`
      } else if (!targetPath.startsWith('/')) {
        const currentBase = subPath ?? '/'
        const basePath = currentBase.split('?')[0]
        const base = basePath.endsWith('/') ? basePath : `${basePath}/`
        targetPath = `${base}${targetPath}`
      }
      const pathOnly = targetPath.split('?')[0].replace(/\/+/g, '/') || '/'
      const query = targetPath.includes('?') ? targetPath.substring(targetPath.indexOf('?')) : ''
      targetPath = `${pathOnly}${query}`

      const tabMatch = FE_TABS.find((t) => `/${t.key}` === pathOnly)
      if (tabMatch && !query) {
        setFeTab(tabMatch.key)
        resetSubNav()
        return
      }
      navigateToSubPath(targetPath)
    },
    [subPath, feTab, navigateToSubPath, resetSubNav],
  )

  useEffect(() => {
    if (mode !== 'fe' || !feNode || !feAdminId) return
    if (subPath) return
    void fetchFePanel(feNode, feTab, feAdminId)
  }, [mode, feNode, feAdminId, feTab, subPath, fetchFePanel])

  const onFeTabChange = useCallback(
    (k: string) => {
      setFeTab(k as FeTabKey)
      resetSubNav()
      setFeError(null)
    },
    [resetSubNav],
  )

  // 已选中的 Tab 再点不会触发 onChange；下钻时需 onTabClick 清 subPath 回根
  const onFeTabClick = useCallback(
    (key: string) => {
      if (key === feTab && subPath != null) {
        resetSubNav()
        setFeError(null)
      }
    },
    [feTab, subPath, resetSubNav],
  )

  const domainOptions = useMemo(() => {
    const set = new Set<string>()
    for (const p of bePreview) {
      const d = String(p.domain || '').trim()
      if (d) set.add(d)
    }
    const hasUngrouped = bePreview.some(
      (p) => !String(p.domain || '').trim() && (p.endpoints || []).length > 0,
    )
    const list = Array.from(set).sort((a, b) => a.localeCompare(b))
    if (hasUngrouped) list.push('__ungrouped__')
    return list.map((d) => ({
      value: d,
      label: d === '__ungrouped__' ? '未分组' : d,
    }))
  }, [bePreview])

  const beHostOptions = useMemo(() => {
    if (!domain) return []
    const endpoints = new Set<string>()
    for (const p of bePreview) {
      const d = String(p.domain || '').trim()
      if (domain === '__ungrouped__') {
        if (d) continue
      } else if (d !== domain) {
        continue
      }
      for (const e of p.endpoints || []) {
        const h = String(e || '').trim()
        if (h) endpoints.add(h)
      }
    }
    return Array.from(endpoints)
      .sort((a, b) => a.localeCompare(b))
      .map((h) => ({ value: h, label: h }))
  }, [bePreview, domain])

  useEffect(() => {
    if (!beHostOptions.length) {
      setBeHost(undefined)
      return
    }
    if (!beHostOptions.some((o) => o.value === beHost)) {
      setBeHost(beHostOptions[0].value)
    }
  }, [beHostOptions, beHost])

  const beIframeSrc = useMemo(() => {
    if (mode !== 'be' || !beHost) return ''
    return beProxyURL(beHost)
  }, [mode, beHost])

  useEffect(() => {
    seedAuthCookie()
  }, [beIframeSrc])

  const embedHtml = subPath != null && subHtml != null ? subHtml : renderPanelHtml(fePanels[feTab])
  const showBack = subPath != null

  if (!loading && mode === 'fe' && rows.length === 0) {
    return (
      <div className="ops-page">
        <Tabs
          activeKey={mode}
          onChange={(k) => setMode(k as 'fe' | 'be')}
          items={[
            { key: 'fe', label: 'FE 查询' },
            { key: 'be', label: 'BE 查询' },
          ]}
        />
        <Empty
          description={
            <span>
              尚未登记 FE 凭据，请先到 <Link to="/config/fe-admin">配置中心 · FE管理</Link> 录入。
            </span>
          }
        />
      </div>
    )
  }

  return (
    <div className="ops-page dolphin-ops-page sr-nodes-page">
      <div className="dolphin-ops-shell">
        <div className="sr-nodes-toolbar">
          <Space wrap size={[8, 8]} align="center">
            <span className="sr-nodes-label">域名</span>
            {mode === 'fe' ? (
              <Select
                style={{ minWidth: 200, maxWidth: 320 }}
                placeholder="选择域名"
                value={feDomain}
                loading={loading}
                onChange={onFeDomainChange}
                options={feDomainOptions}
                showSearch
                optionFilterProp="label"
              />
            ) : (
              <Select
                style={{ minWidth: 200, maxWidth: 320 }}
                placeholder={domainOptions.length ? '选择域名' : '无注册 BE'}
                value={domain}
                loading={loading}
                onChange={(v) => {
                  setDomain(v)
                  setBeHost(undefined)
                }}
                options={domainOptions}
                showSearch
                optionFilterProp="label"
                disabled={!domainOptions.length}
              />
            )}
            <Tabs
              size="small"
              activeKey={mode}
              onChange={(k) => setMode(k as 'fe' | 'be')}
              items={[
                { key: 'fe', label: 'FE 查询' },
                { key: 'be', label: 'BE 查询' },
              ]}
              style={{ marginBottom: 0 }}
            />
            {mode === 'fe' ? (
              <>
                <span className="sr-nodes-label">FE 节点</span>
                <Select
                  style={{ minWidth: 180, maxWidth: 280 }}
                  placeholder={!feDomain ? '请先选域名' : '选择 FE 节点'}
                  value={feNode}
                  onChange={onFeNodeChange}
                  options={feNodeOptions}
                  showSearch
                  optionFilterProp="label"
                  disabled={!feDomain || !feNodeOptions.length}
                />
              </>
            ) : (
              <>
                <span className="sr-nodes-label">BE 地址</span>
                <Select
                  style={{ minWidth: 180, maxWidth: 280 }}
                  placeholder={!domain ? '请先选域名' : beHostOptions.length ? '选择 BE' : '无匹配 BE'}
                  value={beHost}
                  onChange={setBeHost}
                  options={beHostOptions}
                  showSearch
                  optionFilterProp="label"
                  disabled={!domain || !beHostOptions.length}
                />
              </>
            )}
          </Space>
        </div>

        {mode === 'fe' ? (
          <div className="dolphin-ops-frame-wrap sr-fe-embed-wrap">
            <div className="sr-fe-tab-bar">
              <div className="sr-fe-tab-mark" aria-hidden />
              <Tabs
                size="small"
                activeKey={feTab}
                onChange={onFeTabChange}
                onTabClick={onFeTabClick}
                items={FE_TABS.map((t) => ({ key: t.key, label: t.label }))}
                style={{ marginBottom: 0, flex: 1 }}
              />
              {showBack ? (
                <Button size="small" icon={<RollbackOutlined />} onClick={goBack}>
                  返回
                </Button>
              ) : null}
            </div>
            {feError ? (
              <Alert
                type="error"
                showIcon
                style={{ margin: '0 8px 8px' }}
                message={feError}
                description={
                  <span>
                    请到 <Link to="/config/fe-admin">配置中心 · FE管理</Link> 核对用户名、密码与 FE
                    节点（HTTP 端口，通常 8030）。
                  </span>
                }
              />
            ) : null}
            <div className="sr-fe-card">
              {feLoading && !embedHtml ? (
                <div className="sr-nodes-empty">
                  <Spin tip="加载 FE 页面…">
                    <div style={{ padding: 48 }} />
                  </Spin>
                </div>
              ) : embedHtml ? (
                <SrFeHtmlView html={embedHtml} onNavigate={onEmbedNavigate} />
              ) : (
                <div className="sr-nodes-empty">
                  <Empty description={!feNode ? '请选择域名与 FE 节点' : '暂无内容'} />
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="dolphin-ops-frame-wrap sr-be-frame-wrap">
            {loading || !beIframeSrc ? (
              <div className="sr-nodes-empty">
                {loading ? (
                  <Spin tip="加载中…">
                    <div style={{ padding: 48 }} />
                  </Spin>
                ) : !domainOptions.length ? (
                  <Empty
                    description={
                      <span>
                        尚未登记 StarRocks BE 监控目标，请先到{' '}
                        <Link to="/config/monitor-targets">配置中心 · 监控目标注册</Link> 录入。
                      </span>
                    }
                  />
                ) : (
                  <Empty description="请选择域名与 BE 地址" />
                )}
              </div>
            ) : (
              <iframe
                key={beIframeSrc}
                src={beIframeSrc}
                className="embed dolphin-ops-iframe"
                title="starrocks-be"
                allow="clipboard-read; clipboard-write"
                sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads allow-modals"
              />
            )}
          </div>
        )}
      </div>
    </div>
  )
}
