import React from 'react'
import { Button, Result } from 'antd'
import { useNavigate } from 'react-router-dom'

export default function Forbidden() {
  const navigate = useNavigate()
  return (
    <div style={{ display: 'grid', placeItems: 'center', height: '100vh' }}>
      <Result
        status="403"
        title="403"
        subTitle="无权访问该页面"
        extra={
          <Button type="primary" onClick={() => navigate('/')}>
            返回首页
          </Button>
        }
      />
    </div>
  )
}
