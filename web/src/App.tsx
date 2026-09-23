import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Loader2, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { TooltipProvider } from './components/ui/tooltip'
import { ConnectionBar } from './components/ConnectionBar'
import type { ProfileMetadata } from './components/CredentialManager'
import { DialogHost, createDialogs, type PendingDialog } from './components/dialogs'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './components/ui/resizable'
import { Explorer } from './components/Explorer'
import { SearchPalette } from './components/SearchPalette'
import { SplitWorkspace } from './components/SplitWorkspace'
import { TabContent } from './components/TabContent'
import { TabStrip } from './components/TabStrip'
import { FloatingScrollbars } from './components/FloatingScrollbars'
import { apiClient } from './lib/api'
import { readStoredTabs } from './lib/tabs'
import { useTabs } from './hooks/useTabs'
import { useSessions } from './hooks/useSessions'
import { useExplorer } from './hooks/useExplorer'
import { useQueryRunner } from './hooks/useQueryRunner'
import { useTableOps } from './hooks/useTableOps'
import { useObjectOps } from './hooks/useObjectOps'
import { useSplit } from './hooks/useSplit'
import type { SideView } from './types'
import { formatSqlText } from './lib/format'
import { DEFAULT_QUICK_ACCESS, QUICK_ACCESS_VIEWS } from './lib/workspace'
import { useAppPreference } from './lib/storage'
import { useAppearance } from './hooks/useAppearance'
import { useCommand, useCommandRegistration } from './shortcuts/ShortcutProvider'

/* Thin shell: hook composition + layout. All domain logic lives in hooks/*
   (sessions, tabs, explorer, query-run, table-ops, object-ops, useSplit for
   the 2-pane workspace); per-tab rendering in components/TabContent
   (composition root), the tab bar in TabStrip, split chrome in
   SplitWorkspace; feature components stay presentational (props in,
   callbacks out). */
