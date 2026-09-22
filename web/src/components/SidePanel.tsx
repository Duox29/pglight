import { useEffect, useState } from 'react'
import { Ban, Skull, Trash2 } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Card } from './ui/card'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'
import { Switch } from './ui/switch'
import { DataGrid } from './ui/data-grid'
import { EmptyNote, ErrorText } from './ui/feedback'
import { Tip } from './ui/tooltip'
import type { ConnFields } from './ConnectionBar'
import type { HistoryEntry, SavedConnection, SessionInfo, SideView, Snippet } from '@/types'
import type { DialogsApi } from './dialogs'
import { AppearancePanel } from './AppearancePanel'
import { SettingsPanel } from './SettingsPanel'
import { CredentialManager, type ProfileMetadata } from './CredentialManager'
import { KeyboardShortcutsPanel } from './KeyboardShortcutsPanel'
import { AliasesPanel } from './AliasesPanel'
import { LogsPanel } from './LogsPanel'
import { api, q } from '@/lib/api'
import { QUICK_ACCESS_VIEWS, WORKSPACE_VIEW_META, WORKSPACE_VIEWS } from '@/lib/workspace'

interface Props {
  view: SideView
  onView: (v: SideView) => void
  session: string
  history: HistoryEntry[]
  snippets: Snippet[]
  onOpenSql: (sql: string) => void
  onDeleteSnippet: (i: number) => void
  dialogs: DialogsApi
  quickAccess: SideView[]
  onQuickAccessChange: (view: SideView, enabled: boolean) => void
  connection: {
    fields: ConnFields
    setFields: (fields: ConnFields) => void
    saved: SavedConnection[]
    onConnect: (profileId?: string) => Promise<string>
    onTest: (profileId?: string, password?: string) => Promise<{ database?: string; version?: string; latency_ms?: number }>
    onSave: (name: string, savePassword: boolean, clearPassword: boolean, metadata: ProfileMetadata) => Promise<void>
    onDuplicate: (name: string) => Promise<string>
    onDelete: (id: string) => Promise<void>
    vault: { exists: boolean; unlocked: boolean }
    onVaultAction: (action: 'setup' | 'unlock' | 'lock' | 'change_password', masterPassword?: string, newPassword?: string) => Promise<void>
    autoLogin: boolean
    onAutoLogin: (value: boolean) => void
    sessions: SessionInfo[]
    activeId: string
    onSwitch: (id: string) => void
    onDisconnectOne: (id: string) => void
    deadIds: Record<string, boolean>
    onReconnectOne: (id: string) => void
    onReconnectAll: () => void
  }
}

