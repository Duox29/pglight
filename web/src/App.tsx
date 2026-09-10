import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Loader2, Plus, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { Button } from './components/ui/button'
import { Card } from './components/ui/card'
import { Separator } from './components/ui/separator'
import { ConnectionBar, type ConnFields } from './components/ConnectionBar'
import { CredentialManager } from './components/CredentialManager'
import { DialogHost, createDialogs, type PendingDialog } from './components/dialogs'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './components/ui/resizable'
import { Explorer } from './components/Explorer'
import { QueryConsole, explainToText } from './components/QueryConsole'
import { TableWorkspace } from './components/TableWorkspace'
import { BrowserView } from './components/BrowserView'
import { DocsView } from './components/DocsView'
import { ErdView } from './components/ErdView'
import { SidePanel } from './components/SidePanel'
import { SearchPalette } from './components/SearchPalette'
import { api, apiClient, q } from './lib/api'
import { useLocalStorage } from './lib/storage'
import type {
  DbInfo,
  HistoryEntry,
  ObjectDetail,
  SavedConnection,
  SchemaGroup,
  SideView,
  Snippet,
  StoredTab,
  Tab,
  TableSubtab,
  TableTabT,
} from './types'
import { cn } from './lib/utils'

const DEFAULT_SQL = 'SELECT * FROM information_schema.tables LIMIT 20;'

const BROWSER_DEFS: Record<string, { title: string; url: string; cols: string[] }> = {
  extensions: { title: 'Extensions', url: '/api/extensions', cols: ['name', 'default_version', 'installed_version', 'comment'] },
  roles: { title: 'Roles', url: '/api/roles', cols: ['name', 'superuser', 'login', 'createdb', 'member_of'] },
}

function slimTab(t: Tab): StoredTab | null {
  switch (t.kind) {
    case 'query': {
      // Keep a capped snapshot of the last result (never auto re-run: the SQL
      // could be DML). Oversized snapshots are dropped to protect storage.
      let snapshot: StoredTab['snapshot']
      const r = t.results?.[0]
      if (r && r.columns?.length && !r.stale) {
        const snap = {
          columns: r.columns,
          rows: (r.rows ?? []).slice(0, 50).map((row) => row.map((c) => (typeof c === 'string' ? c.slice(0, 200) : c))),
          at: new Date().toLocaleTimeString(),
        }
        try {
          if (JSON.stringify(snap).length <= 200_000) snapshot = snap
        } catch {
          snapshot = undefined
        }
      }
      return { id: t.id, kind: t.kind, title: t.title, sql: t.sql, limit: t.limit, snapshot }
    }
    case 'table':
      return { id: t.id, kind: t.kind, title: t.title, schema: t.schema, table: t.table, subtab: t.subtab, limit: t.limit, offset: t.offset, filter: t.filter, order: t.order }
    case 'browser':
      return { id: t.id, kind: t.kind, title: t.title, key: t.id.replace(/^b_/, '') }
    case 'erd':
      return { id: t.id, kind: t.kind, title: t.title, schema: t.schema }
    case 'docs':
      return { id: 'docs', kind: 'docs', title: 'Docs' }
  }
}

function readJSON<T>(key: string, fallback: T): T {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? 'null') as T
    return v ?? fallback
  } catch {
    return fallback
  }
}

function readStoredTabs(): StoredTab[] {
  try {
    const raw = JSON.parse(localStorage.getItem('open-tabs') ?? '[]') as StoredTab[]
    if (!Array.isArray(raw)) return []
    return raw.filter((t) => t && typeof t.id === 'string' && typeof t.kind === 'string').slice(0, 20)
  } catch {
    return []
  }
}

