import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, AutoComplete, Button, DatePicker, Form, Input, InputNumber, Select, Space, message } from 'antd'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import './KafkaOrder.css'

const TOPIC_RE = /^[a-zA-Z0-9._-]+$/

function emptyForm() {
  return {
    env: '',
    instance_id: '',
    action: 'create_topic',
    topic: '',
    partition: undefined as number | undefined,
    replication: undefined as number | undefined,
    cleanup_policy: 'delete',
    retention: 86400000 as number | undefined,
    assigned_to: '',
    description: '',
    scheduled_at: '' as string,
  }
}

function instLabel(i: any) {
  const addr = i.host ? `${i.host}:${i.port}` : i.domain_name || ''
  return i.name ? `${i.name} — ${addr}` : addr
}

export default function KafkaOrder() {
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()
  const [instances, setInstances] = useState<any[]>([])
  const [instLoading, setInstLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => {
      const next = { ...f, ...patch }
      antdForm.setFieldsValue({
        ...next,
        scheduled_at: next.scheduled_at ? dayjs(next.scheduled_at, 'YYYY-MM-DD HH:mm') : null,
      })
      return next
    })
  }

  const { restoreDraft, saveNow, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'kafka',
    label: 'Kafka工单',
    path: '/ops/kafka',
    getValues: () => formRef.current,
    applyValues: (d) => {
      const next = { ...emptyForm(), ...d }
      setForm(next)
      antdForm.setFieldsValue({
        ...next,
        scheduled_at: next.scheduled_at ? dayjs(next.scheduled_at, 'YYYY-MM-DD HH:mm') : null,
      })
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  const envOptions = useMemo(() => {
    const set = new Set(['test', 'prod'])
    for (const i of instances) {
      if (i.idc) set.add(i.idc)
    }
    return [...set]
  }, [instances])

  const kafkaOptions = useMemo(() => {
    if (!form.env) return []
    return instances.filter((i) => !i.idc || i.idc === form.env)
  }, [instances, form.env])

  const currentInst = useMemo(
    () => instances.find((i) => i.id === form.instance_id),
    [instances, form.instance_id],
  )
  const approvers = useMemo(() => currentInst?.approvers || [], [currentInst])
  const approverPlaceholder = !form.instance_id
    ? '请先选择 Kafka 地址'
    : !approvers.length
      ? '该 Kafka 未配置审批人员，可手动填写或到实例管理维护'
      : '选填，来自实例中配置的审批人员'

  function onEnvChange(env: string) {
    patchForm({ env, instance_id: '', assigned_to: '' })
  }

  function onInstChange(instance_id: string) {
    const inst = instances.find((i) => i.id === instance_id)
    const first = (inst?.approvers || [])[0] || ''
    patchForm({ instance_id, assigned_to: first })
  }

  async function loadInst() {
    setInstLoading(true)
    try {
      const { data } = await api.get('/kafka/instances')
      setInstances(data.data || [])
    } finally {
      setInstLoading(false)
    }
  }

  function reset() {
    const next = emptyForm()
    setForm(next)
    antdForm.resetFields()
    antdForm.setFieldsValue(next)
  }

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    const f = formRef.current
    const inst = instances.find((i) => i.id === f.instance_id)
    const extra: string[] = []
    if (f.action === 'create_topic') {
      extra.push(
        `partition=${f.partition}`,
        `rf=${f.replication}`,
        `cleanup=${f.cleanup_policy}`,
        `retention=${f.retention}`,
      )
    }
    if (f.scheduled_at) extra.push(`scheduled=${f.scheduled_at}`)
    setSubmitting(true)
    try {
      await api.post('/kafka/orders', {
        cluster: inst ? instLabel(inst) : f.instance_id,
        topic: f.topic,
        action: f.action,
        assigned_to: f.assigned_to,
        description: extra.length ? `${f.description}\n${extra.join(' ')}` : f.description,
      })
      message.success('工单创建成功')
      clearOnSubmitSuccess()
      reset()
      navigate('/ops/tickets')
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      await loadInst()
      if (!cancelled) restoreDraft()
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const isCreate = form.action === 'create_topic'

  return (
    <div className="kafka-order order-page">
      <Form
        form={antdForm}
        layout="vertical"
        className="order-split kafka-split-form"
        initialValues={form}
        onValuesChange={(_, all) => {
          const scheduled =
            all.scheduled_at && dayjs.isDayjs(all.scheduled_at)
              ? (all.scheduled_at as Dayjs).format('YYYY-MM-DD HH:mm')
              : all.scheduled_at || ''
          setForm((f) => ({ ...f, ...all, scheduled_at: scheduled }))
        }}
        onSubmitCapture={(e) => e.preventDefault()}
      >
        <aside className="order-aside">
          {form.env && !instLoading && !kafkaOptions.length ? (
            <Alert
              type="warning"
              showIcon
              className="mb"
              message="当前环境暂无可用 Kafka 资源，请先在「实例」中登记 db_type=kafka 的实例"
            />
          ) : null}
          <Form.Item name="env" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              allowClear
              showSearch
              placeholder="请选择环境"
              onChange={onEnvChange}
              options={envOptions.map((e) => ({ value: e, label: e }))}
            />
          </Form.Item>
          <Form.Item
            name="instance_id"
            label="Kafka 地址"
            rules={[{ required: true, message: '请选择 Kafka 地址' }]}
          >
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              disabled={!form.env}
              loading={instLoading}
              placeholder="选择环境的 kafka 地址"
              onChange={onInstChange}
              options={kafkaOptions.map((i) => ({ value: i.id, label: instLabel(i) }))}
            />
          </Form.Item>
          <Form.Item name="action" label="工单类型" rules={[{ required: true, message: '请选择工单类型' }]}>
            <Select
              options={[
                { value: 'create_topic', label: 'Create Topic' },
                { value: 'delete_topic', label: 'Delete Topic' },
              ]}
            />
          </Form.Item>
          {isCreate ? (
            <>
              <Form.Item
                name="partition"
                label="Partition"
                rules={[{ required: true, message: '请输入分区数' }]}
              >
                <InputNumber min={1} max={100000} precision={0} style={{ width: '100%' }} placeholder="分区" />
              </Form.Item>
              <Form.Item
                name="replication"
                label="Replication Factor"
                rules={[{ required: true, message: '请输入副本数' }]}
              >
                <InputNumber min={1} max={32} precision={0} style={{ width: '100%' }} placeholder="副本数" />
              </Form.Item>
              <Form.Item
                name="cleanup_policy"
                label="Cleanup Policy"
                rules={[{ required: true, message: '请选择 Cleanup Policy' }]}
              >
                <Select
                  options={[
                    { value: 'delete', label: 'Delete' },
                    { value: 'compact', label: 'Compact' },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="retention"
                label="Retention"
                rules={[{ required: true, message: '请输入 Retention（毫秒）' }]}
              >
                <InputNumber
                  min={1}
                  precision={0}
                  style={{ width: '100%' }}
                  placeholder="毫秒，如 86400000"
                />
              </Form.Item>
            </>
          ) : null}
          <Form.Item name="assigned_to" label="审核人">
            <AutoComplete
              allowClear
              placeholder={approverPlaceholder}
              options={approvers.map((a: string) => ({ value: a, label: a }))}
              filterOption={(input, option) =>
                String(option?.value || '')
                  .toLowerCase()
                  .includes(String(input || '').toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item
            name="description"
            label="工单说明"
            rules={[{ required: true, message: '请填写工单说明' }]}
          >
            <Input.TextArea rows={3} placeholder="请输入业务背景及申请原因" />
          </Form.Item>
          <Form.Item name="scheduled_at" label="定时执行" getValueFromEvent={(v: Dayjs | null) => v}>
            <DatePicker
              showTime={{ format: 'HH:mm' }}
              format="YYYY-MM-DD HH:mm"
              style={{ width: '100%' }}
              placeholder="选择时间"
            />
          </Form.Item>
          <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
          <Form.Item className="aside-actions">
            <Space>
              <Button type="primary" loading={submitting} onClick={submit}>
                提交工单
              </Button>
              <Button onClick={() => saveNow()}>保存草稿</Button>
              <Button onClick={reset}>重置</Button>
            </Space>
          </Form.Item>
        </aside>

        <main className="order-main">
          <div className="kafka-main-title">Topic 名</div>
          <p className="kafka-topic-hint">多个 topic 用逗号隔开；仅支持字母、数字、点、下划线、横线</p>
          <Form.Item
            name="topic"
            className="kafka-topic-item"
            rules={[
              { required: true, message: '请输入 Topic 名' },
              {
                validator: async (_, val) => {
                  const parts = String(val || '')
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean)
                  if (!parts.length) throw new Error('请输入 Topic 名')
                  for (const t of parts) {
                    if (!TOPIC_RE.test(t)) {
                      throw new Error(`Topic 名称仅支持字母、数字、点、下划线、横线：${t}`)
                    }
                  }
                },
              },
            ]}
          >
            <Input.TextArea className="kafka-topic-editor" placeholder="例如：orders,orders_dlq" />
          </Form.Item>
        </main>
      </Form>
    </div>
  )
}
