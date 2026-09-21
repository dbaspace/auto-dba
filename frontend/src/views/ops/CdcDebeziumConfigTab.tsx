import React, { useEffect, useMemo, useState } from 'react'
import { Button, Drawer, Input, Modal, Pagination, Space, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import { formatOperationType, isSensitiveConfigKey, operationTypeTagColor } from './cdc'
import './CdcDebeziumConfigTab.css'

interface ConfigRow {
  key: string
  value: string
  isNew: boolean
}

interface Props {
  taskId: string
  extra: any
  pending?: boolean
  onUpdated?: () => void
}

function stateColor(s: string): string {
  const u = String(s || '').toUpperCase()
  if (u === 'RUNNING') return 'success'
  if (u === 'PAUSED' || u === 'UNASSIGNED') return 'warning'
  if (u === 'FAILED') return 'error'
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
    .map(([key, value]) => ({ key, value: String(value ?? ''), isNew: false }))
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

export default function CdcDebeziumConfigTab({ taskId, extra, pending = false, onUpdated }: Props) {
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
  const [diffRows, setDiffRows] = useState<any[]>([])
  const [previewOpen, setPreviewOpen] = useState(false)
  const [preview, setPreview] = useState<any>(null)
  const [restoreTarget, setRestoreTarget] = useState<any>(null)
  const [restoring, setRestoring] = useState(false)

  const filteredRows = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return rows
    return rows.filter(
      (r) => String(r.key).toLowerCase().includes(q) || String(r.value).toLowerCase().includes(q),
    )
  }, [rows, keyword])
  const prettyJSON = useMemo(() => JSON.stringify(rowsToConfig(rows), null, 2), [rows])

  async function load() {
    if (!taskId) return
    setLoading(true)
    try {
      const { data } = await api.get(`/cdc-sync/tasks/${taskId}/connector`)
      const d = data.data || {}
      setDetail(d)
      setRows(configToRows(d.config || {}))
      setEditing(false)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId])

  function cancelEdit() {
    setEditing(false)
    setRows(configToRows(detail.config || {}))
  }
  function addRow() {
    setRows((prev) => [...prev, { key: '', value: '', isNew: true }])
  }
  function removeRow(row: ConfigRow) {
    setRows((prev) => prev.filter((r) => r !== row))
  }
  function updateRow(row: ConfigRow, patch: Partial<ConfigRow>) {
    setRows((prev) => prev.map((r) => (r === row ? { ...r, ...patch } : r)))
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
      await api.put(`/cdc-sync/tasks/${taskId}/connector`, { config: rowsToConfig(rows) })
      message.success('已保存并写入版本历史')
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
    const keys = new Set([...Object.keys(cur), ...Object.keys(hist)])
    setDiffRows(
      [...keys]
        .sort()
        .map((key) => {
          const c = cur[key]
          const h = hist[key]
          let kind = '相同'
          if (c == null) kind = '删除'
          else if (h == null) kind = '新增'
          else if (c !== h) kind = '修改'
          return { key, kind, current: c ?? '—', historical: h ?? '—' }
        })
        .filter((r) => r.kind !== '相同'),
    )
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
          <Input
            size="small"
            placeholder="配置项"
            value={row.key}
            onChange={(e) => updateRow(row, { key: e.target.value })}
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
          isLong(row) ? (
            <Input.TextArea
              size="small"
              autoSize={{ minRows: 2, maxRows: 6 }}
              value={row.value}
              onChange={(e) => updateRow(row, { value: e.target.value })}
            />
          ) : isSensitiveConfigKey(row.key) ? (
            <Input.Password size="small" value={row.value} onChange={(e) => updateRow(row, { value: e.target.value })} />
          ) : (
            <Input size="small" value={row.value} onChange={(e) => updateRow(row, { value: e.target.value })} />
          )
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
    ...(editing
      ? [
          {
            title: '操作',
            key: 'op',
            width: 80,
            render: (_: any, row: ConfigRow) => (
              <Button type="link" danger onClick={() => removeRow(row)}>
                删除
              </Button>
            ),
          } as any,
        ]
      : []),
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
          <Button type="link" size="small" danger onClick={() => beginRestore(r)}>
            恢复
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="dbz-tab">
      <div className="dbz-summary">
        <div className="dbz-summary-title">
          <strong>{detail.name || extra.connector_name || '—'}</strong>
          {detail.state && <Tag color={stateColor(detail.state)}>{detail.state}</Tag>}
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
            <Button size="small" type="primary" disabled={pending} onClick={() => setEditing(true)}>
              保存
            </Button>
          ) : (
            <>
              <Button size="small" onClick={cancelEdit}>
                取消
              </Button>
              <Button size="small" type="primary" loading={saving} onClick={save}>
                确认保存
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

      <Table
        rowKey={(r) => r.key + (r.isNew ? '__new' : '')}
        loading={loading}
        size="small"
        columns={cfgColumns}
        dataSource={filteredRows}
        pagination={false}
        scroll={{ y: 520 }}
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
        <Button className="mb" disabled={versions.length < 2} onClick={() => beginRestore(versions[1])}>
          恢复上一版
        </Button>
        <Table
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
        <Table
          rowKey="key"
          size="small"
          dataSource={detailRows}
          pagination={false}
          scroll={{ y: 420 }}
          columns={[
            { title: 'Key', dataIndex: 'key' },
            { title: 'Value', dataIndex: 'value' },
          ]}
        />
      </Modal>

      <Modal title="与当前配置对比" open={diffOpen} footer={null} width={860} onCancel={() => setDiffOpen(false)}>
        <Table
          rowKey="key"
          size="small"
          dataSource={diffRows}
          pagination={false}
          scroll={{ y: 420 }}
          columns={[
            { title: 'Key', dataIndex: 'key', width: 200 },
            { title: '变化', dataIndex: 'kind', width: 80 },
            { title: '当前', dataIndex: 'current' },
            { title: '历史版本', dataIndex: 'historical' },
          ]}
        />
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
