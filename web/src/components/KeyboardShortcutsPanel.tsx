import { useState, type KeyboardEvent } from 'react'
import { Button } from './ui/button'
import { Card } from './ui/card'
import { Input } from './ui/input'
import type { DialogsApi } from './dialogs'
import { COMMANDS } from '@/commands/registry'
import type { CommandId } from '@/commands/types'
import { useCommand } from '@/shortcuts/ShortcutProvider'
import { isReservedShortcut } from '@/shortcuts/reserved'
import { normalizeShortcut, shortcutForDisplay, shortcutFromEvent } from '@/shortcuts/normalize'

export function KeyboardShortcutsPanel({ dialogs }: { dialogs: DialogsApi }) {
  const commands = useCommand()
  const [search, setSearch] = useState('')
  const [recording, setRecording] = useState<CommandId | null>(null)

  const assign = async (id: CommandId, event: KeyboardEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      setRecording(null)
      return
    }
    const binding = normalizeShortcut(shortcutFromEvent(event.nativeEvent) ?? '')
    if (!binding) return
    const conflict = commands.conflictFor(binding, id)
    if (conflict) {
      const replace = await dialogs.confirm({
        title: 'Shortcut already assigned',
        description: `${shortcutForDisplay(binding)} is already assigned to ${COMMANDS.find((command) => command.id === conflict)?.title ?? conflict}. Replace it?`,
        confirmText: 'Replace',
      })
      if (!replace) return
      commands.setBindings(conflict, commands.formatBinding(conflict).filter((value) => value !== binding))
    }
    commands.setBindings(id, [binding])
    setRecording(null)
  }

  const visibleCommands = COMMANDS.filter((command) => `${command.title} ${command.id} ${command.category}`.toLowerCase().includes(search.toLowerCase()))

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <Card className="flex min-h-0 flex-1 flex-col p-2.5">
        <div className="mb-2 text-[12px] font-semibold">Keyboard Shortcuts</div>
        <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search commands…" className="mb-2" />
        <div className="min-h-0 flex-1 overflow-auto">
          {visibleCommands.map((command) => {
            const binding = commands.formatBinding(command.id)[0]
            return (
              <div key={command.id} className="flex items-center gap-2 border-b py-1.5 last:border-0">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[12px]">{command.title}</div>
                  <div className="text-[10px] text-muted-foreground">{command.category}</div>
                </div>
                {recording === command.id ? (
                  <Button size="sm" variant="secondary" autoFocus onKeyDown={(event) => void assign(command.id, event)}>Press keys…</Button>
                ) : (
                  <Button size="sm" variant="outline" onClick={() => setRecording(command.id)}>
                    {binding ? shortcutForDisplay(binding) : 'Unassigned'}
                  </Button>
                )}
                {binding && isReservedShortcut(binding) && <span className="text-[10px] text-amber-500">Browser reserved</span>}
                {binding && <Button size="sm" variant="ghost" onClick={() => commands.resetBinding(command.id)} aria-label={`Reset ${command.title}`}>Reset</Button>}
              </div>
            )
          })}
        </div>
        <div className="mt-2 flex shrink-0 items-center justify-between gap-2 text-[10px] text-muted-foreground">
          <Button className="shrink-0" size="sm" variant="secondary" onClick={async () => {
            const ok = await dialogs.confirm({ title: 'Reset keyboard shortcuts?', description: 'Restore every command to its default binding.', confirmText: 'Reset all' })
            if (ok) commands.resetAll()
          }}>Reset all</Button>
        </div>
      </Card>
    </div>
  )
}
