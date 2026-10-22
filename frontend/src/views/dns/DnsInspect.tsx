import React, { useEffect, useMemo, useState } from 'react'
import { Button, Card, Segmented, Space, Statistic, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { ISSUE_COLOR, ISSUE_TEXT, Issue, errMsg, fmtTime } from './dnsApi'

const CODE_TEXT: Record<string, string> = {
  cname_conflict: 'CNAME 与其它类型同名',
  cname_multi: '多条 CNAME',
  cname_loop: 'CNAME 循环',
  target_is_ip: '目标为 IP',
  target_missing: '目标不存在',
  ttl_low: 'TTL 过低',
  ip_outside_ranges: 'IP 不在解析范围网段',
  dangling_record: '悬空记录',
  instance_domain_missing: '实例域名缺失',
}

export default function DnsInspect() {
  const [issues, setIssues] = useState<Issue[]>([])
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [records, setRecords] = useState(0)
  const [checkedAt, setCheckedAt] = useState(0)
  const [level, setLevel] = useState<string>('all')
  const [loading, setLoading] = useState(false)

  async function run() {
    setLoading(true)
    try {
      const { data } = await api.get('/dns/inspect')
      setIssues(data.data || [])
      setCounts(data.counts || {})
      setRecords(data.records || 0)
      setCheckedAt(data.checked_at || 0)
    } catch (e: any) {
      message.error(errMsg(e, '体检失败'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { run() }, [])

  const shown = useMemo(() => (level === 'all' ? issues : issues.filter((i) => i.level === level)), [issues, level])

  return (
    <Space direction="vertical" style={{ width: '100%' }} size={16}>
      <Card size="small" extra={<Button type="primary" loading={loading} onClick={run}>重新体检</Button>} title={`解析体检（${fmtTime(checkedAt)}）`}>
        <Space size={48}>
          <Statistic title="运行中记录" value={records} />
          <Statistic title="错误" value={counts.error || 0} valueStyle={{ color: '#cf1322' }} />
          <Statistic title="提示" value={counts.warn || 0} valueStyle={{ color: '#d46b08' }} />
          <Statistic title="信息" value={counts.info || 0} valueStyle={{ color: '#1677ff' }} />
        </Space>
        <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 8 }}>
          错误级问题在保存时会被拦截，这里出现说明是历史数据；悬空记录只是提示，可能是非数据库用途的记录。
        </div>
      </Card>
      <Segmented
        value={level}
        onChange={(v) => setLevel(String(v))}
        options={[{ value: 'all', label: '全部' }, { value: 'error', label: '错误' }, { value: 'warn', label: '提示' }, { value: 'info', label: '信息' }]}
      />
      <ResponsiveTable
        rowKey={(r: Issue) => `${r.code}-${r.message}`}
        loading={loading}
        dataSource={shown}
        pagination={{ pageSize: 50 }}
        columns={[
          { title: '级别', dataIndex: 'level', width: 80, render: (v: string) => <Tag color={ISSUE_COLOR[v]}>{ISSUE_TEXT[v] || v}</Tag> },
          { title: '规则', dataIndex: 'code', width: 180, render: (v: string) => CODE_TEXT[v] || v },
          { title: '说明', dataIndex: 'message' },
        ]}
      />
    </Space>
  )
}
