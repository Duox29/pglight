import { useMemo, useState } from 'react'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from './button'
import { Input } from './input'
import { Popover, PopoverContent, PopoverTrigger } from './popover'
import { ScrollArea } from './scroll-area'

export interface SearchSelectOption {
  value: string
  label: string
}

/* Searchable single-select (Radix Popover + filter input + option list).
   Use where option lists are long or user-named (FK sources, columns):
   plain Select has no search. No native title tooltips — labels truncate. */
export function SearchSelect(p: {
  value: string
  options: SearchSelectOption[]
  placeholder?: string
  emptyText?: string
  onChange: (v: string) => void
  triggerClassName?: string
  ariaLabel?: string
}) {
  const [open, setOpen] = useState(false)
  const [term, setTerm] = useState('')
  const selected = p.options.find((o) => o.value === p.value)
  const filtered = useMemo(() => {
    const t = term.trim().toLowerCase()
    if (!t) return p.options
    return p.options.filter((o) => o.label.toLowerCase().includes(t) || o.value.toLowerCase().includes(t))
  }, [term, p.options])

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v)
        if (v) setTerm('')
      }}
    >
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          aria-label={p.ariaLabel ?? p.placeholder ?? 'Select option'}
          className={cn('h-7 justify-between gap-1 px-2 text-[12px] font-normal', p.triggerClassName ?? 'w-[150px]')}
        >
          <span className="truncate">{selected ? selected.label : (p.placeholder ?? 'Select…')}</span>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[260px] p-1.5" align="start">
        <Input
          autoFocus
          className="h-7 text-[12px]"
          placeholder="Type to filter…"
          value={term}
          onChange={(e) => setTerm(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && filtered[0]) {
              p.onChange(filtered[0].value)
              setOpen(false)
            }
          }}
        />
        <ScrollArea className="max-h-[220px]">
          <div className="flex flex-col gap-px py-1">
            {filtered.map((o) => (
              <button
                key={o.value}
                type="button"
                onClick={() => {
                  p.onChange(o.value)
                  setOpen(false)
                }}
                className={cn(
                  'flex w-full items-center gap-1.5 rounded-sm px-2 py-1.5 text-left text-[12px] outline-none hover:bg-accent hover:text-accent-foreground',
                  o.value === p.value && 'bg-accent/60',
                )}
              >
                <span className={cn('h-3.5 w-3.5 shrink-0', o.value === p.value ? 'opacity-100' : 'opacity-0')}>
                  <Check className="h-3.5 w-3.5" />
                </span>
                <span className="truncate font-mono">{o.label}</span>
              </button>
            ))}
            {!filtered.length && <div className="px-2 py-1.5 text-[12px] text-muted-foreground">{p.emptyText ?? 'No matches'}</div>}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  )
}
