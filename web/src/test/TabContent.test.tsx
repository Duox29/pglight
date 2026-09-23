import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from './utils'
import { TabContent, type TabContentProps } from '../components/TabContent'
import type { DialogsApi } from '../components/dialogs'
import type { ErdTabT, ObjectTabT, Tab, TableTabT } from '@/types'

const apiMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', () => ({
  api: apiMock,
  apiClient: {},
  q: (session: string, path: string) => `${path}&session_id=${session}`,
}))

vi.mock('../components/ErdView', () => ({
  ErdView: (props: { onSchema: (schema: string) => void; onReload: () => void }) => (
    <div>
      <button type="button" onClick={() => props.onSchema('alpha')}>Load alpha</button>
      <button type="button" onClick={() => props.onSchema('beta')}>Load beta</button>
      <button type="button" onClick={props.onReload}>Reload</button>
    </div>
  ),
}))

vi.mock('../components/TableWorkspace', () => ({
  TableWorkspace: (props: { tab: TableTabT }) => <input aria-label="table filter" defaultValue={props.tab.filter} />,
}))

function tableTab(id: string, table: string, filter: string): TableTabT {
  return {
    id,
    kind: 'table',
    title: table,
    sessionId: 's1',
    schema: 'public',
    table,
    subtab: 'data',
    limit: 100,
    offset: 0,
    filter,
    order: '',
    result: null,
    cols: null,
    ddl: null,
    constraints: null,
    triggers: null,
    stats: null,
  }
}

function functionTab(id: string, name: string, def: string): ObjectTabT {
  return {
    id,
    kind: 'object',
    title: name,
    sessionId: 's1',
    objectKind: 'function',
    schema: 'public',
    name,
    def,
    details: {},
  }
}

function baseProps(): TabContentProps {
  return {
    tab: null,
    connected: true,
    running: {},
    txnFor: vi.fn(() => ({
      inTxn: false,
      autocommit: true,
      connLabel: '',
      connTip: '',
      onAutocommit: vi.fn(),
      onTxn: vi.fn(),
    })),
    markTxn: vi.fn(),
    updateTab: vi.fn(),
    newQueryTab: vi.fn(),
    openErd: vi.fn(),
    openTable: vi.fn(),
    query: {
      runQuery: vi.fn(),
      cancelQuery: vi.fn(),
      explainQuery: vi.fn(),
      setSnippets: vi.fn(),
    },
    table: {
      loadTablePage: vi.fn(),
      loadTableMeta: vi.fn(),
      rowOp: vi.fn(),
      alterTable: vi.fn(),
      renameTable: vi.fn(),
    },
    object: {
      loadObjectDef: vi.fn(),
      saveSequence: vi.fn(),
      saveFunction: vi.fn(),
      enumAdd: vi.fn(),
      renameObject: vi.fn(),
      dropObject: vi.fn(),
    },
    explorer: { copyName: vi.fn(), schemas: [] },
    sessions: [],
    savedConnections: [],
    dialogs: {} as DialogsApi,
    session: 's1',
    history: [],
    onHistoryChange: vi.fn(),
    snippets: [],
    onOpenSql: vi.fn(),
    onDeleteSnippet: vi.fn(),
    onSaveSnippet: vi.fn(async () => undefined),
    quickAccess: [],
    onQuickAccessChange: vi.fn(),
    connection: {
      fields: { host: '', port: '5432', user: '', password: '', dbname: '', sslmode: 'prefer' },
      setFields: vi.fn(),
      saved: [],
      onConnect: vi.fn(async () => 's1'),
      onTest: vi.fn(async () => ({})),
      onSave: vi.fn(async () => undefined),
      onDuplicate: vi.fn(async () => 'copy'),
      onDelete: vi.fn(async () => undefined),
      vault: { exists: false, unlocked: false },
      onVaultAction: vi.fn(async () => undefined),
      autoLogin: false,
      onAutoLogin: vi.fn(),
      sessions: [],
      activeId: 's1',
      onSwitch: vi.fn(),
      onDisconnectOne: vi.fn(),
      deadIds: {},
      onReconnectOne: vi.fn(),
      onReconnectAll: vi.fn(),
    },
  }
}

describe('TabContent tab isolation', () => {
  it('resets a table filter when switching table tabs', () => {
    const props = baseProps()
    const { rerender } = renderUi(<TabContent {...props} tab={tableTab('t-users', 'users', 'active = true')} />)
    expect(screen.getByLabelText('table filter')).toHaveValue('active = true')

    rerender(<TabContent {...props} tab={tableTab('t-orders', 'orders', "status = 'paid'")} />)
    expect(screen.getByLabelText('table filter')).toHaveValue("status = 'paid'")
  })

  it('clears an object edit buffer when switching object tabs', () => {
    const props = baseProps()
    const { rerender } = renderUi(<TabContent {...props} tab={functionTab('f-a', 'a()', 'SELECT A()')} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('textbox')).toHaveValue('SELECT A()')

    rerender(<TabContent {...props} tab={functionTab('f-b', 'b()', 'SELECT B()')} />)
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).toBeNull()
  })
})

describe('TabContent ERD request ordering', () => {
  it('ignores a slower response for an older schema request', async () => {
    const props = baseProps()
    let current: Tab = { id: 'e1', kind: 'erd', title: 'ERD public', sessionId: 's1', schema: 'public', data: null }
    const updateTab = vi.fn((id: string, fn: (value: Tab) => Tab) => {
      void id
      current = fn(current)
    })
    props.updateTab = updateTab
    const tab = current as ErdTabT
    const responses: Array<(value: unknown) => void> = []
    apiMock.mockImplementation(() => new Promise((resolve) => responses.push(resolve)))
    renderUi(<TabContent {...props} tab={tab} />)

    fireEvent.click(screen.getByRole('button', { name: 'Load alpha' }))
    fireEvent.click(screen.getByRole('button', { name: 'Load beta' }))
    expect(responses).toHaveLength(2)

    responses[1]({ nodes: ['beta'], edges: [] })
    await Promise.resolve()
    responses[0]({ nodes: ['alpha'], edges: [] })
    await Promise.resolve()

    expect(current.kind).toBe('erd')
    if (current.kind !== 'erd') return
    expect(current.schema).toBe('beta')
    expect(current.data?.nodes).toEqual(['beta'])
  })
})
