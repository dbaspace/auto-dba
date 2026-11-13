import React, { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Button, Card, Input, message } from 'antd'
import api from '../api/request'
import loginBg from '../assets/login-bg.jpg'
import BrandMark from '../components/BrandMark'

export default function Register() {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [passwordConfirm, setPasswordConfirm] = useState('')
  const [loading, setLoading] = useState(false)

  async function onSubmit() {
    const name = username.trim()
    if (!name) {
      message.warning('请填写用户名')
      return
    }
    if (password.length < 6) {
      message.warning('密码至少 6 位')
      return
    }
    if (password !== passwordConfirm) {
      message.warning('两次输入的密码不一致')
      return
    }
    setLoading(true)
    try {
      const { data } = await api.post(
        '/auth/register',
        {
          username: name,
          display_name: displayName.trim(),
          password,
          password_confirm: passwordConfirm,
        },
        { silent: true },
      )
      message.success(data?.message || '注册已提交，请等待管理员审批')
      navigate('/login', { replace: true })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '注册失败')
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
          <h2 style={{ margin: 0, textAlign: 'center' }}>注册账号</h2>
        </div>
        <p style={{ color: '#909399', fontSize: 13, textAlign: 'center', marginBottom: 16 }}>
          提交后需管理员审批。通过后平台角色为开发者，并加入默认租户。
        </p>
        <Input
          style={{ marginBottom: 12 }}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="用户名"
        />
        <Input
          style={{ marginBottom: 12 }}
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder="显示名（可空）"
        />
        <Input.Password
          style={{ marginBottom: 12 }}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="密码，至少 6 位"
        />
        <Input.Password
          style={{ marginBottom: 12 }}
          value={passwordConfirm}
          onChange={(e) => setPasswordConfirm(e.target.value)}
          placeholder="确认密码"
          onPressEnter={onSubmit}
        />
        <Button type="primary" block loading={loading} onClick={onSubmit}>
          注册
        </Button>
        <p style={{ textAlign: 'center', marginTop: 16 }}>
          <Link to="/login">返回登录</Link>
        </p>
      </Card>
    </div>
  )
}
