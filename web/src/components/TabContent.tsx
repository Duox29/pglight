import { toast } from 'sonner'
import { BrowserView } from './BrowserView'
import { DocsView } from './DocsView'
import { ErdView } from './ErdView'
import { ObjectView } from './ObjectView'
import { QueryConsole } from './QueryConsole'
import { TableWorkspace } from './TableWorkspace'
import { SidePanel } from './SidePanel'
import type { ProfileMetadata } from './CredentialManager'
import type { DialogsApi } from './dialogs'
import type { UseExplorer } from '../hooks/useExplorer'
import type { UseObjectOps } from '../hooks/useObjectOps'
import type { UseQueryRunner } from '../hooks/useQueryRunner'
import type { UseSessions } from '../hooks/useSessions'
import type { UseTableOps } from '../hooks/useTableOps'
import type { UseTabs } from '../hooks/useTabs'
import { api, apiClient, q } from '../lib/api'
import { download, resultToCSV, resultToInserts, resultToJSON } from '../lib/format'
import { erdConnectionIdFor, pkOf, qi } from '../lib/tabs'
import type { HistoryEntry, SideView, Snippet, Tab } from '../types'
import type { ConnFields } from './ConnectionBar'
import type { SavedConnection, SessionInfo } from '../types'

const erdRequestSeq = new Map<string, number>()

function nextErdRequest(tabId: string) {
  const token = (erdRequestSeq.get(tabId) ?? 0) + 1
  erdRequestSeq.set(tabId, token)
  return token
}

function isCurrentErdRequest(tabId: string, token: number) {
  return erdRequestSeq.get(tabId) === token
}

