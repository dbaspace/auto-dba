import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Select,
  Tooltip,
  message,
} from 'antd'
import api from '../../api/request'
import {
  STRATEGY_OPTIONS,
  RUN_MODE_OPTIONS,
  SCRIPT_TYPE_OPTIONS,
  PG_DELETE_MODE_OPTIONS,
  SCRIPT_TEMPLATES,
  encodeCondition,
  decodeMeta,
  isStockScript,
} from './archive.ts'
import './archive.css'

type DialogForm = {
  id: string
  name: string
  strategy: string
  run_mode: string
  schedule_expr: string
  approver: string
  env: string
  db_type: string
  source_instance_id: string
  source_db: string
  table_name: string
  target_host: string
  target_db: string
  target_table: string
  oss_bucket: string
  pg_delete_mode: string
  script_type: string
  script_content: string
  sql: string
}

function emptyForm(): DialogForm {
  return {
    id: '',
    name: '',
    strategy: 'backup_target',
    run_mode: 'immediate',
    schedule_expr: '',
    approver: '',
    env: '',
    db_type: '',
    source_instance_id: '',
    source_db: '',
    table_name: '',
    target_host: '',
    target_db: '',
    target_table: '',
    oss_bucket: '',
    pg_delete_mode: 'auto',
    script_type: 'python',
    script_content: SCRIPT_TEMPLATES.python,
    sql: '',
  }
}

