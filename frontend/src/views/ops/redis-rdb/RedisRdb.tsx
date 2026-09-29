import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  Empty,
  Input,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd'
import { CloudDownloadOutlined, CloudUploadOutlined, DeleteOutlined, ReloadOutlined } from '@ant-design/icons'
import ResponsiveTable from '../../../components/ResponsiveTable'
import {
  deleteRedisRdbJob,
  fetchRedisRdbAgents,
  fetchRedisRdbInstances,
  fetchRedisRdbJob,
  fetchRedisRdbJobs,
  fetchRedisRdbOnline,
  fetchRedisRdbReport,
  fetchRedisRdbViaAgent,
  formatBytes,
  uploadRedisRdb,
} from './services/redisRdbApi'
import './RedisRdb.css'

type TabKey = 'online' | 'jobs' | 'report'

const TTL_LABEL: Record<string, string> = {
  no_ttl: '无 TTL',
  expired: '已过期',
  lt_1h: '< 1 小时',
  '1h_24h': '1h–24h',
  '1d_7d': '1–7 天',
  gt_7d: '> 7 天',
}

function levelColor(level?: string) {
  if (level === 'critical') return 'red'
  if (level === 'warn') return 'orange'
  return 'blue'
}

function statusTag(status?: string) {
  const map: Record<string, string> = {
    pending: 'default',
    running: 'processing',
    succeeded: 'success',
    failed: 'error',
  }
  return <Tag color={map[status || ''] || 'default'}>{status || '-'}</Tag>
}

