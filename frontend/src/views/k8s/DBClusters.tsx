import React, { useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Select, Space, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import api from '../../api/request'
import { createDBCluster, listDBClusters, loadPicker } from './services/k8sApi'
import './k8s.css'

export default function K8sDBClusters() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm()

  async function load() {
    setLoading(true)
    try {
      setRows(await listDBClusters(clusterId || undefined))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [clusterId])

  const columns: ColumnsType<any> = [
    { title: '名称', dataIndex: 'cr_name' },
    { title: '引擎', dataIndex: 'engine', width: 100 },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: '状态', dataIndex: 'status', width: 120 },
    { title: '端点', dataIndex: 'endpoint' },
    { title: '实例 ID', dataIndex: 'instance_id' },
    {
      title: '操作',
      width: 220,
      render: (_, row) => (
        <Space size="small">
          <Button size="small" onClick={() => onScale(row)}>扩缩</Button>
          <Button size="small" onClick={() => onSwitch(row)}>切换</Button>
          <Button size="small" danger onClick={() => onDelete(row)}>删除</Button>
        </Space>
      ),
    },
  ]

  async function onCreate() {
    const v = await form.validateFields()
    if (!clusterId) {
      message.warning('请选择集群')
      return
    }
    try {
      await createDBCluster(clusterId, {
        ...v,
        resources: { cpu: v.cpu, memory: v.memory },
      })
      message.success('已创建')
      setOpen(false)
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '创建失败')
    }
  }

  function onScale(row: any) {
    let replicas = 1
    Modal.confirm({
      title: `扩缩 ${row.cr_name}`,
      content: <InputNumber min={1} defaultValue={1} onChange={(v) => { replicas = Number(v || 1) }} />,
      onOk: async () => {
        await api.post(`/k8s/db-clusters/${row.id}/scale`, { replicas })
        message.success('已提交')
        load()
      },
    })
  }

  function onSwitch(row: any) {
    let target = ''
    Modal.confirm({
      title: `切换 ${row.cr_name}`,
      content: <Input placeholder="目标 Primary Pod 名" onChange={(e) => { target = e.target.value }} />,
      onOk: async () => {
        await api.post(`/k8s/db-clusters/${row.id}/switchover`, { target })
        message.success('已提交')
      },
    })
  }

  function onDelete(row: any) {
    Modal.confirm({
      title: `删除 ${row.cr_name}?`,
      content: '可选同时注销自动登记的实例',
      okText: '删除并注销实例',
      cancelText: '仅删除 CR',
      onOk: async () => {
        await api.delete(`/k8s/db-clusters/${row.id}`, { params: { unregister_instance: '1' } })
        message.success('已删除')
        load()
      },
      onCancel: async () => {
        await api.delete(`/k8s/db-clusters/${row.id}`)
        load()
      },
    })
  }

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>数据库服务</h3>
        <Space>
          <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
          <Button type="primary" onClick={() => { form.resetFields(); form.setFieldsValue({ engine: 'postgres', replicas: 1, version: '16' }); setOpen(true) }}>
            创建
          </Button>
        </Space>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} />
      <Modal title="创建数据库集群" open={open} onOk={onCreate} onCancel={() => setOpen(false)} width={640} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="engine" label="引擎" rules={[{ required: true }]}>
            <Select options={[
              { value: 'postgres', label: 'PostgreSQL (CNPG)' },
              { value: 'mysql', label: 'MySQL (Percona)' },
              { value: 'redis', label: 'Redis' },
              { value: 'starrocks', label: 'StarRocks' },
              { value: 'kubeblocks', label: 'KubeBlocks' },
            ]} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="namespace" label="命名空间" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="version" label="版本"><Input /></Form.Item>
          <Form.Item name="replicas" label="副本数"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="storage_class" label="StorageClass"><Input /></Form.Item>
          <Form.Item name="storage_size" label="存储大小"><Input placeholder="20Gi" /></Form.Item>
          <Form.Item name="cpu" label="CPU"><Input placeholder="500m" /></Form.Item>
          <Form.Item name="memory" label="内存"><Input placeholder="1Gi" /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
