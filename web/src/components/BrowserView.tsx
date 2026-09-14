import { RefreshCw } from 'lucide-react'
import { Button } from './ui/button'
import { DataGrid } from './ui/data-grid'
import { EmptyNote, ErrorText } from './ui/feedback'
import type { BrowserTabT } from '@/types'

export function BrowserView(props: { tab: BrowserTabT; onReload: () => void }) {
  const { tab: t } = props
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1.5">
        <b className="text-sm">{t.title}</b>
        <span className="flex-1" />
        <Button size="sm" variant="secondary" onClick={props.onReload} aria-label="Reload">
          <RefreshCw className="h-3.5 w-3.5" />
        </Button>
      </div>
      <ErrorText message={t.error} />
      {t.rows ? (
        <DataGrid data={{ columns: t.cols, rows: t.rows.map((r) => t.cols.map((c) => r[c])) }} />
      ) : (
        <EmptyNote text={t.error ? 'Failed to load' : 'Loading…'} />
      )}
    </div>
  )
}
