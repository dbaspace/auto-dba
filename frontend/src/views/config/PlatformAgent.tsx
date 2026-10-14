import React, { useEffect, useRef, useState } from 'react'
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Select, Space, Switch, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import '../backup/backup.css'

const DEPLOY_POLL_MS = 2000

const BACKUP_ENGINE_OPTS = [
  { label: 'PostgreSQL', value: 'postgres' },
  { label: 'MySQL', value: 'mysql' },
  { label: 'TiDB', value: 'tidb' },
  { label: 'OceanBase', value: 'oceanbase' },
]

function parseBackupEngines(engines?: string) {
  const allow = new Set(['postgres', 'mysql', 'tidb', 'oceanbase', 'pg', 'postgresql', 'ob'])
  return String(engines || '')
    .split(',')
    .map((s) => s.trim().toLowerCase())
    .filter((s) => allow.has(s))
    .map((s) => (s === 'pg' || s === 'postgresql' ? 'postgres' : s === 'ob' ? 'oceanbase' : s))
}

function parseEscalateTag(tags?: string) {
  for (const p of String(tags || '').split(',')) {
    const t = p.trim()
    if (t.startsWith('escalate:')) return t.slice('escalate:'.length).trim()
  }
  return ''
}

function deployStatusColor(s: string) {
  if (s === 'success') return 'success'
  if (s === 'failed') return 'error'
  if (s === 'running') return 'processing'
  return 'default'
}

function runtimeTag(status?: string) {
  if (status === 'running') return <Tag color="success">运行中</Tag>
  if (status === 'stopped') return <Tag color="default">已停止</Tag>
  if (status === 'checking') return <Tag color="processing">检测中</Tag>
  return <Tag>未知</Tag>
}

