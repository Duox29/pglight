import { act, renderHook } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { useTableOps } from './useTableOps'
import type { QueryResult } from '../lib/api'
import type { Tab } from '../types'

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }))

vi.mock('../lib/api', () => ({
  api: apiMock,
  apiClient: {},
  q: (_session: string, path: string) => path,
}))

const table = (): Tab => ({
  id: 't1', kind: 'table', title: 'authors', sessionId: 's1', schema: 'public', table: 'authors',
  subtab: 'data', limit: 100, offset: 0, filter: '', order: '', result: null,
  cols: null, ddl: null, constraints: null, triggers: null, stats: null,
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

describe('useTableOps request ordering', () => {
  it('keeps the newest table page when an older request resolves later', async () => {
    apiMock.mockReset()
    const first = deferred<QueryResult & { in_txn?: boolean }>()
    const second = deferred<QueryResult & { in_txn?: boolean }>()
    apiMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)

    const { result } = renderHook(() => {
      const [tabs, setTabs] = useState<Tab[]>([table()])
      const ops = useTableOps({
        tabs, setTabs, setActiveTab: vi.fn(), updateTab: vi.fn(), markTxn: vi.fn(),
        autocommit: true, dialogs: {} as never, loadExplorer: vi.fn(async () => undefined),
      })
      return { tabs, ops }
    })

    await act(async () => {
      void result.current.ops.loadTablePage('s1', 't1', 'public', 'authors', 100, 0, 'old', '')
      void result.current.ops.loadTablePage('s1', 't1', 'public', 'authors', 100, 0, 'new', '')
      second.resolve({ columns: ['id'], rows: [[2]] })
      await second.promise
    })
    await act(async () => {
      first.resolve({ columns: ['id'], rows: [[1]] })
      await first.promise
    })

    expect(result.current.tabs[0].kind).toBe('table')
    if (result.current.tabs[0].kind === 'table') {
      expect(result.current.tabs[0].filter).toBe('new')
      expect(result.current.tabs[0].result?.rows).toEqual([[2]])
    }
  })
})
