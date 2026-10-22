import React, { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Button, DatePicker, Input, Popconfirm, Select, Space, Tag, Tooltip, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import Auth from '../../components/Auth'
import api from '../../api/request'
import { Perm } from '../../hooks/usePermission'
import DnsChangeDiff, { DiffRow } from './DnsChangeDiff'
import { SOURCE_TEXT, errMsg, fmtTime, handleWriteError, handleWriteResult } from './dnsApi'

interface Batch {
  batch_id: string
  source: string
  actor: string
  ticket_id: string
  reason: string
  count: number
  summary: string
  created_time: number
  rolled_back_by?: string
}

interface ChangeLog {
  id: string
  seq: number
  action: string
  object_type: string
  object_id: string
  summary: string
  before?: any
  after?: any
}

function BatchDetail({ batchId }: { batchId: string }) {
  const [rows, setRows] = useState<DiffRow[] | null>(null)
  useEffect(() => {
    api.get(`/dns/changes/${batchId}`)
      .then(({ data }) => setRows((data.data || []).map((l: ChangeLog) => ({
        key: l.id, action: l.action, object: l.object_type, summary: l.summary, before: l.before, after: l.after,
      }))))
      .catch((e) => message.error(errMsg(e)))
  }, [batchId])
  if (!rows) return <span>加载中…</span>
  return <DnsChangeDiff rows={rows} />
}

export default function DnsChanges() {
  const [params] = useSearchParams()
  const [rows, setRows] = useState<Batch[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState<Record<string, any>>({ object_id: params.get('object_id') || '', actor: '', source: '', keyword: '', from: 0, to: 0 })

  async function load(p = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/dns/changes', { params: { ...filters, page: p, page_size: 20 } })
      setRows(data.data || [])
      setTotal(data.total || 0)
    } catch (e: any) {
      message.error(errMsg(e, '加载失败'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load(1) }, [])

  async function rollback(b: Batch) {
    try {
      const res = await api.post(`/dns/changes/${b.batch_id}/rollback`)
      handleWriteResult(res.data, '已回滚')
      load(page)
    } catch (e: any) {
      handleWriteError(e, '回滚失败')
    }
  }

  return (
    <div>
      <Space style={{ marginBottom: 12 }} wrap>
        <Input allowClear placeholder="对象 ID" style={{ width: 220 }} value={filters.object_id}
          onChange={(e) => setFilters({ ...filters, object_id: e.target.value })} />
        <Input allowClear placeholder="操作人" style={{ width: 140 }} onChange={(e) => setFilters({ ...filters, actor: e.target.value })} />
        <Select allowClear placeholder="来源" style={{ width: 130 }}
          options={Object.entries(SOURCE_TEXT).map(([value, label]) => ({ value, label }))}
          onChange={(v) => setFilters({ ...filters, source: v || '' })} />
        <Input allowClear placeholder="内容关键字" style={{ width: 180 }} onChange={(e) => setFilters({ ...filters, keyword: e.target.value })} />
        <DatePicker.RangePicker showTime onChange={(v) => setFilters({
          ...filters,
          from: v?.[0] ? Math.floor(v[0].valueOf() / 1000) : 0,
          to: v?.[1] ? Math.floor(v[1].valueOf() / 1000) : 0,
        })} />
        <Button type="primary" onClick={() => { setPage(1); load(1) }}>查询</Button>
      </Space>
      <ResponsiveTable
        rowKey="batch_id"
        loading={loading}
        dataSource={rows}
        expandable={{ expandedRowRender: (b: Batch) => <BatchDetail batchId={b.batch_id} /> }}
        pagination={{ current: page, pageSize: 20, total, onChange: (p: number) => { setPage(p); load(p) } }}
        columns={[
          { title: '时间', dataIndex: 'created_time', width: 170, render: (v: number) => fmtTime(v) },
          { title: '来源', dataIndex: 'source', width: 100, render: (v: string) => <Tag>{SOURCE_TEXT[v] || v}</Tag> },
          { title: '操作人', dataIndex: 'actor', width: 120 },
          {
            title: '内容', dataIndex: 'summary',
            render: (v: string, b: Batch) => <span>{v}{b.count > 1 ? <Tag style={{ marginLeft: 6 }}>共 {b.count} 项</Tag> : null}</span>,
          },
          { title: '原因', dataIndex: 'reason', ellipsis: true },
          { title: '工单', dataIndex: 'ticket_id', width: 130, render: (v: string) => v || '-' },
          {
            title: '操作', width: 120,
            render: (_: unknown, b: Batch) => b.rolled_back_by
              ? <Tooltip title={`已由批次 ${b.rolled_back_by} 回滚`}><Tag>已回滚</Tag></Tooltip>
              : (
                <Auth urn={Perm.dns.rollback}>
                  <Popconfirm
                    title="回滚该批次？"
                    description="按相反顺序撤销本批次全部变更；若相关记录之后又被修改，将拒绝回滚并列出冲突。"
                    onConfirm={() => rollback(b)}
                  >
                    <Button type="link" danger>回滚</Button>
                  </Popconfirm>
                </Auth>
              ),
          },
        ]}
      />
    </div>
  )
}
