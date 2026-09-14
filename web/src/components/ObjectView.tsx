import { Copy, FileText, RefreshCw } from 'lucide-react'
import { Button } from './ui/button'
import { Card, CardContent, CardHeader, CardTitle } from './ui/card'
import { Table, TableBody, TableCell, TableRow } from './ui/table'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { ObjectTabT } from '@/types'

interface Props {
  tab: ObjectTabT
  onReload: () => void
  onCopy: (text: string) => void
  onOpenQuery: () => void
}

function fmt(v: unknown): string {
  if (v == null) return '—'
  if (typeof v === 'string') return v === '' ? '—' : v
  return String(v)
}

export function ObjectView(props: Props) {
  const { tab: t } = props
  const entries = Object.entries(t.details ?? {}).filter(([k]) => k !== 'def' && k !== 'definition')
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <b className="text-sm">{t.title}</b>
        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
          {t.objectKind} · {t.schema}
        </span>
        <span className="flex-1" />
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
          {t.def != null && (
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
