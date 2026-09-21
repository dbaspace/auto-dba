import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, InputNumber, Select, Switch, Table, message } from 'antd'
import { CheckCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import api from '../../api/request'

interface CmdRow {
  name: string
  group: string
  crud: string
  syntax: string
  min_args: number
  enabled: boolean
  label?: string
}

export default function RedisCommandRulesPanel() {
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [blacklist, setBlacklist] = useState<string[]>([])
  const [commands, setCommands] = useState<CmdRow[]>([])
  const [savedSnap, setSavedSnap] = useState('')
  const [scrollY, setScrollY] = useState(320)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const tableWrapRef = useRef<HTMLDivElement>(null)

  async function load() {
    setLoading(true)
    try {
      const { data } = await api.get('/redis/command-rules')
      const bl: string[] = data.data?.blacklist || []
      const cmds: CmdRow[] = (data.data?.commands || []).map((c: any) => ({
        name: c.name,
        group: c.group,
        crud: c.crud,
        syntax: c.syntax,
        min_args: Number(c.min_args) || 0,
        enabled: c.enabled !== false,
        label: c.label,
      }))
      setBlacklist(bl)
      setCommands(cmds)
      setSavedSnap(JSON.stringify({ blacklist: bl, commands: cmds }))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  // 按容器剩余高度计算表体 scroll.y，保证右侧出现可滚动条
  useEffect(() => {
    const el = tableWrapRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const update = () => {
      // 预留表头约 39px + 底部分页约 56px
      const next = Math.max(160, el.clientHeight - 39 - 56)
      setScrollY(next)
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const dirty = useMemo(() => {
    return JSON.stringify({ blacklist, commands }) !== savedSnap
  }, [blacklist, commands, savedSnap])

  const filtered = useMemo(() => {
    const q = keyword.trim().toLowerCase()
    if (!q) return commands
    return commands.filter(
      (c) =>
        c.name.toLowerCase().includes(q) ||
        c.group.toLowerCase().includes(q) ||
        c.crud.toLowerCase().includes(q) ||
        String(c.label || '')
          .toLowerCase()
          .includes(q),
    )
  }, [commands, keyword])

  // 搜索条件变化时回到第一页
  useEffect(() => {
    setPage(1)
  }, [keyword])

  function patchCmd(name: string, patch: Partial<CmdRow>) {
    setCommands((prev) => prev.map((c) => (c.name === name ? { ...c, ...patch } : c)))
  }

  async function save() {
    setSaving(true)
    try {
      const payload = {
        blacklist,
        commands: commands.map((c) => ({
          name: c.name,
          enabled: c.enabled,
          min_args: c.min_args,
          label: c.label,
        })),
      }
      const { data } = await api.put('/redis/command-rules', payload)
      const bl: string[] = data.data?.blacklist || blacklist
      const cmds: CmdRow[] = (data.data?.commands || commands).map((c: any) => ({
        name: c.name,
        group: c.group || '',
        crud: c.crud || '',
        syntax: c.syntax || '',
        min_args: Number(c.min_args) || 0,
        enabled: c.enabled !== false,
        label: c.label,
      }))
      setBlacklist(bl)
      setCommands(cmds)
      setSavedSnap(JSON.stringify({ blacklist: bl, commands: cmds }))
      message.success('Redis 命令规则已保存')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="redis-rules-panel">
      <div className="panel-actions" style={{ marginBottom: 12 }}>
        <Input
          size="small"
          value={keyword}
          allowClear
          placeholder="搜索命令 / 分组"
          style={{ width: 220 }}
          onChange={(e) => setKeyword(e.target.value)}
        />
        <Button size="small" icon={<ReloadOutlined />} loading={loading} disabled={saving} onClick={load}>
          刷新
        </Button>
        <Button
          size="small"
          type="primary"
          icon={<CheckCircleOutlined />}
          loading={saving}
          disabled={!dirty}
          onClick={save}
        >
          保存修改{dirty ? '（有变更）' : ''}
        </Button>
      </div>

      <section className="gi-card redis-blacklist-card">
        <div className="gi-head">
          <h2>危险命令黑名单</h2>
          <p>命中黑名单的命令禁止提交与执行。可增删动词（大写）。</p>
        </div>
        <Select
          mode="tags"
          style={{ width: '100%', marginBottom: 12 }}
          placeholder="输入命令动词后回车，如 FLUSHALL"
          value={blacklist}
          tokenSeparators={[',', ' ']}
          onChange={(v) =>
            setBlacklist(
              [...new Set((v as string[]).map((x) => String(x || '').trim().toUpperCase()).filter(Boolean))].sort(),
            )
          }
          disabled={loading || saving}
        />
      </section>

      <section className="gi-card redis-cmd-table-card">
        <div className="gi-head">
          <h2>允许的 CRUD 命令</h2>
          <p>关闭开关后该命令不可用于 Redis 工单；可调整最少参数个数。</p>
        </div>
        <div ref={tableWrapRef} className="redis-cmd-table-wrap">
          <Table
            size="small"
            bordered
            loading={loading}
            rowKey="name"
            dataSource={filtered}
            scroll={{ y: scrollY }}
            pagination={{
              current: page,
              pageSize,
              showSizeChanger: true,
              pageSizeOptions: ['10', '20', '50'],
              showTotal: (total) => `共 ${total} 条`,
              onChange: (p, ps) => {
                setPage(p)
                setPageSize(ps)
              },
            }}
            columns={[
              { title: '分组', dataIndex: 'group', width: 90 },
              { title: '命令', dataIndex: 'name', width: 100 },
              { title: '类型', dataIndex: 'crud', width: 80 },
              { title: '语法', dataIndex: 'syntax', ellipsis: true },
              {
                title: '启用',
                dataIndex: 'enabled',
                width: 80,
                render: (v: boolean, row) => (
                  <Switch
                    size="small"
                    checked={v}
                    disabled={saving}
                    onChange={(checked) => patchCmd(row.name, { enabled: checked })}
                  />
                ),
              },
              {
                title: '最少参数',
                dataIndex: 'min_args',
                width: 110,
                render: (v: number, row) => (
                  <InputNumber
                    size="small"
                    min={0}
                    max={32}
                    value={v}
                    disabled={saving}
                    onChange={(n) => patchCmd(row.name, { min_args: Number(n) || 0 })}
                  />
                ),
              },
            ]}
          />
        </div>
      </section>
    </div>
  )
}
