import { describe, expect, it } from 'vitest'
import { createErdSvg, relatedTableIds } from '../components/erd/erdExport'
import type { ErdRelation, ErdTable } from '../components/erd/erdMapper'

const tables: ErdTable[] = ['users', 'orders', 'items', 'audit'].map((name) => ({
  id: name, name, schema: 'public', ports: {},
  columns: [{ name: 'id', type: 'integer', pk: true, fk: false }],
}))
const relations: ErdRelation[] = [
  { id: 'orders.users', fk: 'fk_users', sourceTable: 'orders', sourceColumn: 'user_id', targetSchema: 'public', targetTable: 'users', targetColumn: 'id', labeled: false },
  { id: 'items.orders', fk: 'fk_orders', sourceTable: 'items', sourceColumn: 'order_id', targetSchema: 'public', targetTable: 'orders', targetColumn: 'id', labeled: false },
]

describe('ERD export model', () => {
  it('finds related tables by undirected depth and includes the root', () => {
    expect([...relatedTableIds('users', relations, 1)].sort()).toEqual(['orders', 'users'])
    expect([...relatedTableIds('users', relations, 2)].sort()).toEqual(['items', 'orders', 'users'])
    expect([...relatedTableIds('users', relations)].sort()).toEqual(['items', 'orders', 'users'])
  })

  it('exports an SVG with the full viewBox, visible relationship, and escaped labels', () => {
    const svg = createErdSvg(tables, relations, { users: { x: 20, y: 30 }, orders: { x: 400, y: 30 }, items: { x: 800, y: 30 }, audit: { x: 1200, y: 30 } }, { title: 'A < B', only: new Set(['users', 'orders']) })
    expect(svg).toContain('viewBox="0 0 692 125"')
    expect(svg).toContain('A &lt; B')
    expect(svg).toContain('user_id → id')
    expect(svg).not.toContain('audit')
    expect(svg).toContain('<svg')
  })

  it('supports compact exports and PK/FK-only column output', () => {
    const columns = [...tables[0].columns, { name: 'email', type: 'text', pk: false, fk: false }]
    const expanded = [{ ...tables[0], columns }, ...tables.slice(1)]
    const positions = Object.fromEntries(expanded.map((table, index) => [table.id, { x: index * 300, y: 0 }]))
    const keysSvg = createErdSvg(expanded, relations, positions, { keysOnly: true })
    expect(keysSvg).toContain('PK · id')
    expect(keysSvg).not.toContain('email')
    const compactSvg = createErdSvg(expanded, relations, positions, { hideColumns: true })
    expect(compactSvg).toContain('columns hidden')
    expect(compactSvg).not.toContain('PK · id')
  })
})
