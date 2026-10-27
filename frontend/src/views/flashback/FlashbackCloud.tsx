import React from 'react'
import { Alert, Button } from 'antd'
import { Link } from 'react-router-dom'

export default function FlashbackCloud() {
  return (
    <div style={{ padding: 16 }}>
      <h1>数据闪回 · 多云密钥</h1>
      <Alert
        type="info"
        showIcon
        message="全局 AK/SK 已停用"
        description="闪回下载 WAL 改为使用实例上绑定的云账号只读密钥。同一云厂商可以有多套账号，并且只对已绑定的平台用户生效。变更密钥不能用于闪回。"
      />
      <p style={{ marginTop: 16 }}>
        地域、下载限速等非密钥参数仍保留在全局参数中。密钥请到云账号页登记并绑定用户，再在实例管理里选择云账号。
      </p>
      <Link to="/config/cloud-accounts"><Button type="primary">前往云账号</Button></Link>
    </div>
  )
}
