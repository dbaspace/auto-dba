import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tag, message } from 'antd'
import { SyncOutlined } from '@ant-design/icons'
import api from '../../api/request'
import { activeDeployHostIds, isAgentDeployActive, useAgentDeployTracker } from '../ops/agentDeployTracker'
import { hostAddrs } from '../ops/node-precheck/nodePrecheck'
import '../backup/backup.css'

type HostInst = { id: string; name: string; host?: string; domain_name?: string; domains?: string[]; port?: number; env_id?: string }
type EnvRecord = { id: string; code?: string; name?: string }
type SrAgent = { id: string; name?: string; base_url: string; host_instance_id?: string }
type SrAgentDeploy = {
  id: string
  host_instance_id: string
  status: string
  listen_port: number
  base_url?: string
  log_text?: string
  created_at?: string
}
type MonitorPreview = { task_name?: string; endpoints?: string[] }

function envRecordLabel(e: EnvRecord): string {
  if (e.name && e.code && e.name !== e.code) return `${e.name} (${e.code})`
  return e.name || e.code || e.id || ''
}

function endpointHost(ep: string): string {
  const s = (ep || '').trim()
  if (!s) return ''
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end > 0) return s.slice(1, end)
  }
  const idx = s.lastIndexOf(':')
  if (idx > 0 && !s.includes('://')) {
    const maybePort = s.slice(idx + 1)
    if (/^\d+$/.test(maybePort)) return s.slice(0, idx)
  }
  return s
}

const statusColor: Record<string, string> = {
  pending: 'default', running: 'processing', success: 'success', failed: 'error',
}

