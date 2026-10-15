import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  AutoComplete, Button, Card, Col, Drawer, Form, Input, InputNumber, Progress, Radio, Row, Select, Space, Statistic, Switch, Tag,
  Tooltip, Typography, message,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import ResponsiveTable from '../../components/ResponsiveTable'
import { AlertExprTemplate, AlertNotifyPolicy, AlertRule, AlertScope, AlertTier, SEV_COLOR } from './types'

type InstOpt = { id: string; name: string; db_type?: string; host?: string; domain_name?: string }
type ClusterOpt = { id: string; idc: string; domain: string; env_name?: string }

const SCOPE_LABEL: Record<string, string> = {
  global: '全局',
  instances: '勾选实例',
  cluster: '按集群',
  tags: '按标签',
}

const defaultExpr = `mysql_global_status_threads_connected{__SCOPE__} / clamp_min(mysql_global_variables_max_connections{__SCOPE__}, 1) >= 0.8`
const defaultMetric = `max by (d_b_instance_name) (qce_postgres_storagerate_max{__SCOPE__}) / 100`
const defaultNotify: AlertNotifyPolicy = {
  max_fire_per_window: 3,
  window: '15m',
  repeat_interval: '5m',
  repeat_on_handles: ['unclaimed'],
  inherit_global: true,
}

function buildScope(v: Record<string, unknown>): AlertScope {
  const mode = String(v.scope_mode || 'global')
  const matcher_key = String(v.scope_matcher_key || 'auto').trim() || 'auto'
  if (mode === 'instances') {
    return { mode, matcher_key, instance_ids: (v.scope_instance_ids as string[]) || [] }
  }
  if (mode === 'cluster') {
    return {
      mode,
      matcher_key,
      cluster: {
        idc: String(v.scope_idc || '').trim(),
        domain: String(v.scope_domain || '').trim(),
      },
    }
  }
  if (mode === 'tags') {
    const raw = v.scope_tags_text
    const tags = Array.isArray(raw)
      ? (raw as string[]).map((s) => String(s).trim()).filter(Boolean)
      : String(raw || '').split(/[,;\n]/).map((s) => s.trim()).filter(Boolean)
    return { mode, matcher_key, tags }
  }
  return { mode: 'global', matcher_key }
}

