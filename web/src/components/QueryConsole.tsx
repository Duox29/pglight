import { useEffect, useRef, useState } from 'react'
import { Play, FileDown, RefreshCw, Sparkles, Square, Star, Trash2, Wand2, LocateFixed } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { SqlEditor, type SqlEditorHandle } from './SqlEditor'
import { ensureSnapshot } from '@/lib/schemaCache'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { DataGrid, sortGridRows } from './ui/data-grid'
import { TxnControls } from './TxnControls'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './ui/resizable'
import { ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './ui/dropdown-menu'
import type { QueryTabT } from '@/types'
import type { DialogsApi } from './dialogs'
import { download, formatSqlText, quoteQualified, resultToCSV, resultToInserts, resultToJSON, fmtPlanText } from '@/lib/format'
import { useCommand } from '@/shortcuts/ShortcutProvider'
import { shortcutForDisplay } from '@/shortcuts/normalize'

interface Props {
  tab: QueryTabT
  inTxn: boolean
  running: boolean
  autocommit: boolean
  connLabel: string
  connTip: string
  onAutocommit: (v: boolean) => void
  onTxn: (action: string) => void
  onSqlChange: (sql: string) => void
  onRun: (sql?: string) => void
  onClearResults: () => void
  onCancel: () => void
  onExplain: (sql?: string) => void
  onLimit: (n: number) => void
  onSaveSnippet: () => void
  dialogs: DialogsApi
}

export function QueryConsole(p: Props) {
  const { tab: t } = p
  const commands = useCommand()
  const [sort, setSort] = useState<{ column: number; direction: 'asc' | 'desc' } | null>(null)
  const editorHandle = useRef<SqlEditorHandle | null>(null)
  // Error line: jump caret + blink 3×, then keep a steady tint until the
  // next run/clear/tab switch. flashTick re-fires the blink when the same
  // line fails twice in a row.
  const jumpToError = () => {
    if (t.errLoc?.line) editorHandle.current?.flashErrorLine(t.errLoc.line, t.errLoc.column)
  }
  useEffect(() => {
    if (t.error && t.errLoc?.line) jumpToError()
    else if (!t.error) editorHandle.current?.clearErrorFlash()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t.error, t.errLoc?.line, t.errLoc?.column, t.flashTick])
  // Run the highlighted selection when present, else the whole script.
  const runSelected = () => {
    const sel = editorHandle.current?.getSelection() ?? ''
    p.onRun(sel.trim() ? sel : undefined)
  }
  const explainSelected = () => {
    const sel = editorHandle.current?.getSelection() ?? ''
    p.onExplain(sel.trim() ? sel : undefined)
  }
  const exportAs = async (fmt: 'csv' | 'json' | 'sql', ri = 0) => {
    const r = t.results?.[ri]
    if (!r) return
    if (fmt === 'csv') download(resultToCSV(r.columns, r.rows), 'result.csv', 'text/csv')
    else if (fmt === 'json') download(resultToJSON(r.columns, r.rows), 'result.json', 'application/json')
    else {
      const name = await p.dialogs.prompt({
        title: 'Export INSERTs',
        placeholder: 'schema.table',
        defaultValue: 'public.my_table',
      })
      if (!name) return
      const [schema, ...rest] = name.split('.')
      const table = rest.length ? rest.join('.') : (schema ?? '')
      const qualified = rest.length ? quoteQualified((schema ?? '').trim(), table.trim()) : quoteQualified('', table.trim())
      download(resultToInserts(r.columns, r.rows, qualified, r.types), 'result.sql', 'text/sql')
    }
  }

  return (
    <ResizablePanelGroup direction="vertical" autoSaveId="pglight-query-split" className="flex h-full min-h-0 flex-col">
      <ResizablePanel defaultSize={30} minSize={12} className="min-h-0">
        <div className="flex h-full min-h-0 flex-col gap-2 pb-2">
      <SqlEditor
        value={t.sql}
        session={t.sessionId}
        onChange={p.onSqlChange}
        onCommand={(id) => {
          if (id === 'query.run') runSelected()
          if (id === 'query.explain') explainSelected()
          if (id === 'query.complete') editorHandle.current?.startCompletion()
        }}
        handleRef={editorHandle}
      />
      <div className="flex flex-wrap items-center gap-1.5">
        {/* PRIMARY: the one action that matters */}
        <Tip content={`Run selection if any, else whole script (${shortcutForDisplay(commands.formatBinding('query.run')[0] ?? 'Mod+Enter')})`}>
          <span className="inline-flex">
            <Button size="sm" onClick={runSelected} disabled={p.running}>
              <Play /> Run
            </Button>
          </span>
        </Tip>
        <Tip content="Cancel running query">
          <span className="inline-flex">
            <Button size="sm" variant="outline" onClick={p.onCancel} disabled={!p.running} aria-label="Cancel running query">
              <Square />
            </Button>
          </span>
        </Tip>
        <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
        {/* TXN: session state lives with Run, not in a separate bar */}
        <TxnControls
          connLabel={p.connLabel}
          connTip={p.connTip}
          inTxn={p.inTxn}
          autocommit={p.autocommit}
          onAutocommit={p.onAutocommit}
          onTxn={p.onTxn}
        />
        {/* QUERY: plan + shape the statement */}
        <Tip content="Explain Analyze the selected SQL, or the whole script when nothing is selected">
          <span className="inline-flex">
            <Button size="sm" variant="ghost" onClick={explainSelected} disabled={p.running}>
              Explain
            </Button>
          </span>
        </Tip>
        <Button size="sm" variant="ghost" onClick={() => p.onSqlChange(formatSqlText(t.sql))}>
          <Wand2 /> Format
        </Button>
        <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
        {/* RESULT: size + export the output */}
        <Tip content={t.limit === 0 ? 'Return all rows up to the 64 MiB safety budget' : 'Maximum rows returned for each SELECT'}>
          <span className="inline-flex">
            <Select value={String(t.limit)} onValueChange={(v) => p.onLimit(Number(v))}>
              <SelectTrigger className="w-[112px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="200">200 rows</SelectItem>
                <SelectItem value="1000">1,000 rows</SelectItem>
                <SelectItem value="5000">5,000 rows</SelectItem>
                <SelectItem value="10000">10,000 rows</SelectItem>
                <SelectItem value="0">No limit</SelectItem>
              </SelectContent>
            </Select>
          </span>
        </Tip>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="ghost" disabled={!t.results?.[0]?.columns?.length}>
              <FileDown /> Export
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onSelect={() => exportAs('csv')}>CSV</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => exportAs('json')}>JSON</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => exportAs('sql')}>INSERTs</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
        {/* UTILITY: housekeeping */}
        <Button size="sm" variant="ghost" onClick={p.onSaveSnippet}>
          <Star /> Snippet
        </Button>
        <Tip content="Refresh autocomplete schema (cached 60s, auto-invalidated on DDL)">
          <span className="inline-flex">
            <Button size="sm" variant="ghost" onClick={() => ensureSnapshot(t.sessionId, true)} aria-label="Refresh autocomplete schema">
              <RefreshCw />
            </Button>
          </span>
        </Tip>
        <Tip content="Clear results">
          <span className="inline-flex">
            <Button size="sm" variant="ghost" onClick={p.onClearResults} disabled={!t.results && !t.error && !t.plan} aria-label="Clear results">
              <Trash2 />
            </Button>
          </span>
        </Tip>
        <span className="text-[12px] text-muted-foreground">{t.meta}</span>
        {t.results?.[0]?.stale && <Badge variant="secondary">Snapshot from last session — Run to refresh</Badge>}
      </div>
        </div>
      </ResizablePanel>
      <ResizableHandle withHandle />
      <ResizablePanel defaultSize={70} minSize={12} className="min-h-0">
        <div className="h-full overflow-auto pt-2">
          <div className="flex flex-col gap-2">
            {(t.error || t.errLoc?.line != null || t.errLoc?.statement_index != null || t.errLoc?.code) && (
              <Card className="border-red-500/40 p-2.5">
                {t.error && <ErrorText message={t.error} />}
                <div className="mt-1 flex flex-wrap items-center gap-2 text-[12px] text-muted-foreground">
                  {t.errLoc?.line != null && (
                    <Tip content="Jump to the failing line in the editor">
                      <button type="button" onClick={jumpToError} className="inline-flex items-center gap-1 font-medium text-foreground underline-offset-2 hover:underline">
                        <LocateFixed className="h-3.5 w-3.5" />
                        Line {t.errLoc.line}
                        {t.errLoc.column != null ? `, col ${t.errLoc.column}` : ''}
                      </button>
                    </Tip>
                  )}
                  {t.errLoc?.statement_index != null && <span>statement {t.errLoc.statement_index + 1}</span>}
                  {t.errLoc?.code && <Badge variant="secondary">{t.errLoc.code}</Badge>}
                </div>
              </Card>
            )}
            {t.plan && <Card className="whitespace-pre-wrap p-2.5 font-mono text-[12px]">{t.plan}</Card>}
            {(t.results ?? []).map((res, ri) => ({ res, ri })).reverse().map(({ res, ri }) => (
              <Card key={ri} className={t.error && t.errLoc?.statement_index === ri ? 'border-red-500/40 p-2.5' : 'p-2.5'}>
                {(t.results?.length ?? 0) > 1 && (
                  <div className="mb-1.5 text-[12px] text-muted-foreground">
                    — result {ri + 1} · {res.rows?.length ?? 0} rows · {res.duration_ms ?? 0}ms
                    {t.error && t.errLoc?.statement_index === ri && <span className="ml-1 font-medium text-red-400">· failed</span>}
                    {res.statement && <pre className="mt-1 max-h-16 overflow-auto rounded bg-muted p-1.5">{res.statement.slice(0, 300)}</pre>}
                  </div>
                )}
                {!res.columns?.length ? (
                  <div className="text-[12px] text-muted-foreground">OK · {res.rows_affected ?? 0} rows affected</div>
                ) : (
                  <>
                    <DataGrid
                      data={{ columns: res.columns, types: res.types, rows: sort == null ? res.rows : sortGridRows(res.rows, sort, res.types) }}
                      sort={sort}
                      onSort={(i) => {
                        setSort((prev) => (prev?.column === i ? { column: i, direction: prev.direction === 'asc' ? 'desc' : 'asc' } : { column: i, direction: 'asc' }))
                      }}
                      selectable
                    />
                  </>
                )}
              </Card>
            ))}
      {t.results && t.results.length === 0 && <Sparkles className="h-4 w-4 text-muted-foreground" />}
          </div>
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}

export function explainToText(raw: unknown): string {
  try {
    const arr = raw as { Plan: Record<string, unknown>; 'Planning Time'?: number; 'Execution Time'?: number; 'Shared Hit Blocks'?: number; 'Shared Read Blocks'?: number }[]
    const plan = fmtPlanText(arr[0].Plan, 0)
    return `${plan}\nPlanning: ${arr[0]['Planning Time']}ms${arr[0]['Execution Time'] != null ? ` · Exec: ${arr[0]['Execution Time']}ms` : ''}${
      arr[0]['Shared Hit Blocks'] != null ? ` · Hit: ${arr[0]['Shared Hit Blocks']} Read: ${arr[0]['Shared Read Blocks']}` : ''
    }`
  } catch {
    return JSON.stringify(raw, null, 2)
  }
}
