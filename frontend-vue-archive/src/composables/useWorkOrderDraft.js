import { onBeforeUnmount, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  clearWorkOrderDraft,
  currentDraftOwnerId,
  loadWorkOrderDraft,
  saveWorkOrderDraft,
} from '../utils/workOrderDraft'

/** 本会话已提示过恢复的 type+userId，避免复挂连弹 */
const restoredToastKeys = new Set()

export function useWorkOrderDraft({
  type,
  label,
  path,
  form,
  getValues,
  applyValues,
  extraWatch = [],
  enabled = true,
}) {
  let skipSave = false
  let timer = 0

  function isEnabled() {
    if (typeof enabled === 'function') return !!enabled()
    if (enabled && typeof enabled === 'object' && 'value' in enabled) return !!enabled.value
    return !!enabled
  }

  function collect() {
    if (typeof getValues === 'function') return getValues()
    return { ...form }
  }

  function persist(silent) {
    if (!isEnabled() || skipSave) return false
    const userId = currentDraftOwnerId()
    if (!userId) {
      if (!silent) ElMessage.warning('未登录，无法保存本地草稿')
      return false
    }
    const ok = saveWorkOrderDraft(
      { type, label, path, hint: '', values: collect() },
      userId,
      { force: !silent },
    )
    if (!silent) {
      if (ok) ElMessage.success('草稿已保存到本地')
      else ElMessage.error('草稿保存失败')
    }
    return ok
  }

  function schedulePersist() {
    if (!isEnabled() || skipSave) return
    window.clearTimeout(timer)
    timer = window.setTimeout(() => persist(true), 800)
  }

  function restoreDraft() {
    if (!isEnabled()) return false
    const userId = currentDraftOwnerId()
    if (!userId) return false
    const draft = loadWorkOrderDraft(type, userId)
    if (!draft) return false
    skipSave = true
    try {
      const values = draft.values || {}
      if (typeof applyValues === 'function') applyValues(values)
      else Object.assign(form, values)
      const toastKey = `${type}:${userId}`
      if (!restoredToastKeys.has(toastKey)) {
        restoredToastKeys.add(toastKey)
        ElMessage.info(`已恢复「${draft.label || label}」本地草稿`)
      }
      return true
    } catch {
      return false
    } finally {
      window.setTimeout(() => { skipSave = false }, 300)
    }
  }

  function saveNow() {
    window.clearTimeout(timer)
    return persist(false)
  }

  function clearOnSubmitSuccess() {
    window.clearTimeout(timer)
    const userId = currentDraftOwnerId()
    if (userId) clearWorkOrderDraft(type, userId)
  }

  const stop = watch(
    extraWatch.length ? [form, ...extraWatch] : form,
    () => schedulePersist(),
    { deep: true },
  )

  onBeforeUnmount(() => {
    window.clearTimeout(timer)
    stop()
  })

  return { restoreDraft, saveNow, clearOnSubmitSuccess }
}
