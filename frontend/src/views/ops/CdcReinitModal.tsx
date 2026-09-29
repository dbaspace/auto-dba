import React, { useEffect, useRef, useState } from 'react'
import { Alert, Checkbox, Collapse, Modal, Radio, Select, Space, Spin, Tag, Typography, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { formatDebeziumConfig } from './cdc'

const { Text } = Typography

export const REINIT_RESOURCE_LABELS: Record<string, string> = {
  publication: 'Publication',
  slot: 'Replication Slot',
  kafka_topic: 'Kafka Topic',
  connector: 'Debezium Connector',
  routine_load: 'Routine Load',
  mapping: '表映射',
}

const ACTION_META: Record<string, { label: string; color: string; dashed?: boolean }> = {
  skip: { label: '跳过', color: 'default' },
  create: { label: '创建', color: 'blue' },
  update: { label: '覆盖/补齐', color: 'orange' },
  resume: { label: 'RESUME', color: 'green' },
  warn: { label: '需人工处理', color: 'red' },
  unknown: { label: '待定', color: 'default', dashed: true },
}

export function ReinitActionTag({ action }: { action: string }) {
  const meta = ACTION_META[action] || { label: action || '—', color: 'default' }
  return (
    <Tag color={meta.color} style={meta.dashed ? { borderStyle: 'dashed' } : undefined}>
      {meta.label}
    </Tag>
  )
}

export function snapshotModeText(v: string): string {
  return { never: '从位点续传（never）', initial: '全量重做（initial）' }[v] || v || '—'
}

interface Props {
  open: boolean
  task: any
  onClose: () => void
  onSubmitted?: () => void
}

type SnapshotChoice = '' | 'never' | 'initial'

export default function CdcReinitModal({ open, task, onClose, onSubmitted }: Props) {
  const [snapshot, setSnapshot] = useState<SnapshotChoice>('')
  const [resumeRL, setResumeRL] = useState(true)
  const [connectorID, setConnectorID] = useState<string>('')
  const [connectors, setConnectors] = useState<any[]>([])
  const [preview, setPreview] = useState<any>(null)
  const [previewErr, setPreviewErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const seq = useRef(0)

  const taskID = task?.id

  useEffect(() => {
    if (!open) return
    setSnapshot('')
    setResumeRL(true)
    setConnectorID('')
    setPreview(null)
    setPreviewErr('')
    api
      .get('/connectors', { silent: true })
      .then(({ data }) => setConnectors(data.data || []))
      .catch(() => setConnectors([]))
  }, [open, taskID])

  function body() {
    const b: Record<string, any> = { resume_routine_loads: resumeRL }
    if (snapshot) b.snapshot_mode = snapshot
    if (connectorID) b.connector_id = connectorID
    return b
  }

  useEffect(() => {
    if (!open || !taskID) return
    const my = ++seq.current
    setLoading(true)
    setPreviewErr('')
    api
      .post(`/cdc-sync/tasks/${taskID}/reinit/preview`, body(), { silent: true, timeout: 60000 })
      .then(({ data }) => {
        if (my === seq.current) setPreview(data.data || null)
      })
      .catch((e: any) => {
        if (my !== seq.current) return
        setPreview(null)
        setPreviewErr(e?.response?.data?.message || e?.message || '预览失败')
      })
      .finally(() => {
        if (my === seq.current) setLoading(false)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, taskID, snapshot, resumeRL, connectorID])

  async function submit() {
    setSubmitting(true)
    try {
      await api.post(`/cdc-sync/tasks/${taskID}/reinit`, body())
      message.success('已提交，正在重新初始化')
      onClose()
      onSubmitted?.()
    } catch {
      /* 拦截器已提示 */
    } finally {
      setSubmitting(false)
    }
  }

  const effective = String(preview?.snapshot_mode || '')
  const currentConnectorID = String(preview?.connector_id || '')
  const connectorOptions = connectors.map((c: any) => ({
    value: String(c.id),
    label: `${c.dbz_host || c.id}${c.env ? `（${c.env}）` : ''}${c.kafka_host ? ` · Kafka ${c.kafka_host}` : ''}`,
  }))

  const columns = [
    {
      title: '资源',
      key: 'resource',
      width: 150,
      render: (_: any, r: any) => REINIT_RESOURCE_LABELS[r.resource] || r.resource,
    },
    { title: '名称', dataIndex: 'name', key: 'name', width: 220, ellipsis: true },
    { title: '所属库', dataIndex: 'db_name', key: 'db_name', width: 120, render: (v: string) => v || '—' },
    {
      title: '已存在',
      key: 'exists',
      width: 80,
      render: (_: any, r: any) => (r.exists ? <Tag color="success">是</Tag> : <Tag>否</Tag>),
    },
    { title: '动作', key: 'action', width: 110, render: (_: any, r: any) => <ReinitActionTag action={r.action} /> },
    { title: '说明', dataIndex: 'message', key: 'message' },
  ]

  return (
    <Modal
      open={open}
      title={`重新初始化「${task?.name || taskID || ''}」`}
      width={1000}
      onCancel={onClose}
      okText="确认重新初始化"
      cancelText="取消"
      okButtonProps={{ danger: effective === 'initial', disabled: !preview || loading, loading: submitting }}
      onOk={submit}
      destroyOnClose
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="按任务原提交配置逐项补齐 Publication / Slot / Kafka Topic / Debezium Connector / StarRocks，已存在的跳过；不走审批，后台异步执行。"
      />
      <Space direction="vertical" size={10} style={{ width: '100%', marginBottom: 12 }}>
        <div>
          <Text strong style={{ marginRight: 12 }}>
            快照模式
          </Text>
          <Radio.Group value={snapshot} onChange={(e) => setSnapshot(e.target.value)}>
            <Radio value="">
              沿用原配置
              {preview && !snapshot ? (
                <Text type={effective === 'initial' ? 'danger' : 'secondary'}>（{effective || 'never'}）</Text>
              ) : null}
            </Radio>
            <Radio value="never">从位点续传（never）</Radio>
            <Radio value="initial">
              <Text type={snapshot === 'initial' ? 'danger' : undefined}>全量重做（initial）</Text>
            </Radio>
          </Radio.Group>
        </div>
        {effective === 'initial' && (
          <Alert type="error" showIcon message="生效的 snapshot_mode=initial：Connect 无 offset 时会对全部表重做全量快照" />
        )}
        <Checkbox checked={resumeRL} onChange={(e) => setResumeRL(e.target.checked)}>
          恢复已暂停的 Routine Load
        </Checkbox>
        <div>
          <Text strong style={{ marginRight: 12 }}>
            换绑 Debezium 服务
          </Text>
          <Select
            allowClear
            style={{ width: 520 }}
            placeholder="不换绑（沿用当前服务，并按注册表最新 dbz_host 刷新地址）"
            value={connectorID || undefined}
            onChange={(v) => setConnectorID(v ? String(v) : '')}
            options={connectorOptions.filter((o) => o.value !== currentConnectorID || connectorID === o.value)}
          />
        </div>
        {preview && (
          <Text type="secondary">
            Connect：{preview.connect_base_url_old || '—'}
            {preview.connect_base_url_new && preview.connect_base_url_new !== preview.connect_base_url_old
              ? ` → ${preview.connect_base_url_new}`
              : ''}
            ；表映射 {preview.mapping_count ?? 0} 个
            {preview.init_incomplete ? '；初始化未完成，将按原配置补齐 StarRocks' : ''}
          </Text>
        )}
      </Space>

      {previewErr && <Alert type="error" showIcon style={{ marginBottom: 12 }} message={previewErr} />}
      <Spin spinning={loading}>
        {(preview?.warnings || []).length > 0 && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="注意"
            description={
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {(preview.warnings || []).map((w: any, i: number) => (
                  <li key={`${w.code}-${i}`}>{w.message}</li>
                ))}
              </ul>
            }
          />
        )}
        <ResponsiveTable
          rowKey={(r: any) => `${r.resource}|${r.db_name || ''}|${r.name}`}
          size="small"
          columns={columns}
          dataSource={preview?.items || []}
          pagination={false}
          scroll={{ y: 320 }}
        />
        {(preview?.configs || []).length > 0 && (
          <Collapse
            style={{ marginTop: 12 }}
            items={(preview.configs || []).map((c: any) => ({
              key: c.connector_name,
              label: `将下发的 Connector 配置：${c.connector_name}${c.db_name ? `（${c.db_name}）` : ''}`,
              children: (
                <pre style={{ maxHeight: 260, overflow: 'auto', margin: 0, fontSize: 12 }}>
                  {formatDebeziumConfig(c.config)}
                </pre>
              ),
            }))}
          />
        )}
      </Spin>
    </Modal>
  )
}
