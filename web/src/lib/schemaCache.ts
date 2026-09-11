import { q } from './api'

/** Schema snapshot served by GET /api/complete (nested: columns stay on tables). */
export interface SnapColumn {
  name: string
  type: string
}
export interface SnapTable {
  schema: string
  name: string
  columns: SnapColumn[]
}
export interface SnapFunc {
  schema: string
  name: string
  args: string
}
export interface SnapFK {
  src: string
  dst: string
}
export interface SchemaSnapshot {
  version: number
  tables: SnapTable[]
  funcs: SnapFunc[]
  fks: SnapFK[]
}

interface RawComplete {
  version: number
  tables: SnapTable[]
  functions?: SnapFunc[]
  funcs?: SnapFunc[]
  fks: SnapFK[]
}

const TTL = 60_000
const mem = new Map<string, { snap: SchemaSnapshot; at: number }>()
const inflight = new Map<string, Promise<SchemaSnapshot | null>>()

async function fetchSnap(session: string, force: boolean, known: SchemaSnapshot | null): Promise<SchemaSnapshot | null> {
  const headers: Record<string, string> = {}
  if (known && !force) headers['If-None-Match'] = `"${known.version}"`
  const res = await fetch(q(session, `/api/complete?${force ? 'refresh=1' : ''}`), { headers })
  if (res.status === 304) return known
  if (!res.ok) return known
  const j = (await res.json()) as RawComplete
  return { version: j.version, tables: j.tables ?? [], funcs: j.funcs ?? j.functions ?? [], fks: j.fks ?? [] }
}

/**
 * Memory-first snapshot loader: at most 1 network request per session per
 * TTL (single-flighted), conditional-GET via ETag otherwise. The CodeMirror
 * completion source reads memory synchronously, so typing never hits network.
 */
export function ensureSnapshot(session: string, force = false): Promise<SchemaSnapshot | null> {
  if (!session) return Promise.resolve(null)
  const hit = mem.get(session)
  if (hit && !force && Date.now() - hit.at < TTL) return Promise.resolve(hit.snap)
  // Stale-while-revalidate: serve the old snapshot now, refresh behind.
  if (hit && !force) {
    void refresh(session, hit.snap)
    return Promise.resolve(hit.snap)
  }
  return refresh(session, hit?.snap ?? null, force)
}

function refresh(session: string, known: SchemaSnapshot | null, force = false): Promise<SchemaSnapshot | null> {
  const run = inflight.get(session)
  if (run) return run
  const p = fetchSnap(session, force, known)
    .then((snap) => {
      if (snap) mem.set(session, { snap, at: Date.now() })
      return snap
    })
    .catch(() => known)
    .finally(() => {
      if (inflight.get(session) === p) inflight.delete(session)
    })
  inflight.set(session, p)
  return p
}

/** Synchronous memory read for the completion source (no fetch on keystroke). */
export function getSnapshotCached(session: string): SchemaSnapshot | null {
  return mem.get(session)?.snap ?? null
}

/** Drop local state: DDL ran (server also invalidates), session closed. */
export function dropSnapshot(session: string) {
  mem.delete(session)
}
