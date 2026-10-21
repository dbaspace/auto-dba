import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Button, Drawer, Input, InputNumber, Modal, Pagination, Popconfirm, Progress, Select, Space, Tag, message } from 'antd'
import { EyeOutlined } from '@ant-design/icons'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './SlotList.css'

type SlotRow = {
  id: string
  host: string
  port: number
  database: string
  slot_name: string
  active: boolean
  active_pid?: number
  restart_lsn?: string
  confirmed_flush_lsn?: string
  lag_bytes?: number
  connector?: string
  connector_name?: string
  connect_base?: string
  task_name?: string
  topic?: string
}

type CollectJob = {
  status?: string
  percent?: number
  message?: string
  skipped?: number
  started?: boolean
}

type RestartJob = {
  status?: string
  percent?: number
  message?: string
  current?: string
  succeeded?: number
  failed?: number
  skipped?: number
  started?: boolean
  logs?: string[]
}

type OpLogRow = {
  id?: string
  kind?: string
  line?: string
  created_at?: string
}

function lagMB(bytes?: number) {
  const n = Number(bytes || 0) / 1024 / 1024
  if (!Number.isFinite(n) || n <= 0) return '0 MB'
  return `${n.toFixed(2)} MB`
}

function canRestart(row: SlotRow) {
  return !!(row.connect_base || row.connector) && !!(row.connector_name || row.task_name)
}

