import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { api, q } from '../lib/api'
import { BROWSER_DEFS, DEFAULT_SQL, isWorkspaceView, readStoredTabs, shortSid, slimTab } from '../lib/tabs'
import type { ObjectKind, SideView, StoredTab, Tab } from '../types'

export interface TabLoaders {
  loadTablePage: (sid: string, id: string, schema: string, table: string, limit: number, offset: number, filter: string, order: string) => Promise<void>
  loadTableMeta: (sid: string, id: string, schema: string, table: string) => Promise<void>
  loadObjectDef: (sid: string, id: string, kind: ObjectKind, schema: string, name: string) => Promise<void>
}

export interface RestoreSessionLookup {
  remap: Map<string, string>
  isDead: (sid: string) => boolean
}

/* Tabs bound to the session active at creation. All single-tab writes go
   through updateTab(id, fn) with t.kind narrowing; restore is the only bulk
   setTabs. Tab ids embed the session short id so remap keeps dedupe working. */
export function useTabs() {
  const [tabs, setTabs] = useState<Tab[]>([])
  const [activeTab, setActiveTab] = useState<string | null>(null)
  // Monotonic token: stale tab loads (browser/ERD/restore) check it before
  // writing into tab state so a late response cannot overwrite newer tabs.
  const loadSeq = useRef(0)
  const tabSeq = useRef(0)

  const cur = tabs.find((t) => t.id === activeTab) ?? null
  const updateTab = useCallback((id: string, fn: (t: Tab) => Tab) => {
    setTabs((prev) => prev.map((t) => (t.id === id ? fn(t) : t)))
  }, [])

  const newQueryTab = useCallback((sql?: string, sid?: string) => {
    if (!sid) {
      toast.error('Connect to a database first — cannot open query')
      return
    }
    tabSeq.current += 1
    const id = `q${tabSeq.current}`
    setTabs((prev) => [...prev, { id, kind: 'query', title: `Query ${tabSeq.current}`, sessionId: sid, sql: sql ?? DEFAULT_SQL, limit: 200, results: null }])
    setActiveTab(id)
  }, [])

  const openDocsTab = useCallback(() => {
    setTabs((prev) => (prev.find((t) => t.id === 'docs') ? prev : [...prev, { id: 'docs', kind: 'docs', title: 'Docs' }]))
    setActiveTab('docs')
  }, [])

  const openWorkspace = useCallback((view: SideView = 'history') => {
    setTabs((prev) => {
      if (prev.some((t) => t.kind === 'workspace')) return prev.map((t) => (t.kind === 'workspace' ? { ...t, view } : t))
      return [...prev, { id: 'workspace', kind: 'workspace', title: 'Workspace', view }]
    })
    setActiveTab('workspace')
  }, [])

  const openBrowser = useCallback((key: 'extensions' | 'roles', title: string, sid?: string) => {
    if (!sid) {
      toast.error(`Connect to a database first — cannot open ${title}`)
      return
    }
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
  }, [])

  const openErd = useCallback((schema: string, sid?: string) => {
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
  }, [])

  // Table/browser/erd tab ids embed the session's short id; both move so
  // openTableTab dedupe keeps working after a remap.
  const remapTabsSession = useCallback(
    (oldSid: string, newSid: string) => {
      const newId = (t: Tab): string => {
        switch (t.kind) {
          case 'table':
            return `t_${shortSid(newSid)}_${t.schema}_${t.table}`
          case 'browser':
            return `b_${t.id.split('__')[0].replace(/^b_/, '')}__${shortSid(newSid)}`
          case 'erd':
            return `e_${t.schema}__${shortSid(newSid)}`
          case 'object': {
            const shortName = t.name.split('(')[0]
            return `o_${t.objectKind}_${t.schema}_${shortName}__${shortSid(newSid)}`
          }
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

  /* Last-session restore (tabs + autologin). Query tabs get their capped
     snapshot (never auto re-run); all other kinds reload via loaders under
     one shared loadSeq token. */
  const restoreStoredTabs = useCallback(
    (stored: StoredTab[], aliveIds: Set<string>, fallbackSid: string, loaders: TabLoaders, lookup: RestoreSessionLookup) => {
      const rebuilt: Tab[] = []
      const seenIds = new Set<string>()
      let maxQ = 0
      for (const s of stored) {
        if (s.kind === 'workspace') {
          if (seenIds.has('workspace')) continue
          seenIds.add('workspace')
          rebuilt.push({ id: 'workspace', kind: 'workspace', title: 'Workspace', view: isWorkspaceView(s.view) ? s.view : 'history' })
          continue
        }
        if (s.kind === 'docs') {
          if (seenIds.has('docs')) continue
          seenIds.add('docs')
          rebuilt.push({ id: 'docs', kind: 'docs', title: 'Docs' })
          continue
        }
        // Tabs follow their own session 1:1 (remapped after reconnect).
        // Dead sessions are KEPT (badged, reconnectable) — only tabs whose
        // session is unknown entirely fall back, never silently to another DB.
        const remapped = s.sessionId ? lookup.remap.get(s.sessionId) : undefined
        const sid = remapped ?? (s.sessionId && (aliveIds.has(s.sessionId) || lookup.isDead(s.sessionId)) ? s.sessionId : fallbackSid)
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
        } else if (s.kind === 'object' && s.schema && s.name && s.objectKind) {
          const shortName = s.name.split('(')[0]
          const id = `o_${s.objectKind}_${s.schema}_${shortName}__${shortSid(sid)}`
          if (seenIds.has(id)) continue
          seenIds.add(id)
          rebuilt.push({ id, kind: 'object', title: s.name, sessionId: sid, objectKind: s.objectKind, schema: s.schema, name: s.name, def: null, details: null })
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
        if (t.kind === 'docs' || t.kind === 'workspace' || t.kind === 'query') continue
        const sid = t.sessionId
        if (t.kind === 'table') {
          void loaders.loadTablePage(sid, t.id, t.schema, t.table, t.limit, t.offset, t.filter, t.order)
          void loaders.loadTableMeta(sid, t.id, t.schema, t.table)
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
        } else if (t.kind === 'object') {
          void loaders.loadObjectDef(sid, t.id, t.objectKind, t.schema, t.name)
        }
      }
    },
    [newQueryTab],
  )

  const closeTab = useCallback(
    (id: string) => {
      setTabs((prev) => {
        const next = prev.filter((t) => t.id !== id)
        if (activeTab === id) setActiveTab(next[0]?.id ?? null)
        return next
      })
    },
    [activeTab],
  )
  const closeOthers = useCallback((id: string) => {
    setTabs((prev) => (prev.some((t) => t.id === id) ? prev.filter((t) => t.id === id) : prev))
    setActiveTab(id)
  }, [])

  const closeRight = useCallback(
    (id: string) => {
      setTabs((prev) => {
        const i = prev.findIndex((t) => t.id === id)
        if (i < 0) return prev
        const next = prev.slice(0, i + 1)
        if (activeTab !== id && !next.some((t) => t.id === activeTab)) setActiveTab(id)
        return next
      })
    },
    [activeTab],
  )

  const closeLeft = useCallback(
    (id: string) => {
      setTabs((prev) => {
        const i = prev.findIndex((t) => t.id === id)
        if (i <= 0) return prev
        const next = prev.slice(i)
        if (!next.some((t) => t.id === activeTab)) setActiveTab(id)
        return next
      })
    },
    [activeTab],
  )

  const closeAllTabs = useCallback(() => {
    setTabs([])
    setActiveTab(null)
  }, [])

  const activateTabAt = useCallback((index: number) => {
    const tab = tabs[index]
    if (tab) setActiveTab(tab.id)
  }, [tabs])

  const activateNextTab = useCallback(() => {
    if (tabs.length < 1) return
    const index = tabs.findIndex((tab) => tab.id === activeTab)
    setActiveTab(tabs[(index + 1 + tabs.length) % tabs.length].id)
  }, [tabs, activeTab])

  const activatePreviousTab = useCallback(() => {
    if (tabs.length < 1) return
    const index = tabs.findIndex((tab) => tab.id === activeTab)
    setActiveTab(tabs[(index - 1 + tabs.length) % tabs.length].id)
  }, [tabs, activeTab])

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

  return {
    tabs, setTabs, activeTab, setActiveTab, cur, updateTab,
    newQueryTab, openDocsTab, openWorkspace, openBrowser, openErd,
    closeTab, closeOthers, closeRight, closeLeft, closeAllTabs,
    activateTabAt, activateNextTab, activatePreviousTab,
    remapTabsSession, restoreStoredTabs, readStoredTabs,
  }
}

export type UseTabs = ReturnType<typeof useTabs>