/** Node Agent 登记与下发，接口与集群部署页原来的 Node Agent 页签相同。 */
export default function NodeAgentPanel() {
  const [agents, setAgents] = useState<SrAgent[]>([])
  const [deploys, setDeploys] = useState<SrAgentDeploy[]>([])
  const [hosts, setHosts] = useState<HostInst[]>([])
  const [envs, setEnvs] = useState<EnvRecord[]>([])
  const [agentStatus, setAgentStatus] = useState<Record<string, { status: string; message?: string }>>({})
  const [agentEnvId, setAgentEnvId] = useState('')
  const [agentAddr, setAgentAddr] = useState('')
  const [agentPage, setAgentPage] = useState(1)
  const [agentPageSize, setAgentPageSize] = useState(10)
  const [actingId, setActingId] = useState('')
  const [deployOpen, setDeployOpen] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [deploySource, setDeploySource] = useState<'monitor' | 'host'>('monitor')
  const [fePreview, setFePreview] = useState<MonitorPreview[]>([])
  const [bePreview, setBePreview] = useState<MonitorPreview[]>([])
  const [deployMonTask, setDeployMonTask] = useState<string>()
  const [deployMonRows, setDeployMonRows] = useState<{ host: string; hostId?: string; name?: string }[]>([])
  const [deployMonSelected, setDeployMonSelected] = useState<string[]>([])
  const [registerOpen, setRegisterOpen] = useState(false)
  const [deployLog, setDeployLog] = useState<SrAgentDeploy | null>(null)
  const [deployForm] = Form.useForm()
  const [agentForm] = Form.useForm()

  const loadHosts = useCallback(async () => {
    const res = await api.get('/instances', { params: { db_type: 'host' }, silent: true }).catch(() => ({ data: { data: [] } }))
    setHosts(res.data?.data || [])
  }, [])
  const loadEnvs = useCallback(async () => {
    const res = await api.get('/environments', { silent: true }).catch(() => ({ data: { data: [] } }))
    setEnvs(res.data?.data || [])
  }, [])
  const loadDeploys = useCallback(async () => {
    try {
      const res = await api.get('/sr-cluster/agents/deploys', { params: { limit: 200 }, silent: true })
      setDeploys(res.data?.data || [])
    } catch {
      message.error('下发记录加载失败')
    }
  }, [])
  const loadAgents = useCallback(async () => {
    let list: SrAgent[] = []
    try {
      const res = await api.get('/sr-cluster/agents', { silent: true })
      list = res.data?.data || []
      setAgents(list)
    } catch {
      message.error('Node Agent 加载失败')
      return
    }
    if (!list.length) {
      setAgentStatus({})
      return
    }
    const checking: Record<string, { status: string; message?: string }> = {}
    list.forEach((a) => { checking[a.id] = { status: 'checking' } })
    setAgentStatus((prev) => ({ ...prev, ...checking }))
    try {
      const stRes = await api.get('/sr-cluster/agents/status', { silent: true })
      const map: Record<string, { status: string; message?: string }> = {}
      ;(stRes.data?.data || []).forEach((s: { id: string; status: string; message?: string }) => {
        map[s.id] = { status: s.status, message: s.message }
      })
      setAgentStatus(map)
    } catch {
      const fail: Record<string, { status: string; message?: string }> = {}
      list.forEach((a) => { fail[a.id] = { status: 'unknown', message: '探活超时' } })
      setAgentStatus(fail)
    }
  }, [])

  const deployTracker = useAgentDeployTracker<SrAgentDeploy>({
    basePath: '/sr-cluster/agents/deploys',
    agentName: 'Node Agent',
    hostLabel: (id) => {
      const h = hosts.find((x) => x.id === id)
      return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
    },
    onShowLog: (row) => setDeployLog(row),
    onFinished: () => { void loadAgents(); void loadDeploys() },
    refreshList: () => { void loadDeploys() },
  })
  const { trackFromList } = deployTracker
  useEffect(() => { trackFromList(deploys) }, [deploys, trackFromList])

  useEffect(() => {
    void loadHosts()
    void loadEnvs()
    void loadAgents()
    void loadDeploys()
  }, [loadHosts, loadEnvs, loadAgents, loadDeploys])

  const deployingHostIds = useMemo(() => activeDeployHostIds(deploys), [deploys])
  const envLabelById = useMemo(() => {
    const m = new Map<string, string>()
    envs.forEach((e) => { if (e.id) m.set(e.id, envRecordLabel(e)) })
    return m
  }, [envs])
  const filteredAgents = useMemo(() => {
    const q = agentAddr.trim().toLowerCase()
    return agents.filter((a) => {
      const host = a.host_instance_id ? hosts.find((h) => h.id === a.host_instance_id) : undefined
      if (agentEnvId && host?.env_id !== agentEnvId) return false
      if (!q) return true
      const parts = [a.name, a.base_url, host?.name, host?.host, host?.domain_name, ...(host?.domains || [])]
      return parts.some((p) => String(p || '').toLowerCase().includes(q))
    })
  }, [agents, hosts, agentEnvId, agentAddr])
  const filteredDeploys = useMemo(() => {
    const q = agentAddr.trim().toLowerCase()
    if (!q) return deploys
    return deploys.filter((d) => {
      const host = hosts.find((h) => h.id === d.host_instance_id)
      const parts = [d.base_url, host?.name, host?.host, host?.domain_name, ...(host?.domains || [])]
      return parts.some((p) => String(p || '').toLowerCase().includes(q))
    })
  }, [deploys, hosts, agentAddr])

  const openDeploy = async () => {
    deployForm.resetFields()
    deployForm.setFieldsValue({
      listen_port: 8091,
      remote_dir: '/home/sysadmin/auto-dba/node-agent',
      escalate: false,
      host_instance_ids: [],
    })
    setDeploySource('monitor')
    setDeployMonTask(undefined)
    setDeployMonRows([])
    setDeployMonSelected([])
    await loadHosts()
    try {
      const [feRes, beRes] = await Promise.all([
        api.get('/monitor-targets/preview', { params: { kind: 'starrocks-fe' } }),
        api.get('/monitor-targets/preview', { params: { kind: 'starrocks-be' } }),
      ])
      setFePreview(feRes.data?.data || [])
      setBePreview(beRes.data?.data || [])
    } catch {
      setFePreview([])
      setBePreview([])
    }
    setDeployOpen(true)
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Alert
        type="info"
        showIcon
        message="StarRocks Node Agent 下发与登记"
        description={(
          <>
            SSH 下发 <code>deploy/node-agent</code> 到 host 实例（默认 <code>:8091</code>，<code>/v1/sr/*</code>）。
            成功后自动登记，供集群绑定。<strong>不是</strong>平台 Agent（:8090）。下发前请在控制面执行 <code>cd deploy/node-agent && ./build.sh</code>。
            下发仅安装或重启 Agent，不启停 FE/BE。
          </>
        )}
      />
      <Space wrap>
        <Select
          allowClear
          placeholder="环境"
          style={{ width: 180 }}
          value={agentEnvId || undefined}
          options={envs.map((e) => ({ value: e.id, label: envRecordLabel(e) }))}
          onChange={(v) => { setAgentEnvId(v || ''); setAgentPage(1) }}
        />
        <Input
          allowClear
          placeholder="地址或名称：主机、域名、URL、Agent 名"
          style={{ width: 240 }}
          value={agentAddr}
          onChange={(e) => { setAgentAddr(e.target.value); setAgentPage(1) }}
        />
        <Button onClick={() => { void loadAgents(); void loadDeploys() }}>刷新</Button>
        <Button type="primary" onClick={() => { void openDeploy() }}>下发</Button>
        <Button onClick={() => { agentForm.resetFields(); setRegisterOpen(true) }}>手工登记</Button>
      </Space>
      <Table
        rowKey="id"
        size="small"
        title={() => '已登记 Agent'}
        dataSource={filteredAgents}
        pagination={{
          current: agentPage,
          pageSize: agentPageSize,
          size: 'small',
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50],
          showTotal: (t) => `共 ${t} 个`,
          onChange: (page, pageSize) => { setAgentPage(page); setAgentPageSize(pageSize) },
        }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          {
            title: '环境',
            width: 140,
            render: (_: unknown, row: SrAgent) => {
              const host = row.host_instance_id ? hosts.find((h) => h.id === row.host_instance_id) : undefined
              if (!host?.env_id) return '—'
              return envLabelById.get(host.env_id) || '—'
            },
          },
          { title: 'URL', dataIndex: 'base_url' },
          {
            title: '状态',
            width: 100,
            render: (_: unknown, row: SrAgent) => {
              const st = agentStatus[row.id]?.status || 'unknown'
              const color = st === 'running' ? 'success' : st === 'stopped' ? 'default' : st === 'checking' ? 'processing' : 'warning'
              const label = st === 'running' ? '运行中' : st === 'stopped' ? '已停止' : st === 'checking' ? '检测中' : '未知'
              return <Tag color={color} title={agentStatus[row.id]?.message}>{label}</Tag>
            },
          },
          {
            title: '来源',
            width: 100,
            render: (_: unknown, row: SrAgent) => (row.host_instance_id ? <Tag color="blue">SSH 下发</Tag> : <Tag>手工</Tag>),
          },
          {
            title: '操作',
            width: 280,
            render: (_: unknown, row: SrAgent) => {
              const busy = actingId === row.id
              const deployed = !!row.host_instance_id
              const running = agentStatus[row.id]?.status === 'running'
              const applyStatus = (st: { id?: string; status?: string; message?: string } | undefined) => {
                if (st?.id) setAgentStatus((m) => ({ ...m, [st.id!]: { status: st.status || 'unknown', message: st.message } }))
              }
              return (
                <Space wrap size={0}>
                  <Button
                    type="link"
                    size="small"
                    loading={busy}
                    onClick={async () => {
                      setActingId(row.id)
                      try {
                        const res = await api.post(`/sr-cluster/agents/${row.id}/probe`)
                        applyStatus(res.data?.data)
                        if (res.data?.data?.status === 'running') message.success('运行中')
                        else message.warning(res.data?.data?.message || '未运行')
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || e.message)
                      } finally {
                        setActingId('')
                      }
                    }}
                  >
                    刷新状态
                  </Button>
                  {deployed ? (
                    running ? (
                      <Button
                        type="link"
                        size="small"
                        danger
                        loading={busy}
                        onClick={async () => {
                          setActingId(row.id)
                          try {
                            const res = await api.post(`/sr-cluster/agents/${row.id}/stop`)
                            message.success('已停止')
                            applyStatus(res.data?.data)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || e.message)
                          } finally {
                            setActingId('')
                          }
                        }}
                      >
                        停止
                      </Button>
                    ) : (
                      <Button
                        type="link"
                        size="small"
                        loading={busy}
                        onClick={async () => {
                          setActingId(row.id)
                          try {
                            const res = await api.post(`/sr-cluster/agents/${row.id}/start`)
                            message.success('已启动')
                            applyStatus(res.data?.data)
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || e.message)
                          } finally {
                            setActingId('')
                          }
                        }}
                      >
                        启动
                      </Button>
                    )
                  ) : null}
                  <Button
                    type="link"
                    danger
                    size="small"
                    onClick={() => {
                      Modal.confirm({
                        title: '删除 Agent',
                        content: `确认删除「${row.name || row.base_url}」登记？删除后，该主机上的集群节点将无法通过 Agent 运维；在该主机重新下发 Agent 后会自动恢复绑定。`,
                        okType: 'danger',
                        onOk: async () => {
                          const res = await api.delete(`/sr-cluster/agents/${row.id}`)
                          const affected: { name: string; detail?: string }[] = res.data?.affected_nodes || []
                          if (affected.length) {
                            message.warning(
                              `已删除。以下节点需重新下发 Agent 后才能运维：${affected.map((n) => (n.detail ? `${n.name}（${n.detail}）` : n.name)).join('、')}`,
                              6,
                            )
                          } else {
                            message.success('已删除')
                          }
                          void loadAgents()
                        },
                      })
                    }}
                  >
                    删除
                  </Button>
                </Space>
              )
            },
          },
        ]}
      />
      <Table
        rowKey="id"
        size="small"
        title={() => '下发记录'}
        dataSource={filteredDeploys}
        pagination={{ pageSize: 10, size: 'small', showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        columns={[
          {
            title: '主机',
            dataIndex: 'host_instance_id',
            render: (id: string) => {
              const h = hosts.find((x) => x.id === id)
              return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
            },
          },
          {
            title: '状态',
            dataIndex: 'status',
            width: 110,
            render: (s: string, row: SrAgentDeploy) => (
              <Tag color={statusColor[s] || 'default'} icon={isAgentDeployActive(row) ? <SyncOutlined spin /> : undefined}>{s}</Tag>
            ),
          },
          { title: '端口', dataIndex: 'listen_port', width: 70 },
          { title: 'Base URL', dataIndex: 'base_url', ellipsis: true },
          { title: '时间', dataIndex: 'created_at', width: 180 },
          {
            title: '操作',
            width: 80,
            render: (_: unknown, row: SrAgentDeploy) => (
              <Button type="link" size="small" onClick={() => setDeployLog(row)}>日志</Button>
            ),
          },
        ]}
      />

      <Modal
        title="SSH 下发 Node Agent"
        open={deployOpen}
        confirmLoading={deployBusy}
        width={720}
        onCancel={() => setDeployOpen(false)}
        onOk={async () => {
          const v = await deployForm.validateFields()
          let ids: string[] = []
          if (deploySource === 'monitor') {
            ids = deployMonSelected.filter(Boolean)
            if (!ids.length) {
              message.warning('请勾选已匹配到主机实例的节点')
              return
            }
          } else {
            ids = (v.host_instance_ids || []).filter(Boolean)
            if (!ids.length) {
              message.warning('请选择至少一台目标主机')
              return
            }
          }
          setDeployBusy(true)
          try {
            const res = await api.post('/sr-cluster/agents/deploy-batch', {
              host_instance_ids: ids,
              listen_port: v.listen_port || 8091,
              remote_dir: v.remote_dir,
              escalate: v.escalate ? 'sudo_su' : '',
            })
            const d = res.data?.data || {}
            const results: { host_instance_id: string; deploy_id?: string; error?: string }[] = d.results || []
            results.forEach((r) => { if (r.deploy_id) deployTracker.track(r.deploy_id) })
            message.success(`已提交：成功 ${d.ok_count || 0}，失败 ${d.fail_count || 0}（进度见下发记录）`)
            if (d.fail_count > 0) {
              Modal.warning({
                title: '部分主机下发失败',
                content: (
                  <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
                    {results.filter((r) => r.error).map((r) => (
                      <li key={r.host_instance_id}>{r.host_instance_id}：{r.error}</li>
                    ))}
                  </ul>
                ),
              })
            }
            setDeployOpen(false)
            void loadDeploys()
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message)
          } finally {
            setDeployBusy(false)
          }
        }}
        destroyOnClose
        okText="开始下发"
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="下发前请确认控制面已构建制品；对已运行集群安全"
          description={(
            <>
              <div><code>cd deploy/node-agent && ./build.sh</code></div>
              <div style={{ marginTop: 6 }}>仅安装/重启 Node Agent，不启停 FE/BE、不装包、不 init 盘。监听端口请用 8091，禁止填业务端口。</div>
            </>
          )}
        />
        <Form form={deployForm} layout="vertical" initialValues={{ listen_port: 8091, remote_dir: '/home/sysadmin/auto-dba/node-agent', escalate: false }}>
          <Form.Item label="目标来源">
            <Select
              value={deploySource}
              onChange={(v) => {
                setDeploySource(v)
                setDeployMonSelected([])
                deployForm.setFieldsValue({ host_instance_ids: [] })
              }}
              options={[
                { value: 'monitor', label: '从监控目标（StarRocks FE/BE）批量' },
                { value: 'host', label: '从已登记主机多选' },
              ]}
            />
          </Form.Item>
          {deploySource === 'monitor' ? (
            <>
              <Form.Item label="监控任务" required>
                <Select
                  showSearch
                  allowClear
                  placeholder="选择 StarRocks 监控任务"
                  value={deployMonTask}
                  options={Array.from(new Set([
                    ...fePreview.map((p) => p.task_name),
                    ...bePreview.map((p) => p.task_name),
                  ].filter(Boolean) as string[])).map((t) => ({ value: t, label: t }))}
                  onChange={(task) => {
                    setDeployMonTask(task)
                    const eps = new Set<string>()
                    for (const p of [...fePreview, ...bePreview]) {
                      if (p.task_name !== task) continue
                      for (const e of p.endpoints || []) {
                        const h = endpointHost(e) || String(e || '').trim()
                        if (h) eps.add(h)
                      }
                    }
                    const rows = [...eps].sort().map((host) => {
                      const hn = host.toLowerCase()
                      const inst = hosts.find((x) => hostAddrs(x).includes(hn))
                      return { host, hostId: inst && !deployingHostIds.has(inst.id) ? inst.id : undefined, name: inst?.name }
                    })
                    setDeployMonRows(rows)
                    setDeployMonSelected(rows.filter((r) => r.hostId).map((r) => r.hostId!))
                  }}
                />
              </Form.Item>
              <Form.Item label="节点主机" extra="仅「已匹配主机实例」可下发；未登记请先到实例管理登记 host 并配置 SSH">
                {deployMonRows.length === 0 ? <span>请先选择监控任务</span> : (
                  <>
                    <Space style={{ marginBottom: 8 }}>
                      <Button size="small" onClick={() => setDeployMonSelected(deployMonRows.filter((r) => r.hostId).map((r) => r.hostId!))}>全选</Button>
                      <Button
                        size="small"
                        onClick={() => {
                          const matched = deployMonRows.filter((r) => r.hostId).map((r) => r.hostId!)
                          const cur = new Set(deployMonSelected)
                          setDeployMonSelected(matched.filter((id) => !cur.has(id)))
                        }}
                      >
                        反选
                      </Button>
                    </Space>
                    <Checkbox.Group style={{ width: '100%' }} value={deployMonSelected} onChange={(vals) => setDeployMonSelected(vals as string[])}>
                      <Space direction="vertical" style={{ width: '100%' }}>
                        {deployMonRows.map((r) => (
                          <Checkbox key={r.host} value={r.hostId || `__miss__${r.host}`} disabled={!r.hostId}>
                            {r.host}
                            {r.hostId
                              ? <Tag color="blue" style={{ marginLeft: 8 }}>{r.name || r.hostId}</Tag>
                              : <Tag color="error" style={{ marginLeft: 8 }}>未登记主机实例</Tag>}
                          </Checkbox>
                        ))}
                      </Space>
                    </Checkbox.Group>
                  </>
                )}
              </Form.Item>
            </>
          ) : (
            <Form.Item
              name="host_instance_ids"
              label="目标主机"
              rules={[{ required: true, type: 'array', min: 1, message: '请选择至少一台主机' }]}
              extra="须在实例管理登记类型为 host；按主机环境选用 Nacos SSH 密钥档案，未配置则用全局密钥。"
            >
              <Select
                mode="multiple"
                showSearch
                optionFilterProp="label"
                placeholder={hosts.length ? '选择一台或多台主机' : '暂无 host 实例'}
                options={hosts.map((h) => ({
                  value: h.id,
                  disabled: deployingHostIds.has(h.id),
                  label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}${deployingHostIds.has(h.id) ? '（下发中）' : ''}`,
                }))}
              />
            </Form.Item>
          )}
          <Form.Item name="escalate" label="提权执行（sudo su -）" valuePropName="checked">
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item
            name="listen_port"
            label="监听端口"
            extra="默认 8091；勿与平台 Agent 8090 / 业务端口冲突"
            rules={[{ required: true, type: 'number', min: 1024, max: 65535, message: '端口须在 1024–65535' }]}
          >
            <InputNumber min={1024} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="remote_dir" label="远程目录">
            <Input placeholder="/home/sysadmin/auto-dba/node-agent" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title="登记 Node Agent" open={registerOpen} destroyOnClose onCancel={() => setRegisterOpen(false)} onOk={() => agentForm.submit()}>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="StarRocks Node Agent（默认 :8091）"
          description="须提供 /v1/sr/*。平台 Agent（:8090）不能登记到这里。"
        />
        <Form
          form={agentForm}
          layout="vertical"
          onFinish={async (v) => {
            await api.post('/sr-cluster/agents', v)
            message.success('已登记')
            setRegisterOpen(false)
            void loadAgents()
          }}
        >
          <Form.Item name="name" label="名称"><Input /></Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]} extra="推荐 http://主机:8091">
            <Input placeholder="http://host:8091" />
          </Form.Item>
          <Form.Item name="username" label="用户"><Input /></Form.Item>
          <Form.Item name="password" label="密码"><Input.Password /></Form.Item>
        </Form>
      </Modal>

      <Modal title="下发日志" open={!!deployLog} onCancel={() => setDeployLog(null)} footer={<Button onClick={() => setDeployLog(null)}>关闭</Button>} width={720}>
        {deployLog && (
          <>
            <p>状态 <Tag color={statusColor[deployLog.status] || 'default'}>{deployLog.status}</Tag> {deployLog.base_url || ''}</p>
            <pre style={{ maxHeight: 420, overflow: 'auto', background: '#111', color: '#eee', padding: 12, fontSize: 12 }}>{deployLog.log_text || '（无日志）'}</pre>
          </>
        )}
      </Modal>
    </Space>
  )
}
