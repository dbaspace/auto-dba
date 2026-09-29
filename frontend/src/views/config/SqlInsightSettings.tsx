import React, { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Divider,
  Form,
  Input,
  InputNumber,
  Space,
  Switch,
  Tabs,
  Typography,
  message,
} from 'antd'
import { useSearchParams } from 'react-router-dom'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import './SqlInsightSettings.css'

type FieldType = 'bool' | 'number' | 'cron' | 'text' | 'url' | 'password'
type AwrGroup = 'prometheus' | 'report' | 'push'
type SettingTab = 'awr' | 'space' | 'smart'

interface FieldDef {
  key: string
  label: string
  type: FieldType
  default: string
  description: string
  placeholder?: string
  group?: AwrGroup
}

const AWR_DEFS: FieldDef[] = [
  {
    key: 'awr_prometheus_base_url',
    label: 'Prometheus 地址',
    type: 'url',
    default: '',
    description: '同时供 AWR 报告与「Postgres监控」看板。开发环境例：http://query.universe.restosuite.ai（勿带 /graph）',
    group: 'prometheus',
    placeholder: 'http://query.example.com',
  },
  {
    key: 'awr_prometheus_auth_token',
    label: '鉴权 Token',
    type: 'password',
    default: '',
    description: '可选鉴权 token',
    group: 'prometheus',
    placeholder: '可选',
  },
  {
    key: 'awr_prom_instance_label',
    label: '库指标关联标签名',
    type: 'text',
    default: 'id',
    description: '库指标关联标签名',
    group: 'prometheus',
    placeholder: 'id',
  },
  {
    key: 'awr_prom_host_label',
    label: '主机指标关联标签名',
    type: 'text',
    default: 'instance',
    description: '主机指标关联标签名',
    group: 'prometheus',
    placeholder: 'instance',
  },
  {
    key: 'awr_enabled',
    label: '总开关',
    type: 'bool',
    default: 'true',
    description: '总开关',
    group: 'report',
  },
  {
    key: 'awr_daily_cron',
    label: '每日生成 Cron',
    type: 'cron',
    default: '0 30 1 * * *',
    description: '每日生成 cron（6 段含秒）',
    group: 'report',
    placeholder: '0 30 1 * * *',
  },
  {
    key: 'awr_host_metrics_enabled',
    label: '主机(node_exporter)指标开关',
    type: 'bool',
    default: 'false',
    description: '主机(node_exporter)指标开关',
    group: 'report',
  },
  {
    key: 'awr_retention_days',
    label: '报告保留天数',
    type: 'number',
    default: '30',
    description: '报告保留天数',
    group: 'report',
  },
  {
    key: 'awr_push_enabled',
    label: '每日推送开关',
    type: 'bool',
    default: 'false',
    description: '每日推送开关',
    group: 'push',
  },
  {
    key: 'awr_push_receiver',
    label: '推送接收人',
    type: 'text',
    default: '',
    description: '推送接收人 username（逗号分隔）',
    group: 'push',
    placeholder: '多个用逗号分隔，如 alice,bob',
  },
]

const SPACE_DEFS: FieldDef[] = [
  {
    key: 'pg_space_enabled',
    label: '定时采集开关',
    type: 'bool',
    default: 'true',
    description: '关闭后不再按 Cron 自动采集；手动采集不受影响',
  },
  {
    key: 'pg_space_daily_cron',
    label: '表空间每日采集 Cron',
    type: 'cron',
    default: '0 0 3 * * *',
    description:
      '6 段（秒 分 时 日 月 周）。例：0 0 3 * * * = 每天 03:00:00。每天定点须写成「0 分 时 * * *」，勿写成每小时表达式。改后约 30 秒生效',
    placeholder: '0 0 3 * * *',
  },
  {
    key: 'pg_space_retention_days',
    label: '快照保留天数',
    type: 'number',
    default: '90',
    description: '超过天数的快照将被清理',
  },
]

