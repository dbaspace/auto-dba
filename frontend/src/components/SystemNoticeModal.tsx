import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Modal } from 'antd'
import api from '../api/request'
import { systemNoticeDismissKey } from '../utils/systemNoticeSession'

type ActiveNotice = {
  show?: boolean
  notice_id?: string
  title?: string
  body?: string
  version?: number
  show_mode?: string
}

/** 登录进入主界面后按策略弹出系统通知。 */
export default function SystemNoticeModal({ userId }: { userId?: string }) {
  const [open, setOpen] = useState(false)
  const [notice, setNotice] = useState<ActiveNotice | null>(null)
  const [acking, setAcking] = useState(false)
  const loadedFor = useRef('')

  const load = useCallback(async () => {
    if (!userId) return
    try {
      const { data } = await api.get('/system-notice/active')
      const row: ActiveNotice = data.data || {}
      if (!row.show || !row.notice_id) {
        setOpen(false)
        setNotice(null)
        return
      }
      const ver = Number(row.version || 1)
      // every_login：仅抑制「同一次登录停留」内重复弹，key 必须含 userId
      if (row.show_mode === 'every_login') {
        const key = systemNoticeDismissKey(userId, String(row.notice_id), ver)
        if (sessionStorage.getItem(key) === '1') {
          setOpen(false)
          setNotice(null)
          return
        }
      }
      setNotice(row)
      setOpen(true)
    } catch {
      // 不影响主流程
    }
  }, [userId])

  useEffect(() => {
    if (!userId) {
      loadedFor.current = ''
      setOpen(false)
      setNotice(null)
      return
    }
    if (loadedFor.current === userId) return
    loadedFor.current = userId
    load()
  }, [userId, load])

  async function onOk() {
    if (!notice?.notice_id || !userId) {
      setOpen(false)
      return
    }
    const ver = Number(notice.version || 1)
    setAcking(true)
    try {
      await api.post('/system-notice/ack', { notice_id: notice.notice_id, version: ver })
    } catch {
      // ack 失败仍关闭本次；下次新登录会再拉
    } finally {
      if (notice.show_mode === 'every_login') {
        sessionStorage.setItem(systemNoticeDismissKey(userId, String(notice.notice_id), ver), '1')
      }
      setOpen(false)
      setAcking(false)
    }
  }

  return (
    <Modal
      open={open}
      title={notice?.title || '系统通知'}
      width={760}
      centered
      okText="我知道了"
      cancelButtonProps={{ style: { display: 'none' } }}
      closable={false}
      maskClosable={false}
      keyboard={false}
      confirmLoading={acking}
      onOk={onOk}
      destroyOnClose
      styles={{ body: { maxHeight: 'none', overflow: 'visible', paddingTop: 12 } }}
    >
      <div
        style={{
          whiteSpace: 'pre-wrap',
          lineHeight: 1.75,
          fontSize: 14,
          color: 'rgba(0,0,0,0.85)',
        }}
      >
        {notice?.body || ''}
      </div>
    </Modal>
  )
}
