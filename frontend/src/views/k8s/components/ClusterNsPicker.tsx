import React, { useEffect, useState } from 'react'
import { Select, Space } from 'antd'
import { listClusters, listResources, loadPicker, savePicker, type K8sCluster } from '../services/k8sApi'

type Props = {
  value?: { clusterId?: string; ns?: string }
  onChange?: (v: { clusterId?: string; ns?: string }) => void
  showNs?: boolean
}

export default function ClusterNsPicker({ value, onChange, showNs = true }: Props) {
  const [clusters, setClusters] = useState<K8sCluster[]>([])
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [clusterId, setClusterId] = useState(value?.clusterId || loadPicker().clusterId || '')
  const [ns, setNs] = useState(value?.ns || loadPicker().ns || '')

  useEffect(() => {
    listClusters().then(setClusters).catch(() => setClusters([]))
  }, [])

  useEffect(() => {
    if (!clusterId || !showNs) return
    listResources(clusterId, 'namespaces')
      .then((data) => {
        const items = data?.items || []
        setNamespaces(
          items.map((it: any) => it?.metadata?.name).filter(Boolean),
        )
      })
      .catch(() => setNamespaces([]))
  }, [clusterId, showNs])

  function emit(cid: string, namespace: string) {
    const v = { clusterId: cid, ns: namespace }
    savePicker(v)
    onChange?.(v)
  }

  return (
    <Space wrap>
      <Select
        style={{ minWidth: 220 }}
        placeholder="选择集群"
        value={clusterId || undefined}
        options={clusters.map((c) => ({
          value: c.id,
          label: `${c.name}${c.status ? ` (${c.status})` : ''}`,
        }))}
        onChange={(v) => {
          setClusterId(v)
          setNs('')
          emit(v, '')
        }}
        allowClear
      />
      {showNs && (
        <Select
          style={{ minWidth: 180 }}
          placeholder="命名空间（可选）"
          value={ns || undefined}
          options={namespaces.map((n) => ({ value: n, label: n }))}
          onChange={(v) => {
            setNs(v)
            emit(clusterId, v)
          }}
          allowClear
          showSearch
        />
      )}
    </Space>
  )
}
