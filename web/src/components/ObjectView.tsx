import { useState } from 'react'
import { Copy, FileText, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Button } from './ui/button'
import { Card, CardContent, CardHeader, CardTitle } from './ui/card'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Table, TableBody, TableCell, TableRow } from './ui/table'
import { Textarea } from './ui/textarea'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { ObjectTabT } from '@/types'

export interface SeqOpts {
  increment: string
  minValue: string
  maxValue: string
  cache: string
  restart: string
  cycle: boolean
}

interface Props {
  tab: ObjectTabT
  onReload: () => void
  onCopy: (text: string) => void
  onOpenQuery: () => void
  onSaveSequence: (opts: SeqOpts) => void
  onSaveFunction: (sql: string) => void
  onEnumAdd: (value: string) => void
  onRenameRequest: () => void
  onDropRequest: () => void
}

function fmt(v: unknown): string {
  if (v == null) return '—'
  if (typeof v === 'string') return v === '' ? '—' : v
  return String(v)
}

function str(details: Record<string, unknown> | null, key: string): string {
  const v = details?.[key]
  return typeof v === 'string' || typeof v === 'number' ? String(v) : ''
}

export function ObjectView(props: Props) {
  const { tab: t } = props
  const [editing, setEditing] = useState(false)
  const [seq, setSeq] = useState<SeqOpts | null>(null)
  const [funcSql, setFuncSql] = useState('')
  const [enumVal, setEnumVal] = useState('')
  const entries = Object.entries(t.details ?? {}).filter(([k]) => k !== 'def' && k !== 'definition')
  const isEnum = t.objectKind === 'type' && (t.details?.['kind'] === 'enum' || t.details?.['labels'] != null)

  const startEdit = () => {
    if (t.objectKind === 'sequence') {
      setSeq({
        increment: str(t.details, 'increment'),
        minValue: str(t.details, 'minimum_value'),
        maxValue: str(t.details, 'maximum_value') === '9223372036854775807' ? '' : str(t.details, 'maximum_value'),
        cache: str(t.details, 'cache_size'),
        restart: '',
        cycle: String(t.details?.['cycle_option'] ?? '').toLowerCase() === 'yes',
      })
    } else if (t.objectKind === 'function') {
      setFuncSql(t.def ?? '')
    }
    setEditing(true)
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <b className="text-sm">{t.title}</b>
        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
          {t.objectKind} · {t.schema}
        </span>
        <span className="flex-1" />
        {(t.objectKind === 'sequence' || t.objectKind === 'function') && (
          <Button size="sm" variant="secondary" onClick={() => (editing ? setEditing(false) : startEdit())} aria-label="Edit">
            <Pencil className="h-3.5 w-3.5" />
          </Button>
        )}
        {t.objectKind !== 'function' && (
          <Button size="sm" variant="secondary" onClick={props.onRenameRequest} aria-label="Rename">
            <span className="text-[12px]">Rename</span>
          </Button>
        )}
        <Button size="sm" variant="secondary" onClick={props.onDropRequest} aria-label="Drop">
          <Trash2 className="h-3.5 w-3.5 text-destructive" />
        </Button>
        <Button size="sm" variant="secondary" onClick={props.onOpenQuery} aria-label="Open in query">
          <FileText className="h-3.5 w-3.5" />
        </Button>
        <Button size="sm" variant="secondary" onClick={props.onReload} aria-label="Reload">
          <RefreshCw className="h-3.5 w-3.5" />
        </Button>
      </div>
      <ErrorText message={t.error} />
      {t.def == null && !t.error ? (
        <EmptyNote text="Loading…" />
      ) : (
        <>
          {entries.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Properties</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableBody>
                    {entries.map(([k, v]) => (
                      <TableRow key={k}>
                        <TableCell className="w-40 font-medium text-muted-foreground">{k}</TableCell>
                        <TableCell className="whitespace-pre-wrap break-words">{fmt(v)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
          {isEnum && (
            <Card>
              <CardHeader>
                <CardTitle>Add value</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex items-center gap-1.5">
                  <Input
                    placeholder="new_label"
                    value={enumVal}
                    onChange={(e) => setEnumVal(e.target.value)}
                    aria-label="New enum value"
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      props.onEnumAdd(enumVal)
                      setEnumVal('')
                    }}
                    aria-label="Add enum value"
                  >
                    <Plus className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </CardContent>
            </Card>
          )}
          {editing && t.objectKind === 'sequence' && seq && (
            <Card>
              <CardHeader>
                <CardTitle>Edit options</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex flex-col gap-1.5">
                  {(
                    [
                      ['increment', 'Increment by'],
                      ['minValue', 'Min value (empty = unchanged)'],
                      ['maxValue', 'Max value (empty = unchanged)'],
                      ['cache', 'Cache size'],
                      ['restart', 'Restart with (empty = keep)'],
                    ] as const
                  ).map(([k, label]) => (
                    <label key={k} className="flex items-center gap-2 text-[12px]">
                      <span className="w-48 shrink-0 text-muted-foreground">{label}</span>
                      <Input value={seq[k]} onChange={(e) => setSeq({ ...seq, [k]: e.target.value })} aria-label={label} />
                    </label>
                  ))}
                  <label className="flex items-center gap-2 text-[12px]">
                    <span className="w-48 shrink-0 text-muted-foreground">Cycle</span>
                    <Switch checked={seq.cycle} onCheckedChange={(v) => setSeq({ ...seq, cycle: v })} aria-label="Cycle" />
                  </label>
                  <div className="flex gap-1.5">
                    <Button
                      size="sm"
                      onClick={() => {
                        props.onSaveSequence(seq)
                        setEditing(false)
                      }}
                    >
                      Save
                    </Button>
                    <Button size="sm" variant="secondary" onClick={() => setEditing(false)}>
                      Cancel
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
          {editing && t.objectKind === 'function' && (
            <Card>
              <CardHeader>
                <div className="flex items-center gap-1.5">
                  <CardTitle>Edit definition</CardTitle>
                  <span className="flex-1" />
                  <Button
                    size="sm"
                    onClick={() => {
                      props.onSaveFunction(funcSql)
                      setEditing(false)
                    }}
                  >
                    Save
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => setEditing(false)}>
                    Cancel
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                <Textarea
                  className="min-h-[240px] font-mono text-[12px]"
                  value={funcSql}
                  onChange={(e) => setFuncSql(e.target.value)}
                  aria-label="Function definition"
                  spellCheck={false}
                />
              </CardContent>
            </Card>
          )}
          {t.def != null && !(editing && t.objectKind === 'function') && (
            <Card>
              <CardHeader>
                <div className="flex items-center gap-1.5">
                  <CardTitle>Definition</CardTitle>
                  <span className="flex-1" />
                  <Button size="sm" variant="secondary" onClick={() => props.onCopy(t.def ?? '')} aria-label="Copy definition">
                    <Copy className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                <pre className="overflow-auto rounded bg-muted p-2 text-[12px]">{t.def}</pre>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  )
}
