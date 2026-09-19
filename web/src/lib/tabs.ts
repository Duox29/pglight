import type { SavedConnection, SessionInfo, StoredTab, Tab, TableTabT } from '../types'

export const DEFAULT_SQL = 'SELECT * FROM information_schema.tables LIMIT 20;'

export const qi = (s: string) => `"${s.replace(/"/g, '""')}"`

export const shortSid = (sid: string) => (sid.length > 6 ? sid.slice(-6) : sid)

/** Primary-key columns from /api/columns metadata, or [] when the table has no safe row identity. */
export function pkOf(t: TableTabT): string[] {
  const cols = t.cols ?? []
  const pks = cols.filter((c) => String(c['pk'] ?? '').toLowerCase() === 't' || c['pk'] === true).map((c) => String(c['name'] ?? ''))
  return pks.filter(Boolean)
}

// Mirrors the backend complete-cache DDL detector (ddlRe): after a
// schema-changing run the editor snapshot is dropped and refetched once.
export const DDL_RE = /\b(CREATE|ALTER|DROP|TRUNCATE|COMMENT)\b/i

export const BROWSER_DEFS: Record<string, { title: string; url: string; cols: string[] }> = {
  extensions: { title: 'Extensions', url: '/api/extensions', cols: ['name', 'default_version', 'installed_version', 'comment'] },
  roles: { title: 'Roles', url: '/api/roles', cols: ['name', 'superuser', 'login', 'createdb', 'member_of'] },
}

export function slimTab(t: Tab): StoredTab | null {
  const sessionId = (t as { sessionId?: string }).sessionId
  switch (t.kind) {
    case 'query': {
      // Keep a capped snapshot of the last result (never auto re-run: the SQL
      // could be DML). Oversized snapshots are dropped to protect storage.
      // Snapshots hold result data, so they persist only when the Privacy
      // setting allows it (default off).
      let snapshot: StoredTab['snapshot']
      const r = t.results?.[0]
      if (r && r.columns?.length && !r.stale && readJSON<boolean>('privacy.persistSnapshots', false)) {
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
    case 'object':
      return { id: t.id, kind: t.kind, title: t.title, sessionId, schema: t.schema, name: t.name, objectKind: t.objectKind }
    case 'docs':
      return { id: 'docs', kind: 'docs', title: 'Docs' }
  }
}

export function readJSON<T>(key: string, fallback: T): T {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? 'null') as T
    return v ?? fallback
  } catch {
    return fallback
  }
}

export function readStoredTabs(): StoredTab[] {
  try {
    const raw = JSON.parse(localStorage.getItem('open-tabs') ?? '[]') as StoredTab[]
    if (!Array.isArray(raw)) return []
    return raw.filter((t) => t && typeof t.id === 'string' && typeof t.kind === 'string').slice(0, 20)
  } catch {
    return []
  }
}

/** Saved-connection id backing an ERD tab's session (for the ERD toolbar), or undefined. Pure derivation. */
export function erdConnectionIdFor(sessions: SessionInfo[], saved: SavedConnection[], tab: Tab | null): string | undefined {
  if (tab?.kind !== 'erd') return undefined
  const sess = sessions.find((s) => s.id === tab.sessionId)
  return saved.find((c) => c.id && c.host === (sess?.host ?? '') && String(c.port) === String(sess?.port ?? '') && c.user === (sess?.user ?? '') && c.dbname === (sess?.dbname ?? '') && c.sslmode === (sess?.sslmode ?? ''))?.id
}

/** Tabs eligible for the split right pane: everything except the pinned (left) one. */
export function splitOptions(tabs: Tab[], pinnedId?: string): Tab[] {
  return tabs.filter((t) => t.id !== pinnedId)
}
