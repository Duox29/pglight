import { Database, Search, History, Star, LayoutDashboard, Plus, Settings } from 'lucide-react'
import { Button } from './ui/button'
import { Badge } from './ui/badge'

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
  inTxn: boolean
  onSearch: () => void
  onPanel: (v: 'history' | 'snippets' | 'server' | 'settings') => void
  onNewQuery: () => void
}) {
  return (
    <header className="flex flex-wrap items-center gap-1.5 border-b bg-card px-2.5 py-2">
      <span className="flex items-center gap-1.5 text-sm font-bold">
        <Database className="h-4 w-4" /> DbClient
      </span>
      {props.inTxn ? (
        <Badge variant="warning">IN TRANSACTION</Badge>
      ) : (
        <Badge variant="success">AUTOCOMMIT</Badge>
      )}
      <Badge variant={props.connected ? 'success' : 'secondary'}>
        {props.connected ? 'connected' : 'disconnected'}
      </Badge>
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
      <Button size="sm" variant="ghost" onClick={() => props.onPanel('settings')} title="Settings">
        <Settings />
      </Button>
      <Button size="sm" variant="secondary" onClick={props.onNewQuery}>
        <Plus /> Query
      </Button>
    </header>
  )
}
