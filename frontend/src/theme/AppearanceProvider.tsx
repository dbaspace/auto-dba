import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { ConfigProvider, theme as antTheme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import {
  AppearanceState,
  ColorMode,
  ThemePresetId,
  applyAppearanceToDom,
  getPreset,
  readAppearance,
  resolveColorMode,
  writeAppearance,
} from './appearance'
import './appearance.css'

type AppearanceCtx = AppearanceState & {
  setPreset: (preset: ThemePresetId) => void
  setMode: (mode: ColorMode) => void
  resolvedMode: 'light' | 'dark'
}

const Ctx = createContext<AppearanceCtx | null>(null)

export function useAppearance(): AppearanceCtx {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAppearance must be used within AppearanceProvider')
  return v
}

export function AppearanceProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<AppearanceState>(() => readAppearance())
  const [systemDark, setSystemDark] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)').matches : false,
  )

  useEffect(() => {
    applyAppearanceToDom(state)
    writeAppearance(state)
  }, [state, systemDark])

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setSystemDark(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  const setPreset = useCallback((preset: ThemePresetId) => {
    setState((s) => ({ ...s, preset }))
  }, [])

  const setMode = useCallback((mode: ColorMode) => {
    setState((s) => ({ ...s, mode }))
  }, [])

  const resolvedMode = resolveColorMode(state.mode)
  const preset = getPreset(state.preset)

  const value = useMemo(
    () => ({
      ...state,
      setPreset,
      setMode,
      resolvedMode,
    }),
    [state, setPreset, setMode, resolvedMode],
  )

  return (
    <Ctx.Provider value={value}>
      <ConfigProvider
        locale={zhCN}
        theme={{
          algorithm: resolvedMode === 'dark' ? antTheme.darkAlgorithm : antTheme.defaultAlgorithm,
          token: {
            colorPrimary: preset.primary,
            colorLink: preset.primary,
            borderRadius: 2,
            fontSize: 12,
          },
        }}
      >
        {children}
      </ConfigProvider>
    </Ctx.Provider>
  )
}
