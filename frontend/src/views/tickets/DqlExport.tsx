import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Button, Form, Input, Select, Space, Switch, message } from 'antd'
import api from '../../api/request'
import { useAppDispatch, useAppSelector } from '../../store'
import { goTicketListAndClose } from './goTicketList'
import { dbLabel } from '../../utils/db'
import { envCodeInGroup, loadTicketEnvGroups, resolveTicketEnvValue, type TicketEnvGroup } from '../../utils/ticketEnv'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import './DqlExport.css'

const SENSITIVE =
  /phone|mobile|email|mail|id_card|idcard|身份证|手机|邮箱|password|passwd|secret|token|card_no|bank|身份证号/i

const DEFAULT_DB_TYPES = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'starrocks', label: 'StarRocks' },
  { value: 'clickhouse', label: 'ClickHouse' },
]

function emptyForm() {
  return {
    db_type: '',
    env: '',
    database_id: '',
    instance_id: '',
    db_name: '',
    sql_content: '',
    assigned_to: '',
    need_sensitive_data: false,
    dba_assigned_to: '',
    security_assigned_to: '',
    description: '',
  }
}

function parseTables(sql: string) {
  const out: string[] = []
  const re = /\b(?:from|join)\s+([`"']?[\w.]+[`"']?)/gi
  let m: RegExpExecArray | null
  while ((m = re.exec(String(sql || '')))) {
    const name = String(m[1] || '').replace(/[`"']/g, '')
    if (name && !out.includes(name)) out.push(name)
  }
  return out
}

function stripSQLComments(sql: string) {
  return String(sql || '')
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/--[^\n]*/g, ' ')
}

/** 解析 SQL 中最后一个 LIMIT 行数；LIMIT a,b 取 b；无 LIMIT 返回 null */
function parseLimitFromSQL(sql: string): number | null {
  const s = stripSQLComments(sql)
  const re = /\blimit\s+(\d+)(?:\s*,\s*(\d+))?/gi
  let last: number | null = null
  let m: RegExpExecArray | null
  while ((m = re.exec(s))) {
    const n = m[2] != null ? Number(m[2]) : Number(m[1])
    if (Number.isFinite(n) && n >= 0) last = n
  }
  return last
}

export default function DqlExport() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)

  const leaveToList = () => goTicketListAndClose(dispatch, navigate, location, menus)
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()
  const [saving, setSaving] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [envGroups, setEnvGroups] = useState<TicketEnvGroup[]>([])
  const envGroupsRef = useRef<TicketEnvGroup[]>([])
  const [dbTypes, setDbTypes] = useState(DEFAULT_DB_TYPES)
  const [approvers, setApprovers] = useState<string[]>([])
  const [extraApprovers, setExtraApprovers] = useState<string[]>([])
  /** 草稿带来的行数提示；仅当 SQL 无 LIMIT 时参与估算 */
  const [draftRowHint, setDraftRowHint] = useState(0)
  const [resultCols, setResultCols] = useState<string[]>([])
  const resultColsRef = useRef(resultCols)
  resultColsRef.current = resultCols

  const rowCount = useMemo(() => {
    const lim = parseLimitFromSQL(form.sql_content)
    if (lim != null) return lim
    // 无 LIMIT：保守按需审批，避免漏审
    return Math.max(draftRowHint, 501)
  }, [form.sql_content, draftRowHint])
  const rowCountRef = useRef(rowCount)
  rowCountRef.current = rowCount

  const needStrictApproval = !!(form.need_sensitive_data || rowCount > 500)

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => {
      const next = { ...f, ...patch }
      antdForm.setFieldsValue(next)
      return next
    })
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'dql',
    label: '数据导出',
    path: '/ops/dql',
    getValues: () => ({
      ...formRef.current,
      row_count: rowCountRef.current,
      columns: resultColsRef.current,
    }),
    applyValues: (d) => {
      const next = {
        ...emptyForm(),
        database_id: d.database_id || '',
        instance_id: d.instance_id || '',
        db_name: d.db_name || '',
        env: d.env || '',
        db_type: String(d.db_type || '').toLowerCase(),
        sql_content: d.sql_content || d.sql || '',
        assigned_to: d.assigned_to || '',
        need_sensitive_data: !!d.need_sensitive_data,
        dba_assigned_to: d.dba_assigned_to || '',
        security_assigned_to: d.security_assigned_to || '',
        description: d.description || '',
      }
      formRef.current = next
      setForm(next)
      antdForm.setFieldsValue(next)
      setDraftRowHint(Number(d.row_count || 0))
      setResultCols(Array.isArray(d.columns) ? d.columns : [])
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, rowCount, resultCols, schedulePersist])

  // 不需要加强审批时清空 DBA/安全，避免误提交
  useEffect(() => {
    if (needStrictApproval) return
    if (!form.dba_assigned_to && !form.security_assigned_to) return
    patchForm({ dba_assigned_to: '', security_assigned_to: '' })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [needStrictApproval])

  const instMap = useMemo(() => {
    const m: Record<string, any> = {}
    for (const i of instances) m[i.id] = i
    return m
  }, [instances])

  const dbOptions = useMemo(() => {
    return databases.filter((d) => {
      const inst = instMap[d.instance_id] || {}
      if (form.db_type && String(inst.db_type || '').toLowerCase() !== form.db_type) return false
      if (form.env && !envCodeInGroup(inst.idc || '', form.env, envGroups)) return false
      return true
    })
  }, [databases, instMap, form.db_type, form.env, envGroups])

  const parsedTables = useMemo(() => parseTables(form.sql_content), [form.sql_content])

  const matchMask = useMemo(() => {
    const cols = resultCols.filter((c) => SENSITIVE.test(c))
    const sqlHits = [...String(form.sql_content || '').matchAll(/[`"'.\w]+/g)]
      .map((m) => m[0].replace(/[`"']/g, ''))
      .filter((n) => SENSITIVE.test(n))
    const names = [...new Set([...cols, ...sqlHits])]
    if (!names.length) return []
    const table = parsedTables[0] || ''
    return [{ key: names.join(','), table, columns: names }]
  }, [resultCols, form.sql_content, parsedTables])

  function onFilterChange(patch: Partial<ReturnType<typeof emptyForm>>) {
    const next = { ...formRef.current, ...patch }
    const options = databases.filter((d) => {
      const inst = instMap[d.instance_id] || {}
      if (next.db_type && String(inst.db_type || '').toLowerCase() !== next.db_type) return false
      if (next.env && !envCodeInGroup(inst.idc || '', next.env, envGroupsRef.current)) return false
      return true
    })
    if (next.database_id && !options.some((d) => d.id === next.database_id)) {
      patchForm({ ...patch, database_id: '', instance_id: '', db_name: '' })
      setApprovers([])
      return
    }
    patchForm(patch)
  }

  async function loadApprovers(instanceId: string, dbName: string) {
    if (!instanceId) {
      setApprovers([])
      return
    }
    try {
      const { data } = await api.get('/tickets/target-approvers', {
        params: { instance_id: instanceId, db_name: dbName },
      })
      const list = (data.data || []).map((a: any) => a.name || a).filter(Boolean)
      setApprovers(list)
      if (!formRef.current.assigned_to && list[0]) {
        patchForm({ assigned_to: list[0] })
      }
    } catch {
      setApprovers([])
    }
  }

  async function onDbChange(
    databaseId: string,
    opts?: { databases?: any[]; instances?: any[] },
  ) {
    const dbList = opts?.databases || databases
    const instList = opts?.instances || instances
    const localInstMap: Record<string, any> = {}
    for (const i of instList) localInstMap[i.id] = i
    const db = dbList.find((d) => d.id === databaseId) || null
    const instanceId = db?.instance_id || formRef.current.instance_id || ''
    const dbName = db?.db_name || formRef.current.db_name || ''
    const inst = localInstMap[instanceId]
    const patch: Partial<ReturnType<typeof emptyForm>> = {
      database_id: databaseId,
      instance_id: instanceId,
      db_name: dbName,
    }
    if (inst) {
      if (!formRef.current.db_type) patch.db_type = String(inst.db_type || '').toLowerCase()
      if (!formRef.current.env) patch.env = resolveTicketEnvValue(inst.idc || '', envGroupsRef.current)
    }
    patchForm(patch)
    await loadApprovers(instanceId, dbName)
  }

  function applyDraft() {
    restoreDraft()
  }

  async function loadMeta(): Promise<{ instances: any[]; databases: any[] }> {
    const [instRes, dbRes, argRes, apprRes] = await Promise.all([
      api.get('/instances'),
      api.get('/databases'),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
      api.get('/create-db/approvers').catch(() => ({ data: { data: [] } })),
    ])
    const instList = instRes.data.data || []
    const dbList = dbRes.data.data || []
    setInstances(instList)
    setDatabases(dbList)
    const groups = await loadTicketEnvGroups()
    envGroupsRef.current = groups
    setEnvGroups(groups)
    const raw = (argRes.data.data || []).find((a: any) => a.key === 'ticket_instance_types')?.value || ''
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length) {
        setDbTypes(
          parsed
            .filter(
              (t: any) =>
                t.value && !['kafka', 'redis', 'mongodb', 'elasticsearch', 'cassandra'].includes(t.value),
            )
            .map((t: any) => ({ value: t.value, label: t.label || t.value })),
        )
      }
    } catch {
      /* keep default */
    }
    setExtraApprovers([
      ...new Set<string>((apprRes.data.data || []).map((a: any) => a.name || a).filter(Boolean)),
    ])
    return { instances: instList, databases: dbList }
  }

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    const f = formRef.current
    if (!f.sql_content.trim()) {
      message.error('SQL语句不能为空')
      return
    }
    const hasDBA = !!f.dba_assigned_to
    const hasSec = !!f.security_assigned_to
    const strict = !!(f.need_sensitive_data || rowCount > 500)
    if (strict) {
      if (hasDBA !== hasSec || !hasDBA) {
        message.error('请同步设置 DBA审批人 和 安全审批人')
        return
      }
    }
    setSaving(true)
    try {
      const { data } = await api.post('/tickets', {
        type: 'dql',
        instance_id: f.instance_id,
        database_id: f.database_id,
        db_name: f.db_name,
        sql_content: f.sql_content,
        description: f.description,
        assigned_to: f.assigned_to,
        need_sensitive_data: f.need_sensitive_data,
        dba_assigned_to: strict ? f.dba_assigned_to : '',
        security_assigned_to: strict ? f.security_assigned_to : '',
        row_count: rowCount,
      })
      message.success('工单创建成功 ' + (data.data?.ticket_no || ''))
      clearOnSubmitSuccess()
      leaveToList()
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      applyDraft()
      const meta = await loadMeta()
      if (cancelled) return
      const mapped = resolveTicketEnvValue(formRef.current.env, envGroupsRef.current)
      if (mapped !== formRef.current.env) {
        formRef.current = { ...formRef.current, env: mapped }
        patchForm({ env: mapped })
      }
      const picked = (meta.databases || []).find((d: any) => d.id === formRef.current.database_id)
      const pickedInst = (meta.instances || []).find((i: any) => i.id === (picked?.instance_id || formRef.current.instance_id))
      if (
        formRef.current.env &&
        pickedInst &&
        !envCodeInGroup(pickedInst.idc || '', formRef.current.env, envGroupsRef.current)
      ) {
        formRef.current = { ...formRef.current, database_id: '', instance_id: '', db_name: '' }
        patchForm({ database_id: '', instance_id: '', db_name: '' })
      }
      // setForm 异步，草稿恢复后用 antdForm 同步取值，避免跳过审批人加载
      const af = antdForm.getFieldsValue(true) || {}
      const f = { ...formRef.current, ...af }
      if (f.database_id) {
        await onDbChange(String(f.database_id), meta)
      } else if (f.instance_id) {
        await loadApprovers(String(f.instance_id), String(f.db_name || ''))
      }
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 库/实例变化时刷新审批人（手动改选或其它恢复路径）
  const approverKeyRef = useRef('')
  useEffect(() => {
    const instanceId = String(form.instance_id || '').trim()
    const dbName = String(form.db_name || '').trim()
    const databaseId = String(form.database_id || '').trim()
    const key = `${databaseId}|${instanceId}|${dbName}`
    if (!instanceId || key === approverKeyRef.current) return
    approverKeyRef.current = key
    void loadApprovers(instanceId, dbName)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.database_id, form.instance_id, form.db_name])

  return (
    <div className="dql-page order-page">
      <Form
        form={antdForm}
        layout="vertical"
        className="order-split dql-split-form"
        initialValues={form}
        onValuesChange={(_, all) => setForm((f) => ({ ...f, ...all }))}
        onSubmitCapture={(e) => e.preventDefault()}
      >
        <aside className="order-aside">
          <Form.Item name="db_type" label="数据库类型" rules={[{ required: true, message: '请选择数据库类型' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择数据库类型"
              onChange={(v) => onFilterChange({ db_type: v })}
              options={dbTypes.map((t) => ({
                value: t.value,
                label: (
                  <span className="type-opt">
                    <DbTypeIcon type={t.value} size={16} />
                    <span>{t.label}</span>
                  </span>
                ),
              }))}
            />
          </Form.Item>
          <Form.Item name="env" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              showSearch
              placeholder="请选择环境"
              onChange={(v) => onFilterChange({ env: v })}
              options={envGroups.map((e) => ({ value: e.code, label: e.label }))}
            />
          </Form.Item>
          <Form.Item name="database_id" label="数据库" rules={[{ required: true, message: '请选择数据库' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择数据库"
              onChange={(v) => onDbChange(v)}
              options={dbOptions.map((d) => ({ value: d.id, label: dbLabel(d) }))}
            />
          </Form.Item>
          <Form.Item
            name="assigned_to"
            label="选择导出审批人"
            rules={[{ required: true, message: '请选择审批人' }]}
            extra={<div className="form-extra">如果没有审批人，请联系运维配置。</div>}
          >
            <Select
              showSearch
              placeholder="请选择审批人"
              options={approvers.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>

          <Form.Item
            name="need_sensitive_data"
            label="原文导出敏感字段"
            valuePropName="checked"
            extra={
              matchMask.length ? (
                <div className="form-extra">
                  检测到敏感字段：{matchMask.map((m) => m.columns.join(', ')).join('；')}（默认加密导出）
                </div>
              ) : undefined
            }
          >
            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
          </Form.Item>
          {needStrictApproval ? (
            <>
              <Form.Item
                name="dba_assigned_to"
                label="DBA审批人"
                rules={[{ required: true, message: '请选择DBA审批人' }]}
              >
                <Select
                  allowClear
                  showSearch
                  placeholder="请选择审批人"
                  options={extraApprovers.map((a) => ({ value: a, label: a }))}
                />
              </Form.Item>
              <Form.Item
                name="security_assigned_to"
                label="安全审批人"
                rules={[{ required: true, message: '请选择安全审批人' }]}
              >
                <Select
                  allowClear
                  showSearch
                  placeholder="请选择审批人"
                  options={extraApprovers.map((a) => ({ value: a, label: a }))}
                />
              </Form.Item>
            </>
          ) : null}

          <Form.Item
            name="description"
            label="导出原因"
            rules={[{ required: true, message: '请输入导出原因' }]}
          >
            <Input.TextArea rows={2} placeholder="请输入导出原因" />
          </Form.Item>
          <Form.Item className="aside-actions">
            <Space>
              <Button type="primary" loading={saving} onClick={submit}>
                提交申请
              </Button>
              <Button onClick={() => saveDraft()}>保存草稿</Button>
              <Button onClick={leaveToList}>取消</Button>
            </Space>
          </Form.Item>
        </aside>

        <main className="order-main">
          <div className="dql-main-title">填写SQL</div>
          <Form.Item
            name="sql_content"
            required
            className="dql-sql-item"
            rules={[{ required: true, message: 'SQL语句不能为空' }]}
          >
            <Input.TextArea placeholder="SELECT ... " className="dql-sql-editor" rows={12} />
          </Form.Item>
          <p className="dql-sql-hint">编辑框内填写导出查询 SQL；提交前请确认语句只读。</p>
        </main>
      </Form>
    </div>
  )
}
