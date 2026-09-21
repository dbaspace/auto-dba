import React, { useLayoutEffect, useRef, useState } from 'react'
import { Button, Card, Tag, Tooltip } from 'antd'
import {
  ArrowDownOutlined,
  ArrowRightOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  ClockCircleFilled,
  LoadingOutlined,
  WarningFilled,
} from '@ant-design/icons'
import './PipelineVisualization.css'

export interface PipelineExtra {
  label: string
  value: string
}
export interface PipelineNode {
  id: string
  name: string
  detail?: string
  tone?: string
  extras?: PipelineExtra[]
  message?: string
  ready?: boolean
}

interface Props {
  nodes?: PipelineNode[]
  onOpenConnector?: () => void
}

const META: Record<string, { color: string; bg: string }> = {
  'is-ok': { color: '#52c41a', bg: '#f6ffed' },
  'is-run': { color: '#1677ff', bg: '#e6f4ff' },
  'is-error': { color: '#ff4d4f', bg: '#fff2f0' },
  'is-warn': { color: '#d48806', bg: '#fffbe6' },
  pending: { color: '#8c8c8c', bg: '#fafafa' },
}

function bgOf(tone?: string) {
  return (META[tone || ''] || META.pending).bg
}
function colorOf(tone?: string) {
  return (META[tone || ''] || META.pending).color
}
function IconOf({ tone, color }: { tone?: string; color: string }) {
  const style = { color, fontSize: 16 }
  if (tone === 'is-ok') return <CheckCircleFilled style={style} />
  if (tone === 'is-error') return <CloseCircleFilled style={style} />
  if (tone === 'is-run') return <LoadingOutlined style={style} />
  if (tone === 'is-warn') return <WarningFilled style={style} />
  return <ClockCircleFilled style={style} />
}
function isTagValue(e: PipelineExtra) {
  const v = String(e?.value ?? '')
  if (['是', '否', 'active', 'inactive'].includes(v)) return true
  if (['Publication 表', 'topics', '配置表数'].includes(e?.label) && /^\d+$/.test(v)) return true
  return false
}
function tagColor(v: string) {
  const s = String(v)
  if (s === '是' || s === 'active') return 'success'
  if (s === '否' || s === 'inactive') return 'default'
  return 'success'
}

function Arrow() {
  return (
    <span className="pv-arrow" aria-hidden>
      <ArrowRightOutlined />
    </span>
  )
}

function NodeCard({ n, onOpenConnector }: { n: PipelineNode; onOpenConnector?: () => void }) {
  return (
    <Tooltip title={n.message || ''}>
      <Card size="small" className="pv-card" styles={{ body: { padding: 8, background: bgOf(n.tone) } }}>
        <div className="flex items-start gap-1.5 mb-1">
          <span className="shrink-0 leading-none pt-0.5">
            <IconOf tone={n.tone} color={colorOf(n.tone)} />
          </span>
          <span className="font-medium text-sm leading-snug break-words">{n.name}</span>
        </div>
        {n.detail ? <div className="text-xs text-gray-600 mb-1 break-all leading-snug">{n.detail}</div> : null}
        {n.extras?.length ? (
          <div className="flex flex-col gap-0.5 text-xs leading-snug">
            {n.extras.map((e) => (
              <div key={e.label} className="flex flex-wrap items-baseline gap-x-1.5 gap-y-0">
                <span className="text-gray-400 shrink-0">{e.label}</span>
                <span className="text-gray-700 break-all">
                  {isTagValue(e) ? (
                    <Tag color={tagColor(e.value)} bordered={false} className="!m-0 !leading-none">
                      {e.value}
                    </Tag>
                  ) : (
                    e.value
                  )}
                </span>
              </div>
            ))}
          </div>
        ) : null}
        {n.id === 'debezium' ? (
          <div className="mt-1.5 pt-1.5 border-t border-gray-200">
            <Button
              type="link"
              size="small"
              className="!px-0 !h-auto text-xs"
              disabled={!n.ready}
              onClick={() => onOpenConnector?.()}
            >
              查看运行状态
            </Button>
          </div>
        ) : null}
      </Card>
    </Tooltip>
  )
}

/**
 * 全链路管道可视化
 *
 *   PG → Publication → Slot → Debezium → Kafka   （左对齐，按内容取宽）
 *                                            ↓      （落在 Kafka 与 StarRocks 中点）
 *                             StarRocks → Routine Load   （整组贴右）
 */
export default function PipelineVisualization({ nodes = [], onOpenConnector }: Props) {
  const head = nodes.slice(0, 5)
  const starrocks = nodes[5]
  const routineLoad = nodes[6]

  const boardRef = useRef<HTMLDivElement>(null)
  const kafkaRef = useRef<HTMLDivElement>(null)
  const srRef = useRef<HTMLDivElement>(null)
  const [turn, setTurn] = useState<{ left: number; top: number } | null>(null)

  useLayoutEffect(() => {
    const update = () => {
      const board = boardRef.current
      const kEl = kafkaRef.current
      const sEl = srRef.current
      if (!board || !kEl || !sEl) {
        setTurn(null)
        return
      }
      const br = board.getBoundingClientRect()
      const kr = kEl.getBoundingClientRect()
      const sr = sEl.getBoundingClientRect()
      // Kafka 底边中心 ↔ StarRocks 顶边中心，取几何中点
      const x1 = kr.left + kr.width / 2 - br.left
      const y1 = kr.bottom - br.top
      const x2 = sr.left + sr.width / 2 - br.left
      const y2 = sr.top - br.top
      setTurn({ left: (x1 + x2) / 2, top: (y1 + y2) / 2 })
    }

    update()
    const ro = new ResizeObserver(update)
    if (boardRef.current) ro.observe(boardRef.current)
    if (kafkaRef.current) ro.observe(kafkaRef.current)
    if (srRef.current) ro.observe(srRef.current)
    window.addEventListener('resize', update)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', update)
    }
  }, [nodes])

  return (
    <div className="pv-scroll">
      <div className="pv-board" ref={boardRef}>
        <div className="pv-row-head">
          {head.map((n, i) => (
            <React.Fragment key={n.id}>
              {i > 0 ? <Arrow /> : null}
              <div ref={i === 4 ? kafkaRef : undefined} className="pv-head-cell">
                <NodeCard n={n} onOpenConnector={onOpenConnector} />
              </div>
            </React.Fragment>
          ))}
        </div>

        <div className="pv-tail-block">
          {starrocks ? (
            <div className="pv-tail-sr" ref={srRef}>
              <NodeCard n={starrocks} onOpenConnector={onOpenConnector} />
            </div>
          ) : null}
          {starrocks && routineLoad ? <Arrow /> : null}
          {routineLoad ? (
            <div className="pv-tail-rl">
              <NodeCard n={routineLoad} onOpenConnector={onOpenConnector} />
            </div>
          ) : null}
        </div>

        {turn ? (
          <div
            className="pv-turn"
            aria-hidden
            style={{ left: turn.left, top: turn.top }}
          >
            <ArrowDownOutlined />
          </div>
        ) : null}
      </div>
    </div>
  )
}
