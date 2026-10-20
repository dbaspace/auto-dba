import React, { useState } from 'react'
import { Alert, Button, Form, Input, InputNumber, Modal, Space, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import api from '../../api/request'
import { loadPicker } from './services/k8sApi'
import './k8s.css'

type HelmApp = {
  name: string
  namespace: string
  status?: string
  version?: string
  chart?: string
  revision?: string
}

export default function K8sApps() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [rows, setRows] = useState<HelmApp[]>([])
  const [loading, setLoading] = useState(false)
  const [hint, setHint] = useState('')
  const [upOpen, setUpOpen] = useState(false)
  const [rbOpen, setRbOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [upForm] = Form.useForm()
  const [rbForm] = Form.useForm()

  async function load() {
    if (!clusterId) {
      message.warning('请先选择集群')
      return
    }
    setLoading(true)
    try {
      const r = await api.get(`/k8s/clusters/${clusterId}/apps`)
      setRows(r.data?.data || [])
      setHint(r.data?.message || '')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
      setRows([])
    } finally {
      setLoading(false)
    }
  }

  async function doUpgrade(values: any) {
    setBusy(true)
    try {
      const sets = String(values.set || '')
        .split('\n')
        .map((s: string) => s.trim())
        .filter(Boolean)
      await api.post(`/k8s/clusters/${clusterId}/apps/upgrade`, {
        release: values.release,
        namespace: values.namespace,
        chart: values.chart,
        set: sets,
        wait: !!values.wait,
      })
      message.success('Helm upgrade 已提交')
      setUpOpen(false)
      void load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || 'upgrade 失败')
    } finally {
      setBusy(false)
    }
  }

  async function doRollback(values: any) {
    setBusy(true)
    try {
      await api.post(`/k8s/clusters/${clusterId}/apps/rollback`, {
        release: values.release,
        namespace: values.namespace,
        revision: Number(values.revision) || 0,
      })
      message.success('Helm rollback 已提交')
      setRbOpen(false)
      void load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || 'rollback 失败')
    } finally {
      setBusy(false)
    }
  }

  const columns: ColumnsType<HelmApp> = [
    { title: 'Release', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Chart', dataIndex: 'chart', render: (v) => v || '-' },
    { title: 'Revision', dataIndex: 'revision', render: (v, r) => v || r.version || '-' },
    {
      title: '状态',
      dataIndex: 'status',
      render: (v) => (v ? <Tag>{v}</Tag> : '-'),
    },
    {
      title: '操作',
      width: 160,
      render: (_, r) => (
        <Space size="small">
          <Button
            type="link"
            size="small"
            onClick={() => {
              rbForm.setFieldsValue({ release: r.name, namespace: r.namespace, revision: undefined })
              setRbOpen(true)
            }}
          >
            回滚
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>Helm 应用</h3>
        <Space>
          <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
          <Button
            onClick={() => {
              upForm.resetFields()
              setUpOpen(true)
            }}
            disabled={!clusterId}
          >
            Install / Upgrade
          </Button>
          <Button type="primary" onClick={load} loading={loading}>
            刷新
          </Button>
        </Space>
      </div>
      <Alert
        type="info"
        showIcon
        message="Helm Day2（本机 helm CLI）"
        description="列表仍从 owner=helm 的 Secret 汇总；upgrade/rollback 由平台主机执行 helm，需已安装 Helm 3 且集群凭据可写临时 kubeconfig。Agent 隧道场景可能不可用。"
        style={{ marginBottom: 12 }}
      />
      {hint && <p style={{ color: '#888', marginBottom: 12 }}>{hint}</p>}
      <ResponsiveTable rowKey={(r) => `${r.namespace}/${r.name}`} loading={loading} columns={columns} dataSource={rows} />

      <Modal
        title="Helm upgrade --install"
        open={upOpen}
        onCancel={() => setUpOpen(false)}
        onOk={() => upForm.submit()}
        confirmLoading={busy}
        destroyOnClose
      >
        <Form form={upForm} layout="vertical" onFinish={doUpgrade}>
          <Form.Item name="release" label="Release" rules={[{ required: true }]}>
            <Input placeholder="my-release" />
          </Form.Item>
          <Form.Item name="namespace" label="Namespace" rules={[{ required: true }]}>
            <Input placeholder="default" />
          </Form.Item>
          <Form.Item name="chart" label="Chart" rules={[{ required: true }]} extra="如 bitnami/nginx 或本地路径 /charts/app">
            <Input placeholder="repo/chart 或路径" />
          </Form.Item>
          <Form.Item name="set" label="--set（每行一条）">
            <Input.TextArea rows={3} placeholder="image.tag=1.2.3" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="Helm rollback"
        open={rbOpen}
        onCancel={() => setRbOpen(false)}
        onOk={() => rbForm.submit()}
        confirmLoading={busy}
        destroyOnClose
      >
        <Form form={rbForm} layout="vertical" onFinish={doRollback}>
          <Form.Item name="release" label="Release" rules={[{ required: true }]}>
            <Input disabled />
          </Form.Item>
          <Form.Item name="namespace" label="Namespace" rules={[{ required: true }]}>
            <Input disabled />
          </Form.Item>
          <Form.Item name="revision" label="Revision" extra="留空则回滚到上一版本">
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
