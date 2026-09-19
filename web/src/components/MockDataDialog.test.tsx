import { describe, expect, it, vi, beforeEach } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderUi } from '../test/utils'
import { MockDataDialog } from './MockDataDialog'

const meta = {
  columns: [
    { name: 'id', data_type: 'bigint', udt: 'int8', nullable: false, default: "nextval('t'::regclass)", identity: false, generated: false, primary_key: true, unique: true, enum_values: [], semantic_hint: '' },
    { name: 'email', data_type: 'text', udt: 'text', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: true, enum_values: [], semantic_hint: 'email' },
    { name: 'age', data_type: 'integer', udt: 'int4', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: false, enum_values: [], semantic_hint: '' },
    { name: 'company_id', data_type: 'bigint', udt: 'int8', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: false, enum_values: [], semantic_hint: '' },
  ],
  foreign_keys: [
    { name: 'fk_company', column: 'company_id', ref_schema: 'public', ref_table: 'companies', ref_column: 'id' },
  ],
  checks: [],
}

function mockFetch() {
  return vi.fn((url: string, init?: RequestInit) => {
    let body: unknown = {}
    if (url.includes('/api/mock-data/meta')) body = meta
    else if (url.includes('/api/mock-data/preview')) {
      const req = JSON.parse(String(init?.body ?? '{}')) as { mode?: string }
      body = {
        columns: ['id', 'email', 'age'],
        rows: [['<database default>', 'a@x.test', 42]],
        warnings: req.mode === 'advanced' ? ['demo warning'] : [],
        seed: 7,
      }
    } else if (url.includes('/api/mock-data/generate')) body = { generated: 5, inserted: 5, seed: 7, duration_ms: 3, in_txn: false }
    return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) })
  })
}

function props(over: Partial<React.ComponentProps<typeof MockDataDialog>> = {}) {
  return {
    open: true,
    onOpenChange: vi.fn(),
    sessionId: 's1',
    schema: 'public',
    table: 'users',
    onGenerated: vi.fn(),
    ...over,
  }
}

