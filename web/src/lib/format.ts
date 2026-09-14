/** Quote a PostgreSQL identifier (table, column, schema). `"a""b"` escaping. */
export function quoteIdent(s: string): string {
  return `"${s.replace(/"/g, '""')}"`
}

/** Quote schema.table with each part identifier-quoted. Pass-through when dotted already. */
export function quoteQualified(schema: string, table: string): string {
  if (!schema) return table.includes('.') ? table : quoteIdent(table)
  return `${quoteIdent(schema)}.${quoteIdent(table)}`
}

/** Serialize a driver value to a SQL literal. Objects/arrays use JSON. */
export function quoteLiteral(v: unknown, type?: string): string {
  if (v == null) return 'NULL'
  if (typeof v === 'boolean') return v ? 'TRUE' : 'FALSE'
  if (typeof v === 'number') return Number.isFinite(v) ? String(v) : 'NULL'
  if (typeof v === 'bigint') return String(v)
  if (typeof v === 'object') {
    const t = (type ?? '').toLowerCase()
    if (Array.isArray(v) && (t.includes('int') || t.includes('numeric') || t.includes('float') || t.includes('double') || t.includes('real') || t.includes('decimal'))) {
      return `ARRAY[${v.map((x) => quoteLiteral(x)).join(', ')}]`
    }
    return `'${JSON.stringify(v).replace(/'/g, "''")}'`
  }
  return `'${String(v).replace(/'/g, "''")}'`
}

interface SqlTok {
  text: string
  code: boolean
}

/** Split SQL into code vs string/identifier/comment spans so the formatter only touches real syntax. */
function lexSql(s: string): SqlTok[] {
  const out: SqlTok[] = []
  let buf = ''
  let i = 0
  const flush = () => {
    if (buf) out.push({ text: buf, code: true })
    buf = ''
  }
  while (i < s.length) {
    const c = s[i]
    const next = s[i + 1]
    if (c === '-' && next === '-') {
      flush()
      let j = i + 2
      while (j < s.length && s[j] !== '\n') j++
      out.push({ text: s.slice(i, j), code: false })
      i = j
    } else if (c === '/' && next === '*') {
      flush()
      const end = s.indexOf('*/', i + 2)
      const j = end < 0 ? s.length : end + 2
      out.push({ text: s.slice(i, j), code: false })
      i = j
    } else if (c === "'") {
      flush()
      let j = i + 1
      while (j < s.length) {
        if (s[j] === "'") {
          if (s[j + 1] === "'") j += 2
          else {
            j++
            break
          }
        } else j++
      }
      out.push({ text: s.slice(i, j), code: false })
      i = j
    } else if (c === '"') {
      flush()
      let j = i + 1
      while (j < s.length) {
        if (s[j] === '"') {
          if (s[j + 1] === '"') j += 2
          else {
            j++
            break
          }
        } else j++
      }
      out.push({ text: s.slice(i, j), code: false })
      i = j
    } else if (c === '$') {
      const tag = /^\$[A-Za-z_][\w$]*\$/.exec(s.slice(i))
      if (tag) {
        flush()
        const close = s.indexOf(tag[0], i + tag[0].length)
        const j = close < 0 ? s.length : close + tag[0].length
        out.push({ text: s.slice(i, j), code: false })
        i = j
      } else {
        buf += c
        i++
      }
    } else {
      buf += c
      i++
    }
  }
  flush()
  return out
}

