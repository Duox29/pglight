import { useState } from 'react'
import type { TableChangesPayload } from '@/lib/api'

type Update = NonNullable<TableChangesPayload['updates']>[number]
type Delete = NonNullable<TableChangesPayload['deletes']>[number]
type Pending = { updates: Update[]; inserts: Record<string, unknown>[]; deletes: Delete[] }
const savedPending = new Map<string, Pending>()
const emptyPending = (): Pending => ({ updates: [], inserts: [], deletes: [] })

export function clearStagedTableChanges(tabId: string) {
  savedPending.delete(tabId)
}

export function remapStagedTableChanges(oldTabId: string, newTabId: string) {
  const pending = savedPending.get(oldTabId)
  if (pending) {
    savedPending.delete(oldTabId)
    savedPending.set(newTabId, pending)
  }
}

const identity = (key: Record<string, unknown>) => JSON.stringify(Object.keys(key).sort().map((name) => [name, key[name]]))
const equal = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

export function useStagedTableChanges(tabId?: string) {
  const [pending, setPending] = useState<Pending>(() => tabId ? savedPending.get(tabId) ?? emptyPending() : emptyPending())
  const setChanges = (update: (current: Pending) => Pending) => setPending((current) => {
    const next = update(current)
    if (tabId) {
      if (next.updates.length + next.inserts.length + next.deletes.length) savedPending.set(tabId, next)
      else savedPending.delete(tabId)
    }
    return next
  })

  const stageUpdate = (input: { key: Record<string, unknown>; before: Record<string, unknown>; column: string; value: unknown }) => {
    const id = identity(input.key)
    setChanges((current) => {
      if (current.deletes.some((item) => identity(item.key) === id)) return current
      const existing = current.updates.find((item) => identity(item.key) === id)
      const changes = { ...(existing?.changes ?? {}) }
      if (equal(input.before[input.column], input.value)) delete changes[input.column]
      else changes[input.column] = input.value
      const updates = current.updates.filter((item) => identity(item.key) !== id)
      if (Object.keys(changes).length) updates.push({ key: input.key, before: existing?.before ?? input.before, changes })
      return { ...current, updates }
    })
  }

  const stageInsert = (values: Record<string, unknown>) => setChanges((current) => ({ ...current, inserts: [...current.inserts, values] }))

  const stageDelete = (item: Delete) => setChanges((current) => ({
    ...current,
    updates: current.updates.filter((update) => identity(update.key) !== identity(item.key)),
    deletes: current.deletes.some((existing) => identity(existing.key) === identity(item.key)) ? current.deletes : [...current.deletes, item],
  }))

  const undoCell = (key: Record<string, unknown>, column: string) => setChanges((current) => ({
    ...current,
    updates: current.updates.flatMap((item) => {
      if (identity(item.key) !== identity(key)) return [item]
      const changes = { ...item.changes }
      delete changes[column]
      return Object.keys(changes).length ? [{ ...item, changes }] : []
    }),
  }))

  const undoRow = (key: Record<string, unknown>) => setChanges((current) => ({
    ...current,
    updates: current.updates.filter((item) => identity(item.key) !== identity(key)),
    deletes: current.deletes.filter((item) => identity(item.key) !== identity(key)),
  }))

  const undoInsert = (index: number) => setChanges((current) => ({
    ...current,
    inserts: current.inserts.filter((_, i) => i !== index),
  }))

  const discard = () => setChanges(() => emptyPending())
  const pendingCount = pending.updates.length + pending.inserts.length + pending.deletes.length

  return { pending, pendingCount, stageUpdate, stageInsert, stageDelete, undoCell, undoRow, undoInsert, discard }
}
