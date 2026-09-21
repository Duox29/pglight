import { BookOpen, Database, KeyRound, MoreHorizontal, Power, Search } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './ui/dropdown-menu'
import type { SideView } from '@/types'
import { WORKSPACE_VIEW_META } from '@/lib/workspace'

export interface ConnFields {
  host: string
  port: string
  user: string
  password: string
  dbname: string
  sslmode: string
  profileId?: string
}

export function ConnectionBar(props: {
  connected: boolean
  onSearch: () => void
  searchShortcut?: string
  quickAccess: SideView[]
  onWorkspace: (v: SideView) => void
  onDocs: () => void
  onShutdown: () => void
}) {
  return (
    <header className="flex flex-wrap items-center gap-1.5 border-b bg-card px-2.5 py-2">
      <span className="flex items-center gap-1.5 text-sm font-bold">
        <Database className="h-4 w-4" /> pglight
      </span>
      {/* Center slot: takes remaining width so the search box sits mid-header */}
      <div className="flex min-w-0 flex-1 justify-center px-2">
        <Button size="sm" variant="outline" onClick={props.onSearch} className="w-full max-w-[420px] justify-start font-normal text-muted-foreground">
          <Search /> Search objects / Command Palette <kbd className="ml-auto font-mono text-[10px] text-muted-foreground/70">{props.searchShortcut ?? 'Mod+K'}</kbd>
        </Button>
      </div>
      {/* These two buttons are configured in Workspace → Quick Access. */}
      {props.quickAccess.map((view, index) => {
        const meta = WORKSPACE_VIEW_META[view]
        const Icon = meta.icon
        return <Button key={`${view}-${index}`} size="sm" variant="ghost" onClick={() => props.onWorkspace(view)}><Icon /> {view === 'server' ? 'Dashboard' : meta.label}</Button>
      })}
      {/* Utility: workspace navigation and docs stay in one overflow menu. */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="ghost" aria-label="More tools">
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => props.onWorkspace('connections')}>
            <KeyRound /> Connections
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => props.onWorkspace('quick-access')}>
            <MoreHorizontal /> Workspace
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={props.onDocs}>
            <BookOpen /> Docs
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Tip content="Close all connections and stop the server">
        <Button size="sm" variant="ghost" aria-label="Shutdown" onClick={props.onShutdown} className="text-destructive hover:bg-destructive/10 hover:text-destructive">
          <Power />
        </Button>
      </Tip>
    </header>
  )
}
