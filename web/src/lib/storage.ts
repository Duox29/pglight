import { useState } from 'react'

/** Typed fetch wrapper around the Go backend API. */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  return (await res.json()) as T
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
