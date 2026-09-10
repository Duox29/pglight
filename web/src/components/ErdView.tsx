import { Button } from './ui/button'
import { EmptyNote } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import type { ErdTabT } from '@/types'

export function ErdView(props: {
  tab: ErdTabT
  schemas: string[]
  onSchema: (s: string) => void
  onReload: () => void
  onOpenTable: (schema: string, table: string) => void
}) {
  const { tab: t } = props
  const nodes = t.data?.nodes ?? []
  const edges = t.data?.edges ?? []
  const W = 170
  const H = 56
  const perRow = Math.max(2, Math.ceil(Math.sqrt(Math.max(1, nodes.length) * 1.6)))
  const pos: Record<string, { x: number; y: number }> = {}
  nodes.forEach((n, i) => {
    pos[n] = { x: (i % perRow) * (W + 40) + 10, y: Math.floor(i / perRow) * (H + 50) + 10 }
  })
  const height = Math.ceil(nodes.length / perRow) * (H + 50) + 20
  const width = perRow * (W + 40) + 20

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <b className="text-sm">ERD — {t.schema}</b>
        <Select value={t.schema} onValueChange={props.onSchema}>
          <SelectTrigger className="w-[160px]">
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
        <Button size="sm" variant="secondary" onClick={props.onReload}>
          ↻
        </Button>
        <span className="text-[12px] text-muted-foreground">click a table to open</span>
      </div>
      {!t.data ? (
        <EmptyNote text="Loading…" />
      ) : !nodes.length ? (
        <EmptyNote text="No tables in this schema" />
      ) : (
        <div className="overflow-auto rounded-lg border bg-[#0b0d11] p-2">
          <svg width={width} height={height}>
            <defs>
              <marker id="arr" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8" fill="none" stroke="#1f6feb" />
              </marker>
            </defs>
            {edges.map((e, i) => {
              const a = pos[e.src_table]
              const b = pos[e.dst_table]
              if (!a || !b) return null
              const x1 = a.x + W / 2
              const y1 = a.y + H
              const x2 = b.x + W / 2
              const y2 = b.y
              return (
                <g key={i}>
                  <line x1={x1} y1={y1} x2={x2} y2={y2} stroke="#1f6feb" strokeWidth={1.5} markerEnd="url(#arr)" />
                  <text x={(x1 + x2) / 2 + 4} y={(y1 + y2) / 2} fill="#888" fontSize={9}>
                    {e.src_col}
                  </text>
                </g>
              )
            })}
            {nodes.map((n) => {
              const p = pos[n]
              const deg = edges.filter((e) => e.src_table === n || e.dst_table === n).length
              return (
                <g key={n} style={{ cursor: 'pointer' }} onClick={() => props.onOpenTable(t.schema, n)}>
                  <rect x={p.x} y={p.y} width={W} height={H} rx={8} fill="#13161c" stroke="#2a2f3a" />
                  <text x={p.x + 10} y={p.y + 22} fill="#e6e6e6" fontSize={12} fontWeight={600}>
                    {n.slice(0, 22)}
                  </text>
                  <text x={p.x + 10} y={p.y + 40} fill="#888" fontSize={10}>
                    {deg} relation{deg === 1 ? '' : 's'}
                  </text>
                </g>
              )
            })}
          </svg>
        </div>
      )}
    </div>
  )
}
