import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, DatePicker, Empty, Input, Select, Space, Spin, Typography, message } from 'antd'
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import dayjs, { Dayjs } from 'dayjs'
import ChartBox from '../dashboard/ChartBox'
import { useAppearance } from '../../theme/AppearanceProvider'
import {
  fetchDbMonitorClusters,
  fetchDbMonitorInstances,
  fetchDbMonitorPanels,
  fetchDbMonitorQuery,
  instanceHasTag,
  resolveInstanceDomain,
  resolveInstanceIdc,
  resolveInstanceTagLabels,
  type MonitorCluster,
  type MonitorInstance,
  type PanelResult,
} from './db-monitor/services/dbMonitorApi'
import api from '../../api/request'
import {
  buildLegendRows,
  buildTimeseriesOption,
  computeLegendLayout,
  type LegendSort,
  type LegendSortBy,
} from './db-monitor/chartOption'
import { monitorTemplate, type MonitorEngine, type TemplatePanel } from './db-monitor/templates'
import './DbMonitor.css'

const { RangePicker } = DatePicker

type RangePreset = {
  key: string
  label: string
  amount: number
  unit: 'minute' | 'hour' | 'day' | 'week'
}

const RANGE_PRESETS: RangePreset[] = [
  { key: '30m', label: '30 分钟', amount: 30, unit: 'minute' },
  { key: '1h', label: '1 小时', amount: 1, unit: 'hour' },
  { key: '3h', label: '3 小时', amount: 3, unit: 'hour' },
  { key: '6h', label: '6 小时', amount: 6, unit: 'hour' },
  { key: '12h', label: '12 小时', amount: 12, unit: 'hour' },
  { key: '1d', label: '1 天', amount: 1, unit: 'day' },
  { key: '3d', label: '3 天', amount: 3, unit: 'day' },
  { key: '1w', label: '1 周', amount: 1, unit: 'week' },
]

const DEFAULT_PRESET_KEY = '3h'
const SR_REST_SECTIONS = ['cluster_overview', 'query_statistic', 'fe_jvm', 'be']

/** 所有模版 section 默认折叠；仅用户点开后为 false */
function defaultCollapsedMap(eng: MonitorEngine): Record<string, boolean> {
  const m: Record<string, boolean> = {}
  for (const sec of monitorTemplate(eng)) {
    m[sec.key] = true
  }
  return m
}

function rangeFromPreset(p: RangePreset): [Dayjs, Dayjs] {
  const end = dayjs()
  return [end.subtract(p.amount, p.unit), end]
}

function defaultRange(): [Dayjs, Dayjs] {
  const p = RANGE_PRESETS.find((x) => x.key === DEFAULT_PRESET_KEY)!
  return rangeFromPreset(p)
}

function mergeByKey(list: PanelResult[]): Record<string, PanelResult> {
  const m: Record<string, PanelResult> = {}
  for (const p of list) {
    if (p?.key) m[p.key] = p
  }
  return m
}

type Props = {
  engine?: MonitorEngine
}

