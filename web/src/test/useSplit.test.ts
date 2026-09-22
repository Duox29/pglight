import { describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { toast } from 'sonner'
import { useSplit } from '../hooks/useSplit'
import type { Tab } from '../types'

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}))

const tab = (id: string, title = id): Tab => ({
  id, kind: 'query', title, sessionId: 's1', sql: 'SELECT 1', limit: 200, results: null,
})

function setup(tabs: Tab[], activeTab: string | null) {
  const onActivate = vi.fn()
  const hook = renderHook(
    ({ tabs, activeTab }) => useSplit({ tabs, activeTab, onActivate }),
    { initialProps: { tabs, activeTab } },
  )
  return { ...hook, onActivate }
}

describe('useSplit initial state', () => {
  it('starts unpinned with horizontal direction', () => {
    const { result } = setup([tab('a'), tab('b')], 'a')
    expect(result.current.pinnedId).toBeNull()
    expect(result.current.pinnedTab).toBeNull()
    expect(result.current.splitDir).toBe('horizontal')
  })

  it('starts unpinned with no tabs', () => {
    const { result } = setup([], null)
    expect(result.current.pinnedId).toBeNull()
    expect(result.current.pinnedTab).toBeNull()
  })
})

describe('openSplit pin semantics', () => {
  it('pins the active tab and activates the next neighbor', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b'), tab('c')], 'a')
    act(() => { result.current.openSplit('a', 'horizontal') })
    expect(result.current.pinnedId).toBe('a')
    expect(onActivate).toHaveBeenCalledWith('b')
    // Emulate App: strip flips to b, pin stays on a.
    rerender({ tabs: [tab('a'), tab('b'), tab('c')], activeTab: 'b' })
    expect(result.current.pinnedTab?.id).toBe('a')
  })

  it('activates the previous neighbor when the active tab is last', () => {
    const { result, onActivate } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('b', 'vertical') })
    expect(result.current.pinnedId).toBe('b')
    expect(onActivate).toHaveBeenCalledWith('a')
    expect(result.current.splitDir).toBe('vertical')
  })

  it('pins an inactive tab without touching the active tab', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b'), tab('c')], 'a')
    act(() => { result.current.openSplit('c', 'vertical') })
    expect(result.current.pinnedId).toBe('c')
    expect(onActivate).not.toHaveBeenCalled()
    rerender({ tabs: [tab('a'), tab('b'), tab('c')], activeTab: 'a' })
    expect(result.current.pinnedTab?.id).toBe('c')
  })

  it('falls back to the active tab for unknown ids', () => {
    const { result, onActivate } = setup([tab('a'), tab('b')], 'a')
    act(() => { result.current.openSplit('nope', 'horizontal') })
    expect(result.current.pinnedId).toBe('a')
    expect(onActivate).toHaveBeenCalledWith('b')
  })

  it('falls back to the active tab for empty ids', () => {
    const { result, onActivate } = setup([tab('a'), tab('b')], 'a')
    act(() => { result.current.openSplit('', 'horizontal') })
    expect(result.current.pinnedId).toBe('a')
    expect(onActivate).toHaveBeenCalledWith('b')
  })

  it('refuses to split a lone tab', () => {
    const { result, onActivate } = setup([tab('a')], 'a')
    act(() => { result.current.openSplit('a', 'horizontal') })
    expect(result.current.pinnedId).toBeNull()
    expect(result.current.pinnedTab).toBeNull()
    expect(onActivate).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledWith('Open at least 2 tabs to split the view')
  })

  it('refuses to split with no tabs at all', () => {
    const { result, onActivate } = setup([], null)
    act(() => { result.current.openSplit('a', 'horizontal') })
    expect(result.current.pinnedId).toBeNull()
    expect(onActivate).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledWith('Open a tab first')
  })
})

