import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, AutoComplete, Button, Form, Input, Select, Space, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import { buildEnvOptions, type EnvOption } from '../../utils/env'
import {
  filterAccessDomainRows,
  parseSourceKey,
  sourceKey,
  sourcePrimaryLabel,
  sourceSecondaryLabel,
  sourcesFromAccessDomains,
  type AccessDomainRow,
  type DomainSource,
} from '../../utils/instanceSource'
import { splitRedisArgs } from '../../utils/redisArgs'
import './RedisOrder.css'

type CmdItem = {
  group: string
  name: string
  crud: string
  min_args: number
}

interface CheckRow {
  stage: string
  error_level: string
  message: string
  command: string
}

const CRUD_RANK: Record<string, number> = { read: 1, create: 2, update: 3, delete: 4 }
const CRUD_CN: Record<string, string> = {
  read: '读取',
  create: '写入',
  update: '更新',
  delete: '删除',
}

function emptyForm() {
  return {
    env: '',
    instance_id: '',
    domain: '',
    db_name: '',
    assigned_to: '',
    command: '',
    description: '',
  }
}

function isProdEnv(env: string) {
  const e = String(env || '').toLowerCase()
  return e === 'prod' || e.includes('prod') || e.includes('生产')
}

function parseScriptLocal(text: string, catalog: CmdItem[], blacklist: Set<string>) {
  const byName = new Map(catalog.map((c) => [c.name.toUpperCase(), c]))
  let commentCount = 0
  let valid = 0
  let invalid = 0
  let best = 0
  let highest = ''
  const rows: CheckRow[] = []
  for (const raw of String(text || '').split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) {
      commentCount++
      continue
    }
    const { parts, error: parseErr } = splitRedisArgs(line)
    if (parseErr) {
      invalid++
      rows.push({
        stage: '校验',
        error_level: 'ERROR',
        message: `命令解析失败: ${parseErr}`,
        command: line,
      })
      continue
    }
    const verb = (parts[0] || '').toUpperCase()
    const meta = byName.get(verb)
    if (blacklist.has(verb)) {
      invalid++
      rows.push({
        stage: '校验',
        error_level: 'ERROR',
        message: `禁止执行危险命令: ${verb}`,
        command: line,
      })
      continue
    }
    if (!meta) {
      invalid++
      rows.push({
        stage: '校验',
        error_level: 'ERROR',
        message: `命令不在允许的 CRUD 白名单内: ${verb || '(空)'}`,
        command: line,
      })
      continue
    }
    if (parts.length - 1 < (meta.min_args || 0)) {
      invalid++
      rows.push({
        stage: '校验',
        error_level: 'ERROR',
        message: `${meta.name} 至少需要 ${meta.min_args} 个参数（当前 ${parts.length - 1}）`,
        command: line,
      })
      continue
    }
    valid++
    rows.push({
      stage: '校验',
      error_level: 'OK',
      message: `${meta.crud || 'ok'}`,
      command: line,
    })
    const rank = CRUD_RANK[String(meta.crud || '').toLowerCase()] || 0
    if (rank > best) {
      best = rank
      highest = String(meta.crud || '').toLowerCase()
    }
  }
  return {
    valid,
    invalid,
    commentCount,
    highestRisk: highest ? CRUD_CN[highest] || highest : '—',
    rows,
  }
}

