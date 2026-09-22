import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, screen } from '@testing-library/react'
import { renderUi } from './utils'
import { CommandProvider } from '@/shortcuts/ShortcutProvider'
import { SearchPalette } from '../components/SearchPalette'

const apiMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/api', () => ({
  api: apiMock,
  q: (session: string, path: string) => `${path}&session_id=${session}`,
  apiClient: {
    getShortcutSettings: vi.fn(async () => ({ version: 1, overrides: {} })),
    saveShortcutSettings: vi.fn(async () => ({ version: 1, overrides: {} })),
  },
}))

function renderPalette(session: string) {
  return renderUi(
    <CommandProvider>
      <SearchPalette open onOpenChange={vi.fn()} session={session} onOpenTable={vi.fn()} />
    </CommandProvider>,
  )
}

afterEach(() => {
  vi.useRealTimers()
  apiMock.mockReset()
})

describe('SearchPalette stale responses', () => {
  it('ignores a response after the query is shortened below two characters', async () => {
    vi.useFakeTimers()
    const responses: Array<(value: unknown) => void> = []
    apiMock.mockImplementation(() => new Promise((resolve) => responses.push(resolve)))
    renderPalette('s1')

    fireEvent.change(screen.getByPlaceholderText('Run a command or search database objects…'), { target: { value: 'ab' } })
    act(() => { vi.advanceTimersByTime(200) })
    fireEvent.change(screen.getByPlaceholderText('Run a command or search database objects…'), { target: { value: 'a' } })
    responses[0]([{ kind: 'table', schema: 'public', name: 'users', detail: '' }])
    await act(async () => { await Promise.resolve() })

    expect(screen.queryByText('public.users')).toBeNull()
  })

  it('ignores an in-flight response from the previous session', async () => {
    vi.useFakeTimers()
    const responses: Array<(value: unknown) => void> = []
    apiMock.mockImplementation(() => new Promise((resolve) => responses.push(resolve)))
    const view = renderPalette('s1')

    fireEvent.change(screen.getByPlaceholderText('Run a command or search database objects…'), { target: { value: 'ab' } })
    act(() => { vi.advanceTimersByTime(200) })
    view.rerender(
      <CommandProvider>
        <SearchPalette open onOpenChange={vi.fn()} session="s2" onOpenTable={vi.fn()} />
      </CommandProvider>,
    )
    responses[0]([{ kind: 'table', schema: 'public', name: 'users', detail: '' }])
    await act(async () => { await Promise.resolve() })

    expect(screen.queryByText('public.users')).toBeNull()
  })
})
