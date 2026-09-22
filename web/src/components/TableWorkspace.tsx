import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, FileDown, Pencil, Plus, RefreshCw, Sparkles, Trash2, Upload, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { Input } from './ui/input'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { Switch } from './ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'
import { ErrorText, EmptyNote } from './ui/feedback'
import type { TableSubtab, TableTabT } from '@/types'
import type { DialogsApi } from './dialogs'
import { ColumnDialog, ConstraintDialog, type ColumnValues } from './ColumnEditor'
import { IndexDialog, TriggerDialog } from './IndexTriggerEditor'
import { MockDataDialog } from './MockDataDialog'
import { download, parseCSV, quoteQualified, resultToCSV, resultToInserts } from '@/lib/format'
import { useGridSelection } from '@/hooks/useGridSelection'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from './ui/context-menu'

interface Props {
  tab: TableTabT
  onSubtab: (s: TableSubtab) => void
  onFilterChange: (filter: string, order: string) => void
  onApply: () => void
  onPage: (d: number) => void
  onEditCell: (col: string, orig: Record<string, unknown>) => void
  onDeleteRow: (orig: Record<string, unknown>) => void
  onCopyInsert: (orig: Record<string, unknown>) => void
  onExportRows: (rows: Record<string, unknown>[], fmt: 'csv' | 'json' | 'sql') => void
  onCopyRows: (rows: Record<string, unknown>[]) => void
  onDeleteRows: (rows: Record<string, unknown>[]) => void
  onInsert: () => void
  onMaintenance: (op: string) => void
  onImport: (columns: string[], rows: unknown[][]) => void
  onOpenErd: () => void
  onAlter: (p: {
    op: string
    column?: string
    new_name?: string
    type?: string
    nullable?: boolean
    default?: string
    drop_default?: boolean
    constraint?: string
    def?: string
    cascade?: boolean
    index?: string
    unique?: boolean
    method?: string
    columns?: string[]
    include?: string[]
    where?: string
    trigger?: string
    timing?: string
    events?: string[]
    for_each?: string
    function?: string
    when?: string
    update_of?: string[]
  }) => Promise<void>
  onRenameTable: () => void
  dialogs: DialogsApi
}

