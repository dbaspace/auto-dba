import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Button, DatePicker, Form, Input, Select, Space, Tooltip, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import dayjs, { Dayjs } from 'dayjs'
import api from '../../api/request'
import { buildEnvOptions } from '../../utils/env'
import {
  filterAccessDomainRows,
  parseSourceKey,
  sourcePrimaryLabel,
  sourceSecondaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
  type DomainSource,
} from '../../utils/instanceSource'
import DbTypeIcon from '../../components/DbTypeIcon'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import { useAppDispatch, useAppSelector } from '../../store'
import { goTicketListAndClose } from './goTicketList'
import './CreateDb.css'

function unionApproverNames(raws: string[]) {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of raws) {
    for (const name of String(raw || '').split(/[,，\s]+/)) {
      const n = name.trim()
      if (!n || seen.has(n)) continue
      seen.add(n)
      out.push(n)
    }
  }
  return out
}

const permissionOps = [
  { label: '读操作', value: 'r' },
  { label: '写操作', value: 'c' },
  { label: '更新操作', value: 'u' },
  { label: '删除操作', value: 'd' },
]

const DB_TYPES = [
  { value: 'mysql', label: 'MySQL' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'starrocks', label: 'StarRocks' },
]

function emptyForm() {
  return {
    env: '',
    instance_type: '',
    instance_ids: [] as string[],
    username: '',
    permissions: [] as string[],
    assigned_to: '',
    description: '',
    scheduled_at: '' as string,
    sql_content: '',
  }
}

function levelLabel(lv: any) {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return '错误'
  if (x === 'WARN' || x === 'WARNING') return '警告'
  if (x === 'INFO' || x === 'OK') return '通过'
  return lv || '—'
}

function levelClass(lv: any) {
  const x = String(lv || '').toUpperCase()
  if (x === 'ERROR') return 'lv-error'
  if (x === 'WARN' || x === 'WARNING') return 'lv-warn'
  if (x === 'INFO' || x === 'OK') return 'lv-ok'
  return ''
}

function parseSelectedSources(keys: string[]) {
  return keys
    .map((k) => parseSourceKey(k))
    .filter((p): p is { instance_id: string; domain: string } => Boolean(p))
}

