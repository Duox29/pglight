import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from '../test/utils'
import { TabStrip } from './TabStrip'
import type { SessionInfo, Tab } from '../types'

const tab = (id: string, title: string): Tab => ({
  id, kind: 'query', title, sessionId: 's1', sql: 'SELECT 1', limit: 200, results: null,
})
const sessions: SessionInfo[] = [
  { id: 's1', host: 'h', port: '5432', user: 'u', dbname: 'testdb', sslmode: 'prefer' },
]

function props(over: Partial<React.ComponentProps<typeof TabStrip>> = {}) {
  return {
    tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')],
    activeTab: 'a',
    sessions,
    splitHint: true,
    pinnedId: null,
    onActivate: vi.fn(),
    onClose: vi.fn(),
    onCloseOthers: vi.fn(),
    onCloseRight: vi.fn(),
    onCloseLeft: vi.fn(),
    onCloseAll: vi.fn(),
    onNewQuery: vi.fn(),
    onSplit: vi.fn(),
    onUnpin: vi.fn(),
    ...over,
  }
}

describe('TabStrip tabs', () => {
  it('renders titles with db badges', () => {
    renderUi(<TabStrip {...props()} />)
    expect(screen.getByRole('tab', { name: /Alpha/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Beta/ })).toBeInTheDocument()
    expect(screen.getAllByText('testdb').length).toBeGreaterThan(0)
  })

  it('clicking a tab activates it', () => {
    const p = props()
    renderUi(<TabStrip {...p} />)
    fireEvent.click(screen.getByRole('tab', { name: /Beta/ }))
    expect(p.onActivate).toHaveBeenCalledWith('b')
    expect(p.onClose).not.toHaveBeenCalled()
  })

  it('clicking X closes without activating', () => {
    const p = props()
    const { container } = renderUi(<TabStrip {...p} />)
    const tabEl = screen.getByRole('tab', { name: /Beta/ })
    const x = tabEl.querySelector('svg')
    expect(x).not.toBeNull()
    fireEvent.click(x as SVGElement)
    expect(p.onClose).toHaveBeenCalledWith('b')
    expect(p.onActivate).not.toHaveBeenCalled()
    expect(container).toBeDefined()
  })
})

describe('TabStrip fixed action zone (regression: buttons must not scroll away)', () => {
  it('keeps +Query outside the scrollable tablist', () => {
    renderUi(<TabStrip {...props()} />)
    const tablist = screen.getByRole('tablist')
    const queryBtn = screen.getByRole('button', { name: 'Query' })
    expect(tablist.contains(queryBtn)).toBe(false)
  })

  it('keeps split buttons outside the scrollable tablist', () => {
    renderUi(<TabStrip {...props()} />)
    const tablist = screen.getByRole('tablist')
    const right = screen.getByRole('button', { name: 'Split right' })
    const down = screen.getByRole('button', { name: 'Split down' })
    expect(tablist.contains(right)).toBe(false)
    expect(tablist.contains(down)).toBe(false)
  })

  it('split buttons pin the active tab', () => {
    const p = props()
    renderUi(<TabStrip {...p} />)
    fireEvent.click(screen.getByRole('button', { name: 'Split right' }))
    expect(p.onSplit).toHaveBeenCalledWith('a', 'horizontal')
    fireEvent.click(screen.getByRole('button', { name: 'Split down' }))
    expect(p.onSplit).toHaveBeenCalledWith('a', 'vertical')
  })

  it('hides split buttons when splitHint is false', () => {
    renderUi(<TabStrip {...props({ splitHint: false })} />)
    expect(screen.queryByRole('button', { name: 'Split right' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Split down' })).toBeNull()
    // +Query stays — it is always reachable.
    expect(screen.getByRole('button', { name: 'Query' })).toBeInTheDocument()
  })
})

describe('TabStrip pin badge', () => {
  it('shows no badge without a pin', () => {
    const { container } = renderUi(<TabStrip {...props()} />)
    expect(container.querySelector('.lucide-pin')).toBeNull()
  })

  it('badges the pinned tab and unpins on click', () => {
    const p = props({ pinnedId: 'b' })
    const { container } = renderUi(<TabStrip {...p} />)
    const pin = container.querySelector('.lucide-pin')
    expect(pin).not.toBeNull()
    fireEvent.click(pin as Element)
    expect(p.onUnpin).toHaveBeenCalledTimes(1)
    expect(p.onActivate).not.toHaveBeenCalled()
  })
})

describe('TabStrip context menu', () => {
  // NOTE: jsdom + Radix ContextMenu only opens the first menu per file, so
  // menu-opening tests live one-per-file (see TabStripMenu.test.tsx).
  it('Split Down pins that tab', async () => {
    const p = props()
    renderUi(<TabStrip {...p} />)
    fireEvent.contextMenu(screen.getByRole('tab', { name: /Gamma/ }))
    const item = await screen.findByRole('menuitem', { name: 'Split Down' })
    fireEvent.click(item)
    expect(p.onSplit).toHaveBeenCalledWith('c', 'vertical')
  })
})
