import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Space, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import api from '../../api/request'
import { applyYAML, listResources, loadPicker } from './services/k8sApi'
import './k8s.css'

export default function K8sResources() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [apiResources, setApiResources] = useState<any[]>([])
  const [gvr, setGvr] = useState('')
  const [rows, setRows] = useState<any[]>([])
  const [yamlOpen, setYamlOpen] = useState(false)
  const [form] = Form.useForm()

  useEffect(() => {
    if (!clusterId) return
    api
      .get(`/k8s/clusters/${clusterId}/api-resources`)
      .then((r) => setApiResources(r.data.data || []))
      .catch(() => setApiResources([]))
  }, [clusterId])

  async function load() {
    if (!clusterId || !gvr) return
    try {
      const alias = gvr.includes('/') ? gvr.replace(/\//g, '~') : gvr
      const data = await listResources(clusterId, alias)
      setRows(data?.items || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    }
  }

  useEffect(() => {
    load()
  }, [clusterId, gvr])

  const columns: ColumnsType<any> = [
    { title: '名称', render: (_, r) => r?.metadata?.name },
    { title: '命名空间', render: (_, r) => r?.metadata?.namespace || '-' },
    { title: 'Kind', render: (_, r) => r?.kind },
  ]

  async function onApply(dryRun: boolean) {
    const v = await form.validateFields()
    try {
      const res = await applyYAML(clusterId, v.yaml, dryRun)
      if (dryRun) {
        Modal.info({
          title: 'Dry-run Diff',
          width: 720,
          content: <pre className="k8s-yaml">{res?.diff || '(no change)'}</pre>,
        })
      } else {
        message.success('已应用')
        setYamlOpen(false)
        load()
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || 'Apply 失败')
    }
  }

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>资源浏览器</h3>
        <Space wrap>
          <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
          <Select
            style={{ minWidth: 320 }}
            showSearch
            placeholder="选择 API 资源"
            value={gvr || undefined}
            options={apiResources.map((r) => ({
              value: r.gvr,
              label: `${r.kind} (${r.gvr})`,
            }))}
            onChange={setGvr}
          />
          <Button type="primary" onClick={() => { form.resetFields(); setYamlOpen(true) }}>YAML Apply</Button>
        </Space>
      </div>
      <ResponsiveTable rowKey={(r) => r?.metadata?.uid || r?.metadata?.name} columns={columns} dataSource={rows} />
      <Modal
        title="YAML Apply"
        open={yamlOpen}
        onCancel={() => setYamlOpen(false)}
        footer={[
          <Button key="dry" onClick={() => onApply(true)}>Dry-run</Button>,
          <Button key="ok" type="primary" onClick={() => onApply(false)}>Apply</Button>,
        ]}
        width={800}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="yaml" rules={[{ required: true }]}>
            <Input.TextArea rows={18} placeholder="粘贴 YAML" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
