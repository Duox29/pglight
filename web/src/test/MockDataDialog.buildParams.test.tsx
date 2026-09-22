import { describe, expect, it, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderUi } from './utils'
import { MockDataDialog, buildParams } from '../components/MockDataDialog'

const meta = {
  columns: [
    { name: 'price', data_type: 'numeric', udt: 'numeric', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: false, enum_values: [], semantic_hint: '' },
    { name: 'day', data_type: 'date', udt: 'date', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: false, enum_values: [], semantic_hint: '' },
    { name: 'n', data_type: 'integer', udt: 'int4', nullable: false, default: null, identity: false, generated: false, primary_key: false, unique: false, enum_values: [], semantic_hint: '' },
  ],
  foreign_keys: [],
  checks: [],
}

function mockFetch() {
  return vi.fn((url: string) => {
    const body = url.includes('/api/mock-data/meta')
      ? meta
      : { columns: ['price'], rows: [], warnings: [], seed: 1 }
    return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) })
  })
}

function props(over: Partial<React.ComponentProps<typeof MockDataDialog>> = {}) {
  return {
    open: true,
    onOpenChange: vi.fn(),
    sessionId: 's1',
    schema: 'public',
    table: 't',
    onGenerated: vi.fn(),
    ...over,
  }
}

describe('buildParams generator-aware', () => {
  it('keeps decimal floats (min=1.25 max=9.75)', () => {
    const { params, errors } = buildParams('decimal', { min: '1.25', max: '9.75', scale: '2' })
    expect(errors).toEqual([])
    expect(params).toEqual({ min: 1.25, max: 9.75, scale: 2 })
  })

  it('keeps date strings verbatim (no Number coercion)', () => {
    const { params, errors } = buildParams('datetime', { min: '2025-01-01', max: '2025-12-31' })
    expect(errors).toEqual([])
    expect(params).toEqual({ min: '2025-01-01', max: '2025-12-31' })
  })

  it('rejects invalid integer input instead of silently dropping it', () => {
    const { params, errors } = buildParams('integer', { min: '1.9', max: 'abc' })
    expect(params).toEqual({})
    expect(errors).toHaveLength(2)
  })
})

describe('MockDataDialog param regressions', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', mockFetch())
  })

  it('composes exact decimal and date bodies for a preview request', () => {
    // Mirrors buildFieldConfigs: one buildParams call per column, keyed by
    // that column's generator. Decimal 1.25–9.75 must survive as floats and
    // date bounds as strings (the old global pass dropped both).
    const cols: { column: string; generator: string; params: Record<string, string> }[] = [
      { column: 'price', generator: 'decimal', params: { min: '1.25', max: '9.75', scale: '2' } },
      { column: 'day', generator: 'date', params: { min: '2025-01-01', max: '2025-12-31' } },
      { column: 'n', generator: 'integer', params: { min: '0', max: '10' } },
    ]
    const fields = cols.map((c) => {
      const { params, errors } = buildParams(c.generator, c.params)
      expect(errors).toEqual([])
      return { column: c.column, generator: c.generator, params }
    })
    expect(fields).toEqual([
      { column: 'price', generator: 'decimal', params: { min: 1.25, max: 9.75, scale: 2 } },
      { column: 'day', generator: 'date', params: { min: '2025-01-01', max: '2025-12-31' } },
      { column: 'n', generator: 'integer', params: { min: 0, max: 10 } },
    ])
  })

  it('preview rejects an invalid seed instead of silently using a random one', async () => {
    const user = userEvent.setup()
    renderUi(<MockDataDialog {...props()} />)
    await waitFor(() => expect(screen.getByText(/Generate 1,000 Rows/)).toBeEnabled())
    // Same reject-instead-of-drop contract as invalid integer params (which
    // are covered at unit level above and surface a toast via
    // buildFieldConfigs in the dialog).
    const fetchMock = vi.mocked(fetch)
    const before = fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/mock-data/preview')).length
    await user.clear(screen.getByLabelText(/Seed/))
    await user.type(screen.getByLabelText(/Seed/), '12.9')
    await user.click(screen.getByRole('button', { name: /Preview/ }))
    const after = fetchMock.mock.calls.filter(([url]) => String(url).includes('/api/mock-data/preview')).length
    expect(after).toBe(before)
  })
})
