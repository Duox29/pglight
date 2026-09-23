import { ERD_NODE_WIDTH, type ErdPos } from './erdLayout'
import type { ErdRelation, ErdTable } from './erdMapper'

const PAD = 24
const HEADER = 57
const ROW = 20

export function relatedTableIds(root: string, relations: ErdRelation[], depth?: number): Set<string> {
  const found = new Set([root])
  const neighbors = new Map<string, string[]>()
  const link = (from: string, to: string) => {
    const items = neighbors.get(from) ?? []
    items.push(to)
    neighbors.set(from, items)
  }
  for (const relation of relations) {
    link(relation.sourceTable, relation.targetTable)
    link(relation.targetTable, relation.sourceTable)
  }
  let frontier = [root]
  let level = 0
  while (frontier.length && (depth === undefined || level < depth)) {
    const next: string[] = []
    for (const id of frontier) {
      for (const other of neighbors.get(id) ?? []) {
        if (!found.has(other)) { found.add(other); next.push(other) }
      }
    }
    frontier = next
    level++
  }
  return found
}

function esc(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&apos;')
}

export function createErdSvg(
  tables: ErdTable[],
  relations: ErdRelation[],
  positions: Record<string, ErdPos>,
  options: { title?: string; only?: Set<string>; hideColumns?: boolean; keysOnly?: boolean; dark?: boolean } = {},
): string {
  const visible = tables.filter((table) => !options.only || options.only.has(table.id))
  if (!visible.length) return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1" />'
  const coords = visible.map((table) => positions[table.id] ?? { x: 0, y: 0 })
  const minX = Math.min(...coords.map((p) => p.x))
  const minY = Math.min(...coords.map((p) => p.y))
  const maxX = Math.max(...visible.map((table) => (positions[table.id]?.x ?? 0) + ERD_NODE_WIDTH))
  const maxY = Math.max(...visible.map((table) => {
    const filteredCount = table.columns.filter((column) => !options.keysOnly || column.pk || column.fk).length
    const nodeHeight = options.hideColumns ? HEADER + ROW : HEADER + Math.max(1, filteredCount) * ROW + (filteredCount > 30 ? 20 : 0)
    return (positions[table.id]?.y ?? 0) + nodeHeight
  }))
  const width = Math.ceil(maxX - minX + PAD * 2)
  const height = Math.ceil(maxY - minY + PAD * 2)
  const x = (id: string) => (positions[id]?.x ?? 0) - minX + PAD
  const y = (id: string) => (positions[id]?.y ?? 0) - minY + PAD
  const ink = options.dark ? '#f4f4f5' : '#18181b'
  const muted = options.dark ? '#a1a1aa' : '#71717a'
  const panel = options.dark ? '#18181b' : '#ffffff'
  const border = options.dark ? '#52525b' : '#d4d4d8'
  const edgeMarkup = relations.filter((relation) => visible.some((table) => table.id === relation.sourceTable) && visible.some((table) => table.id === relation.targetTable)).map((relation) => {
    const sx = x(relation.sourceTable) + ERD_NODE_WIDTH
    const sy = y(relation.sourceTable) + HEADER
    const tx = x(relation.targetTable)
    const ty = y(relation.targetTable) + HEADER
    const mx = (sx + tx) / 2
    return `<path d="M ${sx} ${sy} C ${mx} ${sy}, ${mx} ${ty}, ${tx} ${ty}" fill="none" stroke="${ink}" stroke-width="1.5"/><text x="${mx}" y="${(sy + ty) / 2 - 4}" text-anchor="middle" font-family="system-ui,sans-serif" font-size="10" fill="${muted}">${esc(relation.sourceColumn)} → ${esc(relation.targetColumn)}</text>`
  }).join('')
  const nodeMarkup = visible.map((table) => {
    const px = x(table.id), py = y(table.id)
    const columns = options.hideColumns ? '' : table.columns.filter((column) => !options.keysOnly || column.pk || column.fk).map((column, index) => {
      const cy = py + HEADER + 18 + index * ROW
      const key = column.pk ? 'PK · ' : column.fk ? 'FK · ' : ''
      return `<text x="${px + 10}" y="${cy}" font-family="system-ui,sans-serif" font-size="12" fill="${column.pk || column.fk ? ink : muted}">${esc(key + column.name)} <tspan fill="${muted}">${esc(column.type ?? '')}</tspan></text>`
    }).join('')
    const nodeHeight = options.hideColumns ? HEADER + ROW : HEADER + Math.max(1, table.columns.filter((column) => !options.keysOnly || column.pk || column.fk).length) * ROW
    const hiddenSummary = options.hideColumns ? `<text x="${px + 10}" y="${py + HEADER + 15}" font-family="system-ui,sans-serif" font-size="11" fill="${muted}">${table.columns.length} columns hidden</text>` : ''
    return `<g><rect x="${px}" y="${py}" width="${ERD_NODE_WIDTH}" height="${nodeHeight}" rx="8" fill="${panel}" stroke="${border}"/><text x="${px + 10}" y="${py + 19}" font-family="system-ui,sans-serif" font-size="13" font-weight="700" fill="${ink}">${esc(table.name)}</text><text x="${px + 10}" y="${py + 34}" font-family="system-ui,sans-serif" font-size="10" fill="${muted}">${esc(table.schema)}</text><path d="M ${px} ${py + HEADER} h ${ERD_NODE_WIDTH}" stroke="${border}"/>${columns}${hiddenSummary}</g>`
  }).join('')
  const title = options.title ? `<title>${esc(options.title)}</title>` : ''
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${title}<rect width="100%" height="100%" fill="${panel}"/>${edgeMarkup}${nodeMarkup}</svg>`
}
