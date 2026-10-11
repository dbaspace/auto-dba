import React, { useEffect, useMemo, useRef } from 'react'
import * as echarts from 'echarts'
import type { EChartsCoreOption } from 'echarts'
import { CI_TYPES, type CmdbCI, type CmdbGraph } from './services/cmdbApi'

const TYPE_COLORS: Record<string, string> = {
  instance: '#1677ff',
  database: '#13c2c2',
  host: '#722ed1',
  region: '#eb2f96',
  environment: '#fa8c16',
  ha_cluster: '#f5222d',
  sr_cluster: '#2f54eb',
  db_deploy_cluster: '#52c41a',
  proxy_service: '#a0d911',
  db_account: '#faad14',
  cdc_task: '#36cfc9',
  backup_policy: '#9254de',
  monitor_job: '#597ef7',
  monitor_group: '#85a5ff',
  connector: '#ff85c0',
}

const SECONDARY_RELS = new Set(['hosted_on', 'runs', 'member_of'])

const PRIMARY_RELS = new Set([
  'contains',
  'owns',
  'manages',
  'runs_on',
  'protects',
  'has_account',
  'data_flow',
  'in_env',
  'in_region',
  'scrapes',
  'uses_ha',
  'belongs_to',
  'depends_on',
  'belongs_to_app',
])

function isSecondaryRel(relType: string) {
  if (SECONDARY_RELS.has(relType)) return true
  if (PRIMARY_RELS.has(relType)) return false
  return false
}

function nodeKey(type: string, id: string) {
  return `${type}|${id}`
}

function shortName(name: string, id: string) {
  const n = (name || id || '').trim()
  if (n.length <= 28) return n
  return `${n.slice(0, 26)}…`
}

function typeLabel(type: string) {
  return CI_TYPES.find((t) => t.value === type)?.label || type
}

type PrimaryLink = { source: string; target: string; relText: string; curveness: number }

type Props = {
  graph: CmdbGraph
  relFilter?: string
  height?: string
  onNodeClick?: (ci: CmdbCI) => void
}

function buildGraphParts(graph: CmdbGraph, relFilter: string | undefined, nodeByKey: Map<string, CmdbCI>) {
  const edges = (graph.edges || []).filter((e) => !relFilter || e.rel_type === relFilter)
  const usedKeys = new Set<string>()
  for (const e of edges) {
    usedKeys.add(nodeKey(e.src_type, e.src_id))
    usedKeys.add(nodeKey(e.dst_type, e.dst_id))
  }
  for (const n of graph.nodes || []) {
    usedKeys.add(nodeKey(n.type, n.id))
  }

  const categories: { name: string }[] = []
  const catIndex = new Map<string, number>()
  const ensureCat = (type: string) => {
    if (catIndex.has(type)) return catIndex.get(type)!
    const idx = categories.length
    catIndex.set(type, idx)
    categories.push({ name: typeLabel(type) })
    return idx
  }

  const data = Array.from(usedKeys).map((key) => {
    const ci = nodeByKey.get(key)
    const [type, ...rest] = key.split('|')
    const id = rest.join('|')
    const displayName = shortName(ci?.name || id, id)
    const tLabel = typeLabel(type || 'unknown')
    const cat = ensureCat(type || 'unknown')
    const color = TYPE_COLORS[type] || '#8c8c8c'
    return {
      id: key,
      name: `${displayName} · ${tLabel}`,
      category: cat,
      value: type,
      symbolSize: type === 'instance' || type === 'database' ? 28 : 22,
      itemStyle: { color },
      label: {
        show: true,
        formatter: `${displayName}\n[${tLabel}]`,
        fontSize: 10,
        lineHeight: 13,
      },
      ciType: type,
      ciId: id,
      displayName,
    }
  })

  const seenDir = new Set<string>()
  const pairCount = new Map<string, number>()
  const rawLinks: { source: string; target: string; relType: string; primary: boolean }[] = []

  for (const e of edges) {
    const s = nodeKey(e.src_type, e.src_id)
    const t = nodeKey(e.dst_type, e.dst_id)
    if (!usedKeys.has(s) || !usedKeys.has(t) || s === t) continue
    const dk = `${s}>${t}>${e.rel_type}`
    if (seenDir.has(dk)) continue
    seenDir.add(dk)
    const primary = !isSecondaryRel(e.rel_type)
    rawLinks.push({ source: s, target: t, relType: e.rel_type, primary })
    const pk = s < t ? `${s}::${t}` : `${t}::${s}`
    pairCount.set(pk, (pairCount.get(pk) || 0) + 1)
  }

  const pairOffset = new Map<string, number>()
  const primaryLinks: PrimaryLink[] = []
  const links = rawLinks.map((l) => {
    const pk = l.source < l.target ? `${l.source}::${l.target}` : `${l.target}::${l.source}`
    const total = pairCount.get(pk) || 1
    let curveness = 0.12
    if (total > 1) {
      const idx = pairOffset.get(pk) || 0
      pairOffset.set(pk, idx + 1)
      curveness = (idx % 2 === 0 ? 1 : -1) * (0.18 + Math.floor(idx / 2) * 0.08)
      if (l.source > l.target) curveness = -curveness
    }
    const role = l.primary ? '主' : '次'
    const relText = `${role} · ${l.relType}`
    if (l.primary) {
      primaryLinks.push({ source: l.source, target: l.target, relText, curveness })
    }
    return {
      source: l.source,
      target: l.target,
      relText,
      primary: l.primary,
      label: {
        show: false,
        formatter: relText,
        fontSize: 10,
        color: l.primary ? '#1677ff' : '#8c8c8c',
      },
      lineStyle: l.primary
        ? { type: 'solid' as const, color: '#1677ff', width: 2, curveness, opacity: 0.9 }
        : { type: 'dashed' as const, color: '#bfbfbf', width: 1, curveness, opacity: 0.85 },
      symbol: ['none', 'arrow'],
      symbolSize: l.primary ? [0, 8] : [0, 5],
      emphasis: {
        label: { show: true, formatter: relText, fontSize: 10 },
        lineStyle: {
          width: l.primary ? 3 : 1.5,
          color: l.primary ? '#0958d9' : '#8c8c8c',
        },
      },
    }
  })

  return { categories, data, links, primaryLinks, nodeByKey }
}

