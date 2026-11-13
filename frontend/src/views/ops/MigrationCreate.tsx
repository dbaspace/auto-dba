import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Link,
  useLocation,
  useNavigate,
  useSearchParams } from 'react-router-dom'
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
  Tag,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import dayjs from 'dayjs'
import api from '../../api/request'
import { useAppDispatch, useAppSelector } from '../../store'
import { closeTab, openTab, tabKey } from '../../store/tabsSlice'
import { goTicketListAndClose } from '../tickets/goTicketList'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import {
  filterAccessDomainRows,
  parseSourceKey,
  sourceKey,
  sourcePrimaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
} from '../../utils/instanceSource'
import MigrationObjectSelect, { MigrationObject } from './MigrationObjectSelect'
import './MigrationCreate.css'

interface MigForm {
  name: string
  description: string
  db_type: string
  env: string
  source_env: string
  target_env: string
  start_mode: string
  scheduled_at: string
  assigned_approver: string
  source_instance_id: string
  target_instance_id: string
  source_domain: string
  target_domain: string
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
  oss_envs?: string[]
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
    source_env: 'test',
    target_env: 'test',
    start_mode: 'immediate',
    scheduled_at: '',
    assigned_approver: '',
    source_instance_id: '',
    target_instance_id: '',
    source_domain: '',
    target_domain: '',
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

function conflictLabel(v: any) {
  return (
    ({ abort: '报错中止', skip: '跳过已存在', truncate: '先清空再写入', drop_create: '先删除再创建' } as Record<
      string,
      string
    >)[v] || v
  )
}
function domainFor(rows: AccessDomainRow[], instanceId: string, host: string) {
  const list = (rows || []).filter((r) => String(r.instance_id || '') === String(instanceId || ''))
  const hostKey = String(host || '').trim().toLowerCase()
  const hit = list.find((r) => String(r.domain || '').trim().toLowerCase() === hostKey)
  return String((hit || list[0])?.domain || '')
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
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const editTaskId = searchParams.get('editTaskId') || ''
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const [step, setStep] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [prechecking, setPrechecking] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
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
    oss_envs: [],
  })
  const [approverNames, setApproverNames] = useState<string[]>([])
  const [form, setForm] = useState<MigForm>(emptyForm())
  const approvalOn = settings.approval_enabled !== false

  const formRef = useRef(form)
  formRef.current = form
  const stepRef = useRef(step)
  stepRef.current = step

  function patchForm(patch: Partial<MigForm>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const approvers = approverNames

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
  const sourceDomainOptions = useMemo(
    () =>
      sourcesFromAccessDomains(
        filterAccessDomainRows(accessDomains, { env: form.source_env, dbType: form.db_type }),
      ),
    [accessDomains, form.source_env, form.db_type],
  )
  const targetDomainOptions = useMemo(
    () =>
      sourcesFromAccessDomains(
        filterAccessDomainRows(accessDomains, { env: form.target_env, dbType: form.db_type }),
      ),
    [accessDomains, form.target_env, form.db_type],
  )

  function clearEndpoints() {
    return {
      source_instance_id: '',
      target_instance_id: '',
      source_domain: '',
      target_domain: '',
      objects: [] as MigrationObject[],
      whole_dbs: [] as string[],
    }
  }

  function onDbTypeChange(v: string) {
    patchForm({ ...clearEndpoints(), db_type: v })
    setSrcConn(null)
    setTgtConn(null)
  }

  function onSourceEnvChange(v: string) {
    patchForm({
      source_instance_id: '',
      source_domain: '',
      objects: [],
      whole_dbs: [],
      source_env: v,
      env: v,
    })
    setSrcConn(null)
  }

  function onTargetEnvChange(v: string) {
    patchForm({
      target_instance_id: '',
      target_domain: '',
      target_env: v,
    })
    setTgtConn(null)
  }

  function onEndpointChange(side: 'source' | 'target', key: string) {
    const parsed = parseSourceKey(key)
    if (side === 'source') {
      patchForm({
        source_instance_id: parsed?.instance_id || '',
        source_domain: parsed?.domain || '',
        objects: [],
        whole_dbs: [],
      })
      setSrcConn(null)
      return
    }
    patchForm({
      target_instance_id: parsed?.instance_id || '',
      target_domain: parsed?.domain || '',
    })
    setTgtConn(null)
  }

  function next() {
    if (step === 0) {
      if (!form.name || !form.db_type || !form.source_env || !form.target_env) {
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
    if (
      step === 1 &&
      (!form.source_domain || !form.target_domain || !form.source_instance_id || !form.target_instance_id || !endpointsOk)
    ) {
      message.warning('请选择源/目标域名并完成连通测试')
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
    const domain = side === 'source' ? form.source_domain : form.target_domain
    if (side === 'source') setSrcTesting(true)
    else setTgtTesting(true)
    try {
      const { data } = await api.get(`/migration/instances/${id}/connect`, {
        params: {
          domain,
          env: side === 'source' ? form.source_env : form.target_env,
          db_type: form.db_type,
        },
      })
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
      if (!nextForm.source_env) nextForm.source_env = nextForm.env || 'test'
      if (!nextForm.target_env) nextForm.target_env = nextForm.env || nextForm.source_env
      nextForm.env = nextForm.source_env
      if (!String(nextForm.source_domain || '').trim()) nextForm.source_instance_id = ''
      if (!String(nextForm.target_domain || '').trim()) nextForm.target_instance_id = ''
      setForm(nextForm)
      if (Number.isFinite(v.step)) setStep(v.step)
    },
  })

  useEffect(() => {
    if (editTaskId) return
    schedulePersist()
  }, [form, step, schedulePersist, editTaskId])

  function payload() {
    const f = formRef.current
    return {
      name: f.name,
      description: f.description,
      db_type: f.db_type,
      env: f.source_env || f.env,
      source_env: f.source_env || f.env,
      target_env: f.target_env || f.source_env || f.env,
      start_mode: f.start_mode,
      scheduled_at: f.scheduled_at,
      assigned_approver: f.assigned_approver,
      reviewer: f.assigned_approver,
      approver: f.assigned_approver,
      assigned_to: f.assigned_approver,
      source_instance_id: f.source_instance_id,
      target_instance_id: f.target_instance_id,
      source_domain: f.source_domain,
      target_domain: f.target_domain,
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
    if (approvalOn && !form.assigned_approver) {
      message.warning('请选择审核人')
      return
    }
    setSubmitting(true)
    try {
      const req = editTaskId
        ? api.put(`/migration/tasks/${editTaskId}`, payload(), { timeout: 60000 })
        : api.post('/migration/tasks', payload(), { timeout: 60000 })
      const { data } = await req
      clearOnSubmitSuccess()
      if (data.ok === false) {
        message.warning(data.message || '已创建但预检未通过')
        const id = data.data?.id
        navigate(id ? `/ops/migration/${id}` : '/ops/migration')
        return
      }
      if (data.approval_required === false) {
        message.success(form.start_mode === 'scheduled' ? '预检通过，已进入定时' : '预检通过，已开始执行')
        const curKey = tabKey(location.pathname, location.search)
        dispatch(openTab({ path: '/ops/migration', menus, title: '数据迁移' }))
        dispatch(closeTab(curKey))
        navigate('/ops/migration')
        return
      }
      message.success('已提交审批，通过后自动执行')
      goTicketListAndClose(dispatch, navigate, location, menus)
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      const [instRes, domainRes, envRes, setRes, approverRes] = await Promise.all([
        api.get('/instances'),
        api.get('/access-domains').catch(() => ({ data: { data: [] } })),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
        api.get('/migration/settings').catch(() => ({ data: { data: {} } })),
        api.get('/create-db/approvers').catch(() => ({ data: { data: [] } })),
      ])
      setInstances(instRes.data.data || [])
      setAccessDomains(domainRes.data.data || [])
      const envs = (envRes.data.data || [])
        .map((e: any) => e.code || e.name || e.id)
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
      const names: string[] = (approverRes.data.data || [])
        .map((x: any) => (typeof x === 'string' ? x : x?.name || x?.username || ''))
        .map((s: string) => String(s).trim())
        .filter(Boolean)
      setApproverNames(names)
      if (editTaskId) {
        const { data: taskRes } = await api.get(`/migration/tasks/${editTaskId}`)
        const t = taskRes.data
        if (t && t.status === 'pre_check_failed') {
          const objects = (t.objects || []).map((o: any) => ({
            source_db_name: o.source_db_name,
            source_schema: o.source_schema || '',
            source_table_name: o.source_table_name,
            target_db_name: o.target_db_name,
            target_schema: o.target_schema || '',
            target_table_name: o.target_table_name,
            total_bytes: Number(o.total_bytes || 0),
          }))
          const domains = domainRes.data.data || []
          setForm({
            ...emptyForm(),
            name: t.name || '',
            description: t.description || '',
            db_type: t.db_type || 'postgres',
            env: t.source_env || t.env || 'test',
            source_env: t.source_env || t.env || 'test',
            target_env: t.target_env || t.env || 'test',
            start_mode: t.start_mode || 'immediate',
            scheduled_at: t.scheduled_at || '',
            assigned_approver: t.assigned_approver || '',
            source_instance_id: t.source_instance_id || '',
            target_instance_id: t.target_instance_id || '',
            source_domain: domainFor(domains, t.source_instance_id, t.source_host),
            target_domain: domainFor(domains, t.target_instance_id, t.target_host),
            migration_mode: t.migration_mode || 'full_data',
            schema_strategy: t.schema_strategy || 'init',
            conflict_strategy: t.conflict_strategy || 'abort',
            transfer_mode: t.transfer_mode || 'direct',
            batch_size: t.batch_size || 1000,
            speed_limit: Number(t.speed_limit || 0),
            source_db: t.source_db || '',
            target_db: t.target_db || '',
            tables: objects.map((o: MigrationObject) => o.source_table_name),
            objects,
            whole_dbs: [],
          })
          setStep(0)
        } else {
          message.warning('仅预检失败的任务可以重新编辑')
        }
      }
      setForm((f) => {
        if (!f.assigned_approver && names[0]) return { ...f, assigned_approver: names[0] }
        return f
      })
      if (!editTaskId) restoreDraft()
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!form.source_env) return
    let cancelled = false
    ;(async () => {
      try {
        const { data } = await api.get('/migration/settings', { params: { env: form.source_env } })
        if (cancelled) return
        const row = data.data || {}
        setSettings((prev) => ({
          ...prev,
          oss_configured: Boolean(row.oss_configured),
          oss_envs: Array.isArray(row.oss_envs) ? row.oss_envs : prev.oss_envs,
        }))
      } catch {
        /* ignore */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [form.source_env])

  const ossReadyForEnv =
    Boolean(settings.oss_configured) || (settings.oss_envs || []).includes(form.source_env)

  return (
    <div className="mig-create">
      <div className="mig-create-body">
        <aside className="mig-steps">
          <Steps
            direction="vertical"
            current={step}
            items={[
              { title: '基本信息' },
              { title: '选择端点' },
              { title: '迁移策略' },
              { title: '选择对象' },
              { title: '预检提交' },
            ]}
          />
        </aside>
        <div className="mig-create-main">
          <div className="ops-title">
            <h1>{editTaskId ? '编辑迁移任务' : '新建迁移任务'}</h1>
            <Button onClick={() => navigate('/ops/migration')}>返回列表</Button>
          </div>
          {(form.source_env === 'prod' || form.target_env === 'prod') && (
            <Alert
              type="warning"
              showIcon
              className="mb"
              message="源端或目标端为生产环境，请务必确认配置与审批要求。"
            />
          )}
      <div style={{ display: step === 3 ? 'block' : 'none' }}>
        <MigrationObjectSelect
          instanceId={form.source_instance_id}
          domain={form.source_domain}
          env={form.source_env}
          dbType={form.db_type}
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
              <Form.Item label="源端环境" required>
                <Select
                  style={{ width: '100%' }}
                  value={form.source_env}
                  onChange={onSourceEnvChange}
                  options={envOptions.map((e) => ({ label: e, value: e }))}
                />
              </Form.Item>
              <Form.Item label="目标端环境" required>
                <Select
                  style={{ width: '100%' }}
                  value={form.target_env}
                  onChange={onTargetEnvChange}
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
              <Form.Item label="审核人" required={approvalOn}>
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
              <Form.Item label="源端域名" required>
                <Select
                  showSearch
                  optionFilterProp="label"
                  placeholder="从域名管理选择"
                  style={{ width: '100%' }}
                  value={
                    form.source_instance_id && form.source_domain
                      ? sourceKey(form.source_instance_id, form.source_domain)
                      : undefined
                  }
                  onChange={(v) => onEndpointChange('source', v)}
                  options={sourceDomainOptions.map((s) => ({ value: s.key, label: sourcePrimaryLabel(s) }))}
                />
              </Form.Item>
              <Form.Item>
                <Button
                  disabled={!form.source_domain}
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
              <Form.Item label="目标端域名" required>
                <Select
                  showSearch
                  optionFilterProp="label"
                  placeholder="从域名管理选择"
                  style={{ width: '100%' }}
                  value={
                    form.target_instance_id && form.target_domain
                      ? sourceKey(form.target_instance_id, form.target_domain)
                      : undefined
                  }
                  onChange={(v) => onEndpointChange('target', v)}
                  options={targetDomainOptions.map((s) => ({ value: s.key, label: sourcePrimaryLabel(s) }))}
                />
              </Form.Item>
              <Form.Item>
                <Button
                  disabled={!form.target_domain}
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
                  {form.transfer_mode === 'oss' && form.db_type !== 'postgres' && (
                    <Alert type="warning" showIcon className="mt" message="OSS 中转当前仅支持 PostgreSQL" />
                  )}
                  {form.transfer_mode === 'oss' && !ossReadyForEnv && (
                    <Alert
                      type="warning"
                      showIcon
                      className="mt"
                      message={
                        <span>
                          请先在 <Link to="/config/oss">配置中心 → OSS 配置</Link> 中为源端环境「
                          {form.source_env}」配置连接信息
                        </span>
                      }
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
              <Form.Item label="限速（行/秒）" extra="留空或 0 时使用全局默认限速">
                <InputNumber
                  min={0}
                  max={1000000}
                  step={100}
                  value={form.speed_limit}
                  onChange={(v) => patchForm({ speed_limit: v == null ? 0 : Number(v) })}
                />
              </Form.Item>
            </>
          )}

          {step === 4 && (
            <>
              <Descriptions column={2} bordered>
                <Descriptions.Item label="任务名称">{form.name}</Descriptions.Item>
                <Descriptions.Item label="源端环境">{form.source_env}</Descriptions.Item>
                <Descriptions.Item label="目标端环境">{form.target_env}</Descriptions.Item>
                <Descriptions.Item label="类型">{form.db_type}</Descriptions.Item>
                <Descriptions.Item label="源端域名">{form.source_domain || '—'}</Descriptions.Item>
                <Descriptions.Item label="目标端域名">{form.target_domain || '—'}</Descriptions.Item>
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
                <ResponsiveTable
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
          <Button type="primary" loading={submitting} disabled={approvalOn && !form.assigned_approver} onClick={submit}>
            {approvalOn ? '提交审批' : '提交'}
          </Button>
        )}
      </div>
      {step === 4 && approvalOn && <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>}
        </div>
      </div>
    </div>
  )
}
