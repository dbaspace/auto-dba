import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Checkbox, Modal, Space, Spin, Switch, Table, Tag, Tooltip, message,
} from 'antd'
import api from '../../api/request'

export type ConfKeyChange = { key: string; op: 'add' | 'del' | 'change'; old?: string; new?: string }
export type ConfLine = { op: 'equal' | 'add' | 'del'; old_no?: number; new_no?: number; text: string }
export type ConfDrift = {
  detected: boolean; keys?: ConfKeyChange[]; applied_at?: string; change_id?: string; revision_id?: string
}
export type ConfDiffResp = {
  base_label: string; base_content: string; target_content: string
  keys: ConfKeyChange[]; lines: ConfLine[]; added: number; deleted: number
  drift?: ConfDrift | null
}
/** 一次下发（配置下发 / 配置回退）在某节点上的「修改前 → 修改后」。 */
export type ConfHistoryItem = {
  change_id: string; change_type: string; created_at: string; created_by: string
  before_revision_id: string; after_revision_id: string
  keys: ConfKeyChange[]; added: number; deleted: number
}
/** conf-patch 预演结果：同角色批量下发计划。 */
export type ConfPatchPlan = {
  role: string; base_label: string; changes: ConfKeyChange[]; local_keys: string[]
  nodes: { node_id: string; host: string; role: string; source: boolean; content: string }[]
}
type NodeBrief = { node_id: string; host: string; role: string }

const CHANGE_TYPE: Record<string, string> = { config_apply: '配置下发', config_rollback: '配置回退' }

const KEY_OP: Record<string, { label: string; color: string }> = {
  add: { label: '新增', color: 'green' },
  del: { label: '删除', color: 'red' },
  change: { label: '修改', color: 'gold' },
}

const MONO: React.CSSProperties = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const errMsg = (e: any): string => e?.response?.data?.message || e?.message || String(e)

