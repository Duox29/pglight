import { describe, expect, it } from 'vitest'
import { erdCanvasKey } from '../components/ErdView'

describe('ERD canvas identity', () => {
  it('separates identical schemas from different sessions', () => {
    const data = { nodes: ['users'], edges: [] }
    const first = erdCanvasKey({ sessionId: 's1', schema: 'public' }, data)
    const second = erdCanvasKey({ sessionId: 's2', schema: 'public' }, data)

    expect(first).not.toBe(second)
  })
})
