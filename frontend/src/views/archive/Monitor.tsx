import React, { useEffect, useMemo, useState } from 'react'
import { Button, Card, Col, InputNumber, Input, Modal, Row, Spin, Tag, message } from 'antd'
import api from '../../api/request'
import './archive.css'

const KEY = 'auto-dba-archive-monitor'

interface Rule {
  id: string
  title: string
  rule_type?: string
  threshold: number
  status: string
}
interface AlertItem {
  id: string
  time?: string
  message?: string
  severity?: string
  acked?: boolean
}

function severityText(s: any): string {
  return ({ critical: '严重', warning: '警告', info: '提示' } as Record<string, string>)[s] || '提示'
}
function severityColor(s: any): string {
  return ({ critical: 'error', warning: 'warning', info: 'default' } as Record<string, string>)[s] || 'default'
}

export default function Monitor() {
  const [loading, setLoading] = useState(false)
  const [ov, setOv] = useState<any>({})
  const [rules, setRules] = useState<Rule[]>([])
  const [alerts, setAlerts] = useState<AlertItem[]>([])
  const [ruleOpen, setRuleOpen] = useState(false)
  const [editing, setEditing] = useState<Rule | null>(null)
  const [title, setTitle] = useState('')
  const [threshold, setThreshold] = useState<number>(3)

  const kpis = useMemo(
    () => [
      { label: '运行中', value: ov.running ?? 0, bg: '#1677ff', mark: '▶' },
      { label: '今日完成', value: ov.completed_today ?? 0, bg: '#52c41a', mark: '✓' },
      { label: '今日失败', value: ov.failed_today ?? 0, bg: '#f5222d', mark: '✕' },
    ],
    [ov],
  )

  function ruleTitle(rule: Rule): string {
    if (rule.title && rule.threshold != null) return `${rule.title} > ${rule.threshold}`
    return rule.title || rule.id
  }

  function persist(nextRules: Rule[], nextAlerts: AlertItem[]) {
    localStorage.setItem(KEY, JSON.stringify({ rules: nextRules, alerts: nextAlerts }))
  }

  function openCreate() {
    setEditing(null)
    setTitle('')
    setThreshold(3)
    setRuleOpen(true)
  }
  function openEdit(rule: Rule) {
    setEditing(rule)
    setTitle(rule.title)
    setThreshold(Number(rule.threshold) || 0)
    setRuleOpen(true)
  }

  function saveRule() {
    if (!title.trim()) {
      message.warning('请输入规则名称')
      return
    }
    let nextRules: Rule[]
    if (editing) {
      nextRules = rules.map((r) =>
        r.id === editing.id ? { ...r, title: title.trim(), threshold } : r,
      )
      message.success('规则已更新')
    } else {
      nextRules = [
        ...rules,
        {
          id: String(Date.now()),
          title: title.trim(),
          rule_type: title.trim(),
          threshold,
          status: 'normal',
        },
      ]
      message.success('规则已创建')
    }
    setRules(nextRules)
    persist(nextRules, alerts)
    setRuleOpen(false)
  }

  function removeRule(rule: Rule) {
    Modal.confirm({
      title: '删除规则',
      icon: undefined,
      content: `确定删除「${rule.title || rule.id}」？`,
      okText: '确定',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk() {
        const nextRules = rules.filter((r) => r.id !== rule.id)
        setRules(nextRules)
        persist(nextRules, alerts)
        message.success('规则已删除')
      },
    })
  }

  function ack(alert: AlertItem) {
    const nextAlerts = alerts.map((a) => (a.id === alert.id ? { ...a, acked: true } : a))
    setAlerts(nextAlerts)
    persist(rules, nextAlerts)
    message.success('告警已确认')
  }

  useEffect(() => {
    ;(async () => {
      setLoading(true)
      try {
        const st = (await api.get('/archive/stats')).data.data || {}
        setOv({
          running: st.running,
          completed_today: st.completed_today,
          failed_today: st.failed_today,
        })
        let savedRules: Rule[] = []
        let savedAlerts: AlertItem[] = []
        try {
          const saved = JSON.parse(localStorage.getItem(KEY) || '{}')
          savedRules = saved.rules || []
          savedAlerts = saved.alerts || []
        } catch {
          /* ignore */
        }
        if (
          (st.failed_today || 0) > 0 &&
          !savedAlerts.some((a) => a.message?.includes('今日失败'))
        ) {
          savedAlerts = [
            {
              id: String(Date.now()),
              time: new Date().toLocaleString(),
              message: `今日失败任务 ${st.failed_today} 个`,
              severity: 'warning',
              acked: false,
            },
            ...savedAlerts,
          ].slice(0, 20)
          persist(savedRules, savedAlerts)
        }
        setRules(savedRules)
        setAlerts(savedAlerts)
      } finally {
        setLoading(false)
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <Spin spinning={loading}>
      <div className="archive-shell">
        <div className="gray-page">
          <div className="page-head">
            <h1>监控与告警</h1>
            <p>实时任务状态、性能指标与告警规则。</p>
          </div>

          <Row gutter={16} className="mb">
            {kpis.map((item) => (
              <Col key={item.label} xs={24} sm={8}>
                <div className="kpi-card">
                  <div className="kpi-ico" style={{ background: item.bg }}>
                    {item.mark}
                  </div>
                  <div>
                    <div className="kpi-num">{item.value}</div>
                    <div className="kpi-label">{item.label}</div>
                  </div>
                </div>
              </Col>
            ))}
          </Row>

          <Card
            className="panel mb"
            title={<span className="card-title">告警规则</span>}
            extra={
              <Button type="link" onClick={openCreate}>
                新增规则
              </Button>
            }
          >
            {!rules.length ? (
              <div className="empty">暂无告警规则</div>
            ) : (
              <ul className="rule-list">
                {rules.map((rule) => (
                  <li key={rule.id}>
                    <span className="rule-left">
                      <i
                        className={
                          'dot ' +
                          (rule.status === 'triggered' ? 'dot-danger' : 'dot-ok')
                        }
                      />
                      <span className="rule-title">{ruleTitle(rule)}</span>
                    </span>
                    <span className="rule-ops">
                      <Button type="link" size="small" onClick={() => openEdit(rule)}>
                        编辑
                      </Button>
                      <Button type="link" size="small" danger onClick={() => removeRule(rule)}>
                        删除
                      </Button>
                      <Tag color={rule.status === 'triggered' ? 'error' : 'success'}>
                        {rule.status === 'triggered' ? '已触发' : '正常'}
                      </Tag>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card className="panel" title={<span className="card-title">最近告警</span>}>
            {!alerts.length ? (
              <div className="empty">暂无最近告警</div>
            ) : (
              <ul className="rule-list">
                {alerts.map((alert) => (
                  <li key={alert.id}>
                    <span className="alert-text">
                      {[alert.time, alert.message].filter(Boolean).join(' ')}
                    </span>
                    <span className="rule-ops">
                      {!alert.acked && (
                        <Button type="link" size="small" onClick={() => ack(alert)}>
                          确认
                        </Button>
                      )}
                      <Tag color={severityColor(alert.severity)}>
                        {severityText(alert.severity)}
                      </Tag>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Modal
            open={ruleOpen}
            title={editing ? '编辑规则' : '新增规则'}
            width={480}
            destroyOnClose
            onCancel={() => setRuleOpen(false)}
            footer={[
              <Button key="cancel" onClick={() => setRuleOpen(false)}>
                取消
              </Button>,
              <Button key="ok" type="primary" onClick={saveRule}>
                {editing ? '保存' : '创建'}
              </Button>,
            ]}
          >
            <div style={{ marginBottom: 16 }}>
              <div style={{ marginBottom: 6 }}>
                <span style={{ color: '#ff4d4f', marginRight: 4 }}>*</span>规则名称
              </div>
              <Input
                value={title}
                maxLength={64}
                placeholder="如：任务执行超时"
                onChange={(e) => setTitle(e.target.value)}
              />
            </div>
            <div>
              <div style={{ marginBottom: 6 }}>
                <span style={{ color: '#ff4d4f', marginRight: 4 }}>*</span>阈值
              </div>
              <InputNumber
                min={0}
                style={{ width: '100%' }}
                value={threshold}
                onChange={(v) => setThreshold(Number(v ?? 0))}
              />
            </div>
          </Modal>
        </div>
      </div>
    </Spin>
  )
}
