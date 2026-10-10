import React, { useEffect, useState } from 'react'
import { Button, Card, List, Typography, message } from 'antd'
import { useNavigate } from 'react-router-dom'
import api from '../api/request'
import { useAppDispatch } from '../store'
import { setUser } from '../store/authSlice'

type Tenant = { id: string; code: string; name: string; status: string }

export default function SelectTenant() {
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const [tenants, setTenants] = useState<Tenant[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const cached = localStorage.getItem('pending_tenants')
    if (cached) {
      try {
        setTenants(JSON.parse(cached) || [])
      } catch {
        /* ignore */
      }
    }
    api
      .get('/tenants/mine', { silent: true })
      .then(({ data }) => setTenants(data.data || []))
      .catch(() => {})
  }, [])

  async function choose(t: Tenant) {
    setLoading(true)
    try {
      const { data } = await api.post('/auth/select-tenant', { tenant_id: t.id })
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      localStorage.removeItem('pending_tenants')
      dispatch(setUser(data.user))
      message.success(`已进入 ${t.name}`)
      navigate('/', { replace: true })
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: '#f5f5f5' }}>
      <Card title="选择租户" style={{ width: 420 }}>
        <Typography.Paragraph type="secondary">你属于多个租户，请选择要进入的工作空间。</Typography.Paragraph>
        <List
          dataSource={tenants}
          locale={{ emptyText: '暂无可用租户，请联系管理员' }}
          renderItem={(t) => (
            <List.Item
              actions={[
                <Button key="go" type="primary" loading={loading} onClick={() => choose(t)}>
                  进入
                </Button>,
              ]}
            >
              <List.Item.Meta title={t.name} description={t.code} />
            </List.Item>
          )}
        />
      </Card>
    </div>
  )
}
