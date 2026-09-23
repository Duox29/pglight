import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { clearStagedTableChanges, useStagedTableChanges } from '@/hooks/useStagedTableChanges'
import { formatTableChangesPreview } from '@/lib/format'

describe('useStagedTableChanges', () => {
  it('coalesces cell edits and removes a cell change when restored to its original value', () => {
    const { result } = renderHook(() => useStagedTableChanges())
    act(() => result.current.stageUpdate({ key: { id: 1 }, before: { id: 1, name: 'A', active: true }, column: 'name', value: 'B' }))
    act(() => result.current.stageUpdate({ key: { id: 1 }, before: { id: 1, name: 'A', active: true }, column: 'active', value: false }))
    expect(result.current.pending.updates).toEqual([{ key: { id: 1 }, before: { id: 1, name: 'A', active: true }, changes: { name: 'B', active: false } }])
    act(() => result.current.stageUpdate({ key: { id: 1 }, before: { id: 1, name: 'A', active: true }, column: 'name', value: 'A' }))
    expect(result.current.pending.updates[0].changes).toEqual({ active: false })
  })

  it('undoes one cell and replaces a row update with a staged delete', () => {
    const { result } = renderHook(() => useStagedTableChanges())
    const before = { id: 2, name: 'old' }
    act(() => result.current.stageUpdate({ key: { id: 2 }, before, column: 'name', value: 'new' }))
    act(() => result.current.undoCell({ id: 2 }, 'name'))
    expect(result.current.pending.updates).toEqual([])
    act(() => result.current.stageUpdate({ key: { id: 2 }, before, column: 'name', value: 'new' }))
    act(() => result.current.stageDelete({ key: { id: 2 }, before }))
    expect(result.current.pending.updates).toEqual([])
    expect(result.current.pending.deletes).toEqual([{ key: { id: 2 }, before }])
    act(() => result.current.undoRow({ id: 2 }))
    expect(result.current.pending.deletes).toEqual([])
  })

  it('renders a safe SQL preview for staged values and identifiers', () => {
    expect(formatTableChangesPreview('public', 'order"items', {
      updates: [{ key: { id: 3 }, before: { id: 3, note: "old" }, changes: { note: "x'); DROP TABLE t;--" } }],
      inserts: [], deletes: [],
    })).toEqual([`UPDATE "public"."order""items" SET "note" = 'x''); DROP TABLE t;--' WHERE "id" IS NOT DISTINCT FROM 3 AND "note" IS NOT DISTINCT FROM 'old';`])
  })

  it('supports staged inserts, undo, and discard', () => {
    const { result } = renderHook(() => useStagedTableChanges())
    act(() => result.current.stageInsert({ id: 4, name: 'new' }))
    act(() => result.current.stageInsert({ id: 5, name: 'another' }))
    expect(result.current.pendingCount).toBe(2)
    act(() => result.current.undoInsert(0))
    expect(result.current.pending.inserts).toEqual([{ id: 5, name: 'another' }])
    act(() => result.current.discard())
    expect(result.current.pendingCount).toBe(0)
  })

  it('keeps staged changes when a table tab is temporarily unmounted', () => {
    const first = renderHook(() => useStagedTableChanges('table-tab-1'))
    act(() => first.result.current.stageInsert({ id: 9, name: 'saved in memory' }))
    first.unmount()
    const second = renderHook(() => useStagedTableChanges('table-tab-1'))
    expect(second.result.current.pending.inserts).toEqual([{ id: 9, name: 'saved in memory' }])
    second.unmount()
    clearStagedTableChanges('table-tab-1')
  })
})
