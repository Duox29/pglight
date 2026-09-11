import { BookOpen, Database, Search, History, Star, LayoutDashboard, Settings } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'

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
  onPanel: (v: 'history' | 'snippets' | 'server' | 'settings') => void
  onDocs: () => void
}) {
  return (
    <header className="flex flex-wrap items-center gap-1.5 border-b bg-card px-2.5 py-2">
      <span className="flex items-center gap-1.5 text-sm font-bold">
        <Database className="h-4 w-4" /> pglight
      </span>
      <span className="flex-1" />
      <Button size="sm" variant="outline" onClick={props.onSearch}>
        <Search /> <span className="hidden xl:inline">Search (Ctrl+K)</span>
      </Button>
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('history')}>
        <History /> History
      </Button>
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('snippets')}>
        <Star /> Snippets
      </Button>
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('server')}>
        <LayoutDashboard /> Dashboard
      </Button>
      <Tip content="Settings">
        <Button size="sm" variant="ghost" onClick={() => props.onPanel('settings')} aria-label="Settings">
          <Settings />
        </Button>
      </Tip>
      <Tip content="Documentation">
        <Button size="sm" variant="ghost" onClick={props.onDocs} aria-label="Documentation">
          <BookOpen /> Docs
        </Button>
      </Tip>
    </header>
  )
}