export function SidePanel(p: Props) {
  const [filter, setFilter] = useState('')
  const [payload, setPayload] = useState<unknown>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (p.view === 'history' || p.view === 'snippets' || p.view === 'aliases' || p.view === 'connections' || p.view === 'appearance' || p.view === 'settings' || p.view === 'shortcuts' || p.view === 'logs' || p.view === 'quick-access') return
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
      <div className="px-3 pt-3">
        <div className="text-sm font-semibold">Workspace</div>
        <div className="mt-0.5 text-[11px] text-muted-foreground">History, database monitoring, settings, and tools</div>
      </div>
      <div className="px-2.5 py-2">
        <Tabs value={p.view} onValueChange={(v) => p.onView(v as SideView)}>
          <TabsList className="w-full flex-nowrap justify-start overflow-x-auto overflow-y-hidden">
            {WORKSPACE_VIEWS.map((v) => {
              const Icon = WORKSPACE_VIEW_META[v].icon
              return <TabsTrigger key={v} value={v} className="gap-1.5"><Icon className="h-3.5 w-3.5" />{WORKSPACE_VIEW_META[v].label}</TabsTrigger>
            })}
          </TabsList>
        </Tabs>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden p-2.5">
        {p.view === 'history' && (
          <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto">
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
          <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-auto">
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
        {p.view === 'quick-access' && <QuickAccessView quickAccess={p.quickAccess} onChange={p.onQuickAccessChange} />}
        {p.view === 'connections' && <CredentialManager {...p.connection} dialogs={p.dialogs} />}
        {(p.view === 'server' || p.view === 'activity' || p.view === 'locks' || p.view === 'stats' || p.view === 'appearance' || p.view === 'settings' || p.view === 'shortcuts' || p.view === 'logs') && (
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-hidden">
            <ErrorText message={error} />
            {p.view === 'server' && payload != null && !(payload as { error?: string }).error && <ServerView data={payload as ServerInfo} />}
            {p.view === 'activity' && payload != null && Array.isArray(payload) && (
              <ActivityView rows={payload as ActivityRow[]} session={p.session} dialogs={p.dialogs} />
            )}
            {p.view === 'locks' && payload != null && Array.isArray(payload) && (
              <LocksView rows={payload as LockRow[]} />
            )}
            {p.view === 'stats' && payload != null && !(payload as { error?: string }).error && <StatsView data={payload as StatsInfo} />}
            {p.view === 'appearance' && <AppearancePanel />}
            {p.view === 'settings' && <SettingsPanel dialogs={p.dialogs} />}
            {p.view === 'shortcuts' && <KeyboardShortcutsPanel dialogs={p.dialogs} />}
            {p.view === 'logs' && <LogsPanel />}
          </div>
        )}
      </div>
    </div>
  )
}

function QuickAccessView({ quickAccess, onChange }: { quickAccess: SideView[]; onChange: (view: SideView, enabled: boolean) => void }) {
  return (
    <div className="flex flex-col gap-2">
      <Card className="p-3">
        <div className="mb-1 text-[12px] font-semibold">Connection Bar shortcuts</div>
        <p className="mb-3 text-[11px] text-muted-foreground">Toggle the workspace sections that should appear beside the search button.</p>
        <div className="flex flex-col divide-y">
          {QUICK_ACCESS_VIEWS.map((view) => {
            const Icon = WORKSPACE_VIEW_META[view].icon
            return (
              <label key={view} className="flex items-center justify-between gap-3 py-2 text-[12px] first:pt-0 last:pb-0">
                <span className="flex items-center gap-2"><Icon className="h-3.5 w-3.5 text-muted-foreground" />{WORKSPACE_VIEW_META[view].label}</span>
                <Switch checked={quickAccess.includes(view)} onCheckedChange={(enabled) => onChange(view, enabled)} aria-label={`Show ${WORKSPACE_VIEW_META[view].label} in Connection Bar`} />
              </label>
            )
          })}
        </div>
      </Card>
      <Card className="p-3 text-[11px] text-muted-foreground">
        The shortcut buttons open the Workspace tab directly on the selected section. Your choices are saved with the app preferences.
      </Card>
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
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <Card className="p-2.5 text-[12px]">
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">version</span><b className="text-right">{(data.version ?? '').split(' ').slice(0, 3).join(' ')}</b></div>
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">database</span><b>{data.database} ({data.db_size})</b></div>
        <div className="flex justify-between border-b border-dashed py-1"><span className="text-muted-foreground">uptime</span><b>{data.uptime}</b></div>
        <div className="flex justify-between py-1"><span className="text-muted-foreground">connections</span><b>{data.connections}/{data.max_connections}</b></div>
      </Card>
      <b className="text-[12px]">Settings</b>
      <DataGrid
        data={{ columns: ['name', 'setting', 'unit'], rows: (data.settings ?? []).map((s) => [s.name, s.setting, s.unit]) }}
        containerClassName="min-h-0 max-h-full flex-1"
      />
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
    <div className="flex min-h-0 flex-1 flex-col gap-1.5">
      <DataGrid
        data={{
          columns: ['pid', 'user', 'state', 'duration', 'query', 'actions'],
          rows: rows.map((r) => [r.pid, r.user, r.state, r.duration, (r.query ?? '').slice(0, 120), null]),
        }}
        containerClassName="min-h-0 max-h-full flex-1"
        renderCell={(_value, row, _rowIndex, columnIndex) => {
          if (columnIndex !== 5) return undefined
          const pid = Number(row[0])
          return (
            <div className="flex gap-1">
              <Tip content={`Cancel backend ${pid}`}>
                <Button size="sm" variant="ghost" onClick={() => act(pid)} aria-label={`Cancel backend ${pid}`}>
                  <Ban className="h-3 w-3" />
                </Button>
              </Tip>
              <Tip content={`Terminate backend ${pid}`}>
                <Button size="sm" variant="ghost" onClick={() => act(pid, true)} aria-label={`Terminate backend ${pid}`}>
                  <Skull className="h-3 w-3" />
                </Button>
              </Tip>
            </div>
          )
        }}
        onCellClick={(v, col) => {
          if (col === 'pid' && navigator.clipboard) navigator.clipboard.writeText(String(v))
        }}
      />
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
    <div className="flex min-h-0 flex-1 flex-col gap-1.5">
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
        containerClassName="min-h-0 max-h-full flex-1"
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
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto">
      <b className="text-[12px]">Databases</b>
      <DataGrid data={{ columns: ['name', 'backends', 'hit%', 'size'], rows: (data.databases ?? []).map((d) => [d.name, d.backends, d.hit_ratio, d.size]) }} />
      <b className="text-[12px]">Top tables</b>
      <DataGrid data={{ columns: ['schema', 'table', 'size', 'live', 'dead'], rows: (data.top_tables ?? []).map((t) => [t.schema, t.table, t.size, t.live, t.dead]) }} />
    </div>
  )
}
