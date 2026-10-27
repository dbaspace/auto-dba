import React, { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Select, Space, Switch, Table, Tag, message } from 'antd'
import { Link } from 'react-router-dom'
import api from '../../../api/request'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'

const vendors = [
  { value: 'tencent', label: '腾讯云' },
  { value: 'aliyun', label: '阿里云' },
  { value: 'huawei', label: '华为云' },
  { value: 'aws', label: 'AWS' },
]

type Opt = { id: string; code?: string; name?: string }

export default function CloudAccounts() {
  const [rows, setRows] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [platformRegions, setPlatformRegions] = useState<Opt[]>([])
  const [envs, setEnvs] = useState<Opt[]>([])
  const [editRow, setEditRow] = useState<any | null>(null)
  const [creating, setCreating] = useState(false)
  const [keyRow, setKeyRow] = useState<any>(null)
  const [userRow, setUserRow] = useState<any>(null)
  const [form] = Form.useForm()
  const [keyForm] = Form.useForm()

  async function load() {
    const { data } = await api.get('/cloud/accounts')
    setRows(data.data || [])
  }

  useEffect(() => {
    void load().catch((e) => message.error(e?.response?.data?.message || '加载失败'))
    api.get('/users').then(({ data }) => setUsers(data.data || data || [])).catch(() => {})
    api.get('/regions').then(({ data }) => setPlatformRegions(data.data || [])).catch(() => {})
    api.get('/environments').then(({ data }) => setEnvs(data.data || [])).catch(() => {})
  }, [])

  function regionLabel(id: string) {
    const r = platformRegions.find((x) => x.id === id)
    return r ? (r.name || r.code || id) : id
  }

  function envLabel(id: string) {
    const e = envs.find((x) => x.id === id)
    return e ? (e.name || e.code || id) : id
  }

  function openCreate() {
    setEditRow(null)
    form.resetFields()
    form.setFieldsValue({ enabled: true, platform_region_ids: [], env_ids: [], sync_engines: [] })
    setCreating(true)
  }

  function openEdit(row: any) {
    setCreating(false)
    setEditRow(row)
    form.setFieldsValue({
      vendor: row.vendor,
      name: row.name,
      regions: (row.regions || []).join(', '),
      platform_region_ids: row.platform_region_ids || [],
      env_ids: row.env_ids || [],
      sync_engines: row.sync_engines || [],
      enabled: row.enabled !== false,
    })
  }

  return (
    <div style={{ padding: 16 }}>
      <Space style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0 }}>云账号</h2>
        <Auth urn={Perm.cloud.keys}>
          <Button type="primary" onClick={openCreate}>新增账号</Button>
        </Auth>
      </Space>
      <p>
        同一云厂商可以登记多套账号。云地域用于同步拉取；平台区域/环境用于与本平台资源对齐。
        只读密钥用于同步和闪回，变更密钥只用于已审批工单。密钥不会回显。
      </p>
      <Table
        rowKey="id"
        dataSource={rows}
        pagination={false}
        scroll={{ x: 1100 }}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '厂商', dataIndex: 'vendor', render: (v) => vendors.find((x) => x.value === v)?.label || v },
          { title: '云地域', dataIndex: 'regions', render: (v) => (v || []).join(', ') || '—' },
          {
            title: '平台区域',
            dataIndex: 'platform_region_ids',
            render: (ids: string[]) => (ids || []).length
              ? (ids || []).map((id) => <Tag key={id}>{regionLabel(id)}</Tag>)
              : '—',
          },
          {
            title: '平台环境',
            dataIndex: 'env_ids',
            render: (ids: string[]) => (ids || []).length
              ? (ids || []).map((id) => <Tag key={id}>{envLabel(id)}</Tag>)
              : '—',
          },
          { title: '启用', dataIndex: 'enabled', render: (v) => (v === false ? <Tag>停用</Tag> : <Tag color="green">启用</Tag>) },
          { title: '只读', dataIndex: 'has_readonly', render: (v) => v ? <Tag color="green">已登记</Tag> : <Tag>未登记</Tag> },
          { title: '变更', dataIndex: 'has_operate', render: (v) => v ? <Tag color="blue">已登记</Tag> : <Tag>未登记</Tag> },
          { title: 'SecretId', dataIndex: 'secret_id', render: (v) => v || '—' },
          {
            title: '操作',
            render: (_, row) => (
              <Auth urn={Perm.cloud.keys}>
                <Space wrap>
                  <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
                  <Button size="small" onClick={() => { keyForm.resetFields(); setKeyRow(row) }}>登记密钥</Button>
                  <Button size="small" onClick={() => setUserRow({ ...row, user_ids: row.user_ids || [] })}>绑定用户</Button>
                </Space>
              </Auth>
            ),
          },
        ]}
      />
      <Modal
        title={editRow ? `编辑云账号 · ${editRow.name}` : '新增云账号'}
        open={creating || !!editRow}
        onCancel={() => { setCreating(false); setEditRow(null) }}
        onOk={() => form.submit()}
      >
        <Form form={form} layout="vertical" onFinish={async (v) => {
          const regions = String(v.regions || '').split(/[,，\s]+/).map((s: string) => s.trim()).filter(Boolean)
          const body = {
            name: v.name,
            regions,
            platform_region_ids: v.platform_region_ids || [],
            env_ids: v.env_ids || [],
            sync_engines: v.sync_engines || [],
            enabled: v.enabled !== false,
            vendor: v.vendor,
          }
          try {
            if (editRow) {
              await api.put(`/cloud/accounts/${editRow.id}`, body)
              message.success('已保存')
            } else {
              await api.post('/cloud/accounts', body)
              message.success('已创建')
            }
            setCreating(false)
            setEditRow(null)
            void load()
          } catch (e: any) {
            message.error(e?.response?.data?.message || '保存失败')
          }
        }}>
          <Form.Item name="vendor" label="厂商" rules={[{ required: true }]}>
            <Select options={vendors} disabled={!!editRow} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item
            name="regions"
            label="云地域"
            extra="云厂商 Region，多个用逗号分隔，如 ap-beijing,ap-guangzhou；同步按此列表拉取"
          >
            <Input placeholder="ap-beijing,ap-guangzhou" />
          </Form.Item>
          <Form.Item
            name="platform_region_ids"
            label="平台区域"
            extra={<>来自 <Link to="/config/regions">配置中心 · 区域管理</Link>，可多选</>}
          >
            <Select
              mode="multiple"
              allowClear
              placeholder="选择本平台区域"
              options={platformRegions.map((r) => ({ value: r.id, label: r.name || r.code || r.id }))}
            />
          </Form.Item>
          <Form.Item
            name="sync_engines"
            label="定时同步资源"
            extra="定时任务只同步勾选的引擎。不勾选则该账号不参与定时同步。页面上的「立即同步」不受此项限制。"
          >
            <Select
              mode="multiple"
              allowClear
              placeholder="选择要定时同步的资源"
              options={[
                { value: 'postgres', label: 'PostgreSQL' },
                { value: 'mysql', label: 'MySQL' },
                { value: 'redis', label: 'Redis' },
                { value: 'host', label: '主机' },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="env_ids"
            label="平台环境"
            extra={<>来自 <Link to="/config/environments">配置中心 · 环境管理</Link>，可多选</>}
          >
            <Select
              mode="multiple"
              allowClear
              placeholder="选择本平台环境"
              options={envs.map((e) => ({ value: e.id, label: e.name || e.code || e.id }))}
            />
          </Form.Item>
          {editRow && (
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch checkedChildren="启用" unCheckedChildren="停用" />
            </Form.Item>
          )}
        </Form>
      </Modal>
      <Modal title={`登记密钥 · ${keyRow?.name || ''}`} open={!!keyRow} onCancel={() => setKeyRow(null)} onOk={() => keyForm.submit()}>
        <Form form={keyForm} layout="vertical" onFinish={async (v) => {
          await api.put(`/cloud/accounts/${keyRow.id}/keys`, v)
          message.success('已保存，密钥不会回显')
          setKeyRow(null)
          void load()
        }}>
          <Form.Item name="role" label="用途" initialValue="readonly" rules={[{ required: true }]}>
            <Select options={[{ value: 'readonly', label: '只读：清单、慢日志、询价、闪回' }, { value: 'operate', label: '变更：创建、变配、扩盘、重启、切换、改参数' }]} />
          </Form.Item>
          <Form.Item name="secret_id" label="SecretId / AK" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="secret_key" label="SecretKey / SK" rules={[{ required: true }]}><Input.Password /></Form.Item>
        </Form>
      </Modal>
      <Modal title={`绑定用户 · ${userRow?.name || ''}`} open={!!userRow} onCancel={() => setUserRow(null)} onOk={async () => {
        await api.put(`/cloud/accounts/${userRow.id}/users`, { user_ids: userRow.user_ids || [] })
        message.success('已绑定')
        setUserRow(null)
        void load()
      }}>
        <Select
          mode="multiple"
          style={{ width: '100%' }}
          value={userRow?.user_ids || []}
          options={users.map((u) => ({ value: u.id, label: u.display_name || u.username }))}
          onChange={(v) => setUserRow((r: any) => ({ ...r, user_ids: v }))}
        />
        <p>未绑定的用户不能拉 WAL、手动同步或提交变更。闪回说明见 <Link to="/ops/flashback/cloud">多云密钥</Link>。</p>
      </Modal>
    </div>
  )
}
