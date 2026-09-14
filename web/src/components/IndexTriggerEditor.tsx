import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'
import { Tip } from './ui/tooltip'

export interface IndexValues {
  name: string
  unique: boolean
  method: string
  keys: string[]
  include: string[]
  where: string
}

const INDEX_METHODS = [
  { value: 'btree', hint: 'default — equality + range' },
  { value: 'hash', hint: 'equality only' },
  { value: 'gin', hint: 'jsonb / arrays / FTS' },
  { value: 'gist', hint: 'geometry / ranges' },
  { value: 'spgist', hint: 'partitioned search' },
  { value: 'brin', hint: 'huge append-only tables' },
]

export function IndexDialog({
  open,
  onOpenChange,
  columns,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  columns: string[]
  onSubmit: (v: IndexValues) => void
}) {
  const [name, setName] = useState('')
  const [unique, setUnique] = useState(false)
  const [method, setMethod] = useState('btree')
  const [keys, setKeys] = useState<{ col: string; dir: string }[]>([])
  const [expr, setExpr] = useState('')
  const [include, setInclude] = useState<string[]>([])
  const [where, setWhere] = useState('')

  const toggleKey = (col: string) =>
    setKeys((prev) => (prev.some((k) => k.col === col) ? prev.filter((k) => k.col !== col) : [...prev, { col, dir: 'ASC' }]))
  const toggleInclude = (col: string) =>
    setInclude((prev) => (prev.includes(col) ? prev.filter((c) => c !== col) : [...prev, col]))
  const addExpr = () => {
    const t = expr.trim()
    if (!t) return
    setKeys((prev) => [...prev, { col: `(${t})`, dir: '' }])
    setExpr('')
  }
  const keySpecs = keys.map((k) => (k.dir ? `${k.col} ${k.dir}` : k.col))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-auto">
        <DialogHeader>
          <DialogTitle>Create index</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-2.5">
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Optional — Postgres auto-names (tablename_col_idx) when empty.">
              <span className="text-muted-foreground">Name</span>
            </Tip>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="optional" />
          </label>
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <span className="text-muted-foreground">Unique</span>
            <span className="flex items-center gap-2">
              <Switch checked={unique} onCheckedChange={(v) => setUnique(!!v)} />
              <span className="text-muted-foreground">{unique ? 'UNIQUE' : 'non-unique'}</span>
            </span>
          </div>
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="btree fits most cases; gin for jsonb/arrays, brin for huge append-only tables.">
              <span className="text-muted-foreground">Method</span>
            </Tip>
            <Select value={method} onValueChange={setMethod}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INDEX_METHODS.map((m) => (
                  <SelectItem key={m.value} value={m.value}>
                    {m.value} — {m.hint}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-[110px_1fr] gap-2 text-[12px]">
            <Tip content="Checked columns become index keys, in order. Expression keys (e.g. lower(email)) via the input below.">
              <span className="pt-1.5 text-muted-foreground">Keys ({keys.length})</span>
            </Tip>
            <div className="flex max-h-44 flex-col gap-1 overflow-auto rounded-md border p-1.5">
              {columns.length === 0 && <span className="px-1 text-muted-foreground">No columns loaded</span>}
              {columns.map((c) => {
                const sel = keys.find((k) => k.col === c)
                return (
                  <div key={c} className="flex items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent">
                    <Switch checked={!!sel} onCheckedChange={() => toggleKey(c)} aria-label={`Index key ${c}`} />
                    <span className="flex-1 truncate font-mono">{c}</span>
                    {sel && (
                      <Select value={sel.dir} onValueChange={(dir) => setKeys((prev) => prev.map((k) => (k.col === c ? { ...k, dir } : k)))}>
                        <SelectTrigger className="h-6 w-[86px] text-[11px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="ASC">ASC</SelectItem>
                          <SelectItem value="DESC">DESC</SelectItem>
                        </SelectContent>
                      </Select>
                    )}
                  </div>
                )
              })}
              <div className="flex gap-1.5 pt-1">
                <Input value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="(lower(email)) — expression key" className="h-7 font-mono text-[12px]" onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addExpr() } }} />
                <Button size="sm" variant="ghost" onClick={addExpr} aria-label="Add expression key">
                  <Plus />
                </Button>
              </div>
              {keys.filter((k) => k.col.startsWith('(')).map((k) => (
                <div key={k.col} className="flex items-center gap-1.5 rounded bg-muted px-1 py-0.5">
                  <span className="flex-1 truncate font-mono">{k.col}</span>
                  <Button size="sm" variant="ghost" aria-label={`Remove ${k.col}`} onClick={() => setKeys((prev) => prev.filter((x) => x.col !== k.col))}>
                    <X />
                  </Button>
                </div>
              ))}
            </div>
          </div>
          {method === 'btree' && (
            <div className="grid grid-cols-[110px_1fr] gap-2 text-[12px]">
              <Tip content="Covering index payload — columns read without touching the heap. btree only.">
                <span className="pt-1.5 text-muted-foreground">Include</span>
              </Tip>
              <div className="flex max-h-28 flex-col gap-1 overflow-auto rounded-md border p-1.5">
                {columns.filter((c) => !keys.some((k) => k.col === c)).map((c) => (
                  <label key={c} className="flex cursor-pointer items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent">
                    <Switch checked={include.includes(c)} onCheckedChange={() => toggleInclude(c)} aria-label={`Include ${c}`} />
                    <span className="truncate font-mono">{c}</span>
                  </label>
                ))}
              </div>
            </div>
          )}
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Partial index predicate — e.g. active IS TRUE. Empty = whole table.">
              <span className="text-muted-foreground">Where</span>
            </Tip>
            <Input value={where} onChange={(e) => setWhere(e.target.value)} placeholder="optional — e.g. active IS TRUE" className="font-mono" />
          </label>
          {keySpecs.length > 0 && (
            <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[11px]">
              CREATE{unique ? ' UNIQUE' : ''} INDEX ON … USING {method} ({keySpecs.join(', ')})
              {include.length > 0 ? ` INCLUDE (${include.join(', ')})` : ''}{where.trim() ? ` WHERE ${where.trim()}` : ''}
            </pre>
          )}
          <div className="flex justify-end gap-1.5">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button disabled={!keys.length} onClick={() => { onSubmit({ name: name.trim(), unique, method, keys: keySpecs, include, where: where.trim() }); onOpenChange(false) }}>
              Create index
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

