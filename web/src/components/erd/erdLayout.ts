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

interface LNode {
  id: string
  h: number
  layer: number
  x: number
  y: number
}

interface ErdBox {
  ids: string[]
  local: Map<string, ErdPos>
  w: number
  h: number
}

/**
 * Deterministic FK-directed layout, no dependencies.
 * Layout edges run child (FK holder, `sourceTable`) -> parent (referenced,
 * `targetTable`), the same direction as the canvas edge arrows, so beziers
 * flow left to right without bending back: e.g. order_items -> orders ->
 * customers. Cycles are broken by DFS feedback-edge removal, layers by
 * longest path, order within a layer by one barycenter pass. Connected
 * components become layer blocks, isolated tables share one balanced grid
 * block, and blocks shelf-pack row-wise into a roughly square canvas.
 * Saved positions always win.
 */
export function layoutTables(
  tables: ErdTable[],
  relations: ErdRelation[],
  saved: Record<string, ErdPos>,
): Record<string, ErdPos> {
  const out: Record<string, ErdPos> = {}
  if (!tables.length) return out

  const nodes = new Map<string, LNode>()
  for (const t of tables) {
    nodes.set(t.id, { id: t.id, h: estimateHeight(t.columns.length), layer: 0, x: 0, y: 0 })
  }
  const ids = [...nodes.keys()].sort()
  const byId = new Map(tables.map((t) => [t.id, t]))

  // 1. Directed layout edges: child (sourceTable) -> parent (targetTable).
  // `succ` = right-side neighbours, `pred` = left-side neighbours.
  const succ = new Map<string, string[]>()
  const pred = new Map<string, string[]>()
  for (const id of ids) {
    succ.set(id, [])
    pred.set(id, [])
  }
  const link = (left: string, right: string): void => {
    if (!nodes.has(left) || !nodes.has(right) || left === right) return // dangling or self-loop
    if (!succ.get(left)?.includes(right)) succ.get(left)?.push(right)
    if (!pred.get(right)?.includes(left)) pred.get(right)?.push(left)
  }
  for (const r of relations) link(r.sourceTable, r.targetTable)
  for (const id of ids) {
    succ.get(id)?.sort()
    pred.get(id)?.sort()
  }

  // 2. Break cycles: DFS back edges are excluded from layering.
  // Length-prefixed keys stay unambiguous for any table name.
  const key = (u: string, v: string): string => u.length + ':' + u + v
  const visited = new Set<string>()
  const inStack = new Set<string>()
  const back = new Set<string>()
  const dfs = (u: string): void => {
    visited.add(u)
    inStack.add(u)
    for (const v of succ.get(u) ?? []) {
      if (inStack.has(v)) back.add(key(u, v))
      else if (!visited.has(v)) dfs(v)
    }
    inStack.delete(u)
  }
  for (const id of ids) if (!visited.has(id)) dfs(id)

  const keptSucc = new Map<string, string[]>()
  const keptPred = new Map<string, string[]>()
  for (const id of ids) {
    keptSucc.set(id, [])
    keptPred.set(id, [])
  }
  for (const u of ids) {
    for (const v of succ.get(u) ?? []) {
      if (back.has(key(u, v))) continue
      keptSucc.get(u)?.push(v)
      keptPred.get(v)?.push(u)
    }
  }

  // 3. Weakly connected components over all (non-self) FK links.
  const und = new Map<string, Set<string>>()
  for (const id of ids) und.set(id, new Set())
  for (const r of relations) {
    const a = r.sourceTable
    const b = r.targetTable
    if (!nodes.has(a) || !nodes.has(b) || a === b) continue
    und.get(a)?.add(b)
    und.get(b)?.add(a)
  }
  const comps: string[][] = []
  const seen = new Set<string>()
  for (const id of ids) {
    if (seen.has(id)) continue
    const stack = [id]
    const comp: string[] = []
    seen.add(id)
    while (stack.length) {
      const u = stack.pop() as string
      comp.push(u)
      for (const v of und.get(u) ?? []) {
        if (!seen.has(v)) {
          seen.add(v)
          stack.push(v)
        }
      }
    }
    comp.sort((a, b) => a.localeCompare(b))
    comps.push(comp)
  }

  // 4. Longest-path layering per component (Kahn, deterministic).
  for (const comp of comps) {
    const inComp = new Set(comp)
    const indeg = new Map<string, number>()
    for (const u of comp) indeg.set(u, 0)
    for (const u of comp) {
      for (const v of keptSucc.get(u) ?? []) {
        if (inComp.has(v)) indeg.set(v, (indeg.get(v) ?? 0) + 1)
      }
    }
    const layer = new Map<string, number>()
    for (const u of comp) layer.set(u, 0)
    const q = comp.filter((u) => (indeg.get(u) ?? 0) === 0).sort((a, b) => a.localeCompare(b))
    while (q.length) {
      q.sort((a, b) => a.localeCompare(b))
      const u = q.shift() as string
      const lu = layer.get(u) ?? 0
      for (const v of keptSucc.get(u) ?? []) {
        if (!inComp.has(v)) continue
        if ((layer.get(v) ?? 0) < lu + 1) layer.set(v, lu + 1)
        indeg.set(v, (indeg.get(v) ?? 0) - 1)
        if ((indeg.get(v) ?? 0) === 0) q.push(v)
      }
    }
    for (const u of comp) {
      const n = nodes.get(u)
      if (n) n.layer = layer.get(u) ?? 0
    }
  }

  // 5. Barycenter order within each layer, then local x/y per component.
  for (const comp of comps) {
    if (comp.length < 2) continue // isolated tables share one grid block below
    const inComp = new Set(comp)
    const maxLayer = Math.max(0, ...comp.map((u) => nodes.get(u)?.layer ?? 0))
    const layers: string[][] = Array.from({ length: maxLayer + 1 }, () => [])
    for (const u of comp) layers[nodes.get(u)?.layer ?? 0]?.push(u)
    layers[0]?.sort((a, b) => a.localeCompare(b))
    const prevIdx = new Map<string, number>()
    layers[0]?.forEach((u, i) => prevIdx.set(u, i))
    for (let l = 1; l <= maxLayer; l++) {
      layers[l]?.sort((a, b) => {
        const center = (id: string): number => {
          const ps = (keptPred.get(id) ?? []).filter(
            (p) => inComp.has(p) && (nodes.get(p)?.layer ?? -1) === l - 1,
          )
          if (!ps.length) return Number.POSITIVE_INFINITY
          return ps.reduce((s, p) => s + (prevIdx.get(p) ?? 0), 0) / ps.length
        }
        const ba = center(a)
        const bb = center(b)
        if (ba !== bb) return ba - bb
        return a.localeCompare(b)
      })
      layers[l]?.forEach((u, i) => prevIdx.set(u, i))
    }
    for (let l = 0; l <= maxLayer; l++) {
      let y = 0
      for (const u of layers[l] ?? []) {
        const n = nodes.get(u)
        if (!n) continue
        n.x = l * (ERD_NODE_WIDTH + X_GAP)
        n.y = y
        y += n.h + Y_GAP
      }
    }
  }

  // 6. Blocks: one layer block per connected component, one shared grid
  // block for isolated tables; shelf-pack row-wise into a square canvas.
  const boxes: ErdBox[] = []
  for (const comp of comps) {
    if (comp.length < 2) continue
    const local = new Map<string, ErdPos>()
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    for (const u of comp) {
      const n = nodes.get(u)
      if (!n) continue
      local.set(u, { x: n.x, y: n.y })
      minX = Math.min(minX, n.x)
      minY = Math.min(minY, n.y)
      maxX = Math.max(maxX, n.x + ERD_NODE_WIDTH)
      maxY = Math.max(maxY, n.y + n.h)
    }
    for (const [u, p] of local) local.set(u, { x: p.x - minX, y: p.y - minY })
    boxes.push({ ids: comp, local, w: maxX - minX, h: maxY - minY })
  }
  const isolated = comps
    .filter((c) => c.length < 2)
    .map((c) => c[0] as string)
    .sort((a, b) => a.localeCompare(b))
  if (isolated.length) boxes.push(layoutGrid(isolated, nodes))

  boxes.sort((a, b) => b.w * b.h - a.w * a.h || (a.ids[0] ?? '').localeCompare(b.ids[0] ?? ''))
  const totalArea = boxes.reduce((s, b) => s + (b.w + X_GAP) * (b.h + Y_GAP), 0)
  const targetW = Math.max(...boxes.map((b) => b.w), Math.ceil(Math.sqrt(Math.max(totalArea, 0) * 1.4)))
  let curX = 0
  let curY = 0
  let rowH = 0
  for (const b of boxes) {
    if (curX > 0 && curX + b.w > targetW) {
      curX = 0
      curY += rowH + Y_GAP
      rowH = 0
    }
    for (const u of b.ids) {
      const p = b.local.get(u)
      if (p) out[u] = { x: curX + p.x, y: curY + p.y }
    }
    curX += b.w + X_GAP
    rowH = Math.max(rowH, b.h)
  }

  // 7. Saved positions always win; layout covers every table first.
  return { ...out, ...pickSaved(saved, byId) }
}

