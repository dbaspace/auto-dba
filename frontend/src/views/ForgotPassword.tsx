import React, { useState } from 'react'
import { Link } from 'react-router-dom'
import { Button, Card, Input, message } from 'antd'
import api from '../api/request'
import loginBg from '../assets/login-bg.jpg'
import BrandMark from '../components/BrandMark'

export default function ForgotPassword() {
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [loading, setLoading] = useState(false)
  const [doneMsg, setDoneMsg] = useState('')

  async function onSubmit() {
    if (!username.trim()) {
      message.warning('请填写要重置密码的用户名')
      return
    }
    if (!email.trim() || !email.includes('@')) {
      message.warning('请填写有效的接收邮箱')
      return
    }
    setLoading(true)
    setDoneMsg('')
    try {
      const { data } = await api.post(
        '/auth/password/forgot',
        { username: username.trim(), email: email.trim() },
        { silent: true },
      )
      const msg = data?.message || '重置邮件已发送'
      setDoneMsg(msg)
      message.success(msg)
    } catch (e: any) {
      setDoneMsg('')
      message.error(e?.response?.data?.message || e?.message || '提交失败')
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
          <h2 style={{ margin: 0, textAlign: 'center' }}>忘记密码</h2>
        </div>
        <p style={{ color: '#909399', fontSize: 13, textAlign: 'center', marginBottom: 16 }}>
          填写要重置的平台/LDAP 用户名，以及接收重置链接的邮箱。须管理员已启用 SMTP。SSO 请前往统一认证中心。
        </p>
        <Input
          style={{ marginBottom: 12 }}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="要重置的用户名"
        />
        <Input
          style={{ marginBottom: 12 }}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="接收邮箱"
          onPressEnter={onSubmit}
        />
        <Button type="primary" block loading={loading} onClick={onSubmit}>
          发送重置邮件
        </Button>
        {doneMsg ? (
          <p style={{ color: '#67c23a', fontSize: 13, marginTop: 12, textAlign: 'center' }}>{doneMsg}</p>
        ) : null}
        <p style={{ textAlign: 'center', marginTop: 16 }}>
          <Link to="/login">返回登录</Link>
        </p>
      </Card>
    </div>
  )
}