function fmtTime(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function keySummary(keys: ConfKeyChange[], max = 3): string {
  if (!keys.length) return '参数无变化'
  return `${keys.length} 项：${keys.slice(0, max).map((k) => k.key).join(', ')}${keys.length > max ? ' …' : ''}`
}

export async function fetchConfDiff(
  clusterId: string, nodeId: string,
  body: { base: 'remote' | 'applied'; content?: string },
): Promise<ConfDiffResp> {
  const res = await api.post(`/sr-cluster/clusters/${clusterId}/nodes/${nodeId}/conf-diff`, body)
  return res.data?.data
}

/** 远端优先；读不到远端（旧版 Agent 等）时退回最近一次下发。 */
async function fetchDiffVsCurrent(clusterId: string, nodeId: string, content?: string): Promise<{ diff: ConfDiffResp; warn?: string }> {
  try {
    return { diff: await fetchConfDiff(clusterId, nodeId, { base: 'remote', content }) }
  } catch (e) {
    const warn = `读取远端失败，改为与最近一次下发对比：${errMsg(e)}`
    return { diff: await fetchConfDiff(clusterId, nodeId, { base: 'applied', content }), warn }
  }
}

export function KeyDiffTable({ keys }: { keys: ConfKeyChange[] }) {
  if (!keys.length) return <div style={{ color: '#8c8c8c', margin: '4px 0 8px' }}>参数无变化（仅注释 / 空行 / 顺序差异不计）</div>
  return (
    <Table
      size="small"
      rowKey="key"
      pagination={false}
      dataSource={keys}
      scroll={{ y: 180 }}
      style={{ marginBottom: 8 }}
      columns={[
        { title: '参数', dataIndex: 'key', width: 220, render: (k: string) => <span style={MONO}>{k}</span> },
        { title: '变化', dataIndex: 'op', width: 64, render: (op: string) => <Tag color={KEY_OP[op]?.color}>{KEY_OP[op]?.label || op}</Tag> },
        { title: '修改前', dataIndex: 'old', ellipsis: true, render: (v?: string) => <span style={{ ...MONO, color: '#cf1322' }}>{v ?? ''}</span> },
        { title: '修改后', dataIndex: 'new', ellipsis: true, render: (v?: string) => <span style={{ ...MONO, color: '#389e0d' }}>{v ?? ''}</span> },
      ]}
    />
  )
}

const LINE_STYLE: Record<string, React.CSSProperties> = {
  add: { background: '#f6ffed', color: '#237804' },
  del: { background: '#fff1f0', color: '#a8071a' },
  equal: {},
}

export function LineDiffView({ lines, maxHeight = 360 }: { lines: ConfLine[]; maxHeight?: number }) {
  const [onlyChanges, setOnlyChanges] = useState(true)
  const CONTEXT = 3
  const rows = useMemo(() => {
    if (!onlyChanges) return lines.map((l, i) => ({ kind: 'line' as const, line: l, key: `l${i}` }))
    const show = new Array(lines.length).fill(false)
    lines.forEach((l, i) => {
      if (l.op === 'equal') return
      for (let k = Math.max(0, i - CONTEXT); k <= Math.min(lines.length - 1, i + CONTEXT); k++) show[k] = true
    })
    const out: ({ kind: 'line'; line: ConfLine; key: string } | { kind: 'gap'; count: number; key: string })[] = []
    let gap = 0
    lines.forEach((l, i) => {
      if (show[i]) {
        if (gap) out.push({ kind: 'gap', count: gap, key: `g${i}` })
        gap = 0
        out.push({ kind: 'line', line: l, key: `l${i}` })
      } else {
        gap++
      }
    })
    if (gap) out.push({ kind: 'gap', count: gap, key: 'gend' })
    return out
  }, [lines, onlyChanges])

  return (
    <div>
      <div style={{ marginBottom: 6, display: 'flex', alignItems: 'center', gap: 8 }}>
        <Switch size="small" checked={onlyChanges} onChange={setOnlyChanges} />
        <span style={{ color: '#595959' }}>只看变更附近（上下 {CONTEXT} 行）</span>
      </div>
      <div style={{ ...MONO, border: '1px solid #f0f0f0', borderRadius: 6, maxHeight, overflow: 'auto', lineHeight: '20px' }}>
        {rows.length === 0 && <div style={{ padding: 8, color: '#8c8c8c' }}>两侧内容为空</div>}
        {rows.map((r) => (r.kind === 'gap' ? (
          <div key={r.key} style={{ padding: '0 8px', background: '#fafafa', color: '#8c8c8c' }}>··· 省略 {r.count} 行相同内容 ···</div>
        ) : (
          <div key={r.key} style={{ display: 'flex', whiteSpace: 'pre', ...LINE_STYLE[r.line.op] }}>
            <span style={{ width: 40, flexShrink: 0, textAlign: 'right', paddingRight: 6, color: '#bfbfbf', userSelect: 'none' }}>{r.line.old_no || ''}</span>
            <span style={{ width: 40, flexShrink: 0, textAlign: 'right', paddingRight: 6, color: '#bfbfbf', userSelect: 'none' }}>{r.line.new_no || ''}</span>
            <span style={{ width: 16, flexShrink: 0, userSelect: 'none' }}>{r.line.op === 'add' ? '+' : r.line.op === 'del' ? '-' : ' '}</span>
            <span style={{ paddingRight: 8 }}>{r.line.text}</span>
          </div>
        )))}
      </div>
    </div>
  )
}

export function ConfDiffView({ diff, baseLabel, targetLabel = '下发后', maxHeight }: {
  diff: ConfDiffResp; baseLabel?: string; targetLabel?: string; maxHeight?: number
}) {
  const same = diff.added === 0 && diff.deleted === 0
  return (
    <div>
      <Space wrap style={{ marginBottom: 8 }}>
        <span><b>{baseLabel || diff.base_label}</b> → <b>{targetLabel}</b></span>
        {same ? <Tag>内容一致</Tag> : (
          <>
            <Tag color="green">+{diff.added} 行</Tag>
            <Tag color="red">-{diff.deleted} 行</Tag>
            <Tag color="gold">参数变化 {diff.keys.length}</Tag>
          </>
        )}
      </Space>
      <KeyDiffTable keys={diff.keys || []} />
      <LineDiffView lines={diff.lines || []} maxHeight={maxHeight} />
    </div>
  )
}

/** 远端相对最近一次平台下发被手工改过时提示。 */
export function DriftAlert({ drift, style }: { drift?: ConfDrift | null; style?: React.CSSProperties }) {
  if (!drift?.detected) return null
  const keys = drift.keys || []
  return (
    <Alert
      type="error"
      showIcon
      style={style}
      message={`远端配置在平台最近一次下发（${fmtTime(drift.applied_at)}）之后被手工修改：${keys.length} 个参数不同；再次下发会覆盖这些改动`}
      description={(
        <div style={MONO}>
          {keys.slice(0, 6).map((k) => (
            <div key={k.key}>
              {k.key}: {k.op === 'add' ? `（新增）${k.new}` : k.op === 'del' ? `${k.old}（已删除）` : `${k.old} → ${k.new}`}
            </div>
          ))}
          {keys.length > 6 && <div>… 另有 {keys.length - 6} 项</div>}
        </div>
      )}
    />
  )
}

/** 查看某次下发的「修改前 → 修改后」。 */
function ApplyDiffModal({ clusterId, item, onClose }: { clusterId: string; item: ConfHistoryItem | null; onClose: () => void }) {
  const [diff, setDiff] = useState<ConfDiffResp | null>(null)
  const [after, setAfter] = useState('')
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!item) return
    setDiff(null)
    setAfter('')
    setLoading(true)
    const req = item.before_revision_id
      ? api.get(`/sr-cluster/clusters/${clusterId}/conf-revision-diff`, { params: { from: item.before_revision_id, to: item.after_revision_id } })
        .then((res) => setDiff(res.data?.data || null))
      : api.get(`/sr-cluster/clusters/${clusterId}/conf-revisions/${item.after_revision_id}`)
        .then((res) => setAfter(res.data?.data?.content || ''))
    req.catch((e) => message.error(errMsg(e))).finally(() => setLoading(false))
  }, [clusterId, item])
  return (
    <Modal
      title={item ? `查看修改 · ${CHANGE_TYPE[item.change_type] || item.change_type || '下发'} · ${fmtTime(item.created_at)}` : ''}
      open={!!item}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
      width={900}
      destroyOnClose
    >
      <Spin spinning={loading}>
        {diff && <ConfDiffView diff={diff} baseLabel="修改前" targetLabel="修改后" maxHeight={420} />}
        {!diff && item && !item.before_revision_id && (
          <>
            <Alert type="info" showIcon style={{ marginBottom: 8 }} message="这次下发没有记录修改前的文件（首次下发或当时 Agent 版本过低），只能查看修改后内容。" />
            <pre style={{ ...MONO, maxHeight: 460, overflow: 'auto', background: '#fafafa', padding: 12, borderRadius: 6, margin: 0 }}>{after}</pre>
          </>
        )}
      </Spin>
    </Modal>
  )
}

