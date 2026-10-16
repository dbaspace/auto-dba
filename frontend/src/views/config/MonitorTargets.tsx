import React, { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Form,
  Input,
  Modal,
  Popover,
  Row,
  Col,
  Select,
  Space,
  Tag,
  message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import { PlusOutlined, SettingOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import './MonitorTargets.css'

interface TargetGroup {
  id?: string
  endpoints?: string
  labels?: Record<string, string>
  description?: string
}

interface MonitorJob {
  id?: string
  name?: string
  template?: string
  collector?: string
  scheme?: string
  status?: string
  monitor_type?: string
  metrics_path?: string
  scrape_interval?: string
  scrape_timeout?: string
  labels?: Record<string, string>
  description?: string
  target_groups?: TargetGroup[]
  updated_at?: string
}

type LabelPair = { key: string; value: string }

const MONITOR_TYPE_OPTIONS = [
  { value: 'direct', label: '直连类型监控模板' },
  { value: 'proxy', label: 'Proxy 类型监控模板' },
  { value: 'k8s', label: 'K8s 类型监控模板' },
]

const LOCAL_COLLECTOR = { value: '本地登记', label: '本地登记（不接 Prometheus）' }

const INTERVAL_OPTIONS = [
  { value: '10s', label: '10 秒' },
  { value: '20s', label: '20 秒' },
  { value: '30s', label: '30 秒' },
  { value: '1m', label: '1 分钟' },
  { value: '2m', label: '2 分钟' },
  { value: '3m', label: '3 分钟' },
  { value: '5m', label: '5 分钟' },
]

const TIMEOUT_OPTIONS = [
  { value: '10s', label: '10 秒' },
  { value: '20s', label: '20 秒' },
  { value: '30s', label: '30 秒' },
  { value: '1m', label: '1 分钟' },
]

const PRESET_FALLBACK_TAG_KEYS = [
  { key: 'sc_tmp', values: ['direct', 'proxy', 'k8s'] },
  { key: 'domain', values: [] as string[] },
  { key: 'idc', values: [] as string[] },
  { key: 'app', values: ['fe-StarRocks', 'be-StarRocks', 'master-server', 'redis'] },
  { key: 'env', values: [] as string[] },
]

function labelsToPairs(labels?: Record<string, string>): LabelPair[] {
  if (!labels) return []
  return Object.entries(labels).map(([key, value]) => ({ key, value }))
}

function pairsToLabels(pairs: LabelPair[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of pairs) {
    const k = String(p.key || '').trim()
    if (!k) continue
    out[k] = String(p.value ?? '').trim()
  }
  return out
}

function labelsToText(labels?: Record<string, string>) {
  return labelsToPairs(labels)
    .map((p) => `${p.key}=${p.value}`)
    .join(', ')
}

function TagManager({
  value,
  onChange,
  presetKeys,
}: {
  value: LabelPair[]
  onChange: (next: LabelPair[]) => void
  presetKeys: { key: string; values: string[] }[]
}) {
  const [existOpen, setExistOpen] = useState(false)
  const [newOpen, setNewOpen] = useState(false)
  const [existKey, setExistKey] = useState<string>()
  const [existValue, setExistValue] = useState<string>()
  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')

  const existPairs = value.filter((p) => presetKeys.some((k) => k.key === p.key))
  const rawPairs = value.filter((p) => !presetKeys.some((k) => k.key === p.key))

  const valueOptions = useMemo(() => {
    const hit = presetKeys.find((k) => k.key === existKey)
    return (hit?.values || []).map((v) => ({ value: v, label: v }))
  }, [existKey, presetKeys])

  function appendExist() {
    if (!existKey || !existValue) {
      message.warning('请选择标签名和标签值')
      return
    }
    onChange([...value.filter((p) => p.key !== existKey), { key: existKey, value: existValue }])
    setExistKey(undefined)
    setExistValue(undefined)
    setExistOpen(false)
  }

  function appendNew() {
    const k = newKey.trim()
    const v = newValue.trim()
    if (!k || !v) {
      message.warning('请填写标签名和标签值')
      return
    }
    onChange([...value.filter((p) => p.key !== k), { key: k, value: v }])
    setNewKey('')
    setNewValue('')
    setNewOpen(false)
  }

  function removePair(key: string, e?: React.MouseEvent | React.KeyboardEvent) {
    e?.preventDefault?.()
    e?.stopPropagation?.()
    onChange(value.filter((p) => p.key !== key))
  }

  return (
    <div className="mt-tag-picker">
      <div className="mt-tag-row">
        <span className="mt-tag-title">已有标签（{existPairs.length}）</span>
        {existPairs.map((p) => (
          <Tag key={`exist-${p.key}`} closable color="blue" onClose={(e) => removePair(p.key, e)}>
            {p.key}={p.value}
          </Tag>
        ))}
        <Popover
          open={existOpen}
          onOpenChange={setExistOpen}
          trigger="click"
          placement="bottomLeft"
          content={
            <div className="mt-tag-pop" onClick={(e) => e.stopPropagation()}>
              <Form layout="vertical" size="small">
                <Form.Item label="选择标签名">
                  <Select
                    showSearch
                    style={{ width: 200 }}
                    value={existKey}
                    onChange={(v) => {
                      setExistKey(v)
                      setExistValue(undefined)
                    }}
                    options={presetKeys.map((k) => ({ value: k.key, label: k.key }))}
                  />
                </Form.Item>
                <Form.Item label="选择标签值：">
                  <Select
                    showSearch
                    allowClear
                    style={{ width: '100%' }}
                    value={existValue}
                    onChange={setExistValue}
                    options={valueOptions}
                    placeholder="请选择或输入"
                    notFoundContent={null}
                    dropdownRender={(menu) => (
                      <>
                        {menu}
                        <div style={{ padding: 8, borderTop: '1px solid #f0f0f0' }}>
                          <Input
                            size="small"
                            placeholder="自定义标签值"
                            value={existValue}
                            onChange={(e) => setExistValue(e.target.value)}
                            onClick={(e) => e.stopPropagation()}
                          />
                        </div>
                      </>
                    )}
                  />
                </Form.Item>
              </Form>
              <div className="mt-tag-pop-actions">
                <Button type="link" onClick={() => setExistOpen(false)}>
                  取消
                </Button>
                <Button type="primary" icon={<PlusOutlined />} onClick={appendExist}>
                  添加标签
                </Button>
              </div>
            </div>
          }
        >
          <Button type="link" className="mt-add-green" icon={<PlusOutlined />} size="small">
            添加已有标签
          </Button>
        </Popover>
      </div>
      <div className="mt-tag-row">
        <span className="mt-tag-title">新创建标签（{rawPairs.length}）</span>
        {rawPairs.map((p) => (
          <Tag key={`raw-${p.key}`} closable color="orange" onClose={(e) => removePair(p.key, e)}>
            {p.key}={p.value}
          </Tag>
        ))}
        <Popover
          open={newOpen}
          onOpenChange={setNewOpen}
          trigger="click"
          placement="bottomLeft"
          content={
            <div className="mt-tag-pop" onClick={(e) => e.stopPropagation()}>
              <Form layout="vertical" size="small">
                <Form.Item label="标签名">
                  <Input value={newKey} onChange={(e) => setNewKey(e.target.value)} placeholder="如 app" />
                </Form.Item>
                <Form.Item label="标签值">
                  <Input value={newValue} onChange={(e) => setNewValue(e.target.value)} placeholder="如 fe-StarRocks" />
                </Form.Item>
              </Form>
              <div className="mt-tag-pop-actions">
                <Button type="link" onClick={() => setNewOpen(false)}>
                  取消
                </Button>
                <Button type="primary" icon={<PlusOutlined />} onClick={appendNew}>
                  添加标签
                </Button>
              </div>
            </div>
          }
        >
          <Button type="link" className="mt-add-green" icon={<PlusOutlined />} size="small">
            添加新标签
          </Button>
        </Popover>
      </div>
    </div>
  )
}

export default function MonitorTargets() {
  const [rows, setRows] = useState<MonitorJob[]>([])
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [visible, setVisible] = useState(false)
  const [detail, setDetail] = useState<MonitorJob | null>(null)
  const [editingId, setEditingId] = useState('')
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const [targets, setTargets] = useState<TargetGroup[]>([])
  const [jobLabels, setJobLabels] = useState<LabelPair[]>([])
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [relabelText, setRelabelText] = useState('')
  const [metricRelabelText, setMetricRelabelText] = useState('')
  const [paramsText, setParamsText] = useState('')

  const [tgVisible, setTgVisible] = useState(false)
  const [tgEditingIdx, setTgEditingIdx] = useState<number | null>(null)
  const [tgForm] = Form.useForm()
  const [tgLabels, setTgLabels] = useState<LabelPair[]>([])
  const [envs, setEnvs] = useState<{ code?: string; name?: string }[]>([])
  const [tagDefs, setTagDefs] = useState<{ key: string; values: string[] }[]>([])

  const collectorOptions = useMemo(
    () => [
      LOCAL_COLLECTOR,
      ...envs
        .map((e) => {
          const code = String(e.code || '').trim()
          if (!code) return null
          return { value: code, label: String(e.name || '').trim() || code }
        })
        .filter(Boolean) as { value: string; label: string }[],
    ],
    [envs],
  )

  const presetTagKeys = useMemo(() => {
    const codes = envs
      .map((e) => String(e.code || '').trim())
      .filter(Boolean)
      .sort((a, b) => a.localeCompare(b))
    const base = tagDefs.length ? tagDefs : PRESET_FALLBACK_TAG_KEYS
    return base.map((def) => {
      const defined = (def.values || []).map((v) => String(v).trim()).filter(Boolean)
      // 标签管理已配置候选值 → 严格只用这些；未配置时 idc/env 才回退环境编码
      if (defined.length > 0) {
        return { key: def.key, values: defined }
      }
      if (def.key === 'idc' || def.key === 'env') {
        return { key: def.key, values: codes }
      }
      return { key: def.key, values: [] }
    })
  }, [envs, tagDefs])

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/monitor-targets', { params: { keyword: keyword || undefined } })
      setRows(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  async function loadEnvs() {
    try {
      const { data } = await api.get('/environments')
      setEnvs(data.data || [])
    } catch {
      setEnvs([])
    }
  }

  async function loadTags() {
    try {
      const { data } = await api.get('/monitor-tags')
      const list = (data.data || []) as { key?: string; values?: string[] }[]
      setTagDefs(
        list
          .map((t) => ({
            key: String(t.key || '').trim(),
            values: Array.isArray(t.values) ? t.values.map(String) : [],
          }))
          .filter((t) => t.key),
      )
    } catch {
      setTagDefs([])
    }
  }

  useEffect(() => {
    load()
    loadEnvs()
    loadTags()
  }, [])

  function resetAdvanced() {
    setRelabelText('')
    setMetricRelabelText('')
    setParamsText('')
  }

  function openCreate() {
    setEditingId('')
    form.setFieldsValue({
      name: '',
      monitor_type: undefined,
      collector: undefined,
      scheme: 'http',
      metrics_path: '/metrics',
      scrape_interval: '1m',
      scrape_timeout: '10s',
      description: '',
    })
    setTargets([])
    setJobLabels([])
    resetAdvanced()
    setVisible(true)
  }

  async function openEdit(row: MonitorJob) {
    const { data } = await api.get(`/monitor-targets/${row.id}`)
    const it: MonitorJob = data.data || row
    setEditingId(it.id || '')
    form.setFieldsValue({
      name: it.name,
      monitor_type: it.monitor_type || 'direct',
      collector: it.collector || undefined,
      scheme: it.scheme || 'http',
      metrics_path: it.metrics_path || '/metrics',
      scrape_interval: it.scrape_interval || '1m',
      scrape_timeout: it.scrape_timeout || '10s',
      description: it.description || '',
    })
    setTargets(
      (it.target_groups || []).map((g) => ({
        endpoints: g.endpoints,
        // 深拷贝，避免多组共享同一 labels 引用
        labels: { ...(g.labels || {}) },
        description: g.description,
      })),
    )
    // 标签管理仅反映任务级 labels，不与各组混并（各组可有不同 app 等）
    setJobLabels(labelsToPairs(it.labels || {}))
    resetAdvanced()
    setVisible(true)
  }

  async function openDetail(row: MonitorJob) {
    const { data } = await api.get(`/monitor-targets/${row.id}`)
    setDetail(data.data || row)
  }

  function openAddTarget() {
    setTgEditingIdx(null)
    tgForm.setFieldsValue({ endpoints: '', description: '' })
    // 新建目标用当前任务级标签作默认值，可再单独改
    setTgLabels(labelsToPairs(pairsToLabels(jobLabels)))
    setTgVisible(true)
  }

  function openEditTarget(idx: number) {
    const t = targets[idx]
    setTgEditingIdx(idx)
    tgForm.setFieldsValue({ endpoints: t.endpoints || '', description: t.description || '' })
    // 编辑区展示「任务基底 + 本组覆盖」，保存时写回该组完整 labels
    setTgLabels(labelsToPairs({ ...pairsToLabels(jobLabels), ...(t.labels || {}) }))
    setTgVisible(true)
  }

  async function saveTarget() {
    const values = await tgForm.validateFields()
    const endpoints = String(values.endpoints || '')
      .split(/[\n,;]+/)
      .map((s: string) => s.trim())
      .filter(Boolean)
      .join(',')
    if (!endpoints) {
      message.warning('请输入监控目标')
      return
    }
    const next: TargetGroup = {
      endpoints,
      labels: pairsToLabels(tgLabels),
      description: values.description || '',
    }
    if (tgEditingIdx == null) {
      setTargets((prev) => [...prev, next])
    } else {
      setTargets((prev) => prev.map((t, i) => (i === tgEditingIdx ? next : t)))
    }
    // 不回写 jobLabels，避免牵连其它目标组
    setTgVisible(false)
  }

  async function save() {
    const values = await form.validateFields()
    if (!targets.length) {
      message.warning('请至少添加一个监控目标')
      return
    }
    setSaving(true)
    try {
      const typeLabel = MONITOR_TYPE_OPTIONS.find((o) => o.value === values.monitor_type)?.label || '直连类型监控模板'
      const jobL = pairsToLabels(jobLabels)
      const payload = {
        name: values.name,
        template: typeLabel,
        collector: values.collector,
        scheme: values.scheme,
        status: 'active',
        monitor_type: values.monitor_type || 'direct',
        metrics_path: values.metrics_path,
        scrape_interval: values.scrape_interval,
        scrape_timeout: values.scrape_timeout,
        labels: jobL,
        description: values.description || '',
        // 任务级为基底，各组自有 labels 覆盖；互不影响
        target_groups: targets.map((g) => ({
          endpoints: g.endpoints,
          labels: { ...jobL, ...(g.labels || {}) },
          description: g.description || '',
        })),
      }
      if (editingId) {
        await api.put(`/monitor-targets/${editingId}`, payload)
      } else {
        await api.post('/monitor-targets', payload)
      }
      message.success(editingId ? '已更新' : '已创建')
      setVisible(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  function remove(row: MonitorJob) {
    Modal.confirm({
      title: '确认删除该监控任务？',
      onOk: async () => {
        await api.delete(`/monitor-targets/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const jobLabelMap = useMemo(() => pairsToLabels(jobLabels), [jobLabels])

  function onJobLabelsChange(next: LabelPair[]) {
    // 只改任务级，不整表覆盖各目标组
    setJobLabels(next)
  }

  const columns = useMemo(
    () => [
      { title: '任务名', dataIndex: 'name', key: 'name' },
      { title: '采集点', dataIndex: 'collector', key: 'collector', width: 140 },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 100,
        render: (v: string) => <Tag color={v === 'active' ? 'green' : 'default'}>{v || '—'}</Tag>,
      },
      { title: '类型', dataIndex: 'monitor_type', key: 'monitor_type', width: 100 },
      { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 190 },
      {
        title: '操作',
        key: 'action',
        width: 200,
        render: (_: unknown, row: MonitorJob) => (
          <Space>
            <Button type="link" onClick={() => openDetail(row)}>
              详情
            </Button>
            <Button type="link" onClick={() => openEdit(row)}>
              编辑
            </Button>
            <Button type="link" danger onClick={() => remove(row)}>
              删除
            </Button>
          </Space>
        ),
      },
    ],
    [],
  )

  return (
    <div className="ops-page monitor-targets-page">
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 12 }}
        message={
          <span>
            监控目标仅登记在本平台本地库，不会下发到 Prometheus。FE 管理 / 海豚配置会按任务名与目标组 label 预填。标签名请在{' '}
            <Link to="/config/tags">配置中心 · 标签管理</Link> 维护。
          </span>
        }
      />
      <div className="ops-title">
        <Space wrap>
          <Input.Search
            allowClear
            placeholder="关键字"
            style={{ width: 220 }}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onSearch={() => load()}
          />
          <Button onClick={load}>刷新</Button>
          <Button type="primary" onClick={openCreate}>
            创建新监控任务
          </Button>
        </Space>
      </div>
      <div className="ops-table">
        <ResponsiveTable rowKey="id" loading={loading} columns={columns as any} dataSource={rows} pagination={{ pageSize: 10 }} />
      </div>

      <Modal
        className="mt-create-modal"
        title={editingId ? '编辑监控任务' : '创建新监控任务'}
        open={visible}
        onCancel={() => setVisible(false)}
        width={800}
        destroyOnClose
        footer={
          <div className="mt-modal-footer">
            <Button onClick={() => setVisible(false)}>取消</Button>
            <Button type="primary" icon={<PlusOutlined />} loading={saving} onClick={save}>
              {editingId ? '更新监控' : '新建监控'}
            </Button>
          </div>
        }
      >
        <Form
          form={form}
          layout="vertical"
          className="mt-create-form"
          requiredMark={(label, { required }) => (
            <>
              {label}
              {required ? <span className="mt-req">*</span> : null}
            </>
          )}
        >
          <Form.Item
            name="name"
            label="监控任务名称"
            rules={[{ required: true, message: '请输入监控任务名称' }]}
          >
            <Input placeholder="请输入监控任务名称" />
          </Form.Item>

          <Form.Item
            name="monitor_type"
            label="监控类型"
            rules={[{ required: true, message: '请选择监控类型' }]}
          >
            <Select
              placeholder="请选择监控类型"
              options={MONITOR_TYPE_OPTIONS}
              onChange={(v) => {
                if (v === 'direct' || v === 'proxy') {
                  form.setFieldsValue({ metrics_path: form.getFieldValue('metrics_path') || '/metrics' })
                }
              }}
            />
          </Form.Item>

          <Form.Item label="监控目标设置" className="mt-target-field">
            <div className="mt-target-list">
              {targets.map((t, idx) => {
                // 任务级为默认，本组 labels 覆盖（如 FE/BE 不同 app）
                const shown = { ...jobLabelMap, ...(t.labels || {}) }
                return (
                <div className="mt-target-card" key={idx}>
                  <div className="mt-target-actions">
                    <Button type="text" size="small" icon={<EditOutlined />} onClick={() => openEditTarget(idx)} />
                    <Button
                      type="text"
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() => setTargets((prev) => prev.filter((_, i) => i !== idx))}
                    />
                  </div>
                  <div className="mt-target-line">
                    <span className="mt-target-label">监控目标:</span>
                    <span className="mt-target-eps">
                      {String(t.endpoints || '')
                        .split(',')
                        .filter(Boolean)
                        .map((ep) => (
                          <Tag key={ep} color="blue">
                            {ep}
                          </Tag>
                        ))}
                    </span>
                  </div>
                  <div className="mt-target-line">
                    <span className="mt-target-label">监控label:</span>
                    <span>
                      {Object.entries(shown).map(([k, v]) => (
                        <Tag key={k} color="orange">
                          {k}={v}
                        </Tag>
                      ))}
                      {!Object.keys(shown).length ? <span className="mt-muted">无</span> : null}
                    </span>
                  </div>
                  <div className="mt-target-line">
                    <span className="mt-target-label">描述:</span>
                    <span>{t.description || '无描述'}</span>
                  </div>
                </div>
                )
              })}
              <Button type="link" className="mt-add-green" icon={<PlusOutlined />} onClick={openAddTarget}>
                添加监控目标
              </Button>
            </div>
          </Form.Item>

          <Form.Item
            name="collector"
            label="选择采集点"
            rules={[{ required: true, message: '请选择监控采集点' }]}
          >
            <Select
              showSearch
              allowClear
              placeholder="请选择监控采集点"
              options={collectorOptions}
              optionFilterProp="label"
            />
          </Form.Item>

          <Form.Item name="scheme" label="Scheme" rules={[{ required: true, message: '请选择 Scheme' }]}>
            <Select
              options={[
                { value: 'http', label: 'http' },
                { value: 'https', label: 'https' },
              ]}
            />
          </Form.Item>

          <Row gutter={12}>
            <Col span={8}>
              <Form.Item
                name="scrape_interval"
                label="采样间隔"
                rules={[{ required: true, message: '请选择采样间隔' }]}
              >
                <Select options={INTERVAL_OPTIONS} placeholder="请选择采样间隔" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                name="metrics_path"
                label="监控路径："
                rules={[{ required: true, message: '请输入路径' }]}
              >
                <Input placeholder="请输入路径" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                name="scrape_timeout"
                label="采集超时设置："
                rules={[{ required: true, message: '请选择超时' }]}
              >
                <Select options={TIMEOUT_OPTIONS} placeholder="请选择" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item
            label="标签管理"
            extra="任务级默认标签（新建目标时预填、展示基底）。各监控目标可单独编辑 label（如 FE/BE 不同 app），互不影响；改完后点底部「更新监控/新建监控」落库。"
          >
            <TagManager value={jobLabels} onChange={onJobLabelsChange} presetKeys={presetTagKeys} />
          </Form.Item>

          <Form.Item label="高级设置：">
            <Button type="link" icon={<SettingOutlined />} onClick={() => setAdvancedOpen(true)}>
              设置
            </Button>
          </Form.Item>

          <Form.Item name="description" label="监控描述：">
            <Input.TextArea rows={6} placeholder="请输入监控描述" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="目标设置组"
        open={tgVisible}
        onCancel={() => setTgVisible(false)}
        width={720}
        destroyOnClose
        footer={
          <div className="mt-modal-footer">
            <Button onClick={() => setTgVisible(false)}>取消</Button>
            <Button type="primary" onClick={saveTarget}>
              保存
            </Button>
          </div>
        }
      >
        <Form form={tgForm} layout="vertical">
          <Form.Item
            name="endpoints"
            label="监控目标"
            rules={[{ required: true, message: '请输入监控地址' }]}
            extra="多个地址用英文逗号或换行分隔，如：172.16.0.40:59000"
          >
            <Input.TextArea rows={3} placeholder="请输入监控地址, 如： 172.16.0.40:59000" />
          </Form.Item>
          <Form.Item label="标签设置">
            <TagManager value={tgLabels} onChange={setTgLabels} presetKeys={presetTagKeys} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input placeholder="请输入组目标描述" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="高级设置"
        open={advancedOpen}
        onCancel={() => setAdvancedOpen(false)}
        onOk={() => setAdvancedOpen(false)}
        width={640}
        destroyOnClose
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="本期仅本地保存占位，不生效 Relabel / Metric Relabel / Params。"
        />
        <Form layout="vertical">
          <Form.Item label="Relabel Configs（JSON / YAML 文本）">
            <Input.TextArea rows={4} value={relabelText} onChange={(e) => setRelabelText(e.target.value)} placeholder="暂无配置" />
          </Form.Item>
          <Form.Item label="Metric Relabel Configs">
            <Input.TextArea
              rows={4}
              value={metricRelabelText}
              onChange={(e) => setMetricRelabelText(e.target.value)}
              placeholder="暂无配置"
            />
          </Form.Item>
          <Form.Item label="Params">
            <Input.TextArea rows={3} value={paramsText} onChange={(e) => setParamsText(e.target.value)} placeholder="暂无配置" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title="监控任务详情" open={!!detail} onCancel={() => setDetail(null)} footer={null} width={720}>
        {detail ? (
          <div className="detail-blocks">
            <h4>采集设置</h4>
            <p>
              模板：{detail.template || '—'} / 采集点：{detail.collector || '—'}
            </p>
            {(detail.target_groups || []).map((g, i) => (
              <div key={g.id || i} className="mt-target-card">
                <div>目标：{g.endpoints}</div>
                <div>标签：{labelsToText(g.labels) || '—'}</div>
                <div>描述：{g.description || '—'}</div>
              </div>
            ))}
            <h4>基础设置</h4>
            <p>
              Scheme={detail.scheme} / 状态={detail.status} / 路径={detail.metrics_path} / 间隔={detail.scrape_interval} /
              超时={detail.scrape_timeout}
            </p>
            <p>自定义标签：{labelsToText(detail.labels) || '—'}</p>
            <h4>高级设置</h4>
            <p className="mt-muted">暂无配置（本期不接 Relabel / Prometheus）</p>
            {detail.description ? (
              <>
                <h4>监控描述</h4>
                <p>{detail.description}</p>
              </>
            ) : null}
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
