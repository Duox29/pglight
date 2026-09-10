import { useEffect, useState } from 'react'
import { Ban, Skull } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Card } from './ui/card'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'
import { DataGrid } from './ui/data-grid'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { HistoryEntry, SideView, Snippet } from '@/types'
import type { DialogsApi } from './dialogs'
import { SettingsPanel } from './SettingsPanel'
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

const views: SideView[] = ['history', 'snippets', 'server', 'activity', 'locks', 'stats', 'settings', 'logs']

export function SidePanel(p: Props) {
  const [filter, setFilter] = useState('')
  const [payload, setPayload] = useState<unknown>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!p.session) return
    if (p.view === 'history' || p.view === 'snippets' || p.view === 'settings' || p.view === 'logs') return
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
        <Button size="sm" variant="ghost" onClick={p.onClose}>
          ✖
        </Button>
      </div>
      <div className="px-2.5 pb-1">
        <Tabs value={p.view} onValueChange={(v) => p.onView(v as SideView)}>
          <TabsList>
            {views.map((v) => (
              <TabsTrigger key={v} value={v} className="capitalize">
                {v}
              </TabsTrigger>
            ))}
          </TabsList>
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
                    onClick={(e) => {
                      e.stopPropagation()
                      p.onDeleteSnippet(i)
                    }}
                  >
                    del
                  </Button>
                </div>
                <pre className="mt-1 whitespace-pre-wrap text-[11px] text-muted-foreground">{s.sql.slice(0, 200)}</pre>
              </Card>
            ))}
            {!p.snippets.length && <EmptyNote text="No snippets — use ★ in a query tab" />}
          </div>
        )}
        {(p.view === 'server' || p.view === 'activity' || p.view === 'locks' || p.view === 'stats' || p.view === 'settings' || p.view === 'logs') && (
          <div className="flex flex-col gap-2">
            <ErrorText message={error} />
            {p.view === 'server' && payload != null && !(payload as { error?: string }).error && <ServerView data={payload as ServerInfo} />}
            {p.view === 'activity' && payload != null && Array.isArray(payload) && (
              <ActivityView rows={payload as ActivityRow[]} session={p.session} dialogs={p.dialogs} />
            )}
            {p.view === 'locks' && payload != null && Array.isArray(payload) && (
              <DataGrid
                data={{
                  columns: ['pid', 'user', 'locktype', 'relation', 'mode', 'granted', 'query'],
                  rows: (payload as LockRow[]).map((r) => [r.pid, r.user, r.locktype, r.relation, r.mode, String(r.granted), (r.query ?? '').slice(0, 80)]),
                }}
              />
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
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">version</span><b className="text-right">{data.version.split(' ').slice(0, 3).join(' ')}</b></div>
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
    <div className="flex flex-col gap-1">
      {rows.map((r) => (
        <Card key={r.pid} className="p-2 text-[12px]">
          <div className="flex items-center gap-2">
            <b>#{r.pid}</b>
            <span className="text-muted-foreground">{r.user} · {r.state} · {r.duration}</span>
            <span className="flex-1" />
            <Button size="sm" variant="ghost" onClick={() => act(r.pid)}>
              <Ban /> cancel
            </Button>
            <Button size="sm" variant="ghost" onClick={() => act(r.pid, true)}>
              <Skull /> kill
            </Button>
          </div>
          <pre className="mt-1 whitespace-pre-wrap text-[11px] text-muted-foreground">{(r.query ?? '').slice(0, 160)}</pre>
        </Card>
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
