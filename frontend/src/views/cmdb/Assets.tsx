import React, { useEffect, useState } from 'react'
import { Button, Descriptions, Drawer, Input, Select, Space, Table, Tag, message } from 'antd'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import {
  CI_TYPES, createSnapshot, getCI, getImpact, listByApp, listCIs, listSnapshots,
  type CmdbCI, type CmdbImpact, type CmdbSnapshot,
} from './services/cmdbApi'

export default function CmdbAssets() {
  const [type, setType] = useState('instance')
  const [q, setQ] = useState('')
  const [appTag, setAppTag] = useState('')
  const [rows, setRows] = useState<CmdbCI[]>([])
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<(CmdbCI & { tags?: string[] }) | null>(null)
  const [impact, setImpact] = useState<CmdbImpact | null>(null)
  const [snaps, setSnaps] = useState<CmdbSnapshot[]>([])

  async function load() {
    setLoading(true)
    try {
      if (appTag.trim()) {
        const tag = appTag.includes('=') ? appTag.trim() : `app=${appTag.trim()}`
        setRows(await listByApp(tag))
      } else {
        setRows(await listCIs({ type, q: q || undefined, limit: 200 }))
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [type])

  async function openDetail(row: CmdbCI) {
    try {
      const res = await getCI(row.type, row.id)
      setDetail({ ...res.data, tags: res.tags })
      setImpact(null)
      setSnaps([])
      if (row.type === 'instance' || row.type === 'database') {
        try {
          setSnaps(await listSnapshots(row.type, row.id))
        } catch {
          /* optional */
        }
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || '详情加载失败')
    }
  }

  async function runImpact() {
    if (!detail) return
    try {
      setImpact(await getImpact(detail.type, detail.id, 'down', 2))
    } catch (e: any) {
      message.error(e?.response?.data?.message || '影响分析失败')
    }
  }

  async function takeSnap(baseline: boolean) {
    if (!detail) return
    try {
      await createSnapshot(detail.type, detail.id, baseline)
      message.success(baseline ? '已设为基线' : '已快照')
      setSnaps(await listSnapshots(detail.type, detail.id))
    } catch (e: any) {
      message.error(e?.response?.data?.message || '快照失败')
    }
  }

  return (
    <div className="ops-page">
      <div className="ops-toolbar" style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
        <Select style={{ width: 180 }} value={type} options={CI_TYPES} onChange={setType} disabled={!!appTag.trim()} />
        <Input.Search placeholder="搜索名称/ID/标签" allowClear style={{ width: 240 }} value={q} onChange={(e) => setQ(e.target.value)} onSearch={() => void load()} />
        <Input
          allowClear
          style={{ width: 180 }}
          placeholder="业务标签 app=xxx"
          value={appTag}
          onChange={(e) => setAppTag(e.target.value)}
          onPressEnter={() => void load()}
        />
        <Button type="primary" onClick={() => void load()} loading={loading}>
          查询
        </Button>
        <Link to="/cmdb/topology">拓扑 / 影响分析</Link>
      </div>
      <ResponsiveTable
        rowKey={(r) => `${r.type}:${r.id}`}
        loading={loading}
        dataSource={rows}
        columns={[
          { title: '名称', dataIndex: 'name', ellipsis: true },
          { title: '类型', dataIndex: 'type', width: 130 },
          { title: '状态', dataIndex: 'status', width: 100 },
          {
            title: '健康',
            dataIndex: 'health_badges',
            width: 160,
            render: (badges: string[]) => (
              <Space size={[4, 4]} wrap>
                {(badges || []).map((b) => (
                  <Tag key={b} color="warning">
                    {b}
                  </Tag>
                ))}
              </Space>
            ),
          },
          {
            title: '标签',
            dataIndex: 'tags',
            render: (tags: string[], row: CmdbCI) => (
              <Space size={[4, 4]} wrap>
                {(row.app_tags || []).map((t) => (
                  <Tag key={t} color="purple">
                    {t}
                  </Tag>
                ))}
                {(tags || []).filter((t) => !(row.app_tags || []).includes(t)).slice(0, 4).map((t) => (
                  <Tag key={t}>{t}</Tag>
                ))}
              </Space>
            ),
          },
          {
            title: '租户',
            width: 100,
            render: (_: unknown, row: CmdbCI) => {
              const tid = row.attrs?.tenant_id
              return tid ? String(tid).slice(0, 8) : '-'
            },
          },
          { title: '更新时间', dataIndex: 'updated_at', width: 170 },
          {
            title: '操作',
            width: 160,
            render: (_: unknown, row: CmdbCI) => (
              <Space>
                <Button type="link" size="small" onClick={() => void openDetail(row)}>
                  详情
                </Button>
                {row.edit_path ? <Link to={row.edit_path}>编辑</Link> : null}
              </Space>
            ),
          },
        ]}
        pagination={{ pageSize: 20 }}
      />
      <Drawer open={!!detail} width={640} title={detail?.name || '详情'} onClose={() => setDetail(null)}>
        {detail && (
          <>
            <Descriptions column={1} bordered size="small">
              <Descriptions.Item label="ID">{detail.id}</Descriptions.Item>
              <Descriptions.Item label="类型">{detail.type}</Descriptions.Item>
              <Descriptions.Item label="状态">{detail.status || '-'}</Descriptions.Item>
              <Descriptions.Item label="标签">
                <Space wrap>
                  {(detail.tags || []).map((t) => (
                    <Tag key={t}>{t}</Tag>
                  ))}
                </Space>
              </Descriptions.Item>
              <Descriptions.Item label="属性">
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(detail.attrs || {}, null, 2)}</pre>
              </Descriptions.Item>
              {detail.edit_path ? (
                <Descriptions.Item label="配置入口">
                  <Link to={detail.edit_path}>{detail.edit_path}</Link>
                </Descriptions.Item>
              ) : null}
            </Descriptions>
            <Space style={{ marginTop: 16 }} wrap>
              <Button onClick={() => void runImpact()}>影响分析</Button>
              {(detail.type === 'instance' || detail.type === 'database') && (
                <>
                  <Button onClick={() => void takeSnap(false)}>拍快照</Button>
                  <Button onClick={() => void takeSnap(true)}>设为基线</Button>
                </>
              )}
            </Space>
            {impact && (
              <div style={{ marginTop: 16 }}>
                <h4>影响摘要</h4>
                <Space wrap>
                  <Tag>下游 {impact.summary.downstream}</Tag>
                  <Tag>上游 {impact.summary.upstream}</Tag>
                  {Object.entries(impact.summary.by_type || {}).map(([k, v]) => (
                    <Tag key={k}>
                      {k}:{v}
                    </Tag>
                  ))}
                </Space>
              </div>
            )}
            {snaps.length > 0 && (
              <div style={{ marginTop: 16 }}>
                <h4>快照历史</h4>
                <Table
                  size="small"
                  rowKey="id"
                  pagination={{ pageSize: 5 }}
                  dataSource={snaps}
                  columns={[
                    { title: '时间', dataIndex: 'created_at', width: 180 },
                    {
                      title: '基线',
                      dataIndex: 'is_baseline',
                      width: 70,
                      render: (v: boolean) => (v ? <Tag color="green">是</Tag> : '-'),
                    },
                    { title: 'Hash', dataIndex: 'hash', ellipsis: true, render: (v: string) => v?.slice(0, 12) },
                  ]}
                />
              </div>
            )}
          </>
        )}
      </Drawer>
    </div>
  )
}
