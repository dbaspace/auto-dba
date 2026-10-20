import React, { useState } from 'react'
import { Button, Space, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import { detectOperators, loadPicker } from './services/k8sApi'
import './k8s.css'

export default function K8sOperators() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)

  async function onDetect() {
    if (!clusterId) {
      message.warning('请先选择集群')
      return
    }
    setLoading(true)
    try {
      setRows(await detectOperators(clusterId))
      message.success('检测完成')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '检测失败')
    } finally {
      setLoading(false)
    }
  }

  const columns: ColumnsType<any> = [
    { title: '引擎', dataIndex: 'engine' },
    { title: 'Operator', dataIndex: 'name' },
    { title: '版本', dataIndex: 'version' },
    { title: '命名空间', dataIndex: 'namespace' },
    {
      title: '状态',
      dataIndex: 'installed',
      render: (v) => (v ? <Tag color="green">已安装</Tag> : <Tag>未安装</Tag>),
    },
  ]

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>Operator 中心</h3>
        <Space>
          <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
          <Button type="primary" onClick={onDetect} loading={loading}>检测</Button>
        </Space>
      </div>
      <p style={{ color: '#888' }}>默认仅检测客户已安装的 Operator；安装请使用官方 Helm/manifest。</p>
      <ResponsiveTable rowKey={(r) => r.engine + r.name} loading={loading} columns={columns} dataSource={rows} />
    </div>
  )
}