function SlotPeek({
  title,
  value,
  onView,
}: {
  title: string
  value?: string
  onView: (title: string, text: string) => void
}) {
  const wrapRef = useRef<HTMLSpanElement>(null)
  const textRef = useRef<HTMLSpanElement>(null)
  const [overflow, setOverflow] = useState(false)
  const text = String(value ?? '').trim()

  useLayoutEffect(() => {
    const wrap = wrapRef.current
    const el = textRef.current
    if (!wrap || !el) return
    const measure = () => {
      if (wrap.clientWidth <= 0) return
      setOverflow(el.scrollWidth > wrap.clientWidth + 1)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(wrap)
    return () => ro.disconnect()
  }, [text])

  if (!text) return <>—</>
  return (
    <span ref={wrapRef} className="slot-cell-text">
      <span ref={textRef} className="slot-cell-text-main">
        {text}
      </span>
      {overflow ? (
        <button
          type="button"
          className="slot-cell-view"
          title="查看数据"
          aria-label="查看数据"
          onClick={(e) => {
            e.stopPropagation()
            onView(title, text)
          }}
        >
          <EyeOutlined />
        </button>
      ) : null}
    </span>
  )
}

export default function SlotList() {
  const [rows, setRows] = useState<SlotRow[]>([])
  const [loading, setLoading] = useState(false)
  const [refreshedAt, setRefreshedAt] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [status, setStatus] = useState<string>()
  const [lag, setLag] = useState<number | null>(null)
  const [hostQ, setHostQ] = useState('')
  const [taskQ, setTaskQ] = useState('')
  const [applied, setApplied] = useState({ status: '', lag: null as number | null, host: '', task: '' })
  const [job, setJob] = useState<CollectJob | null>(null)
  const [restartJob, setRestartJob] = useState<RestartJob | null>(null)
  const [logOpen, setLogOpen] = useState(false)
  const [opLogs, setOpLogs] = useState<OpLogRow[]>([])
  const [opLogsLoading, setOpLogsLoading] = useState(false)
  const [opLogPage, setOpLogPage] = useState(1)
  const [opLogPageSize, setOpLogPageSize] = useState(20)
  const [opLogTotal, setOpLogTotal] = useState(0)
  const [cellView, setCellView] = useState<{ title: string; text: string } | null>(null)
  const [tablePage, setTablePage] = useState(1)
  const [tablePageSize, setTablePageSize] = useState(20)
  const opLogQuery = useRef({ page: 1, pageSize: 20 })
  const wasRunning = useRef(false)
  const wasRestartRunning = useRef(false)

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true)
    try {
      const { data } = await api.get('/sync-slots')
      setRows(data.data || [])
      setRefreshedAt(new Date().toLocaleString())
      if (!quiet) setSelected([])
    } finally {
      if (!quiet) setLoading(false)
    }
  }, [])

  /** 只拉采集进度，running 期间不打全量 /sync-slots */
  const pollCollect = useCallback(async () => {
    const { data } = await api.get('/sync-slots/collect')
    setJob(data)
    return data?.status === 'running'
  }, [])

  const pollRestart = useCallback(async () => {
    const { data } = await api.get('/sync-slots/restart')
    setRestartJob(data)
    return data?.status === 'running'
  }, [])

  const loadOpLogs = useCallback(async (page?: number, pageSize?: number) => {
    const p = page ?? opLogQuery.current.page
    const ps = pageSize ?? opLogQuery.current.pageSize
    opLogQuery.current = { page: p, pageSize: ps }
    setOpLogPage(p)
    setOpLogPageSize(ps)
    setOpLogsLoading(true)
    try {
      const { data } = await api.get('/sync-slots/logs', { params: { page: p, page_size: ps } })
      setOpLogs(data.data || [])
      setOpLogTotal(Number(data.total || 0))
    } catch {
      setOpLogs([])
      setOpLogTotal(0)
    } finally {
      setOpLogsLoading(false)
    }
  }, [])

  async function collect() {
    if (restartJob?.status === 'running') {
      message.warning('重启进行中，请等待完成后再采集')
      return
    }
    try {
      const { data } = await api.post('/sync-slots/collect')
      setJob(data)
      if (data?.started && data?.status === 'running') {
        message.info(data.message || '已开始采集')
        setLogOpen(true)
        void loadOpLogs(1)
      } else if (data?.status === 'running') {
        message.warning(data.message || '采集进行中')
        setLogOpen(true)
        void loadOpLogs(1)
      } else if (data?.message) {
        message.info(data.message)
        if (data?.status === 'done') await load(true)
      }
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message
      message.error(msg || '启动采集失败')
    }
  }

  async function remove(ids: string[]) {
    await api.post('/sync-slots/delete', { ids })
    message.success('已删除同步列表记录')
    await load()
  }

  function confirmRestart(ids: string[]) {
    if (job?.status === 'running') {
      message.warning('采集进行中，请等待完成后再重启')
      return
    }
    const records = rows.filter((r) => ids.includes(r.id))
    if (!records.length) {
      message.warning('请先勾选要重启的数据')
      return
    }
    const invalid = records.filter((r) => !canRestart(r))
    if (invalid.length) {
      message.warning(`有 ${invalid.length} 条缺少 Kafka 连接器或任务名，无法重启，请调整勾选`)
      return
    }
    const names = records.map((r) => r.task_name || r.connector_name || r.slot_name)
    Modal.confirm({
      title: `确认批量重启 ${records.length} 个同步任务？`,
      content: (
        <div>
          <div style={{ marginBottom: 8 }}>将写业务库 heartbeat 并重启 Connect task 0；不删除 PostgreSQL 复制槽。</div>
          <div style={{ maxHeight: 200, overflow: 'auto', fontSize: 12, color: 'rgba(0,0,0,0.65)' }}>
            {names.map((n, i) => (
              <div key={`${i}-${n}`}>{n}</div>
            ))}
          </div>
        </div>
      ),
      okText: '确认重启',
      cancelText: '取消',
      onOk: () => startRestart(records),
    })
  }

  async function startRestart(records: SlotRow[]) {
    const items = records.map((r) => ({
      id: r.id,
      host: r.host,
      port: r.port,
      slot_name: r.slot_name,
      database: r.database,
    }))
    try {
      const { data } = await api.post('/sync-slots/restart', { items })
      setRestartJob(data)
      if (data?.started && data?.status === 'running') {
        message.info(data.message || '已开始批量重启')
        setLogOpen(true)
        void loadOpLogs(1)
      } else if (data?.status === 'running') {
        message.warning(data.message || '重启进行中')
        setLogOpen(true)
        void loadOpLogs(1)
      } else if (data?.message) {
        message.info(data.message)
      }
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message
      message.error(msg || '启动重启失败')
    }
  }

  useEffect(() => {
    if (job?.status === 'running') wasRunning.current = true
    if (wasRunning.current && job?.status === 'done') {
      wasRunning.current = false
      message.info(job.message || '采集完成')
      setSelected([])
      void load(true)
      if (logOpen) void loadOpLogs()
    }
  }, [job, load, logOpen, loadOpLogs])

  useEffect(() => {
    if (restartJob?.status === 'running') wasRestartRunning.current = true
    if (wasRestartRunning.current && restartJob?.status === 'done') {
      wasRestartRunning.current = false
      const failed = Number(restartJob.failed || 0)
      const ok = Number(restartJob.succeeded || 0)
      if (failed === 0 && ok > 0) message.success(restartJob.message || '重启完成')
      else if (ok === 0 && failed > 0) message.error(restartJob.message || '重启全部失败')
      else message.warning(restartJob.message || '重启已结束')
      void load(true)
      void loadOpLogs()
    }
  }, [restartJob, load, loadOpLogs])

  useEffect(() => {
    void load()
    pollCollect().catch(() => {})
    pollRestart().catch(() => {})
  }, [load, pollCollect, pollRestart])

  useEffect(() => {
    if (job?.status !== 'running') return
    let timer = 0
    let stop = false
    async function tick() {
      if (stop) return
      try {
        const running = await pollCollect()
        if (logOpen) void loadOpLogs()
        if (!stop && running) timer = window.setTimeout(tick, 1500)
      } catch {
        if (!stop) timer = window.setTimeout(tick, 3000)
      }
    }
    timer = window.setTimeout(tick, 800)
    return () => {
      stop = true
      window.clearTimeout(timer)
    }
  }, [job?.status, pollCollect, logOpen, loadOpLogs])

  useEffect(() => {
    if (restartJob?.status !== 'running') return
    let timer = 0
    let stop = false
    async function tick() {
      if (stop) return
      try {
        const running = await pollRestart()
        if (logOpen) void loadOpLogs()
        if (!stop && running) timer = window.setTimeout(tick, 1200)
      } catch {
        if (!stop) timer = window.setTimeout(tick, 2500)
      }
    }
    timer = window.setTimeout(tick, 600)
    return () => {
      stop = true
      window.clearTimeout(timer)
    }
  }, [restartJob?.status, pollRestart, logOpen, loadOpLogs])

  const shown = useMemo(() => {
    const host = applied.host.trim().toLowerCase()
    const task = applied.task.trim().toLowerCase()
    return rows.filter((row) => {
      if (applied.status === 'active' && !row.active) return false
      if (applied.status === 'inactive' && row.active) return false
      if (applied.lag != null && applied.lag > 0) {
        const mb = Number(row.lag_bytes || 0) / 1024 / 1024
        if (mb < applied.lag) return false
      }
      if (host && !`${row.host || ''}`.toLowerCase().includes(host)) return false
      if (task && !`${row.task_name || ''}`.toLowerCase().includes(task)) return false
      return true
    })
  }, [rows, applied])

  const collectRunning = job?.status === 'running'
  const openCell = useCallback((title: string, text: string) => setCellView({ title, text }), [])
  const restartRunning = restartJob?.status === 'running'
  // line 已含「HH:MM:SS [采集|重启] …」，勿再叠 kind 前缀
  const opLogText = useMemo(
    () =>
      opLogs
        .map((row) => {
          const ts = row.created_at ? `${row.created_at} ` : ''
          return `${ts}${row.line || ''}`
        })
        .join('\n'),
    [opLogs],
  )
  const liveRestartLogText = useMemo(() => (restartJob?.logs || []).join('\n'), [restartJob?.logs])
  const showRestartInLog = !!(restartJob && restartJob.status && restartJob.status !== 'idle')
  const showCollectInLog = !!(job && job.status && job.status !== 'idle')

  return (
    <div className="ops-page slot-list">
      <div className="ops-filters" style={{ width: '100%', justifyContent: 'flex-start' }}>
        <div className="toolbar" style={{ margin: 0, width: '100%', justifyContent: 'flex-start', flexWrap: 'wrap', gap: 8 }}>
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 120 }}
            value={status}
            onChange={setStatus}
            options={[
              { value: 'active', label: '活动' },
              { value: 'inactive', label: '非活动' },
            ]}
          />
          <InputNumber
            min={0}
            placeholder="日志积压量(MB)"
            style={{ width: 160 }}
            value={lag}
            onChange={(v) => setLag(typeof v === 'number' ? v : null)}
          />
          <Input
            allowClear
            placeholder="数据库域名"
            style={{ width: 180 }}
            value={hostQ}
            onChange={(e) => setHostQ(e.target.value)}
            onPressEnter={() => setApplied({ status: status || '', lag, host: hostQ, task: taskQ })}
          />
          <Input
            allowClear
            placeholder="任务名"
            style={{ width: 160 }}
            value={taskQ}
            onChange={(e) => setTaskQ(e.target.value)}
            onPressEnter={() => setApplied({ status: status || '', lag, host: hostQ, task: taskQ })}
          />
          <Space>
            <Button type="primary" onClick={() => setApplied({ status: status || '', lag, host: hostQ, task: taskQ })}>
              搜索
            </Button>
            <Button
              onClick={() => {
                setStatus(undefined)
                setLag(null)
                setHostQ('')
                setTaskQ('')
                setApplied({ status: '', lag: null, host: '', task: '' })
              }}
            >
              重置
            </Button>
            <Button loading={collectRunning} disabled={restartRunning} onClick={collect}>
              手动采集
            </Button>
            <Button
              disabled={!selected.length || restartRunning || collectRunning}
              loading={restartRunning}
              onClick={() => confirmRestart(selected)}
            >
              批量重启
            </Button>
            <Button
              onClick={() => {
                setLogOpen(true)
                void loadOpLogs(1)
              }}
            >
              查看操作日志
            </Button>
            <Popconfirm title="只删除同步列表记录，不删除 PostgreSQL 复制槽" onConfirm={() => remove(selected)} disabled={!selected.length}>
              <Button danger disabled={!selected.length}>
                批量删除
              </Button>
            </Popconfirm>
          </Space>
          <span className="hint" style={{ marginLeft: 'auto' }}>
            刷新时间：{refreshedAt || '—'}
          </span>
        </div>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={shown}
          bordered
          rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys.map(String)) }}
          pagination={{
            current: tablePage,
            pageSize: tablePageSize,
            showSizeChanger: true,
            pageSizeOptions: ['20', '50', '100'],
            showTotal: (n) => `共 ${n} 条`,
            onChange: (page, size) => {
              setTablePage(page)
              setTablePageSize(size)
            },
          }}
          tableLayout="fixed"
          scroll={{ x: 1900, y: 'calc(100% - 47px)' }}
          style={{ height: '100%' }}
          columns={[
            { title: 'Kafka连接器', dataIndex: 'connector', width: 156, ellipsis: true, render: (v) => v || '—' },
            {
              title: '实例(主机:端口)',
              width: 210,
              onHeaderCell: () => ({ className: 'slot-host-col' }),
              onCell: () => ({ className: 'slot-peek-cell slot-host-col' }),
              render: (_, row: SlotRow) => (
                <SlotPeek title="实例(主机:端口)" value={row.host ? `${row.host}:${row.port}` : ''} onView={openCell} />
              ),
            },
            {
              title: '数据库',
              dataIndex: 'database',
              width: 140,
              onCell: () => ({ className: 'slot-peek-cell', style: { maxWidth: 140 } }),
              render: (v: string) => <SlotPeek title="数据库" value={v} onView={openCell} />,
            },
            {
              title: '任务名',
              dataIndex: 'task_name',
              width: 160,
              onCell: () => ({ className: 'slot-peek-cell', style: { maxWidth: 160 } }),
              render: (v: string) => <SlotPeek title="任务名" value={v} onView={openCell} />,
            },
            {
              title: '数据槽',
              dataIndex: 'slot_name',
              width: 180,
              onCell: () => ({ className: 'slot-peek-cell', style: { maxWidth: 180 } }),
              render: (v: string) => <SlotPeek title="数据槽" value={v} onView={openCell} />,
            },
            {
              title: 'Slot状态(活动)',
              dataIndex: 'active',
              width: 130,
              render: (v: boolean) => (v ? <Tag color="green">活动</Tag> : <Tag>非活动</Tag>),
            },
            { title: 'slot pid', dataIndex: 'active_pid', width: 100, render: (v) => (v ? v : '—') },
            { title: 'restart_lsn', dataIndex: 'restart_lsn', width: 140, ellipsis: true, render: (v) => v || '—' },
            { title: 'confirmed_flush_lsn', dataIndex: 'confirmed_flush_lsn', width: 170, ellipsis: true, render: (v) => v || '—' },
            { title: 'replication_lag(MB)', width: 160, render: (_, row: SlotRow) => lagMB(row.lag_bytes) },
            {
              title: 'topic',
              dataIndex: 'topic',
              width: 160,
              onCell: () => ({ className: 'slot-peek-cell', style: { maxWidth: 160 } }),
              render: (v: string) => <SlotPeek title="topic" value={v} onView={openCell} />,
            },
          ]}
        />
      </div>
      <Modal
        open={!!cellView}
        title={cellView ? `查看数据 · ${cellView.title}` : '查看数据'}
        footer={null}
        onCancel={() => setCellView(null)}
        destroyOnClose
      >
        <pre className="slot-cell-full">{cellView?.text || ''}</pre>
      </Modal>
      <Drawer
        title="操作日志（采集 / 重启）"
        open={logOpen}
        onClose={() => setLogOpen(false)}
        width={640}
        extra={
          <Space>
            <Button size="small" loading={opLogsLoading} onClick={() => void loadOpLogs()}>
              刷新
            </Button>
            <Button
              size="small"
              disabled={!opLogText && !liveRestartLogText}
              onClick={() => {
                const text = [liveRestartLogText, opLogText].filter(Boolean).join('\n\n')
                void navigator.clipboard.writeText(text).then(
                  () => message.success('已复制'),
                  () => message.error('复制失败'),
                )
              }}
            >
              复制
            </Button>
          </Space>
        }
      >
        {showCollectInLog ? (
          <div style={{ marginBottom: 16 }}>
            <div className="hint" style={{ marginBottom: 4 }}>
              采集进度
              {collectRunning ? '（进行中）' : ''}
            </div>
            <Progress
              percent={job?.percent || 0}
              size="small"
              status={collectRunning ? 'active' : job?.skipped ? 'exception' : 'success'}
            />
            <div className="hint" style={{ marginTop: 4 }}>
              {job?.message || (collectRunning ? '正在采集' : '')}
            </div>
          </div>
        ) : null}
        {showRestartInLog ? (
          <div style={{ marginBottom: 16 }}>
            <div className="hint" style={{ marginBottom: 4 }}>
              重启进度
              {restartRunning ? '（进行中）' : ''}
            </div>
            <Progress
              percent={restartJob?.percent || 0}
              size="small"
              status={
                restartRunning
                  ? 'active'
                  : Number(restartJob?.failed || 0) > 0
                    ? 'exception'
                    : 'success'
              }
            />
            <div className="hint" style={{ marginTop: 4 }}>
              {restartJob?.message ||
                (restartRunning ? `正在重启${restartJob?.current ? `：${restartJob.current}` : ''}` : '')}
            </div>
            {liveRestartLogText ? (
              <pre
                style={{
                  margin: '8px 0 0',
                  maxHeight: 220,
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  fontSize: 12,
                  lineHeight: 1.5,
                  background: 'rgba(0,0,0,0.02)',
                  padding: 8,
                }}
              >
                {liveRestartLogText}
              </pre>
            ) : null}
          </div>
        ) : null}
        <div className="hint" style={{ marginBottom: 4 }}>
          历史操作日志
        </div>
        <div className="slot-op-log-list">
          {opLogsLoading && !opLogs.length ? (
            <div className="slot-op-log-empty">加载中…</div>
          ) : opLogs.length ? (
            opLogs.map((row, i) => (
              <div key={row.id || `${opLogPage}-${i}`} className="slot-op-log-line">
                {row.created_at ? `${row.created_at} ` : ''}
                {row.line || ''}
              </div>
            ))
          ) : (
            <div className="slot-op-log-empty">暂无操作日志（采集与重启会写入此处并持久化）</div>
          )}
        </div>
        <Pagination
          className="slot-op-log-pager"
          size="small"
          current={opLogPage}
          pageSize={opLogPageSize}
          total={opLogTotal}
          showSizeChanger
          pageSizeOptions={['20', '50', '100']}
          showTotal={(n) => `共 ${n} 条`}
          onChange={(p, ps) => void loadOpLogs(p, ps)}
        />
      </Drawer>
    </div>
  )
}
