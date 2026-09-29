import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert, Button, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Switch, message, Collapse,
} from 'antd'
import api from '../../api/request'
import ProxyArchitectureDiagram, {
  parseBackendsFromConf,
  parseBackendsJSON,
  parseBackendsText,
} from './proxy/ProxyArchitectureDiagram'
import './ProxyWorkbench.css'

type ProxyKind = 'haproxy' | 'nginx' | 'lvs'

type Backend = { name?: string; host: string; port: number; weight?: number; backup?: boolean }

type ProxyService = {
  id: string
  name: string
  kind: string
  agent_base_url?: string
  agent_id?: string
  agent_user?: string
  password_set?: boolean
  conf_path: string
  conf_content?: string
  systemd_unit?: string
  vip?: string
  listen_port?: number
  backends_json?: string
  options_json?: string
  topology?: string
  nodes?: { name?: string; role?: string; agent_id?: string; priority?: number }[]
  remark?: string
  status?: string
  pushed_at?: string
  conf_version?: number
}

function parseHANodes(row?: ProxyService | null): { role: string; name?: string; agent_id?: string; priority?: number }[] {
  if (!row) return []
  if (Array.isArray(row.nodes) && row.nodes.length) return row.nodes.map((n) => ({ ...n, role: n.role || '' }))
  if (!row.options_json) return []
  try {
    const o = JSON.parse(row.options_json)
    return Array.isArray(o?.nodes) ? o.nodes : []
  } catch {
    return []
  }
}

const SOURCE_LABEL: Record<string, string> = {
  create: '登记',
  save: '保存',
  push: '下发',
  restore: '恢复',
  regenerate: '重生成',
}

const KIND_LABEL: Record<string, string> = {
  haproxy: 'HAProxy',
  nginx: 'Nginx',
  lvs: 'LVS (Keepalived)',
}

const LB_ALGO = [
  { value: 'rr', label: 'rr 轮询' },
  { value: 'wrr', label: 'wrr 加权轮询' },
  { value: 'lc', label: 'lc 最少连接' },
  { value: 'wlc', label: 'wlc 加权最少连接' },
  { value: 'sh', label: 'sh 源地址哈希' },
  { value: 'dh', label: 'dh 目标哈希' },
  { value: 'lblc', label: 'lblc' },
  { value: 'lblcr', label: 'lblcr' },
  { value: 'sed', label: 'sed' },
  { value: 'nq', label: 'nq' },
]

