import React, { useState } from 'react'
import { Button, Input, Select, Space, message } from 'antd'
import { CI_TYPES, getGraph, type CmdbGraph } from './services/cmdbApi'

export default function CmdbTopology() {
  const [type, setType] = useState('instance')
  const [id, setId] = useState('')
  const [depth, setDepth] = useState(1)
  const [graph, setGraph] = useState<CmdbGraph | null>(null)
  const [loading, setLoading] = useState(false)

  async function load() {
    if (!id.trim()) {
      message.warning('请填写 CI ID')
      return
    }
    setLoading(true)
    try {
      const g = await getGraph(type, id.trim(), depth)
      setGraph(g)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="ops-page">
      <Space wrap style={{ marginBottom: 12 }}>
        <Select style={{ width: 160 }} value={type} options={CI_TYPES} onChange={setType} />
        <Input style={{ width: 320 }} placeholder="配置项 ID" value={id} onChange={(e) => setId(e.target.value)} />
        <Select
          style={{ width: 120 }}
          value={depth}
          options={[
            { value: 1, label: '深度 1' },
            { value: 2, label: '深度 2' },
          ]}
          onChange={setDepth}
        />
        <Button type="primary" loading={loading} onClick={() => void load()}>
          加载拓扑
        </Button>
      </Space>
      {graph && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <div>
            <h4>节点 ({graph.nodes.length})</h4>
            <ul style={{ maxHeight: 480, overflow: 'auto' }}>
              {graph.nodes.map((n) => (
                <li key={`${n.type}:${n.id}`}>
                  <strong>{n.type}</strong> {n.name} <code>{n.id}</code>
                </li>
              ))}
            </ul>
          </div>
          <div>
            <h4>关系 ({graph.edges.length})</h4>
            <ul style={{ maxHeight: 480, overflow: 'auto' }}>
              {graph.edges.map((e, i) => (
                <li key={i}>
                  {e.src_type}:{e.src_id.slice(0, 8)}… — <TagRel t={e.rel_type} /> → {e.dst_type}:{e.dst_id.slice(0, 8)}…{' '}
                  <span style={{ color: '#888' }}>({e.source})</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  )
}

function TagRel({ t }: { t: string }) {
  return <span style={{ color: '#1677ff' }}>{t}</span>
}
