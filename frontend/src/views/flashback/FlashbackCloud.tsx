import React, { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Button, Card, Form, Input, Spin, message } from 'antd'
import api from '../../api/request'
import './flashback.css'

function sourceLabel(src: any) {
  return (
    ({ db: '已存数据库', env: '环境变量', yaml: 'YAML' } as Record<string, string>)[src] || '未配置'
  )
}

function argLabel(a: any) {
  const src = a.source ? ` · ${sourceLabel(a.source)}` : ''
  return `${a.description || a.key}${src}`
}

export default function FlashbackCloud() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [vendors, setVendors] = useState<any[]>([])
  const [args, setArgs] = useState<any[]>([])

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/flashback/cloud-settings')
      const row = data.data || {}
      setVendors(row.vendors || [])
      setArgs((row.args || []).map((a: any) => ({ ...a, value: a.value || '' })))
    } finally {
      setLoading(false)
    }
  }

  async function save() {
    setSaving(true)
    try {
      await api.put('/flashback/cloud-settings', {
        args: args.map((a) => ({ key: a.key, value: a.value, description: a.description })),
      })
      message.success('已保存')
      await load()
    } finally {
      setSaving(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="gray-page fb-cloud">
      <div className="ops-title">
        <h1>数据闪回 · 多云密钥</h1>
        <Button onClick={() => navigate('/ops/flashback/dashboard')}>返回仪表盘</Button>
      </div>
      <p className="lead">
        密钥按云厂商各存一套，写入全局参数后立即生效。下载 WAL 时按「配置 →
        实例」上的云厂商匹配对应 AK/SK；云实例 ID / Region 也填在实例上。密钥类脱敏展示，留空保存表示保留原值。
      </p>

      <Alert
        type="info"
        showIcon
        closable={false}
        className="mb"
        message="与实例的关联方式"
        description={
          <>
            在「配置 →
            实例」选择云厂商并填写云实例 ID、Region。闪回预检/执行时用该厂商的密钥从云平台拉 WAL。
            腾讯云 PostgreSQL 已支持云日志下载；阿里云 / 华为云 / AWS 可先配密钥，下载能力按厂商逐步接入。
          </>
        }
      />

      <Spin spinning={loading}>
        <div className="checks">
          {vendors.map((v) => (
            <div key={v.vendor} className="check">
              <span className={'dot ' + (v.configured ? 'ok' : 'bad')} />
              <div>
                <strong>{v.name}</strong>
                <div className="mono">{v.id_key}</div>
                <div className="meta">{v.configured ? '已配置密钥' : '未配置密钥'}</div>
              </div>
            </div>
          ))}
        </div>
      </Spin>

      <Card
        className="panel"
        title={
          <div className="card-head">
            <span>云厂商密钥与下载参数</span>
            <Button type="link" loading={loading} onClick={load}>
              刷新
            </Button>
          </div>
        }
      >
        <Form layout="vertical" className="grid">
          {args.map((a) => {
            const onChange = (e: React.ChangeEvent<HTMLInputElement>) =>
              setArgs((prev) => prev.map((x) => (x.key === a.key ? { ...x, value: e.target.value } : x)))
            return (
              <Form.Item key={a.key} label={argLabel(a)}>
                {a.secret ? (
                  <Input.Password
                    value={a.value}
                    placeholder="库中已保存则脱敏，留空保留"
                    autoComplete="new-password"
                    visibilityToggle
                    onChange={onChange}
                  />
                ) : (
                  <Input
                    value={a.value}
                    placeholder={a.description}
                    autoComplete="new-password"
                    onChange={onChange}
                  />
                )}
              </Form.Item>
            )
          })}
        </Form>
        <div className="actions">
          <Button type="primary" loading={saving} onClick={save}>
            保存到数据库
          </Button>
        </div>
      </Card>
    </div>
  )
}
