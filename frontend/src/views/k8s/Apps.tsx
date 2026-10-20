import React, { useState } from 'react'
import { Alert, Button } from 'antd'
import ClusterNsPicker from './components/ClusterNsPicker'
import api from '../../api/request'
import { loadPicker } from './services/k8sApi'
import './k8s.css'

export default function K8sApps() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [msg, setMsg] = useState('')

  async function load() {
    if (!clusterId) return
    const r = await api.get(`/k8s/clusters/${clusterId}/apps`)
    setMsg(r.data.message || '暂无应用')
  }

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>Helm 应用</h3>
        <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
      </div>
      <Alert
        type="info"
        showIcon
        message="P4 增强能力"
        description="将接入 helm.sh/helm/v3 SDK，支持仓库管理、install / upgrade / rollback。"
        style={{ marginBottom: 12 }}
      />
      <Button onClick={load}>查询占位接口</Button>
      {msg && <p style={{ marginTop: 12 }}>{msg}</p>}
    </div>
  )
}
