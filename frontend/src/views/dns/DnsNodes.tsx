import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Col, Row, Select, Space, Tag, Tooltip, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import ChartBox from '../dashboard/ChartBox'
import api from '../../api/request'
import { errMsg, fmtTime } from './dnsApi'

interface NodeRow {
  ip: string
  name: string
  updated_time: number
  data_updated_time: number
  change_time: number
  online: boolean
  synced: boolean
  deployed: boolean
  port: number
  metrics_port: number
  cluster_name: string
}

interface Series { label: string; points: { t: number; v: number }[] }

function chartOption(series: Series[], unit: string, scale = 1) {
  return {
    tooltip: { trigger: 'axis' },
    legend: { type: 'scroll', bottom: 0, data: series.map((s) => s.label || '-') },
    grid: { left: 50, right: 16, top: 16, bottom: 40 },
    xAxis: { type: 'time' },
    yAxis: { type: 'value', axisLabel: { formatter: `{value}${unit}` } },
    series: series.map((s) => ({
      name: s.label || '-',
      type: 'line',
      showSymbol: false,
      data: s.points.map((p) => [p.t * 1000, Number((p.v * scale).toFixed(3))]),
    })),
  }
}

export default function DnsNodes() {
  const [nodes, setNodes] = useState<NodeRow[]>([])
  const [loading, setLoading] = useState(false)
  const [minutes, setMinutes] = useState(60)
  const [metrics, setMetrics] = useState<any>(null)

  async function loadNodes() {
    setLoading(true)
    try {
      const { data } = await api.get('/dns/nodes')
      setNodes(data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function loadMetrics(m = minutes) {
    const { data } = await api.get('/dns/metrics', { params: { minutes: m } })
    setMetrics(data)
  }

  useEffect(() => {
    loadNodes().catch((e) => message.error(errMsg(e, '加载节点失败')))
    loadMetrics().catch((e) => message.error(errMsg(e, '加载指标失败')))
  }, [])

  const now = Math.floor(Date.now() / 1000)
  const charts = useMemo(() => {
    if (!metrics?.configured) return null
    return {
      qps: chartOption(metrics.qps || [], ''),
      p99: chartOption(metrics.p99 || [], 'ms', 1000),
      rcode: chartOption(metrics.rcode || [], ''),
      forward: chartOption(metrics.forward || [], ''),
    }
  }, [metrics])
  const metricErrors = Object.entries(metrics?.errors || {})

  return (
    <Space direction="vertical" style={{ width: '100%' }} size={16}>
      <Card title="解析节点" size="small" extra={<Button onClick={() => loadNodes().catch((e) => message.error(errMsg(e)))}>刷新</Button>}>
        <ResponsiveTable
          rowKey={(r: NodeRow) => r.ip + r.name}
          loading={loading}
          dataSource={nodes}
          pagination={false}
          columns={[
            { title: 'IP', dataIndex: 'ip' },
            { title: '名称', dataIndex: 'name' },
            { title: '集群', dataIndex: 'cluster_name', render: (v: string, r: NodeRow) => r.deployed ? v : <Tooltip title="不是通过集群部署安装的节点"><Tag>外部</Tag></Tooltip> },
            { title: '服务端口', dataIndex: 'port', render: (v: number) => v || '-' },
            {
              title: '心跳', dataIndex: 'online',
              render: (v: boolean, r: NodeRow) => !r.updated_time ? <Tag>未上报</Tag> : (
                <Tooltip title={`最近心跳 ${fmtTime(r.updated_time)}`}>
                  {v ? <Tag color="green">在线</Tag> : <Tag color="red">离线 {now - r.updated_time}s</Tag>}
                </Tooltip>
              ),
            },
            {
              title: '数据同步', dataIndex: 'synced',
              render: (v: boolean, r: NodeRow) => !r.updated_time ? '-' : (
                <Tooltip title={`节点加载 ${fmtTime(r.data_updated_time)}；平台最近变更 ${fmtTime(r.change_time)}`}>
                  {v ? <Tag color="green">已同步</Tag> : <Tag color="orange">落后 {Math.max(0, r.change_time - r.data_updated_time)}s</Tag>}
                </Tooltip>
              ),
            },
            { title: '最近加载', dataIndex: 'data_updated_time', render: (v: number) => fmtTime(v) },
            { title: '指标端口', dataIndex: 'metrics_port', render: (v: number) => v || '-' },
          ]}
        />
        <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 8 }}>
          心跳超过 60 秒未更新视为离线；节点数据加载时间早于平台最近一次变更且超过 120 秒视为未同步。两者均由「DNS 节点巡检」任务告警。
        </div>
      </Card>

      <Card
        title="运行指标"
        size="small"
        extra={
          <Space>
            <Select value={minutes} style={{ width: 120 }} onChange={(v) => { setMinutes(v); loadMetrics(v).catch((e) => message.error(errMsg(e))) }}
              options={[{ value: 60, label: '近 1 小时' }, { value: 360, label: '近 6 小时' }, { value: 1440, label: '近 24 小时' }, { value: 10080, label: '近 7 天' }]} />
            <Button onClick={() => loadMetrics().catch((e) => message.error(errMsg(e)))}>刷新</Button>
          </Space>
        }
      >
        {metrics && !metrics.configured && <Alert type="info" showIcon message={metrics.message || '未配置 Prometheus'} />}
        {metricErrors.length > 0 && (
          <Alert type="warning" showIcon style={{ marginBottom: 12 }}
            message={`部分指标查询失败（选择器 ${metrics?.selector}）`}
            description={metricErrors.map(([k, v]) => `${k}: ${v}`).join('；')} />
        )}
        {charts && (
          <Row gutter={[16, 16]}>
            <Col xs={24} lg={12}><div className="sec-title">QPS（按节点）</div><ChartBox option={charts.qps} /></Col>
            <Col xs={24} lg={12}><div className="sec-title">P99 延迟（按节点）</div><ChartBox option={charts.p99} /></Col>
            <Col xs={24} lg={12}><div className="sec-title">应答码（次/秒）</div><ChartBox option={charts.rcode} /></Col>
            <Col xs={24} lg={12}><div className="sec-title">上游转发（次/秒）</div><ChartBox option={charts.forward} /></Col>
          </Row>
        )}
      </Card>
    </Space>
  )
}
