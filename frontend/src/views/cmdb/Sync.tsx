import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Space, Switch, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

type Job = {
  id?: string
  name?: string
  adapter?: string
  cron?: string
  enabled?: boolean
  config_json?: Record<string, unknown>
}

type Run = {
  id: string
  job_id: string
  status: string
  candidates: number
  added: number
  skipped: number
  started_at: string
  finished_at?: string
  log_text?: string
}

const ADAPTERS = [
  { value: 'prometheus_labels', label: 'Prometheus / 监控登记' },
  { value: 'cloud_tencent', label: '腾讯云标注校验' },
  { value: 'http_cmdb', label: '通用 HTTP 企业 CMDB' },
]

export default function CmdbSync() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [visible, setVisible] = useState(false)
  const [editing, setEditing] = useState<Job | null>(null)
  const [form] = Form.useForm()
  const [candidates, setCandidates] = useState<any[]>([])

  async function load() {
    const [j, r] = await Promise.all([api.get('/cmdb/sync/jobs'), api.get('/cmdb/sync/runs', { params: { limit: 30 } })])
    setJobs(j.data.data || [])
    setRuns(r.data.data || [])
  }

  useEffect(() => {
    void load()
  }, [])

  function open(job?: Job) {
    setEditing(job || null)
    form.setFieldsValue({
      name: job?.name || '',
      adapter: job?.adapter || 'prometheus_labels',
      cron: job?.cron || '',
      enabled: job?.enabled ?? true,
      config_text: job?.config_json ? JSON.stringify(job.config_json, null, 2) : '{\n  \n}',
    })
    setVisible(true)
  }

  async function save() {
    const v = await form.validateFields()
    let config_json = {}
    try {
      config_json = JSON.parse(v.config_text || '{}')
    } catch {
      message.error('config JSON 无效')
      return
    }
    const payload = { name: v.name, adapter: v.adapter, cron: v.cron, enabled: v.enabled, config_json }
    if (editing?.id) await api.put('/cmdb/sync/jobs/' + editing.id, { ...payload, id: editing.id })
    else await api.post('/cmdb/sync/jobs', payload)
    message.success('已保存')
    setVisible(false)
    await load()
  }

  async function runJob(id: string) {
    const { data } = await api.post(`/cmdb/sync/jobs/${id}/run`)
    setCandidates(data.data?.candidates || [])
    message.success(`完成：候选 ${(data.data?.candidates || []).length} 条（默认不自动建实例）`)
    await load()
  }

  return (
    <div className="ops-page">
      <Space style={{ marginBottom: 12 }}>
        <Button type="primary" onClick={() => open()}>
          新建同步任务
        </Button>
        <Button onClick={() => void load()}>刷新</Button>
      </Space>
      <ResponsiveTable
        rowKey="id"
        dataSource={jobs}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '适配器', dataIndex: 'adapter', width: 180 },
          { title: 'Cron', dataIndex: 'cron', width: 120 },
          { title: '启用', dataIndex: 'enabled', width: 80, render: (v: boolean) => (v ? '是' : '否') },
          {
            title: '操作',
            width: 220,
            render: (_: unknown, row: Job) => (
              <Space>
                <Button type="link" size="small" onClick={() => open(row)}>
                  编辑
                </Button>
                <Button type="link" size="small" onClick={() => void runJob(row.id!)}>
                  立即执行
                </Button>
                <Button
                  type="link"
                  danger
                  size="small"
                  onClick={() =>
                    Modal.confirm({
                      title: '删除任务？',
                      onOk: async () => {
                        await api.delete('/cmdb/sync/jobs/' + row.id)
                        await load()
                      },
                    })
                  }
                >
                  删除
                </Button>
              </Space>
            ),
          },
        ]}
      />

      <h3 style={{ marginTop: 24 }}>最近运行</h3>
      <ResponsiveTable
        rowKey="id"
        dataSource={runs}
        columns={[
          { title: '开始', dataIndex: 'started_at', width: 180 },
          { title: '状态', dataIndex: 'status', width: 100 },
          { title: '候选', dataIndex: 'candidates', width: 80 },
          { title: '未匹配', dataIndex: 'added', width: 80 },
          { title: '已匹配', dataIndex: 'skipped', width: 80 },
          { title: '结束', dataIndex: 'finished_at', width: 180 },
        ]}
      />

      {candidates.length > 0 && (
        <>
          <h3 style={{ marginTop: 24 }}>最近一次候选</h3>
          <pre style={{ maxHeight: 320, overflow: 'auto', fontSize: 12 }}>{JSON.stringify(candidates, null, 2)}</pre>
        </>
      )}

      <Modal open={visible} title={editing ? '编辑同步任务' : '新建同步任务'} onCancel={() => setVisible(false)} onOk={() => void save()} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="adapter" label="适配器" rules={[{ required: true }]}>
            <Select options={ADAPTERS} />
          </Form.Item>
          <Form.Item name="cron" label="Cron（可选，调度后续接入）">
            <Input placeholder="例如 0 */6 * * *" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="config_text" label="配置 JSON（http_cmdb 需 base_url 等）">
            <Input.TextArea rows={8} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
