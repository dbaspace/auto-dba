import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Progress,
  Select,
  Space,
  Tabs,
  Tag,
  Upload,
  message,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Link } from 'react-router-dom'
import { CloudUploadOutlined, ReloadOutlined } from '@ant-design/icons'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import { sha256File } from '../../utils/sha256File'
import './MonitorTargets.css'

const CLOUD_PRODUCT_OPTS = [
  { value: 'aliyun_oss', label: '阿里云 OSS' },
  { value: 'tencent_cos', label: '腾讯云 COS' },
  { value: 'huawei_obs', label: '华为云 OBS' },
  { value: 'aws_s3', label: 'AWS S3' },
  { value: 's3_compatible', label: 'S3 兼容（MinIO 等）' },
]

const ENDPOINT_HINT: Record<string, string> = {
  aliyun_oss: 'https://oss-cn-hangzhou.aliyuncs.com',
  tencent_cos: 'https://cos.ap-guangzhou.myqcloud.com',
  huawei_obs: 'https://obs.cn-north-4.myhuaweicloud.com',
  aws_s3: 'https://s3.amazonaws.com',
  s3_compatible: 'https://minio.example.com',
}

const CHUNK_SIZE = 16 * 1024 * 1024 // 16 MiB，与后端默认一致
const LS_KEY = 'auto-dba-oss-upload-jobs'
const PART_RETRIES = 3

function cloudProductLabel(code?: string) {
  return CLOUD_PRODUCT_OPTS.find((o) => o.value === code)?.label || code || '—'
}

