import React, { useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, Modal, Pagination, Radio, Select, Space, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'
import './Connectors.css'

interface ConnectorRow {
  id?: string
  tenant_id?: string
  tenant_name?: string
  dbz_host?: string
  env?: string
  kafka_host?: string
  message_schema_mode?: string
  remark?: string
}

interface EnvRow {
  id?: string
  code?: string
  name?: string
}

interface KafkaRow {
  id?: string
  name?: string
  host?: string
  domain_name?: string
  port?: number
}

function kafkaAddress(i?: KafkaRow) {
  const host = String(i?.host || i?.domain_name || '').trim()
  const port = Number(i?.port)
  if (!host) return ''
  const cleaned = host.replace(/:(\d+):\1$/, ':$1')
  if (/:\d+$/.test(cleaned) || cleaned.includes(',')) return cleaned
  if (Number.isFinite(port) && port > 0) return `${cleaned}:${port}`
  return cleaned
}

export default function Connectors() {
  const user = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((user as { impersonating?: boolean })?.impersonating)
  const currentTenant = (user as { current_tenant?: { id?: string } })?.current_tenant
  const currentTenantId = String(currentTenant?.id || '')
  const [rows, setRows] = useState<ConnectorRow[]>([])
  const [envs, setEnvs] = useState<EnvRow[]>([])
  const [kafkas, setKafkas] = useState<KafkaRow[]>([])
  const [loading, setLoading] = useState(false)
  const [visible, setVisible] = useState(false)
  const [saving, setSaving] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [envFilter, setEnvFilter] = useState<string>()
  const [kafkaFilter, setKafkaFilter] = useState('')
  const [dbzFilter, setDbzFilter] = useState('')
  const [editingId, setEditingId] = useState('')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [tenants, setTenants] = useState<{ id: string; name?: string; code?: string }[]>([])
  const [assignOpen, setAssignOpen] = useState(false)
  const [assignTid, setAssignTid] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [form] = Form.useForm()

  const filteredRows = useMemo(() => {
    const kafka = kafkaFilter.trim().toLowerCase()
    const dbz = dbzFilter.trim().toLowerCase()
    return rows.filter((row) => {
      if (envFilter && row.env !== envFilter) return false
      if (kafka && !String(row.kafka_host || '').toLowerCase().includes(kafka)) return false
      if (dbz && !String(row.dbz_host || '').toLowerCase().includes(dbz)) return false
      return true
    })
  }, [rows, envFilter, kafkaFilter, dbzFilter])

  const pagedRows = useMemo(() => {
    const start = (page - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  const envValue = Form.useWatch('env', form)
  const kafkaValue = Form.useWatch('kafka_host', form)

  const envOptions = useMemo(() => {
    const base = envs.map((e) => ({
      value: e.code,
      label: e.name ? `${e.name} (${e.code})` : e.code,
    }))
    if (envValue && !base.some((o) => o.value === envValue)) {
      base.push({ value: envValue, label: `未在接口列表中: ${envValue}` })
    }
    return base
  }, [envs, envValue])

  const kafkaOptions = useMemo(() => {
    const base = kafkas
      .map((k) => {
        const addr = kafkaAddress(k)
        return { value: addr, label: k.name ? `${k.name} — ${addr}` : addr }
      })
      .filter((o) => o.value)
    if (kafkaValue && !base.some((o) => o.value === kafkaValue)) {
      base.push({ value: kafkaValue, label: `未在资源列表: ${kafkaValue}` })
    }
    return base
  }, [kafkas, kafkaValue])

  function envLabel(code?: string) {
    const hit = envs.find((e) => e.code === code || e.id === code || e.name === code)
    if (hit?.name) return hit.code ? `${hit.name} (${hit.code})` : hit.name
    return code || '—'
  }

  function schemaLabel(v?: string) {
    return v === 'with_schema' ? '带 Schema' : '不带 Schema'
  }

  function kafkaLabel(addr?: string) {
    const hit = kafkas.find((k) => kafkaAddress(k) === addr || k.id === addr || k.name === addr)
    if (hit?.name) return `${hit.name} — ${kafkaAddress(hit) || addr}`
    return addr || '—'
  }

  async function load() {
    setLoading(true)
    try {
      const [connRes, envRes, kafkaRes] = await Promise.all([
        api.get('/connectors'),
        api.get('/environments'),
        api.get('/instances', { params: { db_type: 'kafka' } }),
      ])
      setRows(connRes.data.data || [])
      setEnvs(envRes.data.data || [])
      setKafkas(kafkaRes.data.data || [])
    } finally {
      setLoading(false)
    }
  }

  function open(row?: ConnectorRow) {
    setEditingId(row?.id || '')
    form.setFieldsValue({
      dbz_host: row?.dbz_host || '',
      env: row?.env || undefined,
      kafka_host: row?.kafka_host || undefined,
      message_schema_mode: row?.message_schema_mode === 'with_schema' ? 'with_schema' : 'without_schema',
      remark: row?.remark || '',
    })
    setVisible(true)
  }

  async function save() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = {
        dbz_host: (values.dbz_host || '').trim(),
        env: values.env,
        kafka_host: values.kafka_host,
        message_schema_mode: values.message_schema_mode,
        remark: (values.remark || '').trim(),
      }
      if (editingId) await api.put('/connectors/' + editingId, payload)
      else await api.post('/connectors', payload)
      message.success(editingId ? '编辑成功' : '增加成功')
      setVisible(false)
      await load()
    } finally {
      setSaving(false)
    }
  }

  function remove(row: ConnectorRow) {
    Modal.confirm({
      title: '提示',
      content: '确定删除该条连接器？',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete('/connectors/' + row.id)
        message.success('删除成功')
        setSelectedIds((prev) => prev.filter((id) => id !== row.id))
        await load()
      },
    })
  }

  function canEdit(row: ConnectorRow) {
    if (impersonating) return true
    const owner = String(row.tenant_id || '').trim()
    return !owner || owner === currentTenantId
  }

  async function confirmAssign() {
    const ids = selectedIds.filter(Boolean)
    if (!ids.length || !assignTid) {
      message.warning('请选择目标租户')
      return
    }
    setAssigning(true)
    try {
      const { data } = await api.post('/connectors/assign-tenant', { tenant_id: assignTid, ids })
      const failed: { id?: string; dbz_host?: string; message?: string }[] = data.failed || []
      const failedIds = new Set(failed.map((f) => String(f.id || '')))
      setSelectedIds(ids.filter((id) => failedIds.has(id)))
      const ok = Number(data.ok_count || 0)
      if (failed.length) {
        Modal.error({
          title: ok ? `已划拨 ${ok} 条，失败 ${failed.length} 条` : '划拨失败',
          content: (
            <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
              {failed.map((f) => (
                <li key={f.id || f.dbz_host}>
                  {f.dbz_host || f.id}：{f.message || '失败'}
                </li>
              ))}
            </ul>
          ),
        })
      } else {
        message.success(`已划拨 ${ok} 条`)
        setAssignOpen(false)
      }
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || '划拨失败')
    } finally {
      setAssigning(false)
    }
  }

  useEffect(() => {
    load()
    if (!impersonating) return
    void api.get('/tenants').then(({ data }) => {
      setTenants((data.data || []).filter((t: { status?: string }) => t.status !== 'disabled'))
    }).catch(() => setTenants([]))
  }, [impersonating])

  return (
    <div className="ops-page">
      <div className="ops-title connector-toolbar">
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="环境"
          style={{ width: 180 }}
          value={envFilter}
          options={envOptions.filter((o) => !String(o.label).startsWith('未在接口列表中'))}
          onChange={(v) => {
            setEnvFilter(v)
            setPage(1)
          }}
        />
        <Input
          allowClear
          placeholder="Kafka 地址"
          style={{ width: 200 }}
          value={kafkaFilter}
          onChange={(e) => {
            setKafkaFilter(e.target.value)
            setPage(1)
          }}
        />
        <Input
          allowClear
          placeholder="Debezium 地址"
          style={{ width: 200 }}
          value={dbzFilter}
          onChange={(e) => {
            setDbzFilter(e.target.value)
            setPage(1)
          }}
        />
        <Button type="primary" className="connector-add" onClick={() => open()}>
          增加
        </Button>
        {impersonating ? (
          <Auth urn={Perm.tenants.impersonate}>
            <Button
              disabled={!selectedIds.length}
              loading={assigning}
              onClick={() => {
                setAssignTid(currentTenantId)
                setAssignOpen(true)
              }}
            >
              批量指定租户{selectedIds.length ? `（${selectedIds.length}）` : ''}
            </Button>
          </Auth>
        ) : null}
      </div>
      <p className="hint">
        {impersonating
          ? '租户模式：勾选后「批量指定租户」只改连接器归属，不连带 Kafka 实例。'
          : '维护 Debezium Connect 集群。环境来自「环境管理」，Kafka 地址来自已登记的 Kafka 实例。'}
      </p>
      <div className="ops-table">
        <ResponsiveTable
          rowKey={(r) => r.id || r.dbz_host || ''}
          dataSource={pagedRows}
          loading={loading}
          bordered
          pagination={false}
          rowSelection={
            impersonating
              ? {
                  selectedRowKeys: selectedIds,
                  preserveSelectedRowKeys: true,
                  onChange: (keys) => {
                    const pageIds = new Set(pagedRows.map((r) => r.id).filter(Boolean) as string[])
                    const onPage = keys.map((k) => String(k)).filter((id) => pageIds.has(id))
                    setSelectedIds((prev) => [...prev.filter((id) => !pageIds.has(id)), ...onPage])
                  },
                }
              : undefined
          }
          scroll={{ x: 'max-content', y: '100%' }}
          style={{ height: '100%' }}
          columns={[
            { title: 'dbz_host', dataIndex: 'dbz_host', width: 240, ellipsis: true },
            ...(impersonating
              ? [{ title: '所属租户', width: 140, render: (_: unknown, row: ConnectorRow) => row.tenant_name || row.tenant_id || '—' }]
              : []),
            { title: 'env', width: 180, render: (_, row) => envLabel(row.env) },
            {
              title: '消息格式',
              width: 120,
              render: (_, row) => (
                <Tag color={row.message_schema_mode === 'with_schema' ? 'blue' : 'default'}>
                  {schemaLabel(row.message_schema_mode)}
                </Tag>
              ),
            },
            {
              title: 'kafka_host',
              width: 240,
              ellipsis: true,
              render: (_, row) => kafkaLabel(row.kafka_host),
            },
            {
              title: '操作',
              width: 160,
              fixed: 'right',
              render: (_, row) =>
                canEdit(row) ? (
                  <Space size={0}>
                    <Button type="link" onClick={() => open(row)}>
                      编辑
                    </Button>
                    <Button type="link" danger onClick={() => remove(row)}>
                      删除
                    </Button>
                  </Space>
                ) : (
                  '—'
                ),
            },
          ]}
        />
      </div>
      <div className="pager">
        <span className="pager-total">共 {filteredRows.length} 条</span>
        <Pagination
          current={page}
          pageSize={pageSize}
          total={filteredRows.length}
          pageSizeOptions={[10, 20, 50]}
          showSizeChanger
          showQuickJumper
          onChange={(p, ps) => {
            setPage(p)
            setPageSize(ps)
          }}
        />
      </div>

      <Modal
        open={visible}
        title={editingId ? '编辑连接器' : '增加连接器'}
        width={560}
        destroyOnClose
        onCancel={() => setVisible(false)}
        footer={[
          <Button key="cancel" onClick={() => setVisible(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={saving} onClick={save}>
            确定
          </Button>,
        ]}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            label="dbz_host"
            name="dbz_host"
            rules={[{ required: true, message: '请输入 dbz_host' }]}
          >
            <Input placeholder="如 10.100.112.17:8084 或逗号分隔多地址" />
          </Form.Item>
          <Form.Item
            label="env"
            name="env"
            rules={[{ required: true, message: '请选择环境' }]}
            extra="数据来自「环境管理」；保存为环境编码（如 test / prod），非 id"
          >
            <Select showSearch allowClear placeholder="请选择环境" optionFilterProp="label" options={envOptions} />
          </Form.Item>
          <Form.Item
            label="kafka_host"
            name="kafka_host"
            rules={[{ required: true, message: '请选择 Kafka 资源地址' }]}
            extra="数据来自已登记的 Kafka 实例（db_type=kafka）"
          >
            <Select
              showSearch
              allowClear
              placeholder="请选择 Kafka 资源（address）"
              optionFilterProp="label"
              options={kafkaOptions}
            />
          </Form.Item>
          <Form.Item
            label="Kafka 消息格式"
            name="message_schema_mode"
            rules={[{ required: true, message: '请选择 Kafka 消息格式' }]}
            extra="with_schema=带字段描述；without_schema=纯 KV JSON（CDC 默认）"
          >
            <Radio.Group>
              <Radio value="without_schema">不带 Schema</Radio>
              <Radio value="with_schema">带 Schema</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        open={assignOpen}
        title={`批量指定连接器归属租户（${selectedIds.length}）`}
        okText="划拨"
        confirmLoading={assigning}
        onCancel={() => !assigning && setAssignOpen(false)}
        onOk={confirmAssign}
      >
        <p style={{ marginBottom: 12 }}>把选中的连接器划到目标租户。不改 Kafka 实例的归属。</p>
        <Select
          style={{ width: '100%' }}
          showSearch
          optionFilterProp="label"
          value={assignTid || undefined}
          onChange={(v) => setAssignTid(v)}
          options={tenants.map((t) => ({
            value: t.id,
            label: `${t.name || t.code || t.id}${t.id === currentTenantId ? '（当前工作租户）' : ''}`,
          }))}
        />
      </Modal>
    </div>
  )
}
