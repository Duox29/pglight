import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
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
      primaryKeys={[]}
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
      onApplyChanges={vi.fn(async () => true)}
      onMaintenance={vi.fn()}
      onImport={vi.fn()}
      onExportTableCSV={vi.fn()}
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

describe('TableWorkspace staged changes', () => {
  it('shows cell edits as pending and applies them as one batch', async () => {
    const apply = vi.fn(async () => true)
    renderUi(
      <TableWorkspace
        tab={{ ...tab({ columns: ['id', 'name'], rows: [[1, 'A']] }), cols: [{ name: 'id', pk: true }, { name: 'name' }] }}
        primaryKeys={['id']}
        onSubtab={vi.fn()} onFilterChange={vi.fn()} onApply={vi.fn()} onPage={vi.fn()}
        onEditCell={vi.fn(async () => 'B')} onDeleteRow={vi.fn(async () => true)} onCopyInsert={vi.fn()}
        onExportRows={vi.fn()} onCopyRows={vi.fn()} onDeleteRows={vi.fn(async () => true)} onInsert={vi.fn(async () => undefined)}
        onApplyChanges={apply} onMaintenance={vi.fn()} onImport={vi.fn()} onExportTableCSV={vi.fn()} onOpenErd={vi.fn()}
        onAlter={vi.fn(async () => undefined)} onRenameTable={vi.fn()}
        dialogs={{ confirm: vi.fn(async () => true) } as unknown as DialogsApi}
      />,
    )

    fireEvent.doubleClick(screen.getByText('A'))
    await waitFor(() => expect(screen.getByText('B')).toBeInTheDocument())
    expect(screen.getByText('1 pending')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Apply changes' }))
    await waitFor(() => expect(apply).toHaveBeenCalledWith({
      updates: [{ key: { id: 1 }, before: { id: 1, name: 'A' }, changes: { name: 'B' } }],
      inserts: [], deletes: [],
    }))
  })

  it('combines a staged insert and delete with updates in one apply call', async () => {
    const apply = vi.fn(async () => true)
    renderUi(
      <TableWorkspace
        tab={{ ...tab({ columns: ['id', 'name'], rows: [[1, 'A'], [2, 'B']] }), cols: [{ name: 'id', pk: true }, { name: 'name' }] }}
        primaryKeys={['id']}
        onSubtab={vi.fn()} onFilterChange={vi.fn()} onApply={vi.fn()} onPage={vi.fn()}
        onEditCell={vi.fn(async () => 'A2')} onDeleteRow={vi.fn(async () => true)} onCopyInsert={vi.fn()}
        onExportRows={vi.fn()} onCopyRows={vi.fn()} onDeleteRows={vi.fn(async () => true)}
        onInsert={vi.fn(async () => ({ id: 3, name: 'C' }))} onApplyChanges={apply}
        onMaintenance={vi.fn()} onImport={vi.fn()} onExportTableCSV={vi.fn()} onOpenErd={vi.fn()}
        onAlter={vi.fn(async () => undefined)} onRenameTable={vi.fn()}
        dialogs={{ confirm: vi.fn(async () => true) } as unknown as DialogsApi}
      />,
    )

    fireEvent.doubleClick(screen.getByText('A'))
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete row' })[1])
    fireEvent.click(screen.getByRole('button', { name: /Row/ }))
    await waitFor(() => expect(screen.getByText('3 pending')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'Apply changes' }))
    await waitFor(() => expect(apply).toHaveBeenCalledWith({
      updates: [{ key: { id: 1 }, before: { id: 1, name: 'A' }, changes: { name: 'A2' } }],
      inserts: [{ id: 3, name: 'C' }],
      deletes: [{ key: { id: 2 }, before: { id: 2, name: 'B' } }],
    }))
  })
})
