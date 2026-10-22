import React, { useEffect, useState } from 'react'
import { Alert, Button, Card, Col, Descriptions, Form, Input, Row, Select, Space, Table, Tag, message } from 'antd'
import api from '../../api/request'
import { RECORD_TYPES, errMsg, recordValue } from './dnsApi'

interface SimResult {
  name: string
  type: string
  source_ip: string
  range_name: string
  rcode: string
  forward?: string
  answers: { name: string; type: string; data: string; ttl: number }[]
  steps: string[]
}

interface ProbeResult {
  server: string
  rcode: string
  answers: string[]
  error?: string
  elapsed_ms: number
}

interface NodeOpt { ip: string; name: string; port: number; deployed: boolean }

const RCODE_COLOR: Record<string, string> = { NOERROR: 'green', NXDOMAIN: 'orange', FORWARD: 'blue', ERROR: 'red' }

/** 模拟结果中最终应答（去掉 CNAME 链）与实查结果比较。 */
function finalAnswers(sim?: SimResult | null): string[] {
  if (!sim) return []
  const list = sim.answers.filter((a) => a.type === sim.type || sim.type === 'CNAME').map((a) => a.data.toLowerCase())
  return list.sort()
}

function sameAnswers(sim: SimResult | null, probe: ProbeResult | null): boolean | null {
  if (!sim || !probe || sim.rcode === 'FORWARD' || probe.rcode === 'ERROR') return null
  if (sim.rcode === 'NXDOMAIN' || probe.rcode === 'NXDOMAIN') return sim.rcode === probe.rcode
  const a = finalAnswers(sim)
  const b = [...probe.answers].map((x) => x.toLowerCase()).sort()
  return a.join(',') === b.join(',')
}

function ProbeCard({ title, res, source, sim }: { title: string; res: ProbeResult | null; source?: string; sim: SimResult | null }) {
  const same = sameAnswers(sim, res)
  return (
    <Card size="small" title={title} style={{ height: '100%', borderColor: same === false ? '#ff4d4f' : undefined }}>
      {!res ? <span style={{ color: '#8c8c8c' }}>未查询</span> : (
        <Descriptions column={1} size="small">
          <Descriptions.Item label="服务器">{res.server}</Descriptions.Item>
          <Descriptions.Item label="来源 IP">{source || '-'}</Descriptions.Item>
          <Descriptions.Item label="结果"><Tag color={RCODE_COLOR[res.rcode]}>{res.rcode}</Tag>{res.elapsed_ms}ms</Descriptions.Item>
          <Descriptions.Item label="应答">{res.answers.length ? res.answers.map((a) => <div key={a}>{a}</div>) : '-'}</Descriptions.Item>
          {res.error && <Descriptions.Item label="错误">{res.error}</Descriptions.Item>}
          {same !== null && <Descriptions.Item label="与模拟">{same ? <Tag color="green">一致</Tag> : <Tag color="red">不一致</Tag>}</Descriptions.Item>}
        </Descriptions>
      )}
    </Card>
  )
}

