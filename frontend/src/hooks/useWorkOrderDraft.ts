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
  /** 提交成功清草稿后禁止卸载/pagehide 再写回 */
  const clearedRef = useRef(false)
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled
  const getValuesRef = useRef(getValues)
  const applyValuesRef = useRef(applyValues)
  getValuesRef.current = getValues
  applyValuesRef.current = applyValues

  const persist = useCallback(
    (silent: boolean) => {
      if (!enabledRef.current || skipSaveRef.current || clearedRef.current) return false
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
    [type, label, path],
  )

  const persistRef = useRef(persist)
  persistRef.current = persist

  const flushPersist = useCallback(() => {
    window.clearTimeout(timerRef.current)
    timerRef.current = 0
    persistRef.current(true)
  }, [])

  const schedulePersist = useCallback(() => {
    if (!enabledRef.current || skipSaveRef.current || clearedRef.current) return
    window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => persistRef.current(true), 800)
  }, [])

  const restoreDraft = useCallback(() => {
    if (!enabled) return false
    const userId = currentDraftOwnerId()
    if (!userId) return false
    const draft = loadWorkOrderDraft(type, userId)
    if (!draft) return false
    skipSaveRef.current = true
    clearedRef.current = false
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
    timerRef.current = 0
    clearedRef.current = false
    return persist(false)
  }, [persist])

  const clearOnSubmitSuccess = useCallback(() => {
    window.clearTimeout(timerRef.current)
    timerRef.current = 0
    clearedRef.current = true
    skipSaveRef.current = true
    const userId = currentDraftOwnerId()
    if (userId) clearWorkOrderDraft(type, userId)
  }, [type])

  useEffect(() => {
    const onPageHide = () => flushPersist()
    window.addEventListener('pagehide', onPageHide)
    return () => {
      window.removeEventListener('pagehide', onPageHide)
      flushPersist()
    }
  }, [flushPersist])

  return { restoreDraft, saveNow, clearOnSubmitSuccess, schedulePersist }
}
