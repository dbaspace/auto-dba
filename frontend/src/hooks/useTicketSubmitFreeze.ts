import { useCallback, useEffect, useState } from 'react'
import api from '../api/request'

export type TicketSubmitFreezeState = {
  enabled: boolean
  message: string
  loading: boolean
  reload: () => Promise<void>
}

const DEFAULT_MSG = '平台正在发版维护，暂不可提交工单，请稍后再试'

/** 工单提交页预检发版冻结状态。 */
export function useTicketSubmitFreeze(): TicketSubmitFreezeState {
  const [enabled, setEnabled] = useState(false)
  const [message, setMessage] = useState(DEFAULT_MSG)
  const [loading, setLoading] = useState(true)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/ticket-submit-freeze')
      const row = data?.data || {}
      setEnabled(!!row.enabled)
      setMessage(String(row.message || '').trim() || DEFAULT_MSG)
    } catch {
      setEnabled(false)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  return { enabled, message, loading, reload }
}