const SMART_DEFS: FieldDef[] = [
  {
    key: 'sql_insight_analysis_cron_enabled',
    label: '定时分析开关',
    type: 'bool',
    default: 'false',
    description: '关闭后不再按 Cron 自动触发智能分析；手动触发不受影响',
  },
  {
    key: 'sql_insight_analysis_cron',
    label: '每日分析 Cron',
    type: 'cron',
    default: '0 0 3 * * *',
    description:
      '6 段（秒 分 时 日 月 周）。建议晚于 AWR 日任务。例：0 0 3 * * * = 每天 03:00:00',
    placeholder: '0 0 3 * * *',
  },
]

const GROUP_TITLES: Record<AwrGroup, string> = {
  prometheus: 'Prometheus 连接',
  report: '报告生成',
  push: '每日推送',
}
const GROUP_ORDER: AwrGroup[] = ['prometheus', 'report', 'push']

type FormValues = Record<string, string | number | boolean>

function toBool(v: string | undefined): boolean {
  const s = (v ?? '').trim().toLowerCase()
  return s === 'true' || s === '1' || s === 'on' || s === 'yes' || s === '开启'
}

function toFormValues(values: Record<string, string>, defs: FieldDef[]): FormValues {
  const out: FormValues = {}
  defs.forEach((d) => {
    const raw = values[d.key] ?? d.default
    if (d.type === 'bool') out[d.key] = toBool(raw)
    else if (d.type === 'number') out[d.key] = Number(raw) || 0
    else out[d.key] = raw
  })
  return out
}

function toStringValues(form: FormValues, defs: FieldDef[]): Record<string, string> {
  const out: Record<string, string> = {}
  defs.forEach((d) => {
    const v = form[d.key]
    if (d.type === 'bool') out[d.key] = v ? 'true' : 'false'
    else if (d.type === 'number') out[d.key] = String(v ?? d.default)
    else out[d.key] = String(v ?? '').trim()
  })
  return out
}

function renderField(def: FieldDef) {
  switch (def.type) {
    case 'bool':
      return (
        <Form.Item key={def.key} name={def.key} label={def.label} valuePropName="checked" extra={def.description}>
          <Switch />
        </Form.Item>
      )
    case 'number':
      return (
        <Form.Item
          key={def.key}
          name={def.key}
          label={def.label}
          extra={def.description}
          rules={[{ required: true, message: `请输入${def.label}` }]}
        >
          <InputNumber min={1} style={{ width: '100%' }} />
        </Form.Item>
      )
    case 'password':
      return (
        <Form.Item key={def.key} name={def.key} label={def.label} extra={def.description}>
          <Input.Password placeholder={def.placeholder} allowClear autoComplete="new-password" />
        </Form.Item>
      )
    case 'url':
      return (
        <Form.Item
          key={def.key}
          name={def.key}
          label={def.label}
          extra={def.description}
          rules={[{ type: 'url', warningOnly: true, message: '建议填写含 http/https 的地址' }]}
        >
          <Input placeholder={def.placeholder} allowClear />
        </Form.Item>
      )
    case 'cron':
      return (
        <Form.Item
          key={def.key}
          name={def.key}
          label={def.label}
          extra={def.description}
          rules={[
            { required: true, message: '请输入 Cron 表达式' },
            {
              validator: (_, value: string) =>
                !value || value.trim().split(/\s+/).length === 6
                  ? Promise.resolve()
                  : Promise.reject(new Error('Cron 需为 6 段（含秒）')),
            },
          ]}
        >
          <Input placeholder={def.placeholder} allowClear />
        </Form.Item>
      )
    default:
      return (
        <Form.Item key={def.key} name={def.key} label={def.label} extra={def.description}>
          <Input placeholder={def.placeholder} allowClear />
        </Form.Item>
      )
  }
}

