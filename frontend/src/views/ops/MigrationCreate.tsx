import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  DatePicker,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Radio,
  Select,
  Steps,
  Table,
  Tag,
  message,
} from 'antd'
import dayjs from 'dayjs'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import MigrationObjectSelect, { MigrationObject } from './MigrationObjectSelect'
import './MigrationCreate.css'

interface MigForm {
  name: string
  description: string
  db_type: string
  env: string
  start_mode: string
  scheduled_at: string
  assigned_approver: string
  source_instance_id: string
  target_instance_id: string
  migration_mode: string
  schema_strategy: string
  conflict_strategy: string
  transfer_mode: string
  batch_size: number
  speed_limit: number
  source_db: string
  target_db: string
  tables: string[]
  objects: MigrationObject[]
  whole_dbs: string[]
}

interface ConnResult {
  connected?: boolean
  username?: string
  error_message?: string
}

interface Settings {
  approval_enabled?: boolean
  approver_user_ids?: string
  oss_configured?: boolean
  performance?: { batch_size?: number; default_speed_limit?: number }
}

interface PrecheckItem {
  check_item?: string
  check_level?: string
  check_status?: string
  message?: string
}

function emptyForm(): MigForm {
  return {
    name: '',
    description: '',
    db_type: 'postgres',
    env: 'test',
    start_mode: 'immediate',
    scheduled_at: '',
    assigned_approver: '',
    source_instance_id: '',
    target_instance_id: '',
    migration_mode: 'full_data',
    schema_strategy: 'init',
    conflict_strategy: 'abort',
    transfer_mode: 'direct',
    batch_size: 1000,
    speed_limit: 0,
    source_db: '',
    target_db: '',
    tables: [],
    objects: [],
    whole_dbs: [],
  }
}

function matchType(a: any, b: any) {
  const n = (t: any) => {
    t = String(t || '').toLowerCase()
    if (['postgresql', 'pg', 'pgsql'].includes(t)) return 'postgres'
    if (['tidb', 'oceanbase'].includes(t)) return 'mysql'
    return t
  }
  return n(a) === n(b)
}
function instLabel(i: any) {
  const addr = i.host ? `${i.host}:${i.port}` : ''
  return i.name ? `${i.name} — ${addr}` : addr
}
function conflictLabel(v: any) {
  return (
    ({ abort: '报错中止', skip: '跳过已存在', truncate: '先清空再写入', drop_create: '先删除再创建' } as Record<
      string,
      string
    >)[v] || v
  )
}
function validateObjects(list: MigrationObject[]): string {
  const seen = new Set<string>()
  for (const o of list || []) {
    const td = String(o.target_db_name || '').trim()
    const tt = String(o.target_table_name || '').trim()
    if (!td || !tt) return '每条迁移对象的目标库、目标表均不能为空'
    if (td.length > 128 || tt.length > 128) return '目标库名或目标表名过长（单字段不超过 128 字符）'
    const k = `${td}\0${tt}`
    if (seen.has(k)) return '存在重复的目标库.目标表组合，请调整映射'
    seen.add(k)
  }
  return ''
}

