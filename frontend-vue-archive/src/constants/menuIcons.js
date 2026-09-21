import {
  Clock,
  Coin,
  Connection,
  Cpu,
  DataBoard,
  Document,
  Download,
  Edit,
  Files,
  Folder,
  Grid,
  Histogram,
  Key,
  List,
  Location,
  Lock,
  Menu as MenuIcon,
  Monitor,
  Odometer,
  Plus,
  Refresh,
  Search,
  Setting,
  Share,
  Sort,
  Tickets,
  Upload,
  User,
  UserFilled,
  Warning,
} from '@element-plus/icons-vue'

const defs = [
  { value: 'dashboard', label: 'dashboard', icon: Odometer },
  { value: 'odometer', label: 'odometer', icon: Odometer },
  { value: 'databoard', label: 'databoard', icon: DataBoard },
  { value: 'tickets', label: 'tickets', icon: Tickets },
  { value: 'list', label: 'list', icon: List },
  { value: 'document', label: 'document', icon: Document },
  { value: 'download', label: 'download', icon: Download },
  { value: 'edit', label: 'edit', icon: Edit },
  { value: 'share', label: 'share', icon: Share },
  { value: 'key', label: 'key', icon: Key },
  { value: 'plus', label: 'plus', icon: Plus },
  { value: 'refresh', label: 'refresh', icon: Refresh },
  { value: 'connection', label: 'connection', icon: Connection },
  { value: 'upload', label: 'upload', icon: Upload },
  { value: 'sort', label: 'sort', icon: Sort },
  { value: 'lock', label: 'lock', icon: Lock },
  { value: 'clock', label: 'clock', icon: Clock },
  { value: 'folder', label: 'folder', icon: Folder },
  { value: 'location', label: 'location', icon: Location },
  { value: 'monitor', label: 'monitor', icon: Monitor },
  { value: 'search', label: 'search', icon: Search },
  { value: 'cpu', label: 'cpu', icon: Cpu },
  { value: 'grid', label: 'grid', icon: Grid },
  { value: 'files', label: 'files', icon: Files },
  { value: 'histogram', label: 'histogram', icon: Histogram },
  { value: 'coin', label: 'coin', icon: Coin },
  { value: 'setting', label: 'setting', icon: Setting },
  { value: 'menu', label: 'menu', icon: MenuIcon },
  { value: 'user', label: 'user', icon: User },
  { value: 'user-filled', label: 'user-filled', icon: UserFilled },
  { value: 'warning', label: 'warning', icon: Warning },
]

const byKey = {}
for (const d of defs) {
  byKey[d.value] = d.icon
}

export const MENU_ICON_OPTIONS = defs

export function resolveMenuIcon(name) {
  if (!name) return null
  const key = String(name).trim().toLowerCase().replace(/_/g, '-')
  return byKey[key] || null
}
