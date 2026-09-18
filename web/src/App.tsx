import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Loader2, Plus, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { Tip, TooltipProvider } from './components/ui/tooltip'
import { ConnectionBar } from './components/ConnectionBar'
import { CredentialManager } from './components/CredentialManager'
import { DialogHost, createDialogs, type PendingDialog } from './components/dialogs'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './components/ui/resizable'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from './components/ui/context-menu'
import { Explorer } from './components/Explorer'
import { QueryConsole } from './components/QueryConsole'
import { TableWorkspace } from './components/TableWorkspace'
import { BrowserView } from './components/BrowserView'
import { DocsView } from './components/DocsView'
import { ErdView } from './components/ErdView'
import { ObjectView } from './components/ObjectView'
import { SidePanel } from './components/SidePanel'
import { SearchPalette } from './components/SearchPalette'
import { api, apiClient, q } from './lib/api'
import { DEFAULT_SQL, pkOf, qi, readStoredTabs } from './lib/tabs'
import { download, resultToCSV, resultToInserts, resultToJSON } from './lib/format'
import { useTabs } from './hooks/useTabs'
import { useSessions } from './hooks/useSessions'
import { useExplorer } from './hooks/useExplorer'
import { useQueryRunner } from './hooks/useQueryRunner'
import { useTableOps } from './hooks/useTableOps'
import { useObjectOps } from './hooks/useObjectOps'
import type { SideView, Tab } from './types'
import { cn } from './lib/utils'

/* Thin shell: hook composition + layout. All domain logic lives in
   hooks/* (sessions, tabs, explorer, query-run, table-ops, object-ops);
   feature components stay presentational (props in, callbacks out). */
