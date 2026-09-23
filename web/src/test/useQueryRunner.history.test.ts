import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { QueryTabT } from '@/types'
import { useQueryRunner } from '@/hooks/useQueryRunner'

afterEach(() => {
  vi.mocked(globalThis.fetch).mockReset()
  vi.mocked(globalThis.fetch).mockImplementation(async () => new Response('{}', { status: 200 }))
  localStorage.clear()
})

describe('query history diagnostics', () => {
  it('persists a failed query with its SQL state and error message', async () => {
    let recorded: Record<string, unknown> | null = null
    vi.mocked(globalThis.fetch).mockImplementation(async (input, init) => {
      const path = String(input)
      if (path.endsWith('/api/history') && init?.method === 'POST') recorded = JSON.parse(String(init.body)) as Record<string, unknown>
      if (path.endsWith('/api/query')) return new Response(JSON.stringify({ error: 'permission denied', code: '42501', in_txn: false }), { status: 400 })
      return new Response(JSON.stringify({ history: [], snippets: [] }), { status: 200 })
    })
    const tab: QueryTabT = { id: 'q1', kind: 'query', title: 'Query', sessionId: 's1', sql: 'select * from secret_table', limit: 100, results: null }
    const { result } = renderHook(() => useQueryRunner({ tabs: [tab], updateTab: vi.fn(), session: 's1', autocommit: true, markTxn: vi.fn() }))
    await act(async () => { await result.current.runQuery('q1') })
    await waitFor(() => expect(recorded).not.toBeNull())
    expect(recorded).toMatchObject({ session_id: 's1', success: false, error_code: '42501', error_message: 'permission denied' })
  })
})