/** 回退到某次下发的修改前：展示「当前 → 回退后」差异，可选重启。 */
function RollbackModal({
  clusterId, node, item, onCancel, onDone,
}: {
  clusterId: string; node: NodeBrief; item: ConfHistoryItem | null
  onCancel: () => void; onDone: (changeId: string) => void
}) {
  const [diff, setDiff] = useState<ConfDiffResp | null>(null)
  const [warn, setWarn] = useState('')
  const [loading, setLoading] = useState(false)
  const [restart, setRestart] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!item?.before_revision_id) return
    setDiff(null)
    setWarn('')
    setRestart(false)
    setLoading(true)
    ;(async () => {
      try {
        const res = await api.get(`/sr-cluster/clusters/${clusterId}/conf-revisions/${item.before_revision_id}`)
        const r = await fetchDiffVsCurrent(clusterId, node.node_id, res.data?.data?.content || '')
        setDiff(r.diff)
        setWarn(r.warn || '')
      } catch (e) {
        setWarn(errMsg(e))
      } finally {
        setLoading(false)
      }
    })()
  }, [clusterId, node.node_id, item])

  return (
    <Modal
      title="回退到修改前"
      open={!!item}
      width={900}
      onCancel={onCancel}
      destroyOnClose
      okText="确认回退并下发"
      okButtonProps={{ danger: true, loading: busy }}
      onOk={async () => {
        if (!item) return
        setBusy(true)
        try {
          const res = await api.post(`/sr-cluster/clusters/${clusterId}/conf-rollback`, {
            node_id: node.node_id, revision_id: item.before_revision_id, restart,
          })
          message.success('已提交配置回退')
          onDone(res.data?.data?.change_id)
        } catch (e) {
          message.error(errMsg(e))
        } finally {
          setBusy(false)
        }
      }}
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 12 }}
        message={`节点 ${node.role} · ${node.host} 将恢复为 ${item ? fmtTime(item.created_at) : ''} 这次下发之前的配置并重新下发；覆盖前远端自动备份。数据目录项仍按平台锁定值下发。`}
      />
      {warn && <Alert type="info" showIcon message={warn} style={{ marginBottom: 8 }} />}
      <Spin spinning={loading}>
        {diff ? <ConfDiffView diff={diff} baseLabel="当前" targetLabel="回退后" maxHeight={320} /> : <div style={{ minHeight: 80 }} />}
      </Spin>
      <Checkbox style={{ marginTop: 12 }} checked={restart} onChange={(e) => setRestart(e.target.checked)}>
        下发后重启该节点（多数参数需重启生效）
      </Checkbox>
    </Modal>
  )
}

