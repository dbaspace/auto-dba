import React, { useMemo } from 'react'
import { Tag, Tooltip } from 'antd'
import './ProxyArchitectureDiagram.css'

export type ProxyArchBackend = {
  name?: string
  host: string
  port: number
  weight?: number
  backup?: boolean
}

export type ProxyArchHANode = {
  name?: string
  role?: string
  priority?: number
}

export type ProxyArchitectureDiagramProps = {
  kind: string
  name?: string
  vip?: string
  listenPort?: number
  backends: ProxyArchBackend[]
  haNodes?: ProxyArchHANode[]
  /** LVS 可选：转发 / 调度 */
  lbKind?: string
  lbAlgo?: string
  interfaceName?: string
  emptyHint?: string
  className?: string
  /** create 弹窗内预览 */
  compact?: boolean
}

const KIND_TITLE: Record<string, string> = {
  haproxy: 'HAProxy',
  nginx: 'Nginx',
  lvs: 'Keepalived + IPVS',
}

function short(s?: string, max = 18) {
  const t = (s || '').trim()
  if (!t) return '?'
  if (t.length <= max) return t
  return t.slice(0, max - 1) + '…'
}

/**
 * 接入层代理架构图：客户端 → VIP/监听 → 代理 → Real Server。
 * 风格对齐 HA 架构图，不引入额外图库。
 */
