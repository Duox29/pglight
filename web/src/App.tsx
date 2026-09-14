import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Loader2, Play, Plus, RotateCcw, X } from 'lucide-react'
import { Toaster, toast } from 'sonner'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { Card } from './components/ui/card'
import { Switch } from './components/ui/switch'
import { Tip, TooltipProvider } from './components/ui/tooltip'
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
import { api, apiClient, q, type QueryResult } from './lib/api'
import { dropSnapshot, ensureSnapshot } from './lib/schemaCache'
import { forgetSessionConn, readSessionConns, rememberSessionConn } from './lib/reconnect'
import { onUnauthorized, useLocalStorage } from './lib/storage'
import type {
  DbInfo,
  HistoryEntry,
  SavedConnection,
  SchemaGroup,
  SessionInfo,
  SideView,
  Snippet,
  StoredTab,
  Tab,
  TableSubtab,
  TableTabT,
} from './types'
import { cn } from './lib/utils'
import { download, resultToCSV, resultToInserts, resultToJSON } from './lib/format'

const DEFAULT_SQL = 'SELECT * FROM information_schema.tables LIMIT 20;'

const qi = (s: string) => `"${s.replace(/"/g, '""')}"`

/** Primary-key columns from /api/columns metadata, or [] when the table has no safe row identity. */
function pkOf(t: TableTabT): string[] {
  const cols = t.cols ?? []
  const pks = cols.filter((c) => String(c['pk'] ?? '').toLowerCase() === 't' || c['pk'] === true).map((c) => String(c['name'] ?? ''))
  return pks.filter(Boolean)
}

// Mirrors the backend complete-cache DDL detector (ddlRe): after a
// schema-changing run the editor snapshot is dropped and refetched once.
const DDL_RE = /\b(CREATE|ALTER|DROP|TRUNCATE|COMMENT)\b/i

const BROWSER_DEFS: Record<string, { title: string; url: string; cols: string[] }> = {
  extensions: { title: 'Extensions', url: '/api/extensions', cols: ['name', 'default_version', 'installed_version', 'comment'] },
  roles: { title: 'Roles', url: '/api/roles', cols: ['name', 'superuser', 'login', 'createdb', 'member_of'] },
}

