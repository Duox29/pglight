import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { renderUi } from '../test/utils'
import { TabStrip } from './TabStrip'
import type { SessionInfo, Tab } from '../types'

// NOTE: jsdom + Radix ContextMenu only opens the first menu per test file, so
// this file holds exactly one menu-opening test (see TabStrip.test.tsx).
describe('TabStrip context menu (single tab)', () => {
  it('disables split and close-others items with a single tab', async () => {
    const tab: Tab = { id: 'a', kind: 'query', title: 'Alpha', sessionId: 's1', sql: 'SELECT 1', limit: 200, results: null }
    const sessions: SessionInfo[] = [
      { id: 's1', host: 'h', port: '5432', user: 'u', dbname: 'testdb', sslmode: 'prefer' },
    ]
    const noop = vi.fn()
    renderUi(
      <TabStrip
        tabs={[tab]}
        activeTab="a"
        sessions={sessions}
        splitHint={false}
        pinnedId={null}
        onActivate={noop}
        onClose={noop}
        onCloseOthers={noop}
        onCloseRight={noop}
        onCloseLeft={noop}
        onCloseAll={noop}
        onNewQuery={noop}
        onSplit={noop}
        onUnpin={noop}
      />,
    )
    fireEvent.contextMenu(screen.getByRole('tab', { name: /Alpha/ }))
    expect(await screen.findByRole('menuitem', { name: 'Split Right' })).toHaveAttribute('data-disabled')
    expect(await screen.findByRole('menuitem', { name: 'Split Down' })).toHaveAttribute('data-disabled')
    expect(await screen.findByRole('menuitem', { name: 'Close Others' })).toHaveAttribute('data-disabled')
    // Plain Close stays enabled.
    expect(await screen.findByRole('menuitem', { name: 'Close' })).not.toHaveAttribute('data-disabled')
  })
})
