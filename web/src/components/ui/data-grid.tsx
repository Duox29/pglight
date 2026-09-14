import { Fragment } from 'react'
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'
import { EmptyNote } from './feedback'
import { Tip } from './tooltip'

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

function toBool(v: unknown): boolean {
  return v === true || v === 't' || v === 'true' || v === 'TRUE' || v === '1' || v === 1
}

/** Type-aware cell comparator: numerics numerically, bools false<true, NULLs last. */
export function compareGridValues(a: unknown, b: unknown, type?: string): number {
  if (a == null && b == null) return 0
  if (a == null) return 1
  if (b == null) return -1
  if (isNumericType(type)) {
    const na = Number(a)
    const nb = Number(b)
    if (Number.isFinite(na) && Number.isFinite(nb) && na !== nb) return na - nb
    if (Number.isFinite(na) !== Number.isFinite(nb)) return Number.isFinite(na) ? -1 : 1
  } else if (isBoolType(type)) {
    const ba = toBool(a)
    const bb = toBool(b)
    if (ba !== bb) return Number(ba) - Number(bb)
    return 0
  }
  return String(a).localeCompare(String(b), undefined, { numeric: true, sensitivity: 'base' })
}

/** Sort grid rows by column using the column's Postgres type. NULLs always last. */
export function sortGridRows(rows: unknown[][], sort: { column: number; direction: 'asc' | 'desc' }, types?: string[]): unknown[][] {
  const cmp = (a: unknown[], b: unknown[]) => compareGridValues(a[sort.column], b[sort.column], types?.[sort.column])
  return [...rows].sort((a, b) => (sort.direction === 'asc' ? cmp(a, b) : -cmp(a, b)))
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
            <Tip key={i} content={data.types?.[i] ? `${c} · ${data.types[i]}` : c}>
              <TableHead
                onClick={onSort ? () => onSort(i) : undefined}
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
            </Tip>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {(data.rows ?? []).map((r, ri) => (
          <TableRow key={ri} className={ri % 2 ? 'bg-muted/20' : undefined}>
            {r.map((c, ci) => {
              const t = data.types?.[ci]
              const json = c != null && isJsonType(c, t)
              const full = c == null ? 'NULL' : json ? String(c).slice(0, 2000) : String(c).slice(0, 500)
              const cell = (
                <TableCell
                  onClick={onCellClick ? () => onCellClick(c, data.columns[ci]) : undefined}
                  className={`${c == null ? 'italic text-muted-foreground' : ''} ${cellClassName?.(c) ?? ''} ${onCellClick ? 'cursor-pointer' : ''} ${isNumericType(t) ? 'text-right font-mono' : ''} ${isBoolType(t) ? 'text-center' : ''} ${json ? 'font-mono text-[11px]' : ''}`}
                >
                  {c == null ? 'NULL' : isBoolType(t) ? (c === true || c === 't' || c === 'true' ? 'true' : c === false || c === 'f' || c === 'false' ? 'false' : String(c)) : String(c).slice(0, 300)}
                </TableCell>
              )
              return full.length > 40 ? (
                <Tip key={ci} content={<span className="break-all font-mono text-[11px]">{full}</span>}>
                  {cell}
                </Tip>
              ) : (
                <Fragment key={ci}>{cell}</Fragment>
              )
            })}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
