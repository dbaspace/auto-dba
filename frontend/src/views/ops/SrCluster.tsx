import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert, AutoComplete, Breadcrumb, Button, Card, Checkbox, Descriptions, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs, Tag, Tooltip, Upload, message,
} from 'antd'
import { MinusCircleOutlined, PlusOutlined, SyncOutlined } from '@ant-design/icons'
import type { ColumnType } from 'antd/es/table'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import api from '../../api/request'
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'
import { useAppSelector } from '../../store'
import {
  NodePrecheckPanel, agentMatchesHost, agentMismatchMessage, hostAddrs, hostPrimaryAddr, resolveNodeHostInstanceId, useHostApprovers,
  type NodePrecheckResult,
} from './node-precheck/nodePrecheck'
import {
  ChangeCategoryTabs, SR_CHANGE_CATEGORIES, changeCategoryLabel, changeCategoryOf, filterChangesByCategory, pickChangeInCategory,
  type ChangeCategoryKey,
} from './changeCategories'
import {
  ChangeConfRollbackModal, ConfApplyConfirmModal, ConfHistoryPanel, keySummary, type ConfPatchPlan,
} from './SrConfDiff'
import { activeDeployHostIds, isAgentDeployActive, useAgentDeployTracker } from './agentDeployTracker'
import { ensureAgentForHost, matchAgentByHost } from './ensureAgentForHost'
import './SrCluster.css'

type SrAgent = { id: string; name?: string; base_url: string; username?: string; password_set?: boolean; host_instance_id?: string }
type SrCluster = {
  id: string; name: string; environment?: string; version?: string; release_id?: string
  software_base?: string; be_software_base?: string; data_root?: string
  fe_host?: string; fe_query_port?: number; status?: string
  source?: string // deploy | register
  tenant_id?: string; tenant_name?: string
}
type SrNode = {
  id: string; component: string; host: string; agent_id?: string; is_primary_fe?: boolean
  fe_role?: string; status?: string; systemd_unit?: string
  software_path?: string; data_root?: string
}
type SrNodeLive = {
  role?: string; alive?: string; version?: string; decommissioned?: boolean; tablet_num?: string
  replayed_journal_id?: string; last_heartbeat?: string; start_time?: string
  cpu_cores?: string; total_capacity?: string; avail_capacity?: string; data_used?: string; used_pct?: string
}
type SrInstanceLink = { id: string; name: string; host: string; port: number; password_set?: boolean; matched_by?: string }
type SrMonitorLink = { id: string; name: string; collector?: string; fe_count: number; be_count: number; missing?: string[] }
type SrChangeStep = {
  id: string; seq: number; step_key: string; title: string; status: string; log_text?: string
  node_id?: string; started_at?: string; ended_at?: string
}
type SrChange = {
  id: string; cluster_id: string; type: string; status: string; log_text?: string; payload_json?: string
  steps?: SrChangeStep[]
  created_by?: string; created_at?: string; updated_at?: string
}

const CHANGE_TYPE_LABEL: Record<string, string> = {
  cluster_init: '集群初始化', cluster_reinit: '集群重装', add_fe: '扩容 FE', add_be: '扩容 BE',
  restart_node: '重启节点', start_node: '启动节点', stop_node: '停止节点', remove_fe: '下线 FE',
  remove_be: '下线 BE', decommission_be: '下线 BE', recommission_be: '恢复 BE', upgrade: '版本升级', upgrade_rollback: '升级回滚',
  config_apply: '配置下发', config_rollback: '配置回退',
}

