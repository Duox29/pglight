import { useRef, useState } from 'react'

export interface CtxCell {
  value: unknown
  col: string
}

const emptySelection = new Set<string>()

/* Shared single/bulk row-selection flow for result grids (Open Data table +
   query DataGrid). One flow everywhere:
   - plain left-click  → select exactly this row (drops previous selection)
   - ctrl/meta+click   → toggle this row
   - shift+click       → range from anchor
   - right-click       → never preventDefault here; if the row is outside the
     current selection it becomes the single selection, otherwise the
     multi-selection is kept. The right-clicked cell is recorded for the
     "Copy cell value" menu item. Let the event bubble so the Radix
     ContextMenu trigger can open (calling preventDefault would make Radix
     skip handleOpen via composeEventHandlers checkForDefaultPrevented). */
export function useGridSelection(pageRows: unknown[][], rowKey: (r: unknown[], index: number) => string) {
  const [selection, setSelection] = useState<{ rows: unknown[][]; keys: Set<string> }>({ rows: pageRows, keys: emptySelection })
  const [ctxCell, setCtxCell] = useState<CtxCell | null>(null)
  const anchor = useRef(0)
  const hasCurrentRows = selection.rows === pageRows
  const sel = hasCurrentRows ? selection.keys : emptySelection
  const visibleCtxCell = hasCurrentRows ? ctxCell : null

  const setSel = (keys: Set<string>) => setSelection({ rows: pageRows, keys })

  const selectSingle = (ri: number, r: unknown[]) => {
    setSel(new Set([rowKey(r, ri)]))
    anchor.current = ri
  }

  const toggleRow = (ri: number, r: unknown[]) => {
    const k = rowKey(r, ri)
    setSelection((prev) => {
      const next = new Set(prev.rows === pageRows ? prev.keys : emptySelection)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return { rows: pageRows, keys: next }
    })
    anchor.current = ri
  }

  const rangeTo = (ri: number) => {
    const [a, b] = anchor.current < ri ? [anchor.current, ri] : [ri, anchor.current]
    setSelection((prev) => {
      const next = new Set(prev.rows === pageRows ? prev.keys : emptySelection)
      for (let i = a; i <= b; i++) if (pageRows[i]) next.add(rowKey(pageRows[i], i))
      return { rows: pageRows, keys: next }
    })
  }

  /** Plain click selects single; ctrl/meta toggles; shift ranges. */
  const handleCellClick = (e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }, ri: number, r: unknown[]) => {
    if (e.ctrlKey || e.metaKey) toggleRow(ri, r)
    else if (e.shiftKey) rangeTo(ri)
    else selectSingle(ri, r)
  }

  /** Right-click: keep multi-selection when inside it, else select single. */
  const handleRowContextMenu = (ri: number, r: unknown[]) => {
    if (!sel.has(rowKey(r, ri))) selectSingle(ri, r)
    else anchor.current = ri
  }

  const handleCellContextMenu = (value: unknown, col: string) => {
    setCtxCell({ value, col })
  }

  const clear = () => {
    setSelection({ rows: pageRows, keys: emptySelection })
    setCtxCell(null)
  }

  return { sel, setSel, ctxCell: visibleCtxCell, setCtxCell, anchor, selectSingle, toggleRow, rangeTo, handleCellClick, handleRowContextMenu, handleCellContextMenu, clear }
}