export default function DnsTools() {
  const [form] = Form.useForm()
  const [nodes, setNodes] = useState<NodeOpt[]>([])
  const [sim, setSim] = useState<SimResult | null>(null)
  const [probe, setProbe] = useState<{ res: ProbeResult; source: string } | null>(null)
  const [nodeRes, setNodeRes] = useState<{ res: ProbeResult; source: string } | null>(null)
  const [busy, setBusy] = useState('')
  const [ip, setIp] = useState('')
  const [rev, setRev] = useState<any>(null)

  useEffect(() => {
    api.get('/dns/nodes').then(({ data }) => {
      const list: NodeOpt[] = data.data || []
      setNodes(list)
      if (list[0]) form.setFieldsValue({ node: list[0].ip })
    }).catch(() => undefined)
    form.setFieldsValue({ type: 'A' })
  }, [])

  async function run(kind: 'sim' | 'probe' | 'node' | 'all') {
    const v = await form.validateFields()
    const body = { name: v.name, type: v.type, source_ip: v.source_ip, node: v.node }
    setBusy(kind)
    const tasks: Promise<unknown>[] = []
    if (kind === 'sim' || kind === 'all') {
      if (!v.source_ip) {
        message.warning('平台模拟需要填写来源 IP')
      } else {
        tasks.push(api.post('/dns/resolve/simulate', body).then(({ data }) => setSim(data.data)).catch((e) => message.error(errMsg(e, '模拟失败'))))
      }
    }
    if (kind === 'probe' || kind === 'all') {
      tasks.push(api.post('/dns/resolve/probe', body).then(({ data }) => setProbe({ res: data.data, source: data.source_ip })).catch((e) => message.error(errMsg(e, '实查失败'))))
    }
    if (kind === 'node' || kind === 'all') {
      tasks.push(api.post('/dns/resolve/node', body).then(({ data }) => setNodeRes({ res: data.data, source: data.source_ip })).catch((e) => message.error(errMsg(e, '节点实查失败'))))
    }
    await Promise.all(tasks)
    setBusy('')
  }

  async function reverse() {
    try {
      const { data } = await api.get('/dns/reverse', { params: { ip } })
      setRev(data)
    } catch (e: any) {
      message.error(errMsg(e, '查询失败'))
    }
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} size={16}>
      <Card size="small" title="解析测试">
        <Form form={form} layout="inline">
          <Form.Item name="name" rules={[{ required: true, message: '请填写域名' }]}><Input placeholder="db.example.com" style={{ width: 240 }} /></Form.Item>
          <Form.Item name="type"><Select style={{ width: 100 }} options={RECORD_TYPES.map((t) => ({ value: t, label: t }))} /></Form.Item>
          <Form.Item name="source_ip"><Input placeholder="来源 IP（模拟用）" style={{ width: 170 }} /></Form.Item>
          <Form.Item name="node">
            <Select style={{ width: 200 }} placeholder="节点" options={nodes.map((n) => ({ value: n.ip, label: `${n.ip}${n.name && n.name !== n.ip ? ' ' + n.name : ''}` }))} />
          </Form.Item>
          <Space>
            <Button type="primary" loading={busy === 'all'} onClick={() => run('all')}>全部执行</Button>
            <Button loading={busy === 'sim'} onClick={() => run('sim')}>平台模拟</Button>
            <Button loading={busy === 'probe'} onClick={() => run('probe')}>从平台实查</Button>
            <Button loading={busy === 'node'} onClick={() => run('node')}>从节点实查</Button>
          </Space>
        </Form>
        <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 8 }}>
          平台模拟按 hdns 规则推算（来源 IP → 解析范围 → 记录 → 追 CNAME）；从平台实查的来源 IP 是平台本机；从节点实查经节点上的 Db Agent 向节点内网 IP 查询，来源 IP 是节点自身。
        </div>
      </Card>
      <Row gutter={16}>
        <Col xs={24} lg={8}>
          <Card size="small" title="平台模拟" style={{ height: '100%' }}>
            {!sim ? <span style={{ color: '#8c8c8c' }}>未执行</span> : (
              <>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="来源 IP">{sim.source_ip}</Descriptions.Item>
                  <Descriptions.Item label="解析范围">{sim.range_name}</Descriptions.Item>
                  <Descriptions.Item label="结果"><Tag color={RCODE_COLOR[sim.rcode]}>{sim.rcode === 'FORWARD' ? '转发上游' : sim.rcode}</Tag></Descriptions.Item>
                  <Descriptions.Item label="应答">{sim.answers.length ? sim.answers.map((a, i) => <div key={i}>{a.name} {a.type} {a.data} ({a.ttl}s)</div>) : '-'}</Descriptions.Item>
                </Descriptions>
                <ol style={{ paddingLeft: 18, marginTop: 8, fontSize: 12, color: '#595959' }}>
                  {sim.steps.map((s, i) => <li key={i}>{s}</li>)}
                </ol>
              </>
            )}
          </Card>
        </Col>
        <Col xs={24} lg={8}><ProbeCard title="从平台实查" res={probe?.res || null} source={probe?.source} sim={sim} /></Col>
        <Col xs={24} lg={8}><ProbeCard title="从节点实查" res={nodeRes?.res || null} source={nodeRes?.source} sim={sim} /></Col>
      </Row>

      <Card size="small" title="按 IP 反查">
        <Space>
          <Input placeholder="10.1.2.3" value={ip} onChange={(e) => setIp(e.target.value)} style={{ width: 200 }} onPressEnter={reverse} />
          <Button onClick={reverse}>查询</Button>
        </Space>
        {rev && (
          <div style={{ marginTop: 12 }}>
            {!rev.data.records.length && !rev.data.cnames.length && <Alert type="info" message="没有指向该 IP 的记录" />}
            {(rev.data.records.length > 0 || rev.data.cnames.length > 0) && (
              <Table
                size="small"
                rowKey="id"
                pagination={false}
                dataSource={[...rev.data.records, ...rev.data.cnames]}
                columns={[
                  { title: '解析域', dataIndex: 'zone_id', render: (v: string) => rev.zones?.[v] || v },
                  { title: '主机记录', dataIndex: 'name' },
                  { title: '类型', dataIndex: 'type' },
                  { title: '记录值', render: (_: unknown, r: any) => recordValue(r) },
                  { title: '解析范围', dataIndex: 'parsing_range_id', render: (v: string) => rev.ranges?.[v] || v },
                  { title: '状态', dataIndex: 'status', render: (v: string) => v === 'Active' ? <Tag color="green">运行</Tag> : <Tag>停止</Tag> },
                ]}
              />
            )}
            <div style={{ marginTop: 8 }}>
              关联实例：{rev.instances.length ? rev.instances.map((i: any) => <Tag key={i.instance_id} color="geekblue">{i.instance_name}（{i.addr}）</Tag>) : '无'}
            </div>
          </div>
        )}
      </Card>
    </Space>
  )
}
