import React, { useEffect, useState } from 'react'
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Switch, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import Auth from '../../components/Auth'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import {
  Range, STATUS_OPTIONS, Zone, ZonePolicy, errMsg, fmtTime, handleWriteError, handleWriteResult, loadRanges, loadZones,
} from './dnsApi'

interface RoleOpt { id: string; name: string; code: string }

export default function DnsZones() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.dns.edit)
  const [zones, setZones] = useState<Zone[]>([])
  const [ranges, setRanges] = useState<Range[]>([])
  const [policies, setPolicies] = useState<Record<string, ZonePolicy>>({})
  const [roles, setRoles] = useState<RoleOpt[]>([])
  const [approvers, setApprovers] = useState<string[]>([])
  const [zoneOpen, setZoneOpen] = useState(false)
  const [rangeOpen, setRangeOpen] = useState(false)
  const [editingRange, setEditingRange] = useState<Range | null>(null)
  const [policyZone, setPolicyZone] = useState<Zone | null>(null)
  const [zoneForm] = Form.useForm()
  const [rangeForm] = Form.useForm()
  const [policyForm] = Form.useForm()

  async function load() {
    const [zs, rs, p] = await Promise.all([loadZones(), loadRanges(true), api.get('/dns/policies')])
    setZones(zs)
    setRanges(rs)
    setPolicies(p.data.data || {})
    setRoles(p.data.roles || [])
    setApprovers(p.data.approvers || [])
  }

  useEffect(() => { load().catch((e) => message.error(errMsg(e, '加载失败'))) }, [])

  async function write(fn: () => Promise<any>, ok: string) {
    try {
      const res = await fn()
      handleWriteResult(res.data, ok)
      load()
      return true
    } catch (e: any) {
      handleWriteError(e)
      return false
    }
  }

  async function saveZone() {
    const v = await zoneForm.validateFields()
    if (await write(() => api.post('/dns/zones', { origin: v.origin, reason: v.reason || '' }), '已创建')) {
      setZoneOpen(false)
    }
  }

  function openRange(r: Range | null) {
    setEditingRange(r)
    rangeForm.resetFields()
    if (r) {
      rangeForm.setFieldsValue({ name: r.name, cidrs: (r.network_list || []).join('\n'), description: r.description, status: r.status })
    } else {
      rangeForm.setFieldsValue({ status: 'Active' })
    }
    setRangeOpen(true)
  }

  async function saveRange() {
    const v = await rangeForm.validateFields()
    const body = {
      name: v.name, description: v.description || '', status: v.status, reason: v.reason || '',
      network_list: String(v.cidrs || '').split('\n').map((s: string) => s.trim()).filter(Boolean),
    }
    const ok = editingRange
      ? await write(() => api.put(`/dns/ranges/${editingRange.id}`, body), '已保存')
      : await write(() => api.post('/dns/ranges', body), '已创建')
    if (ok) setRangeOpen(false)
  }

  function openPolicy(z: Zone) {
    const p = policies[z.id]
    setPolicyZone(z)
    policyForm.setFieldsValue({
      require_approval: !!p?.require_approval,
      editor_role_ids: p?.editor_role_ids || [],
      env: p?.env || '',
    })
  }

  async function savePolicy() {
    if (!policyZone) return
    const v = await policyForm.validateFields()
    try {
      await api.put(`/dns/policies/${policyZone.id}`, v)
      message.success('已保存')
      setPolicyZone(null)
      load()
    } catch (e: any) {
      message.error(errMsg(e))
    }
  }

  const roleName = (id: string) => roles.find((r) => r.id === id)?.name || id

  return (
    <Space direction="vertical" style={{ width: '100%' }} size={16}>
      <Card
        title="解析域"
        size="small"
        extra={<Auth urn={Perm.dns.create}><Button type="primary" onClick={() => { zoneForm.resetFields(); setZoneOpen(true) }}>新建解析域</Button></Auth>}
      >
        <ResponsiveTable
          rowKey="id"
          dataSource={zones}
          pagination={false}
          columns={[
            { title: '解析域', dataIndex: 'origin' },
            {
              title: '状态', dataIndex: 'status', width: 90,
              render: (v: string, z: Zone) => (
                <Switch size="small" checked={v === 'Active'} disabled={!canEdit}
                  onChange={(on) => write(() => api.put(`/dns/zones/${z.id}`, { origin: z.origin, status: on ? 'Active' : 'Removed' }), on ? '已启用' : '已停用')} />
              ),
            },
            {
              title: '审批', render: (_: unknown, z: Zone) => policies[z.id]?.require_approval
                ? <Tag color="orange">需审批</Tag> : <Tag>直接生效</Tag>,
            },
            {
              title: '可编辑角色', render: (_: unknown, z: Zone) => {
                const ids = policies[z.id]?.editor_role_ids || []
                return ids.length ? ids.map((id) => <Tag key={id}>{roleName(id)}</Tag>) : <span style={{ color: '#8c8c8c' }}>不限</span>
              },
            },
            { title: '环境', render: (_: unknown, z: Zone) => policies[z.id]?.env || '-' },
            { title: '更新时间', dataIndex: 'updated_time', render: (v: number) => fmtTime(v) },
            {
              title: '操作', width: 180,
              render: (_: unknown, z: Zone) => (
                <Space size={0}>
                  <Auth urn={Perm.dns.policy}><Button type="link" onClick={() => openPolicy(z)}>策略</Button></Auth>
                  <Auth urn={Perm.dns.delete}>
                    <Popconfirm title={`删除解析域 ${z.origin}？须先删除其下全部记录`} onConfirm={() => write(() => api.delete(`/dns/zones/${z.id}`), '已删除')}>
                      <Button type="link" danger>删除</Button>
                    </Popconfirm>
                  </Auth>
                </Space>
              ),
            },
          ]}
        />
        <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 8 }}>
          需审批的解析域：修改会生成 DNS 变更工单，审批人取全局参数 dns_approvers（当前：{approvers.join('、') || 'admin'}），审批通过后自动执行。
        </div>
      </Card>

      <Card
        title="解析范围"
        size="small"
        extra={<Auth urn={Perm.dns.create}><Button type="primary" onClick={() => openRange(null)}>新建解析范围</Button></Auth>}
      >
        <ResponsiveTable
          rowKey="id"
          dataSource={ranges}
          pagination={false}
          columns={[
            { title: '名称', dataIndex: 'name', render: (v: string) => v === 'DEFAULT' ? <span>{v} <Tag>系统</Tag></span> : v },
            { title: '来源网段', dataIndex: 'network_list', render: (v: string[]) => (v || []).map((c) => <div key={c}>{c}</div>) },
            { title: '状态', dataIndex: 'status', render: (v: string) => v === 'Active' ? <Tag color="green">运行</Tag> : <Tag>停止</Tag> },
            { title: '描述', dataIndex: 'description' },
            {
              title: '操作', width: 160,
              render: (_: unknown, r: Range) => r.name === 'DEFAULT' ? <span style={{ color: '#8c8c8c' }}>不参与来源匹配</span> : (
                <Space size={0}>
                  <Auth urn={Perm.dns.edit}><Button type="link" onClick={() => openRange(r)}>编辑</Button></Auth>
                  <Auth urn={Perm.dns.delete}>
                    <Popconfirm title={`删除解析范围 ${r.name}？`} onConfirm={() => write(() => api.delete(`/dns/ranges/${r.id}`), '已删除')}>
                      <Button type="link" danger>删除</Button>
                    </Popconfirm>
                  </Auth>
                </Space>
              ),
            },
          ]}
        />
        <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 8 }}>
          客户端按来源 IP 命中解析范围（按创建顺序取第一个），未命中时走 DEFAULT 并返回 NXDOMAIN。
        </div>
      </Card>

      <Modal title="新建解析域" open={zoneOpen} onOk={saveZone} onCancel={() => setZoneOpen(false)} destroyOnClose>
        <Form form={zoneForm} layout="vertical">
          <Form.Item name="origin" label="解析域" rules={[{ required: true }]}><Input placeholder="restosuite.cn" /></Form.Item>
          <Form.Item name="reason" label="变更原因"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal title={editingRange ? '编辑解析范围' : '新建解析范围'} open={rangeOpen} onOk={saveRange} onCancel={() => setRangeOpen(false)} destroyOnClose>
        <Form form={rangeForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input placeholder="office" /></Form.Item>
          <Form.Item name="cidrs" label="来源网段" rules={[{ required: true }]} extra="每行一个，不允许与其他范围重叠">
            <Input.TextArea rows={4} placeholder="10.100.0.0/16" />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}><Select options={STATUS_OPTIONS} /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="reason" label="变更原因"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal title={`解析域策略：${policyZone?.origin || ''}`} open={!!policyZone} onOk={savePolicy} onCancel={() => setPolicyZone(null)} destroyOnClose>
        <Form form={policyForm} layout="vertical">
          <Form.Item name="require_approval" label="修改需审批" valuePropName="checked"
            extra="开启后，该域下的新增、修改、删除、导入与回滚都会生成工单；TTL 预降不走审批。">
            <Switch />
          </Form.Item>
          <Form.Item name="editor_role_ids" label="可编辑角色" extra="为空时，凡有 DNS 编辑权限的人都能改；不为空时只有这些角色和管理员能改。">
            <Select mode="multiple" allowClear options={roles.map((r) => ({ value: r.id, label: `${r.name}（${r.code}）` }))} />
          </Form.Item>
          <Form.Item name="env" label="环境标识"><Input placeholder="prod / test" /></Form.Item>
        </Form>
      </Modal>
    </Space>
  )
}