export function formatSqlText(s: string): string {
  const kws = [
    'SELECT', 'FROM', 'WHERE', 'LEFT JOIN', 'RIGHT JOIN', 'FULL JOIN', 'INNER JOIN',
    'JOIN', 'ORDER BY', 'GROUP BY', 'HAVING', 'LIMIT', 'OFFSET', 'INSERT INTO',
    'VALUES', 'UPDATE', 'SET', 'DELETE FROM', 'RETURNING', 'ON CONFLICT',
    'UNION ALL', 'UNION', 'EXCEPT', 'WITH', 'BEGIN', 'COMMIT', 'ROLLBACK',
    'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE', 'CREATE INDEX', 'VACUUM', 'ANALYZE', 'EXPLAIN',
  ]
  const fmt = (code: string): string => {
    let out = ' ' + code.replace(/\s+/g, ' ').trim()
    const sorted = [...kws].sort((a, b) => b.length - a.length)
    for (const k of sorted) {
      const re = new RegExp('\\s' + k.replace(/ /g, '\\s+') + '\\s', 'gi')
      out = out.replace(re, '\n' + k + ' ')
    }
    out = out.replace(
      /\b(select|from|where|join|left|right|full|inner|outer|on|order|group|having|limit|offset|insert|into|values|update|set|delete|create|table|alter|drop|index|and|or|not|null|as|distinct|count|sum|avg|coalesce|now|begin|commit|rollback|with|returning|union|except|explain|analyze|vacuum|primary|key|references|default|constraint)\b/gi,
      (m) => m.toUpperCase(),
    )
    return out.trim().replace(/\n+/g, '\n')
  }
  return lexSql(s)
    .map((t) => (t.code ? fmt(t.text) : ` ${t.text} `))
    .join('')
    .replace(/[ \t]+/g, ' ')
    .replace(/ *\n */g, '\n')
    .trim()
    .replace(/\n+/g, '\n')
}
export function detectDelimiter(text: string): ',' | '\t' | ';' {
  const first = text.split(/\r?\n/, 1)[0] ?? ''
  let inQ = false
  const counts: Record<string, number> = { ',': 0, '\t': 0, ';': 0 }
  for (let i = 0; i < first.length; i++) {
    const c = first[i]
    if (c === '"') {
      if (inQ && first[i + 1] === '"') i++
      else inQ = !inQ
    } else if (!inQ && (c === ',' || c === '\t' || c === ';')) counts[c]++
  }
  if (counts['\t'] >= counts[','] && counts['\t'] >= counts[';'] && counts['\t'] > 0) return '\t'
  if (counts[';'] > counts[','] && counts[';'] > 0) return ';'
  return ','
}

export function parseDelimited(text: string, delim: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cur = ''
  let quoted = false
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          cur += '"'
          i++
        } else quoted = false
      } else cur += c
    } else if (c === '"') quoted = true
    else if (c === delim) {
      row.push(cur)
      cur = ''
    } else if (c === '\n') {
      row.push(cur)
      rows.push(row)
      row = []
      cur = ''
    } else if (c !== '\r') cur += c
  }
  row.push(cur)
  rows.push(row)
  return rows.filter((r) => !(r.length === 1 && r[0] === ''))
}

export function parseCSV(text: string, delim?: string): string[][] {
  return parseDelimited(text, delim ?? detectDelimiter(text))
}

export function download(text: string, name: string, type: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([text], { type }))
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 5000)
}

export function resultToCSV(columns: string[], rows: unknown[][]): string {
  const q = (v: unknown) => {
    if (v == null) return ''
    if (typeof v === 'object') return `"${JSON.stringify(v).replace(/"/g, '""')}"`
    return `"${String(v).replace(/"/g, '""')}"`
  }
  return [columns.map(q).join(','), ...rows.map((r) => r.map(q).join(','))].join('\n')
}

export function resultToJSON(columns: string[], rows: unknown[][]): string {
  return JSON.stringify(rows.map((r) => Object.fromEntries(columns.map((c, i) => [c, r[i]]))), null, 2)
}

export function resultToInserts(columns: string[], rows: unknown[][], table: string, types?: string[]): string {
  if (!rows.length) return '-- no rows'
  const cols = columns.map(quoteIdent).join(', ')
  return rows.map((r) => `INSERT INTO ${table} (${cols}) VALUES (${r.map((v, i) => quoteLiteral(v, types?.[i])).join(', ')});`).join('\n')
}

export function fmtPlanText(node: Record<string, unknown>, depth: number): string {
  const pad = '  '.repeat(depth)
  const kids = (node['Plans'] as Record<string, unknown>[] | undefined) ?? []
  const rows = (node['Actual Rows'] as number | undefined) ?? (node['Plan Rows'] as number | undefined)
  const rel = node['Relation Name'] ? ` on ${node['Relation Name']}` : ''
  const time = node['Actual Total Time'] != null ? ` time=${node['Actual Total Time']}ms` : ''
  return (
    `${pad}→ ${node['Node Type']}${rel} cost=${node['Total Cost']} rows=${rows}${time}\n` +
    kids.map((k) => fmtPlanText(k, depth + 1)).join('')
  )
}