/** Balanced grid: columns wrap at ceil(sqrt(n)) so the block stays roughly square. */
function layoutGrid(sortedIds: string[], nodes: Map<string, LNode>): ErdBox {
  const n = sortedIds.length
  const cols = Math.max(1, Math.ceil(Math.sqrt(n)))
  const colY: number[] = new Array<number>(cols).fill(0)
  const local = new Map<string, ErdPos>()
  sortedIds.forEach((id, i) => {
    const c = i % cols
    const x = c * (ERD_NODE_WIDTH + X_GAP)
    const y = colY[c] ?? 0
    local.set(id, { x, y })
    colY[c] = y + (nodes.get(id)?.h ?? 120) + Y_GAP
  })
  const w = cols * (ERD_NODE_WIDTH + X_GAP) - X_GAP
  const h = Math.max(0, ...colY) - Y_GAP
  return { ids: sortedIds, local, w, h: Math.max(h, 0) }
}

function pickSaved(saved: Record<string, ErdPos>, byId: Map<string, ErdTable>): Record<string, ErdPos> {
  const out: Record<string, ErdPos> = {}
  for (const [id, p] of Object.entries(saved ?? {})) {
    if (byId.has(id) && Number.isFinite(p?.x) && Number.isFinite(p?.y)) out[id] = { x: p.x, y: p.y }
  }
  return out
}
