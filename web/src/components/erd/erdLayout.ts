import { ERD_MAX_COLUMNS, type ErdRelation, type ErdTable } from './erdMapper'

export interface ErdPos {
  x: number
  y: number
}

export const ERD_NODE_WIDTH = 264
const X_GAP = 90
const Y_GAP = 48
const ROW_H = 20
const HEADER_H = 57

/** Height estimate must match ErdTableNode: header + capped rows + overflow footer. */
export function estimateHeight(colCount: number): number {
  const rows = Math.min(Math.max(colCount, 1), ERD_MAX_COLUMNS)
  return HEADER_H + rows * ROW_H + (colCount > ERD_MAX_COLUMNS ? 20 : 0)
}

/**
 * Deterministic DAG-ish layout, no dependencies.
 * Connected components (union-find) are laid out by BFS layers from the
 * highest-degree table, biggest component first, shelf-packed top to bottom.
 * Saved positions always win; only unknown tables take layout positions.
 */
export function layoutTables(
  tables: ErdTable[],
  relations: ErdRelation[],
  saved: Record<string, ErdPos>,
): Record<string, ErdPos> {
  const out: Record<string, ErdPos> = {}
  if (!tables.length) return out
  const byId = new Map(tables.map((t) => [t.id, t]))
  const height = new Map(tables.map((t) => [t.id, estimateHeight(t.columns.length)]))

  // Union-find over undirected FK links.
  const parent = new Map(tables.map((t) => [t.id, t.id]))
  const find = (x: string): string => {
    const p = parent.get(x) ?? x
    if (p === x) return x
    const r = find(p)
    parent.set(x, r)
    return r
  }
  const adj = new Map<string, Set<string>>()
  for (const t of tables) adj.set(t.id, new Set())
  for (const r of relations) {
    if (!byId.has(r.sourceTable) || !byId.has(r.targetTable)) continue
    adj.get(r.sourceTable)?.add(r.targetTable)
    adj.get(r.targetTable)?.add(r.sourceTable)
    const a = find(r.sourceTable)
    const b = find(r.targetTable)
    if (a !== b) parent.set(a, b)
  }
  const comps = new Map<string, string[]>()
  for (const t of tables) {
    const root = find(t.id)
    comps.set(root, [...(comps.get(root) ?? []), t.id])
  }
  const ordered = [...comps.values()].sort((a, b) => b.length - a.length)

  let cursorY = 0
  for (const comp of ordered) {
    // BFS layers from the highest-degree member.
    const degree = (id: string) => adj.get(id)?.size ?? 0
    const start = [...comp].sort((a, b) => degree(b) - degree(a))[0]
    const depth = new Map<string, number>([[start, 0]])
    const queue = [start]
    while (queue.length) {
      const cur = queue.shift()!
      for (const nb of adj.get(cur) ?? []) {
        if (!depth.has(nb)) {
          depth.set(nb, (depth.get(cur) ?? 0) + 1)
          queue.push(nb)
        }
      }
    }
    // Unreached (shouldn't happen inside a component) go one layer deeper.
    const maxDepth = Math.max(0, ...depth.values())
    for (const id of comp) if (!depth.has(id)) depth.set(id, maxDepth + 1)
    const layers = new Map<number, string[]>()
    for (const id of comp) {
      const d = depth.get(id) ?? 0
      layers.set(d, [...(layers.get(d) ?? []), id])
    }
    for (const ids of layers.values()) ids.sort((a, b) => degree(b) - degree(a) || a.localeCompare(b))

    let compH = 0
    const local = new Map<string, ErdPos>()
    for (const [d, ids] of [...layers.entries()].sort((a, b) => a[0] - b[0])) {
      let y = 0
      for (const id of ids) {
        local.set(id, { x: d * (ERD_NODE_WIDTH + X_GAP), y })
        y += (height.get(id) ?? 120) + Y_GAP
      }
      compH = Math.max(compH, y - Y_GAP)
    }
    for (const [id, p] of local) {
      if (!saved[id]) out[id] = { x: p.x, y: cursorY + p.y }
    }
    cursorY += Math.max(compH, 0) + Y_GAP * 2
  }
  return { ...out, ...pickSaved(saved, byId) }
}

function pickSaved(saved: Record<string, ErdPos>, byId: Map<string, ErdTable>): Record<string, ErdPos> {
  const out: Record<string, ErdPos> = {}
  for (const [id, p] of Object.entries(saved)) {
    if (byId.has(id) && Number.isFinite(p?.x) && Number.isFinite(p?.y)) out[id] = { x: p.x, y: p.y }
  }
  return out
}
