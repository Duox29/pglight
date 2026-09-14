import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Textarea } from './ui/textarea'
import { Switch } from './ui/switch'
import { Tip } from './ui/tooltip'

export interface PgType {
  value: string
  group: string
  hint: string
}

export const PG_TYPES: PgType[] = [
  { value: 'smallint', group: 'Numeric', hint: '2-byte whole number' },
  { value: 'integer', group: 'Numeric', hint: '4-byte whole number' },
  { value: 'bigint', group: 'Numeric', hint: '8-byte whole number' },
  { value: 'serial', group: 'Numeric', hint: 'auto-increment integer' },
  { value: 'bigserial', group: 'Numeric', hint: 'auto-increment bigint' },
  { value: 'real', group: 'Numeric', hint: '4-byte float' },
  { value: 'double precision', group: 'Numeric', hint: '8-byte float' },
  { value: 'numeric', group: 'Numeric', hint: 'exact, arbitrary precision' },
  { value: 'numeric(10,2)', group: 'Numeric', hint: 'exact with scale' },
  { value: 'money', group: 'Numeric', hint: 'currency amount' },
  { value: 'varchar(255)', group: 'Text', hint: 'variable-length, limit 255' },
  { value: 'varchar', group: 'Text', hint: 'variable-length, no limit' },
  { value: 'char(1)', group: 'Text', hint: 'fixed-length, 1 char' },
  { value: 'text', group: 'Text', hint: 'unlimited text' },
  { value: 'citext', group: 'Text', hint: 'case-insensitive text' },
  { value: 'boolean', group: 'Logical', hint: 'true / false / null' },
  { value: 'date', group: 'Date/time', hint: 'calendar date' },
  { value: 'time', group: 'Date/time', hint: 'time of day' },
  { value: 'timetz', group: 'Date/time', hint: 'time with time zone' },
  { value: 'timestamp', group: 'Date/time', hint: 'date + time, no zone' },
  { value: 'timestamptz', group: 'Date/time', hint: 'date + time, with zone' },
  { value: 'interval', group: 'Date/time', hint: 'time span' },
  { value: 'json', group: 'JSON', hint: 'raw JSON text' },
  { value: 'jsonb', group: 'JSON', hint: 'indexed binary JSON' },
  { value: 'uuid', group: 'IDs / network', hint: 'universally unique id' },
  { value: 'inet', group: 'IDs / network', hint: 'IP address' },
  { value: 'cidr', group: 'IDs / network', hint: 'network address' },
  { value: 'macaddr', group: 'IDs / network', hint: 'MAC address' },
  { value: 'bytea', group: 'Binary', hint: 'binary bytes' },
  { value: 'xml', group: 'Other', hint: 'XML document' },
  { value: 'tsvector', group: 'Other', hint: 'full-text search vector' },
  { value: 'int4range', group: 'Other', hint: 'range of integers' },
  { value: 'daterange', group: 'Other', hint: 'range of dates' },
  { value: 'text[]', group: 'Arrays', hint: 'array of text' },
  { value: 'integer[]', group: 'Arrays', hint: 'array of integers' },
]

function rankType(t: PgType, needle: string): number {
  const v = t.value.toLowerCase()
  if (v === needle) return 0
  if (v.startsWith(needle)) return 1
  if (v.includes(needle)) return 2
  if (t.group.toLowerCase().includes(needle)) return 3
  if (t.hint.toLowerCase().includes(needle)) return 4
  return 99
}

