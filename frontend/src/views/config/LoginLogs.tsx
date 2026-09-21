import React, { useEffect, useState } from 'react'
import { Button, Input, Pagination, Select, Space, Table, Tag } from 'antd'
import api from '../../api/request'
import './LoginLogs.css'

interface LogRow {
  logged_at?: string
  username?: string
  display_name?: string
  ip?: string
  success?: boolean
  message?: string
  user_agent?: string
}

const PAGE_SIZE = 10

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export default function LoginLogs() {
  const [rows, setRows] = useState<LogRow[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [success, setSuccess] = useState<string>('')
  const [page, setPage] = useState(1)

  async function load(nextPage = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/login-logs', {
        params: {
          username: username.trim() || undefined,
          success: success || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
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
      <p className="hint">记录每次登录尝试的时间、用户名、来源 IP 与结果。仅管理员可查看。</p>
      <div className="toolbar">
        <Input
          allowClear
          placeholder="用户名"
          style={{ width: 180 }}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          onPressEnter={reload}
        />
        <Select
          allowClear
          placeholder="结果"
          style={{ width: 140 }}
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
        <Button type="primary" onClick={reload}>
          查询
        </Button>
      </div>
      <div className="ops-table">
        <Table
          rowKey={(r) => `${r.logged_at}-${r.username}-${r.ip}`}
          dataSource={rows}
          loading={loading}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '登录时间', dataIndex: 'logged_at', width: 190, render: (v) => fmtTime(v) },
            { title: '用户名', dataIndex: 'username', width: 140 },
            { title: '显示名', dataIndex: 'display_name', width: 140 },
            { title: '登录 IP', dataIndex: 'ip', width: 160 },
            {
              title: '结果',
              width: 90,
              render: (_, row) => (
                <Tag color={row.success ? 'success' : 'error'}>{row.success ? '成功' : '失败'}</Tag>
              ),
            },
            { title: '说明', dataIndex: 'message', ellipsis: true, width: 160 },
            { title: 'User-Agent', dataIndex: 'user_agent', ellipsis: true, width: 240 },
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
    </div>
  )
}
