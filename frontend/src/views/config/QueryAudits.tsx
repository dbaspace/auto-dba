import React, { useEffect, useState } from 'react'
import { Button, Input, Modal, Pagination, Select, Tag, Typography } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './LoginLogs.css'

interface AuditRow {
  id?: string
  created_at?: string
  username?: string
  display_name?: string
  kind?: string
  instance_name?: string
  db_type?: string
  target?: string
  statement?: string
  success?: boolean
  message?: string
  row_count?: number
  elapsed_ms?: number
  ip?: string
  client_type?: string
  token_id?: string
}

const PAGE_SIZE = 10
const STATEMENT_PREVIEW_LEN = 80

function isLongStatement(s?: string) {
  const v = (s || '').trim()
  if (!v) return false
  return v.length > STATEMENT_PREVIEW_LEN || /[\r\n]/.test(v)
}

function previewStatement(s?: string) {
  const v = (s || '').replace(/\s+/g, ' ').trim()
  if (v.length <= STATEMENT_PREVIEW_LEN) return v
  return `${v.slice(0, STATEMENT_PREVIEW_LEN)}…`
}

const KIND_LABEL: Record<string, string> = {
  db: '数据库',
  kafka: 'Kafka',
  redis: 'Redis',
}

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export default function QueryAudits() {
  const [rows, setRows] = useState<AuditRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [kind, setKind] = useState('')
  const [success, setSuccess] = useState('')
  const [clientType, setClientType] = useState('')
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [detail, setDetail] = useState<AuditRow | null>(null)

  async function load(nextPage = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/query-audits', {
        params: {
          username: username.trim() || undefined,
          kind: kind || undefined,
          success: success || undefined,
          client_type: clientType || undefined,
          q: q.trim() || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
      setDetail(null)
    } finally {
      setLoading(false)
    }
  }

  function reload() {
    setPage(1)
    load(1)
  }

  useEffect(() => {
    load(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="ops-page">
      <p className="hint">记录数据库查询、Kafka 拉消息、Redis 命令的执行结果。仅管理员可查看。</p>
      <div className="toolbar">
        <Input
          allowClear
          placeholder="用户名"
          style={{ width: 140 }}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          onPressEnter={reload}
        />
        <Select
          allowClear
          placeholder="类型"
          style={{ width: 120 }}
          value={kind || undefined}
          onChange={(v) => {
            setKind(v || '')
            setPage(1)
            load(1)
          }}
          options={[
            { value: 'db', label: '数据库' },
            { value: 'kafka', label: 'Kafka' },
            { value: 'redis', label: 'Redis' },
          ]}
        />
        <Select
          allowClear
          placeholder="结果"
          style={{ width: 120 }}
          value={success || undefined}
          onChange={(v) => {
            setSuccess(v || '')
            setPage(1)
            load(1)
          }}
          options={[
            { value: 'true', label: '成功' },
            { value: 'false', label: '失败' },
          ]}
        />
        <Select
          allowClear
          placeholder="客户端"
          style={{ width: 130 }}
          value={clientType || undefined}
          onChange={(v) => {
            setClientType(v || '')
            setPage(1)
            load(1)
          }}
          options={[
            { value: 'browser', label: 'browser' },
            { value: 'mcp', label: 'mcp' },
            { value: 'cli', label: 'cli' },
            { value: 'local_ai', label: 'local_ai' },
            { value: 'machine', label: 'machine' },
          ]}
        />
        <Input
          allowClear
          placeholder="语句关键字"
          style={{ width: 220 }}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onPressEnter={reload}
        />
        <Button type="primary" onClick={reload}>
          查询
        </Button>
      </div>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || `${r.created_at}-${r.username}-${r.statement}`}
          dataSource={rows}
          loading={loading}
          bordered
          pagination={false}
          scroll={{ y: '100%', x: 1280 }}
          style={{ height: '100%' }}
          columns={[
            { title: '时间', dataIndex: 'created_at', width: 180, render: (v) => fmtTime(v) },
            {
              title: '用户',
              width: 140,
              render: (_, row) => row.display_name || row.username || '-',
            },
            {
              title: '类型',
              dataIndex: 'kind',
              width: 90,
              render: (v: string) => KIND_LABEL[v] || v || '-',
            },
            {
              title: '客户端',
              dataIndex: 'client_type',
              width: 90,
              render: (v: string) => v || 'browser',
            },
            { title: '实例', dataIndex: 'instance_name', width: 140, ellipsis: true },
            { title: '目标', dataIndex: 'target', width: 140, ellipsis: true },
            {
              title: '语句',
              dataIndex: 'statement',
              ellipsis: true,
              render: (v: string, row) => {
                if (!v) return '-'
                if (!isLongStatement(v)) return v
                return (
                  <Typography.Link onClick={() => setDetail(row)}>{previewStatement(v)}</Typography.Link>
                )
              },
            },
            {
              title: '结果',
              width: 80,
              render: (_, row) => (
                <Tag color={row.success ? 'success' : 'error'}>{row.success ? '成功' : '失败'}</Tag>
              ),
            },
            { title: '行数', dataIndex: 'row_count', width: 70 },
            {
              title: '耗时',
              dataIndex: 'elapsed_ms',
              width: 80,
              render: (v: number) => (v != null ? `${v} ms` : '-'),
            },
            { title: 'IP', dataIndex: 'ip', width: 130 },
          ]}
        />
      </div>
      <div className="pager">
        <Pagination
          current={page}
          pageSize={PAGE_SIZE}
          total={total}
          showSizeChanger={false}
          showTotal={(t) => `共 ${t} 条`}
          onChange={(p) => {
            setPage(p)
            load(p)
          }}
        />
      </div>
      <Modal
        title="查询语句"
        open={!!detail}
        onCancel={() => setDetail(null)}
        footer={null}
        width={720}
      >
        {detail ? (
          <div>
            {detail.message ? <p className="hint">{detail.message}</p> : null}
            <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', margin: 0 }}>
              {detail.statement || '-'}
            </pre>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