/** 按卡片宽度动态让位图例：测量容器 → 重建 option → 右侧可滚动图例看全节点 */
function MonitorTimeseriesChart(props: {
  panel: PanelResult
  mode: 'light' | 'dark'
  sort: LegendSort
  panelKey: string
  height: string
  onLegendSort: (panelKey: string, by: LegendSortBy) => void
}) {
  const { panel, mode, sort, panelKey, height, onLegendSort } = props
  const wrapRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [hiddenNames, setHiddenNames] = useState<string[]>([])
  useEffect(() => {
    const el = wrapRef.current
    if (!el) return
    const update = () => setWidth(el.clientWidth)
    update()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  // 面板序列集合变化时重置隐藏态
  const seriesSig = useMemo(
    () => (panel.series || []).map((s) => s.name || '').join('\0'),
    [panel.series],
  )
  useEffect(() => {
    setHiddenNames([])
  }, [panel.key, seriesSig])
  const layout = useMemo(() => computeLegendLayout(width), [width])
  const rows = useMemo(() => buildLegendRows(panel, sort), [panel, sort])
  const option = useMemo(
    () =>
      buildTimeseriesOption(panel, mode, sort, width, {
        externalLegend: true,
        hiddenNames,
      }),
    [panel, mode, sort, width, hiddenNames],
  )
  const toggleName = (name: string) => {
    setHiddenNames((prev) => (prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name]))
  }
  const heightPx = Number.parseInt(String(height), 10) || 260
  const legendBand = layout.stacked ? 108 : 0
  return (
    <div
      ref={wrapRef}
      className={`dbmon-chart-wrap dbmon-chart-wrap--split${layout.stacked ? ' dbmon-chart-wrap--stack' : ''}`}
      style={
        {
          ['--dbmon-legend-hdr-w' as string]: `${layout.hdrWidth}px`,
          ['--dbmon-legend-w' as string]: `${layout.legendWidth}px`,
          ['--dbmon-legend-val-w' as string]: `${layout.valColWidth}px`,
          height: heightPx + legendBand,
        } as React.CSSProperties
      }
    >
      <div className="dbmon-chart-plot">
        <ChartBox option={option} height={`${heightPx}px`} />
      </div>
      <div className="dbmon-legend-panel">
        <div className="dbmon-legend-hdr" aria-label="图例排序">
          <span className="dbmon-legend-hdr-spacer" aria-hidden />
          <span className="dbmon-legend-hdr-spacer" aria-hidden />
          <button
            type="button"
            className={`dbmon-legend-sort${sort.by === 'max' ? ' is-active' : ''}`}
            onClick={() => onLegendSort(panelKey, 'max')}
            title={
              sort.by === 'max'
                ? sort.dir === 'desc'
                  ? '按 max 降序，点击升序'
                  : '按 max 升序，点击降序'
                : '按 max 排序'
            }
          >
            max{sort.by === 'max' ? (sort.dir === 'desc' ? ' ↓' : ' ↑') : ''}
          </button>
          <button
            type="button"
            className={`dbmon-legend-sort${sort.by === 'current' ? ' is-active' : ''}`}
            onClick={() => onLegendSort(panelKey, 'current')}
            title={
              sort.by === 'current'
                ? sort.dir === 'desc'
                  ? '按 current 降序，点击升序'
                  : '按 current 升序，点击降序'
                : '按 current 排序'
            }
          >
            current{sort.by === 'current' ? (sort.dir === 'desc' ? ' ↓' : ' ↑') : ''}
          </button>
        </div>
        <div className="dbmon-legend-scroll" role="list">
          {rows.map((row) => {
            const off = hiddenNames.includes(row.name)
            return (
              <button
                key={row.name}
                type="button"
                role="listitem"
                className={`dbmon-legend-row${off ? ' is-off' : ''}`}
                title={off ? '点击显示' : '点击隐藏'}
                onClick={() => toggleName(row.name)}
              >
                <span className="dbmon-legend-dot" style={{ background: row.color }} />
                <span className="dbmon-legend-name">{row.name}</span>
                <span className="dbmon-legend-val">{row.max}</span>
                <span className="dbmon-legend-val">{row.current}</span>
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

export default function DbMonitor({ engine = 'postgres' }: Props) {
  const isSR = engine === 'starrocks'
  const isRedis = engine === 'redis'
  const isHost = engine === 'host'
  const isEnvOnly = engine === 'debezium' || engine === 'dolphinscheduler'
  const useEnvIdc = isSR || isRedis || isHost || isEnvOnly
  const useDomainInstance = isRedis || isHost
  const template = useMemo(() => monitorTemplate(engine), [engine])
  const { resolvedMode } = useAppearance()
  const [instances, setInstances] = useState<MonitorInstance[]>([])
  const [clusters, setClusters] = useState<MonitorCluster[]>([])
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [idc, setIdc] = useState<string | undefined>()
  const [tagFilters, setTagFilters] = useState<string[]>([])
  const [domainFilter, setDomainFilter] = useState<string | undefined>()
  const [instanceId, setInstanceId] = useState<string>('')
  const [clusterId, setClusterId] = useState<string>('')
  const [feInstances, setFeInstances] = useState<string[]>([])
  const [beInstances, setBeInstances] = useState<string[]>([])
  const [range, setRange] = useState<[Dayjs, Dayjs]>(defaultRange)
  const [rangePreset, setRangePreset] = useState<string | null>(DEFAULT_PRESET_KEY)
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => defaultCollapsedMap(engine))
  const [dataMap, setDataMap] = useState<Record<string, PanelResult>>({})
  const [loadingOverview, setLoadingOverview] = useState(false)
  const [loadingRest, setLoadingRest] = useState(false)
  const [queried, setQueried] = useState(false)
  const [chartSort, setChartSort] = useState<Record<string, LegendSort>>({})
  const querySeqRef = useRef(0)
  const settingsLoadedRef = useRef(false)
  const autoQueryTimerRef = useRef<number | null>(null)
  const collapsedRef = useRef(collapsed)
  collapsedRef.current = collapsed

  // 切换监控引擎时各 section 重新默认折叠
  useEffect(() => {
    setCollapsed(defaultCollapsedMap(engine))
  }, [engine])

  useEffect(() => {
    setInstances([])
    setClusters([])
    setEnvOptions([])
    setIdc(undefined)
    setTagFilters([])
    setDomainFilter(undefined)
    setInstanceId('')
    setClusterId('')
    setFeInstances([])
    setBeInstances([])
    setDataMap({})
    setQueried(false)
    setChartSort({})
    settingsLoadedRef.current = false
    if (isSR) {
      Promise.all([
        fetchDbMonitorClusters('starrocks').catch(() => [] as MonitorCluster[]),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
      ]).then(([list, envRes]) => {
        setClusters(list)
        const envs = (envRes?.data?.data || []) as { code?: string; name?: string }[]
        setEnvOptions(
          envs
            .map((e) => {
              const code = String(e.code || '').trim()
              if (!code) return null
              const name = String(e.name || '').trim()
              return { value: code, label: name && name !== code ? `${name} (${code})` : code }
            })
            .filter(Boolean) as { value: string; label: string }[],
        )
      })
    } else {
      const needEnv = isRedis || isHost || isEnvOnly
      const load = needEnv
        ? Promise.all([
            isEnvOnly
              ? Promise.resolve([] as MonitorInstance[])
              : fetchDbMonitorInstances(engine).catch(() => [] as MonitorInstance[]),
            api.get('/environments').catch(() => ({ data: { data: [] } })),
          ]).then(([list, envRes]) => {
            setInstances(list)
            const envs = (envRes?.data?.data || []) as { code?: string; name?: string }[]
            setEnvOptions(
              envs
                .map((e) => {
                  const code = String(e.code || '').trim()
                  if (!code) return null
                  const name = String(e.name || '').trim()
                  return { value: code, label: name && name !== code ? `${name} (${code})` : code }
                })
                .filter(Boolean) as { value: string; label: string }[],
            )
          })
        : fetchDbMonitorInstances(engine)
            .then((list) => setInstances(list))
            .catch(() => setInstances([]))
      void load
    }
  }, [engine, isSR, isRedis, isHost, isEnvOnly])

  const isOverviewPanel = useCallback(
    (key: string) =>
      template.some(
        (sec) =>
          sec.key === 'overview' &&
          (sec.stats.some((p) => p.key === key) || sec.charts.some((p) => p.key === key)),
      ),
    [template],
  )

  const idcOptions = useMemo(() => {
    if (useEnvIdc) {
      // Redis / SR / Debezium / 海豚：与环境管理 code 对齐（Grafana 变量 idc）
      return envOptions
    }
    const set = new Set<string>()
    for (const it of instances) {
      const v = resolveInstanceIdc(it)
      if (v) set.add(v)
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((v) => ({ value: v, label: v }))
  }, [useEnvIdc, envOptions, instances])

  const filteredClusters = useMemo(() => {
    // 环境未选：不列出集群，强制先选环境管理中的环境
    if (!idc) return []
    return clusters.filter((c) => c.idc === idc)
  }, [clusters, idc])

  const selectedCluster = useMemo(
    () => filteredClusters.find((c) => c.id === clusterId) || clusters.find((c) => c.id === clusterId),
    [filteredClusters, clusters, clusterId],
  )

  const filteredByIdc = useMemo(() => {
    if (!idc) return instances
    return instances.filter((it) => resolveInstanceIdc(it) === idc)
  }, [instances, idc])

  const domainOptions = useMemo(() => {
    const set = new Set<string>()
    for (const it of filteredByIdc) {
      const d = resolveInstanceDomain(it)
      if (d) set.add(d)
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((v) => ({ value: v, label: v }))
  }, [filteredByIdc])

  const tagOptions = useMemo(() => {
    const set = new Set<string>()
    for (const it of filteredByIdc) {
      for (const t of resolveInstanceTagLabels(it)) {
        if (t) set.add(t)
      }
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((v) => ({ value: v, label: v, title: v }))
  }, [filteredByIdc])

  const filteredInstances = useMemo(() => {
    if (useDomainInstance) {
      if (!idc || !domainFilter) return []
      return filteredByIdc.filter((it) => resolveInstanceDomain(it) === domainFilter)
    }
    if (!tagFilters.length) return []
    return filteredByIdc.filter((it) => tagFilters.some((t) => instanceHasTag(it, t)))
  }, [useDomainInstance, idc, domainFilter, filteredByIdc, tagFilters])

  useEffect(() => {
    if (isSR) {
      if (!filteredClusters.length) {
        if (clusterId) setClusterId('')
        return
      }
      if (!filteredClusters.some((c) => c.id === clusterId)) {
        setClusterId(filteredClusters[0].id)
        setFeInstances([])
        setBeInstances([])
      }
      return
    }
    if (!filteredInstances.length) {
      if (instanceId) setInstanceId('')
      return
    }
    if (!filteredInstances.some((i) => i.id === instanceId)) {
      setInstanceId(filteredInstances[0].id)
    }
  }, [isSR, filteredClusters, clusterId, filteredInstances, instanceId])

  const runQuery = useCallback(
    async (opts?: { silent?: boolean }) => {
      if (isSR) {
        if (!selectedCluster?.domain) {
          if (!opts?.silent) message.warning('请先选择集群')
          return
        }
      } else if (isEnvOnly) {
        if (!idc) {
          if (!opts?.silent) message.warning('请先选择环境（IDC）')
          return
        }
      } else if (useDomainInstance) {
        if (!idc) {
          if (!opts?.silent) message.warning('请先选择 IDC（环境）')
          return
        }
        if (!domainFilter) {
          if (!opts?.silent) message.warning('请先选择 domain')
          return
        }
        if (!instanceId) {
          if (!opts?.silent) message.warning('请先选择实例')
          return
        }
      } else {
        if (!tagFilters.length) {
          if (!opts?.silent) message.warning('请先选择实例标签')
          return
        }
        if (!instanceId) {
          if (!opts?.silent) message.warning('请先选择实例')
          return
        }
      }
      const seq = ++querySeqRef.current
      setQueried(true)
      settingsLoadedRef.current = false
      setLoadingOverview(true)
      setLoadingRest(true)
      const start = range[0].toISOString()
      const end = range[1].toISOString()

      const base = isSR
        ? {
            engine,
            idc: selectedCluster!.idc,
            domain: selectedCluster!.domain,
            cluster_id: selectedCluster!.id,
            fe_instances: feInstances.length ? feInstances : undefined,
            be_instances: beInstances.length ? beInstances : undefined,
            start,
            end,
            step: 15,
          }
        : isEnvOnly
          ? {
              engine,
              idc: idc || '',
              domain: domainFilter || undefined,
              start,
              end,
              step: 15,
            }
          : {
              instance_id: instanceId,
              engine,
              start,
              end,
              step: 15,
            }

      const restSections = isSR
        ? SR_REST_SECTIONS
        : useDomainInstance
          ? isHost
            ? ['db_stats', 'sessions']
            : ['db_stats']
          : ['db_stats', 'sessions']

      // SR / Redis：overview 完成后再拉 rest，避免与 Prom 并发争用导致整页长尾（大集群尤其明显）
      const staggerRest = isSR || isRedis

      const runOverview = () =>
        fetchDbMonitorQuery({ ...base, sections: ['overview'] })
          .then((panels) => {
            if (seq !== querySeqRef.current) return
            setDataMap((prev) => ({ ...prev, ...mergeByKey(panels) }))
          })
          .catch((e: any) => {
            if (seq !== querySeqRef.current) return
            if (!opts?.silent) {
              message.error(e?.response?.data?.message || e?.message || 'Overview 查询失败')
            }
          })
          .finally(() => {
            if (seq === querySeqRef.current) setLoadingOverview(false)
          })

      const runRest = () =>
        fetchDbMonitorQuery({ ...base, sections: restSections })
          .then((panels) => {
            if (seq !== querySeqRef.current) return
            setDataMap((prev) => ({ ...prev, ...mergeByKey(panels) }))
          })
          .catch((e: any) => {
            if (seq !== querySeqRef.current) return
            if (!opts?.silent) {
              message.error(e?.response?.data?.message || e?.message || '指标查询失败')
            }
          })
          .finally(() => {
            if (seq === querySeqRef.current) setLoadingRest(false)
          })

      if (staggerRest) {
        await runOverview()
        if (seq !== querySeqRef.current) return
        await runRest()
        return
      }

      const overviewP = runOverview()
      const restP = runRest()
      await overviewP
      if (seq !== querySeqRef.current) return

      if (!isSR && !isEnvOnly && !collapsedRef.current.settings && instanceId) {
        try {
          const settings = await fetchDbMonitorPanels({
            instance_id: instanceId,
            engine,
            section: 'settings',
            start,
            end,
            step: 15,
          })
          if (seq !== querySeqRef.current) return
          settingsLoadedRef.current = true
          setDataMap((prev) => ({ ...prev, ...mergeByKey(settings) }))
        } catch {
          /* ignore */
        }
      }
      await restP
    },
    [isSR, isRedis, isHost, isEnvOnly, useDomainInstance, engine, selectedCluster, feInstances, beInstances, instanceId, range, tagFilters, idc, domainFilter],
  )

  const loadSettingsIfNeeded = useCallback(async () => {
    if (isSR || isEnvOnly || !queried || !instanceId || settingsLoadedRef.current) return
    settingsLoadedRef.current = true
    try {
      const start = range[0].toISOString()
      const end = range[1].toISOString()
      const settings = await fetchDbMonitorPanels({
        instance_id: instanceId,
        engine,
        section: 'settings',
        start,
        end,
        step: 15,
      })
      setDataMap((prev) => ({ ...prev, ...mergeByKey(settings) }))
    } catch {
      settingsLoadedRef.current = false
    }
  }, [isSR, isEnvOnly, engine, queried, instanceId, range])

  const toggle = (key: string) => {
    setCollapsed((c) => {
      // true/undefined = 折叠；false = 展开
      const willOpen = c[key] !== false
      if (key === 'settings' && willOpen) {
        void loadSettingsIfNeeded()
      }
      return { ...c, [key]: !willOpen }
    })
  }

  // 选中后 debounce 自动查询
  useEffect(() => {
    if (isSR) {
      if (!selectedCluster?.domain) return
    } else if (isEnvOnly) {
      if (!idc) return
    } else if (useDomainInstance) {
      if (!idc || !domainFilter || !instanceId) return
    } else {
      if (!instanceId || !tagFilters.length) return
    }
    if (autoQueryTimerRef.current) window.clearTimeout(autoQueryTimerRef.current)
    autoQueryTimerRef.current = window.setTimeout(() => {
      void runQuery({ silent: true })
    }, 300)
    return () => {
      if (autoQueryTimerRef.current) window.clearTimeout(autoQueryTimerRef.current)
    }
  }, [
    isSR,
    isRedis,
    isHost,
    isEnvOnly,
    useDomainInstance,
    selectedCluster?.id,
    feInstances,
    beInstances,
    instanceId,
    tagFilters.length,
    idc,
    domainFilter,
    range,
    engine,
    runQuery,
  ])

  const applyRangePreset = (key: string) => {
    const p = RANGE_PRESETS.find((x) => x.key === key)
    if (!p) return
    setRangePreset(key)
    setRange(rangeFromPreset(p))
  }

  const panelLoading = (key: string) => {
    if (isOverviewPanel(key)) return loadingOverview
    return loadingRest
  }

  const sortFor = (key: string): LegendSort => chartSort[key] || { by: 'max', dir: 'desc' }

  const onLegendSort = (panelKey: string, by: LegendSortBy) => {
    setChartSort((prev) => {
      const cur = prev[panelKey] || { by: 'max', dir: 'desc' }
      if (cur.by === by) {
        return { ...prev, [panelKey]: { by, dir: cur.dir === 'desc' ? 'asc' : 'desc' } }
      }
      return { ...prev, [panelKey]: { by, dir: 'desc' } }
    })
  }

  const clusterOptions = useMemo(
    () =>
      filteredClusters.map((c) => ({
        label: c.domain || c.id,
        value: c.id,
      })),
    [filteredClusters],
  )

  const instOptions = useMemo(
    () =>
      filteredInstances.map((i) => {
        const host = String(i.host || '').replace(/:\d+$/, '') || i.name || i.id
        return { label: host, value: i.id }
      }),
    [filteredInstances],
  )

  const feOptions = useMemo(
    () => (selectedCluster?.fe_endpoints || []).map((ep) => ({ label: ep, value: ep })),
    [selectedCluster],
  )
  const beOptions = useMemo(
    () => (selectedCluster?.be_endpoints || []).map((ep) => ({ label: ep, value: ep })),
    [selectedCluster],
  )

  const emptyHint = isSR
    ? '请先选择集群后点击「查询」'
    : isEnvOnly
      ? '请先选择环境后点击「查询」'
      : useDomainInstance
        ? '请先选择 IDC 与 domain 后点击「查询」'
        : '请先选择实例标签后点击「查询」'

  const renderStat = (tpl: TemplatePanel) => {
    const d = dataMap[tpl.key]
    const busy = panelLoading(tpl.key)
    let text = '-'
    if (busy && !d) text = '…'
    else if (d?.error) text = d.error
    else if (d?.text) text = d.text
    else if (d?.value != null && Number.isFinite(d.value)) text = String(d.value)
    else if (!queried) text = '-'
    return (
      <div key={tpl.key} className="dbmon-kpi">
        <div className="dbmon-kpi-title">{tpl.title}</div>
        <div className={`dbmon-kpi-value${d?.error ? ' is-error' : ''}`}>{text}</div>
      </div>
    )
  }

  const renderChart = (tpl: TemplatePanel) => {
    const d = dataMap[tpl.key]
    const height = tpl.span === 'full' ? '280px' : '260px'
    const sort = sortFor(tpl.key)
    const hasSeries = !!(d && (d.series || []).length > 0)
    const busy = panelLoading(tpl.key)
    let body: React.ReactNode
    if (busy && !hasSeries && !d?.error) {
      body = (
        <div className="dbmon-chart-placeholder">
          <Spin size="small" /> <span>加载中…</span>
        </div>
      )
    } else if (d?.error && !hasSeries) {
      body = <Typography.Text type="danger">{d.error}</Typography.Text>
    } else if (hasSeries) {
      body = (
        <MonitorTimeseriesChart
          panel={d!}
          mode={resolvedMode}
          sort={sort}
          panelKey={tpl.key}
          height={height}
          onLegendSort={onLegendSort}
        />
      )
    } else {
      body = <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={queried ? '无数据' : emptyHint} />
    }
    return (
      <div key={tpl.key} className={`dbmon-chart ${tpl.span === 'full' ? 'full' : 'half'}`}>
        <div className="dbmon-chart-title">{tpl.title}</div>
        {body}
      </div>
    )
  }

  return (
    <div className="dbmon-page">
      <div className="dbmon-toolbar">
        <Space wrap size={[12, 8]}>
          <span className="dbmon-label">{isSR || isEnvOnly ? '环境' : 'IDC'}</span>
          <Select
            allowClear
            style={{ minWidth: 96, maxWidth: 160 }}
            placeholder={isSR || isEnvOnly ? '请选择环境' : '全部 IDC'}
            value={idc}
            options={idcOptions}
            onChange={(v) => {
              setIdc(v)
              setTagFilters([])
              setDomainFilter(undefined)
              setClusterId('')
              setFeInstances([])
              setBeInstances([])
            }}
            showSearch
            optionFilterProp="label"
          />
          {isSR ? (
            <>
              <span className="dbmon-label">集群</span>
              <Select
                style={{ minWidth: 180, maxWidth: 280 }}
                value={clusterId || undefined}
                placeholder={
                  !idc ? '请先选择环境' : filteredClusters.length ? '选择集群' : '该环境无合规注册集群'
                }
                options={clusterOptions}
                onChange={(v) => {
                  setClusterId(v || '')
                  setFeInstances([])
                  setBeInstances([])
                }}
                showSearch
                optionFilterProp="label"
                disabled={!idc || !filteredClusters.length}
              />
              <span className="dbmon-label">FE</span>
              <Select
                mode="multiple"
                allowClear
                maxTagCount="responsive"
                style={{ minWidth: 140, maxWidth: 220 }}
                placeholder="All"
                value={feInstances}
                options={feOptions}
                onChange={(v) => setFeInstances(v || [])}
                disabled={!selectedCluster}
              />
              <span className="dbmon-label">BE</span>
              <Select
                mode="multiple"
                allowClear
                maxTagCount="responsive"
                style={{ minWidth: 140, maxWidth: 220 }}
                placeholder="All"
                value={beInstances}
                options={beOptions}
                onChange={(v) => setBeInstances(v || [])}
                disabled={!selectedCluster}
              />
            </>
          ) : isEnvOnly ? (
            <>
              <span className="dbmon-label">{engine === 'debezium' ? 'connector' : 'application'}</span>
              <Input
                allowClear
                style={{ width: 220 }}
                placeholder={engine === 'debezium' ? '可选 connector/name' : '可选 application'}
                value={domainFilter || ''}
                onChange={(e) => setDomainFilter(e.target.value.trim() || undefined)}
                disabled={!idc}
              />
            </>
          ) : useDomainInstance ? (
            <>
              <span className="dbmon-label">domain</span>
              <Select
                allowClear
                style={{ minWidth: 200, maxWidth: 320 }}
                placeholder={!idc ? '请先选择 IDC' : domainOptions.length ? '选择 domain' : '无匹配 domain'}
                value={domainFilter}
                options={domainOptions}
                onChange={(v) => setDomainFilter(v)}
                showSearch
                optionFilterProp="label"
                disabled={!idc || !domainOptions.length}
              />
              <span className="dbmon-label">{isHost ? '主机' : '实例'}</span>
              <Select
                style={{ minWidth: 200, maxWidth: 320 }}
                value={instanceId || undefined}
                placeholder={
                  !domainFilter
                    ? '请先选择 domain'
                    : filteredInstances.length
                      ? isHost
                        ? '选择主机'
                        : '选择实例'
                      : isHost
                        ? '无匹配主机'
                        : '无匹配实例'
                }
                options={instOptions}
                onChange={(v) => setInstanceId(v || '')}
                showSearch
                optionFilterProp="label"
                disabled={!domainFilter || !filteredInstances.length}
              />
            </>
          ) : (
            <>
              <span className="dbmon-label">实例标签</span>
              <Select
                mode="multiple"
                allowClear
                maxTagCount="responsive"
                style={{ minWidth: 200, maxWidth: 360 }}
                popupMatchSelectWidth={false}
                dropdownStyle={{ minWidth: 280 }}
                placeholder="请选择标签"
                value={tagFilters}
                options={tagOptions}
                onChange={(v) => setTagFilters(v || [])}
                showSearch
                optionFilterProp="label"
              />
              <span className="dbmon-label">实例</span>
              <Select
                style={{ minWidth: 200, maxWidth: 320 }}
                value={instanceId || undefined}
                placeholder={
                  !tagFilters.length
                    ? '请先选择标签'
                    : filteredInstances.length
                      ? '选择实例'
                      : '无匹配实例'
                }
                options={instOptions}
                onChange={(v) => setInstanceId(v || '')}
                showSearch
                optionFilterProp="label"
                disabled={!tagFilters.length || !filteredInstances.length}
              />
            </>
          )}
          <span className="dbmon-label">时间</span>
          <div className="dbmon-range-presets" role="group" aria-label="时间快捷">
            {RANGE_PRESETS.map((p) => (
              <button
                key={p.key}
                type="button"
                className={`dbmon-range-preset${rangePreset === p.key ? ' is-active' : ''}`}
                onClick={() => applyRangePreset(p.key)}
              >
                {p.label}
              </button>
            ))}
          </div>
          <RangePicker
            showTime
            value={range}
            onChange={(v) => {
              if (v && v[0] && v[1]) {
                setRangePreset(null)
                setRange([v[0], v[1]])
              }
            }}
          />
          <Button type="primary" icon={<SearchOutlined />} loading={loadingOverview} onClick={() => void runQuery()}>
            查询
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={loadingOverview}
            onClick={() => void runQuery()}
            disabled={!queried}
          >
            刷新
          </Button>
        </Space>
      </div>

      {template.map((sec) => {
        const open = collapsed[sec.key] === false
        return (
          <section key={sec.key} className="dbmon-section">
            <button type="button" className="dbmon-section-hd" onClick={() => toggle(sec.key)}>
              <span>
                {open ? '−' : '+'} {sec.title}
              </span>
            </button>
            {open ? (
              <div className="dbmon-section-bd">
                {sec.stats.length > 0 ? (
                  <div className={`dbmon-kpi-row${sec.key === 'settings' || sec.key === 'fe_jvm' ? ' settings' : ''}`}>
                    {sec.stats.map(renderStat)}
                  </div>
                ) : null}
                {sec.charts.length > 0 ? (
                  <div className="dbmon-chart-grid">{sec.charts.map(renderChart)}</div>
                ) : null}
              </div>
            ) : null}
          </section>
        )
      })}
    </div>
  )
}
