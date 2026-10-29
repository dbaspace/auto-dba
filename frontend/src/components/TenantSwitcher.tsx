import React, { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Modal, Select, Tag, message } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
import api, { abortAuthReissue, beginAuthReissue, finishAuthReissue, getAuthEpoch } from '../api/request'
import { Perm, usePermission } from '../hooks/usePermission'
import { useAppDispatch, useAppSelector } from '../store'
import { setUser } from '../store/authSlice'
import { HOME_PATH, resetTabs } from '../store/tabsSlice'
import { clearQueryTreeCache } from '../views/query/queryTreeCache'

type Tenant = { id: string; code: string; name: string; status?: string }

export default function TenantSwitcher() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const user = useAppSelector((s) => s.auth.user)
  const { hasUrn } = usePermission()
  const canImpersonate = hasUrn(Perm.tenants.impersonate)
  const [switching, setSwitching] = useState(false)
  const [auditOpen, setAuditOpen] = useState(false)
  const [auditLoading, setAuditLoading] = useState(false)
  const [auditOptions, setAuditOptions] = useState<Tenant[]>([])
  const [auditTarget, setAuditTarget] = useState<string>()

  const current = (user as { current_tenant?: Tenant })?.current_tenant
  const tenants = useMemo(() => {
    const cur = (user as { current_tenant?: Tenant })?.current_tenant
    const raw = (user as { tenants?: Tenant[] })?.tenants
    const list = Array.isArray(raw) ? [...raw] : []
    // impersonate 等场景可能只有 current_tenant：补进选项，避免顶栏只显示 UUID
    if (cur?.id && !list.some((t) => t.id === cur.id)) {
      list.unshift(cur)
    }
    return list
  }, [user])

  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)

  if (tenants.length === 0 && !current && !canImpersonate) return null

  function applySession(
    epoch: number,
    data: { token?: string; user?: Record<string, unknown> },
    okMsg: string,
  ): boolean {
    if (epoch !== getAuthEpoch()) {
      abortAuthReissue()
      return false
    }
    const next = data.user || {}
    // 先落盘再 finish：窗口内仍抑制 401；finish 后再 bump 世代
    if (data.token) localStorage.setItem('token', data.token)
    localStorage.setItem('user', JSON.stringify(next))
    if (!finishAuthReissue(epoch)) return false
    clearQueryTreeCache()
    dispatch(setUser(next as any))
    dispatch(resetTabs({ menus: (next as { menus?: unknown[] }).menus || [] }))
    message.success(okMsg)
    navigate(HOME_PATH, { replace: true })
    return true
  }

  async function enterImpersonate(tenantId: string, label?: string) {
    setSwitching(true)
    const epoch = beginAuthReissue()
    try {
      const { data } = await api.post(`/tenants/${tenantId}/impersonate`)
      const name =
        label ||
        data.user?.current_tenant?.name ||
        tenants.find((t) => t.id === tenantId)?.name ||
        tenantId
      applySession(epoch, data, `已进入租户模式：${name}`)
    } catch (e: any) {
      abortAuthReissue()
      message.error(e?.response?.data?.message || '进入租户模式失败')
    } finally {
      setSwitching(false)
    }
  }

  async function exitImpersonate() {
    if (switching) return
    setSwitching(true)
    const epoch = beginAuthReissue()
    try {
      const { data } = await api.post('/tenants/exit-impersonate')
      const back = data.user?.current_tenant?.name || '成员租户'
      const okMsg = data.already_exited ? `已退出租户模式（回到 ${back}）` : `已退出租户模式，回到 ${back}`
      applySession(epoch, data, okMsg)
    } catch (e: any) {
      abortAuthReissue()
      message.error(e?.response?.data?.message || '退出租户模式失败')
    } finally {
      setSwitching(false)
    }
  }

  async function openEnterAudit() {
    setAuditOpen(true)
    setAuditTarget(undefined)
    setAuditLoading(true)
    try {
      const { data } = await api.get('/tenants')
      const list = ((data.data || []) as Tenant[]).filter((t) => t.status !== 'disabled')
      setAuditOptions(list)
    } catch (e: any) {
      setAuditOptions([])
      message.error(e?.response?.data?.message || '加载租户列表失败')
    } finally {
      setAuditLoading(false)
    }
  }

  async function confirmEnterAudit() {
    if (!auditTarget) {
      message.warning('请选择工作租户')
      return
    }
    const t = auditOptions.find((x) => x.id === auditTarget)
    setAuditOpen(false)
    await enterImpersonate(auditTarget, t?.name || t?.code)
  }

  async function onChange(tenantId: string) {
    if (!tenantId || tenantId === current?.id) return

    if (impersonating) {
      const t = tenants.find((x) => x.id === tenantId)
      const label = t?.name || t?.code || tenantId
      Modal.confirm({
        title: '切换工作租户',
        content: `将工作租户切换到「${label}」？仍保持租户模式。`,
        okText: '切换',
        cancelText: '取消',
        onOk: () => enterImpersonate(tenantId, label),
      })
      return
    }

    setSwitching(true)
    const epoch = beginAuthReissue()
    try {
      const { data } = await api.post('/tenants/switch', { tenant_id: tenantId })
      applySession(epoch, data, `已切换到 ${data.user?.current_tenant?.name || tenantId}`)
    } catch (e: any) {
      abortAuthReissue()
      message.error(e?.response?.data?.message || '切换租户失败')
    } finally {
      setSwitching(false)
    }
  }

  function tenantLabel(t: Tenant | undefined | null) {
    if (!t) return ''
    const name = String(t.name || '').trim()
    const code = String(t.code || '').trim()
    if (name) return name
    return code || t.id
  }

  const selectOptions = (impersonating ? tenants : tenants).map((t) => ({
    value: t.id,
    label: t.name?.trim() ? t.name : t.code || t.id,
    title: t.code ? `${t.name || ''}（${t.code}）` : t.name || t.code || t.id,
  }))

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginRight: 12 }}>
      {impersonating ? (
        <Tag
          color="orange"
          closable
          closeIcon={<CloseOutlined />}
          onClose={(e) => {
            e.preventDefault()
            e.stopPropagation()
            if (!switching) void exitImpersonate()
          }}
          style={{ userSelect: 'none' }}
          title="关闭可退出租户模式"
        >
          租户模式
        </Tag>
      ) : canImpersonate ? (
        <Button type="link" size="small" style={{ paddingInline: 4 }} loading={switching} onClick={() => void openEnterAudit()}>
          进入租户模式
        </Button>
      ) : null}
      {(tenants.length > 0 || current) && (
        <Select
          style={{ minWidth: 160 }}
          loading={switching}
          value={current?.id}
          placeholder={impersonating ? '切换工作租户' : '选择租户'}
          title={current ? `编码：${current.code || '-'}` : undefined}
          options={selectOptions}
          optionLabelProp="label"
          optionRender={(opt) => {
            const t = tenants.find((x) => x.id === opt.value)
            const name = tenantLabel(t)
            const code = t?.code
            return (
              <span>
                {name}
                {code && name !== code ? (
                  <span style={{ marginLeft: 8, opacity: 0.55, fontSize: 12 }}>{code}</span>
                ) : null}
              </span>
            )
          }}
          onChange={onChange}
          size="small"
        />
      )}
      <Modal
        title="进入租户模式"
        open={auditOpen}
        onCancel={() => setAuditOpen(false)}
        onOk={() => void confirmEnterAudit()}
        okText="进入租户模式"
        confirmLoading={switching}
        destroyOnClose
      >
        <p style={{ marginBottom: 12, color: '#6b7280', fontSize: 13 }}>
          选择工作租户后进入租户模式：可查看全量资源并为其他用户/租户配置与划拨，可在顶栏关闭退出。
        </p>
        <Select
          showSearch
          optionFilterProp="label"
          style={{ width: '100%' }}
          loading={auditLoading}
          placeholder="搜索并选择租户"
          value={auditTarget}
          onChange={setAuditTarget}
          options={auditOptions.map((t) => ({
            value: t.id,
            label: `${t.name || t.code || t.id}${t.code ? `（${t.code}）` : ''}`,
          }))}
        />
      </Modal>
    </div>
  )
}
