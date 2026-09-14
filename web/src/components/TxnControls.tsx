import { Check, Play, RotateCcw } from 'lucide-react'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Switch } from './ui/switch'
import { Tip } from './ui/tooltip'

/** Session txn controls. Rendered inside the Run toolbar (query) and the table header — no separate bar. */
export function TxnControls(p: {
  connLabel: string
  connTip: string
  inTxn: boolean
  autocommit: boolean
  onAutocommit: (v: boolean) => void
  onTxn: (action: string) => void
}) {
  return (
    <>
      <Tip content={p.connTip}>
        <span className="max-w-[180px] truncate text-[12px] font-semibold">{p.connLabel}</span>
      </Tip>
      <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
      <label className="flex cursor-pointer items-center gap-1 text-[12px] text-muted-foreground">
        <Switch checked={p.autocommit} onCheckedChange={p.onAutocommit} aria-label="Autocommit" />
        autocommit
      </label>
      <Tip content="Begin transaction">
        <span className="inline-flex">
          <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Begin transaction" onClick={() => p.onTxn('begin')} disabled={p.inTxn}>
            <Play />
          </Button>
        </span>
      </Tip>
      <Tip content="Commit transaction">
        <span className="inline-flex">
          <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Commit transaction" onClick={() => p.onTxn('commit')} disabled={!p.inTxn}>
            <Check />
          </Button>
        </span>
      </Tip>
      <Tip content="Rollback transaction">
        <span className="inline-flex">
          <Button size="icon" variant="ghost" className="h-7 w-7" aria-label="Rollback transaction" onClick={() => p.onTxn('rollback')} disabled={!p.inTxn}>
            <RotateCcw />
          </Button>
        </span>
      </Tip>
      <Badge variant="outline" className="gap-1.5 font-normal">
        <span className={p.inTxn ? 'h-2 w-2 rounded-full bg-amber-500' : 'h-2 w-2 rounded-full bg-emerald-500'} aria-hidden />
        {p.inTxn ? 'open transaction' : 'no transaction'}
      </Badge>
      <span className="mx-0.5 h-5 w-px bg-border" aria-hidden />
    </>
  )
}
