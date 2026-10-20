import React, { useEffect, useMemo, useState } from 'react'
import { Button, Drawer, InputNumber, Modal, Space, Tabs, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ResponsiveTable from '../../components/ResponsiveTable'
import ClusterNsPicker from './components/ClusterNsPicker'
import {
  getPodLog,
  getResource,
  listResources,
  loadPicker,
  restartWorkload,
  scaleWorkload,
} from './services/k8sApi'
import './k8s.css'

const KINDS = [
  { key: 'deployments', label: 'Deployment' },
  { key: 'statefulsets', label: 'StatefulSet' },
  { key: 'daemonsets', label: 'DaemonSet' },
  { key: 'jobs', label: 'Job' },
  { key: 'cronjobs', label: 'CronJob' },
  { key: 'pods', label: 'Pod' },
]

export default function K8sWorkloads() {
  const picker = loadPicker()
  const [clusterId, setClusterId] = useState(picker.clusterId || '')
  const [ns, setNs] = useState(picker.ns || '')
  const [kind, setKind] = useState('deployments')
  const [rows, setRows] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<{ title: string; yaml?: string; log?: string } | null>(null)

  async function load() {
    if (!clusterId) return
    setLoading(true)
    try {
      const data = await listResources(clusterId, kind, ns ? { ns } : {})
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
  }, [clusterId, ns, kind])

  const columns: ColumnsType<any> = useMemo(() => {
    const base: ColumnsType<any> = [
      { title: '名称', render: (_, r) => r?.metadata?.name },
      { title: '命名空间', render: (_, r) => r?.metadata?.namespace || '-' },
    ]
    if (kind === 'pods') {
      base.push(
        { title: '阶段', render: (_, r) => r?.status?.phase },
        { title: '节点', render: (_, r) => r?.spec?.nodeName },
      )
    } else if (kind === 'deployments' || kind === 'statefulsets') {
      base.push(
        { title: '就绪', render: (_, r) => `${r?.status?.readyReplicas ?? 0}/${r?.spec?.replicas ?? 0}` },
      )
    }
    base.push({
      title: '操作',
      width: 280,
      render: (_, r) => {
        const name = r?.metadata?.name
        const namespace = r?.metadata?.namespace
        return (
          <Space size="small" wrap>
            <Button size="small" onClick={() => showYaml(namespace, name)}>YAML</Button>
            {kind === 'pods' && (
              <Button size="small" onClick={() => showLog(namespace, name)}>日志</Button>
            )}
            {(kind === 'deployments' || kind === 'statefulsets') && (
              <>
                <Button size="small" onClick={() => onScale(namespace, name, r?.spec?.replicas)}>扩缩</Button>
                <Button size="small" onClick={() => onRestart(namespace, name)}>重启</Button>
              </>
            )}
          </Space>
        )
      },
    })
    return base
  }, [kind, clusterId])

  async function showYaml(namespace: string, name: string) {
    try {
      const data = await getResource(clusterId, kind, namespace || '_', name, true)
      setDetail({ title: `${namespace}/${name}`, yaml: data?.yaml || JSON.stringify(data, null, 2) })
    } catch (e: any) {
      message.error(e?.response?.data?.message || '读取失败')
    }
  }

  async function showLog(namespace: string, name: string) {
    try {
      const data = await getPodLog(clusterId, namespace, name, { tailLines: '200' })
      setDetail({ title: `日志 ${namespace}/${name}`, log: data?.log || '' })
    } catch (e: any) {
      message.error(e?.response?.data?.message || '日志失败')
    }
  }

  function onScale(namespace: string, name: string, current?: number) {
    let replicas = current ?? 1
    Modal.confirm({
      title: `扩缩 ${name}`,
      content: (
        <InputNumber min={0} defaultValue={replicas} onChange={(v) => { replicas = Number(v || 0) }} />
      ),
      onOk: async () => {
        await scaleWorkload(clusterId, kind, namespace, name, replicas)
        message.success('已提交')
        load()
      },
    })
  }

  async function onRestart(namespace: string, name: string) {
    await restartWorkload(clusterId, kind, namespace, name)
    message.success('已触发重启')
    load()
  }

  return (
    <div className="k8s-page">
      <div className="k8s-toolbar">
        <h3>工作负载</h3>
        <ClusterNsPicker
          onChange={(v) => {
            setClusterId(v.clusterId || '')
            setNs(v.ns || '')
          }}
        />
      </div>
      <Tabs
        activeKey={kind}
        onChange={setKind}
        items={KINDS.map((k) => ({ key: k.key, label: k.label }))}
      />
      <ResponsiveTable
        rowKey={(r) => `${r?.metadata?.namespace}/${r?.metadata?.name}`}
        loading={loading}
        columns={columns}
        dataSource={rows}
      />
      <Drawer open={!!detail} onClose={() => setDetail(null)} width={720} title={detail?.title}>
        {detail?.yaml && <pre className="k8s-yaml">{detail.yaml}</pre>}
        {detail?.log != null && <pre className="k8s-log">{detail.log}</pre>}
      </Drawer>
    </div>
  )
}
