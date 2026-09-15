import { BookOpen, Database, History, LayoutDashboard, MoreHorizontal, Search, Settings, Star, Zap } from 'lucide-react'
import { Button } from './ui/button'
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
}) {
  return (
    <header className="flex flex-wrap items-center gap-1.5 border-b bg-card px-2.5 py-2">
      <span className="flex items-center gap-1.5 text-sm font-bold">
        <Database className="h-4 w-4" /> pglight
      </span>
      <span className="flex-1" />
      {/* Object search sits at the right, next to History */}
      <Button size="sm" variant="outline" onClick={props.onSearch} className="min-w-[220px] justify-start font-normal text-muted-foreground">
        <Search /> Search objects… <kbd className="ml-auto font-mono text-[10px] text-muted-foreground/70">Ctrl K</kbd>
      </Button>
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
    </header>
  )
}
