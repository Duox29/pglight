import type { Completion, CompletionSource } from '@codemirror/autocomplete'
import { getSnapshotCached, type SchemaSnapshot } from './schemaCache'
import { getAliasesCached } from './aliases'

/** Clause context of the statement prefix before the cursor. */
export type ScopeKind = 'table' | 'column' | 'dot' | 'keyword'

export interface Scope {
  kind: ScopeKind
  /** qualifier before the dot (alias or table name), dot-context only */
  dot?: string
  /** tables referenced by FROM/JOIN in the current statement */
  tables: { schema: string; name: string; alias: string }[]
}

const RECENT_KEY = 'pglight-complete-recent'

export function getRecent(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]') as unknown
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').slice(0, 50) : []
  } catch {
    return []
  }
}

export function recordUse(label: string) {
  try {
    const next = [label, ...getRecent().filter((x) => x !== label)].slice(0, 50)
    localStorage.setItem(RECENT_KEY, JSON.stringify(next))
  } catch {
    /* private mode — recent boost just won't persist */
  }
}

const KEYWORDS = [
  'SELECT', 'FROM', 'WHERE', 'JOIN', 'INNER', 'LEFT', 'RIGHT', 'FULL', 'OUTER', 'CROSS', 'ON', 'USING',
  'ORDER', 'BY', 'GROUP', 'HAVING', 'LIMIT', 'OFFSET', 'FETCH', 'FIRST', 'DISTINCT', 'ALL', 'ASC', 'DESC', 'NULLS', 'LAST',
  'INSERT', 'INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE', 'RETURNING', 'CONFLICT', 'DO', 'NOTHING',
  'WITH', 'RECURSIVE', 'AS', 'UNION', 'EXCEPT', 'INTERSECT',
  'CREATE', 'ALTER', 'DROP', 'TRUNCATE', 'ADD', 'COLUMN', 'CONSTRAINT', 'RENAME', 'TO',
  'SCHEMA', 'TABLE', 'VIEW', 'MATERIALIZED', 'INDEX', 'SEQUENCE', 'FUNCTION', 'TRIGGER', 'TYPE', 'EXTENSION', 'DATABASE', 'ROLE',
  'PRIMARY', 'KEY', 'FOREIGN', 'REFERENCES', 'CHECK', 'UNIQUE', 'DEFAULT', 'NOT', 'NULL',
  'CASCADE', 'RESTRICT', 'IF', 'EXISTS',
  'BEGIN', 'TRANSACTION', 'COMMIT', 'ROLLBACK', 'SAVEPOINT', 'EXPLAIN', 'ANALYZE', 'VACUUM', 'REINDEX', 'CLUSTER', 'COPY',
  'GRANT', 'REVOKE', 'OWNER',
  'AND', 'OR', 'IS', 'IN', 'BETWEEN', 'LIKE', 'ILIKE', 'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'CAST',
  'WINDOW', 'OVER', 'PARTITION', 'FILTER', 'FOR', 'SKIP', 'LOCKED',
  'COUNT', 'SUM', 'AVG', 'MIN', 'MAX', 'COALESCE', 'NULLIF', 'NOW', 'STRING_AGG', 'ARRAY_AGG', 'EXTRACT', 'GENERATE_SERIES', 'CURRENT_DATE', 'CURRENT_TIMESTAMP',
  'TRUE', 'FALSE',
]

/** Hardcoded fallback when /api/aliases is unreachable (private mode, boot race). */
const FALLBACK_ALIASES = [
  { label: 'ssf', template: 'SELECT * FROM ', detail: 'SELECT * FROM …' },
]



