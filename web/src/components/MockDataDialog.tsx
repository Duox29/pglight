import { useEffect, useMemo, useState } from 'react'
import { Eye, Loader2, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { ScrollArea } from './ui/scroll-area'
import { SearchSelect } from './ui/search-select'
import { DataGrid } from './ui/data-grid'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'
import { Tip } from './ui/tooltip'
import { EmptyNote, ErrorText } from './ui/feedback'
import {
  apiClient,
  type MockConstraint,
  type MockDataColumn,
  type MockDataMeta,
  type MockDataPreview,
  type MockFieldConfig,
} from '@/lib/api'

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  sessionId: string
  schema: string
  table: string
  onGenerated: () => void
}

const GENERATORS: { value: string; label: string }[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'db_default', label: 'DB Default' },
  { value: 'null', label: 'NULL' },
  { value: 'constant', label: 'Constant' },
  { value: 'integer', label: 'Integer' },
  { value: 'decimal', label: 'Decimal' },
  { value: 'boolean', label: 'Boolean' },
  { value: 'string', label: 'String' },
  { value: 'uuid', label: 'UUID' },
  { value: 'date', label: 'Date' },
  { value: 'datetime', label: 'DateTime' },
  { value: 'time', label: 'Time' },
  { value: 'choice', label: 'Choice' },
  { value: 'sequence', label: 'Sequence' },
  { value: 'json', label: 'JSON' },
  { value: 'array', label: 'Array' },
  { value: 'email', label: 'Email' },
  { value: 'first_name', label: 'First Name' },
  { value: 'last_name', label: 'Last Name' },
  { value: 'full_name', label: 'Full Name' },
  { value: 'username', label: 'Username' },
  { value: 'phone', label: 'Phone' },
  { value: 'url', label: 'URL' },
  { value: 'foreign_key', label: 'Foreign Key' },
  { value: 'relative_datetime', label: 'Relative DateTime' },
]

const OPERATORS = ['=', '!=', '<', '<=', '>', '>=']

interface FieldState {
  generator: string
  params: Record<string, string>
  unique: boolean
  nullProb: string
}

function defaultField(c: MockDataColumn): FieldState {
  const dbFilled = c.identity || c.generated || c.default != null
  return {
    generator: dbFilled ? 'db_default' : 'auto',
    params: {},
    unique: c.unique && !c.primary_key,
    nullProb: '',
  }
}

const NUM_KEYS = new Set([
  'min', 'max', 'scale', 'min_length', 'max_length', 'start', 'step',
  'min_items', 'max_items', 'min_offset_days', 'max_offset_days', 'true_probability',
])

function buildParams(params: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(params)) {
    const t = v.trim()
    if (t === '') continue
    if (k === 'values') {
      const vals = t.split(',').map((s) => s.trim()).filter(Boolean)
      if (vals.length) out[k] = vals
      continue
    }
    if (k === 'weights') {
      const nums = t.split(',').map((s) => Number(s.trim())).filter((n) => Number.isFinite(n))
      if (nums.length) out[k] = nums
      continue
    }
    if (NUM_KEYS.has(k)) {
      const n = Number(t)
      if (Number.isFinite(n)) out[k] = n
      continue
    }
    out[k] = t
  }
  return out
}

function ParamInput(p: {
  label: string
  value: string
  placeholder?: string
  onChange: (v: string) => void
}) {
  return (
    <label className="flex items-center gap-1 text-[11px] text-muted-foreground">
      {p.label}
      <Input className="h-7 w-[86px] text-[12px]" value={p.value} placeholder={p.placeholder} onChange={(e) => p.onChange(e.target.value)} />
    </label>
  )
}

