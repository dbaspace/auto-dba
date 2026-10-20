import React, { useEffect, useState } from 'react'
import { Button, Drawer, Space, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import { getResource, listResources, loadPicker } from './services/k8sApi'
import './k8s.css'

type Props = {
  title: string
  gvr: string
  namespaced?: boolean
  extraCols?: ColumnsType<any>
}

export default function K8sResourceList({ title, gvr, namespaced = true, extraCols = [] }: Props) {
  const picker = loadPicker()
  const [clusterId, setClusterId] = useState(picker.clusterId || '')
  const [ns, setNs] = useState(picker.ns || '')
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [yaml, setYaml] = useState('')
  const [open, setOpen] = useState(false)

  async function load() {
    if (!clusterId) return
    setLoading(true)
    try {
      const params = namespaced && ns ? { ns } : {}
      const data = await listResources(clusterId, gvr, params)
      setRows(data?.items || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
      setRows([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [clusterId, ns, gvr])

  const columns: ColumnsType<any> = [
    { title: '名称', render: (_, r) => r?.metadata?.name },
    ...(namespaced ? [{ title: '命名空间', render: (_: any, r: any) => r?.metadata?.namespace || '-' }] as ColumnsType<any> : []),
    ...extraCols,
    {
      title: '操作',
      width: 100,
      render: (_, r) => (
        <Button
          size="small"
          onClick={async () => {
            const data = await getResource(
              clusterId,
              gvr,
              r?.metadata?.namespace || '_',
              r?.metadata?.name,
              true,
            )
            setYaml(data?.yaml || '')
            setOpen(true)
          }}
        >
          YAML
        </Button>
      ),
    },
  ]

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>{title}</h3>
        <Space>
          <ClusterNsPicker
            showNs={namespaced}
            onChange={(v) => {
              setClusterId(v.clusterId || '')
              setNs(v.ns || '')
            }}
          />
          <Button onClick={load}>刷新</Button>
        </Space>
      </div>
      <ResponsiveTable
        rowKey={(r) => `${r?.metadata?.namespace}/${r?.metadata?.uid || r?.metadata?.name}`}
        loading={loading}
        columns={columns}
        dataSource={rows}
      />
      <Drawer open={open} onClose={() => setOpen(false)} width={720} title="YAML">
        <pre className="k8s-yaml">{yaml}</pre>
      </Drawer>
    </div>
  )
}

export function K8sNodes() {
  return (
    <K8sResourceList
      title="节点"
      gvr="nodes"
      namespaced={false}
      extraCols={[
        { title: '版本', render: (_, r) => r?.status?.nodeInfo?.kubeletVersion },
        {
          title: 'Ready',
          render: (_, r) =>
            (r?.status?.conditions || []).find((c: any) => c.type === 'Ready')?.status || '-',
        },
      ]}
    />
  )
}

export function K8sNetwork() {
  return (
    <K8sResourceList
      title="网络 · Service"
      gvr="services"
      extraCols={[
        { title: '类型', render: (_, r) => r?.spec?.type },
        { title: 'ClusterIP', render: (_, r) => r?.spec?.clusterIP },
      ]}
    />
  )
}

export function K8sStorage() {
  return (
    <K8sResourceList
      title="存储 · PVC"
      gvr="pvc"
      extraCols={[
        { title: '状态', render: (_, r) => r?.status?.phase },
        { title: '容量', render: (_, r) => r?.status?.capacity?.storage },
      ]}
    />
  )
}
