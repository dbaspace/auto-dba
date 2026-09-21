export function unlockAppShell() {
  const app = document.getElementById('app')
  app?.removeAttribute('inert')
  app?.removeAttribute('aria-hidden')
  document.body.classList.remove('el-popup-parent--hidden')
  document.documentElement.classList.remove('el-popup-parent--hidden')
  document.body.style.removeProperty('padding-right')
}

export function installOverlayGuard() {
  const app = document.getElementById('app')
  if (!app) return
  unlockAppShell()
  new MutationObserver(unlockAppShell).observe(app, {
    attributes: true,
    attributeFilter: ['inert', 'aria-hidden'],
  })
}

export function showDialog(flag) {
  unlockAppShell()
  flag.value = true
}

export function unlockPage() {
  unlockAppShell()
}
export function clearAllOverlays() {}
export function markLiveDialogs() {}
export function watchAppUnlock() {
  return () => {}
}
export function isLiveOverlay() {
  return false
}