export default function App() {
	useEffect(() => {
		if (window.location.search.includes('token=')) {
			window.history.replaceState({}, document.title, window.location.pathname + window.location.hash)
		}
	}, [])
  const [dlg, setDlg] = useState<PendingDialog | null>(null)
  const dialogs = useMemo(() => createDialogs(setDlg), [])
  const [quickAccess, setQuickAccess] = useAppPreference<SideView[]>('workspace.quickAccess', DEFAULT_QUICK_ACCESS)
  const { appearance } = useAppearance()
  const commands = useCommand()
  const { paletteOpen, setPaletteOpen } = commands

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
  const { tabs, setTabs, activeTab, setActiveTab, cur, updateTab, newQueryTab } = tabsApi
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

  const split = useSplit({ tabs, activeTab, onActivate: setActiveTab })

  /* ---------- command registrations ---------- */
  const activeQuery = cur?.kind === 'query' ? cur : null
  const activeTable = cur?.kind === 'table' ? cur : null
  const activeRunning = activeTab ? !!running[activeTab] : false
  useCommandRegistration('palette.open', () => setPaletteOpen(true))
  useCommandRegistration('query.new', () => newQueryTab(undefined, activeId), { enabled: !!activeId })
  useCommandRegistration('query.run', () => { if (activeQuery) void queryApi.runQuery(activeQuery.id) }, { enabled: !!activeQuery && !activeRunning })
  useCommandRegistration('query.cancel', () => { if (activeQuery) void queryApi.cancelQuery(activeQuery.id) }, { enabled: !!activeQuery && activeRunning })
  useCommandRegistration('query.format', () => { if (activeQuery) updateTab(activeQuery.id, (tab) => tab.kind === 'query' ? { ...tab, sql: formatSqlText(tab.sql) } : tab) }, { enabled: !!activeQuery })
  useCommandRegistration('query.explain', () => { if (activeQuery) void queryApi.explainQuery(activeQuery.id) }, { enabled: !!activeQuery })
  useCommandRegistration('query.clearResults', () => { if (activeQuery) updateTab(activeQuery.id, (tab) => tab.kind === 'query' ? { ...tab, results: null, error: undefined, plan: undefined } : tab) }, { enabled: !!activeQuery })
  useCommandRegistration('query.saveSnippet', async () => {
    if (!activeQuery) return
    const name = await dialogs.prompt({ title: 'Save query as snippet', defaultValue: activeQuery.title })
    if (!name?.trim()) return
    const result = await apiClient.saveSnippet(name.trim(), activeQuery.sql)
    if (result.error || !result.snippet) toast.error(result.error ?? 'Failed to save snippet')
    else { setSnippets((items) => [{ name: result.snippet!.name, sql: result.snippet!.sql }, ...items.filter((item) => item.name !== result.snippet!.name)]) ; toast.success('Snippet saved') }
  }, { enabled: !!activeQuery })
  useCommandRegistration('table.refresh', () => {
    if (!activeTable) return
    void tableApi.loadTablePage(activeTable.sessionId, activeTable.id, activeTable.schema, activeTable.table, activeTable.limit, activeTable.offset, activeTable.filter, activeTable.order)
    void tableApi.loadTableMeta(activeTable.sessionId, activeTable.id, activeTable.schema, activeTable.table)
  }, { enabled: !!activeTable })
  useCommandRegistration('table.nextPage', () => {
    if (!activeTable || !activeTable.result?.has_more) return
    const offset = activeTable.offset + activeTable.limit
    updateTab(activeTable.id, (tab) => tab.kind === 'table' ? { ...tab, offset } : tab)
    void tableApi.loadTablePage(activeTable.sessionId, activeTable.id, activeTable.schema, activeTable.table, activeTable.limit, offset, activeTable.filter, activeTable.order)
  }, { enabled: !!activeTable && !!activeTable.result?.has_more })
  useCommandRegistration('table.previousPage', () => {
    if (!activeTable || activeTable.offset <= 0) return
    const offset = Math.max(0, activeTable.offset - activeTable.limit)
    updateTab(activeTable.id, (tab) => tab.kind === 'table' ? { ...tab, offset } : tab)
    void tableApi.loadTablePage(activeTable.sessionId, activeTable.id, activeTable.schema, activeTable.table, activeTable.limit, offset, activeTable.filter, activeTable.order)
  }, { enabled: !!activeTable && activeTable.offset > 0 })
  useCommandRegistration('tab.close', () => { if (activeTab) tabsApi.closeTab(activeTab) }, { enabled: !!activeTab })
  useCommandRegistration('tab.closeOthers', () => { if (activeTab) tabsApi.closeOthers(activeTab) }, { enabled: !!activeTab })
  useCommandRegistration('tab.closeLeft', () => { if (activeTab) tabsApi.closeLeft(activeTab) }, { enabled: !!activeTab })
  useCommandRegistration('tab.closeRight', () => { if (activeTab) tabsApi.closeRight(activeTab) }, { enabled: !!activeTab })
  useCommandRegistration('tab.closeAll', tabsApi.closeAllTabs, { enabled: tabs.length > 0 })
  useCommandRegistration('tab.next', tabsApi.activateNextTab, { enabled: tabs.length > 1 })
  useCommandRegistration('tab.previous', tabsApi.activatePreviousTab, { enabled: tabs.length > 1 })
  useCommandRegistration('tab.activate.1', () => tabsApi.activateTabAt(0), { enabled: tabs.length > 0 })
  useCommandRegistration('tab.activate.2', () => tabsApi.activateTabAt(1), { enabled: tabs.length > 1 })
  useCommandRegistration('tab.activate.3', () => tabsApi.activateTabAt(2), { enabled: tabs.length > 2 })
  useCommandRegistration('tab.activate.4', () => tabsApi.activateTabAt(3), { enabled: tabs.length > 3 })
  useCommandRegistration('tab.activate.5', () => tabsApi.activateTabAt(4), { enabled: tabs.length > 4 })
  useCommandRegistration('tab.activate.6', () => tabsApi.activateTabAt(5), { enabled: tabs.length > 5 })
  useCommandRegistration('tab.activate.7', () => tabsApi.activateTabAt(6), { enabled: tabs.length > 6 })
  useCommandRegistration('tab.activate.8', () => tabsApi.activateTabAt(7), { enabled: tabs.length > 7 })
  useCommandRegistration('tab.activate.9', () => tabsApi.activateTabAt(8), { enabled: tabs.length > 8 })
  useCommandRegistration('workspace.history', () => tabsApi.openWorkspace('history'))
  useCommandRegistration('workspace.snippets', () => tabsApi.openWorkspace('snippets'))
  useCommandRegistration('workspace.dashboard', () => tabsApi.openWorkspace('server'))
  useCommandRegistration('workspace.appearance', () => tabsApi.openWorkspace('appearance'))
  useCommandRegistration('workspace.settings', () => tabsApi.openWorkspace('settings'))
  useCommandRegistration('workspace.shortcuts', () => tabsApi.openWorkspace('shortcuts'))
  useCommandRegistration('workspace.docs', tabsApi.openDocsTab)
  useCommandRegistration('split.right', () => { if (activeTab) split.openSplit(activeTab, 'horizontal') }, { enabled: tabs.length > 1 })
  useCommandRegistration('split.down', () => { if (activeTab) split.openSplit(activeTab, 'vertical') }, { enabled: tabs.length > 1 })
  useCommandRegistration('split.swap', split.swapSplit, { enabled: split.pinnedTab != null })
  useCommandRegistration('split.close', split.closeSplit, { enabled: split.pinnedTab != null })
  useCommandRegistration('explorer.refresh', () => void explorerApi.loadExplorer(activeId), { enabled: !!activeId })
  useCommandRegistration('session.connect', () => tabsApi.openWorkspace('connections'))
  useCommandRegistration('session.disconnect', () => sessionsApi.disconnect(), { enabled: connected })

  useEffect(() => {
    if (!activeId) return
    sessionsApi.refreshTxn()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  // Reopen last session's tabs on the first connection of this app load.
  const restored = useRef(false)
  const { restoreStoredTabs } = tabsApi
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

  const connectProfile = (profileId?: string, sshSecrets?: { password?: string; private_key?: string; passphrase?: string }) => {
    if (!profileId) return sessionsApi.connect(true)
    const profile = sessionsApi.saved.find((x) => x.id === profileId)
    if (!profile) return Promise.reject(new Error('Connection profile not found'))
    return sessionsApi.connectProfile(profile, sessionsApi.fields.password, sshSecrets)
  }

  const testConnection = async (profileId?: string, password?: string) => {
    const f = sessionsApi.fields
    const j = await apiClient.testConnection(profileId ? { profile_id: profileId, password: password || undefined } : { host: f.host, port: Number(f.port) || 5432, user: f.user, password: password ?? f.password, dbname: f.dbname, sslmode: f.sslmode })
    if (j.error) throw new Error(j.error)
    return { database: j.database, version: j.version, latency_ms: j.latency_ms }
  }

  const saveConnection = async (name: string, savePassword: boolean, clearPassword: boolean, metadata: ProfileMetadata): Promise<string> => {
    const f = sessionsApi.fields
    const j = await apiClient.saveConnection({ id: f.profileId, name, host: f.host, port: Number(f.port) || 5432, user: f.user, password: f.password, dbname: f.dbname, sslmode: f.sslmode, save_password: savePassword, clear_password: clearPassword, ...metadata })
    if (j.error || !j.connection) throw new Error(j.error ?? 'Failed to save connection')
    const c = j.connection
    sessionsApi.setSaved((list) => [{ id: c.id, name: c.name, host: c.host, port: String(c.port), user: c.user, password: '', dbname: c.dbname, sslmode: c.sslmode ?? f.sslmode, has_password: c.has_password, last_used_at: c.last_used_at, folder_id: c.folder_id, environment: c.environment, color: c.color, description: c.description, favorite: c.favorite, default: c.default, tags: c.tags, options: c.options }, ...list.filter((x) => x.id !== c.id)])
    sessionsApi.setFields({ ...f, password: '', profileId: c.id })
    return c.id ?? ''
  }

  const deleteConnection = async (id: string) => {
    const j = await apiClient.deleteConnection(id)
    if (j.error) throw new Error(j.error)
    sessionsApi.setSaved((list) => list.filter((x) => x.id !== id))
  }

  const duplicateConnection = async (name: string): Promise<string> => {
    const f = sessionsApi.fields
    if (!f.profileId) throw new Error('Select a saved profile first')
    const j = await apiClient.saveConnection({ duplicate_from: f.profileId, name, host: f.host, port: Number(f.port) || 5432, user: f.user, dbname: f.dbname, sslmode: f.sslmode })
    if (j.error || !j.connection) throw new Error(j.error ?? 'Failed to duplicate connection')
    const c = j.connection
    sessionsApi.setSaved((list) => [{ id: c.id, name: c.name, host: c.host, port: String(c.port), user: c.user, password: '', dbname: c.dbname, sslmode: c.sslmode ?? f.sslmode, has_password: c.has_password, last_used_at: c.last_used_at, folder_id: c.folder_id, environment: c.environment, color: c.color, description: c.description, favorite: c.favorite, default: c.default, tags: c.tags, options: c.options }, ...list.filter((x) => x.id !== c.id)])
    sessionsApi.setFields({ ...f, password: '', profileId: c.id })
    return c.id
  }

  return (
    <TooltipProvider delayDuration={200}>
    <div className="flex h-screen flex-col">
      <FloatingScrollbars />
      <DialogHost dlg={dlg} />
      <Toaster
        theme={appearance.mode}
        position="bottom-right"
        gap={6}
        closeButton
        icons={{
          success: <Check className="h-3.5 w-3.5 text-popover-foreground" />,
          error: null,
          info: <Info className="h-3.5 w-3.5 text-popover-foreground" />,
          loading: <Loader2 className="h-3.5 w-3.5 animate-spin text-popover-foreground" />,
          close: <X className="h-3 w-3" />,
        }}
        toastOptions={{
          style: {
            background: 'hsl(var(--popover))',
            color: 'hsl(var(--popover-foreground))',
            border: '1px solid hsl(var(--border))',
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
        searchShortcut={commands.formatBinding('palette.open')[0]}
        quickAccess={quickAccess}
        onWorkspace={(view) => tabsApi.openWorkspace(view)}
        onDocs={() => tabsApi.openDocsTab()}
        onShutdown={() => {
          void (async () => {
            const ok = await dialogs.confirm({
              title: 'Shutdown pglight?',
              description: 'Close all connections and stop the server. You will need to restart it manually.',
              confirmText: 'Shutdown',
              danger: true,
            })
            if (!ok) return
            try {
              const j = await apiClient.shutdown()
              if (j.error) toast.error(j.error)
              else toast.success('Server shutting down — connections closed')
            } catch (e) {
              toast.error(e instanceof Error ? e.message : String(e))
            }
          })()
        }}
      />
      <ResizablePanelGroup direction="horizontal" autoSaveId="pglight-main-layout" className="min-h-0 flex-1">
        <ResizablePanel defaultSize={20} minSize={12} maxSize={32} className="min-h-0">
        <aside className="flex h-full min-h-0 flex-col border-r bg-card">
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
        <ResizablePanel defaultSize={80} minSize={30} className="min-h-0">
        <main className="flex h-full min-h-0 min-w-0 flex-col">
          <TabStrip
            tabs={tabs}
            activeTab={activeTab}
            sessions={sessions}
            splitHint={tabs.length >= 2 && split.pinnedTab == null}
            pinnedId={split.pinnedId}
            onActivate={setActiveTab}
            onClose={tabsApi.closeTab}
            onCloseOthers={tabsApi.closeOthers}
            onCloseRight={tabsApi.closeRight}
            onCloseLeft={tabsApi.closeLeft}
            onCloseAll={tabsApi.closeAllTabs}
            onNewQuery={() => newQueryTab(undefined, activeId)}
            onSplit={split.openSplit}
            onUnpin={split.closeSplit}
          />
          <SplitWorkspace
            primary={split.pinnedTab ?? cur}
            secondary={split.pinnedTab != null ? cur : null}
            dir={split.splitDir}
            tabs={tabs}
            onDir={split.setSplitDir}
            onSecondary={setActiveTab}
            onSwap={split.swapSplit}
            onCloseSplit={split.closeSplit}
            renderPane={(t) => (
              <TabContent
                key={t?.id ?? 'empty'}
                tab={t}
                connected={connected}
                running={running}
                txnFor={sessionsApi.txnFor}
                markTxn={sessionsApi.markTxn}
                updateTab={updateTab}
                newQueryTab={newQueryTab}
                openErd={tabsApi.openErd}
                openTable={openTableForSession}
                query={queryApi}
                table={tableApi}
                object={objectApi}
                explorer={explorerApi}
                sessions={sessions}
                savedConnections={sessionsApi.saved}
                dialogs={dialogs}
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
                quickAccess={quickAccess}
                onQuickAccessChange={(view, enabled) => {
                  setQuickAccess((current) => {
                    const selected = new Set(enabled ? [...current, view] : current.filter((item) => item !== view))
                    return QUICK_ACCESS_VIEWS.filter((item) => selected.has(item))
                  })
                }}
                connection={{
                  fields: sessionsApi.fields,
                  setFields: sessionsApi.setFields,
                  saved: sessionsApi.saved,
                  onConnect: connectProfile,
                  onTest: testConnection,
                  onSave: saveConnection,
                  onDuplicate: duplicateConnection,
                  onDelete: deleteConnection,
                  vault: sessionsApi.vault,
                  onVaultAction: sessionsApi.vaultAction,
                  autoLogin: sessionsApi.autoLogin,
                  onAutoLogin: sessionsApi.setAutoLogin,
                  sessions,
                  activeId,
                  onSwitch: (id) => sessionsApi.setActiveId(id),
                  onDisconnectOne: (id) => sessionsApi.disconnect(id),
                  deadIds: sessionsApi.deadIds,
                  onReconnectOne: (id) => void sessionsApi.reconnectOne(id),
                  onReconnectAll: () => void sessionsApi.reconnectAll(),
                }}
              />
            )}
          />
        </main>
        </ResizablePanel>
      </ResizablePanelGroup>
      <SearchPalette key={paletteOpen ? 'open' : 'closed'} open={paletteOpen} onOpenChange={setPaletteOpen} session={session} onOpenTable={openTableForSession} />
    </div>
    </TooltipProvider>
  )
}