function readNodePixelPositions(chart: echarts.ECharts): Map<string, [number, number]> {
  const pos = new Map<string, [number, number]>()
  try {
    const seriesModel = chart.getModel().getSeriesByIndex(0)
    if (!seriesModel) return pos
    const data = seriesModel.getData()
    data.each((idx: number) => {
      const raw = data.getRawDataItem(idx) as { id?: string } | undefined
      const id = raw?.id != null ? String(raw.id) : String(data.getId(idx))
      const layout = data.getItemLayout(idx) as { x?: number; y?: number } | number[] | undefined
      if (!layout) return
      let x: number | undefined
      let y: number | undefined
      if (Array.isArray(layout)) {
        x = layout[0]
        y = layout[1]
      } else {
        x = layout.x
        y = layout.y
      }
      if (x == null || y == null || Number.isNaN(x) || Number.isNaN(y)) return
      try {
        const pixel = chart.convertToPixel({ seriesIndex: 0 }, [x, y]) as number[]
        if (Array.isArray(pixel) && pixel.length >= 2) {
          pos.set(id, [pixel[0], pixel[1]])
          return
        }
      } catch {
        /* force 布局点本身多为视图坐标 */
      }
      pos.set(id, [x, y])
    })
  } catch {
    /* ignore */
  }
  return pos
}

function syncFlowLines(chart: echarts.ECharts, primaryLinks: PrimaryLink[]) {
  const w = chart.getWidth()
  const h = chart.getHeight()
  if (w < 10 || h < 10) return
  const pos = readNodePixelPositions(chart)
  const flowData: { coords: [number, number][]; relText: string; lineStyle: { curveness: number } }[] = []
  for (const l of primaryLinks) {
    const a = pos.get(l.source)
    const b = pos.get(l.target)
    if (!a || !b) continue
    flowData.push({
      coords: [a, b],
      relText: l.relText,
      lineStyle: { curveness: l.curveness },
    })
  }
  chart.setOption(
    {
      xAxis: { show: false, type: 'value', min: 0, max: w, scale: false },
      yAxis: { show: false, type: 'value', min: 0, max: h, inverse: true, scale: false },
      series: [
        {
          id: 'topo-flow',
          type: 'lines',
          coordinateSystem: 'cartesian2d',
          xAxisIndex: 0,
          yAxisIndex: 0,
          zlevel: 2,
          effect: {
            show: true,
            period: 3.5,
            trailLength: 0.35,
            symbol: 'arrow',
            symbolSize: 5,
            color: '#1677ff',
          },
          lineStyle: { width: 0, opacity: 0 },
          data: flowData,
          silent: true,
        },
      ],
    },
    { lazyUpdate: true },
  )
}

