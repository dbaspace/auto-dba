import React, { useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Select, Switch, Table, message } from 'antd'
import api from '../../../api/request'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'

export default function CloudPolicies() {
  const [rows, setRows] = useState<any[]>([])
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm()

  async function load() {
    const { data } = await api.get('/cloud/policies')
    setRows(data.data || [])
  }
  useEffect(() => { void load().catch((e) => message.error(e?.response?.data?.message || '加载失败')) }, [])

  return (
    <div style={{ padding: 16 }}>
      <h2>自愈策略</h2>
      <p>默认关闭。模式为「只建工单」时不会调用云 API。自动执行会先写系统审核记录，再调用变更密钥。自动策略不会降配、缩盘、隔离或销毁。</p>
      <Auth urn={Perm.cloud.submit}><Button type="primary" onClick={() => { form.resetFields(); form.setFieldsValue({ mode: 'ticket', enabled: false, action: 'restart', cooldown_min: 30, daily_max: 1, disk_step_gb: 10 }); setOpen(true) }}>新增</Button></Auth>
      <Table
        style={{ marginTop: 12 }}
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '实例', dataIndex: 'instance_id' },
          { title: '云实例', dataIndex: 'cloud_instance_id' },
          { title: '动作', dataIndex: 'action' },
          { title: '模式', dataIndex: 'mode', render: (v) => v === 'auto' ? '自动执行' : '只建工单' },
          { title: '开启', dataIndex: 'enabled', render: (v) => v ? '是' : '否' },
          { title: '冷却分钟', dataIndex: 'cooldown_min' },
          { title: '每日上限', dataIndex: 'daily_max' },
          { title: '窗口', dataIndex: 'window' },
        ]}
      />
      <Modal title="自愈策略" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form form={form} layout="vertical" onFinish={async (v) => {
          await api.put('/cloud/policies', v)
          message.success('已保存')
          setOpen(false)
          void load()
        }}>
          <Form.Item name="instance_id" label="平台实例 ID"><Input /></Form.Item>
          <Form.Item name="cloud_instance_id" label="云实例 ID"><Input /></Form.Item>
          <Form.Item name="account_id" label="云账号 ID"><Input /></Form.Item>
          <Form.Item name="action" label="动作">
            <Select options={['restart', 'switchover', 'scale_spec', 'scale_disk'].map((v) => ({ value: v, label: v }))} />
          </Form.Item>
          <Form.Item name="severity" label="告警级别"><Input placeholder="空为不限，如 P1" /></Form.Item>
          <Form.Item name="rule_keyword" label="规则关键字"><Input /></Form.Item>
          <Form.Item name="mode" label="执行方式">
            <Select options={[{ value: 'ticket', label: '只建工单' }, { value: 'auto', label: '自动执行' }]} />
          </Form.Item>
          <Form.Item name="enabled" label="开启" valuePropName="checked"><Switch /></Form.Item>
          <Form.Item name="spec_cap_cpu" label="升配 CPU 上限"><InputNumber min={0} /></Form.Item>
          <Form.Item name="spec_cap_memory" label="升配内存 MB 上限"><InputNumber min={0} /></Form.Item>
          <Form.Item name="disk_step_gb" label="单次扩盘 GB"><InputNumber min={1} /></Form.Item>
          <Form.Item name="cooldown_min" label="冷却分钟"><InputNumber min={0} /></Form.Item>
          <Form.Item name="daily_max" label="每日次数"><InputNumber min={0} /></Form.Item>
          <Form.Item name="window" label="维护窗口" extra="如 01:00-05:00，空表示全天"><Input /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