describe('MockDataDialog', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', mockFetch())
  })

  it('loads metadata and previews in simple mode', async () => {
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    await waitFor(() => expect(screen.getByText('a@x.test')).toBeInTheDocument())
    expect(screen.getByText('<database default>')).toBeInTheDocument()
  })

  it('advanced mode shows per-column generators and generates', async () => {
    const user = userEvent.setup()
    const p = props()
    renderUi(<MockDataDialog {...p} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Advanced' }))
    expect(screen.getByText('email')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Generate 1,000 Rows/ }))
    await waitFor(() => expect(p.onGenerated).toHaveBeenCalled())
    expect(p.onOpenChange).toHaveBeenCalledWith(false)
  })

    it('marks FK columns and offers a searchable value source', async () => {

    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Advanced' }))
    // fk badge next to the FK column, like the pk badge.
    expect(screen.getByText('fk')).toBeInTheDocument()
    // Auto on an FK column resolves to Foreign Key → source picker appears
    // with the referenced table pre-selected.
    expect(screen.getByText('Source')).toBeInTheDocument()
    expect(screen.getByText('public.companies.id')).toBeInTheDocument()
    // Search filters the source list.
    await user.click(screen.getByRole('button', { name: 'FK value source' }))
    const filter = screen.getByPlaceholderText('Type to filter…')
    await user.type(filter, 'zzz-no-match')
    expect(screen.getByText('No matches')).toBeInTheDocument()
    await user.clear(filter)
    await user.type(filter, 'companies')
    // Trigger label + matching option both show the source.
    expect(screen.getAllByText('public.companies.id').length).toBeGreaterThanOrEqual(2)
  })

  it('renders all headers of a wide preview and keeps actions visible', async () => {    const cols = Array.from({ length: 12 }, (_, i) => `col_${i + 1}`)
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const body =
          url.includes('/api/mock-data/meta')
            ? meta
            : { columns: cols, rows: [cols.map((c) => `v_${c}`)], warnings: [], seed: 7 }
        return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) })
      }),
    )
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    for (const c of cols) {
      await waitFor(() => expect(screen.getByText(c)).toBeInTheDocument())
    }
    // Toolbar actions stay in the document (not pushed out by wide content).
    expect(screen.getByRole('button', { name: /Generate 1,000 Rows/ })).toBeVisible()
    expect(screen.getByRole('button', { name: /Preview/ })).toBeVisible()
  })

  it.each([
    { records: '0', seed: '' },
    { records: '1000', seed: 'abc' },
    { records: '20001', seed: '' },
  ])('refuses to send invalid input (records=$records seed=$seed)', async ({ records, seed }) => {
    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    const fetchMock = vi.mocked(fetch)
    const postsBefore = fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/mock-data/preview') || String(url).includes('/api/mock-data/generate')).length
    await user.clear(screen.getByLabelText(/Records/))
    await user.type(screen.getByLabelText(/Records/), records)
    await user.clear(screen.getByLabelText(/Seed/))
    if (seed) await user.type(screen.getByLabelText(/Seed/), seed)
    await user.click(screen.getByRole('button', { name: /Generate/ }))
    const postsAfter = fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/mock-data/preview') || String(url).includes('/api/mock-data/generate')).length
    expect(postsAfter).toBe(postsBefore)
  })

  it('sends the advanced field configs and clamps preview count', async () => {
    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Advanced' }))
    await user.click(screen.getByRole('button', { name: /Preview/ }))
    const fetchMock = vi.mocked(fetch)
    const previewCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/mock-data/preview'))
    expect(previewCall).toBeDefined()
    const req = JSON.parse(String(previewCall?.[1]?.body ?? '{}')) as {
      mode?: string
      count?: number
      seed?: number
      fields?: { column?: string; generator?: string; unique?: boolean; params?: Record<string, unknown> }[]
      constraints?: unknown
    }
    expect(req.mode).toBe('advanced')
    // Preview never asks for more than 20 rows even with Records=1000.
    expect(req.count).toBe(20)
    expect(req.seed).toBeUndefined()
    expect(req.constraints).toBeUndefined()
    const byCol = new Map((req.fields ?? []).map((f) => [f.column, f]))
    expect(byCol.get('id')?.generator).toBe('db_default')
    expect(byCol.get('email')).toMatchObject({ generator: 'auto', unique: true })
    expect(byCol.get('age')?.generator).toBe('auto')
    // FK column resolves through Auto; the picked source travels as params.
    expect(byCol.get('company_id')?.generator).toBe('auto')
  })

  it('sends source params after picking an FK source', async () => {
    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Advanced' }))
    // Re-pick the (single) source so ref_* params are attached.
    // Trigger label and popover option share text; the option lives last.
    await user.click(screen.getByRole('button', { name: 'FK value source' }))
    const matches = screen.getAllByText('public.companies.id')
    const optionBtn = matches[matches.length - 1].closest('button')
    if (!optionBtn) throw new Error('source option button missing')
    await user.click(optionBtn)
    await user.click(screen.getByRole('button', { name: /Preview/ }))
    const fetchMock = vi.mocked(fetch)
    const previewCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/mock-data/preview'))
    const req = JSON.parse(String(previewCall?.[1]?.body ?? '{}')) as {
      fields?: { column?: string; params?: Record<string, unknown> }[]
    }
    const company = (req.fields ?? []).find((f) => f.column === 'company_id')
    expect(company?.params).toMatchObject({ ref_schema: 'public', ref_table: 'companies', ref_column: 'id' })
  })

  it('keeps the dialog open when generation fails', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const body = url.includes('/api/mock-data/meta') ? meta : { error: 'PG blew up' }
        return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) })
      }),
    )
    const p = props()
    renderUi(<MockDataDialog {...p} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: /Generate 1,000 Rows/ }))
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('/api/mock-data/generate'))).toBe(true))
    expect(p.onGenerated).not.toHaveBeenCalled()
    expect(p.onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByRole('button', { name: /Generate 1,000 Rows/ })).toBeInTheDocument()
  })

  it('sends a bare simple request without fields', async () => {
    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    await user.click(screen.getByRole('button', { name: /Preview/ }))
    const previewCall = vi.mocked(fetch).mock.calls.find(([url]) => String(url).includes('/api/mock-data/preview'))
    const req = JSON.parse(String(previewCall?.[1]?.body ?? '{}')) as Record<string, unknown>
    expect(req.mode).toBe('simple')
    expect(req.fields).toBeUndefined()
    expect(req.constraints).toBeUndefined()
  })
})
