/** ERD data model: API DTOs in, canvas models out. No React Flow here. */

export interface ErdColumnDto {
  name: string
  type?: string
  pk?: boolean
  fk?: boolean
}

export interface ErdEdgeDto {
  fk: string
  src_table: string
  src_col: string
  dst_schema: string
  dst_table: string
  dst_col: string
}

export interface ErdDataDto {
  nodes: string[]
  edges: ErdEdgeDto[]
  columns?: Record<string, ErdColumnDto[]>
}

export interface ErdColumn {
  name: string
  type?: string
  pk: boolean
  fk: boolean
}

export interface ErdTable {
  id: string
  schema: string
  name: string
  columns: ErdColumn[]
  /** Columns that need edge handles (appear in any relation). */
  ports: Record<string, boolean>
}

export interface ErdRelation {
  id: string
  fk: string
  sourceTable: string
  sourceColumn: string
  targetSchema: string
  targetTable: string
  targetColumn: string
  /** True when >1 FK links the same table pair (edge shows its column label). */
  labeled: boolean
}

export interface ErdGraph {
  tables: ErdTable[]
  relations: ErdRelation[]
}

/** Max columns rendered per node; the rest collapse into a "+N more" footer. */
export const ERD_MAX_COLUMNS = 30

function edgeId(e: ErdEdgeDto): string {
  return `${e.src_table}.${e.src_col}->${e.dst_schema}.${e.dst_table}.${e.dst_col}:${e.fk}`
}

/** Map the /api/erd payload to canvas models. Drops dangling edges (dst outside the schema). */
export function mapErd(schema: string, dto: ErdDataDto): ErdGraph {
  const names = new Set(dto.nodes ?? [])
  const pairCount = new Map<string, number>()
  for (const e of dto.edges ?? []) {
    if (!names.has(e.src_table)) continue
    if (e.dst_schema !== schema || !names.has(e.dst_table)) continue
    pairCount.set(`${e.src_table}→${e.dst_table}`, (pairCount.get(`${e.src_table}→${e.dst_table}`) ?? 0) + 1)
  }
  const ports: Record<string, Record<string, boolean>> = {}
  const mark = (t: string, c: string) => {
    if (!names.has(t)) return
    ports[t] = ports[t] ?? {}
    ports[t][c] = true
  }
  const relations: ErdRelation[] = []
  const seen = new Set<string>()
  for (const e of dto.edges ?? []) {
    if (!names.has(e.src_table)) continue
    if (e.dst_schema !== schema || !names.has(e.dst_table)) continue
    const id = edgeId(e)
    if (seen.has(id)) continue
    seen.add(id)
    mark(e.src_table, e.src_col)
    mark(e.dst_table, e.dst_col)
    relations.push({
      id,
      fk: e.fk,
      sourceTable: e.src_table,
      sourceColumn: e.src_col,
      targetSchema: e.dst_schema,
      targetTable: e.dst_table,
      targetColumn: e.dst_col,
      labeled: (pairCount.get(`${e.src_table}→${e.dst_table}`) ?? 0) > 1,
    })
  }
  const tables: ErdTable[] = (dto.nodes ?? []).map((n) => {
    const cols = (dto.columns?.[n] ?? []).map((c) => ({
      name: c.name,
      type: c.type,
      pk: !!c.pk,
      fk: !!c.fk || !!ports[n]?.[c.name],
    }))
    return { id: n, schema, name: n, columns: cols, ports: ports[n] ?? {} }
  })
  return { tables, relations }
}

/** Source/target handle ids for a column edge endpoint. */
export const srcHandle = (col: string) => `s:${col}`
export const dstHandle = (col: string) => `t:${col}`
