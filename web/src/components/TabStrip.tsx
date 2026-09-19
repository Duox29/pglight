import { useEffect, useRef } from 'react'
import { Columns2, Pin, Plus, Rows2, X } from 'lucide-react'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from './ui/context-menu'
import { Tip } from './ui/tooltip'
import { DEFAULT_SQL } from '../lib/tabs'
import type { SplitDir } from '../hooks/useSplit'
import type { SessionInfo, Tab } from '../types'
import { cn } from '../lib/utils'

export interface TabStripProps {
  tabs: Tab[]
  activeTab: string | null
  sessions: SessionInfo[]
  /** Show the split buttons (enough tabs, no split active). Owned by App. */
  splitHint: boolean
  /** Tab pinned in the split secondary pane (badge + click to unpin). */
  pinnedId: string | null
  onUnpin: () => void
  onActivate: (id: string) => void
  onClose: (id: string) => void
  onCloseOthers: (id: string) => void
  onCloseRight: (id: string) => void
  onCloseLeft: (id: string) => void
  onCloseAll: () => void
  onNewQuery: () => void
  onSplit: (id: string, dir: SplitDir) => void
}

/* Workspace tab bar: buttons + close/management context menus + new-query +
   split-view entry points. Owns its smooth wheel-scroll behavior. Pure
   chrome: every action arrives via props. */
