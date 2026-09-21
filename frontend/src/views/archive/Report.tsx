import React, { useEffect, useMemo, useState } from 'react'
import { Button, Card, Col, Radio, Row, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import './archive.css'

export default function Report() {
  const [range, setRange] = useState('week')
  const [stats, setStats] = useState<any>({})
  const [tasks, setTasks] = useState<any[]>([])

  const kpis = useMemo(
    () => [
      { label: '总任务', value: stats.total_tasks ?? 0, bg: '#1677ff', mark: '☰' },
      { label: '成功率', value: stats.success_rate ?? '-', bg: '#52c41a', mark: '✓' },
      { label: '失败', value: stats.failed_today ?? 0, bg: '#f5222d', mark: '!' },
      { label: '执行中', value: stats.running ?? 0, bg: '#fa8c16', mark: '▶' },
    ],
    [stats],
  )

  async function load() {
    const data = (await api.get('/archive/stats')).data.data || {}
    setStats(data)
    setTasks((await api.get('/archive/tasks')).data.data || [])
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<any> = [
    { title: '任务', dataIndex: 'name' },
    { title: '源库', dataIndex: 'source_db', width: 140 },
    { title: '表', dataIndex: 'table_name', width: 140 },
    { title: '状态', dataIndex: 'status', width: 100 },
    { title: '时间', dataIndex: 'created_at', width: 180 },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="toolbar mb">
          <Radio.Group
            optionType="button"
            value={range}
            onChange={(e) => setRange(e.target.value)}
          >
            <Radio.Button value="today">今日</Radio.Button>
            <Radio.Button value="week">本周</Radio.Button>
            <Radio.Button value="month">本月</Radio.Button>
          </Radio.Group>
          <Button onClick={load}>刷新</Button>
        </div>
        <Row gutter={16} className="mb">
          {kpis.map((k) => (
            <Col key={k.label} xs={24} sm={12} lg={6}>
              <div className="kpi">
                <div className="kpi-ico" style={{ background: k.bg }}>
                  {k.mark}
                </div>
                <div>
                  <div className="kpi-num">{k.value}</div>
                  <div className="kpi-label">{k.label}</div>
                </div>
              </div>
            </Col>
          ))}
        </Row>
        <Card bordered={false}>
          <div style={{ marginBottom: 12 }}>任务明细</div>
          <Table
            rowKey={(r) => r.id ?? r.name ?? Math.random()}
            dataSource={tasks}
            columns={columns}
            pagination={false}
            locale={{ emptyText: '暂无数据' }}
          />
        </Card>
      </div>
    </div>
  )
}
