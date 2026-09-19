import type { ReactNode } from 'react'
import { ArrowLeftRight, Columns2, Rows2, XCircle } from 'lucide-react'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './ui/resizable'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Tip } from './ui/tooltip'
import type { SplitDir } from '../hooks/useSplit'
import { splitOptions } from '../lib/tabs'
import type { Tab } from '../types'
import { cn } from '../lib/utils'

export interface SplitWorkspaceProps {
  /** Left pane. Pinned tab while split, else the current tab. */
  primary: Tab | null
  /** Right pane. Follows the tab strip while split; null = single pane. */
  secondary: Tab | null
  dir: SplitDir
  tabs: Tab[]
  onDir: (d: SplitDir) => void
  /** User picked a tab for the right pane (excludes the pinned one). */
  onSecondary: (id: string) => void
  onSwap: () => void
  onCloseSplit: () => void
  /** Render prop so this layout chrome never imports feature views. */
  renderPane: (tab: Tab | null) => ReactNode
}

/* Two-pane (max 2) workspace layout. Single pane when secondary is null;
   otherwise a right-pane header (tab picker + direction + swap + close) over
   a resizable split whose sizes persist via autoSaveId. Owns no domain
   state; panes arrive via renderPane. */
export function SplitWorkspace(p: SplitWorkspaceProps) {
  if (p.secondary == null) {
    return <div className="min-h-0 flex-1 overflow-auto p-3">{p.renderPane(p.primary)}</div>
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-1 border-b bg-card px-2">
        <span className="shrink-0 text-[11px] uppercase tracking-wide text-muted-foreground">Split 2/2</span>
        <Select value={p.secondary.id} onValueChange={p.onSecondary}>
          <SelectTrigger className="h-7 w-[200px] text-[12px]">
            <SelectValue placeholder="Second tab" />
          </SelectTrigger>
          <SelectContent>
            {splitOptions(p.tabs, p.primary?.id).map((t) => (
              <SelectItem key={t.id} value={t.id}>
                {t.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          <Tip content="Side by side">
            <button
              onClick={() => p.onDir('horizontal')}
              className={cn('rounded p-1.5 hover:bg-muted', p.dir === 'horizontal' ? 'text-foreground' : 'text-muted-foreground')}
              aria-label="Split side by side"
            >
              <Columns2 className="h-3.5 w-3.5" />
            </button>
          </Tip>
          <Tip content="Stacked below">
            <button
              onClick={() => p.onDir('vertical')}
              className={cn('rounded p-1.5 hover:bg-muted', p.dir === 'vertical' ? 'text-foreground' : 'text-muted-foreground')}
              aria-label="Split stacked"
            >
              <Rows2 className="h-3.5 w-3.5" />
            </button>
          </Tip>
          <Tip content="Swap panes">
            <button onClick={p.onSwap} className="rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Swap panes">
              <ArrowLeftRight className="h-3.5 w-3.5" />
            </button>
          </Tip>
          <Tip content="Close split view">
            <button onClick={p.onCloseSplit} className="rounded p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Close split">
              <XCircle className="h-3.5 w-3.5" />
            </button>
          </Tip>
        </div>
      </div>
      <div className="min-h-0 flex-1">
        <ResizablePanelGroup key={p.dir} direction={p.dir} autoSaveId="pglight-split-layout" className="h-full">
          <ResizablePanel defaultSize={50} minSize={20} className="min-h-0">
            <div className="h-full overflow-auto p-3">{p.renderPane(p.primary)}</div>
          </ResizablePanel>
          <ResizableHandle withHandle />
          <ResizablePanel defaultSize={50} minSize={20} className="min-h-0">
            <div className="h-full overflow-auto p-3">{p.renderPane(p.secondary)}</div>
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </div>
  )
}
