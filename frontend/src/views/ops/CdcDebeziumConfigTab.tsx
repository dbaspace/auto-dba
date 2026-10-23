import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Drawer, Input, Modal, Pagination, Space, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import {
  buildSideBySideLines,
  configToPrettyJson,
  formatOperationType,
  isSensitiveConfigKey,
  operationTypeTagColor,
  pipelineStateText,
} from './cdc'
import './CdcDebeziumConfigTab.css'

interface ConfigRow {
  id: string
  key: string
  value: string
  isNew: boolean
}

let configRowSeq = 0
function nextConfigRowId() {
  configRowSeq += 1
  return `cfg-${configRowSeq}`
}

interface Props {
  taskId: string
  extra: any
  pending?: boolean
  onUpdated?: () => void
  /** Connector 在 Connect 上缺失时打开重新初始化弹窗 */
  onReinit?: () => void
  /** 详情切走时保持挂载；为 false 时不重新拉配置，避免盖掉未保存编辑 */
  active?: boolean
}

type EditableVariant = 'input' | 'password' | 'textarea'

/** 本地草稿受控，停顿或失焦才写回父级 rows，避免整表重渲染把光标打到末尾。 */
const EditableConfigCell = React.memo(function EditableConfigCell({
  rowId,
  value,
  variant,
  placeholder,
  onCommit,
  pasteAsValue,
}: {
  rowId: string
  value: string
  variant: EditableVariant
  placeholder?: string
  onCommit: (rowId: string, text: string) => void
  pasteAsValue?: boolean
}) {
  const [draft, setDraft] = useState(value)
  const draftRef = useRef(value)
  const lastCommittedRef = useRef(value)
  const timer = useRef<number>(0)

  useEffect(() => {
    if (value !== lastCommittedRef.current) {
      lastCommittedRef.current = value
      draftRef.current = value
      setDraft(value)
    }
  }, [value])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const commitNow = (v: string) => {
    window.clearTimeout(timer.current)
    lastCommittedRef.current = v
    onCommit(rowId, v)
  }
  const handleChange = (v: string) => {
    draftRef.current = v
    setDraft(v)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => commitNow(draftRef.current), 200)
  }
  const handleBlur = () => {
    window.clearTimeout(timer.current)
    if (draftRef.current !== lastCommittedRef.current) commitNow(draftRef.current)
  }
  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData?.getData('text')
    if (text == null) return
    e.preventDefault()
    const next = text.replace(/\r?\n/g, '').trim()
    draftRef.current = next
    setDraft(next)
    commitNow(next)
  }

  if (variant === 'textarea') {
    return (
      <Input.TextArea
        size="small"
        autoSize={{ minRows: 2, maxRows: 6 }}
        value={draft}
        placeholder={placeholder}
        onChange={(e) => handleChange(e.target.value)}
        onBlur={handleBlur}
      />
    )
  }
  if (variant === 'password') {
    return (
      <Input.Password
        size="small"
        value={draft}
        placeholder={placeholder}
        onChange={(e) => handleChange(e.target.value)}
        onBlur={handleBlur}
      />
    )
  }
  return (
    <Input
      size="small"
      value={draft}
      placeholder={placeholder}
      onChange={(e) => handleChange(e.target.value)}
      onBlur={handleBlur}
      onPaste={pasteAsValue ? handlePaste : undefined}
    />
  )
})

function stateColor(s: string): string {
  const u = String(s || '').toUpperCase()
  if (u === 'RUNNING') return 'success'
  if (u === 'PAUSED' || u === 'UNASSIGNED') return 'warning'
  if (u === 'FAILED' || u === 'NOT_FOUND') return 'error'
  return 'default'
}
function fmtTime(t: any): string {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return String(t).replace('T', ' ').slice(0, 19)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
function configToRows(cfg: Record<string, any>): ConfigRow[] {
  return Object.entries(cfg || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ id: nextConfigRowId(), key, value: String(value ?? ''), isNew: false }))
}
function rowsToConfig(list: ConfigRow[]): Record<string, any> {
  const out: Record<string, any> = {}
  for (const r of list || []) {
    const k = String(r.key || '').trim()
    if (k) out[k] = r.value ?? ''
  }
  return out
}
function isLong(row: ConfigRow) {
  return String(row.value || '').length > 80 || /\.list$/.test(row.key || '')
}

