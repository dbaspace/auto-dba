import React, { useCallback, useEffect, useState } from 'react'
import { Empty, Select, Space, Spin, message } from 'antd'
import { Link } from 'react-router-dom'
import api from '../../api/request'
import './NacosOps.css'

interface NacosRow {
  id?: string
  environment?: string
  console_url?: string
  username?: string
}

function embedConsoleURL(env: string) {
  // 鉴权走 Cookie（seedAuthCookie）；勿把 JWT 拼进 URL，避免续期导致 iframe remount 反复 bootstrap。
  return `/api/v1/nacos-console/${encodeURIComponent(env)}`
}

function seedAuthCookie() {
  const token = localStorage.getItem('token') || ''
  if (token) {
    document.cookie = `token=${encodeURIComponent(token)}; path=/; SameSite=Lax`
  }
}

export default function NacosOps() {
  const [rows, setRows] = useState<NacosRow[]>([])
  const [env, setEnv] = useState<string>()
  const [loading, setLoading] = useState(false)
  const [warming, setWarming] = useState(false)
  const [readyEnv, setReadyEnv] = useState<string>()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/nacosadmin')
      const list: NacosRow[] = data.data || []
      setRows(list)
      setEnv((prev) => {
        const envs = list.map((r) => r.environment).filter(Boolean) as string[]
        if (prev && envs.includes(prev)) return prev
        return envs[0]
      })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!env) {
      setReadyEnv(undefined)
      return
    }
    let cancelled = false
    seedAuthCookie()
    // 与代登并行挂 iframe：静态走本地资源，不必等 warm 完成才出壳
    setReadyEnv(env)
    setWarming(true)
    ;(async () => {
      try {
        await api.get('/nacosadmin/warm', { params: { env, environment: env } })
      } catch (e: any) {
        if (!cancelled) {
          message.warning(e?.response?.data?.message || e.message || '代登预热失败，仍尝试打开控制台')
        }
      } finally {
        if (!cancelled) {
          setWarming(false)
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [env])

  const envOptions = rows
    .map((r) => r.environment)
    .filter(Boolean)
    .filter((v, i, arr) => arr.indexOf(v) === i)
    .map((v) => ({ value: v as string, label: v as string }))

  const iframeSrc = readyEnv ? embedConsoleURL(readyEnv) : ''

  if (!loading && rows.length === 0) {
    return (
      <div className="ops-page">
        <Empty
          description={
            <span>
              尚未登记 Nacos 控制台凭据，请先到 <Link to="/config/nacos-admin">配置中心 · Nacos 配置</Link> 录入。
            </span>
          }
        />
      </div>
    )
  }

  return (
    <div className="ops-page nacos-ops-page">
      <div className="nacos-ops-shell">
        <div className="nacos-ops-toolbar">
          <Space wrap>
            <span>环境</span>
            <Select
              style={{ width: 180 }}
              value={env}
              options={envOptions}
              onChange={(v) => setEnv(v)}
              placeholder="选择环境"
            />
          </Space>
        </div>
        <div className="nacos-ops-frame-wrap">
          {loading || !iframeSrc ? (
            <div className="nacos-ops-empty">
              <Spin tip="加载中…">
                <div style={{ padding: 48 }} />
              </Spin>
            </div>
          ) : (
            <>
              {warming ? (
                <div className="nacos-ops-warm-hint" aria-live="polite">
                  正在代登…
                </div>
              ) : null}
              <iframe
                key={readyEnv}
                src={iframeSrc}
                className="embed nacos-ops-iframe"
                title="nacos-console"
                allow="clipboard-read; clipboard-write"
                sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-downloads allow-modals"
              />
            </>
          )}
        </div>
      </div>
    </div>
  )
}
