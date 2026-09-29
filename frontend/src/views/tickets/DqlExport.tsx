import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Form, Input, Select, Space, Switch, message } from 'antd'
import api from '../../api/request'
import { dbLabel } from '../../utils/db'
import { buildEnvOptions } from '../../utils/env'
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

export default function DqlExport() {
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()
  const [saving, setSaving] = useState(false)
  const [instances, setInstances] = useState<any[]>([])
  const [databases, setDatabases] = useState<any[]>([])
  const [envs, setEnvs] = useState<string[]>([])
  const [dbTypes, setDbTypes] = useState(DEFAULT_DB_TYPES)
  const [approvers, setApprovers] = useState<string[]>([])
  const [extraApprovers, setExtraApprovers] = useState<string[]>([])
  const [rowCount, setRowCount] = useState(0)
  const [resultCols, setResultCols] = useState<string[]>([])
  const rowCountRef = useRef(rowCount)
  const resultColsRef = useRef(resultCols)
  rowCountRef.current = rowCount
  resultColsRef.current = resultCols

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
      setForm(next)
      antdForm.setFieldsValue(next)
      setRowCount(Number(d.row_count || 0))
      setResultCols(Array.isArray(d.columns) ? d.columns : [])
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, rowCount, resultCols, schedulePersist])

  const instMap = useMemo(() => {
    const m: Record<string, any> = {}
    for (const i of instances) m[i.id] = i
    return m
  }, [instances])

  const dbOptions = useMemo(() => {
    return databases.filter((d) => {
      const inst = instMap[d.instance_id] || {}
      if (form.db_type && String(inst.db_type || '').toLowerCase() !== form.db_type) return false
      if (form.env && (inst.idc || '') !== form.env) return false
      return true
    })
  }, [databases, instMap, form.db_type, form.env])

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
      if (next.env && (inst.idc || '') !== next.env) return false
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
    const { data } = await api.get('/tickets/target-approvers', {
      params: { instance_id: instanceId, db_name: dbName },
    })
    const list = (data.data || []).map((a: any) => a.name || a).filter(Boolean)
    setApprovers(list)
    if (!formRef.current.assigned_to && list[0]) {
      patchForm({ assigned_to: list[0] })
    }
  }

  async function onDbChange(databaseId: string) {
    const db = databases.find((d) => d.id === databaseId) || null
    const instanceId = db?.instance_id || ''
    const dbName = db?.db_name || ''
    const inst = instMap[instanceId]
    const patch: Partial<ReturnType<typeof emptyForm>> = {
      database_id: databaseId,
      instance_id: instanceId,
      db_name: dbName,
    }
    if (inst) {
      if (!formRef.current.db_type) patch.db_type = String(inst.db_type || '').toLowerCase()
      if (!formRef.current.env) patch.env = inst.idc || ''
    }
    patchForm(patch)
    await loadApprovers(instanceId, dbName)
  }

  function applyDraft() {
    restoreDraft()
  }

  async function loadMeta() {
    const [instRes, dbRes, envRes, argRes, apprRes] = await Promise.all([
      api.get('/instances'),
      api.get('/databases'),
      api.get('/environments').catch(() => ({ data: { data: [] } })),
      api.get('/global-args').catch(() => ({ data: { data: [] } })),
      api.get('/create-db/approvers').catch(() => ({ data: { data: [] } })),
    ])
    const instList = instRes.data.data || []
    setInstances(instList)
    setDatabases(dbRes.data.data || [])
    setEnvs(buildEnvOptions(envRes.data.data || [], instList).map((e: any) => e.value))
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
    if (hasDBA !== hasSec) {
      message.error('请同步设置 DBA审批人 和 安全审批人')
      return
    }
    if (f.need_sensitive_data && !hasDBA) {
      message.error('开启原文导出敏感字段需设置DBA审批人和安全审批人')
      return
    }
    if (rowCount > 500 && !hasDBA) {
      message.error('导出数据超过500条需设置DBA审批人和安全审批人')
      return
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
        dba_assigned_to: f.dba_assigned_to,
        security_assigned_to: f.security_assigned_to,
        row_count: rowCount,
      })
      message.success('工单创建成功 ' + (data.data?.ticket_no || ''))
      clearOnSubmitSuccess()
      navigate('/ops/tickets/' + (data.data?.id || ''))
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      applyDraft()
      await loadMeta()
      if (cancelled) return
      const f = formRef.current
      if (f.database_id) await onDbChange(f.database_id)
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
              options={envs.map((e) => ({ value: e, label: e }))}
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

          <div className="warn-box">
            <div className="warn-rules">
              <p>
                1. 原文导出 <b>敏感字段</b> 需开启原文导出敏感字段，并配置安全和DBA审批，
                <b>否则此字段加密导出。</b>
              </p>
              <div className="mask-line">
                <span>检测到查询敏感信息：</span>
                {!matchMask.length ? <span className="muted">无</span> : null}
                {matchMask.map((m) => (
                  <span key={m.key} className="mask-chip">
                    表:{m.table || '—'}，预设敏感字段：{m.columns.join(', ')}
                  </span>
                ))}
              </div>
              <p>
                2. 导出 <b>数据超过500条</b> 需配置安全和DBA审批，<b>否则禁止导出。</b>
              </p>
              <p className="warn-strong">非以上场景无需设置安全和DBA审批。</p>
            </div>
            <Form.Item name="need_sensitive_data" label="原文导出敏感字段" valuePropName="checked">
              <Switch checkedChildren="开启" unCheckedChildren="关闭" />
            </Form.Item>
            <Form.Item name="dba_assigned_to" label="DBA审批人">
              <Select
                allowClear
                showSearch
                placeholder="请选择审批人"
                options={extraApprovers.map((a) => ({ value: a, label: a }))}
              />
            </Form.Item>
            <Form.Item name="security_assigned_to" label="安全审批人">
              <Select
                allowClear
                showSearch
                placeholder="请选择审批人"
                options={extraApprovers.map((a) => ({ value: a, label: a }))}
              />
            </Form.Item>
          </div>

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
              <Button onClick={() => navigate('/ops/tickets')}>取消</Button>
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
            <Input.TextArea placeholder="SELECT ... " className="dql-sql-editor" />
          </Form.Item>
          <p className="dql-sql-hint">编辑框内填写导出查询 SQL；提交前请确认语句只读。</p>
        </main>
      </Form>
    </div>
  )
}
