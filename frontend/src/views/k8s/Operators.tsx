import React, { useState } from 'react'
import { Button, Space, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import api from '../../api/request'
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

  async function onInstall(engine: string) {
    if (!clusterId) return
    try {
      await api.post(`/k8s/clusters/${clusterId}/operators/install`, { engine })
      message.success(`已提交安装 ${engine}（也可只检测客户已装）`)
      onDetect()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '安装失败')
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
    {
      title: '操作',
      width: 120,
      render: (_, r) =>
        r.installed ? null : (
          <Button size="small" type="link" onClick={() => onInstall(r.engine)}>
            平台安装
          </Button>
        ),
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
      <p style={{ color: '#888' }}>支持两种方式：① 检测客户已装好的 Operator；② 由平台代装（CNPG 有默认清单，其它引擎可传清单 URL）。</p>
      <ResponsiveTable rowKey={(r) => r.engine + r.name} loading={loading} columns={columns} dataSource={rows} />
    </div>
  )
}
