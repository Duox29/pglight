import { useEffect, useMemo, useRef, useState } from 'react'
import { Command, Database, Search } from 'lucide-react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { EmptyNote } from './ui/feedback'
import { api, q } from '@/lib/api'
import { COMMANDS } from '@/commands/registry'
import type { CommandId } from '@/commands/types'
import { useCommand } from '@/shortcuts/ShortcutProvider'
import { shortcutForDisplay } from '@/shortcuts/normalize'

interface Hit {
  kind: string
  schema: string
  name: string
  detail: string
}

interface CommandHit {
  type: 'command'
  id: CommandId
  title: string
  category: string
}

type PaletteHit = CommandHit | { type: 'database'; hit: Hit }

export function SearchPalette(props: {
  open: boolean
  onOpenChange: (v: boolean) => void
  session: string
  onOpenTable: (schema: string, table: string) => void
}) {
  const [term, setTerm] = useState('')
  const [dbHits, setDbHits] = useState<Hit[]>([])
  const [selected, setSelected] = useState(0)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const seq = useRef(0)
  const commands = useCommand()

  useEffect(() => () => clearTimeout(timer.current), [])

  const commandHits = useMemo<CommandHit[]>(() => {
    const needle = term.trim().toLowerCase()
    return COMMANDS
      .filter((definition) => !needle || `${definition.title} ${definition.id} ${definition.category}`.toLowerCase().includes(needle))
      .map((definition) => ({ type: 'command', id: definition.id, title: definition.title, category: definition.category }))
  }, [term])

  const hits = useMemo<PaletteHit[]>(() => [
    ...commandHits,
    ...dbHits.map((hit) => ({ type: 'database' as const, hit })),
  ], [commandHits, dbHits])

  const onChange = (value: string) => {
    setTerm(value)
    setSelected(0)
    clearTimeout(timer.current)
    if (value.trim().length < 2 || !props.session) {
      setDbHits([])
      return
    }
    const n = ++seq.current
    const query = value.trim()
    const sid = props.session
    timer.current = setTimeout(async () => {
      try {
        const response = await api<Hit[] | { error?: string }>(q(sid, `/api/search?q=${encodeURIComponent(query)}`))
        if (seq.current !== n) return
        setDbHits(Array.isArray(response) ? response.filter((hit) => hit.kind !== 'function') : [])
      } catch {
        if (seq.current === n) setDbHits([])
      }
    }, 200)
  }

  const go = (item: PaletteHit) => {
    props.onOpenChange(false)
    if (item.type === 'command') {
      void commands.execute(item.id)
      return
    }
    if (item.hit.kind === 'table' || item.hit.kind === 'column') props.onOpenTable(item.hit.schema, item.hit.name.split('.')[0])
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Command className="h-4 w-4" /> Command Palette
          </DialogTitle>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="Run a command or search database objects…"
          value={term}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown') { event.preventDefault(); setSelected((value) => Math.min(value + 1, Math.max(0, hits.length - 1))) }
            if (event.key === 'ArrowUp') { event.preventDefault(); setSelected((value) => Math.max(0, value - 1)) }
            if (event.key === 'Enter' && hits[selected]) { event.preventDefault(); go(hits[selected]) }
          }}
        />
        <div className="max-h-[50vh] overflow-auto">
          {hits.map((item, index) => (
            <Button
              key={item.type === 'command' ? item.id : `${item.hit.kind}:${item.hit.schema}:${item.hit.name}`}
              variant="ghost"
              className={`h-auto w-full justify-start rounded px-2 py-1.5 text-left ${index === selected ? 'bg-accent' : ''}`}
              onMouseEnter={() => setSelected(index)}
              onClick={() => go(item)}
            >
              {item.type === 'command' ? <Command className="mr-2 h-3.5 w-3.5 shrink-0" /> : <Database className="mr-2 h-3.5 w-3.5 shrink-0" />}
              <span className="min-w-0 flex-1 truncate">
                <span className="text-[11px] text-muted-foreground">{item.type === 'command' ? item.category : item.hit.kind}</span>{' '}
                <b className="text-[12px]">{item.type === 'command' ? item.title : `${item.hit.schema}.${item.hit.name}`}</b>{' '}
                {item.type === 'database' && <span className="text-[11px] text-muted-foreground">{item.hit.detail}</span>}
              </span>
              {item.type === 'command' && commands.formatBinding(item.id)[0] && <kbd className="ml-2 shrink-0 font-mono text-[10px] text-muted-foreground">{shortcutForDisplay(commands.formatBinding(item.id)[0])}</kbd>}
            </Button>
          ))}
          {!hits.length && <EmptyNote text={term.trim().length >= 2 ? 'No matching commands or objects' : 'Type to search commands or database objects'} />}
        </div>
        <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
          <span>↑↓ select</span><span>Enter run</span><span>Esc close</span><Search className="ml-auto h-3 w-3" />
        </div>
      </DialogContent>
    </Dialog>
  )
}
