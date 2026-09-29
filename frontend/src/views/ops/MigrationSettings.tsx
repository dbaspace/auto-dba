import React, { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Space,
  Switch,
  Tag,
  Tooltip,
  message,
} from 'antd'
import api from '../../api/request'
import './MigrationSettings.css'

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
  const [savingPerf, setSavingPerf] = useState(false)

  const [approval, setApproval] = useState({ approval_enabled: true, approver_user_ids: '' })
  const [ossConfigured, setOssConfigured] = useState(false)
  const [ossEnvs, setOssEnvs] = useState<string[]>([])
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
      setOssConfigured(Boolean(row.oss_configured))
      setOssEnvs(Array.isArray(row.oss_envs) ? row.oss_envs : [])
      setPerf({ ...emptyPerf(), ...(row.performance || {}) })
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
          <Alert
            type="info"
            showIcon
            className="mb"
            message={
              <span>
                OSS 按环境在 <Link to="/config/oss">配置中心 → OSS 配置</Link> 维护；创建迁移任务选择「OSS
                中转」时按任务环境引用。
              </span>
            }
          />
          <Space direction="vertical" size={4} style={{ width: '100%' }}>
            <div>
              状态：
              {ossConfigured ? <Tag color="success">已有环境配置</Tag> : <Tag color="warning">尚未配置</Tag>}
            </div>
            {ossEnvs.length > 0 ? (
              <div className="hint">已配置环境：{ossEnvs.join('、')}</div>
            ) : (
              <div className="hint">请先到配置中心为各环境录入 OSS。</div>
            )}
            <div>
              <Button type="link" style={{ padding: 0 }} onClick={() => navigate('/config/oss')}>
                前往配置中心
              </Button>
            </div>
          </Space>
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
