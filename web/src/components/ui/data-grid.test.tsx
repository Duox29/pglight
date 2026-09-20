import { describe, expect, it } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from '../../test/utils'
import { DataGrid } from './data-grid'

const data = {
  columns: ['id', 'name'],
  rows: [
    [1, 'a'],
    [2, 'b'],
    [3, 'c'],
  ] as unknown[][],
}

function selectedRows(container: HTMLElement): number {
  return container.querySelectorAll('tbody tr[data-state="selected"]').length
}

function selectedIds(container: HTMLElement): string[] {
  return [...container.querySelectorAll('tbody tr[data-state="selected"] td:first-child')].map((td) => td.textContent ?? '')
}

describe('DataGrid selection flow (no menu opened here)', () => {
  it('plain click selects exactly one row and drops the previous selection', () => {
    const { container } = renderUi(<DataGrid data={data} selectable />)
    fireEvent.click(screen.getByText('a'))
    expect(selectedIds(container)).toEqual(['1'])
    fireEvent.click(screen.getByText('c'))
    expect(selectedIds(container)).toEqual(['3'])
    expect(selectedRows(container)).toBe(1)
  })

  it('ctrl+click toggles rows for bulk select', () => {
    const { container } = renderUi(<DataGrid data={data} selectable />)
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('b'), { ctrlKey: true })
    expect(selectedIds(container).sort()).toEqual(['1', '2'])
    // ctrl+click again deselects
    fireEvent.click(screen.getByText('a'), { ctrlKey: true })
    expect(selectedIds(container)).toEqual(['2'])
  })

  it('shift+click selects a range from the anchor', () => {
    const { container } = renderUi(<DataGrid data={data} selectable />)
    fireEvent.click(screen.getByText('a'))
    fireEvent.click(screen.getByText('c'), { shiftKey: true })
    expect(selectedIds(container).sort()).toEqual(['1', '2', '3'])
  })

  it('non-selectable grid never marks rows selected', () => {
    const { container } = renderUi(<DataGrid data={data} />)
    fireEvent.click(screen.getByText('a'))
    expect(selectedRows(container)).toBe(0)
  })
})
