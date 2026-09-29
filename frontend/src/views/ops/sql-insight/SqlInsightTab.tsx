import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Drawer, Input, Pagination, Radio, Tag, message } from 'antd'
import ResponsiveTable from '../../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../../api/request'
import type { SnapshotFilter } from './types'
import { copyText, durationClass, formatDuration, formatTime, stateColor } from './format'
import { asArray, asTotal } from './services/asArray'

export default function SqlInsightTab(props: {
  filter: SnapshotFilter
  showSql: boolean
  autoRefresh: boolean
  intervalSec: number
  refreshTick: number
  active: boolean
}) {
  const { filter, showSql, autoRefresh, intervalSec, refreshTick, active } = props
  const [slow, setSlow] = useState('all')
  const [loading, setLoading] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [total, setTotal] = useState(0)
  const [current, setCurrent] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [sqlDrawer, setSqlDrawer] = useState<any>(null)

  const hasInstance = Boolean(filter.instanceId)

  useEffect(() => {
    setCurrent(1)
  }, [filter.instanceId, filter.startTime, filter.endTime])

  const load = useCallback(async () => {
    if (!hasInstance) {
      setRows([])
      setTotal(0)
      return
    }
    setLoading(true)
    try {
      const { data } = await api.get('/pg-activity-snapshots', {
        params: {
          instance_id: filter.instanceId,
          start_time: filter.startTime,
          end_time: filter.endTime,
          current,
          page_size: pageSize,
        },
      })
      setRows(asArray(data.data))
      setTotal(asTotal(data, Number(data.total) || 0))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [hasInstance, filter, current, pageSize])

  useEffect(() => {
    if (!active) return
    void load()
  }, [active, load, refreshTick])

  useEffect(() => {
    if (!active || !autoRefresh) return
    const t = setInterval(() => void load(), intervalSec * 1000)
    return () => clearInterval(t)
  }, [active, autoRefresh, intervalSec, load])

  const displayRows = useMemo(() => {
    if (slow === 'all') return rows
    const th = Number(slow)
    return rows.filter((r) => (r.xact_duration ?? 0) > th)
  }, [slow, rows])

  const columns: ColumnsType<any> = [
    { title: '采集时间', dataIndex: 'collected_at', width: 170, render: (v) => formatTime(v) },
    {
      title: '实例',
      width: 180,
      render: (_, row) => (
        <div>
          <div>{row.instance_name || '-'}</div>
          <div className="sub">{row.host}:{row.port}</div>
        </div>
      ),
    },
    { title: 'PID', dataIndex: 'pid', width: 80 },
    { title: '用户', dataIndex: 'p_user', width: 110 },
    { title: '客户端', dataIndex: 'p_host', width: 130 },
    { title: '数据库', dataIndex: 'p_db', width: 120 },
    {
      title: '事务时长',
      width: 110,
      sorter: (a, b) => (a.xact_duration ?? 0) - (b.xact_duration ?? 0),
      render: (_, row) => <span className={durationClass(row.xact_duration)}>{formatDuration(row.xact_duration)}</span>,
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 150,
      render: (v) => <Tag color={stateColor(v)}>{v || '-'}</Tag>,
    },
    ...(showSql
      ? [
          {
            title: 'SQL',
            width: 240,
            ellipsis: true,
            render: (_: any, row: any) =>
              row.info ? (
                <Button type="link" onClick={() => setSqlDrawer(row)}>
                  {row.info}
                </Button>
              ) : (
                <span>-</span>
              ),
          },
        ]
      : []),
  ]

  return (
    <>
      <div className="toolbar extra">
        <span className="muted">慢事务</span>
        <Radio.Group size="small" value={slow} onChange={(e) => setSlow(e.target.value)}>
          <Radio.Button value="all">全部</Radio.Button>
          <Radio.Button value="1">&gt; 1s</Radio.Button>
          <Radio.Button value="10">&gt; 10s</Radio.Button>
        </Radio.Group>
        <Button size="small" loading={loading} onClick={() => void load()}>
          刷新
        </Button>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r, i) => `${r.pid}-${r.collected_at}-${i}`}
          loading={loading}
          dataSource={displayRows}
          columns={columns}
          bordered
          pagination={false}
          scroll={{ y: 'calc(100% - 8px)', x: 'max-content' }}
          locale={{ emptyText: hasInstance ? '暂无数据' : '请选择实例并查询' }}
        />
      </div>
      <div className="pager">
        <Pagination
          current={current}
          pageSize={pageSize}
          total={total}
          pageSizeOptions={[10, 20, 50, 100]}
          showSizeChanger
          showTotal={(t) => `共 ${t} 条`}
          onChange={(p, size) => {
            setCurrent(p)
            if (size !== pageSize) {
              setPageSize(size)
              setCurrent(1)
            }
          }}
        />
      </div>
      <Drawer open={!!sqlDrawer} title="SQL 详情" width={520} onClose={() => setSqlDrawer(null)}>
        {sqlDrawer ? (
          <div className="drawer-meta">
            <div>采集时间：{formatTime(sqlDrawer.collected_at)}</div>
            <div>
              PID：{sqlDrawer.pid} · {sqlDrawer.p_user} @ {sqlDrawer.p_db}
            </div>
            <div>事务时长：{formatDuration(sqlDrawer.xact_duration)}</div>
          </div>
        ) : null}
        <Input.TextArea value={sqlDrawer?.info || ''} rows={16} readOnly />
        <div className="drawer-actions">
          <Button type="primary" onClick={() => void copyText(sqlDrawer?.info)}>
            复制 SQL
          </Button>
        </div>
      </Drawer>
    </>
  )
}
