import type { QueryResult } from './lib/api'

export interface SessionInfo {
  id: string
  host: string
  port: string
  user: string
  dbname: string
  sslmode: string
  /** True when a non-loopback host connects without certificate verification. */
  tls_warn?: boolean
  profile_id?: string
  profile_name?: string
}

export interface SavedConnection {
  id?: string
  name: string
  host: string
  port: string
  user: string
  password: string
  dbname: string
  sslmode: string
	has_password?: boolean
	last_used_at?: string
	folder_id?: string
	environment?: string
	color?: string
	description?: string
	favorite?: boolean
	default?: boolean
	tags?: string[]
	options?: {
		connect_timeout?: number
		keepalive?: number
		application_name?: string
		search_path?: string
		sslrootcert?: string
		sslcert?: string
		sslkey?: string
		unix_socket?: string
	}
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

export interface QueryTabT {
  id: string
  kind: 'query'
  title: string
  sessionId: string
  sql: string
  limit: number
  results: (QueryResult & { statement?: string })[] | null
  error?: string
  errLoc?: { line?: number; column?: number; statement_index?: number; code?: string }
  /** Bumps every failed run so the editor re-blinks the same line. */
  flashTick?: number
  meta?: string
  plan?: string
}

export type TableSubtab = 'data' | 'columns' | 'ddl' | 'indexes' | 'constraints' | 'triggers' | 'stats'

export interface TableTabT {
  id: string
  kind: 'table'
  title: string
  sessionId: string
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
  triggers: { name: string; table: string; event: string; timing: string; statement: string; enabled?: string }[] | null
  stats: Record<string, unknown> | null
}

export interface BrowserTabT {
  id: string
  kind: 'browser'
  title: string
  sessionId: string
  url: string
  cols: string[]
  rows: Record<string, unknown>[] | null
  error?: string
}

export interface DocsTabT {
  id: string
  kind: 'docs'
  title: string
}

export interface WorkspaceTabT {
  id: string
  kind: 'workspace'
  title: string
  view: SideView
}

export interface ErdTabT {
  id: string
  kind: 'erd'
  title: string
  sessionId: string
  schema: string
  data: {
    nodes: string[]
    edges: { fk: string; src_table: string; src_col: string; dst_schema: string; dst_table: string; dst_col: string }[]
    columns?: Record<string, { name: string; type?: string; pk?: boolean; fk?: boolean }[]>
  } | null
}

export type ObjectKind = 'function' | 'sequence' | 'type'

export interface ObjectTabT {
  id: string
  kind: 'object'
  title: string
  sessionId: string
  objectKind: ObjectKind
  schema: string
  name: string
  def: string | null
  details: Record<string, unknown> | null
  error?: string
}

export type Tab = QueryTabT | TableTabT | BrowserTabT | ErdTabT | DocsTabT | WorkspaceTabT | ObjectTabT

export type SideView = 'history' | 'snippets' | 'aliases' | 'server' | 'activity' | 'locks' | 'stats' | 'connections' | 'appearance' | 'settings' | 'shortcuts' | 'logs' | 'quick-access'

/** Capped result snapshot kept for query tabs (never auto re-run). */
export interface QuerySnapshot {
  columns: string[]
  rows: unknown[][]
  meta?: string
  at: string
}

/** Minimal persisted tab shell for last-session restore. */
export interface StoredTab {
  id: string
  kind: string
  title: string
  sessionId?: string
  sql?: string
  limit?: number
  schema?: string
  table?: string
  subtab?: TableSubtab
  filter?: string
  order?: string
  offset?: number
  key?: string
  snapshot?: QuerySnapshot
  objectKind?: ObjectKind
  name?: string
  view?: SideView
}
