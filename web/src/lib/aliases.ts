import { api, type CompletionAlias } from './api'

/**
 * Backend-owned completion aliases (GET /api/aliases): short triggers like
 * ssf expand to full templates on prefix match. Fetched once per boot and
 * after every Settings CRUD op; the CodeMirror source reads memory
 * synchronously so typing never hits network (same shape as schemaCache).
 */
let cached: CompletionAlias[] | null = null
let inflight: Promise<CompletionAlias[]> | null = null

export function ensureAliases(): Promise<CompletionAlias[]> {
  if (cached) return Promise.resolve(cached)
  if (inflight) return inflight
  inflight = api<{ aliases?: CompletionAlias[] }>('/api/aliases')
    .then((j) => {
      cached = j.aliases ?? []
      return cached
    })
    .catch(() => cached ?? [])
    .finally(() => {
      inflight = null
    })
  return inflight
}

/** Refresh after a Settings add/edit/delete/reset. */
export function refreshAliases(): Promise<CompletionAlias[]> {
  cached = null
  return ensureAliases()
}

/** Synchronous memory read for the completion source (no fetch on keystroke). */
export function getAliasesCached(): CompletionAlias[] {
  return cached ?? []
}
