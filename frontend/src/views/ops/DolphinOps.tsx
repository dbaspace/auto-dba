import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Empty, Spin, message } from 'antd'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import './ExternalStatus.css'
import './DolphinOps.css'

interface DsRow {
  id?: string
  environment?: string
  login_url?: string
  username?: string
}

const ENV_STATE_KEY = 'auto-dba-ds-env-state'
const ENV_PICK_KEY = 'auto-dba-ds-env-pick'
const HEADER_PX = '40px'

function embedConsoleURL(env: string) {
  const token = localStorage.getItem('token') || ''
  const q = token ? `?access_token=${encodeURIComponent(token)}` : ''
  return `/api/v1/ds-console/${encodeURIComponent(env)}${q}`
}

function seedAuthCookie() {
  const token = localStorage.getItem('token') || ''
  if (token) {
    document.cookie = `token=${encodeURIComponent(token)}; path=/; SameSite=Lax`
  }
}

function buildEnvState(env: string | undefined, rows: DsRow[]) {
  const options = rows
    .map((r) => r.environment)
    .filter(Boolean)
    .filter((v, i, arr) => arr.indexOf(v) === i)
    .map((v) => ({ value: v as string, label: v as string }))
  return {
    env: env || options[0]?.value || '',
    options,
  }
}

function writeEnvState(env: string | undefined, rows: DsRow[]) {
  try {
    localStorage.setItem(ENV_STATE_KEY, JSON.stringify(buildEnvState(env, rows)))
  } catch {
    /* ignore */
  }
}

function removeEl(el: Element | null) {
  if (!el || !el.parentNode) return
  try {
    el.parentNode.removeChild(el)
  } catch {
    /* ignore */
  }
}

function isProtectedChrome(el: Element) {
  if (el.id === 'auto-dba-ds-env' || el.closest?.('#auto-dba-ds-env')) return true
  if (el.closest?.('.n-layout-header [class*="settings"]')) return true
  if (el.closest?.('.n-layout-header .n-menu')) return true
  if (el.closest?.('.n-layout-header [class*="logo"]')) return true
  return false
}

function hideWatermarkInDoc(doc: Document) {
  doc
    .querySelectorAll('[class*="watermark"], [class*="WaterMark"], [class*="water-mark"], .n-watermark')
    .forEach((el) => {
      ;(el as HTMLElement).style.setProperty('display', 'none', 'important')
      el.querySelectorAll('canvas').forEach(removeEl)
      removeEl(el)
    })

  doc.querySelectorAll('div, section, aside').forEach((el) => {
    if (isProtectedChrome(el)) return
    const computed = window.getComputedStyle(el as HTMLElement)
    const pos = computed.position
    if (pos !== 'fixed' && pos !== 'absolute') return
    if (computed.pointerEvents !== 'none') return
    const op = parseFloat(computed.opacity)
    const tf = String(computed.transform || '')
    const rotated = /matrix|rotate/i.test(tf) && tf !== 'none'
    const lowOp = !Number.isNaN(op) && op > 0 && op < 0.35
    const r = (el as HTMLElement).getBoundingClientRect()
    const covers = r.width > window.innerWidth * 0.4 && r.height > window.innerHeight * 0.4
    if (covers && (rotated || lowOp || el.querySelector('canvas'))) {
      el.querySelectorAll('canvas').forEach(removeEl)
      removeEl(el)
      return
    }
    const st = el.getAttribute('style') || ''
    if (/pointer-events\s*:\s*none/i.test(st) && /position\s*:\s*fixed/i.test(st) && /inset|top\s*:\s*0/i.test(st)) {
      el.querySelectorAll('canvas').forEach(removeEl)
      removeEl(el)
    }
  })
}