function typeKey(t: any): string {
  const n = String(t || '').toLowerCase()
  if (n === 'postgresql' || n === 'pg' || n === 'pgsql') return 'postgres'
  if (n === 'mssql') return 'sqlserver'
  return n
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

type Props = {
  open: boolean
  onClose: () => void
  onSaved?: () => void
  instances?: any[]
  task?: any
  initialStrategy?: string
  lockStrategy?: boolean
}

export default function CreateTaskDialog({
  open,
  onClose,
  onSaved,
  instances = [],
  task = null,
  initialStrategy = '',
  lockStrategy = false,
}: Props) {
  const [form, setForm] = useState<DialogForm>(emptyForm())
  const [saving, setSaving] = useState(false)
  const [loadingDbs, setLoadingDbs] = useState(false)
  const [loadingTables, setLoadingTables] = useState(false)
  const [approvers, setApprovers] = useState<string[]>([])
  const [envOptions, setEnvOptions] = useState<any[]>([
    { value: 'test', label: '测试' },
    { value: 'prod', label: '生产' },
  ])
  const [dbTypes, setDbTypes] = useState<{ value: string; label: string }[]>([
    { value: 'mysql', label: 'MySQL' },
    { value: 'postgresql', label: 'PostgreSQL' },
    { value: 'oracle', label: 'Oracle' },
    { value: 'sqlserver', label: 'SQL Server' },
  ])
  const [localInstances, setLocalInstances] = useState<any[]>([])
  const [sourceDbs, setSourceDbs] = useState<string[]>([])
  const [tables, setTables] = useState<string[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})

  const formRef = useRef<DialogForm>(form)
  formRef.current = form
  const approversRef = useRef<string[]>(approvers)
  approversRef.current = approvers
  const envOptionsRef = useRef<any[]>(envOptions)
  envOptionsRef.current = envOptions

  function patch(p: Partial<DialogForm>) {
    setForm((prev) => ({ ...prev, ...p }))
  }

  const isCustom = form.strategy === 'custom_script'
  const isDelete = form.strategy === 'delete_only'
  const isBackup = form.strategy === 'backup_target'
  const isOss = form.strategy === 'archive_oss'
  const conditionLabel = isDelete ? '删除条件' : '归档条件'
  const conditionTip = isDelete
    ? '填写 SQL 条件表达式，用于筛选需删除的数据行'
    : '填写 SQL 条件表达式，用于筛选需归档的数据行'
  const liveTableList = useMemo(() => {
    const t = typeKey(form.db_type)
    return t === 'mysql' || t === 'postgres'
  }, [form.db_type])

  const allInstances = useMemo(
    () => (instances && instances.length ? instances : localInstances),
    [instances, localInstances],
  )

  function instEnv(i: any): string {
    if (!i) return ''
    if (i.env_id) {
      const hit = envOptionsRef.current.find((e) => e.id === i.env_id || e.value === i.env_id)
      if (hit) return hit.value
    }
    return i.idc || i.env || i.env_code || ''
  }

  const sourceInstances = useMemo(() => {
    let list = allInstances
    if (form.db_type) {
      const want = typeKey(form.db_type)
      list = list.filter((i) => typeKey(i.db_type) === want)
    }
    if (form.env) {
      list = list.filter((i) => !instEnv(i) || instEnv(i) === form.env)
    }
    return list
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allInstances, form.db_type, form.env, envOptions])

  const approverPlaceholder = approvers.length
    ? '请选择审核人'
    : '暂无审核人，请在全局参数设置中配置 migration_approver_user_ids'

  function domainLabel(i: any): string {
    const domain = (i.domains && i.domains[0]) || i.domain_name || ''
    const addr = i.host ? `${i.host}:${i.port}` : ''
    const main = domain || addr || i.name
    return i.name && main !== i.name ? `${i.name} — ${main}` : main
  }

  function applyTask(t: any) {
    const meta = t
      ? decodeMeta(t.condition)
      : {
          strategy: initialStrategy || 'backup_target',
          run_mode: 'immediate',
          sql: '',
          schedule_expr: '',
          extra: {} as Record<string, any>,
        }
    const extra = meta.extra || {}
    if (t) {
      setForm({
        id: t.id,
        name: t.name,
        strategy: meta.strategy,
        run_mode: meta.run_mode,
        schedule_expr: t.schedule_expr || meta.schedule_expr || '',
        approver: t.assigned_to || extra.approver || '',
        env: extra.env || '',
        db_type: extra.db_type || '',
        source_instance_id: t.source_instance_id,
        source_db: t.source_db,
        table_name: t.table_name === 'script' ? '' : t.table_name,
        target_host: extra.target_host || '',
        target_db: extra.target_db || '',
        target_table: extra.target_table || '',
        oss_bucket: extra.oss_bucket || '',
        pg_delete_mode: extra.pg_delete_mode || 'auto',
        script_type: extra.script_type || 'python',
        script_content:
          meta.strategy === 'custom_script' ? meta.sql : SCRIPT_TEMPLATES.python,
        sql: meta.strategy === 'custom_script' ? '' : meta.sql,
      })
      return
    }
    setForm({
      ...emptyForm(),
      strategy: initialStrategy || 'backup_target',
      approver: approversRef.current[0] || '',
      script_content: SCRIPT_TEMPLATES.python,
    })
  }

  function reset() {
    applyTask(null)
    setSourceDbs([])
    setTables([])
    setErrors({})
  }

  function onEnvChange(v: string) {
    const next = v || ''
    setForm((prev) => {
      if (prev.id) return { ...prev, env: next }
      const list = filterSource(allInstances, prev.db_type, next)
      const still = list.some((i) => i.id === prev.source_instance_id)
      if (still) return { ...prev, env: next }
      setSourceDbs([])
      setTables([])
      return { ...prev, env: next, source_instance_id: '', source_db: '', table_name: '' }
    })
  }

  function onTypeChange(v: string) {
    const next = v || ''
    setForm((prev) => {
      if (prev.id) return { ...prev, db_type: next }
      const list = filterSource(allInstances, next, prev.env)
      const still = list.some((i) => i.id === prev.source_instance_id)
      if (still) return { ...prev, db_type: next }
      setSourceDbs([])
      setTables([])
      return { ...prev, db_type: next, source_instance_id: '', source_db: '', table_name: '' }
    })
  }

  function filterSource(list: any[], dbType: string, env: string): any[] {
    let out = list
    if (dbType) {
      const want = typeKey(dbType)
      out = out.filter((i) => typeKey(i.db_type) === want)
    }
    if (env) {
      out = out.filter((i) => !instEnv(i) || instEnv(i) === env)
    }
    return out
  }

  async function onSourceChange(v: string) {
    patch({ source_instance_id: v, source_db: '', table_name: '' })
    setTables([])
    if (form.id) return
    await loadDbs(v, '')
    await loadApprovers(v, '')
  }

  async function onDbChange(v: string) {
    patch({ source_db: v, table_name: '' })
    if (form.id) return
    await loadTables(formRef.current.source_instance_id, v)
    await loadApprovers(formRef.current.source_instance_id, v)
  }

  async function loadDbs(instanceId: string, currentDb: string) {
    if (!instanceId || isCustom) {
      setSourceDbs([])
      return
    }
    setLoadingDbs(true)
    try {
      const [live, reg] = await Promise.all([
        api.get(`/instances/${instanceId}/databases`).catch(() => ({ data: { data: [] } })),
        api
          .get('/databases', { params: { instance_id: instanceId } })
          .catch(() => ({ data: { data: [] } })),
      ])
      const names = (live.data.data || [])
        .filter((d: any) => !d.system)
        .map((d: any) => d.name || d.db_name)
        .filter(Boolean)
      const registered = (reg.data.data || []).map((d: any) => d.db_name).filter(Boolean)
      const set = [...new Set([...names, ...registered])] as string[]
      const db = currentDb || formRef.current.source_db
      if (db && !set.includes(db)) set.unshift(db)
      setSourceDbs(set)
    } catch {
      const db = currentDb || formRef.current.source_db
      setSourceDbs(db ? [db] : [])
    } finally {
      setLoadingDbs(false)
    }
  }

  async function loadTables(instanceId: string, dbName: string) {
    if (!instanceId || !dbName || isCustom || !liveTableList) {
      setTables([])
      return
    }
    setLoadingTables(true)
    try {
      const { data } = await api.get(
        `/migration/instances/${instanceId}/databases/${encodeURIComponent(dbName)}/tables`,
      )
      const list = (data.data || [])
        .map((t: any) => (typeof t === 'string' ? t : t.table_name || t.name))
        .filter(Boolean) as string[]
      const tn = formRef.current.table_name
      if (tn && !list.includes(tn)) list.unshift(tn)
      setTables(list)
    } catch {
      const tn = formRef.current.table_name
      setTables(tn ? [tn] : [])
    } finally {
      setLoadingTables(false)
    }
  }

  async function loadApprovers(instanceId: string, dbName: string) {
    const set = new Set<string>()
    try {
      const argRes = await api.get('/global-args')
      const args = argRes.data.data || []
      String(args.find((a: any) => a.key === 'migration_approver_user_ids')?.value || '')
        .split(/[,，\s]+/)
        .map((s: string) => s.trim())
        .filter(Boolean)
        .forEach((u: string) => set.add(u))
      const def = String(args.find((a: any) => a.key === 'sync_default_approver')?.value || '').trim()
      if (def) set.add(def)
      const raw = args.find((a: any) => a.key === 'ticket_instance_types')?.value || ''
      const parsed = parseTypes(raw)
      if (parsed.length) setDbTypes(parsed)
    } catch {
      /* ignore */
    }
    if (instanceId) {
      try {
        const { data } = await api.get('/tickets/target-approvers', {
          params: { instance_id: instanceId, db_name: dbName },
        })
        for (const u of data.data || []) if (u) set.add(u)
      } catch {
        /* ignore */
      }
    }
    const list = [...set]
    setApprovers(list)
    setForm((prev) => {
      if (!prev.id && !prev.approver && list.length) return { ...prev, approver: list[0] }
      return prev
    })
  }

  async function loadMeta() {
    const jobs: Promise<any>[] = [
      api.get('/environments').catch(() => ({ data: { data: [] } })),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
    ]
    if (!instances?.length) jobs.push(api.get('/instances').catch(() => ({ data: { data: [] } })))
    const [envRes, argRes, instRes] = await Promise.all(jobs)
    const envs = envRes.data.data || []
    const mapped = envs
      .map((e: any) => ({
        id: e.id,
        value: e.code || e.name || e.id,
        label: e.name && e.code && e.name !== e.code ? `${e.name}` : e.name || e.code,
      }))
      .filter((e: any) => e.value)
    if (mapped.length) setEnvOptions(mapped)
    const args = argRes.data.data || []
    const parsed = parseTypes(args.find((a: any) => a.key === 'ticket_instance_types')?.value || '')
    if (parsed.length) setDbTypes(parsed)
    if (instRes) setLocalInstances(instRes.data.data || [])
    await loadApprovers(formRef.current.source_instance_id, formRef.current.source_db)
  }

  // strategy 变化：套用模版脚本
  const prevStrategyRef = useRef(form.strategy)
  useEffect(() => {
    const prev = prevStrategyRef.current
    prevStrategyRef.current = form.strategy
    if (!open || form.id || form.strategy === prev) return
    setErrors({})
    if (form.strategy === 'custom_script' && isStockScript(form.script_content)) {
      patch({ script_content: SCRIPT_TEMPLATES[form.script_type] || SCRIPT_TEMPLATES.python })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.strategy])

  // script_type 变化：套用模版
  const prevScriptTypeRef = useRef(form.script_type)
  useEffect(() => {
    const prev = prevScriptTypeRef.current
    prevScriptTypeRef.current = form.script_type
    if (!open || form.id || !isCustom || form.script_type === prev) return
    if (isStockScript(form.script_content)) {
      patch({ script_content: SCRIPT_TEMPLATES[form.script_type] || '' })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.script_type])

  // 打开时初始化
  useEffect(() => {
    if (!open) return
    applyTask(task)
    ;(async () => {
      await loadMeta()
      const f = formRef.current
      if (f.source_instance_id && f.strategy !== 'custom_script') {
        await loadDbs(f.source_instance_id, f.source_db)
        await loadTables(f.source_instance_id, f.source_db)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  function validate(): boolean {
    const f = formRef.current
    const errs: Record<string, string> = {}
    if (!String(f.name || '').trim()) errs.name = '请输入任务名'
    if (!lockStrategy && !String(f.strategy || '').trim()) errs.strategy = '请选择任务策略'
    if (!String(f.run_mode || '').trim()) errs.run_mode = '请选择执行方式'
    if (f.run_mode === 'scheduled' && !String(f.schedule_expr || '').trim())
      errs.schedule_expr = '请填写调度配置'
    if (!f.id && !String(f.approver || '').trim()) errs.approver = '请选择审核人'
    if (!String(f.env || '').trim()) errs.env = '请选择环境'
    if (!String(f.db_type || '').trim()) errs.db_type = '请选择数据库类型'
    if (!String(f.source_instance_id || '').trim()) errs.source_instance_id = '请选择源端域名'
    if (f.strategy !== 'custom_script') {
      if (!String(f.source_db || '').trim()) errs.source_db = '请选择源端库名'
      if (!String(f.table_name || '').trim())
        errs.table_name = liveTableList ? '请选择源端表名' : '请输入源端表名'
      if (f.strategy === 'backup_target' && !String(f.target_host || '').trim())
        errs.target_host = '请输入目标域名'
      if (f.strategy === 'archive_oss' && !String(f.oss_bucket || '').trim())
        errs.oss_bucket = '请输入目标桶'
      if (!String(f.sql || '').trim())
        errs.sql = f.strategy === 'delete_only' ? '请输入删除条件' : '请输入归档条件'
    } else {
      if (!String(f.script_type || '').trim()) errs.script_type = '请选择脚本类型'
      if (!String(f.script_content || '').trim()) errs.script_content = '请输入脚本内容'
    }
    setErrors(errs)
    return Object.keys(errs).length === 0
  }

  async function submit() {
    if (!validate()) return
    const f = formRef.current
    setSaving(true)
    try {
      const extra: Record<string, any> = {
        env: f.env,
        db_type: f.db_type,
        target_host: isBackup ? f.target_host : '',
        target_db: isBackup ? f.target_db : '',
        target_table: isBackup ? f.target_table : '',
        oss_bucket: isOss ? f.oss_bucket : '',
        pg_delete_mode:
          isDelete && (f.pg_delete_mode === 'cte' || f.pg_delete_mode === 'shard')
            ? f.pg_delete_mode
            : '',
        script_type: isCustom ? f.script_type : '',
      }
      const body = isCustom ? f.script_content : f.sql
      await api.post('/archive/tasks', {
        name: f.name,
        source_instance_id: f.source_instance_id,
        source_db: isCustom ? '' : f.source_db,
        table_name: isCustom ? f.script_type || 'script' : f.table_name,
        schedule_expr: f.run_mode === 'scheduled' ? f.schedule_expr : '',
        approver: f.approver,
        condition: encodeCondition(body, f.strategy, f.run_mode, f.schedule_expr, extra),
      })
      message.success('工单已提交，审批通过后按任务执行方式运行')
      onClose()
      onSaved?.()
    } finally {
      setSaving(false)
    }
  }

  const disabledById = !!form.id

  return (
    <Modal
      open={open}
      title={form.id ? '编辑任务' : '新建任务'}
      width={800}
      onCancel={onClose}
      footer={null}
      destroyOnClose
      className="archive-create-dialog"
    >
      <Form layout="vertical">
        <Form.Item
          label="任务名"
          required
          validateStatus={errors.name ? 'error' : ''}
          help={errors.name}
        >
          <Input
            value={form.name}
            disabled={disabledById}
            placeholder="请输入任务名"
            maxLength={128}
            showCount
            onChange={(e) => patch({ name: e.target.value })}
          />
        </Form.Item>

        {!lockStrategy && (
          <Form.Item
            label="任务策略"
            required
            validateStatus={errors.strategy ? 'error' : ''}
            help={errors.strategy}
          >
            <Radio.Group
              optionType="button"
              value={form.strategy}
              disabled={disabledById}
              onChange={(e) => patch({ strategy: e.target.value })}
            >
              {STRATEGY_OPTIONS.map((s) => (
                <Radio.Button key={s.value} value={s.value}>
                  {s.label}
                </Radio.Button>
              ))}
            </Radio.Group>
          </Form.Item>
        )}

        <Form.Item
          label="执行方式"
          required
          validateStatus={errors.run_mode ? 'error' : ''}
          help={errors.run_mode}
        >
          <Radio.Group
            optionType="button"
            value={form.run_mode}
            disabled={disabledById}
            onChange={(e) => patch({ run_mode: e.target.value })}
          >
            {RUN_MODE_OPTIONS.map((m) => (
              <Radio.Button key={m.value} value={m.value}>
                {m.label}
              </Radio.Button>
            ))}
          </Radio.Group>
        </Form.Item>

        {form.run_mode === 'scheduled' && (
          <Form.Item
            label={
              <span>
                调度配置
                <Tooltip title="支持 cron 表达式，或如：每日 02:00、每小时" placement="top">
                  <span className="help-dot">?</span>
                </Tooltip>
              </span>
            }
            validateStatus={errors.schedule_expr ? 'error' : ''}
            help={errors.schedule_expr}
          >
            <Input
              value={form.schedule_expr}
              disabled={disabledById}
              placeholder="如: 0 2 * * * 或 每日 02:00"
              onChange={(e) => patch({ schedule_expr: e.target.value })}
            />
          </Form.Item>
        )}

        {!form.id && (
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
              placeholder={approverPlaceholder}
              style={{ width: '100%' }}
              value={form.approver ? [form.approver] : []}
              onChange={(v: string[]) => patch({ approver: v.slice(-1)[0] || '' })}
              options={approvers.map((u) => ({ label: u, value: u }))}
            />
          </Form.Item>
        )}

        <Form.Item
          label="环境"
          required
          validateStatus={errors.env ? 'error' : ''}
          help={errors.env}
        >
          <Select
            allowClear
            placeholder="请选择环境"
            style={{ width: '100%' }}
            disabled={disabledById}
            value={form.env || undefined}
            onChange={(v) => onEnvChange(v)}
            options={envOptions.map((e) => ({ label: e.label, value: e.value }))}
          />
        </Form.Item>

        <Form.Item
          label="数据库类型"
          required
          validateStatus={errors.db_type ? 'error' : ''}
          help={errors.db_type}
        >
          <Select
            allowClear
            placeholder="选择类型"
            style={{ width: '100%' }}
            disabled={disabledById}
            value={form.db_type || undefined}
            onChange={(v) => onTypeChange(v)}
            options={dbTypes.map((t) => ({ label: t.label, value: t.value }))}
          />
        </Form.Item>

        {isCustom ? (
          <>
            <Form.Item
              label="源端域名"
              required
              validateStatus={errors.source_instance_id ? 'error' : ''}
              help={errors.source_instance_id}
            >
              <Select
                showSearch
                optionFilterProp="label"
                disabled={disabledById || !form.db_type}
                placeholder={form.db_type ? '请选择源端数据库域名' : '请先选择数据库类型'}
                style={{ width: '100%' }}
                value={form.source_instance_id || undefined}
                onChange={(v) => patch({ source_instance_id: v })}
                options={sourceInstances.map((i) => ({ label: domainLabel(i), value: i.id }))}
              />
            </Form.Item>
            <Form.Item
              label="脚本类型"
              required
              validateStatus={errors.script_type ? 'error' : ''}
              help={errors.script_type}
            >
              <Select
                placeholder="请选择"
                style={{ width: '100%' }}
                disabled={disabledById}
                value={form.script_type}
                onChange={(v) => patch({ script_type: v })}
                options={SCRIPT_TYPE_OPTIONS.map((s) => ({ label: s.label, value: s.value }))}
              />
            </Form.Item>
            {form.script_type === 'python' && (
              <div className="script-guide">
                <div className="guide-title">业务脚本只需写：</div>
                <pre className="guide-code">{SCRIPT_TEMPLATES.python}</pre>
              </div>
            )}
            {form.script_type === 'python' && (
              <Alert
                type="info"
                showIcon
                className="mb"
                message="脚本仅支持 Python 3 及以上，不支持 Python 2。"
              />
            )}
            {form.script_type === 'go' && (
              <Alert
                type="info"
                showIcon
                className="mb"
                message="请按模版填写区修改后提交。host / dbname / tables 为空时用源端；删除条件为 create_time < today - retainDays。"
              />
            )}
            <Form.Item
              label="脚本内容"
              required
              validateStatus={errors.script_content ? 'error' : ''}
              help={errors.script_content}
            >
              <Input.TextArea
                value={form.script_content}
                rows={form.script_type === 'go' ? 16 : 10}
                disabled={disabledById}
                className="mono"
                placeholder={SCRIPT_TEMPLATES[form.script_type] || '请输入脚本内容'}
                onChange={(e) => patch({ script_content: e.target.value })}
              />
            </Form.Item>
          </>
        ) : (
          <>
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
                    disabled={disabledById || !form.db_type}
                    placeholder={form.db_type ? '请选择源端数据库域名' : '请先选择数据库类型'}
                    style={{ width: '100%' }}
                    value={form.source_instance_id || undefined}
                    onChange={(v) => onSourceChange(v)}
                    options={sourceInstances.map((i) => ({ label: domainLabel(i), value: i.id }))}
                  />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  label="源端库名"
                  required
                  validateStatus={errors.source_db ? 'error' : ''}
                  help={errors.source_db}
                >
                  <Select
                    showSearch
                    mode="tags"
                    maxTagCount={1}
                    disabled={disabledById || !form.source_instance_id}
                    loading={loadingDbs}
                    placeholder={form.source_instance_id ? '请选择源端库名' : '请先选择源端域名'}
                    style={{ width: '100%' }}
                    value={form.source_db ? [form.source_db] : []}
                    onChange={(v: string[]) => onDbChange(v.slice(-1)[0] || '')}
                    options={sourceDbs.map((d) => ({ label: d, value: d }))}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Form.Item
              label="源端表名"
              required
              validateStatus={errors.table_name ? 'error' : ''}
              help={errors.table_name}
            >
              {liveTableList ? (
                <Select
                  showSearch
                  mode="tags"
                  maxTagCount={1}
                  disabled={disabledById || !form.source_db}
                  loading={loadingTables}
                  placeholder={
                    form.source_db
                      ? loadingTables
                        ? '加载源端表名…'
                        : '请选择源端表名'
                      : '请先选择源端库名'
                  }
                  style={{ width: '100%' }}
                  value={form.table_name ? [form.table_name] : []}
                  onChange={(v: string[]) => patch({ table_name: v.slice(-1)[0] || '' })}
                  options={tables.map((t) => ({ label: t, value: t }))}
                />
              ) : (
                <Input
                  value={form.table_name}
                  disabled={disabledById}
                  placeholder="源端表名"
                  onChange={(e) => patch({ table_name: e.target.value })}
                />
              )}
            </Form.Item>

            {isBackup && (
              <>
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item
                      label="目标域名"
                      required
                      validateStatus={errors.target_host ? 'error' : ''}
                      help={errors.target_host}
                    >
                      <Input
                        value={form.target_host}
                        disabled={disabledById}
                        placeholder="目标数据库域名"
                        onChange={(e) => patch({ target_host: e.target.value })}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="目标库名">
                      <Input
                        value={form.target_db}
                        disabled={disabledById}
                        placeholder="目标库名"
                        onChange={(e) => patch({ target_db: e.target.value })}
                      />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item label="目标表名">
                  <Input
                    value={form.target_table}
                    disabled={disabledById}
                    placeholder="目标表名"
                    onChange={(e) => patch({ target_table: e.target.value })}
                  />
                </Form.Item>
              </>
            )}

            {isOss && (
              <Form.Item
                label="目标桶"
                required
                validateStatus={errors.oss_bucket ? 'error' : ''}
                help={errors.oss_bucket}
              >
                <Input
                  value={form.oss_bucket}
                  disabled={disabledById}
                  placeholder="如: archive-bucket 或 bucket/prefix"
                  onChange={(e) => patch({ oss_bucket: e.target.value })}
                />
              </Form.Item>
            )}

            {isDelete && (
              <Form.Item
                label={
                  <span>
                    PG 删除模式
                    <Tooltip
                      placement="topLeft"
                      title={
                        <div className="tip-block">
                          <div>
                            <b>自动：</b>不选手动时由系统按预估数据量选择；不传 pg_delete_mode，由后端自动兜底。
                          </div>
                          <div>
                            <b>CTE：</b>适合中小表。按批次循环删除直到删完。
                          </div>
                          <div>
                            <b>Shard：</b>适合超大表。按创建时间切分并导出主键后再删。
                          </div>
                        </div>
                      }
                    >
                      <span className="help-dot">?</span>
                    </Tooltip>
                  </span>
                }
              >
                <Radio.Group
                  optionType="button"
                  value={form.pg_delete_mode}
                  disabled={disabledById}
                  onChange={(e) => patch({ pg_delete_mode: e.target.value })}
                >
                  {PG_DELETE_MODE_OPTIONS.map((m) => (
                    <Radio.Button key={m.value} value={m.value}>
                      {m.label}
                    </Radio.Button>
                  ))}
                </Radio.Group>
              </Form.Item>
            )}

            <Form.Item
              label={
                <span>
                  {conditionLabel}
                  <Tooltip title={conditionTip} placement="top">
                    <span className="help-dot">?</span>
                  </Tooltip>
                </span>
              }
              required
              validateStatus={errors.sql ? 'error' : ''}
              help={errors.sql}
            >
              <Input.TextArea
                value={form.sql}
                rows={4}
                disabled={disabledById}
                placeholder="如: create_time < '2024-01-01'"
                onChange={(e) => patch({ sql: e.target.value })}
              />
            </Form.Item>
          </>
        )}

        <div className="dialog-actions">
          {!form.id && (
            <Button type="primary" loading={saving} onClick={submit}>
              提交工单
            </Button>
          )}
          <Button disabled={saving} onClick={() => (form.id ? onClose() : reset())}>
            {form.id ? '关闭' : '重置'}
          </Button>
        </div>
      </Form>
    </Modal>
  )
}
