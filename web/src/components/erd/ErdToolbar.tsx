import { LayoutGrid, Maximize2, RefreshCw, Search } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../ui/select'

export function ErdToolbar(props: {
  schema: string
  schemas: string[]
  onSchema: (s: string) => void
  onReload: () => void
  onFit: () => void
  onResetLayout: () => void
  query: string
  onQueryChange: (q: string) => void
  onQuerySubmit: () => void
  tableCount: number
  relationCount: number
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <b className="text-sm">ERD — {props.schema}</b>
      <Select value={props.schema} onValueChange={props.onSchema}>
        <SelectTrigger className="w-[160px]" aria-label="Schema">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {props.schemas.map((s) => (
            <SelectItem key={s} value={s}>
              {s}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button size="sm" variant="secondary" onClick={props.onReload} aria-label="Reload diagram">
        <RefreshCw className="h-3.5 w-3.5" />
      </Button>
      <Button size="sm" variant="secondary" onClick={props.onFit} aria-label="Fit view">
        <Maximize2 className="h-3.5 w-3.5" />
      </Button>
      <Button size="sm" variant="secondary" onClick={props.onResetLayout} aria-label="Reset layout">
        <LayoutGrid className="h-3.5 w-3.5" />
      </Button>
      <div className="relative">
        <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={props.query}
          onChange={(e) => props.onQueryChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') props.onQuerySubmit()
          }}
          placeholder="Filter tables…"
          aria-label="Filter tables"
          className="h-7 w-[160px] pl-7 text-xs"
        />
      </div>
      <span className="ml-auto text-[12px] text-muted-foreground">
        {props.tableCount} table{props.tableCount === 1 ? '' : 's'} · {props.relationCount}{' '}
        relation{props.relationCount === 1 ? '' : 's'}
      </span>
    </div>
  )
}
