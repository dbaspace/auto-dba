import React, { useState } from 'react'
import { Alert, Button, Checkbox, Select, Steps, Tag, message } from 'antd'
import ResponsiveTable from '../../components/ResponsiveTable'
import type { ColumnsType } from 'antd/es/table'
import './archive.css'

type Rule = {
  field: string
  type: string
  sensitive: boolean
  algo: string
}

export default function Masking() {
  const [step, setStep] = useState(1)
  const [parallel, setParallel] = useState(true)
  const [compress, setCompress] = useState(false)
  const [rows, setRows] = useState<Rule[]>([
    { field: 'phone', type: 'varchar', sensitive: true, algo: 'mask' },
    { field: 'email', type: 'varchar', sensitive: true, algo: 'hash' },
    { field: 'id_card', type: 'varchar', sensitive: true, algo: 'mask' },
    { field: 'created_at', type: 'timestamp', sensitive: false, algo: 'none' },
  ])

  function setAlgo(field: string, algo: string) {
    setRows((prev) => prev.map((r) => (r.field === field ? { ...r, algo } : r)))
  }

  function submit() {
    message.success('脱敏规则已保存到本地，可在任务管理中关联执行')
  }

  const columns: ColumnsType<Rule> = [
    {
      title: '源字段',
      width: 180,
      render: (_, row) => (
        <span>
          <span>{row.field}</span>
          {row.sensitive && (
            <Tag color="error" style={{ marginLeft: 8 }}>
              敏感
            </Tag>
          )}
        </span>
      ),
    },
    { title: '类型', dataIndex: 'type', width: 120 },
    {
      title: '脱敏算法',
      render: (_, row) => (
        <Select
          style={{ width: 180 }}
          value={row.algo}
          onChange={(v) => setAlgo(row.field, v)}
          options={[
            { label: '哈希', value: 'hash' },
            { label: '掩码', value: 'mask' },
            { label: '替换', value: 'replace' },
            { label: '不处理', value: 'none' },
          ]}
        />
      ),
    },
  ]

  return (
    <div className="archive-shell">
      <div className="gray-page">
        <Steps
          current={step}
          className="mb"
          items={[
            { title: '选择数据' },
            { title: '脱敏规则' },
            { title: '调度策略' },
            { title: '提交' },
          ]}
        />
        <Alert
          type="info"
          className="mb"
          message="按字段配置脱敏算法后随归档任务执行。当前为独立工作台配置向导。"
        />
        <ResponsiveTable
          rowKey={(r) => r.field}
          className="mb"
          dataSource={rows}
          columns={columns}
          pagination={false}
        />
        <Checkbox checked={parallel} onChange={(e) => setParallel(e.target.checked)}>
          并行执行
        </Checkbox>
        <Checkbox
          checked={compress}
          onChange={(e) => setCompress(e.target.checked)}
          style={{ marginLeft: 12 }}
        >
          压缩归档
        </Checkbox>
        <div className="toolbar" style={{ marginTop: 16 }}>
          <Button disabled={step === 0} onClick={() => setStep((s) => s - 1)}>
            上一步
          </Button>
          {step < 3 ? (
            <Button type="primary" onClick={() => setStep((s) => s + 1)}>
              下一步
            </Button>
          ) : (
            <Button type="primary" onClick={submit}>
              提交
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
