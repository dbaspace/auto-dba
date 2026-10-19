import React, { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Collapse, Divider, Form, Input, InputNumber, Radio, Select, Space, Spin, Switch, Tabs, Tag, message } from 'antd'
import api from '../../api/request'
import './Ldap.css'

interface LdapForm {
  enabled: boolean
  ldap_url: string
  bind_dn: string
  bind_password: string
  base_dn: string
  user_filter: string
  display_name_attr: string
  test_username: string
  test_password: string
}

interface SsoForm {
  enabled: boolean
  grant_mode: 'authorization_code' | 'password'
  base_url: string
  callback_url: string
  authorize_path: string
  token_path: string
  userinfo_path: string
  jwks_uri: string
  client_id: string
  client_secret: string
  scope: string
  username_claim: string
  display_name_claim: string
  email_claim: string
  test_username: string
  test_password: string
}

interface SmtpForm {
  enabled: boolean
  host: string
  port: number
  username: string
  password: string
  from: string
  use_tls: boolean
  auth_way: string
  rate: number
  frontend_base_url: string
  test_to: string
}

const DEFAULT_LDAP: LdapForm = {
  enabled: false,
  ldap_url: '',
  bind_dn: '',
  bind_password: '',
  base_dn: '',
  user_filter: '(uid=%s)',
  display_name_attr: 'displayName',
  test_username: '',
  test_password: '',
}

const DEFAULT_SSO: SsoForm = {
  enabled: false,
  grant_mode: 'authorization_code',
  base_url: '',
  callback_url: '',
  authorize_path: '/oauth2/authorize',
  token_path: '/oauth2/token',
  userinfo_path: '/oauth2/userinfo',
  jwks_uri: '',
  client_id: '',
  client_secret: '',
  scope: '',
  username_claim: 'nickname',
  display_name_claim: 'name',
  email_claim: 'email',
  test_username: '',
  test_password: '',
}

const DEFAULT_SMTP: SmtpForm = {
  enabled: false,
  host: '',
  port: 465,
  username: '',
  password: '',
  from: '',
  use_tls: true,
  auth_way: 'PLAIN',
  rate: 2,
  frontend_base_url: 'http://127.0.0.1:5174',
  test_to: '',
}

function composeLdapURL(host: string, port: number, useTLS: boolean): string {
  let h = (host || '').trim()
  if (!h) return ''
  const lower = h.toLowerCase()
  if (lower.startsWith('ldap://') || lower.startsWith('ldaps://')) {
    return h
  }
  const scheme = useTLS ? 'ldaps' : 'ldap'
  const p = port > 0 ? port : useTLS ? 636 : 389
  return `${scheme}://${h}:${p}`
}

function parseLdapURL(raw: string): { host: string; port: number; use_tls: boolean } {
  let s = (raw || '').trim()
  if (!s) {
    return { host: '', port: 389, use_tls: false }
  }
  let useTLS = false
  const lower = s.toLowerCase()
  if (lower.startsWith('ldaps://')) {
    useTLS = true
    s = s.slice('ldaps://'.length)
  } else if (lower.startsWith('ldap://')) {
    s = s.slice('ldap://'.length)
  }
  const slash = s.indexOf('/')
  if (slash >= 0) s = s.slice(0, slash)
  let host = s
  let port = useTLS ? 636 : 389
  const colon = s.lastIndexOf(':')
  if (colon > 0 && !s.includes(']')) {
    const p = parseInt(s.slice(colon + 1), 10)
    if (p > 0) {
      port = p
      host = s.slice(0, colon)
    }
  } else if (s.startsWith('[') && s.includes(']:')) {
    const end = s.indexOf(']:')
    host = s.slice(1, end)
    const p = parseInt(s.slice(end + 2), 10)
    if (p > 0) port = p
  }
  return { host, port, use_tls: useTLS }
}

