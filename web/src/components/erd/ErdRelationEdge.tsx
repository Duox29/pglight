import { memo } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type Edge,
  type EdgeProps,
} from '@xyflow/react'
export interface ErdRelationEdgeData extends Record<string, unknown> {
  /** Source column label, shown only when several FKs link the same table pair. */
  label: string
  dimmed: boolean
}

export type ErdRelationEdgeT = Edge<ErdRelationEdgeData, 'erdRelation'>

function ErdRelationEdgeInner({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  selected,
  data,
  markerEnd,
}: EdgeProps<ErdRelationEdgeT>) {
  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        interactionWidth={16}
        style={{ opacity: data?.dimmed ? 0.15 : 1 }}
      />
      {data?.label && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            className="pointer-events-none absolute rounded border border-[#2a2f3a] bg-[#13161c] px-1 text-[10px] text-muted-foreground"
          >
            {data.label}
          </div>
        </EdgeLabelRenderer>
      )}
      {selected && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            className="pointer-events-none absolute h-2 w-2 rounded-full bg-[#1f6feb]"
          />
        </EdgeLabelRenderer>
      )}
    </>
  )
}

const ErdRelationEdge = memo(ErdRelationEdgeInner)
export default ErdRelationEdge