export default function RedisRdb() {
  const [tab, setTab] = useState<TabKey>('online')
  const [instances, setInstances] = useState<any[]>([])
  const [agents, setAgents] = useState<any[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [agentId, setAgentId] = useState('')
  const [online, setOnline] = useState<any>(null)
  const [jobs, setJobs] = useState<any[]>([])
  const [jobId, setJobId] = useState('')
  const [report, setReport] = useState<any>(null)
  const [note, setNote] = useState('')
  const [loading, setLoading] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [fetching, setFetching] = useState(false)

  const loadInstances = useCallback(async () => {
    try {
      setInstances(await fetchRedisRdbInstances())
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载实例失败')
    }
  }, [])

  const loadAgents = useCallback(async () => {
    try {
      setAgents(await fetchRedisRdbAgents())
    } catch {
      setAgents([])
    }
  }, [])

  const loadJobs = useCallback(async () => {
    try {
      const list = await fetchRedisRdbJobs()
      setJobs(list)
      return list
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载任务失败')
      return []
    }
  }, [])

  const loadOnline = useCallback(async () => {
    if (!instanceId) {
      setOnline(null)
      return
    }
    setLoading(true)
    try {
      setOnline(await fetchRedisRdbOnline(instanceId))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '在线采集失败')
      setOnline(null)
    } finally {
      setLoading(false)
    }
  }, [instanceId])

  const loadReport = useCallback(async (id: string) => {
    if (!id) {
      setReport(null)
      return
    }
    setLoading(true)
    try {
      const job = await fetchRedisRdbJob(id)
      if (job?.status === 'succeeded') {
        setReport(await fetchRedisRdbReport(id))
      } else {
        setReport(null)
        if (job?.status === 'failed') {
          message.error(job.error || '分析失败')
        } else {
          message.info(`任务状态：${job?.status || '未知'}，完成后可查看报告`)
        }
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载报告失败')
      setReport(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadInstances()
    loadAgents()
    loadJobs()
  }, [loadInstances, loadAgents, loadJobs])

  useEffect(() => {
    if (tab === 'online') loadOnline()
  }, [tab, loadOnline])

  useEffect(() => {
    if (tab === 'jobs') loadJobs()
  }, [tab, loadJobs])

  // 轮询运行中任务
  useEffect(() => {
    const running = jobs.some((j) => j.status === 'pending' || j.status === 'running')
    if (!running) return
    const t = window.setInterval(async () => {
      const list = await loadJobs()
      if (jobId) {
        const cur = list.find((j: any) => j.id === jobId)
        if (cur?.status === 'succeeded' && tab === 'report') {
          loadReport(jobId)
        }
      }
    }, 2500)
    return () => window.clearInterval(t)
  }, [jobs, jobId, tab, loadJobs, loadReport])

  const instOptions = useMemo(
    () =>
      instances.map((i) => ({
        value: i.id,
        label: `${i.name || i.id} (${i.host || '-'}:${i.port || '-'})`,
      })),
    [instances],
  )

  const agentOptions = useMemo(
    () =>
      agents
        .filter((a) => a.rdb_ready !== false)
        .map((a) => ({
          value: a.id,
          label: `${a.name || a.id}${a.base_url ? ` · ${a.base_url}` : ''}${a.rdb_ready ? '' : '（RDB 未就绪）'}`,
        })),
    [agents],
  )

  async function onUpload(file: File) {
    setUploading(true)
    try {
      const job = await uploadRedisRdb(file, instanceId || undefined, note || undefined)
      message.success('已提交分析任务')
      setJobId(job?.id || '')
      setTab('jobs')
      await loadJobs()
      if (job?.id) {
        window.setTimeout(() => {
          setTab('report')
          loadReport(job.id)
        }, 1500)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '上传失败')
    } finally {
      setUploading(false)
    }
    return false
  }

  async function onFetchViaAgent() {
    if (!instanceId) {
      message.warning('请先选择 Redis 实例')
      return
    }
    if (!agentId) {
      message.warning('请选择公共主机平台 Agent')
      return
    }
    setFetching(true)
    try {
      const job = await fetchRedisRdbViaAgent(instanceId, agentId, note || undefined)
      message.success('已提交 Agent 拉取任务')
      setJobId(job?.id || '')
      setTab('jobs')
      await loadJobs()
      if (job?.id) {
        window.setTimeout(() => {
          setTab('report')
          loadReport(job.id)
        }, 2000)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '拉取失败')
    } finally {
      setFetching(false)
    }
  }

  async function onDelete(id: string) {
    try {
      await deleteRedisRdbJob(id)
      message.success('已删除')
      if (jobId === id) {
        setJobId('')
        setReport(null)
      }
      await loadJobs()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '删除失败')
    }
  }

  const parsed = online?.parsed || {}
  const adviceList = tab === 'online' ? online?.advice || [] : report?.advice || []

  return (
    <div className="ops-page redis-rdb-page">
      <Typography.Title level={4} style={{ margin: 0 }}>
        Redis 内存分析（RDB）
      </Typography.Title>
      <div className="redis-rdb-hint">
        解析在平台本机完成。可通过「配置中心 → 平台 Agent」（归属含 Redis RDB）拉取，或手动上传
        dump.rdb。不走工单 BGSAVE。
      </div>

      <div className="redis-rdb-toolbar">
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="Redis 实例"
          style={{ minWidth: 260 }}
          options={instOptions}
          value={instanceId || undefined}
          onChange={(v) => setInstanceId(v || '')}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="平台 Agent（公共主机）"
          style={{ minWidth: 280 }}
          options={agentOptions}
          value={agentId || undefined}
          onChange={(v) => setAgentId(v || '')}
        />
        <Input
          placeholder="备注（可选）"
          style={{ width: 160 }}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
        <Button
          type="primary"
          icon={<CloudDownloadOutlined />}
          loading={fetching}
          onClick={onFetchViaAgent}
        >
          经 Agent 拉取并分析
        </Button>
        <Upload accept=".rdb" showUploadList={false} beforeUpload={onUpload}>
          <Button icon={<CloudUploadOutlined />} loading={uploading}>
            上传 RDB
          </Button>
        </Upload>
        <Button icon={<ReloadOutlined />} onClick={() => (tab === 'online' ? loadOnline() : tab === 'jobs' ? loadJobs() : loadReport(jobId))}>
          刷新
        </Button>
      </div>

      <Segmented
        value={tab}
        onChange={(v) => setTab(v as TabKey)}
        options={[
          { label: '在线 INFO', value: 'online' },
          { label: '分析任务', value: 'jobs' },
          { label: 'RDB 报告', value: 'report' },
        ]}
      />

      {tab === 'online' && (
        <>
          {!instanceId ? (
            <Empty description="请选择 Redis 实例后采集在线内存快照" />
          ) : loading && !online ? (
            <Empty description="加载中…" />
          ) : !online ? (
            <Empty description="暂无数据" />
          ) : (
            <>
              <div className="redis-rdb-cards">
                <div className="redis-rdb-card">
                  <div className="label">used_memory</div>
                  <div className="value">{parsed.used_memory_human || formatBytes(parsed.used_memory_bytes)}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">maxmemory</div>
                  <div className="value">{parsed.maxmemory_bytes ? formatBytes(parsed.maxmemory_bytes) : '未设置'}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">使用率</div>
                  <div className="value">
                    {parsed.used_memory_pct_of_max ? `${parsed.used_memory_pct_of_max.toFixed(1)}%` : '-'}
                  </div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">碎片率</div>
                  <div className="value">{parsed.mem_fragmentation_ratio?.toFixed?.(2) ?? '-'}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">Keys</div>
                  <div className="value">{parsed.total_keys ?? '-'}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">带 TTL</div>
                  <div className="value">{parsed.total_expires ?? '-'}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">淘汰策略</div>
                  <div className="value" style={{ fontSize: 14 }}>
                    {parsed.maxmemory_policy || '-'}
                  </div>
                </div>
              </div>
              <AdviceList items={adviceList} />
            </>
          )}
        </>
      )}

      {tab === 'jobs' && (
        <ResponsiveTable
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={jobs}
          pagination={{ pageSize: 20 }}
          columns={[
            {
              title: '文件',
              dataIndex: 'file_name',
              ellipsis: true,
              render: (v: string, r: any) => (
                <a
                  onClick={() => {
                    setJobId(r.id)
                    setTab('report')
                    loadReport(r.id)
                  }}
                >
                  {v || r.id}
                </a>
              ),
            },
            { title: '大小', dataIndex: 'file_size', width: 100, render: (v: number) => formatBytes(v) },
            {
              title: '来源',
              dataIndex: 'source',
              width: 100,
              render: (v: string, r: any) =>
                v === 'agent' ? <Tag color="blue">Agent{r.agent_name ? ` · ${r.agent_name}` : ''}</Tag> : <Tag>上传</Tag>,
            },
            { title: '状态', dataIndex: 'status', width: 100, render: (v: string) => statusTag(v) },
            { title: '实例', dataIndex: 'instance_name', width: 140, ellipsis: true, render: (v: string) => v || '-' },
            { title: '创建人', dataIndex: 'created_by', width: 100 },
            {
              title: '创建时间',
              dataIndex: 'created_at',
              width: 180,
              render: (v: string) => (v ? new Date(v).toLocaleString() : '-'),
            },
            {
              title: '操作',
              width: 120,
              render: (_: any, r: any) => (
                <Space>
                  <Button
                    type="link"
                    size="small"
                    disabled={r.status !== 'succeeded'}
                    onClick={() => {
                      setJobId(r.id)
                      setTab('report')
                      loadReport(r.id)
                    }}
                  >
                    报告
                  </Button>
                  <Button type="link" size="small" danger icon={<DeleteOutlined />} onClick={() => onDelete(r.id)} />
                </Space>
              ),
            },
          ]}
        />
      )}

      {tab === 'report' && (
        <>
          {!jobId ? (
            <Empty description="请从任务列表打开报告，或上传 RDB" />
          ) : !report ? (
            <Empty description={loading ? '加载中…' : '报告尚未就绪'} />
          ) : (
            <>
              <div className="redis-rdb-cards">
                <div className="redis-rdb-card">
                  <div className="label">总 Keys</div>
                  <div className="value">{report.total_keys}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">估算内存</div>
                  <div className="value">{formatBytes(report.total_bytes)}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">有 TTL</div>
                  <div className="value">{report.keys_with_ttl}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">持久化</div>
                  <div className="value">{report.keys_persistent}</div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">Redis</div>
                  <div className="value" style={{ fontSize: 14 }}>
                    {report.redis_version || '-'}
                  </div>
                </div>
                <div className="redis-rdb-card">
                  <div className="label">耗时</div>
                  <div className="value" style={{ fontSize: 14 }}>
                    {report.duration_ms} ms
                  </div>
                </div>
              </div>

              <AdviceList items={adviceList} />

              <Typography.Title level={5}>按类型</Typography.Title>
              <ResponsiveTable
                rowKey="type"
                size="small"
                pagination={false}
                dataSource={report.by_type || []}
                columns={[
                  { title: '类型', dataIndex: 'type', width: 100 },
                  { title: '数量', dataIndex: 'count', width: 100 },
                  { title: '字节', dataIndex: 'bytes', render: (v: number) => formatBytes(v) },
                  {
                    title: '占比',
                    width: 90,
                    render: (_: any, r: any) =>
                      report.total_bytes ? `${((r.bytes / report.total_bytes) * 100).toFixed(1)}%` : '-',
                  },
                ]}
              />

              <Typography.Title level={5}>按 DB</Typography.Title>
              <ResponsiveTable
                rowKey="db"
                size="small"
                pagination={false}
                dataSource={report.by_db || []}
                columns={[
                  { title: 'DB', dataIndex: 'db', width: 80 },
                  { title: '数量', dataIndex: 'count', width: 100 },
                  { title: '字节', dataIndex: 'bytes', render: (v: number) => formatBytes(v) },
                ]}
              />

              <Typography.Title level={5}>Key 前缀 Top</Typography.Title>
              <ResponsiveTable
                rowKey="prefix"
                size="small"
                pagination={{ pageSize: 15 }}
                dataSource={report.by_prefix || []}
                columns={[
                  { title: '前缀', dataIndex: 'prefix', ellipsis: true },
                  { title: '数量', dataIndex: 'count', width: 100 },
                  { title: '字节', dataIndex: 'bytes', width: 120, render: (v: number) => formatBytes(v) },
                  {
                    title: '占比',
                    width: 90,
                    render: (_: any, r: any) =>
                      report.total_bytes ? `${((r.bytes / report.total_bytes) * 100).toFixed(1)}%` : '-',
                  },
                ]}
              />

              <Typography.Title level={5}>TTL 分布</Typography.Title>
              <ResponsiveTable
                rowKey="bucket"
                size="small"
                pagination={false}
                dataSource={report.ttl_buckets || []}
                columns={[
                  {
                    title: '桶',
                    dataIndex: 'bucket',
                    render: (v: string) => TTL_LABEL[v] || v,
                  },
                  { title: '数量', dataIndex: 'count', width: 100 },
                  { title: '字节', dataIndex: 'bytes', render: (v: number) => formatBytes(v) },
                ]}
              />

              <Typography.Title level={5}>大 Key Top</Typography.Title>
              <ResponsiveTable
                rowKey={(r: any) => `${r.db}:${r.key}`}
                size="small"
                pagination={{ pageSize: 20 }}
                dataSource={report.top_keys || []}
                columns={[
                  { title: 'DB', dataIndex: 'db', width: 60 },
                  { title: 'Key', dataIndex: 'key', ellipsis: true },
                  { title: '类型', dataIndex: 'type', width: 80 },
                  { title: '编码', dataIndex: 'encoding', width: 100 },
                  { title: '元素', dataIndex: 'elem_count', width: 80 },
                  { title: '字节', dataIndex: 'bytes', width: 110, render: (v: number) => formatBytes(v) },
                  {
                    title: 'TTL',
                    width: 160,
                    render: (_: any, r: any) =>
                      r.has_ttl ? r.expire_at || `${r.ttl_seconds}s` : '无',
                  },
                ]}
              />
            </>
          )}
        </>
      )}
    </div>
  )
}

function AdviceList({ items }: { items: any[] }) {
  if (!items?.length) return null
  return (
    <div className="redis-rdb-advice">
      <Typography.Title level={5} style={{ marginBottom: 4 }}>
        诊断建议
      </Typography.Title>
      {items.map((a) => (
        <div key={a.code || a.title} className={`redis-rdb-advice-item ${a.level || ''}`}>
          <div className="title">
            <Tag color={levelColor(a.level)}>{a.level || 'info'}</Tag>
            {a.title}
          </div>
          {a.detail ? <div className="detail">{a.detail}</div> : null}
          {a.suggest ? <div className="suggest">建议：{a.suggest}</div> : null}
        </div>
      ))}
    </div>
  )
}
