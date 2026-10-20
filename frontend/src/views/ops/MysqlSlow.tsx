import React, { useEffect, useState } from 'react'
import { Button, InputNumber, Select, Space, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import './Sessions.css'

function isMySQL(i: any) {
  const t = String(i.db_type || '').toLowerCase()
  return t === 'mysql' || t === 'tidb' || t === 'oceanbase' || t === 'mariadb'
}

export default function MysqlSlow() {
  const [instances, setInstances] = useState<any[]>([])
  const [instanceId, setInstanceId] = useState('')
  const [limit, setLimit] = useState(50)
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    api.get('/instances').then(({ data }) => {
      setInstances((data.data || []).filter(isMySQL))
    }).catch((e) => message.error(e?.message || '加载实例失败'))
  }, [])

  async function load() {
    if (!instanceId) {
      setRows([])
      return
    }
    setLoading(true)
    try {
      const { data } = await api.get('/mysql-sessions/digests', {
        params: { instance_id: instanceId, limit },
      })
      setRows(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '查询失败')
      setRows([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId, limit])

  const columns: ColumnsType<any> = [
    { title: '库', dataIndex: 'schema', width: 100, ellipsis: true },
    { title: '调用次数', dataIndex: 'count_star', width: 90 },
    { title: '总耗时(s)', dataIndex: 'sum_timer_wait_sec', width: 100 },
    { title: '平均耗时(s)', dataIndex: 'avg_timer_wait_sec', width: 110 },
    { title: '扫描行', dataIndex: 'sum_rows_examined', width: 90 },
    { title: '返回行', dataIndex: 'sum_rows_sent', width: 90 },
    { title: '语句摘要', dataIndex: 'digest_text', ellipsis: true, width: 360 },
    { title: '最近出现', dataIndex: 'last_seen', width: 170, ellipsis: true },
  ]

  return (
    <div className="ops-page">
      <div className="ops-filters">
        <div className="filter-item">
          <span className="label">实例</span>
          <Select
            value={instanceId || undefined}
            showSearch
            allowClear
            placeholder="选择 MySQL 实例"
            style={{ flex: 1 }}
            optionFilterProp="label"
            onChange={(v) => setInstanceId(v || '')}
            options={instances.map((i) => ({
              value: i.id,
              label: `${i.name || i.host} (${i.db_type})`,
            }))}
          />
        </div>
        <Space>
          <span className="muted">Top</span>
          <InputNumber min={10} max={200} value={limit} onChange={(v) => setLimit(Number(v) || 50)} />
          <Button loading={loading} onClick={load}>刷新</Button>
        </Space>
      </div>
      <p className="muted" style={{ marginBottom: 12 }}>
        数据来自 performance_schema.events_statements_summary_by_digest（按总耗时排序）。需实例开启 performance_schema。
      </p>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.digest || r.digest_text}
          loading={loading}
          dataSource={rows}
          columns={columns}
          bordered
          pagination={{ pageSize: 20 }}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: '请选择实例后查看 Top SQL' }}
        />
      </div>
    </div>
  )
}
