import React, { useMemo } from 'react'
import { Tag, Tooltip } from 'antd'
import './HaArchitectureDiagram.css'

export type ArchController = {
  id: string
  name: string
  kind: string
  base_url?: string
  remark?: string
  role?: 'primary' | 'standby'
}

export type ArchMember = {
  instance_id: string
  instance_name?: string
  host?: string
  port?: number
  role?: string
  registered_role?: string
  healthy?: boolean
  lag_seconds?: number
  error?: string
}

export type HaArchitectureDiagramProps = {
  mode: string
  engine?: string
  vip?: string
  controllers: ArchController[]
  members: ArchMember[]
  /** template 模式：无探测，灰色示意 */
  variant?: 'live' | 'template' | 'card'
  emptyHint?: string
  className?: string
}

function shortHost(host?: string, name?: string) {
  const s = (host || name || '').trim()
  if (!s) return '?'
  if (s.length <= 18) return s
  return s.slice(0, 16) + '…'
}

function nodeTone(m: ArchMember, template: boolean): string {
  if (template) return 'neutral'
  if (m.error || m.healthy === false) return 'bad'
  if (m.lag_seconds != null && m.lag_seconds > 30) return 'warn'
  if (m.healthy) return 'ok'
  return 'neutral'
}

/**
 * 高可用架构图：控制面（Manager）/ 数据面（主从）/ VIP。
 * 不引入额外图库，用 CSS 分层布局，适配 MHA / Orchestrator / manual。
 */
