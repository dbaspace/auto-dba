import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Button, Form, Input, Select, Space, Table, Tabs, Tag, message } from 'antd'
import api from '../../api/request'
import { useWorkOrderDraft } from '../../hooks/useWorkOrderDraft'
import './ResourceApply.css'

type ResourceType = 'database' | 'domain' | 'host' | 'sr_cluster' | 'db_deploy_cluster'
type ApplyIntent = 'visibility' | 'approver'

interface Candidate {
  resource_type: ResourceType
  resource_id: string
  display_name: string
  owner_tenant_id: string
  owner_tenant_name?: string
  suggested_action: 'acl' | 'grant' | 'assign' | 'approver'
  note?: string
}

const ACTION_LABEL: Record<string, string> = {
  acl: '同租 ACL',
  grant: '跨租授权',
  assign: '划拨到本租户',
  approver: '加入审核人',
}

const TYPE_LABEL: Record<string, string> = {
  database: '已登记库',
  domain: '访问域名',
  host: '主机',
  sr_cluster: 'StarRocks 集群',
  db_deploy_cluster: '部署集群',
}

function emptyForm() {
  return {
    intent: 'visibility' as ApplyIntent,
    category: 'db' as 'db' | 'host' | 'cluster',
    resource_type: 'database' as ResourceType,
    selected: [] as Candidate[],
    target_users: [] as string[],
    assigned_to: '',
    reason: '',
  }
}