/** 父页备份：同源 iframe 内强制去 >、压顶栏、钉内容区 top、去水印 */
function polishDsChrome(iframe: HTMLIFrameElement | null) {
  let doc: Document | null = null
  try {
    doc = iframe?.contentDocument || null
  } catch {
    return
  }
  if (!doc?.body) return

  /* 清掉 CSP 注入残留的 body 字面量 > */
  let n: ChildNode | null = doc.body.firstChild
  while (n) {
    const next = n.nextSibling
    if (n.nodeType === Node.TEXT_NODE) {
      const t = String(n.nodeValue || '')
      if (/^[>\s"']*$/.test(t)) {
        try {
          doc.body.removeChild(n)
        } catch {
          /* ignore */
        }
      }
    }
    n = next
    if (n && n.nodeType === Node.ELEMENT_NODE) break
  }

  doc
    .querySelectorAll(
      '.n-layout-sider, .n-layout-toggle-bar, .n-layout-toggle-button, .n-layout-toggle-bar__top, .n-layout-toggle-bar__bottom, .n-layout-sider-toggle-button, [class*="layout-toggle"], [class*="toggle-bar"], [class*="toggle-button"]',
    )
    .forEach((el) => removeEl(el))

  const header = doc.querySelector('.n-layout-header') as HTMLElement | null
  let headerBottom = 48
  let topPx = HEADER_PX
  if (header) {
    header.style.setProperty('height', HEADER_PX, 'important')
    header.style.setProperty('max-height', HEADER_PX, 'important')
    header.style.setProperty('min-height', HEADER_PX, 'important')
    header.style.setProperty('padding-top', '0', 'important')
    header.style.setProperty('padding-bottom', '0', 'important')
    header.style.setProperty('padding-left', '8px', 'important')
    header.style.setProperty('margin-left', '0', 'important')
    header.style.setProperty('overflow', 'hidden', 'important')
    header.style.setProperty('display', 'flex', 'important')
    header.style.setProperty('align-items', 'center', 'important')
    header.style.setProperty('flex-direction', 'row', 'important')
    header.querySelectorAll<HTMLElement>(':scope > *, [class*="container"]').forEach((kid) => {
      kid.style.setProperty('height', HEADER_PX, 'important')
      kid.style.setProperty('max-height', HEADER_PX, 'important')
      kid.style.setProperty('align-items', 'center', 'important')
      kid.style.setProperty('margin-left', '0', 'important')
      kid.style.setProperty('padding-left', '0', 'important')
    })
    header.querySelectorAll<HTMLElement>('[class*="logo"]').forEach((logo) => {
      logo.style.setProperty('margin-left', '0', 'important')
      logo.style.setProperty('padding-left', '0', 'important')
    })
    header.querySelectorAll('button, .n-button, [role="button"]').forEach((btn) => {
      if (btn.closest?.('[class*="settings"]') || btn.closest?.('.n-menu') || btn.closest?.('#auto-dba-ds-env')) return
      const cls = String((btn as HTMLElement).className || '')
      const t = (btn.textContent || '').replace(/\s+/g, '')
      const rect = btn.getBoundingClientRect()
      if (/fold|collapsed|hamburger|menu-fold|MenuFold/i.test(cls)) {
        removeEl(btn)
        return
      }
      if (rect.left > 80 || rect.width > 48 || rect.height > 48) return
      if (t.length <= 1) removeEl(btn)
    })
    const hb = header.getBoundingClientRect()
    if (hb?.bottom) headerBottom = hb.bottom + 8
    if (hb?.height > 0) topPx = `${Math.round(hb.height)}px`
  }

  doc.querySelectorAll('div, button, span, a, i, svg').forEach((el) => {
    if (isProtectedChrome(el)) return
    const rect = el.getBoundingClientRect()
    if (rect.width < 2 || rect.height < 2) return
    if (rect.left >= 28 || rect.top > headerBottom) return
    if (rect.width > 36 || rect.height > 96) return
    const cls = String((el as HTMLElement).className || '')
    const t = (el.textContent || '').replace(/\s+/g, '')
    if (/toggle|sider|fold|hamburger/i.test(cls) || /^[><›‹»«]$/.test(t) || t.length <= 2) {
      removeEl(el)
    }
  })

  const pinContent = (el: Element | null) => {
    if (!el) return
    if (el === header || (header && el.contains(header))) return
    if ((el as HTMLElement).classList?.contains('n-layout-header')) return
    ;(el as HTMLElement).style.setProperty('top', topPx, 'important')
    ;(el as HTMLElement).style.setProperty('left', '0px', 'important')
    ;(el as HTMLElement).style.setProperty('margin-left', '0', 'important')
  }

  try {
    doc.documentElement.style.setProperty('--auto-dba-ds-header-h', topPx)
  } catch {
    /* ignore */
  }

  if (header) {
    let sib = header.nextElementSibling
    while (sib) {
      if (sib.classList?.contains('n-layout') || String(sib.className || '').includes('n-layout')) {
        pinContent(sib)
      }
      sib = sib.nextElementSibling
    }
  }

  doc.querySelectorAll('.n-layout--absolute-positioned, .n-layout').forEach((el) => {
    if (el === header || (header && el.contains(header))) return
    const computed = window.getComputedStyle(el as HTMLElement)
    if (computed.position !== 'absolute') return
    const topVal = parseFloat(computed.top)
    if (!Number.isNaN(topVal) && topVal >= 48 && topVal <= 80) pinContent(el)
  })

  doc.querySelectorAll('.n-layout-content').forEach((el) => {
    ;(el as HTMLElement).style.setProperty('padding', '8px', 'important')
    ;(el as HTMLElement).style.setProperty('margin-left', '0', 'important')
  })

  hideWatermarkInDoc(doc)

  /* 仅柱状图：补底部空间，恢复 X 轴任务名；不改饼图 */
  const win = iframe?.contentWindow as (Window & { echarts?: any }) | null
  const echartsApi = win?.echarts
  if (echartsApi?.getInstanceByDom) {
    doc.querySelectorAll('[_echarts_instance_]').forEach((el) => {
      try {
        const node = el as HTMLElement
        const inst = echartsApi.getInstanceByDom(node)
        if (!inst?.getOption) return
        const opt = inst.getOption() || {}
        const series = opt.series || []
        const isBar = series.some((s: { type?: string }) => s?.type === 'bar')
        if (!isBar) return
        const h = node.clientHeight || 0
        if (h < 400) node.style.setProperty('height', '400px', 'important')
        node.style.setProperty('overflow', 'visible', 'important')
        node.style.setProperty('min-height', '400px', 'important')
        const child = node.firstElementChild as HTMLElement | null
        if (child?.style) {
          child.style.setProperty('overflow', 'visible', 'important')
          if (h < 400) child.style.setProperty('height', '400px', 'important')
        }
        inst.setOption(
          {
            grid: { bottom: 88 },
            xAxis: { axisLabel: { interval: 0, hideOverlap: false } },
          },
          false,
        )
        inst.resize?.()
      } catch {
        /* ignore */
      }
    })
  }
}

export default function DolphinOps() {
  const [rows, setRows] = useState<DsRow[]>([])
  const [env, setEnv] = useState<string>()
  const [loading, setLoading] = useState(false)
  const [warming, setWarming] = useState(false)
  const [readyEnv, setReadyEnv] = useState<string>()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const rowsRef = useRef(rows)
  const envRef = useRef(env)
  rowsRef.current = rows
  envRef.current = env

  const pushEnvStateToIframe = useCallback(() => {
    const win = iframeRef.current?.contentWindow
    if (!win) return
    const state = buildEnvState(envRef.current, rowsRef.current)
    if (!state.options.length) return
    try {
      win.postMessage({ type: 'auto-dba-ds-env-state', ...state }, '*')
    } catch {
      /* ignore */
    }
  }, [])

  const onIframeLoad = useCallback(() => {
    pushEnvStateToIframe()
    polishDsChrome(iframeRef.current)
  }, [pushEnvStateToIframe])

  useEffect(() => {
    if (!readyEnv) return
    const id = window.setInterval(() => polishDsChrome(iframeRef.current), 200)
    return () => window.clearInterval(id)
  }, [readyEnv])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/dsadmin')
      const list: DsRow[] = data.data || []
      setRows(list)
      setEnv((prev) => {
        const envs = list.map((r) => r.environment).filter(Boolean) as string[]
        if (prev && envs.includes(prev)) return prev
        return envs[0]
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    writeEnvState(env, rows)
    pushEnvStateToIframe()
  }, [env, rows, pushEnvStateToIframe])

  useEffect(() => {
    function onPick(next: string) {
      const v = String(next || '').trim()
      if (!v) return
      setEnv((prev) => (prev === v ? prev : v))
    }
    function onStorage(e: StorageEvent) {
      if (e.key === ENV_PICK_KEY && e.newValue) onPick(e.newValue)
    }
    function onMessage(e: MessageEvent) {
      const d = e.data
      if (!d || typeof d !== 'object') return
      if (d.type === 'auto-dba-ds-env' && d.env) onPick(String(d.env))
      if (d.type === 'auto-dba-ds-env-ready') pushEnvStateToIframe()
    }
    window.addEventListener('storage', onStorage)
    window.addEventListener('message', onMessage)
    return () => {
      window.removeEventListener('storage', onStorage)
      window.removeEventListener('message', onMessage)
    }
  }, [pushEnvStateToIframe])

  useEffect(() => {
    if (!env) {
      setReadyEnv(undefined)
      return
    }
    let cancelled = false
    seedAuthCookie()
    writeEnvState(env, rows)
    setWarming(true)
    setReadyEnv(undefined)
    ;(async () => {
      try {
        await api.get('/dsadmin/warm', { params: { env, environment: env } })
      } catch (e: any) {
        if (!cancelled) {
          message.warning(e?.response?.data?.message || e.message || '代登预热失败，仍尝试打开控制台')
        }
      } finally {
        if (!cancelled) {
          setWarming(false)
          setReadyEnv(env)
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [env, rows])

  const iframeSrc = readyEnv ? embedConsoleURL(readyEnv) : ''

  if (!loading && rows.length === 0) {
    return (
      <div className="ops-page">
        <Empty
          description={
            <span>
              尚未登记海豚调度凭据，请先到 <Link to="/config/ds-admin">配置中心 · 海豚配置</Link> 录入。
            </span>
          }
        />
      </div>
    )
  }

  return (
    <div className="ops-page dolphin-ops-page">
      <div className="dolphin-ops-shell">
        <div className="dolphin-ops-frame-wrap">
          {loading || warming || !iframeSrc ? (
            <div className="dolphin-ops-empty">
              <Spin tip={warming ? '正在代登…' : '加载中…'}>
                <div style={{ padding: 48 }} />
              </Spin>
            </div>
          ) : (
            <iframe
              ref={iframeRef}
              key={iframeSrc}
              src={iframeSrc}
              className="embed dolphin-ops-iframe"
              title="dolphin-console"
              allow="clipboard-read; clipboard-write"
              sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads allow-modals"
              onLoad={onIframeLoad}
            />
          )}
        </div>
      </div>
    </div>
  )
}
