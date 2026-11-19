import React, { useEffect, useState } from 'react'
import { Alert, Button, Input, Switch, message } from 'antd'
import api from '../../api/request'
import './archive.css'

export default function Masking() {
  const [enabled, setEnabled] = useState(false)
  const [columns, setColumns] = useState<string[]>([''])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/config/query-mask')
      const cfg = data.data || {}
      const cols = Array.isArray(cfg.columns) ? cfg.columns.map((c: unknown) => String(c)) : []
      setEnabled(!!cfg.enabled)
      setColumns(cols.length ? cols : [''])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function setColumn(i: number, value: string) {
    setColumns((prev) => prev.map((c, idx) => (idx === i ? value : c)))
  }

  async function save() {
    setSaving(true)
    try {
      const { data } = await api.put('/config/query-mask', { enabled, columns })
      const cfg = data.data || {}
      const cols = Array.isArray(cfg.columns) ? cfg.columns : []
      setEnabled(!!cfg.enabled)
      setColumns(cols.length ? cols : [''])
      message.success(cfg.enabled ? '已启用，下次查询按这些列名脱敏' : '已关闭，列名已保留')
    } catch (e: any) {
      message.error(e?.response?.data?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <h1>敏感数据脱敏</h1>
        <Alert
          type="info"
          showIcon
          className="mb"
          message="只作用于数据库查询结果。填写表字段名，大小写不必一致。mobile AS mob、t.mobile 都会按 mobile 脱敏；配置 card 时 carD、Card 也会脱敏。别名本身不会单独触发脱敏。命中的值显示为 ***。"
        />
        <div className="mb" style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <Switch checked={enabled} loading={loading} onChange={setEnabled} />
          <span>启用脱敏</span>
        </div>
        <div style={{ maxWidth: 480 }}>
          {columns.map((col, i) => (
            <div key={i} style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
              <Input
                value={col}
                placeholder="列名，可用逗号分隔多个"
                onChange={(e) => setColumn(i, e.target.value)}
              />
              <Button
                disabled={columns.length === 1 && !col}
                onClick={() => setColumns((prev) => (prev.length === 1 ? [''] : prev.filter((_, idx) => idx !== i)))}
              >
                删除
              </Button>
            </div>
          ))}
          <Button onClick={() => setColumns((prev) => [...prev, ''])}>添加列名</Button>
        </div>
        <div className="toolbar" style={{ marginTop: 16 }}>
          <Button type="primary" loading={saving} onClick={save}>
            保存
          </Button>
        </div>
      </div>
    </div>
  )
}
