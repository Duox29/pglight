import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CommandProvider } from '@/shortcuts/ShortcutProvider'
import { KeyboardShortcutsPanel } from '../components/KeyboardShortcutsPanel'

afterEach(() => {
  const fetchMock = vi.mocked(globalThis.fetch)
  fetchMock.mockReset()
  fetchMock.mockImplementation(async () => ({ ok: true, status: 200, text: async () => '' }) as Response)
})

describe('KeyboardShortcutsPanel', () => {
  it('records a key and sends the complete override payload', async () => {
    const fetchMock = vi.mocked(globalThis.fetch)
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input).endsWith('/api/preferences/shortcuts') && init?.method === 'PUT') {
        return { ok: true, status: 200, text: async () => String(init.body) } as Response
      }
      return { ok: true, status: 200, text: async () => JSON.stringify({ version: 1, overrides: {} }) } as Response
    })
    const dialogs = { confirm: vi.fn().mockResolvedValue(true) }
    render(<CommandProvider><KeyboardShortcutsPanel dialogs={dialogs as never} /></CommandProvider>)

    const runButton = await screen.findByRole('button', { name: 'Ctrl+Enter' })
    fireEvent.click(runButton)
    const recorder = screen.getByRole('button', { name: 'Press keys…' })
    fireEvent.keyDown(recorder, { key: 'F9', code: 'F9' })

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT' && String(init.body).includes('"query.run":["F9"]'))).toBe(true))
    expect(screen.getByRole('button', { name: 'F9' })).toBeInTheDocument()
  })

  it('asks before replacing an existing shortcut binding', async () => {
    const fetchMock = vi.mocked(globalThis.fetch)
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input).endsWith('/api/preferences/shortcuts') && init?.method === 'PUT') return { ok: true, status: 200, text: async () => String(init.body) } as Response
      return { ok: true, status: 200, text: async () => JSON.stringify({ version: 1, overrides: {} }) } as Response
    })
    const dialogs = { confirm: vi.fn().mockResolvedValue(true) }
    render(<CommandProvider><KeyboardShortcutsPanel dialogs={dialogs as never} /></CommandProvider>)

    const runButton = await screen.findByRole('button', { name: 'Ctrl+Enter' })
    fireEvent.click(runButton)
    fireEvent.keyDown(screen.getByRole('button', { name: 'Press keys…' }), { key: 'k', ctrlKey: true })

    await waitFor(() => expect(dialogs.confirm).toHaveBeenCalled())
    expect(dialogs.confirm.mock.calls[0][0].description).toContain('Command Palette')
  })
})
