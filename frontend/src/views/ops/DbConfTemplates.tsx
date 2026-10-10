import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Card, Col, Empty, Form, Input, Modal, Popconfirm, Row, Select, Space, Spin, Tabs, Tag, Typography, message,
} from 'antd'
import api from '../../api/request'

export const CONF_TEMPLATE_ENGINES = ['mysql', 'postgres']

type ConfTemplateView = {
  major: string
  label: string
  content: string
  custom: boolean
  builtin: boolean
  updated_by?: string
  updated_at?: string
}

type ConfTemplateMeta = { vars: string[]; managed_keys: string[]; builtin_majors: string[] }

type ConfTplSource = { kind: 'platform' | 'builtin'; major: string; label: string }

type NodeConfPreview = {
  node_id: string
  host: string
  role: string
  path?: string
  content?: string
  source: ConfTplSource
  error?: string
}

const monoStyle: React.CSSProperties = { fontFamily: 'Menlo, Consolas, monospace', fontSize: 12 }

const errMsg = (e: any) => e?.response?.data?.message || String(e)

function SourceTag({ src }: { src?: ConfTplSource }) {
  if (!src?.label) return null
  return <Tag color={src.kind === 'platform' ? 'blue' : 'default'}>{src.label}</Tag>
}

/** 设置页「参数模板」：平台模板按大版本维护，未自定义的大版本展示内置模板。 */
export function DbConfTemplatesCard({ engine }: { engine: string }) {
  const [list, setList] = useState<ConfTemplateView[]>([])
  const [meta, setMeta] = useState<ConfTemplateMeta>({ vars: [], managed_keys: [], builtin_majors: [] })
  const [loading, setLoading] = useState(false)
  const [active, setActive] = useState<string>()
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [addForm] = Form.useForm()

  const load = useCallback(async (focus?: string) => {
    setLoading(true)
    try {
      const res = await api.get('/db-deploy/conf-templates', { params: { engine } })
      const data: ConfTemplateView[] = res.data?.data || []
      setList(data)
      setMeta(res.data?.meta || { vars: [], managed_keys: [], builtin_majors: [] })
      setDrafts({})
      setActive((cur) => {
        const want = focus ?? cur
        return want && data.some((t) => t.major === want) ? want : data[0]?.major
      })
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [engine])

  useEffect(() => { void load() }, [load])

  const current = useMemo(() => list.find((t) => t.major === active), [list, active])
  const draft = current ? (drafts[current.major] ?? current.content) : ''
  const dirty = !!current && drafts[current.major] !== undefined && drafts[current.major] !== current.content

  const save = async () => {
    if (!current) return
    setSaving(true)
    try {
      await api.put('/db-deploy/conf-templates', { engine, major: current.major, content: draft })
      message.success('已保存，对后续部署 / 扩容生效')
      await load(current.major)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  const reset = async () => {
    if (!current) return
    try {
      await api.post('/db-deploy/conf-templates/reset', { engine, major: current.major })
      message.success(current.builtin ? '已恢复内置模板' : '已删除')
      await load(current.builtin ? current.major : undefined)
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const addMajor = async () => {
    const v = await addForm.validateFields()
    const major = String(v.major).trim()
    if (list.some((t) => t.major === major)) {
      message.warning(`大版本 ${major} 已存在`)
      return
    }
    const from = list.find((t) => t.major === v.copy_from)
    try {
      await api.put('/db-deploy/conf-templates', { engine, major, content: from?.content || '' })
      message.success(`已新增 ${major} 模板`)
      setAddOpen(false)
      await load(major)
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  return (
    <Card
      size="small"
      title="参数模板"
      style={{ marginTop: 16 }}
      extra={(
        <Button size="small" onClick={() => {
          addForm.resetFields()
          addForm.setFieldsValue({ copy_from: active })
          setAddOpen(true)
        }}
        >
          新增大版本模板
        </Button>
      )}
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="部署 / 扩容时按集群版本的大版本选模板：平台模板（精确大版本 → 内置对应大版本 → *）优先，否则用内置模板；再叠加集群覆盖与平台托管项。"
        description={(
          <div style={{ fontSize: 12 }}>
            <div>
              可用变量：
              {meta.vars.map((v) => <Typography.Text key={v} code>{`{{${v}}}`}</Typography.Text>)}
            </div>
            <div style={{ marginTop: 4 }}>
              平台托管（不可填写）：
              {meta.managed_keys.map((k) => <Tag key={k} style={{ marginBottom: 2 }}>{k}</Tag>)}
            </div>
          </div>
        )}
      />
      <Spin spinning={loading}>
        {list.length === 0 ? <Empty /> : (
          <>
            <Tabs
              size="small"
              activeKey={active}
              onChange={setActive}
              items={list.map((t) => ({
                key: t.major,
                label: (
                  <Space size={4}>
                    {t.major === '*' ? '通用 (*)' : t.major}
                    {t.custom ? <Tag color="blue">已自定义</Tag> : <Tag>内置</Tag>}
                  </Space>
                ),
              }))}
            />
            {current && (
              <>
                <Input.TextArea
                  value={draft}
                  onChange={(e) => setDrafts((d) => ({ ...d, [current.major]: e.target.value }))}
                  autoSize={{ minRows: 16, maxRows: 32 }}
                  style={monoStyle}
                  spellCheck={false}
                />
                <Space style={{ marginTop: 8 }} wrap>
                  <Button type="primary" loading={saving} disabled={!dirty && current.custom} onClick={() => void save()}>
                    {current.custom ? '保存' : '保存为平台模板'}
                  </Button>
                  {dirty && <Button onClick={() => setDrafts((d) => { const n = { ...d }; delete n[current.major]; return n })}>撤销修改</Button>}
                  {current.custom && (
                    <Popconfirm
                      title={current.builtin ? '恢复为内置模板？平台自定义内容将被删除' : `删除 ${current.major} 模板？`}
                      onConfirm={() => void reset()}
                    >
                      <Button danger>{current.builtin ? '恢复内置' : '删除'}</Button>
                    </Popconfirm>
                  )}
                  {current.updated_at && (
                    <span className="muted" style={{ fontSize: 12 }}>
                      {current.updated_by || '—'} 更新于 {current.updated_at}
                    </span>
                  )}
                </Space>
              </>
            )}
          </>
        )}
      </Spin>

      <Modal title="新增大版本模板" open={addOpen} onCancel={() => setAddOpen(false)} onOk={() => void addMajor()} destroyOnClose>
        <Form form={addForm} layout="vertical">
          <Form.Item
            name="major"
            label="大版本"
            extra={engine === 'mysql' ? '如 8.0 / 8.4 / 9.1；精确匹配集群版本的 主.次' : '如 15 / 16 / 17；* 为通用'}
            rules={[{ required: true }, { pattern: /^(\*|\d+(\.\d+)?)$/, message: '格式应为 * 或 8.0 / 16' }]}
          >
            <Input placeholder={engine === 'mysql' ? '8.4' : '16'} />
          </Form.Item>
          <Form.Item name="copy_from" label="复制自">
            <Select allowClear options={list.map((t) => ({ value: t.major, label: t.label }))} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}

/** 集群详情「参数」：集群级覆盖 + 各节点最终 conf 预览。 */
export function DbClusterConfModal({
  open, clusterId, clusterName, onClose,
}: { open: boolean; clusterId?: string; clusterName?: string; onClose: () => void }) {
  const [content, setContent] = useState('')
  const [saved, setSaved] = useState('')
  const [managed, setManaged] = useState<string[]>([])
  const [preview, setPreview] = useState<NodeConfPreview[]>([])
  const [major, setMajor] = useState('')
  const [loading, setLoading] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [activeNode, setActiveNode] = useState<string>()

  const runPreview = useCallback(async (override?: string) => {
    if (!clusterId) return
    setPreviewing(true)
    try {
      const res = override === undefined
        ? await api.get(`/db-deploy/clusters/${clusterId}/conf/preview`)
        : await api.post(`/db-deploy/clusters/${clusterId}/conf/preview`, { content: override })
      const data: NodeConfPreview[] = res.data?.data || []
      setPreview(data)
      setMajor(res.data?.major || '')
      setActiveNode((cur) => (cur && data.some((n) => n.node_id === cur) ? cur : data[0]?.node_id))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setPreviewing(false)
    }
  }, [clusterId])

  useEffect(() => {
    if (!open || !clusterId) return
    setLoading(true)
    api.get(`/db-deploy/clusters/${clusterId}/conf`)
      .then((res) => {
        const c = res.data?.data?.content || ''
        setContent(c)
        setSaved(c)
        setManaged(res.data?.managed_keys || [])
      })
      .catch((e) => message.error(errMsg(e)))
      .finally(() => setLoading(false))
    void runPreview()
  }, [open, clusterId, runPreview])

  const save = async () => {
    if (!clusterId) return
    setSaving(true)
    try {
      await api.put(`/db-deploy/clusters/${clusterId}/conf`, { content })
      setSaved(content)
      message.success('已保存集群参数覆盖')
      await runPreview()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  const dirty = content !== saved
  const cur = preview.find((n) => n.node_id === activeNode)

  return (
    <Modal
      title={`参数 · ${clusterName || ''}`}
      open={open}
      onCancel={onClose}
      width={1100}
      footer={null}
      destroyOnClose
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 12 }}
        message="对后续部署 / 扩容生效；已运行实例的在线下发后续提供。"
      />
      <Row gutter={16}>
        <Col span={10}>
          <div style={{ marginBottom: 6, fontWeight: 500 }}>集群覆盖</div>
          <div className="muted" style={{ fontSize: 12, marginBottom: 6 }}>
            每行一个 <Typography.Text code>key = value</Typography.Text>，同名替换模板值，否则追加
            （MySQL 作用于 [mysqld]）；不支持段头、模板变量和托管键：{managed.join(', ')}
          </div>
          <Spin spinning={loading}>
            <Input.TextArea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              autoSize={{ minRows: 14, maxRows: 28 }}
              style={monoStyle}
              spellCheck={false}
              placeholder={'innodb_buffer_pool_size = 8G\nmax_connections = 3000'}
            />
          </Spin>
          <Space style={{ marginTop: 8 }}>
            <Button onClick={() => void runPreview(content)} loading={previewing}>预览</Button>
            <Button type="primary" disabled={!dirty} loading={saving} onClick={() => void save()}>保存</Button>
            {dirty && <Button onClick={() => { setContent(saved); void runPreview() }}>撤销</Button>}
          </Space>
        </Col>
        <Col span={14}>
          <div style={{ marginBottom: 6, fontWeight: 500 }}>
            节点最终配置
            {major && <span className="muted" style={{ fontWeight: 400, marginLeft: 8 }}>大版本 {major}</span>}
            {dirty && <Tag color="orange" style={{ marginLeft: 8 }}>含未保存覆盖</Tag>}
          </div>
          <Spin spinning={previewing}>
            {preview.length === 0 ? <Empty description="暂无节点" /> : (
              <>
                <Tabs
                  size="small"
                  activeKey={activeNode}
                  onChange={setActiveNode}
                  items={preview.map((n) => ({ key: n.node_id, label: `${n.role} · ${n.host}` }))}
                />
                {cur && (
                  <>
                    <Space size={4} style={{ marginBottom: 6 }} wrap>
                      <SourceTag src={cur.source} />
                      {cur.path && <Typography.Text code>{cur.path}</Typography.Text>}
                    </Space>
                    {cur.error ? <Alert type="error" showIcon message={cur.error} /> : (
                      <pre style={{ ...monoStyle, maxHeight: 520, overflow: 'auto', background: '#fafafa', padding: 8, margin: 0, border: '1px solid #f0f0f0' }}>
                        {cur.content}
                      </pre>
                    )}
                  </>
                )}
              </>
            )}
          </Spin>
        </Col>
      </Row>
    </Modal>
  )
}
