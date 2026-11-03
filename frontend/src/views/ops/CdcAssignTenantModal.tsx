import React, { useEffect, useState } from 'react'
import { Modal, Select, message } from 'antd'
import api from '../../api/request'

type TenantOption = { id: string; name?: string; code?: string }

interface Props {
  rows: any[]
  tenants: TenantOption[]
  defaultTenantId?: string
  onClose: () => void
  onDone: () => void
}

/** 平台审计会话下把 CDC 初始化任务（连同子任务）划拨到目标租户。 */
export default function CdcAssignTenantModal({ rows, tenants, defaultTenantId, onClose, onDone }: Props) {
  const [tid, setTid] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const open = rows.length > 0

  useEffect(() => {
    if (open) setTid(defaultTenantId || '')
  }, [open, defaultTenantId])

  function tenantLabel(id: string) {
    const t = tenants.find((x) => x.id === id)
    return t?.name || t?.code || id
  }

  async function submit() {
    if (!tid) {
      message.warning('请选择目标租户')
      return
    }
    const ids = rows.map((r) => r.id).filter(Boolean)
    const label = tenantLabel(tid)
    setSubmitting(true)
    try {
      if (ids.length === 1) {
        const { data } = await api.post(`/cdc-sync/tasks/${ids[0]}/assign-tenant`, { tenant_id: tid })
        message.success(`已划拨到 ${label}（含子任务共 ${Number(data?.moved || 0)} 条变更）`)
        if (data?.warning) message.warning(String(data.warning), 8)
      } else {
        const { data } = await api.post('/cdc-sync/tasks/assign-tenant', { tenant_id: tid, ids })
        const okCount = Number(data?.ok_count || 0)
        const failed: { id?: string; name?: string; message?: string }[] = data?.failed || []
        const warnings: { id?: string; name?: string; message?: string }[] = data?.warnings || []
        if (!failed.length && !warnings.length) {
          message.success(`已划拨 ${okCount} 个任务到 ${label}`)
        } else {
          Modal.warning({
            title: `划拨完成：成功 ${okCount}，失败 ${failed.length}`,
            width: 640,
            content: (
              <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 280, overflow: 'auto' }}>
                {failed.map((f) => (
                  <li key={'f' + f.id}>
                    {f.name || f.id}：{f.message || '失败'}
                  </li>
                ))}
                {warnings.map((w) => (
                  <li key={'w' + w.id} style={{ color: '#ad6800' }}>
                    {w.name || w.id}：{w.message}
                  </li>
                ))}
              </ul>
            ),
          })
        }
      }
      onDone()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '划拨失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={rows.length > 1 ? `批量划拨 CDC 任务到租户（${rows.length}）` : '划拨 CDC 任务到租户'}
      okText="划拨"
      cancelText="取消"
      confirmLoading={submitting}
      onCancel={() => !submitting && onClose()}
      onOk={() => void submit()}
      destroyOnClose
    >
      <p style={{ marginBottom: 8 }}>将连同新增表、移除表、源表加列等子任务一起划拨；工单记录仍留在原租户，链路运行不受影响。</p>
      <ul style={{ margin: '0 0 12px', paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
        {rows.map((r) => (
          <li key={r.id}>
            {r.name || r.id}
            {r.tenant_name || r.tenant_id ? `（当前：${r.tenant_name || r.tenant_id}）` : ''}
          </li>
        ))}
      </ul>
      <Select
        style={{ width: '100%' }}
        showSearch
        optionFilterProp="label"
        placeholder="选择目标租户"
        value={tid || undefined}
        onChange={(v) => setTid(v)}
        options={tenants.map((t) => ({ value: t.id, label: t.name || t.code || t.id }))}
      />
    </Modal>
  )
}
