import React, { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Space,
  Switch,
  Tooltip,
  message,
} from 'antd'
import api from '../../api/request'
import './MigrationSettings.css'

interface OSSConfig {
  endpoint: string
  bucket: string
  prefix: string
  access_key: string
  secret_key: string
  retention_days: number
  chunk_size_mb: number
}

interface PerfConfig {
  max_concurrent_tasks: number
  table_parallelism: number
  batch_size: number
  single_table_timeout_min: number
  default_speed_limit: number
  schedule_tolerance_min: number
}

const perfFields: { key: keyof PerfConfig; label: string; min: number; max: number }[] = [
  { key: 'max_concurrent_tasks', label: '最大并发任务数', min: 1, max: 10 },
  { key: 'table_parallelism', label: '默认多表并行度', min: 1, max: 16 },
  { key: 'batch_size', label: '默认批次大小', min: 100, max: 100000 },
  { key: 'single_table_timeout_min', label: '单表迁移超时（分钟）', min: 1, max: 1440 },
  { key: 'default_speed_limit', label: '默认限速（行/秒）', min: 0, max: 1000000 },
  { key: 'schedule_tolerance_min', label: '定时触发容忍窗口（分钟）', min: 1, max: 60 },
]

function emptyOSS(): OSSConfig {
  return {
    endpoint: '',
    bucket: '',
    prefix: 'migration',
    access_key: '',
    secret_key: '',
    retention_days: 7,
    chunk_size_mb: 100,
  }
}

function emptyPerf(): PerfConfig {
  return {
    max_concurrent_tasks: 3,
    table_parallelism: 4,
    batch_size: 10000,
    single_table_timeout_min: 30,
    default_speed_limit: 0,
    schedule_tolerance_min: 10,
  }
}

