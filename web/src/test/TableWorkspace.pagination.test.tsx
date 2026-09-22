import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { renderUi } from './utils'
import { TableWorkspace } from '../components/TableWorkspace'
import type { DialogsApi } from '../components/dialogs'
import type { TableTabT } from '@/types'

function tab(result: TableTabT['result']): TableTabT {
  return {
    id: 't1', kind: 'table', title: 'authors', sessionId: 's1', schema: 'public', table: 'authors',
    subtab: 'data', limit: 1, offset: 0, filter: '', order: '', result,
    cols: null, ddl: null, constraints: null, triggers: null, stats: null,
  }
}

function renderTable(result: TableTabT['result']) {
  renderUi(
    <TableWorkspace
      tab={tab(result)}
      onSubtab={vi.fn()}
      onFilterChange={vi.fn()}
      onApply={vi.fn()}
      onPage={vi.fn()}
      onEditCell={vi.fn()}
      onDeleteRow={vi.fn()}
      onCopyInsert={vi.fn()}
      onExportRows={vi.fn()}
      onCopyRows={vi.fn()}
      onDeleteRows={vi.fn()}
      onInsert={vi.fn()}
      onMaintenance={vi.fn()}
      onImport={vi.fn()}
      onOpenErd={vi.fn()}
      onAlter={vi.fn(async () => undefined)}
      onRenameTable={vi.fn()}
      dialogs={{} as DialogsApi}
    />,
  )
}

describe('TableWorkspace pagination', () => {
  it('disables Next when the final full page reports has_more=false', () => {
    renderTable({ columns: ['id'], rows: [[1]], has_more: false })
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
  })

  it('keeps Next enabled when the backend reports another page', () => {
    renderTable({ columns: ['id'], rows: [[1]], has_more: true })
    expect(screen.getByRole('button', { name: 'Next page' })).toBeEnabled()
  })
})