export default function AlertRules() {
  const { hasUrn } = usePermission()
  const [rows, setRows] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [saving, setSaving] = useState(false)
  const [testResult, setTestResult] = useState('')
  const [compileHint, setCompileHint] = useState('')
  const [instances, setInstances] = useState<InstOpt[]>([])
  const [clusters, setClusters] = useState<ClusterOpt[]>([])
  const [tagKeys, setTagKeys] = useState<string[]>([])
  const [templates, setTemplates] = useState<AlertExprTemplate[]>([])
  const [form] = Form.useForm()
  const scopeMode = Form.useWatch('scope_mode', form) || 'global'
  const useTiers = Form.useWatch('use_tiers', form)
  const labelsText = Form.useWatch('labels_text', form) || ''
  const selectedInstIDs = Form.useWatch('scope_instance_ids', form) as string[] | undefined
  const selectedInstCount = Array.isArray(selectedInstIDs) ? selectedInstIDs.length : 0
  const serviceHint = useMemo(() => {
    const m = String(labelsText).match(/(?:^|,)\s*service\s*=\s*([^,]+)/i)
    return (m?.[1] || '').trim().toLowerCase()
  }, [labelsText])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/alert-rules')
      setRows(res.data?.data || [])
    } finally {
      setLoading(false)
    }
  }, [])

  const loadMeta = useCallback(async () => {
    try {
      const [instRes, clusterRes, tagRes, tplRes] = await Promise.all([
        api.get('/instances').catch(() => ({ data: { data: [] } })),
        api.get('/db-monitor/clusters', { params: { engine: 'starrocks' } }).catch(() => ({ data: { data: [] } })),
        api.get('/monitor-tags').catch(() => ({ data: { data: [] } })),
        api.get('/alert-rules/templates').catch(() => ({ data: { data: [] } })),
      ])
      setInstances(instRes.data?.data || [])
      setClusters(clusterRes.data?.data || [])
      setTemplates(tplRes.data?.data || [])
      const tags = (tagRes.data?.data || []) as { key?: string; values?: string[] }[]
      const opts: string[] = []
      for (const t of tags) {
        const k = (t.key || '').trim()
        if (!k) continue
        if (t.values?.length) {
          for (const v of t.values) opts.push(`${k}=${v}`)
        } else {
          opts.push(`${k}=`)
        }
      }
      setTagKeys(opts)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => { void load() }, [load])
  useEffect(() => { void loadMeta() }, [loadMeta])

  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({
      severity: 'P2', for_duration: '5m', enabled: true, scope_mode: 'global',
      scope_matcher_key: 'auto', use_tiers: false,
      expr: defaultExpr, labels_text: 'service=mysql, team=DBA',
      notify_inherit: true,
      notify_max_fire: defaultNotify.max_fire_per_window,
      notify_window: defaultNotify.window,
      notify_repeat: defaultNotify.repeat_interval,
      notify_handles: defaultNotify.repeat_on_handles,
      tiers: [
        { op: '>=', value: 0.8, severity: 'P1', for: '5m' },
        { op: '>=', value: 0.9, severity: 'P0', for: '3m' },
      ],
    })
    setTestResult('')
    setCompileHint('')
    setOpen(true)
  }

  const applyTemplate = (id: string) => {
    const t = templates.find((x) => x.id === id)
    if (!t) return
    const labels: string[] = ['team=DBA']
    if (t.service) labels.unshift(`service=${t.service}`)
    const multi = Array.isArray(t.tiers) && t.tiers.length > 0
    form.setFieldsValue({
      name: t.name,
      use_tiers: multi,
      expr: multi ? (t.metric_expr || t.expr) : t.expr,
      metric_expr: t.metric_expr || (multi ? t.expr : ''),
      tiers: multi ? t.tiers : form.getFieldValue('tiers'),
      severity: t.severity || 'P2',
      for_duration: t.for_duration || '5m',
      labels_text: labels.join(', '),
      summary: t.summary || '',
      scope_matcher_key: t.matcher_key || 'auto',
    })
    message.success('已填入模板，可继续修改')
  }

  const openEdit = (r: AlertRule) => {
    setEditing(r)
    const scope = r.scope || { mode: 'global' }
    const np = r.notify_policy || defaultNotify
    const multi = Array.isArray(r.tiers) && r.tiers.length > 0
    form.setFieldsValue({
      ...r,
      use_tiers: multi,
      expr: multi ? (r.metric_expr || r.expr) : r.expr,
      metric_expr: r.metric_expr || '',
      tiers: multi ? r.tiers : [
        { op: '>=', value: 0.8, severity: 'P1', for: '5m' },
        { op: '>=', value: 0.9, severity: 'P0', for: '3m' },
      ],
      labels_text: Object.entries(r.labels || {}).map(([k, v]) => `${k}=${v}`).join(', '),
      summary: r.annotations?.summary || '',
      threshold: r.annotations?.threshold || '',
      scope_mode: scope.mode || 'global',
      scope_matcher_key: scope.matcher_key || 'auto',
      scope_instance_ids: scope.instance_ids || [],
      scope_idc: scope.cluster?.idc || '',
      scope_domain: scope.cluster?.domain || '',
      scope_tags_text: scope.tags || [],
      notify_inherit: np.inherit_global === true || (!np.window && !np.repeat_interval && !np.max_fire_per_window),
      notify_max_fire: np.max_fire_per_window ?? 3,
      notify_window: np.window || '15m',
      notify_repeat: np.repeat_interval || '5m',
      notify_handles: np.repeat_on_handles?.length ? np.repeat_on_handles : ['unclaimed'],
    })
    setTestResult('')
    setCompileHint(scope.matcher
      ? `matcher: ${scope.matcher}${r.compiled_at ? ` · 编译于 ${r.compiled_at}` : ''}`
      : (r.compile_error || ''))
    setOpen(true)
  }

  const parseLabels = (text?: string) => {
    const out: Record<string, string> = {}
    for (const part of (text || '').split(',')) {
      const i = part.indexOf('=')
      if (i <= 0) continue
      const k = part.slice(0, i).trim()
      const v = part.slice(i + 1).trim()
      if (k) out[k] = v
    }
    return out
  }

  const buildNotifyPolicy = (v: Record<string, unknown>): AlertNotifyPolicy => {
    const inherit = !!v.notify_inherit
    if (inherit) {
      return {
        inherit_global: true,
        max_fire_per_window: 0,
        window: '',
        repeat_interval: '',
        repeat_on_handles: Array.isArray(v.notify_handles) ? v.notify_handles as string[] : ['unclaimed'],
      }
    }
    return {
      inherit_global: false,
      max_fire_per_window: Number(v.notify_max_fire ?? 3),
      window: String(v.notify_window || '15m'),
      repeat_interval: String(v.notify_repeat || ''),
      repeat_on_handles: Array.isArray(v.notify_handles) ? v.notify_handles as string[] : ['unclaimed'],
    }
  }

  const submit = async () => {
    const v = await form.validateFields()
    const scope = buildScope(v)
    const multi = !!v.use_tiers
    const expr = String(v.expr || '')
    if (scope.mode !== 'global' && !expr.includes('__SCOPE__')) {
      message.error('非全局作用域时，PromQL 须包含 __SCOPE__')
      return
    }
    let tiers: AlertTier[] = []
    let severity = String(v.severity || 'P2')
    if (multi) {
      tiers = ((v.tiers as AlertTier[]) || []).filter((t) => t && t.severity != null && t.value != null)
      if (tiers.length === 0) {
        message.error('请至少配置一档阈值')
        return
      }
      const rank: Record<string, number> = { P0: 4, P1: 3, P2: 2, P3: 1 }
      severity = [...tiers].sort((a, b) => (rank[b.severity] || 0) - (rank[a.severity] || 0))[0]?.severity || severity
    }
    setSaving(true)
    try {
      const body = {
        name: v.name,
        expr,
        metric_expr: multi ? expr : '',
        severity,
        for_duration: v.for_duration,
        cluster: v.cluster || '',
        enabled: !!v.enabled,
        labels: parseLabels(v.labels_text),
        annotations: { summary: v.summary || '', threshold: v.threshold || '' },
        scope,
        tiers: multi ? tiers : [],
        notify_policy: buildNotifyPolicy(v),
      }
      if (editing) await api.put(`/alert-rules/${editing.id}`, body)
      else await api.post('/alert-rules', body)
      message.success('已保存')
      setOpen(false)
      await load()
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '保存失败'
      message.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const previewCompile = async () => {
    const v = form.getFieldsValue()
    try {
      const res = await api.post('/alert-rules/compile-preview', {
        expr: v.expr, metric_expr: v.use_tiers ? v.expr : undefined, scope: buildScope(v),
      })
      const d = res.data?.data || {}
      if (d.compile_error) {
        setCompileHint(d.compile_error)
        message.warning(d.compile_error)
        return
      }
      setCompileHint(`matcher: ${d.matcher || '(空/全局)'}`)
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '编译预览失败'
      setCompileHint(msg)
    }
  }

  const testRule = async () => {
    const v = form.getFieldsValue()
    if (!v.expr) {
      message.warning('请先填写表达式')
      return
    }
    try {
      const res = await api.post(editing ? `/alert-rules/${editing.id}/test` : '/alert-rules/test', {
        expr: v.expr,
        metric_expr: v.use_tiers ? v.expr : undefined,
        tiers: v.use_tiers ? v.tiers : undefined,
        scope: buildScope(v),
      })
      const d = res.data?.data || {}
      const n = d.count ?? 0
      const instN = (d.instances || []).length
      const tierHits = d.tier_hits
      setTestResult(
        tierHits != null
          ? `序列 ${n} 条，档位命中 ${tierHits}（约 ${instN} 个实例）`
          : `命中 ${n} 条序列（约 ${instN} 个 instance）`,
      )
      if (d.matcher) setCompileHint(`matcher: ${d.matcher}`)
      message.success(`试跑完成：命中 ${n} 条`)
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { message?: string } } })?.response?.data?.message || '试跑失败'
      setTestResult(msg)
    }
  }

  const instOptions = useMemo(() => {
    let list = instances
    if (serviceHint === 'host') {
      list = instances.filter((it) => {
        const t = (it.db_type || '').toLowerCase()
        return t === 'host' || t === 'os' || t === '主机'
      })
    } else if (serviceHint === 'postgres' || serviceHint === 'mysql') {
      list = instances.filter((it) => (it.db_type || '').toLowerCase().includes(serviceHint === 'postgres' ? 'postgre' : 'mysql')
        || (it.db_type || '').toLowerCase() === 'pg')
    }
    return list.map((it) => ({
      value: it.id,
      label: `${it.name || it.id}${it.domain_name ? ` · ${it.domain_name}` : it.host ? ` · ${it.host}` : ''} (${it.db_type || '?'})`,
    }))
  }, [instances, serviceHint])

  const columns = [
    {
      title: '规则名', key: 'name', ellipsis: true,
      render: (_: unknown, r: AlertRule) => {
        const expr = r.compiled_expr || r.expr || ''
        return (
          <Tooltip
            placement="topLeft"
            overlayStyle={{ maxWidth: 560 }}
            title={
              <div style={{ fontFamily: 'monospace', fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                {expr}
                {r.scope?.matcher ? `\n\nmatcher: ${r.scope.matcher}` : ''}
              </div>
            }
          >
            <span style={{ fontWeight: 600, cursor: 'default' }}>{r.name}</span>
          </Tooltip>
        )
      },
    },
    {
      title: '作用域', width: 88,
      render: (_: unknown, r: AlertRule) => {
        const mode = r.scope?.mode || 'global'
        return <Tag color={mode === 'global' ? 'default' : 'blue'}>{SCOPE_LABEL[mode] || mode}</Tag>
      },
    },
    {
      title: '级别', dataIndex: 'severity', width: 88,
      render: (v: string, r: AlertRule) => (
        <Space size={4} wrap>
          <Tag color={SEV_COLOR[v]}>{v}</Tag>
          {r.tiers && r.tiers.length > 0 ? <Tag>多档{r.tiers.length}</Tag> : null}
        </Space>
      ),
    },
    { title: '持续', dataIndex: 'for_duration', width: 64 },
    {
      title: '健康', width: 100,
      render: (_: unknown, r: AlertRule) => (
        <Space size={4}>
          <Progress percent={r.health} size="small" style={{ width: 56, margin: 0 }} showInfo={false}
            strokeColor={r.health >= 85 ? '#059669' : r.health >= 70 ? '#f59e0b' : '#dc2626'} />
          <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{r.health}</span>
        </Space>
      ),
    },
    { title: '7天', dataIndex: 'triggered_7d', width: 56, align: 'right' as const },
    {
      title: '状态', width: 80,
      render: (_: unknown, r: AlertRule) => r.enabled ? <Tag color="success">生效</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '操作', width: 110, fixed: 'right' as const,
      render: (_: unknown, r: AlertRule) => (
        <Space size={0}>
          {hasUrn(Perm.alert.ruleEdit) && <Button type="link" size="small" onClick={() => openEdit(r)}>编辑</Button>}
          {hasUrn(Perm.alert.ruleDelete) && (
            <Button type="link" size="small" danger onClick={async () => {
              await api.delete(`/alert-rules/${r.id}`)
              message.success('已删除')
              await load()
            }}>删除</Button>
          )}
        </Space>
      ),
    },
  ]

  const active = rows.filter((r) => r.enabled).length
  const healthy = rows.filter((r) => r.health >= 85).length

  return (
    <div style={{ padding: 16 }}>
      <Typography.Title level={4} style={{ marginTop: 0 }}>规则管理</Typography.Title>
      <Row gutter={12} style={{ marginBottom: 16 }}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="告警规则" value={rows.length} suffix="条" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="生效中" value={active} suffix="条" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="健康规则" value={healthy} suffix="条" /></Card></Col>
      </Row>
      <Card
        size="small"
        extra={hasUrn(Perm.alert.ruleEdit) ? <Button type="primary" onClick={openCreate}>新建规则</Button> : null}
      >
        <Typography.Paragraph type="secondary" style={{ marginBottom: 12, fontSize: 12 }}>
          悬停规则名可查看完整 PromQL；生产请用实例/集群/标签作用域，避免全局扫描。
        </Typography.Paragraph>
        <ResponsiveTable
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={rows}
          columns={columns}
          pagination={{ pageSize: 20, size: 'small' }}
          scroll={{ x: 720 }}
        />
      </Card>

      <Drawer
        width={640}
        open={open}
        onClose={() => setOpen(false)}
        title={editing ? `编辑规则：${editing.name}` : '新建告警规则'}
        extra={
          <Space>
            {hasUrn(Perm.alert.ruleEdit) && <Button onClick={() => void previewCompile()}>编译预览</Button>}
            {hasUrn(Perm.alert.ruleEdit) && <Button onClick={() => void testRule()}>试跑</Button>}
            <Button type="primary" loading={saving} onClick={() => void submit()}>保存</Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          {!editing && (
            <Form.Item label="选用模板（可选，填入后可改）">
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder="预置样例，非白名单；也可直接手写 PromQL"
                options={templates.map((t) => ({
                  value: t.id,
                  label: `${t.name}${t.service ? ` · ${t.service}` : ''}`,
                }))}
                onChange={(id: string) => { if (id) applyTemplate(id) }}
              />
            </Form.Item>
          )}
          <Form.Item name="name" label="规则名" rules={[{ required: true, message: '请输入规则名' }]}>
            <Input maxLength={200} />
          </Form.Item>
          <Form.Item name="use_tiers" label="多档阈值" valuePropName="checked"
            extra="开启后 PromQL 只写指标（无比较符），阈值与级别在下方分档配置；评估时取最高命中档">
            <Switch />
          </Form.Item>

          <Form.Item
            name="expr"
            label={useTiers ? '指标表达式（无比较符）' : 'PromQL 模板'}
            rules={[{ required: true, message: '请输入表达式' }]}
            extra={useTiers
              ? '示例：max by (d_b_instance_name) (qce_postgres_storagerate_max{__SCOPE__}) / 100'
              : '任意 PromQL；非全局作用域须在 selector 内使用 __SCOPE__。主机磁盘用 node_filesystem；腾讯云 PG 实例存储建议开多档'}
          >
            <Input.TextArea rows={4} style={{ fontFamily: 'monospace' }} placeholder={useTiers ? defaultMetric : defaultExpr} />
          </Form.Item>

          {useTiers && (
            <Form.List name="tiers">
              {(fields, { add, remove }) => (
                <Form.Item label="阈值档位" required>
                  {fields.map((field) => (
                    <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }} wrap>
                      <Form.Item {...field} name={[field.name, 'op']} rules={[{ required: true }]} style={{ marginBottom: 0 }}>
                        <Select style={{ width: 72 }} options={['>=', '>', '<=', '<'].map((v) => ({ value: v, label: v }))} />
                      </Form.Item>
                      <Form.Item {...field} name={[field.name, 'value']} rules={[{ required: true, message: '阈值' }]} style={{ marginBottom: 0 }}>
                        <InputNumber step={0.05} style={{ width: 100 }} />
                      </Form.Item>
                      <Form.Item {...field} name={[field.name, 'severity']} rules={[{ required: true }]} style={{ marginBottom: 0 }}>
                        <Select style={{ width: 80 }} options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))} />
                      </Form.Item>
                      <Form.Item {...field} name={[field.name, 'for']} style={{ marginBottom: 0 }}>
                        <Input placeholder="for" style={{ width: 72 }} />
                      </Form.Item>
                      <MinusCircleOutlined onClick={() => remove(field.name)} />
                    </Space>
                  ))}
                  <Button type="dashed" onClick={() => add({ op: '>=', value: 0.8, severity: 'P2', for: '5m' })} block icon={<PlusOutlined />}>
                    添加档位
                  </Button>
                </Form.Item>
              )}
            </Form.List>
          )}

          <Form.Item name="scope_mode" label="作用域" rules={[{ required: true }]}>
            <Radio.Group
              options={[
                { value: 'global', label: '全局（慎用）' },
                { value: 'instances', label: '勾选实例' },
                { value: 'cluster', label: '按集群' },
                { value: 'tags', label: '按标签' },
              ]}
            />
          </Form.Item>

          <Form.Item
            name="scope_matcher_key"
            label="匹配标签 matcher_key"
            extra="auto：全 postgres→d_b_instance_name，否则 instance；可输入自定义 label 名"
            initialValue="auto"
          >
            <AutoComplete
              options={[
                { value: 'auto', label: '自动' },
                { value: 'instance', label: 'instance（主机/exporter）' },
                { value: 'd_b_instance_name', label: 'd_b_instance_name（腾讯云 PG）' },
                { value: 'domain', label: 'domain' },
              ]}
              placeholder="auto"
            />
          </Form.Item>

          {scopeMode === 'instances' && (
            <Form.Item
              name="scope_instance_ids"
              label="实例"
              rules={[
                { required: true, message: '请勾选实例' },
                {
                  validator: async (_, v) => {
                    const n = Array.isArray(v) ? v.length : 0
                    if (n > 1000) {
                      throw new Error('最多勾选 1000 个实例。更大规模请用「按标签/按集群」；同 IDC 时后端会折叠为短 matcher')
                    }
                  },
                },
              ]}
              extra={
                selectedInstCount >= 50
                  ? `已选 ${selectedInstCount} 个：将尝试按统一 idc/env 折叠；若失败请改用「按标签/按集群」。单规则评估仍 1 次查询，但 firing 过多会影响告警列表。`
                  : serviceHint === 'host'
                    ? '当前 service=host：请选主机实例（node_exporter）'
                    : serviceHint === 'postgres'
                      ? '当前 service=postgres：实例存储请选库实例；勿用主机磁盘规则绑 PG'
                      : '按 labels.service 过滤候选；大批量（≥50）建议同 IDC 或改用按标签'
              }
            >
              <Select
                mode="multiple"
                allowClear
                showSearch
                optionFilterProp="label"
                maxTagCount="responsive"
                options={instOptions}
                placeholder="选择要绑定的实例"
              />
            </Form.Item>
          )}

          {scopeMode === 'cluster' && (
            <>
              <Form.Item label="快速选择（监控注册集群）">
                <Select
                  allowClear
                  showSearch
                  optionFilterProp="label"
                  placeholder="可选：从 StarRocks 监控集群填入"
                  options={clusters.map((c) => ({
                    value: `${c.idc}|${c.domain}`,
                    label: `${c.domain} · ${c.env_name || c.idc}`,
                  }))}
                  onChange={(val: string) => {
                    if (!val) return
                    const [idc, domain] = val.split('|')
                    form.setFieldsValue({ scope_idc: idc, scope_domain: domain })
                  }}
                />
              </Form.Item>
              <Row gutter={12}>
                <Col span={12}>
                  <Form.Item name="scope_idc" label="idc（环境 code）" rules={[{ required: true, message: '必填' }]}>
                    <Input placeholder="如 prod" />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item name="scope_domain" label="domain" rules={[{ required: true, message: '必填' }]}>
                    <Input placeholder="监控 domain 标签" />
                  </Form.Item>
                </Col>
              </Row>
            </>
          )}

          {scopeMode === 'tags' && (
            <Form.Item
              name="scope_tags_text"
              label="标签"
              rules={[{ required: true, message: '请填写标签' }]}
              extra="格式 key=value，多个用逗号分隔；优先匹配实例 tags，也可直接作为 Prom label（idc/domain/env）"
            >
              <Select mode="tags" tokenSeparators={[',']} placeholder="env=prod, app=mysql" options={tagKeys.map((v) => ({ value: v, label: v }))} />
            </Form.Item>
          )}

          {compileHint && (
            <Typography.Paragraph type="secondary" style={{ fontFamily: 'monospace', fontSize: 12 }}>
              {compileHint}
            </Typography.Paragraph>
          )}

          {!useTiers && (
            <Row gutter={12}>
              <Col span={12}>
                <Form.Item name="severity" label="级别" rules={[{ required: true }]}>
                  <Select options={['P0', 'P1', 'P2', 'P3'].map((v) => ({ value: v, label: v }))} />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item name="for_duration" label="持续时间" rules={[{ required: true }]}>
                  <Input placeholder="5m" />
                </Form.Item>
              </Col>
            </Row>
          )}
          {useTiers && (
            <Form.Item name="for_duration" label="默认持续时间（档位未填 for 时）" rules={[{ required: true }]}>
              <Input placeholder="5m" />
            </Form.Item>
          )}
          <Form.Item name="cluster" label="告警标签 cluster">
            <Input placeholder="可选，写入 labels.cluster（与作用域集群不同）" />
          </Form.Item>
          <Form.Item name="labels_text" label="标签" extra="格式 service=mysql, team=DBA">
            <Input />
          </Form.Item>
          <Form.Item name="summary" label="summary">
            <Input />
          </Form.Item>
          {!useTiers && (
            <Form.Item name="threshold" label="阈值说明">
              <Input placeholder="如 > 90%" />
            </Form.Item>
          )}

          <Typography.Text strong style={{ display: 'block', marginBottom: 8 }}>通知频控</Typography.Text>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
            默认跟随「认证配置 → 通知 → 告警发送频率」（如 15m 内最多 3 次）。满额后停发触发/续报；告警恢复仍会通知一条。
          </Typography.Paragraph>
          <Form.Item name="notify_inherit" label="使用全局默认" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, c) => p.notify_inherit !== c.notify_inherit}>
            {() =>
              form.getFieldValue('notify_inherit') ? null : (
                <Row gutter={12}>
                  <Col span={8}>
                    <Form.Item name="notify_max_fire" label="窗口内最多通知" extra="0=不限制">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={8}>
                    <Form.Item name="notify_window" label="计数窗口">
                      <Input placeholder="15m" />
                    </Form.Item>
                  </Col>
                  <Col span={8}>
                    <Form.Item name="notify_repeat" label="未处理续报间隔" extra="空=关闭">
                      <Input placeholder="5m" />
                    </Form.Item>
                  </Col>
                </Row>
              )
            }
          </Form.Item>
          <Form.Item name="notify_handles" label="续报处理态" extra="默认仅待认领时续报">
            <Select
              mode="multiple"
              options={[
                { value: 'unclaimed', label: '待认领' },
                { value: 'acknowledged', label: '已认领' },
                { value: 'processing', label: '处理中' },
              ]}
            />
          </Form.Item>

          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          {testResult && <Typography.Text type="secondary">{testResult}</Typography.Text>}
        </Form>
      </Drawer>
    </div>
  )
}
