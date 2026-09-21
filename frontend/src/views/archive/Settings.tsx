import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Input, InputNumber, Modal, message } from 'antd'
import api from '../../api/request'
import './archive.css'

const MASK = '••••••••'

interface MailSettings {
  host: string
  port: number
  account: string
  password: string
  timeout: number
  sender: string
}
interface WebhookSettings {
  url: string
  secret: string
}
interface StorageSettings {
  availableTb?: number
}
interface Settings {
  mail: MailSettings
  webhook: WebhookSettings
  storage: StorageSettings
}

function defaultSettings(): Settings {
  return {
    mail: { host: '', port: 25, account: '', password: '', timeout: 10, sender: '' },
    webhook: { url: '', secret: '' },
    storage: { availableTb: undefined },
  }
}

function mergeSettings(raw: any): Settings {
  const base = defaultSettings()
  return {
    mail: { ...base.mail, ...(raw?.mail || {}) },
    webhook: { ...base.webhook, ...(raw?.webhook || {}) },
    storage: { ...base.storage, ...(raw?.storage || {}) },
  }
}

const TITLES: Record<string, string> = {
  mail: '邮件告警服务器',
  webhook: 'Webhook 集成',
  storage: '归档存储池',
}

export default function ArchiveSettings() {
  const [settings, setSettings] = useState<Settings>(defaultSettings())
  const [visible, setVisible] = useState(false)
  const [testing, setTesting] = useState(false)
  const [key, setKey] = useState<'mail' | 'webhook' | 'storage'>('mail')
  const [form, setForm] = useState({
    host: '',
    port: 25,
    account: '',
    password: '',
    timeout: 10,
    sender: '',
    url: '',
    secret: '',
    availableTb: 10 as number | undefined,
  })
  // 测试收件人输入弹窗（替代 ElMessageBox.prompt）
  const [promptOpen, setPromptOpen] = useState(false)
  const [promptValue, setPromptValue] = useState('')

  const settingsRef = useRef<Settings>(settings)
  settingsRef.current = settings
  const keyRef = useRef(key)
  keyRef.current = key
  const formRef = useRef(form)
  formRef.current = form

  function patchForm(p: Partial<typeof form>) {
    setForm((prev) => ({ ...prev, ...p }))
  }

  const title = useMemo(() => TITLES[key], [key])

  const rows = useMemo(() => {
    const mailOk = !!(
      settings.mail.host &&
      settings.mail.account &&
      settings.mail.sender &&
      settings.mail.password
    )
    const hookOk = !!settings.webhook.url
    const n = settings.storage.availableTb
    const storageOk = n != null && Number.isFinite(n)
    return [
      { key: 'mail', label: '邮件告警服务器', ok: mailOk, statusText: mailOk ? '已配置' : '待配置' },
      {
        key: 'webhook',
        label: 'Webhook 集成',
        ok: hookOk,
        statusText: hookOk ? '已配置' : '待配置',
      },
      {
        key: 'storage',
        label: '归档存储池',
        ok: storageOk,
        statusText: storageOk ? `可用 ${n} TB` : '待配置',
      },
    ]
  }, [settings])

  async function loadRemote() {
    try {
      const { data } = await api.get('/archive/settings')
      setSettings(mergeSettings(data.data || {}))
    } catch {
      /* keep default */
    }
  }

  async function persist(next: Settings) {
    setSettings(next)
    await api.patch('/archive/settings', next)
    setVisible(false)
    message.success('已保存')
  }

  function edit(k: 'mail' | 'webhook' | 'storage') {
    setKey(k)
    const s = settingsRef.current
    if (k === 'mail') {
      setForm((prev) => ({
        ...prev,
        ...s.mail,
        password: s.mail.password ? MASK : '',
      }))
    } else if (k === 'webhook') {
      setForm((prev) => ({
        ...prev,
        url: s.webhook.url || '',
        secret: s.webhook.secret ? MASK : '',
      }))
    } else {
      setForm((prev) => ({ ...prev, availableTb: s.storage.availableTb }))
    }
    setVisible(true)
  }

  function resolveSecret(next: any, prev: any): string {
    const v = String(next || '').trim()
    if (!v) return ''
    if (v === MASK) return prev || ''
    return v
  }

  function save() {
    const f = formRef.current
    const s = settingsRef.current
    if (keyRef.current === 'mail') {
      persist({
        ...s,
        mail: {
          host: f.host.trim(),
          port: f.port,
          account: f.account.trim(),
          password: resolveSecret(f.password, s.mail.password),
          timeout: f.timeout,
          sender: f.sender.trim(),
        },
      })
      return
    }
    if (keyRef.current === 'webhook') {
      persist({
        ...s,
        webhook: {
          url: f.url.trim(),
          secret: resolveSecret(f.secret, s.webhook.secret),
        },
      })
      return
    }
    persist({ ...s, storage: { availableTb: f.availableTb } })
  }

  async function test() {
    const f = formRef.current
    const s = settingsRef.current
    if (keyRef.current === 'mail') {
      if (!f.host || !f.account || !resolveSecret(f.password, s.mail.password)) {
        message.error('请完整填写 SMTP 主机、账号和密码')
        return
      }
      setPromptValue('')
      setPromptOpen(true)
      return
    }
    if (!f.url.trim()) {
      message.error('请输入 Webhook URL')
      return
    }
    setTesting(true)
    try {
      const { data } = await api.post('/archive/settings/webhook/test', { url: f.url.trim() })
      message.success(data.message || 'Webhook 测试已提交')
    } finally {
      setTesting(false)
    }
  }

  async function sendMailTest() {
    const to = String(promptValue || '').trim()
    if (!to) {
      message.error('请输入测试收件人')
      return
    }
    setPromptOpen(false)
    setTesting(true)
    try {
      const { data } = await api.post('/archive/settings/mail/test', { to })
      message.success(data.message || `已向 ${to} 发起测试`)
    } finally {
      setTesting(false)
    }
  }

  useEffect(() => {
    loadRemote()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const labeled = (label: string, node: React.ReactNode, required?: boolean) => (
    <div style={{ marginBottom: 16 }}>
      <div style={{ marginBottom: 6 }}>
        {required && <span style={{ color: '#ff4d4f', marginRight: 4 }}>*</span>}
        {label}
      </div>
      {node}
    </div>
  )

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="page-head">
          <h1>系统设置</h1>
          <p>全局配置、邮件告警、Webhook、归档存储（写入全局参数 archive_settings）</p>
        </div>

        <Card className="panel set-panel">
          <ul className="set-list">
            {rows.map((item) => (
              <li key={item.key} onClick={() => edit(item.key as any)}>
                <span>{item.label}</span>
                <span className={'status ' + (item.ok ? 'ok' : 'pending')}>
                  {item.statusText}
                </span>
              </li>
            ))}
          </ul>
        </Card>

        <Modal
          open={visible}
          title={title}
          width={key === 'mail' ? 560 : 520}
          destroyOnClose
          onCancel={() => setVisible(false)}
          footer={[
            (key === 'mail' || key === 'webhook') && (
              <Button key="test" loading={testing} onClick={test}>
                测试
              </Button>
            ),
            <Button key="cancel" onClick={() => setVisible(false)}>
              取消
            </Button>,
            <Button key="save" type="primary" onClick={save}>
              保存
            </Button>,
          ]}
        >
          {key === 'mail' && (
            <div>
              {labeled('邮件协议', <Input value="smtp" disabled />)}
              {labeled('邮件类型', <Input value="html" disabled />)}
              {labeled(
                'SMTP主机',
                <Input
                  value={form.host}
                  placeholder="请输入 SMTP 主机"
                  onChange={(e) => patchForm({ host: e.target.value })}
                />,
                true,
              )}
              {labeled(
                'SMTP端口',
                <InputNumber
                  min={1}
                  max={65535}
                  style={{ width: '100%' }}
                  value={form.port}
                  onChange={(v) => patchForm({ port: Number(v ?? 0) })}
                />,
                true,
              )}
              {labeled(
                'SMTP账号',
                <Input
                  value={form.account}
                  placeholder="请输入 SMTP 账号"
                  onChange={(e) => patchForm({ account: e.target.value })}
                />,
                true,
              )}
              {labeled(
                'SMTP密码',
                <Input.Password
                  value={form.password}
                  placeholder="请输入 SMTP 密码"
                  onChange={(e) => patchForm({ password: e.target.value })}
                />,
                true,
              )}
              {labeled(
                'SMTP超时时间',
                <InputNumber
                  min={1}
                  style={{ width: '100%' }}
                  value={form.timeout}
                  onChange={(v) => patchForm({ timeout: Number(v ?? 0) })}
                />,
                true,
              )}
              {labeled(
                '邮件发件人',
                <Input
                  value={form.sender}
                  placeholder="请输入邮件发件人"
                  onChange={(e) => patchForm({ sender: e.target.value })}
                />,
                true,
              )}
            </div>
          )}
          {key === 'webhook' && (
            <div>
              {labeled(
                'Webhook URL',
                <Input
                  value={form.url}
                  placeholder="https://；留空则为待配置"
                  onChange={(e) => patchForm({ url: e.target.value })}
                />,
              )}
              {labeled(
                'Secret',
                <Input.Password
                  value={form.secret}
                  placeholder="可选；已设置时显示掩码"
                  onChange={(e) => patchForm({ secret: e.target.value })}
                />,
              )}
            </div>
          )}
          {key === 'storage' && (
            <div>
              {labeled(
                '可用容量（TB）',
                <InputNumber
                  min={0}
                  step={0.1}
                  style={{ width: '100%' }}
                  value={form.availableTb}
                  onChange={(v) => patchForm({ availableTb: v == null ? undefined : Number(v) })}
                />,
                true,
              )}
            </div>
          )}
        </Modal>

        <Modal
          open={promptOpen}
          title="发送测试邮件"
          okText="发送测试"
          cancelText="取消"
          destroyOnClose
          onCancel={() => setPromptOpen(false)}
          onOk={sendMailTest}
          confirmLoading={testing}
        >
          <Input
            placeholder="请输入测试收件人"
            value={promptValue}
            onChange={(e) => setPromptValue(e.target.value)}
            onPressEnter={sendMailTest}
          />
        </Modal>
      </div>
    </div>
  )
}
