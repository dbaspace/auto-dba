import React, { useEffect, useState } from 'react'
import { Alert, Button, Card, Empty, Space, Tooltip, message } from 'antd'
import api from '../api/request'
import { useAppSelector } from '../store'
import { collectPaths } from '../constants/routes'

type PageActions = {
  page: { id: string; title: string; path: string; urn?: string; permission_desc?: string }
  actions: { id: string; title: string; urn: string; granted: boolean }[]
}

/** 菜单管理里登记了页面、但前端尚未接入组件时的占位：展示页面标题、已登记的操作和空数据表。 */
export default function MenuPagePlaceholder({ pathname }: { pathname: string }) {
  const menus = useAppSelector((s) => s.auth.menus)
  const registered = collectPaths(menus).includes(pathname)
  const [data, setData] = useState<PageActions | null>(null)
  const [loading, setLoading] = useState(registered)

  useEffect(() => {
    if (!registered) return
    let cancelled = false
    setLoading(true)
    api
      .get('/menus/page-actions', { params: { path: pathname }, silent: true })
      .then((res) => {
        if (!cancelled) setData(res.data?.data || null)
      })
      .catch(() => {
        if (!cancelled) setData(null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [pathname, registered])

  if (!registered) {
    return <div style={{ padding: 24, color: '#999' }}>页面不存在</div>
  }

  const title = data?.page.title || pathname
  const actions = data?.actions || []
  return (
    <div style={{ padding: 16 }}>
      <Card
        title={title}
        loading={loading}
        extra={
          <Space>
            {actions.map((a) => (
              <Tooltip key={a.id} title={a.granted ? `${a.urn}（功能尚未接入）` : `无「${a.title}」权限`}>
                <Button disabled={!a.granted} onClick={() => message.info(`「${a.title}」功能尚未接入`)}>
                  {a.title}
                </Button>
              </Tooltip>
            ))}
          </Space>
        }
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="该页面已在「菜单管理」登记，功能尚未接入，暂无数据"
          description={`页面路径 ${pathname}${actions.length ? `；已登记 ${actions.length} 个页面操作` : '；尚未登记页面操作'}`}
        />
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无数据" />
      </Card>
    </div>
  )
}