/** 「下发记录」页签：每次下发一行，可查看修改、回退到修改前。 */
export function ConfHistoryPanel({
  clusterId, node, active, onRolledBack,
}: {
  clusterId: string; node: NodeBrief; active: boolean; onRolledBack: (changeId: string) => void
}) {
  const [list, setList] = useState<ConfHistoryItem[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [viewing, setViewing] = useState<ConfHistoryItem | null>(null)
  const [rollingBack, setRollingBack] = useState<ConfHistoryItem | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setErr('')
    try {
      const res = await api.get(`/sr-cluster/clusters/${clusterId}/nodes/${node.node_id}/conf-history`)
      setList(res.data?.data || [])
    } catch (e) {
      setList([])
      setErr(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [clusterId, node.node_id])

  useEffect(() => {
    if (active) void load()
  }, [active, load])

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Button size="small" onClick={() => void load()} loading={loading}>刷新</Button>
        <span style={{ color: '#8c8c8c' }}>该节点每次下发一行；修改前内容由 Agent 在覆盖前回传。</span>
      </Space>
      {err && <Alert type="warning" showIcon message={err} style={{ marginBottom: 8 }} />}
      <Table
        size="small"
        rowKey={(r) => r.after_revision_id}
        loading={loading}
        dataSource={list}
        pagination={{ pageSize: 10, size: 'small' }}
        locale={{ emptyText: '暂无下发记录' }}
        columns={[
          { title: '时间', dataIndex: 'created_at', width: 150, render: (v: string) => fmtTime(v) },
          {
            title: '类型',
            dataIndex: 'change_type',
            width: 88,
            render: (t: string) => <Tag color={t === 'config_rollback' ? 'orange' : 'blue'}>{CHANGE_TYPE[t] || t || '下发'}</Tag>,
          },
          { title: '操作人', dataIndex: 'created_by', width: 90, ellipsis: true },
          {
            title: '改动',
            ellipsis: { showTitle: true },
            render: (_: unknown, r: ConfHistoryItem) => (r.before_revision_id
              ? <span style={MONO}>{keySummary(r.keys)}</span>
              : <span style={{ color: '#8c8c8c' }}>无修改前记录</span>),
          },
          {
            title: '操作',
            width: 170,
            render: (_: unknown, r: ConfHistoryItem) => (
              <Space size={0}>
                <Button type="link" size="small" onClick={() => setViewing(r)}>查看修改</Button>
                <Tooltip title={r.before_revision_id ? '' : '这次下发没有记录修改前的文件（首次下发或当时 Agent 版本过低）'}>
                  <Button type="link" size="small" danger disabled={!r.before_revision_id} onClick={() => setRollingBack(r)}>
                    回退到修改前
                  </Button>
                </Tooltip>
              </Space>
            ),
          },
        ]}
      />
      <ApplyDiffModal clusterId={clusterId} item={viewing} onClose={() => setViewing(null)} />
      <RollbackModal
        clusterId={clusterId}
        node={node}
        item={rollingBack}
        onCancel={() => setRollingBack(null)}
        onDone={(id) => { setRollingBack(null); onRolledBack(id) }}
      />
    </div>
  )
}

type ApplyRow = { loading: boolean; diff?: ConfDiffResp; error?: string }

/**
 * 下发前确认：逐节点「远端当前 → 下发后」差异；无变化节点默认不勾选，远端被手工改过的节点标红。
 * contents 为各节点下发后的可编辑区内容（同角色批量下发预演结果），缺省时用已保存配置。
 */
export function ConfApplyConfirmModal({
  open, clusterId, nodes, contents, summary, restart, onCancel, onSubmit,
}: {
  open: boolean; clusterId: string; nodes: NodeBrief[]; contents?: Record<string, string>
  summary?: React.ReactNode; restart: boolean
  onCancel: () => void; onSubmit: (nodeIds: string[], restart: boolean) => Promise<void>
}) {
  const [rows, setRows] = useState<Record<string, ApplyRow>>({})
  const [picked, setPicked] = useState<string[]>([])
  const [restartLocal, setRestartLocal] = useState(restart)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setRestartLocal(restart)
    setPicked(nodes.map((n) => n.node_id))
    setRows(Object.fromEntries(nodes.map((n) => [n.node_id, { loading: true }])))
    let cancelled = false
    nodes.forEach((n) => {
      fetchConfDiff(clusterId, n.node_id, { base: 'remote', content: contents?.[n.node_id] })
        .then((diff) => {
          if (cancelled) return
          setRows((prev) => ({ ...prev, [n.node_id]: { loading: false, diff } }))
          if (diff.added === 0 && diff.deleted === 0) setPicked((prev) => prev.filter((id) => id !== n.node_id))
        })
        .catch((e) => {
          if (!cancelled) setRows((prev) => ({ ...prev, [n.node_id]: { loading: false, error: errMsg(e) } }))
        })
    })
    return () => { cancelled = true }
    // nodes / contents 每次打开时由父组件重新计算，仅在打开时拉取
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, clusterId])

  const pending = Object.values(rows).some((r) => r.loading)
  const driftCount = nodes.filter((n) => rows[n.node_id]?.diff?.drift?.detected).length

  return (
    <Modal
      title="下发前确认 · 逐节点修改前后"
      open={open}
      width={980}
      onCancel={onCancel}
      destroyOnClose
      footer={(
        <Space wrap>
          <Checkbox checked={restartLocal} onChange={(e) => setRestartLocal(e.target.checked)}>下发后滚动重启勾选节点</Checkbox>
          <Button onClick={onCancel}>取消</Button>
          <Button
            type="primary"
            loading={busy}
            disabled={!picked.length}
            onClick={async () => {
              setBusy(true)
              try {
                await onSubmit(picked, restartLocal)
              } finally {
                setBusy(false)
              }
            }}
          >
            确认下发（{picked.length}）
          </Button>
        </Space>
      )}
    >
      {summary}
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="修改前为远端当前文件。与远端一致的节点默认不勾选；读取远端失败（如 Agent 版本过低）的节点保持勾选，请展开核对。"
      />
      {driftCount > 0 && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={`${driftCount} 个节点的远端配置在平台最近一次下发后被手工修改，继续下发会覆盖这些改动（覆盖前远端自动备份，可在下发记录中回退）`}
        />
      )}
      <Table
        size="small"
        rowKey="node_id"
        pagination={false}
        dataSource={nodes}
        scroll={{ y: 460 }}
        rowSelection={{ selectedRowKeys: picked, onChange: (keys) => setPicked(keys as string[]) }}
        onRow={(n) => ({ style: rows[n.node_id]?.diff?.drift?.detected ? { background: '#fff1f0' } : {} })}
        expandable={{
          rowExpandable: (n) => !!rows[n.node_id]?.diff,
          expandedRowRender: (n) => {
            const d = rows[n.node_id]?.diff
            return d ? (
              <>
                <DriftAlert drift={d.drift} style={{ marginBottom: 8 }} />
                <ConfDiffView diff={d} baseLabel="修改前（远端）" targetLabel="修改后" maxHeight={280} />
              </>
            ) : null
          },
        }}
        columns={[
          { title: '角色', dataIndex: 'role', width: 60, render: (r: string) => <Tag>{r}</Tag> },
          { title: 'Host', dataIndex: 'host', width: 160 },
          {
            title: '修改前 → 修改后',
            render: (_: unknown, n: NodeBrief) => {
              const r = rows[n.node_id]
              if (!r || r.loading) return <Spin size="small" />
              if (r.error) return <Tooltip title={r.error}><Tag color="red">读取远端失败</Tag><span style={{ color: '#8c8c8c' }}>{r.error.slice(0, 40)}</span></Tooltip>
              const d = r.diff!
              if (d.added === 0 && d.deleted === 0) return <Tag>无变化</Tag>
              return <span style={MONO}>{keySummary(d.keys)}</span>
            },
          },
          {
            title: '手工改动',
            width: 120,
            render: (_: unknown, n: NodeBrief) => {
              const d = rows[n.node_id]?.diff
              if (!d) return '—'
              if (!d.drift) return <span style={{ color: '#bfbfbf' }}>无下发记录</span>
              return d.drift.detected
                ? <Tooltip title={(d.drift.keys || []).map((k) => k.key).join(', ')}><Tag color="red">远端被手工修改</Tag></Tooltip>
                : <Tag color="green">无</Tag>
            },
          },
        ]}
      />
      {pending && <div style={{ marginTop: 8, color: '#8c8c8c' }}>正在读取各节点远端配置…</div>}
    </Modal>
  )
}