export function MockDataDialog(p: Props) {
  const [meta, setMeta] = useState<MockDataMeta | null>(null)
  const [loadedKey, setLoadedKey] = useState('')
  const [metaErr, setMetaErr] = useState<string | undefined>()
  const [mode, setMode] = useState<'simple' | 'advanced'>('simple')
  const [count, setCount] = useState('1000')
  const [seed, setSeed] = useState('')
  const [fields, setFields] = useState<Record<string, FieldState>>({})
  const [constraints, setConstraints] = useState<MockConstraint[]>([])
  const [preview, setPreview] = useState<MockDataPreview | null>(null)
  const [busy, setBusy] = useState<'preview' | 'generate' | null>(null)

  const metaKey = `${p.sessionId}.${p.schema}.${p.table}`
  // Metadata loads once per table while the dialog is open; all setState
  // happens in async callbacks or event handlers (never sync in an effect).
  useEffect(() => {
    if (!p.open || loadedKey === metaKey) return
    apiClient
      .getMockDataMeta(p.sessionId, p.schema, p.table)
      .then((j) => {
        if (j.error) {
          setMetaErr(j.error)
          return
        }
        setMeta(j)
        const init: Record<string, FieldState> = {}
        for (const c of j.columns) init[c.name] = defaultField(c)
        setFields(init)
        setConstraints([])
        setPreview(null)
        setMode('simple')
        setLoadedKey(metaKey)
      })
      .catch((e) => setMetaErr(e instanceof Error ? e.message : String(e)))
  }, [p.open, metaKey, loadedKey, p.sessionId, p.schema, p.table])

  const shown = loadedKey === metaKey ? meta : null

  const countNum = useMemo(() => {
    const n = Math.floor(Number(count))
    return Number.isFinite(n) ? n : 0
  }, [count])
  const seedNum = useMemo(() => {
    const t = seed.trim()
    if (t === '') return undefined
    const n = Math.floor(Number(t))
    return Number.isFinite(n) ? n : undefined
  }, [seed])

  const setField = (col: string, patch: Partial<FieldState>) =>
    setFields((prev) => (prev[col] ? { ...prev, [col]: { ...prev[col], ...patch } } : prev))
  const setParam = (col: string, key: string, value: string) =>
    setFields((prev) =>
      prev[col] ? { ...prev, [col]: { ...prev[col], params: { ...prev[col].params, [key]: value } } } : prev,
    )

  const buildFieldConfigs = (): MockFieldConfig[] | undefined => {
    if (mode !== 'advanced' || !shown) return undefined
    return shown.columns.map((c) => {
      const f = fields[c.name] ?? defaultField(c)
      const cfg: MockFieldConfig = { column: c.name, generator: f.generator }
      const params = buildParams(f.params)
      if (Object.keys(params).length) cfg.params = params
      if (f.unique) cfg.unique = true
      const np = Number(f.nullProb)
      if (c.nullable && f.nullProb.trim() !== '' && Number.isFinite(np) && np > 0)
        cfg.null_probability = Math.min(100, Math.max(0, np)) / 100
      return cfg
    })
  }

  const runPreview = async () => {
    if (!countNum || countNum < 1) {
      toast.error('Records must be >= 1')
      return
    }
    setBusy('preview')
    try {
      const j = await apiClient.previewMockData({
        session_id: p.sessionId,
        schema: p.schema,
        table: p.table,
        mode,
        count: Math.min(countNum, 20),
        ...(seedNum !== undefined ? { seed: seedNum } : {}),
        ...(mode === 'advanced'
          ? { fields: buildFieldConfigs(), constraints: constraints.length ? constraints : undefined }
          : {}),
      })
      if (j.error) toast.error(j.error)
      else {
        setPreview(j)
        if ((j.warnings?.length ?? 0) > 0) toast.info(`${j.warnings?.length} warning${j.warnings?.length === 1 ? '' : 's'}`)
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const runGenerate = async () => {
    if (!countNum || countNum < 1) {
      toast.error('Records must be >= 1')
      return
    }
    if (countNum > 20000) {
      toast.error('Too many rows (max 20000 per request)')
      return
    }
    if (seed.trim() !== '' && seedNum === undefined) {
      toast.error('Seed must be an integer')
      return
    }
    setBusy('generate')
    try {
      const j = await apiClient.generateMockData({
        session_id: p.sessionId,
        schema: p.schema,
        table: p.table,
        mode,
        count: countNum,
        ...(seedNum !== undefined ? { seed: seedNum } : {}),
        ...(mode === 'advanced'
          ? { fields: buildFieldConfigs(), constraints: constraints.length ? constraints : undefined }
          : {}),
      })
      if (j.error) toast.error(j.error, { duration: 6000 })
      else {
        toast.success(`Generated ${j.inserted ?? j.generated ?? countNum} rows${j.in_txn ? ' (in transaction)' : ''}`)
        p.onGenerated()
        p.onOpenChange(false)
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const colNames = shown?.columns.map((c) => c.name) ?? []
  // FK columns (badge + source options), mirroring the pk/unique badges.
  const fkByColumn = useMemo(() => {
    const m = new Map<string, { ref_schema: string; ref_table: string; ref_column: string }[]>()
    for (const fk of shown?.foreign_keys ?? []) {
      const list = m.get(fk.column) ?? []
      list.push({ ref_schema: fk.ref_schema, ref_table: fk.ref_table, ref_column: fk.ref_column })
      m.set(fk.column, list)
    }
    return m
  }, [shown])

  return (
    <Dialog open={p.open} onOpenChange={p.onOpenChange}>
      <DialogContent className="max-h-[86vh] w-[min(1200px,96vw)] overflow-hidden [&>*]:min-h-0 [&>*]:min-w-0">
        <DialogHeader>
          <DialogTitle>
            Generate Mock Data · {p.schema}.{p.table}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-[12px] text-muted-foreground">
            Records
            <Input className="h-8 w-[110px]" value={count} inputMode="numeric" onChange={(e) => setCount(e.target.value)} />
          </label>
          <div className="flex flex-col gap-1 text-[12px] text-muted-foreground">
            Mode
            <div className="flex gap-1" role="radiogroup" aria-label="Generation mode">
              {(['simple', 'advanced'] as const).map((m) => (
                <Button key={m} size="sm" variant={mode === m ? 'default' : 'secondary'} onClick={() => setMode(m)}>
                  {m === 'simple' ? 'Simple' : 'Advanced'}
                </Button>
              ))}
            </div>
          </div>
          <label className="flex flex-col gap-1 text-[12px] text-muted-foreground">
            Seed
            <Input className="h-8 w-[130px]" value={seed} placeholder="optional" inputMode="numeric" onChange={(e) => setSeed(e.target.value)} />
          </label>
          <span className="flex-1" />
          <Button size="sm" variant="secondary" onClick={runPreview} disabled={busy != null || shown == null} className="shrink-0 whitespace-nowrap">
            {busy === 'preview' ? <Loader2 className="animate-spin" /> : <Eye />} Preview
          </Button>
          <Button size="sm" onClick={runGenerate} disabled={busy != null || shown == null} className="shrink-0 whitespace-nowrap">
            {busy === 'generate' ? <Loader2 className="animate-spin" /> : null}
            Generate {countNum > 0 ? countNum.toLocaleString() : ''} Rows
          </Button>
        </div>
        {mode === 'simple' && (
          <div className="text-[12px] text-muted-foreground">
            Datatype-only generation — CHECK, UNIQUE, foreign keys and defaults are validated by PostgreSQL on insert.
          </div>
        )}
        <ErrorText message={metaErr} />
        {!shown && !metaErr && <EmptyNote text="Loading table metadata…" />}
        {mode === 'advanced' && shown && (
          <ScrollArea className="max-h-[30vh] rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Column</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Generator</TableHead>
                  <TableHead>Config</TableHead>
                  <TableHead>NULL %</TableHead>
                  <TableHead>Unique</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {shown.columns.map((c) => {
                  const f = fields[c.name] ?? defaultField(c)
                  return (
                    <TableRow key={c.name}>
                      <TableCell className="font-medium">
                        {c.name}
                        <span className="ml-1 inline-flex gap-1">
                          {c.primary_key && <Badge variant="secondary">pk</Badge>}
                          {fkByColumn.has(c.name) && (
                            <Tip content={`references ${(fkByColumn.get(c.name) ?? []).map((r) => `${r.ref_schema}.${r.ref_table}.${r.ref_column}`).join(', ')}`}>
                              <span className="inline-flex"><Badge variant="secondary">fk</Badge></span>
                            </Tip>
                          )}
                          {c.unique && !c.primary_key && <Badge variant="secondary">unique</Badge>}
                          {!c.nullable && <Badge variant="outline">not null</Badge>}
                        </span>
                      </TableCell>
                      <TableCell className="max-w-[140px] truncate font-mono text-[12px]">{c.data_type}</TableCell>
                      <TableCell>
                        <Select value={f.generator} onValueChange={(v) => setField(c.name, { generator: v })}>
                          <SelectTrigger className="h-7 w-[150px]">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {GENERATORS.map((g) => (
                              <SelectItem key={g.value} value={g.value}>
                                {g.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <GenConfig
                          generator={f.generator}
                          params={f.params}
                          columns={colNames}
                          enumValues={c.enum_values}
                          fkRefs={fkByColumn.get(c.name) ?? []}
                          onParam={(k, v) => setParam(c.name, k, v)}
                        />
                      </TableCell>
                      <TableCell>
                        {c.nullable ? (
                          <Input
                            className="h-7 w-[64px]"
                            value={f.nullProb}
                            placeholder="0"
                            inputMode="numeric"
                            onChange={(e) => setField(c.name, { nullProb: e.target.value })}
                          />
                        ) : (
                          <span className="text-[11px] text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <Switch checked={f.unique} onCheckedChange={(v) => setField(c.name, { unique: v })} aria-label={`Unique ${c.name}`} />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </ScrollArea>
        )}
        {mode === 'advanced' && shown && (
          <div className="flex flex-col gap-1.5">
            <div className="text-[12px] font-semibold">Constraints</div>
            {constraints.map((cc, i) => (
              <div key={i} className="flex items-center gap-1.5">
                <Select value={cc.left.field ?? ''} onValueChange={(v) => setConstraints((prev) => prev.map((x, xi) => (xi === i ? { ...x, left: { field: v } } : x)))}>
                  <SelectTrigger className="h-7 w-[150px]">
                    <SelectValue placeholder="column" />
                  </SelectTrigger>
                  <SelectContent>
                    {colNames.map((n) => (
                      <SelectItem key={n} value={n}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select value={cc.operator} onValueChange={(v) => setConstraints((prev) => prev.map((x, xi) => (xi === i ? { ...x, operator: v } : x)))}>
                  <SelectTrigger className="h-7 w-[70px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {OPERATORS.map((o) => (
                      <SelectItem key={o} value={o}>
                        {o}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select value={cc.right.field ?? ''} onValueChange={(v) => setConstraints((prev) => prev.map((x, xi) => (xi === i ? { ...x, right: { field: v } } : x)))}>
                  <SelectTrigger className="h-7 w-[150px]">
                    <SelectValue placeholder="column" />
                  </SelectTrigger>
                  <SelectContent>
                    {colNames.map((n) => (
                      <SelectItem key={n} value={n}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Tip content="Remove constraint">
                  <Button size="sm" variant="ghost" aria-label="Remove constraint" onClick={() => setConstraints((prev) => prev.filter((_, xi) => xi !== i))}>
                    <Trash2 />
                  </Button>
                </Tip>
              </div>
            ))}
            <div>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setConstraints((prev) => [...prev, { kind: 'compare', left: {}, operator: '<=', right: {} }])}
              >
                <Plus /> Add constraint
              </Button>
            </div>
          </div>
        )}
        {preview && (
          <div className="flex min-h-0 flex-col gap-1.5">
            <div className="text-[12px] text-muted-foreground">
              {preview.rows?.length ?? 0} preview rows{preview.seed != null ? ` · seed ${preview.seed}` : ''}
              {(preview.warnings?.length ?? 0) > 0 && (
                <span className="text-yellow-500"> · {preview.warnings?.length} warning{(preview.warnings?.length ?? 0) === 1 ? '' : 's'}</span>
              )}
            </div>
            {(preview.warnings?.length ?? 0) > 0 && (
              <ScrollArea className="max-h-[12vh] rounded-md border border-yellow-900/50 p-2">
                {preview.warnings?.map((w, i) => (
                  <div key={i} className="text-[12px] text-yellow-500">
                    {w}
                  </div>
                ))}
              </ScrollArea>
            )}
            <ScrollArea className="max-h-[26vh] rounded-md border">
              {/* Same result grid as the query console (sticky header,
                  truncated cells with Tips, NULL styling, zebra rows). */}
              <DataGrid
                data={{ columns: preview.columns ?? [], rows: preview.rows ?? [] }}
                cellClassName={(v) => (v === '<database default>' ? 'text-muted-foreground' : undefined)}
              />
            </ScrollArea>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

export interface MockFKRef {
  ref_schema: string
  ref_table: string
  ref_column: string
}

function fkRefValue(r: MockFKRef): string {
  return `${r.ref_schema}.${r.ref_table}.${r.ref_column}`
}

function GenConfig(p: {
  generator: string
  params: Record<string, string>
  columns: string[]
  enumValues: string[]
  fkRefs: MockFKRef[]
  onParam: (k: string, v: string) => void
}) {
  const g = p.generator
  const val = (k: string) => p.params[k] ?? ''
  // FK value pool: explicit source (values from the referenced table) or the
  // column's own FK default. Shown for Foreign Key and for Auto on FK columns
  // (Auto resolves to Foreign Key server-side).
  if ((g === 'foreign_key' || g === 'auto') && p.fkRefs.length > 0) {
    const cur = ['ref_schema', 'ref_table', 'ref_column'].map((k) => (p.params[k] ?? '').trim())
    const curIdx = p.fkRefs.findIndex(
      (r) => r.ref_schema === cur[0] && r.ref_table === cur[1] && r.ref_column === cur[2],
    )
    const selIdx = curIdx >= 0 ? curIdx : 0
    const pickSource = (i: number) => {
      const r = p.fkRefs[i]
      if (!r) return
      p.onParam('ref_schema', r.ref_schema)
      p.onParam('ref_table', r.ref_table)
      p.onParam('ref_column', r.ref_column)
    }
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-1">
          <span className="text-[11px] text-muted-foreground">Source</span>
          <SearchSelect
            value={String(selIdx)}
            options={p.fkRefs.map((r, i) => ({ value: String(i), label: fkRefValue(r) }))}
            ariaLabel="FK value source"
            triggerClassName="w-[170px]"
            onChange={(v) => pickSource(Number(v))}
          />
        </div>
        {g === 'foreign_key' && (
          <div className="flex items-center gap-1">
            <span className="text-[11px] text-muted-foreground">Mode</span>
            <Select value={val('mode') || 'random'} onValueChange={(v) => p.onParam('mode', v)}>
              <SelectTrigger className="h-7 w-[110px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="random">Uniform</SelectItem>
                <SelectItem value="sequential">Sequential</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}
      </div>
    )
  }
  switch (g) {
    case 'integer':
      return (
        <div className="flex gap-1">
          <ParamInput label="min" value={val('min')} placeholder="0" onChange={(v) => p.onParam('min', v)} />
          <ParamInput label="max" value={val('max')} placeholder="10000" onChange={(v) => p.onParam('max', v)} />
        </div>
      )
    case 'decimal':
      return (
        <div className="flex gap-1">
          <ParamInput label="min" value={val('min')} placeholder="0" onChange={(v) => p.onParam('min', v)} />
          <ParamInput label="max" value={val('max')} placeholder="10000" onChange={(v) => p.onParam('max', v)} />
          <ParamInput label="scale" value={val('scale')} placeholder="2" onChange={(v) => p.onParam('scale', v)} />
        </div>
      )
    case 'string':
      return (
        <div className="flex gap-1">
          <ParamInput label="min" value={val('min_length')} placeholder="8" onChange={(v) => p.onParam('min_length', v)} />
          <ParamInput label="max" value={val('max_length')} placeholder="24" onChange={(v) => p.onParam('max_length', v)} />
        </div>
      )
    case 'choice':
      return (
        <div className="flex gap-1">
          <ParamInput
            label="values"
            value={val('values') || p.enumValues.join(', ')}
            placeholder="a, b, c"
            onChange={(v) => p.onParam('values', v)}
          />
          <ParamInput label="weights" value={val('weights')} placeholder="1, 1" onChange={(v) => p.onParam('weights', v)} />
        </div>
      )
    case 'sequence':
      return (
        <div className="flex gap-1">
          <ParamInput label="start" value={val('start')} placeholder="1" onChange={(v) => p.onParam('start', v)} />
          <ParamInput label="step" value={val('step')} placeholder="1" onChange={(v) => p.onParam('step', v)} />
        </div>
      )
    case 'date':
    case 'datetime':
      return (
        <div className="flex gap-1">
          <ParamInput label="min" value={val('min')} placeholder="2020-01-01" onChange={(v) => p.onParam('min', v)} />
          <ParamInput label="max" value={val('max')} placeholder="2026-01-01" onChange={(v) => p.onParam('max', v)} />
        </div>
      )
    case 'constant':
      return (
        <div className="flex gap-1">
          <ParamInput label="value" value={val('value')} placeholder="fixed value" onChange={(v) => p.onParam('value', v)} />
        </div>
      )
    case 'array':
      return (
        <div className="flex gap-1">
          <ParamInput label="min" value={val('min_items')} placeholder="0" onChange={(v) => p.onParam('min_items', v)} />
          <ParamInput label="max" value={val('max_items')} placeholder="5" onChange={(v) => p.onParam('max_items', v)} />
        </div>
      )
    case 'foreign_key':
      return (
        <div className="flex items-center gap-1">
          <span className="text-[11px] text-muted-foreground">Mode</span>
          <Select value={val('mode') || 'random'} onValueChange={(v) => p.onParam('mode', v)}>
            <SelectTrigger className="h-7 w-[110px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="random">Uniform</SelectItem>
              <SelectItem value="sequential">Sequential</SelectItem>
            </SelectContent>
          </Select>
        </div>
      )
    case 'relative_datetime':
      return (
        <div className="flex items-center gap-1">
          <Select value={val('source')} onValueChange={(v) => p.onParam('source', v)}>
            <SelectTrigger className="h-7 w-[120px]">
              <SelectValue placeholder="source" />
            </SelectTrigger>
            <SelectContent>
              {p.columns.map((n) => (
                <SelectItem key={n} value={n}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <ParamInput label="+" value={val('min_offset_days')} placeholder="0d" onChange={(v) => p.onParam('min_offset_days', v)} />
          <ParamInput label="–" value={val('max_offset_days')} placeholder="30d" onChange={(v) => p.onParam('max_offset_days', v)} />
        </div>
      )
    default:
      return <span className="text-[11px] text-muted-foreground">—</span>
  }
}
