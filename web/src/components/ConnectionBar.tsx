import { BookOpen, Database, History, LayoutDashboard, MoreHorizontal, Power, Search, Settings, Star, Zap } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './ui/dropdown-menu'

export interface ConnFields {
  host: string
  port: string
  user: string
  password: string
  dbname: string
  sslmode: string
}

export function ConnectionBar(props: {
  connected: boolean
  onSearch: () => void
  onPanel: (v: 'history' | 'snippets' | 'aliases' | 'server' | 'settings') => void
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
          <Search /> Search objects… <kbd className="ml-auto font-mono text-[10px] text-muted-foreground/70">Ctrl K</kbd>
        </Button>
      </div>
      {/* Secondary: workspace tools stay visible */}
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('history')}>
        <History /> History
      </Button>
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('server')}>
        <LayoutDashboard /> Dashboard
      </Button>
      {/* Utility: one overflow menu instead of three peer buttons */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="ghost" aria-label="More tools">
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => props.onPanel('snippets')}>
            <Star /> Snippets
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => props.onPanel('aliases')}>
            <Zap /> Aliases
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => props.onPanel('settings')}>
            <Settings /> Settings
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
