import React, { useEffect, useMemo, useState } from 'react'
import { Button, Select, Space, Tag, Typography, message } from 'antd'
import { CI_TYPES, getGraph, getImpact, listCIs, type CmdbCI, type CmdbGraph, type CmdbImpact } from './services/cmdbApi'
import TopologyGraph from './TopologyGraph'

function placeholderFor(type: string) {
  if (type === 'host') return '搜索主机名或 host:IP'
  const label = CI_TYPES.find((t) => t.value === type)?.label || '配置项'
  return `搜索${label}名称或 ID`
}

function optionLabel(ci: CmdbCI) {
  return `${ci.name} (${ci.id})`
}

export default function CmdbTopology() {
  const [type, setType] = useState('instance')
  const [id, setId] = useState('')
  const [depth, setDepth] = useState(2)
  const [relFilter, setRelFilter] = useState<string | undefined>()
  const [graph, setGraph] = useState<CmdbGraph | null>(null)
  const [impact, setImpact] = useState<CmdbImpact | null>(null)
  const [direction, setDirection] = useState('down')
  const [loading, setLoading] = useState(false)
  const [optionsLoading, setOptionsLoading] = useState(false)
  const [cis, setCis] = useState<CmdbCI[]>([])
  const [searchText, setSearchText] = useState('')

  async function loadOptions(ciType: string) {
    setOptionsLoading(true)
    try {
      const list = await listCIs({ type: ciType, limit: 500 })
      setCis(list)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载配置项列表失败')
      setCis([])
    } finally {
      setOptionsLoading(false)
    }
  }

  useEffect(() => {
    void loadOptions(type)
  }, [])

  function onTypeChange(next: string) {
    setType(next)
    setId('')
    setSearchText('')
    setGraph(null)
    setImpact(null)
    setRelFilter(undefined)
    void loadOptions(next)
  }

  const options = useMemo(() => {
    const seen = new Set<string>()
    const out: { value: string; label: string }[] = []
    const push = (value: string, label: string) => {
      if (!value || seen.has(value)) return
      seen.add(value)
      out.push({ value, label })
    }
    for (const ci of cis) {
      push(ci.id, optionLabel(ci))
    }
    if (id && !seen.has(id)) {
      push(id, id)
    }
    const q = searchText.trim()
    if (q && !seen.has(q)) {
      push(q, q)
    }
    return out
  }, [cis, id, searchText])

  async function loadGraph(ciType = type, ciId = id) {
    if (!ciId.trim()) {
      message.warning('请选择配置项')
      return
    }
    setLoading(true)
    try {
      const g = await getGraph(ciType, ciId.trim(), depth)
      setGraph(g)
      setImpact(null)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  async function loadImpact() {
    if (!id.trim()) {
      message.warning('请选择配置项')
      return
    }
    setLoading(true)
    try {
      const rep = await getImpact(type, id.trim(), direction, depth)
      setImpact(rep)
      setGraph({ nodes: rep.nodes, edges: rep.edges })
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '影响分析失败')
    } finally {
      setLoading(false)
    }
  }

  async function focusNode(n: CmdbCI) {
    setType(n.type)
    setSearchText('')
    setRelFilter(undefined)
    setOptionsLoading(true)
    try {
      const list = await listCIs({ type: n.type, limit: 500 })
      setCis(list)
    } catch {
      setCis([])
    } finally {
      setOptionsLoading(false)
    }
    setId(n.id)
    await loadGraph(n.type, n.id)
  }

  const edges = (graph?.edges || []).filter((e) => !relFilter || e.rel_type === relFilter)
  const relTypes = Array.from(new Set((graph?.edges || []).map((e) => e.rel_type))).sort()

  return (
    <div className="ops-page">
      <Space wrap style={{ marginBottom: 8 }}>
        <Select style={{ width: 180 }} value={type} options={CI_TYPES} onChange={onTypeChange} />
        <Select
          showSearch
          allowClear
          style={{ width: 360 }}
          placeholder={placeholderFor(type)}
          loading={optionsLoading}
          options={options}
          optionFilterProp="label"
          value={id || undefined}
          searchValue={searchText}
          onSearch={setSearchText}
          onChange={(v) => {
            setId(v || '')
            setSearchText('')
          }}
          onInputKeyDown={(e) => {
            if (e.key === 'Enter' && searchText.trim()) {
              setId(searchText.trim())
            }
          }}
          filterOption={(input, option) => {
            const label = String(option?.label || '').toLowerCase()
            const value = String(option?.value || '').toLowerCase()
            const q = input.toLowerCase()
            return label.includes(q) || value.includes(q)
          }}
          notFoundContent={optionsLoading ? '加载中…' : '无匹配项，可粘贴完整 ID 后回车'}
        />
        <Select
          style={{ width: 120 }}
          value={depth}
          options={[
            { value: 1, label: '深度 1' },
            { value: 2, label: '深度 2' },
            { value: 3, label: '深度 3' },
          ]}
          onChange={setDepth}
        />
        <Select
          allowClear
          style={{ width: 160 }}
          placeholder="关系类型"
          value={relFilter}
          options={relTypes.map((t) => ({ value: t, label: t }))}
          onChange={setRelFilter}
        />
        <Button type="primary" loading={loading} onClick={() => void loadGraph()}>
          加载拓扑
        </Button>
        <Select
          style={{ width: 120 }}
          value={direction}
          options={[
            { value: 'down', label: '下游影响' },
            { value: 'up', label: '上游依赖' },
            { value: 'both', label: '双向' },
          ]}
          onChange={setDirection}
        />
        <Button loading={loading} onClick={() => void loadImpact()}>
          影响分析
        </Button>
      </Space>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 12, marginTop: 0 }}>
        从左侧类型选择根节点后加载拓扑图。实线箭头为主关系、虚线为次/反向；悬停边可看关系名。点击节点可切换根节点并重新加载；也可粘贴完整 ID（主机为 <code>host:IP</code>）后回车。
      </Typography.Paragraph>
      {impact && (
        <div style={{ marginBottom: 12, padding: 12, background: '#fafafa', borderRadius: 6 }}>
          <Space wrap>
            <Tag color="blue">方向 {impact.direction}</Tag>
            <Tag>上游 {impact.summary.upstream}</Tag>
            <Tag color="orange">下游 {impact.summary.downstream}</Tag>
            {Object.entries(impact.summary.by_type || {}).map(([k, v]) => (
              <Tag key={k}>
                {k}: {v}
              </Tag>
            ))}
            {(impact.summary.app_tags || []).map((t) => (
              <Tag key={t} color="purple">
                {t}
              </Tag>
            ))}
          </Space>
        </div>
      )}
      {graph && (
        <>
          <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
            {graph.nodes.length} 节点 · {edges.length} 关系
          </Typography.Text>
          <TopologyGraph graph={graph} relFilter={relFilter} onNodeClick={(n) => void focusNode(n)} />
        </>
      )}
    </div>
  )
}
