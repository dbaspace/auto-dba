import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, DatePicker, Descriptions, Drawer, Empty, Form, Input, Modal, Radio, Select, Space, Table, Tabs, Tag,
  Timeline, Tooltip, Typography, message,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { useAppSelector } from '../../store'
import { Perm, usePermission } from '../../hooks/usePermission'
import {
  TODO_IMAGE_MAX, TodoImageThumbs, TodoImageUpload, clipboardImages, pickValidImages, uploadTodoImageFile,
} from './TodoImages'

type Todo = {
  id: string
  title: string
  source: string
  source_ref: string
  owner: string
  followers: string[]
  plan_at: string
  latest_progress: string
  next_action: string
  status: string
  closed_at: string
  description: string
  created_by: string
  created_at: string
  updated_at: string
  overdue: boolean
  due_soon: boolean
  display_status: string
  subtask_total: number
  subtask_done: number
}

type Subtask = {
  id: string
  todo_id: string
  seq: number
  title: string
  description: string
  owner: string
  plan_at: string
  status: string
  progress: string
  done_at: string
  created_by: string
  created_at: string
  updated_at: string
}

type TodoLog = {
  id: string; subtask_id: string; action: string; from_status: string; to_status: string; content: string; operator: string
  created_at: string
}

type TodoImage = { id: string; todo_id: string; scope: string; ref_id: string; filename: string; created_at: string }

type Detail = { todo: Todo; logs: TodoLog[]; subtasks: Subtask[]; images?: TodoImage[] }

/** 某处（事项 / 子任务 / 跟进记录）的图片 ID。 */
function imagesOf(images: TodoImage[] | undefined, scope: string, refId = '') {
  return (images || []).filter((m) => m.scope === scope && m.ref_id === refId).map((m) => m.id)
}

type UserOption = { username: string; display_name?: string }

const TABS = [
  { key: 'mine', label: '我的待办' },
  { key: 'follow', label: '待我跟进' },
  { key: 'created', label: '我发起的' },
  { key: 'team', label: '团队待办' },
  { key: 'closed', label: '已闭环' },
  { key: 'warning', label: '超期预警' },
]

const SOURCES: Record<string, { label: string; color: string }> = {
  alert: { label: '告警', color: 'red' },
  ticket: { label: '工单', color: 'blue' },
  change: { label: '变更', color: 'purple' },
  inspection: { label: '巡检', color: 'cyan' },
  manual: { label: '手工创建', color: 'default' },
}

const STATUSES: Record<string, { label: string; color: string }> = {
  pending: { label: '待处理', color: 'default' },
  following: { label: '跟进中', color: 'processing' },
  handover: { label: '待交接', color: 'orange' },
  closed: { label: '已闭环', color: 'success' },
  overdue: { label: '已超期', color: 'error' },
}

const SUB_STATUSES: Record<string, { label: string; color: string }> = {
  pending: { label: '待处理', color: 'default' },
  doing: { label: '进行中', color: 'processing' },
  done: { label: '已完成', color: 'success' },
}

const ACTION_LABEL: Record<string, string> = {
  create: '创建', edit: '编辑', progress: '更新进展', handover: '转交', close: '闭环', reopen: '重新打开',
  subtask_add: '新增子任务', subtask_edit: '编辑子任务', subtask_progress: '子任务进展', subtask_delete: '删除子任务',
}

function subOverdue(s: Subtask) {
  return s.status !== 'done' && !!s.plan_at && dayjs(s.plan_at).isBefore(dayjs())
}

function MultiLine({ value }: { value?: string }) {
  if (!value) return <>—</>
  return (
    <Typography.Paragraph
      ellipsis={{ rows: 3, tooltip: <div style={{ whiteSpace: 'pre-wrap' }}>{value}</div> }}
      style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}
    >
      {value}
    </Typography.Paragraph>
  )
}

function fmt(t?: string) {
  return t ? dayjs(t).format('YYYY-MM-DD HH:mm') : '—'
}

function remainText(planAt: string) {
  const mins = dayjs(planAt).diff(dayjs(), 'minute')
  const abs = Math.abs(mins)
  const text = abs >= 1440 ? `${Math.floor(abs / 1440)} 天` : abs >= 60 ? `${Math.floor(abs / 60)} 小时` : `${abs} 分钟`
  return mins < 0 ? `已超期 ${text}` : `剩余 ${text}`
}