export interface TabContentProps {
  tab: Tab | null
  connected: boolean
  running: UseQueryRunner['running']
  txnFor: UseSessions['txnFor']
  markTxn: UseSessions['markTxn']
  updateTab: UseTabs['updateTab']
  newQueryTab: UseTabs['newQueryTab']
  openErd: UseTabs['openErd']
  openTable: (schema: string, table: string, sid?: string) => void
  query: Pick<UseQueryRunner, 'runQuery' | 'cancelQuery' | 'explainQuery' | 'setSnippets'>
  table: Pick<UseTableOps, 'loadTablePage' | 'loadTableMeta' | 'rowOp' | 'alterTable' | 'renameTable'>
  object: Pick<UseObjectOps, 'loadObjectDef' | 'saveSequence' | 'saveFunction' | 'enumAdd' | 'renameObject' | 'dropObject'>
  explorer: Pick<UseExplorer, 'copyName' | 'schemas'>
  sessions: UseSessions['sessions']
  savedConnections: UseSessions['saved']
  dialogs: DialogsApi
  session: string
  history: HistoryEntry[]
  snippets: Snippet[]
  onOpenSql: (sql: string) => void
  onDeleteSnippet: (i: number) => void
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

/* Tab-kind composition root (succeeds the former inline switch in App.tsx):
   pure delegation from one tab to its feature view. Owns no state; every
   action arrives via props as hook APIs bound in App. Feature views stay
   presentational and never import each other. */
function TabContentInner(p: TabContentProps) {
  const { tab } = p
  if (tab == null) {
    return <div className="text-muted-foreground">{p.connected ? 'Open a table or run a query.' : 'Connect to a database to begin.'}</div>
  }
  if (tab.kind === 'workspace') {
    return (
      <SidePanel
        view={tab.view}
        onView={(view) => p.updateTab(tab.id, (x) => x.kind === 'workspace' ? { ...x, view } : x)}
        session={p.session}
        history={p.history}
        snippets={p.snippets}
        onOpenSql={p.onOpenSql}
        onDeleteSnippet={p.onDeleteSnippet}
        dialogs={p.dialogs}
        quickAccess={p.quickAccess}
        onQuickAccessChange={p.onQuickAccessChange}
        connection={p.connection}
      />
    )
  }
  if (tab.kind === 'query') {
    return (
      <QueryConsole
        key={tab.id}
        tab={tab}
        running={!!p.running[tab.id]}
        {...p.txnFor(tab.sessionId)}
        onSqlChange={(sql) => p.updateTab(tab.id, (x) => (x.kind === 'query' ? { ...x, sql } : x))}
        onRun={(sql) => p.query.runQuery(tab.id, sql)}
        onCancel={() => p.query.cancelQuery(tab.id)}
        onClearResults={() => p.updateTab(tab.id, (x) => (x.kind === 'query' ? { ...x, results: null, error: undefined, errLoc: undefined, flashTick: undefined, plan: undefined, meta: undefined } : x))}
        onExplain={(sql) => p.query.explainQuery(tab.id, sql)}
        onLimit={(n) => p.updateTab(tab.id, (x) => (x.kind === 'query' ? { ...x, limit: n } : x))}
        onSaveSnippet={async () => {
          const name = await p.dialogs.prompt({
            title: 'Save snippet',
            defaultValue: tab.sql.slice(0, 40),
          })
          if (name) {
            const j = await apiClient.saveSnippet(name.trim(), tab.sql)
            if (j.error || !j.snippet) toast.error(j.error ?? 'Failed to save snippet')
            else p.query.setSnippets((s) => [{ name: j.snippet!.name, sql: j.snippet!.sql }, ...s.filter((x) => x.name !== j.snippet!.name)])
          }
        }}
        dialogs={p.dialogs}
      />
    )
  }
  if (tab.kind === 'table') {
    return (
      <TableWorkspace
        tab={tab}
        onSubtab={(s) => {
          p.updateTab(tab.id, (x) => (x.kind === 'table' ? { ...x, subtab: s } : x))
          if (s !== 'data') p.table.loadTableMeta(tab.sessionId, tab.id, tab.schema, tab.table)
        }}
        onFilterChange={(filter, order) => p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, 0, filter, order)}
        onApply={() => p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, tab.offset, tab.filter, tab.order)}
        onPage={(d) => p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, Math.max(0, tab.offset + d * tab.limit), tab.filter, tab.order)}
        onEditCell={async (col, orig) => {
          const keys = pkOf(tab)
          if (!keys.length) {
            toast.error('No primary key — editing is disabled for this table')
            return
          }
          const missing = keys.filter((k) => orig[k] == null)
          if (missing.length) {
            toast.error(`Primary key column ${missing.join(', ')} is NULL — cannot identify this row`)
            return
          }
          const v = await p.dialogs.promptNullable({
            title: `Edit ${col}`,
            description: 'Leave empty to keep the value as-is, or Set NULL for NULL',
            defaultValue: orig[col] == null ? '' : String(orig[col]),
          })
          if (v == null) return
          const value = typeof v === 'object' ? null : v
          const where: Record<string, unknown> = {}
          for (const k of keys) where[k] = orig[k]
          p.table.rowOp(tab.id, 'update', { [col]: value }, where, true)
        }}
        onDeleteRow={async (orig) => {
          const keys = pkOf(tab)
          if (!keys.length) {
            toast.error('No primary key — deletion is disabled for this table')
            return
          }
          const ok = await p.dialogs.confirm({
            title: 'Delete row?',
            description: `This will delete this row from ${tab.schema}.${tab.table}.`,
            confirmText: 'Delete',
            danger: true,
          })
          if (!ok) return
          const where: Record<string, unknown> = {}
          for (const k of keys) where[k] = orig[k]
          p.table.rowOp(tab.id, 'delete', {}, where, true)
        }}
        onCopyInsert={(orig) => {
          p.explorer.copyName(resultToInserts(Object.keys(orig), [Object.values(orig)], `${qi(tab.schema)}.${qi(tab.table)}`))
        }}
        onExportRows={(rows, fmt) => {
          if (!rows.length) return
          const cols = tab.result?.columns ?? Object.keys(rows[0])
          const types = tab.result?.types
          const matrix = rows.map((r) => cols.map((c) => r[c]))
          const base = `${tab.schema}.${tab.table}-selected`
          if (fmt === 'csv') download(resultToCSV(cols, matrix), `${base}.csv`, 'text/csv')
          else if (fmt === 'json') download(resultToJSON(cols, matrix), `${base}.json`, 'application/json')
          else download(resultToInserts(cols, matrix, `${qi(tab.schema)}.${qi(tab.table)}`, types), `${base}.sql`, 'text/sql')
        }}
        onCopyRows={(rows) => {
          if (!rows.length) return
          const cols = tab.result?.columns ?? Object.keys(rows[0])
          p.explorer.copyName(resultToCSV(cols, rows.map((r) => cols.map((c) => r[c]))))
        }}
        onDeleteRows={async (rows) => {
          if (!rows.length) return
          if (!tab.sessionId) {
            toast.error('Session closed — reconnect to delete rows')
            return
          }
          const keys = pkOf(tab)
          if (!keys.length) {
            toast.error('No primary key — deletion is disabled for this table')
            return
          }
          const ok = await p.dialogs.confirm({
            title: `Delete ${rows.length} row${rows.length === 1 ? '' : 's'}?`,
            description: `This will delete ${rows.length} row${rows.length === 1 ? '' : 's'} from ${tab.schema}.${tab.table}.`,
            confirmText: 'Delete',
            danger: true,
          })
          if (!ok) return
          try {
            // PK-only predicates; the server deletes them in one
            // transaction and aborts on the first non-unique match.
            const where = rows.map((r) => Object.fromEntries(keys.map((k) => [k, r[k]])))
            const j = await apiClient.batchDelete({ session_id: tab.sessionId, schema: tab.schema, table: tab.table, where })
            if (j.error) toast.error(j.error)
            else {
              p.markTxn(tab.sessionId, !!j.in_txn)
              toast.success(`Deleted ${j.deleted ?? rows.length} row${(j.deleted ?? rows.length) === 1 ? '' : 's'}`)
            }
            p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, tab.offset, tab.filter, tab.order)
          } catch (e) {
            toast.error(e instanceof Error ? e.message : String(e))
            p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, tab.offset, tab.filter, tab.order)
          }
        }}
        onInsert={async () => {
          if (!tab.result) return
          const vals = await p.dialogs.form({
            title: `Insert into ${tab.schema}.${tab.table}`,
            description: 'Empty = skip · per-field N button = NULL',
            fields: tab.result.columns.map((c) => ({ key: c, label: c, allowNull: true })),
            submitText: 'Insert',
          })
          if (!vals) return
          const clean: Record<string, unknown> = {}
          for (const [k, v] of Object.entries(vals)) {
            if (v === null) clean[k] = null
            else if (v !== '') clean[k] = v
          }
          p.table.rowOp(tab.id, 'insert', clean, {})
        }}
        onMaintenance={async (op) => {
          const ok = await p.dialogs.confirm({
            title: `${op.toUpperCase()} ${tab.schema}.${tab.table}?`,
            confirmText: op.toUpperCase(),
          })
          if (!ok) return
          try {
            const j = await apiClient.maintenance(tab.sessionId, tab.schema, tab.table, op)
            if (j.error) toast.error(j.error)
            else {
              toast.success('OK: ' + (j.result || op))
              p.table.loadTableMeta(tab.sessionId, tab.id, tab.schema, tab.table)
            }
          } catch (e) {
            toast.error(e instanceof Error ? e.message : String(e))
          }
        }}
        onImport={async (columns, rows) => {
          try {
            const j = await apiClient.importRows({ session_id: tab.sessionId, schema: tab.schema, table: tab.table, columns, rows })
            if (j.error) toast.error(j.error)
            else {
              toast.success(`Imported ${j.rows_affected} rows`)
              p.table.loadTablePage(tab.sessionId, tab.id, tab.schema, tab.table, tab.limit, tab.offset, tab.filter, tab.order)
            }
          } catch (e) {
            toast.error(e instanceof Error ? e.message : String(e))
          }
        }}
        onOpenErd={() => p.openErd(tab.schema, tab.sessionId)}
        onAlter={(pl) => p.table.alterTable(tab.id, pl)}
        onRenameTable={() => p.table.renameTable(tab.id)}
        dialogs={p.dialogs}
      />
    )
  }
  if (tab.kind === 'browser') {
    return (
      <BrowserView
        tab={tab}
        onReload={() =>
          api<Record<string, unknown>[] | { error: string }>(q(tab.sessionId, `${tab.url}?`))
            .then((j) => {
              p.updateTab(tab.id, (x) =>
                x.kind === 'browser' ? { ...x, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : x,
              )
            })
            .catch((e: unknown) => {
              p.updateTab(tab.id, (x) => (x.kind === 'browser' ? { ...x, rows: null, error: e instanceof Error ? e.message : String(e) } : x))
            })
        }
      />
    )
  }
  if (tab.kind === 'erd') {
    return (
      <ErdView
        tab={tab}
        connectionId={erdConnectionIdFor(p.sessions, p.savedConnections, tab)}
        schemas={p.explorer.schemas.map((s) => s.schema)}
        onSchema={(s) => {
          const token = nextErdRequest(tab.id)
          p.updateTab(tab.id, (x) => (x.kind === 'erd' ? { ...x, schema: s, title: `ERD ${s}`, data: null } : x))
          api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(tab.sessionId, `/api/erd?schema=${encodeURIComponent(s)}`))
            .then((j) => {
              if (!isCurrentErdRequest(tab.id, token)) return
              p.updateTab(tab.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
            })
            .catch((e: unknown) => {
              if (!isCurrentErdRequest(tab.id, token)) return
              p.updateTab(tab.id, (x) => (x.kind === 'erd' ? { ...x, data: null } : x))
              toast.error(e instanceof Error ? e.message : String(e))
            })
        }}
        onReload={() => {
          const token = nextErdRequest(tab.id)
          void api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(tab.sessionId, `/api/erd?schema=${encodeURIComponent(tab.schema)}`))
            .then((j) => {
              if (!isCurrentErdRequest(tab.id, token)) return
              p.updateTab(tab.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
            })
            .catch((e: unknown) => {
              if (!isCurrentErdRequest(tab.id, token)) return
              p.updateTab(tab.id, (x) => (x.kind === 'erd' ? { ...x, data: null } : x))
              toast.error(e instanceof Error ? e.message : String(e))
            })
        }}
        onOpenTable={(schema, table) => p.openTable(schema, table, tab.sessionId)}
      />
    )
  }
  if (tab.kind === 'object') {
    return (
      <ObjectView
        tab={tab}
        onReload={() => p.object.loadObjectDef(tab.sessionId, tab.id, tab.objectKind, tab.schema, tab.name)}
        onCopy={p.explorer.copyName}
        onOpenQuery={() =>
          p.newQueryTab(
            tab.def ?? `-- ${tab.objectKind} ${tab.schema}.${tab.name}\nSELECT 1;`,
            tab.sessionId,
          )
        }
        onSaveSequence={(o) => p.object.saveSequence(tab.id, o)}
        onSaveFunction={(sql) => p.object.saveFunction(tab.id, sql)}
        onEnumAdd={(v) => p.object.enumAdd(tab.id, v)}
        onRenameRequest={() => p.object.renameObject(tab.id)}
        onDropRequest={() => p.object.dropObject(tab.id)}
      />
    )
  }
  return <DocsView />
}

/** Remount tab-local feature state when the pane switches to another tab. */
export function TabContent(p: TabContentProps) {
  return <TabContentInner key={p.tab?.id ?? 'empty'} {...p} />
}
