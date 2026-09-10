/** Small SQL formatter (keyword uppercasing + clause newlines). No deps. */
export function formatSqlText(s: string): string {
  const kws = [
    'SELECT', 'FROM', 'WHERE', 'LEFT JOIN', 'RIGHT JOIN', 'FULL JOIN', 'INNER JOIN',
    'JOIN', 'ORDER BY', 'GROUP BY', 'HAVING', 'LIMIT', 'OFFSET', 'INSERT INTO',
    'VALUES', 'UPDATE', 'SET', 'DELETE FROM', 'RETURNING', 'ON CONFLICT',
    'UNION ALL', 'UNION', 'EXCEPT', 'WITH', 'BEGIN', 'COMMIT', 'ROLLBACK',
    'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE', 'CREATE INDEX', 'VACUUM', 'ANALYZE', 'EXPLAIN',
  ]
  let out = ' ' + s.replace(/\s+/g, ' ').trim()
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

export function parseCSV(text: string): string[][] {
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
    else if (c === ',') {
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

export function download(text: string, name: string, type: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob([text], { type }))
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 5000)
}

export function resultToCSV(columns: string[], rows: unknown[][]): string {
  const q = (v: unknown) => `"${String(v ?? '').replace(/"/g, '""')}"`
  return [columns.join(','), ...rows.map((r) => r.map(q).join(','))].join('\n')
}

export function resultToJSON(columns: string[], rows: unknown[][]): string {
  return JSON.stringify(rows.map((r) => Object.fromEntries(columns.map((c, i) => [c, r[i]]))), null, 2)
}

export function resultToInserts(columns: string[], rows: unknown[][], table: string): string {
  const q = (v: unknown) =>
    v == null ? 'NULL' : typeof v === 'number' ? String(v) : `'${String(v).replace(/'/g, "''")}'`
  if (!rows.length) return '-- no rows'
  return rows.map((r) => `INSERT INTO ${table} (${columns.join(', ')}) VALUES (${r.map(q).join(', ')});`).join('\n')
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