const TRIGGER_EVENTS = ['INSERT', 'UPDATE', 'DELETE', 'TRUNCATE'] as const

export interface TriggerValues {
  name: string
  timing: string
  events: string[]
  forEach: string
  function: string
  updateOf: string[]
  when: string
}

export function TriggerDialog({
  open,
  onOpenChange,
  columns,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  columns: string[]
  onSubmit: (v: TriggerValues) => void
}) {
  const [name, setName] = useState('')
  const [timing, setTiming] = useState('BEFORE')
  const [events, setEvents] = useState<string[]>(['INSERT'])
  const [forEach, setForEach] = useState('ROW')
  const [fn, setFn] = useState('')
  const [updateOf, setUpdateOf] = useState<string[]>([])
  const [when, setWhen] = useState('')

  const toggleEvent = (e: string) =>
    setEvents((prev) => (prev.includes(e) ? prev.filter((x) => x !== e) : [...prev, e]))
  const toggleUpdateOf = (c: string) =>
    setUpdateOf((prev) => (prev.includes(c) ? prev.filter((x) => x !== c) : [...prev, c]))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-auto">
        <DialogHeader>
          <DialogTitle>Create trigger</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-2.5">
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <span className="text-muted-foreground">Name</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="audit_trigger" />
          </label>
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="BEFORE can modify NEW / skip the write; AFTER sees the final row; INSTEAD OF is for views.">
              <span className="text-muted-foreground">Timing</span>
            </Tip>
            <Select value={timing} onValueChange={setTiming}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="BEFORE">BEFORE</SelectItem>
                <SelectItem value="AFTER">AFTER</SelectItem>
                <SelectItem value="INSTEAD OF">INSTEAD OF</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <span className="text-muted-foreground">Events</span>
            <div className="flex flex-wrap gap-1.5">
              {TRIGGER_EVENTS.map((e) => (
                <Button
                  key={e}
                  size="sm"
                  variant={events.includes(e) ? 'secondary' : 'outline'}
                  onClick={() => toggleEvent(e)}
                >
                  {e}
                </Button>
              ))}
            </div>
          </div>
          {events.includes('UPDATE') && (
            <div className="grid grid-cols-[110px_1fr] gap-2 text-[12px]">
              <Tip content="Fire only when these columns change. Empty = any UPDATE.">
                <span className="pt-1.5 text-muted-foreground">Update of</span>
              </Tip>
              <div className="flex max-h-28 flex-col gap-1 overflow-auto rounded-md border p-1.5">
                {columns.map((c) => (
                  <label key={c} className="flex cursor-pointer items-center gap-1.5 rounded px-1 py-0.5 hover:bg-accent">
                    <Switch checked={updateOf.includes(c)} onCheckedChange={() => toggleUpdateOf(c)} aria-label={`Update of ${c}`} />
                    <span className="truncate font-mono">{c}</span>
                  </label>
                ))}
              </div>
            </div>
          )}
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="ROW fires per row (can use NEW/OLD); STATEMENT fires once per statement.">
              <span className="text-muted-foreground">For each</span>
            </Tip>
            <Select value={forEach} onValueChange={setForEach}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="ROW">ROW</SelectItem>
                <SelectItem value="STATEMENT">STATEMENT</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Trigger function returning trigger — e.g. audit_fn(). Must already exist.">
              <span className="text-muted-foreground">Function</span>
            </Tip>
            <Input value={fn} onChange={(e) => setFn(e.target.value)} placeholder="audit_fn()" className="font-mono" />
          </label>
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Row-level condition using OLD/NEW — e.g. OLD.status IS DISTINCT FROM NEW.status. Empty = always fire.">
              <span className="text-muted-foreground">When</span>
            </Tip>
            <Input value={when} onChange={(e) => setWhen(e.target.value)} placeholder="optional — e.g. OLD.x IS DISTINCT FROM NEW.x" className="font-mono" />
          </label>
          <div className="flex justify-end gap-1.5">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              disabled={!name.trim() || !events.length || !fn.trim()}
              onClick={() => {
                onSubmit({ name: name.trim(), timing, events, forEach, function: fn.trim(), updateOf, when: when.trim() })
                onOpenChange(false)
              }}
            >
              Create trigger
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
