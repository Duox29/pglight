import { describe, expect, it } from 'vitest'
import { erdConnectionIdFor, isWorkspaceView, pkOf, slimTab, splitOptions } from '../lib/tabs'
import type { SavedConnection, SessionInfo, Tab, TableTabT } from '../types'

const q = (id: string): Tab => ({
  id, kind: 'query', title: id, sessionId: 's1', sql: 'SELECT 1', limit: 200, results: null,
})

describe('splitOptions', () => {
  const tabs = [q('a'), q('b'), q('c')]

  it('excludes the pinned tab and keeps order', () => {
    expect(splitOptions(tabs, 'b').map((t) => t.id)).toEqual(['a', 'c'])
  })

  it('returns everything without a pin', () => {
    expect(splitOptions(tabs).map((t) => t.id)).toEqual(['a', 'b', 'c'])
    expect(splitOptions(tabs, undefined).map((t) => t.id)).toEqual(['a', 'b', 'c'])
  })

  it('returns everything for an unknown pin (edge)', () => {
    expect(splitOptions(tabs, 'zzz').map((t) => t.id)).toEqual(['a', 'b', 'c'])
  })

  it('handles empty tabs', () => {
    expect(splitOptions([], 'a')).toEqual([])
  })
})

describe('workspace tabs', () => {
  it('accepts workspace views and persists the selected view', () => {
    const workspace: Tab = { id: 'workspace', kind: 'workspace', title: 'Workspace', view: 'quick-access' }
    expect(isWorkspaceView('quick-access')).toBe(true)
    expect(isWorkspaceView('not-a-view')).toBe(false)
    expect(slimTab(workspace)).toEqual({ id: 'workspace', kind: 'workspace', title: 'Workspace', view: 'quick-access' })
  })
})

describe('erdConnectionIdFor', () => {
  const sessions: SessionInfo[] = [
    { id: 's1', host: 'h', port: '5432', user: 'u', dbname: 'db', sslmode: 'prefer' },
  ]
  const saved: SavedConnection[] = [
    { id: 'c1', name: 'n', host: 'h', port: '5432', user: 'u', password: '', dbname: 'db', sslmode: 'prefer' },
    { id: 'c2', name: 'other', host: 'x', port: '5432', user: 'u', password: '', dbname: 'db', sslmode: 'prefer' },
  ]
  const erd: Tab = { id: 'e', kind: 'erd', title: 'ERD', sessionId: 's1', schema: 'public', data: null }

  it('resolves the saved connection backing the ERD session', () => {
    expect(erdConnectionIdFor(sessions, saved, erd)).toBe('c1')
  })

  it('returns undefined for non-ERD tabs and null', () => {
    expect(erdConnectionIdFor(sessions, saved, q('a'))).toBeUndefined()
    expect(erdConnectionIdFor(sessions, saved, null)).toBeUndefined()
  })

  it('returns undefined with no matching session or connection', () => {
    expect(erdConnectionIdFor([], saved, erd)).toBeUndefined()
    expect(erdConnectionIdFor(sessions, [], erd)).toBeUndefined()
  })
})

describe('pkOf', () => {
  const table = (cols: Record<string, unknown>[]): TableTabT => ({
    id: 't', kind: 'table', title: 't', sessionId: 's1', schema: 'public', table: 't',
    subtab: 'data', limit: 100, offset: 0, filter: '', order: '',
    result: null, cols, ddl: null, constraints: null, triggers: null, stats: null,
  })

  it('collects pk columns (t/true markers)', () => {
    const t = table([
      { name: 'id', pk: 't' },
      { name: 'code', pk: true },
      { name: 'name', pk: 'f' },
      { name: 'note' },
    ])
    expect(pkOf(t)).toEqual(['id', 'code'])
  })

  it('returns [] without pks', () => {
    expect(pkOf(table([{ name: 'a' }]))).toEqual([])
    expect(pkOf(table([]))).toEqual([])
  })
})
