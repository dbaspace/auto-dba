import React, { useCallback, useEffect, useMemo, useRef } from 'react'

const BASE = '/starrocks-webroot/be/'

function buildSrcDoc(bodyHtml: string): string {
  // bodyHtml 已是清洗后的片段；样式/脚本仅在 iframe 内加载，避免污染 KeePlus 外壳
  return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<base href="${BASE}"/>
<link rel="stylesheet" href="${BASE}bootstrap/css/bootstrap.min.css"/>
<link rel="stylesheet" href="${BASE}bootstrap/css/bootstrap-table.min.css"/>
<link rel="stylesheet" href="${BASE}starrocks.css"/>
<style>
  html, body { margin: 0; padding: 8px 12px; background: #fff; }
  .container { margin: 0 !important; max-width: none !important; width: 100% !important; padding-left: 0 !important; padding-right: 0 !important; }
  table { width: 100%; }
  a { cursor: pointer; }
</style>
</head>
<body>
${bodyHtml}
<script src="${BASE}jquery-3.5.0.min.js"><\/script>
<script src="${BASE}bootstrap/js/bootstrap.min.js"><\/script>
<script src="${BASE}bootstrap/js/bootstrap-table.min.js"><\/script>
<script src="${BASE}starrocks.js"><\/script>
<script>
(function () {
  function initTables() {
    if (!window.jQuery || !jQuery.fn || !jQuery.fn.bootstrapTable) return;
    jQuery('table[data-toggle="table"]').each(function () {
      var $t = jQuery(this);
      try { if ($t.data('bootstrap.table')) $t.bootstrapTable('destroy'); } catch (e) {}
      try { $t.bootstrapTable(); } catch (e) {}
    });
  }
  if (document.readyState === 'complete') initTables();
  else window.addEventListener('load', initTables);
  setTimeout(initTables, 200);
})();
<\/script>
</body>
</html>`
}

export default function SrFeHtmlView({
  html,
  onNavigate,
}: {
  html: string
  onNavigate?: (href: string) => void
}) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const onNavigateRef = useRef(onNavigate)
  onNavigateRef.current = onNavigate

  const srcDoc = useMemo(() => (html ? buildSrcDoc(html) : ''), [html])

  const syncHeight = useCallback(() => {
    const iframe = iframeRef.current
    if (!iframe) return
    try {
      const doc = iframe.contentDocument
      if (!doc?.body) return
      const h = Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight, 120)
      iframe.style.height = `${h + 16}px`
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    const iframe = iframeRef.current
    if (!iframe || !srcDoc) return

    const onLoad = () => {
      syncHeight()
      try {
        const doc = iframe.contentDocument
        if (!doc) return

        const handleClick = (e: MouseEvent) => {
          const anchor = (e.target as HTMLElement | null)?.closest?.('a') as HTMLAnchorElement | null
          if (!anchor) return
          const href = anchor.getAttribute('href')
          if (!href || href === '#' || href.startsWith('javascript:')) return
          e.preventDefault()
          e.stopPropagation()
          if (/^https?:\/\//i.test(href)) {
            window.open(href, '_blank', 'noopener')
            return
          }
          onNavigateRef.current?.(href)
        }
        doc.addEventListener('click', handleClick)

        const ro =
          typeof ResizeObserver !== 'undefined'
            ? new ResizeObserver(() => syncHeight())
            : null
        if (doc.body && ro) ro.observe(doc.body)

        const intervalId = window.setInterval(syncHeight, 500)
        const stopAt = window.setTimeout(() => clearInterval(intervalId), 8000)

        iframe.dataset.cleanup = '1'
        ;(iframe as HTMLIFrameElement & { __srCleanup?: () => void }).__srCleanup = () => {
          doc.removeEventListener('click', handleClick)
          ro?.disconnect()
          clearInterval(intervalId)
          clearTimeout(stopAt)
        }
      } catch {
        /* ignore */
      }
    }

    iframe.addEventListener('load', onLoad)
    return () => {
      iframe.removeEventListener('load', onLoad)
      const cleanup = (iframe as HTMLIFrameElement & { __srCleanup?: () => void }).__srCleanup
      cleanup?.()
    }
  }, [srcDoc, syncHeight])

  if (!html) return null

  return (
    <iframe
      ref={iframeRef}
      className="sr-fe-html-iframe"
      title="starrocks-fe-embed"
      srcDoc={srcDoc}
      sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
    />
  )
}
