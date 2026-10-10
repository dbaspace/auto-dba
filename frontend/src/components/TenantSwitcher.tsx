import React, { useMemo, useState } from 'react'
import { Select, Tag, message } from 'antd'
import api from '../api/request'
import { useAppDispatch, useAppSelector } from '../store'
import { setUser } from '../store/authSlice'
import { resetTabs } from '../store/tabsSlice'

type Tenant = { id: string; code: string; name: string }

export default function TenantSwitcher() {
  const dispatch = useAppDispatch()
  const user = useAppSelector((s) => s.auth.user)
  const [switching, setSwitching] = useState(false)

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

  if (tenants.length === 0 && !current) return null

  async function onChange(tenantId: string) {
    if (!tenantId || tenantId === current?.id) return
    setSwitching(true)
    try {
      const { data } = await api.post('/tenants/switch', { tenant_id: tenantId })
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      dispatch(setUser(data.user))
      dispatch(resetTabs({}))
      message.success(`已切换到 ${data.user?.current_tenant?.name || tenantId}`)
      location.hash = '#/'
      location.reload()
    } finally {
      setSwitching(false)
    }
  }

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginRight: 12 }}>
      {impersonating ? <Tag color="orange">审计中</Tag> : null}
      <Select
        style={{ minWidth: 160 }}
        loading={switching}
        value={current?.id}
        placeholder="选择租户"
        options={tenants.map((t) => ({
          value: t.id,
          label: t.name || t.code || t.id,
        }))}
        optionLabelProp="label"
        onChange={onChange}
        size="small"
      />
    </div>
  )
}
