import { useCallback, useRef } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { toast } from 'sonner'
import type { DialogsApi } from '../components/dialogs'
import { api, apiClient, q, type QueryResult } from '../lib/api'
import { shortSid } from '../lib/tabs'
import type { Tab, TableSubtab, TableTabT } from '../types'

export interface TableAlterPayload {
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
}

export interface TableOpsDeps {
  tabs: Tab[]
  setTabs: Dispatch<SetStateAction<Tab[]>>
  setActiveTab: Dispatch<SetStateAction<string | null>>
  updateTab: (id: string, fn: (t: Tab) => Tab) => void
  markTxn: (sid: string, v: boolean) => void
  autocommit: boolean
  dialogs: DialogsApi
  loadExplorer: (sid?: string) => Promise<void>
}

/* Table-tab data plane, bound to each tab's own session. Row edits require
   a primary key (see pkOf); without one the UI refuses instead of guessing. */
export function useTableOps(deps: TableOpsDeps) {
  const { tabs, setTabs, setActiveTab, markTxn, autocommit, dialogs, loadExplorer } = deps
  const pageRequestSeq = useRef<Record<string, number>>({})
  const metaRequestSeq = useRef<Record<string, number>>({})

  const loadTablePage = useCallback(
    async (sid: string, id: string, schema: string, table: string, limit: number, offset: number, filter: string, order: string) => {
      if (!sid) return
      const seq = (pageRequestSeq.current[id] ?? 0) + 1
      pageRequestSeq.current[id] = seq
      let j: QueryResult & { total?: number; in_txn?: boolean; error?: string }
      try {
        j = await api<QueryResult & { total?: number; in_txn?: boolean; error?: string }>(
          q(sid, `/api/table-data?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}&limit=${limit}&offset=${offset}&filter=${encodeURIComponent(filter)}&order=${encodeURIComponent(order)}`),
        )
      } catch (e) {
        if (pageRequestSeq.current[id] !== seq) return
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, error: e instanceof Error ? e.message : String(e) } : t)))
        return
      }
      if (pageRequestSeq.current[id] !== seq) return
      if (j.error) {
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, error: j.error } : t)))
        return
      }
      markTxn(sid, !!j.in_txn)
      setTabs((prev) =>
        prev.map((t) => (t.id === id && t.kind === 'table' ? { ...t, result: j, error: undefined, limit, offset, filter, order } : t)),
      )
    },
    [markTxn, setTabs],
  )

  const loadTableMeta = useCallback(
    async (sid: string, id: string, schema: string, table: string) => {
      if (!sid) return
      const seq = (metaRequestSeq.current[id] ?? 0) + 1
      metaRequestSeq.current[id] = seq
      const [cols, ddl, cons, trg, stats] = await Promise.all([
        api<unknown>(q(sid, `/api/columns?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/ddl?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/constraints?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/triggers?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
        api<unknown>(q(sid, `/api/table-stats?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)).catch(() => null),
      ])
      if (metaRequestSeq.current[id] !== seq) return
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
    [setTabs],
  )

  const openTableTab = useCallback(
    (schema: string, table: string, sidOver?: string) => {
      const sid = sidOver
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
    [setTabs, setActiveTab, loadTablePage, loadTableMeta],
  )

  const rowOp = useCallback(
    async (tabId: string, op: string, values: Record<string, unknown>, where: Record<string, unknown>, single?: boolean) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'table' || !t.sessionId) return
      const sid = t.sessionId
      try {
        if (!autocommit) {
          const st = await apiClient.txn(sid, 'status')
          if (!st.in_txn) await apiClient.txn(sid, 'begin')
        }
        const j = await apiClient.rowOp({ session_id: sid, schema: t.schema, table: t.table, op, values, where, single })
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
    async (tabId: string, p: TableAlterPayload) => {
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
    [tabs, markTxn, loadTablePage, loadTableMeta, loadExplorer, setTabs, setActiveTab],
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

  return { loadTablePage, loadTableMeta, openTableTab, rowOp, alterTable, renameTable }
}

export type UseTableOps = ReturnType<typeof useTableOps>
