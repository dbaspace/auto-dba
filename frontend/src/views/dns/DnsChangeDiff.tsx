import React from 'react'
import { Table, Tag } from 'antd'
import { ACTION_TEXT, OBJECT_TEXT, recordValue } from './dnsApi'

/** 变更前后对比。before/after 为 ZoneSnap / RangeSnap / RecordSnap，缺省表示不存在。 */
export interface DiffRow {
  key: string
  action: string
  object: string
  summary?: string
  before?: any
  after?: any
}

const FIELD_TEXT: Record<string, string> = {
  origin: '解析域',
  name: '名称',
  type: '类型',
  data: '记录值',
  ttl: 'TTL',
  zone_id: '解析域 ID',
  parsing_range_id: '解析范围 ID',
  network_list: '来源网段',
  description: '描述',
  status: '状态',
}

const HIDDEN = new Set(['id', 'mail_host', 'preference'])

function show(field: string, v: any, obj: any): string {
  if (v === undefined || v === null || v === '') return '-'
  if (field === 'data' && obj?.type === 'MX') return recordValue(obj)
  if (Array.isArray(v)) return v.join(', ')
  if (field === 'status') return v === 'Active' ? '运行' : v === 'Removed' ? '停止' : String(v)
  return String(v)
}

function FieldDiff({ before, after }: { before?: any; after?: any }) {
  const keys = Array.from(new Set([...Object.keys(before || {}), ...Object.keys(after || {})])).filter((k) => !HIDDEN.has(k))
  return (
    <div style={{ fontSize: 12, lineHeight: '20px' }}>
      {keys.map((k) => {
        const b = show(k, before?.[k], before)
        const a = show(k, after?.[k], after)
        const changed = before && after && b !== a
        if (before && after && !changed && k !== 'name' && k !== 'origin' && k !== 'type') return null
        return (
          <div key={k}>
            <span style={{ color: '#8c8c8c' }}>{FIELD_TEXT[k] || k}：</span>
            {before && after ? (
              changed ? (
                <>
                  <span style={{ textDecoration: 'line-through', color: '#cf1322' }}>{b}</span>
                  <span style={{ margin: '0 6px' }}>→</span>
                  <span style={{ color: '#389e0d' }}>{a}</span>
                </>
              ) : (
                <span>{a}</span>
              )
            ) : (
              <span style={{ color: after ? '#389e0d' : '#cf1322' }}>{after ? a : b}</span>
            )}
          </div>
        )
      })}
    </div>
  )
}

export default function DnsChangeDiff({ rows }: { rows: DiffRow[] }) {
  return (
    <Table
      size="small"
      rowKey="key"
      pagination={false}
      dataSource={rows}
      columns={[
        {
          title: '操作',
          width: 120,
          render: (_: unknown, r: DiffRow) => (
            <span>
              <Tag color={r.action === 'delete' ? 'red' : r.action === 'create' ? 'green' : 'blue'}>{ACTION_TEXT[r.action] || r.action}</Tag>
              {OBJECT_TEXT[r.object] || r.object}
            </span>
          ),
        },
        { title: '内容', render: (_: unknown, r: DiffRow) => (r.before || r.after ? <FieldDiff before={r.before} after={r.after} /> : r.summary) },
      ]}
    />
  )
}

/** 工单载荷（sql_content）转为对比行：Expect 为提交时的当前值。 */
export function payloadRows(raw: string): { rows: DiffRow[]; reason: string; kind: string; batchId: string } {
  let p: any = {}
  try {
    p = JSON.parse(raw || '{}')
  } catch {
    return { rows: [], reason: '', kind: '', batchId: '' }
  }
  if (p.kind === 'rollback') {
    const rows = (p.items || []).map((it: any, i: number) => ({ key: String(i), action: it.action, object: it.object, summary: it.summary }))
    return { rows, reason: p.reason || '', kind: 'rollback', batchId: p.batch_id || '' }
  }
  const rows: DiffRow[] = (p.ops || []).map((op: any, i: number) => {
    const expect = op.expect && op.expect !== 'null' ? op.expect : undefined
    return {
      key: String(i),
      action: op.action,
      object: op.object,
      summary: p.items?.[i]?.summary,
      before: op.action === 'create' ? undefined : expect,
      after: op.action === 'delete' ? undefined : op.after,
    }
  })
  return { rows, reason: p.reason || '', kind: 'ops', batchId: '' }
}