export default function SqlInsightSettings() {
  const [params, setParams] = useSearchParams()
  const tabParam = params.get('tab')
  const activeTab: SettingTab =
    tabParam === 'space' || tabParam === 'smart' || tabParam === 'awr' ? tabParam : 'awr'

  const [awrForm] = Form.useForm<FormValues>()
  const [spaceForm] = Form.useForm<FormValues>()
  const [smartForm] = Form.useForm<FormValues>()
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [knownKeys, setKnownKeys] = useState<Set<string>>(new Set())

  const allDefs = useMemo(() => [...AWR_DEFS, ...SPACE_DEFS, ...SMART_DEFS], [])

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/global-args')
      const rows: { key?: string; value?: string }[] = data.data || []
      const map: Record<string, string> = {}
      const keys = new Set<string>()
      rows.forEach((r) => {
        if (!r.key) return
        map[r.key] = r.value ?? ''
        keys.add(r.key)
      })
      setKnownKeys(keys)
      awrForm.setFieldsValue(toFormValues(map, AWR_DEFS))
      spaceForm.setFieldsValue(toFormValues(map, SPACE_DEFS))
      smartForm.setFieldsValue(toFormValues(map, SMART_DEFS))
    } finally {
      setLoading(false)
    }
  }

  async function save() {
    const [awrValues, spaceValues, smartValues] = await Promise.all([
      awrForm.validateFields(),
      spaceForm.validateFields(),
      smartForm.validateFields(),
    ])
    const payload = {
      ...toStringValues(awrValues, AWR_DEFS),
      ...toStringValues(spaceValues, SPACE_DEFS),
      ...toStringValues(smartValues, SMART_DEFS),
    }
    setSaving(true)
    try {
      for (const def of allDefs) {
        const body = {
          key: def.key,
          value: payload[def.key] ?? def.default,
          description: def.description,
        }
        if (knownKeys.has(def.key)) {
          await api.put('/global-args', body)
        } else {
          await api.post('/global-args', body)
        }
      }
      message.success('已保存（Cron 约 30 秒内热更新）')
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
    <div className="ops-page sql-insight-settings">
      <div className="settings-head">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            SQL 洞察设置
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            配置 AWR、表空间定时采集与智能分析日任务。底层写入全局参数，改后约 30 秒热更新。
          </Typography.Paragraph>
        </div>
        <Space>
          <Button onClick={() => load()} loading={loading}>
            刷新
          </Button>
          <Auth urn={Perm.args.edit}>
            <Button type="primary" onClick={save} loading={saving}>
              保存
            </Button>
          </Auth>
        </Space>
      </div>

      <Tabs
        activeKey={activeTab}
        onChange={(k) => setParams(k === 'awr' ? {} : { tab: k })}
        items={[
          {
            key: 'awr',
            label: 'AWR 报告',
            children: (
              <Form form={awrForm} layout="vertical" className="settings-form" disabled={loading}>
                {GROUP_ORDER.map((g) => {
                  const defs = AWR_DEFS.filter((d) => d.group === g)
                  return (
                    <div key={g} className="settings-group">
                      <Divider orientation="left">{GROUP_TITLES[g]}</Divider>
                      {defs.map(renderField)}
                    </div>
                  )
                })}
              </Form>
            ),
          },
          {
            key: 'space',
            label: '表空间分析',
            children: (
              <Form form={spaceForm} layout="vertical" className="settings-form" disabled={loading}>
                <Typography.Paragraph type="secondary">
                  配置表空间定时采集与快照保留。手动采集不受此处开关影响；改后约 30 秒热更新生效。
                </Typography.Paragraph>
                {SPACE_DEFS.map(renderField)}
              </Form>
            ),
          },
          {
            key: 'smart',
            label: '智能分析',
            children: (
              <Form form={smartForm} layout="vertical" className="settings-form" disabled={loading}>
                <Typography.Paragraph type="secondary">
                  配置每日自动智能分析。手动触发不受此处开关影响；建议 Cron 晚于 AWR 日任务。
                </Typography.Paragraph>
                {SMART_DEFS.map(renderField)}
              </Form>
            ),
          },
        ]}
      />
    </div>
  )
}
