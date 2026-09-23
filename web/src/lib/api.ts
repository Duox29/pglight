import { api, apiStream } from './storage'
import type { CommandId } from '@/commands/types'

export { api }

export interface ConnectParams {
  host: string
  port: number
  user: string
  password: string
  dbname: string
  sslmode: string
  session_id?: string
  profile_id?: string
  ssh_password?: string
  ssh_private_key?: string
  ssh_passphrase?: string
}

export interface VaultStatus {
  exists: boolean
  unlocked: boolean
  version?: number
  auto_lock_seconds?: number
}

export interface ConnectionProfile {
  id: string
  name: string
  host: string
  port: number
  user: string
  dbname: string
  sslmode?: string
  has_password: boolean
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
    ssh_enabled?: boolean
    ssh_host?: string
    ssh_port?: number
    ssh_user?: string
    ssh_auth_method?: string
    ssh_host_key?: string
  }
}

export interface QueryResult {
  columns: string[]
  types?: string[]
  rows: unknown[][]
  rows_affected?: number
  duration_ms?: number
  total?: number
  has_more?: boolean
  statement?: string
  in_txn?: boolean
  /** True when restored from last session's snapshot — Run to refresh. */
  stale?: boolean
}

export interface TableChangesPayload {
  session_id: string
  schema: string
  table: string
  updates?: { key: Record<string, unknown>; before: Record<string, unknown>; changes: Record<string, unknown> }[]
  inserts?: Record<string, unknown>[]
  deletes?: { key: Record<string, unknown>; before: Record<string, unknown> }[]
}

let csvDownloadFrame: HTMLIFrameElement | null = null
let csvDownloadErrorHandler: ((message: string) => void) | undefined

function getCSVDownloadFrame(): HTMLIFrameElement {
  if (csvDownloadFrame?.isConnected) return csvDownloadFrame
  const frame = document.createElement('iframe')
  frame.name = 'pglight-csv-download'
  frame.title = 'CSV download'
  frame.hidden = true
  frame.addEventListener('load', () => {
    const text = frame.contentDocument?.body?.textContent?.trim()
    if (!text) return
    try {
      const result = JSON.parse(text) as { error?: unknown }
      if (typeof result.error === 'string') csvDownloadErrorHandler?.(result.error)
    } catch {
      // Successful attachment downloads do not navigate the hidden frame.
    }
  })
  document.body.append(frame)
  csvDownloadFrame = frame
  return frame
}

function submitCSVDownload(payload: {
  session_id: string
  schema: string
  table: string
  filter?: string
  order?: string
}, onError?: (message: string) => void): void {
  csvDownloadErrorHandler = onError
  const form = document.createElement('form')
  form.method = 'POST'
  form.action = '/api/export/csv'
  form.enctype = 'application/x-www-form-urlencoded'
  form.target = getCSVDownloadFrame().name
  form.hidden = true
  for (const [name, value] of Object.entries(payload)) {
    if (value === undefined) continue
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = name
    input.value = value
    form.append(input)
  }
  document.body.append(form)
  form.submit()
  form.remove()
}

export interface MultiResult {
  results: QueryResult[]
  duration_ms: number
  in_txn?: boolean
}

export interface QueryErrorLoc {
  line?: number
  column?: number
  code?: string
  statement_index?: number
}

export type QueryResponse = QueryResult & { results?: QueryResult[]; statements?: number; error?: string } & QueryErrorLoc

export interface LoggingConfig {
  enabled: boolean
  level: string
  log_http: boolean
  log_query: boolean
  slow_ms: number
  max_entries: number
}

export interface SecurityConfig {
  allow_lan_access: boolean
  effective_mode?: 'loopback' | 'lan'
  restart_required?: boolean
}

export interface ShortcutSettings {
  version: 1
  overrides: Partial<Record<CommandId, string[]>>
}

export interface LogEntry {
  time: string
  level: string
  category: string
  message: string
  duration_ms?: number
  detail?: string
}