function slimTab(t: Tab): StoredTab | null {
  const sessionId = (t as { sessionId?: string }).sessionId
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
      return { id: t.id, kind: t.kind, title: t.title, sessionId, sql: t.sql, limit: t.limit, snapshot }
    }
    case 'table':
      return { id: t.id, kind: t.kind, title: t.title, sessionId, schema: t.schema, table: t.table, subtab: t.subtab, limit: t.limit, offset: t.offset, filter: t.filter, order: t.order }
    case 'browser':
      return { id: t.id, kind: t.kind, title: t.title, sessionId, key: t.id.split('__')[0].replace(/^b_/, '') }
    case 'erd':
      return { id: t.id, kind: t.kind, title: t.title, sessionId, schema: t.schema }
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
  // Multi-session: N live backend pools. Tabs bind to one session id each;
  // the explorer + txn bar follow the active session. No passwords persist.
  const [sessions, setSessions] = useLocalStorage<SessionInfo[]>('sessions', [])
  const [activeId, setActiveId] = useLocalStorage<string>('active-sid', '')
  const active = sessions.find((s) => s.id === activeId) ?? null
  const session = active?.id ?? ''
  const connected = !!active
  const [inTxnMap, setInTxnMap] = useState<Record<string, boolean>>({})
  const inTxn = active ? !!inTxnMap[active.id] : false
  const markTxn = useCallback((sid: string, v: boolean) => {
    setInTxnMap((m) => (m[sid] === v ? m : { ...m, [sid]: v }))
  }, [])
  const setInTxn = useCallback(
    (v: boolean) => {
      if (activeId) markTxn(activeId, v)
    },
    [activeId, markTxn],
  )
  const [autocommit, setAutocommit] = useLocalStorage<boolean>('autocommit', true)
  const [autoLogin, setAutoLogin] = useLocalStorage<boolean>('auto-login', true)
  const [databases, setDatabases] = useState<DbInfo[]>([])
  const [schemas, setSchemas] = useState<SchemaGroup[]>([])
  const [tabs, setTabs] = useState<Tab[]>([])
  const [activeTab, setActiveTab] = useState<string | null>(null)
  const [running, setRunning] = useState<Record<string, boolean>>({})
  const [history, setHistory] = useLocalStorage<HistoryEntry[]>('hist', [])
  const [snippets, setSnippets] = useLocalStorage<Snippet[]>('snippets', [])
  // SQL actually sent per running tab (may be a selection, LIMIT-wrapped
  // server-side). Used to match the backend in pg_stat_activity on Cancel.
  const runSql = useRef<Record<string, string>>({})
  // Monotonic token: stale tab loads (browser/ERD/restore) check it before
  // writing into tab state so a late response cannot overwrite newer tabs.
  const loadSeq = useRef(0)
  const [sideOpen, setSideOpen] = useState(false)
  const [sideView, setSideView] = useState<SideView>('history')
  const [credOpen, setCredOpen] = useLocalStorage<boolean>('conn-panel-open', true)
  const [dlg, setDlg] = useState<PendingDialog | null>(null)
  const dialogs = useMemo(() => createDialogs(setDlg), [])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const tabSeq = useRef(0)
  // Dead-session tracking: backend pools die on server restart while tabs
  // persist. deadIds is state (badges); deadRef mirrors it for stable
  // callbacks; retriedRef bounds auto-reconnect to one attempt per session.
  const [deadIds, setDeadIds] = useState<Record<string, boolean>>({})
  const [bootDone, setBootDone] = useState(false)
  const deadRef = useRef<Record<string, boolean>>({})
  const retriedRef = useRef<Record<string, boolean>>({})
  const remapRef = useRef<Map<string, string>>(new Map())
  const sessionsRef = useRef(sessions)
  useEffect(() => {
    sessionsRef.current = sessions
  })
  const reconnectOneRef = useRef<(sid: string, opts?: { silent?: boolean }) => Promise<string>>(() => Promise.resolve(''))
  const reconnectAllRef = useRef<() => Promise<void>>(() => Promise.resolve())

  const clearDead = useCallback((sid: string) => {
    if (!sid || !deadRef.current[sid]) return
    delete deadRef.current[sid]
    setDeadIds((prev) => {
      if (!prev[sid]) return prev
      const n = { ...prev }
      delete n[sid]
      return n
    })
  }, [])

  const markDead = useCallback((sid: string, opts?: { silent?: boolean }) => {
    if (!sid || deadRef.current[sid]) return
    const known = sessionsRef.current.some((s) => s.id === sid) || !!readSessionConns()[sid]
    if (!known) return
    deadRef.current[sid] = true
    setDeadIds((prev) => (prev[sid] ? prev : { ...prev, [sid]: true }))
    if (!opts?.silent) {
      toast.error('Session lost (server restart?) — reconnect from Connections', {
        action: { label: 'Reconnect', onClick: () => void reconnectOneRef.current(sid) },
      })
    }
    if (readJSON<boolean>('auto-login', true) && !retriedRef.current[sid]) {
      retriedRef.current[sid] = true
      void reconnectOneRef.current(sid, { silent: true })
    }
  }, [])

  const cur = tabs.find((t) => t.id === activeTab) ?? null
  const updateTab = useCallback((id: string, fn: (t: Tab) => Tab) => {
    setTabs((prev) => prev.map((t) => (t.id === id ? fn(t) : t)))
  }, [])

  /* ---------- connection (multi-session) ---------- */
  const bootConnect = useCallback(
    async (f: ConnFields, fresh: boolean, dbnameOver?: string, sidOver?: string, activate = true, quiet = false): Promise<string> => {
      const dbname = dbnameOver || f.dbname
      const j = await apiClient.connect({
        host: f.host,
        port: Number(f.port) || 5432,
        user: f.user,
        password: f.password,
        dbname,
        sslmode: f.sslmode,
        session_id: fresh ? '' : sidOver || session || undefined,
      })
      if (j.error || !j.session_id) {
        if (!quiet) toast.error(j.error ?? 'Connect failed')
        return ''
      }
      const sid = j.session_id
      const info: SessionInfo = j.info
        ? { id: sid, host: j.info.host, port: String(j.info.port ?? f.port), user: j.info.user || f.user, dbname: j.info.dbname || dbname, sslmode: j.info.sslmode || f.sslmode }
        : { id: sid, host: f.host, port: f.port, user: f.user, dbname, sslmode: f.sslmode }
      setSessions((prev) => (prev.some((s) => s.id === sid) ? prev.map((s) => (s.id === sid ? info : s)) : [...prev, info]))
      if (activate) setActiveId(sid)
      markTxn(sid, !!j.info?.in_txn)
      rememberSessionConn(sid, { ...f, dbname })
      clearDead(sid)
      try {
        localStorage.setItem('last-conn', JSON.stringify({ ...f, dbname }))
      } catch {
        /* private mode etc. — autologin just won't persist */
      }
      return sid
    },
    [session, setSessions, setActiveId, markTxn, clearDead],
  )

  // Same connection target already has a pool? Reuse it — never spawn a
  // duplicate pool (each pool holds up to 8 backends; duplicates pile up
  // in pg_stat_activity fast).
  const findSession = useCallback(
    (host: string, port: string | number, user: string, dbname: string, sslmode: string) =>
      sessions.find(
        (s) =>
          s.host === host &&
          String(s.port) === String(port) &&
          s.user === user &&
          s.dbname === dbname &&
          (s.sslmode || '') === (sslmode || ''),
      ),
    [sessions],
  )

  const dropSession = useCallback(
    (sid: string) => {
      forgetSessionConn(sid)
      clearDead(sid)
      const next = sessions.filter((s) => s.id !== sid)
      setSessions(next)
      if (sid === activeId) setActiveId(next[0]?.id ?? '')
      setInTxnMap((m) => {
        if (!(sid in m)) return m
        const n = { ...m }
        delete n[sid]
        return n
      })
    },
    [activeId, sessions, setSessions, setActiveId, clearDead],
  )

  const disconnect = useCallback(
    (sid?: string) => {
      const target = sid ?? activeId
      if (target) apiClient.disconnect(target).catch(() => undefined)
      if (target) dropSnapshot(target)
      if (target) dropSession(target)
      if (sessions.length <= 1) setCredOpen(true)
    },
    [activeId, dropSession, sessions.length, setCredOpen],
  )

  const refreshTxn = useCallback(async () => {
    if (!session) return
    try {
      const j = await apiClient.txn(session, 'status')
      markTxn(session, !!j.in_txn)
    } catch {
      /* heartbeat stays stale; next run surfaces it */
    }
  }, [session, markTxn])

  /* ---------- reconnect (dead session → fresh pool, tabs follow) ---------- */
  // Table/browser/erd tab ids embed the session's short id; both move so
  // openTableTab dedupe keeps working after a remap.
  const remapTabsSession = useCallback(
    (oldSid: string, newSid: string) => {
      const short = (sid: string) => (sid.length > 6 ? sid.slice(-6) : sid)
      const newId = (t: Tab): string => {
        switch (t.kind) {
          case 'table':
            return `t_${short(newSid)}_${t.schema}_${t.table}`
          case 'browser':
            return `b_${t.id.split('__')[0].replace(/^b_/, '')}__${short(newSid)}`
          case 'erd':
            return `e_${t.schema}__${short(newSid)}`
          default:
            return t.id
        }
      }
      const pairs = tabs
        .filter((t) => (t as { sessionId?: string }).sessionId === oldSid && newId(t) !== t.id)
        .map((t) => [t.id, newId(t)] as const)
      setTabs((prev) =>
        prev.map((t) => {
          if ((t as { sessionId?: string }).sessionId !== oldSid || !('sessionId' in t)) return t
          const hit = pairs.find(([o]) => o === t.id)
          return { ...t, id: hit ? hit[1] : t.id, sessionId: newSid }
        }),
      )
      if (pairs.length) setActiveTab((a) => pairs.find(([o]) => o === a)?.[1] ?? a)
    },
    [tabs],
  )

  const reconnectOne = useCallback(
    async (oldSid: string, opts?: { silent?: boolean }): Promise<string> => {
      const f = readSessionConns()[oldSid]
      if (!f || !f.host) {
        if (!opts?.silent) toast.error('No saved credentials for this session — connect manually')
        return ''
      }
      const nid = await bootConnect({ ...f }, true, undefined, undefined, false, !!opts?.silent)
      if (!nid) return ''
      forgetSessionConn(oldSid)
      dropSnapshot(oldSid)
      remapTabsSession(oldSid, nid)
      setSessions((prev) => prev.filter((s) => s.id !== oldSid))
      setActiveId((a) => (a === oldSid ? nid : a))
      clearDead(oldSid)
      if (!opts?.silent) toast.success(`Reconnected ${f.user}@${f.host}/${f.dbname}`)
      return nid
    },
    [bootConnect, remapTabsSession, clearDead, setSessions, setActiveId],
  )

  const reconnectAll = useCallback(async () => {
    const ids = Object.keys(deadRef.current)
    let ok = 0
    for (const id of ids) {
      try {
        const nid = await reconnectOne(id, { silent: true })
        if (nid) ok++
      } catch {
        /* keep going — remaining sessions still get their attempt */
      }
    }
    if (ok) toast.success(`Reconnected ${ok} session${ok > 1 ? 's' : ''}`)
    else toast.error('Reconnect failed — check credentials')
  }, [reconnectOne])

  const connect = useCallback(
    (fresh: boolean, dbnameOver?: string) => {
      if (fresh) {
        const dbname = dbnameOver || fields.dbname
        const hit = findSession(fields.host, fields.port, fields.user, dbname, fields.sslmode)
        if (hit) {
          if (deadIds[hit.id]) {
            return reconnectOne(hit.id).then((nid) => {
              if (nid) setActiveId(nid)
              return nid
            })
          }
          setActiveId(hit.id)
          return Promise.resolve(hit.id)
        }
      }
      return bootConnect(fields, fresh, dbnameOver)
    },
    [bootConnect, fields, findSession, deadIds, reconnectOne, setActiveId],
  )

  const doTxn = useCallback(
    async (action: string) => {
      if (!session) return
      try {
        const j = await apiClient.txn(session, action)
        if (j.error) toast.error(j.error)
        markTxn(session, !!j.in_txn)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [session, markTxn],
  )

  /* ---------- explorer (follows the active session) ---------- */
  const loadExplorer = useCallback(async (sidOver?: string) => {
    const sid = sidOver ?? activeId
    if (!sid) return
    let sch: unknown
    let tbl: unknown
    let dbs: unknown
    try {
      ;[sch, tbl, dbs] = await Promise.all([
        api<unknown>(q(sid, '/api/schemas?')),
        api<unknown>(q(sid, '/api/tables?')),
        api<unknown>(q(sid, '/api/databases?')),
      ])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      return
    }
    const [views, matviews, foreign, functions, sequences, types] = await Promise.all(
      (['views', 'matviews', 'foreign', 'functions', 'sequences', 'types'] as const).map((k) =>
        api<unknown>(q(sid, `/api/objects?kind=${k}`)).catch(() => []),
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
    // Warm the editor snapshot while we're here: one fetch per session per
    // minute, shared by every keystroke of every query tab on it.
    void ensureSnapshot(sid)
  }, [activeId])

  useEffect(() => {
    if (!activeId) return
    // External-system sync: reload server state on session change. Setters run
    // in async continuations after fetch, not during render.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadExplorer(activeId)
    refreshTxn()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  const switchDb = useCallback(
    async (name: string) => {
      if (!active) return
      if (name === active.dbname) return
      // Same target already pooled? Just switch — no new session.
      const hit = findSession(active.host, active.port, active.user, name, active.sslmode)
      if (hit) {
        if (deadIds[hit.id]) {
          const nid = await reconnectOne(hit.id)
          if (nid) setActiveId(nid)
        } else setActiveId(hit.id)
        return
      }
      // No confirm: clicking a database switches immediately in a new
      // session; the current session stays open, tabs keep theirs.
      let password = fields.password
      if (!password) {
        const v = await dialogs.prompt({ title: `Password for ${active.user}@${active.host}`, description: `Opening ${name} as ${active.user}` })
        if (v == null) return
        password = v
      }
      void bootConnect({ host: active.host, port: active.port, user: active.user, password, dbname: active.dbname, sslmode: active.sslmode }, true, name)
    },
    [bootConnect, dialogs, active, fields.password, findSession, deadIds, reconnectOne, setActiveId],
  )

  /* ---------- tabs (each bound to the session active at creation) ---------- */
  const shortSid = (sid: string) => (sid.length > 6 ? sid.slice(-6) : sid)
  const newQueryTab = useCallback(
    (sql?: string, sidOver?: string) => {
      const sid = sidOver ?? activeId
      if (!sid) {
        toast.error('Connect to a database first — cannot open query')
        return
      }
      tabSeq.current += 1
      const id = `q${tabSeq.current}`
      setTabs((prev) => [...prev, { id, kind: 'query', title: `Query ${tabSeq.current}`, sessionId: sid, sql: sql ?? DEFAULT_SQL, limit: 200, results: null }])
      setActiveTab(id)
    },
    [activeId],
  )

  /* ---------- table ops (per-tab session) ---------- */
  const loadTablePage = useCallback(
    async (sid: string, id: string, schema: string, table: string, limit: number, offset: number, filter: string, order: string) => {
      if (!sid) return
      let j: QueryResult & { total?: number; in_txn?: boolean; error?: string }
      try {
        j = await api<QueryResult & { total?: number; in_txn?: boolean; error?: string }>(
          q(sid, `/api/table-data?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}&limit=${limit}&offset=${offset}&filter=${encodeURIComponent(filter)}&order=${encodeURIComponent(order)}`),
        )
      } catch (e) {
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, error: e instanceof Error ? e.message : String(e) } : t)))
        return
      }
      if (j.error) {
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, error: j.error } : t)))
        return
      }
      markTxn(sid, !!j.in_txn)
      setTabs((prev) =>
        prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, result: j, error: undefined, limit, offset, filter, order } : t)),
      )
    },
    [markTxn],
  )

  const loadTableMeta = useCallback(
    async (sid: string, id: string, schema: string, table: string) => {
      if (!sid) return
      const [cols, ddl, cons, trg, stats] = await Promise.all([
        api<unknown>(q(sid, `/api/columns?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/ddl?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/constraints?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/triggers?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/table-stats?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
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
    [],
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
    (schema: string, table: string, sidOver?: string) => {
      const sid = sidOver ?? activeId
      if (!sid) {
        toast.error(`Connect to a database first — cannot open ${schema}.${table}`)
        return
      }
      const id = `t_${shortSid(sid)}_${schema}_${table}`
      setTabs((prev) =>
        prev.find((t) => t.id === id)
          ? prev
          : [...prev, { id, kind: 'table', title: table, sessionId: sid, schema, table, subtab: 'data' as TableSubtab, limit: 100, offset: 0, filter: '', order: '', result: null, cols: null, ddl: null, constraints: null, triggers: null, stats: null }],
      )
      setActiveTab(id)
      loadTablePage(sid, id, schema, table, 100, 0, '', '')
      loadTableMeta(sid, id, schema, table)
    },
    [activeId, loadTablePage, loadTableMeta],
  )
  /* ---------- explorer context menu (Explorer stays presentational) ---------- */
  const newScopedQuery = useCallback(
    (scope: { db?: string; schema?: string; table?: string }) => {
      if (!needSession('query')) return
      if (scope.table && scope.schema) {
        newQueryTab(`SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM ${qi(scope.schema)}.${qi(scope.table)} LIMIT 100;`)
        return
      }
      if (scope.schema) {
        const first = schemas.find((x) => x.schema === scope.schema)?.tables[0]?.name
        if (first) {
          newQueryTab(`SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM ${qi(scope.schema)}.${qi(first)} LIMIT 100;`)
        } else {
          newQueryTab(
            `SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM information_schema.tables WHERE table_schema = '${scope.schema.replace(/'/g, "''")}' LIMIT 50;`,
          )
        }
        return
      }
      if (scope.db) newQueryTab(`-- DB: ${scope.db}\n${DEFAULT_SQL}`)
    },
    [needSession, newQueryTab, schemas],
  )

  const createSchema = useCallback(
    async (db?: string) => {
      if (!needSession('schema')) return
      if (db && active && db !== active.dbname) {
        toast.error(`Session "${active.dbname}" is active — switch sessions to create a schema in "${db}"`)
        return
      }
      const name = await dialogs.prompt({ title: 'New schema', placeholder: 'my_schema' })
      if (name == null) return
      const trimmed = name.trim()
      if (!trimmed) return
      const j = await apiClient.runQuery(session, `CREATE SCHEMA ${qi(trimmed)}`)
      if (j.error) {
        toast.error(j.error)
        return
      }
      setInTxn(!!j.in_txn)
      toast.success(`Created schema ${trimmed}`)
      loadExplorer()
    },
    [session, needSession, dialogs, active, loadExplorer, setInTxn],
  )

  const createTable = useCallback(
    async (schema: string) => {
      if (!needSession(schema)) return
      const v = await dialogs.form({
        title: `New table in ${schema}`,
        fields: [
          { key: 'name', label: 'Table name', placeholder: 'my_table' },
          { key: 'columns', label: 'Columns DDL', placeholder: 'id SERIAL PRIMARY KEY, created_at TIMESTAMPTZ DEFAULT now()' },
        ],
      })
      if (v == null) return
      const name = (v.name ?? '').trim()
      if (!name) return
      const cols = (v.columns ?? '').trim() || 'id SERIAL PRIMARY KEY, created_at TIMESTAMPTZ DEFAULT now()'
      const j = await apiClient.runQuery(session, `CREATE TABLE ${qi(schema)}.${qi(name)} (${cols})`)
      if (j.error) {
        toast.error(j.error)
        return
      }
      setInTxn(!!j.in_txn)
      toast.success(`Created table ${schema}.${name}`)
      loadExplorer()
      openTableTab(schema, name)
    },
    [session, needSession, dialogs, loadExplorer, openTableTab, setInTxn],
  )

  const exportTable = useCallback(
    async (schema: string, table: string, fmt: 'csv' | 'sql') => {
      if (!needSession(`${schema}.${table}`)) return
      const j = await api<{ columns?: string[]; types?: string[]; rows?: unknown[][]; error?: string }>(
        q(session, `/api/table-data?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}&limit=1000&offset=0&filter=&order=`),
      )
      if (j.error) {
        toast.error(j.error)
        return
      }
      const columns = j.columns ?? []
      const rows = (j.rows ?? []) as unknown[][]
      if (fmt === 'csv') download(resultToCSV(columns, rows), `${schema}.${table}.csv`, 'text/csv')
      else download(resultToInserts(columns, rows, `${qi(schema)}.${qi(table)}`, j.types), `${schema}.${table}.sql`, 'text/sql')
    },
    [session, needSession],
  )

  const copyName = useCallback(async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success('Copied')
    } catch {
      toast.error('Copy failed')
    }
  }, [])

  const openBrowser = useCallback(
    (key: 'extensions' | 'roles', title: string) => {
      if (!activeId) {
        toast.error(`Connect to a database first — cannot open ${title}`)
        return
      }
      const sid = activeId
      const id = `b_${key}__${shortSid(sid)}`
      const url = key === 'extensions' ? '/api/extensions' : '/api/roles'
      const cols = key === 'extensions' ? ['name', 'default_version', 'installed_version', 'comment'] : ['name', 'superuser', 'login', 'createdb', 'member_of']
      const token = ++loadSeq.current
      setTabs((prev) => (prev.find((t) => t.id === id) ? prev : [...prev, { id, kind: 'browser', title, sessionId: sid, url, cols, rows: null }]))
      setActiveTab(id)
      api<Record<string, unknown>[] | { error: string }>(q(sid, `${url}?`))
        .then((j) => {
          if (loadSeq.current !== token) return
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'browser' ? { ...t, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : t)))
        })
        .catch((e: unknown) => {
          if (loadSeq.current !== token) return
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'browser' ? { ...t, rows: null, error: e instanceof Error ? e.message : String(e) } : t)))
        })
    },
    [activeId],
  )

  const openErd = useCallback(
    (schema: string, sidOver?: string) => {
      const sid = sidOver ?? activeId
      if (!sid) {
        toast.error(`Connect to a database first — cannot open ERD ${schema}`)
        return
      }
      const id = `e_${schema}__${shortSid(sid)}`
      const token = ++loadSeq.current
      setTabs((prev) => (prev.find((t) => t.id === id) ? prev : [...prev, { id, kind: 'erd', title: `ERD ${schema}`, sessionId: sid, schema, data: null }]))
      setActiveTab(id)
      api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(sid, `/api/erd?schema=${encodeURIComponent(schema)}`))
        .then((j) => {
          if (loadSeq.current !== token) return
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'erd' ? { ...t, data: j } : t)))
        })
        .catch((e: unknown) => {
          if (loadSeq.current !== token) return
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'erd' ? { ...t, data: null } : t)))
          toast.error(e instanceof Error ? e.message : String(e))
        })
    },
    [activeId],
  )

  /* ---------- last-session restore (tabs + autologin) ---------- */
  const restoreStoredTabs = useCallback(
    (stored: StoredTab[], aliveIds: Set<string>, fallbackSid: string) => {
      const rebuilt: Tab[] = []
      const seenIds = new Set<string>()
      let maxQ = 0
      for (const s of stored) {
        // Tabs follow their own session 1:1 (remapped after reconnect).
        // Dead sessions are KEPT (badged, reconnectable) — only tabs whose
        // session is unknown entirely fall back, never silently to another DB.
        const remapped = s.sessionId ? remapRef.current.get(s.sessionId) : undefined
        const sid =
          remapped ??
          (s.sessionId && (aliveIds.has(s.sessionId) || deadRef.current[s.sessionId]) ? s.sessionId : fallbackSid)
        if (!sid) continue
        if (s.kind === 'query') {
          const m = /^q(\d+)$/.exec(s.id)
          if (m) maxQ = Math.max(maxQ, Number(m[1]))
          if (seenIds.has(s.id)) continue
          seenIds.add(s.id)
          rebuilt.push({
            id: s.id, kind: 'query', title: s.title || 'Query', sessionId: sid, sql: s.sql ?? DEFAULT_SQL, limit: s.limit ?? 200,
            results: s.snapshot ? [{ columns: s.snapshot.columns, rows: s.snapshot.rows, rows_affected: s.snapshot.rows.length, stale: true }] : null,
          })
        } else if (s.kind === 'table' && s.schema && s.table) {
          const id = `t_${shortSid(sid)}_${s.schema}_${s.table}`
          if (seenIds.has(id)) continue
          seenIds.add(id)
          rebuilt.push({
            id, kind: 'table', title: s.table, sessionId: sid, schema: s.schema, table: s.table,
            subtab: s.subtab ?? 'data', limit: s.limit ?? 100, offset: s.offset ?? 0, filter: s.filter ?? '', order: s.order ?? '',
            result: null, cols: null, ddl: null, constraints: null, triggers: null, stats: null,
          })
        } else if (s.kind === 'browser' && s.key && BROWSER_DEFS[s.key]) {
          const d = BROWSER_DEFS[s.key]
          const id = `b_${s.key}__${shortSid(sid)}`
          if (seenIds.has(id)) continue
          seenIds.add(id)
          rebuilt.push({ id, kind: 'browser', title: d.title, sessionId: sid, url: d.url, cols: d.cols, rows: null })
        } else if (s.kind === 'erd' && s.schema) {
          const id = `e_${s.schema}__${shortSid(sid)}`
          if (seenIds.has(id)) continue
          seenIds.add(id)
          rebuilt.push({ id, kind: 'erd', title: `ERD ${s.schema}`, sessionId: sid, schema: s.schema, data: null })
        } else if (s.kind === 'docs') {
          if (seenIds.has('docs')) continue
          seenIds.add('docs')
          rebuilt.push({ id: 'docs', kind: 'docs', title: 'Docs' })
        }
      }
      if (!rebuilt.length) {
        newQueryTab(undefined, fallbackSid)
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
      const token = ++loadSeq.current
      for (const t of rebuilt) {
        if (t.kind === 'docs' || t.kind === 'query') continue
        const sid = t.sessionId
        if (t.kind === 'table') {
          void loadTablePage(sid, t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
          void loadTableMeta(sid, t.id, t.schema, t.table)
        } else if (t.kind === 'browser') {
          api<Record<string, unknown>[] | { error: string }>(q(sid, `${t.url}?`))
            .then((j) => {
              if (loadSeq.current !== token) return
              setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'browser' ? { ...x, rows: Array.isArray(j) ? j : null, error: (j as { error?: string }).error } : x)))
            })
            .catch((e: unknown) => {
              if (loadSeq.current !== token) return
              setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'browser' ? { ...x, rows: null, error: e instanceof Error ? e.message : String(e) } : x)))
            })
        } else if (t.kind === 'erd') {
          api<NonNullable<Extract<Tab, { kind: 'erd' }>['data']>>(q(sid, `/api/erd?schema=${encodeURIComponent(t.schema)}`))
            .then((j) => {
              if (loadSeq.current !== token) return
              setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'erd' ? { ...x, data: j } : x)))
            })
            .catch((e: unknown) => {
              if (loadSeq.current !== token) return
              setTabs((prev) => prev.map((x) => (x.id === t.id && x.kind === 'erd' ? { ...x, data: null } : x)))
              toast.error(e instanceof Error ? e.message : String(e))
            })
        }
      }
    },
    [loadTablePage, loadTableMeta, newQueryTab],
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
    async (id: string, sqlOver?: string) => {
      const t = tabs.find((x) => x.id === id)
      if (!t || t.kind !== 'query' || !t.sessionId) return
      const sid = t.sessionId
      const sql = sqlOver ?? t.sql
      runSql.current[id] = sql
      setRunning((r) => ({ ...r, [id]: true }))
      updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: undefined, plan: undefined } : x))
      try {
        if (!autocommit) {
          const st = await apiClient.txn(sid, 'status')
          if (!st.in_txn) await apiClient.txn(sid, 'begin')
        }
        const j = await apiClient.runQuery(sid, sql, t.limit || undefined)
        markTxn(sid, !!(j as { in_txn?: boolean }).in_txn)
        if (DDL_RE.test(sql)) {
          dropSnapshot(sid)
          void ensureSnapshot(sid, true)
        }
        if (j.error && !(j as { results?: unknown }).results) {
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: j.error, results: null } : x))
          return
        }
        if (j.results) {
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, results: j.results ?? null, meta: `${j.results!.length} statements · ${(j as { duration_ms?: number }).duration_ms}ms`, error: (j as { error?: string }).error } : x))
          pushHist(sql, (j as { duration_ms?: number }).duration_ms, j.results.reduce((a, r) => a + (r.rows?.length ?? 0), 0))
        } else {
          updateTab(id, (x) =>
            x.kind === 'query'
              ? { ...x, results: [j], meta: `${(j.rows ?? []).length} rows · ${j.duration_ms}ms`, error: undefined }
              : x,
          )
          pushHist(sql, j.duration_ms, (j.rows ?? []).length)
        }
      } catch (e) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: e instanceof Error ? e.message : String(e), results: null } : x))
      } finally {
        setRunning((r) => ({ ...r, [id]: false }))
        delete runSql.current[id]
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tabs, autocommit, updateTab, markTxn],
  )

  // Cancel the in-flight query of a tab. Backends are pool-scoped by
  // application_name (pglight:<session>), so the tab's own pool is matched
  // first; the sent SQL only disambiguates concurrent runs in one session.
  const cancelQuery = useCallback(
    async (id: string) => {
      const t = tabs.find((x) => x.id === id)
      const sid = t && t.kind === 'query' ? t.sessionId : session
      const sent = runSql.current[id]
      if (!sent || !sid) {
        toast.error('No running query to cancel')
        return
      }
      try {
        const norm = (s: string) => s.replace(/\s+/g, ' ').trim().replace(/;+\s*$/, '').slice(0, 200)
        const needle = norm(sent)
        const sqlHit = (query: unknown) => {
          const h = norm(String(query ?? ''))
          if (!h) return false
          return h.includes(needle) || (needle.includes(h) && h.length > 10)
        }
        const rows = await api<Record<string, unknown>[]>(q(sid, '/api/activity?'))
        // NOTE: the poll-exclusion below must test the full query text — the
        // activity statement mentions pg_stat_activity only after column 80.
        const live = (Array.isArray(rows) ? rows : []).filter(
          (r) => r.state === 'active' && !String(r.query ?? '').includes('pg_stat_activity'),
        )
        // Own pool first (exact when a single backend runs there).
        const own = live.filter((r) => String(r.app ?? '') === `pglight:${sid}`)
        const pick = (pool: Record<string, unknown>[]): Record<string, unknown> | null => {
          if (!pool.length) return null
          if (pool.length === 1) return pool[0]
          const matched = pool.filter((r) => sqlHit(r.query))
          return matched.length === 1 ? matched[0] : null
        }
        let hit = pick(own)
        let ambiguous = !!own.length && !hit
        if (!hit && !ambiguous) {
          // Untagged pools (connected before the backend upgrade): fall back
          // to matching the sent SQL across all backends.
          const matched = live.filter((r) => sqlHit(r.query))
          if (matched.length === 1) hit = matched[0]
          else ambiguous = !!matched.length
        }
        if (!hit) {
          toast.error(ambiguous ? 'Multiple running queries — cancel from Dashboard' : 'No running backend found')
          return
        }
        const j = await apiClient.cancel(sid, Number(hit.pid))
        if (j.error) toast.error(j.error)
        else toast.success(`Cancelled backend ${hit.pid}`)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [session, tabs],
  )

  const explainQuery = useCallback(
    async (id: string, analyze: boolean) => {
      const t = tabs.find((x) => x.id === id)
      if (!t || t.kind !== 'query' || !t.sessionId) return
      try {
        const j = await apiClient.explain(t.sessionId, t.sql, analyze)
        if ((j as { error?: string }).error) {
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: (j as { error: string }).error } : x))
          return
        }
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, plan: explainToText(j), error: undefined } : x))
      } catch (e) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: e instanceof Error ? e.message : String(e) } : x))
      }
    },
    [tabs, updateTab],
  )

  const rowOp = useCallback(
    async (tabId: string, op: string, values: Record<string, unknown>, where: Record<string, unknown>) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'table' || !t.sessionId) return
      const sid = t.sessionId
      try {
        if (!autocommit) {
          const st = await apiClient.txn(sid, 'status')
          if (!st.in_txn) await apiClient.txn(sid, 'begin')
        }
        const j = await apiClient.rowOp({ session_id: sid, schema: t.schema, table: t.table, op, values, where })
        if (j.error) {
          toast.error(j.error)
          return
        }
        markTxn(sid, !!j.in_txn)
        loadTablePage(sid, t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [tabs, markTxn, autocommit, loadTablePage],
  )

  const alterTable = useCallback(
    async (
      tabId: string,
      p: {
        op: string
        column?: string
        new_name?: string
        type?: string
        nullable?: boolean
        default?: string
        drop_default?: boolean
        constraint?: string
        def?: string
        cascade?: boolean
        index?: string
        unique?: boolean
        method?: string
        columns?: string[]
        include?: string[]
        where?: string
        trigger?: string
        timing?: string
        events?: string[]
        for_each?: string
        function?: string
        when?: string
        update_of?: string[]
      },
    ) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'table' || !t.sessionId) return
      const sid = t.sessionId
      try {
        const j = await apiClient.alterTable({ session_id: sid, schema: t.schema, table: t.table, ...p })
        if (j.error) {
          toast.error(j.error)
          return
        }
        markTxn(sid, !!j.in_txn)
        if (p.op === 'rename_table' && p.new_name) {
          const nn = p.new_name
          const nid = `t_${shortSid(sid)}_${t.schema}_${nn}`
          setTabs((prev) => prev.map((x) => (x.id === tabId && x.kind === 'table' ? { ...x, id: nid, table: nn, title: nn } : x)))
          setActiveTab(nid)
          loadTablePage(sid, nid, t.schema, nn, t.limit, 0, t.filter, t.order)
          loadTableMeta(sid, nid, t.schema, nn)
        } else {
          loadTablePage(sid, t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
          loadTableMeta(sid, t.id, t.schema, t.table)
        }
        toast.success('Table altered')
        loadExplorer(sid)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [tabs, markTxn, loadTablePage, loadTableMeta, loadExplorer],
  )

  const renameTable = useCallback(
    async (tabId: string) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'table') return
      const v = await dialogs.prompt({ title: `Rename ${t.schema}.${t.table}`, defaultValue: t.table })
      if (v == null) return
      const nn = v.trim()
      if (!nn || nn === t.table) return
      alterTable(tabId, { op: 'rename_table', new_name: nn })
    },
    [tabs, dialogs, alterTable],
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

  // Reconcile stored sessions against the live server (a restart invalidates
  // all pools). Adopts the server list when non-empty; stored sessions
  // missing from the server are KEPT (badged dead, reconnectable) — never
  // silently dropped. Returns the live sessions for the boot remap.
  const reconcileSessions = useCallback(async (): Promise<SessionInfo[]> => {
    try {
      const j = await apiClient.listSessions()
      const alive = Array.isArray(j.sessions) ? j.sessions : []
      if (alive.length) {
        const mapped: SessionInfo[] = alive.map((s) => ({
          id: s.id,
          host: s.host || '',
          port: String(s.port ?? ''),
          user: s.user || '',
          dbname: s.dbname || 'postgres',
          sslmode: s.sslmode || '',
        }))
        setSessions((prev) => {
          const ids = new Set(mapped.map((s) => s.id))
          return [...mapped, ...prev.filter((s) => !ids.has(s.id))]
        })
        for (const s of alive) if (s.in_txn) markTxn(s.id, true)
        setActiveId((prev) => (mapped.some((s) => s.id === prev) ? prev : (mapped[0]?.id ?? '')))
        return mapped
      }
    } catch {
      /* server down — boot falls through to per-session reconnect */
    }
    return []
  }, [setSessions, setActiveId, markTxn])

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

  // Backend 401s (dead session) feed markDead: silent auto-retry once,
  // toast with Reconnect action otherwise. Heartbeat catches server
  // restarts while the page stays open (no failed query needed).
  useEffect(() => {
    reconnectOneRef.current = reconnectOne
    reconnectAllRef.current = reconnectAll
  })
  useEffect(() => onUnauthorized((sid) => markDead(sid)), [markDead])
  useEffect(() => {
    const check = async () => {
      try {
        const j = await apiClient.listSessions()
        const alive = new Set((Array.isArray(j.sessions) ? j.sessions : []).map((s) => s.id))
        for (const s of sessionsRef.current) {
          if (!alive.has(s.id)) markDead(s.id)
        }
      } catch {
        /* server down — retry on next tick, no toast spam */
      }
    }
    const timer = setInterval(check, 30000)
    const onVis = () => {
      if (document.visibilityState === 'visible') void check()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [markDead])

  // Boot: reconcile, then reconnect each dead stored session 1:1 (parallel
  // credentials from session-conns) so tabs keep their own DB. Tab restore
  // runs in the effect below once bootDone flips.
  const booted = useRef(false)
  useEffect(() => {
    if (booted.current) return
    booted.current = true
    void (async () => {
      const alive = await reconcileSessions()
      const aliveIds = new Set(alive.map((s) => s.id))
      const stored = sessionsRef.current
      const auto = readJSON<boolean>('auto-login', true)
      if (auto) {
        for (const s of stored) {
          if (aliveIds.has(s.id) || !readSessionConns()[s.id]) continue
          const nid = await reconnectOneRef.current(s.id, { silent: true })
          if (nid) {
            remapRef.current.set(s.id, nid)
            aliveIds.add(nid)
          }
        }
        // First-ever run (nothing stored): legacy single last-conn.
        if (!aliveIds.size && !stored.length) {
          const last = readJSON<ConnFields | null>('last-conn', null)
          if (last && last.host) {
            setFields(last)
            // Fresh pool: the previous server run (if any) is gone.
            await bootConnect(last, true)
          }
        }
        // Still-unmapped stored sessions are dead — badge, don't drop.
        for (const s of stored) {
          if (!aliveIds.has(s.id) && !remapRef.current.has(s.id)) markDead(s.id, { silent: true })
        }
        const lost = stored.filter((s) => !aliveIds.has(s.id))
        if (lost.length && aliveIds.size) {
          toast.error(`${lost.length} session${lost.length > 1 ? 's' : ''} lost (server restart?) — tabs kept`, {
            action: { label: 'Reconnect all', onClick: () => void reconnectAllRef.current() },
          })
        }
      }
      try {
        localStorage.removeItem('sid')
      } catch {
        /* legacy key — ignore */
      }
      setBootDone(true)
    })()
    // Boot-only effect by design (guarded by ref, not deps).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Reopen last session's tabs on the first connection of this app load.
  const restored = useRef(false)
  useEffect(() => {
    if (!connected || restored.current || !activeId || !bootDone) return
    restored.current = true
    const stored = readStoredTabs()
    const aliveIds = new Set(sessions.map((s) => s.id))
    // One-shot restore; loaders resolve into state asynchronously.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (stored.length) restoreStoredTabs(stored, aliveIds, activeId)
    else newQueryTab(undefined, activeId)
  }, [connected, activeId, sessions, bootDone, newQueryTab, restoreStoredTabs])

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
        onDocs={() => openDocsTab()}
      />
      {connected && active && (
        <div className="flex items-center gap-2 border-b bg-card px-2.5 py-1.5 text-[12px]">
          <Tip content={`${active.user}@${active.host}:${active.port}/${active.dbname}`}>
            <span className="truncate font-semibold">
              {active.user}@{active.host}/{active.dbname}
            </span>
          </Tip>
          <Separator orientation="vertical" className="h-4" />
          <label className="flex cursor-pointer items-center gap-1.5 text-muted-foreground">
            <Switch checked={autocommit} onCheckedChange={setAutocommit} aria-label="Autocommit" />
            autocommit
          </label>
          <Separator orientation="vertical" className="h-4" />
          <Tip content="Begin transaction">
            <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Begin transaction" onClick={() => doTxn('begin')} disabled={inTxn}>
              <Play />
            </Button>
          </Tip>
          <Tip content="Commit transaction">
            <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Commit transaction" onClick={() => doTxn('commit')} disabled={!inTxn}>
              <Check />
            </Button>
          </Tip>
          <Tip content="Rollback transaction">
            <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Rollback transaction" onClick={() => doTxn('rollback')} disabled={!inTxn}>
              <RotateCcw />
            </Button>
          </Tip>
          <Badge variant="outline" className="gap-1.5 font-normal">
            <span className={cn('h-2 w-2 rounded-full', inTxn ? 'bg-amber-500' : 'bg-emerald-500')} />
            {inTxn ? 'open transaction — Commit or Rollback' : 'no transaction'}
          </Badge>
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
            }}
            autoLogin={autoLogin}
            onAutoLogin={setAutoLogin}
            connected={connected}
            sessions={sessions}
            activeId={activeId}
            onSwitch={(id) => setActiveId(id)}
            onDisconnectOne={(id) => disconnect(id)}
            deadIds={deadIds}
            onReconnectOne={(id) => void reconnectOne(id)}
            onReconnectAll={() => void reconnectAll()}
          />
          <Explorer
            connected={connected}
            databases={databases}
            schemas={schemas}
            currentDb={active?.dbname ?? fields.dbname}
            onSwitchDb={switchDb}
            onOpenTable={openTableTab}
            onOpenBrowser={openBrowser}
            onOpenErd={openErd}
            onRefresh={() => loadExplorer()}
            onNewQuery={newScopedQuery}
            onNewSchema={createSchema}
            onNewTable={createTable}
            onExport={exportTable}
            onCopy={copyName}
          />
        </aside>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={55} minSize={30} className="min-h-0">
        <main className="flex h-full min-h-0 min-w-0 flex-col">
          <div className="flex h-10 items-end gap-1 overflow-x-auto border-b bg-card px-2 pt-1.5" role="tablist" aria-label="Workspace tabs">
            {tabs.map((t) => {
              const sid = (t as { sessionId?: string }).sessionId
              const db = sessions.find((s) => s.id === sid)?.dbname ?? ''
              const dirty = t.kind === 'query' && !t.results && !t.error && t.sql !== DEFAULT_SQL
              return (
                <button
                  key={t.id}
                  role="tab"
                  aria-selected={t.id === activeTab}
                  onClick={() => setActiveTab(t.id)}
                  onMouseDown={(e) => {
                    if (e.button === 1) {
                      e.preventDefault()
                      closeTab(t.id)
                    }
                  }}
                  title={t.kind === 'query' ? `${t.title} · ${db || 'no session'}` : `${t.title}`}
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
                          closeTab(t.id)
                        }}
                      />
                    </span>
                  </Tip>
                  <span className="max-w-[160px] truncate">{t.title}</span>
                  {t.kind !== 'docs' && db && (
                    <Tip content={`Session database: ${db}`}>
                      <span className="max-w-[80px] truncate rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">
                        {db}
                      </span>
                    </Tip>
                  )}
                </button>
              )
            })}
            <Tip content="New query (Ctrl+K then Enter)">
              <button
                onClick={() => newQueryTab(undefined)}
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
                tab={cur}
                inTxn={!!inTxnMap[cur.sessionId]}
                running={!!running[cur.id]}
                onSqlChange={(sql) => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, sql } : x))}
                onRun={(sql) => runQuery(cur.id, sql)}
                onCancel={() => cancelQuery(cur.id)}
                onClearResults={() => updateTab(cur.id, (x) => (x.kind === 'query' ? { ...x, results: null, error: undefined, plan: undefined, meta: undefined } : x))}
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
                inTxn={!!inTxnMap[cur.sessionId]}
                onSubtab={(s) => {
                  updateTab(cur.id, (x) => (x.kind === 'table' ? { ...x, subtab: s } : x))
                  if (s !== 'data') loadTableMeta(cur.sessionId, cur.id, cur.schema, cur.table)
                }}
                onFilterChange={(filter, order) => loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, 0, filter, order)}
                onApply={() => loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)}
                onPage={(d) => loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, Math.max(0, cur.offset + d * cur.limit), cur.filter, cur.order)}
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
                  const v = await dialogs.prompt({
                    title: `Edit ${col}`,
                    description: 'Type __NULL__ for NULL',
                    defaultValue: orig[col] == null ? '__NULL__' : String(orig[col]),
                  })
                  if (v == null) return
                  const where: Record<string, unknown> = {}
                  for (const k of keys) where[k] = orig[k]
                  rowOp(cur.id, 'update', { [col]: v }, where)
                }}
                onDeleteRow={async (orig) => {
                  const ok = await dialogs.confirm({
                    title: 'Delete row?',
                    description: `This will delete this row from ${cur.schema}.${cur.table}.`,
                    confirmText: 'Delete',
                    danger: true,
                  })
                  if (ok) rowOp(cur.id, 'delete', {}, orig)
                }}
                onCopyInsert={(orig) => {
                  copyName(resultToInserts(Object.keys(orig), [Object.values(orig)], `${qi(cur.schema)}.${qi(cur.table)}`))
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
                  copyName(resultToCSV(cols, rows.map((r) => cols.map((c) => r[c]))))
                }}
                onDeleteRows={async (rows) => {
                  if (!rows.length) return
                  if (!cur.sessionId) {
                    toast.error('Session closed — reconnect to delete rows')
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
                    if (!autocommit) {
                      const st = await apiClient.txn(cur.sessionId, 'status')
                      if (!st.in_txn) await apiClient.txn(cur.sessionId, 'begin')
                    }
                    let failed = 0
                    let curTxn = false
                    for (const w of rows) {
                      const j = await apiClient.rowOp({ session_id: cur.sessionId, schema: cur.schema, table: cur.table, op: 'delete', values: {}, where: w })
                      if (j.error) failed++
                      curTxn = !!j.in_txn
                    }
                    markTxn(cur.sessionId, curTxn)
                    if (failed) toast.error(`Failed to delete ${failed} row${failed === 1 ? '' : 's'}`)
                    else toast.success(`Deleted ${rows.length} row${rows.length === 1 ? '' : 's'}`)
                    loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                  } catch (e) {
                    toast.error(e instanceof Error ? e.message : String(e))
                    loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                  }
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
                  try {
                    const j = await apiClient.maintenance(cur.sessionId, cur.schema, cur.table, op)
                    if (j.error) toast.error(j.error)
                    else {
                      toast.success('OK: ' + (j.result || op))
                      loadTableMeta(cur.sessionId, cur.id, cur.schema, cur.table)
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
                      loadTablePage(cur.sessionId, cur.id, cur.schema, cur.table, cur.limit, cur.offset, cur.filter, cur.order)
                    }
                  } catch (e) {
                    toast.error(e instanceof Error ? e.message : String(e))
                  }
                }}
                onOpenErd={() => openErd(cur.schema, cur.sessionId)}
                onAlter={(pl) => alterTable(cur.id, pl)}
                onRenameTable={() => renameTable(cur.id)}
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
                schemas={schemas.map((s) => s.schema)}
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
                onOpenTable={(schema, table) => openTableTab(schema, table, cur.sessionId)}
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
        {connected && active
          ? `${sessions.length} session${sessions.length === 1 ? '' : 's'} · ${active.user}@${active.host}:${active.port}/${active.dbname} · Ctrl+K search · Ctrl+Enter run`
          : 'disconnected · Ctrl+K search · Ctrl+Enter run'}
      </Card>
      <SearchPalette key={paletteOpen ? 'open' : 'closed'} open={paletteOpen} onOpenChange={setPaletteOpen} session={session} onOpenTable={openTableTab} />
    </div>
    </TooltipProvider>
  )
}
