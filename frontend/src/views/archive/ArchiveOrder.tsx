import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Col, Form, Input, Radio, Row, Select, message } from 'antd'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import { encodeCondition } from './archive.ts'
import './archive.css'

type OrderForm = {
  db_type: string
  source_instance_id: string
  source_db: string
  source_table: string
  target_instance_id: string
  target_db: string
  target_table: string
  table_type: string
  archive_condition: string
  run_mode: string
  schedule_expr: string
  approver: string
}

function emptyForm(): OrderForm {
  return {
    db_type: '',
    source_instance_id: '',
    source_db: '',
    source_table: '',
    target_instance_id: '',
    target_db: '',
    target_table: '',
    table_type: '',
    archive_condition: '',
    run_mode: 'immediate',
    schedule_expr: '',
    approver: '',
  }
}

function typeKey(t: any): string {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'mssql') return 'sqlserver'
  return n
}

function domainLabel(i: any): string {
  const domain = (i.domains && i.domains[0]) || i.domain_name || ''
  const addr = i.host ? `${i.host}:${i.port}` : ''
  const main = domain || addr || i.name
  return i.name && main !== i.name ? `${i.name} — ${main}` : main
}

function parseTypes(raw: any): { value: string; label: string }[] {
  try {
    const arr = JSON.parse(String(raw || '').trim() || '[]')
    if (!Array.isArray(arr)) return []
    return arr
      .map((it: any) => ({
        value: String(it?.value || '').trim(),
        label: String(it?.label || it?.value || '').trim(),
      }))
      .filter((it: any) => it.value)
  } catch {
    return []
  }
}

