import React, { useEffect, useMemo, useState } from 'react'
import { Button, Select, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

type InstanceRow = {
  id?: string
  name?: string
  host?: string
  domain_name?: string
  port?: number
  db_type?: string
  username?: string
  visibility?: string
}

function instanceHost(row: InstanceRow): string {
  return String(row.host || row.domain_name || '').trim()
}

function isHostType(dbType?: string) {
  const t = String(dbType || '').toLowerCase()
  return t === 'host' || t === 'os' || t === '主机'
}

export default function TerminalPage() {
  const [instances, setInstances] = useState<InstanceRow[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [loading, setLoading] = useState(false)
  const [opening, setOpening] = useState(false)

  const hosts = useMemo(
    () => instances.filter((i) => isHostType(i.db_type) && !!instanceHost(i)),
    [instances],
  )

  useEffect(() => {
    setLoading(true)
    api
      .get('/instances', { params: { db_type: 'host' } })
      .then((res) => setInstances(res.data?.data || []))
      .catch(() => message.error('加载主机实例失败'))
      .finally(() => setLoading(false))
  }, [])

  async function openTerminal(id: string) {
    if (!id) {
      message.warning('请先选择主机')
      return
    }
    setOpening(true)
    try {
      const res = await api.post(`/instances/${id}/open-terminal`)
      const url = res.data?.data?.terminal_url as string
      if (!url) {
        message.error(res.data?.message || '未返回终端地址')
        return
      }
      window.open(url, '_blank', 'noopener,noreferrer')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '打开终端失败')
    } finally {
      setOpening(false)
    }
  }

  return (
    <div className="ops-page" style={{ padding: 16, overflow: 'auto' }}>
      <h2 style={{ marginBottom: 12 }}>主机终端</h2>
      <p style={{ color: 'var(--app-text-secondary, #666)', marginBottom: 16 }}>
        数据来自「实例管理 → 主机」。需具备菜单权限，且该主机对你可见（全局或已授权）后才能打开 SSH 终端。
      </p>
      <div style={{ display: 'flex', gap: 12, marginBottom: 16, alignItems: 'center' }}>
        <Select
          showSearch
          allowClear
          placeholder="选择已授权主机"
          style={{ minWidth: 360 }}
          optionFilterProp="label"
          value={instanceId || undefined}
          onChange={(v) => setInstanceId(v || '')}
          options={hosts.map((i) => ({
            value: i.id as string,
            label: `${i.name || i.id} (${instanceHost(i)})`,
          }))}
        />
        <Button type="primary" loading={opening} onClick={() => openTerminal(instanceId)}>
          打开终端
        </Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={hosts}
        pagination={{ pageSize: 10 }}
        locale={{ emptyText: '暂无已授权主机，请先在实例管理登记「主机」并授权' }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          {
            title: 'Host',
            render: (_, row) => instanceHost(row),
          },
          { title: 'SSH 端口', dataIndex: 'port', width: 100 },
          { title: 'SSH 用户', dataIndex: 'username', width: 120, render: (v) => v || 'sysadmin' },
          {
            title: '操作',
            width: 120,
            render: (_, row) => (
              <Button type="link" loading={opening} onClick={() => openTerminal(row.id || '')}>
                终端
              </Button>
            ),
          },
        ]}
      />
    </div>
  )
}
