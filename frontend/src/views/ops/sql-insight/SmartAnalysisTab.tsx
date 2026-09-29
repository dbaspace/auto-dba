import React, { useCallback, useEffect, useState } from 'react'
import {
  Button,
  Checkbox,
  DatePicker,
  Drawer,
  Empty,
  Form,
  Input,
  Modal,
  Segmented,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import ResponsiveTable from '../../../components/ResponsiveTable'
import Auth from '../../../components/Auth'
import { Perm } from '../../../hooks/usePermission'
import { PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import dayjs, { Dayjs } from 'dayjs'
import type { SnapshotFilter } from './types'
import {
  SCENARIO_TAG_OPTS,
  createAnalysisRun,
  feedbackFinding,
  fetchAnalysisFindings,
  fetchAnalysisMetrics,
  fetchAnalysisRules,
  fetchAnalysisRun,
  fetchAnalysisRuns,
} from './services/smartApi'
import { asArray } from './services/asArray'

const { Text, Paragraph, Title } = Typography
const { TextArea } = Input

type ViewKey = 'runs' | 'metrics' | 'rules'

function parseDeep(raw: any): any {
  if (!raw) return null
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw)
    } catch {
      return null
    }
  }
  return raw
}

export default function SmartAnalysisTab(props: { filter: SnapshotFilter; active: boolean }) {
  const { filter, active } = props
  const [view, setView] = useState<ViewKey>('runs')
  const [loading, setLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const [rows, setRows] = useState<any[]>([])
  const [runModal, setRunModal] = useState(false)
  const [runDate, setRunDate] = useState<Dayjs>(() => dayjs().subtract(1, 'day'))
  const [scenario, setScenario] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [detailOpen, setDetailOpen] = useState(false)
  const [runDetail, setRunDetail] = useState<any>(null)
  const [findings, setFindings] = useState<any[]>([])
  const [findingDeep, setFindingDeep] = useState<any>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      if (view === 'runs') {
        const data = await fetchAnalysisRuns({
          host: filter.host || undefined,
          port: filter.port || undefined,
          current: 1,
          page_size: 50,
        })
        setRows(asArray(data?.data))
      } else if (view === 'metrics') {
        setRows(asArray(await fetchAnalysisMetrics()))
      } else {
        setRows(asArray(await fetchAnalysisRules()))
      }
    } catch (e: any) {
      if (e?.response?.status !== 404) {
        message.error(e?.response?.data?.message || e?.message || '加载失败')
      }
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [view, filter.host, filter.port])

  useEffect(() => {
    if (!active) return
    void load()
  }, [active, load])

  async function onRun() {
    setRunning(true)
    try {
      await createAnalysisRun({
        date: runDate.format('YYYY-MM-DD'),
        host: filter.host || undefined,
        port: filter.port || undefined,
        user_scenario: scenario || undefined,
        scenario_tags: tags.length ? tags : undefined,
      })
      message.success('已触发分析')
      setRunModal(false)
      setView('runs')
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '触发失败（需先有 AWR 日报告）')
    } finally {
      setRunning(false)
    }
  }

  async function openRun(runId: string) {
    setDetailOpen(true)
    try {
      const [detail, findingsRes] = await Promise.all([
        fetchAnalysisRun(runId),
        fetchAnalysisFindings({ run_id: runId, current: 1, page_size: 100 }),
      ])
      setRunDetail(detail)
      setFindings(asArray(findingsRes?.data))
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载运行详情失败')
      setRunDetail(null)
      setFindings([])
    }
  }

  async function onFeedback(id: string, feedback: 'confirm' | 'dismiss') {
    try {
      await feedbackFinding(id, { feedback })
      message.success(feedback === 'confirm' ? '已确认' : '已忽略')
      if (runDetail?.id) await openRun(runDetail.id)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '反馈失败')
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <Segmented
          value={view}
          onChange={(v) => setView(v as ViewKey)}
          options={[
            { label: '分析运行', value: 'runs' },
            { label: '指标管理', value: 'metrics' },
            { label: '规则管理', value: 'rules' },
          ]}
        />
        <Space>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>
            刷新
          </Button>
          {view === 'runs' ? (
            <Auth urn={Perm.sqlInsight.analyze}>
              <Button
                type="primary"
                icon={<PlayCircleOutlined />}
                onClick={() => {
                  setRunDate(filter.endTime ? dayjs(filter.endTime) : dayjs().subtract(1, 'day'))
                  setRunModal(true)
                }}
              >
                手动分析
              </Button>
            </Auth>
          ) : null}
        </Space>
      </div>

      {view === 'runs' ? (
        <ResponsiveTable
          size="small"
          loading={loading}
          rowKey={(r) => String(r.id)}
          dataSource={rows}
          locale={{ emptyText: <Empty description="暂无运行记录。生成 AWR 后可手动分析。" /> }}
          columns={[
            { title: '分析日', dataIndex: 'analysis_date', width: 120 },
            { title: '状态', dataIndex: 'status', width: 100 },
            {
              title: '严重度',
              dataIndex: 'severity',
              width: 100,
              render: (v: string) =>
                v ? <Tag color={v === 'critical' ? 'error' : v === 'warning' ? 'warning' : 'default'}>{v}</Tag> : '-',
            },
            { title: '发现数', dataIndex: 'finding_count', width: 90 },
            {
              title: '场景',
              dataIndex: 'user_scenario',
              ellipsis: true,
              render: (v: string, r: any) => v || (r.scenario_tags || []).join(',') || '-',
            },
            {
              title: '操作',
              width: 100,
              render: (_: any, row: any) => (
                <Button type="link" size="small" onClick={() => void openRun(row.id)}>
                  详情
                </Button>
              ),
            },
          ]}
          pagination={false}
        />
      ) : null}

      {view === 'metrics' ? (
        <ResponsiveTable
          size="small"
          loading={loading}
          rowKey={(r) => String(r.id || r.metric_key)}
          dataSource={rows}
          columns={[
            { title: '指标键', dataIndex: 'metric_key', width: 180 },
            { title: '来源', dataIndex: 'source', width: 100 },
            { title: '字段', dataIndex: 'builtin_field', width: 160 },
            { title: '启用', dataIndex: 'enabled', width: 80, render: (v: any) => (v ? '是' : '否') },
          ]}
          pagination={false}
        />
      ) : null}

      {view === 'rules' ? (
        <ResponsiveTable
          size="small"
          loading={loading}
          rowKey={(r) => String(r.id)}
          dataSource={rows}
          columns={[
            { title: '名称', dataIndex: 'name', width: 160 },
            { title: '类型', dataIndex: 'rule_kind', width: 100 },
            { title: '方向', dataIndex: 'direction', width: 100 },
            { title: '启用', dataIndex: 'enabled', width: 80, render: (v: any) => (v ? '是' : '否') },
          ]}
          pagination={false}
        />
      ) : null}

      <Modal
        title="触发智能分析"
        open={runModal}
        onCancel={() => setRunModal(false)}
        onOk={() => void onRun()}
        confirmLoading={running}
        destroyOnClose
      >
        <Form layout="vertical">
          <Form.Item label="分析日" extra="须与该实例 AWR 报告日一致；默认昨天">
            <DatePicker value={runDate} onChange={(d) => d && setRunDate(d)} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="业务场景">
            <TextArea
              rows={3}
              maxLength={1000}
              value={scenario}
              onChange={(e) => setScenario(e.target.value)}
              placeholder="例如：今晚大促预热，连接池已临时调大，预计 TPS 上升属正常"
            />
          </Form.Item>
          <Form.Item label="场景标签">
            <Checkbox.Group
              options={SCENARIO_TAG_OPTS}
              value={tags}
              onChange={(v) => setTags(v as string[])}
            />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer open={detailOpen} title="分析运行详情" width={720} onClose={() => setDetailOpen(false)}>
        {runDetail ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Space wrap>
              <Tag>{runDetail.status}</Tag>
              <Tag color={runDetail.severity === 'critical' ? 'error' : 'default'}>{runDetail.severity || '-'}</Tag>
              <Text type="secondary">分析日 {runDetail.analysis_date}</Text>
            </Space>
            {runDetail.user_scenario ? (
              <Paragraph>
                <Text strong>场景：</Text>
                {runDetail.user_scenario}
              </Paragraph>
            ) : null}
            {(runDetail.scenario_tags || []).length ? (
              <Space wrap>
                {(runDetail.scenario_tags || []).map((t: string) => (
                  <Tag key={t}>{SCENARIO_TAG_OPTS.find((o) => o.value === t)?.label || t}</Tag>
                ))}
              </Space>
            ) : null}
            {runDetail.narrative || runDetail.summary ? (
              <Paragraph>
                <Title level={5}>结论</Title>
                {runDetail.narrative || runDetail.summary}
              </Paragraph>
            ) : null}
            <Title level={5}>Findings</Title>
            <ResponsiveTable
              size="small"
              rowKey={(r) => String(r.id)}
              dataSource={findings}
              pagination={false}
              expandable={{
                expandedRowRender: (r) => {
                  const deep = parseDeep(r.deep_analysis || r.evidence?.deep_analysis)
                  if (!deep) return <Text type="secondary">无深度分析</Text>
                  return (
                    <div style={{ fontSize: 13 }}>
                      {deep.diagnosis || deep.summary ? <Paragraph>{deep.diagnosis || deep.summary}</Paragraph> : null}
                      {deep.suggestion || deep.actions ? (
                        <Paragraph type="secondary">建议：{deep.suggestion || JSON.stringify(deep.actions)}</Paragraph>
                      ) : null}
                      <Button size="small" type="link" onClick={() => setFindingDeep(deep)}>
                        查看完整 deep_analysis
                      </Button>
                    </div>
                  )
                },
              }}
              columns={[
                { title: '标题', dataIndex: 'title', width: 180 },
                { title: '严重度', dataIndex: 'severity', width: 90 },
                { title: '说明', dataIndex: 'message', ellipsis: true },
                {
                  title: '反馈',
                  width: 140,
                  render: (_: any, r: any) => (
                    <Space>
                      <Button size="small" type="link" onClick={() => void onFeedback(r.id, 'confirm')}>
                        确认
                      </Button>
                      <Button size="small" type="link" onClick={() => void onFeedback(r.id, 'dismiss')}>
                        忽略
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </div>
        ) : (
          <Empty />
        )}
      </Drawer>

      <Drawer open={!!findingDeep} title="深度分析" width={520} onClose={() => setFindingDeep(null)}>
        <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(findingDeep, null, 2)}</pre>
      </Drawer>
    </div>
  )
}
