export type ColorMode = 'light' | 'dark' | 'system'
export type ThemePresetId =
  | 'business-black'
  | 'classic-purple'
  | 'fresh-green'
  | 'rose-pink'
  | 'ocean-blue'
  | 'aurora'

export type AppearanceState = {
  preset: ThemePresetId
  mode: ColorMode
}

export type ThemePreset = {
  id: ThemePresetId
  label: string
  primary: string
  primarySoft: string
  siderBg: string
  siderText: string
  siderActiveBg: string
  siderActiveColor: string
  headerBg: string
  pageBg: string
  contentBg: string
  border: string
  /** 缩略图侧栏色 */
  thumbSider: string
  /** 缩略图强调色（可用渐变） */
  thumbAccent: string
}

export const APPEARANCE_STORAGE_KEY = 'auto-dba-appearance'
export const SIDER_COLLAPSED_KEY = 'auto-dba-sider-collapsed'

export const THEME_PRESETS: ThemePreset[] = [
  {
    id: 'business-black',
    label: '商务黑',
    primary: '#1677ff',
    primarySoft: '#e6f4ff',
    siderBg: '#1f1f1f',
    siderText: '#f5f5f5',
    siderActiveBg: '#111827',
    siderActiveColor: '#69b1ff',
    headerBg: '#ffffff',
    pageBg: '#f0f2f5',
    contentBg: '#ffffff',
    border: '#e5e7eb',
    thumbSider: '#1f1f1f',
    thumbAccent: '#1677ff',
  },
  {
    id: 'classic-purple',
    label: '经典紫',
    primary: '#722ed1',
    primarySoft: '#f9f0ff',
    siderBg: '#faf5ff',
    siderText: '#374151',
    siderActiveBg: '#f3e8ff',
    siderActiveColor: '#722ed1',
    headerBg: '#ffffff',
    pageBg: '#f5f0ff',
    contentBg: '#ffffff',
    border: '#ede4ff',
    thumbSider: '#faf5ff',
    thumbAccent: '#722ed1',
  },
  {
    id: 'fresh-green',
    label: '清新绿',
    primary: '#52c41a',
    primarySoft: '#f6ffed',
    siderBg: '#f6ffed',
    siderText: '#374151',
    siderActiveBg: '#d9f7be',
    siderActiveColor: '#389e0d',
    headerBg: '#ffffff',
    pageBg: '#f0f9e8',
    contentBg: '#ffffff',
    border: '#d9f7be',
    thumbSider: '#f6ffed',
    thumbAccent: '#52c41a',
  },
  {
    id: 'rose-pink',
    label: '玫瑰粉',
    primary: '#eb2f96',
    primarySoft: '#fff0f6',
    siderBg: '#fff0f6',
    siderText: '#374151',
    siderActiveBg: '#ffd6e7',
    siderActiveColor: '#c41d7f',
    headerBg: '#ffffff',
    pageBg: '#fff5f9',
    contentBg: '#ffffff',
    border: '#ffd6e7',
    thumbSider: '#fff0f6',
    thumbAccent: '#eb2f96',
  },
  {
    id: 'ocean-blue',
    label: '海洋蓝',
    primary: '#1677ff',
    primarySoft: '#e6f4ff',
    siderBg: '#f0f7ff',
    siderText: '#374151',
    siderActiveBg: '#e6f4ff',
    siderActiveColor: '#1677ff',
    headerBg: '#ffffff',
    pageBg: '#eef5fc',
    contentBg: '#ffffff',
    border: '#d6e4ff',
    thumbSider: '#e6f4ff',
    thumbAccent: '#1677ff',
  },
  {
    id: 'aurora',
    label: '极光渐变',
    primary: '#6366f1',
    primarySoft: '#eef2ff',
    siderBg: '#eef2ff',
    siderText: '#374151',
    siderActiveBg: '#e0e7ff',
    siderActiveColor: '#4f46e5',
    headerBg: '#ffffff',
    pageBg: '#e8edff',
    contentBg: '#ffffff',
    border: '#c7d2fe',
    thumbSider: 'linear-gradient(160deg, #a78bfa, #60a5fa)',
    thumbAccent: 'linear-gradient(90deg, #8b5cf6, #3b82f6)',
  },
]

const DEFAULT: AppearanceState = { preset: 'ocean-blue', mode: 'system' }

export function getPreset(id: ThemePresetId): ThemePreset {
  return THEME_PRESETS.find((p) => p.id === id) || THEME_PRESETS[4]
}

export function readAppearance(): AppearanceState {
  try {
    const raw = localStorage.getItem(APPEARANCE_STORAGE_KEY)
    if (!raw) return { ...DEFAULT }
    const parsed = JSON.parse(raw) as Partial<AppearanceState>
    const preset = THEME_PRESETS.some((p) => p.id === parsed.preset)
      ? (parsed.preset as ThemePresetId)
      : DEFAULT.preset
    const mode: ColorMode =
      parsed.mode === 'light' || parsed.mode === 'dark' || parsed.mode === 'system'
        ? parsed.mode
        : DEFAULT.mode
    return { preset, mode }
  } catch {
    return { ...DEFAULT }
  }
}

export function writeAppearance(state: AppearanceState) {
  localStorage.setItem(APPEARANCE_STORAGE_KEY, JSON.stringify(state))
}

export function resolveColorMode(mode: ColorMode): 'light' | 'dark' {
  if (mode === 'light' || mode === 'dark') return mode
  if (typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return 'dark'
  }
  return 'light'
}

export function applyAppearanceToDom(state: AppearanceState) {
  const preset = getPreset(state.preset)
  const resolved = resolveColorMode(state.mode)
  const root = document.documentElement
  root.setAttribute('data-theme-preset', preset.id)
  root.setAttribute('data-color-mode', resolved)
  root.setAttribute('data-color-mode-pref', state.mode)

  const isDark = resolved === 'dark'
  const siderBg = isDark ? '#141414' : preset.siderBg
  const siderText = isDark || preset.id === 'business-black' ? '#f5f5f5' : preset.siderText
  const headerBg = isDark ? '#1f1f1f' : preset.headerBg
  const border = isDark ? '#303030' : preset.border
  // 浅色：page 与 content 同色，避免窄表单旁露出 tint 留白（与深色铺满观感一致）
  const contentBg = isDark ? '#1f1f1f' : preset.contentBg
  const pageBg = isDark ? '#141414' : contentBg
  const primarySoft = isDark ? '#111827' : preset.primarySoft

  root.style.setProperty('--app-color-primary', preset.primary)
  root.style.setProperty('--app-color-primary-soft', primarySoft)
  root.style.setProperty('--app-sider-bg', siderBg)
  root.style.setProperty('--app-sider-text', siderText)
  root.style.setProperty('--app-sider-active-bg', isDark ? '#1e293b' : preset.siderActiveBg)
  root.style.setProperty('--app-sider-active-color', isDark ? preset.primary : preset.siderActiveColor)
  root.style.setProperty('--app-header-bg', headerBg)
  root.style.setProperty('--app-border', border)
  root.style.setProperty('--app-page-bg', pageBg)
  root.style.setProperty('--app-content-bg', contentBg)
  root.style.setProperty('--app-text', isDark ? '#f5f5f5' : '#1f2937')
  root.style.setProperty('--app-text-secondary', isDark ? '#a3a3a3' : '#8c8c8c')
}

export function readSiderCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDER_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

export function writeSiderCollapsed(collapsed: boolean) {
  localStorage.setItem(SIDER_COLLAPSED_KEY, collapsed ? '1' : '0')
}
