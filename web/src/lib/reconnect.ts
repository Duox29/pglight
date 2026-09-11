import type { ConnFields } from '@/components/ConnectionBar'

/**
 * Per-session connection memory: which credentials opened each backend
 * session. Same trust level as the existing `last-conn` / saved connections
 * (plaintext localStorage) — no new threat model. Enables 1:1 session
 * remapping after a server restart instead of collapsing every tab onto a
 * single fallback session (which could silently run queries on the wrong DB).
 */
const KEY = 'session-conns'

export function readSessionConns(): Record<string, ConnFields> {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}') as unknown
    if (v && typeof v === 'object') return v as Record<string, ConnFields>
    return {}
  } catch {
    return {}
  }
}

function writeAll(all: Record<string, ConnFields>) {
  try {
    localStorage.setItem(KEY, JSON.stringify(all))
  } catch {
    /* private mode — reconnect memory just won't persist */
  }
}

export function rememberSessionConn(sid: string, f: ConnFields) {
  if (!sid) return
  const all = readSessionConns()
  all[sid] = { ...f }
  writeAll(all)
}

export function forgetSessionConn(sid: string) {
  const all = readSessionConns()
  if (!(sid in all)) return
  delete all[sid]
  writeAll(all)
}
