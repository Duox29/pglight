import { useState } from 'react'

/** Typed fetch wrapper around the Go backend API. */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (res.status === 401) notifyUnauthorized(path, init)
  return (await res.json()) as T
}

type UnauthorizedCb = (sessionId: string) => void
const unauthorizedCbs = new Set<UnauthorizedCb>()

/** Subscribe to backend 401s (dead/unknown session). Returns an unsubscribe. */
export function onUnauthorized(cb: UnauthorizedCb): () => void {
  unauthorizedCbs.add(cb)
  return () => {
    unauthorizedCbs.delete(cb)
  }
}

// 401s only happen on session-scoped endpoints; the sid travels either in
// the query string or the JSON body. Endpoints without a sid are ignored.
function notifyUnauthorized(path: string, init?: RequestInit) {
  let sid = /[?&]session_id=([^&]*)/.exec(path)?.[1] ?? ''
  if (sid) {
    try {
      sid = decodeURIComponent(sid)
    } catch {
      /* keep raw */
    }
  }
  if (!sid && init?.body && typeof init.body === 'string') {
    try {
      sid = (JSON.parse(init.body) as { session_id?: string }).session_id ?? ''
    } catch {
      /* not JSON — ignore */
    }
  }
  if (!sid) return
  for (const cb of unauthorizedCbs) {
    try {
      cb(sid)
    } catch {
      /* subscriber bug must not break the request */
    }
  }
}

export function useLocalStorage<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key)
      return raw ? (JSON.parse(raw) as T) : initial
    } catch {
      return initial
    }
  })
  const set = (v: T | ((prev: T) => T)) => {
    setValue((prev) => {
      const next = typeof v === 'function' ? (v as (p: T) => T)(prev) : v
      try {
        localStorage.setItem(key, JSON.stringify(next))
      } catch {
        /* ignore */
      }
      return next
    })
  }
  return [value, set] as const
}
