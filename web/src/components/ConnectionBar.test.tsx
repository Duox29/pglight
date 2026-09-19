import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderUi } from '../test/utils'
import { ConnectionBar } from './ConnectionBar'

function props(over: Partial<React.ComponentProps<typeof ConnectionBar>> = {}) {
  return {
    connected: true,
    onSearch: vi.fn(),
    onPanel: vi.fn(),
    onDocs: vi.fn(),
    onShutdown: vi.fn(),
    ...over,
  }
}

describe('ConnectionBar', () => {
  it('routes header actions to their callbacks', async () => {
    const user = userEvent.setup()
    const p = props()
    renderUi(<ConnectionBar {...p} />)
    await user.click(screen.getByRole('button', { name: 'Shutdown' }))
    expect(p.onShutdown).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole('button', { name: /Search objects/ }))
    expect(p.onSearch).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole('button', { name: 'History' }))
    expect(p.onPanel).toHaveBeenCalledWith('history')
    await user.click(screen.getByRole('button', { name: 'Dashboard' }))
    expect(p.onPanel).toHaveBeenCalledWith('server')
  })

  // NOTE: the overflow DropdownMenu itself is Radix behavior (covered for
  // menus in TabStripMenu.test.tsx); what matters here is the wiring.
})
