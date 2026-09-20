import { useCallback, useState } from 'react'
import { toast } from 'sonner'
import { useAppPreference } from '../lib/storage'
import type { Tab } from '../types'

export type SplitDir = 'horizontal' | 'vertical'

interface UseSplitOpts {
  tabs: Tab[]
  activeTab: string | null
  onActivate: (id: string) => void
}

/* Split-view state for the workspace (max 2 panes) with pin semantics. The
   left pane shows the pinned tab; the right pane keeps following activeTab,
   so flipping tabs in the strip only swaps the right half while the pin stays
   put. Closing the split re-activates the pinned tab (split origin).
   pinnedTab is derived — never synced via effect — so a closed tab, or
   one equal to the active tab, simply hides the split instead of cascading
   renders. Direction persists via useAppPreference (the sanctioned
   localStorage path per AGENTS.md §3.4). */
export function useSplit({ tabs, activeTab, onActivate }: UseSplitOpts) {
  const [pinnedId, setPinnedId] = useState<string | null>(null)
  const [splitDir, setSplitDir] = useAppPreference<SplitDir>('pglight-split-dir', 'horizontal')
  const pinnedTab = pinnedId == null || pinnedId === activeTab ? null : (tabs.find((t) => t.id === pinnedId) ?? null)

  const openSplit = useCallback((id: string, dir: SplitDir) => {
    const target = tabs.some((t) => t.id === id) ? id : activeTab
    if (target == null) {
      toast.error('Open a tab first')
      return
    }
    if (target !== activeTab) {
      // Pin an inactive tab; the primary pane keeps the active tab.
      setSplitDir(dir)
      setPinnedId(target)
      return
    }
    // Pin the current tab and move the primary pane to the adjacent tab
    // (next, else previous) so both halves show something right away.
    const idx = tabs.findIndex((t) => t.id === target)
    const neighbor = tabs[idx + 1] ?? tabs[idx - 1]
    if (!neighbor) {
      toast.error('Open at least 2 tabs to split the view')
      return
    }
    setSplitDir(dir)
    setPinnedId(target)
    onActivate(neighbor.id)
  }, [tabs, activeTab, onActivate, setSplitDir])

  const swapSplit = useCallback(() => {
    if (pinnedTab == null || activeTab == null) return
    const a = activeTab
    onActivate(pinnedTab.id)
    setPinnedId(a)
  }, [pinnedTab, activeTab, onActivate])

  // Closing the split returns to the pinned tab (the split origin) instead
  // of staying on the neighbor the primary pane had moved to. No-op when
  // there is no visible pin (already active, or pinned tab closed).
  const closeSplit = useCallback(() => {
    if (pinnedId != null && pinnedId !== activeTab && tabs.some((t) => t.id === pinnedId)) {
      onActivate(pinnedId)
    }
    setPinnedId(null)
  }, [pinnedId, activeTab, tabs, onActivate])

  return { pinnedId, setPinnedId, splitDir, setSplitDir, pinnedTab, openSplit, swapSplit, closeSplit }
}

export type UseSplit = ReturnType<typeof useSplit>
