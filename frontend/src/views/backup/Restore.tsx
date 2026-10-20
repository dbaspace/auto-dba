import React, { useEffect, useMemo, useState } from 'react'
import { Button, Checkbox, DatePicker, Form, Input, Select, Space, message } from 'antd'
import dayjs from 'dayjs'
import { useNavigate } from 'react-router-dom'
import api from '../../api/request'
import './backup.css'

export default function BackupRestore() {
  const [form] = Form.useForm()
  const navigate = useNavigate()
  const [runs, setRuns] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [submitting, setSubmitting] = useState(false)
  const restoreKind = Form.useWatch('restore_kind', form) || 'logical'
  const sourceRunId = Form.useWatch('source_run_id', form)

  useEffect(() => {
    ;(async () => {
      const [r, inst] = await Promise.all([
        api.get('/backup/runs', { params: { status: 'success', limit: 200 } }),
        api.get('/instances', { params: { limit: 500 } }),
      ])
      setRuns(r.data.data || [])
      setInstances(inst.data.data || inst.data || [])
    })().catch((e) => message.error(e?.message || '加载失败'))
  }, [])

  const selectedRun = useMemo(() => runs.find((x) => x.id === sourceRunId), [runs, sourceRunId])
  const isPhysicalBase = useMemo(() => {
    const m = String(selectedRun?.method || '')
    return m.startsWith('physical')
  }, [selectedRun])

  useEffect(() => {
    if (!selectedRun) return
    const m = String(selectedRun.method || 'logical_full')
    if (m.startsWith('physical')) {
      const cur = form.getFieldValue('restore_kind')
      if (cur !== 'physical' && cur !== 'pitr') {
        form.setFieldsValue({ restore_kind: 'physical' })
      }
    } else {
      form.setFieldsValue({ restore_kind: 'logical' })
    }
  }, [selectedRun, form])

  async function onFinish(values: any) {
    setSubmitting(true)
    try {
      const body: any = {
        source_run_id: values.source_run_id,
        target_instance_id: values.target_instance_id,
        target_db: values.target_db,
        description: values.description,
        confirm_prod: true,
        restore_kind: values.restore_kind || 'logical',
      }
      if (body.restore_kind === 'physical' || body.restore_kind === 'pitr') {
        body.data_dir = values.data_dir
        body.confirm_stop = !!values.confirm_stop
      }
      if (body.restore_kind === 'pitr') {
        const rt = values.recover_to
        body.recover_to = dayjs.isDayjs(rt) ? rt.toISOString() : String(rt || '')
        body.wal_or_binlog_dir = values.wal_or_binlog_dir
      }
      const { data } = await api.post('/backup/restore', body)
      const issue = data?.data?.issue
      message.success('已创建恢复工单')
      if (issue?.id) {
        navigate(`/ops/tickets/${issue.id}`)
      } else {
        navigate('/ops/tickets')
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  const kindOptions = isPhysicalBase
    ? [
        { value: 'physical', label: '物理全量恢复' },
        { value: 'pitr', label: 'PITR（物理基线 + WAL/binlog）' },
      ]
    : [{ value: 'logical', label: '逻辑恢复' }]

  return (
    <div className="backup-page" style={{ maxWidth: 720 }}>
      <h3 style={{ marginBottom: 16 }}>备份恢复</h3>
      <p style={{ color: '#666', marginBottom: 24 }}>
        逻辑恢复：Agent 执行 pg_restore / mysql 导入。物理全量：将备份目录恢复到 Agent 本机 data_dir（须先停库并清空目录）。
        PITR：在物理基线上挂载本机已归档的 WAL/binlog 目录，追到指定时间点（须自行保证连续归档）。
      </p>
      <Form form={form} layout="vertical" onFinish={onFinish} initialValues={{ restore_kind: 'logical' }}>
        <Form.Item name="source_run_id" label="备份记录" rules={[{ required: true, message: '请选择备份记录' }]}>
          <Select
            showSearch
            optionFilterProp="label"
            options={runs.map((r) => ({
              value: r.id,
              label: `${r.policy_name || r.id} · ${r.method || 'logical'} · ${r.engine || ''} · ${r.finished_at || r.started_at || ''}`,
            }))}
          />
        </Form.Item>
        <Form.Item name="restore_kind" label="恢复方式" rules={[{ required: true }]}>
          <Select options={kindOptions} />
        </Form.Item>
        <Form.Item name="target_instance_id" label="目标实例" rules={[{ required: true, message: '请选择目标实例' }]}>
          <Select
            showSearch
            optionFilterProp="label"
            options={instances.map((i: any) => ({
              value: i.id,
              label: `${i.name || i.host} (${i.db_type || ''})`,
            }))}
          />
        </Form.Item>
        {restoreKind === 'logical' ? (
          <Form.Item name="target_db" label="目标库名" rules={[{ required: true, message: '请填写目标库' }]}>
            <Input placeholder="恢复写入的数据库名" />
          </Form.Item>
        ) : (
          <>
            <Form.Item
              name="data_dir"
              label="Agent 本机数据目录"
              rules={[{ required: true, message: '请填写绝对路径 data_dir' }]}
              extra="例如 /var/lib/postgresql/data 或 /var/lib/mysql；须为空且实例已停库"
            >
              <Input placeholder="/absolute/path/to/datadir" />
            </Form.Item>
            {restoreKind === 'pitr' ? (
              <>
                <Form.Item
                  name="recover_to"
                  label="恢复到时间点"
                  rules={[{ required: true, message: '请选择 recover_to' }]}
                  extra="RFC3339，将写入 recovery 目标（PG）或 mysqlbinlog --stop-datetime（MySQL）"
                >
                  <DatePicker showTime style={{ width: '100%' }} />
                </Form.Item>
                <Form.Item
                  name="wal_or_binlog_dir"
                  label="WAL / binlog 目录（Agent 本机）"
                  rules={[{ required: true, message: '请填写连续归档目录' }]}
                  extra="PG：含已归档 WAL 段；MySQL：含连续 binlog 文件。须已提前放到 Agent 可读路径"
                >
                  <Input placeholder="/absolute/path/to/wal_or_binlog" />
                </Form.Item>
              </>
            ) : null}
            <Form.Item name="confirm_stop" valuePropName="checked" rules={[{
              validator: (_, v) => (v ? Promise.resolve() : Promise.reject(new Error('须确认已停库'))),
            }]}>
              <Checkbox>已确认目标实例停库，且 data_dir 可被覆盖</Checkbox>
            </Form.Item>
          </>
        )}
        <Form.Item name="description" label="说明">
          <Input.TextArea rows={3} placeholder="可选" />
        </Form.Item>
        <Space>
          <Button type="primary" htmlType="submit" loading={submitting}>
            提交恢复工单
          </Button>
          <Button onClick={() => navigate('/ops/backup/runs')}>返回执行记录</Button>
        </Space>
      </Form>
    </div>
  )
}
