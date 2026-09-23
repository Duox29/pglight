import { Copy, Download, Image, LayoutGrid, Maximize2, Network, RefreshCw, Search } from 'lucide-react'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { Switch } from '../ui/switch'
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
  onAutoLayout?: () => void
  onResetLayout: () => void
  query: string
  onQueryChange: (q: string) => void
  onQuerySubmit: () => void
  tableCount: number
  relationCount: number
  focusDepth: string
  onFocusDepth: (depth: string) => void
  focusEnabled: boolean
  onFocusEnabled: (enabled: boolean) => void
  canFocus: boolean
  hideColumns: boolean
  onHideColumns: (hide: boolean) => void
  keysOnly: boolean
  onKeysOnly: (keysOnly: boolean) => void
  onExportSvg: () => void
  onExportPng: () => void
  onCopyImage: () => void
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
      {props.onAutoLayout && (
        <Button size="sm" variant="secondary" onClick={props.onAutoLayout} aria-label="Auto arrange">
          <Network className="h-3.5 w-3.5" />
        </Button>
      )}
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
      <label className="flex items-center gap-1 text-[11px] text-muted-foreground"><Switch checked={props.focusEnabled} disabled={!props.canFocus} onCheckedChange={props.onFocusEnabled} aria-label="Hide unrelated tables" />Related</label>
      <Select value={props.focusDepth} onValueChange={props.onFocusDepth}>
        <SelectTrigger className="h-7 w-[78px]" aria-label="Related depth"><SelectValue /></SelectTrigger>
        <SelectContent><SelectItem value="1">Depth 1</SelectItem><SelectItem value="2">Depth 2</SelectItem><SelectItem value="3">Depth 3</SelectItem><SelectItem value="all">All</SelectItem></SelectContent>
      </Select>
      <label className="flex items-center gap-1 text-[11px] text-muted-foreground"><Switch checked={props.hideColumns} onCheckedChange={props.onHideColumns} aria-label="Hide columns" />Hide columns</label>
      <label className="flex items-center gap-1 text-[11px] text-muted-foreground"><Switch checked={props.keysOnly} disabled={props.hideColumns} onCheckedChange={props.onKeysOnly} aria-label="Show primary and foreign keys only" />PK/FK</label>
      <Button size="sm" variant="outline" onClick={props.onExportSvg} aria-label="Export SVG"><Download className="h-3.5 w-3.5" />SVG</Button>
      <Button size="sm" variant="outline" onClick={props.onExportPng} aria-label="Export PNG"><Image className="h-3.5 w-3.5" />PNG</Button>
      <Button size="sm" variant="outline" onClick={props.onCopyImage} aria-label="Copy diagram image"><Copy className="h-3.5 w-3.5" />Copy</Button>
      <span className="ml-auto text-[12px] text-muted-foreground">
        {props.tableCount} table{props.tableCount === 1 ? '' : 's'} · {props.relationCount}{' '}
        relation{props.relationCount === 1 ? '' : 's'}
      </span>
    </div>
  )
}
