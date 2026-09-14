import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  getConnectedEdges,
  useEdgesState,
  useNodesState,
  useOnSelectionChange,
  useReactFlow,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { toast } from 'sonner'
import { EmptyNote } from './ui/feedback'
import ErdTableNode, { type ErdTableNodeT } from './erd/ErdTableNode'
import ErdRelationEdge, { type ErdRelationEdgeT } from './erd/ErdRelationEdge'
import { ErdToolbar } from './erd/ErdToolbar'
import { layoutTables, type ErdPos } from './erd/erdLayout'
import { dstHandle, mapErd, srcHandle, type ErdDataDto } from './erd/erdMapper'
import { clearErdLayout, loadErdLayout, saveErdLayout } from './erd/erdStorage'
import type { ErdTabT } from '@/types'
import { cn } from '@/lib/utils'

type FlowNode = ErdTableNodeT
type FlowEdge = ErdRelationEdgeT

const nodeTypes = { erdTable: ErdTableNode }
const edgeTypes = { erdRelation: ErdRelationEdge }
const FIT = { padding: 0.18, maxZoom: 1 } as const

function copyText(text: string) {
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).then(
      () => toast.success('Copied'),
      () => toast.error('Copy failed'),
    )
  } else {
    toast.error('Clipboard unavailable')
  }
}

