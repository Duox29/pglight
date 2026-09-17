import type { ConnFields } from '@/components/ConnectionBar'

// Credentials are runtime-only. Persistent connection profiles deliberately
// never contain passwords; reconnect after a full page reload requires a fresh
// password entry.
const sessions = new Map<string, ConnFields>()

export function readSessionConns(): Record<string, ConnFields> {
  return Object.fromEntries([...sessions.entries()].map(([k, v]) => [k, { ...v }]))
}
export function rememberSessionConn(sid: string, f: ConnFields) {
  if (sid) sessions.set(sid, { ...f })
}
export function forgetSessionConn(sid: string) { sessions.delete(sid) }
