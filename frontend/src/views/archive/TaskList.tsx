import React, { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Modal, Pagination, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import api from '../../api/request'
import CreateTaskDialog from './CreateTaskDialog'
import {
  approvalText,
  canRun,
  decodeMeta,
  isApproved,
  scheduleLabel,
  sourceTarget,
  statusText,
  statusType,
  strategyLabel,
} from './archive.ts'
import './archive.css'

const tagColor: Record<string, string> = {
  success: 'success',
  danger: 'error',
  warning: 'warning',
  info: 'default',
  primary: 'blue',
}

export default function TaskList() {
  const navigate = useNavigate()
  const [rows, setRows] = useState<any[]>([])
  const [instances, setInstances] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [visible, setVisible] = useState(false)
  const [current, setCurrent] = useState<any>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const pageRows = useMemo(
    () => rows.slice((page - 1) * pageSize, page * pageSize),
    [rows, page, pageSize],
  )

  function openCreate() {
    setCurrent(null)
    setVisible(true)
  }
  function openView(row: any) {
    setCurrent(row)
    setVisible(true)
  }

  async function load() {
    setLoading(true)
    try {
      const [taskRes, instRes] = await Promise.all([
        api.get('/archive/tasks'),
        api.get('/instances'),
      ])
      setRows(taskRes.data.data || [])
      setInstances(instRes.data.data || [])
    } finally {
      setLoading(false)
    }
  }

  async function run(row: any) {
    if (!canRun(row)) {
      message.warning('归档任务须审批通过后才能执行')
      return
    }
    await api.post(`/archive/tasks/${row.id}/run`)
    message.success('已触发执行，可在控制台查看本次记录')
    await load()
  }

  function pause(row: any) {
    Modal.confirm({
      title: '停止任务',
      content: `仅停止后续触发，不影响已在执行的任务。确认停止「${row.name}」？`,
      okText: '停止后续触发',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.post(`/archive/tasks/${row.id}/pause`)
        message.success('已停止后续触发')
        await load()
      },
    })
  }

  useEffect(() => {
    load()
  }, [])

  const columns: ColumnsType<any> = [
    {
      title: '任务名称',
      minWidth: 180,
      ellipsis: true,
      render: (_, row) => (
        <button type="button" className="name-link" onClick={() => openView(row)}>
          {row.name}
        </button>
      ),
    },
    {
      title: '源 -> 目标',
      minWidth: 220,
      ellipsis: true,
      render: (_, row) => sourceTarget(row, instances),
    },
    {
      title: '策略',
      width: 128,
      render: (_, row) => (
        <Tag color="blue">{strategyLabel(decodeMeta(row.condition).strategy)}</Tag>
      ),
    },
    { title: '调度', width: 120, render: (_, row) => scheduleLabel(row) },
    {
      title: '状态',
      width: 110,
      render: (_, row) => (
        <Tag color={tagColor[statusType(row.status)] || 'default'}>{statusText(row.status)}</Tag>
      ),
    },
    {
      title: '审批',
      width: 100,
      render: (_, row) =>
        row.issue_id ? (
          <Button type="link" onClick={() => navigate('/ops/tickets/' + row.issue_id)}>
            {approvalText(row.approval_status)}
          </Button>
        ) : (
          <span className="muted">{approvalText(row.approval_status)}</span>
        ),
    },
    {
      title: '操作',
      width: 200,
      fixed: 'right',
      render: (_, row) => (
        <div className="ops">
          <Button type="link" onClick={() => openView(row)}>
            查看
          </Button>
          <Button type="link" disabled={row.status === 'running'} onClick={() => openView(row)}>
            编辑
          </Button>
          <Button type="link" disabled={!canRun(row)} onClick={() => run(row)}>
            执行
          </Button>
          <Button
            type="link"
            danger
            disabled={row.status === 'paused' || !isApproved(row)}
            onClick={() => pause(row)}
          >
            停止
          </Button>
        </div>
      ),
    },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <div className="page-head row">
          <h1>任务列表</h1>
          <div className="toolbar">
            <Button type="primary" onClick={openCreate}>
              新建任务
            </Button>
            <Button onClick={load}>刷新</Button>
          </div>
        </div>

        <Card bordered={false} className="panel">
          <Table
            rowKey={(r) => r.id ?? r.name}
            dataSource={pageRows}
            columns={columns}
            loading={loading}
            pagination={false}
            scroll={{ x: true }}
            locale={{ emptyText: '暂无归档任务' }}
          />
          <div className="pager">
            <Pagination
              current={page}
              pageSize={pageSize}
              total={rows.length}
              showTotal={(t) => `共 ${t} 条`}
              pageSizeOptions={[10, 20, 50, 100]}
              showSizeChanger
              onChange={(p, ps) => {
                setPage(p)
                setPageSize(ps)
              }}
            />
          </div>
        </Card>

        <CreateTaskDialog
          open={visible}
          onClose={() => setVisible(false)}
          instances={instances}
          task={current}
          onSaved={load}
        />
      </div>
    </div>
  )
}