export function TableWorkspace(p: Props) {
  const { tab: t } = p
  const fileRef = useRef<HTMLInputElement>(null)
  const [colDlg, setColDlg] = useState<{ mode: 'add' | 'edit'; initial: ColumnValues } | null>(null)
  const [conDlg, setConDlg] = useState(false)
  const [idxDlg, setIdxDlg] = useState(false)
  const [trgDlg, setTrgDlg] = useState(false)
  const [mockOpen, setMockOpen] = useState(false)
  const colNames = (t.cols ?? []).map((c) => String(c['name'] ?? '')).filter(Boolean)
  const pkCols = (t.cols ?? [])
    .filter((c) => String(c['pk'] ?? '').toLowerCase() === 't' || c['pk'] === true)
    .map((c) => String(c['name'] ?? ''))
    .filter(Boolean)
  const pkIdx = pkCols.map((c) => (t.result?.columns ?? []).indexOf(c)).filter((i) => i >= 0)
  const hasIdentity = pkIdx.length > 0
  // Bulk row selection. Keyed by primary-key values when the table exposes
  // them; falls back to row index plus serialized content.
  // Flow: plain click = select single (drops the rest), ctrl/meta = toggle,
  // shift = range, right-click keeps multi-selection when inside it.
  const pageRows = t.result?.rows ?? []
  const pageCols = t.result?.columns ?? []
  const rowKey = (r: unknown[], index: number) => (hasIdentity ? JSON.stringify(pkIdx.map((i) => r[i])) : JSON.stringify([index, r]))
  const gridSel = useGridSelection(pageRows, rowKey)
  const { sel, setSel } = gridSel
  // New page / new data → drop the old selection (keys belong to other rows).
  useEffect(() => {
    gridSel.clear()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t.id, t.offset, t.result])
  const selRecs = pageRows.filter((r, i) => sel.has(rowKey(r, i))).map((r) => Object.fromEntries(pageCols.map((c, i) => [c, r[i]])))

  const startImport = () => fileRef.current?.click()

  const handleFile = async (file: File) => {
    const maxImportFileBytes = 16 * 1024 * 1024
    if (file.size > maxImportFileBytes) {
      toast.error('File is too large (maximum 16 MiB)')
      return
    }
    const text = await file.text()
    const name = file.name.toLowerCase()
    const rows = parseCSV(text, name.endsWith('.tsv') ? '\t' : undefined)
    if (!rows.length) {
      toast.error('Empty file')
      return
    }
    const widths = new Set(rows.map((r) => r.length))
    if (widths.size > 1) {
      toast.error(`Ragged ${name.endsWith('.tsv') ? 'TSV' : 'CSV'}: rows have ${[...widths].sort((a, b) => a - b).join('/')} columns`)
      return
    }
    const header = rows[0].map((h) => h.trim())
    const tableCols = t.result?.columns ?? []
    const mapIdx = header.map((h) => tableCols.findIndex((c) => c.toLowerCase() === h.toLowerCase()))
    const useHeader = mapIdx.some((i) => i >= 0)
    let cols: string[]
    let data: unknown[][]
    if (useHeader) {
      const keep = mapIdx.map((ci, i) => ({ ci, i })).filter((x) => x.ci >= 0)
      cols = keep.map((x) => tableCols[x.ci])
      data = rows.slice(1).map((r) => keep.map((x) => (r[x.i] ?? '').trim() === '' ? '' : r[x.i]))
    } else {
      cols = tableCols
      data = rows.map((r) => r.map((v) => v))
    }
    if (!cols.length) {
      toast.error('No matching columns. Header: ' + header.join(', '))
      return
    }
    const ok = await p.dialogs.confirm({
      title: 'Import CSV',
      description: `Import ${data.length} rows into ${t.schema}.${t.table} (${cols.join(', ')})?${useHeader && cols.length < header.length ? ` ${header.length - cols.length} unmatched CSV column(s) will be ignored.` : ''}`,
      confirmText: 'Import',
    })
    if (!ok) return
    p.onImport(cols, data)
  }

  const copyDdl = () => {
    if (t.ddl?.ddl && navigator.clipboard) navigator.clipboard.writeText(t.ddl.ddl)
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2 overflow-hidden">
      <div className="flex flex-wrap items-end gap-1.5">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{t.table}</div>
          <div className="truncate text-[11px] text-muted-foreground">
            {t.schema}{t.result?.total != null ? ` · ${t.result.total} rows` : ''}{t.ddl?.owner ? ` · owner ${t.ddl.owner}` : ''}
          </div>
        </div>
        <span className="flex-1" />
        <Tabs value={t.subtab === 'constraints' || t.subtab === 'triggers' ? 'columns' : t.subtab} onValueChange={(v) => p.onSubtab(v as TableSubtab)}>
          <TabsList aria-label="Table sections">
            <TabsTrigger value="data">Data</TabsTrigger>
            <TabsTrigger value="columns">Structure</TabsTrigger>
            <TabsTrigger value="ddl">SQL</TabsTrigger>
            <TabsTrigger value="indexes">Indexes</TabsTrigger>
            <TabsTrigger value="stats">Stats</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      {t.subtab === 'data' && (
        <div className="flex min-h-0 flex-1 flex-col gap-2">
          <div className="flex flex-wrap items-center gap-1.5">
            <Input
              className="w-[240px]"
              placeholder="WHERE … e.g. id > 10"
              defaultValue={t.filter}
              id={`flt-${t.id}`}
            />
            <Input className="w-[150px]" placeholder="ORDER … e.g. id DESC" defaultValue={t.order} id={`ord-${t.id}`} />
            <Button
              size="sm"
              onClick={() => {
                const f = (document.getElementById(`flt-${t.id}`) as HTMLInputElement)?.value ?? t.filter
                const o = (document.getElementById(`ord-${t.id}`) as HTMLInputElement)?.value ?? t.order
                p.onFilterChange(f, o)
              }}
            >
              Apply
            </Button>
            <Button size="sm" variant="secondary" onClick={p.onInsert}>
              <Plus /> Row
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => t.result && download(resultToCSV(t.result.columns, t.result.rows), `${t.table}.csv`, 'text/csv')}
            >
              <FileDown /> CSV
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => t.result && download(resultToInserts(t.result.columns, t.result.rows, quoteQualified(t.schema, t.table), t.result.types), `${t.table}.sql`, 'text/sql')}
            >
              INSERTs
            </Button>
            <Button size="sm" variant="ghost" onClick={startImport}>
              <Upload /> Import CSV
            </Button>
            <Tip content="Generate mock data">
              <Button size="sm" variant="ghost" onClick={() => setMockOpen(true)} aria-label="Generate mock data">
                <Sparkles /> Generate
              </Button>
            </Tip>
            <MockDataDialog
              open={mockOpen}
              onOpenChange={setMockOpen}
              sessionId={t.sessionId}
              schema={t.schema}
              table={t.table}
              onGenerated={p.onApply}
            />
            <input
              ref={fileRef}
              type="file"
              accept=".csv,.tsv,.txt"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) handleFile(f)
                e.target.value = ''
              }}
            />
          </div>
          <div className="flex items-center gap-1.5">
            <Button size="sm" variant="ghost" onClick={() => p.onPage(-1)} disabled={t.offset === 0} aria-label="Previous page">
              <ArrowLeft />
            </Button>
            <span className="text-[12px] text-muted-foreground">offset {t.offset}</span>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => p.onPage(1)}
              disabled={
                t.result?.has_more === false ||
                (t.result?.has_more == null &&
                  (t.result?.total != null ? t.offset + t.limit >= t.result.total : (t.result?.rows.length ?? t.limit) < t.limit))
              }
              aria-label="Next page"
            >
              <ArrowRight />
            </Button>
            <span className="text-[12px] text-muted-foreground">
              {t.result ? <><b className="font-medium text-foreground">{t.result.rows.length} rows</b>{t.result.total != null ? ` / ${t.result.total} total` : ''} · {t.result.duration_ms}ms</> : ''}
            </span>
            {selRecs.length > 0 && (
              <>
                <span className="text-[12px] font-semibold">{selRecs.length} selected</span>
                <Button size="sm" variant="ghost" onClick={() => setSel(new Set())}>
                  Clear
                </Button>
              </>
            )}
            <span className="flex-1" />
            <Tip content="Reload">
              <Button size="sm" variant="ghost" onClick={p.onApply} aria-label="Reload">
                <RefreshCw />
              </Button>
            </Tip>
          </div>
          <ErrorText message={t.error} />
          {t.result ? (
            <ContextMenu>
              <ContextMenuTrigger asChild>
                <div className="flex min-h-0 flex-1 flex-col">
                <Table containerClassName="min-h-0 max-h-full flex-1">
                  <TableHeader>
                    <TableRow onContextMenu={() => gridSel.setCtxCell(null)}>
                      {t.result.columns.map((c) => (
                        <TableHead key={c}>{c}</TableHead>
                      ))}
                      <TableHead aria-label="Row actions"><Pencil className="h-3 w-3 text-muted-foreground" /></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {t.result.rows.map((r, ri) => {
                      const orig = Object.fromEntries(t.result!.columns.map((c, i) => [c, r[i]]))
                      const k = rowKey(r, ri)
                      return (
                        <TableRow
                          key={ri}
                          data-state={sel.has(k) ? 'selected' : undefined}
                          onContextMenu={() => gridSel.handleRowContextMenu(ri, r)}
                        >
                          {r.map((c, ci) => (
                              <TableCell
                                key={ci}
                                className="cursor-text bg-sky-950/30"
                                onDoubleClick={() => p.onEditCell(t.result!.columns[ci], orig)}
                                onClick={(e) => {
                                  if (e.detail > 1) return
                                  gridSel.handleCellClick(e, ri, r)
                                }}
                                // Record the cell for "Copy cell value" but
                                // NEVER preventDefault — that would stop the
                                // Radix menu from opening (see useGridSelection).
                                onContextMenu={() => {
                                  gridSel.handleCellContextMenu(c, t.result!.columns[ci])
                                  gridSel.handleRowContextMenu(ri, r)
                                }}
                              >
                                {c == null ? <span className="italic text-muted-foreground">NULL</span> : String(c).slice(0, 200)}
                              </TableCell>
                          ))}
                          <TableCell>
                            <div className="flex gap-1">
                              <Tip content="Copy row as INSERT">
                                <Button size="sm" variant="ghost" aria-label="Copy row as INSERT" onClick={() => p.onCopyInsert(orig)}>
                                  <Copy className="h-3 w-3" />
                                </Button>
                              </Tip>
                              <Tip content={hasIdentity ? 'Delete row' : 'No primary key — deletion disabled'}>
                                <span className="inline-flex">
                                  <Button size="sm" variant="ghost" aria-label="Delete row" disabled={!hasIdentity} onClick={() => p.onDeleteRow(orig)}>
                                    <Trash2 className="h-3 w-3" />
                                  </Button>
                                </span>
                              </Tip>
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
                </div>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuLabel>{selRecs.length ? `${selRecs.length} row${selRecs.length === 1 ? '' : 's'} selected` : 'No rows selected'}</ContextMenuLabel>
                <ContextMenuItem
                  disabled={gridSel.ctxCell == null || gridSel.ctxCell.value == null}
                  onSelect={() => {
                    if (gridSel.ctxCell?.value != null && navigator.clipboard) navigator.clipboard.writeText(String(gridSel.ctxCell.value))
                  }}
                >
                  Copy cell value{gridSel.ctxCell ? ` (${gridSel.ctxCell.col})` : ''}
                </ContextMenuItem>
                <ContextMenuSub>
                  <ContextMenuSubTrigger disabled={!selRecs.length}>Export</ContextMenuSubTrigger>
                  <ContextMenuSubContent>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'csv')}>CSV</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'json')}>JSON</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'sql')}>INSERTs</ContextMenuItem>
                  </ContextMenuSubContent>
                </ContextMenuSub>
                <ContextMenuItem disabled={!selRecs.length} onSelect={() => p.onCopyRows(selRecs)}>
                  Copy {selRecs.length ? `${selRecs.length} row${selRecs.length === 1 ? '' : 's'}` : 'rows'}
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem disabled={!selRecs.length || !hasIdentity} className="text-red-400 focus:text-red-400" onSelect={() => p.onDeleteRows(selRecs)}>
                  {hasIdentity ? 'Delete' : 'Delete (no primary key)'}
                </ContextMenuItem>
                <ContextMenuItem disabled={!selRecs.length} onSelect={gridSel.clear}>
                  Clear selection
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          ) : (
            <EmptyNote text={t.error ? 'Failed to load' : 'Loading…'} />
          )}
        </div>
      )}

      <Tabs value={t.subtab} className={t.subtab === 'data' ? 'hidden' : 'flex min-h-0 flex-1 flex-col'}>
        {(t.subtab === 'columns' || t.subtab === 'constraints' || t.subtab === 'triggers') && (
          <div className="flex items-center gap-1 text-[12px]" role="tablist" aria-label="Structure sections">
            {(['columns', 'constraints', 'triggers'] as TableSubtab[]).map((s) => (
              <button
                key={s}
                role="tab"
                aria-selected={s === t.subtab}
                onClick={() => p.onSubtab(s)}
                className={s === t.subtab ? 'rounded bg-muted px-2 py-1 font-semibold capitalize text-foreground' : 'rounded px-2 py-1 capitalize text-muted-foreground hover:text-foreground'}
              >
                {s}
              </button>
            ))}
          </div>
        )}
        <TabsContent value="columns" className="min-h-0 flex-1 overflow-auto">
          <div className="mb-2 flex items-center gap-1.5">
            <span className="text-[12px] text-muted-foreground">{t.cols ? `${t.cols.length} columns` : ''} · double-click a row to edit</span>
            <span className="flex-1" />
            <Button size="sm" variant="ghost" onClick={p.onRenameTable}>
              <Pencil /> Rename table
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setColDlg({ mode: 'add', initial: { name: '', type: '', nullable: true, defDefault: '' } })}
            >
              <Plus /> Column
            </Button>
          </div>
          {t.cols ? (
            <Table containerClassName="min-h-0 max-h-full">
              <TableHeader>
                <TableRow>
                  {['name', 'type', 'nullable', 'default', 'pk', 'comment'].map((c) => <TableHead key={c}>{c}</TableHead>)}
                  <TableHead className="w-[76px]">actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {t.cols.map((c, i) => {
                  const nm = String(c['name'] ?? '')
                  const nullable = String(c['nullable'] ?? '').toUpperCase() === 'YES' || c['nullable'] === true
                  const initial: ColumnValues = {
                    name: nm,
                    type: String(c['type'] ?? ''),
                    nullable,
                    defDefault: c['default'] == null ? '' : String(c['default']),
                  }
                  return (
                    <TableRow key={i} className="cursor-pointer" onDoubleClick={() => setColDlg({ mode: 'edit', initial })}>
                      {['name', 'type', 'nullable', 'default', 'pk', 'comment'].map((k) => (
                        <TableCell key={k} className="max-w-[320px] truncate">{c[k] == null ? '' : String(c[k])}</TableCell>
                      ))}
                      <TableCell>
                        <div className="flex gap-1">
                          <Tip content="Edit column (rename / type / nullable / default)">
                            <Button size="sm" variant="ghost" aria-label={`Edit ${nm}`} onClick={() => setColDlg({ mode: 'edit', initial })}>
                              <Pencil />
                            </Button>
                          </Tip>
                          <Tip content={`Drop column ${nm}`}>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={`Drop ${nm}`}
                              onClick={async () => {
                                const ok = await p.dialogs.confirm({
                                  title: `Drop column ${nm}?`,
                                  description: `${t.schema}.${t.table}.${nm} will be removed. This cannot be undone.`,
                                  confirmText: 'Drop',
                                  danger: true,
                                })
                                if (ok) p.onAlter({ op: 'drop_column', column: nm })
                              }}
                            >
                              <Trash2 />
                            </Button>
                          </Tip>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading columns…" />
          )}
          {colDlg && (
            <ColumnDialog
              open
              onOpenChange={(v) => { if (!v) setColDlg(null) }}
              mode={colDlg.mode}
              initial={colDlg.initial}
              onSubmit={async (nv) => {
                if (colDlg.mode === 'add') {
                  await p.onAlter({ op: 'add_column', column: nv.name, type: nv.type, nullable: nv.nullable, default: nv.defDefault })
                  return
                }
                const orig = colDlg.initial
                let cur = orig.name
                if (nv.name !== orig.name) {
                  await p.onAlter({ op: 'rename_column', column: cur, new_name: nv.name })
                  cur = nv.name
                }
                if (nv.type !== orig.type) await p.onAlter({ op: 'alter_type', column: cur, type: nv.type })
                if (nv.nullable !== orig.nullable) await p.onAlter({ op: 'set_nullable', column: cur, nullable: nv.nullable })
                if (nv.defDefault !== orig.defDefault) {
                  if (!nv.defDefault) await p.onAlter({ op: 'set_default', column: cur, drop_default: true })
                  else await p.onAlter({ op: 'set_default', column: cur, default: nv.defDefault })
                }
                if (nv.name === orig.name && nv.type === orig.type && nv.nullable === orig.nullable && nv.defDefault === orig.defDefault) {
                  toast.info('No changes')
                }
              }}
            />
          )}
        </TabsContent>
        <TabsContent value="ddl" className="min-h-0 flex-1 overflow-auto">
          {t.ddl ? (
            <div className="flex flex-col gap-2">
              <div className="text-[12px] text-muted-foreground">
                owner: {t.ddl.owner} {t.ddl.comment}
              </div>
              <Card className="p-2.5">
                <div className="mb-1 flex items-center gap-2">
                  <span className="font-mono text-[12px] font-semibold">CREATE TABLE {t.schema}.{t.table}</span>
                  <span className="flex-1" />
                  <Button size="sm" variant="ghost" onClick={copyDdl} aria-label="Copy DDL">
                    <Copy className="h-3.5 w-3.5" /> Copy
                  </Button>
                </div>
                <pre className="overflow-auto rounded-md border border-border bg-background p-2.5 font-mono text-[12px] leading-relaxed">{t.ddl.ddl}</pre>
              </Card>
              <Card className="p-2.5">
                <div className="mb-1 text-[12px] font-semibold">Constraints</div>
                <pre className="overflow-auto rounded-md border border-border/60 bg-muted/40 p-2 font-mono text-[12px]">{(t.ddl.constraints ?? []).map((c) => c.def).join('\n') || '—'}</pre>
              </Card>
              <Card className="p-2.5">
                <div className="mb-1 text-[12px] font-semibold">Indexes</div>
                <pre className="overflow-auto rounded-md border border-border/60 bg-muted/40 p-2 font-mono text-[12px]">{(t.ddl.indexes ?? []).map((c) => c.def).join('\n') || '—'}</pre>
              </Card>
            </div>
          ) : (
            <EmptyNote text="Loading DDL…" />
          )}
        </TabsContent>
        <TabsContent value="indexes" className="min-h-0 flex-1 overflow-auto">
          <div className="mb-2 flex items-center gap-1.5">
            <span className="text-[12px] text-muted-foreground">{t.ddl ? `${t.ddl.indexes?.length ?? 0} indexes` : ''}</span>
            <span className="flex-1" />
            <Button size="sm" variant="secondary" onClick={() => setIdxDlg(true)}>
              <Plus /> Index
            </Button>
          </div>
          {t.ddl ? (
            <Table containerClassName="min-h-0 max-h-full">
              <TableHeader>
                <TableRow>
                  <TableHead>name</TableHead>
                  <TableHead>definition</TableHead>
                  <TableHead className="w-[96px]">actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(t.ddl.indexes ?? []).map((x, i) => (
                  <TableRow key={i}>
                    <TableCell className="max-w-[240px] truncate">{x.name}</TableCell>
                    <TableCell className="font-mono">{x.def}</TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Tip content={`Rename index ${x.name}`}>
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={`Rename ${x.name}`}
                            onClick={async () => {
                              const v = await p.dialogs.prompt({ title: `Rename index ${x.name}`, defaultValue: x.name })
                              if (v == null) return
                              const nn = v.trim()
                              if (!nn || nn === x.name) return
                              p.onAlter({ op: 'rename_index', index: x.name, new_name: nn })
                            }}
                          >
                            <Pencil />
                          </Button>
                        </Tip>
                        <Tip content={`Drop index ${x.name}`}>
                          <Button
                            size="sm"
                            variant="ghost"
                            aria-label={`Drop ${x.name}`}
                            onClick={async () => {
                              const ok = await p.dialogs.confirm({
                                title: `Drop index ${x.name}?`,
                                description: 'Queries may get slower. This cannot be undone.',
                                confirmText: 'Drop',
                                danger: true,
                              })
                              if (ok) p.onAlter({ op: 'drop_index', index: x.name })
                            }}
                          >
                            <Trash2 />
                          </Button>
                        </Tip>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
          {idxDlg && (
            <IndexDialog
              open
              onOpenChange={(v) => { if (!v) setIdxDlg(false) }}
              columns={colNames}
              onSubmit={(iv) => p.onAlter({
                op: 'create_index',
                index: iv.name || undefined,
                unique: iv.unique,
                method: iv.method,
                columns: iv.keys,
                include: iv.include.length ? iv.include : undefined,
                where: iv.where || undefined,
              })}
            />
          )}
        </TabsContent>
        <TabsContent value="constraints" className="min-h-0 flex-1 overflow-auto">
          <div className="mb-2 flex items-center gap-1.5">
            <span className="text-[12px] text-muted-foreground">{t.constraints ? `${t.constraints.length} constraints` : ''}</span>
            <span className="flex-1" />
            <Button size="sm" variant="secondary" onClick={() => setConDlg(true)}>
              <Plus /> Constraint
            </Button>
          </div>
          {t.constraints ? (
            <Table containerClassName="min-h-0 max-h-full">
              <TableHeader>
                <TableRow>
                  <TableHead>name</TableHead>
                  <TableHead>type</TableHead>
                  <TableHead>definition</TableHead>
                  <TableHead className="w-[52px]">actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {t.constraints.map((x, i) => (
                  <TableRow key={i}>
                    <TableCell>{x.name}</TableCell>
                    <TableCell>{x.type}</TableCell>
                    <TableCell className="font-mono">{x.def}</TableCell>
                    <TableCell>
                      <Tip content={`Drop constraint ${x.name}`}>
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`Drop ${x.name}`}
                          onClick={async () => {
                            const ok = await p.dialogs.confirm({
                              title: `Drop constraint ${x.name}?`,
                              description: 'This cannot be undone.',
                              confirmText: 'Drop',
                              danger: true,
                            })
                            if (ok) p.onAlter({ op: 'drop_constraint', constraint: x.name })
                          }}
                        >
                          <Trash2 />
                        </Button>
                      </Tip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
          {conDlg && (
            <ConstraintDialog
              open
              onOpenChange={(v) => { if (!v) setConDlg(false) }}
              onSubmit={(cv) => p.onAlter({ op: 'add_constraint', constraint: cv.name || undefined, def: cv.def })}
            />
          )}
        </TabsContent>
        <TabsContent value="triggers" className="min-h-0 flex-1 overflow-auto">
          <div className="mb-2 flex items-center gap-1.5">
            <span className="text-[12px] text-muted-foreground">{t.triggers ? `${t.triggers.length} triggers` : ''}</span>
            <span className="flex-1" />
            <Button size="sm" variant="secondary" onClick={() => setTrgDlg(true)}>
              <Plus /> Trigger
            </Button>
          </div>
          {t.triggers ? (
            <Table containerClassName="min-h-0 max-h-full">
              <TableHeader>
                <TableRow>
                  {['name', 'table', 'event', 'timing', 'statement'].map((c) => <TableHead key={c}>{c}</TableHead>)}
                  <TableHead>status</TableHead>
                  <TableHead className="w-[96px]">actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {t.triggers.map((x, i) => {
                  const disabled = (x.enabled ?? 'O') === 'D'
                  return (
                    <TableRow key={i}>
                      <TableCell className="max-w-[200px] truncate">{x.name}</TableCell>
                      <TableCell>{x.table}</TableCell>
                      <TableCell>{x.event}</TableCell>
                      <TableCell>{x.timing}</TableCell>
                      <TableCell className="max-w-[320px] truncate font-mono">{x.statement}</TableCell>
                      <TableCell>
                        <Badge variant={disabled ? 'secondary' : 'success'}>{disabled ? 'disabled' : 'enabled'}</Badge>
                      </TableCell>
                      <TableCell>
                        <div className="flex gap-1">
                          <Tip content={disabled ? `Enable trigger ${x.name}` : `Disable trigger ${x.name}`}>
                            <span className="inline-flex h-7 items-center px-2">
                              <Switch
                                checked={!disabled}
                                onCheckedChange={() => p.onAlter({ op: disabled ? 'enable_trigger' : 'disable_trigger', trigger: x.name })}
                                aria-label={`${disabled ? 'Enable' : 'Disable'} ${x.name}`}
                              />
                            </span>
                          </Tip>
                          <Tip content={`Drop trigger ${x.name}`}>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={`Drop ${x.name}`}
                              onClick={async () => {
                                const ok = await p.dialogs.confirm({
                                  title: `Drop trigger ${x.name}?`,
                                  description: 'This cannot be undone.',
                                  confirmText: 'Drop',
                                  danger: true,
                                })
                                if (ok) p.onAlter({ op: 'drop_trigger', trigger: x.name })
                              }}
                            >
                              <Trash2 />
                            </Button>
                          </Tip>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
          {trgDlg && (
            <TriggerDialog
              open
              onOpenChange={(v) => { if (!v) setTrgDlg(false) }}
              columns={colNames}
              onSubmit={(tv) => p.onAlter({
                op: 'create_trigger',
                trigger: tv.name,
                timing: tv.timing,
                events: tv.events,
                for_each: tv.forEach,
                function: tv.function,
                update_of: tv.updateOf.length ? tv.updateOf : undefined,
                when: tv.when || undefined,
              })}
            />
          )}
        </TabsContent>
        <TabsContent value="stats" className="min-h-0 flex-1 overflow-auto">
          {t.stats ? (
            <div className="flex flex-col gap-2">
              <Card className="p-2.5">
                {Object.entries(t.stats).map(([k, v]) => (
                  <div key={k} className="flex justify-between border-b border-dashed py-1 text-[12px] last:border-0">
                    <span className="text-muted-foreground">{k}</span>
                    <b>{String(v)}</b>
                  </div>
                ))}
              </Card>
              <div className="flex gap-1.5">
                <Button size="sm" variant="secondary" onClick={() => p.onMaintenance('vacuum')}>
                  VACUUM
                </Button>
                <Button size="sm" variant="secondary" onClick={() => p.onMaintenance('analyze')}>
                  ANALYZE
                </Button>
                <Button size="sm" variant="secondary" onClick={() => p.onMaintenance('reindex')}>
                  REINDEX
                </Button>
                <Button size="sm" variant="ghost" onClick={p.onOpenErd}>
                  ERD
                </Button>
              </div>
            </div>
          ) : (
            <EmptyNote text="Loading stats…" />
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}
