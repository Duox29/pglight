import type { QueryResult } from './lib/api'

export interface SavedConnection {
  name: string
  host: string
  port: string
  user: string
  password: string
  dbname: string
  sslmode: string
}

export interface HistoryEntry {
  sql: string
  ms?: number
  n?: number
  at: string
}

export interface Snippet {
  name: string
  sql: string
}

export interface DbInfo {
  name: string
  size?: string
  allow_conn?: boolean
}

export interface ObjRef {
  schema: string
  name: string
  type?: string
  est_rows?: number
  lang?: string
}

export interface SchemaGroup {
  schema: string
  tables: ObjRef[]
  views: ObjRef[]
  matviews: ObjRef[]
  foreign: ObjRef[]
  functions: ObjRef[]
  sequences: ObjRef[]
  types: ObjRef[]
}

export interface ObjectDetail {
  title: string
  kind: string
  body: React.ReactNode
}

export interface QueryTabT {
  id: string
  kind: 'query'
  title: string
  sql: string
  limit: number
  results: (QueryResult & { statement?: string })[] | null
  error?: string
  meta?: string
  plan?: string
}

export type TableSubtab = 'data' | 'columns' | 'ddl' | 'indexes' | 'constraints' | 'triggers' | 'stats'

export interface TableTabT {
  id: string
  kind: 'table'
  title: string
  schema: string
  table: string
  subtab: TableSubtab
  limit: number
  offset: number
  filter: string
  order: string
  result: (QueryResult & { total?: number }) | null
  error?: string
  cols: Record<string, unknown>[] | null
  ddl: {
    ddl: string
    owner?: string
    comment?: string
    indexes?: { name: string; def: string }[]
    constraints?: { name: string; type?: string; def: string }[]
    foreign_keys?: { name: string; def: string }[]
    triggers?: { name: string; def: string }[]
  } | null
  constraints: { name: string; type: string; def: string }[] | null
  triggers: { name: string; table: string; event: string; timing: string; statement: string }[] | null
  stats: Record<string, unknown> | null
}

export interface BrowserTabT {
  id: string
  kind: 'browser'
  title: string
  url: string
  cols: string[]
  rows: Record<string, unknown>[] | null
  error?: string
}

export interface ErdTabT {
  id: string
  kind: 'erd'
  title: string
  schema: string
  data: { nodes: string[]; edges: { fk: string; src_table: string; src_col: string; dst_schema: string; dst_table: string; dst_col: string }[] } | null
}

export type Tab = QueryTabT | TableTabT | BrowserTabT | ErdTabT

export type SideView = 'history' | 'snippets' | 'server' | 'activity' | 'locks' | 'stats' | 'settings'

/** Minimal persisted tab shell for last-session restore (no results). */
export interface StoredTab {
  id: string
  kind: string
  title: string
  sql?: string
  limit?: number
  schema?: string
  table?: string
  subtab?: TableSubtab
  filter?: string
  order?: string
  offset?: number
  key?: string
}
