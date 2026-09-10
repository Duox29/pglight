import { useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, FileDown, Plus, RefreshCw, Upload, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './ui/table'
import { ErrorText, EmptyNote } from './ui/feedback'
import type { TableSubtab, TableTabT } from '@/types'
import type { DialogsApi } from './dialogs'
import { download, parseCSV, resultToCSV, resultToInserts } from '@/lib/format'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from './ui/context-menu'

interface Props {
  tab: TableTabT
  inTxn: boolean
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
  dialogs: DialogsApi
}

export function TableWorkspace(p: Props) {
  const { tab: t } = p
  const fileRef = useRef<HTMLInputElement>(null)
  // Bulk row selection, keyed by serialized row content so it survives
  // reloads/paging. Identical duplicate rows share one key (known limit).
  const [sel, setSel] = useState<Set<string>>(new Set())
  const anchor = useRef(0)
  const pageRows = t.result?.rows ?? []
  const pageCols = t.result?.columns ?? []
  const rowKey = (r: unknown[]) => JSON.stringify(r)
  const selRecs = pageRows.filter((r) => sel.has(rowKey(r))).map((r) => Object.fromEntries(pageCols.map((c, i) => [c, r[i]])))
  const toggleRow = (ri: number, r: unknown[]) => {
    const k = rowKey(r)
    setSel((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })
    anchor.current = ri
  }
  const rangeTo = (ri: number) => {
    const [a, b] = anchor.current < ri ? [anchor.current, ri] : [ri, anchor.current]
    setSel((prev) => {
      const next = new Set(prev)
      for (let i = a; i <= b; i++) if (pageRows[i]) next.add(rowKey(pageRows[i]))
      return next
    })
  }

  const startImport = () => fileRef.current?.click()

  const handleFile = async (file: File) => {
    const text = await file.text()
    const rows = parseCSV(text)
    if (!rows.length) {
      toast.error('Empty file')
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
      description: `Import ${data.length} rows into ${t.schema}.${t.table} (${cols.join(', ')})?`,
      confirmText: 'Import',
    })
    if (!ok) return
    p.onImport(cols, data)
  }

  const copyDdl = () => {
    if (t.ddl?.ddl && navigator.clipboard) navigator.clipboard.writeText(t.ddl.ddl)
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-sm font-semibold">
          {t.schema}.{t.table}
        </span>
        {p.inTxn && <Badge variant="warning">IN TXN</Badge>}
        <span className="flex-1" />
        <Tabs value={t.subtab} onValueChange={(v) => p.onSubtab(v as TableSubtab)}>
          <TabsList>
            {(['data', 'columns', 'ddl', 'indexes', 'constraints', 'triggers', 'stats'] as TableSubtab[]).map((s) => (
              <TabsTrigger key={s} value={s} className="capitalize">
                {s}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      {t.subtab === 'data' && (
        <div className="flex flex-col gap-2">
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
              onClick={() => t.result && download(resultToInserts(t.result.columns, t.result.rows, `${t.schema}.${t.table}`), `${t.table}.sql`, 'text/sql')}
            >
              INSERTs
            </Button>
            <Button size="sm" variant="ghost" onClick={startImport}>
              <Upload /> Import CSV
            </Button>
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
            <Button size="sm" variant="ghost" onClick={() => p.onPage(-1)}>
              <ArrowLeft />
            </Button>
            <span className="text-[12px] text-muted-foreground">offset {t.offset}</span>
            <Button size="sm" variant="ghost" onClick={() => p.onPage(1)}>
              <ArrowRight />
            </Button>
            <span className="text-[12px] text-muted-foreground">
              {t.result ? `${t.result.rows.length} rows${t.result.total != null ? ` / ${t.result.total} total` : ''} · ${t.result.duration_ms}ms` : ''}
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
            <Button size="sm" variant="ghost" onClick={p.onApply} title="Reload">
              <RefreshCw />
            </Button>
          </div>
          <ErrorText message={t.error} />
          {t.result ? (
            <ContextMenu>
              <ContextMenuTrigger asChild>
                <Table>
                  <TableHeader>
                    <TableRow>
                      {t.result.columns.map((c) => (
                        <TableHead key={c}>{c}</TableHead>
                      ))}
                      <TableHead>✎</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {t.result.rows.map((r, ri) => {
                      const orig = Object.fromEntries(t.result!.columns.map((c, i) => [c, r[i]]))
                      const k = rowKey(r)
                      return (
                        <TableRow
                          key={ri}
                          data-state={sel.has(k) ? 'selected' : undefined}
                          onContextMenu={() => {
                            if (!sel.has(k)) {
                              setSel(new Set([k]))
                              anchor.current = ri
                            }
                          }}
                        >
                          {r.map((c, ci) => (
                            <TableCell
                              key={ci}
                              title="Click to copy · Ctrl-click to select · Shift-click for range · Right-click for menu"
                              className="cursor-text bg-sky-950/30"
                              onDoubleClick={() => p.onEditCell(t.result!.columns[ci], orig)}
                              onClick={(e) => {
                                if (e.ctrlKey || e.metaKey) {
                                  toggleRow(ri, r)
                                  return
                                }
                                if (e.shiftKey) {
                                  rangeTo(ri)
                                  return
                                }
                                if (c != null && navigator.clipboard) navigator.clipboard.writeText(String(c))
                              }}
                            >
                              {c == null ? <span className="italic text-muted-foreground">NULL</span> : String(c).slice(0, 200)}
                            </TableCell>
                          ))}
                          <TableCell>
                            <div className="flex gap-1">
                              <Button size="sm" variant="ghost" title="Copy row as INSERT" onClick={() => p.onCopyInsert(orig)}>
                                ⧉
                              </Button>
                              <Button size="sm" variant="ghost" onClick={() => p.onDeleteRow(orig)}>
                                del
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </ContextMenuTrigger>
              <ContextMenuContent>
                <ContextMenuLabel>{selRecs.length ? `${selRecs.length} row${selRecs.length === 1 ? '' : 's'} selected` : 'No rows selected'}</ContextMenuLabel>
                <ContextMenuSub>
                  <ContextMenuSubTrigger disabled={!selRecs.length}>Export</ContextMenuSubTrigger>
                  <ContextMenuSubContent>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'csv')}>CSV</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'json')}>JSON</ContextMenuItem>
                    <ContextMenuItem onSelect={() => p.onExportRows(selRecs, 'sql')}>INSERTs</ContextMenuItem>
                  </ContextMenuSubContent>
                </ContextMenuSub>
                <ContextMenuItem disabled={!selRecs.length} onSelect={() => p.onCopyRows(selRecs)}>
                  Copy
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem disabled={!selRecs.length} className="text-red-400 focus:text-red-400" onSelect={() => p.onDeleteRows(selRecs)}>
                  Delete
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          ) : (
            <EmptyNote text={t.error ? 'Failed to load' : 'Loading…'} />
          )}
        </div>
      )}

      <Tabs value={t.subtab}>
        <TabsContent value="columns">
          {t.cols ? (
            <Table>
              <TableHeader>
                <TableRow>{['name', 'type', 'nullable', 'default', 'pk', 'comment'].map((c) => <TableHead key={c}>{c}</TableHead>)}</TableRow>
              </TableHeader>
              <TableBody>
                {t.cols.map((c, i) => (
                  <TableRow key={i}>
                    {['name', 'type', 'nullable', 'default', 'pk', 'comment'].map((k) => (
                      <TableCell key={k}>{c[k] == null ? '' : String(c[k])}</TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading columns…" />
          )}
        </TabsContent>
        <TabsContent value="ddl">
          {t.ddl ? (
            <div className="flex flex-col gap-2">
              <div className="text-[12px] text-muted-foreground">
                owner: {t.ddl.owner} {t.ddl.comment}
              </div>
              <Card className="p-2.5">
                <div className="mb-1 flex items-center gap-2">
                  <span className="text-[12px] font-semibold">CREATE TABLE</span>
                  <Button size="sm" variant="ghost" onClick={copyDdl}>
                    <Copy /> Copy
                  </Button>
                </div>
                <pre className="overflow-auto rounded bg-muted p-2 font-mono text-[12px]">{t.ddl.ddl}</pre>
              </Card>
              <Card className="p-2.5">
                <div className="mb-1 text-[12px] font-semibold">Constraints</div>
                <pre className="overflow-auto font-mono text-[12px]">{(t.ddl.constraints ?? []).map((c) => c.def).join('\n') || '—'}</pre>
              </Card>
              <Card className="p-2.5">
                <div className="mb-1 text-[12px] font-semibold">Indexes</div>
                <pre className="overflow-auto font-mono text-[12px]">{(t.ddl.indexes ?? []).map((c) => c.def).join('\n') || '—'}</pre>
              </Card>
            </div>
          ) : (
            <EmptyNote text="Loading DDL…" />
          )}
        </TabsContent>
        <TabsContent value="indexes">
          {t.ddl ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>name</TableHead>
                  <TableHead>definition</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(t.ddl.indexes ?? []).map((x, i) => (
                  <TableRow key={i}>
                    <TableCell>{x.name}</TableCell>
                    <TableCell className="font-mono">{x.def}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
        </TabsContent>
        <TabsContent value="constraints">
          {t.constraints ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>name</TableHead>
                  <TableHead>type</TableHead>
                  <TableHead>definition</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {t.constraints.map((x, i) => (
                  <TableRow key={i}>
                    <TableCell>{x.name}</TableCell>
                    <TableCell>{x.type}</TableCell>
                    <TableCell className="font-mono">{x.def}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
        </TabsContent>
        <TabsContent value="triggers">
          {t.triggers ? (
            <Table>
              <TableHeader>
                <TableRow>
                  {['name', 'table', 'event', 'timing', 'statement'].map((c) => <TableHead key={c}>{c}</TableHead>)}
                </TableRow>
              </TableHeader>
              <TableBody>
                {t.triggers.map((x, i) => (
                  <TableRow key={i}>
                    <TableCell>{x.name}</TableCell>
                    <TableCell>{x.table}</TableCell>
                    <TableCell>{x.event}</TableCell>
                    <TableCell>{x.timing}</TableCell>
                    <TableCell className="font-mono">{x.statement}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyNote text="Loading…" />
          )}
        </TabsContent>
        <TabsContent value="stats">
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
