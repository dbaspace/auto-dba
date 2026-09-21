import React, { useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { Button, Card, Input, message } from 'antd'
import api from '../api/request'
import loginBg from '../assets/login-bg.jpg'
import BrandMark from '../components/BrandMark'

export default function ResetPassword() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const token = useMemo(() => params.get('token') || '', [params])
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit() {
    if (!token) {
      message.error('重置链接无效')
      return
    }
    if (password.length < 6) {
      message.warning('新密码至少 6 位')
      return
    }
    if (password !== confirm) {
      message.warning('两次输入的密码不一致')
      return
    }
    setLoading(true)
    try {
      const { data } = await api.post(
        '/auth/password/reset',
        {
          token,
          new_password: password,
        },
        { silent: true },
      )
      message.success(data?.message || '密码已重置')
      navigate('/login', { replace: true })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '重置失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        padding: 24,
        background: `linear-gradient(180deg, rgba(12,24,48,0.38), rgba(12,24,48,0.52)), url(${loginBg}) center / cover no-repeat`,
      }}
    >
      <Card style={{ width: 'min(400px, 100%)', borderRadius: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <BrandMark size={56} />
          <h2 style={{ margin: 0, textAlign: 'center' }}>重置密码</h2>
        </div>
        {!token ? (
          <p style={{ color: '#c0352b', fontSize: 13, textAlign: 'center' }}>
            缺少重置令牌，请从邮件中的链接打开
          </p>
        ) : (
          <>
            <Input.Password
              style={{ marginBottom: 12 }}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="新密码（至少 6 位）"
            />
            <Input.Password
              style={{ marginBottom: 12 }}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder="确认新密码"
              onPressEnter={onSubmit}
            />
            <Button type="primary" block loading={loading} onClick={onSubmit}>
              确认重置
            </Button>
          </>
        )}
        <p style={{ textAlign: 'center', marginTop: 16 }}>
          <Link to="/login">返回登录</Link>
        </p>
      </Card>
    </div>
  )
}
