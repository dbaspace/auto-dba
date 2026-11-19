import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, DatePicker, Empty, Modal, Select, Space, Switch, Tabs, message } from 'antd'
import { DownloadOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import { downloadTextFile } from '../query/exportResult'
import Auth from '../../components/Auth'
import { Perm, usePermission } from '../../hooks/usePermission'
import type { InsightTabKey, SnapshotFilter } from './sql-insight/types'
import SqlInsightTab from './sql-insight/SqlInsightTab'
import InsightAnalysisTab from './sql-insight/InsightAnalysisTab'
import VisualSummaryTab from './sql-insight/VisualSummaryTab'
import AwrReportTab from './sql-insight/AwrReportTab'
import PgSpaceTab from './sql-insight/PgSpaceTab'
import SmartAnalysisTab from './sql-insight/SmartAnalysisTab'
import CloudSlowLogTab from './sql-insight/CloudSlowLogTab'
import CloudSlowPipelineTab from './sql-insight/CloudSlowPipelineTab'
import TopologyTab, { type TopologyExportRow } from './sql-insight/TopologyTab'
import './SqlInsight.css'

const { RangePicker } = DatePicker

function defaultRange(): [Dayjs, Dayjs] {
  const end = dayjs()
  return [end.subtract(1, 'hour'), end]
}

function csvCell(value: string | number) {
  const text = String(value ?? '')
  if (/[",\n]/.test(text)) return `"${text.replace(/"/g, '""')}"`
  return text
}

function tagValue(tags: unknown, key: string) {
  const list = Array.isArray(tags) ? tags : []
  const prefix = `${key}=`
  for (const raw of list) {
    const text = String(raw || '').trim()
    if (text.toLowerCase().startsWith(prefix)) return text.slice(prefix.length).trim()
  }
  return ''
}

function instDomain(i: any) {
  const fromTag = tagValue(i?.tags, 'domain')
  if (fromTag) return fromTag
  if (i?.domain_name) return String(i.domain_name)
  const domains = Array.isArray(i?.domains) ? i.domains.filter(Boolean) : []
  return domains[0] ? String(domains[0]) : ''
}

function instLabel(i: any) {
  const name = i?.name || '未命名实例'
  const domain = instDomain(i)
  if (!domain) return name
  return `${name} (${domain}${i?.port ? `:${i.port}` : ''})`
}

function isPG(i: any) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'postgres' || t === 'postgresql' || t === 'pg'
}

function buildFilter(instanceId: string, range: [Dayjs, Dayjs], instances: any[]): SnapshotFilter {
  const inst = instances.find((i) => i.id === instanceId)
  const host = inst ? String(inst.host || inst.domain_name || '') : ''
  const port = inst ? Number(inst.port) || undefined : undefined
  return {
    instanceId: instanceId || '',
    startTime: range[0].toISOString(),
    endTime: range[1].toISOString(),
    host: host || undefined,
    port,
  }
}

export default function SqlInsight() {
  const { hasUrn } = usePermission()
  const [instances, setInstances] = useState<any[]>([])
  const [instancesLoading, setInstancesLoading] = useState(false)
  const [draftInstance, setDraftInstance] = useState('')
  const [draftRange, setDraftRange] = useState<[Dayjs, Dayjs]>(defaultRange())
  const [filter, setFilter] = useState<SnapshotFilter>(() => buildFilter('', defaultRange(), []))
  const [tab, setTab] = useState<InsightTabKey>('sql')
  const [showSql, setShowSql] = useState(true)
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [intervalSec, setIntervalSec] = useState(15)
  const [refreshTick, setRefreshTick] = useState(0)
  const [collecting, setCollecting] = useState(false)
  const [environments, setEnvironments] = useState<any[]>([])
  const [topoEnv, setTopoEnv] = useState('')
  const [topoInstDraft, setTopoInstDraft] = useState('')
  const [topoQueryId, setTopoQueryId] = useState('')
  const [topoTick, setTopoTick] = useState(0)
  const [topoRows, setTopoRows] = useState<TopologyExportRow[]>([])
  const onTopoRows = useCallback((list: TopologyExportRow[]) => setTopoRows(list), [])

  const loadInstances = useCallback(async () => {
    setInstancesLoading(true)
    try {
      const { data } = await api.get('/instances', { params: { db_type: 'postgres' } })
      const list = (data.data || []).filter(isPG)
      setInstances(list)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载实例失败')
    } finally {
      setInstancesLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadInstances()
  }, [loadInstances])

  const applyQuery = () => {
    const nextRange = draftRange?.length === 2 ? draftRange : defaultRange()
    setFilter(buildFilter(draftInstance, nextRange, instances))
    setRefreshTick((n) => n + 1)
  }

  const resetQuery = () => {
    const next = defaultRange()
    setDraftInstance('')
    setDraftRange(next)
    setFilter(buildFilter('', next, instances))
    setRefreshTick((n) => n + 1)
    void loadInstances()
  }

  const collect = () => {
    Modal.confirm({
      title: '手动采集活动快照',
      content: filter.instanceId
        ? '将对当前实例立即采集一次活动会话，确定？'
        : '将对所有 PostgreSQL 实例立即采集一次活动会话，可能耗时较久，确定？',
      okText: '确定采集',
      onOk: async () => {
        setCollecting(true)
        try {
          const { data } = await api.post(
            '/pg-activity-snapshots/collect',
            { instance_id: filter.instanceId || undefined },
            { timeout: 120000 },
          )
          message.success(`采集完成：处理 ${data.instances_processed ?? 0} 个实例，写入 ${data.rows_written ?? 0} 条`)
          if ((data.warnings || []).length) message.warning(data.warnings.slice(0, 3).join('；'))
          setRefreshTick((n) => n + 1)
        } finally {
          setCollecting(false)
        }
      },
    })
  }

  const topoInstances = useMemo(() => {
    if (!topoEnv) return []
    const env = environments.find((e) => e.id === topoEnv)
    const wanted = new Set(
      [topoEnv, env?.code, env?.name]
        .map((v) => String(v || '').trim().toLowerCase())
        .filter(Boolean),
    )
    return instances.filter((i) => {
      const keys = [i.env_id, i.idc, i.env, i.env_code]
        .map((v) => String(v || '').trim().toLowerCase())
        .filter(Boolean)
      return keys.some((k) => wanted.has(k))
    })
  }, [environments, instances, topoEnv])

  const queryTopology = () => {
    if (!topoInstDraft) {
      message.warning('请选择实例')
      return
    }
    setTopoQueryId(topoInstDraft)
    setTopoTick((n) => n + 1)
  }

  const resetTopology = () => {
    setTopoEnv('')
    setTopoInstDraft('')
    setTopoQueryId('')
    setTopoTick((n) => n + 1)
  }

  const exportTopology = () => {
    if (!topoRows.length) {
      message.warning('请先查询')
      return
    }
    const header = ['客户端地址', '数据库', '连接数', 'Pod', '命名空间', '节点']
    const body = topoRows.map((row) => [row.host, row.database, row.connections, row.pod, row.namespace, row.node])
    const csv = `\uFEFF${[header, ...body].map((cols) => cols.map(csvCell).join(',')).join('\n')}`
    const stamp = new Date().toISOString().replace(/[-:]/g, '').slice(0, 15)
    downloadTextFile(`topology_${stamp}.csv`, csv)
    message.success('已导出')
  }

  const allowedTabs = useMemo(() => {
    const order: InsightTabKey[] = ['sql', 'analysis', 'visual', 'awr', 'space', 'smart', 'cloud', 'pipeline', 'topology']
    const urnOf: Record<InsightTabKey, string> = {
      sql: Perm.sqlInsight.tab.sql,
      analysis: Perm.sqlInsight.tab.analysis,
      visual: Perm.sqlInsight.tab.visual,
      awr: Perm.sqlInsight.tab.awr,
      space: Perm.sqlInsight.tab.space,
      smart: Perm.sqlInsight.tab.smart,
      cloud: Perm.sqlInsight.tab.cloud,
      pipeline: Perm.sqlInsight.tab.pipeline,
      topology: Perm.sqlInsight.tab.topology,
    }
    return order.filter((key) => hasUrn(urnOf[key]))
  }, [hasUrn])

  const loadEnvironments = useCallback(async () => {
    try {
      const { data } = await api.get('/environments')
      setEnvironments(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载环境失败')
    }
  }, [])

  const canTopology = hasUrn(Perm.sqlInsight.tab.topology)
  useEffect(() => {
    if (!canTopology) return
    void loadEnvironments()
  }, [canTopology, loadEnvironments])

  const visibleTab = allowedTabs.includes(tab) ? tab : allowedTabs[0]

  const tabItems = useMemo(() => {
    if (!visibleTab) return []
    const cloudFilter = buildFilter(draftInstance, draftRange, instances)
    const all: { key: InsightTabKey; label: string; children: React.ReactNode }[] = [
      {
        key: 'sql',
        label: '快照查询',
        children: (
          <SqlInsightTab
            filter={filter}
            showSql={showSql}
            autoRefresh={autoRefresh}
            intervalSec={intervalSec}
            refreshTick={refreshTick}
            active={visibleTab === 'sql'}
          />
        ),
      },
      {
        key: 'analysis',
        label: '洞察分析',
        children: <InsightAnalysisTab filter={filter} active={visibleTab === 'analysis'} refreshTick={refreshTick} />,
      },
      {
        key: 'visual',
        label: '可视汇总',
        children: <VisualSummaryTab filter={filter} active={visibleTab === 'visual'} refreshTick={refreshTick} />,
      },
      {
        key: 'awr',
        label: 'AWR报告',
        children: <AwrReportTab filter={filter} active={visibleTab === 'awr'} />,
      },
      {
        key: 'space',
        label: '表空间分析',
        children: <PgSpaceTab filter={filter} active={visibleTab === 'space'} />,
      },
      {
        key: 'smart',
        label: '智能分析',
        children: <SmartAnalysisTab filter={filter} active={visibleTab === 'smart'} />,
      },
      {
        key: 'cloud',
        label: '云慢日志',
        children: <CloudSlowLogTab filter={cloudFilter} active={visibleTab === 'cloud'} />,
      },
      {
        key: 'pipeline',
        label: '同步监控',
        children: <CloudSlowPipelineTab active={visibleTab === 'pipeline'} />,
      },
      {
        key: 'topology',
        label: '拓扑视图',
        children: (
          <TopologyTab
            instanceId={topoQueryId}
            refreshTick={topoTick}
            active={visibleTab === 'topology'}
            onRows={onTopoRows}
          />
        ),
      },
    ]
    return all.filter((item) => allowedTabs.includes(item.key))
  }, [allowedTabs, autoRefresh, draftInstance, draftRange, filter, instances, intervalSec, onTopoRows, refreshTick, showSql, topoQueryId, topoTick, visibleTab])

  const tabExtra = (
    <Space wrap size="small">
      {visibleTab === 'sql' ? (
        <>
          <span className="muted">显示 SQL</span>
          <Switch size="small" checked={showSql} onChange={setShowSql} />
          <span className="muted">自动刷新</span>
          <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
          <Select
            value={intervalSec}
            size="small"
            disabled={!autoRefresh}
            style={{ width: 80 }}
            onChange={setIntervalSec}
            options={[15, 30, 45, 60].map((s) => ({ label: `${s}s`, value: s }))}
          />
          <Button size="small" icon={<ReloadOutlined />} onClick={() => setRefreshTick((n) => n + 1)}>
            刷新
          </Button>
          <Auth urn={Perm.sqlInsight.collect}>
            <Button size="small" type="primary" loading={collecting} onClick={collect}>
              手动采集
            </Button>
          </Auth>
        </>
      ) : null}
    </Space>
  )

  return (
    <div className="ops-page">
      {visibleTab === 'topology' ? (
        <div className="ops-filters">
          <div className="filter-item" style={{ flex: 'none' }}>
            <Select
              value={topoEnv || undefined}
              showSearch
              allowClear
              placeholder="选择环境"
              style={{ width: 200 }}
              optionFilterProp="label"
              onChange={(v) => {
                setTopoEnv(v || '')
                setTopoInstDraft('')
              }}
              options={environments.map((e) => ({
                label: e.name || e.code || e.id,
                value: e.id,
              }))}
            />
          </div>
          <div className="filter-item" style={{ flex: 'none' }}>
            <Select
              value={topoInstDraft || undefined}
              showSearch
              allowClear
              placeholder="选择实例地址"
              style={{ width: 360 }}
              popupMatchSelectWidth={false}
              optionFilterProp="label"
              disabled={!topoEnv}
              loading={instancesLoading}
              notFoundContent={instancesLoading ? '正在加载实例' : '该环境没有 PostgreSQL 实例'}
              onChange={(v) => setTopoInstDraft(v || '')}
              options={topoInstances.map((i) => ({ label: instLabel(i), value: i.id }))}
            />
          </div>
          <span className="label">最近 5 分钟</span>
          <Button type="primary" icon={<SearchOutlined />} onClick={queryTopology}>
            查询
          </Button>
          <Button onClick={resetTopology}>重置</Button>
          <Button icon={<DownloadOutlined />} disabled={!topoRows.length} onClick={exportTopology}>导出</Button>
        </div>
      ) : (
        <div className="ops-filters">
          <div className="filter-item" style={{ flex: 'none' }}>
            <Select
              value={draftInstance || undefined}
              showSearch
              allowClear
              placeholder="选择 PostgreSQL 实例"
              style={{ width: 360 }}
              popupMatchSelectWidth={false}
              optionFilterProp="label"
              onChange={(v) => setDraftInstance(v || '')}
              options={instances.map((i) => ({ label: instLabel(i), value: i.id }))}
            />
          </div>
          <div className="filter-item" style={{ flex: 'none' }}>
            <span className="label">时间范围</span>
            <RangePicker
              value={draftRange}
              showTime
              format="YYYY-MM-DD HH:mm:ss"
              placeholder={['开始', '结束']}
              style={{ width: 380 }}
              onChange={(v) => {
                if (v && v[0] && v[1]) setDraftRange([v[0], v[1]])
              }}
            />
          </div>
          <Button type="primary" icon={<SearchOutlined />} onClick={applyQuery}>
            查询
          </Button>
          <Button onClick={resetQuery}>重置</Button>
        </div>
      )}

      {tabItems.length === 0 || !visibleTab ? (
        <Empty description="当前角色没有可查看的页签" />
      ) : (
        <Tabs
          activeKey={visibleTab}
          onChange={(k) => setTab(k as InsightTabKey)}
          tabBarExtraContent={visibleTab === 'sql' ? tabExtra : undefined}
          items={tabItems.map(({ key, label, children }) => ({ key, label, children }))}
        />
      )}
    </div>
  )
}