/** 配置中心 · 平台 Agent：定义归属后供备份 / RDB / 高可用等模块引用 */
export default function PlatformAgent() {
  const [agents, setAgents] = useState<any[]>([])
  const [hosts, setHosts] = useState<any[]>([])
  const [deploys, setDeploys] = useState<any[]>([])
  const [agentStatus, setAgentStatus] = useState<Record<string, string>>({})
  const [actingId, setActingId] = useState('')
  const [loading, setLoading] = useState(false)
  const [agOpen, setAgOpen] = useState(false)
  const [depOpen, setDepOpen] = useState(false)
  const [logOpen, setLogOpen] = useState(false)
  const [logText, setLogText] = useState('')
  const [preflightOK, setPreflightOK] = useState(false)
  const [preflightBusy, setPreflightBusy] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [preflightStep, setPreflightStep] = useState('')
  const [preflightReport, setPreflightReport] = useState<{
    ok?: boolean
    summary?: string
    missing?: any[]
  } | null>(null)
  const [agForm] = Form.useForm()
  const [depForm] = Form.useForm()
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const agGroups: string[] = Form.useWatch('enabled_groups', agForm) || []
  const depGroups: string[] = Form.useWatch('enabled_groups', depForm) || []
  const agNeedBackup = agGroups.includes('backup')
  const depNeedBackup = depGroups.includes('backup')

  function resetPreflight() {
    setPreflightOK(false)
    setPreflightReport(null)
    setPreflightStep('')
  }

  function closeDeployModal() {
    setDepOpen(false)
    resetPreflight()
    setPreflightBusy(false)
    setDeployBusy(false)
  }

  async function loadDeploys() {
    const { data } = await api.get('/platform/agents/deploys', { params: { limit: 50 } })
    const list = data.data || []
    setDeploys(list)
    return list as any[]
  }

  async function refreshAgentStatus(list?: any[]) {
    const rows = list || agents
    if (!rows.length) {
      setAgentStatus({})
      return
    }
    const checking: Record<string, string> = {}
    for (const r of rows) checking[r.id] = 'checking'
    setAgentStatus((prev) => ({ ...prev, ...checking }))
    try {
      const { data } = await api.get('/platform/agents/status')
      const map: Record<string, string> = {}
      for (const it of data.data || []) {
        map[it.id] = it.status || 'unknown'
      }
      setAgentStatus(map)
    } catch {
      const fail: Record<string, string> = {}
      for (const r of rows) fail[r.id] = 'unknown'
      setAgentStatus(fail)
    }
  }

  async function load() {
    setLoading(true)
    try {
      const [a, h] = await Promise.all([
        api.get('/platform/agents'),
        api.get('/instances', { params: { db_type: 'host' } }),
      ])
      const agentList = a.data.data || []
      setAgents(agentList)
      const all = h.data.data || []
      setHosts(all.filter((it: any) => {
        const t = String(it.db_type || '').toLowerCase()
        return t === 'host' || t === 'os' || t === '主机'
      }))
      await loadDeploys()
      await refreshAgentStatus(agentList)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  function stopPoll() {
    if (pollRef.current) {
      clearInterval(pollRef.current)
      pollRef.current = null
    }
  }

  function startPoll(deployId?: string) {
    stopPoll()
    pollRef.current = setInterval(async () => {
      try {
        const list = await loadDeploys()
        const busy = list.some((d) => d.status === 'pending' || d.status === 'running')
        if (deployId) {
          const cur = list.find((d) => d.id === deployId)
          if (cur?.status === 'success') {
            message.success('Agent 下发成功并已登记')
            await load()
            stopPoll()
            return
          }
          if (cur?.status === 'failed') {
            message.error('下发失败，请查看日志')
            stopPoll()
            return
          }
        } else if (!busy) {
          stopPoll()
        }
      } catch {
        /* ignore transient */
      }
    }, DEPLOY_POLL_MS)
  }

  useEffect(() => {
    const busy = deploys.some((d) => d.status === 'pending' || d.status === 'running')
    if (busy && !pollRef.current) startPoll()
  }, [deploys])

  async function saveAgent() {
    const v = await agForm.validateFields()
    const groups: string[] = v.enabled_groups || []
    const bakEngines: string[] = v.backup_engines || []
    if (groups.includes('backup') && !bakEngines.length) {
      message.error('勾选备份控制时请至少选择一种实例类型')
      return
    }
    await api.post('/platform/agents', {
      name: v.name,
      base_url: v.base_url,
      username: v.username || '',
      password: v.password || '',
      enabled_groups: groups,
      backup_engines: bakEngines,
      tags: v.tags || '',
      remark: v.remark || '',
    })
    message.success('已登记 Agent')
    setAgOpen(false)
    agForm.resetFields()
    await load()
  }

  async function runPreflight() {
    const v = await depForm.validateFields()
    const groups: string[] = v.enabled_groups || ['backup', 'rdb']
    const bakEngines: string[] = v.backup_engines || []
    if (groups.includes('backup') && !bakEngines.length) {
      message.error('勾选备份控制时请至少选择一种实例类型')
      return
    }
    const escalate = v.escalate ? 'sudo_su' : ''
    setPreflightBusy(true)
    setPreflightOK(false)
    setPreflightReport(null)
    const steps = ['连接目标主机']
    if (escalate) steps.push('提权探针（sudo su -）')
    steps.push('按归属检查工具依赖')
    setPreflightStep(steps.join(' → ') + ' …')
    try {
      const { data } = await api.post('/platform/agents/preflight', {
        host_instance_id: v.host_instance_id,
        remote_dir: v.remote_dir || '',
        workdir: v.workdir || '',
        enabled_groups: groups,
        backup_engines: bakEngines,
        escalate,
      })
      const ok = !!data?.data?.ok
      const summary = data?.data?.summary || ''
      const missing = data?.data?.missing || []
      setPreflightReport({ ok, summary, missing })
      setPreflightOK(ok)
      setPreflightStep(ok ? '预检完成' : '预检未通过')
      if (ok) message.success('预检通过，可点击下发')
      else message.warning('预检未通过，请按结果安装依赖后重试')
    } catch (e: any) {
      const msg = e?.response?.data?.message || '预检失败'
      setPreflightReport({ ok: false, summary: msg, missing: [] })
      setPreflightOK(false)
      setPreflightStep('预检失败')
      message.error(msg)
    } finally {
      setPreflightBusy(false)
    }
  }

  async function runDeploy() {
    if (!preflightOK) {
      message.warning('请先完成预检并通过')
      return
    }
    const v = await depForm.validateFields()
    const groups: string[] = v.enabled_groups || ['backup', 'rdb']
    const bakEngines: string[] = v.backup_engines || []
    if (groups.includes('backup') && !bakEngines.length) {
      message.error('勾选备份控制时请至少选择一种实例类型')
      return
    }
    const escalate = v.escalate ? 'sudo_su' : ''
    setDeployBusy(true)
    try {
      const { data } = await api.post('/platform/agents/deploy', {
        host_instance_id: v.host_instance_id,
        listen_port: v.listen_port || 8090,
        remote_dir: v.remote_dir || '',
        workdir: v.workdir || '',
        enabled_groups: groups,
        backup_engines: bakEngines,
        escalate,
      })
      const deployId = data?.data?.deploy_id
      message.success('已创建下发任务')
      closeDeployModal()
      depForm.resetFields()
      await loadDeploys()
      if (deployId) startPoll(deployId)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '下发失败')
    } finally {
      setDeployBusy(false)
    }
  }

  async function redeploy(row: any) {
    Modal.confirm({
      title: '重新下发 Agent',
      content: `将通过 SSH 覆盖部署「${row.name}」绑定主机上的 Agent，确认继续？`,
      onOk: async () => {
        const { data } = await api.post(`/platform/agents/${row.id}/redeploy`, {})
        const deployId = data?.data?.deploy_id
        message.success('已创建重新下发任务')
        await loadDeploys()
        if (deployId) startPoll(deployId)
      },
    })
  }

  async function probe(row: any) {
    setActingId(row.id)
    try {
      const { data } = await api.post(`/platform/agents/${row.id}/probe`)
      setAgentStatus((prev) => ({ ...prev, [row.id]: data.status || (data.ok ? 'running' : 'stopped') }))
      if (data.ok) message.success(data.message || '运行中')
      else message.warning(data.message || '未运行')
    } finally {
      setActingId('')
    }
  }

  async function startAgent(row: any) {
    setActingId(row.id)
    try {
      const { data } = await api.post(`/platform/agents/${row.id}/start`)
      setAgentStatus((prev) => ({ ...prev, [row.id]: data?.data?.status || 'running' }))
      message.success('已启动')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '启动失败')
    } finally {
      setActingId('')
    }
  }

  async function stopAgent(row: any) {
    Modal.confirm({
      title: '停止 Agent',
      content: `确认停止「${row.name}」远端进程（${row.base_url || ''}）？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        setActingId(row.id)
        try {
          const { data } = await api.post(`/platform/agents/${row.id}/stop`)
          setAgentStatus((prev) => ({ ...prev, [row.id]: data?.data?.status || 'stopped' }))
          message.success('已停止')
        } catch (e: any) {
          message.error(e?.response?.data?.message || '停止失败')
        } finally {
          setActingId('')
        }
      },
    })
  }

  function delAgent(row: any) {
    const isDeployed = !!row.host_instance_id
    Modal.confirm({
      title: '删除 Agent',
      content: isDeployed
        ? `将先 SSH 停止「${row.name}」远端进程（${row.base_url || ''}），再删除登记。确认继续？`
        : `确认删除「${row.name}」？（手动登记，仅删平台记录）`,
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.delete(`/platform/agents/${row.id}`)
          message.success(isDeployed ? '已停止远端并删除' : '已删除')
          await load()
        } catch (e: any) {
          const msg = e?.response?.data?.message || '删除失败'
          Modal.confirm({
            title: '停止远端失败',
            content: `${msg}\n\n是否仅删除平台登记（远端进程可能仍存活）？`,
            okText: '仅删登记',
            okButtonProps: { danger: true },
            onOk: async () => {
              await api.delete(`/platform/agents/${row.id}`, { params: { force: 1 } })
              message.success('已删除登记')
              await load()
            },
          })
        }
      },
    })
  }

  function showLog(row: any) {
    setLogText(row.log_text || '（暂无日志）')
    setLogOpen(true)
  }

  const hostName = (id: string) => {
    const h = hosts.find((x) => x.id === id)
    return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
  }

  const agCols: ColumnsType<any> = [
    { title: '名称', dataIndex: 'name', minWidth: 120 },
    { title: 'Base URL', dataIndex: 'base_url', minWidth: 180, ellipsis: true },
    {
      title: '运行状态',
      width: 100,
      render: (_, r) => runtimeTag(agentStatus[r.id]),
    },
    {
      title: '归属',
      width: 200,
          render: (_, r) => {
            const groups = r.enabled_groups || '-'
            const bak = parseBackupEngines(r.engines)
            const esc = parseEscalateTag(r.tags)
            return (
              <span>
                {groups}
                {bak.length ? (
                  <div style={{ color: 'var(--ant-color-text-secondary)', fontSize: 12 }}>
                    备份：{bak.join(',')}
                  </div>
                ) : null}
                {esc ? <Tag color="orange">提权 {esc}</Tag> : null}
              </span>
            )
          },
    },
    {
      title: '引擎',
      dataIndex: 'engines',
      width: 140,
      ellipsis: true,
    },
    {
      title: '来源',
      width: 100,
      render: (_, r) =>
        r.host_instance_id ? <Tag color="blue">平台下发</Tag> : <Tag>手动登记</Tag>,
    },
    {
      title: '操作',
      width: 320,
      render: (_, r) => {
        const busy = actingId === r.id
        const deployed = !!r.host_instance_id
        const running = agentStatus[r.id] === 'running'
        return (
          <Space wrap size={0}>
            <Button type="link" size="small" loading={busy} onClick={() => probe(r)}>
              刷新状态
            </Button>
            {deployed ? (
              running ? (
                <Button type="link" size="small" danger loading={busy} onClick={() => stopAgent(r)}>
                  停止
                </Button>
              ) : (
                <Button type="link" size="small" loading={busy} onClick={() => startAgent(r)}>
                  启动
                </Button>
              )
            ) : null}
            {deployed ? (
              <Button type="link" size="small" onClick={() => redeploy(r)}>
                重新下发
              </Button>
            ) : null}
            <Button type="link" danger size="small" onClick={() => delAgent(r)}>
              删除
            </Button>
          </Space>
        )
      },
    },
  ]

  const depCols: ColumnsType<any> = [
    {
      title: '主机',
      minWidth: 160,
      ellipsis: true,
      render: (_, r) => hostName(r.host_instance_id),
    },
    {
      title: '状态',
      width: 100,
      render: (_, r) => <Tag color={deployStatusColor(r.status)}>{r.status}</Tag>,
    },
    { title: '端口', dataIndex: 'listen_port', width: 80 },
    { title: 'Base URL', dataIndex: 'base_url', minWidth: 160, ellipsis: true },
    { title: '时间', dataIndex: 'created_at', width: 170 },
    {
      title: '操作',
      width: 80,
      render: (_, r) => (
        <Button type="link" size="small" onClick={() => showLog(r)}>
          日志
        </Button>
      ),
    },
  ]

  return (
    <div className="backup-page">
      <div className="backup-toolbar">
        <h2>平台 Agent</h2>
        <span className="backup-hint">
          平台级执行面：定义时勾选归属（备份 / Redis RDB / 高可用），业务模块按归属引用。
        </span>
        <Space>
          <Button onClick={() => refreshAgentStatus()}>刷新状态</Button>
          <Button
            onClick={() => {
              depForm.resetFields()
              depForm.setFieldsValue({
                listen_port: 8090,
                remote_dir: '/home/sysadmin/auto-dba/platform-agent',
                enabled_groups: ['backup', 'rdb'],
                backup_engines: ['postgres', 'mysql'],
                escalate: false,
              })
              resetPreflight()
              setDepOpen(true)
            }}
          >
            下发到主机
          </Button>
          <Button
            type="primary"
            onClick={() => {
              agForm.resetFields()
              agForm.setFieldsValue({
                enabled_groups: ['backup', 'rdb'],
                backup_engines: ['postgres', 'mysql'],
              })
              setAgOpen(true)
            }}
          >
            手动登记
          </Button>
        </Space>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={agCols} dataSource={agents} />
      <div className="backup-toolbar" style={{ marginTop: 24 }}>
        <h3>下发记录</h3>
      </div>
      <ResponsiveTable rowKey="id" loading={loading} columns={depCols} dataSource={deploys} pagination={{ pageSize: 10 }} />

      <Modal title="手动登记 Agent" open={agOpen} onCancel={() => setAgOpen(false)} onOk={saveAgent} destroyOnClose>
        <Form
          form={agForm}
          layout="vertical"
          initialValues={{ enabled_groups: ['backup', 'rdb'], backup_engines: ['postgres', 'mysql'] }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]}>
            <Input placeholder="http://10.0.0.8:8090" />
          </Form.Item>
          <Form.Item
            name="enabled_groups"
            label="归属"
            rules={[{ required: true, message: '请至少选择一个归属' }]}
            extra="业务模块按归属过滤后引用本 Agent"
          >
            <Checkbox.Group
              options={[
                { label: '备份控制', value: 'backup' },
                { label: 'Redis RDB 分析', value: 'rdb' },
                { label: '高可用', value: 'ha' },
              ]}
            />
          </Form.Item>
          {agNeedBackup ? (
            <Form.Item
              name="backup_engines"
              label="备份实例类型"
              rules={[{ required: true, message: '请至少选择一种实例类型' }]}
              extra="仅对勾选类型做工具预检（如仅 PostgreSQL 则不要求 mysqldump）"
            >
              <Checkbox.Group options={BACKUP_ENGINE_OPTS} />
            </Form.Item>
          ) : null}
          <Form.Item name="username" label="BasicAuth 用户">
            <Input />
          </Form.Item>
          <Form.Item name="password" label="BasicAuth 密码">
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="SSH 下发平台 Agent"
        open={depOpen}
        onCancel={closeDeployModal}
        destroyOnClose
        width={640}
        footer={
          <Space>
            <Button onClick={closeDeployModal} disabled={preflightBusy || deployBusy}>
              取消
            </Button>
            <Button type="default" loading={preflightBusy} disabled={deployBusy} onClick={runPreflight}>
              预检
            </Button>
            <Button type="primary" loading={deployBusy} disabled={!preflightOK || preflightBusy} onClick={runDeploy}>
              下发
            </Button>
          </Space>
        }
      >
        <Form
          form={depForm}
          layout="vertical"
          initialValues={{
            listen_port: 8090,
            remote_dir: '/home/sysadmin/auto-dba/platform-agent',
            enabled_groups: ['backup', 'rdb'],
            backup_engines: ['postgres', 'mysql'],
            escalate: false,
          }}
          onValuesChange={() => {
            if (preflightOK || preflightReport || preflightStep) resetPreflight()
          }}
        >
          <Form.Item
            name="host_instance_id"
            label="目标主机"
            rules={[{ required: true, message: '请选择 host 类型实例' }]}
            extra="需在「实例管理」中登记类型为 host 的机器，且平台 terminal.ssh_identity_file 可达。"
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder={hosts.length ? '选择主机' : '暂无 host 实例'}
              options={hosts.map((h) => ({
                value: h.id,
                label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}`,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="escalate"
            label="提权执行（sudo su -）"
            valuePropName="checked"
            extra="登录用户仍为实例 Username（如 sysadmin）；预检/下发命令以 root 执行。需目标机配置免密 sudo（NOPASSWD）。开启后可将远程目录设为 /opt/auto-dba/platform-agent 等 root 目录。"
          >
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item
            name="enabled_groups"
            label="归属"
            rules={[{ required: true, message: '请至少选择一个归属' }]}
            extra="仅检查已勾选归属；缺失必选工具将阻断下发并给出安装提示。"
          >
            <Checkbox.Group
              options={[
                { label: '备份控制', value: 'backup' },
                { label: 'Redis RDB 分析', value: 'rdb' },
                { label: '高可用', value: 'ha' },
              ]}
            />
          </Form.Item>
          {depNeedBackup ? (
            <Form.Item
              name="backup_engines"
              label="备份实例类型"
              rules={[{ required: true, message: '请至少选择一种实例类型' }]}
              extra="按所选类型检查对应客户端（PostgreSQL→pg_dump；MySQL/TiDB/OceanBase→mysqldump）。物理工具为可选。"
            >
              <Checkbox.Group options={BACKUP_ENGINE_OPTS} />
            </Form.Item>
          ) : null}
          <Form.Item
            name="listen_port"
            label="Agent 监听端口"
            extra="默认 8090。下发仅安装/重启 Agent，不启停数据库等业务服务；禁止填业务端口"
            rules={[{ required: true, type: 'number', min: 1024, max: 65535, message: '端口须在 1024–65535' }]}
          >
            <InputNumber min={1024} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="remote_dir"
            label="远程目录"
            extra="须对 SSH 用户可写；默认家目录路径。勿填无权限的 /opt（除非已 chown）。"
          >
            <Input placeholder="/home/sysadmin/auto-dba/platform-agent" />
          </Form.Item>
          <Form.Item name="workdir" label="Agent workdir" extra="空则使用 remote_dir/data">
            <Input placeholder="/home/sysadmin/auto-dba/platform-agent/data" />
          </Form.Item>
        </Form>

        {(preflightBusy || preflightReport || preflightStep) && (
          <div style={{ marginTop: 16 }}>
            {preflightBusy ? (
              <Alert type="info" showIcon message="预检进行中" description={preflightStep || '正在检查…'} />
            ) : preflightReport ? (
              <Alert
                type={preflightReport.ok ? 'success' : 'error'}
                showIcon
                message={preflightReport.ok ? '预检通过' : '预检未通过'}
                description={
                  <div>
                    {preflightStep ? <div style={{ marginBottom: 8, color: 'var(--ant-color-text-secondary)' }}>{preflightStep}</div> : null}
                    <pre style={{ whiteSpace: 'pre-wrap', margin: 0, fontSize: 12, maxHeight: 220, overflow: 'auto' }}>
                      {preflightReport.summary ||
                        (preflightReport.missing || [])
                          .map((m: any) =>
                            m.title || m.missing_cmd
                              ? `· 【${m.title || m.key}】缺少命令：${m.missing_cmd || m.tool || '-'}`
                              : `· ${m.key}: ${m.hint || m.detail || '缺失'}`,
                          )
                          .join('\n') ||
                        '无详细信息'}
                    </pre>
                  </div>
                }
              />
            ) : null}
          </div>
        )}
      </Modal>

      <Modal title="下发日志" open={logOpen} onCancel={() => setLogOpen(false)} footer={null} width={720}>
        <pre className="backup-deploy-log">{logText}</pre>
      </Modal>
    </div>
  )
}
