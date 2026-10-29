import React, { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Select, Space, Switch, message } from 'antd'
import api from '../../api/request'
import { usePermission } from '../../hooks/usePermission'
import {
  RECOMMENDED_SYSTEM_NOTICE_BODY,
  RECOMMENDED_SYSTEM_NOTICE_TITLE,
} from '../../utils/systemNoticeCopy'

const SHOW_MODE_OPTIONS = [
  { value: 'on_version_change', label: '版本变更再弹（改文案并提高版本后再次提醒）' },
  { value: 'once', label: '仅首次（每个用户确认后不再弹出）' },
  { value: 'every_login', label: '每次登录（每次进入主界面都弹）' },
]

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
      const { data } = await api.put('/settings/system-notice', {
        id: noticeId || undefined,
        title: values.title,
        body: values.body,
        enabled: !!values.enabled,
        show_mode: values.show_mode,
        bump_version: !!values.bump_version,
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
    <div className="page-card" style={{ maxWidth: 720 }}>
      <h2 style={{ marginTop: 0 }}>系统通知</h2>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="登录后弹框"
        description="启用后，用户进入主界面时按「弹出策略」显示 Modal。可用「填入推荐文案」套用基于本平台能力的须知（租户隔离、平台资源申请、工单变更、只读查询与审计、安全提醒），再按需改写后保存。"
      />
      <Form form={form} layout="vertical" disabled={loading || !canEdit}>
        <Form.Item name="enabled" label="启用" valuePropName="checked">
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
          rules={[{ required: true, message: '请输入正文' }]}
          extra="建议写清：租户切换、工单流程、查询边界与安全提醒。可用下方按钮一键填入推荐稿。"
        >
          <Input.TextArea rows={14} placeholder={RECOMMENDED_SYSTEM_NOTICE_BODY.slice(0, 80) + '…'} />
        </Form.Item>
        <Form.Item
          name="bump_version"
          label="保存时提高版本号"
          valuePropName="checked"
          extra={`当前版本：${version}。勾选后，策略为「版本变更再弹」的用户会再次看到弹框。`}
        >
          <Switch />
        </Form.Item>
        {canEdit ? (
          <Space wrap>
            <Button type="primary" loading={saving} onClick={save}>
              保存
            </Button>
            <Button onClick={fillRecommended}>填入推荐文案</Button>
            <Button onClick={load}>刷新</Button>
          </Space>
        ) : (
          <Alert type="warning" message="当前账号无编辑权限" />
        )}
      </Form>
    </div>
  )
}