export default function RedisOrder() {
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [antdForm] = Form.useForm()
  const [accessDomains, setAccessDomains] = useState<AccessDomainRow[]>([])
  const [instById, setInstById] = useState<Record<string, any>>({})
  const [envOptions, setEnvOptions] = useState<EnvOption[]>([])
  const [catalog, setCatalog] = useState<CmdItem[]>([])
  const [blacklist, setBlacklist] = useState<Set<string>>(() => new Set())
  const [instLoading, setInstLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [checkRows, setCheckRows] = useState<CheckRow[]>([])
  const [checked, setChecked] = useState(false)
  const [checkPassed, setCheckPassed] = useState(false)

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => {
      const next = { ...f, ...patch }
      antdForm.setFieldsValue({
        ...next,
        source_key: next.instance_id && next.domain ? sourceKey(next.instance_id, next.domain) : undefined,
      })
      return next
    })
  }

  const { restoreDraft, saveNow, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'redis',
    label: 'Redis工单',
    path: '/ops/redis',
    getValues: () => formRef.current,
    applyValues: (d) => {
      const next = { ...emptyForm(), ...d }
      setForm(next)
      antdForm.setFieldsValue({
        ...next,
        source_key: next.instance_id && next.domain ? sourceKey(next.instance_id, next.domain) : undefined,
      })
    },
  })

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  const redisOptions = useMemo((): DomainSource[] => {
    if (!form.env) return []
    return sourcesFromAccessDomains(filterAccessDomainRows(accessDomains, { env: form.env, dbType: 'redis' }))
  }, [accessDomains, form.env])

  const currentInst = useMemo(() => instById[form.instance_id], [instById, form.instance_id])
  const approvers = useMemo(() => currentInst?.approvers || [], [currentInst])
  const approverPlaceholder = !form.instance_id
    ? '请先选择 Redis 地址'
    : !approvers.length
      ? '该实例未配置审批人员，可手动填写'
      : '选填，来自实例中配置的审批人员'

  const scriptStats = useMemo(
    () => parseScriptLocal(form.command, catalog, blacklist),
    [form.command, catalog, blacklist],
  )

  const prodRisk = isProdEnv(form.env) || isProdEnv(currentInst?.idc || '')

  function onEnvChange(env: string) {
    patchForm({ env, instance_id: '', domain: '', assigned_to: '' })
    setChecked(false)
    setCheckPassed(false)
    setCheckRows([])
  }

  async function onSourceChange(key: string) {
    const parsed = parseSourceKey(key)
    if (!parsed) {
      patchForm({ instance_id: '', domain: '', assigned_to: '' })
      return
    }
    let inst = instById[parsed.instance_id]
    if (!inst) {
      try {
        const { data } = await api.get(`/instances/${parsed.instance_id}`, { silent: true })
        inst = data.data
        if (inst) setInstById((m) => ({ ...m, [parsed.instance_id]: inst }))
      } catch {
        /* keep empty approvers */
      }
    }
    const first = (inst?.approvers || [])[0] || ''
    patchForm({ instance_id: parsed.instance_id, domain: parsed.domain, assigned_to: first })
  }

  async function loadData() {
    setInstLoading(true)
    try {
      const [domRes, envRes, catRes] = await Promise.all([
        api.get('/access-domains', { params: { db_type: 'redis' } }),
        api.get('/environments').catch(() => ({ data: { data: [] } })),
        api.get('/redis/command-catalog'),
      ])
      const domains: AccessDomainRow[] = domRes.data.data || []
      setAccessDomains(domains)
      setEnvOptions(buildEnvOptions(envRes.data.data || [], domains))
      setCatalog(catRes.data.data || [])
      setBlacklist(new Set((catRes.data.blacklist || []).map((x: string) => String(x).toUpperCase())))
    } finally {
      setInstLoading(false)
    }
  }

  function runCheck() {
    const st = parseScriptLocal(formRef.current.command, catalog, blacklist)
    setCheckRows(st.rows)
    setChecked(true)
    if (st.invalid > 0) {
      setCheckPassed(false)
      message.error(`检测未通过：${st.invalid} 条不支持`)
      return false
    }
    if (st.valid < 1) {
      setCheckPassed(false)
      message.warning('至少需要一条有效 Redis 命令')
      return false
    }
    setCheckPassed(true)
    message.success(`检测通过：${st.valid} 条有效命令，最高风险 ${st.highestRisk}`)
    return true
  }

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    if (!checkPassed) {
      message.warning('请先执行「命令检测」并通过后再提交')
      return
    }
    // 提交前再检一次，防止状态与内容不一致
    if (!runCheck()) return
    const f = formRef.current
    setSubmitting(true)
    try {
      await api.post('/redis/orders', {
        env: f.env,
        instance_id: f.instance_id,
        domain: f.domain,
        db_name: f.db_name,
        command: f.command,
        assigned_to: f.assigned_to,
        description: f.description,
      })
      message.success('工单创建成功')
      clearOnSubmitSuccess()
      navigate('/ops/tickets')
    } finally {
      setSubmitting(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      await loadData()
      if (!cancelled) restoreDraft()
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    setChecked(false)
    setCheckPassed(false)
  }, [form.command])

  const checkColumns: ColumnsType<CheckRow> = [
    { title: '阶段', dataIndex: 'stage', width: 80 },
    {
      title: '错误等级',
      dataIndex: 'error_level',
      width: 100,
      render: (v: string) => (
        <span className={v === 'ERROR' ? 'lv-error' : v === 'WARN' ? 'lv-warn' : 'lv-ok'}>{v}</span>
      ),
    },
    { title: '错误信息', dataIndex: 'message', ellipsis: true },
    { title: '命令', dataIndex: 'command', ellipsis: true },
  ]

  return (
    <div className="order-page redis-order">
      <div className="order-split">
        <aside className="order-aside">
          <Form
            form={antdForm}
            layout="vertical"
            initialValues={form}
            onSubmitCapture={(e) => e.preventDefault()}
            onValuesChange={(_, all) => setForm((f) => ({ ...f, ...all }))}
          >
            <Form.Item name="env" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
              <Select
                showSearch
                allowClear
                placeholder="请选择"
                optionFilterProp="label"
                onChange={onEnvChange}
                options={envOptions.map((e) => ({ value: e.value, label: e.label }))}
              />
            </Form.Item>

            <Form.Item
              name="source_key"
              label="Redis 地址"
              rules={[{ required: true, message: '请选择 Redis 地址' }]}
            >
              <Select
                showSearch
                allowClear
                disabled={!form.env}
                loading={instLoading}
                placeholder={form.env ? '请选择' : '请先选择环境'}
                optionFilterProp="label"
                value={form.instance_id && form.domain ? sourceKey(form.instance_id, form.domain) : undefined}
                onChange={(v) => void onSourceChange(v || '')}
                options={redisOptions.map((s) => ({
                  value: s.key,
                  label: `${sourcePrimaryLabel(s)} ${sourceSecondaryLabel(s)}`.trim(),
                  primary: sourcePrimaryLabel(s),
                  secondary: sourceSecondaryLabel(s),
                }))}
                optionRender={(opt) => (
                  <div className="ds-opt">
                    <span>{(opt.data as any).primary}</span>
                    <span className="muted">{(opt.data as any).secondary}</span>
                  </div>
                )}
              />
            </Form.Item>

            {form.env && !instLoading && !redisOptions.length ? (
              <Alert
                type="warning"
                showIcon
                className="hint-alert"
                message="当前环境暂无 Redis 域名，请先在「域名管理」同步并划拨"
              />
            ) : null}

            {prodRisk ? (
              <Alert
                type="error"
                showIcon
                className="hint-alert"
                message="生产环境，命令执行不可逆，请谨慎操作"
              />
            ) : null}

            <Form.Item name="db_name" label="操作库 (DB)" rules={[{ required: true, message: '请选择操作库' }]}>
              <Select
                placeholder="请选择操作库"
                options={Array.from({ length: 16 }, (_, i) => ({
                  value: `db${i}`,
                  label: `DB ${i}`,
                }))}
              />
            </Form.Item>

            <Form.Item label="审核人" name="assigned_to">
              <AutoComplete
                allowClear
                placeholder={approverPlaceholder}
                options={approvers.map((a: string) => ({ value: a, label: a }))}
                filterOption={(input, option) =>
                  String(option?.value || '')
                    .toLowerCase()
                    .includes(String(input || '').toLowerCase())
                }
              />
            </Form.Item>

            <Form.Item
              name="description"
              label="描述"
              rules={[{ required: true, message: '请输入描述' }]}
            >
              <Input allowClear placeholder="如：清理过期缓存 Key" />
            </Form.Item>

            <Form.Item className="aside-actions">
              <Space wrap>
                <Button
                  type="primary"
                  loading={submitting}
                  disabled={!checkPassed || submitting}
                  onClick={submit}
                >
                  提交
                </Button>
                <Button onClick={() => saveNow()}>保存草稿</Button>
              </Space>
            </Form.Item>
          </Form>
        </aside>

        <main className="order-main">
          <div className="redis-main-title">填写命令</div>
          <Alert
            type="warning"
            showIcon
            className="hint-alert"
            message={`当前共 ${scriptStats.valid} 条命令，最高风险: ${scriptStats.highestRisk}。每行一条命令，支持 # 注释；含空格 / JSON / 反斜杠请用引号（与 redis-cli 一致）。`}
          />
          <Input.TextArea
            className="sql-editor"
            autoSize={{ minRows: 14, maxRows: 28 }}
            placeholder={
              '# 每行一条；JSON/空格/反斜杠请加引号\n' +
              'SET user:1 "{\\"name\\":\\"alice\\"}"\n' +
              "HSET u meta '{\"role\":\"admin\"}'\n" +
              'SET path "c:\\\\data\\\\a.json"'
            }
            value={form.command}
            onChange={(e) => patchForm({ command: e.target.value })}
          />
          <div className="sql-actions">
            <Button type="primary" onClick={runCheck}>
              命令检测
            </Button>
            <span className="redis-stat">
              有效 {scriptStats.valid} · 注释/空行 {scriptStats.commentCount} · 不支持{' '}
              <span className={scriptStats.invalid ? 'lv-error' : ''}>{scriptStats.invalid}</span>
            </span>
          </div>
          <p className="hint">
            {checkPassed
              ? '命令检测已通过，可以提交'
              : checked
                ? '检测未通过，请修改命令后重新检测'
                : '请先点击「命令检测」并通过后，再提交'}
          </p>
          <ResponsiveTable
            dataSource={checkRows}
            columns={checkColumns}
            size="small"
            bordered
            rowKey={(_, i) => String(i)}
            pagination={false}
            locale={{ emptyText: '暂无检测结果' }}
          />
        </main>
      </div>
    </div>
  )
}