export default function ResourceApply() {
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm())
  const formRef = useRef(form)
  formRef.current = form
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [candLoading, setCandLoading] = useState(false)
  const [searchQ, setSearchQ] = useState('')
  const [approvers, setApprovers] = useState<string[]>([])
  const [approverLoading, setApproverLoading] = useState(false)
  const [targetUserOpts, setTargetUserOpts] = useState<string[]>([])
  const [targetUserLoading, setTargetUserLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [antdForm] = Form.useForm()

  const isApproverIntent = form.intent === 'approver'

  function patchForm(patch: Partial<ReturnType<typeof emptyForm>>) {
    setForm((f) => ({ ...f, ...patch }))
  }

  const { restoreDraft, saveNow: saveDraft, clearOnSubmitSuccess, schedulePersist } = useWorkOrderDraft({
    type: 'resource_apply',
    label: '平台资源申请',
    path: '/ops/resource-apply',
    getValues: () => ({ ...formRef.current }),
    applyValues: (v) => {
      const intent: ApplyIntent = v?.intent === 'approver' ? 'approver' : 'visibility'
      const next = {
        ...emptyForm(),
        ...(v || {}),
        intent,
        category: intent === 'approver' ? 'db' : (v?.category || 'db'),
        resource_type: intent === 'approver' ? 'database' : (v?.resource_type || 'database'),
        selected: Array.isArray(v?.selected) ? v.selected : [],
        target_users: Array.isArray(v?.target_users) ? v.target_users.map(String) : [],
      }
      setForm(next)
      antdForm.setFieldsValue(next)
    },
    enabled: true,
  })

  useEffect(() => {
    restoreDraft()
  }, [restoreDraft])

  useEffect(() => {
    schedulePersist()
  }, [form, schedulePersist])

  const resourceTypesForCategory = useMemo((): ResourceType[] => {
    if (isApproverIntent) return ['database']
    if (form.category === 'host') return ['host']
    if (form.category === 'cluster') return ['sr_cluster', 'db_deploy_cluster']
    return ['database', 'domain']
  }, [form.category, isApproverIntent])

  useEffect(() => {
    const first = resourceTypesForCategory[0]
    if (!resourceTypesForCategory.includes(form.resource_type)) {
      patchForm({ resource_type: first })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.category, form.intent])

  async function loadCandidates(type: ResourceType, q: string, intent: ApplyIntent) {
    setCandLoading(true)
    try {
      const { data } = await api.get('/resource-apply/candidates', {
        params: { type, q: q || undefined, intent },
      })
      setCandidates(data.data || [])
    } catch (e: any) {
      message.error(e.response?.data?.message || '加载候选资源失败')
      setCandidates([])
    } finally {
      setCandLoading(false)
    }
  }

  useEffect(() => {
    void loadCandidates(form.resource_type, searchQ, form.intent)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.resource_type, form.intent])

  const hasAssign = !isApproverIntent && form.selected.some((s) => s.suggested_action === 'assign')
  const grantOwner =
    !isApproverIntent ? form.selected.find((s) => s.suggested_action === 'grant')?.owner_tenant_id || '' : ''

  async function loadApprovers() {
    setApproverLoading(true)
    try {
      const { data } = await api.get('/resource-apply/approvers', {
        params: {
          intent: form.intent,
          has_assign: hasAssign ? '1' : undefined,
          owner_tenant_id: !hasAssign && grantOwner ? grantOwner : undefined,
        },
      })
      const opts = (data.data || []).map((x: any) => String(x.value || x.label || x))
      setApprovers(opts)
      if (opts.length && !opts.includes(form.assigned_to)) {
        patchForm({ assigned_to: opts[0] })
        antdForm.setFieldValue('assigned_to', opts[0])
      }
    } catch {
      setApprovers([])
    } finally {
      setApproverLoading(false)
    }
  }

  useEffect(() => {
    void loadApprovers()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasAssign, grantOwner, form.selected.length, form.intent])

  async function loadTargetUsers() {
    if (!isApproverIntent) {
      setTargetUserOpts([])
      return
    }
    setTargetUserLoading(true)
    try {
      // 复用现网租户成员接口，避免旧后端缺少 /resource-apply/target-users 导致刷新 404
      const { data } = await api.get('/tenant-acl-options')
      const users = data.data?.users || data.users || []
      const opts = (Array.isArray(users) ? users : [])
        .map((x: any) => String(x.username || x.value || x.label || x).trim())
        .filter(Boolean)
      setTargetUserOpts(opts)
      const kept = form.target_users.filter((u) => opts.includes(u))
      if (kept.length !== form.target_users.length) {
        patchForm({ target_users: kept })
        antdForm.setFieldValue('target_users', kept)
      }
    } catch (e: any) {
      setTargetUserOpts([])
      message.error(e.response?.data?.message || '加载库审核人候选失败')
    } finally {
      setTargetUserLoading(false)
    }
  }

  useEffect(() => {
    void loadTargetUsers()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.intent])

  function switchIntent(intent: ApplyIntent) {
    if (intent === form.intent) return
    patchForm({
      intent,
      category: 'db',
      resource_type: 'database',
      selected: [],
      target_users: [],
      assigned_to: '',
    })
    antdForm.setFieldsValue({ assigned_to: undefined, target_users: [] })
    setSearchQ('')
    setCandidates([])
  }

  function toggleSelect(row: Candidate, checked: boolean) {
    setForm((f) => {
      const key = `${row.resource_type}:${row.resource_id}`
      const rest = f.selected.filter((s) => `${s.resource_type}:${s.resource_id}` !== key)
      if (!checked) {
        return { ...f, selected: rest }
      }
      if (f.intent === 'approver' && rest.length > 0) {
        const owner = rest[0].owner_tenant_id
        if (row.owner_tenant_id && owner && row.owner_tenant_id !== owner) {
          message.warning('库审核人申请所选库须归属同一租户')
          return f
        }
      }
      // 意图为审核人时强制展示建议处置
      const nextRow =
        f.intent === 'approver'
          ? { ...row, suggested_action: 'approver' as const }
          : row
      return { ...f, selected: [...rest, nextRow] }
    })
  }

  const selectedKeys = useMemo(
    () => new Set(form.selected.map((s) => `${s.resource_type}:${s.resource_id}`)),
    [form.selected],
  )

  function reset() {
    clearOnSubmitSuccess()
    const next = emptyForm()
    setForm(next)
    antdForm.setFieldsValue(next)
    setSearchQ('')
    navigate('/ops/tickets')
  }

  async function submit() {
    try {
      await antdForm.validateFields()
    } catch {
      return
    }
    if (!form.selected.length) {
      message.warning('请至少选择一项资源')
      return
    }
    if (isApproverIntent && !form.target_users.length) {
      message.warning('请至少选择一名库审核人')
      return
    }
    setSubmitting(true)
    try {
      const { data } = await api.post('/resource-apply', {
        intent: form.intent,
        items: form.selected.map((s) => ({
          resource_type: s.resource_type,
          resource_id: s.resource_id,
        })),
        target_users: isApproverIntent ? form.target_users : undefined,
        assigned_to: form.assigned_to,
        reason: form.reason,
        description: form.reason,
      })
      clearOnSubmitSuccess()
      message.success(isApproverIntent ? '已提交库审核人申请' : '已提交平台资源申请')
      navigate(`/ops/tickets/${data.data?.id || ''}`)
    } catch (e: any) {
      message.error(e.response?.data?.message || '提交失败')
    } finally {
      setSubmitting(false)
    }
  }

  const capabilityDesc = isApproverIntent
    ? '为所选库指定工单审核人：从本租户成员中多选用户，审批通过后写入该库审核人名单。本单审批人须为平台管理员或 issue_global_approver_whitelist。与引擎 GRANT 无关；私有库会对目标用户同步开通可见。'
    : '用于申请本租户对已登记库、访问域名、主机及集群的可见与使用权限。审批通过后，系统按资源归属自动开通：本租户内纳入可访问名单，跨租户建立只读授权，默认租户资源则划拨至本租户。引擎账号 GRANT 请走「权限申请」；申请成为工单审核人请切换到「库审核人」。'

  const approverPlaceholder = hasAssign
    ? '含划拨项：请选平台管理员'
    : isApproverIntent
      ? '请选平台管理员或超级审核白名单用户'
      : '选择审批人'

  return (
    <div className="ops-page resource-apply-page">
      <Alert
        className="ra-capability"
        type="info"
        showIcon
        message="平台资源申请能力说明"
        description={capabilityDesc}
      />
      <Form form={antdForm} layout="vertical" className="resource-apply-form" initialValues={emptyForm()}>
        <Tabs
          className="ra-intent-tabs"
          activeKey={form.intent}
          onChange={(k) => switchIntent(k as ApplyIntent)}
          items={[
            { key: 'visibility', label: '资源可见' },
            { key: 'approver', label: '库审核人' },
          ]}
        />
        {!isApproverIntent && (
          <Tabs
            activeKey={form.category}
            onChange={(k) => patchForm({ category: k as typeof form.category })}
            items={[
              { key: 'db', label: '数据库资源' },
              { key: 'host', label: '主机' },
              { key: 'cluster', label: '集群' },
            ]}
          />
        )}
        <div className="ra-toolbar">
          <Select
            style={{ width: 180 }}
            value={form.resource_type}
            onChange={(v) => patchForm({ resource_type: v })}
            options={resourceTypesForCategory.map((t) => ({ value: t, label: TYPE_LABEL[t] }))}
            disabled={isApproverIntent}
          />
          <Input.Search
            allowClear
            placeholder={isApproverIntent ? '搜索本租户已登记库' : '搜索名称'}
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            onSearch={(v) => void loadCandidates(form.resource_type, v, form.intent)}
            style={{ maxWidth: 320 }}
          />
          <Button onClick={() => void loadCandidates(form.resource_type, searchQ, form.intent)} loading={candLoading}>
            刷新
          </Button>
        </div>
        <Table
          size="small"
          rowKey={(r) => `${r.resource_type}:${r.resource_id}`}
          loading={candLoading}
          dataSource={candidates}
          pagination={{ pageSize: 10 }}
          rowSelection={{
            selectedRowKeys: [...selectedKeys],
            onSelect: (row, checked) => toggleSelect(row, checked),
            onSelectAll: (checked, _selected, changeRows) => {
              changeRows.forEach((row) => toggleSelect(row, checked))
            },
          }}
          columns={[
            { title: '名称', dataIndex: 'display_name' },
            { title: '类型', dataIndex: 'resource_type', width: 140, render: (v) => TYPE_LABEL[v] || v },
            { title: '归属租户', dataIndex: 'owner_tenant_name', width: 140, render: (v, r) => v || r.owner_tenant_id },
            {
              title: '建议处置',
              dataIndex: 'suggested_action',
              width: 140,
              render: (v) => <Tag>{ACTION_LABEL[v] || v}</Tag>,
            },
            { title: '说明', dataIndex: 'note', ellipsis: true },
          ]}
        />
        {form.selected.length > 0 && (
          <div className="ra-selected">
            <div className="ra-selected-title">已选 {form.selected.length} 项</div>
            <Space wrap size={[8, 8]}>
              {form.selected.map((s) => (
                <Tag
                  key={`${s.resource_type}:${s.resource_id}`}
                  closable
                  onClose={() => toggleSelect(s, false)}
                >
                  {TYPE_LABEL[s.resource_type]} · {s.display_name}（
                  {ACTION_LABEL[isApproverIntent ? 'approver' : s.suggested_action] || s.suggested_action}）
                </Tag>
              ))}
            </Space>
          </div>
        )}
        {isApproverIntent && (
          <Form.Item
            label="库审核人"
            name="target_users"
            rules={[{ required: true, type: 'array', min: 1, message: '请至少选择一名库审核人' }]}
          >
            <Select
              mode="multiple"
              showSearch
              allowClear
              loading={targetUserLoading}
              placeholder="从本租户成员中选择，将获得所选库的工单审核权限"
              value={form.target_users}
              onChange={(v) => patchForm({ target_users: v || [] })}
              options={targetUserOpts.map((a) => ({ value: a, label: a }))}
              style={{ maxWidth: 560 }}
            />
          </Form.Item>
        )}
        <Form.Item label="审批人" name="assigned_to" rules={[{ required: true, message: '请选择审批人' }]}>
          <Select
            showSearch
            allowClear
            loading={approverLoading}
            placeholder={approverPlaceholder}
            value={form.assigned_to || undefined}
            onChange={(v) => patchForm({ assigned_to: v || '' })}
            options={approvers.map((a) => ({ value: a, label: a }))}
            style={{ maxWidth: 360 }}
          />
        </Form.Item>
        <Form.Item label="申请原因" name="reason" rules={[{ required: true, message: '请填写原因' }]}>
          <Input.TextArea
            rows={3}
            placeholder={
              isApproverIntent
                ? '说明为何需要为所选用户开通库工单审核权限'
                : '说明需要开通查询/工单等资源可见的原因'
            }
            value={form.reason}
            onChange={(e) => patchForm({ reason: e.target.value })}
            style={{ maxWidth: 560 }}
          />
        </Form.Item>
        <p className="hint">
          {isApproverIntent
            ? '库审核人写入所选库 approvers；审批人仅限平台管理员与超级审核白名单。私有库会对目标用户同步开通可见。'
            : '审批通过后自动生效。手工 grant / 审计划拨 / DCL 权限申请仍可继续使用。'}
        </p>
        <Space>
          <Button onClick={reset}>取消</Button>
          <Button onClick={() => saveDraft()}>保存草稿</Button>
          <Button type="primary" loading={submitting} onClick={() => void submit()}>
            提交申请
          </Button>
        </Space>
      </Form>
    </div>
  )
}
