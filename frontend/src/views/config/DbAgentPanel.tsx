import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tag, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import api from '../../api/request'
import { activeDeployHostIds, isAgentDeployActive, useAgentDeployTracker } from '../ops/agentDeployTracker'

type HostInst = { id: string; name: string; host?: string; domain_name?: string; domains?: string[]; port?: number; env_id?: string }
type EnvRecord = { id: string; code?: string; name?: string }
type DbAgent = { id: string; name?: string; base_url?: string; host_instance_id?: string }
type DbDeploy = {
  id: string
  host_instance_id: string
  status: string
  listen_port?: number
  base_url?: string
  log_text?: string
  created_at?: string
}

const STATUS_COLOR: Record<string, string> = {
  pending: 'default', running: 'processing', success: 'success', failed: 'error',
}

/** DB Agent 登记与下发，接口与库部署页原来的 DB Agent 页签相同。 */
export default function DbAgentPanel() {
  const [agents, setAgents] = useState<DbAgent[]>([])
  const [deploys, setDeploys] = useState<DbDeploy[]>([])
  const [hosts, setHosts] = useState<HostInst[]>([])
  const [envs, setEnvs] = useState<EnvRecord[]>([])
  const [agentEnvId, setAgentEnvId] = useState('')
  const [agentAddr, setAgentAddr] = useState('')
  const [agentPage, setAgentPage] = useState(1)
  const [registerOpen, setRegisterOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [deployLog, setDeployLog] = useState<DbDeploy | null>(null)
  const [agentForm] = Form.useForm()
  const [deployForm] = Form.useForm()

  const load = useCallback(async () => {
    const [ag, dep] = await Promise.all([
      api.get('/db-deploy/agents', { silent: true }).catch(() => null),
      api.get('/db-deploy/agents/deploys', { silent: true }).catch(() => null),
    ])
    if (ag) setAgents(ag.data?.data || [])
    else message.error('DB Agent 加载失败')
    if (dep) setDeploys(dep.data?.data || [])
    else message.error('下发记录加载失败')
    const [hostRes, envRes] = await Promise.all([
      api.get('/instances', { params: { db_type: 'host' }, silent: true }).catch(() => ({ data: { data: [] } })),
      api.get('/environments', { silent: true }).catch(() => ({ data: { data: [] } })),
    ])
    setHosts(hostRes.data?.data || [])
    setEnvs(envRes.data?.data || [])
  }, [])

  const deployTracker = useAgentDeployTracker<DbDeploy>({
    basePath: '/db-deploy/agents/deploys',
    agentName: 'DB Agent',
    hostLabel: (id) => {
      const h = hosts.find((x) => x.id === id)
      return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
    },
    onShowLog: (row) => setDeployLog(row),
    onFinished: () => { void load() },
    refreshList: () => { void load() },
  })
  const { trackFromList } = deployTracker
  useEffect(() => { trackFromList(deploys) }, [deploys, trackFromList])

  useEffect(() => { void load() }, [load])

  const deployingHostIds = useMemo(() => activeDeployHostIds(deploys), [deploys])
  const filteredAgents = useMemo(() => {
    const q = agentAddr.trim().toLowerCase()
    return agents.filter((row) => {
      const host = row.host_instance_id ? hosts.find((h) => h.id === row.host_instance_id) : undefined
      if (agentEnvId && host?.env_id !== agentEnvId) return false
      if (!q) return true
      const parts = [row.name, row.base_url, host?.host, host?.domain_name, ...(host?.domains || [])]
      return parts.some((p) => String(p || '').toLowerCase().includes(q))
    })
  }, [agents, hosts, agentEnvId, agentAddr])
  const hostLabel = (id: string) => {
    const h = hosts.find((x) => x.id === id)
    return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Alert
        type="info"
        showIcon
        message="DB Agent 下发与登记（MySQL / PostgreSQL / Redis 自建节点）"
        description={(
          <>
            SSH 下发 <code>deploy/db-agent</code> 到 host 实例，默认 <code>:8092</code>（<code>/v1/db/*</code>）。
            不是 Node Agent（:8091），也不是平台 Agent（:8090）。下发前请在控制面执行 <code>cd deploy/db-agent && ./build.sh</code>。
            下发仅安装或重启 Agent，不启停业务库。
          </>
        )}
      />
      <Space wrap>
        <Button
          type="primary"
          onClick={() => {
            deployForm.resetFields()
            deployForm.setFieldsValue({ listen_port: 8092, remote_dir: '/home/sysadmin/auto-dba/db-agent', escalate: false })
            setDeployOpen(true)
          }}
        >
          下发
        </Button>
        <Button onClick={() => { agentForm.resetFields(); setRegisterOpen(true) }}>手工登记</Button>
        <Button onClick={() => { void load() }}>刷新</Button>
        <Select
          allowClear
          placeholder="环境"
          style={{ width: 180 }}
          value={agentEnvId || undefined}
          options={envs.map((e) => ({
            value: e.id,
            label: e.name && e.code && e.name !== e.code ? `${e.name} (${e.code})` : (e.name || e.code || e.id),
          }))}
          onChange={(v) => { setAgentEnvId(v || ''); setAgentPage(1) }}
        />
        <Input
          allowClear
          placeholder="地址：主机、域名或 URL"
          style={{ width: 240 }}
          value={agentAddr}
          onChange={(e) => { setAgentAddr(e.target.value); setAgentPage(1) }}
        />
      </Space>
      <Table
        rowKey="id"
        size="small"
        title={() => '已登记 DB Agent'}
        dataSource={filteredAgents}
        pagination={{
          current: agentPage,
          pageSize: 10,
          size: 'small',
          onChange: (p) => setAgentPage(p),
        }}
        columns={[
          { title: '名称', dataIndex: 'name', width: 160 },
          { title: 'Base URL', dataIndex: 'base_url' },
          {
            title: '来源',
            width: 100,
            render: (_: unknown, row: DbAgent) => (row.host_instance_id ? <Tag color="blue">SSH 下发</Tag> : <Tag>手工</Tag>),
          },
          { title: 'ID', dataIndex: 'id', width: 240, ellipsis: true },
          {
            title: '操作',
            width: 80,
            render: (_: unknown, row: DbAgent) => (
              <Button
                size="small"
                danger
                onClick={() => {
                  Modal.confirm({
                    title: '删除 Agent',
                    content: `确认删除「${row.name || row.base_url || row.id}」登记？删除后，该主机上的集群节点将无法通过 Agent 运维；在该主机重新下发 Agent 后会自动恢复绑定。`,
                    okButtonProps: { danger: true },
                    onOk: async () => {
                      const res = await api.delete(`/db-deploy/agents/${row.id}`)
                      const affected: { name: string; detail?: string }[] = res.data?.affected_nodes || []
                      if (affected.length) {
                        message.warning(
                          `已删除。以下节点需重新下发 Agent 后才能运维：${affected.map((n) => (n.detail ? `${n.name}（${n.detail}）` : n.name)).join('、')}`,
                          6,
                        )
                      } else {
                        message.success('已删除')
                      }
                      void load()
                    },
                  })
                }}
              >
                删除
              </Button>
            ),
          },
        ]}
      />
      <Table
        rowKey="id"
        size="small"
        title={() => '下发记录'}
        dataSource={deploys}
        pagination={{ pageSize: 8 }}
        columns={[
          { title: '主机', dataIndex: 'host_instance_id', ellipsis: true, render: (id: string) => hostLabel(id) },
          {
            title: '状态',
            dataIndex: 'status',
            width: 110,
            render: (s: string, row: DbDeploy) => (
              <Tag color={STATUS_COLOR[s]} icon={isAgentDeployActive(row) ? <SyncOutlined spin /> : undefined}>{s}</Tag>
            ),
          },
          { title: '端口', dataIndex: 'listen_port', width: 70 },
          { title: 'URL', dataIndex: 'base_url', ellipsis: true },
          { title: '时间', dataIndex: 'created_at', width: 180 },
          {
            title: '日志',
            width: 80,
            render: (_: unknown, row: DbDeploy) => (
              <Button type="link" size="small" onClick={() => setDeployLog(row)}>查看</Button>
            ),
          },
        ]}
      />

      <Modal title="手工登记 DB Agent" open={registerOpen} onCancel={() => setRegisterOpen(false)} onOk={() => agentForm.submit()} destroyOnClose>
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message="Base URL 须指向 DB Agent，默认端口 :8092（/v1/db/*），勿填 Node Agent :8091。" />
        <Form
          form={agentForm}
          layout="vertical"
          onFinish={async (v) => {
            await api.post('/db-deploy/agents', v)
            message.success('已登记')
            setRegisterOpen(false)
            void load()
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]}><Input placeholder="http://10.x.x.x:8092" /></Form.Item>
          <Form.Item name="username" label="Basic 用户"><Input /></Form.Item>
          <Form.Item name="password" label="Basic 密码"><Input.Password /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title="SSH 下发 DB Agent"
        open={deployOpen}
        confirmLoading={deployBusy}
        onCancel={() => setDeployOpen(false)}
        destroyOnClose
        okText="开始下发"
        onOk={async () => {
          const v = await deployForm.validateFields()
          setDeployBusy(true)
          try {
            const res = await api.post('/db-deploy/agents/deploy', {
              host_instance_id: v.host_instance_id,
              listen_port: v.listen_port || 8092,
              remote_dir: v.remote_dir,
              escalate: v.escalate ? 'sudo_su' : '',
            })
            const deployId = res.data?.data?.deploy_id as string
            message.success('已提交下发，进度见下发记录，完成后会通知')
            setDeployOpen(false)
            if (deployId) deployTracker.track(deployId)
            void load()
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message)
          } finally {
            setDeployBusy(false)
          }
        }}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="下发前请确认控制面已构建制品；对已运行集群安全"
          description={(
            <>
              <div><code>cd deploy/db-agent && ./build.sh</code></div>
              <div style={{ marginTop: 6 }}>仅安装/重启 DB Agent，不启停业务库。监听端口请用 8092，禁止填业务端口。</div>
            </>
          )}
        />
        <Form form={deployForm} layout="vertical" initialValues={{ listen_port: 8092, remote_dir: '/home/sysadmin/auto-dba/db-agent', escalate: false }}>
          <Form.Item name="host_instance_id" label="目标主机" rules={[{ required: true, message: '请选择 host 类型实例' }]} extra="须在实例管理登记类型为 host；按主机环境选用 Nacos SSH 密钥档案，未配置则用全局密钥。">
            <Select
              showSearch
              optionFilterProp="label"
              placeholder={hosts.length ? '选择主机' : '暂无 host 实例'}
              options={hosts.map((h) => ({
                value: h.id,
                disabled: deployingHostIds.has(h.id),
                label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}${deployingHostIds.has(h.id) ? '（下发中）' : ''}`,
              }))}
            />
          </Form.Item>
          <Form.Item name="escalate" label="提权执行（sudo su -）" valuePropName="checked">
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item name="listen_port" label="监听端口" extra="默认 8092；勿与平台 8090、Node 8091 及业务端口冲突" rules={[{ required: true, type: 'number', min: 1024, max: 65535, message: '端口须在 1024–65535' }]}>
            <InputNumber min={1024} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="remote_dir" label="远程目录">
            <Input placeholder="/home/sysadmin/auto-dba/db-agent" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title="下发日志" open={!!deployLog} onCancel={() => setDeployLog(null)} footer={<Button onClick={() => setDeployLog(null)}>关闭</Button>} width={720}>
        {deployLog && (
          <>
            <p>状态 <Tag color={STATUS_COLOR[deployLog.status] || 'default'}>{deployLog.status}</Tag>{deployLog.base_url ? ` · ${deployLog.base_url}` : ''}</p>
            <pre style={{ maxHeight: 420, overflow: 'auto', background: '#111', color: '#eee', padding: 12, fontSize: 12 }}>{deployLog.log_text || '（无日志）'}</pre>
          </>
        )}
      </Modal>
    </Space>
  )
}