function formatBytes(n?: number) {
  if (n == null || n < 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function statusTag(status?: string) {
  const map: Record<string, { color: string; text: string }> = {
    uploading: { color: 'processing', text: '上传中' },
    completing: { color: 'processing', text: '合并中' },
    verifying: { color: 'processing', text: '校验中' },
    success: { color: 'success', text: '成功' },
    failed: { color: 'error', text: '失败' },
    aborted: { color: 'default', text: '已取消' },
  }
  const m = map[status || ''] || { color: 'default', text: status || '—' }
  return <Tag color={m.color}>{m.text}</Tag>
}

interface OSSRow {
  id?: string
  environment?: string
  cloud_product?: string
  endpoint?: string
  bucket?: string
  prefix?: string
  access_key?: string
  secret_key?: string
  secret_set?: boolean
  retention_days?: number
  chunk_size_mb?: number
  configured?: boolean
  updated_at?: string
}

interface EnvRow {
  code?: string
  name?: string
}

interface UploadJob {
  id: string
  profile_id: string
  object_key: string
  file_name: string
  file_size: number
  sha256: string
  chunk_size: number
  status: string
  parts?: { part_number: number; etag: string; size: number }[]
  error_message?: string
  total_parts?: number
  done_parts?: number
  profile_env?: string
  profile_bucket?: string
  created_at?: string
  updated_at?: string
  finished_at?: string
}

function rememberJobId(id: string) {
  try {
    const raw = localStorage.getItem(LS_KEY)
    const arr: string[] = raw ? JSON.parse(raw) : []
    if (!arr.includes(id)) {
      arr.unshift(id)
      localStorage.setItem(LS_KEY, JSON.stringify(arr.slice(0, 40)))
    }
  } catch {
    /* ignore */
  }
}

/** 配置中心 · 对象存储（配置 + 分片上传） */
export default function OssUpload() {
  return (
    <div className="ops-page">
      <Tabs
        defaultActiveKey="settings"
        items={[
          { key: 'settings', label: '存储配置', children: <SettingsTab /> },
          { key: 'uploads', label: '上传任务', children: <UploadsTab /> },
        ]}
      />
    </div>
  )
}

function SettingsTab() {
  const [rows, setRows] = useState<OSSRow[]>([])
  const [envs, setEnvs] = useState<EnvRow[]>([])
  const [loading, setLoading] = useState(false)
  const [envFilter, setEnvFilter] = useState<string | undefined>()
  const [visible, setVisible] = useState(false)
  const [editingId, setEditingId] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testOk, setTestOk] = useState(false)
  const [form] = Form.useForm()
  const cloudProduct = Form.useWatch('cloud_product', form) as string | undefined

  const envOptions = envs.map((e) => ({
    value: e.code,
    label: e.name ? `${e.name} (${e.code})` : e.code,
  }))

  async function load() {
    setLoading(true)
    try {
      const [listRes, envRes] = await Promise.all([
        api.get('/oss-settings', { params: { environment: envFilter || undefined } }),
        api.get('/environments'),
      ])
      setRows(listRes.data.data || [])
      setEnvs(envRes.data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function openCreate() {
    setEditingId('')
    setTestOk(false)
    form.resetFields()
    form.setFieldsValue({
      cloud_product: 'aliyun_oss',
      prefix: 'migration',
      retention_days: 7,
      chunk_size_mb: 16,
    })
    setVisible(true)
  }

  function openEdit(row: OSSRow) {
    setEditingId(row.id || '')
    setTestOk(false)
    form.setFieldsValue({
      ...row,
      access_key: row.access_key || '',
      secret_key: '',
    })
    setVisible(true)
  }

  async function testConn() {
    const values = await form.validateFields(['environment', 'cloud_product', 'endpoint', 'bucket'])
    const all = form.getFieldsValue(true)
    setTesting(true)
    try {
      const { data } = await api.post(
        '/oss-settings/test',
        {
          environment: values.environment,
          cloud_product: values.cloud_product,
          endpoint: all.endpoint,
          bucket: all.bucket,
          prefix: all.prefix,
          access_key: all.access_key,
          secret_key: all.secret_key,
        },
        { timeout: 20000 },
      )
      const ok = Boolean(data.ok || data.success)
      setTestOk(ok)
      const msg = data.message || (ok ? 'OSS 连接成功' : '连接失败')
      if (ok) message.success(msg)
      else message.error(msg)
    } catch (e: any) {
      setTestOk(false)
      message.error(e?.response?.data?.message || e?.message || '连接失败')
    } finally {
      setTesting(false)
    }
  }

  async function doSave() {
    const values = await form.validateFields()
    setSaving(true)
    try {
      const payload = { ...values }
      if (editingId) {
        if (!payload.access_key || /^\*+$/.test(String(payload.access_key))) delete payload.access_key
        if (!payload.secret_key || /^\*+$/.test(String(payload.secret_key))) delete payload.secret_key
        await api.put(`/oss-settings/${editingId}`, payload)
      } else {
        await api.post('/oss-settings', payload)
      }
      message.success('已保存')
      setVisible(false)
      await load()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  function save() {
    if (!testOk) {
      Modal.confirm({
        title: '尚未通过连接测试',
        content: '尚未通过连接测试，仍要保存当前 OSS 配置吗？',
        okText: '仍要保存',
        cancelText: '取消',
        onOk: doSave,
      })
      return
    }
    doSave()
  }

  function remove(row: OSSRow) {
    Modal.confirm({
      title: `确认删除环境「${row.environment}」的 OSS 配置？`,
      onOk: async () => {
        await api.delete(`/oss-settings/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const columns: ColumnsType<OSSRow> = [
    { title: '环境', dataIndex: 'environment', key: 'environment', width: 120 },
    {
      title: '云产品',
      dataIndex: 'cloud_product',
      key: 'cloud_product',
      width: 140,
      render: (v: string) => cloudProductLabel(v),
    },
    { title: '访问地址', dataIndex: 'endpoint', key: 'endpoint', ellipsis: true },
    { title: '存储桶', dataIndex: 'bucket', key: 'bucket', width: 140 },
    { title: '前缀', dataIndex: 'prefix', key: 'prefix', width: 100 },
    {
      title: '状态',
      dataIndex: 'configured',
      key: 'configured',
      width: 100,
      render: (v: boolean) => (v ? <Tag color="success">已配置</Tag> : <Tag color="warning">未完整</Tag>),
    },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180 },
    {
      title: '操作',
      key: 'action',
      width: 140,
      render: (_: unknown, row: OSSRow) => (
        <Space>
          <Button type="link" onClick={() => openEdit(row)}>
            编辑
          </Button>
          <Button type="link" danger onClick={() => remove(row)}>
            删除
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <>
      <Alert
        className="cred-warn"
        type="info"
        showIcon
        message={
          <span>
            按环境维护 S3 兼容对象存储（阿里云 OSS / 腾讯云 COS / MinIO 等）。凭证加密入库；上传走平台分片中转，无需配置
            OSS CORS。环境取自 <Link to="/config/environments">配置中心 · 环境管理</Link>。
          </span>
        }
      />
      <div className="ops-title">
        <Space wrap>
          <Select
            allowClear
            placeholder="环境"
            style={{ width: 180 }}
            value={envFilter}
            onChange={(v) => setEnvFilter(v)}
            options={envOptions}
          />
          <Button onClick={() => load()}>查询</Button>
          <Button type="primary" onClick={openCreate}>
            新建
          </Button>
        </Space>
      </div>
      <div className="ops-table">
        <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 10 }} />
      </div>
      <Modal
        title={editingId ? '编辑 OSS 配置' : '新建 OSS 配置'}
        open={visible}
        onCancel={() => setVisible(false)}
        onOk={save}
        confirmLoading={saving}
        width={560}
        destroyOnClose
        okText="保存"
      >
        <Form
          form={form}
          layout="vertical"
          onValuesChange={() => {
            if (testOk) setTestOk(false)
          }}
        >
          <Form.Item name="environment" label="环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择环境管理中的环境"
              options={envOptions}
              disabled={Boolean(editingId)}
            />
          </Form.Item>
          <Form.Item name="cloud_product" label="云产品" rules={[{ required: true, message: '请选择云产品' }]}>
            <Select options={CLOUD_PRODUCT_OPTS} placeholder="请选择对象存储云产品" />
          </Form.Item>
          <Form.Item name="endpoint" label="访问地址" rules={[{ required: true }]}>
            <Input placeholder={ENDPOINT_HINT[cloudProduct || ''] || 'https://...'} />
          </Form.Item>
          <Form.Item name="bucket" label="存储桶名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="prefix" label="文件路径前缀">
            <Input placeholder="migration" />
          </Form.Item>
          <Form.Item
            name="access_key"
            label="AccessKey ID"
            rules={editingId ? [] : [{ required: true }]}
            extra={editingId ? '脱敏展示，修改请重新输入；留空表示不修改' : undefined}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="secret_key"
            label="AccessKey Secret"
            rules={editingId ? [] : [{ required: true }]}
            extra={editingId ? '留空表示不修改' : undefined}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="retention_days" label="临时文件保留天数">
            <InputNumber min={1} max={3650} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="chunk_size_mb" label="默认分片大小 (MB)" extra="上传任务未指定时参考；建议 8～16">
            <InputNumber min={5} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item>
            <Button loading={testing} onClick={testConn}>
              连接测试
            </Button>
            {testOk ? (
              <Tag color="success" style={{ marginLeft: 8 }}>
                已通过
              </Tag>
            ) : null}
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}

function UploadsTab() {
  const [profiles, setProfiles] = useState<OSSRow[]>([])
  const [profileId, setProfileId] = useState<string>()
  const [jobs, setJobs] = useState<UploadJob[]>([])
  const [loading, setLoading] = useState(false)
  const [hashProgress, setHashProgress] = useState(-1)
  const [uploadProgress, setUploadProgress] = useState<{ done: number; total: number; name: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const fileRef = useRef<File | null>(null)

  const loadProfiles = useCallback(async () => {
    const { data } = await api.get('/oss-settings')
    const list: OSSRow[] = (data.data || []).filter((r: OSSRow) => r.configured)
    setProfiles(list)
    if (!profileId && list[0]?.id) setProfileId(list[0].id)
  }, [profileId])

  const loadJobs = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/oss-uploads', { params: { limit: 50 } })
      setJobs(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载任务失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadProfiles().catch(() => {})
    loadJobs()
    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
    }
  }, [])

  function startPoll(jobId: string) {
    if (pollRef.current) clearInterval(pollRef.current)
    pollRef.current = setInterval(async () => {
      try {
        const { data } = await api.get(`/oss-uploads/${jobId}`, { silent: true } as any)
        const j: UploadJob = data.data
        setJobs((prev) => {
          const rest = prev.filter((x) => x.id !== j.id)
          return [j, ...rest]
        })
        if (j.status === 'success' || j.status === 'failed' || j.status === 'aborted') {
          if (pollRef.current) clearInterval(pollRef.current)
          pollRef.current = null
          if (j.status === 'success') message.success('上传并校验成功')
          if (j.status === 'failed') message.error(j.error_message || '上传失败')
        }
      } catch {
        /* ignore */
      }
    }, 2500)
  }

  async function putPartWithRetry(jobId: string, partNumber: number, blob: Blob) {
    let lastErr: any
    for (let i = 0; i < PART_RETRIES; i++) {
      try {
        await api.put(`/oss-uploads/${jobId}/parts/${partNumber}`, blob, {
          headers: { 'Content-Type': 'application/octet-stream' },
          timeout: 30 * 60 * 1000,
          silent: true,
        } as any)
        return
      } catch (e: any) {
        lastErr = e
        await new Promise((r) => setTimeout(r, 800 * (i + 1)))
      }
    }
    throw lastErr
  }

  async function runUpload(file: File, existing?: UploadJob) {
    if (!profileId && !existing) {
      message.warning('请先选择 OSS 配置')
      return
    }
    setBusy(true)
    fileRef.current = file
    try {
      let job = existing
      let sha = existing?.sha256 || ''
      if (!job) {
        setHashProgress(0)
        sha = await sha256File(file, (p) => setHashProgress(Math.round(p * 100)))
        setHashProgress(100)
        // 同内容未完成任务 → 断点续传
        try {
          const { data: listData } = await api.get('/oss-uploads', { params: { limit: 50 } })
          const match = (listData.data || []).find(
            (j: UploadJob) =>
              j.status === 'uploading' &&
              j.sha256 === sha &&
              j.file_size === file.size &&
              (!profileId || j.profile_id === profileId),
          )
          if (match) {
            const { data } = await api.get(`/oss-uploads/${match.id}`)
            job = data.data as UploadJob
            message.info('发现未完成任务，继续分片上传')
          }
        } catch {
          /* ignore */
        }
        if (!job) {
          const { data } = await api.post('/oss-uploads', {
            profile_id: profileId,
            file_name: file.name,
            file_size: file.size,
            sha256: sha,
            chunk_size: CHUNK_SIZE,
          })
          job = data.data as UploadJob
          message.success('任务已创建，开始分片上传')
        }
        rememberJobId(job!.id)
      } else {
        rememberJobId(job.id)
      }
      const chunk = job!.chunk_size || CHUNK_SIZE
      const total = Math.ceil(file.size / chunk)
      const doneSet = new Set((job!.parts || []).map((p) => p.part_number))
      setUploadProgress({ done: doneSet.size, total, name: file.name })

      for (let n = 1; n <= total; n++) {
        if (doneSet.has(n)) continue
        const start = (n - 1) * chunk
        const end = Math.min(start + chunk, file.size)
        const slice = file.slice(start, end)
        await putPartWithRetry(job!.id, n, slice)
        doneSet.add(n)
        setUploadProgress({ done: doneSet.size, total, name: file.name })
      }

      await api.post(`/oss-uploads/${job!.id}/complete`)
      message.info('分片已齐，后台合并并校验 SHA-256…')
      startPoll(job!.id)
      await loadJobs()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '上传失败')
      await loadJobs()
    } finally {
      setBusy(false)
      setHashProgress(-1)
      setUploadProgress(null)
    }
  }

  async function resumeJob(row: UploadJob) {
    const file = fileRef.current
    if (!file || file.name !== row.file_name || file.size !== row.file_size) {
      message.warning('请重新选择同名同大小的文件后点「继续上传」')
      return
    }
    const { data } = await api.get(`/oss-uploads/${row.id}`)
    await runUpload(file, data.data)
  }

  async function abortJob(row: UploadJob) {
    Modal.confirm({
      title: `取消上传「${row.file_name}」？`,
      content: '将 Abort Multipart，已上传分片在 OSS 侧清理。',
      onOk: async () => {
        await api.post(`/oss-uploads/${row.id}/abort`)
        message.success('已取消')
        await loadJobs()
      },
    })
  }

  const columns: ColumnsType<UploadJob> = [
    { title: '文件', dataIndex: 'file_name', key: 'file_name', ellipsis: true },
    {
      title: '大小',
      dataIndex: 'file_size',
      key: 'file_size',
      width: 100,
      render: (v: number) => formatBytes(v),
    },
    {
      title: '进度',
      key: 'progress',
      width: 120,
      render: (_: unknown, row) => `${row.done_parts || 0}/${row.total_parts || '?'}`,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => statusTag(v),
    },
    { title: '环境', dataIndex: 'profile_env', key: 'profile_env', width: 100 },
    { title: '对象键', dataIndex: 'object_key', key: 'object_key', ellipsis: true },
    {
      title: '错误',
      dataIndex: 'error_message',
      key: 'error_message',
      ellipsis: true,
      render: (v: string) => v || '—',
    },
    { title: '更新', dataIndex: 'updated_at', key: 'updated_at', width: 170 },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_: unknown, row) => (
        <Space>
          {row.status === 'uploading' ? (
            <Button type="link" onClick={() => resumeJob(row)}>
              继续上传
            </Button>
          ) : null}
          {row.status === 'uploading' || row.status === 'failed' ? (
            <Button type="link" danger onClick={() => abortJob(row)}>
              取消
            </Button>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="大文件经平台分片转发到 OSS（默认 16MiB/片），刷新后可续传；全部片齐后后台 Complete 并流式校验 SHA-256。3GB 级校验耗时与下载同量级。"
      />
      <div className="ops-title">
        <Space wrap>
          <Select
            placeholder="选择 OSS 配置"
            style={{ minWidth: 260 }}
            value={profileId}
            onChange={setProfileId}
            options={profiles.map((p) => ({
              value: p.id,
              label: `${p.environment} · ${p.bucket} (${cloudProductLabel(p.cloud_product)})`,
            }))}
          />
          <Upload
            beforeUpload={(file) => {
              runUpload(file)
              return false
            }}
            showUploadList={false}
            disabled={busy || !profileId}
          >
            <Button type="primary" icon={<CloudUploadOutlined />} loading={busy} disabled={!profileId}>
              选择文件上传
            </Button>
          </Upload>
          <Button icon={<ReloadOutlined />} onClick={() => loadJobs()} disabled={busy}>
            刷新
          </Button>
        </Space>
      </div>
      {hashProgress >= 0 ? (
        <div style={{ marginBottom: 12 }}>
          <div>计算 SHA-256…</div>
          <Progress percent={hashProgress} size="small" />
        </div>
      ) : null}
      {uploadProgress ? (
        <div style={{ marginBottom: 12 }}>
          <div>
            上传 {uploadProgress.name}：{uploadProgress.done}/{uploadProgress.total} 片
          </div>
          <Progress
            percent={uploadProgress.total ? Math.round((uploadProgress.done / uploadProgress.total) * 100) : 0}
            size="small"
          />
        </div>
      ) : null}
      <div className="ops-table">
        <ResponsiveTable rowKey="id" loading={loading} columns={columns} dataSource={jobs} pagination={{ pageSize: 10 }} />
      </div>
    </>
  )
}

/** @deprecated 使用 OssUpload；保留别名以免旧引用断裂 */
export { OssUpload as MigrationOSS }