export default function ProxyArchitectureDiagram({
  kind,
  name,
  vip,
  listenPort,
  backends,
  haNodes,
  lbKind,
  lbAlgo,
  interfaceName,
  emptyHint,
  className,
  compact,
}: ProxyArchitectureDiagramProps) {
  const k = (kind || '').toLowerCase()
  const title = KIND_TITLE[k] || kind || 'Proxy'
  const entry =
    vip
      ? `VIP ${vip}${listenPort ? `:${listenPort}` : ''}`
      : listenPort
        ? `*:${listenPort}`
        : ''

  const nodes = useMemo(() => {
    return (backends || []).filter((b) => (b.host || '').trim())
  }, [backends])

  const pair = useMemo(() => {
    return (haNodes || []).filter((n) => (n.role || n.name || '').trim())
  }, [haNodes])

  if (!nodes.length && !entry && !pair.length) {
    return (
      <div className={`proxy-arch ${compact ? 'proxy-arch--compact' : ''} ${className || ''}`}>
        <div className="proxy-arch-empty">{emptyHint || '配置后端实例后显示架构图'}</div>
      </div>
    )
  }

  return (
    <div className={`proxy-arch ${compact ? 'proxy-arch--compact' : ''} ${className || ''}`}>
      <div className="proxy-arch-meta">
        <Tag color="processing">{title}</Tag>
        {name ? <Tag>{name}</Tag> : null}
        {pair.length ? <Tag color="geekblue">HA ×{pair.length}</Tag> : null}
        {k === 'lvs' && lbKind ? <Tag>lb_kind {lbKind}</Tag> : null}
        {k === 'lvs' && lbAlgo ? <Tag>lb_algo {lbAlgo}</Tag> : null}
        <span className="proxy-arch-hint">接入层流量路径 · 配置与图同源</span>
      </div>

      <div className="proxy-arch-flow">
        <div className="proxy-arch-layer">
          <div className="proxy-arch-layer-title">客户端</div>
          <div className="proxy-arch-node proxy-arch-node--client">
            <div className="proxy-arch-node-badge">App / Client</div>
            <div className="proxy-arch-node-title">业务流量</div>
          </div>
        </div>

        <div className="proxy-arch-arrow" aria-hidden>
          <span>访问</span>
        </div>

        <div className="proxy-arch-layer">
          <div className="proxy-arch-layer-title">流量入口 · VIP</div>
          <div className="proxy-arch-node proxy-arch-node--vip">
            <div className="proxy-arch-node-badge">VIP</div>
            <div className="proxy-arch-node-title">{entry || '未配置'}</div>
            {interfaceName ? (
              <div className="proxy-arch-node-sub">dev {interfaceName}</div>
            ) : null}
          </div>
        </div>

        <div className="proxy-arch-arrow" aria-hidden>
          <span>VRRP</span>
        </div>

        <div className="proxy-arch-layer">
          <div className="proxy-arch-layer-title">
            代理层 · {title}
            {pair.length ? ` · ${pair.length} 节点` : ''}
          </div>
          {pair.length > 0 ? (
            <div className="proxy-arch-rs-row">
              {pair.map((n, i) => {
                const role = (n.role || '').toLowerCase()
                const isMaster = role === 'master'
                return (
                  <Tooltip
                    key={`${n.name}-${i}`}
                    title={[n.name, role, n.priority != null ? `priority ${n.priority}` : ''].filter(Boolean).join(' · ')}
                  >
                    <div className={`proxy-arch-node proxy-arch-node--proxy ${isMaster ? '' : 'is-backup'}`}>
                      <div className="proxy-arch-node-badge">{isMaster ? 'MASTER' : 'BACKUP'}</div>
                      <div className="proxy-arch-node-title">{short(n.name || (isMaster ? 'master' : `backup${i}`), 14)}</div>
                      <div className="proxy-arch-node-sub">
                        {n.priority != null ? `prio ${n.priority}` : k.toUpperCase()}
                      </div>
                    </div>
                  </Tooltip>
                )
              })}
            </div>
          ) : (
            <div className="proxy-arch-node proxy-arch-node--proxy">
              <div className="proxy-arch-node-badge">{k.toUpperCase()}</div>
              <div className="proxy-arch-node-title">{short(name || title, 22)}</div>
              <div className="proxy-arch-node-sub">
                {k === 'lvs'
                  ? `${lbKind || 'DR'} · ${lbAlgo || 'wrr'}`
                  : 'balance → backends'}
              </div>
            </div>
          )}
        </div>

        <div className="proxy-arch-arrow" aria-hidden>
          <span>{k === 'lvs' ? 'Real Server' : 'upstream'}</span>
        </div>

        <div className="proxy-arch-layer proxy-arch-layer--rs">
          <div className="proxy-arch-layer-title">
            后端实例{nodes.length ? ` · ${nodes.length}` : ''}
          </div>
          {nodes.length === 0 ? (
            <div className="proxy-arch-node proxy-arch-node--empty-rs">尚未配置后端</div>
          ) : (
            <div className="proxy-arch-rs-row">
              {nodes.map((b, i) => {
                const label = b.name || `rs${i + 1}`
                const addr = `${b.host}:${b.port || listenPort || '?'}`
                const w = b.weight && b.weight > 0 ? b.weight : undefined
                return (
                  <Tooltip
                    key={`${b.host}-${b.port}-${i}`}
                    title={[addr, w != null ? `weight ${w}` : '', b.backup ? 'backup' : ''].filter(Boolean).join(' · ')}
                  >
                    <div className={`proxy-arch-node proxy-arch-node--rs ${b.backup ? 'is-backup' : ''}`}>
                      <div className="proxy-arch-node-badge">{b.backup ? 'Backup' : 'RS'}</div>
                      <div className="proxy-arch-node-title">{short(label, 14)}</div>
                      <div className="proxy-arch-node-sub">{short(addr, 22)}</div>
                      {w != null && <div className="proxy-arch-node-weight">w={w}</div>}
                    </div>
                  </Tooltip>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/** 解析 backends_json 或行文本 */
export function parseBackendsJSON(raw?: string): ProxyArchBackend[] {
  if (!raw || !raw.trim()) return []
  try {
    const v = JSON.parse(raw)
    if (!Array.isArray(v)) return []
    return v
      .map((x: any) => ({
        name: x?.name || '',
        host: String(x?.host || '').trim(),
        port: Number(x?.port) || 0,
        weight: Number(x?.weight) || undefined,
        backup: !!x?.backup,
      }))
      .filter((b: ProxyArchBackend) => b.host)
  } catch {
    return []
  }
}

/** 每行 host:port 或 host:port,weight */
export function parseBackendsText(text?: string, defaultPort = 3306): ProxyArchBackend[] {
  if (!text) return []
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, i) => {
      const [hp, w] = line.split(',')
      const [host, portS] = (hp || '').split(':')
      return {
        name: `s${i + 1}`,
        host: (host || '').trim(),
        port: Number(portS) || defaultPort,
        weight: Number(w) || 100,
      }
    })
    .filter((b) => b.host)
}

/** 从常见 conf 片段启发式提取后端（草稿改过 conf 时兜底） */
export function parseBackendsFromConf(kind: string, conf?: string, defaultPort = 3306): ProxyArchBackend[] {
  if (!conf) return []
  const k = kind.toLowerCase()
  const out: ProxyArchBackend[] = []
  const seen = new Set<string>()
  const add = (host: string, port: number, name?: string, weight?: number, backup?: boolean) => {
    const key = `${host}:${port}`
    if (!host || seen.has(key)) return
    seen.add(key)
    out.push({ name, host, port: port || defaultPort, weight, backup })
  }
  if (k === 'haproxy') {
    // server name host:port ... weight N [backup]
    const re = /^\s*server\s+(\S+)\s+(\S+?):(\d+)(?:\s+.*?\bweight\s+(\d+))?/gim
    let m: RegExpExecArray | null
    while ((m = re.exec(conf))) {
      add(m[2], Number(m[3]), m[1], m[4] ? Number(m[4]) : undefined, /\bbackup\b/i.test(m[0]))
    }
  } else if (k === 'nginx') {
    const re = /^\s*server\s+(\S+?):(\d+)(?:\s+weight=(\d+))?/gim
    let m: RegExpExecArray | null
    while ((m = re.exec(conf))) {
      add(m[1], Number(m[2]), undefined, m[3] ? Number(m[3]) : undefined)
    }
  } else if (k === 'lvs') {
    const re = /^\s*real_server\s+(\S+)\s+(\d+)/gim
    let m: RegExpExecArray | null
    while ((m = re.exec(conf))) {
      // weight 常在下一块；粗略再扫附近
      const slice = conf.slice(m.index, m.index + 200)
      const wm = /weight\s+(\d+)/i.exec(slice)
      add(m[1], Number(m[2]), undefined, wm ? Number(wm[1]) : undefined)
    }
  }
  return out
}