/** 变更单「回退本次下发」：各节点回到该次下发前的远端原文件。 */
export function ChangeConfRollbackModal({
  clusterId, change, onCancel, onDone,
}: {
  clusterId: string; change: { id: string; type: string; steps?: { step_key: string; title: string }[] } | null
  onCancel: () => void; onDone: (changeId: string) => void
}) {
  const [restart, setRestart] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (change) setRestart(false) }, [change])
  const targets = (change?.steps || []).filter((s) => s.step_key.startsWith('put_conf_'))
  return (
    <Modal
      title="回退本次下发"
      open={!!change}
      onCancel={onCancel}
      destroyOnClose
      okText="确认回退"
      okButtonProps={{ danger: true, loading: busy }}
      onOk={async () => {
        if (!change) return
        setBusy(true)
        try {
          const res = await api.post(`/sr-cluster/clusters/${clusterId}/conf-rollback`, { change_id: change.id, restart })
          message.success('已提交配置回退')
          onDone(res.data?.data?.change_id)
        } catch (e) {
          message.error(errMsg(e))
        } finally {
          setBusy(false)
        }
      }}
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 12 }}
        message="各节点将回到本次下发前的远端原文件（由 Agent 覆盖前回传）后重新下发。未记录修改前文件的节点（首次下发 / 旧版 Agent）会被跳过。"
      />
      {targets.length > 0 && (
        <div style={{ marginBottom: 12 }}>
          <div style={{ color: '#595959', marginBottom: 4 }}>本次下发涉及：</div>
          {targets.map((s) => <div key={s.step_key} style={MONO}>{s.title}</div>)}
        </div>
      )}
      <Checkbox checked={restart} onChange={(e) => setRestart(e.target.checked)}>回退后重启相关节点</Checkbox>
    </Modal>
  )
}