export function TabStrip(p: TabStripProps) {
  const tablistRef = useRef<HTMLDivElement>(null)
  const wheelTarget = useRef<number | null>(null)
  const wheelRaf = useRef(0)

  useEffect(() => {
    const bar = tablistRef.current
    if (!bar) return
    const step = () => {
      if (wheelTarget.current == null) return
      const diff = wheelTarget.current - bar.scrollLeft
      if (Math.abs(diff) < 0.5) {
        bar.scrollLeft = wheelTarget.current
        wheelTarget.current = null
        return
      }
      bar.scrollLeft += diff * 0.25
      wheelRaf.current = requestAnimationFrame(step)
    }
    const kick = () => {
      cancelAnimationFrame(wheelRaf.current)
      wheelRaf.current = requestAnimationFrame(step)
    }
    // React attaches wheel listeners as passive at the root, so preventDefault
    // needs a native non-passive listener.
    const onWheel = (e: WheelEvent) => {
      if (bar.scrollWidth <= bar.clientWidth + 1) return
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return
      e.preventDefault()
      const unit = e.deltaMode === 1 ? 16 : 1
      const dy = e.deltaY * unit
      // Trackpads emit small fractional deltas that are already smooth —
      // apply directly. Notched wheels get eased toward a target.
      if (Math.abs(dy) < 40) {
        wheelTarget.current = null
        cancelAnimationFrame(wheelRaf.current)
        bar.scrollLeft += dy
        return
      }
      if (wheelTarget.current == null) wheelTarget.current = bar.scrollLeft
      wheelTarget.current = Math.min(Math.max(wheelTarget.current + dy, 0), bar.scrollWidth - bar.clientWidth)
      kick()
    }
    bar.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      bar.removeEventListener('wheel', onWheel)
      cancelAnimationFrame(wheelRaf.current)
      wheelTarget.current = null
    }
  }, [])

  return (
    <div className="flex h-10 items-stretch border-b bg-card pt-1.5">
      <div ref={tablistRef} className="flex min-w-0 flex-1 items-end gap-1 overflow-x-auto px-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden" role="tablist" aria-label="Workspace tabs">
      {p.tabs.map((t) => {
        const sid = (t as { sessionId?: string }).sessionId
        const db = p.sessions.find((s) => s.id === sid)?.dbname ?? ''
        const dirty = t.kind === 'query' && !t.results && !t.error && t.sql !== DEFAULT_SQL
        const idx = p.tabs.findIndex((x) => x.id === t.id)
        return (
          <ContextMenu key={t.id}>
            <ContextMenuTrigger asChild>
              <button
                role="tab"
                aria-selected={t.id === p.activeTab}
                onClick={() => p.onActivate(t.id)}
                onMouseDown={(e) => {
                  if (e.button === 1) {
                    e.preventDefault()
                    p.onClose(t.id)
                  }
                }}
                className={cn(
                  'group flex items-center gap-1.5 whitespace-nowrap rounded-t-md border border-b-0 px-2.5 py-1.5 text-[12px]',
                  t.id === p.activeTab ? 'bg-background font-semibold' : 'bg-muted text-muted-foreground hover:text-foreground',
                )}
              >
                <span aria-hidden className={cn('h-1.5 w-1.5 rounded-full', t.id === p.activeTab ? 'bg-primary' : dirty ? 'bg-amber-500' : 'bg-transparent')} />
                <Tip content="Close tab (middle-click also closes)">
                  <span className="flex shrink-0">
                    <X
                      className="h-3 w-3 opacity-60 hover:opacity-100"
                      onClick={(e) => {
                        e.stopPropagation()
                        p.onClose(t.id)
                      }}
                    />
                  </span>
                </Tip>
                <Tip content={t.kind === 'query' ? `${t.title} · ${db || 'no session'}` : `${t.title}`}>
                  <span className="max-w-[160px] truncate">{t.title}</span>
                </Tip>
                {t.id === p.pinnedId && (
                  <Tip content="Pinned in split view — click to unpin">
                    <span
                      className="flex shrink-0"
                      onClick={(e) => {
                        e.stopPropagation()
                        p.onUnpin()
                      }}
                    >
                      <Pin className="h-3 w-3 text-primary" />
                    </span>
                  </Tip>
                )}
                {t.kind !== 'docs' && db && (
                  <Tip content={`Session database: ${db}`}>
                    <span className="max-w-[80px] truncate rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">
                      {db}
                    </span>
                  </Tip>
                )}
              </button>
            </ContextMenuTrigger>
            <ContextMenuContent>
              <ContextMenuItem onSelect={() => p.onClose(t.id)}>Close</ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem disabled={p.tabs.length < 2} onSelect={() => p.onSplit(t.id, 'horizontal')}>
                Split Right
              </ContextMenuItem>
              <ContextMenuItem disabled={p.tabs.length < 2} onSelect={() => p.onSplit(t.id, 'vertical')}>
                Split Down
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem disabled={p.tabs.length < 2} onSelect={() => p.onCloseOthers(t.id)}>
                Close Others
              </ContextMenuItem>
              <ContextMenuItem disabled={idx < 0 || idx >= p.tabs.length - 1} onSelect={() => p.onCloseRight(t.id)}>
                Close to the Right
              </ContextMenuItem>
              <ContextMenuItem disabled={idx <= 0} onSelect={() => p.onCloseLeft(t.id)}>
                Close to the Left
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={p.onCloseAll}>Close All</ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        )
      })}
      </div>
      <div className="flex shrink-0 items-center gap-0.5 border-l border-border px-2">
      <Tip content="New query (Ctrl+K then Enter)">
        <button
          onClick={p.onNewQuery}
          className="flex shrink-0 items-center gap-1 whitespace-nowrap px-2 py-1.5 text-[12px] text-muted-foreground hover:text-foreground"
        >
          <Plus className="h-3.5 w-3.5" /> Query
        </button>
      </Tip>
      {p.splitHint && (
        <>
          <Tip content="Split right — pin this tab, open the adjacent tab beside it">
            <button
              onClick={() => p.onSplit(p.activeTab ?? '', 'horizontal')}
              className="flex shrink-0 items-center px-1.5 py-1.5 text-muted-foreground hover:text-foreground"
              aria-label="Split right"
            >
              <Columns2 className="h-3.5 w-3.5" />
            </button>
          </Tip>
          <Tip content="Split down — pin this tab, open the adjacent tab below it">
            <button
              onClick={() => p.onSplit(p.activeTab ?? '', 'vertical')}
              className="flex shrink-0 items-center px-1.5 py-1.5 text-muted-foreground hover:text-foreground"
              aria-label="Split down"
            >
              <Rows2 className="h-3.5 w-3.5" />
            </button>
          </Tip>
        </>
      )}
      </div>
    </div>
  )
}
