import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Form, Input, Modal, Select, Space, Switch, Tag, Typography, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'

interface EnvOpt {
  code?: string
  name?: string
}

interface BoundEnv {
  code?: string
  name?: string
}

interface Row {
  id: string
  name: string
  base_url: string
  env_codes?: string[]
  environments?: BoundEnv[]
  enabled?: boolean
  updated_at?: string
  host_instance_id?: string
}

interface DeployRow {
  id: string
  status?: string
  base_url?: string
  agent_id?: string
  listen_port?: number
  created_at?: string
}

export default function SqlExecutors() {
  const [rows, setRows] = useState<Row[]>([])
  const [deploys, setDeploys] = useState<DeployRow[]>([])
  const [envs, setEnvs] = useState<EnvOpt[]>([])
  const [hosts, setHosts] = useState<{ id: string; host?: string; domain_name?: string; domains?: string[] }[]>([])
  const [filterEnv, setFilterEnv] = useState('')
  const [filterAddr, setFilterAddr] = useState('')
  const [page, setPage] = useState(1)
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [actingId, setActingId] = useState('')
  const [statusMap, setStatusMap] = useState<Record<string, { status: string; message?: string }>>({})
  const [form] = Form.useForm()

  async function load() {
    try {
      const [{ data }, envRes] = await Promise.all([
        api.get('/sql-executors', { silent: true }),
        api.get('/environments', { silent: true }).catch(() => ({ data: { data: [] } })),
      ])
      const list = data.data || []
      setRows(list)
      setEnvs(envRes.data.data || [])
      void refreshStatus(list)
    } catch (e: any) {
      message.error(e?.response?.data?.message || '执行器列表加载失败')
    }
    void api.get('/sql-executors/deploys', { silent: true })
      .catch(() => api.get('/platform/agents/sql-executors/deploys', { silent: true }))
      .then((depRes) => {
        setDeploys(
          (depRes?.data?.data || []).filter((d: DeployRow) => d.status === 'success' && d.base_url && d.agent_id),
        )
      })
      .catch(() => {})
    void api.get('/instances', { params: { db_type: 'host' }, silent: true })
      .then((hostRes) => setHosts(hostRes.data?.data || []))
      .catch(() => {})
  }

  async function refreshStatus(list: Row[]) {
    if (!list.length) {
      setStatusMap({})
      return
    }
    const checking: Record<string, { status: string }> = {}
    for (const row of list) checking[row.id] = { status: 'checking' }
    setStatusMap((prev) => ({ ...prev, ...checking }))
    try {
      const { data } = await api.get('/platform/agents/sql-executors/status', { silent: true })
      const map: Record<string, { status: string; message?: string }> = {}
      for (const it of data.data || []) map[it.id] = { status: it.status || 'unknown', message: it.message }
      setStatusMap(map)
    } catch {
      const fail: Record<string, { status: string; message?: string }> = {}
      for (const row of list) fail[row.id] = { status: 'unknown', message: '状态获取失败' }
      setStatusMap(fail)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  function open(row?: Row) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      name: row?.name || '',
      base_url: row?.base_url || '',
      deploy_id: undefined,
      env_codes: row?.env_codes || [],
      enabled: row?.enabled !== false,
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const body = {
        name: values.name,
        env_codes: values.env_codes,
        enabled: values.enabled !== false,
        deploy_id: editingId ? undefined : values.deploy_id,
      }
      if (editingId) await api.put(`/sql-executors/${editingId}`, body)
      else await api.post('/sql-executors', body)
      message.success('已保存')
      setVisible(false)
      await load()
    } catch (e: any) {
      if (e?.errorFields) return
      message.error(e?.response?.data?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function probe(row: Row) {
    try {
      const { data } = await api.post(`/sql-executors/${row.id}/probe`)
      setStatusMap((prev) => ({ ...prev, [row.id]: { status: 'running', message: data.message } }))
      message.success(data.message || '探活通过')
    } catch (e: any) {
      setStatusMap((prev) => ({ ...prev, [row.id]: { status: 'stopped', message: e?.response?.data?.message } }))
      message.error(e?.response?.data?.message || '探活失败')
    }
  }

  async function stop(row: Row) {
    Modal.confirm({
      title: '停止 SQL 执行器',
      content: `确认停止「${row.name}」远端进程（${row.base_url || ''}）？停止后该执行器会禁用，绑定环境的工单不再发到这里。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        setActingId(row.id)
        try {
          await api.post(`/sql-executors/${row.id}/stop`)
          message.success('已停止并禁用')
          await load()
        } catch (e: any) {
          message.error(e?.response?.data?.message || '停止失败')
        } finally {
          setActingId('')
        }
      },
    })
  }

  async function redeploy(row: Row) {
    Modal.confirm({
      title: '重新下发 SQL 执行器',
      content: `将通过 SSH 覆盖部署「${row.name}」绑定主机上的 sql-agent，确认继续？`,
      onOk: async () => {
        setActingId(row.id)
        try {
          await api.post(`/sql-executors/${row.id}/redeploy`, {})
          message.success('已创建重新下发任务')
          await load()
        } catch (e: any) {
          message.error(e?.response?.data?.message || '重新下发失败')
        } finally {
          setActingId('')
        }
      },
    })
  }

  function remove(row: Row) {
    const isDeployed = !!row.host_instance_id
    const envs = (row.environments || []).map((e) => e.name || e.code).filter(Boolean).join('、')
    Modal.confirm({
      title: '删除 SQL 执行器',
      content: isDeployed
        ? `将先 SSH 停止「${row.name}」远端进程（${row.base_url || ''}），再删除登记。${
            envs ? `绑定环境：${envs}。` : ''
          }这些环境的 DDL/DML 将不再发到这台机器。确认继续？`
        : `确认删除「${row.name}」登记？${envs ? `绑定环境：${envs}。` : ''}`,
      okButtonProps: { danger: true },
      onOk: async () => {
        setActingId(row.id)
        try {
          await api.delete(`/sql-executors/${row.id}`)
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
              await api.delete(`/sql-executors/${row.id}`, { params: { force: 1 } })
              message.success('已删除登记')
              await load()
            },
          })
        } finally {
          setActingId('')
        }
      },
    })
  }

  const envOptions = envs.map((e) => ({ value: e.code, label: e.name ? `${e.name}（${e.code}）` : e.code }))
  const filteredRows = useMemo(() => {
    const q = filterAddr.trim().toLowerCase()
    return rows.filter((row) => {
      if (filterEnv) {
        const codes = [
          ...(row.env_codes || []),
          ...(row.environments || []).map((e) => e.code || ''),
        ].filter(Boolean)
        if (!codes.includes(filterEnv)) return false
      }
      if (!q) return true
      const host = row.host_instance_id ? hosts.find((h) => h.id === row.host_instance_id) : undefined
      const parts = [row.name, row.base_url, host?.host, host?.domain_name, ...(host?.domains || [])]
      return parts.some((p) => String(p || '').toLowerCase().includes(q))
    })
  }, [rows, hosts, filterEnv, filterAddr])

  return (
    <div>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="执行器随上方下发登记"
        description={
          <div>
            <div>1. 先在「环境管理」确认海外环境已经存在，实例的机房使用这些环境的编码。</div>
            <div>2. 点本页上方「下发」，把 sql-agent 下到能直连这些库的机器。下发时填写名称和绑定环境，成功后登记到下方列表。数据库账号只在实例管理填写。</div>
            <div>3. 点探活。通过后，这些环境上的 DDL/DML 才交给这台执行器。失败时会写明是地址不通，还是环境已经绑在另一台执行器上。需要改名称或环境时点「编辑」。</div>
          </div>
        }
      />
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder="环境"
          style={{ width: 180 }}
          value={filterEnv || undefined}
          options={envOptions}
          onChange={(v) => { setFilterEnv(v || ''); setPage(1) }}
        />
        <Input
          allowClear
          placeholder="地址：主机、域名或 URL"
          style={{ width: 240 }}
          value={filterAddr}
          onChange={(e) => { setFilterAddr(e.target.value); setPage(1) }}
        />
      </Space>
      <ResponsiveTable
        rowKey="id"
        dataSource={filteredRows}
        pagination={{ current: page, pageSize: 10, onChange: (p) => setPage(p) }}
        locale={{ emptyText: '还没有执行器。请先用本页上方「下发」。' }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '地址', dataIndex: 'base_url' },
          {
            title: '绑定环境',
            render: (_, row) =>
              (row.environments || []).length === 0
                ? '-'
                : (row.environments || []).map((e) => (
                    <Tag key={e.code}>{e.name || e.code}</Tag>
                  )),
          },
          {
            title: '状态',
            width: 100,
            render: (_, row) => {
              if (row.enabled === false) return <Tag title="已禁用，工单不会发到这里">已禁用</Tag>
              const st = statusMap[row.id]?.status || 'checking'
              const color = st === 'running' ? 'success' : st === 'stopped' ? 'default' : st === 'checking' ? 'processing' : 'warning'
              const label = st === 'running' ? '运行中' : st === 'stopped' ? '已停止' : st === 'checking' ? '检测中' : '未知'
              return <Tag color={color} title={statusMap[row.id]?.message}>{label}</Tag>
            },
          },
          {
            title: '操作',
            width: 360,
            render: (_, row) => {
              const busy = actingId === row.id
              const deployed = !!row.host_instance_id
              return (
                <Space wrap>
                  <Button size="small" loading={busy} onClick={() => probe(row)}>
                    探活
                  </Button>
                  <Button size="small" loading={busy} onClick={() => open(row)}>
                    编辑
                  </Button>
                  {deployed ? (
                    <Button size="small" danger loading={busy} onClick={() => stop(row)}>
                      停止
                    </Button>
                  ) : null}
                  {deployed ? (
                    <Button size="small" loading={busy} onClick={() => redeploy(row)}>
                      重新下发
                    </Button>
                  ) : null}
                  <Button size="small" danger loading={busy} onClick={() => remove(row)}>
                    删除
                  </Button>
                </Space>
              )
            },
          },
        ]}
      />
      <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
        探活通过后，工单才会把该环境的 SQL 发到这里。
      </Typography.Paragraph>
      <Modal
        open={visible}
        title={editingId ? '编辑执行器' : '新增执行器'}
        width={560}
        destroyOnClose
        onCancel={() => setVisible(false)}
        onOk={save}
        confirmLoading={saving}
      >
        <Form form={form} labelCol={{ style: { width: 96 } }}>
          {!editingId && deploys.length === 0 ? (
            <Alert type="warning" showIcon style={{ marginBottom: 12 }} message="还没有下发成功的记录，请先用本页上方「下发」。" />
          ) : null}
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="如 欧洲执行器" />
          </Form.Item>
          {!editingId ? (
            <Form.Item label="下发记录" name="deploy_id" rules={[{ required: true, message: '请选择下发成功的记录' }]}>
              <Select
                placeholder={deploys.length ? '选择下发成功的记录' : '暂无成功记录'}
                options={deploys.map((d) => ({
                  value: d.id,
                  label: `${d.base_url}${d.created_at ? ' · ' + d.created_at : ''}`,
                }))}
              />
            </Form.Item>
          ) : null}
          <Form.Item noStyle shouldUpdate={(a, b) => a.deploy_id !== b.deploy_id || a.base_url !== b.base_url}>
            {() => {
              const hit = deploys.find((d) => d.id === form.getFieldValue('deploy_id'))
              const url = editingId ? form.getFieldValue('base_url') : hit?.base_url || ''
              return (
                <Form.Item label="地址" extra="从下发成功记录带出，不能手填。">
                  <Input value={url} readOnly placeholder="选择下发记录后带出" />
                </Form.Item>
              )
            }}
          </Form.Item>
          <Form.Item
            label="绑定环境"
            name="env_codes"
            extra="从环境管理选择。一台执行器可以选多个，一个环境只能绑一台。"
            rules={[{ required: true, message: '请选择环境' }]}
          >
            <Select mode="multiple" options={envOptions} placeholder="选择环境" />
          </Form.Item>
          <Form.Item label="启用" name="enabled" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
