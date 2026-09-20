import { useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Database,
  RefreshCw,
  Table2,
  Eye,
  Layers,
  Network,
  FunctionSquare,
  Hash,
  Shapes,
  ServerCog,
  Blocks,
  Users,
} from 'lucide-react'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { ScrollArea } from './ui/scroll-area'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from './ui/context-menu'
import type { DbInfo, ObjectKind, SchemaGroup } from '@/types'
import { cn } from '@/lib/utils'

interface Props {
  databases: DbInfo[]
  schemas: SchemaGroup[]
  currentDb: string
  connected: boolean
  onSwitchDb: (name: string) => void
  onOpenTable: (schema: string, table: string) => void
  onOpenBrowser: (key: 'extensions' | 'roles', title: string) => void
  onOpenErd: (schema: string) => void
  onOpenObject: (kind: ObjectKind, schema: string, name: string) => void
  onRefresh: () => void
  onNewQuery: (scope: { db?: string; schema?: string; table?: string }) => void
  onNewSchema: (db: string) => void
  onNewTable: (schema: string) => void
  onExport: (schema: string, table: string, fmt: 'csv' | 'sql') => void
  onCopy: (text: string) => void
}

function Group(props: {
  icon: React.ReactNode
  label: string
  items: { name: string }[]
  render: (name: string) => React.ReactNode
  onOpen?: (name: string) => void
  defaultOpen?: boolean
  forceOpen?: boolean
  menu?: (name: string) => React.ReactNode
}) {
  const [open, setOpen] = useState(props.defaultOpen ?? false)
  const shown = props.forceOpen ? true : open
  if (!props.items.length) return null
  return (
    <div>
      <button
        className="flex w-full items-center gap-1.5 rounded px-1 py-0.5 text-left text-[11px] font-semibold uppercase tracking-wide text-muted-foreground hover:bg-accent hover:text-foreground"
        onClick={() => setOpen(!shown)}
      >
        {shown ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {props.icon}
        <span>
          {props.label} ({props.items.length})
        </span>
      </button>
      {shown && (
        <div className="ml-3 border-l border-border/60 pl-1">
          {props.items.map((t) => (
            <ContextMenu key={t.name}>
              <ContextMenuTrigger asChild>
                <div
                  className="cursor-pointer truncate rounded px-1.5 py-0.5 text-[12px] hover:bg-accent"
                  onClick={() => props.onOpen?.(t.name)}
                >
                  {props.render(t.name)}
                </div>
              </ContextMenuTrigger>
              {props.menu?.(t.name)}
            </ContextMenu>
          ))}
        </div>
      )}
    </div>
  )
}

export function Explorer(p: Props) {
  const [filter, setFilter] = useState('')
  const [openSchemas, setOpenSchemas] = useState<Record<string, boolean>>({})
  const f = filter.trim().toLowerCase()
  const match = (schema: string, name: string) => (schema + '.' + name).toLowerCase().includes(f)
  const matchCount = f
    ? p.databases.filter((d) => d.name.toLowerCase().includes(f)).length +
      p.schemas.reduce((n, s) => {
        const hit = (name: string) => (s.schema + '.' + name).toLowerCase().includes(f)
        return (
          n +
          s.tables.filter((t) => hit(t.name)).length +
          s.views.filter((t) => hit(t.name)).length +
          s.matviews.filter((t) => hit(t.name)).length +
          s.foreign.filter((t) => hit(t.name)).length +
          s.functions.filter((t) => hit(t.name)).length +
          s.sequences.filter((t) => hit(t.name)).length +
          s.types.filter((t) => hit(t.name)).length
        )
      }, 0)
    : 0
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1.5 p-2">
        <Input placeholder="Filter objects…" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter objects" />
        {filter && (
          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground" aria-live="polite">
            {matchCount} match{matchCount === 1 ? '' : 'es'}
          </span>
        )}
        <Tip content="Refresh">
          <Button size="icon" variant="ghost" onClick={p.onRefresh} aria-label="Refresh">
            <RefreshCw />
          </Button>
        </Tip>
      </div>
      <ScrollArea className="min-h-0 flex-1 px-2">
        <div className="pb-2">
          <div className="flex items-center gap-1 px-1 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            <Database className="h-3.5 w-3.5" /> Databases ({p.databases.length})
          </div>
          <div className="ml-3">
            {p.databases
              .filter((d) => d.name.toLowerCase().includes(f))
              .slice(0, 60)
              .map((d) => (
                <ContextMenu key={d.name}>
                  <Tip content="Click to reconnect to this database">
                    <ContextMenuTrigger asChild>
                      <div
                        className={cn(
                          'flex cursor-pointer items-center gap-1.5 truncate rounded px-1.5 py-0.5 text-[12px] hover:bg-accent',
                          d.name === p.currentDb ? 'font-semibold text-foreground' : 'text-muted-foreground',
                        )}
                        onClick={() => p.onSwitchDb(d.name)}
                      >
                        <span aria-hidden className={d.name === p.currentDb ? 'text-emerald-500' : 'opacity-40'}>●</span>
                        <span className="truncate">{d.name}</span>
                        <span className="text-muted-foreground"> {d.size ?? ''}</span>
                      </div>
                    </ContextMenuTrigger>
                  </Tip>
                  <ContextMenuContent>
                    <ContextMenuItem onSelect={() => p.onNewQuery({ db: d.name })}>New Query</ContextMenuItem>
                    <ContextMenuItem disabled={!p.connected} onSelect={() => p.onNewSchema(d.name)}>New Schema…</ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => p.onSwitchDb(d.name)}>Switch to this database</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onCopy(d.name)}>Copy name</ContextMenuItem>
                    <ContextMenuItem onSelect={p.onRefresh}>Refresh</ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ))}
          </div>
          {p.schemas.map((s) => {
            const T = s.tables.filter((t) => match(s.schema, t.name))
            const V = s.views.filter((t) => match(s.schema, t.name))
            const M = s.matviews.filter((t) => match(s.schema, t.name))
            const F = s.foreign.filter((t) => match(s.schema, t.name))
            const Fn = s.functions.filter((t) => match(s.schema, t.name))
            const Sq = s.sequences.filter((t) => match(s.schema, t.name))
            const Ty = s.types.filter((t) => match(s.schema, t.name))
            const total = T.length + V.length + M.length + F.length + Fn.length + Sq.length + Ty.length
            if (f && !total && !s.schema.toLowerCase().includes(f)) return null
            const isOpen = openSchemas[s.schema] ?? true
            const fullMenu = (name: string) => (
              <ContextMenuContent>
                <ContextMenuItem onSelect={() => p.onOpenTable(s.schema, name)}>Open Data</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onNewQuery({ schema: s.schema, table: name })}>New Query</ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem onSelect={() => p.onExport(s.schema, name, 'csv')}>Export CSV</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onExport(s.schema, name, 'sql')}>Export INSERTs</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onCopy(`${s.schema}.${name}`)}>Copy qualified name</ContextMenuItem>
                <ContextMenuItem onSelect={p.onRefresh}>Refresh</ContextMenuItem>
              </ContextMenuContent>
            )
            const objectMenu = (kind: ObjectKind) => (name: string) => (
              <ContextMenuContent>
                <ContextMenuItem onSelect={() => p.onOpenObject(kind, s.schema, name)}>View definition</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onNewQuery({ schema: s.schema })}>New Query</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onCopy(`${s.schema}.${name}`)}>Copy name</ContextMenuItem>
                <ContextMenuItem onSelect={p.onRefresh}>Refresh</ContextMenuItem>
              </ContextMenuContent>
            )
            return (
              <div key={s.schema}>
                <ContextMenu>
                  <ContextMenuTrigger asChild>
                    <button
                      className="flex w-full items-center gap-1.5 rounded px-1 py-1 text-left hover:bg-accent"
                      onClick={() => setOpenSchemas((m) => ({ ...m, [s.schema]: !isOpen }))}
                    >
                      {isOpen ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
                      <span className="text-[13px] font-semibold text-foreground">{s.schema}</span>
                      <span className="text-[11px] font-normal text-muted-foreground">({T.length + V.length + M.length + F.length})</span>
                    </button>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem onSelect={() => p.onNewQuery({ schema: s.schema })}>New Query</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onNewTable(s.schema)}>New Table…</ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => p.onOpenErd(s.schema)}>Open ERD</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onCopy(s.schema)}>Copy name</ContextMenuItem>
                    <ContextMenuItem onSelect={p.onRefresh}>Refresh</ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
                {isOpen && (
                  <div className="ml-2 border-l border-border/50 pl-1">
                    <Group icon={<Table2 className="h-3 w-3" />} label="Tables" items={T} defaultOpen forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Table2 className="h-3 w-3 shrink-0 text-muted-foreground" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenTable(s.schema, n)} menu={fullMenu} />
                    <Group icon={<Eye className="h-3 w-3" />} label="Views" items={V} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Eye className="h-3 w-3 shrink-0 text-muted-foreground" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenTable(s.schema, n)} menu={fullMenu} />
                    <Group icon={<Layers className="h-3 w-3" />} label="MatViews" items={M} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Layers className="h-3 w-3 shrink-0 text-muted-foreground" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenTable(s.schema, n)} menu={fullMenu} />
                    <Group icon={<Network className="h-3 w-3" />} label="Foreign" items={F} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Network className="h-3 w-3 shrink-0 text-muted-foreground" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenTable(s.schema, n)} menu={fullMenu} />
                    <Group icon={<FunctionSquare className="h-3 w-3" />} label="Functions" items={Fn} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><FunctionSquare className="h-3 w-3 shrink-0 text-purple-400/70" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenObject('function', s.schema, n)} menu={objectMenu('function')} />
                    <Group icon={<Hash className="h-3 w-3" />} label="Sequences" items={Sq} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Hash className="h-3 w-3 shrink-0 text-amber-400/70" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenObject('sequence', s.schema, n)} menu={objectMenu('sequence')} />
                    <Group icon={<Shapes className="h-3 w-3" />} label="Types" items={Ty} forceOpen={!!f} render={(n) => <span className="flex items-center gap-1.5"><Shapes className="h-3 w-3 shrink-0 text-cyan-400/70" /><span className="truncate">{n}</span></span>} onOpen={(n) => p.onOpenObject('type', s.schema, n)} menu={objectMenu('type')} />
                  </div>
                )}
              </div>
            )
          })}
          <div className="flex items-center gap-1 px-1 py-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            <ServerCog className="h-3.5 w-3.5" /> Server objects
          </div>
          {p.connected ? (
            <div className="ml-3 flex flex-col gap-px">
              <Tip content="Installed Postgres extensions (pg_catalog)">
                <div className="flex cursor-pointer items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px] text-muted-foreground hover:bg-accent hover:text-foreground" onClick={() => p.onOpenBrowser('extensions', 'Extensions')}><Blocks className="h-3 w-3" /> Extensions</div>
              </Tip>
              <Tip content="Users, groups and their attributes">
                <div className="flex cursor-pointer items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px] text-muted-foreground hover:bg-accent hover:text-foreground" onClick={() => p.onOpenBrowser('roles', 'Roles')}><Users className="h-3 w-3" /> Roles</div>
              </Tip>
              <Tip content="Graph of a schema">
                <div className="flex cursor-pointer items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px] text-muted-foreground hover:bg-accent hover:text-foreground" onClick={() => p.onOpenErd('public')}><Network className="h-3 w-3" /> ERD</div>
              </Tip>
            </div>
          ) : (
            <Tip content="Connect to a database first">
              <div className="ml-3 flex flex-col gap-px opacity-40">
                <div className="flex cursor-not-allowed items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px]" aria-disabled><Blocks className="h-3 w-3" /> Extensions</div>
                <div className="flex cursor-not-allowed items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px]" aria-disabled><Users className="h-3 w-3" /> Roles</div>
                <div className="flex cursor-not-allowed items-center gap-1.5 rounded px-1.5 py-0.5 text-[12px]" aria-disabled><Network className="h-3 w-3" /> ERD</div>
              </div>
            </Tip>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