export default function HaArchitectureDiagram({
  mode,
  vip,
  controllers,
  members,
  variant = 'live',
  emptyHint,
  className,
}: HaArchitectureDiagramProps) {
  const template = variant === 'template' || variant === 'card'
  const compact = variant === 'card'

  const { primaries, replicas, others } = useMemo(() => {
    const p: ArchMember[] = []
    const r: ArchMember[] = []
    const o: ArchMember[] = []
    for (const m of members) {
      const role = (m.role || m.registered_role || '').toLowerCase()
      if (role === 'primary' || role === 'single') p.push(m)
      else if (role === 'replica') r.push(m)
      else o.push(m)
    }
    return { primaries: p, replicas: r, others: o }
  }, [members])

  const primaryCtrls = controllers.filter((c) => c.role !== 'standby')
  const standbyCtrls = controllers.filter((c) => c.role === 'standby')
  const modeLower = (mode || '').toLowerCase()
  const showControl = modeLower === 'mha' || modeLower === 'orchestrator' || modeLower === 'proxysql_repl' || primaryCtrls.length > 0
  const controlLabel =
    modeLower === 'mha' ? 'MHA Manager' :
      modeLower === 'orchestrator' ? 'Orchestrator' :
        modeLower === 'proxysql_repl' ? 'ProxySQL' :
          '控制器'

  const hasData = members.length > 0 || controllers.length > 0 || template

  if (!hasData) {
    return (
      <div className={`ha-arch ${compact ? 'ha-arch--card' : ''} ${className || ''}`}>
        <div className="ha-arch-empty">{emptyHint || '暂无成员与控制器，登记后显示架构图'}</div>
      </div>
    )
  }

  return (
    <div className={`ha-arch ${compact ? 'ha-arch--card' : ''} ${template && variant === 'template' ? 'ha-arch--template' : ''} ${className || ''}`}>
      <div className="ha-arch-meta">
        <Tag>{mode || 'manual'}</Tag>
        {vip ? <Tag color="blue">VIP {vip}</Tag> : null}
        {variant === 'live' && <span className="ha-arch-hint">探测与登记同源 · 切主请走变更</span>}
        {variant === 'template' && <span className="ha-arch-hint">架构模板预览</span>}
      </div>

      {vip && (
        <div className="ha-arch-layer ha-arch-layer--vip">
          <div className="ha-arch-layer-title">流量入口</div>
          <div className="ha-arch-node ha-arch-node--vip">
            <div className="ha-arch-node-title">VIP</div>
            <div className="ha-arch-node-sub">{vip}</div>
          </div>
          <div className="ha-arch-vline" />
        </div>
      )}

      {showControl && (
        <div className="ha-arch-layer ha-arch-layer--ctrl">
          <div className="ha-arch-layer-title">控制面 · {controlLabel}</div>
          <div className="ha-arch-row">
            {(primaryCtrls.length ? primaryCtrls : [{ id: 'tpl-mgr', name: controlLabel, kind: modeLower || 'ctrl', role: 'primary' as const }]).map((c) => (
              <Tooltip key={c.id} title={[c.base_url, c.remark].filter(Boolean).join('\n') || undefined}>
                <div className={`ha-arch-node ha-arch-node--mgr ${c.role === 'standby' ? 'is-standby' : 'is-primary'}`}>
                  <div className="ha-arch-node-badge">{c.role === 'standby' ? 'Standby' : 'Primary'}</div>
                  <div className="ha-arch-node-title">{c.name || controlLabel}</div>
                  <div className="ha-arch-node-sub">{c.kind}{c.base_url ? ` · ${shortHost(c.base_url)}` : ''}</div>
                </div>
              </Tooltip>
            ))}
            {standbyCtrls.map((c) => (
              <Tooltip key={c.id} title={[c.base_url, c.remark].filter(Boolean).join('\n') || undefined}>
                <div className="ha-arch-node ha-arch-node--mgr is-standby">
                  <div className="ha-arch-node-badge">Standby</div>
                  <div className="ha-arch-node-title">{c.name}</div>
                  <div className="ha-arch-node-sub">{c.kind}{c.base_url ? ` · ${shortHost(c.base_url)}` : ''}</div>
                </div>
              </Tooltip>
            ))}
            {template && !controllers.length && modeLower === 'mha' && (
              <div className="ha-arch-node ha-arch-node--mgr is-standby is-ghost">
                <div className="ha-arch-node-badge">Standby</div>
                <div className="ha-arch-node-title">可选备 Manager</div>
                <div className="ha-arch-node-sub">可登记多台 masterha_manager</div>
              </div>
            )}
          </div>
          <div className="ha-arch-vline" />
          <div className="ha-arch-dash-hint">管理通道（SSH / API）</div>
        </div>
      )}

      {!showControl && (modeLower === 'manual' || modeLower === 'async_repl') && (
        <div className="ha-arch-layer ha-arch-layer--ctrl">
          <div className="ha-arch-layer-title">控制面</div>
          <div className="ha-arch-node ha-arch-node--manual">无自动切换控制器 · 人工/手册切换</div>
          <div className="ha-arch-vline" />
        </div>
      )}

      <div className="ha-arch-layer ha-arch-layer--data">
        <div className="ha-arch-layer-title">数据面 · 复制拓扑</div>
        <div className="ha-arch-data">
          <div className="ha-arch-col">
            {(primaries.length ? primaries : (template ? [{ instance_id: 'tpl-p', instance_name: 'Primary', role: 'primary', healthy: true }] : [])).map((m) => (
              <div key={m.instance_id} className={`ha-arch-node ha-arch-node--db tone-${nodeTone(m, template)}`}>
                <div className="ha-arch-node-badge">Primary</div>
                <div className="ha-arch-node-title">{shortHost(m.host, m.instance_name)}</div>
                <div className="ha-arch-node-sub">
                  {m.port ? `:${m.port}` : ''}
                  {!template && m.lag_seconds != null ? ` · lag ${Number(m.lag_seconds).toFixed(1)}s` : ''}
                </div>
              </div>
            ))}
          </div>
          {(replicas.length > 0 || template) && (
            <>
              <div className="ha-arch-arrows" aria-hidden>
                <span>复制</span>
              </div>
              <div className="ha-arch-col ha-arch-col--replicas">
                {(replicas.length ? replicas : (template ? [
                  { instance_id: 'tpl-r1', instance_name: 'Replica-1', role: 'replica', healthy: true },
                  { instance_id: 'tpl-r2', instance_name: 'Replica-N', role: 'replica', healthy: true },
                ] : [])).map((m) => (
                  <div key={m.instance_id} className={`ha-arch-node ha-arch-node--db tone-${nodeTone(m, template)}`}>
                    <div className="ha-arch-node-badge">Replica</div>
                    <div className="ha-arch-node-title">{shortHost(m.host, m.instance_name)}</div>
                    <div className="ha-arch-node-sub">
                      {m.port ? `:${m.port}` : ''}
                      {!template && m.lag_seconds != null ? ` · lag ${Number(m.lag_seconds).toFixed(1)}s` : ''}
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
        {others.length > 0 && (
          <div className="ha-arch-row" style={{ marginTop: 10 }}>
            {others.map((m) => (
              <div key={m.instance_id} className={`ha-arch-node ha-arch-node--db tone-${nodeTone(m, template)}`}>
                <div className="ha-arch-node-badge">{m.role || 'node'}</div>
                <div className="ha-arch-node-title">{shortHost(m.host, m.instance_name)}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
