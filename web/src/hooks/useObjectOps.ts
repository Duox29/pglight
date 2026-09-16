import { useCallback } from 'react'
import type { Dispatch, SetStateAction } from 'react'
import { toast } from 'sonner'
import type { DialogsApi } from '../components/dialogs'
import { type SeqOpts } from '../components/ObjectView'
import { api, apiClient, q } from '../lib/api'
import { qi, shortSid } from '../lib/tabs'
import type { ObjectKind, Tab } from '../types'

export interface ObjectOpsDeps {
  tabs: Tab[]
  setTabs: Dispatch<SetStateAction<Tab[]>>
  setActiveTab: Dispatch<SetStateAction<string | null>>
  markTxn: (sid: string, v: boolean) => void
  loadExplorer: (sid?: string) => Promise<void>
  closeTab: (id: string) => void
  dialogs: DialogsApi
}

/* Object tabs (functions / sequences / types): definition loading plus the
   DDL writers. Drops confirm per-overload for functions. */
export function useObjectOps(deps: ObjectOpsDeps) {
  const { tabs, setTabs, setActiveTab, markTxn, loadExplorer, closeTab, dialogs } = deps

  const loadObjectDef = useCallback(
    async (sid: string, id: string, kind: ObjectKind, schema: string, name: string) => {
      if (!sid) return
      try {
        if (kind === 'function') {
          const rows = await api<({ schema?: string; name?: string; def?: string; lang?: string; kind?: string } & Record<string, unknown>)[] | { error: string }>(
            q(sid, `/api/func-def?schema=${encodeURIComponent(schema)}&name=${encodeURIComponent(name)}`),
          )
          if (!Array.isArray(rows)) throw new Error(rows.error || 'Failed to load function definition')
          const row = rows[0] as (Record<string, unknown> & { def?: unknown }) | undefined
          if (!row) throw new Error(`Function ${schema}.${name} not found`)
          const def = typeof row.def === 'string' ? row.def : null
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'object' ? { ...t, def, details: { ...row }, error: undefined } : t)))
        } else {
          const url = kind === 'sequence' ? '/api/seq-def' : '/api/type-def'
          const j = await api<Record<string, unknown> & { definition?: unknown; error?: string }>(
            q(sid, `${url}?schema=${encodeURIComponent(schema)}&name=${encodeURIComponent(name)}`),
          )
          if (typeof j.error === 'string' && j.error) throw new Error(j.error)
          const def = typeof j.definition === 'string' ? j.definition : null
          setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'object' ? { ...t, def, details: { ...j }, error: undefined } : t)))
        }
      } catch (e) {
        setTabs((prev) => prev.map((t) => (t.id === id && t.kind === 'object' ? { ...t, def: null, error: e instanceof Error ? e.message : String(e) } : t)))
      }
    },
    [setTabs],
  )

  const openObjectTab = useCallback(
    (kind: ObjectKind, schema: string, name: string, sidOver?: string) => {
      const sid = sidOver
      if (!sid) {
        toast.error(`Connect to a database first — cannot open ${schema}.${name}`)
        return
      }
      const shortName = name.split('(')[0]
      const id = `o_${kind}_${schema}_${shortName}__${shortSid(sid)}`
      setTabs((prev) =>
        prev.find((t) => t.id === id)
          ? prev
          : [...prev, { id, kind: 'object', title: name, sessionId: sid, objectKind: kind, schema, name, def: null, details: null }],
      )
      setActiveTab(id)
      void loadObjectDef(sid, id, kind, schema, name)
    },
    [setTabs, setActiveTab, loadObjectDef],
  )

  const runObjectDDL = useCallback(
    async (tabId: string, sql: string, success: string) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'object' || !t.sessionId) return
      const sid = t.sessionId
      try {
        const j = await apiClient.runQuery(sid, sql)
        if (j.error && !j.results) {
          toast.error(j.error)
          return
        }
        markTxn(sid, !!j.in_txn)
        toast.success(success)
        loadObjectDef(sid, t.id, t.objectKind, t.schema, t.name)
        loadExplorer(sid)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [tabs, markTxn, loadObjectDef, loadExplorer],
  )

  const saveSequence = useCallback(
    (tabId: string, o: SeqOpts) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'object' || t.objectKind !== 'sequence') return
      const num = (v: string, label: string): number | null | undefined => {
        if (!v.trim()) return null
        const n = Number(v)
        if (!Number.isInteger(n)) {
          toast.error(`${label} must be an integer`)
          return undefined
        }
        return n
      }
      const incr = num(o.increment, 'Increment')
      const min = num(o.minValue, 'Min value')
      const max = num(o.maxValue, 'Max value')
      const cache = num(o.cache, 'Cache')
      const restart = num(o.restart, 'Restart')
      if (incr === undefined || min === undefined || max === undefined || cache === undefined || restart === undefined) return
      const parts: string[] = []
      if (incr != null) parts.push(`INCREMENT BY ${incr}`)
      if (min != null) parts.push(`MINVALUE ${min}`)
      if (max != null) parts.push(`MAXVALUE ${max}`)
      if (cache != null) parts.push(`CACHE ${cache}`)
      if (restart != null) parts.push(`RESTART WITH ${restart}`)
      parts.push(o.cycle ? 'CYCLE' : 'NO CYCLE')
      if (!parts.length) return
      void runObjectDDL(tabId, `ALTER SEQUENCE ${qi(t.schema)}.${qi(t.name)} ${parts.join(' ')}`, 'Sequence altered')
    },
    [tabs, runObjectDDL],
  )

  const saveFunction = useCallback(
    (tabId: string, sql: string) => {
      if (!sql.trim()) {
        toast.error('Definition is empty')
        return
      }
      void runObjectDDL(tabId, sql, 'Function replaced')
    },
    [runObjectDDL],
  )

  const enumAdd = useCallback(
    (tabId: string, value: string) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'object' || t.objectKind !== 'type') return
      const v = value.trim()
      if (!v) return
      void runObjectDDL(tabId, `ALTER TYPE ${qi(t.schema)}.${qi(t.name)} ADD VALUE '${v.replace(/'/g, "''")}'`, `Added value ${v}`)
    },
    [tabs, runObjectDDL],
  )

  const renameObject = useCallback(
    async (tabId: string) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'object' || t.objectKind === 'function') return
      const v = await dialogs.prompt({ title: `Rename ${t.schema}.${t.name}`, defaultValue: t.name })
      if (v == null) return
      const nn = v.trim()
      if (!nn || nn === t.name) return
      const keyword = t.objectKind === 'sequence' ? 'SEQUENCE' : 'TYPE'
      const sid = t.sessionId
      try {
        const j = await apiClient.runQuery(sid, `ALTER ${keyword} ${qi(t.schema)}.${qi(t.name)} RENAME TO ${qi(nn)}`)
        if (j.error && !j.results) {
          toast.error(j.error)
          return
        }
        markTxn(sid, !!j.in_txn)
        const shortName = nn.split('(')[0]
        const nid = `o_${t.objectKind}_${t.schema}_${shortName}__${shortSid(sid)}`
        setTabs((prev) => prev.map((x) => (x.id === tabId && x.kind === 'object' ? { ...x, id: nid, name: nn, title: nn, def: null, details: null } : x)))
        setActiveTab(nid)
        loadObjectDef(sid, nid, t.objectKind, t.schema, nn)
        loadExplorer(sid)
        toast.success(`Renamed to ${nn}`)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [tabs, dialogs, markTxn, loadObjectDef, loadExplorer, setTabs, setActiveTab],
  )

  const dropObject = useCallback(
    async (tabId: string) => {
      const t = tabs.find((x) => x.id === tabId)
      if (!t || t.kind !== 'object' || !t.sessionId) return
      const sid = t.sessionId
      const qlit = (s: string) => `'${s.replace(/'/g, "''")}'`
      try {
        if (t.objectKind === 'function') {
          const base = t.name.split('(')[0]
          const found = await apiClient.runQuery(sid, `SELECT p.oid::regprocedure::text AS fn FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = ${qlit(t.schema)} AND p.proname = ${qlit(base)}`)
          if (found.error && !found.results) {
            toast.error(found.error)
            return
          }
          const rows = found.rows ?? found.results?.[0]?.rows ?? []
          const fns = rows.map((r) => String(r[0]))
          if (!fns.length) {
            toast.error(`Function ${t.schema}.${t.name} not found`)
            return
          }
          const ok = await dialogs.confirm({
            title: `Drop function ${base}?`,
            description: `This will drop ${fns.length} overload${fns.length === 1 ? '' : 's'} in ${t.schema}.`,
            confirmText: 'Drop',
            danger: true,
          })
          if (!ok) return
          for (const fn of fns) {
            const j = await apiClient.runQuery(sid, `DROP FUNCTION ${fn}`)
            if (j.error && !j.results) {
              toast.error(j.error)
              return
            }
            markTxn(sid, !!j.in_txn)
          }
        } else {
          const keyword = t.objectKind === 'sequence' ? 'SEQUENCE' : 'TYPE'
          const ok = await dialogs.confirm({
            title: `Drop ${keyword.toLowerCase()} ${t.schema}.${t.name}?`,
            confirmText: 'Drop',
            danger: true,
          })
          if (!ok) return
          const j = await apiClient.runQuery(sid, `DROP ${keyword} ${qi(t.schema)}.${qi(t.name)}`)
          if (j.error && !j.results) {
            toast.error(j.error)
            return
          }
          markTxn(sid, !!j.in_txn)
        }
        toast.success('Dropped')
        closeTab(tabId)
        loadExplorer(sid)
      } catch (e) {
        toast.error(e instanceof Error ? e.message : String(e))
      }
    },
    [tabs, dialogs, markTxn, loadExplorer, closeTab],
  )

  return { loadObjectDef, openObjectTab, saveSequence, saveFunction, enumAdd, renameObject, dropObject }
}

export type UseObjectOps = ReturnType<typeof useObjectOps>
