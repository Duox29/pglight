import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'
import { EmptyNote } from './feedback'

export interface GridData {
  columns: string[]
  rows: unknown[][]
}

/** Prebuilt result grid: sticky header, truncated cells, NULL styling. */
export function DataGrid({
  data,
  onSort,
  onCellClick,
  cellClassName,
}: {
  data: GridData | null | undefined
  onSort?: (colIndex: number) => void
  onCellClick?: (value: unknown, col: string) => void
  cellClassName?: (value: unknown) => string | undefined
}) {
  if (!data || !data.columns) return <EmptyNote text="No data" />
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {data.columns.map((c, i) => (
            <TableHead
              key={i}
              onClick={onSort ? () => onSort(i) : undefined}
              className={onSort ? 'cursor-pointer select-none hover:text-foreground' : undefined}
              title={onSort ? 'Click to sort' : c}
            >
              {c}
              {onSort ? ' ⇅' : ''}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {(data.rows ?? []).map((r, ri) => (
          <TableRow key={ri}>
            {r.map((c, ci) => (
              <TableCell
                key={ci}
                title={c == null ? 'NULL' : String(c)}
                onClick={onCellClick ? () => onCellClick(c, data.columns[ci]) : undefined}
                className={`${c == null ? 'italic text-muted-foreground' : ''} ${cellClassName?.(c) ?? ''} ${onCellClick ? 'cursor-pointer' : ''}`}
              >
                {c == null ? 'NULL' : String(c).slice(0, 300)}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
