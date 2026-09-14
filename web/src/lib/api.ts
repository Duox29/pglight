import { api } from './storage'

export { api }

export interface ConnectParams {
  host: string
  port: number
  user: string
  password: string
  dbname: string
  sslmode: string
  session_id?: string
}

export interface QueryResult {
  columns: string[]
  types?: string[]
  rows: unknown[][]
  rows_affected?: number
  duration_ms?: number
  total?: number
  statement?: string
  in_txn?: boolean
  /** True when restored from last session's snapshot — Run to refresh. */
  stale?: boolean
}

export interface MultiResult {
  results: QueryResult[]
  duration_ms: number
  in_txn?: boolean
}

export type QueryResponse = QueryResult & { results?: QueryResult[]; error?: string }

export interface LoggingConfig {
  enabled: boolean
  level: string
  log_http: boolean
  log_query: boolean
  slow_ms: number
  max_entries: number
}

export interface LogEntry {
  time: string
  level: string
  category: string
  message: string
  duration_ms?: number
  detail?: string
}

export interface SessionInfo {
  id: string
  host: string
  port: number
  user: string
  dbname: string
  sslmode?: string
  in_txn?: boolean
  connected_at?: string
}

export const q = (session: string, path: string) =>
  `${path}${path.includes('?') ? '&' : '?'}session_id=${encodeURIComponent(session)}`

export const apiClient = {
  connect: (b: ConnectParams) =>
    api<{ session_id?: string; info?: SessionInfo; error?: string }>('/api/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(b),
    }),
  listSessions: () => api<{ sessions: SessionInfo[] }>(`/api/sessions`),
  disconnect: (session: string) => api('/api/disconnect?session_id=' + encodeURIComponent(session)),
  txn: (session: string, action: string) =>
    api<{ ok?: boolean; in_txn?: boolean; error?: string }>('/api/txn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session, action }),
    }),
  runQuery: (session: string, sql: string, limit?: number) =>
    api<QueryResponse>('/api/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session, sql, limit }),
    }),
  explain: (session: string, sql: string, analyze: boolean) =>
    api<unknown>('/api/explain', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session, sql, analyze }),
    }),
  rowOp: (p: {
    session_id: string
    schema: string
    table: string
    op: string
    values: Record<string, unknown>
    where: Record<string, unknown>
  }) =>
    api<{ rows_affected?: number; in_txn?: boolean; error?: string }>('/api/row', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
  maintenance: (session: string, schema: string, table: string, op: string) =>
    api<{ ok?: boolean; result?: string; error?: string }>('/api/maintenance', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session, schema, table, op }),
    }),
  importRows: (p: {
    session_id: string
    schema: string
    table: string
    columns: string[]
    rows: unknown[][]
  }) =>
    api<{ rows_affected?: number; error?: string }>('/api/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
  alterTable: (p: {
    session_id: string
    schema: string
    table: string
    op: string
    column?: string
    new_name?: string
    type?: string
    nullable?: boolean
    default?: string
    drop_default?: boolean
    constraint?: string
    def?: string
    cascade?: boolean
    index?: string
    unique?: boolean
    method?: string
    columns?: string[]
    include?: string[]
    where?: string
    trigger?: string
    timing?: string
    events?: string[]
    for_each?: string
    function?: string
    when?: string
    update_of?: string[]
  }) =>
    api<{ ok?: boolean; in_txn?: boolean; error?: string }>('/api/alter-table', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
  cancel: (session: string, pid: number, kill?: boolean) =>
    api<{ ok?: boolean; error?: string }>(`/api/cancel?session_id=${encodeURIComponent(session)}&pid=${pid}${kill ? '&kill=1' : ''}`),
  getSettings: () => api<{ logging: LoggingConfig }>(`/api/settings`),
  saveSettings: (logging: LoggingConfig) =>
    api<{ logging: LoggingConfig; error?: string }>(`/api/settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ logging }),
    }),
  getLogs: (params: { limit?: number; level?: string; category?: string }) => {
    const sp = new URLSearchParams()
    if (params.limit) sp.set('limit', String(params.limit))
    if (params.level) sp.set('level', params.level)
    if (params.category) sp.set('category', params.category)
    const qs = sp.toString()
    return api<{ entries: LogEntry[]; error?: string }>(`/api/logs${qs ? '?' + qs : ''}`)
  },
  clearLogs: () => api<{ ok?: boolean; error?: string }>(`/api/logs`, { method: 'DELETE' }),
}