/** Extract FROM/JOIN tables (with aliases) from the current statement. */
function scopeTables(stmt: string): Scope['tables'] {
  const out: Scope['tables'] = []
  const re = /(?:FROM|JOIN)\s+((?:"[^"]+"|[\w$]+)(?:\s*\.\s*(?:"[^"]+"|[\w$]+))?)(?:\s+(?:AS\s+)?((?:"[^"]+"|[\w$]+)))?/gi
  let m: RegExpExecArray | null
  while ((m = re.exec(stmt))) {
    const head = m[1].replace(/"/g, '').trim()
    const alias = (m[2] ?? '').replace(/"/g, '').trim()
    if (/^(SELECT|WHERE|GROUP|ORDER|LATERAL|UNNEST)$/i.test(head)) continue
    const parts = head.split('.').map((s) => s.trim()).filter(Boolean)
    const name = parts.pop() ?? ''
    const schema = parts.pop() ?? ''
    if (!name) continue
    out.push({ schema, name, alias: /^(ON|USING|WHERE|GROUP|ORDER|LIMIT|LEFT|RIGHT|FULL|INNER|OUTER|CROSS|JOIN)$/i.test(alias) ? '' : alias })
  }
  return out
}

/** Classify the completion context from the statement text before the cursor. */
export function parseScope(before: string): Scope {
  const stmt = before.split(';').pop() ?? before
  const dot = /([A-Za-z_][\w$]*)\.\s*[\w$]*$/.exec(stmt)
  if (dot) return { kind: 'dot', dot: dot[1], tables: scopeTables(stmt) }
  const tables = scopeTables(stmt)
  const up = stmt.toUpperCase()
  const lastIdx = (re: RegExp) => {
    const g = new RegExp(re.source, re.flags.includes('g') ? re.flags : re.flags + 'g')
    let last = -1
    let m: RegExpExecArray | null
    while ((m = g.exec(up))) last = m.index
    return last
  }
  const fromPos = Math.max(lastIdx(/\bFROM\b/), lastIdx(/\bJOIN\b/), lastIdx(/\bUPDATE\b/), lastIdx(/\bINTO\b/))
  const colPos = Math.max(
    lastIdx(/\bSELECT\b/), lastIdx(/\bWHERE\b/), lastIdx(/\bGROUP\s+BY\b/), lastIdx(/\bORDER\s+BY\b/),
    lastIdx(/\bHAVING\b/), lastIdx(/\bON\b/), lastIdx(/\bSET\b/), lastIdx(/\bVALUES\b/),
  )
  if (/^\s*$/.test(stmt) || /\(\s*$/.test(stmt) || /;\s*$/.test(before)) return { kind: 'keyword', tables }
  if (fromPos >= 0 && fromPos > colPos) return { kind: 'table', tables }
  if (colPos >= 0) return { kind: 'column', tables }
  return fromPos >= 0 ? { kind: 'table', tables } : { kind: 'keyword', tables }
}

function fuzzyScore(label: string, prefix: string): number {
  if (!prefix) return 1
  const l = label.toLowerCase()
  const p = prefix.toLowerCase()
  if (l.startsWith(p)) return 100
  if (l.split(/[_\s]/).some((w) => w.startsWith(p))) return 60
  if (l.includes(p)) return 40
  let li = 0
  for (let pi = 0; pi < p.length; pi++) {
    li = l.indexOf(p[pi], li)
    if (li < 0) return -Infinity
    li++
  }
  return 20
}

function qualifierMatches(dot: string, t: { schema: string; name: string; alias: string }): boolean {
  const d = dot.toLowerCase()
  if (t.alias && t.alias.toLowerCase() === d) return true
  if (t.name.toLowerCase() === d) return true
  if (t.schema && `${t.schema}.${t.name}`.toLowerCase() === d) return true
  return false
}

function resolveDot(snap: SchemaSnapshot, dot: string) {
  const d = dot.toLowerCase()
  const qualified = d.split('.')
  if (qualified.length === 2) {
    const hit = snap.tables.find((t) => t.schema.toLowerCase() === qualified[0] && t.name.toLowerCase() === qualified[1])
    if (hit) return hit
  }
  for (const t of snap.tables) {
    if (t.name.toLowerCase() === d || `${t.schema}.${t.name}`.toLowerCase() === d) return t
  }
  return null
}

/**
 * Typing guard: no popup inside 'string literals' (-- comments ignored when
 * counting quotes), and no auto-popup on an empty prefix (right after a
 * space/newline) unless invoked explicitly (Ctrl+Space) or completing right
 * after a dot (`alias.` still lists columns with no prefix typed yet).
 */
export function completionAllowed(before: string, prefix: string, explicit: boolean, isDot: boolean): boolean {
  const code = before.replace(/--[^\n]*/g, '')
  let inStr = false
  for (let i = 0; i < code.length; i++) {
    if (code[i] === "'") {
      if (inStr && code[i + 1] === "'") {
        i++ // '' is an escaped quote, not a delimiter
        continue
      }
      inStr = !inStr
    }
  }
  if (inStr) return false
  if (!prefix && !explicit && !isDot) return false
  return true
}

/**
 * Build a CodeMirror completion source that reads only from the in-memory
 * snapshot (zero network per keystroke) and ranks scope-aware: prefix >
 * word-boundary > substring > fuzzy, boosted by FROM-scope, alias, FK
 * neighbours and recent use — strictly more signals than Ctrl+K's ILIKE.
 */
export function createCompleteSource(session: string): CompletionSource {
  return (ctx) => {
    const snap = getSnapshotCached(session)
    if (!snap) return null
    const word = ctx.matchBefore(/[\w$]*(\.[\w$]*)?/)
    const before = ctx.state.sliceDoc(0, ctx.pos)
    const scope = parseScope(before)
    // Dot-context: the qualifier is the token before the dot, prefix after it.
    const dotParts = /([\w$]+)\.\s*([\w$]*)$/.exec(before)
    const prefix = (dotParts ? dotParts[2] : word?.text.replace(/^.*\./, '') ?? '').toLowerCase()
    if (!completionAllowed(before, prefix, !!ctx.explicit, scope.kind === 'dot')) return null
    const from = dotParts ? ctx.pos - dotParts[2].length : (word?.from ?? ctx.pos)

    const recent = new Set(getRecent())
    const inScope = new Set(scope.tables.map((t) => `${t.schema}.${t.name}`.toLowerCase()))
    const fkNeighbours = new Set<string>()
    for (const fk of snap.fks) {
      const [sSchema, sTable] = fk.src.split('.')
      const [dSchema, dTable] = fk.dst.split('.')
      if (inScope.has(`${sSchema}.${sTable}`.toLowerCase())) fkNeighbours.add(`${dSchema}.${dTable}`.toLowerCase())
      if (inScope.has(`${dSchema}.${dTable}`.toLowerCase())) fkNeighbours.add(`${sSchema}.${sTable}`.toLowerCase())
    }

    const opts: Completion[] = []
    const push = (c: Completion, base: number, extra = 0) => {
      const s = fuzzyScore(c.label, prefix)
      if (s === -Infinity) return
      opts.push({ ...c, boost: base + s + extra })
    }

    if (scope.kind === 'dot' && scope.dot) {
      const scoped = scope.tables.find((t) => qualifierMatches(scope.dot!, t))
      const target = scoped
        ? snap.tables.find((t) => t.name.toLowerCase() === scoped.name.toLowerCase() && (!scoped.schema || t.schema.toLowerCase() === scoped.schema.toLowerCase()))
        : resolveDot(snap, scope.dot)
      for (const t of snap.tables) {
        if (scoped && (t.name.toLowerCase() !== scoped.name.toLowerCase() || (scoped.schema && t.schema.toLowerCase() !== scoped.schema.toLowerCase()))) continue
        if (!scoped && !target) {
          // Unknown qualifier: fall back to alias names themselves.
          for (const a of scope.tables) push({ label: a.alias, type: 'alias', detail: `${a.schema}.${a.name}` }, -20)
          break
        }
        if (target && t !== target) continue
        for (const c of t.columns) {
          push(
            { label: c.name, type: 'column', detail: `${t.schema}.${t.name} · ${c.type}` },
            40,
            (recent.has(c.name) ? 10 : 0) + (scoped ? 25 : 0),
          )
        }
      }
      return { from, options: opts.sort((a, b) => (b.boost ?? 0) - (a.boost ?? 0)).slice(0, 50) }
    }

    if (scope.kind === 'table') {
      for (const t of snap.tables) {
        const key = `${t.schema}.${t.name}`.toLowerCase()
        push(
          {
            label: t.name, type: 'table', detail: t.schema,
            apply: t.schema && t.schema !== 'public' ? `${t.schema}.${t.name}` : undefined,
          },
          30,
          (inScope.has(key) ? -15 : 0) + (fkNeighbours.has(key) ? 15 : 0) + (recent.has(t.name) ? 10 : 0),
        )
      }
      for (const k of KEYWORDS) {
        if (/^(FROM|JOIN|WHERE|LIMIT)$/.test(k)) push({ label: k, type: 'keyword' }, -30)
      }
      return { from, options: opts.sort((a, b) => (b.boost ?? 0) - (a.boost ?? 0)).slice(0, 50) }
    }

    // Column context (SELECT/WHERE/ON/...): scoped columns first.
    const seenCols = new Set<string>()
    for (const st of scope.tables) {
      const t = snap.tables.find((x) => x.name.toLowerCase() === st.name.toLowerCase() && (!st.schema || x.schema.toLowerCase() === st.schema.toLowerCase()))
      if (!t) continue
      const tag = st.alias || t.name
      for (const c of t.columns) {
        seenCols.add(c.name.toLowerCase())
        push(
          {
            label: c.name, type: 'column', detail: `${tag} · ${c.type}`,
            apply: st.alias ? `${st.alias}.${c.name}` : undefined,
          },
          50,
          (recent.has(c.name) ? 10 : 0) + (st.alias ? 5 : 0),
        )
      }
      if (st.alias) push({ label: st.alias, type: 'alias', detail: `${st.schema}.${st.name}` }, 20)
    }
    for (const t of snap.tables) {
      const key = `${t.schema}.${t.name}`.toLowerCase()
      push({ label: t.name, type: 'table', detail: t.schema }, -10, fkNeighbours.has(key) ? 15 : 0)
      if (opts.length > 400) break
      for (const c of t.columns) {
        if (seenCols.has(c.name.toLowerCase())) continue
        push({ label: c.name, type: 'column', detail: `${t.schema}.${t.name}` }, -25, recent.has(c.name) ? 10 : 0)
        if (opts.length > 900) break
      }
    }
    for (const f of snap.funcs) {
      push({ label: f.name, type: 'function', detail: `${f.schema}${f.args}` }, -5)
    }
    for (const k of KEYWORDS) push({ label: k, type: 'keyword' }, -40)
    for (const a of getAliasesCached()) {
      if (!prefix || a.trigger.startsWith(prefix)) {
        opts.push({ label: a.trigger, type: 'snippet', detail: a.detail || a.expansion.slice(0, 60), apply: a.expansion, boost: 95 })
      }
    }
    if (!getAliasesCached().length) {
      for (const s of FALLBACK_ALIASES) {
        if (!prefix || s.label.startsWith(prefix)) opts.push({ label: s.label, type: 'snippet', detail: s.detail, apply: s.template, boost: 95 })
      }
    }
    if (scope.kind === 'keyword') {
      for (const k of KEYWORDS) push({ label: k, type: 'keyword' }, 10)
    }
    return { from, options: opts.sort((a, b) => (b.boost ?? 0) - (a.boost ?? 0)).slice(0, 50) };
  }
}
