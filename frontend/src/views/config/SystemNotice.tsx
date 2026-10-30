import React, { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Select, Space, Switch, message } from 'antd'
import api from '../../api/request'
import { usePermission } from '../../hooks/usePermission'
import {
  RECOMMENDED_SYSTEM_NOTICE_BODY,
  RECOMMENDED_SYSTEM_NOTICE_TITLE,
} from '../../utils/systemNoticeCopy'
import './SystemNotice.css'

const SHOW_MODE_OPTIONS = [
  { value: 'on_version_change', label: '版本变更再弹（改文案并提高版本后再次提醒）' },
  { value: 'once', label: '仅首次（每个用户确认后不再弹出）' },
  { value: 'every_login', label: '每次登录（每次进入主界面都弹）' },
]

const DEFAULT_FREEZE_MSG = '平台正在发版维护，暂不可提交工单，请稍后再试'

export default function SystemNotice() {
  const { hasUrn } = usePermission()
  const canEdit = hasUrn('um.action.system-notice.edit')
  const [form] = Form.useForm()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [version, setVersion] = useState(1)
  const [noticeId, setNoticeId] = useState('')

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/settings/system-notice')
      const row = data.data || {}
      setNoticeId(String(row.id || ''))
      setVersion(Number(row.version || 1))
      form.setFieldsValue({
        enabled: !!row.enabled,
        title: row.title || '',
        body: row.body || '',
        show_mode: row.show_mode || 'on_version_change',
        bump_version: false,
        ticket_submit_freeze: !!row.ticket_submit_freeze,
        ticket_submit_freeze_message: row.ticket_submit_freeze_message || '',
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function fillRecommended() {
    form.setFieldsValue({
      title: RECOMMENDED_SYSTEM_NOTICE_TITLE,
      body: RECOMMENDED_SYSTEM_NOTICE_BODY,
      bump_version: true,
    })
    message.info('已填入推荐文案，并勾选提高版本号；请确认后点击保存')
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const title = String(values.title || '').trim() || '系统通知'
      const { data } = await api.put('/settings/system-notice', {
        id: noticeId || undefined,
        title,
        body: values.body || '',
        enabled: !!values.enabled,
        show_mode: values.show_mode,
        bump_version: !!values.bump_version,
        ticket_submit_freeze: !!values.ticket_submit_freeze,
        ticket_submit_freeze_message: String(values.ticket_submit_freeze_message || '').trim(),
      })
      const row = data.data || {}
      setNoticeId(String(row.id || noticeId))
      setVersion(Number(row.version || version))
      form.setFieldValue('bump_version', false)
      message.success('已保存系统通知')
      await load()
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="page-card system-notice-page">
      <h2 style={{ marginTop: 0 }}>系统通知</h2>
      <Form form={form} layout="vertical" disabled={loading || !canEdit}>
        <div className="system-notice-grid">
          <div className="system-notice-col">
            <h3>登录弹框</h3>
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              message="登录后弹框"
              description="启用后，用户进入主界面时按「弹出策略」显示 Modal。可用「填入推荐文案」套用基于本平台能力的须知，再按需改写后保存。"
            />
            <Form.Item name="enabled" label="启用登录弹框" valuePropName="checked">
              <Switch checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>
            <Form.Item
              name="show_mode"
              label="弹出策略"
              rules={[{ required: true, message: '请选择弹出策略' }]}
            >
              <Select options={SHOW_MODE_OPTIONS} />
            </Form.Item>
            <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
              <Input maxLength={120} placeholder={RECOMMENDED_SYSTEM_NOTICE_TITLE} />
            </Form.Item>
            <Form.Item
              name="body"
              label="正文"
              extra="建议写清：租户切换、工单流程、查询边界与安全提醒。可用下方按钮一键填入推荐稿。"
            >
              <Input.TextArea rows={9} placeholder={RECOMMENDED_SYSTEM_NOTICE_BODY.slice(0, 80) + '…'} />
            </Form.Item>
            <Form.Item
              name="bump_version"
              label="保存时提高版本号"
              valuePropName="checked"
              extra={`当前版本：${version}。勾选后，策略为「版本变更再弹」的用户会再次看到弹框。`}
            >
              <Switch />
            </Form.Item>
          </div>

          <div className="system-notice-col">
            <h3>发版冻结</h3>
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 16 }}
              message="禁止提交新工单"
              description="开启后，用户无法再提交新工单（已在执行的工单不受影响，也可随时开启）。关闭登录弹框不影响本开关。"
            />
            <Form.Item name="ticket_submit_freeze" label="禁止提交工单" valuePropName="checked">
              <Switch checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>
            <Form.Item
              name="ticket_submit_freeze_message"
              label="冻结提示文案"
              extra={`可空，空则使用默认：${DEFAULT_FREEZE_MSG}`}
            >
              <Input.TextArea rows={3} maxLength={500} placeholder={DEFAULT_FREEZE_MSG} />
            </Form.Item>
          </div>
        </div>

        <div className="system-notice-footer">
          {canEdit ? (
            <Space wrap>
              <Button type="primary" loading={saving} onClick={save}>
                保存
              </Button>
              <Button onClick={fillRecommended}>填入推荐文案</Button>
              <Button onClick={load}>刷新</Button>
            </Space>
          ) : (
            <Alert type="warning" message="当前账号无编辑权限" style={{ maxWidth: 360 }} />
          )}
        </div>
      </Form>
    </div>
  )
}