export interface CompletionAlias {
  trigger: string
  expansion: string
  detail?: string
  builtin: boolean
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
  tls_warn?: boolean
  profile_id?: string
  profile_name?: string
  ssh_tunnel?: boolean
  ssh_host?: string
  ssh_port?: number
  ssh_user?: string
  ssh_auth_method?: string
  ssh_host_key?: string
}

export const q = (session: string, path: string) =>
  `${path}${path.includes('?') ? '&' : '?'}session_id=${encodeURIComponent(session)}`

export const apiClient = {
  backup: (p: { session_id: string; format: 'custom' | 'plain'; schema_only?: boolean; data_only?: boolean }) =>
    api<JobSnapshot>('/api/backup', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p) }),
  restore: (p: { session_id: string; file: File; overwrite: true }) => {
    const body = new FormData()
    body.set('session_id', p.session_id)
    body.set('overwrite', String(p.overwrite))
    body.set('file', p.file)
    return api<JobSnapshot>('/api/restore', { method: 'POST', body })
  },
  job: (session: string, id: string) => api<JobSnapshot>(q(session, `/api/jobs/${encodeURIComponent(id)}?`)),
  cancelJob: (session: string, id: string) => api<{ ok?: boolean; error?: string }>(q(session, `/api/jobs/${encodeURIComponent(id)}?`), { method: 'DELETE' }),
  downloadJob: (session: string, id: string) => apiStream(`${q(session, `/api/jobs/${encodeURIComponent(id)}?`)}&action=download`, { method: 'POST' }),
  tableChanges: (payload: TableChangesPayload) =>
    api<{ rows_affected?: number; in_txn?: boolean; error?: string }>('/api/table-changes', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
  exportCSV: (p: { session_id: string; schema: string; table: string; filter?: string; order?: string }, onError?: (message: string) => void) =>
    submitCSVDownload(p, onError),
  connect: (b: ConnectParams) =>
    api<{ session_id?: string; info?: SessionInfo; error?: string }>('/api/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(b),
    }),
  listSessions: () => api<{ sessions: SessionInfo[] }>(`/api/sessions`),
  disconnect: (session: string) => api('/api/disconnect?session_id=' + encodeURIComponent(session), { method: 'DELETE' }),
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
  explain: (session: string, sql: string) =>
    api<unknown>('/api/explain', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: session, sql }),
    }),
  explainError: (v: unknown): string | undefined => {
    if (v && typeof v === 'object' && 'error' in v && typeof v.error === 'string') return v.error
    return undefined
  },
  rowOp: (p: {
    session_id: string
    schema: string
    table: string
    op: string
    values: Record<string, unknown>
    where: Record<string, unknown>
    /** Single-row UI action: backend guarantees exactly one row affected. */
    single?: boolean
  }) =>
    api<{ rows_affected?: number; in_txn?: boolean; error?: string }>('/api/row', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
  batchDelete: (p: { session_id: string; schema: string; table: string; where: Record<string, unknown>[] }) =>
    api<{ deleted?: number; in_txn?: boolean; error?: string }>('/api/rows-delete', {
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
  importCSV: (p: { session_id: string; schema: string; table: string; columns: string[]; file: File; delimiter?: string }) => {
    const body = new FormData()
    body.set('session_id', p.session_id)
    body.set('schema', p.schema)
    body.set('table', p.table)
    body.set('columns', JSON.stringify(p.columns))
    body.set('file', p.file)
    if (p.delimiter) body.set('delimiter', p.delimiter)
    return api<{ rows_affected?: number; in_txn?: boolean; error?: string }>('/api/import/csv', { method: 'POST', body })
  },
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
    api<{ ok?: boolean; error?: string }>(`/api/cancel?session_id=${encodeURIComponent(session)}&pid=${pid}${kill ? '&kill=1' : ''}`, { method: 'POST' }),
  getSettings: () => api<{ logging: LoggingConfig; security: SecurityConfig }>(`/api/settings`),
  saveSettings: (logging?: LoggingConfig, security?: SecurityConfig) =>
    api<{ logging: LoggingConfig; security: SecurityConfig; error?: string }>(`/api/settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        ...(logging ? { logging } : {}),
        ...(security ? { security: { allow_lan_access: security.allow_lan_access } } : {}),
      }),
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
  getShortcutSettings: () => api<ShortcutSettings>(`/api/preferences/shortcuts`),
  saveShortcutSettings: (overrides: Partial<Record<CommandId, string[]>>) =>
    api<ShortcutSettings & { error?: string }>(`/api/preferences/shortcuts`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: 1, overrides }),
    }),
  listAliases: () => api<{ aliases?: CompletionAlias[]; error?: string }>(`/api/aliases`),
  saveAlias: (trigger: string, expansion: string) =>
    api<{ alias?: CompletionAlias; error?: string }>(`/api/aliases`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ trigger, expansion }),
    }),
  deleteAlias: (trigger: string) =>
    api<{ ok?: boolean; error?: string }>(`/api/aliases?trigger=${encodeURIComponent(trigger)}`, { method: 'DELETE' }),
  resetAliases: () => api<{ ok?: boolean; error?: string }>(`/api/aliases`, { method: 'DELETE' }),
  listSnippets: () => api<{ snippets: { id: string; name: string; sql: string }[] }>('/api/snippets'),
  saveSnippet: (name: string, sql: string) => api<{ snippet?: { id: string; name: string; sql: string }; error?: string }>('/api/snippets', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, sql }) }),
  deleteSnippet: (name: string) => api<{ ok?: boolean; error?: string }>(`/api/snippets?name=${encodeURIComponent(name)}`, { method: 'DELETE' }),
  listHistory: (filters: { q?: string; connection_id?: string; status?: 'success' | 'failed' | ''; statement_type?: string; min_ms?: number; pinned?: boolean; limit?: number; offset?: number } = {}) => {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(filters)) if (value !== undefined && value !== '' && value !== false) params.set(key, String(value))
    return api<{ history: import('@/types').HistoryEntry[]; has_more: boolean }>(`/api/history?${params.toString()}`)
  },
  addHistory: (sql: string, ms?: number, n?: number, details?: { session_id?: string; success?: boolean; error_code?: string; error_message?: string }) => api('/api/history', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ sql, ms, n, ...details }) }),
  pinHistory: (id: string, pinned: boolean) => api<{ ok?: boolean; error?: string }>('/api/history', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id, pinned }) }),
  deleteHistoryEntry: (id: string) => api<{ ok?: boolean; error?: string }>(`/api/history?id=${encodeURIComponent(id)}`, { method: 'DELETE' }),
  clearHistory: () => api<{ ok?: boolean; error?: string }>('/api/history', { method: 'DELETE' }),
  listConnections: (query = '') => api<{ connections: ConnectionProfile[]; folders?: { id: string; name: string; parent_id?: string }[]; vault_unlocked?: boolean }>(`/api/connections${query ? `?${query}` : ''}`),
  saveConnection: (p: { id?: string; name: string; host: string; port: number; user: string; dbname: string; sslmode: string; password?: string; save_password?: boolean; clear_password?: boolean; duplicate_from?: string; folder_id?: string; environment?: string; color?: string; description?: string; favorite?: boolean; default?: boolean; tags?: string[]; connect_timeout?: number; keepalive?: number; application_name?: string; search_path?: string; sslrootcert?: string; sslcert?: string; sslkey?: string; unix_socket?: string; ssh_enabled?: boolean; ssh_host?: string; ssh_port?: number; ssh_user?: string; ssh_auth_method?: string; ssh_host_key?: string; ssh_password?: string; ssh_private_key?: string; ssh_passphrase?: string }) => api<{ connection?: ConnectionProfile; error?: string }>('/api/connections', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p) }),
  deleteConnection: (id: string) => api<{ ok?: boolean; error?: string }>(`/api/connections?id=${encodeURIComponent(id)}`, { method: 'DELETE' }),
  saveConnectionFolder: (p: { id?: string; name: string; parent_id?: string }) => api<{ folder?: { id: string; name: string; parent_id?: string }; error?: string }>('/api/connections/folders', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p) }),
  deleteConnectionFolder: (id: string) => api<{ ok?: boolean; error?: string }>(`/api/connections/folders?id=${encodeURIComponent(id)}`, { method: 'DELETE' }),
  exportConnections: (encrypted = false, password?: string) => api<ConnectionProfile[] | { format: string; version: number; connections: ConnectionProfile[] }>('/api/connections/export', { method: encrypted ? 'POST' : 'GET', ...(encrypted ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ encrypted: true, password }) } : {}) }),
  importConnections: (payload: unknown, password?: string) => api<{ ok?: boolean; imported?: number; error?: string }>('/api/connections/import', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ payload, password }) }),
  testConnection: (p: { profile_id?: string; host?: string; port?: number; user?: string; dbname?: string; sslmode?: string; password?: string }) => api<{ ok?: boolean; message?: string; database?: string; version?: string; latency_ms?: number; error?: string }>('/api/connections/test', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(p) }),
  getVault: () => api<VaultStatus & { error?: string }>('/api/vault'),
  vaultAction: (action: 'setup' | 'unlock' | 'lock' | 'change_password', master_password?: string, new_password?: string) => api<VaultStatus & { ok?: boolean; error?: string }>('/api/vault', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(action === 'change_password' ? { action, current_password: master_password, new_password } : { action, master_password }) }),
  shutdown: () => api<{ ok?: boolean; error?: string }>(`/api/shutdown`, { method: 'POST' }),
  getMockDataMeta: (session: string, schema: string, table: string) =>
    api<MockDataMeta>(q(session, `/api/mock-data/meta?schema=${encodeURIComponent(schema)}&table=${encodeURIComponent(table)}`)),
  previewMockData: (p: MockDataRequest) =>
    api<MockDataPreview>(`/api/mock-data/preview`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
  generateMockData: (p: MockDataRequest) =>
    api<MockDataResult>(`/api/mock-data/generate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    }),
}

export interface JobSnapshot {
  id: string
  type: 'backup' | 'restore' | string
  state: 'queued' | 'running' | 'done' | 'failed' | 'cancelled'
  rows: number
  bytes: number
  error?: string
  file_name?: string
}

export interface MockDataColumn {
  name: string
  data_type: string
  udt: string
  nullable: boolean
  default: string | null
  identity: boolean
  generated: boolean
  primary_key: boolean
  unique: boolean
  enum_values: string[]
  semantic_hint: string
}

export interface MockDataFK {
  name: string
  column: string
  ref_schema: string
  ref_table: string
  ref_column: string
}

export interface MockDataCheck {
  name: string
  definition: string
  kind: string
  warning?: string
}

export interface MockDataMeta {
  columns: MockDataColumn[]
  foreign_keys: MockDataFK[]
  checks: MockDataCheck[]
  error?: string
}

export interface MockFieldConfig {
  column: string
  generator: string
  params?: Record<string, unknown>
  unique?: boolean
  null_probability?: number
}

export interface MockConstraintSide {
  field?: string
  value?: unknown
}

export interface MockConstraint {
  kind: string
  left: MockConstraintSide
  right: MockConstraintSide
  operator: string
}

export interface MockDataRequest {
  session_id: string
  schema: string
  table: string
  mode: 'simple' | 'advanced'
  count: number
  seed?: number
  fields?: MockFieldConfig[]
  constraints?: MockConstraint[]
}

export interface MockDataPreview {
  columns?: string[]
  rows?: unknown[][]
  warnings?: string[]
  seed?: number
  in_txn?: boolean
  error?: string
}

export interface MockDataResult {
  generated?: number
  inserted?: number
  seed?: number
  duration_ms?: number
  in_txn?: boolean
  error?: string
}
