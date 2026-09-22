import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from './utils'
import { SplitWorkspace } from '../components/SplitWorkspace'
import type { Tab } from '../types'

const tab = (id: string, title: string): Tab => ({
  id, kind: 'query', title, sessionId: 's1', sql: 'SELECT 1', limit: 200, results: null,
})
const A = tab('a', 'Alpha')
const B = tab('b', 'Beta')
const C = tab('c', 'Gamma')

function props(over: Partial<React.ComponentProps<typeof SplitWorkspace>> = {}) {
  return {
    primary: A,
    secondary: null,
    dir: 'horizontal' as const,
    tabs: [A, B, C],
    onDir: vi.fn(),
    onSecondary: vi.fn(),
    onSwap: vi.fn(),
    onCloseSplit: vi.fn(),
    renderPane: vi.fn((t: Tab | null) => <span>{t ? `pane:${t.title}` : 'pane:empty'}</span>),
    ...over,
  }
}

describe('SplitWorkspace single pane', () => {
  it('renders only the primary tab', () => {
    const p = props()
    renderUi(<SplitWorkspace {...p} />)
    expect(p.renderPane).toHaveBeenCalledTimes(1)
    expect(p.renderPane).toHaveBeenCalledWith(A)
    expect(screen.getByText('pane:Alpha')).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).toBeNull()
  })

  it('renders the empty state for null primary', () => {
    const p = props({ primary: null })
    renderUi(<SplitWorkspace {...p} />)
    expect(p.renderPane).toHaveBeenCalledWith(null)
    expect(screen.getByText('pane:empty')).toBeInTheDocument()
  })
})

describe('SplitWorkspace split panes', () => {
  it('renders both panes through renderPane', () => {
    const p = props({ secondary: B })
    renderUi(<SplitWorkspace {...p} />)
    expect(p.renderPane).toHaveBeenCalledWith(A)
    expect(p.renderPane).toHaveBeenCalledWith(B)
    expect(screen.getByText('pane:Alpha')).toBeInTheDocument()
    expect(screen.getByText('pane:Beta')).toBeInTheDocument()
  })

  it('picker trigger shows the right-pane tab', () => {
    renderUi(<SplitWorkspace {...props({ secondary: B })} />)
    expect(screen.getByRole('combobox')).toHaveTextContent('Beta')
  })

  it('picker lists every tab (filtering is unit-tested via splitOptions)', () => {
    // Radix Select content does not open under jsdom (no pointer capture),
    // so the exclusion logic lives in splitOptions() with its own tests.
    renderUi(<SplitWorkspace {...props({ secondary: B })} />)
    expect(screen.getByRole('combobox')).toBeInTheDocument()
  })

  it('direction buttons switch layout', () => {
    const p = props({ secondary: B })
    renderUi(<SplitWorkspace {...p} />)
    fireEvent.click(screen.getByRole('button', { name: 'Split stacked' }))
    expect(p.onDir).toHaveBeenCalledWith('vertical')
    fireEvent.click(screen.getByRole('button', { name: 'Split side by side' }))
    expect(p.onDir).toHaveBeenCalledWith('horizontal')
  })

  it('swap and close buttons fire', () => {
    const p = props({ secondary: B })
    renderUi(<SplitWorkspace {...p} />)
    fireEvent.click(screen.getByRole('button', { name: 'Swap panes' }))
    expect(p.onSwap).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Close split' }))
    expect(p.onCloseSplit).toHaveBeenCalledTimes(1)
  })
})
