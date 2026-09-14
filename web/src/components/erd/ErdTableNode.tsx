import { memo } from 'react'
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import { Copy, Crosshair, KeyRound, Link2, Table2 } from 'lucide-react'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from '../ui/context-menu'
import { Tip } from '../ui/tooltip'
import { cn } from '@/lib/utils'
import { ERD_MAX_COLUMNS, dstHandle, srcHandle, type ErdTable } from './erdMapper'
import { ERD_NODE_WIDTH } from './erdLayout'

export interface ErdTableNodeData extends Record<string, unknown> {
  table: ErdTable
  degree: number
  dimmed: boolean
  onOpenTable: (schema: string, table: string) => void
  onCopyName: (table: string) => void
  onFocusRelated: (table: string) => void
}
export type ErdTableNodeT = Node<ErdTableNodeData, 'erdTable'>

const ROW_H = 20

function ErdTableNodeInner({ data, selected }: NodeProps<ErdTableNodeT>) {
  const { table, degree, dimmed } = data
  const shown = table.columns.slice(0, ERD_MAX_COLUMNS)
  const hidden = table.columns.length - shown.length
  const open = () => data.onOpenTable(table.schema, table.name)

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          style={{ width: ERD_NODE_WIDTH }}
          className={cn(
            'overflow-hidden rounded-lg border bg-[#13161c] text-left shadow-sm transition-colors',
            selected
              ? 'border-[#1f6feb] shadow-[0_0_0_1px_#1f6feb]'
              : 'border-[#2a2f3a] hover:border-[#3a4152] hover:shadow-lg',
            dimmed && 'opacity-40',
          )}
        >
          <button
            type="button"
            onClick={open}
            onDoubleClick={open}
            aria-label={`Open table ${table.schema}.${table.name}`}
            className="flex h-14 w-full flex-col justify-center gap-0.5 px-2.5 text-left focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <span className="flex w-full items-baseline gap-2">
              <span className="flex-1 truncate text-[12px] font-semibold text-foreground">
                {table.name}
              </span>
              <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
                {degree} rel{degree === 1 ? '' : 's'}
              </span>
            </span>
            <span className="truncate text-[10px] text-muted-foreground">{table.schema}</span>
          </button>
          <div className="border-t border-[#232936]" />
          {shown.length === 0 && (
            <div className="flex h-5 items-center px-2.5 text-[11px] italic text-muted-foreground">
              no columns
            </div>
          )}
          {shown.map((c) => (
            <Tip key={c.name} content={`${c.name}${c.type ? ` ${c.type}` : ''}${c.pk ? ' PK' : ''}${c.fk ? ' FK' : ''}`}>
              <div className="relative flex h-5 items-center gap-1.5 px-2.5 text-[12px]">
              {c.pk && <KeyRound className="h-3 w-3 shrink-0 text-amber-400/90" aria-label="primary key" />}
              {c.fk && <Link2 className="h-3 w-3 shrink-0 text-sky-400/90" aria-label="foreign key" />}
              {!c.pk && !c.fk && <span className="w-3 shrink-0" />}
              <span className={cn('flex-1 truncate', c.pk && 'font-medium text-foreground')}>
                {c.name}
              </span>
              {c.type && (
                <span className="max-w-[90px] shrink-0 truncate text-[11px] text-muted-foreground">
                  {c.type}
                </span>
              )}
              {table.ports[c.name] && (
                <>
                  <Handle
                    type="target"
                    position={Position.Left}
                    id={dstHandle(c.name)}
                    isConnectable={false}
                    style={{ top: ROW_H / 2 }}
                    className="!h-2 !w-2 !border !border-[#0b0d11] !bg-[#1f6feb]"
                  />
                  <Handle
                    type="source"
                    position={Position.Right}
                    id={srcHandle(c.name)}
                    isConnectable={false}
                    style={{ top: ROW_H / 2 }}
                    className="!h-2 !w-2 !border !border-[#0b0d11] !bg-[#1f6feb]"
                  />
                </>
              )}
              </div>
            </Tip>
          ))}
          {hidden > 0 && (
            <div className="flex h-5 items-center px-2.5 text-[11px] text-muted-foreground">
              +{hidden} more column{hidden === 1 ? '' : 's'}
            </div>
          )}
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={open}>
          <Table2 className="mr-2 h-3.5 w-3.5" /> Open table
        </ContextMenuItem>
        <ContextMenuItem onClick={() => data.onCopyName(table.name)}>
          <Copy className="mr-2 h-3.5 w-3.5" /> Copy qualified name
        </ContextMenuItem>
        <ContextMenuItem onClick={() => data.onFocusRelated(table.name)}>
          <Crosshair className="mr-2 h-3.5 w-3.5" /> Focus related tables
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

const ErdTableNode = memo(ErdTableNodeInner)
export default ErdTableNode