export default function MigrationCreate() {
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [prechecking, setPrechecking] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [envOptions, setEnvOptions] = useState<string[]>(['test', 'prod'])
  const [precheckItems, setPrecheckItems] = useState<PrecheckItem[]>([])
  const [srcConn, setSrcConn] = useState<ConnResult | null>(null)
  const [tgtConn, setTgtConn] = useState<ConnResult | null>(null)
  const [srcTesting, setSrcTesting] = useState(false)
  const [tgtTesting, setTgtTesting] = useState(false)
  const [settings, setSettings] = useState<Settings>({
    approval_enabled: true,
    approver_user_ids: '',
    oss_configured: false,
  })
  const [extraApprovers, setExtraApprovers] = useState<string[]>([])
  const [form, setForm] = useState<MigForm>(emptyForm())

  const formRef = useRef(form)
  formRef.current = form
  const stepRef = useRef(step)
  stepRef.current = step

  function patchForm(patch: Partial<MigForm>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const approvers = useMemo(() => {
    const set = new Set(
      String(settings.approver_user_ids || '')
        .split(/[,，\s]+/)
        .map((s) => s.trim())
        .filter(Boolean),
    )
    for (const u of extraApprovers) if (u) set.add(u)
    const src = instances.find((i) => i.id === form.source_instance_id)
    for (const u of src?.approvers || []) if (u) set.add(u)
    return [...set]
  }, [settings.approver_user_ids, extraApprovers, instances, form.source_instance_id])

  const endpointsOk = useMemo(
    () => Boolean(srcConn?.connected && tgtConn?.connected),
    [srcConn, tgtConn],
  )
  const selectedSourceDbs = useMemo(
    () => [...new Set((form.objects || []).map((o) => o.source_db_name))].join('、'),
    [form.objects],
  )
  const selectedTargetDbs = useMemo(
    () => [...new Set((form.objects || []).map((o) => o.target_db_name))].join('、'),
    [form.objects],
  )
  const sourceInst = useMemo(
    () => instances.filter((i) => matchType(i.db_type, form.db_type)),
    [instances, form.db_type],
  )
  const targetInst = sourceInst

  function onDbTypeChange(v: string) {
    patchForm({ source_instance_id: '', target_instance_id: '', db_type: v })
    setSrcConn(null)
    setTgtConn(null)
  }

  function next() {
    if (step === 0) {
      if (!form.name || !form.db_type || !form.env) {
        message.warning('请完善基本信息')
        return
      }
      if (form.start_mode === 'scheduled' && !form.scheduled_at) {
        message.warning('请填写定时时间')
        return
      }
      if (!form.assigned_approver) {
        message.warning('请选择审核人')
        return
      }
    }
    if (step === 1 && (!form.source_instance_id || !form.target_instance_id || !endpointsOk)) {
      message.warning('请选择源/目标实例并完成连通测试')
      return
    }
    if (step === 3) {
      if (!form.objects?.length) {
        message.warning('请至少选择一张表，或勾选整个库（将包含该库下未排除的表）')
        return
      }
      const err = validateObjects(form.objects)
      if (err) {
        message.warning(err)
        return
      }
    }
    setStep((s) => s + 1)
  }

  async function testConn(side: 'source' | 'target') {
    const id = side === 'source' ? form.source_instance_id : form.target_instance_id
    if (side === 'source') setSrcTesting(true)
    else setTgtTesting(true)
    try {
      const { data } = await api.get(`/migration/instances/${id}/connect`)
      if (side === 'source') setSrcConn(data)
      else setTgtConn(data)
    } finally {
      if (side === 'source') setSrcTesting(false)
      else setTgtTesting(false)
    }
  }

  const { restoreDraft, saveNow, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'migration',
    label: '数据迁移',
    path: '/ops/migration/create',
    getValues: () => ({ form: { ...formRef.current }, step: stepRef.current }),
    applyValues: (v: any) => {
      const nextForm = { ...emptyForm(), ...(v.form || v) }
      setForm(nextForm)
      if (Number.isFinite(v.step)) setStep(v.step)
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, step, schedulePersist])

  function payload() {
    const f = formRef.current
    return {
      name: f.name,
      description: f.description,
      db_type: f.db_type,
      env: f.env,
      start_mode: f.start_mode,
      scheduled_at: f.scheduled_at,
      assigned_approver: f.assigned_approver,
      reviewer: f.assigned_approver,
      approver: f.assigned_approver,
      assigned_to: f.assigned_approver,
      source_instance_id: f.source_instance_id,
      target_instance_id: f.target_instance_id,
      source_db: f.objects[0]?.source_db_name || f.source_db,
      target_db: f.objects[0]?.target_db_name || f.target_db,
      tables: f.objects.map((o) => o.source_table_name),
      objects: f.objects,
      migration_mode: f.migration_mode,
      schema_strategy: f.schema_strategy,
      conflict_strategy: f.conflict_strategy,
      transfer_mode: f.transfer_mode,
      batch_size: f.batch_size,
      speed_limit: f.speed_limit,
    }
  }

  async function doPrecheck() {
    setPrechecking(true)
    try {
      const { data } = await api.post('/migration/tasks/precheck', payload(), { timeout: 60000 })
      setPrecheckItems(data.items || [])
      if (data.ok) message.success('预检通过')
      else message.warning('预检未通过')
    } finally {
      setPrechecking(false)
    }
  }

  async function submit() {
    if (!form.objects?.length) {
      message.warning('请至少选择一张表')
      return
    }
    const objErr = validateObjects(form.objects)
    if (objErr) {
      message.warning(objErr)
      return
    }
    if (!form.assigned_approver) {
      message.warning('请选择审核人')
      return
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/migration/tasks', payload(), { timeout: 60000 })
      clearOnSubmitSuccess()
      if (data.ok === false) {
        message.warning(data.message || '已创建但预检未通过')
        const id = data.data?.id
        navigate(id ? `/ops/migration/${id}` : '/ops/migration')
        return
      }
      message.success('已提交审批，通过后自动执行')
      const row = data.data || {}
      if (row.issue_id) navigate('/ops/tickets/' + row.issue_id)
      else navigate(row.id ? `/ops/migration/${row.id}` : '/ops/migration')
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      const [instRes, envRes, setRes, argRes] = await Promise.all([
        api.get('/instances'),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
        api.get('/migration/settings').catch(() => ({ data: { data: {} } })),
        api.get('/global-args').catch(() => ({ data: { data: [] } })),
      ])
      setInstances(instRes.data.data || [])
      const envs = (envRes.data.data || [])
        .map((e: any) => e.name || e.code || e.id)
        .filter(Boolean)
      if (envs.length) setEnvOptions(envs)
      const s: Settings = { ...setRes.data.data }
      setSettings((prev) => ({ ...prev, ...s }))
      const p = s.performance || {}
      setForm((f) => {
        let batch_size = f.batch_size
        let speed_limit = f.speed_limit
        if ((p.batch_size || 0) > 0 && f.batch_size === 1000) batch_size = p.batch_size as number
        if (Number.isFinite(p.default_speed_limit) && f.speed_limit === 0)
          speed_limit = p.default_speed_limit as number
        return { ...f, batch_size, speed_limit }
      })
      const args = argRes.data.data || []
      const extras: string[] = []
      String(args.find((a: any) => a.key === 'sync_default_approver')?.value || '')
        .split(/[,，\s]+/)
        .map((str) => str.trim())
        .filter(Boolean)
        .forEach((u) => extras.push(u))
      setExtraApprovers(extras)
      setForm((f) => {
        if (!f.assigned_approver) {
          const set = new Set(
            String(s.approver_user_ids || '')
              .split(/[,，\s]+/)
              .map((str) => str.trim())
              .filter(Boolean),
          )
          for (const u of extras) if (u) set.add(u)
          const first = [...set][0]
          if (first) return { ...f, assigned_approver: first }
        }
        return f
      })
      restoreDraft()
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="mig-create">
      <div className="ops-title">
        <h1>新建迁移任务</h1>
        <Button onClick={() => navigate('/ops/migration')}>返回列表</Button>
      </div>
      {form.env === 'prod' && (
        <Alert
          type="warning"
          showIcon
          className="mb"
          message="当前为生产环境，请务必确认配置与审批要求。"
        />
      )}
      <Steps
        current={step}
        className="mb"
        labelPlacement="vertical"
        items={[
          { title: '基本信息' },
          { title: '选择端点' },
          { title: '迁移策略' },
          { title: '选择对象' },
          { title: '预检提交' },
        ]}
      />

      <div style={{ display: step === 3 ? 'block' : 'none' }}>
        <MigrationObjectSelect
          instanceId={form.source_instance_id}
          objects={form.objects}
          wholeDbs={form.whole_dbs}
          onObjectsChange={(objects) => patchForm({ objects })}
          onWholeDbsChange={(whole_dbs) => patchForm({ whole_dbs })}
        />
      </div>

      <div style={{ display: step === 3 ? 'none' : 'block' }}>
        <Form layout="vertical" className="white-card">
          {step === 0 && (
            <>
              <Form.Item label="任务名称" required>
                <Input
                  maxLength={80}
                  showCount
                  value={form.name}
                  onChange={(e) => patchForm({ name: e.target.value })}
                />
              </Form.Item>
              <Form.Item label="说明">
                <Input.TextArea
                  rows={3}
                  value={form.description}
                  onChange={(e) => patchForm({ description: e.target.value })}
                />
              </Form.Item>
              <Form.Item label="数据库类型" required>
                <Select
                  style={{ width: '100%' }}
                  value={form.db_type}
                  onChange={onDbTypeChange}
                  options={[
                    { label: 'MySQL', value: 'mysql' },
                    { label: 'PostgreSQL', value: 'postgres' },
                  ]}
                />
              </Form.Item>
              <Form.Item label="环境" required>
                <Select
                  style={{ width: '100%' }}
                  value={form.env}
                  onChange={(v) => patchForm({ env: v })}
                  options={envOptions.map((e) => ({ label: e, value: e }))}
                />
              </Form.Item>
              <Form.Item label="启动方式">
                <Radio.Group
                  value={form.start_mode}
                  onChange={(e) => patchForm({ start_mode: e.target.value })}
                >
                  <Radio value="immediate">立即启动</Radio>
                  <Radio value="scheduled">定时启动</Radio>
                </Radio.Group>
              </Form.Item>
              {form.start_mode === 'scheduled' && (
                <Form.Item label="定时时间" required>
                  <DatePicker
                    showTime
                    style={{ width: '100%' }}
                    value={form.scheduled_at ? dayjs(form.scheduled_at) : null}
                    onChange={(v) =>
                      patchForm({ scheduled_at: v ? v.format('YYYY-MM-DD HH:mm:ss') : '' })
                    }
                  />
                </Form.Item>
              )}
              <Form.Item label="审核人" required>
                <Select
                  showSearch
                  mode="tags"
                  maxCount={1}
                  placeholder="审批通过后自动执行"
                  style={{ width: '100%' }}
                  value={form.assigned_approver ? [form.assigned_approver] : []}
                  onChange={(v) => patchForm({ assigned_approver: v[v.length - 1] || '' })}
                  options={approvers.map((u) => ({ label: u, value: u }))}
                />
              </Form.Item>
            </>
          )}

          {step === 1 && (
            <>
              <Form.Item label="源实例" required>
                <Select
                  showSearch
                  optionFilterProp="label"
                  style={{ width: '100%' }}
                  value={form.source_instance_id || undefined}
                  onChange={(v) => {
                    patchForm({ source_instance_id: v })
                    setSrcConn(null)
                  }}
                  options={sourceInst.map((i) => ({ value: i.id, label: instLabel(i) }))}
                />
              </Form.Item>
              <Form.Item>
                <Button
                  disabled={!form.source_instance_id}
                  loading={srcTesting}
                  onClick={() => testConn('source')}
                >
                  测试源端连通
                </Button>
                {srcConn && (
                  <span className={srcConn.connected ? 'ok' : 'bad'}>
                    {srcConn.connected ? `连接成功 ${srcConn.username || ''}` : srcConn.error_message}
                  </span>
                )}
              </Form.Item>
              <Form.Item label="目标实例" required>
                <Select
                  showSearch
                  optionFilterProp="label"
                  style={{ width: '100%' }}
                  value={form.target_instance_id || undefined}
                  onChange={(v) => {
                    patchForm({ target_instance_id: v })
                    setTgtConn(null)
                  }}
                  options={targetInst.map((i) => ({ value: i.id, label: instLabel(i) }))}
                />
              </Form.Item>
              <Form.Item>
                <Button
                  disabled={!form.target_instance_id}
                  loading={tgtTesting}
                  onClick={() => testConn('target')}
                >
                  测试目标端连通
                </Button>
                {tgtConn && (
                  <span className={tgtConn.connected ? 'ok' : 'bad'}>
                    {tgtConn.connected ? `连接成功 ${tgtConn.username || ''}` : tgtConn.error_message}
                  </span>
                )}
              </Form.Item>
            </>
          )}

          {step === 2 && (
            <>
              <Form.Item label="迁移模式">
                <Radio.Group
                  value={form.migration_mode}
                  onChange={(e) => patchForm({ migration_mode: e.target.value })}
                >
                  <Radio value="full_data">全量迁移</Radio>
                  <Radio value="schema_only">仅结构迁移</Radio>
                </Radio.Group>
              </Form.Item>
              <Form.Item label="结构同步策略">
                <Radio.Group
                  value={form.schema_strategy}
                  onChange={(e) => patchForm({ schema_strategy: e.target.value })}
                >
                  <Radio value="init">结构初始化</Radio>
                  <Radio value="no_init">不结构初始化</Radio>
                </Radio.Group>
              </Form.Item>
              <Form.Item label="冲突处理策略">
                <Select
                  style={{ width: '100%' }}
                  value={form.conflict_strategy}
                  onChange={(v) => patchForm({ conflict_strategy: v })}
                  options={[
                    { label: '报错中止', value: 'abort' },
                    { label: '跳过已存在', value: 'skip' },
                    { label: '先清空再写入', value: 'truncate' },
                    { label: '先删除再创建', value: 'drop_create' },
                  ]}
                />
              </Form.Item>
              {form.conflict_strategy === 'drop_create' && (
                <Alert
                  type="warning"
                  showIcon
                  className="mb"
                  message="将删除目标端已存在对象后再创建，请谨慎使用。"
                />
              )}
              {form.migration_mode === 'full_data' && (
                <Form.Item label="数据中转方式">
                  <Radio.Group
                    value={form.transfer_mode}
                    onChange={(e) => patchForm({ transfer_mode: e.target.value })}
                  >
                    <Radio value="direct">直连流式传输</Radio>
                    <Radio value="oss">OSS 中转</Radio>
                  </Radio.Group>
                  {form.transfer_mode === 'oss' && !settings.oss_configured && (
                    <Alert
                      type="warning"
                      showIcon
                      className="mt"
                      message="请先在迁移设置中配置 OSS 连接信息"
                    />
                  )}
                </Form.Item>
              )}
              <Form.Item label="批次大小">
                <InputNumber
                  min={100}
                  max={100000}
                  step={100}
                  value={form.batch_size}
                  onChange={(v) => patchForm({ batch_size: Number(v) })}
                />
              </Form.Item>
            </>
          )}

          {step === 4 && (
            <>
              <Descriptions column={2} bordered>
                <Descriptions.Item label="任务名称">{form.name}</Descriptions.Item>
                <Descriptions.Item label="环境">{form.env}</Descriptions.Item>
                <Descriptions.Item label="类型">{form.db_type}</Descriptions.Item>
                <Descriptions.Item label="启动">
                  {form.start_mode === 'scheduled' ? `定时 ${form.scheduled_at}` : '审批通过后立即执行'}
                </Descriptions.Item>
                <Descriptions.Item label="审核人">{form.assigned_approver || '—'}</Descriptions.Item>
                <Descriptions.Item label="源库">
                  {selectedSourceDbs || form.source_db || '—'}
                </Descriptions.Item>
                <Descriptions.Item label="目标库">
                  {selectedTargetDbs || form.target_db || '—'}
                </Descriptions.Item>
                <Descriptions.Item label="迁移模式">
                  {form.migration_mode === 'schema_only' ? '仅结构迁移' : '全量迁移'}
                </Descriptions.Item>
                <Descriptions.Item label="结构策略">
                  {form.schema_strategy === 'no_init' ? '不初始化' : '结构初始化'}
                </Descriptions.Item>
                <Descriptions.Item label="冲突策略">{conflictLabel(form.conflict_strategy)}</Descriptions.Item>
                <Descriptions.Item label="表数量">
                  {form.objects.length || form.tables.length}
                </Descriptions.Item>
              </Descriptions>
              <div className="pre-actions">
                <Button loading={prechecking} onClick={doPrecheck}>
                  预检
                </Button>
              </div>
              {precheckItems.length > 0 && (
                <Table
                  className="mb"
                  rowKey={(_, idx) => String(idx)}
                  bordered
                  size="small"
                  pagination={false}
                  dataSource={precheckItems}
                  columns={[
                    { title: '检查项', dataIndex: 'check_item', width: 180 },
                    {
                      title: '级别',
                      width: 90,
                      render: (_: any, row: PrecheckItem) => row.check_level,
                    },
                    {
                      title: '结果',
                      width: 100,
                      render: (_: any, row: PrecheckItem) => (
                        <Tag color={row.check_status === 'passed' ? 'success' : 'error'}>
                          {row.check_status}
                        </Tag>
                      ),
                    },
                    { title: '说明', dataIndex: 'message' },
                  ]}
                />
              )}
            </>
          )}
        </Form>
      </div>

      <div className="mig-nav">
        <Button onClick={() => saveNow()}>保存草稿</Button>
        {step > 0 && <Button onClick={() => setStep((s) => s - 1)}>上一步</Button>}
        {step < 4 ? (
          <Button type="primary" disabled={step === 1 && !endpointsOk} onClick={next}>
            下一步
          </Button>
        ) : (
          <Button type="primary" loading={submitting} disabled={!form.assigned_approver} onClick={submit}>
            提交审批
          </Button>
        )}
      </div>
      {step === 4 && <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>}
    </div>
  )
}
