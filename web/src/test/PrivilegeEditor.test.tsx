import { describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderUi } from './utils'
import { PrivilegeEditor } from '../components/PrivilegeEditor'
import type { DialogsApi } from '../components/dialogs'

describe('PrivilegeEditor', () => {
  it('previews staged GRANT SQL before applying it', async () => {
    const user = userEvent.setup()
    const confirm = vi.fn(async () => true)
    const sent: { action?: string; changes?: unknown[] }[] = []
    vi.mocked(globalThis.fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.startsWith('/api/privileges?')) return new Response(JSON.stringify({ roles: [{ name: 'reader', superuser: false, create_role: false, create_db: false, login: true }], grants: [], rls: { enabled: false, forced: false, in_txn: false, policies: [] } }), { status: 200 })
      if (url === '/api/privileges' && init?.method === 'POST') {
        const body = JSON.parse(String(init.body)) as { action?: string; changes?: unknown[] }
        sent.push(body)
        if (body.action === 'preview_grants') return new Response(JSON.stringify({ sql: ['GRANT SELECT ON TABLE "public"."items" TO "reader"'] }), { status: 200 })
        return new Response(JSON.stringify({ ok: true, in_txn: false }), { status: 200 })
      }
      return new Response('{}', { status: 200 })
    })
    const dialogs = { confirm, prompt: vi.fn(), promptNullable: vi.fn(), form: vi.fn() } as unknown as DialogsApi
    renderUi(<PrivilegeEditor session="s1" dialogs={dialogs} />)
    await user.type(screen.getByRole('textbox', { name: 'Privilege table' }), 'items')
    await user.click(screen.getByRole('button', { name: 'Load object' }))
    await screen.findByRole('switch', { name: 'SELECT grant for reader' })
    await user.click(screen.getByRole('switch', { name: 'SELECT grant for reader' }))
    await user.click(screen.getByRole('button', { name: 'Preview SQL' }))
    expect(await screen.findByText(/GRANT SELECT ON TABLE/)).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Apply previewed SQL' }))
    await waitFor(() => expect(sent.map((x) => x.action)).toEqual(['preview_grants', 'apply_grants']))
    expect(confirm).toHaveBeenCalledTimes(1)
  })
})
