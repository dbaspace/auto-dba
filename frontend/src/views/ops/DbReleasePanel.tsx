import React, { useMemo, useState } from 'react'
import {
  Alert, Button, Form, Input, Modal, Select, Space, Switch, Table, Tag, Tooltip, Upload, message,
} from 'antd'
import api from '../../api/request'

export type DbRelease = {
  id: string
  engine: string
  version: string
  major_version?: string
  channel?: string
  object_key: string
  checksum: string
  filename?: string
  arch?: string
  pack_format?: string
  size_bytes?: number
  enabled: boolean
  remark?: string
  status?: 'ready' | 'pending' | 'hashing'
}

type EngineRule = { placeholder: string; pattern?: RegExp; patternMsg?: string; channels: string[]; filename: string }

const ENGINE_RULES: Record<string, EngineRule> = {
  mysql: {
    placeholder: '8.0.36 / 8.4.3 / 5.7.44',
    pattern: /^(5|8|9)\.\d+\.\d+$/,
    patternMsg: 'MySQL 版本号格式为 主.次.修订，如 8.0.36',
    channels: ['LTS', 'Innovation', 'GA'],
    filename: 'mysql-8.0.36-linux-glibc2.17-x86_64.tar.xz',
  },
  postgres: {
    placeholder: '16.4 / 17.2',
    pattern: /^(1[3-9]|[2-9]\d)(\.\d+)?$/,
    patternMsg: 'PostgreSQL 需 13 及以上，格式 主版本.小版本，如 16.4',
    channels: ['stable'],
    filename: 'postgresql-16.4-linux-x86_64.tar.gz',
  },
}
const DEFAULT_RULE: EngineRule = { placeholder: '如 7.2.4', channels: ['stable'], filename: 'xxx.tar.gz' }

const STATUS_TAG: Record<string, { color: string; label: string }> = {
  ready: { color: 'success', label: '就绪' },
  pending: { color: 'warning', label: '待补包' },
  hashing: { color: 'processing', label: '计算 sha256 中' },
}

const SHA_RE = /^[a-fA-F0-9]{64}$/
const MONO: React.CSSProperties = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const errMsg = (e: any): string => e?.response?.data?.message || e?.message || String(e)

function fmtSize(n?: number): string {
  if (!n) return ''
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(2)} GB`
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`
  return `${Math.ceil(n / 1024)} KB`
}

function previewKey(prefix: string, version: string, arch: string, filename: string): string {
  let p = (prefix || '').trim()
  if (p && !p.endsWith('/')) p += '/'
  return `${p}${(version || '{version}').trim()}/${arch || 'x86_64'}/${filename || '{文件名}'}`
}

