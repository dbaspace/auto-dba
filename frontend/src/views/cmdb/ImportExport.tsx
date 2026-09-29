import React, { useState } from 'react'
import { Button, Radio, Space, Switch, Upload, message } from 'antd'
import api from '../../api/request'

export default function CmdbImportExport() {
  const [format, setFormat] = useState<'json' | 'csv'>('json')
  const [dryRun, setDryRun] = useState(true)
  const [conflict, setConflict] = useState<'skip' | 'update'>('skip')
  const [result, setResult] = useState<any>(null)

  async function doExport() {
    if (format === 'csv') {
      const res = await api.get('/cmdb/export', { params: { types: 'instance', format: 'csv' }, responseType: 'blob' })
      const url = URL.createObjectURL(res.data)
      const a = document.createElement('a')
      a.href = url
      a.download = 'cmdb-instances.csv'
      a.click()
      URL.revokeObjectURL(url)
      return
    }
    const { data } = await api.get('/cmdb/export', { params: { types: 'instance', format: 'json' } })
    const blob = new Blob([JSON.stringify(data.data || [], null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'cmdb-instances.json'
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="ops-page">
      <h3>导出实例</h3>
      <Space style={{ marginBottom: 24 }}>
        <Radio.Group value={format} onChange={(e) => setFormat(e.target.value)}>
          <Radio.Button value="json">JSON</Radio.Button>
          <Radio.Button value="csv">CSV</Radio.Button>
        </Radio.Group>
        <Button type="primary" onClick={() => void doExport()}>
          导出
        </Button>
      </Space>

      <h3>导入实例</h3>
      <Space style={{ marginBottom: 12 }}>
        <span>Dry-run</span>
        <Switch checked={dryRun} onChange={setDryRun} />
        <span>冲突策略</span>
        <Radio.Group value={conflict} onChange={(e) => setConflict(e.target.value)}>
          <Radio.Button value="skip">跳过</Radio.Button>
          <Radio.Button value="update">更新</Radio.Button>
        </Radio.Group>
      </Space>
      <Upload
        accept=".json,.csv,application/json,text/csv"
        showUploadList={false}
        beforeUpload={async (file) => {
          const text = await file.text()
          const isCsv = file.name.endsWith('.csv') || text.startsWith('id,') || text.startsWith('name,')
          const { data } = await api.post(`/cmdb/import?dry_run=${dryRun ? 1 : 0}&conflict=${conflict}`, text, {
            headers: { 'Content-Type': isCsv ? 'text/csv' : 'application/json' },
          })
          setResult(data.data)
          message.success(dryRun ? 'Dry-run 完成' : '导入完成')
          return false
        }}
      >
        <Button>选择文件导入</Button>
      </Upload>
      {result && (
        <pre style={{ marginTop: 16, background: 'var(--ant-color-fill-quaternary, #f5f5f5)', padding: 12, maxHeight: 360, overflow: 'auto' }}>
          {JSON.stringify(result, null, 2)}
        </pre>
      )}
    </div>
  )
}
