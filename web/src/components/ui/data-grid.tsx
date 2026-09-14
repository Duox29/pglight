import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'
import { EmptyNote } from './feedback'

export interface GridData {
  columns: string[]
  rows: unknown[][]
  /** Postgres type OIDs parallel to columns (from /api/query `types`, e.g. "23"=int4, "16"=bool, "114"=json). */
  types?: string[]
}

function isNumericType(t?: string) {
  if (!t) return false
  // Backend sends pg OID numbers ("23") — /api/query `types` are DataTypeOID
  // strings, not names. Match both: 20/21/23/26 int, 700/701 float,
  // 790/791 money, 1700 numeric, 2950/2951 uuid-adjacent excluded.
  if (/^(20|21|23|26|27|28|29|700|701|790|791|1700|2981|2982)$/.test(t)) return true
  return /int|numeric|decimal|float|double|real|money|serial|bigint|smallint/i.test(t)
}

function isBoolType(t?: string) {
  return t === '16' || (!!t && /bool/i.test(t))
}

function isJsonType(v: unknown, t?: string) {
  if (t === '114' || t === '199' || (t && /json/i.test(t))) return true
  if (typeof v !== 'string') return false
  const s = v.trim()
  return (s.startsWith('{') && s.endsWith('}')) || (s.startsWith('[') && s.endsWith(']'))
}
/** Prebuilt result grid: sticky header, truncated cells, NULL styling. */
export function DataGrid({
  data,
  onSort,
  sort,
  onCellClick,
  cellClassName,
}: {
  data: GridData | null | undefined
  onSort?: (colIndex: number) => void
  sort?: { column: number; direction: 'asc' | 'desc' } | null
  onCellClick?: (value: unknown, col: string) => void
  cellClassName?: (value: unknown) => string | undefined
}) {
  if (!data || !data.columns) return <EmptyNote text="No data" />
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          {data.columns.map((c, i) => (
            <TableHead
              key={i}
              onClick={onSort ? () => onSort(i) : undefined}
              title={data.types?.[i] ? `${c} · ${data.types[i]}` : c}
              className={`${onSort ? 'cursor-pointer select-none hover:text-foreground' : ''} ${sort?.column === i ? 'text-foreground' : ''} ${isNumericType(data.types?.[i]) ? 'text-right' : ''}`}
            >
              <span className="inline-flex items-center gap-1">
                {c}
                {data.types?.[i] && <span className="font-normal opacity-60">{data.types[i]}</span>}
                {onSort &&
                  (sort?.column === i ? (
                    sort.direction === 'asc' ? (
                      <ArrowUp className="h-3 w-3" />
                    ) : (
                      <ArrowDown className="h-3 w-3" />
                    )
                  ) : (
                    <ChevronsUpDown className="h-3 w-3 opacity-40" />
                  ))}
              </span>
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {(data.rows ?? []).map((r, ri) => (
          <TableRow key={ri} className={ri % 2 ? 'bg-muted/20' : undefined}>
            {r.map((c, ci) => {
              const t = data.types?.[ci]
              const json = c != null && isJsonType(c, t)
              return (
                <TableCell
                  key={ci}
                  onClick={onCellClick ? () => onCellClick(c, data.columns[ci]) : undefined}
                  title={c == null ? 'NULL' : json ? String(c).slice(0, 2000) : String(c).slice(0, 500)}
                  className={`${c == null ? 'italic text-muted-foreground' : ''} ${cellClassName?.(c) ?? ''} ${onCellClick ? 'cursor-pointer' : ''} ${isNumericType(t) ? 'text-right font-mono' : ''} ${isBoolType(t) ? 'text-center' : ''} ${json ? 'font-mono text-[11px]' : ''}`}
                >
                  {c == null ? 'NULL' : isBoolType(t) ? (c === true || c === 't' || c === 'true' ? 'true' : c === false || c === 'f' || c === 'false' ? 'false' : String(c)) : String(c).slice(0, 300)}
                </TableCell>
              )
            })}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
