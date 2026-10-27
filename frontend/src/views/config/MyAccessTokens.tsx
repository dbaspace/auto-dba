import React, { useEffect, useState } from 'react'
import { Alert, Button, Input, InputNumber, Modal, Popconfirm, Select, Space, Tag, Typography, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './LoginLogs.css'

interface StatusData {
  enabled?: boolean
  enforce_machine_pat?: boolean
  has_grant?: boolean
  grant?: { scopes?: string; expires_at?: string; status?: string }
}

interface PatRow {
  id?: string
  name?: string
  token_prefix?: string
  scopes?: string
  client_type?: string
  expires_at?: string
  last_used_at?: string
  revoked_at?: string
  created_at?: string
}

function fmtTime(v?: string) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export default function MyAccessTokens() {
  const [status, setStatus] = useState<StatusData>({})
  const [rows, setRows] = useState<PatRow[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('mcp')
  const [clientType, setClientType] = useState('mcp')
  const [days, setDays] = useState<number | null>(90)
  const [createdToken, setCreatedToken] = useState('')

  async function load() {
    setLoading(true)
    try {
      const [st, list] = await Promise.all([
        api.get('/client-access/me'),
        api.get('/client-access/pats'),
      ])
      setStatus(st.data || {})
      setRows(list.data?.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  async function createPAT() {
    try {
      const { data } = await api.post('/client-access/pats', {
        name: name.trim() || 'default',
        client_type: clientType,
        days: days || 0,
      })
      setCreatedToken(data.token || '')
      setOpen(false)
      message.success('已创建，请立即复制保存令牌')
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '创建失败')
    }
  }

  async function revoke(id?: string) {
    if (!id) return
    try {
      await api.delete('/client-access/pats/' + id)
      message.success('已吊销')
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '吊销失败')
    }
  }

  return (
    <div className="ops-page">
      <p className="hint">
        个人访问令牌（PAT）供本地 AI 工具、CLI、MCP 调用平台查询接口。请求头使用{' '}
        <Typography.Text code>Authorization: Bearer &lt;token&gt;</Typography.Text>
        ，建议带 <Typography.Text code>X-Client-Type: mcp|cli|local_ai</Typography.Text>。
      </p>
      {!status.has_grant ? (
        <Alert type="warning" showIcon message="尚未开通客户端访问授权，请联系管理员在「客户端访问授权」中开通。" />
      ) : (
        <Alert
          type="success"
          showIcon
          message={`已授权（scopes=${status.grant?.scopes || 'query'}）`}
          description={status.grant?.expires_at ? `过期时间：${fmtTime(status.grant.expires_at)}` : '授权不过期'}
          style={{ marginBottom: 12 }}
        />
      )}
      <div className="toolbar">
        <Button type="primary" disabled={!status.has_grant} onClick={() => setOpen(true)}>
          签发令牌
        </Button>
        <Button onClick={load}>刷新</Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name', width: 120 },
          { title: '前缀', dataIndex: 'token_prefix', width: 160 },
          { title: '类型', dataIndex: 'client_type', width: 100 },
          { title: '范围', dataIndex: 'scopes', width: 100 },
          {
            title: '状态',
            width: 90,
            render: (_: any, r: PatRow) =>
              r.revoked_at ? <Tag>已吊销</Tag> : <Tag color="green">有效</Tag>,
          },
          { title: '过期', dataIndex: 'expires_at', width: 170, render: fmtTime },
          { title: '最近使用', dataIndex: 'last_used_at', width: 170, render: fmtTime },
          { title: '创建', dataIndex: 'created_at', width: 170, render: fmtTime },
          {
            title: '操作',
            width: 90,
            render: (_: any, r: PatRow) =>
              !r.revoked_at ? (
                <Popconfirm title="确认吊销该令牌？" onConfirm={() => revoke(r.id)}>
                  <Button type="link" danger size="small">
                    吊销
                  </Button>
                </Popconfirm>
              ) : (
                '-'
              ),
          },
        ]}
      />
      <Modal title="签发个人访问令牌" open={open} onCancel={() => setOpen(false)} onOk={createPAT} okText="签发">
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} />
          <Select
            style={{ width: '100%' }}
            value={clientType}
            onChange={setClientType}
            options={[
              { value: 'mcp', label: 'mcp' },
              { value: 'cli', label: 'cli' },
              { value: 'local_ai', label: 'local_ai' },
            ]}
          />
          <InputNumber style={{ width: '100%' }} min={0} placeholder="有效天数" value={days as any} onChange={(v) => setDays(v)} />
        </Space>
      </Modal>
      <Modal
        title="请保存令牌"
        open={!!createdToken}
        onCancel={() => setCreatedToken('')}
        onOk={() => {
          navigator.clipboard?.writeText(createdToken)
          message.success('已复制')
          setCreatedToken('')
        }}
        okText="复制并关闭"
      >
        <Alert type="warning" showIcon message="仅显示一次，关闭后无法再查看明文" style={{ marginBottom: 12 }} />
        <Typography.Paragraph copyable>{createdToken}</Typography.Paragraph>
      </Modal>
    </div>
  )
}
