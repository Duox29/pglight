import { useMemo, useState } from 'react'
import { BookOpen } from 'lucide-react'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { Card } from './ui/card'

interface Section {
  id: string
  title: string
  body: React.ReactNode
}

function P({ children }: { children: React.ReactNode }) {
  return <p className="text-[13px] leading-relaxed text-muted-foreground">{children}</p>
}

function Li({ children }: { children: React.ReactNode }) {
  return <li className="text-[13px] leading-relaxed text-muted-foreground">{children}</li>
}

function K({ children }: { children: React.ReactNode }) {
  return <code className="rounded bg-muted px-1 py-0.5 font-mono text-[12px] text-foreground">{children}</code>
}

const SECTIONS: Section[] = [
  {
    id: 'connect',
    title: 'Connecting',
    body: (
      <>
        <P>Fill host, port, user, password, database and sslmode in the Connections panel (left sidebar), then Connect. The panel collapses on success.</P>
        <ul className="mt-1.5 list-disc space-y-1 pl-5">
          <Li><K>Save</K> stores the connection (including password) in this browser for reuse.</Li>
          <Li><K>Auto-connect on startup</K> replays the last successful connection when the app opens.</Li>
          <Li>Click a database in the Explorer to reconnect to it. Disconnecting keeps your tabs; they reload on next connect.</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'explorer',
    title: 'Explorer',
    body: (
      <>
        <P>Browse databases, then per-schema groups: Tables, Views, MatViews, Foreign tables, Functions, Sequences and Types, plus instance-level Server objects (Extensions, Roles, ERD shortcut).</P>
        <ul className="mt-1.5 list-disc space-y-1 pl-5">
          <Li>Click a table to open it; <K>double-click</K> a table, view or function for its definition in the Object panel below.</Li>
          <Li>The filter box matches <K>schema.name</K> across every group.</Li>
          <Li>Server objects need a live connection — they stay dimmed until you connect.</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'query',
    title: 'Query console',
    body: (
      <>
        <P>Each Query tab is an independent console. <K>Ctrl+Enter</K> runs; the limiter wraps single SELECTs automatically.</P>
        <ul className="mt-1.5 list-disc space-y-1 pl-5">
          <Li><K>Multi-statement scripts</K> run in order and render one result block per statement; a failing statement reports which one failed.</Li>
          <Li><K>Explain / Analyze</K> shows the plan with timing and buffer stats.</Li>
          <Li><K>Format</K> uppercases keywords and breaks clauses; <K>★ Snippet</K> saves the SQL to the Snippets panel.</Li>
          <Li>Export any result as <K>CSV / JSON / INSERTs</K>; click a cell to copy it; click a header to sort.</Li>
          <Li>With <K>autocommit off</K>, running DML auto-opens a transaction — Commit or Rollback from the txn bar. The <K>IN TXN</K> badge marks tabs inside it.</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'table',
    title: 'Table workspace',
    body: (
      <>
        <P>Tables open with seven sub-tabs: Data, Columns, DDL, Indexes, Constraints, Triggers, Stats.</P>
        <ul className="mt-1.5 list-disc space-y-1 pl-5">
          <Li><K>Data</K>: free-form WHERE filter, ORDER input, paging, per-row edit (double-click a cell, <K>__NULL__</K> for NULL), copy row as INSERT (⧉), delete, insert row form, CSV export/INSERT export. Bulk select with Ctrl-click / Shift-click, right-click for Export (CSV/JSON/INSERTs), Copy and bulk Delete.</Li>
          <Li><K>Import CSV</K>: header row maps to columns automatically (or positional fallback); confirm before inserting.</Li>
          <Li><K>Stats</K> shows sizes, seq/idx scans, live/dead tuples and vacuum ages, with VACUUM / ANALYZE / REINDEX actions (refused inside an open txn, like pgAdmin).</Li>
          <Li>UPDATE/DELETE without a key are refused server-side.</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'erd',
    title: 'ERD diagram',
    body: (
      <>
        <P>Foreign-key graph of one schema: boxes are tables (sized by relation count), arrows point from child column to parent. Switch schema from the dropdown; click any box to open that table.</P>
      </>
    ),
  },
  {
    id: 'dashboard',
    title: 'Dashboard (right panel)',
    body: (
      <>
        <ul className="list-disc space-y-1 pl-5">
          <Li><K>History</K>: every executed statement with timing and row counts — click to reopen as a query. Filterable.</Li>
          <Li><K>Snippets</K>: your saved SQL library.</Li>
          <Li><K>Server</K>: version, size, uptime, connections, key settings.</Li>
          <Li><K>Activity</K>: live sessions with cancel/kill per backend (auto-refreshes).</Li>
          <Li><K>Locks</K>: lock waits with blockers first (auto-refreshes).</Li>
          <Li><K>Stats</K>: per-database stats and top tables by size.</Li>
          <Li><K>Settings</K>: logging config + live log viewer (see below).</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'logging',
    title: 'Settings & logging',
    body: (
      <>
        <P>Every request and statement is captured by central logging (no log calls scattered in handlers). Steady queries log at debug; slow queries warn; failures error.</P>
        <ul className="mt-1.5 list-disc space-y-1 pl-5">
          <Li><K>Level</K> filters verbosity (debug shows each SQL statement).</Li>
          <Li><K>Slow threshold</K> marks queries slower than N ms as warnings.</Li>
          <Li><K>Max entries</K> bounds the in-memory ring buffer; config persists in <K>data/logging.json</K>.</Li>
          <Li><K>Defaults</K> restores the factory config; the log viewer filters by level/category, auto-refreshes, and can be cleared.</Li>
        </ul>
      </>
    ),
  },
  {
    id: 'session',
    title: 'Session restore',
    body: (
      <>
        <P>Reload the page and you return to the same workspace: autologin replays the last connection (a stored session is verified first, never trusted blindly), then every open tab is rebuilt — tables, browsers and diagrams reload their data, and query tabs bring back their last result as a labeled snapshot (queries are never auto re-run). The open tab, the autocommit toggle and the layout sizes are restored too.</P>
      </>
    ),
  },
  {
    id: 'shortcuts',
    title: 'Keyboard shortcuts',
    body: (
      <>
        <ul className="list-disc space-y-1 pl-5">
          <Li><K>Ctrl/⌘ + Enter</K> — run query in the active console</Li>
          <Li><K>Ctrl/⌘ + K</K> — global object search (Enter jumps to the top hit)</Li>
          <Li><K>Esc</K> — close search / dialogs</Li>
          <Li><K>Enter</K> — confirm dialogs and the palette input</Li>
        </ul>
      </>
    ),
  },
]

export function DocsView() {
  const [filter, setFilter] = useState('')
  const list = useMemo(() => {
    const f = filter.trim().toLowerCase()
    if (!f) return SECTIONS
    return SECTIONS.filter((s) => (s.title + ' ' + textOf(s.id)).toLowerCase().includes(f))
  }, [filter])

  const jump = (id: string) => {
    document.getElementById(`docs-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div className="mx-auto flex max-w-[860px] flex-col gap-3">
      <div className="flex items-center gap-2">
        <BookOpen className="h-4 w-4" />
        <b className="text-sm">Documentation</b>
        <span className="flex-1" />
        <Input className="w-[220px]" placeholder="Filter sections…" value={filter} onChange={(e) => setFilter(e.target.value)} />
      </div>
      <div className="flex flex-wrap gap-1.5">
        {SECTIONS.map((s) => (
          <Button key={s.id} size="sm" variant="ghost" onClick={() => jump(s.id)}>
            {s.title}
          </Button>
        ))}
      </div>
      {list.map((s) => (
        <Card key={s.id} id={`docs-${s.id}`} className="scroll-mt-2 p-3.5">
          <div className="mb-1.5 text-[13px] font-semibold">{s.title}</div>
          {s.body}
        </Card>
      ))}
      {!list.length && <Card className="p-6 text-center text-muted-foreground">No sections match.</Card>}
    </div>
  )
}

function textOf(id: string): string {
  const hints: Record<string, string> = {
    connect: 'connect login password saved auto-connect disconnect database',
    explorer: 'tree tables views functions sequences types extensions roles filter definition',
    query: 'sql run multi-statement explain analyze format snippet export csv json insert transaction autocommit',
    table: 'data edit delete copy insert csv import vacuum analyze reindex columns ddl indexes constraints triggers stats',
    erd: 'diagram foreign key graph schema',
    dashboard: 'history snippets server activity locks stats sessions cancel kill',
    logging: 'settings log level debug slow threshold config',
    session: 'restore reopen tabs autologin persist reload',
    shortcuts: 'keyboard ctrl enter escape search',
  }
  return hints[id] ?? ''
}
