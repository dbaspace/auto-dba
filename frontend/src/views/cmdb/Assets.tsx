import React, { useEffect, useState } from 'react'
import { Button, Descriptions, Drawer, Input, Select, Space, Tag, message } from 'antd'
import { Link } from 'react-router-dom'
import ResponsiveTable from '../../components/ResponsiveTable'
import { CI_TYPES, getCI, listCIs, type CmdbCI } from './services/cmdbApi'

export default function CmdbAssets() {
  const [type, setType] = useState('instance')
  const [q, setQ] = useState('')
  const [rows, setRows] = useState<CmdbCI[]>([])
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<(CmdbCI & { tags?: string[] }) | null>(null)

  async function load() {
    setLoading(true)
    try {
      const list = await listCIs({ type, q: q || undefined, limit: 200 })
      setRows(list)
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
    } catch (e: any) {
      message.error(e?.response?.data?.message || '详情加载失败')
    }
  }

  return (
    <div className="ops-page">
      <div className="ops-toolbar" style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
        <Select style={{ width: 160 }} value={type} options={CI_TYPES} onChange={setType} />
        <Input.Search placeholder="搜索名称/ID/标签" allowClear style={{ width: 260 }} value={q} onChange={(e) => setQ(e.target.value)} onSearch={() => void load()} />
        <Button type="primary" onClick={() => void load()} loading={loading}>
          查询
        </Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        loading={loading}
        dataSource={rows}
        columns={[
          { title: '名称', dataIndex: 'name', ellipsis: true },
          { title: '类型', dataIndex: 'type', width: 120 },
          { title: '状态', dataIndex: 'status', width: 100 },
          {
            title: '标签',
            dataIndex: 'tags',
            render: (tags: string[]) => (
              <Space size={[4, 4]} wrap>
                {(tags || []).slice(0, 6).map((t) => (
                  <Tag key={t}>{t}</Tag>
                ))}
              </Space>
            ),
          },
          { title: '更新时间', dataIndex: 'updated_at', width: 180 },
          {
            title: '操作',
            width: 160,
            render: (_: unknown, row: CmdbCI) => (
              <Space>
                <Button type="link" size="small" onClick={() => void openDetail(row)}>
                  详情
                </Button>
                {row.edit_path ? (
                  <Link to={row.edit_path}>编辑</Link>
                ) : null}
              </Space>
            ),
          },
        ]}
        pagination={{ pageSize: 20 }}
      />
      <Drawer open={!!detail} width={560} title={detail?.name || '详情'} onClose={() => setDetail(null)}>
        {detail && (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="ID">{detail.id}</Descriptions.Item>
            <Descriptions.Item label="类型">{detail.type}</Descriptions.Item>
            <Descriptions.Item label="状态">{detail.status || '-'}</Descriptions.Item>
            <Descriptions.Item label="标签">
              <Space wrap>
                {(detail.tags || detail.tags || []).map((t) => (
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
        )}
      </Drawer>
    </div>
  )
}
