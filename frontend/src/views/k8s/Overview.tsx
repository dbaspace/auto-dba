import React, { useEffect, useState } from 'react'
import { Card, List, Tag } from 'antd'
import ClusterNsPicker from './components/ClusterNsPicker'
import { getOverview, loadPicker, multiSummary } from './services/k8sApi'
import './k8s.css'

export default function K8sOverview() {
  const [clusterId, setClusterId] = useState(loadPicker().clusterId || '')
  const [ov, setOv] = useState<any>(null)
  const [summary, setSummary] = useState<any>(null)

  useEffect(() => {
    multiSummary().then(setSummary).catch(() => null)
  }, [])

  useEffect(() => {
    if (!clusterId) {
      setOv(null)
      return
    }
    getOverview(clusterId).then(setOv).catch(() => setOv(null))
  }, [clusterId])

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>集群概览</h3>
        <ClusterNsPicker showNs={false} onChange={(v) => setClusterId(v.clusterId || '')} />
      </div>
      {summary && (
        <div className="k8s-cards">
          <div className="k8s-card"><div className="label">集群总数</div><div className="value">{summary.summary?.total ?? 0}</div></div>
          <div className="k8s-card"><div className="label">健康</div><div className="value">{summary.summary?.healthy ?? 0}</div></div>
          <div className="k8s-card"><div className="label">降级</div><div className="value">{summary.summary?.degraded ?? 0}</div></div>
          <div className="k8s-card"><div className="label">不可达</div><div className="value">{summary.summary?.unreachable ?? 0}</div></div>
        </div>
      )}
      {ov && (
        <>
          <div className="k8s-cards">
            <div className="k8s-card"><div className="label">版本</div><div className="value" style={{ fontSize: 16 }}>{ov.version || '-'}</div></div>
            <div className="k8s-card"><div className="label">节点 Ready</div><div className="value">{ov.node_ready}/{ov.node_total}</div></div>
            <div className="k8s-card"><div className="label">CPU Requests</div><div className="value" style={{ fontSize: 16 }}>{ov.cpu_requests || '-'}</div></div>
            <div className="k8s-card"><div className="label">Mem Requests</div><div className="value" style={{ fontSize: 16 }}>{ov.mem_requests || '-'}</div></div>
          </div>
          <Card size="small" title="Pod 阶段" style={{ marginBottom: 12 }}>
            {Object.entries(ov.pod_phases || {}).map(([k, v]) => (
              <Tag key={k}>{k}: {String(v)}</Tag>
            ))}
          </Card>
          <Card size="small" title="异常 Pod Top" style={{ marginBottom: 12 }}>
            <List
              size="small"
              dataSource={ov.abnormal_pods || []}
              renderItem={(item: any) => (
                <List.Item>
                  <Tag color="red">{item.phase}</Tag> {item.namespace}/{item.name} {item.reason || ''}
                </List.Item>
              )}
            />
          </Card>
          <Card size="small" title="最近 Warning 事件">
            <List
              size="small"
              dataSource={ov.recent_events || []}
              renderItem={(item: any) => (
                <List.Item>
                  <div>
                    <Tag>{item.reason}</Tag>
                    {item.namespace}/{item.name}: {item.message}
                  </div>
                </List.Item>
              )}
            />
          </Card>
        </>
      )}
    </div>
  )
}