describe('pinnedTab derivation (edge cases)', () => {
  it('hides the split when the active tab is the pinned one', () => {
    const { result, rerender } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    // Pin persists, but the visible split collapses (no duplicate panes).
    rerender({ tabs: [tab('a'), tab('b')], activeTab: 'a' })
    expect(result.current.pinnedId).toBe('a')
    expect(result.current.pinnedTab).toBeNull()
    // Flipping away re-shows the same pin.
    rerender({ tabs: [tab('a'), tab('b')], activeTab: 'b' })
    expect(result.current.pinnedTab?.id).toBe('a')
  })

  it('hides the split when the pinned tab is closed', () => {
    const { result, rerender } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    rerender({ tabs: [tab('b')], activeTab: 'b' })
    expect(result.current.pinnedTab).toBeNull()
  })

  it('keeps the pin while the strip flips through other tabs', () => {
    const all = [tab('a'), tab('b'), tab('c')]
    const { result, rerender } = setup(all, 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    for (const active of ['b', 'c', 'b']) {
      rerender({ tabs: all, activeTab: active })
      expect(result.current.pinnedTab?.id).toBe('a')
    }
  })
})

describe('swapSplit / closeSplit / direction', () => {
  it('swaps pin and active tab', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    act(() => { result.current.swapSplit() })
    expect(onActivate).toHaveBeenCalledWith('a')
    // Emulate App: active a, pin moved to b.
    rerender({ tabs: [tab('a'), tab('b')], activeTab: 'a' })
    expect(result.current.pinnedId).toBe('b')
    expect(result.current.pinnedTab?.id).toBe('b')
  })

  it('swap is a no-op without a visible split', () => {
    const { result, onActivate } = setup([tab('a'), tab('b')], 'a')
    act(() => { result.current.swapSplit() })
    expect(onActivate).not.toHaveBeenCalled()
    expect(result.current.pinnedId).toBeNull()
  })

  it('closeSplit clears the pin and returns to the pinned tab', () => {
    const { result, onActivate } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    expect(result.current.pinnedId).toBe('a')
    act(() => { result.current.closeSplit() })
    expect(onActivate).toHaveBeenCalledWith('a')
    expect(result.current.pinnedId).toBeNull()
    expect(result.current.pinnedTab).toBeNull()
  })

  it('closeSplit returns to the split origin (split from A, close lands on A)', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b')], 'a')
    act(() => { result.current.openSplit('a', 'horizontal') })
    expect(result.current.pinnedId).toBe('a')
    expect(onActivate).toHaveBeenCalledWith('b')
    // Emulate App: strip moved to b, pin stays on a.
    rerender({ tabs: [tab('a'), tab('b')], activeTab: 'b' })
    onActivate.mockClear()
    act(() => { result.current.closeSplit() })
    expect(onActivate).toHaveBeenCalledWith('a')
    expect(result.current.pinnedId).toBeNull()
  })

  it('closeSplit does not re-activate when the pin is already active', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    // User flips the strip onto the pinned tab: split hidden, pin persists.
    rerender({ tabs: [tab('a'), tab('b')], activeTab: 'a' })
    onActivate.mockClear()
    act(() => { result.current.closeSplit() })
    expect(onActivate).not.toHaveBeenCalled()
    expect(result.current.pinnedId).toBeNull()
  })

  it('closeSplit does not re-activate a closed pinned tab', () => {
    const { result, rerender, onActivate } = setup([tab('a'), tab('b')], 'b')
    act(() => { result.current.openSplit('a', 'horizontal') })
    rerender({ tabs: [tab('b')], activeTab: 'b' })
    onActivate.mockClear()
    act(() => { result.current.closeSplit() })
    expect(onActivate).not.toHaveBeenCalled()
    expect(result.current.pinnedId).toBeNull()
  })

  it('direction can be toggled', () => {
    const { result } = setup([tab('a'), tab('b')], 'a')
    act(() => { result.current.setSplitDir('vertical') })
    expect(result.current.splitDir).toBe('vertical')
  })
})
