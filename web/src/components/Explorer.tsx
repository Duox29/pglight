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
} from 'lucide-react'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { ScrollArea } from './ui/scroll-area'
import { Card } from './ui/card'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from './ui/context-menu'
import type { DbInfo, ObjectDetail, SchemaGroup } from '@/types'
import { cn } from '@/lib/utils'

interface Props {
  databases: DbInfo[]
  schemas: SchemaGroup[]
  currentDb: string
  connected: boolean
  detail: ObjectDetail | null
  onSwitchDb: (name: string) => void
  onOpenTable: (schema: string, table: string) => void
  onShowDDL: (schema: string, table: string) => void
  onShowView: (schema: string, name: string) => void
  onShowFunc: (schema: string, name: string) => void
  onShowSeq: (schema: string, name: string) => void
  onShowType: (schema: string, name: string) => void
  onOpenBrowser: (key: 'extensions' | 'roles', title: string) => void
  onOpenErd: (schema: string) => void
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
  onOpen: (name: string) => void
  onDetail?: (name: string) => void
  menu?: (name: string) => React.ReactNode
}) {
  const [open, setOpen] = useState(true)
  if (!props.items.length) return null
  return (
    <div>
      <button
        className="flex w-full items-center gap-1 rounded px-1 py-0.5 text-left text-[12px] text-sky-300/90 hover:bg-accent"
        onClick={() => setOpen(!open)}
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {props.icon}
        <span>
          {props.label} ({props.items.length})
        </span>
      </button>
      {open && (
        <div className="ml-3">
          {props.items.map((t) => (
            <ContextMenu key={t.name}>
              <ContextMenuTrigger asChild>
                <div
                  className="cursor-pointer truncate rounded px-1.5 py-0.5 text-[12px] hover:bg-accent"
                  title="Click to open · double-click for definition"
                  onClick={() => props.onOpen(t.name)}
                  onDoubleClick={() => props.onDetail?.(t.name)}
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
  const f = filter.toLowerCase()
  const match = (schema: string, name: string) => (schema + '.' + name).toLowerCase().includes(f)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex gap-1.5 p-2">
        <Input placeholder="Filter objects…" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <Button size="icon" variant="ghost" onClick={p.onRefresh} title="Refresh">
          <RefreshCw />
        </Button>
      </div>
      <ScrollArea className="min-h-0 flex-1 px-2">
        <div className="pb-2">
          <div className="flex items-center gap-1 px-1 py-1 text-[12px] font-semibold text-sky-300">
            <Database className="h-3.5 w-3.5" /> Databases ({p.databases.length})
          </div>
          <div className="ml-3">
            {p.databases
              .filter((d) => d.name.toLowerCase().includes(f))
              .slice(0, 60)
              .map((d) => (
                <ContextMenu key={d.name}>
                  <ContextMenuTrigger asChild>
                    <div
                      className={cn(
                        'cursor-pointer truncate rounded px-1.5 py-0.5 text-[12px] hover:bg-accent',
                        d.name === p.currentDb && 'font-semibold text-foreground',
                      )}
                      title="Click to reconnect to this database"
                      onClick={() => p.onSwitchDb(d.name)}
                    >
                      {d.name === p.currentDb ? '●' : '○'} {d.name}
                      <span className="text-muted-foreground"> {d.size ?? ''}</span>
                    </div>
                  </ContextMenuTrigger>
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
            const fullMenu = (onDetail: (name: string) => void) => (name: string) => (
              <ContextMenuContent>
                <ContextMenuItem onSelect={() => p.onOpenTable(s.schema, name)}>Open Data</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onNewQuery({ schema: s.schema, table: name })}>New Query</ContextMenuItem>
                <ContextMenuItem onSelect={() => onDetail(name)}>View Definition</ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem onSelect={() => p.onExport(s.schema, name, 'csv')}>Export CSV</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onExport(s.schema, name, 'sql')}>Export INSERTs</ContextMenuItem>
                <ContextMenuItem onSelect={() => p.onCopy(`${s.schema}.${name}`)}>Copy qualified name</ContextMenuItem>
                <ContextMenuItem onSelect={p.onRefresh}>Refresh</ContextMenuItem>
              </ContextMenuContent>
            )
            const slimMenu = (name: string) => (
              <ContextMenuContent>
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
                      className="flex w-full items-center gap-1 rounded px-1 py-1 text-left text-[12px] font-semibold text-sky-300 hover:bg-accent"
                      onClick={() => setOpenSchemas((m) => ({ ...m, [s.schema]: !isOpen }))}
                    >
                      {isOpen ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      {s.schema} ({T.length + V.length + M.length + F.length})
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
                  <div className="ml-2">
                    <Group icon={<Table2 className="h-3 w-3" />} label="Tables" items={T} render={(n) => `▦ ${n}`} onOpen={(n) => p.onOpenTable(s.schema, n)} onDetail={(n) => p.onShowDDL(s.schema, n)} menu={fullMenu((n) => p.onShowDDL(s.schema, n))} />
                    <Group icon={<Eye className="h-3 w-3" />} label="Views" items={V} render={(n) => `👁 ${n}`} onOpen={(n) => p.onOpenTable(s.schema, n)} onDetail={(n) => p.onShowView(s.schema, n)} menu={fullMenu((n) => p.onShowView(s.schema, n))} />
                    <Group icon={<Layers className="h-3 w-3" />} label="MatViews" items={M} render={(n) => `▦ ${n}`} onOpen={(n) => p.onOpenTable(s.schema, n)} onDetail={(n) => p.onShowView(s.schema, n)} menu={fullMenu((n) => p.onShowView(s.schema, n))} />
                    <Group icon={<Network className="h-3 w-3" />} label="Foreign" items={F} render={(n) => `⛓ ${n}`} onOpen={(n) => p.onOpenTable(s.schema, n)} onDetail={(n) => p.onShowDDL(s.schema, n)} menu={fullMenu((n) => p.onShowDDL(s.schema, n))} />
                    <Group icon={<FunctionSquare className="h-3 w-3" />} label="Functions" items={Fn} render={(n) => `ƒ ${n}`} onOpen={(n) => p.onShowFunc(s.schema, n)} onDetail={(n) => p.onShowFunc(s.schema, n)} menu={slimMenu} />
                    <Group icon={<Hash className="h-3 w-3" />} label="Sequences" items={Sq} render={(n) => `🔢 ${n}`} onOpen={(n) => p.onShowSeq(s.schema, n)} menu={slimMenu} />
                    <Group icon={<Shapes className="h-3 w-3" />} label="Types" items={Ty} render={(n) => `◈ ${n}`} onOpen={(n) => p.onShowType(s.schema, n)} menu={slimMenu} />
                  </div>
                )}
              </div>
            )
          })}
          <div className="flex items-center gap-1 px-1 py-1 text-[12px] font-semibold text-sky-300">
            <ServerCog className="h-3.5 w-3.5" /> Server objects
          </div>
          <div className={cn('ml-3', !p.connected && 'pointer-events-none opacity-40')} title={p.connected ? undefined : 'Connect to a database first'}>
            <div className="cursor-pointer rounded px-1.5 py-0.5 text-[12px] hover:bg-accent" title="Installed Postgres extensions (pg_catalog)" onClick={() => p.onOpenBrowser('extensions', 'Extensions')}>○ Extensions</div>
            <div className="cursor-pointer rounded px-1.5 py-0.5 text-[12px] hover:bg-accent" title="Users, groups and their attributes" onClick={() => p.onOpenBrowser('roles', 'Roles')}>○ Roles</div>
            <div className="cursor-pointer rounded px-1.5 py-0.5 text-[12px] hover:bg-accent" title="Foreign-key graph of a schema" onClick={() => p.onOpenErd('public')}>○ ERD</div>
          </div>
        </div>
      </ScrollArea>
      <div className="max-h-[38%] overflow-auto border-t p-2">
        <Card className="p-2.5">
          <div className="mb-1 text-[12px] font-semibold">{p.detail?.title ?? 'Object'}</div>
          <div className="text-[12px]">{p.detail?.body ?? <span className="text-muted-foreground">Select a table</span>}</div>
        </Card>
      </div>
    </div>
  )
}
