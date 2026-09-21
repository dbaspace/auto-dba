import React, { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Switch,
  Spin,
  Tabs,
  message,
} from 'antd'
import {
  CheckCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import api from '../../api/request'
import ReviewRuleField, { RuleRow } from './ReviewRuleField'
import RedisCommandRulesPanel from './RedisCommandRulesPanel'
import './ReviewRules.css'

type Engine = 'mysql' | 'postgres' | 'starrocks' | 'redis'
const ENGINE_TABS: { key: Engine; label: string }[] = [
  { key: 'mysql', label: 'MySQL' },
  { key: 'postgres', label: 'PostgreSQL' },
  { key: 'starrocks', label: 'StarRocks' },
  { key: 'redis', label: 'Redis' },
]

function isSqlEngine(eng: Engine): eng is 'mysql' | 'postgres' | 'starrocks' {
  return eng !== 'redis'
}

interface EngineCache {
  rows: RuleRow[]
  drafts: Record<string, string>
  saved: Record<string, string>
  loaded: boolean
}

interface GiForm {
  enabled: boolean
  host: string
  port: number
  user: string
  password: string
}

interface AddForm {
  name: string
  label: string
  value: string
}

function isBoolText(v: unknown): boolean {
  const s = String(v ?? '').trim().toLowerCase()
  return s === 'true' || s === 'false'
}
function isTrue(v: unknown): boolean {
  return String(v ?? '').trim().toLowerCase() === 'true'
}
function isDml(row: RuleRow & { category?: string }): boolean {
  return (
    /^dml/i.test(String(row?.name ?? '').trim()) ||
    String(row?.category ?? '').toLowerCase() === 'dml'
  )
}
function isWide(row: RuleRow): boolean {
  const v = String(row?.value ?? '').trim()
  return v.startsWith('{') || v.startsWith('[')
}

function normalizeList(data: any): RuleRow[] {
  if (Array.isArray(data)) return data
  if (data && typeof data === 'object') {
    return [...(data.ddl || []), ...(data.dml || [])]
  }
  return []
}

function emptyCache(): EngineCache {
  return { rows: [], drafts: {}, saved: {}, loaded: false }
}

export default function ReviewRules() {
  const [engine, setEngine] = useState<Engine>('mysql')
  const [keyword, setKeyword] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [adding, setAdding] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [addForm] = Form.useForm<AddForm>()

  const [giSaving, setGiSaving] = useState(false)
  const [giPinging, setGiPinging] = useState(false)
  const [giLoaded, setGiLoaded] = useState(false)
  const [giPasswordSet, setGiPasswordSet] = useState(false)
  const [giForm, setGiForm] = useState<GiForm>({
    enabled: false,
    host: '',
    port: 4000,
    user: '',
    password: '',
  })

  const [caches, setCaches] = useState<Record<Engine, EngineCache>>(() => ({
    mysql: emptyCache(),
    postgres: emptyCache(),
    starrocks: emptyCache(),
    redis: emptyCache(),
  }))

  const cache = caches[engine]
  const rows = cache.rows
  const drafts = cache.drafts

  const giPwdPlaceholder = giPasswordSet ? '已配置，留空则不修改' : '可空'

  function patchCache(eng: Engine, patch: Partial<EngineCache>) {
    if (!isSqlEngine(eng)) return
    setCaches((prev) => ({ ...prev, [eng]: { ...prev[eng], ...patch } }))
  }

  function setDraft(name: string, value: string) {
    if (!isSqlEngine(engine)) return
    setCaches((prev) => ({
      ...prev,
      [engine]: { ...prev[engine], drafts: { ...prev[engine].drafts, [name]: value } },
    }))
  }

  const filteredRows = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((row) => {
      const current = String(drafts[row.name] ?? row.value ?? '')
      return (
        row.name.toLowerCase().includes(q) ||
        String(row.label ?? '').toLowerCase().includes(q) ||
        current.toLowerCase().includes(q)
      )
    })
  }, [rows, drafts, keyword])

  const dirtyItems = useMemo(() => {
    return rows
      .map((row) => {
        const next = String(drafts[row.name] ?? '')
        const prev = String(cache.saved[row.name] ?? '')
        return next !== prev ? { name: row.name, value: next } : null
      })
      .filter(Boolean) as { name: string; value: string }[]
  }, [rows, drafts, cache.saved])

  const mysqlCols = useMemo(() => {
    const list = filteredRows
    const size = Math.ceil(list.length / 3) || 0
    return [0, 1, 2].map((i) => list.slice(i * size, (i + 1) * size))
  }, [filteredRows])

  const ddlRows = useMemo(() => filteredRows.filter((r) => !isDml(r)), [filteredRows])
  const dmlRows = useMemo(() => filteredRows.filter((r) => isDml(r)), [filteredRows])

  function grouped(list: RuleRow[]) {
    const switches: RuleRow[] = []
    const inputs: RuleRow[] = []
    const wides: RuleRow[] = []
    for (const row of list) {
      if (isWide(row)) wides.push(row)
      else if (isBoolText(row.value)) switches.push(row)
      else inputs.push(row)
    }
    return { switches, inputs, wides }
  }

  async function loadGI() {
    if (giLoaded) return
    try {
      const { data } = await api.get('/review-rules/goinception')
      const d = data.data || {}
      setGiForm({
        enabled: !!d.enabled,
        host: d.host || '',
        port: d.port || 4000,
        user: d.user || '',
        password: '',
      })
      setGiPasswordSet(!!d.password_set)
      setGiLoaded(true)
    } catch {
      setGiLoaded(true)
    }
  }

  async function load(force?: boolean) {
    if (!isSqlEngine(engine)) return
    if (engine === 'mysql') await loadGI()
    if (caches[engine].loaded && !force) return
    setLoading(true)
    try {
      const { data } = await api.get('/review-rules', { params: { engine } })
      const list = normalizeList(data.data)
      const nextDrafts: Record<string, string> = {}
      const nextSaved: Record<string, string> = {}
      for (const row of list) {
        nextDrafts[row.name] = String(row.value ?? '')
        nextSaved[row.name] = String(row.value ?? '')
      }
      patchCache(engine, { rows: list, drafts: nextDrafts, saved: nextSaved, loaded: true })
    } finally {
      setLoading(false)
    }
  }

  function reload() {
    return load(true)
  }

  async function save() {
    if (!isSqlEngine(engine)) return
    if (!dirtyItems.length) {
      message.info('没有需要保存的修改')
      return
    }
    setSaving(true)
    try {
      const { data } = await api.put('/review-rules', { engine, items: dirtyItems })
      if (data.gi_synced === false && data.message) {
        message.warning(data.message)
      } else {
        message.success('已保存')
      }
      await reload()
    } finally {
      setSaving(false)
    }
  }

  async function saveGI() {
    setGiSaving(true)
    try {
      const { data } = await api.put('/review-rules/goinception', {
        enabled: giForm.enabled,
        host: giForm.host.trim(),
        port: giForm.port,
        user: giForm.user.trim(),
        password: giForm.password,
      })
      const d = data.data || {}
      setGiPasswordSet(!!d.password_set)
      setGiForm((prev) => ({ ...prev, password: '' }))
      message.success('goInception 连接已保存')
    } finally {
      setGiSaving(false)
    }
  }

  async function pingGI() {
    setGiPinging(true)
    try {
      const { data } = await api.post('/review-rules/goinception/ping', {
        host: giForm.host.trim(),
        port: giForm.port,
        user: giForm.user.trim(),
        password: giForm.password,
      })
      message.success(data.message || '连接成功')
    } finally {
      setGiPinging(false)
    }
  }

  function openAdd() {
    addForm.resetFields()
    setAddOpen(true)
  }

  async function onAddOk() {
    if (!isSqlEngine(engine)) return
    const values = await addForm.validateFields()
    setAdding(true)
    try {
      const { data } = await api.post('/review-rules', {
        engine,
        name: values.name.trim(),
        label: values.label.trim(),
        value: values.value.trim(),
      })
      if (data.gi_synced === false && data.message) {
        message.warning(data.message)
      } else {
        message.success('已增加')
      }
      setAddOpen(false)
      await reload()
    } finally {
      setAdding(false)
    }
  }

  function onTabChange(next: Engine) {
    setEngine(next)
    setKeyword('')
  }

  // 初次进入及切换引擎时加载（Redis 面板自行拉取）
  useEffect(() => {
    if (!isSqlEngine(engine)) return
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [engine])

  const actionBar = (
    <div className={engine === 'mysql' ? 'title-actions' : 'panel-actions'}>
      <Input
        size="small"
        value={keyword}
        allowClear
        placeholder="搜索参数名 / 中文名 / 当前值"
        style={{ width: 260 }}
        onChange={(e) => setKeyword(e.target.value)}
      />
      <Button size="small" icon={<PlusOutlined />} disabled={saving || adding} onClick={openAdd}>
        增加参数
      </Button>
      <Button size="small" icon={<ReloadOutlined />} loading={loading} disabled={saving} onClick={reload}>
        刷新
      </Button>
      <Button
        size="small"
        type="primary"
        icon={<CheckCircleOutlined />}
        loading={saving}
        disabled={dirtyItems.length === 0}
        onClick={save}
      >
        保存修改{dirtyItems.length ? `（${dirtyItems.length}）` : ''}
      </Button>
    </div>
  )

  return (
    <div className="ops-page review-page">
      <div className="ops-title">
        <h1>审核规则</h1>
        {engine === 'mysql' ? actionBar : null}
      </div>

      <Tabs
        className="inst-tabs"
        activeKey={engine}
        onChange={(k) => onTabChange(k as Engine)}
        items={ENGINE_TABS.map((t) => ({ key: t.key, label: t.label }))}
      />

      {engine === 'redis' ? (
        <div className="review-body">
          <RedisCommandRulesPanel />
        </div>
      ) : engine === 'mysql' ? (
        <Spin spinning={loading} className="review-body-spin" wrapperClassName="review-body-spin">
          <div className="review-body">
            <section className="gi-card">
              <div className="gi-head">
                <h2>goInception 连接</h2>
                <p>
                  启用后，MySQL / TiDB 工单审核走 goInception（账号可空，连接时随机生成会话）。未启用则仍用本地规则。
                </p>
              </div>
              <div className="gi-form">
                <div className="gi-field">
                  <span className="gi-label">启用</span>
                  <Switch
                    size="small"
                    checked={giForm.enabled}
                    disabled={giSaving}
                    onChange={(v) => setGiForm((p) => ({ ...p, enabled: v }))}
                  />
                </div>
                <div className="gi-field">
                  <span className="gi-label">地址</span>
                  <Input
                    size="small"
                    value={giForm.host}
                    placeholder="如 10.0.0.8"
                    style={{ width: 180 }}
                    disabled={giSaving}
                    onChange={(e) => setGiForm((p) => ({ ...p, host: e.target.value }))}
                  />
                </div>
                <div className="gi-field">
                  <span className="gi-label">端口</span>
                  <InputNumber
                    size="small"
                    value={giForm.port}
                    min={1}
                    max={65535}
                    disabled={giSaving}
                    onChange={(v) => setGiForm((p) => ({ ...p, port: Number(v) || 0 }))}
                  />
                </div>
                <div className="gi-field">
                  <span className="gi-label">用户</span>
                  <Input
                    size="small"
                    value={giForm.user}
                    placeholder="可空"
                    style={{ width: 140 }}
                    disabled={giSaving}
                    onChange={(e) => setGiForm((p) => ({ ...p, user: e.target.value }))}
                  />
                </div>
                <div className="gi-field">
                  <span className="gi-label">密码</span>
                  <Input.Password
                    size="small"
                    value={giForm.password}
                    placeholder={giPwdPlaceholder}
                    style={{ width: 180 }}
                    autoComplete="off"
                    disabled={giSaving}
                    onChange={(e) => setGiForm((p) => ({ ...p, password: e.target.value }))}
                  />
                </div>
                <div className="gi-field">
                  <Button size="small" type="primary" loading={giSaving} onClick={saveGI}>
                    保存连接
                  </Button>
                  <Button size="small" loading={giPinging} disabled={giSaving} onClick={pingGI}>
                    测试连接
                  </Button>
                </div>
              </div>
            </section>

            {filteredRows.length === 0 ? (
              <div className="empty">{keyword.trim() ? '无匹配参数' : '暂无参数'}</div>
            ) : (
              <div className="rule-grid">
                {mysqlCols.map((col, i) => (
                  <div className="rule-col" key={i}>
                    {col.map((row) => (
                      <div className="rule-row" key={row.name}>
                        <span className="rule-label" title={row.label || row.name}>
                          {row.label || '—'}
                        </span>
                        <div className="rule-ctrl">
                          {isBoolText(row.value) ? (
                            <Switch
                              checked={isTrue(drafts[row.name])}
                              disabled={saving}
                              size="small"
                              onChange={(on) => setDraft(row.name, on ? 'true' : 'false')}
                            />
                          ) : (
                            <Input
                              value={drafts[row.name]}
                              size="small"
                              className="rule-input"
                              placeholder="请输入参数值"
                              disabled={saving}
                              onChange={(e) => setDraft(row.name, e.target.value)}
                            />
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                ))}
              </div>
            )}
          </div>
        </Spin>
      ) : (
        <div className="review-body">
          {actionBar}
          <Spin spinning={loading}>
            {ddlRows.length === 0 && dmlRows.length === 0 ? (
              <div className="empty">{keyword.trim() ? '无匹配参数' : '暂无参数'}</div>
            ) : (
              <>
                {[
                  { title: 'DDL', list: ddlRows },
                  { title: 'DML', list: dmlRows },
                ].map(({ title, list }) => {
                  const g = grouped(list)
                  return (
                    <section className="rule-section" key={title}>
                      <h2>{title}</h2>
                      {list.length === 0 ? (
                        <div className="empty-sm">{keyword.trim() ? '无匹配参数' : '暂无参数'}</div>
                      ) : (
                        <div className="rule-grid">
                          {g.switches.map((row) => (
                            <div className="rule-cell" key={row.name}>
                              <ReviewRuleField
                                row={row}
                                value={drafts[row.name]}
                                saving={saving}
                                onChange={setDraft}
                              />
                            </div>
                          ))}
                          {g.inputs.length > 0 && (
                            <div className="rule-wide-wrap">
                              {g.inputs.map((row) => (
                                <div className="rule-cell" key={row.name}>
                                  <ReviewRuleField
                                    row={row}
                                    value={drafts[row.name]}
                                    saving={saving}
                                    wide
                                    onChange={setDraft}
                                  />
                                </div>
                              ))}
                            </div>
                          )}
                          {g.wides.map((row) => (
                            <div className="rule-span" key={row.name}>
                              <ReviewRuleField
                                row={row}
                                value={drafts[row.name]}
                                saving={saving}
                                wide
                                onChange={setDraft}
                              />
                            </div>
                          ))}
                        </div>
                      )}
                    </section>
                  )
                })}
              </>
            )}
          </Spin>
        </div>
      )}

      <Modal
        open={addOpen}
        title="增加参数"
        width={480}
        confirmLoading={adding}
        maskClosable={!adding}
        destroyOnClose
        onOk={onAddOk}
        onCancel={() => setAddOpen(false)}
        okText="确定"
        cancelText="取消"
      >
        <Form form={addForm} labelCol={{ style: { width: 80 } }} preserve={false}>
          <Form.Item
            label="参数名"
            name="name"
            rules={[
              { required: true, message: '请输入参数名' },
              {
                validator: (_, v) => {
                  const name = String(v ?? '').trim()
                  if (!name) return Promise.resolve()
                  if (rows.some((row) => row.name.toLowerCase() === name.toLowerCase())) {
                    return Promise.reject(new Error('该参数已在列表中'))
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input placeholder={engine === 'mysql' ? '如 check_dml_where' : '如 DDLMaxColumnCount'} />
          </Form.Item>
          <Form.Item
            label="中文名"
            name="label"
            rules={[{ required: true, message: '请输入中文名' }]}
          >
            <Input placeholder={engine === 'mysql' ? '如 检查 DML WHERE 条件' : '如 建表最大列数'} />
          </Form.Item>
          <Form.Item
            label="当前值"
            name="value"
            rules={[{ required: true, message: '请输入参数值' }]}
          >
            <Input placeholder={engine === 'mysql' ? '如 true' : '如 true 或 64'} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
