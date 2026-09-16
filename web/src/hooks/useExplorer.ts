import { useCallback, useEffect, useState } from 'react'
import type { Dispatch, RefObject, SetStateAction } from 'react'
import { toast } from 'sonner'
import type { ConnFields } from '../components/ConnectionBar'
import type { DialogsApi } from '../components/dialogs'
import { api, apiClient, q } from '../lib/api'
import { ensureSnapshot } from '../lib/schemaCache'
import { download, resultToCSV, resultToInserts } from '../lib/format'
import { DEFAULT_SQL, qi } from '../lib/tabs'
import type { DbInfo, SchemaGroup, SessionInfo } from '../types'

export interface ExplorerDeps {
  session: string
  active: SessionInfo | null
  activeId: string
  activeIdRef: RefObject<string>
  password: string
  dialogs: DialogsApi
  setInTxn: (v: boolean) => void
  openTableTab: (schema: string, table: string, sid?: string) => void
  newQueryTab: (sql?: string, sid?: string) => void
  bootConnect: (f: ConnFields, fresh: boolean, dbnameOver?: string, sidOver?: string, activate?: boolean, quiet?: boolean) => Promise<string>
  findSession: (host: string, port: string | number, user: string, dbname: string, sslmode: string) => SessionInfo | undefined
  deadIds: Record<string, boolean>
  reconnectOne: (sid: string, opts?: { silent?: boolean }) => Promise<string>
  setActiveId: Dispatch<SetStateAction<string>>
}

/* Explorer tree + context-menu actions. The tree follows the active session;
   loadExplorer warms the editor snapshot while fetching. */
export function useExplorer(deps: ExplorerDeps) {
  const { session, active, activeId, activeIdRef, password, dialogs, setInTxn, openTableTab, newQueryTab } = deps
  const [databases, setDatabases] = useState<DbInfo[]>([])
  const [schemas, setSchemas] = useState<SchemaGroup[]>([])

  const loadExplorer = useCallback(async (sidOver?: string) => {
    const sid = sidOver ?? activeId
    if (!sid) {
      setSchemas([])
      setDatabases([])
      return
    }
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
    // Disconnect landed mid-flight: drop the stale payload instead of
    // repainting a dead session's tree (only F5 fixed it before).
    if (activeIdRef.current !== sid) return
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
  }, [activeId, activeIdRef])

  useEffect(() => {
    if (!activeId) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- clearing the tree on logout is external-session sync, not derived render state
      setSchemas([])
      setDatabases([])
      return
    }
    // External-system sync: reload server state on session change.
    loadExplorer(activeId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

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

  const switchDb = useCallback(
    async (name: string) => {
      if (!active) return
      if (name === active.dbname) return
      // Same target already pooled? Just switch — no new session.
      const hit = deps.findSession(active.host, active.port, active.user, name, active.sslmode)
      if (hit) {
        if (deps.deadIds[hit.id]) {
          const nid = await deps.reconnectOne(hit.id)
          if (nid) deps.setActiveId(nid)
        } else deps.setActiveId(hit.id)
        return
      }
      // No confirm: clicking a database switches immediately in a new
      // session; the current session stays open, tabs keep theirs.
      let pw = password
      if (!pw) {
        const v = await dialogs.prompt({ title: `Password for ${active.user}@${active.host}`, description: `Opening ${name} as ${active.user}` })
        if (v == null) return
        pw = v
      }
      void deps.bootConnect({ host: active.host, port: active.port, user: active.user, password: pw, dbname: active.dbname, sslmode: active.sslmode }, true, name)
    },
    [deps, password, dialogs, active],
  )

  /* ---------- explorer context menu (Explorer stays presentational) ---------- */
  const newScopedQuery = useCallback(
    (scope: { db?: string; schema?: string; table?: string }) => {
      if (!needSession('query')) return
      if (scope.table && scope.schema) {
        newQueryTab(`SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM ${qi(scope.schema)}.${qi(scope.table)} LIMIT 100;`, activeId)
        return
      }
      if (scope.schema) {
        const first = schemas.find((x) => x.schema === scope.schema)?.tables[0]?.name
        if (first) {
          newQueryTab(`SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM ${qi(scope.schema)}.${qi(first)} LIMIT 100;`, activeId)
        } else {
          newQueryTab(
            `SET search_path TO ${qi(scope.schema)}, public;\nSELECT * FROM information_schema.tables WHERE table_schema = '${scope.schema.replace(/'/g, "''")}' LIMIT 50;`,
            activeId,
          )
        }
        return
      }
      if (scope.db) newQueryTab(`-- DB: ${scope.db}\n${DEFAULT_SQL}`, activeId)
    },
    [needSession, newQueryTab, schemas, activeId],
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
      loadExplorer(activeId)
    },
    [session, activeId, needSession, dialogs, active, loadExplorer, setInTxn],
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
      loadExplorer(activeId)
      openTableTab(schema, name, activeId)
    },
    [session, activeId, needSession, dialogs, loadExplorer, openTableTab, setInTxn],
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

  return {
    databases, setDatabases, schemas, setSchemas,
    loadExplorer, switchDb, newScopedQuery, createSchema, createTable,
    exportTable, copyName,
  }
}

export type UseExplorer = ReturnType<typeof useExplorer>
