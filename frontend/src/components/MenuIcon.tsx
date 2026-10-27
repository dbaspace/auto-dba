import React from 'react'
import {
  AppstoreOutlined,
  AudioMutedOutlined,
  AuditOutlined,
  BellOutlined,
  CarryOutOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloudOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  CodeOutlined,
  GlobalOutlined,
  ToolOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  FileOutlined,
  FileTextOutlined,
  FolderOutlined,
  FundOutlined,
  KeyOutlined,
  LockOutlined,
  LoginOutlined,
  MonitorOutlined,
  OrderedListOutlined,
  PauseCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SearchOutlined,
  SettingOutlined,
  ShareAltOutlined,
  SortAscendingOutlined,
  StopOutlined,
  SwapOutlined,
  TableOutlined,
  TagsOutlined,
  TeamOutlined,
  UserOutlined,
  WarningOutlined,
  EnvironmentOutlined,
  ProfileOutlined,
} from '@ant-design/icons'

const byKey: Record<string, React.ComponentType<{ style?: React.CSSProperties }>> = {
  dashboard: DashboardOutlined,
  odometer: DashboardOutlined,
  databoard: FundOutlined,
  tickets: ProfileOutlined,
  list: OrderedListOutlined,
  document: FileTextOutlined,
  download: DownloadOutlined,
  edit: EditOutlined,
  share: ShareAltOutlined,
  key: KeyOutlined,
  plus: PlusOutlined,
  refresh: ReloadOutlined,
  connection: ClusterOutlined,
  upload: CloudUploadOutlined,
  sort: SortAscendingOutlined,
  lock: LockOutlined,
  clock: ClockCircleOutlined,
  folder: FolderOutlined,
  location: EnvironmentOutlined,
  monitor: MonitorOutlined,
  search: SearchOutlined,
  cpu: MonitorOutlined,
  grid: AppstoreOutlined,
  tag: TagsOutlined,
  files: FileOutlined,
  histogram: FundOutlined,
  coin: DatabaseOutlined,
  setting: SettingOutlined,
  menu: AppstoreOutlined,
  user: UserOutlined,
  'user-filled': UserOutlined,
  team: TeamOutlined,
  login: LoginOutlined,
  safety: SafetyCertificateOutlined,
  audit: AuditOutlined,
  warning: WarningOutlined,
  bell: BellOutlined,
  robot: RobotOutlined,
  notification: BellOutlined,
  mute: AudioMutedOutlined,
  'audio-muted': AudioMutedOutlined,
  stop: StopOutlined,
  pause: PauseCircleOutlined,
  check: CheckOutlined,
  delete: DeleteOutlined,
  swap: SwapOutlined,
  table: TableOutlined,
  'todo-list': CarryOutOutlined,
  cluster: ClusterOutlined,
  cloud: CloudOutlined,
  server: ClusterOutlined,
  appstore: AppstoreOutlined,
  global: GlobalOutlined,
  code: CodeOutlined,
  tool: ToolOutlined,
  database: DatabaseOutlined,
  mysql: DatabaseOutlined,
  redis: DatabaseOutlined,
}

export type MenuIconTone = { color: string; background: string }

const tones: Record<string, MenuIconTone> = {
  user: { color: '#1677ff', background: '#e6f4ff' },
  'user-filled': { color: '#1677ff', background: '#e6f4ff' },
  team: { color: '#722ed1', background: '#f9f0ff' },
  menu: { color: '#13c2c2', background: '#e6fffb' },
  login: { color: '#fa8c16', background: '#fff7e6' },
  safety: { color: '#52c41a', background: '#f6ffed' },
  audit: { color: '#eb2f96', background: '#fff0f6' },
  dashboard: { color: '#1677ff', background: '#e6f4ff' },
  odometer: { color: '#1677ff', background: '#e6f4ff' },
  databoard: { color: '#2f54eb', background: '#f0f5ff' },
  tickets: { color: '#722ed1', background: '#f9f0ff' },
  list: { color: '#595959', background: '#f5f5f5' },
  document: { color: '#13c2c2', background: '#e6fffb' },
  download: { color: '#1677ff', background: '#e6f4ff' },
  edit: { color: '#fa8c16', background: '#fff7e6' },
  share: { color: '#2f54eb', background: '#f0f5ff' },
  key: { color: '#faad14', background: '#fffbe6' },
  plus: { color: '#52c41a', background: '#f6ffed' },
  refresh: { color: '#13c2c2', background: '#e6fffb' },
  connection: { color: '#1677ff', background: '#e6f4ff' },
  upload: { color: '#722ed1', background: '#f9f0ff' },
  sort: { color: '#2f54eb', background: '#f0f5ff' },
  lock: { color: '#fa541c', background: '#fff2e8' },
  clock: { color: '#fa8c16', background: '#fff7e6' },
  folder: { color: '#faad14', background: '#fffbe6' },
  location: { color: '#eb2f96', background: '#fff0f6' },
  monitor: { color: '#1677ff', background: '#e6f4ff' },
  search: { color: '#13c2c2', background: '#e6fffb' },
  cpu: { color: '#722ed1', background: '#f9f0ff' },
  grid: { color: '#2f54eb', background: '#f0f5ff' },
  tag: { color: '#eb2f96', background: '#fff0f6' },
  files: { color: '#595959', background: '#f5f5f5' },
  histogram: { color: '#1677ff', background: '#e6f4ff' },
  coin: { color: '#faad14', background: '#fffbe6' },
  setting: { color: '#8c8c8c', background: '#f5f5f5' },
  warning: { color: '#fa541c', background: '#fff2e8' },
  bell: { color: '#fa541c', background: '#fff2e8' },
  robot: { color: '#1677ff', background: '#e6f4ff' },
  notification: { color: '#fa541c', background: '#fff2e8' },
  mute: { color: '#8c8c8c', background: '#f5f5f5' },
  'audio-muted': { color: '#8c8c8c', background: '#f5f5f5' },
  stop: { color: '#fa541c', background: '#fff2e8' },
  pause: { color: '#faad14', background: '#fffbe6' },
  check: { color: '#52c41a', background: '#f6ffed' },
  delete: { color: '#ff4d4f', background: '#fff1f0' },
  swap: { color: '#13c2c2', background: '#e6fffb' },
  table: { color: '#1677ff', background: '#e6f4ff' },
  'todo-list': { color: '#52c41a', background: '#f6ffed' },
}

const defaultTone: MenuIconTone = { color: '#6b7280', background: '#f5f5f5' }

function normalizeIconKey(name?: string): string {
  return String(name || '')
    .trim()
    .toLowerCase()
    .replace(/_/g, '-')
}

export function menuIconTone(name?: string): MenuIconTone {
  const key = normalizeIconKey(name)
  return (key && tones[key]) || defaultTone
}

export function resolveMenuIcon(name?: string, color?: string): React.ReactNode {
  if (!name) return null
  const key = normalizeIconKey(name)
  const Comp = byKey[key]
  if (!Comp) return null
  const style = color ? { color } : undefined
  return <Comp style={style} />
}

export default function MenuIcon({ name }: { name?: string }) {
  const tone = menuIconTone(name)
  return <>{resolveMenuIcon(name, tone.color)}</>
}