export default function CdcDebeziumConfigTab({ taskId, extra, pending = false, onUpdated, onReinit, active = true }: Props) {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState(false)
  const [detail, setDetail] = useState<any>({})
  const [rows, setRows] = useState<ConfigRow[]>([])
  const [keyword, setKeyword] = useState('')
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [jsonOpen, setJsonOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [versions, setVersions] = useState<any[]>([])
  const [verLoading, setVerLoading] = useState(false)
  const [verPage, setVerPage] = useState(1)
  const [verSize, setVerSize] = useState(10)
  const [verTotal, setVerTotal] = useState(0)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailRows, setDetailRows] = useState<ConfigRow[]>([])
  const [diffOpen, setDiffOpen] = useState(false)
  const [diffVersionLabel, setDiffVersionLabel] = useState('')
  const [diffCurrentJson, setDiffCurrentJson] = useState('')
  const [diffHistoricalJson, setDiffHistoricalJson] = useState('')
  const diffLeftRef = useRef<HTMLDivElement>(null)
  const diffRightRef = useRef<HTMLDivElement>(null)
  const diffScrollLock = useRef(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [preview, setPreview] = useState<any>(null)
  const [restoreTarget, setRestoreTarget] = useState<any>(null)
  const [restoring, setRestoring] = useState(false)
  const editingRef = useRef(false)
  const tableWrapRef = useRef<HTMLDivElement>(null)
  const scrollToNewRef = useRef(false)

  function setEditingState(v: boolean) {
    editingRef.current = v
    setEditing(v)
  }

  const filteredRows = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return rows
    return rows.filter(
      (r) => String(r.key).toLowerCase().includes(q) || String(r.value).toLowerCase().includes(q),
    )
  }, [rows, keyword])
  const prettyJSON = useMemo(() => JSON.stringify(rowsToConfig(rows), null, 2), [rows])
  const diffLines = useMemo(
    () => buildSideBySideLines(diffCurrentJson, diffHistoricalJson),
    [diffCurrentJson, diffHistoricalJson],
  )
  const missing = !!detail.connector_missing

  function syncDiffScroll(from: 'left' | 'right') {
    if (diffScrollLock.current) return
    const src = from === 'left' ? diffLeftRef.current : diffRightRef.current
    const dst = from === 'left' ? diffRightRef.current : diffLeftRef.current
    if (!src || !dst) return
    diffScrollLock.current = true
    dst.scrollTop = src.scrollTop
    dst.scrollLeft = src.scrollLeft
    requestAnimationFrame(() => {
      diffScrollLock.current = false
    })
  }

  async function load() {
    if (!taskId) return
    setLoading(true)
    try {
      const { data } = await api.get(`/cdc-sync/tasks/${taskId}/connector`)
      const d = data.data || {}
      setDetail(d)
      setRows(configToRows(d.config || {}))
      setEditingState(false)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!active || editingRef.current) return
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, active])

  function cancelEdit() {
    setEditingState(false)
    setRows(configToRows(detail.config || {}))
  }
  function addRow() {
    setKeyword('')
    scrollToNewRef.current = true
    setRows((prev) => [...prev, { id: nextConfigRowId(), key: '', value: '', isNew: true }])
  }

  useEffect(() => {
    if (!scrollToNewRef.current) return
    const id = requestAnimationFrame(() => {
      scrollToNewRef.current = false
      const body = tableWrapRef.current?.querySelector('.ant-table-body')
      if (body instanceof HTMLElement) body.scrollTop = body.scrollHeight
    })
    return () => cancelAnimationFrame(id)
  }, [rows])
  function removeRow(id: string) {
    setRows((prev) => prev.filter((r) => r.id !== id))
  }
  const commitKey = useCallback((id: string, text: string) => {
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, key: text } : r)))
  }, [])
  const commitValue = useCallback((id: string, text: string) => {
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, value: text } : r)))
  }, [])
  async function copyKey(row: ConfigRow) {
    const text = String(row.key || '')
    if (!text) {
      message.warning('这一行没有 key')
      return
    }
    try {
      await navigator.clipboard.writeText(text)
      message.success('已复制 key')
    } catch {
      message.error('复制失败')
    }
  }

  async function copyJSON() {
    try {
      await navigator.clipboard.writeText(prettyJSON)
      message.success(`已复制 ${rows.length} 项配置`)
    } catch {
      message.error('复制失败')
    }
  }

  async function save() {
    setSaving(true)
    try {
      const { data } = await api.put(`/cdc-sync/tasks/${taskId}/connector`, { config: rowsToConfig(rows) })
      if (data?.restart_error) message.warning(String(data.message || data.restart_error))
      else message.success(String(data?.message || '已保存并写入版本历史'))
      await load()
      onUpdated?.()
    } catch (e: any) {
      message.error(e.response?.data?.message || e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function loadVersions(page = verPage, size = verSize) {
    setVerLoading(true)
    try {
      const { data } = await api.get(`/cdc-sync/tasks/${taskId}/connector/versions`, {
        params: { current: page, page_size: size },
      })
      setVersions(data.data?.list || [])
      setVerTotal(data.data?.pagination?.total || 0)
    } catch {
      setVersions([])
      setVerTotal(0)
    } finally {
      setVerLoading(false)
    }
  }

  async function fetchVersion(row: any) {
    const { data } = await api.get(`/cdc-sync/tasks/${taskId}/connector/versions/${row.id}`)
    return data.data
  }
  async function showDetail(row: any) {
    const d = await fetchVersion(row)
    setDetailRows(configToRows(d?.config || {}))
    setDetailOpen(true)
  }
  async function showDiff(row: any) {
    const d = await fetchVersion(row)
    const hist = d?.config || {}
    const cur = rowsToConfig(rows)
    const ver = row.version || row.version_no || d?.version || d?.version_no || ''
    setDiffVersionLabel(ver ? String(ver) : '')
    setDiffCurrentJson(configToPrettyJson(cur))
    setDiffHistoricalJson(configToPrettyJson(hist))
    setDiffOpen(true)
  }
  async function beginRestore(row: any) {
    if (!row?.id) return
    setRestoreTarget(row)
    const { data } = await api.post(
      `/cdc-sync/tasks/${taskId}/connector/versions/${row.id}/restore/preview`,
      {},
    )
    setPreview(data.data)
    setPreviewOpen(true)
  }
  async function confirmRestore() {
    if (!restoreTarget?.id) return
    setRestoring(true)
    try {
      await api.post(`/cdc-sync/tasks/${taskId}/connector/versions/${restoreTarget.id}/restore`, {
        sync_pipeline: true,
      })
      message.success('已恢复并覆盖当前配置')
      setPreviewOpen(false)
      setHistoryOpen(false)
      await load()
      onUpdated?.()
    } catch (e: any) {
      message.error(e.response?.data?.message || e.message || '恢复失败')
    } finally {
      setRestoring(false)
    }
  }

  const cfgColumns: ColumnsType<ConfigRow> = [
    {
      title: 'Key',
      key: 'key',
      render: (_, row) =>
        editing && row.isNew ? (
          <EditableConfigCell
            rowId={row.id}
            value={row.key}
            variant="input"
            placeholder="配置项"
            pasteAsValue
            onCommit={commitKey}
          />
        ) : (
          <code>{row.key}</code>
        ),
    },
    {
      title: 'Value',
      key: 'value',
      render: (_, row) =>
        editing ? (
          <EditableConfigCell
            rowId={row.id}
            value={row.value}
            variant={isLong(row) ? 'textarea' : isSensitiveConfigKey(row.key) ? 'password' : 'input'}
            onCommit={commitValue}
          />
        ) : (
          <span className="dbz-val">
            {isSensitiveConfigKey(row.key) && !revealed[row.key] ? '••••••••' : row.value}
            {isSensitiveConfigKey(row.key) && (
              <Button
                type="link"
                size="small"
                onClick={() => setRevealed((r) => ({ ...r, [row.key]: !r[row.key] }))}
              >
                {revealed[row.key] ? '隐藏' : '显示'}
              </Button>
            )}
          </span>
        ),
    },
    {
      title: '操作',
      key: 'op',
      width: editing ? 150 : 90,
      render: (_: any, row: ConfigRow) => (
        <Space size={0}>
          <Button type="link" size="small" onClick={() => void copyKey(row)}>
            复制 key
          </Button>
          {editing ? (
            <Button type="link" size="small" danger onClick={() => removeRow(row.id)}>
              删除
            </Button>
          ) : null}
        </Space>
      ),
    },
  ]

  const verColumns: ColumnsType<any> = [
    { title: '版本', width: 80, render: (_, r) => <Tag color="blue">v{r.version || r.version_no}</Tag> },
    { title: '保存时间', width: 180, render: (_, r) => fmtTime(r.created_at) },
    { title: '操作人', dataIndex: 'operator', width: 120 },
    {
      title: '操作类型',
      width: 100,
      render: (_, r) => (
        <Tag color={operationTypeTagColor(r.change_source || r.operation_id)}>
          {formatOperationType(r.change_source || r.operation_id)}
        </Tag>
      ),
    },
    { title: '操作来源', width: 130, render: (_, r) => r.operator_ip || '—' },
    { title: '来源', width: 120, render: (_, r) => <Tag>{r.change_source || r.operation_id || '—'}</Tag> },
    { title: '配置项', dataIndex: 'config_key_count', width: 80 },
    {
      title: '操作',
      width: 220,
      fixed: 'right',
      render: (_, r) => (
        <Space size={4}>
          <Button type="link" size="small" onClick={() => showDetail(r)}>
            查看
          </Button>
          <Button type="link" size="small" onClick={() => showDiff(r)}>
            对比
          </Button>
          <Button type="link" size="small" danger disabled={missing} onClick={() => beginRestore(r)}>
            恢复
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="dbz-tab" ref={tableWrapRef}>
      <div className="dbz-summary">
        <div className="dbz-summary-title">
          <strong>{detail.name || extra.connector_name || '—'}</strong>
          {detail.state && <Tag color={stateColor(detail.state)}>{pipelineStateText(detail.state)}</Tag>}
          {(detail.debezium_mode || extra.debezium_mode) && (
            <Tag color="blue" bordered={false}>
              模式：{detail.debezium_mode || extra.debezium_mode}
            </Tag>
          )}
        </div>
        <div className="dbz-summary-meta">
          <span>配置项：{rows.length}</span>
          <span>模板：{detail.template || extra.debezium_template || '—'}</span>
        </div>
      </div>
      {missing && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={detail.warning || 'Kafka Connect 上未找到该 Connector，可通过“重新初始化”补齐'}
          description={
            (detail.missing_connectors || []).length
              ? `缺失：${detail.missing_connectors.join('、')}（Connect：${detail.connect_base_url || '—'}）`
              : undefined
          }
          action={
            detail.can_reinit && onReinit ? (
              <Button size="small" type="primary" onClick={onReinit}>
                重新初始化
              </Button>
            ) : undefined
          }
        />
      )}

      <div className="dbz-toolbar">
        <Input
          allowClear
          size="small"
          placeholder="搜索 key 或 value"
          className="dbz-search"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
        />
        <div className="dbz-actions">
          <Button size="small" disabled={editing} onClick={load}>
            刷新
          </Button>
          <Button size="small" onClick={copyJSON}>
            复制
          </Button>
          <Button size="small" onClick={() => setJsonOpen(true)}>
            JSON视图
          </Button>
          {!editing ? (
            <Button size="small" type="primary" disabled={pending || missing} onClick={() => setEditingState(true)}>
              编辑
            </Button>
          ) : (
            <>
              <Button size="small" onClick={cancelEdit}>
                取消
              </Button>
              <Button size="small" type="primary" loading={saving} onClick={save}>
                保存
              </Button>
            </>
          )}
          <Button
            size="small"
            onClick={() => {
              setHistoryOpen(true)
              loadVersions(1, verSize)
              setVerPage(1)
            }}
          >
            配置版本历史
          </Button>
        </div>
      </div>

      <ResponsiveTable
        rowKey="id"
        loading={loading}
        size="small"
        columns={cfgColumns}
        dataSource={filteredRows}
        pagination={false}
        scroll={{ x: 'max-content', y: 520 }}
        locale={{ emptyText: '暂无配置' }}
      />
      {editing && (
        <div className="dbz-add">
          <Button size="small" onClick={addRow}>
            新增配置项
          </Button>
        </div>
      )}

      <Drawer title="配置版本历史" width={960} open={historyOpen} onClose={() => setHistoryOpen(false)} destroyOnClose>
        <Button className="mb" disabled={versions.length < 2 || missing} onClick={() => beginRestore(versions[1])}>
          恢复上一版
        </Button>
        <ResponsiveTable
          rowKey="id"
          loading={verLoading}
          size="small"
          columns={verColumns}
          dataSource={versions}
          pagination={false}
          scroll={{ x: 1100 }}
          locale={{ emptyText: '暂无历史版本' }}
        />
        <div className="pager">
          共 {verTotal} 条
          <Pagination
            size="small"
            current={verPage}
            pageSize={verSize}
            total={verTotal}
            pageSizeOptions={['10', '20', '50']}
            showSizeChanger
            onChange={(p, s) => {
              setVerPage(p)
              setVerSize(s)
              loadVersions(p, s)
            }}
          />
        </div>
      </Drawer>

      <Modal title="JSON 视图" open={jsonOpen} footer={null} width={720} onCancel={() => setJsonOpen(false)}>
        <pre className="cfg-pre">{prettyJSON}</pre>
      </Modal>

      <Modal title="版本配置" open={detailOpen} footer={null} width={720} onCancel={() => setDetailOpen(false)}>
        <ResponsiveTable
          rowKey="key"
          size="small"
          dataSource={detailRows}
          pagination={false}
          scroll={{ x: 'max-content', y: 420 }}
          columns={[
            { title: 'Key', dataIndex: 'key' },
            { title: 'Value', dataIndex: 'value' },
          ]}
        />
      </Modal>

      <Modal
        title={diffVersionLabel ? `配置对比 — ${diffVersionLabel}` : '配置对比'}
        open={diffOpen}
        footer={null}
        width={960}
        destroyOnClose
        onCancel={() => setDiffOpen(false)}
      >
        <div className="dbz-diff-note">
          【注】左侧为当前运行配置，右侧为选中历史版本（敏感字段已脱敏）
        </div>
        <div className="dbz-diff-panes">
          <div className="dbz-diff-pane" ref={diffLeftRef} onScroll={() => syncDiffScroll('left')}>
            {diffLines.map((line, idx) => (
              <div key={`L${idx}`} className={`dbz-diff-line dbz-diff-${line.kind}`}>
                <span className="dbz-diff-gutter">{line.leftNo || ''}</span>
                <span className="dbz-diff-text">{line.leftText || '\u00a0'}</span>
              </div>
            ))}
          </div>
          <div className="dbz-diff-pane" ref={diffRightRef} onScroll={() => syncDiffScroll('right')}>
            {diffLines.map((line, idx) => (
              <div key={`R${idx}`} className={`dbz-diff-line dbz-diff-${line.kind}`}>
                <span className="dbz-diff-gutter">{line.rightNo || ''}</span>
                <span className="dbz-diff-text">{line.rightText || '\u00a0'}</span>
              </div>
            ))}
          </div>
        </div>
      </Modal>

      <Modal
        title="恢复预览"
        open={previewOpen}
        width={640}
        onCancel={() => setPreviewOpen(false)}
        okText="确认恢复并覆盖"
        cancelText="取消"
        confirmLoading={restoring}
        onOk={confirmRestore}
      >
        {preview && (
          <div className="preview-body">
            <p>
              将覆盖当前 Connector 配置为 <strong>v{preview.version}</strong>。
            </p>
            {!!preview.table_include_add?.length && <p>将加回表：{preview.table_include_add.join('、')}</p>}
            {!!preview.table_include_drop?.length && <p>将移除表：{preview.table_include_drop.join('、')}</p>}
            <p>
              配置变更：新增 {preview.config_diff?.added?.length || 0}，删除 {preview.config_diff?.removed?.length || 0}
              ，修改 {preview.config_diff?.changed?.length || 0}
            </p>
          </div>
        )}
      </Modal>
    </div>
  )
}
