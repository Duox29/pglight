import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SavedConnection } from '@/types'
import { useSessions } from './useSessions'

afterEach(() => {
  vi.mocked(globalThis.fetch).mockReset()
  vi.mocked(globalThis.fetch).mockImplementation(async () => ({ ok: true, status: 200, text: async () => '' }) as Response)
  localStorage.clear()
})

describe('useSessions profile connections', () => {
  it('reuses an existing session when connecting the same saved profile again', async () => {
    const fetchMock = vi.mocked(globalThis.fetch)
    fetchMock.mockImplementation(async (input) => {
      const path = String(input)
      if (path.endsWith('/api/connections')) {
        return new Response(JSON.stringify({ connections: [{ id: 'profile-1', name: 'Local', host: 'localhost', port: 5432, user: 'postgres', dbname: 'postgres', sslmode: 'disable', has_password: false }] }), { status: 200 })
      }
      if (path.endsWith('/api/vault')) return new Response(JSON.stringify({ exists: false, unlocked: false }), { status: 200 })
      if (path.endsWith('/api/sessions')) return new Response(JSON.stringify({ sessions: [] }), { status: 200 })
      if (path.endsWith('/api/preferences')) return new Response(JSON.stringify({ preferences: {} }), { status: 200 })
      if (path.endsWith('/api/connect')) {
        return new Response(JSON.stringify({ session_id: 'session-1', info: { host: 'localhost', port: 5432, user: 'postgres', dbname: 'postgres', sslmode: 'disable', in_txn: false } }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })

    const { result } = renderHook(() => useSessions({ onRemap: vi.fn() }))
    await waitFor(() => expect(result.current.saved).toHaveLength(1))
    const profile: SavedConnection = result.current.saved[0]

    let first = ''
    await act(async () => {
      first = await result.current.connectProfile(profile, 'postgres')
    })
    let second = ''
    await act(async () => {
      second = await result.current.connectProfile(profile, 'postgres')
    })

    expect(first).toBe('session-1')
    expect(second).toBe('session-1')
    expect(fetchMock.mock.calls.filter(([input]) => String(input).endsWith('/api/connect'))).toHaveLength(1)
    expect(result.current.sessions).toHaveLength(1)
  })
})
