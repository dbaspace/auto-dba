/** sessionStorage 前缀：系统通知「本会话已关闭」标记 */
export const SYSTEM_NOTICE_DISMISS_PREFIX = 'system_notice_dismissed'

export function systemNoticeDismissKey(userId: string, noticeId: string, version: number) {
  return `${SYSTEM_NOTICE_DISMISS_PREFIX}:${userId}:${noticeId}:${version}`
}

/** 登录/登出时清理，避免跨用户或跨登录会话残留。 */
export function clearSystemNoticeDismissals() {
  try {
    const keys: string[] = []
    for (let i = 0; i < sessionStorage.length; i++) {
      const k = sessionStorage.key(i)
      if (k && k.startsWith(SYSTEM_NOTICE_DISMISS_PREFIX)) keys.push(k)
    }
    for (const k of keys) sessionStorage.removeItem(k)
  } catch {
    /* ignore */
  }
}
