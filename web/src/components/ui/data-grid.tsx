import { Fragment } from 'react'
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'
import { EmptyNote } from './feedback'
import { Tip } from './tooltip'
import { Button } from './button'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from './context-menu'
import { useGridSelection } from '@/hooks/useGridSelection'
import { download, resultToCSV, resultToJSON } from '@/lib/format'

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

function copyText(text: string) {
  try {
    const p = navigator.clipboard?.writeText(text)
    if (p) p.catch(() => {})
  } catch {
    /* clipboard unavailable — menu still closes normally */
  }
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
  selectable,
}: {
  data: GridData | null | undefined
  onSort?: (colIndex: number) => void
  sort?: { column: number; direction: 'asc' | 'desc' } | null
  /** Legacy click-to-copy. Ignored when `selectable` is on (selection + context menu take over). */
  onCellClick?: (value: unknown, col: string) => void
  cellClassName?: (value: unknown) => string | undefined
  /** Opt-in row selection + right-click menu (single/bulk select, copy/export).
      Off by default so read-only panels (logs, stats, browser) stay plain. */
  selectable?: boolean
}) {
  const pageRows = data?.rows ?? []
  // Always called (cheap) so hook order stays stable; only wired when selectable.
  const gridSel = useGridSelection(pageRows, (r, i) => JSON.stringify([i, r]))
  if (!data || !data.columns) return <EmptyNote text="No data" />
  const selRows = selectable ? pageRows.filter((r, i) => gridSel.sel.has(JSON.stringify([i, r]))) : []

  const copySelectedCSV = () => {
    if (!selRows.length) return
    copyText(resultToCSV(data.columns, selRows))
  }
  const exportSelected = (fmt: 'csv' | 'json') => {
    if (!selRows.length) return
    if (fmt === 'csv') download(resultToCSV(data.columns, selRows), 'selected-rows.csv', 'text/csv')
    else download(resultToJSON(data.columns, selRows), 'selected-rows.json', 'application/json')
  }

  const grid = (
    <Table>
      <TableHeader>
        <TableRow
          className="hover:bg-transparent"
          onContextMenu={selectable ? () => gridSel.setCtxCell(null) : undefined}
        >
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
        {(data.rows ?? []).map((r, ri) => {
          const selected = selectable && gridSel.sel.has(JSON.stringify([ri, r]))
          return (
            <TableRow
              key={ri}
              className={ri % 2 ? 'bg-muted/20' : undefined}
              data-state={selected ? 'selected' : undefined}
            >
              {r.map((c, ci) => {
                const t = data.types?.[ci]
                const json = c != null && isJsonType(c, t)
                const full = c == null ? 'NULL' : json ? String(c).slice(0, 2000) : String(c).slice(0, 500)
                const cell = (
                  <TableCell
                    onClick={
                      selectable
                        ? (e) => gridSel.handleCellClick(e, ri, r)
                        : onCellClick
                          ? () => onCellClick(c, data.columns[ci])
                          : undefined
                    }
                    // Record the cell for "Copy cell value" but NEVER
                    // preventDefault here — Radix skips handleOpen when
                    // defaultPrevented, which would kill the menu.
                    onContextMenu={
                      selectable
                        ? () => {
                            gridSel.handleCellContextMenu(c, data.columns[ci])
                            gridSel.handleRowContextMenu(ri, r)
                          }
                        : undefined
                    }
                    className={`${c == null ? 'italic text-muted-foreground' : ''} ${cellClassName?.(c) ?? ''} ${selectable ? '' : onCellClick ? 'cursor-pointer' : ''} ${isNumericType(t) ? 'text-right font-mono' : ''} ${isBoolType(t) ? 'text-center' : ''} ${json ? 'font-mono text-[11px]' : ''}`}
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
          )
        })}
      </TableBody>
    </Table>
  )

  if (!selectable) return grid
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div>
          {selRows.length > 0 && (
            <div className="mb-1 flex items-center gap-2 text-[12px] text-muted-foreground">
              <span>
                <b className="font-medium text-foreground">{selRows.length} selected</b>
              </span>
              <Button size="sm" variant="ghost" onClick={gridSel.clear}>
                Clear
              </Button>
            </div>
          )}
          {grid}
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuLabel>{selRows.length ? `${selRows.length} row${selRows.length === 1 ? '' : 's'} selected` : 'No rows selected'}</ContextMenuLabel>
        <ContextMenuItem
          disabled={gridSel.ctxCell == null || gridSel.ctxCell.value == null}
          onSelect={() => {
            if (gridSel.ctxCell?.value != null) copyText(String(gridSel.ctxCell.value))
          }}
        >
          Copy cell value{gridSel.ctxCell ? ` (${gridSel.ctxCell.col})` : ''}
        </ContextMenuItem>
        <ContextMenuItem disabled={!selRows.length} onSelect={copySelectedCSV}>
          Copy {selRows.length ? `${selRows.length} row${selRows.length === 1 ? '' : 's'}` : 'rows'} (CSV)
        </ContextMenuItem>
        <ContextMenuSub>
          <ContextMenuSubTrigger disabled={!selRows.length}>Export selected</ContextMenuSubTrigger>
          <ContextMenuSubContent>
            <ContextMenuItem onSelect={() => exportSelected('csv')}>CSV</ContextMenuItem>
            <ContextMenuItem onSelect={() => exportSelected('json')}>JSON</ContextMenuItem>
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSeparator />
        <ContextMenuItem disabled={!selRows.length} onSelect={gridSel.clear}>
          Clear selection
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}