type ModalKind = 'create' | 'edit' | 'progress' | 'subtask_add' | 'subtask_edit' | 'handover' | 'close' | 'reopen'

type ModalState = { kind: ModalKind; todo?: Todo; subtask?: Subtask; subs?: Subtask[]; initial?: Record<string, unknown> }

export default function TodoFollowUp() {
  const me = String(useAppSelector((s) => s.auth.user)?.username || '')
  const { hasUrn } = usePermission()
  const [tab, setTab] = useState('mine')
  const [rows, setRows] = useState<Todo[]>([])
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [loading, setLoading] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [source, setSource] = useState<string>()
  const [status, setStatus] = useState<string>()
  const [owner, setOwner] = useState<string>()
  const [users, setUsers] = useState<UserOption[]>([])
  const [modal, setModal] = useState<ModalState | null>(null)
  const [saving, setSaving] = useState(false)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [form] = Form.useForm()

  const userName = useCallback((u?: string) => {
    if (!u) return '—'
    const hit = users.find((x) => x.username === u)
    return hit?.display_name && hit.display_name !== u ? `${hit.display_name}（${u}）` : u
  }, [users])
  const userOptions = useMemo(
    () => users.map((u) => ({ value: u.username, label: u.display_name && u.display_name !== u.username ? `${u.display_name}（${u.username}）` : u.username })),
    [users],
  )

  const canFollow = (t: Todo) =>
    hasUrn(Perm.todo.follow) && (t.owner === me || t.created_by === me || t.followers.includes(me) || hasUrn(Perm.todo.edit))

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/todos', { params: { tab, keyword: keyword || undefined, source, status, owner } })
      setRows(res.data?.data || [])
      setCounts(res.data?.counts || {})
    } finally {
      setLoading(false)
    }
  }, [tab, keyword, source, status, owner])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    api.get('/todos/users', { silent: true }).then((res) => setUsers(res.data?.data || [])).catch(() => setUsers([]))
  }, [])

  const openDetail = async (id: string) => {
    const res = await api.get(`/todos/${id}`)
    setDetail(res.data?.data || null)
  }

  const progressDefaults = (s?: Subtask) =>
    s ? { subtask_id: s.id, status: s.status === 'pending' ? 'doing' : s.status } : {}

  const openModal = async (kind: ModalKind, todo?: Todo, extra?: { subtask?: Subtask; subs?: Subtask[] }) => {
    let subs = extra?.subs
    let images: TodoImage[] | undefined
    if (todo && (kind === 'progress' || kind === 'edit' || kind === 'subtask_edit')) {
      const res = await api.get(`/todos/${todo.id}`)
      const d = res.data?.data as Detail | undefined
      subs = subs || d?.subtasks || []
      images = d?.images
    }
    let initial: Record<string, unknown> = {}
    if (kind === 'create') {
      initial = { source: 'manual', owner: me, plan_at: dayjs().add(1, 'day').hour(18).minute(0).second(0), subtasks: [{}] }
    } else if (kind === 'edit' && todo) {
      initial = {
        ...todo,
        plan_at: todo.plan_at ? dayjs(todo.plan_at) : undefined,
        image_ids: imagesOf(images, 'todo'),
      }
    } else if (kind === 'progress') {
      initial = progressDefaults(extra?.subtask || subs?.find((s) => s.status !== 'done') || subs?.[0])
    } else if (kind === 'subtask_add' && todo) {
      initial = { owner: todo.owner }
    } else if (kind === 'subtask_edit' && extra?.subtask) {
      const s = extra.subtask
      initial = {
        title: s.title,
        description: s.description,
        owner: s.owner,
        plan_at: s.plan_at ? dayjs(s.plan_at) : undefined,
        image_ids: imagesOf(images, 'subtask', s.id),
      }
    }
    setModal({ kind, todo, subtask: extra?.subtask, subs, initial })
  }

  const subtaskPayload = (s: { title?: string; description?: string; owner?: string; plan_at?: Dayjs; image_ids?: string[] }) => ({
    title: s.title, description: s.description, owner: s.owner, plan_at: s.plan_at?.format(), image_ids: s.image_ids || [],
  })

  // 弹窗内任意位置粘贴截图（如在进展输入框里 Ctrl+V），追加到本弹窗的主图片字段
  const [pasting, setPasting] = useState(false)
  const onModalPaste = async (e: React.ClipboardEvent) => {
    if (!modal || modal.kind === 'handover' || modal.kind === 'reopen') return
    if (modal.kind === 'progress' && !modal.subs?.length) return
    const files = clipboardImages(e)
    if (!files.length) return
    e.preventDefault()
    const current = (form.getFieldValue('image_ids') as string[] | undefined) || []
    const picked = pickValidImages(files, TODO_IMAGE_MAX - current.length)
    if (!picked.length) return
    setPasting(true)
    try {
      const ids: string[] = []
      for (const f of picked) {
        const id = await uploadTodoImageFile(f)
        if (id) ids.push(id)
      }
      form.setFieldValue('image_ids', [...((form.getFieldValue('image_ids') as string[] | undefined) || []), ...ids])
      message.success(`已粘贴 ${ids.length} 张图片`)
    } finally {
      setPasting(false)
    }
  }

  const submit = async () => {
    if (!modal) return
    if (modal.kind === 'progress' && !modal.subs?.length) {
      message.warning('请先新增子任务')
      return
    }
    const v = await form.validateFields()
    const id = modal.todo?.id
    setSaving(true)
    try {
      if (modal.kind === 'create' || modal.kind === 'edit') {
        const body = {
          ...v,
          plan_at: (v.plan_at as Dayjs | undefined)?.format(),
          followers: v.followers || [],
          image_ids: v.image_ids || [],
          subtasks: modal.kind === 'create' ? (v.subtasks || []).map(subtaskPayload) : undefined,
        }
        if (modal.kind === 'create') await api.post('/todos', body)
        else await api.put(`/todos/${id}`, body)
        message.success('已保存')
      } else if (modal.kind === 'progress') {
        const res = await api.post(`/todos/${id}/subtasks/${v.subtask_id}/progress`, {
          status: v.status, progress: v.progress, image_ids: v.image_ids || [],
        })
        message.success(res.data?.data?.status === 'closed' ? '子任务已全部完成，事项已自动闭环' : '已保存')
      } else if (modal.kind === 'subtask_add') {
        await api.post(`/todos/${id}/subtasks`, subtaskPayload(v))
        message.success('已保存')
      } else if (modal.kind === 'subtask_edit') {
        await api.put(`/todos/${id}/subtasks/${modal.subtask?.id}`, subtaskPayload(v))
        message.success('已保存')
      } else {
        await api.post(`/todos/${id}/${modal.kind}`, v)
        message.success('已保存')
      }
      setModal(null)
      await load()
      if (detail && id && detail.todo.id === id) await openDetail(id)
    } finally {
      setSaving(false)
    }
  }

  const remove = (t: Todo) => {
    Modal.confirm({
      title: '删除事项',
      content: `删除「${t.title}」及其跟进记录？`,
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete(`/todos/${t.id}`)
        message.success('已删除')
        if (detail?.todo.id === t.id) setDetail(null)
        await load()
      },
    })
  }

  const removeSubtask = (t: Todo, s: Subtask) => {
    Modal.confirm({
      title: '删除子任务',
      content: `删除子任务「${s.title}」？已有的跟进记录会保留。`,
      okButtonProps: { danger: true },
      onOk: async () => {
        const res = await api.delete(`/todos/${t.id}/subtasks/${s.id}`)
        message.success(res.data?.data?.status === 'closed' ? '子任务已全部完成，事项已自动闭环' : '已删除')
        await load()
        await openDetail(t.id)
      },
    })
  }

  const actionButtons = (t: Todo) => {
    const follow = canFollow(t)
    const closed = t.status === 'closed'
    const openSubs = (t.subtask_total || 0) - (t.subtask_done || 0)
    return (
      <Space size={0} wrap onClick={(e) => e.stopPropagation()}>
        {follow && !closed && <Button type="link" size="small" onClick={() => void openModal('progress', t)}>更新进展</Button>}
        {follow && !closed && <Button type="link" size="small" onClick={() => void openModal('subtask_add', t)}>新增子任务</Button>}
        {follow && !closed && <Button type="link" size="small" onClick={() => void openModal('handover', t)}>转交</Button>}
        {follow && !closed && (openSubs > 0 ? (
          <Tooltip title={`还有 ${openSubs} 个子任务未完成，全部完成后将自动闭环`}>
            <Button type="link" size="small" disabled>闭环</Button>
          </Tooltip>
        ) : (
          <Button type="link" size="small" onClick={() => void openModal('close', t)}>闭环</Button>
        ))}
        {follow && closed && <Button type="link" size="small" onClick={() => void openModal('reopen', t)}>重新打开</Button>}
        {hasUrn(Perm.todo.edit) && !closed && <Button type="link" size="small" onClick={() => void openModal('edit', t)}>编辑</Button>}
        {hasUrn(Perm.todo.delete) && <Button type="link" size="small" danger onClick={() => remove(t)}>删除</Button>}
      </Space>
    )
  }

  const statusTag = (t: Todo) => {
    const s = STATUSES[t.display_status] || { label: t.display_status, color: 'default' }
    const tag = <Tag color={s.color}>{s.label}</Tag>
    return t.overdue ? <Tooltip title={`实际状态：${STATUSES[t.status]?.label || t.status}`}>{tag}</Tooltip> : tag
  }

  const columns = [
    {
      title: '事项名称',
      dataIndex: 'title',
      width: 220,
      render: (v: string, t: Todo) => (
        <Space size={4}>
          <Typography.Link onClick={() => void openDetail(t.id)}>{v}</Typography.Link>
          {t.due_soon && <Tag color="warning">即将到期</Tag>}
        </Space>
      ),
    },
    {
      title: '子任务',
      key: 'subtasks',
      width: 80,
      render: (_: unknown, t: Todo) =>
        t.subtask_total ? (
          <Tag color={t.subtask_done === t.subtask_total ? 'success' : 'processing'}>{t.subtask_done}/{t.subtask_total}</Tag>
        ) : '—',
    },
    {
      title: '来源',
      dataIndex: 'source',
      width: 150,
      render: (v: string, t: Todo) => (
        <Space size={4}>
          <Tag color={SOURCES[v]?.color}>{SOURCES[v]?.label || v}</Tag>
          {t.source_ref && <Typography.Text type="secondary" ellipsis style={{ maxWidth: 90 }}>{t.source_ref}</Typography.Text>}
        </Space>
      ),
    },
    { title: '负责人', dataIndex: 'owner', width: 120, render: (v: string) => userName(v) },
    {
      title: '跟进人',
      dataIndex: 'followers',
      width: 140,
      render: (v: string[]) => (v?.length ? v.map((u) => userName(u)).join('、') : '—'),
    },
    {
      title: '计划完成时间',
      dataIndex: 'plan_at',
      width: 160,
      render: (v: string, t: Todo) => (
        <div>
          <div style={t.overdue ? { color: '#cf1322' } : undefined}>{fmt(v)}</div>
          {t.status !== 'closed' && v && (
            <div style={{ fontSize: 12, color: t.overdue ? '#cf1322' : t.due_soon ? '#d46b08' : '#999' }}>{remainText(v)}</div>
          )}
        </div>
      ),
    },
    { title: '最新进展', dataIndex: 'latest_progress', width: 220, render: (v: string) => <MultiLine value={v} /> },
    { title: '下一步动作', dataIndex: 'next_action', width: 220, render: (v: string) => <MultiLine value={v} /> },
    { title: '状态', dataIndex: 'display_status', width: 90, render: (_: string, t: Todo) => statusTag(t) },
    { title: '闭环时间', dataIndex: 'closed_at', width: 140, render: (v: string) => fmt(v) },
    { title: '操作', key: 'op', fixed: 'right' as const, width: 240, render: (_: unknown, t: Todo) => actionButtons(t) },
  ]

  const subColumns = (t: Todo) => {
    const follow = canFollow(t) && t.status !== 'closed'
    const canDelete = hasUrn(Perm.todo.edit) && t.status !== 'closed'
    return [
      { title: '#', key: 'idx', width: 40, render: (_: unknown, __: Subtask, i: number) => i + 1 },
      {
        title: '子任务',
        dataIndex: 'title',
        render: (v: string, s: Subtask) => (
          <div>
            <div style={{ whiteSpace: 'pre-wrap' }}>{v}</div>
            {s.description && <div style={{ whiteSpace: 'pre-wrap', fontSize: 12, color: '#888', marginTop: 2 }}>{s.description}</div>}
            {imagesOf(detail?.images, 'subtask', s.id).length > 0 && (
              <div style={{ marginTop: 4 }}>
                <TodoImageThumbs ids={imagesOf(detail?.images, 'subtask', s.id)} size={40} />
              </div>
            )}
          </div>
        ),
      },
      { title: '负责人', dataIndex: 'owner', width: 110, render: (v: string) => userName(v) },
      {
        title: '计划完成',
        dataIndex: 'plan_at',
        width: 130,
        render: (v: string, s: Subtask) => <span style={subOverdue(s) ? { color: '#cf1322' } : undefined}>{fmt(v)}</span>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 80,
        render: (v: string) => <Tag color={SUB_STATUSES[v]?.color}>{SUB_STATUSES[v]?.label || v}</Tag>,
      },
      {
        title: '最新进展',
        dataIndex: 'progress',
        render: (v: string, s: Subtask) =>
          v ? (
            <div>
              <div style={{ whiteSpace: 'pre-wrap' }}>{v}</div>
              <div style={{ fontSize: 12, color: '#999' }}>{fmt(s.updated_at)}</div>
            </div>
          ) : '—',
      },
      ...(follow || canDelete
        ? [{
          title: '操作',
          key: 'op',
          width: 150,
          render: (_: unknown, s: Subtask) => (
            <Space size={0} wrap>
              {follow && (
                <Button type="link" size="small" onClick={() => void openModal('progress', t, { subtask: s, subs: detail?.subtasks })}>
                  更新进展
                </Button>
              )}
              {follow && <Button type="link" size="small" onClick={() => void openModal('subtask_edit', t, { subtask: s })}>编辑</Button>}
              {canDelete && <Button type="link" size="small" danger onClick={() => removeSubtask(t, s)}>删除</Button>}
            </Space>
          ),
        }]
        : []),
    ]
  }

  const modalTitle: Record<ModalKind, string> = {
    create: '新建待办', edit: '编辑待办', progress: '更新子任务进展', subtask_add: '新增子任务', subtask_edit: '编辑子任务',
    handover: '转交负责人', close: '闭环', reopen: '重新打开',
  }

  const subtaskFields = (
    <>
      <Form.Item name="title" label="子任务名称" rules={[{ required: true, message: '请输入子任务名称' }]}>
        <Input maxLength={200} />
      </Form.Item>
      <Form.Item name="description" label="子任务描述" extra="具体要做的动作、步骤或验收标准">
        <Input.TextArea rows={3} maxLength={2000} />
      </Form.Item>
      <Space style={{ display: 'flex' }} align="start">
        <Form.Item name="owner" label="负责人" style={{ width: 220 }} extra="不填时沿用事项负责人">
          <Select allowClear showSearch options={userOptions} optionFilterProp="label" />
        </Form.Item>
        <Form.Item name="plan_at" label="计划完成时间">
          <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" />
        </Form.Item>
      </Space>
      <Form.Item name="image_ids" label="图片">
        <TodoImageUpload />
      </Form.Item>
    </>
  )

  return (
    <div className="ops-page">
      <Tabs
        activeKey={tab}
        onChange={(k) => { setTab(k); setStatus(undefined) }}
        items={TABS.map((t) => ({
          key: t.key,
          label: (
            <span style={t.key === 'warning' && counts.warning ? { color: '#cf1322' } : undefined}>
              {t.label} ({counts[t.key] ?? 0})
            </span>
          ),
        }))}
        tabBarExtraContent={
          hasUrn(Perm.todo.create) ? <Button type="primary" onClick={() => void openModal('create')}>新建待办</Button> : null
        }
      />
      <Space wrap style={{ marginBottom: 12 }}>
        <Input.Search
          allowClear
          placeholder="事项名称 / 来源单号 / 进展"
          style={{ width: 240 }}
          onSearch={(v) => setKeyword(v.trim())}
        />
        <Select
          allowClear
          placeholder="来源"
          style={{ width: 120 }}
          value={source}
          onChange={setSource}
          options={Object.entries(SOURCES).map(([value, s]) => ({ value, label: s.label }))}
        />
        {tab !== 'closed' && (
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 120 }}
            value={status}
            onChange={setStatus}
            options={Object.entries(STATUSES).filter(([k]) => k !== 'closed').map(([value, s]) => ({ value, label: s.label }))}
          />
        )}
        {(tab === 'team' || tab === 'closed' || tab === 'warning' || tab === 'created') && (
          <Select
            allowClear
            showSearch
            placeholder="负责人"
            style={{ width: 160 }}
            value={owner}
            onChange={setOwner}
            options={userOptions}
            optionFilterProp="label"
          />
        )}
        <Button onClick={() => void load()}>刷新</Button>
      </Space>
      <div className="ops-table">
        <ResponsiveTable<Todo>
          rowKey="id"
          size="small"
          bordered
          loading={loading}
          dataSource={rows}
          columns={columns}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无待办" /> }}
          pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (n) => `共 ${n} 条` }}
        />
      </div>

      <Modal
        title={modal ? modalTitle[modal.kind] + (modal.todo ? ` · ${modal.todo.title}` : '') : ''}
        open={!!modal}
        width={modal?.kind === 'create' ? 760 : modal?.kind === 'edit' ? 640 : 560}
        confirmLoading={saving}
        onOk={() => void submit()}
        onCancel={() => setModal(null)}
        okButtonProps={{ danger: modal?.kind === 'close', disabled: pasting }}
        destroyOnClose
      >
        <div onPaste={(e) => void onModalPaste(e)}>
        <Form
          key={`${modal?.kind}-${modal?.todo?.id ?? ''}-${modal?.subtask?.id ?? ''}`}
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={modal?.initial}
        >
          {(modal?.kind === 'create' || modal?.kind === 'edit') && (
            <>
              <Form.Item name="title" label="事项名称" rules={[{ required: true, message: '请输入事项名称' }]}>
                <Input maxLength={200} />
              </Form.Item>
              <Space style={{ display: 'flex' }} align="start">
                <Form.Item name="source" label="来源" rules={[{ required: true }]} style={{ width: 160 }}>
                  <Select options={Object.entries(SOURCES).map(([value, s]) => ({ value, label: s.label }))} />
                </Form.Item>
                <Form.Item name="source_ref" label="来源单号 / 链接" style={{ width: 420 }}>
                  <Input placeholder="如告警 ID、工单号、变更单号、巡检报告" />
                </Form.Item>
              </Space>
              <Space style={{ display: 'flex' }} align="start">
                <Form.Item name="owner" label="负责人" rules={[{ required: true, message: '请选择负责人' }]} style={{ width: 200 }}>
                  <Select showSearch options={userOptions} optionFilterProp="label" />
                </Form.Item>
                <Form.Item name="followers" label="跟进人" style={{ width: 380 }}>
                  <Select mode="multiple" allowClear options={userOptions} optionFilterProp="label" />
                </Form.Item>
              </Space>
              <Form.Item name="plan_at" label="计划完成时间" rules={[{ required: true, message: '请选择计划完成时间' }]}>
                <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" />
              </Form.Item>
              <Form.Item name="description" label="描述">
                <Input.TextArea rows={3} placeholder="可直接 Ctrl+V 粘贴问题截图" />
              </Form.Item>
              <Form.Item name="image_ids" label="图片">
                <TodoImageUpload />
              </Form.Item>
              {modal?.kind === 'create' && (
                <Form.List name="subtasks">
                  {(fields, { add, remove }) => (
                    <Form.Item label="子任务" extra="事项拆分的具体动作；负责人不填时沿用事项负责人，计划完成时间可不填">
                      {fields.map(({ key, name }, idx) => (
                        <div
                          key={key}
                          style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: '8px 12px 0', marginBottom: 8 }}
                        >
                          <Space align="baseline" style={{ display: 'flex' }}>
                            <span style={{ color: '#999' }}>{idx + 1}.</span>
                            <Form.Item
                              name={[name, 'title']}
                              rules={[{ required: true, message: '请输入子任务名称' }]}
                              style={{ width: 260, marginBottom: 8 }}
                            >
                              <Input placeholder="子任务名称" maxLength={200} />
                            </Form.Item>
                            <Form.Item name={[name, 'owner']} style={{ width: 150, marginBottom: 8 }}>
                              <Select allowClear showSearch placeholder="负责人" options={userOptions} optionFilterProp="label" />
                            </Form.Item>
                            <Form.Item name={[name, 'plan_at']} style={{ marginBottom: 8 }}>
                              <DatePicker showTime={{ format: 'HH:mm' }} format="YYYY-MM-DD HH:mm" placeholder="计划完成时间" />
                            </Form.Item>
                            <MinusCircleOutlined onClick={() => remove(name)} />
                          </Space>
                          <Form.Item name={[name, 'description']} style={{ marginBottom: 8 }}>
                            <Input.TextArea rows={2} maxLength={2000} placeholder="子任务描述：具体要做的动作、步骤或验收标准" />
                          </Form.Item>
                          <Form.Item name={[name, 'image_ids']} style={{ marginBottom: 8 }}>
                            <TodoImageUpload size={56} />
                          </Form.Item>
                        </div>
                      ))}
                      <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add()}>添加子任务</Button>
                    </Form.Item>
                  )}
                </Form.List>
              )}
            </>
          )}
          {modal?.kind === 'progress' && (
            <>
              <div style={{ textAlign: 'right', marginTop: -8 }}>
                <Button type="link" size="small" icon={<PlusOutlined />} onClick={() => void openModal('subtask_add', modal.todo)}>
                  新增子任务
                </Button>
              </div>
              {modal.subs?.length ? (
                <>
                  <Form.Item name="subtask_id" label="子任务" rules={[{ required: true, message: '请选择子任务' }]}>
                    <Select
                      options={modal.subs.map((s, i) => ({
                        value: s.id,
                        label: `${i + 1}. ${s.title}（${SUB_STATUSES[s.status]?.label || s.status}）`,
                      }))}
                      onChange={(sid: string) => form.setFieldsValue(progressDefaults(modal.subs?.find((s) => s.id === sid)))}
                    />
                  </Form.Item>
                  <Form.Item noStyle shouldUpdate={(a, b) => a.subtask_id !== b.subtask_id}>
                    {({ getFieldValue }) => {
                      const s = modal.subs?.find((x) => x.id === getFieldValue('subtask_id'))
                      if (!s || (!s.description && !s.progress)) return null
                      return (
                        <Alert
                          type="info"
                          style={{ marginBottom: 16 }}
                          message={s.title}
                          description={
                            <div style={{ whiteSpace: 'pre-wrap' }}>
                              {s.description && <div>描述：{s.description}</div>}
                              {s.progress && <div style={{ marginTop: s.description ? 6 : 0 }}>当前进展（{fmt(s.updated_at)}）：{s.progress}</div>}
                            </div>
                          }
                        />
                      )
                    }}
                  </Form.Item>
                  <Form.Item name="status" label="子任务状态" rules={[{ required: true, message: '请选择状态' }]}>
                    <Radio.Group
                      optionType="button"
                      options={Object.entries(SUB_STATUSES).map(([value, s]) => ({ value, label: s.label }))}
                    />
                  </Form.Item>
                  <Form.Item
                    name="progress"
                    label="进展说明"
                    rules={[{ required: true, message: '请填写进展说明' }]}
                    extra="追加到跟进记录，历史进展不会被覆盖；全部子任务完成后将自动闭环事项"
                  >
                    <Input.TextArea rows={3} placeholder="可直接在此 Ctrl+V 粘贴截图" />
                  </Form.Item>
                  <Form.Item name="image_ids" label="截图（可选）">
                    <TodoImageUpload />
                  </Form.Item>
                </>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无子任务，请先新增子任务" />
              )}
            </>
          )}
          {(modal?.kind === 'subtask_add' || modal?.kind === 'subtask_edit') && subtaskFields}
          {modal?.kind === 'handover' && (
            <>
              <Form.Item name="owner" label="新负责人" rules={[{ required: true, message: '请选择新负责人' }]}>
                <Select
                  showSearch
                  options={userOptions.filter((o) => o.value !== modal.todo?.owner)}
                  optionFilterProp="label"
                />
              </Form.Item>
              <Form.Item name="note" label="交接说明">
                <Input.TextArea rows={3} />
              </Form.Item>
              <Typography.Text type="secondary">转交后状态为「待交接」，原负责人自动加入跟进人；新负责人更新进展后转为「跟进中」。</Typography.Text>
            </>
          )}
          {modal?.kind === 'close' && (
            <>
              <Form.Item name="note" label="闭环说明" rules={[{ required: true, message: '请填写闭环说明' }]}>
                <Input.TextArea rows={3} placeholder="处理结果，将作为最新进展；可直接 Ctrl+V 粘贴截图" />
              </Form.Item>
              <Form.Item name="image_ids" label="验收截图（可选）">
                <TodoImageUpload />
              </Form.Item>
            </>
          )}
          {modal?.kind === 'reopen' && (
            <Form.Item name="note" label="重新打开原因">
              <Input.TextArea rows={3} />
            </Form.Item>
          )}
        </Form>
        </div>
      </Modal>

      <Drawer
        title={detail?.todo.title}
        open={!!detail}
        width={900}
        onClose={() => setDetail(null)}
        extra={detail ? actionButtons(detail.todo) : null}
      >
        {detail && (
          <>
            <Descriptions column={2} size="small" bordered>
              <Descriptions.Item label="来源">
                <Tag color={SOURCES[detail.todo.source]?.color}>{SOURCES[detail.todo.source]?.label || detail.todo.source}</Tag>
                {detail.todo.source_ref}
              </Descriptions.Item>
              <Descriptions.Item label="状态">{statusTag(detail.todo)}</Descriptions.Item>
              <Descriptions.Item label="负责人">{userName(detail.todo.owner)}</Descriptions.Item>
              <Descriptions.Item label="跟进人">
                {detail.todo.followers.length ? detail.todo.followers.map((u) => userName(u)).join('、') : '—'}
              </Descriptions.Item>
              <Descriptions.Item label="计划完成时间">{fmt(detail.todo.plan_at)}</Descriptions.Item>
              <Descriptions.Item label="闭环时间">{fmt(detail.todo.closed_at)}</Descriptions.Item>
              <Descriptions.Item label="发起人">{userName(detail.todo.created_by)}</Descriptions.Item>
              <Descriptions.Item label="创建时间">{fmt(detail.todo.created_at)}</Descriptions.Item>
              <Descriptions.Item label="最新进展" span={2}>
                <div style={{ whiteSpace: 'pre-wrap' }}>{detail.todo.latest_progress || '—'}</div>
              </Descriptions.Item>
              <Descriptions.Item label="下一步动作（未完成子任务）" span={2}>
                <div style={{ whiteSpace: 'pre-wrap' }}>{detail.todo.next_action || '—'}</div>
              </Descriptions.Item>
              <Descriptions.Item label="描述" span={2}>
                <div style={{ whiteSpace: 'pre-wrap' }}>{detail.todo.description || '—'}</div>
              </Descriptions.Item>
              {imagesOf(detail.images, 'todo').length > 0 && (
                <Descriptions.Item label="图片" span={2}>
                  <TodoImageThumbs ids={imagesOf(detail.images, 'todo')} size={72} />
                </Descriptions.Item>
              )}
            </Descriptions>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 20, marginBottom: 8 }}>
              <Typography.Title level={5} style={{ margin: 0 }}>
                子任务（{detail.todo.subtask_done}/{detail.todo.subtask_total}）
              </Typography.Title>
              {canFollow(detail.todo) && detail.todo.status !== 'closed' && (
                <Button size="small" icon={<PlusOutlined />} onClick={() => void openModal('subtask_add', detail.todo)}>
                  新增子任务
                </Button>
              )}
            </div>
            <Table<Subtask>
              rowKey="id"
              size="small"
              bordered
              pagination={false}
              dataSource={detail.subtasks}
              columns={subColumns(detail.todo)}
              locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无子任务" /> }}
            />
            <Typography.Title level={5} style={{ marginTop: 20 }}>跟进记录</Typography.Title>
            {detail.logs.length ? (
              <Timeline
                items={detail.logs.map((l) => ({
                  color: l.action === 'close' ? 'green' : l.action === 'handover' ? 'orange' : l.subtask_id ? 'gray' : 'blue',
                  children: (
                    <div>
                      <div>
                        {l.subtask_id && <Tag style={{ marginRight: 6 }}>子任务</Tag>}
                        <b>{ACTION_LABEL[l.action] || l.action}</b>
                        <span style={{ color: '#999', marginLeft: 8 }}>{userName(l.operator)} · {fmt(l.created_at)}</span>
                        {l.from_status && l.from_status !== l.to_status && (
                          <span style={{ color: '#999', marginLeft: 8 }}>
                            {STATUSES[l.from_status]?.label || l.from_status} → {STATUSES[l.to_status]?.label || l.to_status}
                          </span>
                        )}
                      </div>
                      {l.content && <div style={{ whiteSpace: 'pre-wrap', marginTop: 4 }}>{l.content}</div>}
                      {imagesOf(detail.images, 'log', l.id).length > 0 && (
                        <div style={{ marginTop: 6 }}>
                          <TodoImageThumbs ids={imagesOf(detail.images, 'log', l.id)} size={56} />
                        </div>
                      )}
                    </div>
                  ),
                }))}
              />
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无跟进记录" />
            )}
          </>
        )}
      </Drawer>
    </div>
  )
}
