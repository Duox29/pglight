import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CommandProvider, useCommand, useCommandRegistration } from './ShortcutProvider'

function Harness({ onRun }: { onRun: () => void }) {
  const command = useCommand()
  useCommandRegistration('query.run', onRun)
  return (
    <>
      <button onClick={() => command.setBindings('query.run', ['F9'])}>set custom</button>
      <button onClick={() => command.resetBinding('query.run')}>reset</button>
      <span>{command.formatBinding('query.run').join(',')}</span>
      <input aria-label="editor input" />
    </>
  )
}

function mockShortcutFetch(overrides: Record<string, string[]> = {}) {
  const fetchMock = vi.mocked(globalThis.fetch)
  fetchMock.mockImplementation(async (input, init) => {
    const path = String(input)
    if (path.endsWith('/api/preferences/shortcuts') && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as { version: number; overrides: Record<string, string[]> }
      return { ok: true, status: 200, text: async () => JSON.stringify(body) } as Response
    }
    if (path.endsWith('/api/preferences/shortcuts')) {
      return { ok: true, status: 200, text: async () => JSON.stringify({ version: 1, overrides }) } as Response
    }
    return { ok: true, status: 200, text: async () => '' } as Response
  })
  return fetchMock
}

afterEach(() => {
  const fetchMock = vi.mocked(globalThis.fetch)
  fetchMock.mockReset()
  fetchMock.mockImplementation(async () => ({ ok: true, status: 200, text: async () => '' }) as Response)
})

describe('CommandProvider', () => {
  it('loads server overrides, dispatches custom keys, and persists reset', async () => {
    const fetchMock = mockShortcutFetch({ 'query.run': ['F9'] })
    const onRun = vi.fn()
    render(<CommandProvider><Harness onRun={onRun} /></CommandProvider>)
    await screen.findByText('F9')

    fireEvent.keyDown(document, { key: 'Enter', ctrlKey: true })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(onRun).not.toHaveBeenCalled()
    fireEvent.keyDown(document, { key: 'F9' })
    await waitFor(() => expect(onRun).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: 'reset' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT' && String(init.body).includes('"overrides":{}'))).toBe(true))
  })

  it('ignores global query shortcuts inside editable inputs', async () => {
    mockShortcutFetch()
    const onRun = vi.fn()
    render(<CommandProvider><Harness onRun={onRun} /></CommandProvider>)
    const input = screen.getByRole('textbox', { name: 'editor input' })
    input.focus()
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(onRun).not.toHaveBeenCalled()
  })

  it('finds conflicts against effective default and custom bindings', async () => {
    function ConflictHarness() {
      const command = useCommand()
      return <span>{command.conflictFor('Mod+K') ?? 'none'}</span>
    }
    mockShortcutFetch({ 'query.run': ['F9'] })
    render(<CommandProvider><ConflictHarness /></CommandProvider>)
    expect(screen.getByText('palette.open')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('palette.open')).toBeInTheDocument())
  })
})