export default function App() {
  const [dlg, setDlg] = useState<PendingDialog | null>(null)
  const dialogs = useMemo(() => createDialogs(setDlg), [])
  const [sideOpen, setSideOpen] = useState(false)
  const [sideView, setSideView] = useState<SideView>('history')
  const [paletteOpen, setPaletteOpen] = useState(false)

  // One-time migration from the legacy browser-only stores. Upload first,
  // verify through successful API responses, then remove only the migrated
  // keys. Passwords are intentionally never migrated or persisted.
  useEffect(() => {
    const marker = 'pglight-data-migrated-v1'
    if (localStorage.getItem(marker) === '1') return
    void (async () => {
      let ok = true
      try {
        const snippets = JSON.parse(localStorage.getItem('snippets') ?? '[]') as { name?: string; sql?: string }[]
        for (const s of Array.isArray(snippets) ? snippets : []) {
          if (!s.name || !s.sql) continue
          const r = await apiClient.saveSnippet(s.name, s.sql)
          if (r.error) { ok = false; break }
        }
        if (ok) {
          const history = JSON.parse(localStorage.getItem('hist') ?? '[]') as { sql?: string; ms?: number; n?: number }[]
          for (const h of Array.isArray(history) ? history.slice(0, 200) : []) {
            if (!h.sql) continue
            const r = await apiClient.addHistory(h.sql, h.ms, h.n) as { error?: string }
            if (r.error) { ok = false; break }
          }
        }
        if (ok) {
          const conns = JSON.parse(localStorage.getItem('conns') ?? '[]') as { name?: string; host?: string; port?: string; user?: string; dbname?: string; sslmode?: string }[]
          for (const c of Array.isArray(conns) ? conns : []) {
            if (!c.name || !c.host || !c.user || !c.dbname) continue
            const r = await apiClient.saveConnection({ name: c.name, host: c.host, port: Number(c.port) || 5432, user: c.user, dbname: c.dbname, sslmode: c.sslmode || 'prefer' })
            if (r.error) { ok = false; break }
          }
        }
      } catch { ok = false }
      if (ok) {
        localStorage.setItem(marker, '1')
        for (const key of ['snippets', 'hist', 'conns']) localStorage.removeItem(key)
      }
    })()
  }, [])

  // Ref bridges break the table↔explorer construction cycle: table ops
  // refresh the explorer after DDL, and explorer's create-table opens the
  // new table tab. Both only fire on user events, never during render.
  const loadExplorerRef = useRef<(sid?: string) => Promise<void>>(async () => {})
  const openTableTabRef = useRef<(schema: string, table: string, sid?: string) => void>(() => {})

  const tabsApi = useTabs()
  const { tabs, setTabs, activeTab, setActiveTab, cur, updateTab } = tabsApi
  const sessionsApi = useSessions({ onRemap: tabsApi.remapTabsSession })
  const { active, activeId, session, connected, sessions, bootDone, remapRef, deadRef } = sessionsApi
  const tableApi = useTableOps({
    tabs, setTabs, setActiveTab, updateTab,
    markTxn: sessionsApi.markTxn, autocommit: sessionsApi.autocommit, dialogs,
    loadExplorer: (...a) => loadExplorerRef.current(...a),
  })
  const objectApi = useObjectOps({
    tabs, setTabs, setActiveTab,
    markTxn: sessionsApi.markTxn, dialogs, closeTab: tabsApi.closeTab,
    loadExplorer: (...a) => loadExplorerRef.current(...a),
  })
  const explorerApi = useExplorer({
    session, active, activeId, activeIdRef: sessionsApi.activeIdRef,
    password: sessionsApi.fields.password, dialogs, setInTxn: sessionsApi.setInTxn,
    openTableTab: (...a) => openTableTabRef.current(...a),
    newQueryTab: tabsApi.newQueryTab,
    bootConnect: sessionsApi.bootConnect, findSession: sessionsApi.findSession,
    deadIds: sessionsApi.deadIds, reconnectOne: sessionsApi.reconnectOne,
    setActiveId: sessionsApi.setActiveId,
  })
  // Bridges resolve after render (same idiom as reconnectOneRef): the table
  // and explorer hooks only call them from user events, never during render.
  useEffect(() => {
    loadExplorerRef.current = explorerApi.loadExplorer
    openTableTabRef.current = tableApi.openTableTab
  })
  const queryApi = useQueryRunner({
    tabs, updateTab, session,
    autocommit: sessionsApi.autocommit, markTxn: sessionsApi.markTxn,
  })
  const { running, history, snippets, setSnippets } = queryApi

  /* ---------- tab strip: smooth wheel scroll, no scrollbar ---------- */
  const tablistRef = useRef<HTMLDivElement>(null)
  const wheelTarget = useRef<number | null>(null)
  const wheelRaf = useRef(0)

  useEffect(() => {
    const bar = tablistRef.current
    if (!bar) return
    const step = () => {
      if (wheelTarget.current == null) return
      const diff = wheelTarget.current - bar.scrollLeft
      if (Math.abs(diff) < 0.5) {
        bar.scrollLeft = wheelTarget.current
        wheelTarget.current = null
        return
      }
      bar.scrollLeft += diff * 0.25
      wheelRaf.current = requestAnimationFrame(step)
    }
    const kick = () => {
      cancelAnimationFrame(wheelRaf.current)
      wheelRaf.current = requestAnimationFrame(step)
    }
    // React attaches wheel listeners as passive at the root, so preventDefault
    // needs a native non-passive listener.
    const onWheel = (e: WheelEvent) => {
      if (bar.scrollWidth <= bar.clientWidth + 1) return
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return
      e.preventDefault()
      const unit = e.deltaMode === 1 ? 16 : 1
      const dy = e.deltaY * unit
      // Trackpads emit small fractional deltas that are already smooth —
      // apply directly. Notched wheels get eased toward a target.
      if (Math.abs(dy) < 40) {
        wheelTarget.current = null
        cancelAnimationFrame(wheelRaf.current)
        bar.scrollLeft += dy
        return
      }
      if (wheelTarget.current == null) wheelTarget.current = bar.scrollLeft
      wheelTarget.current = Math.min(Math.max(wheelTarget.current + dy, 0), bar.scrollWidth - bar.clientWidth)
      kick()
    }
    bar.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      bar.removeEventListener('wheel', onWheel)
      cancelAnimationFrame(wheelRaf.current)
      wheelTarget.current = null
    }
  }, [])

  /* ---------- global keys ---------- */
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen(true)
      }
    }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [])

  useEffect(() => {
    if (!activeId) return
    sessionsApi.refreshTxn()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  // Reopen last session's tabs on the first connection of this app load.
  const restored = useRef(false)
  const { restoreStoredTabs, newQueryTab } = tabsApi
  useEffect(() => {
    if (!connected || restored.current || !activeId || !bootDone) return
    restored.current = true
    const stored = readStoredTabs()
    const aliveIds = new Set(sessions.map((s) => s.id))
    // One-shot restore; loaders resolve into state asynchronously.
    if (stored.length)
      restoreStoredTabs(
        stored,
        aliveIds,
        activeId,
        { loadTablePage: tableApi.loadTablePage, loadTableMeta: tableApi.loadTableMeta, loadObjectDef: objectApi.loadObjectDef },
        { remap: remapRef.current, isDead: (sid) => !!deadRef.current[sid] },
      )
    else newQueryTab(undefined, activeId)
  }, [connected, activeId, sessions, bootDone, newQueryTab, restoreStoredTabs, tableApi, objectApi, remapRef, deadRef])

  const openTableForSession = (schema: string, table: string, sid?: string) => tableApi.openTableTab(schema, table, sid ?? activeId)
  const erdConnectionId = cur?.kind === 'erd'
    ? sessionsApi.saved.find((c) => c.id && c.host === (sessions.find((s) => s.id === cur.sessionId)?.host ?? '') && String(c.port) === String(sessions.find((s) => s.id === cur.sessionId)?.port ?? '') && c.user === (sessions.find((s) => s.id === cur.sessionId)?.user ?? '') && c.dbname === (sessions.find((s) => s.id === cur.sessionId)?.dbname ?? '') && c.sslmode === (sessions.find((s) => s.id === cur.sessionId)?.sslmode ?? ''))?.id
    : undefined

  return (
    <TooltipProvider delayDuration={200}>
    <div className="flex h-screen flex-col">
      <DialogHost dlg={dlg} />
      <Toaster
        theme="dark"
        position="bottom-right"
        gap={6}
        icons={{
          success: <Check className="h-3.5 w-3.5 text-white" />,
          error: <X className="h-3.5 w-3.5 text-white" />,
          info: <Info className="h-3.5 w-3.5 text-white" />,
          loading: <Loader2 className="h-3.5 w-3.5 animate-spin text-white" />,
        }}
        toastOptions={{
          style: {
            background: '#0a0a0a',
            color: '#fafafa',
            border: '1px solid #262626',
            borderRadius: '6px',
            boxShadow: 'none',
            fontSize: '12.5px',
            padding: '10px 12px',
          },
        }}
      />
      <ConnectionBar
        connected={connected}
        onSearch={() => setPaletteOpen(true)}
        onPanel={(v) => {
          setSideView(v)
          setSideOpen(true)
        }}
        onDocs={() => tabsApi.openDocsTab()}
      />
      <ResizablePanelGroup direction="horizontal" autoSaveId="pglight-main-layout" className="min-h-0 flex-1">
        <ResizablePanel defaultSize={20} minSize={12} maxSize={32} className="min-h-0">
        <aside className="flex h-full min-h-0 flex-col border-r bg-card">
          <CredentialManager
            fields={sessionsApi.fields}
            setFields={sessionsApi.setFields}
            saved={sessionsApi.saved}
            open={sessionsApi.credOpen}
            onOpenChange={sessionsApi.setCredOpen}
            onPickSaved={(i) => {
              const c = sessionsApi.saved[i]
              if (c) sessionsApi.setFields({ host: c.host, port: c.port, user: c.user, password: c.password, dbname: c.dbname, sslmode: c.sslmode })
            }}
            onDeleteSaved={(i) => {
              const item = sessionsApi.saved[i]
              if (!item) return
              void apiClient.deleteConnection(item.name).then((j) => {
                if (j.error) toast.error(j.error)
                else sessionsApi.setSaved((l) => l.filter((_, x) => x !== i))
              }).catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
            }}
            onConnect={() => {
              sessionsApi.connect(true).then((ok) => {
                if (ok) sessionsApi.setCredOpen(false)
              })
            }}
            onSave={async () => {
              const name = await dialogs.prompt({
                title: 'Save connection',
                defaultValue: `${sessionsApi.fields.host}/${sessionsApi.fields.dbname}`,
              })
              if (name == null) return
              const f = sessionsApi.fields
              const savedName = name.trim() || 'conn'
              const j = await apiClient.saveConnection({ name: savedName, host: f.host, port: Number(f.port) || 5432, user: f.user, dbname: f.dbname, sslmode: f.sslmode })
              if (j.error || !j.connection) toast.error(j.error ?? 'Failed to save connection')
              else sessionsApi.setSaved((l) => [{ id: j.connection!.id, name: j.connection!.name, host: j.connection!.host, port: String(j.connection!.port), user: j.connection!.user, password: '', dbname: j.connection!.dbname, sslmode: j.connection!.sslmode ?? f.sslmode }, ...l.filter((x) => x.name !== j.connection!.name)])
            }}
            onDisconnect={() => {
              sessionsApi.disconnect()
            }}
            autoLogin={sessionsApi.autoLogin}
            onAutoLogin={sessionsApi.setAutoLogin}
            connected={connected}
            sessions={sessions}
            activeId={activeId}
            onSwitch={(id) => sessionsApi.setActiveId(id)}
            onDisconnectOne={(id) => sessionsApi.disconnect(id)}
            deadIds={sessionsApi.deadIds}
            onReconnectOne={(id) => void sessionsApi.reconnectOne(id)}
            onReconnectAll={() => void sessionsApi.reconnectAll()}
          />
          <Explorer
            connected={connected}
            databases={explorerApi.databases}
            schemas={explorerApi.schemas}
            currentDb={active?.dbname ?? sessionsApi.fields.dbname}
            onSwitchDb={explorerApi.switchDb}
            onOpenTable={openTableForSession}
            onOpenBrowser={(key, title) => tabsApi.openBrowser(key, title, activeId)}
            onOpenErd={(s) => tabsApi.openErd(s, activeId)}
            onOpenObject={(kind, s, n) => objectApi.openObjectTab(kind, s, n, activeId)}
            onRefresh={() => explorerApi.loadExplorer(activeId)}
            onNewQuery={explorerApi.newScopedQuery}
            onNewSchema={explorerApi.createSchema}
            onNewTable={explorerApi.createTable}
            onExport={explorerApi.exportTable}
            onCopy={explorerApi.copyName}
          />
        </aside>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={55} minSize={30} className="min-h-0">
        <main className="flex h-full min-h-0 min-w-0 flex-col">
          <div ref={tablistRef} className="flex h-10 items-end gap-1 overflow-x-auto border-b bg-card px-2 pt-1.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden" role="tablist" aria-label="Workspace tabs">
            {tabs.map((t) => {
              const sid = (t as { sessionId?: string }).sessionId
              const db = sessions.find((s) => s.id === sid)?.dbname ?? ''
              const dirty = t.kind === 'query' && !t.results && !t.error && t.sql !== DEFAULT_SQL
              const idx = tabs.findIndex((x) => x.id === t.id)
              return (
                <ContextMenu key={t.id}>
                  <ContextMenuTrigger asChild>
                    <button
                      role="tab"
                      aria-selected={t.id === activeTab}
                      onClick={() => setActiveTab(t.id)}
                      onMouseDown={(e) => {
                        if (e.button === 1) {
                          e.preventDefault()
                          tabsApi.closeTab(t.id)
                        }
                      }}
                      className={cn(
                        'group flex items-center gap-1.5 whitespace-nowrap rounded-t-md border border-b-0 px-2.5 py-1.5 text-[12px]',
                        t.id === activeTab ? 'bg-background font-semibold' : 'bg-muted text-muted-foreground hover:text-foreground',
                      )}
                    >
                      <span aria-hidden className={cn('h-1.5 w-1.5 rounded-full', t.id === activeTab ? 'bg-primary' : dirty ? 'bg-amber-500' : 'bg-transparent')} />
                      <Tip content="Close tab (middle-click also closes)">
                        <span className="flex shrink-0">
                          <X
                            className="h-3 w-3 opacity-60 hover:opacity-100"
                            onClick={(e) => {
                              e.stopPropagation()
                              tabsApi.closeTab(t.id)
                            }}
                          />
                        </span>
                      </Tip>
                      <Tip content={t.kind === 'query' ? `${t.title} · ${db || 'no session'}` : `${t.title}`}>
                        <span className="max-w-[160px] truncate">{t.title}</span>
                      </Tip>
                      {t.kind !== 'docs' && db && (
                        <Tip content={`Session database: ${db}`}>
                          <span className="max-w-[80px] truncate rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">
                            {db}
                          </span>
                        </Tip>
                      )}
                    </button>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem onSelect={() => tabsApi.closeTab(t.id)}>Close</ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem disabled={tabs.length < 2} onSelect={() => tabsApi.closeOthers(t.id)}>
                      Close Others
                    </ContextMenuItem>
                    <ContextMenuItem disabled={idx < 0 || idx >= tabs.length - 1} onSelect={() => tabsApi.closeRight(t.id)}>
                      Close to the Right
                    </ContextMenuItem>
                    <ContextMenuItem disabled={idx <= 0} onSelect={() => tabsApi.closeLeft(t.id)}>
                      Close to the Left
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={tabsApi.closeAllTabs}>Close All</ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              )
            })}
            <Tip content="New query (Ctrl+K then Enter)">
              <button
                onClick={() => newQueryTab(undefined, activeId)}
                className="ml-auto flex shrink-0 items-center gap-1 whitespace-nowrap px-2 py-1.5 text-[12px] text-muted-foreground hover:text-foreground"
              >
                <Plus className="h-3.5 w-3.5" /> Query
              </button>
            </Tip>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {!cur && <div className="text-muted-foreground">{connected ? 'Open a table or run a query.' : 'Connect to a database to begin.'}</div>}
            {cur?.kind === 'query' && (
              <QueryConsole
                key={cur.id}
                tab={cur}
                running={!!running[cur.id]}
                {...sessionsApi.txnFor(cur.sessionId)}
                onSqlChange={(sql) => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, sql } : x))}
                onRun={(sql) => queryApi.runQuery(cur.id, sql)}
                onCancel={() => queryApi.cancelQuery(cur.id)}
                onClearResults={() => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, results: null, error: undefined, errLoc: undefined, flashTick: undefined, plan: undefined, meta: undefined } : x))}
                onExplain={(a) => queryApi.explainQuery(cur.id, a)}
                onLimit={(n) => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, limit: n } : x))}
                onSaveSnippet={async () => {
                  const name = await dialogs.prompt({
                    title: 'Save snippet',
                    defaultValue: cur.sql.slice(0, 40),
                  })
                  if (name) {
                    const j = await apiClient.saveSnippet(name.trim(), cur.sql)
                    if (j.error || !j.snippet) toast.error(j.error ?? 'Failed to save snippet')
                    else setSnippets((s) => [{ name: j.snippet!.name, sql: j.snippet!.sql }, ...s.filter((x) => x.name !== j.snippet!.name)])
                  }
                }}
                dialogs={dialogs}
              />
            )}
            {cur?.kind === 'table' && (
              <TableWorkspace
                tab={cur}
                onSubtab={(s) => {
                  updateTab(cur.id, (x) => (x.kind === 'table' ? { ...x, subtab: s } : x))
                  if (s !== 'data') tableApi.loadTableMeta(cur.sessionId, cur.id, cur.schema, cur.table)
                }}
                onFilterChange={(filter, order) => tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, 0, filter, order)}
                onApply={() => tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)}
                onPage={(d) => tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, Math.max(0, cur.offset + d * cur.limit), cur.filter, cur.order)}
                onEditCell={async (col, orig) => {
                  const keys = pkOf(cur)
                  if (!keys.length) {
                    toast.error('No primary key — editing is disabled for this table')
                    return
                  }
                  const missing = keys.filter((k) => orig[k] == null)
                  if (missing.length) {
                    toast.error(`Primary key column ${missing.join(', ')} is NULL — cannot identify this row`)
                    return
                  }
                  const v = await dialogs.promptNullable({
                    title: `Edit ${col}`,
                    description: 'Leave empty to keep the value as-is, or Set NULL for NULL',
                    defaultValue: orig[col] == null ? '' : String(orig[col]),
                  })
                  if (v == null) return
                  const value = typeof v === 'object' ? null : v
                  const where: Record<string, unknown> = {}
                  for (const k of keys) where[k] = orig[k]
                  tableApi.rowOp(cur.id, 'update', { [col]: value }, where, true)
                }}
                onDeleteRow={async (orig) => {
                  const keys = pkOf(cur)
                  if (!keys.length) {
                    toast.error('No primary key — deletion is disabled for this table')
                    return
                  }
                  const ok = await dialogs.confirm({
                    title: 'Delete row?',
                    description: `This will delete this row from ${cur.schema}.${cur.table}.`,
                    confirmText: 'Delete',
                    danger: true,
                  })
                  if (!ok) return
                  const where: Record<string, unknown> = {}
                  for (const k of keys) where[k] = orig[k]
                  tableApi.rowOp(cur.id, 'delete', {}, where, true)
                }}
                onCopyInsert={(orig) => {
                  explorerApi.copyName(resultToInserts(Object.keys(orig), [Object.values(orig)], `${qi(cur.schema)}.${qi(cur.table)}`))
                }}
                onExportRows={(rows, fmt) => {
                  if (!rows.length) return
                  const cols = cur.result?.columns ?? Object.keys(rows[0])
                  const types = cur.result?.types
                  const matrix = rows.map((r) => cols.map((c) => r[c]))
                  const base = `${cur.schema}.${cur.table}-selected`
                  if (fmt === 'csv') download(resultToCSV(cols, matrix), `${base}.csv`, 'text/csv')
                  else if (fmt === 'json') download(resultToJSON(cols, matrix), `${base}.json`, 'application/json')
                  else download(resultToInserts(cols, matrix, `${qi(cur.schema)}.${qi(cur.table)}`, types), `${base}.sql`, 'text/sql')
                }}
                onCopyRows={(rows) => {
                  if (!rows.length) return
                  const cols = cur.result?.columns ?? Object.keys(rows[0])
                  explorerApi.copyName(resultToCSV(cols, rows.map((r) => cols.map((c) => r[c]))))
                }}
                onDeleteRows={async (rows) => {
                  if (!rows.length) return
                  if (!cur.sessionId) {
                    toast.error('Session closed — reconnect to delete rows')
                    return
                  }
                  const keys = pkOf(cur)
                  if (!keys.length) {
                    toast.error('No primary key — deletion is disabled for this table')
                    return
                  }
                  const ok = await dialogs.confirm({
                    title: `Delete ${rows.length} row${rows.length === 1 ? '' : 's'}?`,
                    description: `This will delete ${rows.length} row${rows.length === 1 ? '' : 's'} from ${cur.schema}.${cur.table}.`,
                    confirmText: 'Delete',
                    danger: true,
                  })
                  if (!ok) return
                  try {
                    // PK-only predicates; the server deletes them in one
                    // transaction and aborts on the first non-unique match.
                    const where = rows.map((r) => Object.fromEntries(keys.map((k) => [k, r[k]])))
                    const j = await apiClient.batchDelete({ session_id: cur.sessionId, schema: cur.schema, table: cur.table, where })
                    if (j.error) toast.error(j.error)
                    else {
                      sessionsApi.markTxn(cur.sessionId, !!j.in_txn)
                      toast.success(`Deleted ${j.deleted ?? rows.length} row${(j.deleted ?? rows.length) === 1 ? '' : 's'}`)
                    }
                    tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                  } catch (e) {
                    toast.error(e instanceof Error ? e.message : String(e))
                    tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                  }
                }}
                onInsert={async () => {
                  if (!cur.result) return
                  const vals = await dialogs.form({
                    title: `Insert into ${cur.schema}.${cur.table}`,
                    description: 'Empty = skip · per-field N button = NULL',
                    fields: cur.result.columns.map((c) => ({ key: c, label: c, allowNull: true })),
                    submitText: 'Insert',
                  })
                  if (!vals) return
                  const clean: Record<string, unknown> = {}
                  for (const [k, v] of Object.entries(vals)) {
                    if (v === null) clean[k] = null
                    else if (v !== '') clean[k] = v
                  }
                  tableApi.rowOp(cur.id, 'insert', clean, {})
                }}
                onMaintenance={async (op) => {
                  const ok = await dialogs.confirm({
                    title: `${op.toUpperCase()} ${cur.schema}.${cur.table}?`,
                    confirmText: op.toUpperCase(),
                  })
                  if (!ok) return
                  try {
                    const j = await apiClient.maintenance(cur.sessionId, cur.schema, cur.table, op)
                    if (j.error) toast.error(j.error)
                    else {
                      toast.success('OK: ' + (j.result || op))
                      tableApi.loadTableMeta(cur.sessionId, cur.id, cur.schema, cur.table)
                    }
                  } catch (e) {
                    toast.error(e instanceof Error ? e.message : String(e))
                  }
                }}
                onImport={async (columns, rows) => {
                  try {
                    const j = await apiClient.importRows({ session_id: cur.sessionId, schema: cur.schema, table: cur.table, columns, rows })
                    if (j.error) toast.error(j.error)
                    else {
                      toast.success(`Imported ${j.rows_affected} rows`)
                      tableApi.loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                    }
                  } catch (e) {
                    toast.error(e instanceof Error ? e.message : String(e))
                  }
                }}
                onOpenErd={() => tabsApi.openErd(cur.schema, cur.sessionId)}
                onAlter={(pl) => tableApi.alterTable(cur.id, pl)}
                onRenameTable={() => tableApi.renameTable(cur.id)}
                dialogs={dialogs}
              />
            )}
            {cur?.kind === 'browser' && (
              <BrowserView
                tab={cur}
                onReload={() =>
                  api<Record<string, unknown>[] | { error: string }>(q(cur.sessionId, `${cur.url}?`))
                    .then((j) => {
                      updateTab(cur.id, (x) =>
                        x.kind === 'browser' ? { ...x, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : x,
                      )
                    })
                    .catch((e: unknown) => {
                      updateTab(cur.id, (x) => (x.kind === 'browser' ? { ...x, rows: null, error: e instanceof Error ? e.message : String(e) } : x))
                    })
                }
              />
            )}
            {cur?.kind === 'erd' && (
              <ErdView
                tab={cur}
                connectionId={erdConnectionId}
                schemas={explorerApi.schemas.map((s) => s.schema)}
                onSchema={(s) => {
                  updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, schema: s, title: `ERD ${s}`, data: null } : x))
                  api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(cur.sessionId, `/api/erd?schema=${encodeURIComponent(s)}`))
                    .then((j) => {
                      updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
                    })
                    .catch((e: unknown) => {
                      updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: null } : x))
                      toast.error(e instanceof Error ? e.message : String(e))
                    })
                }}
                onReload={() =>
                  api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(cur.sessionId, `/api/erd?schema=${encodeURIComponent(cur.schema)}`))
                    .then((j) => {
                      updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
                    })
                    .catch((e: unknown) => {
                      updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: null } : x))
                      toast.error(e instanceof Error ? e.message : String(e))
                    })
                }
                onOpenTable={(schema, table) => openTableForSession(schema, table, cur.sessionId)}
              />
            )}
            {cur?.kind === 'object' && (
              <ObjectView
                tab={cur}
                onReload={() => objectApi.loadObjectDef(cur.sessionId, cur.id, cur.objectKind, cur.schema, cur.name)}
                onCopy={explorerApi.copyName}
                onOpenQuery={() =>
                  newQueryTab(
                    cur.def ?? `-- ${cur.objectKind} ${cur.schema}.${cur.name}\nSELECT 1;`,
                    cur.sessionId,
                  )
                }
                onSaveSequence={(o) => objectApi.saveSequence(cur.id, o)}
                onSaveFunction={(sql) => objectApi.saveFunction(cur.id, sql)}
                onEnumAdd={(v) => objectApi.enumAdd(cur.id, v)}
                onRenameRequest={() => objectApi.renameObject(cur.id)}
                onDropRequest={() => objectApi.dropObject(cur.id)}
              />
            )}
            {cur?.kind === 'docs' && <DocsView />}
          </div>
        </main>
        </ResizablePanel>
        {sideOpen && (
          <>
          <ResizableHandle withHandle />
          <ResizablePanel defaultSize={25} minSize={15} maxSize={50} className="min-h-0">
          <aside className="flex h-full min-h-0 flex-col border-l bg-card">
            <SidePanel
              view={sideView}
              onView={setSideView}
              onClose={() => setSideOpen(false)}
              session={session}
              history={history}
              snippets={snippets}
              onOpenSql={(sql) => newQueryTab(sql, activeId)}
              onDeleteSnippet={(i) => {
                const item = snippets[i]
                if (!item) return
                void apiClient.deleteSnippet(item.name).then((j) => {
                  if (j.error) toast.error(j.error)
                  else setSnippets((s) => s.filter((_, x) => x !== i))
                }).catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
              }}
              dialogs={dialogs}
            />
          </aside>
          </ResizablePanel>
          </>
        )}
      </ResizablePanelGroup>
      <SearchPalette key={paletteOpen ? 'open' : 'closed'} open={paletteOpen} onOpenChange={setPaletteOpen} session={session} onOpenTable={openTableForSession} />
    </div>
    </TooltipProvider>
  )
}
