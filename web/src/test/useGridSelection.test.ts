import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useGridSelection } from '../hooks/useGridSelection'

describe('useGridSelection duplicate rows', () => {
  it('keeps identical rows independently selectable by index', () => {
    const rows = [['same'], ['same']]
    const { result } = renderHook(() => useGridSelection(rows, (row, ...indexes: number[]) => JSON.stringify([indexes[0], row])))

    act(() => {
      result.current.handleCellClick({ ctrlKey: false, metaKey: false, shiftKey: false }, 0, rows[0])
      result.current.handleCellClick({ ctrlKey: true, metaKey: false, shiftKey: false }, 1, rows[1])
    })

    expect(result.current.sel.size).toBe(2)
  })

  it('clears selection when the displayed rows change', () => {
    const { result, rerender } = renderHook(({ rows }) => useGridSelection(rows, (row, index) => JSON.stringify([index, row])), {
      initialProps: { rows: [['old']] },
    })

    act(() => {
      result.current.handleCellClick({ ctrlKey: false, metaKey: false, shiftKey: false }, 0, ['old'])
    })
    expect(result.current.sel.size).toBe(1)

    rerender({ rows: [['new']] })

    expect(result.current.sel.size).toBe(0)
  })
})
