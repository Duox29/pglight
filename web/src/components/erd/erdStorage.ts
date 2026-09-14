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
