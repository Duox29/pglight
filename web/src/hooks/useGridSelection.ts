import { useRef, useState } from 'react'

export interface CtxCell {
  value: unknown
  col: string
}

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
export function useGridSelection(pageRows: unknown[][], rowKey: (r: unknown[]) => string) {
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [ctxCell, setCtxCell] = useState<CtxCell | null>(null)
  const anchor = useRef(0)

  const selectSingle = (ri: number, r: unknown[]) => {
    setSel(new Set([rowKey(r)]))
    anchor.current = ri
  }

  const toggleRow = (ri: number, r: unknown[]) => {
    const k = rowKey(r)
    setSel((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })
    anchor.current = ri
  }

  const rangeTo = (ri: number) => {
    const [a, b] = anchor.current < ri ? [anchor.current, ri] : [ri, anchor.current]
    setSel((prev) => {
      const next = new Set(prev)
      for (let i = a; i <= b; i++) if (pageRows[i]) next.add(rowKey(pageRows[i]))
      return next
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
    if (!sel.has(rowKey(r))) selectSingle(ri, r)
    else anchor.current = ri
  }

  const handleCellContextMenu = (value: unknown, col: string) => {
    setCtxCell({ value, col })
  }

  const clear = () => {
    setSel(new Set())
    setCtxCell(null)
  }

  return { sel, setSel, ctxCell, setCtxCell, anchor, selectSingle, toggleRow, rangeTo, handleCellClick, handleRowContextMenu, handleCellContextMenu, clear }
}
