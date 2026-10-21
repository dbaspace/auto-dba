import React from 'react'
import { Alert, Card, Typography } from 'antd'
import CopilotPanel from '../../components/copilot/CopilotPanel'
import { Perm, usePermission } from '../../hooks/usePermission'

export default function AiAssistant() {
  const { hasUrn } = usePermission()
  const allowed = hasUrn(Perm.copilot.chat)

  return (
    <div style={{ padding: 16, display: 'flex', flexDirection: 'column', height: 'calc(100vh - 64px)' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, marginBottom: 12 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>AI助手</Typography.Title>
        <Typography.Text type="secondary">默认优先知识库直答，可强制调用大模型</Typography.Text>
      </div>
      {!allowed ? (
        <Alert type="warning" showIcon message="无 Copilot 对话权限" description="需要操作权限「Copilot 对话」（um.action.copilot.chat）。" />
      ) : (
        <Card
          size="small"
          styles={{ body: { height: '100%', display: 'flex', flexDirection: 'column', minHeight: 0 } }}
          style={{ flex: 1, minHeight: 0 }}
        >
          <CopilotPanel active layout="page" />
        </Card>
      )}
    </div>
  )
}
