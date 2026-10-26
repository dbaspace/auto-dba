import React, { useEffect, useState } from 'react'
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
  username: string
  password_set?: boolean
  env_codes?: string[]
  environments?: BoundEnv[]
  enabled?: boolean
  updated_at?: string
}

export default function SqlExecutors() {
  const [rows, setRows] = useState<Row[]>([])
  const [envs, setEnvs] = useState<EnvOpt[]>([])
  const [localCodes, setLocalCodes] = useState<string[]>([])
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [form] = Form.useForm()

  async function load() {
    const [{ data }, envRes] = await Promise.all([api.get('/sql-executors'), api.get('/environments')])
    setRows(data.data || [])
    setLocalCodes(data.local_idc_codes || [])
    setEnvs(envRes.data.data || [])
  }

  useEffect(() => {
    load().catch((e) => message.error(e?.response?.data?.message || '加载失败'))
  }, [])

  function open(row?: Row) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      name: row?.name || '',
      base_url: row?.base_url || '',
      username: row?.username || '',
      password: '',
      env_codes: row?.env_codes || [],
      enabled: row?.enabled !== false,
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const body = { ...values, enabled: values.enabled !== false }
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

  async function saveLocal(codes: string[]) {
    setLocalCodes(codes)
    try {
      await api.put('/sql-executors/local-idc', { codes })
    } catch (e: any) {
      message.error(e?.response?.data?.message || '保存本服务环境失败')
    }
  }

  async function probe(row: Row) {
    try {
      const { data } = await api.post(`/sql-executors/${row.id}/probe`)
      message.success(data.message || '探活通过')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '探活失败')
    }
  }

  async function remove(row: Row) {
    Modal.confirm({
      title: `删除执行器「${row.name}」？`,
      onOk: async () => {
        await api.delete(`/sql-executors/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const envOptions = envs.map((e) => ({ value: e.code, label: e.name ? `${e.name}（${e.code}）` : e.code }))

  return (
    <div>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="执行器要按这个顺序做一次，平台不会自动创建"
        description={
          <div>
            <div>1. 先在「环境管理」确认海外环境已经存在，实例的机房使用这些环境的编码。</div>
            <div>2. 在能直连这些库的机器上启动执行器。北京只需要能访问这个地址，不必从北京直连海外数据库端口。</div>
            <Typography.Text code>sql-agent -listen :8093 -user &lt;账号&gt; -pass &lt;密码&gt;</Typography.Text>
            <div>3. 在本页新增一台，填写地址和账号，多选要绑定的环境，保存。</div>
            <div>4. 点探活。通过后，这些环境上的 DDL/DML 才交给这台执行器。失败时会写明是地址不通、账号密码不对，还是环境已经绑在另一台执行器上。</div>
            <div>本服务所在环境不用装执行器。请在下方选上本机所在环境：选中的继续本进程直连。选上之后，其它没有绑定执行器的环境会记失败，不会再从本机直连过去。</div>
          </div>
        }
      />
      <Space style={{ marginBottom: 16 }} wrap>
        <span>本服务所在环境</span>
        <Select
          mode="multiple"
          allowClear
          style={{ minWidth: 280 }}
          placeholder="选择后，这些环境继续本机直连"
          value={localCodes}
          options={envOptions}
          onChange={(v) => saveLocal(v)}
        />
        <Button type="primary" onClick={() => open()}>
          新增执行器
        </Button>
      </Space>
      <ResponsiveTable
        rowKey="id"
        dataSource={rows}
        locale={{ emptyText: '还没有执行器。先在能连上目标库的机器启动 sql-agent，再点「新增执行器」。' }}
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
            title: '启用',
            width: 80,
            render: (_, row) => (row.enabled ? '是' : '否'),
          },
          {
            title: '操作',
            width: 220,
            render: (_, row) => (
              <Space>
                <Button size="small" onClick={() => probe(row)}>
                  探活
                </Button>
                <Button size="small" onClick={() => open(row)}>
                  编辑
                </Button>
                <Button size="small" danger onClick={() => remove(row)}>
                  删除
                </Button>
              </Space>
            ),
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
          <Form.Item label="名称" name="name" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="如 欧洲执行器" />
          </Form.Item>
          <Form.Item
            label="地址"
            name="base_url"
            extra="填执行器的 http://主机:8093。这台机器必须能连所选环境里的数据库。"
            rules={[{ required: true, message: '请填写地址' }]}
          >
            <Input placeholder="http://主机:8093" />
          </Form.Item>
          <Form.Item label="账号" name="username" rules={[{ required: true, message: '请填写账号' }]}>
            <Input />
          </Form.Item>
          <Form.Item label="密码" name="password" extra={editingId ? '留空表示不修改' : undefined}>
            <Input.Password />
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
