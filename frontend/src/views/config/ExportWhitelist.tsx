import React, { useEffect, useRef, useState } from 'react'
import { Button, Checkbox, DatePicker, Form, Input, Modal, Select, Space, message } from 'antd'
import type { InputRef } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import api from '../../api/request'
import dayjs from 'dayjs'

interface Row {
  id: string
  username: string
  db_ids: string[]
  expire_at?: string
  updated_at?: string
}

interface DbOpt {
  id: string
  db_name?: string
  instance_name?: string
}

interface UserOpt {
  username: string
  display_name?: string
}

function userLabel(u: UserOpt) {
  const name = String(u.display_name || '').trim()
  if (name && name !== u.username) return `${u.username}（${name}）`
  return u.username
}

function dbLabel(d: DbOpt) {
  return `${d.db_name || d.id}${d.instance_name ? ` · ${d.instance_name}` : ''}`
}

export default function ExportWhitelist() {
  const [rows, setRows] = useState<Row[]>([])
  const [dbs, setDbs] = useState<DbOpt[]>([])
  const [users, setUsers] = useState<UserOpt[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Row | null>(null)
  const [dbKeyword, setDbKeyword] = useState('')
  const [form] = Form.useForm()
  const selectedDbIds: string[] = Form.useWatch('db_ids', form) || []
  const popupHost = useRef<HTMLDivElement>(null)
  const dbFilterRef = useRef<InputRef>(null)

  const load = async () => {
    const { data } = await api.get('/export-whitelist')
    setRows(data?.data || [])
  }

  useEffect(() => {
    void load().catch((e) => message.error(e?.response?.data?.message || '加载失败'))
    api.get('/databases').then(({ data }) => setDbs(data?.data || [])).catch(() => undefined)
    api.get('/users').then(({ data }) => {
      const list = (data?.data || []) as UserOpt[]
      setUsers(list.filter((u) => String(u.username || '').trim()))
    }).catch((e) => message.error(e?.response?.data?.message || '加载用户列表失败'))
  }, [])

  function openEdit(row?: Row) {
    setEditing(row || null)
    setDbKeyword('')
    form.setFieldsValue({
      username: row?.username || undefined,
      db_ids: row?.db_ids || [],
      expire_at: row?.expire_at ? dayjs(row.expire_at) : null,
    })
    setOpen(true)
  }

  async function save() {
    const values = await form.validateFields()
    const body = {
      username: String(values.username || '').trim(),
      db_ids: values.db_ids || [],
      expire_at: values.expire_at ? values.expire_at.toDate().toISOString() : '',
    }
    if (editing) {
      await api.put(`/export-whitelist/${editing.id}`, body)
    } else {
      await api.post('/export-whitelist', body)
    }
    message.success('已保存')
    setOpen(false)
    await load()
  }

  async function remove(row: Row) {
    Modal.confirm({
      title: `删除 ${row.username} 的导出白名单？`,
      onOk: async () => {
        await api.delete(`/export-whitelist/${row.id}`)
        message.success('已删除')
        await load()
      },
    })
  }

  const popupContainer = () => popupHost.current || document.body
  const keyword = dbKeyword.trim().toLowerCase()
  const matchedDbs = dbs.filter((d) => !keyword || dbLabel(d).toLowerCase().includes(keyword))
  const matchedIds = matchedDbs.map((d) => d.id)
  const matchedSelected = matchedIds.filter((id) => selectedDbIds.includes(id))
  const matchedAllChecked = matchedIds.length > 0 && matchedSelected.length === matchedIds.length
  const matchedIndeterminate = matchedSelected.length > 0 && !matchedAllChecked

  function toggleMatchedDbs(checked: boolean) {
    const next = new Set(selectedDbIds)
    matchedIds.forEach((id) => (checked ? next.add(id) : next.delete(id)))
    form.setFieldsValue({ db_ids: [...next] })
  }

  return (
    <div style={{ padding: 16 }}>
      <div style={{ marginBottom: 12, display: 'flex', justifyContent: 'space-between' }}>
        <div>
          <div style={{ fontSize: 16, fontWeight: 600 }}>导出白名单</div>
          <div style={{ color: '#6b7280', marginTop: 4 }}>名单内用户可直接导出当前查询结果。过期时间为空表示一直有效。</div>
        </div>
        <Button type="primary" onClick={() => openEdit()}>新增</Button>
      </div>
      <ResponsiveTable
        rowKey="id"
        dataSource={rows}
        pagination={false}
        columns={[
          { title: '用户名', dataIndex: 'username', width: 160 },
          {
            title: '数据库',
            dataIndex: 'db_ids',
            render: (ids: string[]) => (ids || []).map((id) => {
              const db = dbs.find((d) => d.id === id)
              return db ? dbLabel(db) : id
            }).join('、'),
          },
          {
            title: '过期时间',
            dataIndex: 'expire_at',
            width: 200,
            render: (v: string) => v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '一直有效',
          },
          {
            title: '操作',
            width: 140,
            render: (_: unknown, row: Row) => (
              <Space>
                <Button size="small" onClick={() => openEdit(row)}>编辑</Button>
                <Button size="small" danger onClick={() => void remove(row)}>删除</Button>
              </Space>
            ),
          },
        ]}
      />
      <Modal
        open={open}
        title={editing ? '编辑导出白名单' : '新增导出白名单'}
        onCancel={() => setOpen(false)}
        onOk={() => void save().catch((e) => message.error(e?.response?.data?.message || e?.message || '保存失败'))}
        destroyOnClose
      >
        <div ref={popupHost} style={{ position: 'relative' }}>
          <Form form={form} layout="vertical">
            <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请选择用户' }]}>
              <Select
                showSearch
                allowClear
                placeholder="选择登录名"
                optionFilterProp="label"
                placement="bottomLeft"
                getPopupContainer={popupContainer}
                options={users.map((u) => ({ value: u.username, label: userLabel(u) }))}
              />
            </Form.Item>
            <Form.Item name="db_ids" label="数据库" rules={[{ required: true, message: '请选择数据库' }]}>
              <Select
                mode="multiple"
                showSearch={false}
                allowClear
                placeholder="勾选数据库，可在下拉里继续搜索"
                placement="bottomLeft"
                getPopupContainer={popupContainer}
                options={matchedDbs.map((d) => ({ value: d.id, label: dbLabel(d) }))}
                menuItemSelectedIcon={null}
                notFoundContent={keyword ? '无匹配数据库' : '暂无数据库'}
                onDropdownVisibleChange={(visible) => {
                  if (!visible) setDbKeyword('')
                  else queueMicrotask(() => dbFilterRef.current?.focus({ preventScroll: true }))
                }}
                dropdownRender={(menu) => (
                  <div>
                    <div
                      style={{ padding: '8px 12px 4px' }}
                      onMouseDown={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        queueMicrotask(() => dbFilterRef.current?.focus({ preventScroll: true }))
                      }}
                    >
                      <Input
                        ref={dbFilterRef}
                        allowClear
                        value={dbKeyword}
                        placeholder="输入库名或实例过滤，可换关键字再搜"
                        onChange={(e) => setDbKeyword(e.target.value)}
                        onMouseDown={(e) => e.stopPropagation()}
                        onKeyDown={(e) => e.stopPropagation()}
                      />
                    </div>
                    <div
                      style={{ padding: '4px 12px 8px', borderBottom: '1px solid #f0f0f0' }}
                      onMouseDown={(e) => e.preventDefault()}
                    >
                      <Checkbox
                        checked={matchedAllChecked}
                        indeterminate={matchedIndeterminate}
                        disabled={matchedIds.length === 0}
                        onChange={(e) => toggleMatchedDbs(e.target.checked)}
                      >
                        全选当前结果 ({matchedIds.length})
                      </Checkbox>
                    </div>
                    {menu}
                  </div>
                )}
                optionRender={(opt) => (
                  <span>
                    <Checkbox
                      checked={selectedDbIds.includes(String(opt.value))}
                      style={{ pointerEvents: 'none', marginRight: 8 }}
                    />
                    {opt.label}
                  </span>
                )}
              />
            </Form.Item>
            <Form.Item name="expire_at" label="过期时间">
              <DatePicker showTime style={{ width: '100%' }} getPopupContainer={popupContainer} />
            </Form.Item>
          </Form>
        </div>
      </Modal>
    </div>
  )
}
