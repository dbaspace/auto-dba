import React, { useCallback, useEffect, useState } from 'react'
import { Button, Drawer, Input, Pagination, Radio, message } from 'antd'
import ResponsiveTable from '../../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../../api/request'
import type { SnapshotFilter } from './types'
import { copyText, formatDuration, formatTime, truncate } from './format'
import { asArray, asTotal } from './services/asArray'

export default function InsightAnalysisTab(props: {
  filter: SnapshotFilter
  active: boolean
  refreshTick: number
}) {
  const { filter, active, refreshTick } = props
  const [draftDb, setDraftDb] = useState('')
  const [draftUser, setDraftUser] = useState('')
  const [pDb, setPDb] = useState('')
  const [pUser, setPUser] = useState('')
  const [orderBy, setOrderBy] = useState('count')
  const [loading, setLoading] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [total, setTotal] = useState(0)
  const [current, setCurrent] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [statDrawer, setStatDrawer] = useState<any>(null)

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
      const { data } = await api.get('/pg-activity-snapshots/stats', {
        params: {
          instance_id: filter.instanceId,
          start_time: filter.startTime,
          end_time: filter.endTime,
          p_db: pDb || undefined,
          p_user: pUser || undefined,
          order_by: orderBy,
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
  }, [hasInstance, filter, pDb, pUser, orderBy, current, pageSize])

  useEffect(() => {
    if (!active) return
    void load()
  }, [active, load, refreshTick])

  const columns: ColumnsType<any> = [
    {
      title: '归一化 SQL',
      width: 260,
      render: (_, row) => (
        <Button type="link" onClick={() => setStatDrawer(row)}>
          {truncate(row.query_normalized, 80)}
        </Button>
      ),
    },
    { title: '次数', dataIndex: 'count', width: 80 },
    { title: '平均时长', width: 110, render: (_, row) => formatDuration(row.avg_xact_duration) },
    { title: '最大时长', width: 110, render: (_, row) => formatDuration(row.max_xact_duration) },
    { title: '库数', dataIndex: 'distinct_dbs', width: 70 },
    { title: '首次出现', width: 170, render: (_, row) => formatTime(row.first_seen) },
    { title: '最近出现', width: 170, render: (_, row) => formatTime(row.last_seen) },
  ]

  return (
    <>
      <div className="toolbar extra">
        <Input value={draftDb} allowClear placeholder="数据库" style={{ width: 160 }} onChange={(e) => setDraftDb(e.target.value)} />
        <Input value={draftUser} allowClear placeholder="用户" style={{ width: 140 }} onChange={(e) => setDraftUser(e.target.value)} />
        <Radio.Group size="small" value={orderBy} onChange={(e) => { setOrderBy(e.target.value); setCurrent(1) }}>
          <Radio.Button value="count">按次数</Radio.Button>
          <Radio.Button value="avg_duration">按平均时长</Radio.Button>
          <Radio.Button value="max_duration">按最大时长</Radio.Button>
          <Radio.Button value="last_seen">按最近出现</Radio.Button>
        </Radio.Group>
        <Button
          size="small"
          type="primary"
          onClick={() => {
            setPDb(draftDb)
            setPUser(draftUser)
            setCurrent(1)
          }}
        >
          查询
        </Button>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r, i) => `${r.query_normalized || ''}-${i}`}
          loading={loading}
          dataSource={rows}
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
      <Drawer open={!!statDrawer} title="归一化 SQL" width={520} onClose={() => setStatDrawer(null)}>
        <Input.TextArea value={statDrawer?.query_normalized || ''} rows={10} readOnly />
        <div className="drawer-meta">样例 SQL</div>
        <Input.TextArea value={statDrawer?.sample_info || ''} rows={10} readOnly />
        <div className="drawer-actions">
          <Button type="primary" onClick={() => void copyText(statDrawer?.query_normalized)}>
            复制模板
          </Button>
        </div>
      </Drawer>
    </>
  )
}
