import { useRef, useState } from 'react'
import { Play, FileDown, RefreshCw, Sparkles, Square, Star, Trash2, Wand2, ChevronDown } from 'lucide-react'
import { Button } from './ui/button'
import { Tip } from './ui/tooltip'
import { SqlEditor, type SqlEditorHandle } from './SqlEditor'
import { ensureSnapshot } from '@/lib/schemaCache'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { DataGrid, sortGridRows } from './ui/data-grid'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './ui/resizable'
import { ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './ui/dropdown-menu'
import type { QueryTabT } from '@/types'
import type { DialogsApi } from './dialogs'
import { download, formatSqlText, quoteQualified, resultToCSV, resultToInserts, resultToJSON, fmtPlanText } from '@/lib/format'

interface Props {
  tab: QueryTabT
  inTxn: boolean
  running: boolean
  onSqlChange: (sql: string) => void
  onRun: (sql?: string) => void
  onClearResults: () => void
  onCancel: () => void
  onExplain: (analyze: boolean) => void
  onLimit: (n: number) => void
  onSaveSnippet: () => void
  dialogs: DialogsApi
}

export function QueryConsole(p: Props) {
  const { tab: t } = p
  const [sort, setSort] = useState<{ column: number; direction: 'asc' | 'desc' } | null>(null)
  const editorHandle = useRef<SqlEditorHandle | null>(null)
  // Run the highlighted selection when present, else the whole script.
  const runSelected = () => {
    const sel = editorHandle.current?.getSelection() ?? ''
    p.onRun(sel.trim() ? sel : undefined)
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
      const table = rest.length ? rest.join('.') : schema
      const qualified = rest.length ? quoteQualified(schema.trim(), table.trim()) : quoteQualified('', table.trim())
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
        onCtrlEnter={runSelected}
        handleRef={editorHandle}
      />
      <div className="flex flex-wrap items-center gap-1.5">
        {/* PRIMARY: the one action that matters */}
        <Tip content="Run selection if any, else whole script (Ctrl+Enter)">
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
        {/* QUERY: plan + shape the statement */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="secondary">
              Explain <ChevronDown />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            <DropdownMenuItem onSelect={() => p.onExplain(false)}>Explain</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => p.onExplain(true)}>Explain Analyze</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button size="sm" variant="ghost" onClick={() => p.onSqlChange(formatSqlText(t.sql))}>
          <Wand2 /> Format
        </Button>
        <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
        {/* RESULT: size + export the output */}
        <Select value={String(t.limit)} onValueChange={(v) => p.onLimit(Number(v))}>
          <SelectTrigger className="w-[100px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="200">200 rows</SelectItem>
            <SelectItem value="1000">1000 rows</SelectItem>
            <SelectItem value="0">no limit</SelectItem>
          </SelectContent>
        </Select>
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
        {p.inTxn && <Badge variant="warning">IN TXN</Badge>}
      </div>
        </div>
      </ResizablePanel>
      <ResizableHandle withHandle />
      <ResizablePanel defaultSize={70} minSize={12} className="min-h-0">
        <div className="h-full overflow-auto pt-2">
          <div className="flex flex-col gap-2">
      <ErrorText message={t.error} />
      {t.plan && (
        <Card className="whitespace-pre-wrap p-2.5 font-mono text-[12px]">{t.plan}</Card>
      )}
      {(t.results ?? []).map((res, ri) => ({ res, ri })).reverse().map(({ res, ri }) => (
        <Card key={ri} className="p-2.5">
          {(t.results?.length ?? 0) > 1 && (
            <div className="mb-1.5 text-[12px] text-muted-foreground">
              — result {ri + 1} · {res.rows?.length ?? 0} rows · {res.duration_ms ?? 0}ms
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
                onCellClick={(v) => {
                  if (v != null && navigator.clipboard) navigator.clipboard.writeText(String(v))
                }}
              />
              <div className="mt-1.5 flex items-center gap-2 text-[12px] text-muted-foreground">
                <span className="font-medium text-foreground">{res.rows.length} row{res.rows.length === 1 ? '' : 's'}</span>
                <span>·</span>
                <span>{res.duration_ms ?? 0}ms</span>
                <span>·</span>
                <span>click a cell to copy · click a header to sort</span>
              </div>
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
