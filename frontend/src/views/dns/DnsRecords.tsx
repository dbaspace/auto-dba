import React, { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Alert, Button, Drawer, Dropdown, Form, Input, InputNumber, Modal, Popconfirm, Radio, Select, Space, Switch, Table, Tag, Tooltip, Upload, message,
} from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import Auth from '../../components/Auth'
import api from '../../api/request'
import { Perm, usePermission } from '../../hooks/usePermission'
import {
  Issue, RECORD_TYPES, Range, Rec, STATUS_OPTIONS, TTLHold, TTL_OPTIONS, Zone,
  downloadExport, errMsg, fmtTime, handleWriteError, handleWriteResult, loadRanges, loadZones, recordValue,
} from './dnsApi'

const IMPORT_ACTION: Record<string, { text: string; color: string }> = {
  create: { text: '新增', color: 'green' },
  update: { text: '修改', color: 'blue' },
  unchanged: { text: '不变', color: 'default' },
  error: { text: '错误', color: 'red' },
  unsupported: { text: '不支持', color: 'orange' },
}

export default function DnsRecords() {
  const navigate = useNavigate()
  const { hasUrn } = usePermission()
  const canEdit = hasUrn(Perm.dns.edit)
  const canDelete = hasUrn(Perm.dns.delete)
  const [zones, setZones] = useState<Zone[]>([])
  const [ranges, setRanges] = useState<Range[]>([])
  const [zoneId, setZoneId] = useState('')
  const [rows, setRows] = useState<Rec[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [filters, setFilters] = useState({ parsing_range_id: '', type: '', status: '', name: '', data: '' })
  const [selected, setSelected] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Rec | null>(null)
  const [viewing, setViewing] = useState<Rec | null>(null)
  const [ttlOpen, setTtlOpen] = useState(false)
  const [holdsOpen, setHoldsOpen] = useState(false)
  const [holds, setHolds] = useState<TTLHold[]>([])
  const [importOpen, setImportOpen] = useState(false)
  const [form] = Form.useForm()
  const [ttlForm] = Form.useForm()

  const zone = zones.find((z) => z.id === zoneId)
  const suffix = useMemo(() => (zone ? `.${zone.origin}` : '.'), [zone])

  async function loadMeta() {
    const [zs, rs] = await Promise.all([loadZones(), loadRanges()])
    setZones(zs)
    setRanges(rs)
    setZoneId((cur) => cur || zs[0]?.id || '')
  }

  async function loadRecords(nextPage = page) {
    if (!zoneId) {
      setRows([])
      setTotal(0)
      return
    }
    setLoading(true)
    try {
      const { data } = await api.get('/dns-records', { params: { zone_id: zoneId, page: nextPage, page_size: 20, ...filters } })
      setRows(data.data || [])
      setTotal(data.total || 0)
      setSelected([])
    } finally {
      setLoading(false)
    }
  }

  async function loadHolds() {
    const { data } = await api.get('/dns/ttl-holds', { params: { status: 'holding' } })
    setHolds(data.data || [])
  }

  useEffect(() => { loadMeta().catch((e) => message.error(errMsg(e, '加载失败'))) }, [])
  useEffect(() => { loadRecords(1).catch((e) => message.error(errMsg(e, '加载记录失败'))) }, [zoneId])

  function openCreate() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ type: 'A', ttl: 600, status: 'Active', zone_id: zoneId, preference: 10 })
    setOpen(true)
  }

  function openEdit(row: Rec) {
    setEditing(row)
    form.setFieldsValue({
      type: row.type,
      names: row.name,
      data: row.type === 'MX' ? row.mail_host : row.data,
      preference: row.preference || 10,
      ttl: row.ttl,
      zone_id: row.zone_id,
      parsing_range_id: row.parsing_range_id,
      status: row.status,
      description: row.description,
      reason: '',
    })
    setOpen(true)
  }

  function bodyOf(r: Rec, status: string) {
    return {
      name: r.name, type: r.type, data: r.type === 'MX' ? r.mail_host : r.data, mail_host: r.mail_host, preference: r.preference,
      ttl: r.ttl, zone_id: r.zone_id, parsing_range_id: r.parsing_range_id, description: r.description, status,
    }
  }

  async function save() {
    const v = await form.validateFields()
    const payload = {
      type: v.type,
      data: v.data,
      mail_host: v.type === 'MX' ? v.data : '',
      preference: v.preference,
      ttl: v.ttl,
      zone_id: v.zone_id,
      parsing_range_id: v.parsing_range_id,
      status: v.status,
      description: v.description || '',
      reason: v.reason || '',
    }
    try {
      let res
      if (editing) {
        res = await api.put(`/dns-records/${editing.id}`, { ...payload, name: String(v.names).trim() })
      } else {
        const names = String(v.names || '').split('\n').map((s) => s.trim()).filter(Boolean)
        res = await api.post('/dns-records', { ...payload, names })
      }
      handleWriteResult(res.data)
      setOpen(false)
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e)
    }
  }

  async function toggle(row: Rec, on: boolean) {
    try {
      const res = await api.put(`/dns-records/${row.id}`, bodyOf(row, on ? 'Active' : 'Removed'))
      handleWriteResult(res.data, on ? '已启用' : '已停用')
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e, '启停失败')
    }
  }

  async function remove(row: Rec) {
    try {
      const res = await api.delete(`/dns-records/${row.id}`)
      handleWriteResult(res.data, '已删除')
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e, '删除失败')
    }
  }

  async function batchRemove() {
    try {
      const res = await api.post('/dns-records/batch-delete', { ids: selected })
      handleWriteResult(res.data, `已删除 ${selected.length} 条`)
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e, '删除失败')
    }
  }

  async function lowerTTL() {
    const v = await ttlForm.validateFields()
    try {
      const res = await api.post('/dns/ttl-holds', { record_ids: selected, ttl: v.ttl, minutes: v.minutes })
      handleWriteResult(res.data, `已预降 ${selected.length} 条记录的 TTL`)
      setTtlOpen(false)
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e, '预降失败')
    }
  }

  async function restoreHold(id: string) {
    try {
      const { data } = await api.post(`/dns/ttl-holds/${id}/restore`)
      message.success(data.status === 'restored' ? '已恢复' : data.status === 'skipped' ? 'TTL 已被修改，未覆盖' : '记录已不存在')
      loadHolds()
      loadRecords(page)
    } catch (e: any) {
      handleWriteError(e, '恢复失败')
    }
  }

  return (
    <div>
      <Space style={{ marginBottom: 12 }} wrap>
        <span>解析域</span>
        <Select
          style={{ width: 220 }}
          value={zoneId || undefined}
          placeholder="请选择解析域"
          options={zones.map((z) => ({ value: z.id, label: z.origin }))}
          onChange={(v) => setZoneId(v)}
        />
        <Auth urn={Perm.dns.create}><Button type="primary" onClick={openCreate} disabled={!zoneId}>新建记录</Button></Auth>
        <Auth urn={Perm.dns.import}><Button onClick={() => setImportOpen(true)}>导入</Button></Auth>
        <Dropdown
          menu={{
            items: [
              { key: 'csv', label: '当前解析域（CSV）' },
              { key: 'zone', label: '当前解析域 + 所选范围（zone 文件）', disabled: !filters.parsing_range_id },
              { key: 'all', label: '全部记录（CSV）' },
            ],
            onClick: ({ key }) => {
              const p: Record<string, string> =
                key === 'all' ? { format: 'csv' }
                  : key === 'zone' ? { format: 'zone', zone_id: zoneId, range_id: filters.parsing_range_id }
                    : { format: 'csv', zone_id: zoneId, range_id: filters.parsing_range_id }
              downloadExport(p).catch((e) => message.error(errMsg(e, '导出失败')))
            },
          }}
        >
          <Button>导出</Button>
        </Dropdown>
        <Auth urn={Perm.dns.edit}>
          <Button disabled={!selected.length} onClick={() => { ttlForm.setFieldsValue({ ttl: 30, minutes: 60 }); setTtlOpen(true) }}>预降 TTL</Button>
        </Auth>
        <Button onClick={() => { loadHolds().catch((e) => message.error(errMsg(e))); setHoldsOpen(true) }}>预降中的记录</Button>
        <Auth urn={Perm.dns.delete}>
          <Popconfirm title={`删除选中的 ${selected.length} 条记录？`} onConfirm={batchRemove} disabled={!selected.length}>
            <Button danger disabled={!selected.length}>批量删除</Button>
          </Popconfirm>
        </Auth>
      </Space>
      <Space style={{ marginBottom: 12 }} wrap>
        <Select allowClear placeholder="解析范围" style={{ width: 160 }}
          options={ranges.map((r) => ({ value: r.id, label: r.name }))}
          onChange={(v) => setFilters({ ...filters, parsing_range_id: v || '' })} />
        <Select allowClear placeholder="类型" style={{ width: 120 }}
          options={RECORD_TYPES.map((t) => ({ value: t, label: t }))}
          onChange={(v) => setFilters({ ...filters, type: v || '' })} />
        <Select allowClear placeholder="状态" style={{ width: 120 }} options={STATUS_OPTIONS}
          onChange={(v) => setFilters({ ...filters, status: v || '' })} />
        <Input allowClear placeholder="主机名" style={{ width: 160 }} onChange={(e) => setFilters({ ...filters, name: e.target.value })} />
        <Input allowClear placeholder="记录值" style={{ width: 160 }} onChange={(e) => setFilters({ ...filters, data: e.target.value })} />
        <Button onClick={() => { setPage(1); loadRecords(1) }}>查询</Button>
      </Space>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        rowSelection={canEdit || canDelete ? { selectedRowKeys: selected, onChange: (keys: React.Key[]) => setSelected(keys.map(String)) } : undefined}
        pagination={{ current: page, pageSize: 20, total, onChange: (p: number) => { setPage(p); loadRecords(p) } }}
        columns={[
          { title: '主机记录', dataIndex: 'name' },
          { title: '类型', dataIndex: 'type', width: 80 },
          { title: '记录值', dataIndex: 'data', render: (_: string, r: Rec) => recordValue(r) },
          {
            title: '解析范围', dataIndex: 'parsing_range_name',
            render: (v: string, r: Rec) => <Tooltip title={(r.network_list || []).join(', ')}>{v}</Tooltip>,
          },
          {
            title: 'TTL', dataIndex: 'ttl', width: 130,
            render: (v: number, r: Rec) => (
              <span>
                {v}s
                {r.ttl_hold && (
                  <Tooltip title={`原 TTL ${r.ttl_hold.orig_ttl}s，预计 ${fmtTime(r.ttl_hold.restore_at)} 恢复`}>
                    <Tag color="orange" style={{ marginLeft: 6 }}>预降中</Tag>
                  </Tooltip>
                )}
              </span>
            ),
          },
          {
            title: '状态', dataIndex: 'status', width: 80,
            render: (v: string, r: Rec) => <Switch size="small" checked={v === 'Active'} disabled={!canEdit} onChange={(on) => toggle(r, on)} />,
          },
          {
            title: '关联实例', dataIndex: 'instances',
            render: (v: Rec['instances']) => (v || []).length
              ? (v || []).map((i) => <Tag key={i.instance_id} color="geekblue">{i.instance_name}</Tag>)
              : <span style={{ color: '#bfbfbf' }}>-</span>,
          },
          { title: '描述', dataIndex: 'description', ellipsis: true },
          {
            title: '操作', width: 220,
            render: (_: unknown, r: Rec) => (
              <Space size={0}>
                <Button type="link" onClick={() => setViewing(r)}>查看</Button>
                <Button type="link" onClick={() => navigate(`/dns/changes?object_id=${r.id}`)}>历史</Button>
                <Auth urn={Perm.dns.edit}><Button type="link" onClick={() => openEdit(r)}>编辑</Button></Auth>
                <Auth urn={Perm.dns.delete}>
                  <Popconfirm title="删除该记录？" onConfirm={() => remove(r)}>
                    <Button type="link" danger>删除</Button>
                  </Popconfirm>
                </Auth>
              </Space>
            ),
          },
        ]}
      />

      <Modal title={editing ? '编辑记录' : '新建域名解析'} open={open} onOk={save} onCancel={() => setOpen(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="type" label="记录类型" rules={[{ required: true }]}>
            <Select options={RECORD_TYPES.map((t) => ({ value: t, label: t }))} />
          </Form.Item>
          <Form.Item name="names" label="主机记录" rules={[{ required: true }]} extra={editing ? undefined : '每行一个；@ 表示解析域本身'}>
            {editing ? <Input addonAfter={suffix} /> : <Input.TextArea rows={4} placeholder={'每行一个\n' + suffix} />}
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.type !== b.type}>
            {() => (
              <>
                <Form.Item name="data" label={form.getFieldValue('type') === 'MX' ? '邮件主机' : '记录值'} rules={[{ required: true }]}>
                  <Input />
                </Form.Item>
                {form.getFieldValue('type') === 'MX' && (
                  <Form.Item name="preference" label="优先级" rules={[{ required: true }]}>
                    <InputNumber min={0} max={65535} />
                  </Form.Item>
                )}
              </>
            )}
          </Form.Item>
          <Form.Item name="ttl" label="TTL" rules={[{ required: true }]}>
            <Select options={TTL_OPTIONS} />
          </Form.Item>
          <Form.Item name="parsing_range_id" label="解析范围" rules={[{ required: true }]}>
            <Select placeholder="请选择解析范围" options={ranges.map((r) => ({ value: r.id, label: r.name }))} />
          </Form.Item>
          <Form.Item name="zone_id" label="解析域" rules={[{ required: true }]}>
            <Select options={zones.map((z) => ({ value: z.id, label: z.origin }))} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]}>
            <Select options={STATUS_OPTIONS} />
          </Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="reason" label="变更原因" extra="写入变更记录；需审批的解析域会作为工单说明"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal title="记录详情" open={!!viewing} footer={null} onCancel={() => setViewing(null)}>
        {viewing && (
          <div>
            <p>主机记录：{viewing.name}{suffix}</p>
            <p>类型：{viewing.type}</p>
            <p>记录值：{recordValue(viewing)}</p>
            <p>解析范围：{viewing.parsing_range_name} {(viewing.network_list || []).join(', ')}</p>
            <p>TTL：{viewing.ttl}s{viewing.ttl_hold ? `（预降中，原 ${viewing.ttl_hold.orig_ttl}s，${fmtTime(viewing.ttl_hold.restore_at)} 恢复）` : ''}</p>
            <p>状态：{viewing.status === 'Active' ? '运行' : '停止'}</p>
            <p>关联实例：{(viewing.instances || []).map((i) => i.instance_name).join('、') || '-'}</p>
            <p>描述：{viewing.description || '-'}</p>
            <p>更新时间：{fmtTime(viewing.updated_time)}</p>
          </div>
        )}
      </Modal>

      <Modal title={`预降 TTL（${selected.length} 条）`} open={ttlOpen} onOk={lowerTTL} onCancel={() => setTtlOpen(false)} destroyOnClose>
        <Alert type="info" showIcon style={{ marginBottom: 12 }}
          message="用于迁移、割接前缩短缓存时间。到期后由「DNS 节点巡检」任务恢复原 TTL；若期间 TTL 被手动修改，则不会覆盖。" />
        <Form form={ttlForm} layout="vertical">
          <Form.Item name="ttl" label="临时 TTL（秒）" rules={[{ required: true }]}>
            <InputNumber min={1} max={3600} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item name="minutes" label="多少分钟后恢复" rules={[{ required: true }]}>
            <InputNumber min={1} max={10080} style={{ width: 200 }} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer title="预降中的记录" open={holdsOpen} width={760} onClose={() => setHoldsOpen(false)}>
        <Table
          size="small"
          rowKey="id"
          dataSource={holds}
          pagination={false}
          columns={[
            { title: '记录', render: (_: unknown, h: TTLHold) => `${h.record_name || h.record_id} ${h.record_type || ''}` },
            { title: '解析域', dataIndex: 'zone_origin' },
            { title: 'TTL', render: (_: unknown, h: TTLHold) => `${h.lowered_ttl}s（原 ${h.orig_ttl}s）` },
            { title: '预计恢复', dataIndex: 'restore_at', render: (v: number) => fmtTime(v) },
            { title: '操作人', dataIndex: 'actor' },
            {
              title: '操作',
              render: (_: unknown, h: TTLHold) => (
                <Auth urn={Perm.dns.edit}>
                  <Popconfirm title="立即恢复原 TTL？" onConfirm={() => restoreHold(h.id)}>
                    <Button type="link">立即恢复</Button>
                  </Popconfirm>
                </Auth>
              ),
            },
          ]}
        />
      </Drawer>

      <ImportModal open={importOpen} ranges={ranges} onClose={() => setImportOpen(false)} onDone={() => { setImportOpen(false); loadRecords(1) }} />
    </div>
  )
}