function fmtDuration(start?: string, end?: string): string {
  if (!start) return '—'
  const s = new Date(start).getTime()
  const e = end ? new Date(end).getTime() : Date.now()
  if (Number.isNaN(s) || Number.isNaN(e) || e < s) return '—'
  const sec = Math.round((e - s) / 1000)
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m${sec % 60}s`
  return `${Math.floor(sec / 3600)}h${Math.floor((sec % 3600) / 60)}m`
}

function fmtClock(v?: string): string {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function lastLogLine(log?: string): string {
  const lines = String(log || '').trim().split('\n').map((l) => l.trim()).filter(Boolean)
  return lines[lines.length - 1] || ''
}

/** 步骤日志里最后一条「失败:」行（引擎追加），没有则取最后一行。 */
function lastFailLine(log?: string): string {
  const lines = String(log || '').trim().split('\n')
  for (let i = lines.length - 1; i >= 0; i--) {
    const idx = lines[i].indexOf('失败: ')
    if (idx >= 0) return lines[i].slice(idx + 4).trim()
  }
  return lastLogLine(log)
}

/** 与后端 nodeStillUpgraded 一致：该节点已替换 bin/lib 且之后未回滚。 */
function stepCanRollback(s: SrChangeStep): boolean {
  if (!s.step_key.startsWith('upgrade_node_')) return false
  const log = s.log_text || ''
  const replaced = Math.max(log.lastIndexOf('[replaced]'), log.lastIndexOf('升级后启服失败'))
  const rolled = log.lastIndexOf('[rolled_back]')
  if (replaced >= 0 && replaced > rolled) return true
  return s.status === 'success' && rolled < 0
}

function changeBackupSuffix(ch: SrChange): string {
  try {
    return String(JSON.parse(ch.payload_json || '{}')?.backup_suffix || '')
  } catch {
    return ''
  }
}
type SrReleaseArtifact = {
  id: string; role: string; object_key: string; sha256: string
  filename?: string; size_bytes?: number; format?: string
}
type SrRelease = {
  id: string; version: string; channel?: string; jdk_min?: string; jdk_max?: string; enabled?: boolean
  artifacts?: SrReleaseArtifact[]
}

/** 与后端 srcluster.DefaultJDKRange 一致：2.x 8–11；3.0–3.4 11–17；3.5+ 17+。 */
function defaultJdkRange(version?: string): { jdk_min: string; jdk_max: string } | null {
  const m = /(\d+)\.(\d+)/.exec(String(version || ''))
  if (!m) return null
  const major = Number(m[1])
  const minor = Number(m[2])
  if (major < 3) return { jdk_min: '8', jdk_max: '11' }
  if (major === 3 && minor < 5) return { jdk_min: '11', jdk_max: '17' }
  return { jdk_min: '17', jdk_max: '' }
}

function jdkRangeLabel(min?: string, max?: string): string {
  const lo = String(min || '').trim()
  const hi = String(max || '').trim()
  if (lo && hi) return `${lo} – ${hi}`
  if (lo) return `${lo}+`
  if (hi) return `≤ ${hi}`
  return '—'
}
type SrOSSBind = {
  oss_setting_id?: string
  prefix?: string
  oss_environment?: string
  oss_endpoint?: string
  oss_bucket?: string
  oss_configured?: boolean
}
type OSSOption = { id: string; environment?: string; endpoint?: string; bucket?: string; configured?: boolean }

/** 版本发行通道：英文存库，界面展示中文 */
const SR_CHANNEL_OPTIONS = [
  { value: 'lts', label: '长期支持（LTS）' },
  { value: 'stable', label: '稳定版' },
  { value: 'preview', label: '预览版' },
] as const

function srChannelLabel(channel?: string): string {
  const c = (channel || 'stable').toLowerCase()
  const hit = SR_CHANNEL_OPTIONS.find((o) => o.value === c)
  return hit ? hit.label : (channel || '稳定版')
}

type MonitorPreview = {
  task_name?: string
  env?: string
  domain?: string
  version?: string
  endpoints?: string[]
  labels?: Record<string, string>
}
type HostInst = { id: string; name: string; host?: string; domain_name?: string; domains?: string[]; port?: number }
type SrAgentDeploy = {
  id: string
  host_instance_id: string
  agent_id?: string
  status: string
  listen_port: number
  remote_dir: string
  base_url?: string
  log_text?: string
  created_at?: string
  finished_at?: string
}

function endpointHost(ep: string): string {
  const s = (ep || '').trim()
  if (!s) return ''
  // host:port or [ipv6]:port
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end > 0) return s.slice(1, end)
  }
  const idx = s.lastIndexOf(':')
  if (idx > 0 && !s.includes('://')) {
    const maybePort = s.slice(idx + 1)
    if (/^\d+$/.test(maybePort)) return s.slice(0, idx)
  }
  return s
}

function matchAgentForHost(agents: SrAgent[], host: string): SrAgent | undefined {
  return matchAgentByHost(agents, host) as SrAgent | undefined
}

const statusColor: Record<string, string> = {
  draft: 'default', initializing: 'processing', ready: 'success', failed: 'error',
  pending: 'default', running: 'processing', success: 'success', cancelled: 'warning',
}

/** 集群纳管生命周期态展示（failed = 初始化/重建失败，非启停运维失败） */
const clusterStatusLabel: Record<string, string> = {
  draft: '草稿',
  initializing: '初始化中',
  ready: '就绪',
  failed: '初始化失败',
}

function renderClusterStatus(s?: string) {
  const key = s || ''
  return <Tag color={statusColor[key]}>{clusterStatusLabel[key] || key || '-'}</Tag>
}

type FailureHint = {
  summary: string
  howto: string[]
  actionTab?: 'settings' | 'agents' | 'clusters'
  actionLabel?: string
}

/** 根据步骤/变更错误文案给出处理指引 */
function changeFailureHint(raw: string): FailureHint {
  const text = (raw || '').trim()
  const lower = text.toLowerCase()
  if (/JDK\s*\d+\s*is not supported|Infer+ed JAVA_HOME|系统默认 java/i.test(text)) {
    return {
      summary: 'StarRocks 启动时使用的 JDK 版本不满足要求（systemd 启动由 /usr/bin/java 推断 JAVA_HOME）',
      howto: [
        '在失败节点把系统默认 java 切到 /home/jdk（满足要求的 JDK）：update-alternatives --install /usr/bin/java java /home/jdk/bin/java 1200 && update-alternatives --install /usr/bin/javac javac /home/jdk/bin/javac 1200，再用 --config java / --config javac 选择 /home/jdk',
        'java -version 确认后，对该变更点「重试」：会重新校验 JDK，已替换过 bin/lib 的节点只做启动',
        '不想继续升级时，可在下方失败步骤点「回滚该节点」恢复旧版本 bin/lib 并启动',
      ],
    }
  }
  if (/升级后启服失败|回滚后启动失败/.test(text)) {
    return {
      summary: lastFailLine(text) || '升级后启动失败',
      howto: [
        '查看下方失败步骤里的「诊断信息」（systemctl status / journalctl / fe.out 或 be.out 末尾）定位原因',
        '在目标机修复后，对该变更点「重试」：已替换过 bin/lib 的节点只做启动',
        '需要恢复旧版本时，在失败步骤点「回滚该节点」',
      ],
    }
  }
  if (/白名单|不在环境/i.test(text)) {
    return {
      summary: text || '版本校验失败',
      howto: [
        '打开「设置」→「版本目录」，确认目标 release 仍存在且已启用',
        '编辑集群重新选择版本（以 release_id 为准，不再使用环境版本白名单）',
        '保存后回到变更页点「重试」，或重新「集群初始化」',
      ],
      actionTab: 'settings',
      actionLabel: '前往版本目录',
    }
  }
  if (/未 Alive|SHOW PROC.*frontends|SHOW PROC.*backends/i.test(text)) {
    return {
      summary: text || 'FE/BE 尚未全部 Alive',
      howto: [
        '9030 可连不代表 SHOW PROC 里 Alive=true，FE 刚启动时常需再等 1～3 分钟',
        '在 FE 上执行 mysql -h$fe -P9030 -uroot -e "SHOW PROC \'/frontends\'" 看 Alive / ErrMsg',
        '确认 fe 进程与 fe.log 无报错后，对该变更点「重试」（步骤会轮询等待 Alive）',
      ],
    }
  }
  if (/缺少\s*(fe|be)\s*分包|分包\s*object_key|object_key\/sha256|sha256\s*不完整|sha256\s*缺少/i.test(text) || lower.includes('object_key')) {
    const role = /\bfe\b/i.test(text) && !/\bbe\b/i.test(text) ? 'FE' : /\bbe\b/i.test(text) && !/\bfe\b/i.test(text) ? 'BE' : 'FE/BE'
    const shaHint = /sha256|不完整|\d+\s*位/i.test(text)
    return {
      summary: text || `版本缺少 ${role} 分包`,
      howto: shaHint
        ? [
            '打开「设置」→「版本目录」，展开本集群版本，检查 fe/be 的 sha256 是否为 64 位十六进制',
            '在本地执行：shasum -a 256 StarRocks-x.y.z.fe.tar.gz（macOS）或 sha256sum …（Linux）',
            '将完整 64 位结果粘贴到对应行并「保存」，再回变更页「重试」',
          ]
        : [
            '打开「设置」→「版本目录」，找到本集群绑定的版本并展开',
            `点击「从 OSS 同步」或「登记/上传分包」，确认 ${role} 的 object_key 与 64 位 sha256 已保存`,
            '若曾删除重登记版本，请重启后端后再点「重试」（会按版本号自动重绑 release_id）',
          ],
      actionTab: 'settings',
      actionLabel: '前往设置登记分包',
    }
  }
  if (/未绑定\s*release|请先在版本目录登记|选择版本/i.test(text)) {
    return {
      summary: text || '集群未绑定版本',
      howto: [
        '打开「集群」编辑该集群，选择已登记的版本（release）',
        '或在「设置」→「版本目录」先登记版本与分包，再绑定到集群',
        '保存后回到变更页「重试」',
      ],
      actionTab: 'clusters',
      actionLabel: '前往集群页',
    }
  }
  if (/oss|presign|签名|bucket|凭证/i.test(text)) {
    return {
      summary: text || 'OSS 制品访问失败',
      howto: [
        '打开「设置」→「制品 OSS 绑定」，确认环境/Bucket/凭证完整',
        '点击「测试」确认可达，再核对版本分包的 object_key 是否存在',
        '修好后回到变更页「重试」',
      ],
      actionTab: 'settings',
      actionLabel: '前往设置检查 OSS',
    }
  }
  if (/cipher:\s*message authentication|message authentication failed/i.test(text)) {
    return {
      summary: 'Node Agent 密码解密失败（平台密钥或登记密码不匹配）',
      howto: [
        '打开「Node Agent」页，对该节点重新设置/保存 Agent 密码（与目标机 Agent 实际密码一致）',
        '确认平台配置的 secret 密钥未更换；若曾轮换密钥，需重新登记各 Agent 密码',
        '修好后回到变更页对该失败变更点「重试」',
      ],
      actionTab: 'agents',
      actionLabel: '前往 Node Agent',
    }
  }
  if (/node\s*agent|探活|agent.*unreachable|连接.*8091|timeout/i.test(text)) {
    return {
      summary: text || 'Node Agent 不可达',
      howto: [
        '打开「Node Agent」页，确认目标机 Agent 状态为 running',
        '必要时在该页对节点执行启动/重装，并核对 base_url 与端口',
        '探活通过后回到变更页「重试」',
      ],
      actionTab: 'agents',
      actionLabel: '前往 Node Agent',
    }
  }
  if (/预检|precheck|磁盘|端口|jdk/i.test(text)) {
    return {
      summary: text || '预检未通过',
      howto: [
        '根据下方失败步骤日志逐项排查（端口占用、磁盘、JDK、路径权限等）',
        '在目标机修好后，对该变更点「重试」继续执行',
      ],
    }
  }
  return {
    summary: text || '变更失败，请查看下方步骤日志',
    howto: [
      '阅读失败步骤的日志定位原因',
      '修复后对该变更点「重试」；若需中止可「取消」后重新初始化',
    ],
  }
}

export default function SrClusterPage() {
  const navigate = useNavigate()
  const authUser = useAppSelector((s) => s.auth.user)
  const impersonating = Boolean((authUser as { impersonating?: boolean })?.impersonating)
  const currentTenant = (authUser as { current_tenant?: { id?: string; name?: string; code?: string } })?.current_tenant
  const currentTenantId = currentTenant?.id || ''
  const currentTenantLabel = currentTenant?.name || currentTenant?.code || '当前租户'
  const [tab, setTab] = useState('clusters')
  const [clusters, setClusters] = useState<SrCluster[]>([])
  const [agents, setAgents] = useState<SrAgent[]>([])
  const [agentStatus, setAgentStatus] = useState<Record<string, { status: string; message?: string }>>({})
  const [agentActingId, setAgentActingId] = useState('')
  const [changes, setChanges] = useState<SrChange[]>([])
  const [releases, setReleases] = useState<SrRelease[]>([])
  const [ossBind, setOssBind] = useState<SrOSSBind>({})
  const [ossOptions, setOssOptions] = useState<OSSOption[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<SrCluster | null>(null)
  const [nodes, setNodes] = useState<SrNode[]>([])
  const [nodeLive, setNodeLive] = useState<Record<string, SrNodeLive>>({})
  const [nodeLiveErr, setNodeLiveErr] = useState('')
  const [nodeLiveLoading, setNodeLiveLoading] = useState(false)
  const nodeLiveReq = useRef('')
  const [clusterInst, setClusterInst] = useState<SrInstanceLink | null>(null)
  const [clusterMon, setClusterMon] = useState<SrMonitorLink | null>(null)
  const [registrySyncing, setRegistrySyncing] = useState(false)
  const [changeDetail, setChangeDetail] = useState<SrChange | null>(null)
  const [changeCat, setChangeCat] = useState<ChangeCategoryKey>('init')
  const [clusterModal, setClusterModal] = useState(false)
  const [clusterModalMode, setClusterModalMode] = useState<'create' | 'register'>('create')
  const [feProbeBusy, setFeProbeBusy] = useState(false)
  const [feProbedRoles, setFeProbedRoles] = useState<Record<string, string>>({})
  const [fePreview, setFePreview] = useState<MonitorPreview[]>([])
  const [bePreview, setBePreview] = useState<MonitorPreview[]>([])
  const [monitorTask, setMonitorTask] = useState<string | undefined>()
  const [monitorDomain, setMonitorDomain] = useState<string | undefined>()
  const [nodeModal, setNodeModal] = useState(false)
  const [nodePrecheck, setNodePrecheck] = useState<NodePrecheckResult | null>(null)
  const [nodePrecheckBusy, setNodePrecheckBusy] = useState(false)
  const [nodeSubmitBusy, setNodeSubmitBusy] = useState(false)
  const nodeApprovers = useHostApprovers()
  const ticketApprovers = useHostApprovers()
  const [agentModal, setAgentModal] = useState(false)
  /** 快速登记后回填到集群表单某一行 */
  const [agentBindTarget, setAgentBindTarget] = useState<{ list: 'fe_nodes' | 'be_nodes'; index: number } | null>(null)
  const [deployModal, setDeployModal] = useState(false)
  const [deploySource, setDeploySource] = useState<'host' | 'monitor'>('monitor')
  const [deployMonTask, setDeployMonTask] = useState<string | undefined>()
  const [deployMonRows, setDeployMonRows] = useState<{ host: string; hostId?: string; name?: string }[]>([])
  const [deployMonSelected, setDeployMonSelected] = useState<string[]>([])
  const [deployBusy, setDeployBusy] = useState(false)
  const [hosts, setHosts] = useState<HostInst[]>([])
  const [deploys, setDeploys] = useState<SrAgentDeploy[]>([])
  const [deployLog, setDeployLog] = useState<SrAgentDeploy | null>(null)
  const [releaseModal, setReleaseModal] = useState(false)
  const [artifactModal, setArtifactModal] = useState<SrRelease | null>(null)
  const [syncBusyId, setSyncBusyId] = useState('')
  const [shaDraft, setShaDraft] = useState<Record<string, string>>({})
  const [shaSavingId, setShaSavingId] = useState('')
  const [upgradeModal, setUpgradeModal] = useState(false)
  const [upgradePrecheck, setUpgradePrecheck] = useState<{
    ok: boolean
    summary?: string
    items?: { name: string; ok: boolean; level?: string; message?: string }[]
  } | null>(null)
  const [upgradePrecheckBusy, setUpgradePrecheckBusy] = useState(false)
  const [upgradeBusy, setUpgradeBusy] = useState(false)
  const [configModal, setConfigModal] = useState(false)
  const [configPreviews, setConfigPreviews] = useState<{
    node_id: string; host: string; role: string; remote_path?: string
    source?: string; content?: string; editable?: string; has_draft?: boolean
    vars?: Record<string, unknown>
  }[]>([])
  const [configSelectedNodeId, setConfigSelectedNodeId] = useState<string>('')
  const [configNodeIds, setConfigNodeIds] = useState<string[]>([])
  const [configEditBody, setConfigEditBody] = useState('')
  const [configRestart, setConfigRestart] = useState(false)
  const [configBusy, setConfigBusy] = useState(false)
  const [configShowTpl, setConfigShowTpl] = useState(false)
  const [configFe, setConfigFe] = useState('')
  const [configBe, setConfigBe] = useState('')
  const [configTab, setConfigTab] = useState<'edit' | 'history'>('edit')
  const [configPatchPlan, setConfigPatchPlan] = useState<ConfPatchPlan | null>(null)
  const [changeConfRollback, setChangeConfRollback] = useState<SrChange | null>(null)
  const [tplFe, setTplFe] = useState('')
  const [tplBe, setTplBe] = useState('')
  const [tplBusy, setTplBusy] = useState(false)
  const [envOptions, setEnvOptions] = useState<{ value: string; label: string }[]>([])
  const [clusterForm] = Form.useForm()
  const [nodeForm] = Form.useForm()
  const [agentForm] = Form.useForm()
  const [deployForm] = Form.useForm()
  const [ossForm] = Form.useForm()
  const [releaseForm] = Form.useForm()
  const releaseJdkTouched = useRef(false)
  const [artifactForm] = Form.useForm()
  const [upgradeForm] = Form.useForm()
  const [ticketForm] = Form.useForm()
  const [ticketDraft, setTicketDraft] = useState<{
    action: string
    title: string
    summary: string
    clusterId: string
    payload: Record<string, unknown>
    nodeComponent?: string
  } | null>(null)
  const [ticketBusy, setTicketBusy] = useState(false)

  const submitSrTicket = async (
    clusterId: string,
    action: string,
    payload: Record<string, unknown>,
    assignedTo?: string,
    description?: string,
  ) => {
    const res = await api.post('/sr-cluster/tickets', {
      cluster_id: clusterId,
      action,
      assigned_to: assignedTo,
      description,
      payload,
    })
    const issue = res.data?.data?.issue
    const no = issue?.ticket_no || issue?.id || ''
    message.success(no ? `已提交审批工单 ${no}` : '已提交审批工单')
    if (issue?.id) {
      Modal.confirm({
        title: '工单已创建',
        content: `工单 ${no} 待审批通过后自动执行。是否打开工单详情？`,
        okText: '查看工单',
        cancelText: '关闭',
        onOk: () => navigate(`/ops/tickets/${issue.id}`),
      })
    }
    return issue
  }

  const loadTicketApprovers = async (targetNodes: SrNode[], form: typeof ticketForm) => {
    const ids = targetNodes.map((n) => resolveNodeHostInstanceId(n, agents, hosts)).filter(Boolean)
    const list = await ticketApprovers.load(ids)
    if (list.length > 0 && !form.getFieldValue('assigned_to')) form.setFieldsValue({ assigned_to: list[0] })
  }

  const runUpgradePrecheck = async (clusterId: string, rid: string) => {
    setUpgradePrecheckBusy(true)
    try {
      const pre = await api.post(`/sr-cluster/clusters/${clusterId}/upgrade/precheck`, { release_id: rid })
      setUpgradePrecheck(pre.data?.data || null)
    } catch (e: any) {
      setUpgradePrecheck({
        ok: false,
        summary: e?.response?.data?.message || e.message || '预检请求失败',
        items: [],
      })
    } finally {
      setUpgradePrecheckBusy(false)
    }
  }

  const openReupgradeByActualVersion = (ch: SrChange) => {
    if (!selected || selected.id !== ch.cluster_id) {
      message.warning('请先在上方打开该变更所属的集群')
      return
    }
    let rid = ''
    try {
      rid = String(JSON.parse(ch.payload_json || '{}')?.to_release_id || '')
    } catch { /* payload 非 JSON 时按空处理 */ }
    if (!rid) {
      message.error('该变更缺少目标版本（to_release_id）')
      return
    }
    upgradeForm.resetFields()
    upgradeForm.setFieldsValue({ release_id: rid })
    setUpgradePrecheck(null)
    setUpgradeModal(true)
    void loadTicketApprovers(nodes, upgradeForm)
    void runUpgradePrecheck(selected.id, rid)
  }

  const ticketApproverField = (
    <Form.Item
      name="assigned_to"
      label="审批人"
      rules={[{ required: true, message: '请选择审批人' }]}
      extra={ticketApprovers.loading ? '正在加载审批人…' : '按节点所在主机的审批人配置加载'}
    >
      <Select
        showSearch
        allowClear
        loading={ticketApprovers.loading}
        placeholder="选择审批人"
        options={ticketApprovers.approvers.map((a) => ({ value: a, label: a }))}
      />
    </Form.Item>
  )

  const openTicketDraft = (draft: NonNullable<typeof ticketDraft>, targetNodes: SrNode[]) => {
    ticketForm.resetFields()
    setTicketDraft(draft)
    void loadTicketApprovers(targetNodes, ticketForm)
  }

  function assignClusterToCurrentTenant(row: SrCluster, e?: React.MouseEvent) {
    e?.stopPropagation()
    if (!row.id || !currentTenantId) return
    const from = row.tenant_name || row.tenant_id || '其它租户'
    Modal.confirm({
      title: '划拨到当前租户',
      content: `将集群「${row.name}」（当前归属：${from}）划拨到「${currentTenantLabel}」？关联的资源实例、已登记库/账号与监控任务一并迁移。`,
      okText: '划拨',
      onOk: async () => {
        await api.post(`/sr-cluster/clusters/${row.id}/assign-tenant`, { tenant_id: currentTenantId })
        message.success('已划拨到 ' + currentTenantLabel)
        await loadClusters()
      },
    })
  }

  const loadClusters = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.get('/sr-cluster/clusters')
      setClusters(res.data?.data || [])
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message)
    } finally {
      setLoading(false)
    }
  }, [])
  const loadAgents = useCallback(async (): Promise<SrAgent[]> => {
    try {
      const res = await api.get('/sr-cluster/agents')
      const list: SrAgent[] = res.data?.data || []
      setAgents(list)
      try {
        const stRes = await api.get('/sr-cluster/agents/status')
        const arr: { id: string; status: string; message?: string }[] = stRes.data?.data || []
        const map: Record<string, { status: string; message?: string }> = {}
        arr.forEach((s) => { map[s.id] = { status: s.status, message: s.message } })
        setAgentStatus(map)
      } catch {
        /* 状态探测失败不影响列表 */
      }
      return list
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message)
      return []
    }
  }, [])

  /** 仅删平台登记；不停 FE/BE、不删 Agent、不删资源配置实例；可再「登记已有」 */
  function unregisterCluster(row: SrCluster) {
    Modal.confirm({
      title: `注销登记「${row.name}」？`,
      width: 520,
      content: (
        <div>
          <p style={{ marginBottom: 8 }}>将删除本页集群/节点拓扑、变更单与配置草稿。</p>
          <p style={{ marginBottom: 8, color: '#389e0d' }}>
            不会停止远程 FE/BE、不会删除 Node Agent、不会删除资源配置中的 StarRocks 实例与监控目标。
          </p>
          <p style={{ marginBottom: 0, color: '#666' }}>注销后可用「登记已有集群」按主机重新登记。</p>
        </div>
      ),
      okText: '确认注销',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api.delete(`/sr-cluster/clusters/${row.id}`)
        message.success('已注销登记（远程服务与 Agent 未改动）')
        if (selected?.id === row.id) {
          setSelected(null)
          setNodes([])
        }
        await loadClusters()
      },
    })
  }

  const reprobeClusterLayout = useCallback(async (cluster: SrCluster) => {
    try {
      const res = await api.post(`/sr-cluster/clusters/${cluster.id}/probe-layout`, { apply: true })
      const d = res.data?.data || {}
      const layout = d.layout || {}
      if (d.cluster) setSelected(d.cluster)
      if (d.nodes) setNodes(d.nodes)
      message.success(`已更新路径：FE=${layout.fe_software_base || d.cluster?.software_base || '—'} BE=${layout.be_software_base || d.cluster?.be_software_base || '—'}`)
      const warns: string[] = layout.warnings || []
      if (warns.length) message.warning(warns.join('；'), 8)
      await loadClusters()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '探测失败')
    }
  }, [loadClusters])

  const bindClusterNodeAgent = useCallback(async (list: 'fe_nodes' | 'be_nodes', index: number, hostRaw: string) => {
    const host = endpointHost(hostRaw) || String(hostRaw || '').trim()
    if (!host) return
    // 登记模式：仅匹配已有 Agent，不强制自动创建（可暂空稍后补）
    const allowCreate = clusterModalMode !== 'register'
    const id = await ensureAgentForHost({
      agents,
      host,
      port: 8091,
      createPath: '/sr-cluster/agents',
      namePrefix: 'sr',
      allowCreate,
      reloadAgents: loadAgents,
      silent: clusterModalMode === 'register',
    })
    if (id) {
      clusterForm.setFieldValue([list, index, 'agent_id'], id)
    }
  }, [agents, clusterForm, clusterModalMode, loadAgents])

  const resolveFePasswordFromAdmin = useCallback(async (opts?: { silent?: boolean }) => {
    const feNodes: { host?: string }[] = clusterForm.getFieldValue('fe_nodes') || []
    const hosts = feNodes.map((n) => endpointHost(n.host || '') || String(n.host || '').trim()).filter(Boolean)
    const task = String(monitorTask || clusterForm.getFieldValue('name') || '').trim()
    const environment = String(clusterForm.getFieldValue('environment') || '').trim()
    try {
      const { data } = await api.post('/feadmin/resolve-credential', {
        hosts,
        task_name: task,
        environment,
        domain: monitorDomain || undefined,
      })
      const d = data?.data || {}
      if (d.password) {
        clusterForm.setFieldsValue({
          fe_password: d.password,
          fe_user: d.username || 'root',
        })
        if (!opts?.silent) {
          message.success(`已从 FE 管理获取密码（${d.matched_by || '匹配'}）`)
        }
        return String(d.password)
      }
    } catch (e: any) {
      if (!opts?.silent) {
        message.warning(e?.response?.data?.message || '未从 FE 管理匹配到密码')
      }
    }
    return ''
  }, [clusterForm, monitorDomain, monitorTask])

  const probeRegisterLayout = useCallback(async (hosts: string[], password: string) => {
    if (!hosts.length) return
    try {
      const { data } = await api.post('/sr-cluster/probe-layout', {
        fe_host: hosts[0],
        fe_password: password,
        fe_user: clusterForm.getFieldValue('fe_user') || 'root',
        fe_query_port: 9030,
        hosts,
      })
      const layout = data?.data || {}
      const patch: Record<string, string> = {}
      if (layout.fe_software_base) patch.software_base = layout.fe_software_base
      if (layout.be_software_base) patch.be_software_base = layout.be_software_base
      if (layout.be_data_root) patch.data_root = layout.be_data_root
      if (Object.keys(patch).length) {
        clusterForm.setFieldsValue(patch)
        message.success(`已回填路径：FE=${patch.software_base || '—'} BE=${patch.be_software_base || '—'} 数据=${patch.data_root || '—'}`)
      }
      const warns: string[] = layout.warnings || []
      if (warns.length) message.warning(warns.join('；'), 8)
    } catch (e: any) {
      message.warning(e?.response?.data?.message || e.message || '目录探测失败（可手填）')
    }
  }, [clusterForm])

  const probeRegisterFERoles = useCallback(async () => {
    const feNodes: { host?: string }[] = clusterForm.getFieldValue('fe_nodes') || []
    const hosts = feNodes.map((n) => endpointHost(n.host || '') || String(n.host || '').trim()).filter(Boolean)
    if (!hosts.length) {
      message.warning('请先填写 FE Host')
      return
    }
    let password = String(clusterForm.getFieldValue('fe_password') || '')
    if (!password) {
      password = await resolveFePasswordFromAdmin({ silent: true })
    }
    setFeProbeBusy(true)
    try {
      const { data } = await api.post('/sr-cluster/probe-fe-roles', {
        fe_host: hosts[0],
        fe_password: password,
        fe_user: clusterForm.getFieldValue('fe_user') || 'root',
        fe_query_port: 9030,
      })
      const roles: { host?: string; role?: string }[] = data?.data || []
      const map: Record<string, string> = {}
      for (const r of roles) {
        const h = endpointHost(r.host || '') || String(r.host || '').trim()
        if (h && r.role) map[h] = String(r.role).toUpperCase()
      }
      setFeProbedRoles(map)
      const next = feNodes.map((n, i) => {
        const h = endpointHost(n.host || '') || String(n.host || '').trim()
        const role = map[h] || ''
        const isLeader = role === 'LEADER'
        return {
          ...n,
          is_primary_fe: isLeader || (i === 0 && !Object.values(map).includes('LEADER')),
          fe_role: role === 'OBSERVER' ? 'observer' : 'follower',
        }
      })
      // 保证仅一个首 FE
      let primarySeen = false
      for (const n of next) {
        if (n.is_primary_fe && !primarySeen) {
          primarySeen = true
        } else if (n.is_primary_fe) {
          n.is_primary_fe = false
        }
      }
      if (!primarySeen && next.length) next[0].is_primary_fe = true
      clusterForm.setFieldsValue({ fe_nodes: next })
      const leader = roles.find((r) => String(r.role).toUpperCase() === 'LEADER')
      message.success(leader ? `已探测：Leader=${leader.host}` : `已探测 ${roles.length} 个 FE 角色`)
      await probeRegisterLayout(hosts, password)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '探测失败')
    } finally {
      setFeProbeBusy(false)
    }
  }, [clusterForm, resolveFePasswordFromAdmin, probeRegisterLayout])
  const loadHosts = useCallback(async () => {
    try {
      const res = await api.get('/instances', { params: { db_type: 'host' } })
      setHosts(res.data?.data || [])
    } catch {
      setHosts([])
    }
  }, [])
  const loadDeploys = useCallback(async () => {
    try {
      const res = await api.get('/sr-cluster/agents/deploys', { params: { limit: 200 } })
      setDeploys(res.data?.data || [])
    } catch {
      setDeploys([])
    }
  }, [])
  const deployTracker = useAgentDeployTracker<SrAgentDeploy>({
    basePath: '/sr-cluster/agents/deploys',
    agentName: 'Node Agent',
    hostLabel: (id) => {
      const h = hosts.find((x) => x.id === id)
      return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
    },
    onShowLog: (row) => setDeployLog(row),
    onFinished: () => { void loadAgents() },
    refreshList: () => { void loadDeploys() },
  })
  const { trackFromList: trackDeploysFromList } = deployTracker
  useEffect(() => { trackDeploysFromList(deploys) }, [deploys, trackDeploysFromList])
  const deployingHostIds = useMemo(() => activeDeployHostIds(deploys), [deploys])
  const loadChanges = useCallback(async (clusterId?: string) => {
    const res = await api.get('/sr-cluster/changes', { params: { cluster_id: clusterId || undefined, limit: 50 } })
    setChanges(res.data?.data || [])
  }, [])
  const loadReleases = useCallback(async () => {
    const res = await api.get('/sr-cluster/releases')
    setReleases(res.data?.data || [])
  }, [])
  const saveArtifactSha = useCallback(async (release: SrRelease, art: SrReleaseArtifact) => {
    const raw = (shaDraft[art.id] ?? art.sha256 ?? '').trim().toLowerCase()
    if (!/^[a-f0-9]{64}$/.test(raw)) {
      Modal.warning({
        title: `${art.role} 的 sha256 无法保存`,
        width: 520,
        content: (
          <div>
            <p>须为 <strong>64 位</strong>十六进制，当前 <strong>{raw.length}</strong> 位（不完整或含非法字符）。</p>
            <p style={{ marginBottom: 8 }}>在本地对包文件计算后，把整行结果粘贴进输入框再点保存：</p>
            <pre style={{ background: '#f5f5f5', padding: 8, fontSize: 12 }}>
{`# macOS
shasum -a 256 StarRocks-3.2.10.${art.role}.tar.gz

# Linux
sha256sum StarRocks-3.2.10.${art.role}.tar.gz`}
            </pre>
            <p style={{ color: '#666', fontSize: 12 }}>只复制前面 64 个字符（不要带文件名）。</p>
          </div>
        ),
      })
      return
    }
    setShaSavingId(art.id)
    try {
      await api.post(`/sr-cluster/releases/${release.id}/artifacts`, {
        role: art.role,
        object_key: art.object_key,
        filename: art.filename || art.object_key?.split('/').pop(),
        size_bytes: art.size_bytes,
        format: art.format || 'tar.gz',
        sha256: raw,
      })
      message.success(`${art.role} sha256 已保存`)
      setShaDraft((prev) => {
        const next = { ...prev }
        delete next[art.id]
        return next
      })
      await loadReleases()
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '保存失败')
    } finally {
      setShaSavingId('')
    }
  }, [loadReleases, shaDraft])
  const syncReleaseFromOSS = useCallback(async (r: SrRelease) => {
    setSyncBusyId(r.id)
    try {
      const res = await api.post(`/sr-cluster/releases/${r.id}/artifacts/sync-from-oss`, null, { timeout: 120000 })
      const data = res.data?.data || {}
      const lines = [
        data.fe?.message,
        data.be?.message,
      ].filter(Boolean) as string[]
      const statuses = [data.fe?.status, data.be?.status]
      const okCount = statuses.filter((s) => s === 'ok' || s === 'hashing').length
      const hashing = statuses.some((s) => s === 'hashing')
      await loadReleases()
      if (artifactModal?.id === r.id) {
        const refreshed = (await api.get('/sr-cluster/releases')).data?.data as SrRelease[] | undefined
        const hit = (refreshed || []).find((x) => x.id === r.id)
        if (hit) setArtifactModal(hit)
      }
      Modal[okCount > 0 ? (hashing ? 'info' : 'success') : 'warning']({
        title: `从 OSS 同步 · ${r.version}`,
        content: (
          <div style={{ whiteSpace: 'pre-wrap' }}>
            {(lines.length ? lines : [data.message || '无结果']).map((t) => <div key={t}>{t}</div>)}
            {hashing && (
              <div style={{ marginTop: 8, color: '#666', fontSize: 12 }}>
                大包 sha256 在后台计算，约 1～数分钟后点刷新或重新展开版本行即可看到；也可在 OSS 上传同名 `.sha256` 侧车以跳过下载。
              </div>
            )}
            <div style={{ marginTop: 8, color: '#666', fontSize: 12 }}>
              支持扁平：{'{prefix}'}StarRocks-{r.version}.fe|be.tar.gz；或分层：{'{prefix}'}{r.version}/fe|be/
            </div>
          </div>
        ),
      })
      if (hashing) {
        window.setTimeout(() => { void loadReleases() }, 15000)
      }
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '同步失败')
    } finally {
      setSyncBusyId('')
    }
  }, [artifactModal?.id, loadReleases])
  const loadOSS = useCallback(async () => {
    const res = await api.get('/sr-cluster/oss')
    const d = res.data?.data || {}
    setOssBind(d)
    setOssOptions(res.data?.options || [])
    ossForm.setFieldsValue({
      oss_setting_id: d.oss_setting_id || undefined,
      prefix: d.prefix || 'starrocks/',
    })
  }, [ossForm])
  const loadEnvironments = useCallback(async () => {
    try {
      const res = await api.get('/environments')
      const list = res.data?.data || []
      let opts = list
        .map((e: { code?: string; name?: string }) => ({
          value: e.code || e.name || '',
          label: e.name && e.code && e.name !== e.code ? `${e.name} (${e.code})` : (e.name || e.code || ''),
        }))
        .filter((e: { value: string }) => e.value)
      if (!opts.length) {
        opts = [
          { value: 'test', label: '测试 (test)' },
          { value: 'prod', label: '生产 (prod)' },
        ]
      }
      setEnvOptions(opts)
    } catch {
      setEnvOptions([
        { value: 'test', label: '测试 (test)' },
        { value: 'prod', label: '生产 (prod)' },
      ])
    }
  }, [])
  const loadConfTemplates = useCallback(async () => {
    try {
      const res = await api.get('/sr-cluster/conf-templates')
      const list: { role: string; content?: string }[] = res.data?.data || []
      setTplFe(list.find((x) => x.role === 'fe')?.content || '')
      setTplBe(list.find((x) => x.role === 'be')?.content || '')
    } catch {
      /* ignore */
    }
  }, [])

  const openCluster = async (row: SrCluster) => {
    if (selected?.id === row.id) {
      setSelected(null)
      setNodes([])
      return
    }
    setSelected(row)
    setClusterInst(null)
    setClusterMon(null)
    await reloadClusterDetail(row.id, row)
  }

  const reloadClusterDetail = async (clusterId: string, fallback?: SrCluster) => {
    const res = await api.get(`/sr-cluster/clusters/${clusterId}`)
    const d = res.data?.data || {}
    setSelected(d.cluster || fallback || null)
    setNodes(d.nodes || [])
    setClusterInst(d.instance || null)
    setClusterMon(d.monitor_job || null)
  }

  const syncRegistry = async (clusterId: string) => {
    setRegistrySyncing(true)
    try {
      const res = await api.post(`/sr-cluster/clusters/${clusterId}/sync-registry`)
      const r = res.data?.data || {}
      const inst = r.instance as SrInstanceLink | undefined
      const mon = r.monitor_job as SrMonitorLink | undefined
      const warnings: string[] = r.warnings || []
      Modal[warnings.length ? 'warning' : 'success']({
        title: '同步到配置中心',
        width: 560,
        content: (
          <div style={{ lineHeight: 1.9 }}>
            <div>
              资源实例：
              {inst ? `${r.instance_created ? '已登记' : '已关联'} ${inst.name}（${inst.host}:${inst.port}）` : '未登记'}
            </div>
            <div>
              监控任务：
              {mon ? `${r.monitor_created ? '已注册' : '已同步'} ${mon.name}（FE ${mon.fe_count} / BE ${mon.be_count}）` : '无可同步的节点'}
            </div>
            {warnings.map((w) => <div key={w} style={{ color: '#d46b08' }}>{w}</div>)}
          </div>
        ),
      })
      await reloadClusterDetail(clusterId)
      void loadNodeLive(clusterId)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message)
    } finally {
      setRegistrySyncing(false)
    }
  }

  const loadNodeLive = useCallback(async (clusterId: string) => {
    nodeLiveReq.current = clusterId
    setNodeLiveLoading(true)
    try {
      const res = await api.get(`/sr-cluster/clusters/${clusterId}/live`, { silent: true })
      if (nodeLiveReq.current !== clusterId) return
      setNodeLive(res.data?.data?.nodes || {})
      setNodeLiveErr(res.data?.message || '')
      const removed: { host: string }[] = res.data?.data?.removed_nodes || []
      if (removed.length > 0) {
        message.info(`BE ${removed.map((n) => n.host).join('、')} 已完成在线缩容并移出集群，已移除节点登记`)
        void reloadClusterDetail(clusterId)
      }
    } catch (e: any) {
      if (nodeLiveReq.current !== clusterId) return
      setNodeLive({})
      setNodeLiveErr(e?.response?.data?.message || e.message || '请求失败')
    } finally {
      if (nodeLiveReq.current === clusterId) setNodeLiveLoading(false)
    }
  }, [])

  useEffect(() => {
    setNodeLive({})
    setNodeLiveErr('')
    if (!selected?.id) {
      nodeLiveReq.current = ''
      setNodeLiveLoading(false)
      return
    }
    void loadNodeLive(selected.id)
  }, [selected?.id, loadNodeLive])

  const pollChange = useCallback(async (id: string) => {
    const res = await api.get(`/sr-cluster/changes/${id}`)
    setChangeDetail(res.data?.data || null)
    return res.data?.data as SrChange
  }, [])

  // 任意入口打开变更时，分类子 Tab 跟随该变更，保证列表与详情一致
  useEffect(() => {
    if (changeDetail?.type) setChangeCat(changeCategoryOf(SR_CHANGE_CATEGORIES, changeDetail.type))
  }, [changeDetail?.id, changeDetail?.type])

  const selectChangeCategory = (key: ChangeCategoryKey) => {
    setChangeCat(key)
    const id = pickChangeInCategory(filterChangesByCategory(SR_CHANGE_CATEGORIES, changes, key))
    if (id) void pollChange(id)
    else setChangeDetail(null)
  }

  useEffect(() => {
    loadClusters(); loadAgents(); loadChanges(); loadReleases(); loadOSS(); loadHosts(); loadDeploys(); loadConfTemplates(); loadEnvironments()
  }, [loadClusters, loadAgents, loadChanges, loadReleases, loadOSS, loadHosts, loadDeploys, loadConfTemplates, loadEnvironments])

  useEffect(() => {
    if (!changeDetail?.id) return
    if (changeDetail.status !== 'running' && changeDetail.status !== 'pending') return
    const t = setInterval(() => {
      void pollChange(changeDetail.id).then((ch) => {
        if (ch && ch.status !== 'running' && ch.status !== 'pending') void loadChanges(selected?.id)
      })
    }, 2000)
    return () => clearInterval(t)
  }, [changeDetail?.id, changeDetail?.status, pollChange, loadChanges, selected?.id])

  // 工单详情「查看变更日志」跳转：?cluster=&change=
  const [searchParams] = useSearchParams()
  const urlChangeId = searchParams.get('change') || ''
  const urlClusterId = searchParams.get('cluster') || ''
  useEffect(() => {
    if (!urlChangeId) return
    setTab('changes')
    void pollChange(urlChangeId).catch(() => message.warning('变更单不存在或已删除'))
    void loadChanges(urlClusterId || undefined)
  }, [urlChangeId, urlClusterId, pollChange, loadChanges])

  // 进入变更页且未选中任何变更时，自动打开最近一个执行中 / 失败的变更
  useEffect(() => {
    if (tab !== 'changes' || changeDetail || urlChangeId || !changes.length) return
    const target = changes.find((c) => c.status === 'running' || c.status === 'pending')
      || changes.find((c) => c.status === 'failed')
    if (target) void pollChange(target.id)
  }, [tab, changes, changeDetail, urlChangeId, pollChange])

  const rollbackUpgradeNode = (ch: SrChange, step: SrChangeStep) => {
    const suffix = changeBackupSuffix(ch)
    Modal.confirm({
      title: '回滚该节点',
      width: 520,
      content: (
        <div>
          <p>将对「{step.title}」执行：</p>
          <ol style={{ paddingLeft: 18 }}>
            <li>停止服务</li>
            <li>把当前 bin/lib 移到 bak.{suffix || '<备份>'}.failed-时间戳/，并用升级前备份 bak.{suffix || '<备份>'}/bin、lib 还原</li>
            <li>启动旧版本并校验 SHOW PROC</li>
          </ol>
          <p style={{ color: '#666', fontSize: 12 }}>会生成一条「升级回滚」变更，可在本页查看日志；之后对原升级变更「重试」会重新升级该节点。</p>
        </div>
      ),
      okText: '确认回滚',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          const res = await api.post(`/sr-cluster/changes/${ch.id}/rollback-node`, { node_id: step.node_id })
          message.success('已提交回滚变更')
          const id = res.data?.data?.change_id
          if (id) await pollChange(id)
          loadChanges(selected?.id)
        } catch (e: any) {
          message.error(e?.response?.data?.message || e.message || '回滚失败')
        }
      },
    })
  }

  const afterChange = async (changeId?: string) => {
    setTab('changes')
    if (changeId) await pollChange(changeId)
    loadChanges(selected?.id)
    loadClusters()
    if (selected) openCluster(selected)
  }

  /** 打开变更页的指定分类，并展示该分类最近一次（失败 / 执行中优先）变更日志 */
  const showClusterChangeLogs = async (clusterId: string, category: ChangeCategoryKey) => {
    setTab('changes')
    setChangeCat(category)
    const res = await api.get('/sr-cluster/changes', { params: { cluster_id: clusterId, limit: 50 } })
    const list: SrChange[] = res.data?.data || []
    setChanges(list)
    const target = pickChangeInCategory(filterChangesByCategory(SR_CHANGE_CATEGORIES, list, category))
    if (target) {
      await pollChange(target)
      return
    }
    setChangeDetail(null)
    message.info(`暂无${changeCategoryLabel(SR_CHANGE_CATEGORIES, category)}`)
  }

  /** 将 failed（或已有节点的 draft）标为 ready；默认 FE 探活，不等于重新初始化 */
  const markClusterReady = (cluster: SrCluster) => {
    Modal.confirm({
      title: '标记集群为就绪？',
      width: 520,
      content: (
        <div>
          <p>
            仅修改纳管生命周期标签（
            <code>failed</code>
            {' '}
            →
            {' '}
            <code>ready</code>
            ），
            <strong>不会</strong>
            停服、装包或重新初始化。
          </p>
          <p style={{ color: '#666', marginBottom: 0 }}>
            请确认 FE 业务已正常，且已补齐 FE 密码（否则探活会失败）。若探活失败但业务确认正常，可在下一提示中选择强制标记。
          </p>
        </div>
      ),
      okText: '标记就绪',
      cancelText: '取消',
      onOk: async () => {
        try {
          const res = await api.post(`/sr-cluster/clusters/${cluster.id}/mark-ready`, {})
          const updated = res.data?.data as SrCluster | undefined
          message.success('已标记为就绪')
          await loadClusters()
          if (updated) {
            setSelected((prev) => (prev && prev.id === cluster.id ? { ...prev, ...updated, status: 'ready' } : prev))
          } else {
            setSelected((prev) => (prev && prev.id === cluster.id ? { ...prev, status: 'ready' } : prev))
          }
          void loadNodeLive(cluster.id)
        } catch (e: any) {
          const msg = e?.response?.data?.message || e.message || '标记失败'
          Modal.confirm({
            title: '标记就绪未成功',
            width: 560,
            content: (
              <div style={{ whiteSpace: 'pre-wrap' }}>
                <div>{msg}</div>
                <div style={{ marginTop: 12, color: '#666', fontSize: 12 }}>
                  常见原因：FE 密码未配置（Access denied / using password: NO）。请到实例管理或 FE 管理补密后重试；若确认集群业务已健康，可强制标记（跳过探活）。
                </div>
              </div>
            ),
            okText: '强制标记就绪',
            okButtonProps: { danger: true },
            cancelText: '取消',
            onOk: async () => {
              try {
                const res = await api.post(`/sr-cluster/clusters/${cluster.id}/mark-ready`, { force: true })
                const updated = res.data?.data as SrCluster | undefined
                message.success('已强制标记为就绪')
                await loadClusters()
                if (updated) {
                  setSelected((prev) => (prev && prev.id === cluster.id ? { ...prev, ...updated, status: 'ready' } : prev))
                } else {
                  setSelected((prev) => (prev && prev.id === cluster.id ? { ...prev, status: 'ready' } : prev))
                }
              } catch (e2: any) {
                message.error(e2?.response?.data?.message || e2.message || '强制标记失败')
                throw e2
              }
            },
          })
          throw e
        }
      },
    })
  }

  const startClusterInit = async (cluster: SrCluster) => {
    try {
      const res = await api.post(`/sr-cluster/clusters/${cluster.id}/init`)
      message.success('已提交初始化，正在打开变更日志')
      await afterChange(res.data?.data?.change_id)
    } catch (e: any) {
      const msg = e?.response?.data?.message || e.message || '初始化失败'
      const catalogHit = cluster.version
        ? releases.find((r) => r.version === cluster.version)
        : undefined
      const staleBind = !!(cluster.release_id && !releases.some((r) => r.id === cluster.release_id) && catalogHit)
      Modal.error({
        title: '集群初始化未启动',
        width: 560,
        content: (
          <div style={{ whiteSpace: 'pre-wrap' }}>
            <div>{msg}</div>
            <div style={{ marginTop: 12, color: '#666', fontSize: 12 }}>
              集群声明版本：{cluster.version || '—'}；release_id：{cluster.release_id || '—'}
              <br />
              {/不存在|未启用/.test(msg)
                ? '请到「设置 → 版本目录」确认目标版本仍在且已启用，或编辑集群重新选择版本（以 release_id 为准）。'
                : staleBind
                  ? `版本目录已有同名版本 ${catalogHit!.version}（新 ID ${catalogHit!.id}）。请重启后端后再点初始化（会按版本自动重绑）；也可编辑集群重新选择版本。`
                  : '请核对版本目录分包（含 64 位 sha256）。下方历史变更日志可能是旧失败记录，修好后请对失败变更点「重试」。'}
            </div>
          </div>
        ),
        okText: '查看变更并重试',
        onOk: () => {
          void showClusterChangeLogs(cluster.id, 'init')
        },
      })
    }
  }

  const startClusterReinit = (cluster: SrCluster) => {
    const soft = cluster.software_base || '/home/dorisdb_base'
    const data = cluster.data_root || '/data'
    openTicketDraft({
      action: 'cluster_reinit',
      title: '提交重新初始化审批',
      summary: `将对集群 ${cluster.name}：停服 → 备份 ${soft} / ${data} → 完整重装初始化。审批通过后自动执行。`,
      clusterId: cluster.id,
      payload: {},
    }, nodes)
  }

  const openConfigModal = async (focusNodeId?: string) => {
    if (!selected) return
    try {
      const [prevRes, cfgRes] = await Promise.all([
        api.get(`/sr-cluster/clusters/${selected.id}/configs/preview`),
        api.get(`/sr-cluster/clusters/${selected.id}/configs`),
      ])
      const list = prevRes.data?.data || []
      setConfigPreviews(list)
      const prefer = (focusNodeId && list.find((p: { node_id: string }) => p.node_id === focusNodeId)) || list[0]
      setConfigNodeIds(prefer?.node_id ? [prefer.node_id] : [])
      setConfigSelectedNodeId(prefer?.node_id || '')
      setConfigEditBody(prefer?.editable || '')
      const d = cfgRes.data?.data || {}
      setConfigFe(d.fe || '')
      setConfigBe(d.be || '')
      setConfigRestart(false)
      setConfigShowTpl(false)
      setConfigTab('edit')
      setConfigModal(true)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message)
    }
  }

  // 同一次下发只能勾选与当前编辑节点同角色的节点；切到另一角色时只保留该节点
  const configCurRole = configPreviews.find((p) => p.node_id === configSelectedNodeId)?.role || ''
  const selectConfigNode = (nodeId: string) => {
    const p = configPreviews.find((x) => x.node_id === nodeId)
    if (p && p.role !== configCurRole) setConfigNodeIds([nodeId])
    setConfigSelectedNodeId(nodeId)
    setConfigEditBody(p?.editable || '')
  }

  const openClusterModal = async (mode: 'create' | 'register') => {
    setClusterModalMode(mode)
    clusterForm.resetFields()
    setFeProbedRoles({})
    clusterForm.setFieldsValue({
      software_base: '/home/dorisdb_base',
      data_root: '/data',
      fe_user: 'root',
      init_now: false,
      fe_nodes: [{ host: '', agent_id: undefined, is_primary_fe: true, fe_role: 'follower' }],
      be_nodes: [{ host: '', agent_id: undefined }],
    })
    setMonitorTask(undefined)
    setMonitorDomain(undefined)
    if (mode === 'register') {
      try {
        const [feRes, beRes] = await Promise.all([
          api.get('/monitor-targets/preview', { params: { kind: 'starrocks-fe' } }),
          api.get('/monitor-targets/preview', { params: { kind: 'starrocks-be' } }),
        ])
        setFePreview(feRes.data?.data || [])
        setBePreview(beRes.data?.data || [])
      } catch {
        setFePreview([])
        setBePreview([])
      }
    }
    setClusterModal(true)
  }

  const monitorTaskOptions = useMemo(() => {
    const names = new Set<string>()
    ;[...fePreview, ...bePreview].forEach((p) => { if (p.task_name) names.add(p.task_name) })
    return Array.from(names).map((n) => ({ value: n, label: n }))
  }, [fePreview, bePreview])

  const monitorDomainOptions = useMemo(() => {
    const names = new Set<string>()
    ;[...fePreview, ...bePreview]
      .filter((p) => !monitorTask || p.task_name === monitorTask)
      .forEach((p) => { if (p.domain) names.add(p.domain) })
    return Array.from(names).map((n) => ({ value: n, label: n }))
  }, [fePreview, bePreview, monitorTask])

  const agentSelectOptions = useMemo(
    () => agents.map((a) => ({ value: a.id, label: `${a.name || a.id} (${a.base_url})` })),
    [agents],
  )

  const goNodeAgentTab = () => {
    setClusterModal(false)
    setTab('agents')
  }

  const nodeHostId = Form.useWatch('host_instance_id', nodeForm) as string | undefined
  const nodeHostInst = useMemo(() => hosts.find((h) => h.id === nodeHostId) || null, [hosts, nodeHostId])
  const nodeAgentOptions = useMemo(
    () => agents
      .filter((a) => agentMatchesHost(a, nodeHostInst))
      .map((a) => ({ value: a.id, label: `${a.name || a.id} (${a.base_url})` })),
    [agents, nodeHostInst],
  )

  const onNodeHostChange = async (hostId?: string) => {
    const inst = hosts.find((h) => h.id === hostId) || null
    const matched = agents.filter((a) => agentMatchesHost(a, inst))
    nodeForm.setFieldsValue({
      host: hostPrimaryAddr(inst),
      agent_id: matched.length === 1 ? matched[0].id : undefined,
      assigned_to: undefined,
    })
    const list = await nodeApprovers.load(hostId)
    if (nodeForm.getFieldValue('host_instance_id') === hostId && list.length > 0) {
      nodeForm.setFieldsValue({ assigned_to: list[0] })
    }
  }

  const buildAddNodePayload = (v: Record<string, any>): Record<string, unknown> => {
    const payload: Record<string, unknown> = {
      component: v.component,
      host: v.host,
      host_instance_id: v.host_instance_id,
      agent_id: v.agent_id,
      software_base: v.software_base,
    }
    if (v.component === 'fe') {
      payload.is_primary_fe = v.is_primary_fe
      payload.fe_role = v.fe_role
    } else {
      payload.data_root = v.data_root
    }
    return payload
  }

  const runNodePrecheck = async () => {
    if (!selected) return
    const v = await nodeForm.validateFields()
    setNodePrecheckBusy(true)
    try {
      const res = await api.post(`/sr-cluster/clusters/${selected.id}/nodes/precheck`, {
        ...buildAddNodePayload(v),
        assigned_to: v.assigned_to,
      })
      setNodePrecheck(res.data?.data || null)
    } catch (e: any) {
      setNodePrecheck(null)
      message.error(e?.response?.data?.message || e.message || '基础校验失败')
    } finally {
      setNodePrecheckBusy(false)
    }
  }

  const submitAddNode = async () => {
    if (!selected || !nodePrecheck?.ok) return
    const v = await nodeForm.validateFields()
    setNodeSubmitBusy(true)
    try {
      await submitSrTicket(selected.id, 'add_node', buildAddNodePayload(v), v.assigned_to, v.description)
      setNodeModal(false)
    } catch (e: any) {
      message.error(e?.response?.data?.message || e.message || '提交失败')
    } finally {
      setNodeSubmitBusy(false)
    }
  }

  const openQuickAgentRegister = (list: 'fe_nodes' | 'be_nodes', index: number) => {
    const host = String(clusterForm.getFieldValue([list, index, 'host']) || '').trim()
    const h = endpointHost(host) || host
    if (!h) {
      message.warning('请先填写该行 Host')
      return
    }
    setAgentBindTarget({ list, index })
    agentForm.resetFields()
    agentForm.setFieldsValue({
      name: `sr-${h}`,
      base_url: `http://${h}:8091`,
    })
    setAgentModal(true)
  }

  const applyMonitorImport = () => {
    if (!monitorTask) {
      message.warning('请选择监控任务')
      return
    }
    const match = (p: MonitorPreview) => p.task_name === monitorTask && (!monitorDomain || p.domain === monitorDomain)
    const feHits = fePreview.filter(match)
    const beHits = bePreview.filter(match)
    const feEps = feHits.flatMap((p) => p.endpoints || [])
    const beEps = beHits.flatMap((p) => p.endpoints || [])
    const feHosts = Array.from(new Set(feEps.map(endpointHost).filter(Boolean)))
    const beHosts = Array.from(new Set(beEps.map(endpointHost).filter(Boolean)))
    if (!feHosts.length && !beHosts.length) {
      message.warning('该监控任务下无 FE/BE 端点，请手工填写或检查监控目标')
      return
    }
    const hit = [...feHits, ...beHits].find((p) => p.version || p.labels?.version || p.labels?.sr_version)
      || [...feHits, ...beHits][0]
    const envHit = hit?.env
    const ver = (hit?.version
      || hit?.labels?.version
      || hit?.labels?.sr_version
      || hit?.labels?.starrocks_version
      || '').trim()
    const matchedRelease = ver
      ? releases.find((r) => r.enabled !== false && r.version === ver)
      : undefined
    const feNodes = feHosts.length
      ? feHosts.map((host, i) => ({
        host,
        agent_id: matchAgentForHost(agents, host)?.id,
        is_primary_fe: i === 0,
        fe_role: 'follower',
      }))
      : [{ host: '', agent_id: undefined, is_primary_fe: true, fe_role: 'follower' }]
    const beNodes = beHosts.length
      ? beHosts.map((host) => ({
        host,
        agent_id: matchAgentForHost(agents, host)?.id,
      }))
      : [{ host: '', agent_id: undefined }]
    const autoBound = [...feNodes, ...beNodes].filter((n: { agent_id?: string }) => n.agent_id).length
    clusterForm.setFieldsValue({
      environment: envHit || clusterForm.getFieldValue('environment'),
      name: clusterForm.getFieldValue('name') || monitorTask,
      version: ver || clusterForm.getFieldValue('version'),
      release_id: matchedRelease?.id || undefined,
      fe_nodes: feNodes,
      be_nodes: beNodes,
    })
    const parts = [`已导入 FE ${feHosts.length} / BE ${beHosts.length}`]
    if (ver) parts.push(`版本 ${ver}${matchedRelease ? '（已匹配制品目录）' : ''}`)
    else parts.push('监控目标无 version 标签，请手工填写版本')
    if (autoBound > 0) parts.push(`已按 host 自动绑定 ${autoBound} 个 Node Agent`)
    else parts.push('Agent 可暂空，登记后到 Node Agent Tab 批量下发再补绑')
    message.success(parts.join('；'))
    void resolveFePasswordFromAdmin({ silent: true }).then((pwd) => {
      if (pwd) message.info('已从 FE 管理自动填入密码', 3)
    })
  }

  return (
    <div className="sr-cluster-page">
      <Breadcrumb
        style={{ marginBottom: 12 }}
        items={[
          { title: <Link to="/ops/cluster-deploy">集群部署</Link> },
          { title: 'StarRocks' },
        ]}
      />
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="StarRocks 工作台"
        description="版本包存 OSS，Node Agent 按 FE/BE 角色签名下载到 software_base/{fe|be}。初始化/扩容/升级/重启/下线走变更单步骤。"
      />
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'clusters',
            label: '集群',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Space>
                  <Button type="primary" onClick={() => openClusterModal('create')}>新建集群</Button>
                  <Button onClick={() => openClusterModal('register')}>登记已有集群</Button>
                  <Button onClick={loadClusters}>刷新</Button>
                </Space>
                {impersonating ? (
                  <Alert
                    type="info"
                    showIcon
                    style={{ marginBottom: 8 }}
                    message={`审计中：列表含全平台集群。其它租户资源可划拨到「${currentTenantLabel}」，该租户成员即可运维。`}
                  />
                ) : null}
                <Table
                  rowKey="id"
                  loading={loading}
                  dataSource={clusters}
                  pagination={false}
                  onRow={(row) => ({ onClick: () => openCluster(row), style: { cursor: 'pointer' } })}
                  columns={[
                    {
                      title: '名称',
                      dataIndex: 'name',
                      render: (name: string, row: SrCluster) => (
                        <Space size={6} wrap>
                          <Button
                            type="link"
                            style={{ padding: 0, height: 'auto', fontWeight: 500 }}
                            onClick={(e) => { e.stopPropagation(); void openCluster(row) }}
                          >
                            {name}
                            <span style={{ marginLeft: 4, opacity: 0.55, fontSize: 12 }}>
                              {selected?.id === row.id ? '收起' : '详情'}
                            </span>
                          </Button>
                          <Tag color={row.source === 'register' ? 'blue' : 'default'}>
                            {row.source === 'register' ? '登记' : '新建'}
                          </Tag>
                        </Space>
                      ),
                    },
                    ...(impersonating
                      ? [{
                          title: '所属租户',
                          width: 140,
                          render: (_: unknown, row: SrCluster) => {
                            const same = row.tenant_id && currentTenantId && row.tenant_id === currentTenantId
                            const label = row.tenant_name || row.tenant_id || '—'
                            return same ? <Tag color="green">{label}</Tag> : <Tag color="orange">{label}</Tag>
                          },
                        } as ColumnType<SrCluster>]
                      : []),
                    { title: '环境', dataIndex: 'environment' },
                    { title: '版本', dataIndex: 'version' },
                    { title: '状态', dataIndex: 'status', render: (s: string) => renderClusterStatus(s) },
                    { title: 'FE', dataIndex: 'fe_host' },
                    {
                      title: '操作',
                      width: impersonating ? 220 : 100,
                      render: (_: unknown, row: SrCluster) => (
                        <Space size={0} onClick={(e) => e.stopPropagation()}>
                          {impersonating && row.tenant_id && row.tenant_id !== currentTenantId ? (
                            <Auth urn={Perm.tenants.impersonate}>
                              <Button type="link" onClick={(e) => assignClusterToCurrentTenant(row, e)}>
                                划拨到当前租户
                              </Button>
                            </Auth>
                          ) : null}
                          <Button type="link" danger onClick={(e) => { e.stopPropagation(); void unregisterCluster(row) }}>
                            注销登记
                          </Button>
                        </Space>
                      ),
                    },
                  ]}
                />
                {selected && (
                  <Card
                    title={`集群 · ${selected.name}`}
                    extra={(
                      <Space wrap onClick={(e) => e.stopPropagation()}>
                        <Button onClick={() => {
                          nodeForm.resetFields()
                          nodeForm.setFieldsValue({
                            component: 'be',
                            is_primary_fe: false,
                            fe_role: 'follower',
                            software_base: selected?.software_base || '/home/dorisdb_base',
                            data_root: selected?.data_root || '/data',
                          })
                          setNodePrecheck(null)
                          void nodeApprovers.load(undefined)
                          void loadHosts()
                          setNodeModal(true)
                        }}
                        >
                          添加节点
                        </Button>
                        {selected.status !== 'ready' && selected.status !== 'initializing' && (
                          <Button type="primary" onClick={() => { void startClusterInit(selected) }}>
                            集群初始化
                          </Button>
                        )}
                        {(selected.status === 'failed' || (selected.status === 'draft' && nodes.length > 0)) && (
                          <Tooltip title="仅改纳管标签为 ready，不停服/不装包；默认先探活 FE。启停失败不会导致「初始化失败」。">
                            <Button onClick={() => { markClusterReady(selected) }}>
                              标记就绪
                            </Button>
                          </Tooltip>
                        )}
                        {(selected.status === 'ready' || selected.status === 'failed' || selected.status === 'initializing') && (
                          <Button danger onClick={() => { startClusterReinit(selected) }}>
                            重新初始化
                          </Button>
                        )}
                        <Button onClick={() => {
                          upgradeForm.resetFields()
                          setUpgradePrecheck(null)
                          setUpgradeModal(true)
                          void loadTicketApprovers(nodes, upgradeForm)
                        }}
                        >
                          升级
                        </Button>
                        <Button type="primary" onClick={() => { void openConfigModal() }}>配置</Button>
                        <Tooltip title="登记/关联「资源配置 → 实例管理」中的 StarRocks 实例，并同步「配置中心 → 监控目标注册」">
                          <Button loading={registrySyncing} onClick={() => { void syncRegistry(selected.id) }}>同步到配置中心</Button>
                        </Tooltip>
                        <Tooltip title="从 FE CONFIG / BE /varz 重新发现并写回 FE/BE software_base 与数据目录">
                          <Button onClick={() => { void reprobeClusterLayout(selected) }}>重新探测目录</Button>
                        </Tooltip>
                        <Button danger onClick={() => { void unregisterCluster(selected) }}>注销登记</Button>
                        <Button type="link" onClick={() => { setSelected(null); setNodes([]) }}>关闭</Button>
                      </Space>
                    )}
                  >
                    <Descriptions size="small" column={2}>
                      <Descriptions.Item label="版本">{selected.version} ({selected.release_id || '-'})</Descriptions.Item>
                      <Descriptions.Item label="状态">
                        <Space size={6}>
                          {renderClusterStatus(selected.status)}
                          <Tag color={selected.source === 'register' ? 'blue' : 'default'}>
                            {selected.source === 'register' ? '登记' : '新建'}
                          </Tag>
                        </Space>
                      </Descriptions.Item>
                      <Descriptions.Item label="环境">{selected.environment}</Descriptions.Item>
                      <Descriptions.Item label="FE software_base">{selected.software_base}</Descriptions.Item>
                      <Descriptions.Item label="BE software_base">{selected.be_software_base || selected.software_base || '—'}</Descriptions.Item>
                      <Descriptions.Item label="BE 数据目录">{selected.data_root || '—'}</Descriptions.Item>
                      <Descriptions.Item label="资源实例">
                        {clusterInst ? (
                          <Space size={4} wrap>
                            <Link to="/config/instances?db_type=starrocks">{clusterInst.name}</Link>
                            <span style={{ color: '#999' }}>{clusterInst.host}:{clusterInst.port}</span>
                            {clusterInst.password_set
                              ? <Tag color="success">已设密码</Tag>
                              : <Tooltip title="实例未设密码时，连接 FE 使用集群自身保存的密码（初装为空）"><Tag>未设密码</Tag></Tooltip>}
                            <Tag>{clusterInst.matched_by === 'tag' ? '标签关联' : '按主机匹配'}</Tag>
                          </Space>
                        ) : <span style={{ color: '#999' }}>未登记</span>}
                      </Descriptions.Item>
                      <Descriptions.Item label="监控任务">
                        {clusterMon ? (
                          <Space size={4} wrap>
                            <Link to="/config/monitor-targets">{clusterMon.name}</Link>
                            <span style={{ color: '#999' }}>
                              采集点 {clusterMon.collector || '—'} · FE {clusterMon.fe_count} / BE {clusterMon.be_count} 个目标
                            </span>
                            {(clusterMon.missing || []).length > 0 && (
                              <Tooltip title="平台无法推导，请到「配置中心 → 监控目标注册」手动补充">
                                <Tag color="warning">待补充：{(clusterMon.missing || []).join('、')}</Tag>
                              </Tooltip>
                            )}
                          </Space>
                        ) : <span style={{ color: '#999' }}>未注册</span>}
                      </Descriptions.Item>
                    </Descriptions>
                    {(() => {
                      const rid = selected.release_id || ''
                      const ver = selected.version || ''
                      const idOk = !rid || releases.some((r) => r.id === rid)
                      const byVer = ver ? releases.find((r) => r.version === ver) : undefined
                      if (idOk || !byVer) return null
                      return (
                        <Alert
                          type="warning"
                          showIcon
                          style={{ marginTop: 12 }}
                          message="制品绑定已失效"
                          description={`集群 release_id=${rid} 在版本目录中不存在，但同名版本 ${ver} 已登记（当前 ID ${byVer.id}）。再次「集群初始化」将按版本名自动重绑；也可编辑集群重新选择版本。`}
                        />
                      )
                    })()}
                    {nodeLiveErr && !nodeLiveLoading && (
                      <Alert
                        type="warning"
                        showIcon
                        style={{ marginTop: 12 }}
                        message={`无法读取 FE 实时状态：${nodeLiveErr}`}
                        description={(
                          <>
                            角色显示为登记值；升级、扩缩容等需要连接 FE 的操作也会失败。请在
                            {' '}
                            <Link to="/config/instances?db_type=starrocks">资源配置 → 实例管理 → StarRocks</Link>
                            {' '}
                            中维护该集群的实例密码
                            {clusterInst ? `（当前关联 ${clusterInst.name}）` : '，或点「同步到配置中心」自动登记后补填密码'}
                            。
                          </>
                        )}
                      />
                    )}
                    {(() => {
                      const feNodes = nodes.filter((n) => n.component === 'fe')
                      const beNodes = nodes.filter((n) => n.component !== 'fe')
                      const feRoleCol: ColumnType<SrNode> = {
                        title: '角色',
                        dataIndex: 'fe_role',
                        width: 120,
                        render: (v: string, row: SrNode) => {
                          const live = nodeLive[row.id]
                          const registered = (v || 'follower').toLowerCase()
                          const role = (live?.role || '').toUpperCase()
                          if (!role) {
                            const why = nodeLiveLoading
                              ? '正在读取实时角色'
                              : `未能读取实时角色${nodeLiveErr ? `（${nodeLiveErr}）` : '（该节点未出现在 SHOW PROC \'/frontends\' 中）'}，显示为登记值`
                            return (
                              <Tooltip title={why}>
                                <span style={{ color: '#999' }}>{registered}</span>
                              </Tooltip>
                            )
                          }
                          const roleColor: Record<string, string> = { LEADER: 'gold', FOLLOWER: 'blue', OBSERVER: 'default' }
                          const liveRoleTag = <Tag color={roleColor[role] || 'default'}>{role}</Tag>
                          const registeredMismatch = role !== 'LEADER' && role.toLowerCase() !== registered
                          return registeredMismatch
                            ? <Tooltip title={`登记为 ${registered}`}>{liveRoleTag}</Tooltip>
                            : liveRoleTag
                        },
                      }
                      const hostCol: ColumnType<SrNode> = {
                        title: 'Host',
                        dataIndex: 'host',
                        width: 190,
                        render: (v: string, row: SrNode) => (
                          <Space size={4}>
                            <Tooltip title={`软件路径：${row.software_path || '—'}`}>
                              <span>{v}</span>
                            </Tooltip>
                            {row.is_primary_fe && (
                              <Tooltip title="集群初始化时第一个启动的 FE，不代表当前 Leader">
                                <Tag>首FE</Tag>
                              </Tooltip>
                            )}
                          </Space>
                        ),
                      }
                      const journalCol: ColumnType<SrNode> = {
                        title: <Tooltip title="已回放的元数据日志 ID（ReplayedJournalId）；Follower/Observer 明显小于 Leader 说明元数据回放落后">ReplayedJournalId</Tooltip>,
                        key: 'replayed_journal_id',
                        width: 150,
                        render: (_: unknown, row: SrNode) => nodeLive[row.id]?.replayed_journal_id || '—',
                      }
                      const heartbeatCol: ColumnType<SrNode> = {
                        title: 'LastHeartbeat',
                        key: 'last_heartbeat',
                        width: 170,
                        render: (_: unknown, row: SrNode) => nodeLive[row.id]?.last_heartbeat || '—',
                      }
                      const startTimeCol: ColumnType<SrNode> = {
                        title: 'StartTime',
                        key: 'start_time',
                        width: 170,
                        render: (_: unknown, row: SrNode) => nodeLive[row.id]?.start_time || '—',
                      }
                      const dataRootCol: ColumnType<SrNode> = {
                        title: <Tooltip title="BE storage_root_path 或数据盘挂载点">数据目录</Tooltip>,
                        dataIndex: 'data_root',
                        ellipsis: true,
                        render: (v: string) => v || selected?.data_root || '/data',
                      }
                      const cpuCol: ColumnType<SrNode> = {
                        title: 'CPU核数',
                        key: 'cpu_cores',
                        width: 80,
                        render: (_: unknown, row: SrNode) => nodeLive[row.id]?.cpu_cores || '—',
                      }
                      const tabletCol: ColumnType<SrNode> = {
                        title: <Tooltip title="该 BE 上的 tablet 数量（TabletNum），来自 SHOW PROC '/backends'">Tablet数</Tooltip>,
                        key: 'tablet_num',
                        width: 90,
                        render: (_: unknown, row: SrNode) => nodeLive[row.id]?.tablet_num || '—',
                      }
                      const storageCol: ColumnType<SrNode> = {
                        title: <Tooltip title="数据目录所在磁盘的总量与使用率（含非 StarRocks 文件），来自 SHOW PROC '/backends'">存储</Tooltip>,
                        key: 'storage',
                        render: (_: unknown, row: SrNode) => {
                          const live = nodeLive[row.id]
                          if (!live?.total_capacity) return '—'
                          const pct = (live.used_pct || '').replace(/\s+/g, '')
                          const pctNum = parseFloat(pct)
                          const color = pctNum >= 95 ? '#cf1322' : pctNum >= 85 ? '#d46b08' : undefined
                          return (
                            <Tooltip title={`StarRocks 数据 ${live.data_used || '—'}，可用 ${live.avail_capacity || '—'}`}>
                              <span style={{ whiteSpace: 'nowrap' }}>
                                {live.total_capacity}
                                {pct ? <>（已用 <span style={{ color }}>{pct}</span>）</> : ''}
                              </span>
                            </Tooltip>
                          )
                        },
                      }
                      const statusCol: ColumnType<SrNode> = {
                        title: '状态',
                        dataIndex: 'status',
                        width: 170,
                        render: (s: string, row: SrNode) => {
                          const live = nodeLive[row.id]
                          if (live?.decommissioned || s === 'decommissioning') {
                            const tablets = live?.tablet_num
                            return (
                              <Space size={4}>
                                <Tooltip title="SystemDecommissioned=true；tablet 迁移完成后自动移出集群">
                                  <Tag color="orange">下线中</Tag>
                                </Tooltip>
                                {tablets && <span style={{ color: '#999', fontSize: 12 }}>剩余 tablet {tablets}</span>}
                              </Space>
                            )
                          }
                          // 有 SHOW PROC 实时数据时以 Alive 为准，避免库里 stopped 误显「已停止」
                          if (live?.alive != null && live.alive !== '') {
                            const alive = String(live.alive).toLowerCase() === 'true'
                            return (
                              <Tooltip title={`SHOW PROC Alive=${live.alive}；库状态=${s || '—'}`}>
                                <Tag color={alive ? 'success' : 'default'}>{alive ? '运行中' : '已停止'}</Tag>
                              </Tooltip>
                            )
                          }
                          const map: Record<string, { color: string; text: string }> = {
                            ready: { color: 'success', text: '运行中' },
                            stopped: { color: 'default', text: '已停止' },
                            pending: { color: 'processing', text: '待加入' },
                            failed: { color: 'error', text: '失败' },
                          }
                          const m = map[s] || { color: 'default', text: s || '—' }
                          return <Tag color={m.color}>{m.text}</Tag>
                        },
                      }
                      const actionCol: ColumnType<SrNode> = {
                          title: '操作',
                          width: 180,
                          render: (_: unknown, row: SrNode) => {
                            const live = nodeLive[row.id]
                            if (row.component === 'be' && live?.decommissioned) {
                              return (
                                <Space size="small" onClick={(e) => e.stopPropagation()}>
                                  <Button size="small" type="link" onClick={() => {
                                    if (!selected) return
                                    openTicketDraft({
                                      action: 'restore_node',
                                      title: '提交恢复节点审批',
                                      summary: `将恢复下线中的 BE ${row.host}（集群 ${selected.name}）：审批通过后执行 CANCEL DECOMMISSION BACKEND "${row.host}:<heartbeat_port>"（BE 进程已停止则先启动），确认 SystemDecommissioned=false 且 Alive 后状态恢复为运行中。在线缩容一旦完成（节点已移出 SHOW PROC '/backends'）则无法恢复。`,
                                      clusterId: selected.id,
                                      payload: { node_id: row.id },
                                      nodeComponent: row.component,
                                    }, [row])
                                  }}
                                  >
                                    恢复
                                  </Button>
                                </Space>
                              )
                            }
                            if (row.status === 'decommissioning') {
                              return (
                                <Tooltip title={nodeLiveErr ? `未能读取实时状态（${nodeLiveErr}），点「刷新」后可恢复` : '下线中；迁移完成后自动移出集群'}>
                                  <span style={{ color: '#999' }}>下线中</span>
                                </Tooltip>
                              )
                            }
                            return (
                            <Space size="small" onClick={(e) => e.stopPropagation()}>
                              {(row.status === 'pending' || row.status === 'failed') && (
                                <Button size="small" type="link" onClick={() => {
                                  if (!selected) return
                                  openTicketDraft({
                                    action: 'add_node',
                                    title: '提交加入节点审批',
                                    summary: `将节点 ${row.component}@${row.host} 加入集群 ${selected.name}。审批通过后自动执行扩容编排。`,
                                    clusterId: selected.id,
                                    payload: { node_id: row.id },
                                  }, [row])
                                }}
                                >
                                  加入
                                </Button>
                              )}
                              <Button
                                size="small"
                                type="link"
                                danger={(() => {
                                  const live = nodeLive[row.id]
                                  if (live?.alive != null && live.alive !== '') {
                                    return String(live.alive).toLowerCase() === 'true'
                                  }
                                  return row.status === 'ready'
                                })()}
                                onClick={async () => {
                                  if (!selected) return
                                  const live = nodeLive[row.id]
                                  const running = (live?.alive != null && live.alive !== '')
                                    ? String(live.alive).toLowerCase() === 'true'
                                    : row.status === 'ready'
                                  try {
                                    const res = await api.post(
                                      `/sr-cluster/clusters/${selected.id}/nodes/${row.id}/${running ? 'stop' : 'start'}`,
                                    )
                                    message.success(running ? '已提交停止' : '已提交启动')
                                    afterChange(res.data?.data?.change_id)
                                  } catch (e: any) {
                                    message.error(e?.response?.data?.message || e.message || (running ? '停止失败' : '启动失败'))
                                  }
                                }}
                              >
                                {(() => {
                                  const live = nodeLive[row.id]
                                  if (live?.alive != null && live.alive !== '') {
                                    return String(live.alive).toLowerCase() === 'true' ? '停止' : '启动'
                                  }
                                  return row.status === 'ready' ? '停止' : '启动'
                                })()}
                              </Button>
                              <Button size="small" type="link" onClick={async () => {
                                try {
                                  const res = await api.post(`/sr-cluster/clusters/${selected.id}/nodes/${row.id}/restart`)
                                  afterChange(res.data?.data?.change_id)
                                } catch (e: any) {
                                  message.error(e?.response?.data?.message || e.message || '重启失败')
                                }
                              }}
                              >
                                重启
                              </Button>
                              <Button size="small" type="link" danger onClick={() => {
                                if (!selected) return
                                openTicketDraft({
                                  action: 'remove_node',
                                  title: '提交下线节点审批',
                                  summary: row.component === 'be'
                                    ? `将在线缩容 BE ${row.host}（集群 ${selected.name}）：审批通过后执行 ALTER SYSTEM DECOMMISSION BACKEND，BE 进程不停止；tablet 迁移完成、节点移出 SHOW PROC '/backends' 后平台自动删除登记，迁移期间可走工单恢复。`
                                    : `将下线节点 ${row.component}@${row.host}（集群 ${selected.name}）。审批通过后自动执行。`,
                                  clusterId: selected.id,
                                  payload: { node_id: row.id, force_drop: false },
                                  nodeComponent: row.component,
                                }, [row])
                              }}
                              >
                                下线
                              </Button>
                            </Space>
                            )
                          },
                      }
                      const groupTitle = (label: string, count: number) => (
                        <span style={{ fontWeight: 500 }}>
                          {label}
                          <span style={{ marginLeft: 6, color: '#999', fontWeight: 400 }}>{count}</span>
                        </span>
                      )
                      return (
                        <>
                          <div style={{ marginTop: 16, marginBottom: 8, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                            {groupTitle('Frontend（FE）', feNodes.length)}
                            <Button size="small" loading={nodeLiveLoading} onClick={() => { void loadNodeLive(selected.id) }}>
                              刷新
                            </Button>
                          </div>
                          <Table
                            rowKey="id"
                            size="small"
                            dataSource={feNodes}
                            pagination={false}
                            locale={{ emptyText: '暂无 FE 节点' }}
                            columns={[feRoleCol, hostCol, journalCol, heartbeatCol, startTimeCol, statusCol, actionCol]}
                          />
                          <div style={{ marginTop: 20, marginBottom: 8 }}>
                            {groupTitle('Backend（BE）', beNodes.length)}
                          </div>
                          <Table
                            key={selected.id}
                            rowKey="id"
                            size="small"
                            dataSource={beNodes}
                            pagination={{
                              defaultPageSize: 20,
                              pageSizeOptions: [20, 50, 100],
                              showSizeChanger: true,
                              showTotal: (total) => `共 ${total} 个 BE`,
                            }}
                            locale={{ emptyText: '暂无 BE 节点' }}
                            columns={[hostCol, dataRootCol, cpuCol, storageCol, tabletCol, statusCol, actionCol]}
                          />
                        </>
                      )
                    })()}
                  </Card>
                )}
              </Space>
            ),
          },
          {
            key: 'changes',
            label: '变更',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Button onClick={() => loadChanges(selected?.id)}>刷新</Button>
                <ChangeCategoryTabs
                  categories={SR_CHANGE_CATEGORIES}
                  changes={changes}
                  active={changeCat}
                  onChange={selectChangeCategory}
                />
                <Table
                  rowKey="id"
                  dataSource={filterChangesByCategory(SR_CHANGE_CATEGORIES, changes, changeCat)}
                  locale={{ emptyText: `暂无${changeCategoryLabel(SR_CHANGE_CATEGORIES, changeCat)}` }}
                  pagination={false}
                  onRow={(row) => ({
                    onClick: () => { void pollChange(row.id) },
                    style: { cursor: 'pointer', background: changeDetail?.id === row.id ? 'var(--ant-color-primary-bg, #e6f4ff)' : undefined },
                  })}
                  columns={[
                    { title: '类型', dataIndex: 'type', width: 110, render: (t: string) => CHANGE_TYPE_LABEL[t] || t },
                    { title: '状态', dataIndex: 'status', width: 100, render: (s: string) => <Tag color={statusColor[s]}>{s}</Tag> },
                    {
                      title: '摘要',
                      dataIndex: 'log_text',
                      ellipsis: { showTitle: true },
                      render: (t: string) => lastLogLine(t) || '—',
                    },
                    { title: '发起人', dataIndex: 'created_by', width: 100 },
                    { title: '开始时间', dataIndex: 'created_at', width: 140, render: (v: string) => fmtClock(v) },
                    {
                      title: '耗时',
                      width: 90,
                      render: (_: unknown, row: SrChange) => fmtDuration(
                        row.created_at,
                        row.status === 'running' || row.status === 'pending' ? undefined : row.updated_at,
                      ),
                    },
                    {
                      title: '操作',
                      render: (_: unknown, row: SrChange) => (
                        <Space onClick={(e) => e.stopPropagation()}>
                          {(row.status === 'running' || row.status === 'pending') && (
                            <Button size="small" danger onClick={async () => {
                              await api.post(`/sr-cluster/changes/${row.id}/cancel`); pollChange(row.id); loadChanges()
                            }}
                            >
                              取消
                            </Button>
                          )}
                          {(row.status === 'failed' || row.status === 'cancelled') && (
                            <Button size="small" onClick={async () => {
                              await api.post(`/sr-cluster/changes/${row.id}/retry`); pollChange(row.id); loadChanges()
                            }}
                            >
                              重试
                            </Button>
                          )}
                        </Space>
                      ),
                    },
                  ]}
                />
                {changeDetail && (() => {
                  const failedSteps = (changeDetail.steps || []).filter((s) => s.status === 'failed')
                  const isFailed = changeDetail.status === 'failed' || failedSteps.length > 0
                  const failRaw = failedSteps.map((s) => s.log_text || '').filter(Boolean).join('\n')
                    || changeDetail.log_text
                    || ''
                  const hint = changeFailureHint(failRaw)
                  const hintSummary = hint.summary === failRaw.trim() ? (lastFailLine(failRaw) || hint.summary) : hint.summary
                  const canRollbackChange = changeDetail.type === 'upgrade'
                    && (changeDetail.status === 'failed' || changeDetail.status === 'cancelled')
                  const releaseForArtifact = selected?.release_id
                    ? releases.find((r) => r.id === selected.release_id)
                    : undefined
                  return (
                  <Card
                    title={`步骤 · ${CHANGE_TYPE_LABEL[changeDetail.type] || changeDetail.type} · ${changeDetail.id.slice(0, 8)}`}
                    extra={(
                      <Space size={8}>
                        {changeDetail.type === 'upgrade' && changeDetail.status === 'success' && (
                          <Tooltip title="变更已成功但节点实际版本与目标不符时使用（例如 bin/lib 未替换成功），只升级版本不符的节点">
                            <Button size="small" onClick={() => openReupgradeByActualVersion(changeDetail)}>
                              按实际版本补升级
                            </Button>
                          </Tooltip>
                        )}
                        {(changeDetail.type === 'config_apply' || changeDetail.type === 'config_rollback')
                          && changeDetail.status !== 'running' && changeDetail.status !== 'pending' && (
                          <Tooltip title="各节点回到本次下发前的远端原文件">
                            <Button size="small" danger onClick={() => setChangeConfRollback(changeDetail)}>
                              回退本次下发
                            </Button>
                          </Tooltip>
                        )}
                        <Tag color={statusColor[changeDetail.status]}>{changeDetail.status}</Tag>
                      </Space>
                    )}
                  >
                    {isFailed && (
                      <Alert
                        className="sr-change-fail-alert"
                        type="error"
                        showIcon
                        style={{ marginBottom: 12 }}
                        message={(
                          <span>
                            初始化/变更失败
                            {failedSteps[0] ? ` · ${failedSteps[0].title}` : ''}
                          </span>
                        )}
                        description={(
                          <div className="sr-change-fail-body">
                            <div className="sr-change-fail-summary">{hintSummary}</div>
                            {failedSteps.length > 0 && (
                              <ul className="sr-change-fail-steps">
                                {failedSteps.map((s) => (
                                  <li key={s.id}>
                                    <strong>#{s.seq} {s.title}</strong>
                                    {s.log_text ? ` — ${lastFailLine(s.log_text)}` : ''}
                                    <span style={{ color: '#8c8c8c', fontWeight: 400 }}>（完整日志与诊断信息见下方步骤）</span>
                                  </li>
                                ))}
                              </ul>
                            )}
                            <div className="sr-change-fail-howto-title">怎么处理</div>
                            <ol className="sr-change-fail-howto">
                              {hint.howto.map((line) => (
                                <li key={line}>{line}</li>
                              ))}
                            </ol>
                            <Space wrap style={{ marginTop: 8 }}>
                              {hint.actionTab && (
                                <Button
                                  type="primary"
                                  danger
                                  size="small"
                                  onClick={() => {
                                    setTab(hint.actionTab!)
                                    if (hint.actionTab === 'settings' && releaseForArtifact) {
                                      artifactForm.resetFields()
                                      setArtifactModal(releaseForArtifact)
                                    }
                                  }}
                                >
                                  {hint.actionLabel || '前往处理'}
                                </Button>
                              )}
                              {(changeDetail.status === 'failed' || changeDetail.status === 'cancelled') && (
                                <Button
                                  size="small"
                                  onClick={async () => {
                                    await api.post(`/sr-cluster/changes/${changeDetail.id}/retry`)
                                    pollChange(changeDetail.id)
                                    loadChanges()
                                  }}
                                >
                                  重试该变更
                                </Button>
                              )}
                            </Space>
                          </div>
                        )}
                      />
                    )}
                    <Table
                      rowKey="id"
                      size="small"
                      pagination={false}
                      dataSource={changeDetail.steps || []}
                      rowClassName={(row) => (row.status === 'failed' ? 'sr-step-row-fail' : '')}
                      columns={[
                        { title: '#', dataIndex: 'seq', width: 48 },
                        { title: '步骤', dataIndex: 'title', width: 200 },
                        {
                          title: '状态',
                          dataIndex: 'status',
                          width: 100,
                          render: (s: string) => <Tag color={statusColor[s]}>{s}</Tag>,
                        },
                        { title: '开始', dataIndex: 'started_at', width: 120, render: (v: string) => fmtClock(v) },
                        {
                          title: '耗时',
                          width: 80,
                          render: (_: unknown, row: SrChangeStep) => fmtDuration(
                            row.started_at,
                            row.status === 'running' ? undefined : row.ended_at,
                          ),
                        },
                        {
                          title: '日志',
                          dataIndex: 'log_text',
                          render: (t: string, row: SrChangeStep) => (
                            <div className={`sr-step-log-cell${row.status === 'failed' ? ' sr-step-log-fail' : ''}`}>
                              {t || '—'}
                            </div>
                          ),
                        },
                        ...(canRollbackChange ? [{
                          title: '操作',
                          width: 110,
                          render: (_: unknown, row: SrChangeStep) => (stepCanRollback(row) && row.node_id ? (
                            <Button size="small" danger onClick={() => rollbackUpgradeNode(changeDetail, row)}>
                              回滚该节点
                            </Button>
                          ) : null),
                        }] : []),
                      ]}
                    />
                    <div style={{ marginTop: 12, fontWeight: 600 }}>
                      {changeCategoryLabel(SR_CHANGE_CATEGORIES, changeCategoryOf(SR_CHANGE_CATEGORIES, changeDetail.type))}
                    </div>
                    <pre className={`sr-change-log${isFailed ? ' sr-change-log-fail' : ''}`}>
                      {[
                        changeDetail.log_text ? `[变更] ${changeDetail.log_text}` : '',
                        ...(changeDetail.steps || []).map((s) => {
                          const body = (s.log_text || '').trim() || (s.status === 'pending' ? '（未执行）' : '—')
                          return `[#${s.seq} ${s.status}] ${s.title}\n${body}`
                        }),
                      ].filter(Boolean).join('\n\n') || '（暂无日志）'}
                    </pre>
                  </Card>
                  )
                })()}
              </Space>
            ),
          },
          {
            key: 'settings',
            label: '设置',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="large">
                <Card
                  title="制品 OSS 绑定"
                  extra={(
                    <Space>
                      <Button onClick={async () => {
                        const v = await ossForm.validateFields()
                        await api.put('/sr-cluster/oss', v)
                        message.success('已保存'); loadOSS()
                      }}
                      >
                        保存
                      </Button>
                      <Button onClick={async () => {
                        const res = await api.post('/sr-cluster/oss/test')
                        if (res.data?.ok) message.success(res.data.message)
                        else message.error(res.data?.message || '失败')
                      }}
                      >
                        测试所选 OSS
                      </Button>
                    </Space>
                  )}
                >
                  <Alert
                    type="info"
                    showIcon
                    style={{ marginBottom: 12 }}
                    message={<>Endpoint / Bucket / AKSK 在 <a href="/config/oss">配置中心 → OSS 配置</a> 维护；此处仅选择 OSS 并定义制品 prefix。</>}
                    description="下方「版本目录」支持按扁平文件名或分层路径从该 OSS 一键同步 FE/BE 分包。"
                  />
                  <Form form={ossForm} layout="vertical" style={{ maxWidth: 560 }}>
                    <Form.Item name="oss_setting_id" label="选择 OSS" rules={[{ required: true, message: '请选择配置中心 OSS' }]}>
                      <Select
                        placeholder="按环境选择"
                        options={ossOptions.map((o) => ({
                          value: o.id,
                          label: `${o.environment || o.id} · ${o.bucket || '-'} (${o.endpoint || '-'})${o.configured ? '' : ' [未配全]'}`,
                        }))}
                      />
                    </Form.Item>
                    <Form.Item name="prefix" label="制品 Prefix" rules={[{ required: true }]} extra="分层：{prefix}{version}/{fe|be}/…；也可扁平：{prefix}StarRocks-{version}.fe|be.tar.gz">
                      <Input placeholder="starrocks/" />
                    </Form.Item>
                  </Form>
                  {ossBind.oss_setting_id && (
                    <div style={{ color: '#666', fontSize: 12 }}>
                      当前：环境 {ossBind.oss_environment || '-'} / {ossBind.oss_bucket || '-'} @ {ossBind.oss_endpoint || '-'}
                      {ossBind.oss_configured ? ' · 已配置' : ' · 凭证不完整'}
                    </div>
                  )}
                </Card>
                <Card
                  title="版本目录"
                  extra={<Button type="primary" onClick={() => { releaseForm.resetFields(); releaseJdkTouched.current = false; setReleaseModal(true) }}>登记版本</Button>}
                >
                  <Alert
                    type="info"
                    showIcon
                    style={{ marginBottom: 12 }}
                    message="从 OSS 同步 FE/BE 分包"
                    description={(
                      (() => {
                        const pfx = ossBind.prefix || 'starrocks/'
                        return (
                          <div className="sr-oss-sync-hint">
                            <p>先「登记版本」并填写 JDK，再将安装包上传到已绑定 OSS，点「从 OSS 同步」自动写入 object_key / sha256（也可「登记/上传分包」手工维护）。</p>
                            <p><strong>扁平命名</strong>（推荐，包直接放在 prefix 下）：</p>
                            <pre>{`${pfx}StarRocks-{version}.fe.tar.gz\n${pfx}StarRocks-{version}.be.tar.gz`}</pre>
                            <p>示例：{`${pfx}StarRocks-3.2.10.fe.tar.gz`}、{`${pfx}StarRocks-3.2.10.be.tar.gz`}</p>
                            <p><strong>分层路径</strong>（同样支持）：</p>
                            <pre>{`${pfx}{version}/fe/{filename}\n${pfx}{version}/be/{filename}`}</pre>
                            <p>同步按版本号精确匹配文件名中的 fe/be；优先读 `.sha256` 侧车或对象元数据，大包不在请求内整包下载（改为后台算 sha256）。脚本上传的包可在展开行直接粘贴并保存 sha256。</p>
                          </div>
                        )
                      })()
                    )}
                  />
                  <Table
                    rowKey="id"
                    dataSource={releases}
                    pagination={false}
                    expandable={{
                      expandedRowRender: (r: SrRelease) => (
                        <Table
                          rowKey="id"
                          size="small"
                          pagination={false}
                          dataSource={r.artifacts || []}
                          columns={[
                            { title: '角色', dataIndex: 'role', width: 56 },
                            { title: 'object_key', dataIndex: 'object_key', ellipsis: true },
                            {
                              title: 'sha256',
                              dataIndex: 'sha256',
                              width: 480,
                              render: (v: string, art: SrReleaseArtifact) => {
                                const draft = shaDraft[art.id] ?? v ?? ''
                                const valid = /^[a-fA-F0-9]{64}$/.test(draft)
                                const dirty = shaDraft[art.id] !== undefined && shaDraft[art.id] !== (v || '')
                                // 不完整或未改完的值也必须能点「保存」，以便弹出更正说明
                                const canSave = draft.length > 0 && (!valid || dirty || !v)
                                return (
                                  <div>
                                    <Space.Compact style={{ width: '100%' }}>
                                      <Input
                                        size="small"
                                        value={draft}
                                        status={!draft || !valid ? 'warning' : undefined}
                                        placeholder="粘贴 64 位 sha256"
                                        style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
                                        onChange={(e) => {
                                          // 允许粘贴带文件名的 shasum 输出，自动取第一个 token
                                          let next = e.target.value.trim()
                                          const m = next.match(/^[a-fA-F0-9]{64}\b/)
                                          if (m) next = m[0]
                                          setShaDraft((prev) => ({ ...prev, [art.id]: next }))
                                        }}
                                      />
                                      <Button
                                        size="small"
                                        type={canSave ? 'primary' : 'default'}
                                        loading={shaSavingId === art.id}
                                        disabled={!canSave}
                                        onClick={() => { void saveArtifactSha(r, art) }}
                                      >
                                        保存
                                      </Button>
                                    </Space.Compact>
                                    <div style={{ fontSize: 11, marginTop: 2, color: valid ? '#8c8c8c' : '#d48806' }}>
                                      {valid ? '已是 64 位，改动后可再保存' : `当前 ${draft.length}/64 位，请粘贴完整 sha256 后再保存（可用 shasum -a 256）`}
                                    </div>
                                  </div>
                                )
                              },
                            },
                            { title: '大小', dataIndex: 'size_bytes', width: 120 },
                          ]}
                        />
                      ),
                    }}
                    columns={[
                      { title: '版本', dataIndex: 'version' },
                      {
                        title: '发行通道',
                        dataIndex: 'channel',
                        render: (v: string) => <Tag>{srChannelLabel(v)}</Tag>,
                      },
                      { title: 'JDK', dataIndex: 'jdk_min', render: (_: string, r: SrRelease) => jdkRangeLabel(r.jdk_min, r.jdk_max) },
                      { title: '启用', dataIndex: 'enabled', render: (v: boolean) => (v ? '是' : '否') },
                      {
                        title: '操作',
                        render: (_: unknown, r: SrRelease) => (
                          <Space wrap>
                            <Button size="small" loading={syncBusyId === r.id} onClick={() => { void syncReleaseFromOSS(r) }}>
                              从 OSS 同步
                            </Button>
                            <Button size="small" onClick={() => { artifactForm.resetFields(); setArtifactModal(r) }}>登记/上传分包</Button>
                            <Button size="small" danger onClick={async () => { await api.delete(`/sr-cluster/releases/${r.id}`); loadReleases() }}>删除</Button>
                          </Space>
                        ),
                      },
                    ]}
                  />
                </Card>
                <Card
                  title="FE / BE 配置模板（平台默认）"
                  extra={(
                    <Space>
                      <Button
                        loading={tplBusy}
                        onClick={async () => {
                          setTplBusy(true)
                          try {
                            const res = await api.post('/sr-cluster/conf-templates/reset')
                            const list: { role: string; content?: string }[] = res.data?.data || []
                            setTplFe(list.find((x) => x.role === 'fe')?.content || '')
                            setTplBe(list.find((x) => x.role === 'be')?.content || '')
                            message.success('已恢复为内置现网模板')
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || e.message)
                          } finally {
                            setTplBusy(false)
                          }
                        }}
                      >
                        恢复内置模板
                      </Button>
                      <Button
                        type="primary"
                        loading={tplBusy}
                        onClick={async () => {
                          setTplBusy(true)
                          try {
                            await api.put('/sr-cluster/conf-templates', { role: 'fe', content: tplFe })
                            await api.put('/sr-cluster/conf-templates', { role: 'be', content: tplBe })
                            message.success('平台模板已保存')
                            loadConfTemplates()
                          } catch (e: any) {
                            message.error(e?.response?.data?.message || e.message)
                          } finally {
                            setTplBusy(false)
                          }
                        }}
                      >
                        保存模板
                      </Button>
                    </Space>
                  )}
                >
                  <Alert
                    type="info"
                    showIcon
                    style={{ marginBottom: 12 }}
                    message="集群无独立草稿时使用本模板。FE 含 JAVA_OPTS / 端口 / frontend_address 等；勿写 meta_dir / storage_root_path（下发时平台追加）。旧短模板可点「恢复内置模板」。"
                  />
                  <Tabs
                    items={[
                      {
                        key: 'fe',
                        label: 'fe.conf',
                        children: (
                          <Input.TextArea
                            value={tplFe}
                            onChange={(e) => setTplFe(e.target.value)}
                            rows={14}
                            style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
                          />
                        ),
                      },
                      {
                        key: 'be',
                        label: 'be.conf',
                        children: (
                          <Input.TextArea
                            value={tplBe}
                            onChange={(e) => setTplBe(e.target.value)}
                            rows={14}
                            style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
                          />
                        ),
                      },
                    ]}
                  />
                </Card>
              </Space>
            ),
          },
          {
            key: 'agents',
            label: 'Node Agent',
            children: (
              <Space direction="vertical" style={{ width: '100%' }} size="middle">
                <Alert
                  type="info"
                  showIcon
                  message="StarRocks Node Agent 下发与登记（本页完成）"
                  description={(
                    <>
                      SSH 下发
                      {' '}
                      <code>deploy/node-agent</code>
                      {' '}
                      到 host 实例（默认
                      {' '}
                      <code>:8091</code>
                      ，
                      {' '}
                      <code>/v1/sr/*</code>
                      ）。成功后自动写入本表，供集群绑定。
                      <strong>不是</strong>
                      配置中心「平台 Agent」(:8090)。下发前请在控制面机器执行
                      {' '}
                      <code>cd deploy/node-agent && ./build.sh</code>
                      。对已有集群：下发
                      <strong>仅安装/重启 Agent 自身</strong>
                      ，不启停 FE/BE、不改业务 systemd/二进制/数据盘。
                    </>
                  )}
                />
                <Space wrap>
                  <Button type="primary" onClick={async () => {
                    deployForm.resetFields()
                    deployForm.setFieldsValue({
                      listen_port: 8091,
                      remote_dir: '/home/sysadmin/auto-dba/node-agent',
                      escalate: false,
                      host_instance_ids: [],
                    })
                    setDeploySource('monitor')
                    setDeployMonTask(undefined)
                    setDeployMonRows([])
                    setDeployMonSelected([])
                    loadHosts()
                    try {
                      const [feRes, beRes] = await Promise.all([
                        api.get('/monitor-targets/preview', { params: { kind: 'starrocks-fe' } }),
                        api.get('/monitor-targets/preview', { params: { kind: 'starrocks-be' } }),
                      ])
                      setFePreview(feRes.data?.data || [])
                      setBePreview(beRes.data?.data || [])
                    } catch {
                      setFePreview([])
                      setBePreview([])
                    }
                    setDeployModal(true)
                  }}
                  >
                    SSH 下发
                  </Button>
                  <Button onClick={() => {
                    setAgentBindTarget(null)
                    agentForm.resetFields()
                    setAgentModal(true)
                  }}
                  >
                    手工登记
                  </Button>
                  <Button onClick={() => { loadAgents(); loadDeploys() }}>刷新</Button>
                </Space>
                <Table
                  rowKey="id"
                  size="small"
                  title={() => '已登记 Agent'}
                  dataSource={agents}
                  pagination={{
                    pageSize: 10, size: 'small', showSizeChanger: true, pageSizeOptions: [10, 20, 50],
                    showTotal: (t) => `共 ${t} 个`,
                  }}
                  columns={[
                    { title: '名称', dataIndex: 'name' },
                    { title: 'URL', dataIndex: 'base_url' },
                    {
                      title: '状态',
                      width: 100,
                      render: (_: unknown, row: SrAgent) => {
                        const st = agentStatus[row.id]?.status || 'unknown'
                        const color = st === 'running' ? 'success' : st === 'stopped' ? 'default' : 'warning'
                        const label = st === 'running' ? '运行中' : st === 'stopped' ? '已停止' : '未知'
                        return <Tag color={color} title={agentStatus[row.id]?.message}>{label}</Tag>
                      },
                    },
                    {
                      title: '来源',
                      width: 100,
                      render: (_: unknown, row: SrAgent) => (row.host_instance_id
                        ? <Tag color="blue">SSH 下发</Tag>
                        : <Tag>手工</Tag>),
                    },
                    {
                      title: '操作',
                      width: 280,
                      render: (_: unknown, row: SrAgent) => {
                        const busy = agentActingId === row.id
                        const deployed = !!row.host_instance_id
                        const running = agentStatus[row.id]?.status === 'running'
                        const applyStatus = (st: { id?: string; status?: string; message?: string } | undefined) => {
                          if (st?.id) setAgentStatus((m) => ({ ...m, [st.id!]: { status: st.status || 'unknown', message: st.message } }))
                        }
                        return (
                          <Space wrap size={0}>
                            <Button
                              type="link"
                              size="small"
                              loading={busy}
                              onClick={async () => {
                                setAgentActingId(row.id)
                                try {
                                  const res = await api.post(`/sr-cluster/agents/${row.id}/probe`)
                                  const st = res.data?.data
                                  applyStatus(st)
                                  if (st?.status === 'running') message.success('运行中')
                                  else message.warning(st?.message || '未运行')
                                } catch (e: any) {
                                  message.error(e?.response?.data?.message || e.message)
                                } finally {
                                  setAgentActingId('')
                                }
                              }}
                            >
                              刷新状态
                            </Button>
                            {deployed ? (
                              running ? (
                                <Button
                                  type="link"
                                  size="small"
                                  danger
                                  loading={busy}
                                  onClick={async () => {
                                    setAgentActingId(row.id)
                                    try {
                                      const res = await api.post(`/sr-cluster/agents/${row.id}/stop`)
                                      message.success('已停止')
                                      applyStatus(res.data?.data)
                                    } catch (e: any) {
                                      message.error(e?.response?.data?.message || e.message)
                                    } finally {
                                      setAgentActingId('')
                                    }
                                  }}
                                >
                                  停止
                                </Button>
                              ) : (
                                <Button
                                  type="link"
                                  size="small"
                                  loading={busy}
                                  onClick={async () => {
                                    setAgentActingId(row.id)
                                    try {
                                      const res = await api.post(`/sr-cluster/agents/${row.id}/start`)
                                      message.success('已启动')
                                      applyStatus(res.data?.data)
                                    } catch (e: any) {
                                      message.error(e?.response?.data?.message || e.message)
                                    } finally {
                                      setAgentActingId('')
                                    }
                                  }}
                                >
                                  启动
                                </Button>
                              )
                            ) : null}
                            <Button
                              type="link"
                              danger
                              size="small"
                              onClick={async () => {
                                Modal.confirm({
                                  title: '删除 Agent',
                                  content: `确认删除「${row.name || row.base_url}」登记？删除后，该主机上的集群节点将无法通过 Agent 运维；在该主机重新下发 Agent 后会自动恢复绑定。`,
                                  okType: 'danger',
                                  onOk: async () => {
                                    let affected: { name: string; detail?: string }[] = []
                                    try {
                                      const res = await api.delete(`/sr-cluster/agents/${row.id}`)
                                      affected = res.data?.affected_nodes || []
                                    } catch {
                                      return
                                    }
                                    if (affected.length) {
                                      message.warning(
                                        `已删除。以下节点需重新下发 Agent 后才能运维：${affected
                                          .map((n) => (n.detail ? `${n.name}（${n.detail}）` : n.name))
                                          .join('、')}`,
                                        6,
                                      )
                                    } else {
                                      message.success('已删除')
                                    }
                                    loadAgents()
                                  },
                                })
                              }}
                            >
                              删除
                            </Button>
                          </Space>
                        )
                      },
                    },
                  ]}
                />
                <Table
                  rowKey="id"
                  size="small"
                  title={() => '下发记录'}
                  dataSource={deploys}
                  pagination={{
                    pageSize: 10, size: 'small', showSizeChanger: true, pageSizeOptions: [10, 20, 50],
                    showTotal: (t) => `共 ${t} 条`,
                  }}
                  columns={[
                    {
                      title: '主机',
                      dataIndex: 'host_instance_id',
                      render: (id: string) => {
                        const h = hosts.find((x) => x.id === id)
                        return h ? `${h.name} (${h.host || h.domain_name || '-'})` : id
                      },
                    },
                    {
                      title: '状态',
                      dataIndex: 'status',
                      width: 110,
                      render: (s: string, row: SrAgentDeploy) => (
                        <Tag color={statusColor[s] || 'default'} icon={isAgentDeployActive(row) ? <SyncOutlined spin /> : undefined}>{s}</Tag>
                      ),
                    },
                    { title: '端口', dataIndex: 'listen_port', width: 70 },
                    { title: 'Base URL', dataIndex: 'base_url', ellipsis: true },
                    { title: '时间', dataIndex: 'created_at', width: 180 },
                    {
                      title: '操作',
                      width: 80,
                      render: (_: unknown, row: SrAgentDeploy) => (
                        <Button type="link" size="small" onClick={() => setDeployLog(row)}>日志</Button>
                      ),
                    },
                  ]}
                />
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title={clusterModalMode === 'register' ? '登记已有集群' : '新建集群（含 FE/BE 角色）'}
        open={clusterModal}
        width={860}
        onOk={async () => {
          const v = await clusterForm.validateFields()
          const feNodes = (v.fe_nodes || []).map((n: any) => ({
            component: 'fe',
            host: n.host,
            agent_id: n.agent_id,
            is_primary_fe: !!n.is_primary_fe,
            fe_role: n.is_primary_fe ? 'follower' : (n.fe_role || 'follower'),
          }))
          const beNodes = (v.be_nodes || []).map((n: any) => ({
            component: 'be',
            host: n.host,
            agent_id: n.agent_id,
          }))
          const nodes = [...feNodes, ...beNodes]
          if (!feNodes.length) {
            message.error('至少配置 1 个 FE')
            return
          }
          if (feNodes.filter((n: any) => n.is_primary_fe).length !== 1) {
            message.error('须且仅指定 1 个首 FE（bootstrap，不带 --helper）')
            return
          }
          if (feNodes.length % 2 === 0) {
            message.error('FE 节点数须为奇数')
            return
          }
          const registering = clusterModalMode === 'register'
          if (registering) {
            if (!String(v.version || '').trim() && !v.release_id) {
              message.error('请填写版本（可从监控 tag version 导入），或选择已登记的制品版本')
              return
            }
          } else if (!v.release_id) {
            message.error('请选择版本（release）')
            return
          }
          try {
            const matched = v.release_id
              ? releases.find((r) => r.id === v.release_id)
              : releases.find((r) => r.enabled !== false && r.version === String(v.version || '').trim())
            const res = await api.post('/sr-cluster/clusters', {
              name: v.name,
              environment: v.environment,
              version: String(v.version || matched?.version || '').trim(),
              release_id: matched?.id || v.release_id || undefined,
              software_base: v.software_base,
              be_software_base: v.be_software_base || v.software_base,
              data_root: v.data_root,
              fe_password: v.fe_password,
              nodes,
              init_now: registering ? false : !!v.init_now,
              register_existing: registering,
              source_monitor_task: registering ? monitorTask : undefined,
            })
            message.success(
              registering
                ? '已登记为 ready，可直接维护'
                : (res.data?.data?.change_id ? '已创建并提交初始化' : '已创建'),
            )
            if (res.data?.warning) message.warning(res.data.warning, 8)
            setClusterModal(false)
            clusterForm.resetFields()
            loadClusters()
            if (res.data?.data?.change_id) {
              setTab('changes')
              pollChange(res.data.data.change_id)
            }
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message)
          }
        }}
        onCancel={() => setClusterModal(false)}
        destroyOnClose
      >
        {clusterModalMode === 'register' ? (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="登记已有集群（不装包、不初始化）"
            description={(
              <>
                写入拓扑与凭据后状态为 ready，用于扩缩/升级/重启等维护。可从
                {' '}
                <Link to="/config/monitor-targets">监控目标注册</Link>
                {' '}
                的 StarRocks FE/BE 导入 host；每节点仍须绑定 Node Agent。路径/密码须与现场一致。
              </>
            )}
          />
        ) : (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="角色决定部署与启动方式"
            description="FE：奇数个；唯一「首 FE」bootstrap 启动（无 --helper）；其余 FE 为 follower/observer，初始化时 ADD FOLLOWER + --helper。BE：disk/init → 装包 → ADD BACKEND。禁止无角色节点。"
          />
        )}
        {clusterModalMode === 'register' && (
          <Card size="small" title="从监控目标导入" style={{ marginBottom: 12 }}>
            {monitorTaskOptions.length === 0 ? (
              <Alert
                type="warning"
                showIcon
                message={(
                  <>
                    暂无 StarRocks FE/BE 监控目标，请先到
                    {' '}
                    <Link to="/config/monitor-targets">配置中心 · 监控目标注册</Link>
                    {' '}
                    录入，或下方手工填写。
                  </>
                )}
              />
            ) : (
              <Space wrap style={{ width: '100%' }}>
                <Select
                  style={{ width: 240 }}
                  placeholder="监控任务"
                  options={monitorTaskOptions}
                  value={monitorTask}
                  onChange={(v) => { setMonitorTask(v); setMonitorDomain(undefined) }}
                  allowClear
                />
                <Select
                  style={{ width: 180 }}
                  placeholder="domain（可选）"
                  options={monitorDomainOptions}
                  value={monitorDomain}
                  onChange={setMonitorDomain}
                  allowClear
                  disabled={!monitorTask}
                />
                <Button type="primary" onClick={applyMonitorImport}>导入预填</Button>
              </Space>
            )}
          </Card>
        )}
        <Form
          form={clusterForm}
          layout="vertical"
          initialValues={{
            software_base: '/home/dorisdb_base',
            be_software_base: '/home/dorisdb_base',
            data_root: '/data',
            fe_user: 'root',
            init_now: false,
            fe_nodes: [{ host: '', agent_id: undefined, is_primary_fe: true, fe_role: 'follower' }],
            be_nodes: [{ host: '', agent_id: undefined }],
          }}
        >
          <Form.Item name="name" label="集群名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item
            name="environment"
            label="环境"
            rules={[{ required: true, message: '请选择环境' }]}
            extra={(
              <>
                来自
                {' '}
                <Link to="/config/environments">配置中心 · 环境管理</Link>
              </>
            )}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="请选择环境"
              options={envOptions}
            />
          </Form.Item>
          {clusterModalMode === 'register' ? (
            <>
              <Form.Item
                name="version"
                label="版本"
                rules={[{ required: true, message: '填写版本号，或从监控 tag version 导入' }]}
                extra={releases.length === 0
                  ? '来自监控目标标签 version（亦可手填）。制品目录为空时登记不强制绑定 Release。'
                  : '优先用监控 tag；若与设置中的版本目录一致会自动关联 release_id'}
              >
                <AutoComplete
                  options={releases.filter((r) => r.enabled !== false).map((r) => ({ value: r.version }))}
                  placeholder="如 3.2.8"
                  onChange={(val) => {
                    const hit = releases.find((r) => r.enabled !== false && r.version === val)
                    clusterForm.setFieldsValue({ release_id: hit?.id })
                  }}
                />
              </Form.Item>
              {releases.length > 0 && (
                <Form.Item name="release_id" label="关联制品版本（可选）">
                  <Select
                    allowClear
                    placeholder="可留空"
                    options={releases.filter((r) => r.enabled !== false).map((r) => ({ value: r.id, label: `${r.version}（${srChannelLabel(r.channel)}）` }))}
                    onChange={(id) => {
                      const hit = releases.find((r) => r.id === id)
                      if (hit) clusterForm.setFieldsValue({ version: hit.version })
                    }}
                  />
                </Form.Item>
              )}
            </>
          ) : (
            <Form.Item name="release_id" label="版本" rules={[{ required: true }]}>
              <Select
                notFoundContent={releases.length === 0 ? '请先在「设置」登记版本包' : undefined}
                options={releases.filter((r) => r.enabled !== false).map((r) => ({ value: r.id, label: `${r.version}（${srChannelLabel(r.channel)}）` }))}
              />
            </Form.Item>
          )}
          <Form.Item
            name="software_base"
            label="FE software_base"
            extra={clusterModalMode === 'register' ? '可由探测从 internal_log_dir 推断（如 /data1/dorisdb_base）' : '装包根目录；FE 路径为 {software_base}/fe'}
          >
            <Input placeholder="/home/dorisdb_base" />
          </Form.Item>
          <Form.Item
            name="be_software_base"
            label="BE software_base"
            extra={clusterModalMode === 'register' ? '可由探测从 BE /varz 的 sys_minidump_dir 推断；可与 FE 不同' : '装包根目录；BE 路径为 {be_software_base}/be'}
          >
            <Input placeholder="/home/dorisdb_base" />
          </Form.Item>
          <Form.Item
            name="data_root"
            label="BE 数据目录（storage_root_path）"
            extra={clusterModalMode === 'register'
              ? '登记时填探测到的完整目录（如 /data1/dorisdb/be/storage），下发时原样写入，不拼接路径'
              : '填写实际数据目录，下发时原样写入 be.conf。不存在时自动选用未使用磁盘格式化挂载；已存在时须位于非系统盘'}
          >
            <Input placeholder="/data1/dorisdb/be/storage" />
          </Form.Item>
          <Form.Item
            name="fe_password"
            label={clusterModalMode === 'register' ? 'FE 密码' : 'FE 密码（初装可空）'}
            extra={clusterModalMode === 'register'
              ? '优先从「配置中心 · FE 管理」按 FE 主机匹配；亦可留空由服务端回退 FE 管理 / 实例密码'
              : '初装后 root 上了密码，请在「资源配置 → 实例管理 → StarRocks」的对应实例中维护，平台优先使用实例密码'}
          >
            <Input.Password />
          </Form.Item>
          {clusterModalMode === 'register' ? (
            <div style={{ marginBottom: 12 }}>
              <Space wrap>
                <Button onClick={() => void resolveFePasswordFromAdmin()}>从 FE 管理获取密码</Button>
                <Button loading={feProbeBusy} onClick={() => void probeRegisterFERoles()}>
                  探测 FE 角色与目录
                </Button>
                <Button loading={feProbeBusy} onClick={async () => {
                  const feNodes: { host?: string }[] = clusterForm.getFieldValue('fe_nodes') || []
                  const hosts = feNodes.map((n) => endpointHost(n.host || '') || String(n.host || '').trim()).filter(Boolean)
                  if (!hosts.length) {
                    message.warning('请先填写 FE Host')
                    return
                  }
                  let password = String(clusterForm.getFieldValue('fe_password') || '')
                  if (!password) password = await resolveFePasswordFromAdmin({ silent: true })
                  setFeProbeBusy(true)
                  try {
                    await probeRegisterLayout(hosts, password)
                  } finally {
                    setFeProbeBusy(false)
                  }
                }}
                >
                  仅探测目录
                </Button>
              </Space>
              <div className="muted" style={{ marginTop: 4 }}>
                连接首个 FE（:9030）探测角色，并从 ADMIN SHOW FRONTEND CONFIG / BE :8040/varz 回填 software_base 与数据目录
              </div>
            </div>
          ) : null}

          {clusterModalMode === 'register' ? (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 12 }}
              message="登记已有：Node Agent 可暂空"
              description={(
                <>
                  先保存拓扑与 FE 凭据；Agent（
                  <code>:8091</code>
                  ）可稍后在「Node Agent」Tab 从监控批量下发后再绑。填 Host 时若已有匹配 Agent 会自动选中。
                </>
              )}
            />
          ) : agents.length === 0 ? (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 12 }}
              message="尚无 Node Agent 可选项"
              description={(
                <>
                  新建装机须绑定 Node Agent（默认
                  {' '}
                  <code>:8091</code>
                  ）。请
                  {' '}
                  <Button type="link" size="small" style={{ padding: 0 }} onClick={goNodeAgentTab}>打开 Node Agent Tab</Button>
                  {' '}
                  或点「按主机登记」。
                </>
              )}
            />
          ) : null}

          <div style={{ fontWeight: 600, marginBottom: 8 }}>
            FE 节点（须奇数；填 Host 自动匹配已有 Agent
            {clusterModalMode === 'register' ? '，可暂空' : ''}
            ）
          </div>
          <Form.List name="fe_nodes">
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => {
                  const hostVal = clusterForm.getFieldValue(['fe_nodes', field.name, 'host'])
                  const probed = feProbedRoles[endpointHost(hostVal || '') || String(hostVal || '').trim()]
                  return (
                  <Space key={field.key} align="start" style={{ display: 'flex', marginBottom: 8 }} wrap>
                    <Form.Item {...field} name={[field.name, 'host']} rules={[{ required: true, message: 'host' }]} style={{ width: 160 }}>
                      <Input
                        placeholder="FE Host"
                        onBlur={(e) => void bindClusterNodeAgent('fe_nodes', field.name, e.target.value)}
                      />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, 'agent_id']}
                      rules={clusterModalMode === 'register' ? [] : [{ required: true, message: 'Agent' }]}
                      style={{ width: 260 }}
                    >
                      <Select
                        allowClear={clusterModalMode === 'register'}
                        placeholder={clusterModalMode === 'register' ? '可选 Agent / 稍后补' : 'Node Agent (:8091)'}
                        options={agentSelectOptions}
                        notFoundContent={agents.length === 0 ? '暂无 Agent' : '无匹配'}
                        showSearch
                        optionFilterProp="label"
                      />
                    </Form.Item>
                    <Button type="link" size="small" onClick={() => openQuickAgentRegister('fe_nodes', field.name)}>
                      按主机登记
                    </Button>
                    <Form.Item {...field} name={[field.name, 'is_primary_fe']} valuePropName="checked" style={{ marginBottom: 0 }}>
                      <Checkbox>首 FE</Checkbox>
                    </Form.Item>
                    {probed ? <Tag color={probed === 'LEADER' ? 'gold' : 'blue'}>{probed}</Tag> : null}
                    <Form.Item {...field} name={[field.name, 'fe_role']} style={{ width: 120 }}>
                      <Select
                        allowClear
                        placeholder="子角色"
                        options={[{ value: 'follower', label: 'follower' }, { value: 'observer', label: 'observer' }]}
                      />
                    </Form.Item>
                    {fields.length > 1 && <MinusCircleOutlined onClick={() => remove(field.name)} />}
                  </Space>
                  )
                })}
                <Button type="dashed" onClick={() => add({ is_primary_fe: false, fe_role: 'follower' })} icon={<PlusOutlined />} block>
                  添加 FE
                </Button>
              </>
            )}
          </Form.List>

          <div style={{ fontWeight: 600, margin: '16px 0 8px' }}>
            BE 节点（填 Host 自动匹配已有 Agent
            {clusterModalMode === 'register' ? '，可暂空' : ''}
            ）
          </div>
          <Form.List name="be_nodes">
            {(fields, { add, remove }) => (
              <>
                {fields.map((field) => (
                  <Space key={field.key} align="start" style={{ display: 'flex', marginBottom: 8 }} wrap>
                    <Form.Item {...field} name={[field.name, 'host']} rules={[{ required: true, message: 'host' }]} style={{ width: 160 }}>
                      <Input
                        placeholder="BE Host"
                        onBlur={(e) => void bindClusterNodeAgent('be_nodes', field.name, e.target.value)}
                      />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, 'agent_id']}
                      rules={clusterModalMode === 'register' ? [] : [{ required: true, message: 'Agent' }]}
                      style={{ width: 260 }}
                    >
                      <Select
                        allowClear={clusterModalMode === 'register'}
                        placeholder={clusterModalMode === 'register' ? '可选 Agent / 稍后补' : 'Node Agent (:8091)'}
                        options={agentSelectOptions}
                        notFoundContent={agents.length === 0 ? '暂无 Agent' : '无匹配'}
                        showSearch
                        optionFilterProp="label"
                      />
                    </Form.Item>
                    <Button type="link" size="small" onClick={() => openQuickAgentRegister('be_nodes', field.name)}>
                      按主机登记
                    </Button>
                    {fields.length > 0 && <MinusCircleOutlined onClick={() => remove(field.name)} />}
                  </Space>
                ))}
                <Button type="dashed" onClick={() => add()} icon={<PlusOutlined />} block>
                  添加 BE
                </Button>
              </>
            )}
          </Form.List>

          {clusterModalMode === 'create' && (
            <Form.Item name="init_now" valuePropName="checked" style={{ marginTop: 16 }}>
              <Checkbox>创建后立即提交集群初始化变更单</Checkbox>
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title="添加节点（提交审批）"
        open={nodeModal}
        onCancel={() => setNodeModal(false)}
        destroyOnClose
        footer={[
          <Button key="cancel" onClick={() => setNodeModal(false)}>取消</Button>,
          <Button key="check" loading={nodePrecheckBusy} onClick={() => { void runNodePrecheck() }}>基础校验</Button>,
          <Button
            key="submit"
            type="primary"
            loading={nodeSubmitBusy}
            disabled={!nodePrecheck?.ok || nodePrecheckBusy}
            onClick={() => { void submitAddNode() }}
          >
            提交工单
          </Button>,
        ]}
      >
        <Alert type="info" showIcon style={{ marginBottom: 12 }} message="审批通过后自动登记节点并执行加入编排（装包 / ADD FOLLOWER 或 ADD BACKEND）。" />
        <Form
          form={nodeForm}
          layout="vertical"
          initialValues={{ component: 'be', is_primary_fe: false, fe_role: 'follower' }}
          onValuesChange={(changed) => {
            setNodePrecheck(null)
            if ('host_instance_id' in changed) void onNodeHostChange(changed.host_instance_id)
          }}
        >
          <Form.Item name="component" label="组件角色" rules={[{ required: true }]}>
            <Select options={[{ value: 'fe', label: 'FE' }, { value: 'be', label: 'BE' }]} />
          </Form.Item>
          <Form.Item
            name="host_instance_id"
            label="Host（已登记主机）"
            rules={[{ required: true, message: '请选择主机' }]}
            extra={nodeHostInst ? `节点地址：${hostPrimaryAddr(nodeHostInst) || '—'}` : '主机来自实例管理中 db_type=host 的登记'}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择主机"
              notFoundContent="暂无主机实例，请先在实例管理登记主机"
              options={hosts.map((h) => ({ value: h.id, label: `${h.name} (${hostPrimaryAddr(h) || '无地址'})` }))}
            />
          </Form.Item>
          <Form.Item name="host" hidden rules={[{ required: true, message: '所选主机缺少 Host/域名' }]}><Input /></Form.Item>
          <Form.Item
            name="agent_id"
            label="Node Agent"
            dependencies={['host_instance_id']}
            rules={[
              { required: true, message: '请选择与主机同地址的 Node Agent' },
              {
                validator: async (_, val) => {
                  if (!val || !nodeHostInst) return
                  const ag = agents.find((a) => a.id === val)
                  if (ag && !agentMatchesHost(ag, nodeHostInst)) throw new Error(agentMismatchMessage(ag, nodeHostInst))
                },
              },
            ]}
            extra={nodeHostInst && nodeAgentOptions.length === 0 ? (
              <span>
                该主机尚无 Node Agent（:8091），
                <Button type="link" size="small" style={{ padding: 0 }} onClick={() => { setNodeModal(false); setTab('agents') }}>去下发 Agent</Button>
              </span>
            ) : '只列出与所选主机地址一致的 Agent'}
          >
            <Select
              placeholder={nodeHostInst ? '选择 Node Agent' : '请先选择主机'}
              disabled={!nodeHostInst}
              options={nodeAgentOptions}
            />
          </Form.Item>
          <Form.Item
            name="software_base"
            label="dorisdb_base（软件程序目录）"
            rules={[{ required: true, message: '请填写软件目录' }]}
            extra="装包路径为 {dorisdb_base}/fe 或 {dorisdb_base}/be"
          >
            <Input placeholder="/home/dorisdb_base" />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, cur) => prev.component !== cur.component}>
            {() => nodeForm.getFieldValue('component') === 'fe' ? (
              <>
                <Form.Item name="is_primary_fe" label="首 FE（仅新建集群 bootstrap 使用；扩容勿勾）" valuePropName="checked"><Switch /></Form.Item>
                <Form.Item name="fe_role" label="FE 子角色" extra="不可指定 leader；扩容一般为 follower，启动带 --helper">
                  <Select options={[{ value: 'follower', label: 'follower' }, { value: 'observer', label: 'observer' }]} />
                </Form.Item>
              </>
            ) : (
              <>
                <Form.Item
                  name="data_root"
                  label="数据目录（storage_root_path）"
                  rules={[{ required: true, message: '请填写数据目录' }]}
                  extra="填写实际数据目录，下发时原样写入。不存在时自动选用未使用磁盘格式化挂载；已存在时须位于非系统盘"
                >
                  <Input placeholder="/data1/dorisdb/be/storage" />
                </Form.Item>
                <Alert type="warning" showIcon message="BE：校验数据目录（空闲盘格式化挂载 / 已挂载直接使用 / 系统盘拒绝）→ 装包到 dorisdb_base/be → ADD BACKEND" />
              </>
            )}
          </Form.Item>
          <Form.Item
            name="assigned_to"
            label="审批人"
            rules={[{ required: true, message: '请选择审批人' }]}
            extra={nodeHostInst && !nodeApprovers.loading && nodeApprovers.approvers.length === 0
              ? '该主机未配置审批人，请在实例管理为主机配置审批人'
              : '按所选主机的审批人配置加载'}
          >
            <Select
              showSearch
              allowClear
              loading={nodeApprovers.loading}
              disabled={!nodeHostInst}
              placeholder={nodeHostInst ? '选择审批人' : '请先选择主机'}
              options={nodeApprovers.approvers.map((a) => ({ value: a, label: a }))}
            />
          </Form.Item>
          <Form.Item name="description" label="事由"><Input.TextArea rows={2} /></Form.Item>
        </Form>
        <NodePrecheckPanel result={nodePrecheck} busy={nodePrecheckBusy} />
      </Modal>

      <Modal
        title="SSH 下发 Node Agent"
        open={deployModal}
        confirmLoading={deployBusy}
        width={720}
        onCancel={() => setDeployModal(false)}
        onOk={async () => {
          const v = await deployForm.validateFields()
          let ids: string[] = []
          if (deploySource === 'monitor') {
            ids = deployMonSelected.filter(Boolean)
            if (!ids.length) {
              message.warning('请勾选已匹配到主机实例的节点')
              return
            }
          } else {
            ids = (v.host_instance_ids || []).filter(Boolean)
            if (!ids.length) {
              message.warning('请选择至少一台目标主机')
              return
            }
          }
          setDeployBusy(true)
          try {
            const res = await api.post('/sr-cluster/agents/deploy-batch', {
              host_instance_ids: ids,
              listen_port: v.listen_port || 8091,
              remote_dir: v.remote_dir,
              escalate: v.escalate ? 'sudo_su' : '',
            })
            const d = res.data?.data || {}
            const results: { host_instance_id: string; deploy_id?: string; error?: string }[] = d.results || []
            for (const r of results) {
              if (r.deploy_id) deployTracker.track(r.deploy_id)
            }
            message.success(`已提交：成功 ${d.ok_count || 0}，失败 ${d.fail_count || 0}（进度见下发记录）`)
            if (d.fail_count > 0) {
              Modal.warning({
                title: '部分主机下发失败',
                content: (
                  <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>
                    {results.filter((r) => r.error).map((r) => (
                      <li key={r.host_instance_id}>{r.host_instance_id}：{r.error}</li>
                    ))}
                  </ul>
                ),
              })
            }
            setDeployModal(false)
            loadDeploys()
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message)
          } finally {
            setDeployBusy(false)
          }
        }}
        destroyOnClose
        okText="开始下发"
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="下发前请确认控制面已构建制品；对已运行集群安全"
          description={(
            <>
              <div><code>cd deploy/node-agent && ./build.sh</code></div>
              <div style={{ marginTop: 6 }}>
                仅安装/重启 Node Agent，不启停 FE/BE、不装包、不 init 盘。监听端口请用 8091（约定 8090–8199），禁止填 9030/8030/8040 等业务端口。
              </div>
            </>
          )}
        />
        <Form form={deployForm} layout="vertical" initialValues={{ listen_port: 8091, remote_dir: '/home/sysadmin/auto-dba/node-agent', escalate: false }}>
          <Form.Item label="目标来源">
            <Select
              value={deploySource}
              onChange={(v) => {
                setDeploySource(v)
                setDeployMonSelected([])
                deployForm.setFieldsValue({ host_instance_ids: [] })
              }}
              options={[
                { value: 'monitor', label: '从监控目标（StarRocks FE/BE）批量' },
                { value: 'host', label: '从已登记主机多选' },
              ]}
            />
          </Form.Item>
          {deploySource === 'monitor' ? (
            <>
              <Form.Item label="监控任务" required>
                <Select
                  showSearch
                  allowClear
                  placeholder="选择 StarRocks 监控任务"
                  value={deployMonTask}
                  options={Array.from(new Set([
                    ...fePreview.map((p) => p.task_name),
                    ...bePreview.map((p) => p.task_name),
                  ].filter(Boolean) as string[])).map((t) => ({ value: t, label: t }))}
                  onChange={(task) => {
                    setDeployMonTask(task)
                    const eps = new Set<string>()
                    for (const p of [...fePreview, ...bePreview]) {
                      if (p.task_name !== task) continue
                      for (const e of p.endpoints || []) {
                        const h = endpointHost(e) || String(e || '').trim()
                        if (h) eps.add(h)
                      }
                    }
                    const rows = [...eps].sort().map((host) => {
                      const hn = host.toLowerCase()
                      const inst = hosts.find((x) => hostAddrs(x).includes(hn))
                      return {
                        host,
                        hostId: inst && !deployingHostIds.has(inst.id) ? inst.id : undefined,
                        name: inst?.name,
                      }
                    })
                    setDeployMonRows(rows)
                    setDeployMonSelected(rows.filter((r) => r.hostId).map((r) => r.hostId!))
                  }}
                />
              </Form.Item>
              <Form.Item
                label="节点主机"
                extra="仅「已匹配主机实例」可下发；未登记请先到实例管理登记 host 并配置 SSH"
              >
                {deployMonRows.length === 0 ? (
                  <span className="muted">请先选择监控任务</span>
                ) : (
                  <Checkbox.Group
                    style={{ width: '100%' }}
                    value={deployMonSelected}
                    onChange={(vals) => setDeployMonSelected(vals as string[])}
                  >
                    <Space direction="vertical" style={{ width: '100%' }}>
                      {deployMonRows.map((r) => (
                        <Checkbox
                          key={r.host}
                          value={r.hostId || `__miss__${r.host}`}
                          disabled={!r.hostId}
                        >
                          {r.host}
                          {r.hostId
                            ? <Tag color="blue" style={{ marginLeft: 8 }}>{r.name || r.hostId}</Tag>
                            : <Tag color="error" style={{ marginLeft: 8 }}>未登记主机实例</Tag>}
                        </Checkbox>
                      ))}
                    </Space>
                  </Checkbox.Group>
                )}
              </Form.Item>
            </>
          ) : (
            <Form.Item
              name="host_instance_ids"
              label="目标主机"
              rules={[{ required: true, type: 'array', min: 1, message: '请选择至少一台主机' }]}
              extra="须在实例管理登记类型为 host；依赖 terminal.ssh_identity_file。"
            >
              <Select
                mode="multiple"
                showSearch
                optionFilterProp="label"
                placeholder={hosts.length ? '选择一台或多台主机' : '暂无 host 实例'}
                options={hosts.map((h) => ({
                  value: h.id,
                  disabled: deployingHostIds.has(h.id),
                  label: `${h.name} — ${h.host || h.domain_name || '-'} :${h.port || 22}${deployingHostIds.has(h.id) ? '（下发中）' : ''}`,
                }))}
              />
            </Form.Item>
          )}
          <Form.Item name="escalate" label="提权执行（sudo su -）" valuePropName="checked">
            <Switch checkedChildren="开" unCheckedChildren="关" />
          </Form.Item>
          <Form.Item
            name="listen_port"
            label="监听端口"
            extra="默认 8091；勿与平台 Agent 8090 / 业务端口（9030、8030、8040 等）冲突"
            rules={[{ required: true, type: 'number', min: 1024, max: 65535, message: '端口须在 1024–65535' }]}
          >
            <InputNumber min={1024} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="remote_dir" label="远程目录">
            <Input placeholder="/home/sysadmin/auto-dba/node-agent" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="下发日志"
        open={!!deployLog}
        onCancel={() => setDeployLog(null)}
        footer={<Button onClick={() => setDeployLog(null)}>关闭</Button>}
        width={720}
      >
        {deployLog && (
          <>
            <p>
              状态
              {' '}
              <Tag color={statusColor[deployLog.status] || 'default'}>{deployLog.status}</Tag>
              {' '}
              {deployLog.base_url || ''}
            </p>
            <pre style={{ maxHeight: 420, overflow: 'auto', background: '#111', color: '#eee', padding: 12, fontSize: 12 }}>
              {deployLog.log_text || '（无日志）'}
            </pre>
          </>
        )}
      </Modal>

      <Modal
        title="登记 Node Agent"
        open={agentModal}
        onOk={async () => {
          const v = await agentForm.validateFields()
          const res = await api.post('/sr-cluster/agents', v)
          const created = res.data?.data as SrAgent | undefined
          message.success('已登记')
          setAgentModal(false)
          await loadAgents()
          if (agentBindTarget && created?.id) {
            clusterForm.setFieldValue(
              [agentBindTarget.list, agentBindTarget.index, 'agent_id'],
              created.id,
            )
          }
          setAgentBindTarget(null)
        }}
        onCancel={() => { setAgentModal(false); setAgentBindTarget(null) }}
        destroyOnClose
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="StarRocks Node Agent（默认 :8091）"
          description={(
            <>
              须部署
              {' '}
              <code>deploy/node-agent</code>
              {' '}
              并提供
              {' '}
              <code>/v1/sr/*</code>
              。配置中心「平台 Agent」(:8090) 不能用于本下拉。
            </>
          )}
        />
        <Form form={agentForm} layout="vertical">
          <Form.Item name="name" label="名称"><Input /></Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]} extra="推荐 http://主机:8091">
            <Input placeholder="http://host:8091" />
          </Form.Item>
          <Form.Item name="username" label="用户"><Input /></Form.Item>
          <Form.Item name="password" label="密码"><Input.Password /></Form.Item>
        </Form>
      </Modal>

      <Modal title="登记版本" open={releaseModal} onOk={async () => {
        const v = await releaseForm.validateFields()
        await api.post('/sr-cluster/releases', v)
        message.success('已登记'); setReleaseModal(false); loadReleases()
      }} onCancel={() => setReleaseModal(false)} destroyOnClose
      >
        <Form
          form={releaseForm}
          layout="vertical"
          initialValues={{ channel: 'stable' }}
          onValuesChange={(changed) => {
            if ('jdk_min' in changed || 'jdk_max' in changed) {
              releaseJdkTouched.current = true
            } else if ('version' in changed && !releaseJdkTouched.current) {
              const def = defaultJdkRange(changed.version)
              if (def) releaseForm.setFieldsValue(def)
            }
          }}
        >
          <Form.Item name="version" label="版本号" rules={[{ required: true }]}><Input placeholder="3.2.8" /></Form.Item>
          <Form.Item
            name="channel"
            label="发行通道"
            extra="长期支持：推荐生产长期维护线；稳定版：常规生产可用；预览版：尝鲜/验证，不建议生产。"
          >
            <Select options={[...SR_CHANNEL_OPTIONS]} />
          </Form.Item>
          <div style={{ display: 'flex', gap: 8 }}>
            <Form.Item name="jdk_min" label="JDK 最低" style={{ flex: 1 }} rules={[{ pattern: /^\d*$/, message: '填主版本号，如 11' }]}>
              <Input placeholder="11" />
            </Form.Item>
            <Form.Item name="jdk_max" label="JDK 最高" style={{ flex: 1 }} rules={[{ pattern: /^\d*$/, message: '填主版本号，如 17；留空不限' }]}>
              <Input placeholder="留空不限" />
            </Form.Item>
          </div>
          <div style={{ marginTop: -12, marginBottom: 16, fontSize: 12, color: '#888' }}>
            按版本号自动填写：2.x 8–11；3.0–3.4 11–17；3.5+ 17+，可手改。升级时节点 JDK 不满足会从 OSS 安装 jdk-11 / jdk-21。
          </div>
          <Form.Item name="remark" label="备注"><Input /></Form.Item>
        </Form>
      </Modal>

      <Modal title={artifactModal ? `分包 · ${artifactModal.version}` : '分包'} open={!!artifactModal} onCancel={() => setArtifactModal(null)} footer={null} destroyOnClose width={640}>
        {artifactModal && (
          <Space direction="vertical" style={{ width: '100%' }} size="middle">
            <Alert
              type="info"
              showIcon
              message="可从 OSS 同步 object_key；脚本上传的包也可在此补全或修改 sha256"
              action={(
                <Button size="small" type="primary" loading={syncBusyId === artifactModal.id} onClick={() => { void syncReleaseFromOSS(artifactModal) }}>
                  从 OSS 同步
                </Button>
              )}
            />
            {(artifactModal.artifacts || []).length > 0 && (
              <div style={{ fontSize: 12, color: '#666' }}>
                当前分包：{(artifactModal.artifacts || []).map((a) => `${a.role}:${a.sha256 ? '已有 sha256' : '缺 sha256'}`).join('；')}
              </div>
            )}
            <Form
              form={artifactForm}
              layout="vertical"
              initialValues={{ role: 'fe', format: 'tar.gz' }}
              onValuesChange={(changed) => {
                if (changed.role && artifactModal) {
                  const hit = (artifactModal.artifacts || []).find((a) => a.role === changed.role)
                  if (hit) {
                    artifactForm.setFieldsValue({
                      object_key: hit.object_key,
                      filename: hit.filename || hit.object_key?.split('/').pop(),
                      sha256: hit.sha256 || '',
                      format: hit.format || 'tar.gz',
                    })
                  }
                }
              }}
            >
              <Form.Item name="role" label="角色" rules={[{ required: true }]}>
                <Select options={[{ value: 'fe' }, { value: 'be' }]} />
              </Form.Item>
              <Form.Item name="filename" label="文件名"><Input placeholder="StarRocks-x.y.z-fe.tar.gz" /></Form.Item>
              <Form.Item name="object_key" label="Object Key（可空，按约定自动生成）"><Input /></Form.Item>
              <Form.Item
                name="sha256"
                label="SHA256"
                extra="脚本上传后把本机算出的 sha256 粘贴到这里保存即可；也可在版本展开行直接改"
                rules={[{ pattern: /^([a-fA-F0-9]{64})?$/, message: '须为 64 位十六进制' }]}
              >
                <Input placeholder="64 位 hex" />
              </Form.Item>
              <Button type="primary" onClick={async () => {
                const v = await artifactForm.validateFields()
                await api.post(`/sr-cluster/releases/${artifactModal.id}/artifacts`, v)
                message.success('已保存'); loadReleases()
                const refreshed = (await api.get('/sr-cluster/releases')).data?.data as SrRelease[] | undefined
                const hit = (refreshed || []).find((x) => x.id === artifactModal.id)
                if (hit) setArtifactModal(hit)
              }}
              >
                保存元数据
              </Button>
            </Form>
            <Upload
              maxCount={1}
              customRequest={async (opt) => {
                const role = artifactForm.getFieldValue('role') || 'fe'
                const fd = new FormData()
                fd.append('role', role)
                fd.append('file', opt.file as File)
                try {
                  await api.post(`/sr-cluster/releases/${artifactModal.id}/artifacts/upload`, fd, {
                    headers: { 'Content-Type': 'multipart/form-data' },
                    timeout: 3600000,
                  })
                  message.success('上传成功'); loadReleases(); opt.onSuccess?.({})
                } catch (e: any) {
                  message.error(e?.response?.data?.message || e.message)
                  opt.onError?.(e)
                }
              }}
            >
              <Button>上传到 OSS（自动算 sha256 并登记）</Button>
            </Upload>
          </Space>
        )}
      </Modal>

      <Modal
        title="集群升级（提交审批）"
        open={upgradeModal}
        confirmLoading={upgradeBusy || upgradePrecheckBusy}
        okButtonProps={{ disabled: upgradePrecheckBusy || (upgradePrecheck != null && !upgradePrecheck.ok) }}
        onOk={async () => {
          if (!selected) return
          const v = await upgradeForm.validateFields()
          setUpgradeBusy(true)
          try {
            const pre = await api.post(`/sr-cluster/clusters/${selected.id}/upgrade/precheck`, { release_id: v.release_id })
            const data = pre.data?.data
            setUpgradePrecheck(data || null)
            if (!data?.ok) {
              Modal.error({
                title: '升级前检查未通过',
                width: 640,
                content: (
                  <div>
                    <div style={{ marginBottom: 8 }}>{data?.summary || '请修复下列项后再提交工单'}</div>
                    <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 280, overflow: 'auto' }}>
                      {(data?.items || []).filter((it: { ok: boolean }) => !it.ok).map((it: { name: string; message?: string }) => (
                        <li key={it.name} style={{ marginBottom: 4 }}>
                          <b>{it.name.startsWith('jdk_') ? 'JDK' : it.name}</b>
                          {it.message ? `：${it.message}` : ''}
                        </li>
                      ))}
                    </ul>
                  </div>
                ),
              })
              return
            }
            await submitSrTicket(selected.id, 'upgrade', { release_id: v.release_id }, v.assigned_to, v.description)
            setUpgradeModal(false)
          } catch (e: any) {
            const msg = e?.response?.data?.message || e.message || '提交失败'
            Modal.error({ title: '工单未创建', width: 560, content: <div style={{ whiteSpace: 'pre-wrap' }}>{msg}</div> })
          } finally {
            setUpgradeBusy(false)
          }
        }}
        onCancel={() => { setUpgradeModal(false); void ticketApprovers.load(undefined) }}
        destroyOnClose
        okText="检查通过后提交工单"
      >
        <Form form={upgradeForm} layout="vertical">
          <Form.Item name="release_id" label="目标版本" rules={[{ required: true }]}>
            <Select
              options={releases.filter((r) => r.enabled !== false).map((r) => ({ value: r.id, label: r.version }))}
              onChange={async (rid) => {
                if (!selected || !rid) {
                  setUpgradePrecheck(null)
                  return
                }
                await runUpgradePrecheck(selected.id, rid)
              }}
            />
          </Form.Item>
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="须预检通过才可建单；审批通过后自动滚动升级（全部 BE → backends → FE follower → FE leader）。"
          />
          {upgradePrecheckBusy && <Alert type="info" showIcon message="正在执行升级前检查…" style={{ marginBottom: 12 }} />}
          {upgradePrecheck && (
            <Alert
              type={upgradePrecheck.ok
                ? ((upgradePrecheck.items || []).some((it) => it.level === 'warn') ? 'warning' : 'success')
                : 'error'}
              showIcon
              style={{ marginBottom: 8 }}
              message={upgradePrecheck.summary || (upgradePrecheck.ok ? '检查通过' : '检查未通过')}
              description={(
                <ul style={{ margin: '8px 0 0', paddingLeft: 18, maxHeight: 220, overflow: 'auto' }}>
                  {(upgradePrecheck.items || []).map((it) => (
                    <li
                      key={it.name}
                      style={{ color: !it.ok ? '#a61d24' : it.level === 'warn' ? '#ad6800' : undefined }}
                    >
                      {!it.ok ? '✗' : it.level === 'warn' ? '!' : '✓'}
                      {' '}
                      {it.name.startsWith('jdk_') ? 'JDK' : it.name}
                      {it.message ? ` — ${it.message}` : ''}
                    </li>
                  ))}
                </ul>
              )}
            />
          )}
          {ticketApproverField}
          <Form.Item name="description" label="事由"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title={ticketDraft?.title || '提交审批'}
        open={!!ticketDraft}
        confirmLoading={ticketBusy}
        okText="提交工单"
        okButtonProps={{ danger: ticketDraft?.action === 'cluster_reinit' || ticketDraft?.action === 'remove_node' }}
        onCancel={() => { setTicketDraft(null); ticketForm.resetFields(); void ticketApprovers.load(undefined) }}
        onOk={async () => {
          if (!ticketDraft) return
          const v = await ticketForm.validateFields()
          setTicketBusy(true)
          try {
            const payload = { ...ticketDraft.payload }
            if (ticketDraft.action === 'remove_node' && v.force_drop) {
              payload.force_drop = true
            }
            await submitSrTicket(ticketDraft.clusterId, ticketDraft.action, payload, v.assigned_to, v.description)
            setTicketDraft(null)
            ticketForm.resetFields()
          } catch (e: any) {
            message.error(e?.response?.data?.message || e.message || '提交失败')
          } finally {
            setTicketBusy(false)
          }
        }}
        destroyOnClose
      >
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} message={ticketDraft?.summary || ''} />
        <Form form={ticketForm} layout="vertical">
          {ticketDraft?.action === 'remove_node' && ticketDraft.nodeComponent === 'be' && (
            <Form.Item
              name="force_drop"
              valuePropName="checked"
              extra="默认 DECOMMISSION 在线缩容（不停服，迁移期间可恢复）；勾选则直接 DROP BACKEND，BE 立即移出集群且不可恢复"
            >
              <Checkbox>强制 DROP（force_drop）</Checkbox>
            </Form.Item>
          )}
          {ticketApproverField}
          <Form.Item name="description" label="事由"><Input.TextArea rows={2} /></Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`配置 · ${selected?.name || ''}`}
        open={configModal}
        width={960}
        onCancel={() => setConfigModal(false)}
        destroyOnClose
        footer={(
          <Space wrap>
            <Button onClick={() => setConfigModal(false)}>取消</Button>
            <Button
              loading={configBusy}
              disabled={!configSelectedNodeId}
              onClick={async () => {
                if (!selected || !configSelectedNodeId) return
                setConfigBusy(true)
                try {
                  const res = await api.put(
                    `/sr-cluster/clusters/${selected.id}/nodes/${configSelectedNodeId}/config`,
                    { content: configEditBody },
                  )
                  message.success('已保存该节点配置')
                  const updated = res.data?.data
                  setConfigPreviews((prev) => prev.map((p) => (
                    p.node_id === configSelectedNodeId
                      ? {
                        ...p,
                        content: updated?.content || p.content,
                        editable: updated?.editable || configEditBody,
                        source: updated?.source || 'node',
                        has_draft: true,
                      }
                      : p
                  )))
                  if (updated?.editable) setConfigEditBody(updated.editable)
                } catch (e: any) {
                  message.error(e?.response?.data?.message || e.message)
                } finally {
                  setConfigBusy(false)
                }
              }}
            >
              保存当前节点
            </Button>
            <Button
              type="primary"
              loading={configBusy}
              disabled={!configNodeIds.length}
              onClick={async () => {
                if (!selected) return
                if (!configNodeIds.length) {
                  message.warning('请至少勾选一个下发节点')
                  return
                }
                setConfigBusy(true)
                try {
                  const res = await api.post(`/sr-cluster/clusters/${selected.id}/conf-patch`, {
                    source_node_id: configSelectedNodeId,
                    content: configEditBody,
                    node_ids: configNodeIds,
                    dry_run: true,
                  })
                  setConfigPatchPlan(res.data?.data || null)
                } catch (e: any) {
                  message.error(e?.response?.data?.message || e.message)
                } finally {
                  setConfigBusy(false)
                }
              }}
            >
              下发到勾选节点…
            </Button>
          </Space>
        )}
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={(
            <>
              左侧选节点查看已解析参数（非 {'{{模板}}'}），编辑后「下发」。一次只能下发同一角色（FE 或 BE）：当前节点下发编辑后的全文，
              其他勾选节点只同步本次改动的参数（frontend_address / priority_networks 等节点专属参数不同步）。
              meta_dir / storage_root_path 由平台按 data_root 锁定追加。
            </>
          )}
        />
        <div style={{ display: 'flex', gap: 16, alignItems: 'stretch', minHeight: 420 }}>
          <div style={{ width: 280, flexShrink: 0 }}>
            <div style={{ marginBottom: 8, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span>节点</span>
              <Button
                type="link"
                size="small"
                onClick={() => {
                  const same = configPreviews.filter((p) => p.role === configCurRole).map((p) => p.node_id)
                  setConfigNodeIds(configNodeIds.length === same.length ? [configSelectedNodeId] : same)
                }}
              >
                {configNodeIds.length === configPreviews.filter((p) => p.role === configCurRole).length
                  ? '只选当前'
                  : `全选 ${configCurRole.toUpperCase()}`}
              </Button>
            </div>
            <Table
              size="small"
              rowKey="node_id"
              pagination={false}
              dataSource={configPreviews}
              scroll={{ y: 360 }}
              onRow={(row) => ({
                onClick: () => selectConfigNode(row.node_id),
                style: {
                  cursor: 'pointer',
                  background: row.node_id === configSelectedNodeId ? 'var(--ant-color-primary-bg, #e6f4ff)' : undefined,
                },
              })}
              rowSelection={{
                selectedRowKeys: configNodeIds,
                hideSelectAll: true,
                onChange: (keys) => setConfigNodeIds(keys as string[]),
                getCheckboxProps: (row) => ({
                  disabled: row.role !== configCurRole,
                  title: row.role !== configCurRole ? '不同角色不能同时下发，请点击该行切换后单独下发' : undefined,
                }),
              }}
              columns={[
                {
                  title: '角色',
                  dataIndex: 'role',
                  width: 48,
                  render: (r: string) => <Tag>{r}</Tag>,
                },
                {
                  title: 'Host',
                  dataIndex: 'host',
                  ellipsis: true,
                },
                {
                  title: '来源',
                  dataIndex: 'source',
                  width: 72,
                  render: (s: string, row) => (
                    <Tag color={row.has_draft || s === 'node' ? 'blue' : 'default'}>{s || '-'}</Tag>
                  ),
                },
              ]}
            />
            <div style={{ marginTop: 12 }}>
              <Checkbox checked={configRestart} onChange={(e) => setConfigRestart(e.target.checked)}>
                下发后滚动重启勾选节点
              </Checkbox>
            </div>
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            {(() => {
              const cur = configPreviews.find((p) => p.node_id === configSelectedNodeId)
              if (!cur || !selected) return <Alert type="warning" message="暂无节点" />
              const v = cur.vars || {}
              return (
                <Tabs
                  size="small"
                  activeKey={configTab}
                  onChange={(k) => setConfigTab(k as 'edit' | 'history')}
                  style={{ marginTop: -8 }}
                  items={[
                    {
                      key: 'edit',
                      label: '编辑',
                      children: (
                        <>
                          <Descriptions size="small" column={2} style={{ marginBottom: 8 }}>
                            <Descriptions.Item label="节点">{cur.role} · {cur.host}</Descriptions.Item>
                            <Descriptions.Item label="远端">{cur.remote_path || '-'}</Descriptions.Item>
                            <Descriptions.Item label="来源">{cur.source}{cur.has_draft ? '（已改）' : ''}</Descriptions.Item>
                            <Descriptions.Item label="关键参数">
                              {cur.role === 'be'
                                ? `be_port=${v.BEPort ?? '-'} be_http=${v.HTTPPort ?? '-'} hb=${v.HeartbeatPort ?? '-'} brpc=${v.BRPCPort ?? '-'} starlet=${v.StarletPort ?? '-'} net=${v.PriorityNetworks || '-'}`
                                : `http=${v.HTTPPort ?? '-'} rpc=${v.RPCPort ?? '-'} query=${v.QueryPort ?? '-'} edit=${v.EditPort ?? '-'} net=${v.PriorityNetworks || '-'}`}
                            </Descriptions.Item>
                          </Descriptions>
                          <Input.TextArea
                            value={configEditBody}
                            onChange={(e) => setConfigEditBody(e.target.value)}
                            rows={16}
                            style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 12 }}
                          />
                        </>
                      ),
                    },
                    {
                      key: 'history',
                      label: '下发记录',
                      children: (
                        <ConfHistoryPanel
                          clusterId={selected.id}
                          node={{ node_id: cur.node_id, host: cur.host, role: cur.role }}
                          active={configTab === 'history'}
                          onRolledBack={(changeId) => {
                            setConfigModal(false)
                            afterChange(changeId)
                          }}
                        />
                      ),
                    },
                  ]}
                />
              )
            })()}
            <div style={{ marginTop: 12, display: configTab === 'edit' ? undefined : 'none' }}>
              <Button type="link" size="small" style={{ padding: 0 }} onClick={() => setConfigShowTpl(!configShowTpl)}>
                {configShowTpl ? '收起角色模板（高级）' : '编辑角色模板（高级，含 {{变量}}）'}
              </Button>
              {configShowTpl && (
                <div style={{ marginTop: 8 }}>
                  <Tabs
                    size="small"
                    items={[
                      {
                        key: 'fe',
                        label: '集群 fe 草稿',
                        children: (
                          <Input.TextArea value={configFe} onChange={(e) => setConfigFe(e.target.value)} rows={8} style={{ fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12 }} />
                        ),
                      },
                      {
                        key: 'be',
                        label: '集群 be 草稿',
                        children: (
                          <Input.TextArea value={configBe} onChange={(e) => setConfigBe(e.target.value)} rows={8} style={{ fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 12 }} />
                        ),
                      },
                    ]}
                  />
                  <Button
                    size="small"
                    style={{ marginTop: 8 }}
                    loading={configBusy}
                    onClick={async () => {
                      if (!selected) return
                      setConfigBusy(true)
                      try {
                        await api.put(`/sr-cluster/clusters/${selected.id}/configs`, { fe: configFe, be: configBe })
                        const prevRes = await api.get(`/sr-cluster/clusters/${selected.id}/configs/preview`)
                        const list = prevRes.data?.data || []
                        setConfigPreviews(list)
                        const cur = list.find((p: { node_id: string }) => p.node_id === configSelectedNodeId) || list[0]
                        if (cur) {
                          setConfigSelectedNodeId(cur.node_id)
                          setConfigEditBody(cur.editable || '')
                        }
                        message.success('集群角色草稿已保存，预览已刷新')
                      } catch (e: any) {
                        message.error(e?.response?.data?.message || e.message)
                      } finally {
                        setConfigBusy(false)
                      }
                    }}
                  >
                    保存角色草稿并刷新预览
                  </Button>
                </div>
              )}
            </div>
          </div>
        </div>
      </Modal>

      {selected && (
        <ConfApplyConfirmModal
          open={!!configPatchPlan}
          clusterId={selected.id}
          nodes={(configPatchPlan?.nodes || []).map((n) => ({ node_id: n.node_id, host: n.host, role: n.role }))}
          contents={Object.fromEntries((configPatchPlan?.nodes || []).map((n) => [n.node_id, n.content]))}
          summary={configPatchPlan && (
            <Alert
              type={configPatchPlan.changes.length ? 'success' : 'warning'}
              showIcon
              style={{ marginBottom: 12 }}
              message={configPatchPlan.changes.length
                ? `本次改动（相对${configPatchPlan.base_label}）：${keySummary(configPatchPlan.changes, 6)}`
                : `相对${configPatchPlan.base_label}没有参数改动`}
              description={configPatchPlan.nodes.length > 1
                ? `当前节点下发编辑后的全文；其余 ${configPatchPlan.nodes.length - 1} 个 ${configPatchPlan.role.toUpperCase()} 节点只同步上述参数${configPatchPlan.local_keys?.length ? `，节点专属参数 ${configPatchPlan.local_keys.join(', ')} 不同步` : ''}。`
                : undefined}
            />
          )}
          restart={configRestart}
          onCancel={() => setConfigPatchPlan(null)}
          onSubmit={async (nodeIds, restart) => {
            try {
              const res = await api.post(`/sr-cluster/clusters/${selected.id}/conf-patch`, {
                source_node_id: configSelectedNodeId,
                content: configEditBody,
                node_ids: nodeIds,
                restart,
                dry_run: false,
              })
              message.success('已提交配置下发')
              setConfigPatchPlan(null)
              setConfigModal(false)
              afterChange(res.data?.data?.change_id)
            } catch (e: any) {
              message.error(e?.response?.data?.message || e.message)
            }
          }}
        />
      )}
      <ChangeConfRollbackModal
        clusterId={changeConfRollback?.cluster_id || ''}
        change={changeConfRollback}
        onCancel={() => setChangeConfRollback(null)}
        onDone={(id) => {
          setChangeConfRollback(null)
          afterChange(id)
        }}
      />
    </div>
  )
}