export default function TopologyGraph({ graph, relFilter, height = '640px', onNodeClick }: Props) {
  const elRef = useRef<HTMLDivElement | null>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  const onNodeClickRef = useRef(onNodeClick)
  onNodeClickRef.current = onNodeClick
  const primaryLinksRef = useRef<PrimaryLink[]>([])
  const syncTimerRef = useRef<number | null>(null)

  const nodeByKey = useMemo(() => {
    const m = new Map<string, CmdbCI>()
    for (const n of graph.nodes || []) {
      m.set(nodeKey(n.type, n.id), n)
    }
    return m
  }, [graph.nodes])

  const parts = useMemo(() => buildGraphParts(graph, relFilter, nodeByKey), [graph, relFilter, nodeByKey])

  const baseOption = useMemo((): EChartsCoreOption => {
    primaryLinksRef.current = parts.primaryLinks
    return {
      animation: true,
      tooltip: {
        formatter: (p: any) => {
          if (p.dataType === 'edge' || p.seriesType === 'lines') {
            return p.data?.relText || p.data?.label?.formatter || ''
          }
          const d = p.data || {}
          const ci = nodeByKey.get(d.id)
          const tid = ci?.attrs?.tenant_id ? String(ci.attrs.tenant_id) : ''
          return [
            `<b>${d.displayName || d.name || ''}</b>`,
            `类型: ${typeLabel(d.ciType || '')}`,
            `ID: ${d.ciId || ''}`,
            tid ? `租户: ${tid}` : '',
          ]
            .filter(Boolean)
            .join('<br/>')
        },
      },
      legend: [
        {
          data: parts.categories.map((c) => c.name),
          bottom: 22,
          type: 'scroll',
        },
      ],
      graphic: [
        {
          type: 'text',
          left: 'center',
          bottom: 4,
          style: {
            text: '实线+光点流动=主关系 · 虚线=次/反向',
            fill: '#8c8c8c',
            fontSize: 11,
          },
        },
      ],
      xAxis: { show: false, type: 'value', min: 0, max: 100 },
      yAxis: { show: false, type: 'value', min: 0, max: 100, inverse: true },
      series: [
        {
          id: 'topo-graph',
          type: 'graph',
          layout: 'force',
          roam: true,
          draggable: true,
          categories: parts.categories,
          data: parts.data,
          links: parts.links,
          force: {
            repulsion: 720,
            edgeLength: [140, 280],
            gravity: 0.04,
          },
          edgeSymbol: ['none', 'arrow'],
          edgeSymbolSize: 7,
          label: { position: 'bottom' },
          emphasis: {
            focus: 'adjacency',
            scale: true,
          },
          lineStyle: { opacity: 0.9 },
        },
        {
          id: 'topo-flow',
          type: 'lines',
          coordinateSystem: 'cartesian2d',
          zlevel: 2,
          effect: {
            show: true,
            period: 3.5,
            trailLength: 0.35,
            symbol: 'arrow',
            symbolSize: 5,
            color: '#1677ff',
          },
          lineStyle: { width: 0, opacity: 0 },
          data: [],
          silent: true,
        },
      ],
    }
  }, [parts, nodeByKey])

  const scheduleSync = () => {
    if (syncTimerRef.current != null) window.clearTimeout(syncTimerRef.current)
    syncTimerRef.current = window.setTimeout(() => {
      const chart = chartRef.current
      if (!chart) return
      syncFlowLines(chart, primaryLinksRef.current)
    }, 120)
  }

  useEffect(() => {
    if (!elRef.current) return
    const el = elRef.current
    const chart = echarts.init(el)
    chartRef.current = chart

    chart.on('click', (p: any) => {
      if (p?.dataType !== 'node' || !onNodeClickRef.current) return
      const key = String(p.data?.id || '')
      const ci = nodeByKey.get(key)
      if (ci) {
        onNodeClickRef.current(ci)
        return
      }
      const ciType = String(p.data?.ciType || '')
      const ciId = String(p.data?.ciId || '')
      if (ciType && ciId) {
        onNodeClickRef.current({ id: ciId, type: ciType, name: String(p.data?.name || ciId) })
      }
    })

    chart.on('finished', () => scheduleSync())
    chart.on('graphroam', () => scheduleSync())
    chart.on('mouseup', () => scheduleSync())

    const resize = () => {
      chart.resize()
      scheduleSync()
    }
    window.addEventListener('resize', resize)
    let ro: ResizeObserver | null = null
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(() => resize())
      ro.observe(el)
    }
    const raf = requestAnimationFrame(resize)

    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      ro?.disconnect()
      if (syncTimerRef.current != null) window.clearTimeout(syncTimerRef.current)
      chart.dispose()
      chartRef.current = null
    }
    // nodeByKey 仅用于 click 查找，初始化一次即可；最新 map 在 setOption 后由闭包外的 ref 不需要
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    primaryLinksRef.current = parts.primaryLinks
    chart.setOption(baseOption, true)
    requestAnimationFrame(() => {
      chart.resize()
      // 力导向需要几帧稳定后再同步光点
      window.setTimeout(() => syncFlowLines(chart, primaryLinksRef.current), 400)
      window.setTimeout(() => syncFlowLines(chart, primaryLinksRef.current), 1000)
    })
  }, [baseOption, parts.primaryLinks])

  // 保持 click 能读到最新 nodeByKey
  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    chart.off('click')
    chart.on('click', (p: any) => {
      if (p?.dataType !== 'node' || !onNodeClickRef.current) return
      const key = String(p.data?.id || '')
      const ci = nodeByKey.get(key)
      if (ci) {
        onNodeClickRef.current(ci)
        return
      }
      const ciType = String(p.data?.ciType || '')
      const ciId = String(p.data?.ciId || '')
      if (ciType && ciId) {
        onNodeClickRef.current({ id: ciId, type: ciType, name: String(p.data?.name || ciId) })
      }
    })
  }, [nodeByKey])

  return (
    <div style={{ border: '1px solid #f0f0f0', borderRadius: 8, background: '#fafafa', padding: 8 }}>
      <div ref={elRef} style={{ height, width: '100%', minWidth: 0 }} />
    </div>
  )
}
