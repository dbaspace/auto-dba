import React from 'react'
import {
  AppstoreOutlined,
  ClockCircleOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DownloadOutlined,
  EditOutlined,
  FileOutlined,
  FileTextOutlined,
  FolderOutlined,
  FundOutlined,
  KeyOutlined,
  LockOutlined,
  MenuOutlined,
  MonitorOutlined,
  OrderedListOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  SettingOutlined,
  ShareAltOutlined,
  SortAscendingOutlined,
  SwapOutlined,
  TableOutlined,
  UserOutlined,
  WarningOutlined,
  EnvironmentOutlined,
  ProfileOutlined,
} from '@ant-design/icons'

const byKey: Record<string, React.ComponentType> = {
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
  files: FileOutlined,
  histogram: FundOutlined,
  coin: DatabaseOutlined,
  setting: SettingOutlined,
  menu: MenuOutlined,
  user: UserOutlined,
  'user-filled': UserOutlined,
  warning: WarningOutlined,
  swap: SwapOutlined,
  table: TableOutlined,
}

export function resolveMenuIcon(name?: string): React.ReactNode {
  if (!name) return null
  const key = String(name).trim().toLowerCase().replace(/_/g, '-')
  const Comp = byKey[key]
  return Comp ? <Comp /> : null
}

export default function MenuIcon({ name }: { name?: string }) {
  return <>{resolveMenuIcon(name)}</>
}
