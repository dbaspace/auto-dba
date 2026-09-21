import React, { useCallback, useEffect, useState } from 'react'
import { Button, Space, Table, message } from 'antd'
import api from '../../api/request'

export default function SlotList() {
  const [rows, setRows] = useState<any[]>([])

  const load = useCallback(async () => {
    const { data } = await api.get('/sync-slots')
    setRows(data.data || [])
  }, [])

  async function collect() {
    const { data } = await api.post('/sync-slots/collect')
    message.info(data.message || '已采集')
    await load()
  }

  async function remove(row: any) {
    await api.post('/sync-slots/delete', { id: row.id })
    await load()
  }

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="ops-page">
      <div className="ops-title">
        <div className="toolbar" style={{ margin: 0 }}>
          <Space>
            <Button type="primary" onClick={collect}>
              采集
            </Button>
            <Button onClick={load}>刷新</Button>
          </Space>
        </div>
      </div>
      <p className="hint">独立部署按本地 postgres 实例写入占位槽；连接真实库后可替换为 pg_replication_slots 采集。</p>
      <div className="ops-table">
        <Table
          rowKey={(r) => r.id || `${r.host}-${r.port}-${r.slot_name}`}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '主机', dataIndex: 'host', minWidth: 160 },
            { title: '端口', dataIndex: 'port', width: 80 },
            { title: '槽名', dataIndex: 'slot_name', minWidth: 160 },
            { title: '库', dataIndex: 'database', minWidth: 120 },
            {
              title: '活跃',
              dataIndex: 'active',
              width: 80,
              render: (v) => String(v),
            },
            {
              title: '操作',
              width: 90,
              render: (_, row) => (
                <Button type="link" danger onClick={() => remove(row)}>
                  删除
                </Button>
              ),
            },
          ]}
        />
      </div>
    </div>
  )
}