/** 版本目录：登记版本、上传包 / 从 OSS 同步（自动 sha256）、行内修正 sha256。 */
export default function DbReleasePanel({
  engine, releases, prefix, onChanged,
}: {
  engine: string; releases: DbRelease[]; prefix: string; onChanged: () => void
}) {
  const rule = ENGINE_RULES[engine] || DEFAULT_RULE
  const engineOptions = engine === 'redis'
    ? [{ value: 'redis', label: 'redis' }, { value: 'codis', label: 'codis' }]
    : [{ value: engine, label: engine }]
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm()
  const [busyId, setBusyId] = useState('')
  const [shaDraft, setShaDraft] = useState<Record<string, string>>({})
  const watchVersion = Form.useWatch('version', form) as string | undefined
  const watchArch = Form.useWatch('arch', form) as string | undefined
  const watchFile = Form.useWatch('filename', form) as string | undefined
  const effectivePrefix = prefix || `db-artifacts/${engine}/`
  const hasHashing = useMemo(() => releases.some((r) => r.status === 'hashing'), [releases])

  const saveSha = async (r: DbRelease) => {
    const v = (shaDraft[r.id] ?? '').trim().toLowerCase()
    if (!SHA_RE.test(v)) {
      message.warning('sha256 须为 64 位十六进制')
      return
    }
    setBusyId(r.id)
    try {
      await api.put(`/db-deploy/releases/${r.id}`, { checksum: v })
      message.success('sha256 已保存')
      setShaDraft((m) => { const n = { ...m }; delete n[r.id]; return n })
      onChanged()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusyId('')
    }
  }

  const syncFromOSS = async (r: DbRelease) => {
    setBusyId(r.id)
    try {
      const res = await api.post(`/db-deploy/releases/${r.id}/sync-from-oss`, {}, { timeout: 300000 })
      const d = res.data?.data || {}
      if (d.status === 'missing') message.warning(d.message)
      else if (d.status === 'hashing') message.info(d.message, 6)
      else message.success(d.message || '已同步')
      onChanged()
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusyId('')
    }
  }

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message={(
          <>
            先「登记版本」，再「上传包」或把包放到 OSS 后「从 OSS 同步」，系统自动生成 object_key 并计算 sha256。
            约定路径：<span style={MONO}>{previewKey(effectivePrefix, '{version}', '{arch}', '{文件名}')}</span>
            ；同目录放 <span style={MONO}>{'{文件名}.sha256'}</span> 或 <span style={MONO}>SHA256SUMS</span> 可免下载整包。
          </>
        )}
      />
      <Space style={{ marginBottom: 12 }}>
        <Button
          type="primary"
          onClick={() => {
            form.resetFields()
            form.setFieldsValue({ engine: engineOptions[0].value, arch: 'x86_64', channel: rule.channels[0], enabled: true })
            setOpen(true)
          }}
        >
          登记版本
        </Button>
        <Button onClick={onChanged}>刷新</Button>
        {hasHashing && <span style={{ color: '#8c8c8c' }}>有版本正在后台计算 sha256，稍后刷新</span>}
      </Space>
      <Table
        rowKey="id"
        size="small"
        dataSource={releases}
        pagination={{ pageSize: 10, size: 'small', showTotal: (t) => `共 ${t} 个版本` }}
        expandable={{
          expandedRowRender: (r: DbRelease) => (
            <div style={{ ...MONO, lineHeight: '22px' }}>
              <div>object_key：{r.object_key || '—'}</div>
              <div>sha256：{r.checksum || '—'}</div>
              {r.remark && <div style={{ fontFamily: 'inherit' }}>备注：{r.remark}</div>}
            </div>
          ),
        }}
        columns={[
          ...(engine === 'redis' ? [{ title: '引擎', dataIndex: 'engine', width: 80 }] : []),
          { title: '版本', dataIndex: 'version', width: 100, render: (v: string) => <b>{v}</b> },
          { title: '大版本', dataIndex: 'major_version', width: 72 },
          { title: '通道', dataIndex: 'channel', width: 90, render: (v?: string) => (v ? <Tag>{v}</Tag> : '—') },
          { title: '架构', dataIndex: 'arch', width: 80 },
          {
            title: '安装包',
            ellipsis: true,
            render: (_: unknown, r: DbRelease) => (r.filename
              ? <Tooltip title={r.object_key}><span style={MONO}>{r.filename}</span> <span style={{ color: '#8c8c8c' }}>{fmtSize(r.size_bytes)}</span></Tooltip>
              : <span style={{ color: '#8c8c8c' }}>未上传</span>),
          },
          {
            title: 'sha256',
            width: 250,
            render: (_: unknown, r: DbRelease) => {
              const draft = shaDraft[r.id]
              if (draft === undefined) {
                return (
                  <Space size={4}>
                    <span style={MONO}>{r.checksum ? `${r.checksum.slice(0, 12)}…` : '—'}</span>
                    <Button type="link" size="small" onClick={() => setShaDraft((m) => ({ ...m, [r.id]: r.checksum || '' }))}>修改</Button>
                  </Space>
                )
              }
              const valid = SHA_RE.test(draft.trim())
              return (
                <Space.Compact style={{ width: '100%' }}>
                  <Input
                    size="small"
                    value={draft}
                    status={draft && !valid ? 'error' : undefined}
                    onChange={(e) => setShaDraft((m) => ({ ...m, [r.id]: e.target.value }))}
                    style={MONO}
                  />
                  <Button size="small" type="primary" disabled={!valid} loading={busyId === r.id} onClick={() => void saveSha(r)}>存</Button>
                  <Button size="small" onClick={() => setShaDraft((m) => { const n = { ...m }; delete n[r.id]; return n })}>取消</Button>
                </Space.Compact>
              )
            },
          },
          {
            title: '状态',
            dataIndex: 'status',
            width: 110,
            render: (s?: string) => <Tag color={STATUS_TAG[s || 'pending']?.color}>{STATUS_TAG[s || 'pending']?.label || s}</Tag>,
          },
          {
            title: '启用',
            dataIndex: 'enabled',
            width: 64,
            render: (v: boolean, r: DbRelease) => (
              <Switch
                size="small"
                checked={v}
                onChange={async (checked) => {
                  try {
                    await api.put(`/db-deploy/releases/${r.id}`, { enabled: checked })
                    onChanged()
                  } catch (e) {
                    message.error(errMsg(e))
                  }
                }}
              />
            ),
          },
          {
            title: '操作',
            width: 250,
            render: (_: unknown, r: DbRelease) => (
              <Space size={0} wrap>
                <Upload
                  showUploadList={false}
                  accept=".tar.gz,.tgz,.tar.xz,.txz,.tar"
                  customRequest={async ({ file, onSuccess, onError }) => {
                    const fd = new FormData()
                    fd.append('file', file as Blob)
                    setBusyId(r.id)
                    const hide = message.loading('正在上传并计算 sha256…', 0)
                    try {
                      await api.post(`/db-deploy/releases/${r.id}/upload`, fd, { timeout: 3600000 })
                      message.success('上传完成，已回填 object_key 与 sha256')
                      onSuccess?.({})
                      onChanged()
                    } catch (e) {
                      message.error(errMsg(e))
                      onError?.(e as Error)
                    } finally {
                      hide()
                      setBusyId('')
                    }
                  }}
                >
                  <Button type="link" size="small" loading={busyId === r.id}>上传包</Button>
                </Upload>
                <Button type="link" size="small" disabled={busyId === r.id} onClick={() => void syncFromOSS(r)}>从 OSS 同步</Button>
                <Button
                  type="link"
                  size="small"
                  disabled={r.status !== 'ready'}
                  onClick={async () => {
                    try {
                      await api.post(`/db-deploy/releases/${r.id}/presign`)
                      message.success('签发成功（URL 不下发到界面）')
                    } catch (e) {
                      message.error(errMsg(e))
                    }
                  }}
                >
                  试签发
                </Button>
                <Button
                  type="link"
                  size="small"
                  danger
                  onClick={() => Modal.confirm({
                    title: `删除版本 ${r.version}`,
                    content: '只删除登记记录，不删除 OSS 上的安装包。已绑定该版本的集群将无法再部署 / 扩容。',
                    okButtonProps: { danger: true },
                    onOk: async () => {
                      await api.delete(`/db-deploy/releases/${r.id}`)
                      onChanged()
                    },
                  })}
                >
                  删除
                </Button>
              </Space>
            ),
          },
        ]}
      />

      <Modal title="登记版本" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} destroyOnClose>
        <Form
          form={form}
          layout="vertical"
          onFinish={async (v) => {
            try {
              await api.post('/db-deploy/releases', v)
              message.success('已登记，可继续上传包或从 OSS 同步')
              setOpen(false)
              onChanged()
            } catch (e) {
              message.error(errMsg(e))
            }
          }}
        >
          <Form.Item name="engine" label="引擎" rules={[{ required: true }]}>
            <Select options={engineOptions} disabled={engineOptions.length === 1} />
          </Form.Item>
          <Form.Item
            name="version"
            label="版本号"
            rules={[
              { required: true, message: '请填写版本号' },
              ...(rule.pattern ? [{ pattern: rule.pattern, message: rule.patternMsg }] : []),
            ]}
          >
            <Input placeholder={rule.placeholder} />
          </Form.Item>
          <Space style={{ display: 'flex' }} align="start">
            <Form.Item name="channel" label="发行通道" style={{ width: 160 }}>
              <Select options={rule.channels.map((c) => ({ value: c, label: c }))} />
            </Form.Item>
            <Form.Item name="arch" label="架构" style={{ width: 160 }}>
              <Select options={[{ value: 'x86_64', label: 'x86_64' }, { value: 'aarch64', label: 'aarch64' }]} />
            </Form.Item>
          </Space>
          <Form.Item name="filename" label="安装包文件名（可选）" extra="已放在 OSS 约定目录时填写；也可登记后直接上传 / 同步">
            <Input placeholder={rule.filename} />
          </Form.Item>
          <div style={{ margin: '-8px 0 12px', color: '#8c8c8c' }}>
            object_key：<span style={MONO}>{previewKey(effectivePrefix, watchVersion || '', watchArch || '', watchFile || '')}</span>
          </div>
          <Form.Item name="checksum" label="sha256（可选）" rules={[{ pattern: SHA_RE, message: '须为 64 位十六进制' }]}>
            <Input placeholder="留空则上传 / 同步时自动计算" style={MONO} />
          </Form.Item>
          <Form.Item name="remark" label="备注"><Input /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>
    </>
  )
}
