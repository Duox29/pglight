import { useState } from 'react'
import { Play, FileDown, Braces, Sparkles, Star, Wand2 } from 'lucide-react'
import { Button } from './ui/button'
import { Textarea } from './ui/textarea'
import { Badge } from './ui/badge'
import { Card } from './ui/card'
import { DataGrid } from './ui/data-grid'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from './ui/resizable'
import { ErrorText } from './ui/feedback'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import type { QueryTabT } from '@/types'
import type { DialogsApi } from './dialogs'
import { download, formatSqlText, resultToCSV, resultToInserts, resultToJSON, fmtPlanText } from '@/lib/format'

interface Props {
  tab: QueryTabT
  inTxn: boolean
  running: boolean
  onSqlChange: (sql: string) => void
  onRun: () => void
  onExplain: (analyze: boolean) => void
  onLimit: (n: number) => void
  onSaveSnippet: () => void
  dialogs: DialogsApi
}

export function QueryConsole(p: Props) {
  const { tab: t } = p
  const [sortIdx, setSortIdx] = useState<Record<number, boolean>>({})

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
      if (name) download(resultToInserts(r.columns, r.rows, name), 'result.sql', 'text/sql')
    }
  }

  return (
    <ResizablePanelGroup direction="vertical" autoSaveId="pglight-query-split" className="flex h-full min-h-0 flex-col">
      <ResizablePanel defaultSize={30} minSize={12} className="min-h-0">
        <div className="flex h-full min-h-0 flex-col gap-2 pb-2">
      <Textarea
        value={t.sql}
        spellCheck={false}
        className="min-h-0 flex-1 resize-none font-mono"
        onChange={(e) => p.onSqlChange(e.target.value)}
        onKeyDown={(e) => {
          if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
            e.preventDefault()
            p.onRun()
          }
        }}
      />
      <div className="flex flex-wrap items-center gap-1.5">
        <Button size="sm" onClick={p.onRun} disabled={p.running}>
          <Play /> Run (Ctrl+Enter)
        </Button>
        <Button size="sm" variant="secondary" onClick={() => p.onExplain(false)}>
          Explain
        </Button>
        <Button size="sm" variant="secondary" onClick={() => p.onExplain(true)}>
          Analyze
        </Button>
        <Button size="sm" variant="ghost" onClick={() => p.onSqlChange(formatSqlText(t.sql))}>
          <Wand2 /> Format
        </Button>
        <Button size="sm" variant="ghost" onClick={p.onSaveSnippet}>
          <Star /> Snippet
        </Button>
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
        <Button size="sm" variant="ghost" onClick={() => exportAs('csv')}>
          <FileDown /> CSV
        </Button>
        <Button size="sm" variant="ghost" onClick={() => exportAs('json')}>
          <Braces /> JSON
        </Button>
        <Button size="sm" variant="ghost" onClick={() => exportAs('sql')}>
          INSERTs
        </Button>
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
                data={{ columns: res.columns, rows: res.rows }}
                onSort={(i) => {
                  const sorted = [...res.rows].sort((a, b) => String(a[i] ?? '').localeCompare(String(b[i] ?? '')))
                  res.rows = sorted
                  setSortIdx({ ...sortIdx, [ri]: !sortIdx[ri] })
                }}
                onCellClick={(v) => {
                  if (v != null && navigator.clipboard) navigator.clipboard.writeText(String(v))
                }}
              />
              <div className="mt-1.5 flex items-center gap-1.5">
                <span className="text-[12px] text-muted-foreground">{res.rows.length} rows</span>
                <span className="flex-1" />
                <Button size="sm" variant="ghost" onClick={() => exportAs('csv', ri)}>
                  CSV
                </Button>
                <Button size="sm" variant="ghost" onClick={() => exportAs('json', ri)}>
                  JSON
                </Button>
                <Button size="sm" variant="ghost" onClick={() => exportAs('sql', ri)}>
                  INSERTs
                </Button>
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
