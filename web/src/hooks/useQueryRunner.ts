import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { explainToText } from '../components/QueryConsole'
import { api, apiClient, q } from '../lib/api'
import { dropSnapshot, ensureSnapshot } from '../lib/schemaCache'
import { DDL_RE, readJSON } from '../lib/tabs'
import type { HistoryEntry, Snippet, Tab } from '../types'

export interface QueryRunnerDeps {
  tabs: Tab[]
  updateTab: (id: string, fn: (t: Tab) => Tab) => void
  session: string
  autocommit: boolean
  markTxn: (sid: string, v: boolean) => void
}

/* Query-tab execution: run/cancel/explain + history/snippets. Query tabs
   never auto re-run (see useTabs restore); DDL runs invalidate the editor
   snapshot via the shared DDL_RE detector. */
export function useQueryRunner(deps: QueryRunnerDeps) {
  const { tabs, updateTab, session, autocommit, markTxn } = deps
  const [running, setRunning] = useState<Record<string, boolean>>({})
  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [snippets, setSnippets] = useState<Snippet[]>([])
  // SQL actually sent per running tab (may be a selection, LIMIT-wrapped
  // server-side). Used to match the backend in pg_stat_activity on Cancel.
  const runSql = useRef<Record<string, string>>({})

  useEffect(() => {
    if (!readJSON<boolean>('privacy.persistHistory', true)) return
    void Promise.all([apiClient.listHistory(), apiClient.listSnippets()]).then(([h, s]) => {
      setHistory(h.history ?? [])
      setSnippets((s.snippets ?? []).map((x) => ({ name: x.name, sql: x.sql })))
    }).catch(() => undefined)
  }, [])

  const pushHist = (sql: string, ms?: number, n?: number) => {
    const entry: HistoryEntry = { sql: sql.slice(0, 2000), ms, n, at: new Date().toLocaleTimeString() }
    setHistory((h) => [entry, ...h].slice(0, 200))
    if (!readJSON<boolean>('privacy.persistHistory', true)) return
    void apiClient.addHistory(entry.sql, ms, n).catch(() => undefined)
  }

  const runQuery = useCallback(
    async (id: string, sqlOver?: string) => {
      const t = tabs.find((x) => x.id === id)
      if (!t || t.kind !== 'query' || !t.sessionId) return
      const sid = t.sessionId
      const sql = sqlOver ?? t.sql
      runSql.current[id] = sql
      setRunning((r) => ({ ...r, [id]: true }))
      updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: undefined, errLoc: undefined, flashTick: undefined, plan: undefined } : x))
      try {
        if (!autocommit) {
          const st = await apiClient.txn(sid, 'status')
          if (!st.in_txn) await apiClient.txn(sid, 'begin')
        }
        const j = await apiClient.runQuery(sid, sql, t.limit || undefined)
        markTxn(sid, !!j.in_txn)
        const errLoc =
          j.line != null || j.column != null || j.statement_index != null || j.code
            ? { line: j.line, column: j.column, statement_index: j.statement_index, code: j.code }
            : undefined
        if (DDL_RE.test(sql)) {
          dropSnapshot(sid)
          void ensureSnapshot(sid, true)
        }
        if (j.error && !j.results) {
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: j.error, errLoc, flashTick: (x.flashTick ?? 0) + 1, results: null } : x))
          return
        }
        if (j.results) {
          const total = (j as { duration_ms?: number }).duration_ms
          const count = j.statements ?? j.results.length
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, results: j.results ?? null, meta: `${count} statements · ${total ?? 0}ms`, error: j.error, errLoc, flashTick: j.error ? (x.flashTick ?? 0) + 1 : undefined } : x))
        } else {
          updateTab(id, (x) =>
            x.kind === 'query'
              ? { ...x, results: [j], meta: `${(j.rows ?? []).length} rows · ${j.duration_ms}ms`, error: undefined, errLoc: undefined, flashTick: undefined }
              : x,
          )
          pushHist(sql, j.duration_ms, (j.rows ?? []).length)
        }
      } catch (e) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: e instanceof Error ? e.message : String(e), errLoc: undefined, flashTick: undefined, results: null } : x))
      } finally {
        setRunning((r) => ({ ...r, [id]: false }))
        delete runSql.current[id]
      }
    },
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
        const err = apiClient.explainError(j)
        if (err) {
          updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: err, errLoc: undefined, flashTick: undefined } : x))
          return
        }
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, plan: explainToText(j), error: undefined, errLoc: undefined, flashTick: undefined } : x))
      } catch (e) {
        updateTab(id, (x) => (x.kind === 'query' ? { ...x, error: e instanceof Error ? e.message : String(e), errLoc: undefined, flashTick: undefined } : x))
      }
    },
    [tabs, updateTab],
  )

  return { running, history, setHistory, snippets, setSnippets, runQuery, cancelQuery, explainQuery }
}

export type UseQueryRunner = ReturnType<typeof useQueryRunner>
