import React, { useEffect, useMemo, useState } from 'react'
import { Button, Empty, Modal } from 'antd'
import { useNavigate } from 'react-router-dom'
import {
  clearWorkOrderDraft,
  currentDraftOwnerId,
  loadAllWorkOrderDrafts,
  summarizeWorkOrderDraft,
  WorkOrderDraft,
} from '../utils/workOrderDraft'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onChanged?: () => void
}

function fmtTime(at: string): string {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export default function MyDraftsModal({ open, onOpenChange, onChanged }: Props) {
  const navigate = useNavigate()
  const [tick, setTick] = useState(0)
  const userId = currentDraftOwnerId()
  const drafts = useMemo(() => {
    void tick
    if (!userId) return [] as WorkOrderDraft[]
    return loadAllWorkOrderDrafts(userId)
  }, [tick, userId])

  function refresh() {
    setTick((t) => t + 1)
    onChanged?.()
  }

  useEffect(() => {
    if (open) refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, userId])

  function onContinue(d: WorkOrderDraft) {
    onOpenChange(false)
    navigate(d.path || '/ops/tickets')
  }

  function onClearOne(type: string) {
    if (!userId) return
    clearWorkOrderDraft(type, userId)
    refresh()
  }

  function onClearAll() {
    if (!userId) return
    clearWorkOrderDraft(undefined, userId)
    refresh()
  }

  return (
    <Modal
      open={open}
      title={drafts.length ? `我的草稿（${drafts.length}）` : '我的草稿'}
      width={520}
      footer={null}
      onCancel={() => onOpenChange(false)}
      destroyOnClose
    >
      {!drafts.length ? (
        <Empty description={userId ? '暂无本机草稿（清理浏览器缓存后也会为空）' : '未登录，无法读取本机草稿'} />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 8 }}>
            <p style={{ margin: 0, color: '#909399', fontSize: 12, lineHeight: 1.6 }}>
              仅保存在本机浏览器、仅当前登录账号可见，不跨设备、不上报平台。每种工单类型最多 1 条；再次进入同类型创建入口须继续编辑或先清空。 当前共 {drafts.length} 条。
            </p>
            {drafts.length > 1 && (
              <Button danger type="link" onClick={onClearAll}>
                清空全部
              </Button>
            )}
          </div>
          {drafts.map((d) => (
            <div
              key={d.type}
              style={{ border: '1px solid #f0f0f0', background: '#f7f9fc', borderRadius: 8, padding: 12 }}
            >
              <div style={{ fontSize: 14, fontWeight: 500, color: '#303133', marginBottom: 4 }}>{d.label}</div>
              <div style={{ fontSize: 12, color: '#c0c4cc', marginBottom: 8 }}>更新于 {fmtTime(d.updatedAt)}</div>
              <p
                style={{
                  margin: '0 0 8px',
                  fontSize: 12,
                  color: '#606266',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                  display: '-webkit-box',
                  WebkitLineClamp: 3,
                  WebkitBoxOrient: 'vertical',
                  overflow: 'hidden',
                }}
              >
                {summarizeWorkOrderDraft(d)}
              </p>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
                <Button onClick={() => onClearOne(d.type)}>清空</Button>
                <Button type="primary" onClick={() => onContinue(d)}>
                  继续编辑
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}