export default function MigrationSettings() {
  const navigate = useNavigate()
  const [loading, setLoading] = useState(false)
  const [savingApproval, setSavingApproval] = useState(false)
  const [savingOSS, setSavingOSS] = useState(false)
  const [savingPerf, setSavingPerf] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testOk, setTestOk] = useState(false)

  const [approval, setApproval] = useState({ approval_enabled: true, approver_user_ids: '' })
  const [oss, setOss] = useState<OSSConfig>(emptyOSS())
  const [perf, setPerf] = useState<PerfConfig>(emptyPerf())

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/migration/settings')
      const row = data.data || {}
      setApproval({
        approval_enabled: row.approval_enabled !== false,
        approver_user_ids: row.approver_user_ids || '',
      })
      setOss({ ...emptyOSS(), ...(row.oss || {}) })
      setPerf({ ...emptyPerf(), ...(row.performance || {}) })
      setTestOk(false)
    } finally {
      setLoading(false)
    }
  }

  async function saveApproval() {
    setSavingApproval(true)
    try {
      await api.put('/migration/settings', {
        approval_enabled: approval.approval_enabled,
        approver_user_ids: approval.approver_user_ids,
      })
      message.success('已保存')
      await load()
    } finally {
      setSavingApproval(false)
    }
  }

  function ossBody() {
    return {
      endpoint: oss.endpoint,
      bucket: oss.bucket,
      prefix: oss.prefix,
      access_key: oss.access_key,
      secret_key: oss.secret_key,
      retention_days: oss.retention_days,
      chunk_size_mb: oss.chunk_size_mb,
    }
  }

  async function testOSS() {
    setTesting(true)
    try {
      const { data } = await api.post('/migration/oss/test', ossBody(), { timeout: 20000 })
      const ok = Boolean(data.ok || data.success || data.connected)
      setTestOk(ok)
      const msg = data.message || data.error_message || (ok ? 'OSS 连接成功' : '连接失败')
      if (ok) message.success(msg)
      else message.error(msg)
    } catch (e: any) {
      setTestOk(false)
      message.error(e?.response?.data?.message || e?.message || '连接失败')
    } finally {
      setTesting(false)
    }
  }

  async function doSaveOSS() {
    setSavingOSS(true)
    try {
      await api.put('/migration/settings', { oss: ossBody() })
      message.success('已保存')
      setTestOk(false)
      await load()
    } finally {
      setSavingOSS(false)
    }
  }

  function saveOSS() {
    if (!testOk) {
      Modal.confirm({
        title: '尚未通过连接测试',
        content: '尚未通过连接测试，仍要保存当前 OSS 配置吗？',
        okText: '仍要保存',
        cancelText: '取消',
        onOk: doSaveOSS,
      })
      return
    }
    doSaveOSS()
  }

  async function savePerf() {
    setSavingPerf(true)
    try {
      await api.put('/migration/settings', { performance: { ...perf } })
      message.success('已保存')
      await load()
    } finally {
      setSavingPerf(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="gray-page mig-settings">
      <div className="ops-title">
        <h1>迁移设置</h1>
        <Button onClick={() => navigate('/ops/migration')}>返回列表</Button>
      </div>

      <div className="form-narrow">
        <Card
          className="panel"
          bordered
          title={
            <div className="card-head">
              <span>迁移审批（global-args）</span>
              <Button type="link" loading={loading} onClick={load}>
                刷新
              </Button>
            </div>
          }
        >
          <Form layout="vertical">
            <Form.Item label="开启迁移审批">
              <Switch
                checked={approval.approval_enabled}
                onChange={(v) => setApproval((a) => ({ ...a, approval_enabled: v }))}
              />
            </Form.Item>
            <Form.Item label="审批人">
              <div className="hint mb">值为登录用户名，逗号分隔。创建任务时从上述名单选择审核人。</div>
              <Input.TextArea
                rows={2}
                placeholder="如 xusong, chenjiachi"
                value={approval.approver_user_ids}
                onChange={(e) => setApproval((a) => ({ ...a, approver_user_ids: e.target.value }))}
              />
            </Form.Item>
            <Form.Item>
              <Button type="primary" loading={savingApproval} onClick={saveApproval}>
                保存
              </Button>
            </Form.Item>
          </Form>
        </Card>

        <Card className="panel" bordered title="OSS 配置">
          <Form layout="vertical">
            <Form.Item label="OSS 访问地址">
              <Input
                placeholder="https://oss-cn-hangzhou.aliyuncs.com"
                value={oss.endpoint}
                onChange={(e) => setOss((o) => ({ ...o, endpoint: e.target.value }))}
              />
            </Form.Item>
            <Form.Item label="存储桶名称">
              <Input value={oss.bucket} onChange={(e) => setOss((o) => ({ ...o, bucket: e.target.value }))} />
            </Form.Item>
            <Form.Item label="文件路径前缀">
              <Input
                placeholder="migration"
                value={oss.prefix}
                onChange={(e) => setOss((o) => ({ ...o, prefix: e.target.value }))}
              />
            </Form.Item>
            <Form.Item label="AccessKey ID">
              <Input.Password
                placeholder="脱敏展示，修改请重新输入"
                autoComplete="new-password"
                value={oss.access_key}
                onChange={(e) => setOss((o) => ({ ...o, access_key: e.target.value }))}
              />
            </Form.Item>
            <Form.Item label="AccessKey Secret">
              <Input.Password
                placeholder="脱敏展示，修改请重新输入"
                autoComplete="new-password"
                value={oss.secret_key}
                onChange={(e) => setOss((o) => ({ ...o, secret_key: e.target.value }))}
              />
            </Form.Item>
            <Form.Item label="临时文件保留天数">
              <InputNumber
                min={1}
                max={3650}
                style={{ width: '100%' }}
                value={oss.retention_days}
                onChange={(v) => setOss((o) => ({ ...o, retention_days: Number(v) }))}
              />
            </Form.Item>
            <Form.Item label="单文件分片大小 (MB)">
              <InputNumber
                min={1}
                max={10240}
                style={{ width: '100%' }}
                value={oss.chunk_size_mb}
                onChange={(v) => setOss((o) => ({ ...o, chunk_size_mb: Number(v) }))}
              />
            </Form.Item>
            <Form.Item>
              <div className="actions">
                <Button loading={testing} onClick={testOSS}>
                  连接测试
                </Button>
                <Button type="primary" loading={savingOSS} onClick={saveOSS}>
                  保存
                </Button>
              </div>
            </Form.Item>
          </Form>
        </Card>

        <Card className="panel last" bordered title="全局性能参数">
          <Form layout="vertical">
            {perfFields.map((item) => (
              <Form.Item
                key={item.key}
                label={
                  <span>
                    <span>{item.label}</span>
                    <Tooltip title="此为全局默认值，可在创建迁移任务时按需覆盖" placement="top">
                      <span className="help-dot">?</span>
                    </Tooltip>
                  </span>
                }
              >
                <InputNumber
                  min={item.min}
                  max={item.max}
                  style={{ width: '100%' }}
                  value={perf[item.key]}
                  onChange={(v) => setPerf((p) => ({ ...p, [item.key]: Number(v) }))}
                />
              </Form.Item>
            ))}
            <Form.Item>
              <Button type="primary" loading={savingPerf} onClick={savePerf}>
                保存
              </Button>
            </Form.Item>
          </Form>
        </Card>
      </div>
    </div>
  )
}