function LdapPanel() {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [bindSet, setBindSet] = useState(false)
  const [form] = Form.useForm<LdapForm>()

  const pwdPlaceholder = useMemo(
    () => (bindSet ? '已保存则留空不修改' : '请输入绑定密码'),
    [bindSet],
  )

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/ldap')
      const d = data.data || {}
      form.setFieldsValue({
        enabled: !!d.enabled,
        ldap_url: composeLdapURL(d.host || '', d.port > 0 ? d.port : 389, !!d.use_tls),
        bind_dn: d.bind_dn || '',
        bind_password: '',
        base_dn: d.base_dn || '',
        user_filter: d.user_filter || '(uid=%s)',
        display_name_attr: d.display_name_attr || 'displayName',
        test_username: '',
        test_password: '',
      })
      setBindSet(!!d.bind_password_set)
    } finally {
      setLoading(false)
    }
  }

  function payload() {
    const f = form.getFieldsValue()
    const parsed = parseLdapURL(f.ldap_url || '')
    return {
      enabled: f.enabled,
      host: parsed.host,
      port: parsed.port,
      use_tls: parsed.use_tls,
      base_dn: (f.base_dn || '').trim(),
      bind_dn: (f.bind_dn || '').trim(),
      bind_password: f.bind_password || undefined,
      user_filter: (f.user_filter || '').trim() || '(uid=%s)',
      display_name_attr: (f.display_name_attr || '').trim(),
      test_username: (f.test_username || '').trim(),
      test_password: f.test_password,
    }
  }

  async function save() {
    try {
      await form.validateFields(['ldap_url', 'user_filter'])
    } catch {
      return
    }
    const parsed = parseLdapURL(form.getFieldValue('ldap_url') || '')
    if (!parsed.host) {
      message.warning('请填写 LDAP 地址')
      return
    }
    setSaving(true)
    try {
      await api.put('/settings/ldap', payload())
      message.success('已保存')
      form.setFieldsValue({ bind_password: '' })
      await load()
    } finally {
      setSaving(false)
    }
  }

  async function test() {
    try {
      await form.validateFields(['ldap_url'])
    } catch {
      return
    }
    setSaving(true)
    try {
      const { data } = await api.post('/settings/ldap/test', payload())
      message.success(data.message || '连接成功')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Spin spinning={loading}>
      <Form
        form={form}
        className="ldap-form"
        labelCol={{ flex: '140px' }}
        wrapperCol={{ flex: 1 }}
        labelAlign="right"
        colon={false}
        initialValues={DEFAULT_LDAP}
      >
        <div className="ldap-section-title">LDAP 服务器</div>
        <Form.Item label="启用 LDAP" name="enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item
          label="LDAP 地址"
          name="ldap_url"
          required
          rules={[{ required: true, message: '请填写 LDAP 地址' }]}
          extra={<span className="field-hint">如: ldap://localhost:389</span>}
        >
          <Input placeholder="ldap://ldap.example.com:389" />
        </Form.Item>
        <Form.Item label="绑定 DN" name="bind_dn">
          <Input placeholder="cn=admin,ou=users,dc=example,dc=com" />
        </Form.Item>
        <Form.Item label="密码" name="bind_password">
          <Input.Password placeholder={pwdPlaceholder} autoComplete="off" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">LDAP 用户</div>
        <Form.Item
          label="用户 OU"
          name="base_dn"
          extra={<span className="field-hint">多个 OU 使用 | 分割</span>}
        >
          <Input placeholder="dc=example,dc=com" />
        </Form.Item>
        <Form.Item
          label="用户过滤器"
          name="user_filter"
          required
          rules={[{ required: true, message: '请填写用户过滤器' }]}
          extra={<span className="field-hint">如 (uid=%s) 或 (cn=%s)</span>}
        >
          <Input placeholder="(uid=%s)" />
        </Form.Item>
        <Form.Item
          label="显示名属性"
          name="display_name_attr"
          extra={<span className="field-hint">中文名一般用 displayName；若与登录名相同会自动尝试其它属性</span>}
        >
          <Input placeholder="displayName" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">连接测试（可选）</div>
        <Form.Item label="测试账号" name="test_username">
          <Input placeholder="可选" />
        </Form.Item>
        <Form.Item label="测试密码" name="test_password">
          <Input.Password autoComplete="off" placeholder="可选" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-actions">
          <Space size="middle">
            <Button type="primary" loading={saving} onClick={save}>
              保存
            </Button>
            <Button loading={saving} onClick={test}>
              测试连接
            </Button>
            <Button onClick={load} disabled={loading || saving}>
              重置
            </Button>
          </Space>
        </div>
      </Form>
    </Spin>
  )
}

function SsoPanel() {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [secretSet, setSecretSet] = useState(false)
  const [form] = Form.useForm<SsoForm>()

  const secretPlaceholder = useMemo(
    () => (secretSet ? '已保存则留空不修改' : '请输入 Client Secret'),
    [secretSet],
  )

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/sso')
      const d = data.data || {}
      form.setFieldsValue({
        enabled: !!d.enabled,
        grant_mode: d.grant_mode === 'password' ? 'password' : 'authorization_code',
        base_url: d.base_url || '',
        callback_url: d.callback_url || '',
        authorize_path: d.authorize_path || '/oauth2/authorize',
        token_path: d.token_path || '/oauth2/token',
        userinfo_path: d.userinfo_path || '/oauth2/userinfo',
        jwks_uri: d.jwks_uri || '',
        client_id: d.client_id || '',
        client_secret: '',
        scope: d.scope || '',
        username_claim: d.username_claim || 'nickname',
        display_name_claim: d.display_name_claim || 'name',
        email_claim: d.email_claim || 'email',
        test_username: '',
        test_password: '',
      })
      setSecretSet(!!d.client_secret_set)
    } finally {
      setLoading(false)
    }
  }

  function payload() {
    const f = form.getFieldsValue()
    return {
      enabled: f.enabled,
      grant_mode: f.grant_mode === 'password' ? 'password' : 'authorization_code',
      base_url: (f.base_url || '').trim(),
      callback_url: (f.callback_url || '').trim(),
      authorize_path: (f.authorize_path || '').trim() || '/oauth2/authorize',
      token_path: (f.token_path || '').trim() || '/oauth2/token',
      userinfo_path: (f.userinfo_path || '').trim() || '/oauth2/userinfo',
      jwks_uri: (f.jwks_uri || '').trim(),
      client_id: (f.client_id || '').trim(),
      client_secret: f.client_secret || undefined,
      scope: (f.scope || '').trim(),
      username_claim: (f.username_claim || '').trim() || 'nickname',
      display_name_claim: (f.display_name_claim || '').trim() || 'name',
      email_claim: (f.email_claim || '').trim() || 'email',
      test_username: (f.test_username || '').trim(),
      test_password: f.test_password,
    }
  }

  async function save() {
    try {
      await form.validateFields(['base_url', 'client_id'])
    } catch {
      return
    }
    setSaving(true)
    try {
      await api.put('/settings/sso', payload())
      message.success('已保存')
      form.setFieldsValue({ client_secret: '' })
      await load()
    } finally {
      setSaving(false)
    }
  }

  async function test() {
    try {
      await form.validateFields(['base_url', 'client_id'])
    } catch {
      return
    }
    setSaving(true)
    try {
      const { data } = await api.post('/settings/sso/test', payload())
      message.success(data.message || '连接成功')
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  return (
    <Spin spinning={loading}>
      <Form
        form={form}
        className="ldap-form"
        labelCol={{ flex: '140px' }}
        wrapperCol={{ flex: 1 }}
        labelAlign="right"
        colon={false}
        initialValues={DEFAULT_SSO}
      >
        <div className="ldap-section-title">SSO 服务器</div>
        <Form.Item
          label="启用 SSO"
          name="enabled"
          valuePropName="checked"
          extra={<span className="field-hint">启用后，登录页「认证方式」可选 SSO</span>}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          label="授权模式"
          name="grant_mode"
          extra={
            <span className="field-hint">
              授权码：登录页选 SSO 后点登录跳转 IdP。密码：登录页选 SSO 后用账号密码换 token（需 IdP 支持
              grant_type=password）。
            </span>
          }
        >
          <Radio.Group
            options={[
              { value: 'authorization_code', label: '授权码' },
              { value: 'password', label: '密码' },
            ]}
          />
        </Form.Item>
        <Form.Item
          label="SSO 地址"
          name="base_url"
          required
          rules={[{ required: true, message: '请填写 SSO 地址' }]}
          extra={<span className="field-hint">IdP 服务根地址，如 https://sso.example.com（用于拼 Authorize / Token / UserInfo）</span>}
        >
          <Input placeholder="https://sso.example.com" />
        </Form.Item>
        <Form.Item
          label="SSO 回调地址"
          name="callback_url"
          dependencies={['grant_mode']}
          rules={[
            ({ getFieldValue }) => ({
              validator(_, value) {
                if (getFieldValue('grant_mode') === 'authorization_code' && !String(value || '').trim()) {
                  return Promise.reject(new Error('授权码模式下请填写 SSO 回调地址（须与 IdP 白名单逐字相同）'))
                }
                return Promise.resolve()
              },
            }),
          ]}
          extra={
            <span className="field-hint">
              授权码模式必填，须与 IdP 白名单逐字相同。注意：IdP 通常不把 localhost 与 127.0.0.1
              视为等价。本地 Saturn 应用请用 http://127.0.0.1:5174/auth/callback（不要用
              localhost）；若 IdP 已登记后端则填 http://127.0.0.1:8800/api/v1/auth/sso/callback
            </span>
          }
        >
          <Input placeholder="http://127.0.0.1:5174/auth/callback" />
        </Form.Item>
        <Form.Item
          label="Authorize 路径"
          name="authorize_path"
          extra={<span className="field-hint">授权码模式使用；相对 SSO 地址或完整 URL</span>}
        >
          <Input placeholder="/oauth2/authorize" />
        </Form.Item>
        <Form.Item
          label="Token 路径"
          name="token_path"
          extra={<span className="field-hint">相对 SSO 地址，或填完整 URL</span>}
        >
          <Input placeholder="/oauth2/token" />
        </Form.Item>
        <Form.Item
          label="UserInfo 路径"
          name="userinfo_path"
          extra={<span className="field-hint">相对 SSO 地址，或填完整 URL</span>}
        >
          <Input placeholder="/oauth2/userinfo" />
        </Form.Item>
        <Form.Item
          label="JWKS URI"
          name="jwks_uri"
          extra={
            <span className="field-hint">
              可选。填写后优先用 id_token（JWKS 校验）取用户信息；相对 SSO 地址或完整 URL，如 /oauth2/jwks / .well-known/jwks.json
            </span>
          }
        >
          <Input placeholder="/oauth2/jwks" />
        </Form.Item>
        <Form.Item
          label="Client ID"
          name="client_id"
          required
          rules={[{ required: true, message: '请填写 Client ID' }]}
        >
          <Input placeholder="应用 Client ID" />
        </Form.Item>
        <Form.Item label="Client Secret" name="client_secret">
          <Input.Password placeholder={secretPlaceholder} autoComplete="off" />
        </Form.Item>
        <Form.Item label="Scope" name="scope">
          <Input placeholder="可选，如 openid profile" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">用户字段映射</div>
        <Form.Item
          label="用户名 Claim"
          name="username_claim"
          extra={<span className="field-hint">本地登录名映射字段，推荐 nickname；不要用 sub（数字 ID）</span>}
        >
          <Input placeholder="nickname" />
        </Form.Item>
        <Form.Item label="显示名 Claim" name="display_name_claim">
          <Input placeholder="name" />
        </Form.Item>
        <Form.Item
          label="邮箱 Claim"
          name="email_claim"
          extra={<span className="field-hint">用于用户邮箱与工单通知等，推荐 email</span>}
        >
          <Input placeholder="email" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-section-title">连接测试（可选）</div>
        <Form.Item
          label="测试账号"
          name="test_username"
          extra={
            <span className="field-hint">
              仅密码模式可测账号。授权码模式请留空只测连通，完整登录到登录页选择 SSO。
            </span>
          }
        >
          <Input placeholder="可选" />
        </Form.Item>
        <Form.Item label="测试密码" name="test_password">
          <Input.Password autoComplete="off" placeholder="可选" />
        </Form.Item>
        <Divider className="ldap-divider" />
        <div className="ldap-actions">
          <Space size="middle">
            <Button type="primary" loading={saving} onClick={save}>
              保存
            </Button>
            <Button loading={saving} onClick={test}>
              测试连接
            </Button>
            <Button onClick={load} disabled={loading || saving}>
              重置
            </Button>
          </Space>
        </div>
      </Form>
    </Spin>
  )
}

function SmtpPanel() {
  const [form] = Form.useForm<SmtpForm>()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [passwordSet, setPasswordSet] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/smtp')
      const d = data?.data || {}
      setPasswordSet(!!d.password_set)
      form.setFieldsValue({
        ...DEFAULT_SMTP,
        enabled: !!d.enabled,
        host: d.host || '',
        port: d.port || 465,
        username: d.username || '',
        password: '',
        from: d.from || '',
        use_tls: d.use_tls !== false,
        auth_way: d.auth_way || 'PLAIN',
        rate: Number(d.rate) > 0 ? Number(d.rate) : 2,
        frontend_base_url: d.frontend_base_url || 'http://127.0.0.1:5174',
        test_to: '',
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function payload() {
    const f = form.getFieldsValue()
    return {
      enabled: !!f.enabled,
      host: (f.host || '').trim(),
      port: Number(f.port) || 465,
      username: (f.username || '').trim(),
      password: (f.password || '').trim(),
      from: (f.from || '').trim(),
      use_tls: !!f.use_tls,
      auth_way: f.auth_way || 'PLAIN',
      rate: Number(f.rate) > 0 ? Number(f.rate) : 2,
      frontend_base_url: (f.frontend_base_url || '').trim(),
    }
  }

  async function onSave() {
    setSaving(true)
    try {
      await api.put('/settings/smtp', payload())
      message.success('已保存')
      form.setFieldValue('password', '')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function onTest() {
    setTesting(true)
    try {
      const f = form.getFieldsValue()
      const { data } = await api.post('/settings/smtp/test', {
        ...payload(),
        test_to: (f.test_to || '').trim(),
      })
      message.success(data?.message || '测试邮件已发送')
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '测试失败')
    } finally {
      setTesting(false)
    }
  }

  return (
    <Spin spinning={loading}>
      <Form form={form} layout="vertical" initialValues={DEFAULT_SMTP} style={{ maxWidth: 640 }}>
        <Form.Item
          label="启用 SMTP"
          name="enabled"
          valuePropName="checked"
          extra={<span className="field-hint">忘记密码发信前必须开启；仅「发送测试邮件」可不依赖此开关</span>}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          label="SMTP 服务器"
          name="host"
          extra={<span className="field-hint">如 smtp.example.com</span>}
        >
          <Input placeholder="smtp.example.com" />
        </Form.Item>
        <Form.Item label="端口" name="port">
          <Input type="number" placeholder="465" />
        </Form.Item>
        <Form.Item label="启用 TLS" name="use_tls" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item label="认证方式" name="auth_way" extra={<span className="field-hint">PLAIN、LOGIN、CRAM-MD5，默认 PLAIN</span>}>
          <Select
            options={[
              { value: 'PLAIN', label: 'PLAIN' },
              { value: 'LOGIN', label: 'LOGIN' },
              { value: 'CRAM-MD5', label: 'CRAM-MD5' },
            ]}
          />
        </Form.Item>
        <Form.Item label="每秒封数" name="rate" extra={<span className="field-hint">发信限速，默认每秒 2 封</span>}>
          <Input type="number" min={1} placeholder="2" />
        </Form.Item>
        <Form.Item label="账号" name="username">
          <Input placeholder="发信账号 / 邮箱" />
        </Form.Item>
        <Form.Item
          label="密码"
          name="password"
          extra={
            <span className="field-hint">
              {passwordSet ? '已保存，留空表示不修改' : 'SMTP 认证密码或授权码'}
            </span>
          }
        >
          <Input.Password placeholder={passwordSet ? '********' : '请输入'} />
        </Form.Item>
        <Form.Item
          label="发件人"
          name="from"
          extra={<span className="field-hint">From 地址，须为合法邮箱</span>}
        >
          <Input placeholder="noreply@example.com" />
        </Form.Item>
        <Form.Item
          label="前端地址"
          name="frontend_base_url"
          extra={
            <span className="field-hint">
              用于拼密码重置链接，如 http://127.0.0.1:5174
            </span>
          }
        >
          <Input placeholder="http://127.0.0.1:5174" />
        </Form.Item>
        <Divider />
        <Form.Item label="测试收件人" name="test_to">
          <Input placeholder="留空则发到发件人地址" />
        </Form.Item>
        <Space>
          <Button type="primary" loading={saving} onClick={onSave}>
            保存
          </Button>
          <Button loading={testing} onClick={onTest}>
            发送测试邮件
          </Button>
        </Space>
      </Form>
    </Spin>
  )
}

function readyTag(ok: boolean, label?: string) {
  return ok ? (
    <Tag color="success">{label || '已就绪'}</Tag>
  ) : (
    <Tag>{label || '未配置'}</Tag>
  )
}

function smtpStatusTag(ready: boolean, configured: boolean) {
  if (ready) {
    return <Tag color="success">SMTP 就绪</Tag>
  }
  if (configured) {
    return <Tag color="warning">已配置·未启用</Tag>
  }
  return <Tag>未配置</Tag>
}

type RobotRouteRow = { index: number; for: 'alert' | 'ticket' | 'both' }

function NotifyPanel() {
  const [form] = Form.useForm()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<string>('')
  const [providers, setProviders] = useState<Record<string, any>>({})
  const [smtpReady, setSmtpReady] = useState(false)
  const [smtpConfigured, setSmtpConfigured] = useState(false)
  const [testTo, setTestTo] = useState<Record<string, string>>({})
  const [robotRoutes, setRobotRoutes] = useState<RobotRouteRow[]>([])

  function routeFor(index: number): RobotRouteRow['for'] {
    const hit = robotRoutes.find((r) => r.index === index)
    return hit?.for || 'alert'
  }

  function setRouteFor(index: number, forDir: RobotRouteRow['for']) {
    setRobotRoutes((prev) => {
      const rest = prev.filter((r) => r.index !== index)
      return [...rest, { index, for: forDir }].sort((a, b) => a.index - b.index)
    })
  }

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/notify')
      const d = data?.data || {}
      setProviders(d.providers || {})
      setSmtpReady(!!d.smtp_ready)
      setSmtpConfigured(!!d.smtp_configured)
      const saved: RobotRouteRow[] = Array.isArray(d.robot_routes)
        ? d.robot_routes.map((r: any) => ({
            index: Number(r.index),
            for: (r.for === 'ticket' || r.for === 'both' ? r.for : 'alert') as RobotRouteRow['for'],
          }))
        : []
      setRobotRoutes(saved)
      form.setFieldsValue({
        email_on_ticket_create: !!d.email_on_ticket_create,
        email_on_ticket_exec_failed: !!d.email_on_ticket_exec_failed,
        email_on_ticket_await_exec: !!d.email_on_ticket_await_exec,
        email_on_ticket_await_exec_stuck: !!d.email_on_ticket_await_exec_stuck,
        email_on_backup_failed: !!d.email_on_backup_failed,
        await_exec_stuck_hours: Number(d.await_exec_stuck_hours) > 0 ? Number(d.await_exec_stuck_hours) : 2,
        webhook_on_alert: d.webhook_on_alert !== false,
        wecom_on_alert: !!d.wecom_on_alert,
        wecom_on_ticket_create: !!d.wecom_on_ticket_create,
        phone_on_alert: !!d.phone_on_alert,
        email_on_alert: !!d.email_on_alert,
        lark_on_ticket_create: !!d.lark_on_ticket_create,
        lark_on_alert: !!d.lark_on_alert,
        lark_alert_on_alert: !!d.lark_alert_on_alert,
        sms_on_alert: !!d.sms_on_alert,
        dingtalk_on_alert: !!d.dingtalk_on_alert,
        robot_lark_on_alert: !!d.robot_lark_on_alert,
        robots_on_ticket_create: !!d.robots_on_ticket_create,
        alert_notify_max_fire: Number(d.alert_notify_max_fire) >= 0 ? Number(d.alert_notify_max_fire) : 3,
        alert_notify_window: d.alert_notify_window || '15m',
        alert_notify_repeat: d.alert_notify_repeat || '5m',
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function onSave() {
    setSaving(true)
    try {
      const f = form.getFieldsValue()
      if (f.robots_on_ticket_create) {
        const robots = Array.isArray(providers.robots) ? providers.robots : []
        const hasTicket = robots.some((r: any) => {
          const dir = routeFor(Number(r.index))
          return dir === 'ticket' || dir === 'both'
        })
        if (robots.length > 0 && !hasTicket) {
          message.warning('已开「工单待审打群」，请至少把一条机器人方向设为「工单」或「两者」')
        }
      }
      await api.put('/settings/notify', {
        email_on_ticket_create: !!f.email_on_ticket_create,
        email_on_ticket_exec_failed: !!f.email_on_ticket_exec_failed,
        email_on_ticket_await_exec: !!f.email_on_ticket_await_exec,
        email_on_ticket_await_exec_stuck: !!f.email_on_ticket_await_exec_stuck,
        email_on_backup_failed: !!f.email_on_backup_failed,
        await_exec_stuck_hours: Number(f.await_exec_stuck_hours) > 0 ? Number(f.await_exec_stuck_hours) : 2,
        webhook_on_alert: !!f.webhook_on_alert,
        wecom_on_alert: !!f.wecom_on_alert,
        phone_on_alert: !!f.phone_on_alert,
        email_on_alert: !!f.email_on_alert,
        wecom_on_ticket_create: !!f.wecom_on_ticket_create,
        lark_on_ticket_create: !!f.lark_on_ticket_create,
        lark_on_alert: !!f.lark_on_alert,
        lark_alert_on_alert: !!f.lark_alert_on_alert,
        sms_on_alert: !!f.sms_on_alert,
        dingtalk_on_alert: !!f.dingtalk_on_alert,
        robot_lark_on_alert: !!f.robot_lark_on_alert,
        robots_on_ticket_create: !!f.robots_on_ticket_create,
        robot_routes: robotRoutes,
        alert_notify_max_fire: Number(f.alert_notify_max_fire) >= 0 ? Number(f.alert_notify_max_fire) : 3,
        alert_notify_window: String(f.alert_notify_window || '15m').trim() || '15m',
        alert_notify_repeat: String(f.alert_notify_repeat || '').trim(),
      })
      message.success('已保存')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function onTest(channel: string, extra?: { for?: string; index?: number }) {
    const key = extra?.index != null ? `${channel}:${extra.index}:${extra.for || 'alert'}` : channel
    setTesting(key)
    try {
      const { data } = await api.post('/settings/notify/test', {
        channel,
        to: (testTo[channel] || '').trim(),
        ...(extra?.for ? { for: extra.for } : {}),
        ...(extra?.index != null ? { index: extra.index } : {}),
      })
      message.success(data?.message || '测试已发送')
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '测试失败')
    } finally {
      setTesting('')
    }
  }

  function testRow(channel: string, placeholder: string) {
    return (
      <Space wrap style={{ marginTop: 8 }}>
        <Input
          style={{ width: 260 }}
          placeholder={placeholder}
          value={testTo[channel] || ''}
          onChange={(e) => setTestTo((prev) => ({ ...prev, [channel]: e.target.value }))}
          allowClear
        />
        <Button loading={testing === channel} onClick={() => onTest(channel)}>
          发送测试
        </Button>
      </Space>
    )
  }

  const panelLabel = (title: string, ready: boolean, readyLabel?: string) => (
    <Space>
      <span>{title}</span>
      {readyTag(ready, readyLabel)}
    </Space>
  )

  return (
    <Spin spinning={loading}>
      <Form form={form} layout="vertical" style={{ maxWidth: 720 }}>
        <p className="field-hint" style={{ marginBottom: 12 }}>
          渠道凭证（app_id / secret / 群机器人 URL 等）请在 Nacos DataId <code>auto-dba.yaml</code> 的{' '}
          <code>notify</code> 段配置；本页仅开关事件与发送测试。说明见仓库{' '}
          <code>docs/nacos-config.md</code>。
        </p>
        <Collapse
          defaultActiveKey={['email', 'alert_rate', 'lark', 'wecom']}
          items={[
            {
              key: 'alert_rate',
              label: '告警发送频率',
              children: (
                <>
                  <p className="field-hint" style={{ marginBottom: 12 }}>
                    控制监控告警的触发/续报刷屏：滑动窗口内最多发送 N 次；满额后持续异常也不再发，窗口过后再发。
                    告警最终恢复时仍会发送一条「告警已恢复」提示，不受此频控限制。规则可单独覆盖（告警规则 → 通知频控）。
                  </p>
                  <Form.Item
                    label="窗口内最多通知"
                    name="alert_notify_max_fire"
                    extra={<span className="field-hint">含 trigger 与续报；0=不限制。默认 3</span>}
                  >
                    <InputNumber min={0} max={100} style={{ width: 120 }} />
                  </Form.Item>
                  <Form.Item
                    label="计数窗口"
                    name="alert_notify_window"
                    extra={<span className="field-hint">如 15m / 1h。默认 15m</span>}
                  >
                    <Input style={{ width: 160 }} placeholder="15m" />
                  </Form.Item>
                  <Form.Item
                    label="未处理续报间隔"
                    name="alert_notify_repeat"
                    extra={<span className="field-hint">持续 firing 且待认领时续报；空=关闭。默认 5m</span>}
                  >
                    <Input style={{ width: 160 }} placeholder="5m" />
                  </Form.Item>
                </>
              ),
            },
            {
              key: 'email',
              label: (
                <Space>
                  <span>邮件</span>
                  {smtpStatusTag(smtpReady, smtpConfigured)}
                </Space>
              ),
              children: (
                <>
                  {!smtpReady && smtpConfigured ? (
                    <p className="field-hint" style={{ marginBottom: 12 }}>
                      SMTP 已在「邮件」Tab 配置，但未打开「启用 SMTP」——工单/告警邮件不会自动外发；仍可在此发送测试。
                    </p>
                  ) : null}
                  {!smtpConfigured ? (
                    <p className="field-hint" style={{ marginBottom: 12 }}>
                      请先到「邮件」Tab 填写并保存 SMTP（服务器、发件人、密码）。
                    </p>
                  ) : null}
                  <Form.Item label="工单创建" name="email_on_ticket_create" valuePropName="checked"
                    extra={<span className="field-hint">通知审批人（需启用 SMTP + 审批人邮箱；未启用则不会实际外发）</span>}>
                    <Switch />
                  </Form.Item>
                  <Form.Item label="工单执行失败" name="email_on_ticket_exec_failed" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="备份失败" name="email_on_backup_failed" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="审批后待执行" name="email_on_ticket_await_exec" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="待执行超时告警" name="email_on_ticket_await_exec_stuck" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="待执行超时（小时）" name="await_exec_stuck_hours">
                    <InputNumber min={1} max={168} style={{ width: 120 }} />
                  </Form.Item>
                  <Form.Item label="告警邮件" name="email_on_alert" valuePropName="checked"
                    extra={<span className="field-hint">发给值班人 / labels.owner</span>}>
                    <Switch />
                  </Form.Item>
                  {testRow('email', '测试收件邮箱（空=当前用户邮箱）')}
                </>
              ),
            },
            {
              key: 'lark',
              label: panelLabel('飞书私聊', !!providers.lark_ready),
              children: (
                <>
                  <p className="field-hint">
                    Nacos：notify.provider.lark。接收人按 im_account（飞书 user_id / open_id / 邮箱 / 手机号）→ 用户邮箱 → 用户手机号依次尝试；
                    邮箱、手机号须与飞书通讯录一致。应用需开通 im:message，按手机号发送另需 contact:user.id:readonly，且可用范围覆盖接收人。
                    开启后：所有工单通知审核人；建库工单执行成功后另通知申请人（含账号密码）。
                  </p>
                  <Form.Item label="工单创建通知审批人" name="lark_on_ticket_create" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="告警私聊" name="lark_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  {testRow('lark', '飞书 user_id / 邮箱 / 手机号 / open_id（空=当前用户 im_account）')}
                </>
              ),
            },
            {
              key: 'lark_alert',
              label: panelLabel('飞书告警应用', !!providers.alert_lark_ready),
              children: (
                <>
                  <p className="field-hint">独立应用 notify.provider.alert_lark</p>
                  <Form.Item label="告警通知" name="lark_alert_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  {testRow('lark_alert', '飞书 user_id / 邮箱 / 手机号 / open_id')}
                </>
              ),
            },
            {
              key: 'wecom',
              label: panelLabel('企微应用', !!providers.wecom_ready, providers.wecom_ready ? 'Nacos 就绪' : '未配置(可回退库内旧凭证)'),
              children: (
                <>
                  <p className="field-hint">凭证：Nacos notify.provider.wechat（corp_id / agent_id / api_secret）</p>
                  <p className="field-hint">
                    告警可发往 Nacos <code>party_ids</code>（部门 ID，需在应用可见范围）；工单按审批人 im_account。
                    {Array.isArray(providers.wecom_party_ids) && providers.wecom_party_ids.length > 0
                      ? ` 当前部门：${providers.wecom_party_ids.join(', ')}`
                      : ' 当前未配置 party_ids'}
                  </p>
                  <Form.Item label="工单创建通知" name="wecom_on_ticket_create" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="告警通知" name="wecom_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  {testRow('wecom', 'userid 或部门ID（如 ccj / 15 / party:15）')}
                </>
              ),
            },
            {
              key: 'robots',
              label: panelLabel(
                '群机器人',
                Number(providers.robots_count) > 0,
                Number(providers.robots_count) > 0 ? `${providers.robots_count} 个` : '未配置',
              ),
              children: (
                <>
                  <p className="field-hint">
                    Webhook URL 仍配在 Nacos <code>notify.robots</code>（kind + url）。用途方向（告警 / 工单）在本页按条配置并保存到库，不写 Nacos。
                  </p>
                  <Form.Item noStyle shouldUpdate={(prev, cur) => prev.robots_on_ticket_create !== cur.robots_on_ticket_create}>
                    {() => {
                      const ticketOn = !!form.getFieldValue('robots_on_ticket_create')
                      const robots = Array.isArray(providers.robots) ? providers.robots : []
                      const hasTicketRoute = robots.some((r: any) => {
                        const f = routeFor(Number(r.index))
                        return f === 'ticket' || f === 'both'
                      })
                      if (ticketOn && robots.length > 0 && !hasTicketRoute) {
                        return (
                          <Alert
                            type="warning"
                            showIcon
                            style={{ marginBottom: 12 }}
                            message="已开「工单待审打群」，但下方机器人方向都是「告警」"
                            description="请把要用的条目改成「工单」或「两者」，再点保存；否则创建工单不会进群。"
                          />
                        )
                      }
                      return null
                    }}
                  </Form.Item>
                  {Array.isArray(providers.robots) && providers.robots.length > 0 ? (
                    <div style={{ marginBottom: 16 }}>
                      {providers.robots.map((r: any) => {
                        const idx = Number(r.index)
                        const forDir = routeFor(idx)
                        const testAlertKey = `robot:${idx}:alert`
                        const testTicketKey = `robot:${idx}:ticket`
                        return (
                          <div
                            key={idx}
                            style={{
                              display: 'flex',
                              flexWrap: 'wrap',
                              alignItems: 'center',
                              gap: 8,
                              marginBottom: 10,
                              paddingBottom: 10,
                              borderBottom: '1px solid var(--ant-color-split, #f0f0f0)',
                            }}
                          >
                            <Tag>{r.kind || '?'}</Tag>
                            <span className="field-hint" style={{ minWidth: 160 }}>
                              #{idx} {r.url || ''}
                            </span>
                            <Select
                              style={{ width: 120 }}
                              value={forDir}
                              options={[
                                { value: 'alert', label: '告警' },
                                { value: 'ticket', label: '工单' },
                                { value: 'both', label: '两者' },
                              ]}
                              onChange={(v) => setRouteFor(idx, v)}
                            />
                            <Button
                              size="small"
                              loading={testing === testAlertKey}
                              onClick={() => onTest('robot', { for: 'alert', index: idx })}
                            >
                              测告警
                            </Button>
                            <Button
                              size="small"
                              loading={testing === testTicketKey}
                              onClick={() => onTest('robot', { for: 'ticket', index: idx })}
                            >
                              测工单
                            </Button>
                          </div>
                        )
                      })}
                    </div>
                  ) : (
                    <p className="field-hint">尚未加载到群机器人，请先在 Nacos 配置 URL。</p>
                  )}
                  <Form.Item
                    label="工单待审打群"
                    name="robots_on_ticket_create"
                    valuePropName="checked"
                    extra={<span className="field-hint">开启后，方向含「工单」的机器人会收到待审通知</span>}
                  >
                    <Switch />
                  </Form.Item>
                  <Form.Item label="通用 Webhook 告警" name="webhook_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  <Form.Item label="钉钉机器人告警" name="dingtalk_on_alert" valuePropName="checked"
                    extra={<span className="field-hint">总闸：仅对方向含「告警」的钉钉条目生效</span>}>
                    <Switch />
                  </Form.Item>
                  <Form.Item label="飞书群机器人告警" name="robot_lark_on_alert" valuePropName="checked"
                    extra={<span className="field-hint">总闸：仅对方向含「告警」的飞书群条目生效</span>}>
                    <Switch />
                  </Form.Item>
                  <Space wrap style={{ marginTop: 8 }}>
                    <Button loading={testing === 'webhook'} onClick={() => onTest('webhook')}>
                      测试 Webhook
                    </Button>
                    <Button loading={testing === 'dingtalk'} onClick={() => onTest('dingtalk', { for: 'alert' })}>
                      测试钉钉告警
                    </Button>
                    <Button loading={testing === 'robot_lark'} onClick={() => onTest('robot_lark', { for: 'alert' })}>
                      测试飞书群告警
                    </Button>
                  </Space>
                </>
              ),
            },
            {
              key: 'sms',
              label: panelLabel(
                '短信',
                !!providers.sms_ready,
                providers.sms_ready ? `已就绪(${providers.sms_backend || ''})` : '未配置',
              ),
              children: (
                <>
                  <p className="field-hint">Nacos tele_provider + provider.sms；发给用户档案 phone</p>
                  <Form.Item label="告警短信" name="sms_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  {testRow('sms', '手机号（空=当前用户）')}
                </>
              ),
            },
            {
              key: 'call',
              label: panelLabel(
                '电话',
                !!providers.call_ready,
                providers.call_ready ? `已就绪(${providers.call_backend || ''})` : '未配置',
              ),
              children: (
                <>
                  <p className="field-hint">Nacos tele_provider + provider.call（cloopen / huawei / yjzy）</p>
                  <Form.Item label="告警电话" name="phone_on_alert" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                  {testRow('call', '手机号（空=当前用户）')}
                </>
              ),
            },
          ]}
        />
        <Divider />
        <Button type="primary" loading={saving} onClick={onSave}>
          保存开关
        </Button>
      </Form>
    </Spin>
  )
}

export default function AuthConfig() {
  return (
    <div className="ops-page ldap-page">
      <div className="ops-title">
        <h1>认证配置</h1>
      </div>
      <p className="field-hint" style={{ marginBottom: 16 }}>
        开启哪个模块，登录页即可选择对应认证方式。邮件 SMTP 在「邮件」Tab；通知 Tab 按渠道开关事件并发送测试，凭证配在 Nacos notify 段。
      </p>
      <Tabs
        defaultActiveKey="ldap"
        items={[
          { key: 'ldap', label: 'LDAP', children: <LdapPanel /> },
          { key: 'sso', label: 'SSO', children: <SsoPanel /> },
          { key: 'smtp', label: '邮件', children: <SmtpPanel /> },
          { key: 'notify', label: '通知', children: <NotifyPanel /> },
        ]}
      />
    </div>
  )
}
