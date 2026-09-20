import type { LucideIcon } from 'lucide-react'
import { Activity, Database, FileClock, Keyboard, LayoutDashboard, List, Settings, SlidersHorizontal, Terminal, Zap } from 'lucide-react'
import type { SideView } from '@/types'

export const WORKSPACE_VIEW_META: Record<SideView, { label: string; icon: LucideIcon }> = {
  history: { label: 'History', icon: FileClock },
  snippets: { label: 'Snippets', icon: List },
  aliases: { label: 'Aliases', icon: Zap },
  server: { label: 'Server', icon: Database },
  activity: { label: 'Activity', icon: Activity },
  locks: { label: 'Locks', icon: SlidersHorizontal },
  stats: { label: 'Stats', icon: LayoutDashboard },
  settings: { label: 'Settings', icon: Settings },
  shortcuts: { label: 'Shortcuts', icon: Keyboard },
  logs: { label: 'Logs', icon: Terminal },
  'quick-access': { label: 'Quick Access', icon: Zap },
}

export const WORKSPACE_VIEWS: SideView[] = [
  'history', 'snippets', 'aliases', 'server', 'activity', 'locks', 'stats',
  'settings', 'shortcuts', 'logs', 'quick-access',
]

export const QUICK_ACCESS_VIEWS = WORKSPACE_VIEWS.filter((view) => view !== 'quick-access')
export const DEFAULT_QUICK_ACCESS: SideView[] = ['history', 'server']
