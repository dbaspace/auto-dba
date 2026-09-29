import React from 'react'
import { Button, Space, Tag } from 'antd'
import { Link } from 'react-router-dom'
import HaArchitectureDiagram, { ArchController, ArchMember } from './HaArchitectureDiagram'

export type HaArchitectureCardProps = {
  mode?: string
  vip?: string
  status?: string
  haClusterId?: string
  controllerName?: string
  standbyCount?: number
  controllers?: ArchController[]
  members?: ArchMember[]
}

/** 集群部署侧只读 HA 架构摘要卡片 */
export default function HaArchitectureCard({
  mode,
  vip,
  status,
  haClusterId,
  controllerName,
  standbyCount = 0,
  controllers,
  members,
}: HaArchitectureCardProps) {
  const bound = !!haClusterId
  const pending = status === 'ha_pending'
  const href = haClusterId ? `/ops/ha?cluster=${encodeURIComponent(haClusterId)}` : '/ops/ha'

  const ctrls: ArchController[] = controllers?.length
    ? controllers
    : controllerName
      ? [{ id: 'primary', name: controllerName, kind: mode || 'ctrl', role: 'primary' }]
      : []

  return (
    <div className="ha-arch-card-wrap" style={{ marginTop: 12 }}>
      <Space wrap style={{ marginBottom: 8 }}>
        <Tag color={bound ? 'success' : pending ? 'warning' : 'default'}>
          HA {mode || ''}{bound ? ' · 已绑定' : pending ? ' · 待接入' : ''}
        </Tag>
        {controllerName && <Tag>主控 {controllerName}</Tag>}
        {standbyCount > 0 && <Tag>备控 ×{standbyCount}</Tag>}
        <Link to={href}>
          <Button type="link" size="small" style={{ padding: 0 }}>
            查看完整架构
          </Button>
        </Link>
      </Space>
      <HaArchitectureDiagram
        mode={mode || 'manual'}
        vip={vip}
        controllers={ctrls}
        members={members || []}
        variant="card"
        emptyHint="接入 HA 后显示架构摘要"
      />
    </div>
  )
}
