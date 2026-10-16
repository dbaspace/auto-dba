import React, { useEffect, useState } from 'react'
import { Button, Input, Pagination, Space, Tag, message, Popconfirm } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './LoginLogs.css'

interface SessRow {
  user_id?: string
  username?: string
  tenant_id?: string
  jti?: string
  ip?: string
  user_agent?: string
  online_at?: string
  expire_at?: string
  kind?: string
  status?: string
}

const PAGE_SIZE = 10

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export default function LoginSessions() {
  const [rows, setRows] = useState<SessRow[]>([])
  const [total, setTotal] = useState(0)
  const [validCount, setValidCount] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [username, setUsername] = useState('')
  const [page, setPage] = useState(1)
  const [disabledMsg, setDisabledMsg] = useState('')

  async function loadStats() {
    try {
      const { data } = await api.get('/auth/sessions/stats', { silent: true })
      setValidCount(data?.data?.valid_user_count ?? null)
      setDisabledMsg('')
    } catch (e: any) {
      const msg = e?.response?.data?.message || ''
      if (e?.response?.status === 503) setDisabledMsg(msg)
    }
  }

  async function load(nextPage = page) {
    setLoading(true)
    try {
      const { data } = await api.get('/auth/sessions', {
        params: {
          username: username.trim() || undefined,
          page: nextPage,
          page_size: PAGE_SIZE,
        },
        silent: true,
      })
      setRows(data.data || [])
      setTotal(data.total || 0)
      setDisabledMsg('')
    } catch (e: any) {
      const msg = e?.response?.data?.message || '加载失败'
      if (e?.response?.status === 503) {
        setDisabledMsg(msg)
        setRows([])
        setTotal(0)
      } else {
        message.error(msg)
      }
    } finally {
      setLoading(false)
    }
  }

  async function kick(row: SessRow, all?: boolean) {
    try {
      await api.post('/auth/sessions/kick', {
        user_id: row.user_id,
        jti: all ? undefined : row.jti,
      })
      message.success(all ? '已踢下该用户全部会话' : '已踢下该会话')
      load(page)
      loadStats()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '操作失败')
    }
  }

  useEffect(() => {
    load(1)
    loadStats()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="ops-page">
      <p className="hint">
        展示当前<strong>会话有效 / 已登录</strong>用户（以 Redis 为准）。关闭浏览器不等于登出；仅登出、踢人或 TTL
        到期后会话结束。与「长时间未操作」闲置退出是两套规则，先到先失效。
      </p>
      {disabledMsg ? <p className="hint" style={{ color: '#cf1322' }}>{disabledMsg}</p> : null}
      <div className="toolbar">
        <Space wrap>
          {validCount != null ? <Tag color="blue">有效会话用户数：{validCount}</Tag> : null}
          <Input
            allowClear
            placeholder="用户名"
            style={{ width: 180 }}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            onPressEnter={() => {
              setPage(1)
              load(1)
            }}
          />
          <Button
            type="primary"
            onClick={() => {
              setPage(1)
              load(1)
              loadStats()
            }}
          >
            查询
          </Button>
        </Space>
      </div>
      <ResponsiveTable
        rowKey={(r) => `${r.user_id}-${r.jti}`}
        loading={loading}
        dataSource={rows}
        pagination={false}
        columns={[
          { title: '用户名', dataIndex: 'username', width: 120 },
          { title: '状态', dataIndex: 'status', width: 100, render: (v: string) => <Tag color="green">{v || '会话有效'}</Tag> },
          { title: '登录时间', dataIndex: 'online_at', width: 170, render: fmtTime },
          { title: '过期时间', dataIndex: 'expire_at', width: 170, render: fmtTime },
          { title: 'IP', dataIndex: 'ip', width: 130 },
          {
            title: 'UA',
            dataIndex: 'user_agent',
            ellipsis: true,
            render: (v: string) => v || '-',
          },
          {
            title: '操作',
            width: 200,
            render: (_: unknown, row: SessRow) => (
              <Space>
                <Popconfirm title="踢下该终端会话？" onConfirm={() => kick(row, false)}>
                  <Button size="small" danger>
                    踢会话
                  </Button>
                </Popconfirm>
                <Popconfirm title="踢下该用户全部会话？" onConfirm={() => kick(row, true)}>
                  <Button size="small" danger type="primary">
                    踢全部
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <div style={{ marginTop: 16, textAlign: 'right' }}>
        <Pagination
          current={page}
          pageSize={PAGE_SIZE}
          total={total}
          showTotal={(t) => `共 ${t} 用户`}
          onChange={(p) => {
            setPage(p)
            load(p)
          }}
        />
      </div>
    </div>
  )
}
