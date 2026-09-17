import type { ErdPos } from './erdLayout'
import { api } from '@/lib/api'

export interface ErdViewport {
  x: number
  y: number
  zoom: number
}

interface ErdLayoutResponse {
  layout?: Record<string, ErdPos>
  viewport?: ErdViewport | null
  updated_at?: string
  error?: string
}

function validLayout(v: unknown): v is Record<string, ErdPos> {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return false
  return Object.values(v as Record<string, unknown>).every((p) => {
    if (!p || typeof p !== 'object') return false
    const o = p as Record<string, unknown>
    return typeof o.x === 'number' && Number.isFinite(o.x) && typeof o.y === 'number' && Number.isFinite(o.y)
  })
}

function validViewport(v: unknown): v is ErdViewport {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  return typeof o.x === 'number' && Number.isFinite(o.x) &&
    typeof o.y === 'number' && Number.isFinite(o.y) &&
    typeof o.zoom === 'number' && Number.isFinite(o.zoom) && o.zoom > 0
}

// Backend is the source of truth. The optional legacy localStorage helpers are
// retained only for one-time migration of layouts created before v2.
const legacyLayoutKey = (sessionId: string, schema: string) => `pglight-erd-layout:${sessionId}:${schema}`
const legacyViewportKey = (sessionId: string, schema: string) => `pglight-erd-viewport:${sessionId}:${schema}`

function readLegacy(sessionId: string, schema: string): { layout: Record<string, ErdPos>; viewport: ErdViewport | null } | null {
  try {
    const rawLayout = localStorage.getItem(legacyLayoutKey(sessionId, schema))
    const rawViewport = localStorage.getItem(legacyViewportKey(sessionId, schema))
    if (!rawLayout && !rawViewport) return null
    const layoutRaw: unknown = rawLayout ? JSON.parse(rawLayout) : {}
    const viewportRaw: unknown = rawViewport ? JSON.parse(rawViewport) : null
    return {
      layout: validLayout(layoutRaw) ? layoutRaw : {},
      viewport: validViewport(viewportRaw) ? viewportRaw : null,
    }
  } catch {
    return null
  }
}

function clearLegacy(sessionId: string, schema: string): void {
  try {
    localStorage.removeItem(legacyLayoutKey(sessionId, schema))
    localStorage.removeItem(legacyViewportKey(sessionId, schema))
  } catch {
    /* cache cleanup is best-effort */
  }
}

export async function loadErdPersistence(connectionId: string, schema: string, legacySessionId?: string): Promise<{
  layout: Record<string, ErdPos>
  viewport: ErdViewport | null
}> {
  if (!connectionId) return { layout: {}, viewport: null }
  try {
    const result = await api<ErdLayoutResponse>(`/api/erd?layout=1&connection_id=${encodeURIComponent(connectionId)}&schema=${encodeURIComponent(schema)}`)
    if (result.error) throw new Error(result.error)
    const layout = validLayout(result.layout) ? result.layout : {}
    const viewport = validViewport(result.viewport) ? result.viewport : null
    if (legacySessionId) {
      const legacy = readLegacy(legacySessionId, schema)
      if (legacy && (Object.keys(legacy.layout).length || legacy.viewport)) {
        const merged = {
          layout: Object.keys(layout).length ? layout : legacy.layout,
          viewport: viewport ?? legacy.viewport,
        }
        const migrated = await saveErdPersistence(connectionId, schema, merged.layout, merged.viewport)
        if (migrated) clearLegacy(legacySessionId, schema)
        return migrated ? merged : { layout, viewport }
      }
    }
    return { layout, viewport }
  } catch {
    return { layout: {}, viewport: null }
  }
}

export async function saveErdPersistence(
  connectionId: string,
  schema: string,
  layout: Record<string, ErdPos>,
  viewport: ErdViewport | null,
): Promise<boolean> {
  if (!connectionId) return false
  try {
    const result = await api<{ ok?: boolean; error?: string }>('/api/erd?layout=1', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ connection_id: connectionId, schema, layout, viewport }),
    })
    return result.ok === true && !result.error
  } catch {
    return false
  }
}

export async function clearErdPersistence(connectionId: string, schema: string): Promise<boolean> {
  if (!connectionId) return false
  try {
    const result = await api<{ ok?: boolean; error?: string }>(`/api/erd?layout=1&connection_id=${encodeURIComponent(connectionId)}&schema=${encodeURIComponent(schema)}`, { method: 'DELETE' })
    return result.ok === true && !result.error
  } catch {
    return false
  }
}
