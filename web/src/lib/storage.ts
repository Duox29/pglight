import { useEffect, useState } from 'react'

/** Thrown for transport failures and non-JSON bodies. HTTP error statuses */
/** resolve as `{error}` JSON per the backend contract — check `j.error`. */
export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}

/** Typed fetch wrapper around the Go backend API. */
/** Throws ApiError on network failure or invalid JSON, never on `{error}` bodies. */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (e) {
    throw new ApiError(e instanceof Error ? e.message : 'Network request failed', 0)
  }
  if (res.status === 401) notifyUnauthorized(path, init)
  const text = await res.text().catch(() => '')
  if (!text) {
    if (!res.ok) throw new ApiError(`Request failed (${res.status})`, res.status)
    return null as T
  }
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(`Invalid response (${res.status})`, res.status)
  }
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

export function useAppPreference<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key)
      return raw ? (JSON.parse(raw) as T) : initial
    } catch {
      return initial
    }
  })
  useEffect(() => {
    let alive = true
    void api<{ preferences?: Record<string, unknown> }>('/api/preferences').then((j) => {
      if (!alive) return
      if (j.preferences && Object.prototype.hasOwnProperty.call(j.preferences, key)) {
        setValue(j.preferences[key] as T)
      } else {
        void api('/api/preferences', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ [key]: value }) }).catch(() => undefined)
      }
    }).catch(() => undefined)
    return () => { alive = false }
    // Initial cache is only a fast first paint; backend becomes source of truth.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  const set = (v: T | ((prev: T) => T)) => {
    setValue((prev) => {
      const next = typeof v === 'function' ? (v as (p: T) => T)(prev) : v
      try { localStorage.setItem(key, JSON.stringify(next)) } catch { /* cache is best-effort */ }
      void api('/api/preferences', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ [key]: next }) }).catch(() => undefined)
      return next
    })
  }
  return [value, set] as const
}
