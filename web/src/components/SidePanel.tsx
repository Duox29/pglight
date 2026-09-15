import { useEffect, useState } from 'react'
import { Ban, Skull, Trash2, X } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Card } from './ui/card'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'
import { DataGrid } from './ui/data-grid'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { HistoryEntry, SideView, Snippet } from '@/types'
import type { DialogsApi } from './dialogs'
import { SettingsPanel } from './SettingsPanel'
import { AliasesPanel } from './AliasesPanel'
import { LogsPanel } from './LogsPanel'
import { api, q } from '@/lib/api'

interface Props {
  view: SideView
  onView: (v: SideView) => void
  onClose: () => void
  session: string
  history: HistoryEntry[]
  snippets: Snippet[]
  onOpenSql: (sql: string) => void
  onDeleteSnippet: (i: number) => void
  dialogs: DialogsApi
}

const viewGroups: { label: string; views: SideView[] }[] = [
  { label: 'Workspace', views: ['history', 'snippets', 'aliases'] },
  { label: 'Database', views: ['server', 'activity', 'locks', 'stats'] },
  { label: 'System', views: ['settings', 'logs'] },
]
export function SidePanel(p: Props) {
  const [filter, setFilter] = useState('')
  const [payload, setPayload] = useState<unknown>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (p.view === 'history' || p.view === 'snippets' || p.view === 'aliases' || p.view === 'settings' || p.view === 'logs') return
    // Drop the previous view's payload: without this a slow fetch briefly
    // renders stale data (e.g. an array) under the new view and crashes it.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPayload(null)
    setError('')
    let stop = false
    const load = async () => {
      try {
        const url =
          p.view === 'server'
            ? q(p.session, '/api/server-info?')
            : p.view === 'activity'
              ? q(p.session, '/api/activity?')
              : p.view === 'locks'
                ? q(p.session, '/api/locks?')
                : q(p.session, '/api/stats?')
        const j = await api<unknown>(url)
        if (!stop) {
          setPayload(j)
          setError((j as { error?: string })?.error ?? '')
        }
      } catch (e) {
        if (!stop) setError(String(e))
      }
    }
    load()
    const needsRefresh = p.view === 'activity' || p.view === 'locks'
    const t = needsRefresh ? setInterval(load, 5000) : undefined
    return () => {
      stop = true
      if (t) clearInterval(t)
    }
  }, [p.view, p.session])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between px-2.5 pt-2">
        <span className="text-sm font-semibold capitalize">{p.view}</span>
        <Button size="sm" variant="ghost" onClick={p.onClose} aria-label="Close panel">
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="flex flex-col gap-1 px-2.5 pb-1">
        <Tabs value={p.view} onValueChange={(v) => p.onView(v as SideView)}>
          {viewGroups.map((g) => (
            <div key={g.label}>
              <div className="px-1 pb-0.5 pt-1.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{g.label}</div>
              <TabsList className="w-full justify-start">
                {g.views.map((v) => (
                  <TabsTrigger key={v} value={v} className="capitalize">
                    {v}
                  </TabsTrigger>
                ))}
              </TabsList>
            </div>
          ))}
        </Tabs>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2.5">
        {p.view === 'history' && (
          <div className="flex flex-col gap-1.5">
            <Input placeholder="Filter history…" value={filter} onChange={(e) => setFilter(e.target.value)} />
            {p.history
              .filter((h) => !filter || h.sql.toLowerCase().includes(filter.toLowerCase()))
              .map((h, i) => (
                <Card key={i} className="cursor-pointer p-2 hover:bg-accent" onClick={() => p.onOpenSql(h.sql)}>
                  <div className="text-[11px] text-muted-foreground">
                    {h.at} · {h.ms}ms · {h.n} rows
                  </div>
                  <pre className="mt-1 whitespace-pre-wrap text-[11px] text-muted-foreground">{h.sql.slice(0, 200)}</pre>
                </Card>
              ))}
            {!p.history.length && <EmptyNote text="No history yet" />}
          </div>
        )}
        {p.view === 'snippets' && (
          <div className="flex flex-col gap-1.5">
            {p.snippets.map((s, i) => (
              <Card key={i} className="cursor-pointer p-2 hover:bg-accent" onClick={() => p.onOpenSql(s.sql)}>
                <div className="flex items-center justify-between">
                  <b className="text-[12px]">{s.name}</b>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Delete snippet ${s.name}`}
                    onClick={(e) => {
                      e.stopPropagation()
                      p.onDeleteSnippet(i)
                    }}
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </div>
                <pre className="mt-1 whitespace-pre-wrap text-[11px] text-muted-foreground">{s.sql.slice(0, 200)}</pre>
              </Card>
            ))}
            {!p.snippets.length && <EmptyNote text="No snippets — use Snippet in a query tab" />}
          </div>
        )}
        {p.view === 'aliases' && <AliasesPanel dialogs={p.dialogs} />}
        {(p.view === 'server' || p.view === 'activity' || p.view === 'locks' || p.view === 'stats' || p.view === 'settings' || p.view === 'logs') && (
          <div className="flex flex-col gap-2">
            <ErrorText message={error} />
            {p.view === 'server' && payload != null && !(payload as { error?: string }).error && <ServerView data={payload as ServerInfo} />}
            {p.view === 'activity' && payload != null && Array.isArray(payload) && (
              <ActivityView rows={payload as ActivityRow[]} session={p.session} dialogs={p.dialogs} />
            )}
            {p.view === 'locks' && payload != null && Array.isArray(payload) && (
              <LocksView rows={payload as LockRow[]} />
            )}
            {p.view === 'stats' && payload != null && !(payload as { error?: string }).error && <StatsView data={payload as StatsInfo} />}
            {p.view === 'settings' && <SettingsPanel />}
            {p.view === 'logs' && <LogsPanel />}
          </div>
        )}
      </div>
    </div>
  )
}

interface ServerInfo {
  version: string
  database: string
  db_size: string
  uptime: string
  connections: number
  max_connections: number
  settings: { name: string; setting: string; unit: string }[]
}

function ServerView({ data }: { data: ServerInfo }) {
  return (
    <div className="flex flex-col gap-2">
      <Card className="p-2.5 text-[12px]">
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">version</span><b className="text-right">{(data.version ?? '').split(' ').slice(0, 3).join(' ')}</b></div>
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">database</span><b>{data.database} ({data.db_size})</b></div>
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">uptime</span><b>{data.uptime}</b></div>
        <div className="flex justify-between py-1"><span className="text-muted-foreground">connections</span><b>{data.connections}/{data.max_connections}</b></div>
      </Card>
      <b className="text-[12px]">Settings</b>
      <DataGrid data={{ columns: ['name', 'setting', 'unit'], rows: (data.settings ?? []).map((s) => [s.name, s.setting, s.unit]) }} />
    </div>
  )
}

interface ActivityRow {
  pid: number
  user: string
  state: string
  duration: string
  query: string
}

function ActivityView({ rows, session, dialogs }: { rows: ActivityRow[]; session: string; dialogs: DialogsApi }) {
  const act = async (pid: number, kill?: boolean) => {
    if (kill) {
      const ok = await dialogs.confirm({
        title: `Terminate backend ${pid}?`,
        description: 'The server process will be killed immediately.',
        confirmText: 'Terminate',
        danger: true,
      })
      if (!ok) return
    }
    await api(q(session, `/api/cancel?pid=${pid}${kill ? '&kill=1' : ''}`))
  }
  return (
    <div className="flex flex-col gap-1.5">
      <DataGrid
        data={{
          columns: ['pid', 'user', 'state', 'duration', 'query'],
          rows: rows.map((r) => [r.pid, r.user, r.state, r.duration, (r.query ?? '').slice(0, 120)]),
        }}
        onCellClick={(v, col) => {
          if (col === 'pid' && navigator.clipboard) navigator.clipboard.writeText(String(v))
        }}
      />
      {rows.map((r) => (
        <div key={r.pid} className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <span className="font-mono">#{r.pid}</span>
          <span className="min-w-0 flex-1 truncate">{(r.query ?? '').slice(0, 80) || '—'}</span>
          <Button size="sm" variant="ghost" onClick={() => act(r.pid)} aria-label={`Cancel backend ${r.pid}`}>
            <Ban className="h-3 w-3" /> cancel
          </Button>
          <Button size="sm" variant="ghost" onClick={() => act(r.pid, true)} aria-label={`Terminate backend ${r.pid}`}>
            <Skull className="h-3 w-3" /> kill
          </Button>
        </div>
      ))}
      {!rows.length && <EmptyNote text="No sessions" />}
    </div>
  )
}

interface LockRow {
  pid: number
  user: string
  locktype: string
  relation: string
  mode: string
  granted: boolean
  query: string
}

function LocksView({ rows }: { rows: LockRow[] }) {
  const blocked = rows.filter((r) => !r.granted)
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
        <span className="inline-flex items-center gap-1"><span aria-hidden className="h-2 w-2 rounded-full bg-emerald-500" /> {rows.length - blocked.length} granted</span>
        <span className="inline-flex items-center gap-1"><span aria-hidden className="h-2 w-2 rounded-full bg-red-500" /> {blocked.length} waiting</span>
      </div>
      <DataGrid
        data={{
          columns: ['pid', 'user', 'locktype', 'relation', 'mode', 'status', 'query'],
          rows: rows.map((r) => [r.pid, r.user, r.locktype, r.relation, r.mode, r.granted ? 'granted' : 'waiting', (r.query ?? '').slice(0, 80)]),
        }}
        cellClassName={(v) => (v === 'waiting' ? 'font-semibold text-red-400' : v === 'granted' ? 'text-emerald-500' : undefined)}
      />
    </div>
  )
}

interface StatsInfo {
  databases: { name: string; backends: number; hit_ratio: number; size: string }[]
  top_tables: { schema: string; table: string; size: string; live: number; dead: number }[]
}

function StatsView({ data }: { data: StatsInfo }) {
  return (
    <div className="flex flex-col gap-2">
      <b className="text-[12px]">Databases</b>
      <DataGrid data={{ columns: ['name', 'backends', 'hit%', 'size'], rows: (data.databases ?? []).map((d) => [d.name, d.backends, d.hit_ratio, d.size]) }} />
      <b className="text-[12px]">Top tables</b>
      <DataGrid data={{ columns: ['schema', 'table', 'size', 'live', 'dead'], rows: (data.top_tables ?? []).map((t) => [t.schema, t.table, t.size, t.live, t.dead]) }} />
    </div>
  )
}
