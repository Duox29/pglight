import type { ErdPos } from './erdLayout'

const prefix = 'pglight-erd-layout'

/** Local-only layout persistence. Keyed by session + schema, never credentials. */
export const erdLayoutKey = (sessionId: string, schema: string) => `${prefix}:${sessionId}:${schema}`

export function loadErdLayout(sessionId: string, schema: string): Record<string, ErdPos> {
  try {
    const raw = localStorage.getItem(erdLayoutKey(sessionId, schema))
    if (!raw) return {}
    const parsed = JSON.parse(raw) as Record<string, ErdPos>
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

export function saveErdLayout(sessionId: string, schema: string, pos: Record<string, ErdPos>): void {
  try {
    localStorage.setItem(erdLayoutKey(sessionId, schema), JSON.stringify(pos))
  } catch {
    /* private mode / quota — layout just won't persist */
  }
}

export function clearErdLayout(sessionId: string, schema: string): void {
  try {
    localStorage.removeItem(erdLayoutKey(sessionId, schema))
  } catch {
    /* ignore */
  }
}

export interface ErdViewport {
  x: number
  y: number
  zoom: number
}

const viewportPrefix = 'pglight-erd-viewport'

export const erdViewportKey = (sessionId: string, schema: string) =>
  `${viewportPrefix}:${sessionId}:${schema}`

function isViewport(v: unknown): v is ErdViewport {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  return (
    typeof o.x === 'number' &&
    Number.isFinite(o.x) &&
    typeof o.y === 'number' &&
    Number.isFinite(o.y) &&
    typeof o.zoom === 'number' &&
    Number.isFinite(o.zoom) &&
    o.zoom > 0
  )
}

export function loadErdViewport(sessionId: string, schema: string): ErdViewport | null {
  try {
    const raw = localStorage.getItem(erdViewportKey(sessionId, schema))
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    return isViewport(parsed) ? parsed : null
  } catch {
    return null
  }
}

export function saveErdViewport(sessionId: string, schema: string, vp: ErdViewport): void {
  try {
    localStorage.setItem(erdViewportKey(sessionId, schema), JSON.stringify(vp))
  } catch {
    /* private mode / quota — viewport just won't persist */
  }
}

export function clearErdViewport(sessionId: string, schema: string): void {
  try {
    localStorage.removeItem(erdViewportKey(sessionId, schema))
  } catch {
    /* ignore */
  }
}