function ErdCanvasInner(props: {
  tab: ErdTabT
  schemas: string[]
  onSchema: (s: string) => void
  onReload: () => void
  onOpenTable: (schema: string, table: string) => void
}) {
  const { tab: t } = props
  const data = t.data as ErdDataDto | null

  const graph = useMemo(() => (data ? mapErd(t.schema, data) : { tables: [], relations: [] }), [data, t.schema])

  const [query, setQuery] = useState('')
  const [focusId, setFocusId] = useState<string | null>(null)
  const [selected, setSelected] = useState<{ nodes: string[]; edges: string[] }>({
    nodes: [],
    edges: [],
  })
  const flow = useReactFlow()
  const dragPos = useRef<Record<string, ErdPos>>({})

  const openTable = useCallback(
    (schema: string, table: string) => props.onOpenTable(schema, table),
    // onOpenTable is an inline App closure; session identifies its connection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t.sessionId],
  )
  const onSelectionChange = useCallback(
    ({ nodes: sn, edges: se }: { nodes: FlowNode[]; edges: FlowEdge[] }) => {
      setSelected({ nodes: sn.map((n) => n.id), edges: se.map((e) => e.id) })
    },
    [],
  )

  const copyName = useCallback(
    (table: string) => copyText(`"${t.schema}"."${table}"`),
    [t.schema],
  )

  const focusRelated = useCallback(
    (table: string) => {
      const rel = graph.relations.filter(
        (r) => r.sourceTable === table || r.targetTable === table,
      )
      const ids = new Set<string>([table])
      for (const r of rel) {
        ids.add(r.sourceTable)
        ids.add(r.targetTable)
      }
      const targets = flow.getNodes().filter((n) => ids.has(n.id))
      if (targets.length) void flow.fitView({ nodes: targets, ...FIT, duration: 200 })
    },
    [flow, graph],
  )

  // Derived flow state, built once per payload (inner component is keyed by
  // schema+payload, so StrictMode-safe construction needs no reset effect).
  const initial = useMemo(() => {
    const saved = loadErdLayout(t.sessionId, t.schema)
    const pos = layoutTables(graph.tables, graph.relations, saved)
    const degree = new Map<string, number>()
    for (const r of graph.relations) {
      degree.set(r.sourceTable, (degree.get(r.sourceTable) ?? 0) + 1)
      degree.set(r.targetTable, (degree.get(r.targetTable) ?? 0) + 1)
    }
    const nodes: FlowNode[] = graph.tables.map((tbl) => ({
      id: tbl.id,
      type: 'erdTable' as const,
      position: pos[tbl.id] ?? { x: 0, y: 0 },
      data: {
        table: tbl,
        degree: degree.get(tbl.id) ?? 0,
        dimmed: false,
        onOpenTable: openTable,
        onCopyName: copyName,
        onFocusRelated: focusRelated,
      },
    }))
    const edges: FlowEdge[] = graph.relations.map((r) => ({
      id: r.id,
      type: 'erdRelation' as const,
      source: r.sourceTable,
      target: r.targetTable,
      sourceHandle: srcHandle(r.sourceColumn),
      targetHandle: dstHandle(r.targetColumn),
      markerEnd: { type: 'arrowclosed' as const, color: '#1f6feb' },
      data: { label: r.labeled ? r.sourceColumn : '', dimmed: false },
      style: { stroke: '#1f6feb', strokeWidth: 1.5 },
    }))
    return { nodes, edges }
    // Layout inputs only; live drag/selection state must not rebuild the graph.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t.sessionId, t.schema, graph])

  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>(initial.nodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>(initial.edges)

  useOnSelectionChange({ onChange: onSelectionChange })

  const selectedEdge = selected.edges.length === 1 ? edges.find((e) => e.id === selected.edges[0]) : undefined
  const selectedRel = selectedEdge
    ? graph.relations.find((r) => r.id === selectedEdge.id)
    : undefined
  const neighborIds = useMemo(() => {
    const s = new Set<string>()
    if (selected.nodes.length !== 1) return s
    const id = selected.nodes[0]
    s.add(id)
    for (const r of graph.relations) {
      if (r.sourceTable === id) s.add(r.targetTable)
      if (r.targetTable === id) s.add(r.sourceTable)
    }
    return s
  }, [selected.nodes, graph])

  const matchIds = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return null
    return new Set(graph.tables.filter((tb) => tb.name.toLowerCase().includes(q)).map((tb) => tb.id))
  }, [query, graph])

  // Highlight: dim unrelated nodes/edges on selection or search; never rebuild nodes.
  useEffect(() => {
    const active: boolean =
      selected.nodes.length === 1 || !!matchIds || (selected.edges.length === 1 && !!selectedRel)
    const relIds = new Set<string>()
    if (selected.nodes.length === 1) {
      for (const r of graph.relations) {
        if (r.sourceTable === selected.nodes[0] || r.targetTable === selected.nodes[0]) relIds.add(r.id)
      }
    }
    if (selectedRel) relIds.add(selectedRel.id)
    setNodes((prev) =>
      prev.map((n) => {
        const keep =
          !active ||
          (matchIds ? matchIds.has(n.id) : true) &&
            (selected.nodes.length === 1 ? neighborIds.has(n.id) : true) &&
            (selectedRel
              ? n.id === selectedRel.sourceTable || n.id === selectedRel.targetTable
              : true) &&
            (focusId ? n.id === focusId || neighborIds.has(n.id) : true)
        const dimmed: boolean = active && !keep
        const d = n.data
        if (d.dimmed === dimmed) return n
        return { ...n, data: { table: d.table, degree: d.degree, dimmed, onOpenTable: d.onOpenTable, onCopyName: d.onCopyName, onFocusRelated: d.onFocusRelated } }
      }),
    )
    setEdges((prev) =>
      prev.map((e) => {
        const keep =
          !active ||
          (selected.nodes.length === 1 ? relIds.has(e.id) : true) &&
            (selectedRel ? e.id === selectedRel.id : true)
        const dd = e.data ?? { label: '', dimmed: false }
        const dimmed: boolean = active && !keep
        if ((dd.dimmed ?? false) === dimmed && e.selected === selected.edges.includes(e.id)) return e
        return { ...e, selected: selected.edges.includes(e.id), data: { label: dd.label ?? '', dimmed } }
      }),
    )
  }, [selected, neighborIds, selectedRel, matchIds, focusId, graph, setNodes, setEdges])

  const persistDrag = useCallback(() => {
    if (Object.keys(dragPos.current).length === 0) return
    const merged = { ...loadErdLayout(t.sessionId, t.schema), ...dragPos.current }
    saveErdLayout(t.sessionId, t.schema, merged)
    dragPos.current = {}
  }, [t.sessionId, t.schema])

  const onNodeDragStop = useCallback(
    (_: unknown, node: FlowNode) => {
      dragPos.current[node.id] = { x: node.position.x, y: node.position.y }
      persistDrag()
    },
    [persistDrag],
  )

  const onResetLayout = useCallback(() => {
    clearErdLayout(t.sessionId, t.schema)
    const pos = layoutTables(graph.tables, graph.relations, {})
    setNodes((prev) => prev.map((n) => (pos[n.id] ? { ...n, position: pos[n.id] } : n)))
    requestAnimationFrame(() => void flow.fitView({ ...FIT, duration: 200 }))
  }, [t.sessionId, t.schema, graph, setNodes, flow])

  const onFit = useCallback(() => void flow.fitView({ ...FIT, duration: 200 }), [flow])

  const focusSearch = useCallback(() => {
    if (!matchIds?.size) return
    const first = graph.tables.find((tb) => matchIds.has(tb.id))
    if (!first) return
    setFocusId(first.id)
    setSelected({ nodes: [first.id], edges: [] })
    setNodes((prev) => prev.map((n) => ({ ...n, selected: n.id === first.id })))
    const node = flow.getNodes().find((n) => n.id === first.id)
    if (node) {
      void flow.setCenter(node.position.x + 132, node.position.y + 60, { zoom: 1, duration: 200 })
    }
  }, [matchIds, graph, flow, setNodes])

  const edgeCount = useMemo(() => {
    if (selected.nodes.length !== 1) return null
    const id = selected.nodes[0]
    return getConnectedEdges([{ id } as FlowNode], edges).length
  }, [selected.nodes, edges])

  const shownCounts = matchIds ? [...matchIds].length : graph.tables.length

  if (!data) return <EmptyNote text="Loading…" />
  if (!graph.tables.length) return <EmptyNote text="No tables in this schema" />

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <ErdToolbar
        schema={t.schema}
        schemas={props.schemas}
        onSchema={props.onSchema}
        onReload={props.onReload}
        onFit={onFit}
        onResetLayout={onResetLayout}
        query={query}
        onQueryChange={(v) => {
          setQuery(v)
          setFocusId(null)
        }}
        onQuerySubmit={focusSearch}
        tableCount={graph.tables.length}
        relationCount={graph.relations.length}
      />
      {!graph.relations.length && (
        <div className="rounded-md border border-dashed px-3 py-1.5 text-[12px] text-muted-foreground">
          No foreign-key relationships — tables shown without edges.
        </div>
      )}
      <div className="relative min-h-0 flex-1 overflow-hidden rounded-lg border bg-[#0b0d11]">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeDragStop={onNodeDragStop}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          fitView
          fitViewOptions={FIT}
          minZoom={0.1}
          maxZoom={1.75}
          zoomOnScroll
          zoomOnPinch
          onlyRenderVisibleElements
          proOptions={{ hideAttribution: true }}
          colorMode="dark"
          className="[&_.react-flow__attribution]:hidden [&_.react-flow__pane]:!touch-none"
        >
          <Background gap={24} size={1} color="#1a1f2a" />
          <Controls
            showInteractive={false}
            className={cn(
              '[&>button]:!border-[#2a2f3a] [&>button]:!bg-[#13161c] [&>button]:!fill-[#9aa1b2]',
              '[&>button:hover]:!bg-[#1a1f2a]',
            )}
          />
          <MiniMap
            pannable
            zoomable
            className="!border-[#2a2f3a] !bg-[#0b0d11]/90"
            maskColor="rgba(11,13,17,0.7)"
            nodeColor="#2a2f3a"
          />
        </ReactFlow>
        {(matchIds || selected.nodes.length === 1 || selectedRel) && (
          <div className="pointer-events-none absolute left-2 top-2 max-w-[70%] truncate rounded border border-[#2a2f3a] bg-[#13161c]/95 px-2 py-1 text-[11px] text-muted-foreground">
            {matchIds && !selectedRel && selected.nodes.length !== 1
              ? `${shownCounts} matching table${shownCounts === 1 ? '' : 's'} — Enter to focus`
              : selectedRel
                ? `${selectedRel.sourceTable}.${selectedRel.sourceColumn} → ${selectedRel.targetSchema}.${selectedRel.targetTable}.${selectedRel.targetColumn}`
                : `${selected.nodes[0]} · ${edgeCount ?? 0} relation${edgeCount === 1 ? '' : 's'}`}
          </div>
        )}
      </div>
    </div>
  )
}

export function ErdView(props: {
  tab: ErdTabT
  schemas: string[]
  onSchema: (s: string) => void
  onReload: () => void
  onOpenTable: (schema: string, table: string) => void
}) {
  // Provider per tab so fitView/selection hooks are scoped. Inner canvas is
  // keyed by schema + payload shape so a reload/schema switch constructs fresh
  // flow state — no reset effects, StrictMode-safe.
  const data = props.tab.data as ErdDataDto | null
  const innerKey = `${props.tab.schema}:${data?.nodes?.length ?? -1}:${data?.edges?.length ?? -1}:${(data?.nodes ?? []).join(',')}`
  return (
    <ReactFlowProvider>
      <div className="h-full min-h-[420px]">
        <ErdCanvasInner key={innerKey} {...props} />
      </div>
    </ReactFlowProvider>
  )
}
