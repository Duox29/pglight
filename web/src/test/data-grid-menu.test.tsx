import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from './utils'
import { DataGrid } from '../components/ui/data-grid'

// NOTE: jsdom + Radix ContextMenu only opens the first menu per test file, so
// this file holds exactly one menu-opening test (selection-only tests live in
// data-grid.test.tsx).
describe('DataGrid context menu + selection combined', () => {
  it('right-click opens the custom menu, selects the row, and copies the cell', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const { container } = renderUi(
      <DataGrid
        data={{ columns: ['id', 'name'], rows: [[1, 'a'], [2, 'b']] as unknown[][] }}
        selectable
      />,
    )
    // Right-click the second row: menu must open (not the browser default)
    // and the row becomes the single selection.
    fireEvent.contextMenu(screen.getByText('b'))
    expect(await screen.findByRole('menuitem', { name: /Copy cell value/ })).toBeInTheDocument()
    expect(await screen.findByRole('menuitem', { name: /Export selected/ })).toBeInTheDocument()
    expect([...container.querySelectorAll('tbody tr[data-state="selected"] td:first-child')].map((td) => td.textContent)).toEqual(['2'])

    // "Copy cell value" copies the right-clicked cell, not the whole row.
    fireEvent.click(await screen.findByRole('menuitem', { name: /Copy cell value/ }))
    expect(writeText).toHaveBeenCalledWith('b')
  })
})
