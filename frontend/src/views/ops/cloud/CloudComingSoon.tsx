import React from 'react'
import { Link } from 'react-router-dom'
import { Button, Result } from 'antd'

const titles: Record<string, string> = {
  mysql: '云 MySQL',
  redis: '云 Redis',
  hosts: '云主机',
}

export default function CloudComingSoon({ kind }: { kind: 'mysql' | 'redis' | 'hosts' }) {
  const title = titles[kind] || '云资源'
  return (
    <div style={{ padding: 24 }}>
      <Result
        status="info"
        title={`${title}暂未支持`}
        subTitle="本期云资源管理先落地腾讯云 PostgreSQL。MySQL、Redis、主机沿用同一套云账号与菜单结构，后续按厂商接口接入，不会改菜单层级。"
        extra={
          <>
            <Link to="/ops/cloud/postgres"><Button type="primary">打开 PostgreSQL</Button></Link>
            <Link to="/config/cloud-accounts" style={{ marginLeft: 8 }}><Button>配置中心 · 云账号</Button></Link>
          </>
        }
      />
    </div>
  )
}
