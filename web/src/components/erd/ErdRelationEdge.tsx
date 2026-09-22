import { memo } from 'react'
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  getSmoothStepPath,
  getStraightPath,
  type Edge,
  type EdgeProps,
} from '@xyflow/react'
import type { ErdLineType } from './erdMapper'

export interface ErdRelationEdgeData extends Record<string, unknown> {
  /** Source column label, shown only when several FKs link the same table pair. */
  label: string
  dimmed: boolean
  /** Line shape, driven by the canvas toolbar (config, not a separate edge type). */
  line: ErdLineType
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
  const line = data?.line ?? 'bezier'
  const [path, labelX, labelY] =
    line === 'straight'
      ? getStraightPath({ sourceX, sourceY, targetX, targetY })
      : line === 'smoothstep'
        ? getSmoothStepPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition, borderRadius: 8 })
        : getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition })

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
            className="pointer-events-none absolute rounded border border-border bg-card px-1 text-[10px] text-muted-foreground"
          >
            {data.label}
          </div>
        </EdgeLabelRenderer>
      )}
      {selected && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            className="pointer-events-none absolute h-2 w-2 rounded-full bg-primary"
          />
        </EdgeLabelRenderer>
      )}
    </>
  )
}

const ErdRelationEdge = memo(ErdRelationEdgeInner)
export default ErdRelationEdge