export default function App() {
  const [fields, setFields] = useState<ConnFields>({ host: 'localhost', port: '5432', user: 'postgres', password: '', dbname: 'postgres', sslmode: 'disable' })
  const [saved, setSaved] = useLocalStorage<SavedConnection[]>('conns', [])
  const [session, setSession] = useLocalStorage<string>('sid', '')
  // Always starts disconnected; a stored sid is only trusted after the boot
  // check below verifies it (a server restart invalidates all sessions).
  const [connected, setConnected] = useState(false)
  const [inTxn, setInTxn] = useState(false)
  const [autocommit, setAutocommit] = useLocalStorage<boolean>('autocommit', true)
  const [autoLogin, setAutoLogin] = useLocalStorage<boolean>('auto-login', true)
  const [databases, setDatabases] = useState<DbInfo[]>([])
  const [schemas, setSchemas] = useState<SchemaGroup[]>([])
  const [detail, setDetail] = useState<ObjectDetail | null>(null)
  const [tabs, setTabs] = useState<Tab[]>([])
  const [activeTab, setActiveTab] = useState<string | null>(null)
  const [running, setRunning] = useState<Record<string, boolean>>({})
  const [history, setHistory] = useLocalStorage<HistoryEntry[]>('hist', [])
  const [snippets, setSnippets] = useLocalStorage<Snippet[]>('snippets', [])
  const [sideOpen, setSideOpen] = useState(false)
  const [sideView, setSideView] = useState<SideView>('history')
  const [credOpen, setCredOpen] = useLocalStorage<boolean>('conn-panel-open', true)
  const [dlg, setDlg] = useState<PendingDialog | null>(null)
  const dialogs = useMemo(() => createDialogs(setDlg), [])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const tabSeq = useRef(0)

  const cur = tabs.find((t) => t.id === activeTab) ?? null
  const updateTab = useCallback((id: string, fn: (t: Tab) => Tab) => {
    setTabs((prev) => prev.map((t) => (t.id === id ? fn(t) : t)))
  }, [])

  /* ---------- connection ---------- */
  const bootConnect = useCallback(
    async (f: ConnFields, fresh: boolean, dbnameOver?: string) => {
      const j = await apiClient.connect({
        host: f.host,
        port: Number(f.port) || 5432,
        user: f.user,
        password: f.password,
        dbname: dbnameOver || f.dbname,
        sslmode: f.sslmode,
        session_id: fresh ? '' : session || undefined,
      })
      if (j.error || !j.session_id) {
        toast.error(j.error ?? 'Connect failed')
        return false
      }
      try {
        localStorage.setItem('last-conn', JSON.stringify({ ...f, dbname: dbnameOver || f.dbname }))
      } catch {
        /* private mode etc. — autologin just won't persist */
      }
      setSession(j.session_id)
      if (dbnameOver) setFields((f0) => ({ ...f0, dbname: dbnameOver }))
      setConnected(true)
      setInTxn(false)
      return true
    },
    [session, setSession],
  )

  const connect = useCallback(
    (fresh: boolean, dbnameOver?: string) => bootConnect(fields, fresh, dbnameOver),
    [bootConnect, fields],
  )

  const disconnect = useCallback(() => {
    if (session) apiClient.disconnect(session)
    setSession('')
    setConnected(false)
    setInTxn(false)
  }, [session, setSession])

  const refreshTxn = useCallback(async () => {
    if (!session) return
    const j = await apiClient.txn(session, 'status')
    setInTxn(!!j.in_txn)
  }, [session])

  const doTxn = useCallback(
    async (action: string) => {
      if (!session) return
      const j = await apiClient.txn(session, action)
      if (j.error) toast.error(j.error)
      setInTxn(!!j.in_txn)
    },
    [session],
  )

  /* ---------- explorer ---------- */
  const loadExplorer = useCallback(async () => {
    if (!session) return
    const [sch, tbl, dbs] = await Promise.all([
      api<unknown>(q(session, '/api/schemas?')),
      api<unknown>(q(session, '/api/tables?')),
      api<unknown>(q(session, '/api/databases?')),
    ])
    const [views, matviews, foreign, functions, sequences, types] = await Promise.all(
      (['views', 'matviews', 'foreign', 'functions', 'sequences', 'types'] as const).map((k) =>
        api<unknown>(q(session, `/api/objects?kind=${k}`)).catch(() => []),
      ),
    )
    const by: Record<string, SchemaGroup> = {}
    const mk = (s: string): SchemaGroup => ({ schema: s, tables: [], views: [], matviews: [], foreign: [], functions: [], sequences: [], types: [] })
    const put = (arr: unknown, bucket: keyof Omit<SchemaGroup, 'schema'>) => {
      if (!Array.isArray(arr)) return
      for (const o of arr as { schema: string; name: string }[]) {
        const s = o.schema || 'public'
        by[s] = by[s] || mk(s)
        ;(by[s][bucket] as { schema: string; name: string }[]).push(o)
      }
    }
    if (Array.isArray(tbl)) {
      for (const t of tbl as { schema: string; name: string; type: string }[]) {
        const s = t.schema || 'public'
        by[s] = by[s] || mk(s)
        if (t.type === 'VIEW') by[s].views.push(t)
        else by[s].tables.push(t)
      }
    }
    put(views, 'views')
    put(matviews, 'matviews')
    put(foreign, 'foreign')
    put(functions, 'functions')
    put(sequences, 'sequences')
    put(types, 'types')
    const list: SchemaGroup[] = Array.isArray(sch) ? (sch as string[]).map((s) => by[s] || mk(s)) : Object.values(by)
    for (const s of Object.keys(by)) if (!list.find((x) => x.schema === s)) list.push(by[s])
    list.sort((a, b) => a.schema.localeCompare(b.schema))
    setSchemas(list)
    setDatabases(Array.isArray(dbs) ? (dbs as DbInfo[]) : [])
  }, [session])

  useEffect(() => {
    if (!session) return
    // External-system sync: reload server state on session change. Setters run
    // in async continuations after fetch, not during render.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadExplorer()
    refreshTxn()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session])

  const switchDb = useCallback(
    async (name: string) => {
      const ok = await dialogs.confirm({
        title: `Reconnect to "${name}"?`,
        description: 'The current session will be replaced.',
        confirmText: 'Reconnect',
      })
      if (ok) connect(true, name)
    },
    [connect, dialogs],
  )

  /* ---------- object detail ---------- */
  const showDDL = useCallback(
    async (schema: string, table: string) => {
      if (!session) return
      const j = await api<Record<string, unknown>>(q(session, `/api/ddl?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`))
      if ((j as { error?: string }).error) {
        setDetail({ title: `${schema}.${table}`, kind: 'table', body: <span className="text-red-400">{(j as { error: string }).error}</span> })
        return
      }
      const d = j as unknown as { ddl: string; owner?: string; comment?: string; constraints?: { def: string }[]; indexes?: { def: string }[] }
      setDetail({
        title: `${schema}.${table}`,
        kind: 'table',
        body: (
          <div className="flex flex-col gap-1.5">
            {d.owner && <div className="text-muted-foreground">owner: {d.owner}{d.comment ? ` · ${d.comment}` : ''}</div>}
            <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">{d.ddl}</pre>
            <b className="text-[11px]">Constraints</b>
            <pre className="overflow-auto font-mono text-[11px]">{(d.constraints ?? []).map((c) => c.def).join('\n') || '—'}</pre>
            <b className="text-[11px]">Indexes</b>
            <pre className="overflow-auto font-mono text-[11px]">{(d.indexes ?? []).map((c) => c.def).join('\n') || '—'}</pre>
          </div>
        ),
      })
    },
    [session],
  )

  const showView = useCallback(
    async (schema: string, name: string) => {
      if (!session) return
      const j = await api<{ definition?: string; error?: string }>(q(session, `/api/view-def?schema=${encodeURIComponent(schema)}&name=${encodeURIComponent(name)}`))
      setDetail({
        title: `${schema}.${name}`,
        kind: 'view',
        body: j.error ? <span className="text-red-400">{j.error}</span> : <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">{j.definition}</pre>,
      })
    },
    [session],
  )

  const showFunc = useCallback(
    async (schema: string, name: string) => {
      if (!session) return
      const j = await api<{ name: string; lang: string; def: string }[] | { error: string }>(
        q(session, `/api/func-def?schema=${encodeURIComponent(schema)}&name=${encodeURIComponent(name)}`),
      )
      setDetail({
        title: name,
        kind: 'function',
        body: Array.isArray(j) ? (
          <div className="flex flex-col gap-1.5">
            {j.map((f, i) => (
              <div key={i}>
                <b className="text-[11px]">{f.name}</b> <span className="text-muted-foreground">{f.lang}</span>
                <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">{f.def}</pre>
              </div>
            ))}
          </div>
        ) : (
          <span className="text-red-400">{(j as { error: string }).error}</span>
        ),
      })
    },
    [session],
  )

  const showSeq = useCallback((schema: string, name: string) => {
    setDetail({
      title: `${schema}.${name}`,
      kind: 'sequence',
      body: <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">{`SELECT * FROM ${schema}.${name};\n-- nextval('${schema}.${name}')`}</pre>,
    })
  }, [])

  const showType = useCallback(
    async (schema: string, name: string) => {
      if (!session) return
      const j = await api<{ name: string; kind: string; labels: string; comment: string }[]>(q(session, `/api/types?schema=${encodeURIComponent(schema)}`))
      const t = Array.isArray(j) ? j.find((x) => x.name === name) : undefined
      setDetail({
        title: `${schema}.${name}`,
        kind: 'type',
        body: t ? (
          <div>
            <span className="text-muted-foreground">{t.kind}</span>
            <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">{t.labels || t.comment}</pre>
          </div>
        ) : (
          <span className="text-muted-foreground">no info</span>
        ),
      })
    },
    [session],
  )

  /* ---------- tabs ---------- */
  const newQueryTab = useCallback(
    (sql?: string) => {
      tabSeq.current += 1
      const id = `q${tabSeq.current}`
      setTabs((prev) => [...prev, { id, kind: 'query', title: `Query ${tabSeq.current}`, sql: sql ?? DEFAULT_SQL, limit: 200, results: null }])
      setActiveTab(id)
    },
    [],
  )

  /* ---------- table ops ---------- */
  const loadTablePage = useCallback(
    async (id: string, schema: string, table: string, limit: number, offset: number, filter: string, order: string) => {
      if (!session) return
      const j = await api<import('./lib/api').QueryResult & { total?: number; in_txn?: boolean; error?: string }>(
        q(session, `/api/table-data?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}&limit=${limit}&offset=${offset}&filter=${encodeURIComponent(filter)}&order=${encodeURIComponent(order)}`),
      )
      if (j.error) {
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, error: j.error } : t)))
        return
      }
      setInTxn(!!j.in_txn)
      setTabs((prev) =>
        prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, result: j, error: undefined, limit, offset, filter, order } : t)),
      )
    },
    [session],
  )

  const loadTableMeta = useCallback(
    async (id: string, schema: string, table: string) => {
      if (!session) return
      const [cols, ddl, cons, trg, stats] = await Promise.all([
        api<unknown>(q(session, `/api/columns?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(session, `/api/ddl?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(session, `/api/constraints?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(session, `/api/triggers?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(session, `/api/table-stats?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
      ])
      setTabs((prev) =>
        prev.map((t) => {
          if (t.id !== id || t.kind !== 'table') return t
          return {
            ...t,
            cols: Array.isArray(cols) ? (cols as Record<string, unknown>[]) : t.cols,
            ddl: ddl && !(ddl as { error?: string }).error ? (ddl as TableTabT['ddl']) : t.ddl,
            constraints: Array.isArray(cons) ? (cons as TableTabT['constraints']) : t.constraints,
            triggers: Array.isArray(trg) ? (trg as TableTabT['triggers']) : t.triggers,
            stats: stats && !(stats as { error?: string }).error ? (stats as Record<string, unknown>) : t.stats,
          }
        }),
      )
    },
    [session],
  )

  const needSession = useCallback(
    (what: string) => {
      if (!session) {
        toast.error(`Connect to a database first — cannot open ${what}`)
        return false
      }
      return true
    },
    [session],
  )

  const openTableTab = useCallback(
    (schema: string, table: string) => {
      if (!needSession(`${schema}.${table}`)) return
      const id = `t_${schema}_${table}`
      setTabs((prev) =>
        prev.find((t) => t.id === id)
          ? prev
          : [...prev, { id, kind: 'table', title: table, schema, table, subtab: 'data' as TableSubtab, limit: 100, offset: 0, filter: '', order: '', result: null, cols: null, ddl: null, constraints: null, triggers: null, stats: null }],
      )
      setActiveTab(id)
      loadTablePage(id, schema, table, 100, 0, '', '')
      loadTableMeta(id, schema, table)
    },
    [loadTablePage, loadTableMeta, needSession],
  )

  const openBrowser = useCallback(
    (key: 'extensions' | 'roles', title: string) => {
      if (!needSession(title)) return
      const id = `b_${key}`
      const url = key === 'extensions' ? '/api/extensions' : '/api/roles'
      const cols = key === 'extensions' ? ['name', 'default_version', 'installed_version', 'comment'] : ['name', 'superuser', 'login', 'createdb', 'member_of']
      setTabs((prev) => (prev.find((t) => t.id === id) ? prev : [...prev, { id, kind: 'browser', title, url, cols, rows: null }]))
      setActiveTab(id)
      if (session) {
        api<Record<string, unknown>[] | { error: string }>(q(session, `${url}?`)).then((j) => {
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'browser' ? { ...t, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : t)))
        })
      }
    },
    [session, needSession],
  )

  const openErd = useCallback(
    (schema: string) => {
      if (!needSession(`ERD ${schema}`)) return
      const id = `e_${schema}`
      setTabs((prev) => (prev.find((t) => t.id === id) ? prev : [...prev, { id, kind: 'erd', title: `ERD ${schema}`, schema, data: null }]))
      setActiveTab(id)
      if (session) {
        api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(session, `/api/erd?schema=${encodeURIComponent(schema)}`)).then((j) => {
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'erd' ? { ...t, data: j } : t)))
        })
      }
    },
    [session, needSession],
  )

  /* ---------- last-session restore (tabs + autologin) ---------- */
  const restoreStoredTabs = useCallback(
    (stored: StoredTab[]) => {
      const rebuilt: Tab[] = []
      let maxQ = 0
      for (const s of stored) {
        if (s.kind === 'query') {
          const m = /^q(\d+)$/.exec(s.id)
          if (m) maxQ = Math.max(maxQ, Number(m[1]))
          rebuilt.push({
            id: s.id, kind: 'query', title: s.title || 'Query', sql: s.sql ?? DEFAULT_SQL, limit: s.limit ?? 200,
            results: s.snapshot ? [{ columns: s.snapshot.columns, rows: s.snapshot.rows, rows_affected: s.snapshot.rows.length, stale: true }] : null,
          })
        } else if (s.kind === 'table' && s.schema && s.table) {
          rebuilt.push({
            id: `t_${s.schema}_${s.table}`, kind: 'table', title: s.table, schema: s.schema, table: s.table,
            subtab: s.subtab ?? 'data', limit: s.limit ?? 100, offset: s.offset ?? 0, filter: s.filter ?? '', order: s.order ?? '',
            result: null, cols: null, ddl: null, constraints: null, triggers: null, stats: null,
          })
        } else if (s.kind === 'browser' && s.key && BROWSER_DEFS[s.key]) {
          const d = BROWSER_DEFS[s.key]
          rebuilt.push({ id: `b_${s.key}`, kind: 'browser', title: d.title, url: d.url, cols: d.cols, rows: null })
        } else if (s.kind === 'erd' && s.schema) {
          rebuilt.push({ id: `e_${s.schema}`, kind: 'erd', title: `ERD ${s.schema}`, schema: s.schema, data: null })
        } else if (s.kind === 'docs') {
          rebuilt.push({ id: 'docs', kind: 'docs', title: 'Docs' })
        }
      }
      if (!rebuilt.length) {
        newQueryTab()
        return
      }
      tabSeq.current = maxQ
      setTabs(rebuilt)
      const wantActive = (() => {
        try {
          return localStorage.getItem('active-tab')
        } catch {
          return null
        }
      })()
      setActiveTab(wantActive && rebuilt.some((t) => t.id === wantActive) ? wantActive : rebuilt[0].id)
      if (!session) return
      for (const t of rebuilt) {
        if (t.kind === 'table') {
          void loadTablePage(t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
          void loadTableMeta(t.id, t.schema, t.table)
        } else if (t.kind === 'browser') {
          api<Record<string, unknown>[] | { error: string }>(q(session, `${t.url}?`)).then((j) => {
            setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'browser' ? { ...x, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : x)))
          })
        } else if (t.kind === 'erd') {
          api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(session, `/api/erd?schema=${encodeURIComponent(t.schema)}`)).then((j) => {
            setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'erd' ? { ...x, data: j } : x)))
          })
        }
      }
    },
    [session, loadTablePage, loadTableMeta, newQueryTab],
  )

  const openDocsTab = useCallback(() => {
    setTabs((prev) => (prev.find((t) => t.id === 'docs') ? prev : [...prev, { id: 'docs', kind: 'docs', title: 'Docs' }]))
    setActiveTab('docs')
  }, [])

  const closeTab = (id: string) => {
    setTabs((prev) => {
      const next = prev.filter((t) => t.id !== id)
      if (activeTab === id) setActiveTab(next[0]?.id ?? null)
      return next
    })
  }

  /* ---------- query ops ---------- */
  const pushHist = (sql: string, ms?: number, n?: number) => {
    setHistory((h) => [{ sql: sql.slice(0, 2000), ms, n, at: new Date().toLocaleTimeString() }, ...h].slice(0, 200))
  }

  const runQuery = useCallback(
    async (id: string) => {
      const t = tabs.find((x) => x.id === id)
      if (!t || t.kind !== 'query' || !session) return
      if (!autocommit) {
        const st = await apiClient.txn(session, 'status')
        if (!st.in_txn) await apiClient.txn(session, 'begin')
      }
      setRunning((r) => ({ ...r, [id]: true }))
      updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: undefined, plan: undefined } : x))
      const j = await apiClient.runQuery(session, t.sql, t.limit || undefined)
      setRunning((r) => ({ ...r, [id]: false }))
      setInTxn(!!(j as { in_txn?: boolean }).in_txn)
      if (j.error && !(j as { results?: unknown }).results) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: j.error, results: null } : x))
        return
      }
      if (j.results) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, results: j.results ?? null, meta: `${j.results!.length} statements · ${(j as { duration_ms?: number }).duration_ms}ms`, error: (j as { error?: string }).error } : x))
        pushHist(t.sql, (j as { duration_ms?: number }).duration_ms, j.results.reduce((a, r) => a + (r.rows?.length ?? 0), 0))
      } else {
        updateTab(id, (x) =>
          x.kind === 'query'
            ? { ...x, results: [j], meta: `${(j.rows ?? []).length} rows · ${j.duration_ms}ms`, error: undefined }
            : x,
        )
        pushHist(t.sql, j.duration_ms, (j.rows ?? []).length)
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tabs, session, autocommit, updateTab],
  )

  const explainQuery = useCallback(
    async (id: string, analyze: boolean) => {
      const t = tabs.find((x) => x.id === id)
      if (!t || t.kind !== 'query' || !session) return
      const j = await apiClient.explain(session, t.sql, analyze)
      if ((j as { error?: string }).error) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: (j as { error: string }).error } : x))
        return
      }
      updateTab(id, (x) => (x.kind === 'query' ? { ...x, plan: explainToText(j), error: undefined } : x))
    },
    [tabs, session, updateTab],
  )

  const rowOp = useCallback(
    async (tabId: string, op: string, values: Record<string, unknown>, where: Record<string, unknown>) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'table' || !session) return
      if (!autocommit) {
        const st = await apiClient.txn(session, 'status')
        if (!st.in_txn) await apiClient.txn(session, 'begin')
      }
      const j = await apiClient.rowOp({ session_id: session, schema: t.schema, table: t.table, op, values, where })
      if (j.error) {
        toast.error(j.error)
        return
      }
      setInTxn(!!j.in_txn)
      loadTablePage(t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
    },
    [tabs, session, autocommit, loadTablePage],
  )

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

  // Verify a stored sid against the live server (a restart invalidates it).
  const verifyStoredSession = useCallback(
    async (sid: string) => {
      try {
        const j = await api<unknown>(q(sid, '/api/schemas?'))
        if (Array.isArray(j)) setConnected(true)
        else setSession('')
      } catch {
        setSession('')
      }
    },
    [setSession],
  )

  // Persist open tabs so a fresh start can reopen the last session. Skipped
  // while empty pre-hydration so boot never wipes the stored session away.
  const hydrated = useRef(false)
  useEffect(() => {
    if (!hydrated.current) {
      if (tabs.length === 0) return
      hydrated.current = true
    }
    try {
      const slim = tabs.map(slimTab).filter((t): t is StoredTab => t !== null)
      localStorage.setItem('open-tabs', JSON.stringify(slim))
      localStorage.setItem('active-tab', activeTab ?? '')
    } catch {
      /* quota/private mode — session restore just won't persist */
    }
  }, [tabs, activeTab])

  // Autologin once on boot from the last successful connection.
  const booted = useRef(false)
  useEffect(() => {
    if (booted.current) return
    booted.current = true
    const auto = readJSON<boolean>('auto-login', true)
    const last = readJSON<ConnFields | null>('last-conn', null)
    if (auto && last && last.host) {
      // Boot-only handoff: stored credentials move into state once here.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setFields(last)
      // Resume (not fresh): the stored sid reuses the same server pool instead
      // of leaking a new one per reload; the backend recreates it if missing.
      void bootConnect(last, false)
      return
    }
    const sid = (() => {
      try {
        return localStorage.getItem('sid')
      } catch {
        return null
      }
    })()
    if (sid) void verifyStoredSession(sid)
    // Boot-only effect by design (guarded by ref, not deps).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Reopen last session's tabs on the first connection of this app load.
  const restored = useRef(false)
  useEffect(() => {
    if (!connected || restored.current) return
    restored.current = true
    const stored = readStoredTabs()
    // One-shot restore; loaders resolve into state asynchronously.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (stored.length) restoreStoredTabs(stored)
    else newQueryTab()
  }, [connected, newQueryTab, restoreStoredTabs])

  return (
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
        onDocs={() => openDocsTab()}
      />
      {connected && (
        <div className="flex items-center gap-2 border-b bg-card px-2.5 py-1.5 text-[12px]">
          <label className="flex cursor-pointer items-center gap-1.5">
            <input type="checkbox" checked={autocommit} onChange={(e) => setAutocommit(e.target.checked)} />
            autocommit
          </label>
          <Button size="sm" variant="ghost" onClick={() => doTxn('begin')}>
            Begin
          </Button>
          <Button size="sm" variant="ghost" onClick={() => doTxn('commit')}>
            Commit
          </Button>
          <Button size="sm" variant="ghost" onClick={() => doTxn('rollback')}>
            Rollback
          </Button>
          <span className="text-muted-foreground">{inTxn ? '● open transaction — Commit or Rollback' : 'no txn'}</span>
        </div>
      )}
      <ResizablePanelGroup direction="horizontal" autoSaveId="pglight-main-layout" className="min-h-0 flex-1">
        <ResizablePanel defaultSize={20} minSize={12} maxSize={32} className="min-h-0">
        <aside className="flex h-full min-h-0 flex-col border-r bg-card">
          <CredentialManager
            fields={fields}
            setFields={setFields}
            saved={saved}
            open={credOpen}
            onOpenChange={setCredOpen}
            onPickSaved={(i) => {
              const c = saved[i]
              if (c) setFields({ host: c.host, port: c.port, user: c.user, password: c.password, dbname: c.dbname, sslmode: c.sslmode })
            }}
            onDeleteSaved={(i) => setSaved((l) => l.filter((_, x) => x !== i))}
            onConnect={() => {
              connect(true).then((ok) => {
                if (ok) setCredOpen(false)
              })
            }}
            onSave={async () => {
              const name = await dialogs.prompt({
                title: 'Save connection',
                defaultValue: `${fields.host}/${fields.dbname}`,
              })
              if (name == null) return
              setSaved((l) => [...l, { name: name.trim() || 'conn', ...fields }])
            }}
            onDisconnect={() => {
              disconnect()
              setCredOpen(true)
            }}
            autoLogin={autoLogin}
            onAutoLogin={setAutoLogin}
            connected={connected}
          />
          <Explorer
            connected={connected}
            databases={databases}
            schemas={schemas}
            currentDb={fields.dbname}
            detail={detail}
            onSwitchDb={switchDb}
            onOpenTable={openTableTab}
            onShowDDL={showDDL}
            onShowView={showView}
            onShowFunc={showFunc}
            onShowSeq={showSeq}
            onShowType={showType}
            onOpenBrowser={openBrowser}
            onOpenErd={openErd}
            onRefresh={loadExplorer}
          />
        </aside>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={55} minSize={30} className="min-h-0">
        <main className="flex h-full min-h-0 min-w-0 flex-col">
          <div className="flex h-10 items-end gap-1 overflow-x-auto border-b bg-card px-2 pt-1.5">
            {tabs.map((t) => (
              <button
                key={t.id}
                onClick={() => setActiveTab(t.id)}
                className={cn(
                  'flex items-center gap-1.5 whitespace-nowrap rounded-t-md border border-b-0 px-2.5 py-1.5 text-[12px]',
                  t.id === activeTab ? 'bg-background font-semibold' : 'bg-muted text-muted-foreground hover:text-foreground',
                )}
              >
                <span title="Close tab" className="flex shrink-0">
                  <X
                    className="h-3 w-3 opacity-60 hover:opacity-100"
                    onClick={(e) => {
                      e.stopPropagation()
                      closeTab(t.id)
                    }}
                  />
                </span>
                {t.title}
              </button>
            ))}
            <button
              onClick={() => newQueryTab()}
              title="New query"
              className="ml-auto flex shrink-0 items-center gap-1 whitespace-nowrap px-2 py-1.5 text-[12px] text-muted-foreground hover:text-foreground"
            >
              <Plus className="h-3.5 w-3.5" /> Query
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-3">
            {!cur && <div className="text-muted-foreground">{connected ? 'Open a table or run a query.' : 'Connect to a database to begin.'}</div>}
            {cur?.kind === 'query' && (
              <QueryConsole
                tab={cur}
                inTxn={inTxn}
                running={!!running[cur.id]}
                onSqlChange={(sql) => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, sql } : x))}
                onRun={() => runQuery(cur.id)}
                onExplain={(a) => explainQuery(cur.id, a)}
                onLimit={(n) => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, limit: n } : x))}
                onSaveSnippet={async () => {
                  const name = await dialogs.prompt({
                    title: 'Save snippet',
                    defaultValue: cur.sql.slice(0, 40),
                  })
                  if (name) setSnippets((s) => [{ name, sql: cur.sql }, ...s])
                }}
                dialogs={dialogs}
              />
            )}
            {cur?.kind === 'table' && (
              <TableWorkspace
                tab={cur}
                inTxn={inTxn}
                onSubtab={(s) => {
                  updateTab(cur.id, (x) => (x.kind === 'table' ? { ...x, subtab: s } : x))
                  if (s !== 'data') loadTableMeta(cur.id, cur.schema, cur.table)
                }}
                onFilterChange={(filter, order) => loadTablePage(cur.id, cur.schema, cur.table, cur.limit, 0, filter, order)}
                onApply={() => loadTablePage(cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)}
                onPage={(d) => loadTablePage(cur.id, cur.schema, cur.table, cur.limit, Math.max(0, cur.offset + d * cur.limit), cur.filter, cur.order)}
                onEditCell={async (col, orig) => {
                  const first = Object.entries(orig).find(([, v]) => v != null)
                  const v = await dialogs.prompt({
                    title: `Edit ${col}`,
                    description: 'Type __NULL__ for NULL',
                    defaultValue: orig[col] == null ? 'NULL' : String(orig[col]),
                  })
                  if (v == null) return
                  const where: Record<string, unknown> = {}
                  if (first) where[first[0]] = first[1]
                  else Object.assign(where, orig)
                  rowOp(cur.id, 'update', { [col]: v }, where)
                }}
                onDeleteRow={async (orig) => {
                  const ok = await dialogs.confirm({
                    title: 'Delete row?',
                    description: 'This cannot be undone.',
                    confirmText: 'Delete',
                    danger: true,
                  })
                  if (ok) rowOp(cur.id, 'delete', {}, orig)
                }}
                onDuplicateRow={(orig) => {
                  const vals: Record<string, unknown> = {}
                  for (const [k, v] of Object.entries(orig)) if (v != null) vals[k] = String(v)
                  rowOp(cur.id, 'insert', vals, {})
                }}
                onInsert={async () => {
                  if (!cur.result) return
                  const vals = await dialogs.form({
                    title: `Insert into ${cur.schema}.${cur.table}`,
                    description: 'Empty = skip · __NULL__ = NULL',
                    fields: cur.result.columns.map((c) => ({ key: c, label: c })),
                    submitText: 'Insert',
                  })
                  if (!vals) return
                  const clean: Record<string, unknown> = {}
                  for (const [k, v] of Object.entries(vals)) if (v !== '') clean[k] = v
                  rowOp(cur.id, 'insert', clean, {})
                }}
                onMaintenance={async (op) => {
                  const ok = await dialogs.confirm({
                    title: `${op.toUpperCase()} ${cur.schema}.${cur.table}?`,
                    confirmText: op.toUpperCase(),
                  })
                  if (!ok) return
                  const j = await apiClient.maintenance(session, cur.schema, cur.table, op)
                  if (j.error) toast.error(j.error)
                  else {
                    toast.success('OK: ' + (j.result || op))
                    loadTableMeta(cur.id, cur.schema, cur.table)
                  }
                }}
                onImport={async (columns, rows) => {
                  const j = await apiClient.importRows({ session_id: session, schema: cur.schema, table: cur.table, columns, rows })
                  if (j.error) toast.error(j.error)
                  else {
                    toast.success(`Imported ${j.rows_affected} rows`)
                    loadTablePage(cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                  }
                }}
                onOpenErd={() => openErd(cur.schema)}
                dialogs={dialogs}
              />
            )}
            {cur?.kind === 'browser' && (
              <BrowserView
                tab={cur}
                onReload={() =>
                  api<Record<string, unknown>[] | { error: string }>(q(session, `${cur.url}?`)).then((j) => {
                    updateTab(cur.id, (x) =>
                      x.kind === 'browser' ? { ...x, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : x,
                    )
                  })
                }
              />
            )}
            {cur?.kind === 'erd' && (
              <ErdView
                tab={cur}
                schemas={schemas.map((s) => s.schema)}
                onSchema={(s) => {
                  updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, schema: s, title: `ERD ${s}`, data: null } : x))
                  api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(session, `/api/erd?schema=${encodeURIComponent(s)}`)).then((j) => {
                    updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
                  })
                }}
                onReload={() =>
                  api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(session, `/api/erd?schema=${encodeURIComponent(cur.schema)}`)).then((j) => {
                    updateTab(cur.id, (x) => (x.kind === 'erd' ? { ...x, data: j } : x))
                  })
                }
                onOpenTable={openTableTab}
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
              onOpenSql={(sql) => newQueryTab(sql)}
              onDeleteSnippet={(i) => setSnippets((s) => s.filter((_, x) => x !== i))}
              dialogs={dialogs}
            />
          </aside>
          </ResizablePanel>
          </>
        )}
      </ResizablePanelGroup>
      <Separator />
      <Card className="rounded-none border-0 border-t px-2.5 py-1 text-[11px] text-muted-foreground">
        {connected ? `connected · ${fields.user}@${fields.host}:${fields.port}/${fields.dbname}` : 'disconnected'} · Ctrl+K search · Ctrl+Enter run
      </Card>
      <SearchPalette key={paletteOpen ? 'open' : 'closed'} open={paletteOpen} onOpenChange={setPaletteOpen} session={session} onOpenTable={openTableTab} onShowFunc={showFunc} />
    </div>
  )
}
