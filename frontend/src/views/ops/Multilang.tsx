import React, { useCallback, useEffect, useState } from 'react'
import { Button, Table } from 'antd'
import api from '../../api/request'

export default function Multilang() {
  const [rows, setRows] = useState<any[]>([])

  const load = useCallback(async () => {
    const { data } = await api.get('/multilang-template/tasks')
    setRows(data.data || [])
  }, [])

  async function create() {
    await api.post('/multilang-template/tasks', {
      name: 'multilang-' + Date.now(),
      template: 'default',
      instance_id: '',
    })
    await load()
  }

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="ops-page">
      <div className="ops-title">
        <Button type="primary" onClick={create}>
          新建模版任务
        </Button>
      </div>
      <div className="ops-table">
        <Table
          rowKey={(r) => r.id || `${r.name}-${r.created_at}`}
          dataSource={rows}
          bordered
          pagination={false}
          scroll={{ y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: '名称', dataIndex: 'name', minWidth: 180 },
            { title: '状态', dataIndex: 'status', width: 110 },
            { title: '模版', dataIndex: 'template', minWidth: 140 },
            { title: '时间', dataIndex: 'created_at', width: 200 },
          ]}
        />
      </div>
    </div>
  )
}