export default function CreateDb() {
  const navigate = useNavigate()
  const location = useLocation()
  const dispatch = useAppDispatch()
  const menus = useAppSelector((s) => s.auth.menus)
  const [form, setForm] = useState(emptyForm())
  const formMirror = useRef(form)
  formMirror.current = form
  const [antdForm] = Form.useForm()
  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [approvers, setApprovers] = useState<string[]>([])
  const [checkRows, setCheckRows] = useState<any[]>([])
  const [lastCheckFp, setLastCheckFp] = useState<string | null>(null)
  const [dsLoading, setDsLoading] = useState(false)
  const [reviewerLoading, setReviewerLoading] = useState(false)
  const [checkLoading, setCheckLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [defaultReviewer, setDefaultReviewer] = useState('')

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const dataSourceOptions = useMemo((): DomainSource[] => {
    return sourcesFromAccessDomains(
      filterAccessDomainRows(accessDomains, { env: form.env, dbType: form.instance_type }),
    )
  }, [accessDomains, form.env, form.instance_type])

  const selectedSources = useMemo(() => parseSelectedSources(form.instance_ids), [form.instance_ids])

  const checkFp = useMemo(
    () =>
      JSON.stringify({
        env: form.env,
        instance_type: form.instance_type,
        instance_ids: [...form.instance_ids].sort(),
        username: String(form.username || '').trim(),
        permissions: [...form.permissions].sort(),
        sql: String(form.sql_content || '').trim(),
      }),
    [form],
  )

  const checkHasError = checkRows.some((r) => String(r.error_level || '').toUpperCase() === 'ERROR')
  const fingerprintMatches = lastCheckFp != null && lastCheckFp === checkFp
  const submitAllowed = fingerprintMatches && !checkHasError
  const submitDisabledReason =
    lastCheckFp == null
      ? '请先点击「SQL检测」'
      : !fingerprintMatches
        ? 'SQL 或表单与上次检测不一致，请重新检测后再提交'
        : checkHasError
          ? 'SQL 检测存在错误，请修复后再提交'
          : ''

  const dsPlaceholder = !form.env
    ? '请先选择环境'
    : !form.instance_type
      ? '请先选择实例类型'
      : dsLoading
        ? '加载数据源…'
        : '选择数据源(域名) 或搜索...'
  const reviewerPlaceholder = reviewerLoading ? '加载审批人…' : approvers.length ? '请选择审批人' : '暂无审批人'

  function resetCheck() {
    setLastCheckFp(null)
    setCheckRows([])
  }

  function onEnvOrTypeChange(patch: Partial<ReturnType<typeof emptyForm>>) {
    patchForm({ ...patch, instance_ids: [] })
    resetCheck()
  }

  function reset() {
    const next = emptyForm()
    if (defaultReviewer) next.assigned_to = defaultReviewer
    setForm(next)
    antdForm.resetFields()
    antdForm.setFieldsValue(next)
    setCheckRows([])
    setLastCheckFp(null)
  }

  async function check() {
    const sql = String(form.sql_content || '').trim()
    if (!sql) {
      message.warning('请填写 SQL')
      return
    }
    if (
      !form.env ||
      !form.instance_type ||
      !selectedSources.length ||
      !String(form.username || '').trim() ||
      !form.permissions.length
    ) {
      message.warning('检测前请先选择环境、实例类型、数据源、数据库账号与账号权限')
      return
    }
    const instanceIds = [...new Set(selectedSources.map((s) => s.instance_id))]
    const fp = checkFp
    setCheckLoading(true)
    try {
      const { data } = await api.post('/create-db/check-sql', {
        db_type: form.instance_type,
        idc_scope: form.env,
        instance_id: instanceIds[0],
        instance_ids: instanceIds,
        sql_content: sql,
      })
      setCheckRows(
        (data.statements || []).map((r: any) => ({
          stage: r.stage || 'parse',
          error_level: r.error_level || '',
          message: r.message || '',
          sql: r.sql || sql,
          affected_rows: r.affected_rows ?? '—',
        })),
      )
      message[data.ok ? 'success' : 'warning'](data.ok ? '检测通过' : '检测未通过')
    } catch (e: any) {
      const msg = e.response?.data?.message || e.message || 'SQL 检测请求失败'
      message.error(msg)
      setCheckRows([{ stage: '—', error_level: 'ERROR', message: msg, sql, affected_rows: '—' }])
    } finally {
      setCheckLoading(false)
      setLastCheckFp(fp)
    }
  }

  function beautify() {
    const sql = String(form.sql_content || '').trim()
    if (!sql) {
      message.warning('请填写 SQL')
      return
    }
    patchForm({
      sql_content: sql
        .replace(/\s+/g, ' ')
        .replace(/\s*;\s*/g, ';\n')
        .replace(/\b(CREATE DATABASE|IF NOT EXISTS)\b/gi, '\n$1')
        .replace(/\n+/g, '\n')
        .trim(),
    })
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'createdb',
    label: '新增数据库',
    path: '/ops/createdb',
    getValues: () => ({ ...formMirror.current }),
    applyValues: (d) => {
      const next = {
        ...emptyForm(),
        ...d,
        instance_ids: Array.isArray(d.instance_ids) ? d.instance_ids : [],
        permissions: Array.isArray(d.permissions) ? d.permissions : [],
      }
      setForm(next)
      antdForm.setFieldsValue(next)
    },
    enabled: true,
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      message.warning('请完善表单必填项后再提交')
      return
    }
    const sql = String(form.sql_content || '').trim()
    if (!sql) {
      message.error('请填写 SQL')
      return
    }
    if (!submitAllowed) {
      message.warning(submitDisabledReason)
      return
    }
    const sources = selectedSources
    if (!sources.length) {
      message.warning('未解析到有效数据源，请重新选择数据源后再提交')
      return
    }
    const instanceIds = [...new Set(sources.map((s) => s.instance_id))]
    setSubmitting(true)
    try {
      const { data } = await api.post('/create-db', {
        db_type: form.instance_type,
        idc_scope: form.env,
        instance_id: instanceIds[0],
        instance_ids: instanceIds,
        domain: sources[0].domain,
        data_sources: sources.map((s) => ({ instance_id: s.instance_id, domain: s.domain })),
        username: String(form.username || '').trim(),
        permissions: form.permissions,
        assigned_to: form.assigned_to,
        description: form.description,
        scheduled_at: form.scheduled_at || '',
        sql_content: sql,
      })
      const no = data.ticket_no || data.data?.ticket_no || ''
      message.success(
        no
          ? `提交成功，工单号 ${no}。工单审批通过后，账号将写入「配置 → 账号管理」，密码加密存储。`
          : '提交成功。工单审批通过后，账号将写入「配置 → 账号管理」。',
      )
      clearOnSubmitSuccess()
      goTicketListAndClose(dispatch, navigate, location, menus)
    } catch (e: any) {
      message.error(e.response?.data?.message || '提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    ;(async () => {
      restoreDraft()
      setDsLoading(true)
      setReviewerLoading(true)
      try {
        const [domRes, envRes, apprRes] = await Promise.all([
          api.get('/access-domains'),
          api.get('/environments').catch(() => ({ data: { data: [] } })),
          api.get('/create-db/approvers'),
        ])
        const domains: AccessDomainRow[] = domRes.data.data || []
        setAccessDomains(domains)
        setEnvOptions(buildEnvOptions(envRes.data.data || [], domains))
        const uniq = unionApproverNames(
          (apprRes.data.data || []).map((a: any) => String(a?.name || a || '')),
        )
        setApprovers(uniq)
        const def = uniq[0] || ''
        setDefaultReviewer(def)
        setForm((f) => {
          const picked = unionApproverNames([f.assigned_to]).find((n) => uniq.includes(n)) || ''
          const next = picked || def
          if (next && next !== f.assigned_to) {
            antdForm.setFieldsValue({ assigned_to: next })
            return { ...f, assigned_to: next }
          }
          return f
        })
        setForm((f) => {
          const valid = new Set(
            sourcesFromAccessDomains(
              filterAccessDomainRows(domains, { env: f.env, dbType: f.instance_type }),
            ).map((s) => s.key),
          )
          return { ...f, instance_ids: (f.instance_ids || []).filter((id) => valid.has(id)) }
        })
      } finally {
        setDsLoading(false)
        setReviewerLoading(false)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    antdForm.setFieldsValue(form)
  }, [form, antdForm])

  const scheduledValue: Dayjs | null = form.scheduled_at ? dayjs(form.scheduled_at) : null

  return (
    <div className="order-page">
      <div className="order-split">
        <aside className="order-aside">
          <Form form={antdForm} layout="vertical" initialValues={form}>
            <Form.Item label="环境" name="env" rules={[{ required: true, message: '请选择环境' }]}>
              <Select
                showSearch
                allowClear
                placeholder="请选择"
                value={form.env || undefined}
                onChange={(v) => onEnvOrTypeChange({ env: v || '' })}
                options={envOptions}
              />
            </Form.Item>
            <Form.Item label="实例类型" name="instance_type" rules={[{ required: true, message: '请选择实例类型' }]}>
              <Select
                showSearch
                allowClear
                disabled={!form.env}
                placeholder={form.env ? '请选择' : '请先选择环境'}
                value={form.instance_type || undefined}
                onChange={(v) => onEnvOrTypeChange({ instance_type: v || '' })}
                optionLabelProp="label"
                options={DB_TYPES.map((t) => ({
                  value: t.value,
                  label: t.label,
                  optionLabel: (
                    <span className="type-opt">
                      <DbTypeIcon type={t.value} size={16} />
                      <span>{t.label}</span>
                    </span>
                  ),
                }))}
                optionRender={(opt) => (
                  <span className="type-opt">
                    <DbTypeIcon type={String(opt.value)} size={16} />
                    <span>{opt.label}</span>
                  </span>
                )}
              />
            </Form.Item>
            <Form.Item
              label="数据源(别名)"
              name="instance_ids"
              rules={[{ type: 'array', required: true, min: 1, message: '请选择数据源' }]}
            >
              <Select
                mode="multiple"
                showSearch
                allowClear
                disabled={!form.env || !form.instance_type}
                loading={dsLoading}
                placeholder={dsPlaceholder}
                value={form.instance_ids}
                onChange={(v) => {
                  patchForm({ instance_ids: v || [] })
                  resetCheck()
                }}
                optionFilterProp="label"
                options={dataSourceOptions.map((s) => ({
                  value: s.key,
                  label: sourcePrimaryLabel(s),
                }))}
                optionRender={(opt) => {
                  const s = dataSourceOptions.find((x) => x.key === opt.value)
                  return (
                    <div className="ds-opt">
                      <span>{s ? sourcePrimaryLabel(s) : opt.label}</span>
                      {s && sourceSecondaryLabel(s) ? (
                        <span className="muted">{sourceSecondaryLabel(s)}</span>
                      ) : null}
                    </div>
                  )
                }}
              />
            </Form.Item>
            <Form.Item label="数据库账号" name="username" rules={[{ required: true, message: '请输入数据库账号' }]}>
              <Input
                allowClear
                disabled={!form.instance_ids.length}
                placeholder={form.instance_ids.length ? '请输入数据库账号' : '请先选择数据源(别名)'}
                value={form.username}
                onChange={(e) => {
                  patchForm({ username: e.target.value })
                  resetCheck()
                }}
              />
            </Form.Item>
            <Form.Item
              label="账号权限"
              name="permissions"
              rules={[{ type: 'array', required: true, min: 1, message: '请选择账号权限' }]}
            >
              <Select
                mode="multiple"
                showSearch
                allowClear
                placeholder="请选择操作"
                value={form.permissions}
                onChange={(v) => {
                  patchForm({ permissions: v || [] })
                  resetCheck()
                }}
                options={permissionOps}
              />
            </Form.Item>
            <Form.Item
              label="审批人"
              name="assigned_to"
              rules={[{ required: true, message: '请选择审批人' }]}
              extra={
                approvers.length ? (
                  <div className="extra">候选来自 create-db/approvers，默认预填 sync_default_approver</div>
                ) : null
              }
            >
              <Select
                showSearch
                allowClear
                loading={reviewerLoading}
                placeholder={reviewerPlaceholder}
                value={form.assigned_to || undefined}
                onChange={(v) => patchForm({ assigned_to: v || '' })}
                options={approvers.map((a) => ({ value: a, label: a }))}
              />
            </Form.Item>
            <Form.Item label="描述" name="description" rules={[{ required: true, message: '请输入描述' }]}>
              <Input
                allowClear
                placeholder="如：业务上线"
                value={form.description}
                onChange={(e) => patchForm({ description: e.target.value })}
              />
            </Form.Item>
            <Form.Item label="定时执行">
              <DatePicker
                showTime
                format="YYYY-MM-DD HH:mm"
                style={{ width: '100%' }}
                placeholder="年/月/日 --:--"
                value={scheduledValue}
                onChange={(v) => patchForm({ scheduled_at: v ? v.format('YYYY-MM-DD HH:mm:ss') : '' })}
              />
            </Form.Item>
            <p className="hint">提交后超过 8 小时未审核完成，系统将自动取消。</p>
            <Form.Item className="aside-actions">
              <Space wrap>
                <Button disabled={submitting} onClick={reset}>
                  重置
                </Button>
                <Tooltip title={submitAllowed || submitting ? '' : submitDisabledReason}>
                  <Button type="primary" loading={submitting} disabled={!submitAllowed} onClick={submit}>
                    提交
                  </Button>
                </Tooltip>
                <Button disabled={submitting} onClick={() => saveDraft()}>
                  保存草稿
                </Button>
              </Space>
            </Form.Item>
          </Form>
        </aside>

        <main className="order-main">
          <div className="sql-title">填写SQL</div>
          <Input.TextArea
            className="sql-editor"
            autoSize={{ minRows: 16 }}
            placeholder="CREATE DATABASE newdb;"
            value={form.sql_content}
            onChange={(e) => {
              patchForm({ sql_content: e.target.value })
              resetCheck()
            }}
          />
          <div className="sql-actions" style={{ margin: '12px 0', display: 'flex', gap: 8 }}>
            <Button type="primary" loading={checkLoading} disabled={checkLoading} onClick={check}>
              SQL检测
            </Button>
            <Button onClick={beautify}>SQL美化</Button>
          </div>
          <p className="hint">编辑框内可检测、美化 SQL。仅允许 CREATE DATABASE。</p>
          <ResponsiveTable
            rowKey={(_, i) => String(i)}
            size="small"
            bordered
            dataSource={checkRows}
            pagination={false}
            locale={{ emptyText: '暂无检测结果' }}
            columns={[
              { title: '阶段', dataIndex: 'stage', width: 100 },
              {
                title: '错误等级',
                width: 100,
                render: (_: any, r: any) => (
                  <span className={levelClass(r.error_level)}>{levelLabel(r.error_level)}</span>
                ),
              },
              { title: '错误信息', dataIndex: 'message', ellipsis: true },
              { title: 'SQL语句', dataIndex: 'sql', ellipsis: true },
              { title: '影响行数', dataIndex: 'affected_rows', width: 90 },
              { title: '操作', width: 70, render: () => '—' },
            ]}
          />
        </main>
      </div>
    </div>
  )
}
