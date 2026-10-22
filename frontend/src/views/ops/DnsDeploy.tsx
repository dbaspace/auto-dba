import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Select, Space, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

interface Agent { id: string; name: string; base_url: string }
interface Cluster { id: string; name: string; status: string; topology: string; ha_options_json?: string }

/** 集群部署 DNS：登记主机并提交变更单，二进制与 Corefile 由变更单下发。 */
export default function DnsDeploy() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [form] = Form.useForm()

  async function load() {
    const [a, c] = await Promise.all([
      api.get('/db-deploy/agents'),
      api.get('/db-deploy/clusters', { params: { engine: 'dns' } }),
    ])
    setAgents(a.data.data || [])
    setClusters(c.data.data || [])
  }

  useEffect(() => { load().catch(() => {}) }, [])

  async function submit() {
    const v = await form.validateFields()
    const hosts: string[] = String(v.hosts || '').split('\n').map((s) => s.trim()).filter(Boolean)
    if (hosts.length === 0) {
      message.error('请填写主机')
      return
    }
    try {
      const { data } = await api.post('/db-deploy/clusters', {
        engine: 'dns',
        name: v.name,
        topology: hosts.length > 1 ? 'multi_node' : 'standalone',
        deploy_kind: 'self',
        ha_enabled: false,
        ha_options_json: JSON.stringify({
          listen: v.listen || '0.0.0.0:53',
          upstream: v.upstream,
          db_host: v.db_host,
          db_port: Number(v.db_port || 5432),
        }),
      })
      const id = data.data?.id
      for (const host of hosts) {
        const agent = agents.find((a) => a.name === v.agent_id || a.id === v.agent_id)
        await api.post(`/db-deploy/clusters/${id}/nodes`, {
          role: 'dns',
          host,
          agent_id: agent?.id || v.agent_id,
          status: 'pending',
        })
      }
      await api.post(`/db-deploy/clusters/${id}/deploy`)
      message.success('已提交部署变更单')
      form.resetFields()
      load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '提交失败')
    }
  }

  return (
    <div>
      <p>目标主机需已有 DB Agent，并能访问解析库。记录在 DNS管理 中维护，不随变更单下发。</p>
      <Form form={form} layout="vertical" style={{ maxWidth: 560 }} initialValues={{ listen: '0.0.0.0:53', db_port: 5432 }}>
        <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
        <Form.Item name="hosts" label="主机" rules={[{ required: true }]} extra="每行一台，填写节点回连心跳时使用的 IP 或名称">
          <Input.TextArea rows={4} />
        </Form.Item>
        <Form.Item name="agent_id" label="DB Agent" rules={[{ required: true }]}>
          <Select options={agents.map((a) => ({ value: a.id, label: `${a.name} ${a.base_url}` }))} />
        </Form.Item>
        <Form.Item name="listen" label="监听地址"><Input placeholder="0.0.0.0:53" /></Form.Item>
        <Form.Item name="upstream" label="上游 DNS" rules={[{ required: true }]}><Input placeholder="223.5.5.5:53" /></Form.Item>
        <Space>
          <Form.Item name="db_host" label="解析库地址" rules={[{ required: true }]}><Input placeholder="节点能访问的地址" /></Form.Item>
          <Form.Item name="db_port" label="端口"><Input /></Form.Item>
        </Space>
        <Button type="primary" onClick={submit}>提交部署</Button>
      </Form>
      <h3 style={{ marginTop: 24 }}>已登记</h3>
      <ResponsiveTable
        rowKey="id"
        dataSource={clusters}
        pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '拓扑', dataIndex: 'topology' },
          { title: '状态', dataIndex: 'status' },
          {
            title: '操作',
            render: (_: unknown, c: Cluster) => (
              <Button type="link" onClick={async () => {
                try {
                  await api.post(`/db-deploy/clusters/${c.id}/deploy`)
                  message.success('已再次提交变更单')
                  load()
                } catch (e: any) {
                  message.error(e?.response?.data?.message || '提交失败')
                }
              }}>升级 / 重装</Button>
            ),
          },
        ]}
      />
    </div>
  )
}