export function TypeSuggest({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [open, setOpen] = useState(false)
  const [hi, setHi] = useState(0)
  const boxRef = useRef<HTMLDivElement>(null)
  const needle = value.trim().toLowerCase()
  const list = useMemo(() => {
    if (!needle) return PG_TYPES.slice(0, 10)
    return PG_TYPES.map((t) => ({ t, r: rankType(t, needle) }))
      .filter((x) => x.r < 99)
      .sort((a, b) => a.r - b.r || a.t.value.localeCompare(b.t.value))
      .slice(0, 10)
      .map((x) => x.t)
  }, [needle])

  useEffect(() => {
    const close = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])

  return (
    <div ref={boxRef} className="relative">
      <Input
        placeholder={placeholder ?? 'type — e.g. varchar(255)'}
        value={value}
        onChange={(e) => {
          setHi(0)
          setOpen(true)
          onChange(e.target.value)
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={(e) => {
          if (!open || !list.length) return
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setHi((h) => (h + 1) % list.length)
          } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setHi((h) => (h - 1 + list.length) % list.length)
          } else if (e.key === 'Enter') {
            if (document.activeElement === e.currentTarget && list[hi]) {
              // Let the parent form submit normally on Enter, but Tab/click picks suggestion.
            }
          } else if (e.key === 'Escape') {
            setOpen(false)
          }
        }}
      />
      {open && list.length > 0 && (
        <div className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border bg-card shadow-md">
          {list.map((t, i) => (
            <button
              key={t.value}
              type="button"
              className={`flex w-full items-center justify-between gap-2 px-2 py-1.5 text-left text-[12px] hover:bg-accent ${i === hi ? 'bg-accent' : ''}`}
              onMouseEnter={() => setHi(i)}
              onClick={() => {
                onChange(t.value)
                setOpen(false)
              }}
            >
              <span>
                <span className="font-mono font-semibold">{t.value}</span>
                <span className="ml-2 text-muted-foreground">{t.hint}</span>
              </span>
              <span className="shrink-0 text-[10px] text-muted-foreground">{t.group}</span>
            </button>
          ))}
          <div className="border-t px-2 py-1 text-[11px] text-muted-foreground">Custom enum/domain types also work — just type the name.</div>
        </div>
      )}
    </div>
  )
}

export interface ColumnValues {
  name: string
  type: string
  nullable: boolean
  defDefault: string
}

export function ColumnDialog({
  open,
  onOpenChange,
  mode,
  initial,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  mode: 'add' | 'edit'
  initial: ColumnValues
  onSubmit: (v: ColumnValues) => void
}) {
  const [v, setV] = useState<ColumnValues>(initial)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{mode === 'add' ? 'Add column' : `Edit column ${initial.name}`}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-2.5">
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Lowercase letters, digits, underscore. Renames use ALTER … RENAME COLUMN.">
              <span className="text-muted-foreground">Name</span>
            </Tip>
            <Input value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} placeholder="column_name" />
          </label>
          <div className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Start typing to filter — custom enum/domain types are accepted too. Changing type rewrites with USING column::new_type.">
              <span className="text-muted-foreground">Type</span>
            </Tip>
            <TypeSuggest value={v.type} onChange={(type) => setV({ ...v, type })} />
          </div>
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="OFF adds SET NOT NULL. Existing NULLs will block it.">
              <span className="text-muted-foreground">Nullable</span>
            </Tip>
            <span className="flex items-center gap-2">
              <Switch checked={v.nullable} onCheckedChange={(nullable) => setV({ ...v, nullable: !!nullable })} />
              <span className="text-muted-foreground">{v.nullable ? 'NULL allowed' : 'NOT NULL'}</span>
            </span>
          </label>
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="SQL expression — e.g. now(), 0, 'foo'. Empty removes the default.">
              <span className="text-muted-foreground">Default</span>
            </Tip>
            <Input value={v.defDefault} onChange={(e) => setV({ ...v, defDefault: e.target.value })} placeholder="now() · 0 · 'foo' · empty = none" />
          </label>
          <div className="mt-1 flex justify-end gap-1.5">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              disabled={!v.name.trim() || !v.type.trim()}
              onClick={() => {
                onSubmit({ ...v, name: v.name.trim(), type: v.type.trim(), defDefault: v.defDefault.trim() })
                onOpenChange(false)
              }}
            >
              {mode === 'add' ? 'Add column' : 'Save'}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

export function ConstraintDialog({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (v: { name: string; def: string }) => void
}) {
  const [name, setName] = useState('')
  const [def, setDef] = useState('')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add constraint</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-2.5">
          <label className="grid grid-cols-[110px_1fr] items-center gap-2 text-[12px]">
            <Tip content="Optional — Postgres auto-names when empty.">
              <span className="text-muted-foreground">Name</span>
            </Tip>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="optional" />
          </label>
          <div className="grid grid-cols-[110px_1fr] gap-2 text-[12px]">
            <Tip content="Must start with CHECK, UNIQUE, PRIMARY KEY, FOREIGN KEY or EXCLUDE.">
              <span className="pt-1.5 text-muted-foreground">Definition</span>
            </Tip>
            <Textarea value={def} onChange={(e) => setDef(e.target.value)} placeholder={'CHECK (price > 0)\nUNIQUE (email)\nFOREIGN KEY (author_id) REFERENCES authors(id)'} rows={3} className="font-mono" />
          </div>
          <div className="flex justify-end gap-1.5">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button disabled={!def.trim()} onClick={() => { onSubmit({ name: name.trim(), def: def.trim() }); onOpenChange(false) }}>
              Add constraint
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