export default function ArchiveOrder() {
  const navigate = useNavigate()
  const [form, setForm] = useState<OrderForm>(emptyForm())
  const [saving, setSaving] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [approvers, setApprovers] = useState<string[]>([])
  const [dbTypes, setDbTypes] = useState<{ value: string; label: string }[]>([
    { value: 'mysql', label: 'MySQL' },
    { value: 'postgres', label: 'PostgreSQL' },
    { value: 'oracle', label: 'Oracle' },
    { value: 'sqlserver', label: 'SQL Server' },
  ])
  const [errors, setErrors] = useState<Record<string, string>>({})

  const formRef = useRef<OrderForm>(form)
  formRef.current = form

  function patch(p: Partial<OrderForm>) {
    setForm((prev) => ({ ...prev, ...p }))
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'archive',
    label: '归档工单',
    path: '/ops/archive/apply',
    getValues: () => ({ ...formRef.current }),
    applyValues: (d) => {
      setForm({ ...emptyForm(), ...d })
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  const sourceInstances = useMemo(() => {
    if (!form.db_type) return instances
    const want = typeKey(form.db_type)
    return instances.filter((i) => typeKey(i.db_type) === want)
  }, [instances, form.db_type])

  function onTypeChange(v: string) {
    setForm((prev) => {
      const list = v
        ? instances.filter((i) => typeKey(i.db_type) === typeKey(v))
        : instances
      const still = list.some((i) => i.id === prev.source_instance_id)
      return { ...prev, db_type: v, source_instance_id: still ? prev.source_instance_id : '' }
    })
  }

  function reset() {
    setForm(emptyForm())
    setErrors({})
  }

  function validate(): boolean {
    const f = formRef.current
    const errs: Record<string, string> = {}
    if (!String(f.db_type || '').trim()) errs.db_type = '请选择数据库类型'
    if (!String(f.source_instance_id || '').trim())
      errs.source_instance_id = '请输入源端数据库域名'
    if (!String(f.source_table || '').trim()) errs.source_table = '请输入源端表名'
    if (!String(f.table_type || '').trim()) errs.table_type = '请选择库表类型'
    if (!String(f.run_mode || '').trim()) errs.run_mode = '请选择执行方式'
    if (f.run_mode === 'scheduled' && !String(f.schedule_expr || '').trim())
      errs.schedule_expr = '请填写调度配置'
    if (!String(f.approver || '').trim()) errs.approver = '请选择审核人'
    setErrors(errs)
    return Object.keys(errs).length === 0
  }

  async function submit() {
    if (!validate()) return
    const f = formRef.current
    setSaving(true)
    try {
      const tgt = instances.find((i) => i.id === f.target_instance_id)
      const name = [f.source_db, f.source_table].filter(Boolean).join('.') || f.source_table
      const extra = [
        `-- table_type:${f.table_type}`,
        `-- target:${tgt ? domainLabel(tgt) : ''}/${f.target_db || ''}/${f.target_table || ''}`,
        f.archive_condition.trim(),
      ]
        .filter(Boolean)
        .join('\n')
      await api.post('/archive/tasks', {
        name: `${name}_归档`,
        source_instance_id: f.source_instance_id,
        source_db: f.source_db,
        table_name: f.source_table,
        schedule_expr: f.run_mode === 'scheduled' ? f.schedule_expr : '',
        approver: f.approver,
        condition: encodeCondition(
          extra,
          f.table_type === 'full' ? 'backup_target' : 'delete_only',
          f.run_mode,
          f.schedule_expr,
        ),
      })
      message.success('工单已提交，审批通过后按任务执行方式运行')
      clearOnSubmitSuccess()
      reset()
      navigate('/ops/tickets?type=archive')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      restoreDraft()
      const [instRes, argRes] = await Promise.all([
        api.get('/instances'),
        api.get('/global-args'),
      ])
      setInstances(instRes.data.data || [])
      const args = argRes.data.data || []
      const raw = args.find((a: any) => a.key === 'ticket_instance_types')?.value || ''
      const parsed = parseTypes(raw)
      if (parsed.length) setDbTypes(parsed)
      const set = new Set<string>()
      String(args.find((a: any) => a.key === 'migration_approver_user_ids')?.value || '')
        .split(/[,，\s]+/)
        .map((s: string) => s.trim())
        .filter(Boolean)
        .forEach((u: string) => set.add(u))
      const def = String(args.find((a: any) => a.key === 'sync_default_approver')?.value || '').trim()
      if (def) set.add(def)
      const list = [...set]
      setApprovers(list)
      setForm((prev) => (!prev.approver && list.length ? { ...prev, approver: list[0] } : prev))
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="form-wrap">
          <Card bordered={false} className="panel">
            <div style={{ marginBottom: 16 }}>
              <span className="card-title">数据库归档任务</span>
            </div>
            <Form layout="vertical">
              <Form.Item
                label="数据库类型"
                required
                validateStatus={errors.db_type ? 'error' : ''}
                help={errors.db_type}
              >
                <Select
                  showSearch
                  optionFilterProp="label"
                  placeholder="选择类型"
                  style={{ width: '100%' }}
                  value={form.db_type || undefined}
                  onChange={(v) => onTypeChange(v)}
                  options={dbTypes.map((t) => ({ label: t.label, value: t.value }))}
                />
              </Form.Item>

              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item
                    label="源端域名"
                    required
                    validateStatus={errors.source_instance_id ? 'error' : ''}
                    help={errors.source_instance_id}
                  >
                    <Select
                      showSearch
                      optionFilterProp="label"
                      disabled={!form.db_type}
                      placeholder={form.db_type ? '源端数据库域名' : '请先选择类型'}
                      style={{ width: '100%' }}
                      value={form.source_instance_id || undefined}
                      onChange={(v) => patch({ source_instance_id: v })}
                      options={sourceInstances.map((i) => ({ label: domainLabel(i), value: i.id }))}
                    />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item label="源端库名">
                    <Input
                      value={form.source_db}
                      placeholder="源端库名"
                      onChange={(e) => patch({ source_db: e.target.value })}
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Form.Item
                label="源端表名"
                required
                validateStatus={errors.source_table ? 'error' : ''}
                help={errors.source_table}
              >
                <Input
                  value={form.source_table}
                  placeholder="源端表名"
                  onChange={(e) => patch({ source_table: e.target.value })}
                />
              </Form.Item>

              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item label="目标域名">
                    <Select
                      showSearch
                      allowClear
                      optionFilterProp="label"
                      placeholder="目标数据库域名"
                      style={{ width: '100%' }}
                      value={form.target_instance_id || undefined}
                      onChange={(v) => patch({ target_instance_id: v || '' })}
                      options={instances.map((i) => ({ label: domainLabel(i), value: i.id }))}
                    />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item label="目标库名">
                    <Input
                      value={form.target_db}
                      placeholder="目标库名"
                      onChange={(e) => patch({ target_db: e.target.value })}
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Form.Item label="目标表名">
                <Input
                  value={form.target_table}
                  placeholder="目标表名"
                  onChange={(e) => patch({ target_table: e.target.value })}
                />
              </Form.Item>

              <Form.Item
                label="库表类型"
                required
                validateStatus={errors.table_type ? 'error' : ''}
                help={errors.table_type}
              >
                <Select
                  placeholder="请选择"
                  style={{ width: '100%' }}
                  value={form.table_type || undefined}
                  onChange={(v) => patch({ table_type: v })}
                  options={[
                    { label: '全量归档', value: 'full' },
                    { label: '分区表归档', value: 'partition' },
                    { label: '逻辑归档', value: 'logical' },
                  ]}
                />
              </Form.Item>

              <Form.Item
                label="执行方式"
                required
                validateStatus={errors.run_mode ? 'error' : ''}
                help={errors.run_mode}
              >
                <Radio.Group
                  value={form.run_mode}
                  onChange={(e) => patch({ run_mode: e.target.value })}
                >
                  <Radio value="immediate">立即执行</Radio>
                  <Radio value="scheduled">定时任务</Radio>
                </Radio.Group>
              </Form.Item>

              {form.run_mode === 'scheduled' && (
                <Form.Item
                  label="调度配置"
                  required
                  validateStatus={errors.schedule_expr ? 'error' : ''}
                  help={errors.schedule_expr}
                >
                  <Input
                    value={form.schedule_expr}
                    placeholder="如: 0 2 * * * 或 每日 02:00"
                    onChange={(e) => patch({ schedule_expr: e.target.value })}
                  />
                </Form.Item>
              )}

              <Form.Item
                label="审核人"
                required
                validateStatus={errors.approver ? 'error' : ''}
                help={errors.approver}
              >
                <Select
                  showSearch
                  mode="tags"
                  maxTagCount={1}
                  placeholder="请选择审核人"
                  style={{ width: '100%' }}
                  value={form.approver ? [form.approver] : []}
                  onChange={(v: string[]) => patch({ approver: v.slice(-1)[0] || '' })}
                  options={approvers.map((u) => ({ label: u, value: u }))}
                />
              </Form.Item>

              <Form.Item label="归档条件">
                <Input.TextArea
                  value={form.archive_condition}
                  rows={4}
                  placeholder="如: create_time < '2024-01-01'"
                  onChange={(e) => patch({ archive_condition: e.target.value })}
                />
                <div className="hint">可选，用于指定归档数据的 WHERE 条件，如时间范围</div>
              </Form.Item>

              <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
              <div className="actions">
                <Button type="primary" loading={saving} onClick={submit}>
                  提交工单
                </Button>
                <Button onClick={() => saveDraft()}>保存草稿</Button>
                <Button onClick={reset}>重置</Button>
              </div>
            </Form>
          </Card>
        </div>
      </div>
    </div>
  )
}