function ImportModal({ open, ranges, onClose, onDone }: { open: boolean; ranges: Range[]; onClose: () => void; onDone: () => void }) {
  const [format, setFormat] = useState<'csv' | 'zone'>('csv')
  const [content, setContent] = useState('')
  const [fileName, setFileName] = useState('')
  const [rangeName, setRangeName] = useState('')
  const [origin, setOrigin] = useState('')
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<any>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) {
      setContent('')
      setFileName('')
      setPreview(null)
      setReason('')
    }
  }, [open])

  const body = { format, content, range_name: rangeName, origin, reason }

  async function doPreview() {
    setBusy(true)
    try {
      const { data } = await api.post('/dns/import/preview', body)
      setPreview(data)
    } catch (e: any) {
      message.error(errMsg(e, '解析失败'))
    } finally {
      setBusy(false)
    }
  }

  async function doApply() {
    setBusy(true)
    try {
      const { data } = await api.post('/dns/import/apply', body)
      handleWriteResult(data, '导入完成')
      onDone()
    } catch (e: any) {
      handleWriteError(e, '导入失败')
    } finally {
      setBusy(false)
    }
  }

  const counts = preview?.counts || {}
  const issues: Issue[] = preview?.issues || []
  const warnings: Issue[] = preview?.warnings || []
  const canApply = preview && !counts.error && !issues.length && !preview.error && (counts.create || counts.update)

  return (
    <Modal
      title="导入解析记录"
      open={open}
      width={900}
      onCancel={onClose}
      footer={[
        <Button key="c" onClick={onClose}>取消</Button>,
        <Button key="p" onClick={doPreview} loading={busy} disabled={!content || (format === 'zone' && !rangeName)}>预览</Button>,
        <Button key="a" type="primary" onClick={doApply} loading={busy} disabled={!canApply}>确认导入</Button>,
      ]}
    >
      <Space direction="vertical" style={{ width: '100%' }}>
        <Radio.Group value={format} onChange={(e) => { setFormat(e.target.value); setPreview(null) }}>
          <Radio.Button value="csv">CSV</Radio.Button>
          <Radio.Button value="zone">zone 文件</Radio.Button>
        </Radio.Group>
        <div style={{ color: '#8c8c8c', fontSize: 12 }}>
          {format === 'csv'
            ? 'CSV 列：zone,range,name,type,data,ttl,status,description（与导出格式一致）。MX 的 data 写成「优先级 邮件主机」。'
            : 'BIND 格式，支持 $ORIGIN / $TTL 和单行记录；SOA、NS 等不支持的类型会被跳过。'}
          按 域+范围+主机名+类型+值 匹配：不存在则新增，TTL、状态或描述不同则修改；导入不会删除记录。
        </div>
        <Space wrap>
          <Upload
            accept={format === 'csv' ? '.csv,.txt' : '.zone,.txt,.db'}
            maxCount={1}
            showUploadList={false}
            beforeUpload={(file) => {
              file.text().then((t) => { setContent(t); setFileName(file.name); setPreview(null) })
              return false
            }}
          >
            <Button>选择文件</Button>
          </Upload>
          <span>{fileName}</span>
          {format === 'zone' && (
            <>
              <Select placeholder="目标解析范围" style={{ width: 180 }} value={rangeName || undefined}
                options={ranges.map((r) => ({ value: r.name, label: r.name }))} onChange={(v) => { setRangeName(v); setPreview(null) }} />
              <Input placeholder="缺省 $ORIGIN（文件内有则不填）" style={{ width: 240 }} value={origin} onChange={(e) => setOrigin(e.target.value)} />
            </>
          )}
          <Input placeholder="变更原因" style={{ width: 200 }} value={reason} onChange={(e) => setReason(e.target.value)} />
        </Space>
        {preview && (
          <>
            <Space wrap>
              {Object.entries(IMPORT_ACTION).map(([k, v]) => <Tag key={k} color={v.color}>{v.text} {counts[k] || 0}</Tag>)}
            </Space>
            {preview.error && <Alert type="error" showIcon message={preview.error} />}
            {issues.map((i, idx) => <Alert key={'e' + idx} type="error" showIcon message={i.message} />)}
            {warnings.map((i, idx) => <Alert key={'w' + idx} type="warning" showIcon message={i.message} />)}
            <Table
              size="small"
              rowKey={(r: any) => `${r.line}-${r.action}`}
              dataSource={preview.items || []}
              pagination={{ pageSize: 10 }}
              columns={[
                { title: '行', dataIndex: 'line', width: 60 },
                { title: '结果', dataIndex: 'action', width: 80, render: (v: string) => <Tag color={IMPORT_ACTION[v]?.color}>{IMPORT_ACTION[v]?.text || v}</Tag> },
                { title: '域 / 范围', render: (_: unknown, r: any) => `${r.zone || '-'} / ${r.range || '-'}` },
                {
                  title: '记录', render: (_: unknown, r: any) => {
                    const x = r.after || r.before
                    return x ? `${x.name} ${x.type} ${recordValue(x)} TTL ${x.ttl}` : '-'
                  },
                },
                {
                  title: '说明', render: (_: unknown, r: any) => r.message || (r.action === 'update' && r.before && r.after
                    ? `TTL ${r.before.ttl}→${r.after.ttl}；状态 ${r.before.status}→${r.after.status}` : ''),
                },
              ]}
            />
          </>
        )}
        {!preview && content && <Alert type="info" message={`已读取 ${content.split('\n').length} 行，点击「预览」查看对比结果`} />}
      </Space>
    </Modal>
  )
}