export default function ProxyWorkbench({ kind }: { kind: ProxyKind }) {
  const [list, setList] = useState<ProxyService[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<ProxyService | null>(null)
  const [agents, setAgents] = useState<any[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [createMode, setCreateMode] = useState<'register' | 'deploy'>('register')
  const [deploying, setDeploying] = useState(false)
  const [conf, setConf] = useState('')
  const [reloadAfter, setReloadAfter] = useState(true)
  const [form] = Form.useForm()
  const [svcStatus, setSvcStatus] = useState<any>(null)
  const [lvsDiag, setLvsDiag] = useState<any>(null)
  const [ipvsOpen, setIpvsOpen] = useState(false)
  const [verOpen, setVerOpen] = useState(false)
  const [versions, setVersions] = useState<any[]>([])
  const [verTotal, setVerTotal] = useState(0)
  const [verPage, setVerPage] = useState(1)
  const [verLoading, setVerLoading] = useState(false)
  const [verDetail, setVerDetail] = useState<any>(null)
  const [restorePush, setRestorePush] = useState(false)

  const draftName = Form.useWatch('name', form)
  const draftVip = Form.useWatch('vip', form)
  const draftPort = Form.useWatch('listen_port', form)
  const draftBackendsText = Form.useWatch('backends_text', form)
  const draftLbKind = Form.useWatch('lvs_lb_kind', form)
  const draftLbAlgo = Form.useWatch('lvs_lb_algo', form)
  const draftIface = Form.useWatch('lvs_interface', form)
  const draftNodes = Form.useWatch('nodes', form)

  const createPreviewBackends = useMemo(
    () => parseBackendsText(draftBackendsText, Number(draftPort) || 3306),
    [draftBackendsText, draftPort],
  )

  const selectedArchBackends = useMemo(() => {
    if (!selected) return [] as Backend[]
    const fromJson = parseBackendsJSON(selected.backends_json)
    if (fromJson.length) return fromJson
    return parseBackendsFromConf(selected.kind, conf || selected.conf_content, selected.listen_port || 3306)
  }, [selected, conf])

  const selectedLbMeta = useMemo(() => {
    const text = conf || selected?.conf_content || ''
    const kindM = /lb_kind\s+(\w+)/i.exec(text)
    const algoM = /lb_algo\s+(\w+)/i.exec(text)
    const ifM = /interface\s+(\S+)/i.exec(text)
    return {
      lbKind: kindM?.[1],
      lbAlgo: algoM?.[1],
      iface: ifM?.[1],
    }
  }, [conf, selected])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const { data } = await api.get('/proxy/services', { params: { kind } })
      setList(data.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [kind])

  useEffect(() => {
    load()
    api.get('/platform/agents').then((r) => setAgents(r.data?.data || [])).catch(() => {})
  }, [load])

  useEffect(() => {
    setSelected(null)
    setConf('')
    setSvcStatus(null)
    setLvsDiag(null)
  }, [kind])

  const openDetail = async (row: ProxyService) => {
    try {
      const { data } = await api.get(`/proxy/services/${row.id}`)
      const s = data.data as ProxyService
      setSelected(s)
      setConf(s.conf_content || '')
      try {
        const st = await api.get(`/proxy/services/${row.id}/service/status`)
        setSvcStatus(st.data?.data)
      } catch {
        setSvcStatus(null)
      }
      // 三种代理均有 Keepalived VIP，统一拉取 VIP 在本机探测
      try {
        const st = await api.get(`/proxy/services/${row.id}/lvs/status`)
        setLvsDiag(st.data?.data)
      } catch {
        setLvsDiag(null)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || '读取失败')
    }
  }

  const loadVersions = async (svcId: string, page = 1) => {
    setVerLoading(true)
    try {
      const { data } = await api.get(`/proxy/services/${svcId}/versions`, {
        params: { current: page, page_size: 10 },
      })
      setVersions(data.data?.list || [])
      setVerTotal(data.data?.pagination?.total || 0)
      setVerPage(page)
    } catch {
      setVersions([])
      setVerTotal(0)
    } finally {
      setVerLoading(false)
    }
  }

  const openVersionHistory = async () => {
    if (!selected) return
    setVerOpen(true)
    await loadVersions(selected.id, 1)
  }

  const columns = useMemo(
    () => [
      { title: '名称', dataIndex: 'name' },
      { title: '配置路径', dataIndex: 'conf_path', ellipsis: true },
      { title: 'VIP', dataIndex: 'vip', width: 120, render: (v: string) => v || '-' },
      {
        title: 'HA',
        width: 100,
        render: (_: any, r: ProxyService) => {
          const n = parseHANodes(r).length
          if (!n) return '-'
          return <Tag color="geekblue">{r.topology === 'ha_multi' ? `多节点×${n}` : `主备×${n}`}</Tag>
        },
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (v: string) => <Tag color={v === 'active' ? 'green' : 'default'}>{v || 'draft'}</Tag>,
      },
      { title: '最近下发', dataIndex: 'pushed_at', width: 180, render: (v: string) => v || '-' },
      {
        title: '操作',
        width: 100,
        render: (_: any, r: ProxyService) => (
          <Button type="link" size="small" onClick={() => openDetail(r)}>
            管理
          </Button>
        ),
      },
    ],
    [],
  )

  return (
    <div className="proxy-wb">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message={`${KIND_LABEL[kind] || kind} 代理管理`}
        description={
          kind === 'lvs'
            ? '高可用：须主+备 2 个 Director（平台 Agent :8090），各装 keepalived+ipvsadm；同一 VIP，MASTER/BACKUP 自动分配 priority。VIP 不可与已有地址冲突。RS 走 backends。'
            : kind === 'nginx'
              ? '高可用：至少主+备 2 节点，可按需追加更多 backup（最多 8）；各装 nginx+keepalived，同 VIP VRRP 链（扩冗余，VIP 同时仅 MASTER 持有）。亦可用 LVS 做多 Nginx 主动负载。'
              : '高可用：须主+备 2 节点（平台 Agent :8090），各装 haproxy+keepalived；同 VIP，主备漂移。VIP 必填且不可冲突。'
        }
      />
      <Space style={{ marginBottom: 12 }} wrap>
        <Button
          type="primary"
          onClick={() => {
            setCreateMode('deploy')
            form.resetFields()
            form.setFieldsValue({
              kind,
              listen_port: 3306,
              mode: 'tcp',
              enable_start: true,
              push_conf: true,
              skip_install: false,
              lvs_interface: 'eth0',
              lvs_vrid: 51,
              lvs_lb_algo: 'wrr',
              lvs_lb_kind: 'DR',
              lvs_delay_loop: 6,
              nodes: [
                { role: 'master', name: 'master' },
                { role: 'backup', name: 'backup' },
              ],
            })
            setCreateOpen(true)
          }}
        >
          自建部署
        </Button>
        <Button
          onClick={() => {
            setCreateMode('register')
            form.resetFields()
            form.setFieldsValue({
              kind,
              listen_port: 3306,
              mode: 'tcp',
              generate: true,
              lvs_interface: 'eth0',
              lvs_vrid: 51,
              lvs_lb_algo: 'wrr',
              lvs_lb_kind: 'DR',
              lvs_delay_loop: 6,
              nodes: [
                { role: 'master', name: 'master' },
                { role: 'backup', name: 'backup' },
              ],
            })
            setCreateOpen(true)
          }}
        >
          登记已有
        </Button>
        <Button onClick={load}>刷新</Button>
      </Space>
      <Table rowKey="id" loading={loading} dataSource={list} columns={columns as any} pagination={{ pageSize: 10 }} size="middle" />

      {selected && (
        <div className="proxy-wb-panel">
          <div className="proxy-wb-panel-hd">
            <Space wrap>
              <strong>{selected.name}</strong>
              <Tag>{KIND_LABEL[selected.kind] || selected.kind}</Tag>
              {selected.conf_version ? <Tag color="blue">配置 v{selected.conf_version}</Tag> : null}
              {svcStatus && (
                <Tag color={svcStatus.active ? 'green' : 'default'}>
                  {svcStatus.unit}: {svcStatus.active ? '运行中' : svcStatus.state || '未运行'}
                </Tag>
              )}
              {svcStatus?.keepalived && (
                <Tag color={svcStatus.keepalived.active ? 'green' : 'default'}>
                  keepalived: {svcStatus.keepalived.active ? '运行中' : svcStatus.keepalived.state || '未运行'}
                </Tag>
              )}
              {Array.isArray(svcStatus?.ha_nodes) && svcStatus.ha_nodes.map((n: any) => (
                <Tag key={`${n.role}-${n.name}`} color={n.ok && n.status?.active ? 'green' : n.ok ? 'orange' : 'red'}>
                  {(n.role || '').toUpperCase()} {n.name || ''}: {n.ok ? (n.status?.active ? '运行' : (n.status?.state || '未运行')) : (n.error || '不可达')}
                </Tag>
              ))}
              {lvsDiag && typeof lvsDiag.vip_present === 'boolean' && (
                <Tag color={lvsDiag.vip_present ? 'green' : 'orange'}>
                  VIP {lvsDiag.vip_present ? '在本机' : '未在本机'}
                </Tag>
              )}
            </Space>
            <Space wrap>
              <Button
                size="small"
                onClick={async () => {
                  try {
                    const { data } = await api.post('/proxy/preview', {
                      kind: selected.kind,
                      name: selected.name,
                      vip: selected.vip,
                      listen_port: selected.listen_port,
                      mode: 'tcp',
                      backends: selectedArchBackends,
                      lvs: selected.kind === 'lvs'
                        ? {
                            lb_kind: selectedLbMeta.lbKind,
                            lb_algo: selectedLbMeta.lbAlgo,
                            interface: selectedLbMeta.iface,
                          }
                        : undefined,
                    })
                    setConf(data.data?.content || '')
                    message.success('已生成模板（未保存）')
                  } catch (e: any) {
                    message.error(e?.response?.data?.message || '生成失败')
                  }
                }}
              >
                重新生成模板
              </Button>
              <Button size="small" onClick={openVersionHistory}>
                版本历史
              </Button>
              <Button
                size="small"
                onClick={async () => {
                  try {
                    const backends = parseBackendsFromConf(selected.kind, conf, selected.listen_port || 3306)
                    await api.put(`/proxy/services/${selected.id}`, {
                      conf_content: conf,
                      conf_path: selected.conf_path,
                      vip: selected.vip,
                      listen_port: selected.listen_port,
                      backends: backends.length ? backends : undefined,
                    })
                    message.success('草稿已保存（有变更则记版本）')
                    openDetail(selected)
                    load()
                  } catch (e: any) {
                    message.error(e?.response?.data?.message || '保存失败')
                  }
                }}
              >
                保存草稿
              </Button>
              <Button
                type="primary"
                size="small"
                onClick={async () => {
                  try {
                    await api.post(`/proxy/services/${selected.id}/conf/push`, {
                      content: conf,
                      reload: reloadAfter,
                    })
                    message.success(reloadAfter ? '已下发并 reload' : '已下发')
                    openDetail(selected)
                    load()
                  } catch (e: any) {
                    message.error(e?.response?.data?.message || '下发失败')
                  }
                }}
              >
                校验并下发
              </Button>
              <span className="proxy-wb-switch">
                下发后 reload <Switch checked={reloadAfter} onChange={setReloadAfter} size="small" />
              </span>
              {selected.kind === 'lvs' && (
                <Button
                  size="small"
                  onClick={async () => {
                    try {
                      const { data } = await api.get(`/proxy/services/${selected.id}/lvs/ipvs`)
                      setLvsDiag((prev: any) => ({ ...(prev || {}), ipvs_detail: data.data }))
                      setIpvsOpen(true)
                    } catch (e: any) {
                      message.error(e?.response?.data?.message || 'IPVS 查询失败')
                    }
                  }}
                >
                  查看 IPVS
                </Button>
              )}
              {(['reload', 'restart', 'start', 'stop'] as const).map((act) => (
                <Button
                  key={act}
                  size="small"
                  danger={act === 'stop'}
                  onClick={async () => {
                    try {
                      await api.post(`/proxy/services/${selected.id}/service/${act}`)
                      message.success(`已 ${act}`)
                      openDetail(selected)
                    } catch (e: any) {
                      message.error(e?.response?.data?.message || `${act} 失败`)
                    }
                  }}
                >
                  {act}
                </Button>
              ))}
              <Button
                size="small"
                danger
                onClick={async () => {
                  Modal.confirm({
                    title: '删除该代理服务登记？',
                    onOk: async () => {
                      await api.delete(`/proxy/services/${selected.id}`)
                      setSelected(null)
                      load()
                    },
                  })
                }}
              >
                删除
              </Button>
            </Space>
          </div>
          <div className="proxy-wb-meta">
            <span>路径: {selected.conf_path}</span>
            <span>VIP: {selected.vip || '-'}</span>
            <span>
              HA 节点:{' '}
              {parseHANodes(selected).length
                ? parseHANodes(selected).map((n) => `${n.role}${n.name ? `(${n.name})` : ''}`).join(' / ')
                : (selected.agent_base_url || selected.agent_id || '-')}
            </span>
            <span>Unit: {selected.systemd_unit || '-'}</span>
          </div>
          <div className="proxy-wb-arch">
            <ProxyArchitectureDiagram
              kind={selected.kind}
              name={selected.name}
              vip={selected.vip}
              listenPort={selected.listen_port}
              backends={selectedArchBackends}
              haNodes={parseHANodes(selected)}
              lbKind={selected.kind === 'lvs' ? selectedLbMeta.lbKind : undefined}
              lbAlgo={selected.kind === 'lvs' ? selectedLbMeta.lbAlgo : undefined}
              interfaceName={selectedLbMeta.iface || draftIface}
              emptyHint="登记后端实例或在配置中写 server/real_server 后显示架构图"
            />
          </div>
          <Input.TextArea
            className="proxy-wb-editor"
            value={conf}
            onChange={(e) => setConf(e.target.value)}
            rows={22}
            spellCheck={false}
          />
        </div>
      )}

      <Modal
        title="IPVS 表（只读）"
        open={ipvsOpen}
        onCancel={() => setIpvsOpen(false)}
        footer={null}
        width={720}
      >
        <pre className="proxy-wb-ipvs">
          {lvsDiag?.ipvs_detail?.ipvsadm || lvsDiag?.ipvs || '(空)'}
        </pre>
        {lvsDiag?.ipvs_detail?.proc && (
          <Collapse
            size="small"
            items={[{ key: 'proc', label: '/proc/net/ip_vs', children: <pre className="proxy-wb-ipvs">{lvsDiag.ipvs_detail.proc}</pre> }]}
          />
        )}
      </Modal>

      <Modal
        title="配置版本历史"
        open={verOpen}
        onCancel={() => setVerOpen(false)}
        footer={null}
        width={860}
      >
        <div style={{ marginBottom: 8 }}>
          恢复时下发并 reload{' '}
          <Switch checked={restorePush} onChange={setRestorePush} size="small" />
          <span style={{ marginLeft: 8, color: '#6b7280', fontSize: 12 }}>
            默认只回写草稿；开启后同步到 Agent
          </span>
        </div>
        <Table
          rowKey="id"
          size="small"
          loading={verLoading}
          dataSource={versions}
          pagination={{
            current: verPage,
            pageSize: 10,
            total: verTotal,
            onChange: (p) => selected && loadVersions(selected.id, p),
          }}
          columns={[
            { title: '版本', dataIndex: 'version_no', width: 70, render: (v: number) => `v${v}` },
            {
              title: '来源',
              dataIndex: 'change_source',
              width: 90,
              render: (v: string) => SOURCE_LABEL[v] || v,
            },
            { title: '操作人', dataIndex: 'operator', width: 100, render: (v: string) => v || '-' },
            { title: '备注', dataIndex: 'remark', ellipsis: true, render: (v: string) => v || '-' },
            { title: '时间', dataIndex: 'created_at', width: 180 },
            {
              title: '操作',
              width: 160,
              render: (_: any, row: any) => (
                <Space>
                  <Button
                    type="link"
                    size="small"
                    onClick={async () => {
                      try {
                        const { data } = await api.get(`/proxy/services/${selected!.id}/versions/${row.id}`)
                        setVerDetail(data.data)
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || '读取失败')
                      }
                    }}
                  >
                    查看
                  </Button>
                  <Button
                    type="link"
                    size="small"
                    onClick={() => {
                      Modal.confirm({
                        title: `恢复到 v${row.version_no}？`,
                        content: restorePush
                          ? '将覆盖当前草稿，并下发到 Agent 后 reload。'
                          : '将覆盖当前草稿配置；不会自动下发，可稍后「校验并下发」。',
                        onOk: async () => {
                          try {
                            await api.post(`/proxy/services/${selected!.id}/versions/${row.id}/restore`, {
                              push: restorePush,
                              reload: restorePush,
                            })
                            message.success(restorePush ? '已恢复并下发' : '已恢复到草稿')
                            setVerOpen(false)
                            await openDetail(selected!)
                            load()
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || '恢复失败')
                          }
                        },
                      })
                    }}
                  >
                    恢复
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Modal>

      <Modal
        title={verDetail ? `配置内容 · v${verDetail.version_no}` : '配置内容'}
        open={!!verDetail}
        onCancel={() => setVerDetail(null)}
        footer={null}
        width={720}
      >
        {verDetail && (
          <>
            <div className="proxy-wb-meta" style={{ padding: '0 0 8px' }}>
              <span>路径: {verDetail.conf_path}</span>
              {verDetail.vip && <span>VIP: {verDetail.vip}</span>}
              {verDetail.listen_port ? <span>端口: {verDetail.listen_port}</span> : null}
              <span>来源: {SOURCE_LABEL[verDetail.change_source] || verDetail.change_source}</span>
            </div>
            <Input.TextArea
              className="proxy-wb-editor"
              value={verDetail.conf_content || ''}
              rows={18}
              readOnly
              spellCheck={false}
            />
          </>
        )}
      </Modal>

      <Modal
        title={createMode === 'deploy' ? `自建部署 ${KIND_LABEL[kind]}` : `登记已有 ${KIND_LABEL[kind]}`}
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        width={kind === 'nginx' ? 900 : 840}
        confirmLoading={deploying}
        okText={createMode === 'deploy' ? '开始部署' : '登记'}
        onOk={async () => {
          const v = await form.validateFields()
          try {
            const backends: Backend[] = parseBackendsText(v.backends_text, v.listen_port || 3306)
            const nodes = (v.nodes || []).map((n: any, i: number) => ({
              name: n.name || (n.role === 'master' ? 'master' : `backup${i}`),
              role: n.role || (i === 0 ? 'master' : 'backup'),
              agent_id: n.agent_id,
              agent_base_url: n.agent_base_url,
              interface: n.interface || v.lvs_interface || undefined,
            }))
            if (nodes.length < 2) {
              message.error('请至少配置主+备 2 个节点')
              return
            }
            const payload: any = {
              ...v,
              kind,
              backends,
              nodes,
              generate: true,
            }
            delete payload.backends_text
            delete payload.nodes
            payload.nodes = nodes
            delete payload.lvs_interface
            delete payload.lvs_state
            delete payload.lvs_vrid
            delete payload.lvs_priority
            delete payload.lvs_lb_algo
            delete payload.lvs_lb_kind
            delete payload.lvs_delay_loop
            delete payload.lvs_auth_pass
            delete payload.lvs_nopreempt
            delete payload.lvs_persistence
            delete payload.enable_start
            delete payload.push_conf
            delete payload.skip_install
            delete payload.agent_id
            delete payload.agent_base_url
            delete payload.agent_user
            delete payload.password
            if (kind === 'lvs') {
              payload.lvs = {
                interface: v.lvs_interface || 'eth0',
                virtual_router_id: v.lvs_vrid || 51,
                lb_algo: v.lvs_lb_algo,
                lb_kind: v.lvs_lb_kind,
                delay_loop: v.lvs_delay_loop,
                auth_pass: v.lvs_auth_pass || undefined,
                persistence_timeout: v.lvs_persistence || 0,
              }
            } else {
              payload.lvs = {
                interface: v.lvs_interface || 'eth0',
                virtual_router_id: v.lvs_vrid || 51,
                auth_pass: v.lvs_auth_pass || undefined,
              }
            }
            if (createMode === 'deploy') {
              setDeploying(true)
              payload.enable_start = v.enable_start !== false
              payload.push_conf = v.push_conf !== false
              payload.skip_install = !!v.skip_install
              const { data } = await api.post('/proxy/services/deploy', payload)
              message.success(data.data?.message || '已触发部署')
              setCreateOpen(false)
              await load()
              if (data.data?.service?.id) {
                const sid = data.data.service.id
                const jobs: { job_id: string; node?: string }[] = data.data.jobs?.length
                  ? data.data.jobs
                  : (data.data.job_id ? [{ job_id: data.data.job_id }] : [])
                openDetail(data.data.service)
                if (jobs.length) {
                  for (let i = 0; i < 40; i++) {
                    await new Promise((r) => setTimeout(r, 3000))
                    try {
                      let allOk = true
                      let failed: any = null
                      for (const j of jobs) {
                        const st = await api.get(`/proxy/services/${sid}/deploy/jobs/${j.job_id}`)
                        const status = st.data?.data?.job?.status
                        if (status === 'failed') {
                          failed = st.data?.data?.job
                          break
                        }
                        if (status !== 'success') allOk = false
                      }
                      if (failed) {
                        message.error(failed.message || '部署失败')
                        if (failed.log) {
                          Modal.info({ title: '部署日志', width: 720, content: <pre className="proxy-wb-ipvs">{failed.log}</pre> })
                        }
                        load()
                        break
                      }
                      if (allOk) {
                        message.success('各节点装包完成，正在确认配置下发…')
                        await new Promise((r) => setTimeout(r, 3000))
                        await openDetail({ id: sid } as ProxyService)
                        load()
                        break
                      }
                    } catch {
                      /* ignore transient */
                    }
                  }
                }
              }
            } else {
              const { data } = await api.post('/proxy/services', { ...payload, generate: true })
              message.success('已登记')
              setCreateOpen(false)
              await load()
              if (data.data?.id) openDetail(data.data)
            }
          } catch (e: any) {
            message.error(e?.response?.data?.message || (createMode === 'deploy' ? '部署失败' : '创建失败'))
          } finally {
            setDeploying(false)
          }
        }}
      >
        {(createPreviewBackends.length > 0 || draftVip || draftPort || (draftNodes || []).length > 0) && (
          <ProxyArchitectureDiagram
            compact
            kind={kind}
            name={draftName}
            vip={draftVip}
            listenPort={Number(draftPort) || undefined}
            backends={createPreviewBackends}
            haNodes={(draftNodes || []).map((n: any, i: number) => ({
              name: n?.name || (n?.role === 'master' ? 'master' : `backup${i}`),
              role: n?.role || (i === 0 ? 'master' : 'backup'),
            }))}
            lbKind={draftLbKind}
            lbAlgo={draftLbAlgo}
            interfaceName={draftIface}
          />
        )}
        {createMode === 'deploy' && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message={
              kind === 'lvs'
                ? '将在主+备各节点安装 keepalived/ipvsadm；VIP 探测通过后分别下发 MASTER/BACKUP keepalived.conf。'
                : kind === 'nginx'
                  ? '将在各 HA 节点安装 nginx+keepalived；同 VIP 的 VRRP 优先级链；可追加更多 backup。'
                  : '将在主+备各节点安装 haproxy+keepalived；分别下发 MASTER/BACKUP Keepalived 与统一 haproxy.cfg。'
            }
          />
        )}
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="如 mysql-rw-proxy" />
          </Form.Item>
          <Form.Item
            label={kind === 'nginx' ? 'HA 节点（至少主+备，可追加）' : 'HA 节点（主+备）'}
            required
            extra="每个节点绑定一台平台 Agent（:8090）；priority/state 由角色自动生成"
          >
            <Form.List name="nodes">
              {(fields, { add, remove }) => (
                <>
                  {fields.map((field, index) => (
                    <Space key={field.key} align="baseline" wrap style={{ display: 'flex', marginBottom: 8 }}>
                      <Form.Item
                        {...field}
                        name={[field.name, 'role']}
                        rules={[{ required: true, message: '角色' }]}
                        initialValue={index === 0 ? 'master' : 'backup'}
                      >
                        <Select
                          style={{ width: 110 }}
                          options={[
                            { value: 'master', label: 'MASTER' },
                            { value: 'backup', label: 'BACKUP' },
                          ]}
                          disabled={kind !== 'nginx' && index < 2}
                        />
                      </Form.Item>
                      <Form.Item
                        {...field}
                        name={[field.name, 'agent_id']}
                        rules={[{ required: true, message: '选择 Agent' }]}
                      >
                        <Select
                          style={{ minWidth: 260 }}
                          placeholder="平台 Agent"
                          options={agents.map((a) => ({ value: a.id, label: `${a.name} (${a.base_url})` }))}
                        />
                      </Form.Item>
                      <Form.Item {...field} name={[field.name, 'name']}>
                        <Input placeholder="节点名" style={{ width: 100 }} />
                      </Form.Item>
                      {kind === 'nginx' && fields.length > 2 && index >= 2 && (
                        <Button type="link" danger onClick={() => remove(field.name)}>删除</Button>
                      )}
                    </Space>
                  ))}
                  {kind === 'nginx' && fields.length < 8 && (
                    <Button type="dashed" onClick={() => add({ role: 'backup', name: `backup${fields.length}` })} block>
                      追加 backup 节点
                    </Button>
                  )}
                </>
              )}
            </Form.List>
          </Form.Item>
          {createMode === 'deploy' && (
            <Space wrap style={{ marginBottom: 8 }}>
              <Form.Item name="enable_start" label="安装后启动" valuePropName="checked" initialValue>
                <Switch />
              </Form.Item>
              <Form.Item name="push_conf" label="部署后下发配置" valuePropName="checked" initialValue>
                <Switch />
              </Form.Item>
              <Form.Item name="skip_install" label="跳过装包（已安装）" valuePropName="checked">
                <Switch />
              </Form.Item>
            </Space>
          )}
          <Form.Item name="conf_path" label="配置路径" initialValue={kind === 'nginx' ? '/etc/nginx/nginx.conf' : kind === 'lvs' ? '/etc/keepalived/keepalived.conf' : '/etc/haproxy/haproxy.cfg'}>
            <Input />
          </Form.Item>
          <Form.Item name="listen_port" label={kind === 'lvs' ? '虚拟服务端口' : '监听端口'}>
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="vip"
            label="VIP"
            rules={[{ required: true, message: '三种代理均需填写 VIP（由 Keepalived 接管）' }]}
            extra="不可与已登记代理 / HA 集群 VIP 重复，部署时还会探测本机与网络是否已被占用"
          >
            <Input placeholder="10.0.0.100" />
          </Form.Item>
          <Space wrap style={{ width: '100%' }} size="middle">
            <Form.Item name="lvs_interface" label="网卡 interface（共享）" style={{ minWidth: 160 }}>
              <Input placeholder="eth0" />
            </Form.Item>
            <Form.Item name="lvs_vrid" label="virtual_router_id" style={{ minWidth: 120 }}>
              <InputNumber min={1} max={255} />
            </Form.Item>
            <Form.Item name="lvs_auth_pass" label="auth_pass（≤8）" style={{ minWidth: 160 }}>
              <Input.Password placeholder="默认 chgme01" maxLength={8} />
            </Form.Item>
          </Space>
          {kind === 'lvs' && (
            <>
              <Space wrap style={{ width: '100%' }} size="middle">
                <Form.Item name="lvs_lb_kind" label="lb_kind（转发）" style={{ minWidth: 160 }}>
                  <Select
                    options={[
                      { value: 'DR', label: 'DR（直连路由）' },
                      { value: 'NAT', label: 'NAT' },
                      { value: 'TUN', label: 'TUN（隧道）' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name="lvs_lb_algo" label="lb_algo（调度）" style={{ minWidth: 180 }}>
                  <Select options={LB_ALGO} />
                </Form.Item>
                <Form.Item name="lvs_delay_loop" label="delay_loop(秒)" style={{ minWidth: 120 }}>
                  <InputNumber min={1} max={120} />
                </Form.Item>
                <Form.Item name="lvs_persistence" label="会话保持(秒)" style={{ minWidth: 120 }}>
                  <InputNumber min={0} max={86400} placeholder="0=关闭" />
                </Form.Item>
              </Space>
            </>
          )}
          {kind !== 'lvs' && (
            <Form.Item name="mode" label="模式" initialValue="tcp">
              <Select options={[{ value: 'tcp', label: 'TCP' }, { value: 'http', label: 'HTTP' }]} />
            </Form.Item>
          )}
          <Form.Item
            name="backends_text"
            label={kind === 'lvs' ? 'Real Server 列表' : '后端列表'}
            extra="每行 host:port 或 host:port,weight"
          >
            <Input.TextArea rows={4} placeholder={'10.0.0.11:3306,100\n10.0.0.12:3306,100'} />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
