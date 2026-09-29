import React, { useEffect, useRef, useState } from 'react'
import { Alert, Checkbox, Modal, Radio, Select, Space, Tag, Typography, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { statusTagColor, statusText } from './cdc'

const { Text } = Typography

interface Props {
  open: boolean
  onClose: () => void
  onSubmitted?: () => void
}

type SnapshotChoice = '' | 'never' | 'initial'

export default function CdcReinitBatchModal({ open, onClose, onSubmitted }: Props) {
  const [connectors, setConnectors] = useState<any[]>([])
  const [connectorID, setConnectorID] = useState('')
  const [cands, setCands] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [snapshot, setSnapshot] = useState<SnapshotChoice>('')
  const [resumeRL, setResumeRL] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [results, setResults] = useState<any[] | null>(null)
  const [progress, setProgress] = useState<Record<string, any>>({})
  const timer = useRef<number | null>(null)

  useEffect(() => {
    if (!open) return
    setConnectorID('')
    setCands(null)
    setSelected([])
    setSnapshot('')
    setResumeRL(true)
    setResults(null)
    setProgress({})
    api
      .get('/connectors', { silent: true })
      .then(({ data }) => setConnectors(data.data || []))
      .catch(() => setConnectors([]))
  }, [open])

  useEffect(() => {
    if (!open || !connectorID) return
    setLoading(true)
    setResults(null)
    api
      .get('/cdc-sync/tasks/reinit/candidates', { params: { connector_id: connectorID } })
      .then(({ data }) => {
        const d = data.data || {}
        setCands(d)
        setSelected((d.list || []).filter((r: any) => r.can_reinit && !r.connector_exists).map((r: any) => r.task_id))
      })
      .catch(() => setCands(null))
      .finally(() => setLoading(false))
  }, [open, connectorID])

  useEffect(() => {
    if (timer.current) window.clearInterval(timer.current)
    timer.current = null
    const ids = (results || []).filter((r) => r.accepted).map((r) => r.task_id)
    if (!open || !ids.length) return
    const poll = async () => {
      const next: Record<string, any> = {}
      await Promise.all(
        ids.map(async (id: string) => {
          const { data } = await api.get(`/cdc-sync/tasks/${id}`, { silent: true }).catch(() => ({ data: {} as any }))
          if (data?.data) next[id] = data.data
        }),
      )
      setProgress((prev) => ({ ...prev, ...next }))
      if (ids.every((id: string) => next[id] && next[id].status !== 'reinitializing')) {
        if (timer.current) window.clearInterval(timer.current)
        timer.current = null
        onSubmitted?.()
      }
    }
    void poll()
    timer.current = window.setInterval(poll, 3000)
    return () => {
      if (timer.current) window.clearInterval(timer.current)
      timer.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [results, open])

  async function submit() {
    if (!selected.length) {
      message.warning('请至少勾选一个任务')
      return
    }
    setSubmitting(true)
    try {
      const b: Record<string, any> = { connector_id: connectorID, task_ids: selected, resume_routine_loads: resumeRL }
      if (snapshot) b.snapshot_mode = snapshot
      const { data } = await api.post('/cdc-sync/tasks/reinit/batch', b)
      const d = data.data || {}
      setResults(d.list || [])
      message.success(`已受理 ${d.accepted || 0} 个，拒绝 ${d.rejected || 0} 个`)
      onSubmitted?.()
    } catch {
      /* 拦截器已提示 */
    } finally {
      setSubmitting(false)
    }
  }

  const candColumns = [
    { title: '任务', dataIndex: 'task_name', key: 'task_name', width: 200, ellipsis: true },
    { title: '环境', dataIndex: 'env', key: 'env', width: 80, render: (v: string) => v || '—' },
    {
      title: '状态',
      key: 'status',
      width: 110,
      render: (_: any, r: any) => <Tag color={statusTagColor(r.status)}>{statusText(r.status)}</Tag>,
    },
    {
      title: 'Connector',
      key: 'connector',
      width: 220,
      ellipsis: true,
      render: (_: any, r: any) => (r.connector_names || []).join(', ') || r.connector_name || '—',
    },
    { title: '源库', dataIndex: 'source_db_name', key: 'source_db_name', width: 120 },
    {
      title: 'Connect 上',
      key: 'exists',
      width: 100,
      render: (_: any, r: any) =>
        !cands?.connect_reachable ? <Tag>未知</Tag> : r.connector_exists ? <Tag color="success">存在</Tag> : <Tag color="error">缺失</Tag>,
    },
    { title: '说明', dataIndex: 'message', key: 'message', render: (v: string) => v || '—' },
  ]

  const resultColumns = [
    { title: '任务', key: 'task', width: 220, render: (_: any, r: any) => r.task_name || r.task_id },
    {
      title: '受理',
      key: 'accepted',
      width: 80,
      render: (_: any, r: any) => (r.accepted ? <Tag color="blue">已受理</Tag> : <Tag color="error">拒绝</Tag>),
    },
    {
      title: '当前状态',
      key: 'status',
      width: 130,
      render: (_: any, r: any) => {
        const st = progress[r.task_id]?.status || r.status
        return st ? <Tag color={statusTagColor(st)}>{statusText(st)}</Tag> : '—'
      },
    },
    { title: '说明', dataIndex: 'message', key: 'message', render: (v: string) => v || '—' },
  ]

  return (
    <Modal
      open={open}
      title="按 Debezium 服务重新初始化"
      width={1100}
      onCancel={onClose}
      okText={results ? '关闭' : '提交'}
      cancelText="取消"
      okButtonProps={{ loading: submitting, disabled: !results && (!connectorID || !selected.length) }}
      onOk={results ? onClose : submit}
      destroyOnClose
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="适用于某个 Debezium 服务被删除重建后，批量恢复挂在它上面的任务。受理的任务在后台串行执行；批量不支持换绑，需要换绑请在单任务里操作。"
      />
      <Space direction="vertical" size={10} style={{ width: '100%', marginBottom: 12 }}>
        <div>
          <Text strong style={{ marginRight: 12 }}>
            Debezium 服务
          </Text>
          <Select
            style={{ width: 520 }}
            placeholder="选择 Debezium 服务"
            value={connectorID || undefined}
            disabled={!!results}
            onChange={(v) => setConnectorID(String(v || ''))}
            options={connectors.map((c: any) => ({
              value: String(c.id),
              label: `${c.dbz_host || c.id}${c.env ? `（${c.env}）` : ''}`,
            }))}
          />
        </div>
        <div>
          <Text strong style={{ marginRight: 12 }}>
            快照模式
          </Text>
          <Radio.Group value={snapshot} disabled={!!results} onChange={(e) => setSnapshot(e.target.value)}>
            <Radio value="">沿用各任务原配置</Radio>
            <Radio value="never">从位点续传（never）</Radio>
            <Radio value="initial">
              <Text type={snapshot === 'initial' ? 'danger' : undefined}>全量重做（initial）</Text>
            </Radio>
          </Radio.Group>
        </div>
        <Checkbox checked={resumeRL} disabled={!!results} onChange={(e) => setResumeRL(e.target.checked)}>
          恢复已暂停的 Routine Load
        </Checkbox>
        {cands && (
          <Text type={cands.connect_reachable ? 'secondary' : 'danger'}>
            Connect：{cands.connect_base_url || '—'}
            {cands.connect_reachable ? '（可达）' : `（不可达：${cands.message || '未知原因'}）`}
          </Text>
        )}
      </Space>
      {results ? (
        <ResponsiveTable rowKey="task_id" size="small" columns={resultColumns} dataSource={results} pagination={false} />
      ) : (
        <ResponsiveTable
          rowKey="task_id"
          size="small"
          loading={loading}
          columns={candColumns}
          dataSource={cands?.list || []}
          pagination={false}
          scroll={{ y: 360 }}
          rowSelection={{
            selectedRowKeys: selected,
            onChange: (keys) => setSelected(keys.map(String)),
            getCheckboxProps: (r: any) => ({ disabled: !r.can_reinit }),
          }}
          locale={{ emptyText: connectorID ? '该服务下没有 init 任务' : '请先选择 Debezium 服务' }}
        />
      )}
    </Modal>
  )
}
