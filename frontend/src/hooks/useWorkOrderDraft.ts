import { useCallback, useEffect, useRef } from 'react'
import { message } from 'antd'
import {
  clearWorkOrderDraft,
  currentDraftOwnerId,
  loadWorkOrderDraft,
  saveWorkOrderDraft,
} from '../utils/workOrderDraft'

/** 本会话已提示过恢复的 type+userId，避免复挂连弹 */
const restoredToastKeys = new Set<string>()

interface Options {
  type: string
  label: string
  path: string
  getValues: () => Record<string, any>
  applyValues: (values: Record<string, any>) => void
  enabled?: boolean
}

export function useWorkOrderDraft({ type, label, path, getValues, applyValues, enabled = true }: Options) {
  const timerRef = useRef<number>(0)
  const skipSaveRef = useRef(false)
  const getValuesRef = useRef(getValues)
  const applyValuesRef = useRef(applyValues)
  getValuesRef.current = getValues
  applyValuesRef.current = applyValues

  const persist = useCallback(
    (silent: boolean) => {
      if (!enabled || skipSaveRef.current) return false
      const userId = currentDraftOwnerId()
      if (!userId) {
        if (!silent) message.warning('未登录，无法保存本地草稿')
        return false
      }
      const ok = saveWorkOrderDraft(
        { type, label, path, hint: '', values: getValuesRef.current() },
        userId,
        { force: !silent },
      )
      if (!silent) {
        if (ok) message.success('草稿已保存到本地')
        else message.error('草稿保存失败')
      }
      return ok
    },
    [enabled, type, label, path],
  )

  const schedulePersist = useCallback(() => {
    if (!enabled || skipSaveRef.current) return
    window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => persist(true), 800)
  }, [enabled, persist])

  const restoreDraft = useCallback(() => {
    if (!enabled) return false
    const userId = currentDraftOwnerId()
    if (!userId) return false
    const draft = loadWorkOrderDraft(type, userId)
    if (!draft) return false
    skipSaveRef.current = true
    try {
      applyValuesRef.current(draft.values || {})
      const toastKey = `${type}:${userId}`
      if (!restoredToastKeys.has(toastKey)) {
        restoredToastKeys.add(toastKey)
        message.info(`已恢复「${draft.label || label}」本地草稿`)
      }
      return true
    } catch {
      return false
    } finally {
      window.setTimeout(() => {
        skipSaveRef.current = false
      }, 300)
    }
  }, [enabled, type, label])

  const saveNow = useCallback(() => {
    window.clearTimeout(timerRef.current)
    return persist(false)
  }, [persist])

  const clearOnSubmitSuccess = useCallback(() => {
    window.clearTimeout(timerRef.current)
    const userId = currentDraftOwnerId()
    if (userId) clearWorkOrderDraft(type, userId)
  }, [type])

  useEffect(() => {
    return () => {
      window.clearTimeout(timerRef.current)
    }
  }, [])

  return { restoreDraft, saveNow, clearOnSubmitSuccess, schedulePersist }
}
